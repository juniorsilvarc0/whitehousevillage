package reservas

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/disponibilidade"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Servico orquestra o ciclo de vida da reserva.
//
// Ele NÃO calcula preço: delega ao módulo `disponibilidade`, que já carrega o
// estado comercial do banco e chama internal/domain/booking. Reimplementar o
// orçamento aqui faria a mesma estadia custar uma coisa no /quotes e outra no
// POST /reservations — que é exatamente o defeito que a regra 1 do CLAUDE.md
// existe para impedir.
type Servico struct {
	repo *Repository
	// orcamentos é o módulo de disponibilidade. Os repositórios dele usam
	// db.From, então as consultas dele entram NA MESMA TRANSAÇÃO desta criação
	// quando recebem o contexto transacional — o orçamento e a gravação enxergam
	// o mesmo instante do banco.
	orcamentos *disponibilidade.Servico
	tx         *db.TxManager
}

func NovoServico(repo *Repository, orcamentos *disponibilidade.Servico, tx *db.TxManager) *Servico {
	return &Servico{repo: repo, orcamentos: orcamentos, tx: tx}
}

// Resultado é o que o handler escreve. Ele existe por causa da idempotência: a
// repetição de uma chave devolve o CORPO ORIGINAL byte a byte, e não um corpo
// remontado — remontar poderia devolver a reserva já alterada por uma ação
// posterior, e o cliente veria uma resposta "de criação" com dados de agora.
type Resultado struct {
	Status int
	// Corpo é o envelope já pronto (data [+ meta]).
	Corpo any
	// Bruto, quando presente, é o envelope gravado na primeira execução.
	Bruto []byte
	// Local é o header Location das respostas 201.
	Local string
}

// envelope é o formato de resposta do contrato: {"data": …, "meta": …}.
type envelope struct {
	Data any `json:"data"`
	Meta any `json:"meta,omitempty"`
}

// ─────────────────────────── Contexto do ator ───────────────────────

// ator resolve quem está pedindo. Sem identidade não há propriedade, e sem
// propriedade toda consulta deste módulo rodaria sem o filtro que isola a casa.
func ator(ctx context.Context) (*auth.Usuario, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, apperr.Unauthorized
	}
	if u.PropertyID == uuid.Nil {
		return nil, apperr.Internal.WithMessage("Sessão sem propriedade associada.")
	}
	return u, nil
}

// somenteMinhas traduz o escopo do RBAC. `own` vira `AND owner_id = $usuario`
// no SQL — nunca filtro em memória, que faria o `total` da paginação mentir.
func somenteMinhas(ctx context.Context, acao string) bool {
	return auth.SomenteProprios(ctx, Recurso, acao)
}

// ─────────────────────────── Leitura ────────────────────────────────

// Listar devolve a página de reservas e o total.
func (s *Servico) Listar(ctx context.Context, f Filtro) ([]Reserva, int64, error) {
	u, err := ator(ctx)
	if err != nil {
		return nil, 0, err
	}
	f.SomenteMinhas, f.Usuario = somenteMinhas(ctx, auth.AcaoVer), u.ID
	return s.repo.Listar(ctx, u.PropertyID, f)
}

// Buscar devolve uma reserva.
func (s *Servico) Buscar(ctx context.Context, id uuid.UUID) (Reserva, error) {
	u, err := ator(ctx)
	if err != nil {
		return Reserva{}, err
	}
	return s.repo.Buscar(ctx, u.PropertyID, id, somenteMinhas(ctx, auth.AcaoVer), u.ID)
}

// Completa monta a tela da reserva numa chamada.
//
// Os totais e as noites vêm do SNAPSHOT — nada é recalculado na leitura, senão
// a tela mostraria o preço de hoje para uma venda de ontem.
func (s *Servico) Completa(ctx context.Context, id uuid.UUID) (ReservaCompleta, error) {
	u, err := ator(ctx)
	if err != nil {
		return ReservaCompleta{}, err
	}
	so, eu := somenteMinhas(ctx, auth.AcaoVer), u.ID

	reserva, err := s.repo.Buscar(ctx, u.PropertyID, id, so, eu)
	if err != nil {
		return ReservaCompleta{}, err
	}

	completa := ReservaCompleta{Reserva: reserva, Unidades: reserva.Unidades}
	if completa.Noites, err = s.repo.NoitesDe(ctx, id); err != nil {
		return completa, err
	}
	completa.Linhas = AgruparEmLinhas(completa.Noites)
	if completa.Hospedes, err = s.repo.HospedesDe(ctx, id); err != nil {
		return completa, err
	}
	if completa.Timeline, err = s.repo.TimelineDe(ctx, id); err != nil {
		return completa, err
	}

	// A prévia é o mesmo cálculo do ?dry_run=1. Falhar aqui não pode derrubar a
	// tela inteira: reserva sem política congelada (dado antigo) simplesmente
	// não tem prévia, e o resto continua legível.
	if Cancelavel(reserva.Status) {
		if previa, err := s.simular(ctx, id, ""); err == nil {
			completa.PreviaDoCancelamento = &previa
		}
	}
	return completa, nil
}

// ─────────────────────────── POST /reservations ─────────────────────

const rotaDeCriacao = "POST /reservations"

// Criar emite a pré-reserva: recalcula o orçamento, congela o snapshot, aloca
// a(s) unidade(s) e segura o calendário — tudo numa transação.
//
// A ORDEM das gravações não é arbitrária. A reserva nasce PRIMEIRO porque
// `stay_blocks.reservation_id` a exige, e isso faz o lock do contador de código
// ser sempre tomado antes dos locks das unidades: uma ordem de aquisição única
// é o que impede o impasse entre as duas travas (ver a migration 20260826110000).
func (s *Servico) Criar(ctx context.Context, chave string, corpo ReservaCriar) (Resultado, error) {
	u, err := ator(ctx)
	if err != nil {
		return Resultado{}, err
	}
	hash, err := impressao(corpo)
	if err != nil {
		return Resultado{}, err
	}

	var saida Resultado
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		guardada, err := s.repo.reservarChave(ctx, chave, rotaDeCriacao, hash, donoDo(u))
		if err != nil {
			return err
		}
		if guardada != nil {
			saida = repetir(guardada)
			return nil
		}

		reserva, _, err := s.emitir(ctx, u, corpo, EstadoHold, nil, nil)
		if err != nil {
			return err
		}

		env := envelope{Data: reserva}
		if err := s.repo.guardarResposta(ctx, chave, rotaDeCriacao, donoDo(u), http.StatusCreated, env); err != nil {
			return err
		}
		saida = Resultado{Status: http.StatusCreated, Corpo: env, Local: localDaReserva(reserva.ID)}
		return nil
	})
	return saida, err
}

// emitir é o miolo compartilhado por POST /reservations e POST /reschedule:
// orçamento → reserva → snapshot → alocação → calendário → evento.
//
// `remarcadaDe` liga a reserva nova à original; `sinalHerdado` reaplica o sinal
// já recebido quando a remarcação nasce confirmada.
//
// O segundo retorno é o CRÉDITO gerado: o pedaço do sinal herdado que não coube
// na reserva nova. É sempre 0 na criação comum.
func (s *Servico) emitir(ctx context.Context, u *auth.Usuario, corpo ReservaCriar, estado string, remarcadaDe *uuid.UUID, sinalHerdado *int64) (Reserva, int64, error) {
	produto, err := s.repo.Produto(ctx, u.PropertyID, corpo.UnitTypeID)
	if err != nil {
		return Reserva{}, 0, err
	}
	if err := s.repo.ConferirContato(ctx, u.PropertyID, corpo.ContactID); err != nil {
		return Reserva{}, 0, err
	}

	// O orçamento vem do motor, pelo módulo de disponibilidade. Nenhum centavo
	// que chegou no corpo entra na conta — o corpo manda o PEDIDO, e o servidor
	// recalcula tudo. A conversão das datas passa pelo Normalizar() do próprio
	// módulo de orçamento para os dois caminhos falharem igual.
	entrada, err := disponibilidade.Pedido{
		UnitTypeID:  corpo.UnitTypeID,
		CheckIn:     corpo.CheckIn,
		CheckOut:    corpo.CheckOut,
		Hospedes:    corpo.Hospedes,
		DescontoPct: corpo.DescontoPct,
		IsEvento:    corpo.IsEvento,
	}.Normalizar()
	if err != nil {
		return Reserva{}, 0, err
	}

	orcamento, err := s.orcamentos.Orcar(ctx, entrada)
	if err != nil {
		return Reserva{}, 0, err
	}

	// A política é lida DEPOIS do orçamento e presa à versão que ele usou: ler
	// "a vigente" duas vezes abriria uma janela para a reserva congelar uma
	// versão e o preço ter saído de outra.
	comercial, err := s.repo.ContextoComercial(ctx, u.PropertyID, &orcamento.PolicyVersion)
	if err != nil {
		return Reserva{}, 0, err
	}

	var horas *int
	if estado == EstadoHold {
		horas = &comercial.HoldHoras
	}

	reservaID, _, expiraEm, err := s.repo.InserirReserva(ctx, NovaReserva{
		PropertyID: u.PropertyID,
		UnitTypeID: corpo.UnitTypeID,
		ContactID:  corpo.ContactID,
		BrokerID:   corpo.BrokerID,
		// O dono comercial é quem emitiu. É `users(id)`, e não `brokers(id)`,
		// porque quem o RBAC filtra é o usuário autenticado.
		OwnerID:     &u.ID,
		Origem:      corpo.Origem,
		Status:      estado,
		CheckIn:     corpo.CheckIn,
		CheckOut:    corpo.CheckOut,
		Hospedes:    corpo.Hospedes,
		IsEvento:    corpo.IsEvento,
		TipoEvento:  corpo.TipoEvento,
		HoldHoras:   horas,
		RemarcadaDe: remarcadaDe,
		Observacoes: corpo.Observacoes,
		CriadaPor:   &u.ID,
	})
	if err != nil {
		return Reserva{}, 0, err
	}

	versao := orcamento.PolicyVersion
	if err := s.repo.InserirPreco(ctx, reservaID, Preco{
		Subtotal:       orcamento.Subtotal,
		DescontoPct:    orcamento.DescontoPct,
		Desconto:       orcamento.Desconto,
		Limpeza:        orcamento.Limpeza,
		CaucaoEvento:   orcamento.CaucaoEvento,
		Total:          orcamento.Total,
		Sinal:          orcamento.Sinal,
		RateTableID:    &orcamento.RateTableID,
		PolicyVersion:  &versao,
		CancelPolicyID: comercial.PoliticaCancelamento,
	}); err != nil {
		return Reserva{}, 0, err
	}

	if err := s.repo.InserirNoites(ctx, reservaID, corpo.UnitTypeID, noitesDoOrcamento(orcamento)); err != nil {
		return Reserva{}, 0, err
	}
	if err := s.repo.InserirTitular(ctx, reservaID, corpo.ContactID); err != nil {
		return Reserva{}, 0, err
	}

	if err := s.alocar(ctx, u, produto, reservaID, corpo, estado, expiraEm); err != nil {
		return Reserva{}, 0, err
	}

	if err := s.repo.InserirEvento(ctx, reservaID, EventoCriada, map[string]any{
		"status":         estado,
		"total_cents":    orcamento.Total,
		"deposit_cents":  orcamento.Sinal,
		"rate_table_id":  orcamento.RateTableID.String(),
		"policy_version": versao,
	}, &u.ID); err != nil {
		return Reserva{}, 0, err
	}

	// Remarcação confirmada carrega o sinal: sem este evento, /cancel da nova
	// reserva leria "nada foi pago" e devolveria zero ao hóspede que já pagou.
	//
	// O TETO É O MESMO DO /confirm — o ALTO da segunda revisão. Herdar sem
	// revalidar deixava a reserva nova num estado que a própria API recusaria
	// com 422: total 225.000 e `deposit_paid_cents: 830.000` gravados lado a
	// lado. E o /cancel seguinte truncava a base em silêncio, devolvia 225.000 e
	// declarava a conta encerrada — R$ 6.050,00 do hóspede evaporados sem
	// crédito e sem dívida.
	//
	// POR QUE CRÉDITO, E NÃO RECUSAR A REMARCAÇÃO: encurtar a estadia é pedido
	// normal de hóspede, e bloquear a remarcação prenderia quem já pagou dentro
	// da reserva cara — o hóspede teria de cancelar (com retenção) para poder
	// remarcar. E devolver na hora também não dá: a Fase 1 não tem `payables`
	// (o módulo financeiro é de outra fase), e prometer uma devolução que nenhum
	// registro cobra é como o dinheiro some. O que sobra é o que um PMS faz:
	// aplicar o que cabe, registrar o resto como crédito do hóspede num evento
	// append-only, e mostrá-lo em toda tela que fala da conta.
	credito := int64(0)
	if sinalHerdado != nil {
		aplicado := *sinalHerdado
		if aplicado > orcamento.Total {
			credito = aplicado - orcamento.Total
			aplicado = orcamento.Total
		}
		if err := s.repo.InserirEvento(ctx, reservaID, EventoConfirmada, map[string]any{
			"deposit_paid_cents": aplicado,
			"inherited":          true,
			// O valor BRUTO fica na linha: sem ele, a trilha não explica de onde
			// veio o crédito nem permite refazer a conta.
			"inherited_from_cents": *sinalHerdado,
			"credit_cents":         credito,
		}, &u.ID); err != nil {
			return Reserva{}, 0, err
		}
		if credito > 0 {
			payload := map[string]any{
				"credit_cents":  credito,
				"paid_cents":    *sinalHerdado,
				"applied_cents": aplicado,
				"reason":        MotivoRemarcacao,
			}
			if remarcadaDe != nil {
				payload["from_reservation_id"] = remarcadaDe.String()
			}
			if err := s.repo.InserirEvento(ctx, reservaID, EventoCredito, payload, &u.ID); err != nil {
				return Reserva{}, 0, err
			}
		}
	}

	nova, err := s.repo.Buscar(ctx, u.PropertyID, reservaID, false, u.ID)
	if err != nil {
		return Reserva{}, 0, err
	}
	// `reservation_events` é a timeline COMERCIAL, que a tela da reserva mostra
	// ao operador; `audit_log` é a trilha de ESCRITA, que atravessa todas as
	// entidades do sistema e guarda IP, user-agent e request_id. As duas se
	// parecem aqui e divergem em todo o resto: o audit_log responde "quem mexeu
	// no sistema", a timeline responde "o que aconteceu com esta venda".
	if err := audit.Criacao(ctx, s.repo.pool, entidadeReserva, audit.VerboCriado, nova.ID, nova); err != nil {
		return Reserva{}, 0, err
	}
	return nova, credito, nil
}

// alocar escolhe e segura as unidades físicas.
//
// Os dois consumos seguem caminhos DIFERENTES de propósito, e a diferença é a
// correção do CRÍTICO 1:
//
//   - `all_members` (a White House Completa) não passa por `Candidatas`. Aquela
//     consulta filtra `u.active`, e era exatamente isso que fazia a casa inteira
//     ser vendida com sete apartamentos quando um estava em manutenção — sete
//     linhas inseridas, preço de oito, e a constraint sem nada a recusar porque
//     a oitava linha nunca existiu. A composição inteira é conferida e inserida
//     numa instrução só (ver AlocarComposicaoCompleta).
//
//   - `one_member` continua tentando as candidatas na ordem de menor
//     fragmentação e deixando a constraint recusar cada uma — nunca "consulta e
//     depois insere".
func (s *Servico) alocar(ctx context.Context, u *auth.Usuario, produto Produto, reservaID uuid.UUID, corpo ReservaCriar, estado string, expiraEm *time.Time) error {
	bloco := BlocoDeReserva{
		PropertyID: u.PropertyID,
		ReservaID:  reservaID,
		Status:     StatusDoBlocoPara(estado),
		CheckIn:    corpo.CheckIn,
		CheckOut:   corpo.CheckOut,
		ExpiraEm:   expiraEm,
		CriadoPor:  &u.ID,
		// Dono comercial da linha do calendário = dono da venda. É o mesmo nome
		// de coluna que `reservations.owner_id` para o escopo `own` do RBAC
		// virar SQL com uma regra só, sem `if recurso == "calendar"`.
		DonoID: &u.ID,
	}

	if produto.Consome == ConsomeTodosMembros {
		blocos, err := s.repo.AlocarComposicaoCompleta(ctx, produto, bloco)
		if err != nil {
			return err
		}
		return s.repo.VincularUnidades(ctx, reservaID, blocos, false)
	}

	candidatas, err := s.repo.Candidatas(ctx, u.PropertyID, produto.ID, corpo.CheckIn, corpo.CheckOut)
	if err != nil {
		return err
	}
	_, blocoID, err := s.repo.AlocarUmaUnidade(ctx, OrdenarPorFragmentacao(candidatas), bloco,
		produto.Codigo, produto.Declaradas)
	if err != nil {
		return err
	}
	return s.repo.VincularUnidades(ctx, reservaID, []uuid.UUID{blocoID}, false)
}

// ─────────────────────────── PUT / PATCH / DELETE ───────────────────

// Substituir é o PUT: substituição integral dos campos CADASTRAIS. Campo
// opcional ausente volta ao padrão.
func (s *Servico) Substituir(ctx context.Context, id uuid.UUID, corpo ReservaAtualizar) (Reserva, error) {
	if _, ok := corpo.ContactID.Definido(); !ok {
		return Reserva{}, apperr.Validation(map[string]string{"contact_id": "é obrigatório no PUT."})
	}
	if _, ok := corpo.Hospedes.Definido(); !ok {
		return Reserva{}, apperr.Validation(map[string]string{"guests_count": "é obrigatório no PUT."})
	}
	// No PUT o que não veio volta ao padrão — é o que "substituição integral"
	// significa, e é o que diferencia PUT de PATCH.
	if !corpo.BrokerID.Set {
		corpo.BrokerID = httpx.Nulo[uuid.UUID]()
	}
	if !corpo.IsEvento.Set {
		corpo.IsEvento = httpx.De(false)
	}
	if !corpo.TipoEvento.Set {
		corpo.TipoEvento = httpx.Nulo[string]()
	}
	if !corpo.Origem.Set {
		corpo.Origem = httpx.De("direto")
	}
	if !corpo.Observacoes.Set {
		corpo.Observacoes = httpx.Nulo[string]()
	}
	return s.Atualizar(ctx, id, corpo)
}

// Atualizar é o PATCH: campo ausente não muda, `null` limpa.
func (s *Servico) Atualizar(ctx context.Context, id uuid.UUID, corpo ReservaAtualizar) (Reserva, error) {
	u, err := ator(ctx)
	if err != nil {
		return Reserva{}, err
	}

	var out Reserva
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		e, err := s.repo.TravarReserva(ctx, u.PropertyID, id, somenteMinhas(ctx, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}

		contato := e.ContactID
		if v, ok := corpo.ContactID.Definido(); ok {
			contato = v
			if err := s.repo.ConferirContato(ctx, u.PropertyID, v); err != nil {
				return err
			}
		}

		hospedes := e.Hospedes
		if v, ok := corpo.Hospedes.Definido(); ok {
			hospedes = v
		}
		// A capacidade é do PRODUTO e é declarada, nunca somada da composição.
		// Sem esta checagem, um PATCH colocaria 40 pessoas na Completa (que
		// acomoda 24) sem passar pelo motor.
		if hospedes > e.Capacidade {
			return apperr.CapacityExceeded.WithDetails(map[string]any{
				"capacity": e.Capacidade, "requested": hospedes,
			})
		}

		corretor := e.BrokerID
		if v, ok := corpo.BrokerID.Definido(); ok {
			corretor = &v
		} else if corpo.BrokerID.DeveLimpar() {
			corretor = nil
		}

		isEvento := e.IsEvento
		if v, ok := corpo.IsEvento.Definido(); ok {
			isEvento = v
		}

		tipoEvento := textoOpcional(e.TipoEvento, corpo.TipoEvento)
		notas := textoOpcional(e.Observacoes, corpo.Observacoes)

		origem := e.Origem
		if v, ok := corpo.Origem.Definido(); ok {
			origem = v
		}

		if err := s.repo.AtualizarCadastro(ctx, id, contato, corretor, hospedes, isEvento, tipoEvento, origem, notas); err != nil {
			return err
		}
		if err := s.repo.InserirEvento(ctx, id, EventoCadastroEditado, map[string]any{
			"guests_count": hospedes, "source": origem,
		}, &u.ID); err != nil {
			return err
		}
		if err := s.auditarReserva(ctx, audit.VerboAlterado, id,
			cadastroAuditado{
				ContactID: e.ContactID, BrokerID: e.BrokerID, Hospedes: e.Hospedes,
				IsEvento: e.IsEvento, TipoEvento: e.TipoEvento, Origem: e.Origem, Observacoes: e.Observacoes,
			},
			cadastroAuditado{
				ContactID: contato, BrokerID: corretor, Hospedes: hospedes,
				IsEvento: isEvento, TipoEvento: tipoEvento, Origem: origem, Observacoes: notas,
			}); err != nil {
			return err
		}

		out, err = s.repo.Buscar(ctx, u.PropertyID, id, false, u.ID)
		return err
	})
	return out, err
}

// Descartar apaga a reserva que nunca existiu comercialmente.
//
// A partir de `hold` a resposta é 409: apagar reserva quebra o razão e some com
// a prova de quem segurou a data. Quem encerra a partir dali é /cancel.
func (s *Servico) Descartar(ctx context.Context, id uuid.UUID) error {
	u, err := ator(ctx)
	if err != nil {
		return err
	}

	return s.tx.Do(ctx, func(ctx context.Context) error {
		e, err := s.repo.TravarReserva(ctx, u.PropertyID, id, somenteMinhas(ctx, auth.AcaoExcluir), u.ID)
		if err != nil {
			return err
		}
		if e.Status != EstadoQuote {
			return EstadoInvalido.
				WithMessage("Reserva a partir de `hold` não se apaga; use POST /reservations/{id}/cancel.").
				WithDetails(map[string]any{
					"status":  e.Status,
					"allowed": []string{EstadoQuote},
				})
		}
		apagada, err := s.repo.DescartarQuote(ctx, id)
		if err != nil {
			return err
		}
		if !apagada {
			return apperr.NotFound("Reserva")
		}
		// `before` é a ÚNICA cópia que sobra: depois do DELETE a linha não está
		// mais em lugar nenhum, e a trilha é o que responde o que foi apagado.
		return audit.Exclusao(ctx, s.repo.pool, entidadeReserva, audit.VerboExcluido, id, instantaneo(e))
	})
}

// ─────────────────────────── Auxiliares ─────────────────────────────

func repetir(g *respostaGuardada) Resultado {
	return Resultado{Status: g.Status, Bruto: g.Corpo, Local: localDoCorpo(g.Corpo)}
}

func localDaReserva(id uuid.UUID) string { return "/api/v1/reservations/" + id.String() }

// localDoCorpo recupera o Location da resposta repetida a partir do id gravado.
// Repetir um 201 sem Location faria o cliente que confia no header falhar
// justamente no retry, que é quando ele mais precisa funcionar.
func localDoCorpo(bruto []byte) string {
	var env struct {
		Data struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bruto, &env); err != nil || env.Data.ID == uuid.Nil {
		return ""
	}
	return localDaReserva(env.Data.ID)
}

// noitesDoOrcamento traduz as diárias do orçamento para as linhas do snapshot.
// NENHUM valor é recalculado: os centavos são copiados como o motor os produziu.
func noitesDoOrcamento(o disponibilidade.Orcamento) []NoiteDaReserva {
	out := make([]NoiteDaReserva, 0, len(o.Diarias))
	for _, d := range o.Diarias {
		out = append(out, NoiteDaReserva{Noite: d.Data, Tipo: d.Tipo, Preco: d.Preco, Rotulo: d.Rotulo})
	}
	return out
}

// textoOpcional resolve a semântica de Opt[string] sobre coluna anulável:
// ausente mantém o que está gravado, valor troca, `null` limpa. É a distinção
// que a regra 6 do CLAUDE.md exige e que um *string sozinho não expressa.
func textoOpcional(atual *string, campo httpx.Opt[string]) *string {
	if v, ok := campo.Definido(); ok {
		return &v
	}
	if campo.DeveLimpar() {
		return nil
	}
	return atual
}
