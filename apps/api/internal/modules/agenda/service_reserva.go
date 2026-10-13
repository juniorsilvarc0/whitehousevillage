package agenda

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	regra "github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/agenda"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// A agenda que se enche sozinha (F2-20): o que cada passo da reserva faz com
// os eventos dela.
//
// `Ligacao` implementa `reservas.AgendaDaReserva`, a interface que o módulo de
// reservas DECLARA e chama — a agenda não conhece a reserva por dentro, e a
// reserva não conhece a agenda por dentro. Cada método roda NA TRANSAÇÃO da
// reserva (a do contexto): ou o passo da reserva e o efeito na agenda valem
// juntos, ou nenhum vale.
//
// Nenhum método RECUSA o passo da reserva por conflito na agenda: a venda e a
// realocação vencem, e o conflito aparece na leitura (`has_unit_conflict`).
// O erro que sai daqui é defeito (500) ou contenção que o TxManager repete.
//
// Toda chamada lê `agenda_settings` com FOR SHARE: um `PUT /agenda/settings`
// em voo espera a reserva terminar, e a reserva espera o PUT — nenhum evento
// automático nasce com o horário que o PUT acabou de trocar sem ser
// reprogramado por ele.
type Ligacao struct {
	repo *Repository
}

// NovaLigacao monta a ligação sobre o pool. As consultas pegam a transação do
// contexto (`db.From`) — a da reserva.
func NovaLigacao(pool db.DBTX) *Ligacao { return &Ligacao{repo: NewRepository(pool)} }

// preparar lê o que todo passo precisa — a reserva, o relógio da casa e os
// horários (FOR SHARE) —, e exige a transação da reserva.
func (l *Ligacao) preparar(ctx context.Context, reserva uuid.UUID) (reservaDaAgenda, relogio, configuracaoGravada, error) {
	if !db.EmTransacao(ctx) {
		return reservaDaAgenda{}, relogio{}, configuracaoGravada{}, apperr.Internal.WithCause(
			errors.New("agenda: passo da reserva fora de transação — a agenda e a reserva gravariam separadas"))
	}
	res, err := l.repo.Reserva(ctx, reserva)
	if err != nil {
		return res, relogio{}, configuracaoGravada{}, err
	}
	rel, err := l.repo.Relogio(ctx, res.PropriedadeID)
	if err != nil {
		return res, rel, configuracaoGravada{}, err
	}
	cfg, err := l.repo.Configuracao(ctx, res.PropriedadeID, travaCompartilhada)
	return res, rel, cfg, err
}

// AoConfirmar põe na agenda o check-in (dia da entrada), o check-out e a
// limpeza (dia da saída), pelos horários vigentes (`regra.Plan`):
// `origin: reservation`, `agendado`, sem responsável. INSERT sem ON CONFLICT:
// a reserva acabou de sair de `hold`, que não tem agenda, e um segundo
// automático do mesmo tipo é defeito (500), não repetição.
//
// É o efeito da confirmação que a reserva chama no `/confirm` e na remarcação
// que nasce confirmada.
func (l *Ligacao) AoConfirmar(ctx context.Context, reserva uuid.UUID) error {
	res, rel, cfg, err := l.preparar(ctx, reserva)
	if err != nil {
		return err
	}
	planos, err := regra.Plan(res.estadia(), cfg.Regras, rel.zona)
	if err != nil {
		// Defeito de quem chamou (estadia sem noite, configuração incoerente
		// que o banco não deveria ter aceitado): 500, e não confirmar sem
		// agenda.
		return apperr.Internal.WithCause(fmt.Errorf("agenda da reserva %s: %w", res.Codigo, err))
	}
	criados, err := l.repo.InserirAutomaticos(ctx, res.PropriedadeID, reserva, planos, atorOuNulo(ctx), false)
	if err != nil {
		return err
	}
	for _, e := range criados {
		if err := audit.Criacao(ctx, l.repo.pool, entidadeEvento, audit.VerboCriado, e.ID, e.auditavel()); err != nil {
			return err
		}
	}
	return nil
}

// AoCancelar decide a agenda da reserva cancelada — inclusive por no-show e
// pela remarcação, que cancela a original (`regra.OnCancel`). Sem o hóspede
// ter entrado, todo evento agendado ligado a ela vira `cancelado`; com o
// hóspede dentro, o check-out é cancelado, a limpeza vem para hoje e o manual
// que não começou é cancelado. Nada é apagado.
func (l *Ligacao) AoCancelar(ctx context.Context, reserva uuid.UUID, estavaHospedado bool) error {
	_, rel, cfg, err := l.preparar(ctx, reserva)
	if err != nil {
		return err
	}
	eventos, err := l.repo.EventosDaReserva(ctx, reserva)
	if err != nil {
		return err
	}
	return l.aplicar(ctx, eventos, regra.OnCancel(ligados(eventos), estavaHospedado, rel.agora, cfg.Regras, rel.zona))
}

// AoRegistrarEntrada conclui o check-in automático com o instante registrado
// na reserva (`regra.OnCheckIn`). A janela não muda: o hóspede que chega às
// 19:00 chegou atrasado para o check-in das 14:00.
func (l *Ligacao) AoRegistrarEntrada(ctx context.Context, reserva uuid.UUID, em time.Time) error {
	if _, _, _, err := l.preparar(ctx, reserva); err != nil {
		return err
	}
	eventos, err := l.repo.EventosDaReserva(ctx, reserva)
	if err != nil {
		return err
	}
	return l.aplicar(ctx, eventos, regra.OnCheckIn(ligados(eventos), em))
}

// AoRegistrarSaida conclui o check-out automático (`regra.OnCheckOut`). Na
// saída ANTECIPADA, o check-out e a limpeza vêm para o dia em que a unidade
// esvaziou: as noites restantes voltaram à venda, e quem entra amanhã precisa
// da unidade limpa hoje.
func (l *Ligacao) AoRegistrarSaida(ctx context.Context, reserva uuid.UUID, em time.Time) error {
	res, rel, cfg, err := l.preparar(ctx, reserva)
	if err != nil {
		return err
	}
	eventos, err := l.repo.EventosDaReserva(ctx, reserva)
	if err != nil {
		return err
	}
	return l.aplicar(ctx, eventos, regra.OnCheckOut(ligados(eventos), em, res.estadia(), cfg.Regras, rel.zona))
}

// AoRealocar leva junto o evento MANUAL agendado ligado à reserva e preso à
// unidade que saiu (`regra.MovesOnReassign`). O automático não tem unidade
// gravada — já está na nova —, e o concluído fica onde aconteceu.
//
// A agenda NUNCA trava a realocação (decisão de 13/10/2026, contrato de
// `/reassign-unit`). Quem decide se o evento cabe na unidade de destino é a
// constraint dos manuais, no próprio UPDATE; se ela recusar (23P01: outra
// marcação à mão no mesmo instante, na unidade nova), o evento muda SEM
// UNIDADE PRÓPRIA — `unit_id` nulo, ocupando as unidades da reserva, que já é
// a nova —, e a realocação segue. A colisão não some: a leitura a mostra
// (`has_unit_conflict`), e a trilha do evento diz que unidade ele perdeu e
// por quê. Cada evento tem o seu savepoint: a recusa de um não desfaz a
// mudança dos outros, nem nada do resto da transação da reserva.
func (l *Ligacao) AoRealocar(ctx context.Context, reserva, de, para uuid.UUID) error {
	if _, _, _, err := l.preparar(ctx, reserva); err != nil {
		return err
	}
	eventos, err := l.repo.EventosDaReserva(ctx, reserva)
	if err != nil {
		return err
	}
	movem := []eventoGravado{}
	for _, e := range eventos {
		if regra.MovesOnReassign(e.ligado(), de.String()) {
			movem = append(movem, e)
		}
	}
	if len(movem) == 0 {
		return nil
	}
	// Na fila das escritas manuais da unidade de destino, como quem marca à
	// mão: a reserva já está travada, e a ordem de aquisição é sempre reserva
	// → evento → unidade, a mesma da escrita manual.
	if err := l.repo.TravarUnidades(ctx, []uuid.UUID{para}); err != nil {
		return err
	}
	for _, e := range movem {
		if err := l.moverJunto(ctx, e, de, para); err != nil {
			return err
		}
	}
	return nil
}

// moverJunto troca a unidade de UM evento dentro do seu savepoint: no 23P01 da
// constraint dos manuais, volta ao savepoint e grava o evento sem unidade
// própria.
func (l *Ligacao) moverJunto(ctx context.Context, e eventoGravado, de, para uuid.UUID) error {
	exec := l.repo.exec(ctx)
	if _, err := exec.Exec(ctx, `SAVEPOINT agenda_realocacao`); err != nil {
		return db.MapError(err)
	}
	depois, err := l.repo.MoverUnidade(ctx, e.ID, para)
	var disputa *errDisputaDeUnidade
	switch {
	case errors.As(err, &disputa):
		if _, err := exec.Exec(ctx, `ROLLBACK TO SAVEPOINT agenda_realocacao`); err != nil {
			return db.MapError(err)
		}
		if _, err := exec.Exec(ctx, `RELEASE SAVEPOINT agenda_realocacao`); err != nil {
			return db.MapError(err)
		}
		return l.soltarDaUnidade(ctx, e, de, para)
	case err != nil:
		return err
	}
	if _, err := exec.Exec(ctx, `RELEASE SAVEPOINT agenda_realocacao`); err != nil {
		return db.MapError(err)
	}
	return audit.Alteracao(ctx, l.repo.pool, entidadeEvento, verboRealocado, e.ID, e.auditavel(), depois.auditavel())
}

// unidadeSoltaAuditada é o documento da trilha do evento que perdeu a unidade
// na realocação: qual era, para onde a reserva foi e por que ele não foi junto.
type unidadeSoltaAuditada struct {
	UnidadeID     *uuid.UUID `json:"unit_id"`
	UnidadeCodigo string     `json:"unit_code,omitempty"`
	DestinoID     *uuid.UUID `json:"destination_unit_id,omitempty"`
	DestinoCodigo string     `json:"destination_unit_code,omitempty"`
	Motivo        string     `json:"reason,omitempty"`
}

// motivoDaUnidadeSolta é o `reason` estável dessa linha da trilha.
const motivoDaUnidadeSolta = "unidade_de_destino_ocupada_por_marcacao_manual"

// soltarDaUnidade grava o evento sem unidade própria — ele passa a ocupar as
// unidades da reserva — e deixa na trilha a unidade que ele perdeu e o porquê.
func (l *Ligacao) soltarDaUnidade(ctx context.Context, e eventoGravado, de, para uuid.UUID) error {
	if _, err := l.repo.SoltarUnidade(ctx, e.ID); err != nil {
		return err
	}
	codigos, err := l.repo.codigosDasUnidades(ctx, []uuid.UUID{de, para})
	if err != nil {
		return err
	}
	antes := unidadeSoltaAuditada{UnidadeID: &de, UnidadeCodigo: codigos[de]}
	depois := unidadeSoltaAuditada{
		DestinoID: &para, DestinoCodigo: codigos[para], Motivo: motivoDaUnidadeSolta,
	}
	return audit.Alteracao(ctx, l.repo.pool, entidadeEvento, verboRealocadoSemUnidade, e.ID, antes, depois)
}

// aplicar grava a decisão do domínio linha a linha — nunca DELETE — e deixa a
// trilha de cada mudança.
func (l *Ligacao) aplicar(ctx context.Context, eventos []eventoGravado, decisoes []regra.Decision) error {
	if len(decisoes) != len(eventos) {
		return apperr.Internal.WithCause(fmt.Errorf("agenda: %d decisões para %d eventos", len(decisoes), len(eventos)))
	}
	for i, d := range decisoes {
		e := eventos[i]
		var (
			depois eventoGravado
			verbo  string
			err    error
		)
		switch d.Effect {
		case regra.Keep:
			continue
		case regra.CancelIt:
			depois, err = l.repo.Cancelar(ctx, e.ID)
			verbo = verboCancelado
		case regra.CompleteIt:
			var janela *regra.Window
			if !d.Window.Equal(e.janela()) {
				w := d.Window
				janela = &w
			}
			depois, err = l.repo.Concluir(ctx, e.ID, d.DoneAt, atorOuNulo(ctx), janela)
			verbo = verboConcluido
		case regra.MoveIt:
			depois, err = l.repo.MoverJanela(ctx, e.ID, d.Window)
			verbo = verboMovido
		default:
			return apperr.Internal.WithCause(fmt.Errorf("agenda: efeito desconhecido %q", d.Effect))
		}
		if err != nil {
			return err
		}
		if err := audit.Alteracao(ctx, l.repo.pool, entidadeEvento, verbo, e.ID, e.auditavel(), depois.auditavel()); err != nil {
			return err
		}
	}
	return nil
}

func ligados(eventos []eventoGravado) []regra.Linked {
	out := make([]regra.Linked, len(eventos))
	for i, e := range eventos {
		out[i] = e.ligado()
	}
	return out
}

// ═══════════════════════════ O preenchimento ══════════════════════════════

// PreencherReservasConfirmadas gera os três eventos automáticos de cada
// reserva `confirmed` (ou `checked_in`) da propriedade com saída de HOJE em
// diante (dia da casa) — as confirmadas antes de a agenda existir. É o mesmo
// `regra.Plan` do `/confirm`: três cópias da regra divergiriam na primeira
// mudança de horário. A reserva já em curso nasce com o check-in `concluido`,
// no instante registrado na reserva.
//
// Idempotente: `ON CONFLICT (reservation_id, type) WHERE origin =
// 'reservation' DO NOTHING`, e devolve quantos eventos NASCERAM — a segunda
// execução devolve zero. Roda na transação `tx` de quem chama (a etapa do
// seed), com os horários lidos FOR SHARE.
//
// Não grava `audit_log`: é etapa de seed, fora de requisição e sem ator, e a
// trilha da confirmação de cada reserva já existe na linha dela.
func PreencherReservasConfirmadas(ctx context.Context, tx pgx.Tx, propriedade uuid.UUID) (int, error) {
	repo := NewRepository(tx)
	rel, err := repo.Relogio(ctx, propriedade)
	if err != nil {
		return 0, err
	}
	cfg, err := repo.Configuracao(ctx, propriedade, travaCompartilhada)
	if err != nil {
		return 0, err
	}
	reservas, err := repo.ReservasParaPreencher(ctx, propriedade, rel.hoje())
	if err != nil {
		return 0, err
	}
	criados := 0
	for _, res := range reservas {
		estadia := res.estadia()
		if res.Status == ReservaHospedada {
			if estadia.CheckedInAt, err = repo.InstanteDaEntrada(ctx, res.ID); err != nil {
				return criados, err
			}
		}
		planos, err := regra.Plan(estadia, cfg.Regras, rel.zona)
		if err != nil {
			return criados, fmt.Errorf("agenda: preenchendo a reserva %s: %w", res.Codigo, err)
		}
		novos, err := repo.InserirAutomaticos(ctx, propriedade, res.ID, planos, nil, true)
		if err != nil {
			return criados, err
		}
		criados += len(novos)
	}
	return criados, nil
}
