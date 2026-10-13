package agenda

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	regra "github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/agenda"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// ═══════════════════════════ Marcar ═══════════════════════════════════════

// Criar marca o evento à mão — visita, evento ou limpeza avulsa —, no roteiro
// do `Service`. Conflito de UNIDADE recusa (409 UNIT_NOT_AVAILABLE, nada é
// criado); conflito de HORÁRIO avisa (201 com `meta.warnings`).
func (s *Service) Criar(ctx context.Context, c EventoDaAgendaCriar) (EscritaNaAgenda, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return EscritaNaAgenda{}, err
	}
	ator, ok := auth.UsuarioID(ctx)
	if !ok {
		return EscritaNaAgenda{}, apperr.Unauthorized
	}
	esc, err := escopo(ctx, auth.AcaoCriar)
	if err != nil {
		return EscritaNaAgenda{}, err
	}
	inicio, err := instante(c.Inicio)
	if err != nil {
		return EscritaNaAgenda{}, apperr.Validation(map[string]string{"starts_at": "instante inválido."}).WithCause(err)
	}

	var (
		out       EscritaNaAgenda
		candidato regra.Candidate
	)
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		rel, err := s.repo.Relogio(ctx, prop)
		if err != nil {
			return err
		}
		cfg, err := s.repo.Configuracao(ctx, prop, semTrava)
		if err != nil {
			return err
		}

		// `ends_at` ausente é um passo da grade.
		fim := inicio.Add(cfg.Regras.Slot())
		if v, ok := c.Fim.Definido(); ok {
			if fim, err = instante(v); err != nil {
				return apperr.Validation(map[string]string{"ends_at": "instante inválido."}).WithCause(err)
			}
		}
		titulo := strings.TrimSpace(c.Titulo)
		alvo := eventoGravado{
			Tipo: c.Tipo, Origem: string(regra.ByHand), Status: string(regra.Scheduled), Titulo: &titulo,
			Inicio: inicio, Fim: fim, DiaInteiro: c.DiaInteiro.Ou(false),
			UnidadeID: c.UnidadeID, ReservaID: c.ReservaID, ContatoID: c.ContatoID, Notas: textoOuNulo(c.Notas),
		}

		// 2. O responsável: em `own`, o próprio ator.
		if alvo.ResponsavelID, err = regra.ResolveAssignee(regra.AssigneeWrite[uuid.UUID]{
			Write: regra.Create, Scope: esc, Actor: ator, Requested: campo(c.ResponsavelID),
		}); err != nil {
			return s.traduzirRegra(ctx, err)
		}
		// 3. A janela.
		if err := regra.CheckWindow(alvo.janela(), alvo.DiaInteiro, rel.zona, rel.hoje(), regra.MaxEventDays); err != nil {
			return s.traduzirRegra(ctx, err)
		}
		// 4. O que o evento cita existe, é desta casa e está de pé.
		if err := s.conferirVinculos(ctx, prop, alvo, mudancas{unidade: true, reserva: true, contato: true, responsavel: true}); err != nil {
			return err
		}
		// 6. A unidade — com as escritas manuais da mesma unidade em fila
		// (TravarUnidades): quem decide a disputa continua sendo a constraint.
		if candidato, err = s.candidato(ctx, alvo); err != nil {
			return err
		}
		if err := s.repo.TravarUnidades(ctx, uuidsDeTexto(candidato.Units)); err != nil {
			return err
		}
		if err := s.conferirUnidade(ctx, prop, rel, cfg, candidato); err != nil {
			return err
		}
		// 7. A gravação — a disputa entre duas marcações à mão é da constraint.
		gravado, err := s.repo.InserirManual(ctx, prop, novoEvento{
			Tipo: alvo.Tipo, ReservaID: alvo.ReservaID, UnidadeID: alvo.UnidadeID, ContatoID: alvo.ContatoID,
			ResponsavelID: alvo.ResponsavelID, Titulo: titulo, Janela: alvo.janela(), DiaInteiro: alvo.DiaInteiro,
			Notas: alvo.Notas, CriadoPor: &ator,
		})
		if err != nil {
			return err
		}
		// 8. Os avisos de horário: o evento foi gravado, e a tela diz o porquê
		// de ele merecer atenção. O próprio evento já está na agenda do
		// responsável — `Event` o tira da conta.
		candidato.Event = gravado.ID.String()
		avisos, err := s.avisos(ctx, prop, rel, cfg, candidato)
		if err != nil {
			return err
		}
		// 9. A trilha.
		if err := audit.Criacao(ctx, s.repo.pool, entidadeEvento, audit.VerboCriado, gravado.ID, gravado.auditavel()); err != nil {
			return err
		}
		evento, err := s.lerEvento(ctx, prop, gravado.ID, nil, rel, cfg)
		if err != nil {
			return err
		}
		out = EscritaNaAgenda{Evento: evento, Meta: MetaDaEscrita{Avisos: avisos}}
		return nil
	})
	return out, s.depoisDaEscrita(ctx, prop, candidato, err)
}

// ═══════════════════════════ Editar ═══════════════════════════════════════

// proposta é o evento como ficaria depois da escrita, e o que ela muda de
// identidade (pelo nome do contrato).
type proposta struct {
	alvo       eventoGravado
	identidade []string
	pedido     regra.Field[uuid.UUID]
}

// exibido é o que a leitura MOSTRA do evento automático e não está gravado
// nele: o título composto, o titular e as unidades da reserva. Reenviar o que
// foi lido não é mudança — o formulário que devolve o evento inteiro não pode
// falhar pelo que não tocou.
type exibido struct {
	titulo   string
	contato  *uuid.UUID
	unidades map[uuid.UUID]bool
}

func (p *proposta) automatico() bool { return p.alvo.Origem == string(regra.FromReservation) }

func (p *proposta) tipo(v string) {
	if v == p.alvo.Tipo {
		return
	}
	p.identidade = append(p.identidade, "type")
	if !p.automatico() {
		p.alvo.Tipo = v
	}
}

func (p *proposta) titulo(v string, ex exibido) {
	t := strings.TrimSpace(v)
	if p.automatico() {
		if t != ex.titulo {
			p.identidade = append(p.identidade, "title")
		}
		return
	}
	if p.alvo.Titulo != nil && *p.alvo.Titulo == t {
		return
	}
	p.identidade = append(p.identidade, "title")
	p.alvo.Titulo = &t
}

func (p *proposta) diaInteiro(v bool) {
	if v == p.alvo.DiaInteiro {
		return
	}
	p.identidade = append(p.identidade, "all_day")
	if !p.automatico() {
		p.alvo.DiaInteiro = v
	}
}

func (p *proposta) unidade(v *uuid.UUID, ex exibido) {
	if mesmoID(v, p.alvo.UnidadeID) || (p.automatico() && v != nil && ex.unidades[*v]) {
		return
	}
	p.identidade = append(p.identidade, "unit_id")
	if !p.automatico() {
		p.alvo.UnidadeID = v
	}
}

func (p *proposta) reserva(v *uuid.UUID) {
	if mesmoID(v, p.alvo.ReservaID) {
		return
	}
	p.identidade = append(p.identidade, "reservation_id")
	if !p.automatico() {
		p.alvo.ReservaID = v
	}
}

func (p *proposta) contato(v *uuid.UUID, ex exibido) {
	if mesmoID(v, p.alvo.ContatoID) || (p.automatico() && mesmoID(v, ex.contato)) {
		return
	}
	p.identidade = append(p.identidade, "contact_id")
	if !p.automatico() {
		p.alvo.ContatoID = v
	}
}

// Substituir é o PUT. No manual: substitui tudo que o POST aceita (anulável
// omitido vira `null`); `type` e `title` são obrigatórios no manual agendado.
// No automático: só horário, responsável e anotação — o resto omitido fica,
// repetido passa, diferente é 422 no campo (`regra.CheckEdit`).
func (s *Service) Substituir(ctx context.Context, id uuid.UUID, c EventoDaAgendaSubstituir) (EscritaNaAgenda, error) {
	return s.editar(ctx, id, regra.Replace, func(antes eventoGravado, ex exibido) (proposta, error) {
		p := proposta{alvo: antes, pedido: campo(c.ResponsavelID)}
		if !p.automatico() && antes.Status == string(regra.Scheduled) {
			falhas := map[string]string{}
			if !c.Tipo.Set {
				falhas["type"] = "é obrigatório no PUT do evento marcado à mão."
			}
			if !c.Titulo.Set {
				falhas["title"] = "é obrigatório no PUT do evento marcado à mão."
			}
			if len(falhas) > 0 {
				return proposta{}, apperr.Validation(falhas)
			}
		}
		if v, ok := c.Tipo.Definido(); ok {
			p.tipo(v)
		}
		if v, ok := c.Titulo.Definido(); ok {
			p.titulo(v, ex)
		}
		// No automático, os campos de identidade omitidos ficam como estão; no
		// manual, o PUT é substituição integral: omitido volta ao padrão.
		manual := !p.automatico()
		if c.DiaInteiro.Set || manual {
			p.diaInteiro(c.DiaInteiro.Ou(false))
		}
		if c.UnidadeID.Set || manual {
			p.unidade(ponteiro(c.UnidadeID), ex)
		}
		if c.ReservaID.Set || manual {
			p.reserva(ponteiro(c.ReservaID))
		}
		if c.ContatoID.Set || manual {
			p.contato(ponteiro(c.ContatoID), ex)
		}
		var err error
		if p.alvo.Inicio, err = instante(c.Inicio); err != nil {
			return proposta{}, apperr.Validation(map[string]string{"starts_at": "instante inválido."}).WithCause(err)
		}
		if p.alvo.Fim, err = instante(c.Fim); err != nil {
			return proposta{}, apperr.Validation(map[string]string{"ends_at": "instante inválido."}).WithCause(err)
		}
		// A anotação é da agenda nos dois: omitida no PUT, vira nula.
		p.alvo.Notas = notasDoOpt(c.Notas)
		return p, nil
	})
}

// Atualizar é o PATCH: campo ausente não muda, `null` limpa. As mesmas regras
// do PUT, campo a campo. Corpo vazio responde 200 sem mudar nada.
func (s *Service) Atualizar(ctx context.Context, id uuid.UUID, c EventoDaAgendaAtualizar) (EscritaNaAgenda, error) {
	if c.vazio() {
		evento, err := s.buscarNoEscopo(ctx, id, auth.AcaoEditar)
		return EscritaNaAgenda{Evento: evento, Meta: MetaDaEscrita{Avisos: []AvisoDeAgenda{}}}, err
	}
	return s.editar(ctx, id, regra.Patch, func(antes eventoGravado, ex exibido) (proposta, error) {
		p := proposta{alvo: antes, pedido: campo(c.ResponsavelID)}
		if v, ok := c.Tipo.Definido(); ok {
			p.tipo(v)
		}
		if v, ok := c.Titulo.Definido(); ok {
			p.titulo(v, ex)
		}
		if v, ok := c.DiaInteiro.Definido(); ok {
			p.diaInteiro(v)
		}
		if c.UnidadeID.Set {
			p.unidade(ponteiro(c.UnidadeID), ex)
		}
		if c.ReservaID.Set {
			p.reserva(ponteiro(c.ReservaID))
		}
		if c.ContatoID.Set {
			p.contato(ponteiro(c.ContatoID), ex)
		}
		var err error
		if v, ok := c.Inicio.Definido(); ok {
			if p.alvo.Inicio, err = instante(v); err != nil {
				return proposta{}, apperr.Validation(map[string]string{"starts_at": "instante inválido."}).WithCause(err)
			}
		}
		if v, ok := c.Fim.Definido(); ok {
			if p.alvo.Fim, err = instante(v); err != nil {
				return proposta{}, apperr.Validation(map[string]string{"ends_at": "instante inválido."}).WithCause(err)
			}
		}
		if c.Notas.Set {
			p.alvo.Notas = notasDoOpt(c.Notas)
		}
		return p, nil
	})
}

// editar é o roteiro comum do PUT e do PATCH.
func (s *Service) editar(ctx context.Context, id uuid.UUID, verbo regra.Write,
	montar func(antes eventoGravado, ex exibido) (proposta, error)) (EscritaNaAgenda, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return EscritaNaAgenda{}, err
	}
	ator, ok := auth.UsuarioID(ctx)
	if !ok {
		return EscritaNaAgenda{}, apperr.Unauthorized
	}
	esc, err := escopo(ctx, auth.AcaoEditar)
	if err != nil {
		return EscritaNaAgenda{}, err
	}
	responsavel, err := responsavelDoEscopo(ctx, auth.AcaoEditar)
	if err != nil {
		return EscritaNaAgenda{}, err
	}

	var (
		out       EscritaNaAgenda
		candidato regra.Candidate
	)
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		// O evento travado, já no escopo: fora dele é 404, antes de qualquer
		// outra resposta.
		antes, err := s.repo.TravarEvento(ctx, prop, id, responsavel)
		if err != nil {
			return err
		}
		rel, err := s.repo.Relogio(ctx, prop)
		if err != nil {
			return err
		}
		cfg, err := s.repo.Configuracao(ctx, prop, semTrava)
		if err != nil {
			return err
		}
		z := rel.zona

		var (
			ex  exibido
			res reservaDaAgenda
		)
		automatico := antes.Origem == string(regra.FromReservation)
		if automatico && antes.ReservaID != nil {
			if res, err = s.repo.Reserva(ctx, *antes.ReservaID); err != nil {
				return err
			}
			unidades, err := s.repo.UnidadesDaReserva(ctx, res.ID)
			if err != nil {
				return err
			}
			contato := res.ContatoID
			ex = exibido{titulo: regra.AutoTitle(regra.Type(antes.Tipo), res.Codigo), contato: &contato, unidades: map[uuid.UUID]bool{}}
			for _, u := range unidades {
				ex.unidades[u.ID] = true
			}
		}

		p, err := montar(antes, ex)
		if err != nil {
			return err
		}

		// 2. O responsável.
		if p.alvo.ResponsavelID, err = regra.ResolveAssignee(regra.AssigneeWrite[uuid.UUID]{
			Write: verbo, Scope: esc, Actor: ator, Current: antes.ResponsavelID, Requested: p.pedido,
		}); err != nil {
			return s.traduzirRegra(ctx, err)
		}
		janelaMudou := !p.alvo.janela().Equal(antes.janela())

		// 3. A janela — só quando ela (ou o dia inteiro) muda: anotar o evento
		// de dois anos atrás não pode cair na guarda de digitação da janela.
		if janelaMudou || p.alvo.DiaInteiro != antes.DiaInteiro {
			if err := regra.CheckWindow(p.alvo.janela(), p.alvo.DiaInteiro, z, rel.hoje(), regra.MaxEventDays); err != nil {
				return s.traduzirRegra(ctx, err)
			}
		}

		// 4. O que muda de vínculo existe e está de pé.
		m := mudancas{
			unidade:     !mesmoID(p.alvo.UnidadeID, antes.UnidadeID),
			reserva:     !mesmoID(p.alvo.ReservaID, antes.ReservaID),
			contato:     !mesmoID(p.alvo.ContatoID, antes.ContatoID),
			responsavel: !mesmoID(p.alvo.ResponsavelID, antes.ResponsavelID),
		}
		if err := s.conferirVinculos(ctx, prop, p.alvo, m); err != nil {
			return err
		}

		// 5. O que o estado deixa mudar.
		edicao := regra.Edit{Identity: p.identidade, Assignee: m.responsavel, Notes: !mesmoTexto(p.alvo.Notas, antes.Notas)}
		if janelaMudou {
			w := p.alvo.janela()
			edicao.Window = &w
		}
		limites := regra.AutoBounds{}
		if automatico && antes.Tipo == string(regra.Cleaning) && edicao.Window != nil {
			if limites.CheckoutEnd, err = s.fimDoCheckOut(ctx, res, cfg, rel); err != nil {
				return err
			}
		}
		if err := regra.CheckEdit(antes.fatos(z), edicao, limites, z); err != nil {
			return s.traduzirRegra(ctx, err)
		}

		// Reenviar o que está gravado não é mudança: 200 sem escrever.
		// (No automático e no fechado, a identidade que mudasse já foi recusada
		// por CheckEdit; o que sobra de identidade aqui é do manual agendado.)
		mudou := len(edicao.Identity) > 0 || edicao.Window != nil || edicao.Assignee || edicao.Notes
		if !mudou {
			evento, err := s.lerEvento(ctx, prop, id, nil, rel, cfg)
			out = EscritaNaAgenda{Evento: evento, Meta: MetaDaEscrita{Avisos: []AvisoDeAgenda{}}}
			return err
		}

		// 6. A unidade — quando a janela ou o que o evento ocupa muda, e ele
		// continua agendado. Anotar o evento que JÁ colide (a venda chegou
		// depois) não pode ser recusado pela colisão que ninguém criou agora.
		if candidato, err = s.candidato(ctx, p.alvo); err != nil {
			return err
		}
		if p.alvo.Status == string(regra.Scheduled) && (janelaMudou || m.unidade || m.reserva) {
			if err := s.repo.TravarUnidades(ctx, uuidsDeTexto(candidato.Units)); err != nil {
				return err
			}
			if err := s.conferirUnidade(ctx, prop, rel, cfg, candidato); err != nil {
				return err
			}
		}

		// 7. A gravação.
		depois, err := s.repo.GravarEdicao(ctx, p.alvo)
		if err != nil {
			return err
		}

		// 8. Os avisos — quando a janela ou o responsável mudou.
		avisos := []AvisoDeAgenda{}
		if depois.Status == string(regra.Scheduled) && (janelaMudou || m.responsavel) {
			if avisos, err = s.avisos(ctx, prop, rel, cfg, candidato); err != nil {
				return err
			}
		}

		// 9. A trilha.
		if err := audit.Alteracao(ctx, s.repo.pool, entidadeEvento, audit.VerboAlterado, id,
			antes.auditavel(), depois.auditavel()); err != nil {
			return err
		}
		evento, err := s.lerEvento(ctx, prop, id, nil, rel, cfg)
		if err != nil {
			return err
		}
		out = EscritaNaAgenda{Evento: evento, Meta: MetaDaEscrita{Avisos: avisos}}
		return nil
	})
	return out, s.depoisDaEscrita(ctx, prop, candidato, err)
}

// fimDoCheckOut é o limite da limpeza automática: o fim do check-out gravado
// da reserva, ou o da configuração quando ele não existe.
func (s *Service) fimDoCheckOut(ctx context.Context, res reservaDaAgenda, cfg configuracaoGravada, rel relogio) (time.Time, error) {
	gravado, err := s.repo.FimDoCheckOut(ctx, res.ID)
	if err != nil {
		return time.Time{}, err
	}
	if gravado != nil {
		return *gravado, nil
	}
	return rel.zona.At(res.Saida, cfg.Regras.CheckOut), nil
}

// ═══════════════════════════ Transições ═══════════════════════════════════

// Concluir é `agendado → concluido`, com `done_at` e `done_by` do servidor.
// Concluem por aqui o manual e a limpeza automática; check-in e check-out
// automáticos concluem pela reserva (409 `segue_a_reserva`), e só do dia do
// evento em diante (409 `nao_comecou`).
func (s *Service) Concluir(ctx context.Context, id uuid.UUID) (EventoDaAgenda, error) {
	return s.transitar(ctx, id, regra.Complete, auth.AcaoEditar)
}

// Cancelar é o DELETE: desmarca (cancela, não apaga). Só o manual agendado; o
// automático segue a reserva (409 `segue_a_reserva`).
func (s *Service) Cancelar(ctx context.Context, id uuid.UUID) (EventoDaAgenda, error) {
	return s.transitar(ctx, id, regra.Cancel, auth.AcaoExcluir)
}

func (s *Service) transitar(ctx context.Context, id uuid.UUID, acao regra.Action, acaoRBAC string) (EventoDaAgenda, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return EventoDaAgenda{}, err
	}
	responsavel, err := responsavelDoEscopo(ctx, acaoRBAC)
	if err != nil {
		return EventoDaAgenda{}, err
	}
	var out EventoDaAgenda
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarEvento(ctx, prop, id, responsavel)
		if err != nil {
			return err
		}
		rel, err := s.repo.Relogio(ctx, prop)
		if err != nil {
			return err
		}
		if _, err := regra.Next(antes.fatos(rel.zona), acao, rel.hoje()); err != nil {
			return s.traduzirRegra(ctx, err)
		}
		var (
			depois eventoGravado
			verbo  string
		)
		switch acao {
		case regra.Complete:
			depois, err = s.repo.Concluir(ctx, id, nil, atorOuNulo(ctx), nil)
			verbo = verboConcluido
		case regra.Cancel:
			depois, err = s.repo.Cancelar(ctx, id)
			verbo = verboCancelado
		default:
			return apperr.Internal.WithCause(fmt.Errorf("agenda: ação %q sem gravação", acao))
		}
		if err != nil {
			return err
		}
		if err := audit.Alteracao(ctx, s.repo.pool, entidadeEvento, verbo, id, antes.auditavel(), depois.auditavel()); err != nil {
			return err
		}
		cfg, err := s.repo.Configuracao(ctx, prop, semTrava)
		if err != nil {
			return err
		}
		out, err = s.lerEvento(ctx, prop, id, nil, rel, cfg)
		return err
	})
	return out, err
}

// ═══════════════════════════ Peças do roteiro ═════════════════════════════

// mudancas diz o que a escrita muda de vínculo — e é só isso que a etapa 4
// confere: o contato anonimizado depois de citado não trava a edição da
// anotação.
type mudancas struct{ unidade, reserva, contato, responsavel bool }

// conferirVinculos é a etapa 4: o que o evento cita existe nesta casa e está
// de pé. Todas as falhas de uma vez, em `details`, como em todo 422 de campo.
func (s *Service) conferirVinculos(ctx context.Context, prop uuid.UUID, alvo eventoGravado, m mudancas) error {
	falhas := map[string]string{}
	if m.unidade && alvo.UnidadeID != nil {
		u, achou, err := s.repo.Unidade(ctx, prop, *alvo.UnidadeID)
		switch {
		case err != nil:
			return err
		case !achou:
			falhas["unit_id"] = "unidade não encontrada nesta propriedade."
		case !u.Ativa:
			falhas["unit_id"] = fmt.Sprintf("a unidade %s está inativa: não recebe evento.", u.Codigo)
		}
	}
	if m.reserva && alvo.ReservaID != nil {
		status, achou, err := s.repo.EstadoDaReserva(ctx, prop, *alvo.ReservaID)
		switch {
		case err != nil:
			return err
		case !achou:
			falhas["reservation_id"] = "reserva não encontrada nesta propriedade."
		case !aceitaVinculo(status):
			falhas["reservation_id"] = fmt.Sprintf(
				"a reserva está em %s: só reserva em %s recebe evento.", status, strings.Join(estadosQueAceitamVinculo, ", "))
		}
	}
	if (m.unidade || m.reserva) && alvo.UnidadeID != nil && alvo.ReservaID != nil &&
		falhas["unit_id"] == "" && falhas["reservation_id"] == "" {
		unidades, err := s.repo.UnidadesDaReserva(ctx, *alvo.ReservaID)
		if err != nil {
			return err
		}
		daReserva := false
		for _, u := range unidades {
			if u.ID == *alvo.UnidadeID {
				daReserva = true
			}
		}
		if !daReserva {
			falhas["unit_id"] = "a unidade não é desta reserva: o evento ligado a uma reserva acontece numa unidade dela."
		}
	}
	if m.contato && alvo.ContatoID != nil {
		anonimizado, achou, err := s.repo.Contato(ctx, prop, *alvo.ContatoID)
		switch {
		case err != nil:
			return err
		case !achou:
			falhas["contact_id"] = "contato não encontrado nesta propriedade."
		case anonimizado:
			falhas["contact_id"] = "o contato foi anonimizado e não recebe evento novo."
		}
	}
	if m.responsavel && alvo.ResponsavelID != nil {
		ativo, err := s.repo.UsuarioAtivo(ctx, prop, *alvo.ResponsavelID)
		if err != nil {
			return err
		}
		if !ativo {
			falhas["assignee_id"] = "usuário não encontrado, inativo ou de outra propriedade."
		}
	}
	if len(falhas) > 0 {
		return apperr.Validation(falhas)
	}
	return nil
}

func aceitaVinculo(status string) bool {
	for _, s := range estadosQueAceitamVinculo {
		if s == status {
			return true
		}
	}
	return false
}

// candidato monta o evento como o domínio o confere: as unidades que ele
// OCUPA — a gravada, ou as da reserva ligada, ou nenhuma (a reunião sem
// apartamento não ocupa unidade).
func (s *Service) candidato(ctx context.Context, e eventoGravado) (regra.Candidate, error) {
	c := regra.Candidate{Window: e.janela(), AllDay: e.DiaInteiro}
	if e.ID != uuid.Nil {
		c.Event = e.ID.String()
	}
	if e.ReservaID != nil {
		c.Reservation = e.ReservaID.String()
	}
	if e.ResponsavelID != nil {
		c.Assignee = e.ResponsavelID.String()
	}
	switch {
	case e.UnidadeID != nil:
		c.Units = []string{e.UnidadeID.String()}
	case e.ReservaID != nil:
		unidades, err := s.repo.UnidadesDaReserva(ctx, *e.ReservaID)
		if err != nil {
			return c, err
		}
		c.Units = idsEmTexto(unidades)
	}
	return c, nil
}

// conferirUnidade é a etapa 6: `regra.CheckUnit` sobre as ocupações das
// unidades do evento — estadia, manutenção, bloqueio de unidade e eventos
// agendados.
func (s *Service) conferirUnidade(ctx context.Context, prop uuid.UUID, rel relogio, cfg configuracaoGravada, c regra.Candidate) error {
	if len(c.Units) == 0 {
		return nil
	}
	ocupacoes, err := s.repo.Ocupacoes(ctx, prop, uuidsDeTexto(c.Units), c.Window, cfg.Regras, rel.zona)
	if err != nil {
		return err
	}
	if err := regra.CheckUnit(c, ocupacoes, rel.zona); err != nil {
		return s.traduzirRegra(ctx, err)
	}
	return nil
}

// avisos é a etapa 8: os conflitos de HORÁRIO, que nunca recusam.
func (s *Service) avisos(ctx context.Context, prop uuid.UUID, rel relogio, cfg configuracaoGravada, c regra.Candidate) ([]AvisoDeAgenda, error) {
	var responsavel *uuid.UUID
	if c.Assignee != "" {
		if id, err := uuid.Parse(c.Assignee); err == nil {
			responsavel = &id
		}
	}
	bloqueios, agendaDoResponsavel, err := s.repo.Compromissos(ctx, prop, c.Window, cfg.Regras.Buffer(), responsavel)
	if err != nil {
		return nil, err
	}
	out := []AvisoDeAgenda{}
	for _, w := range regra.TimeWarnings(c, cfg.Regras, rel.zona, bloqueios, agendaDoResponsavel) {
		a := AvisoDeAgenda{Tipo: string(w.Kind), Mensagem: w.Message}
		if w.Ref != "" {
			if id, err := uuid.Parse(w.Ref); err == nil {
				a.RefID = &id
			}
			inicio, fim := rel.zona.In(w.Window.Start), rel.zona.In(w.Window.End)
			a.Inicio, a.Fim = &inicio, &fim
		}
		out = append(out, a)
	}
	return out, nil
}

// depoisDaEscrita traduz a disputa que a CONSTRAINT decidiu — depois do
// rollback, relendo os conflitos. Qualquer outro erro passa como veio.
func (s *Service) depoisDaEscrita(ctx context.Context, prop uuid.UUID, c regra.Candidate, err error) error {
	var disputa *errDisputaDeUnidade
	if errors.As(err, &disputa) {
		return s.conflitoDepoisDoRollback(ctx, prop, c, disputa)
	}
	return err
}

// ─────────────────────────── Auxiliares ─────────────────────────────

// campo leva o Opt do corpo ao `regra.Field` — a mesma distinção entre
// ausente, `null` e valor, redeclarada no domínio porque ele não importa a
// camada HTTP.
func campo(o httpx.Opt[uuid.UUID]) regra.Field[uuid.UUID] {
	f := regra.Field[uuid.UUID]{Present: o.Set}
	if v, ok := o.Definido(); ok {
		f.Value = &v
	}
	return f
}

// ponteiro: valor vira ponteiro; ausente e `null` viram nil.
func ponteiro(o httpx.Opt[uuid.UUID]) *uuid.UUID {
	if v, ok := o.Definido(); ok {
		return &v
	}
	return nil
}

func notasDoOpt(o httpx.Opt[string]) *string {
	if v, ok := o.Definido(); ok {
		return textoOuNulo(&v)
	}
	return nil
}

func mesmoID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func mesmoTexto(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func uuidsDeTexto(ts []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ts))
	for _, t := range ts {
		if id, err := uuid.Parse(t); err == nil {
			out = append(out, id)
		}
	}
	return out
}
