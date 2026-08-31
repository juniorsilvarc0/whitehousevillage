package disponibilidade

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

// recursoOrcamentos é o recurso do RBAC de `POST /quotes` e `GET /quotes/{id}` —
// o mesmo código do catálogo semeado em cmd/seed/acesso.go. O service o cita
// para consultar o ESCOPO concedido, que o middleware não repassa.
const recursoOrcamentos = "quotes"

// entidadeOrcamento é o nome da TABELA na trilha de auditoria: é assim que a
// tela de auditoria agrupa e que `audit.Acao` monta `<entidade>.<verbo>`.
const entidadeOrcamento = "quotes"

// ═══════════════════════ POST /quotes com persist ═══════════════════

// Emitir grava o orçamento: o MESMO cálculo de `Orcar`, congelado.
//
// A conta é a de `Orcar` — literalmente, é ele que roda — e por isso simular e
// emitir nunca podem dar números diferentes. O que muda é só o destino: aqui o
// resultado vira uma linha de `quotes` e N de `quote_nights`, com
// `rate_table_id`, `policy_version` e `cancellation_policy_id` congelados junto.
//
// NÃO BLOQUEIA DATA. Nenhuma linha de `stay_blocks` nasce aqui, e a contrapartida
// — este orçamento pode virar `409 DATE_CONFLICT` na hora do ganho — é a regra
// (spec §4), não um defeito.
func (s *Servico) Emitir(ctx context.Context, e Entrada) (OrcamentoSalvo, error) {
	u, err := s.emissor(ctx)
	if err != nil {
		return OrcamentoSalvo{}, err
	}

	var salvo OrcamentoSalvo
	erroDaTx := s.tx.Do(ctx, func(ctx context.Context) error {
		agora, err := s.escrita.Agora(ctx)
		if err != nil {
			return err
		}
		validoAte, err := validade(e.ValidoAte, agora)
		if err != nil {
			return err
		}

		contato, err := s.contatoDaEmissao(ctx, u.PropertyID, e)
		if err != nil {
			return err
		}

		// O cálculo é o de sempre. Rodá-lo DENTRO da transação não é detalhe:
		// é o que faz o preço gravado e o preço devolvido serem o mesmo cálculo,
		// e não dois cálculos que por acaso coincidiram.
		orcamento, err := s.Orcar(ctx, e)
		if err != nil {
			return err
		}

		cancelamento, err := s.escrita.PoliticaDeCancelamentoVigente(ctx, u.PropertyID)
		if err != nil {
			return err
		}

		id, criadoEm, err := s.escrita.InserirOrcamento(ctx, OrcamentoGravavel{
			PropertyID:     u.PropertyID,
			ContactID:      contato,
			OportunidadeID: e.OportunidadeID,
			UnitTypeID:     e.UnitTypeID,
			CheckIn:        e.CheckIn.String(),
			CheckOut:       e.CheckOut.String(),
			Hospedes:       e.Hospedes,
			IsEvento:       e.IsEvento,

			Subtotal:    orcamento.Subtotal,
			DescontoPct: orcamento.DescontoPct,
			Desconto:    orcamento.Desconto,
			Limpeza:     orcamento.Limpeza,
			Caucao:      orcamento.CaucaoEvento,
			Total:       orcamento.Total,
			Sinal:       orcamento.Sinal,

			RateTableID:            orcamento.RateTableID,
			PolicyVersion:          orcamento.PolicyVersion,
			PoliticaCancelamentoID: cancelamento,

			// Dono comercial é quem emitiu — `users(id)`, porque quem o RBAC
			// filtra é o usuário autenticado.
			DonoID:    &u.ID,
			ValidoAte: validoAte,
			CriadoPor: &u.ID,
			Noites:    orcamento.Diarias,
		})
		if err != nil {
			return err
		}

		if e.OportunidadeID != nil {
			if err := s.escrita.AtualizarValorEsperado(ctx, *e.OportunidadeID, orcamento.Total); err != nil {
				return err
			}
		}

		salvo = OrcamentoSalvo{
			Orcamento:              orcamento,
			ID:                     id,
			ContactID:              contato,
			OportunidadeID:         e.OportunidadeID,
			DonoID:                 &u.ID,
			PoliticaCancelamentoID: cancelamento,
			ValidoAte:              validoAte,
			Vencido:                !validoAte.After(agora),
			CriadoEm:               criadoEm,
			UnitTypeID:             e.UnitTypeID,
			CheckIn:                e.CheckIn.String(),
			CheckOut:               e.CheckOut.String(),
			Hospedes:               e.Hospedes,
			IsEvento:               e.IsEvento,
		}
		return audit.Criacao(ctx, s.escrita.Pool(), entidadeOrcamento, audit.VerboCriado, id, salvo)
	})
	return salvo, erroDaTx
}

// emissor resolve quem está emitindo e confere que este Servico sabe gravar.
//
// O segundo teste não é paranoia: `NovoServico` aceita a interface de leitura, e
// um Servico montado sobre um dublê de teste não tem repositório de escrita. Erro
// nomeado é melhor que nil pointer três chamadas adiante.
func (s *Servico) emissor(ctx context.Context) (*auth.Usuario, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok || u.PropertyID == uuid.Nil {
		return nil, apperr.Unauthorized
	}
	if s.escrita == nil {
		return nil, apperr.Internal.WithMessage("Este serviço de orçamento foi montado sem repositório de escrita.")
	}
	return u, nil
}

// validade resolve `valid_until`.
//
// Ausente vale `validadePadraoEmDias` a partir de AGORA — o agora do banco, no
// fuso da casa. No passado é 422 e não um `CHECK` estourando no COMMIT:
// "proposta que nasce vencida não é proposta" merece o nome do campo, e não uma
// mensagem genérica de constraint.
func validade(pedido *time.Time, agora time.Time) (time.Time, error) {
	if pedido == nil {
		return agora.AddDate(0, 0, validadePadraoEmDias), nil
	}
	if !pedido.After(agora) {
		return time.Time{}, apperr.Validation(map[string]string{
			"valid_until": "precisa ser no futuro: proposta que nasce vencida não é proposta.",
		})
	}
	return *pedido, nil
}

// contatoDaEmissao resolve para QUEM é o orçamento e confere a coerência com a
// oportunidade.
//
// Com `opportunity_id`, o contato não é ambíguo: é o do card. Quando o corpo não
// manda `contact_id`, ele é HERDADO — um orçamento pendurado numa negociação sem
// o contato dela seria uma proposta endereçada a ninguém, e o `/win` teria de
// adivinhá-lo depois. Quando manda e diverge, é 422: orçamento colado num card
// de outra pessoa é como o `/win` acabaria criando reserva no nome errado.
func (s *Servico) contatoDaEmissao(ctx context.Context, casa uuid.UUID, e Entrada) (*uuid.UUID, error) {
	if e.ContactID != nil {
		if err := s.escrita.ConferirContato(ctx, casa, *e.ContactID); err != nil {
			return nil, err
		}
	}
	if e.OportunidadeID == nil {
		return e.ContactID, nil
	}

	doCard, err := s.escrita.OportunidadeDoOrcamento(ctx, casa, *e.OportunidadeID)
	if err != nil {
		return nil, err
	}
	if e.ContactID == nil {
		return &doCard, nil
	}
	if *e.ContactID != doCard {
		return nil, apperr.Validation(map[string]string{
			"contact_id": "este contato não é o da oportunidade informada.",
		})
	}
	return e.ContactID, nil
}

// ═══════════════════════ GET /quotes/{id} ═══════════════════════════

// BuscarOrcamento reabre o orçamento emitido COM OS VALORES DE QUANDO FOI
// EMITIDO.
//
// É o ponto inteiro de persistir. Nenhum número desta resposta é recalculado:
// subtotal, desconto, limpeza, caução, total, sinal e o preço de cada noite saem
// de `quotes` e `quote_nights`. Publicar um tarifário novo amanhã não reescreve
// o número que o hóspede ouviu ao telefone (regra 7 do CLAUDE.md).
//
// Três coisas são DERIVADAS, e nenhuma delas é dinheiro do snapshot:
//
//   - `balance_cents` = total − sinal e `avg_nightly_cents` = total ÷ noites,
//     exatos em inteiro. Não são colunas porque guardar o que se deriva é criar
//     a segunda verdade que um dia diverge — e a divisão é a MESMA de
//     `booking.Build`, inteira, para os dois caminhos darem o mesmo centavo.
//   - `min_nights` e `discount_authority` saem da tabela e da versão de política
//     CONGELADAS na linha, não das vigentes. São regra, não valor: reler a
//     alçada da política de hoje faria um orçamento antigo mudar de veredito
//     porque a gestão mexeu no limite ontem.
//   - `expired`, que o banco calcula contra `now()`.
func (s *Servico) BuscarOrcamento(ctx context.Context, id uuid.UUID) (OrcamentoSalvo, error) {
	u, err := s.emissor(ctx)
	if err != nil {
		return OrcamentoSalvo{}, err
	}

	bruto, err := s.escrita.BuscarOrcamento(ctx, u.PropertyID, id,
		auth.SomenteProprios(ctx, recursoOrcamentos, auth.AcaoVer), u.ID)
	if err != nil {
		return OrcamentoSalvo{}, err
	}
	return s.montarSalvo(ctx, u.PropertyID, bruto)
}

// montarSalvo transforma a linha crua no schema do contrato.
func (s *Servico) montarSalvo(ctx context.Context, casa uuid.UUID, b OrcamentoBruto) (OrcamentoSalvo, error) {
	noites, err := s.escrita.NoitesDoOrcamento(ctx, b.ID)
	if err != nil {
		return OrcamentoSalvo{}, err
	}

	// A tabela e a versão são as CONGELADAS na linha — é o que faz a alçada e o
	// mínimo de noites serem os daquele dia.
	comercial, err := s.repo.Contexto(ctx, casa, &b.RateTableID, &b.PolicyVersion)
	if err != nil {
		return OrcamentoSalvo{}, err
	}
	minimos, err := s.repo.EstadiaMinima(ctx, b.RateTableID)
	if err != nil {
		return OrcamentoSalvo{}, err
	}

	minimo := 1
	for _, n := range noites {
		if m, ok := minimos[n.Tipo]; ok && m > minimo {
			minimo = m
		}
	}

	orcamento := Orcamento{
		Noites:        len(noites),
		Subtotal:      b.Subtotal,
		DescontoPct:   b.DescontoPct,
		Desconto:      b.Desconto,
		Limpeza:       b.Limpeza,
		CaucaoEvento:  b.Caucao,
		Total:         b.Total,
		Sinal:         b.Sinal,
		Saldo:         b.Total - b.Sinal,
		MinNoites:     minimo,
		Alcada:        comercial.Politica.Authority(b.DescontoPct),
		PolicyVersion: b.PolicyVersion,
		RateTableID:   b.RateTableID,
		Linhas:        agruparEmLinhas(noites),
		Diarias:       noites,
	}
	if len(noites) > 0 {
		orcamento.MediaPorNoite = b.Total / int64(len(noites))
	}

	return OrcamentoSalvo{
		Orcamento:              orcamento,
		ID:                     b.ID,
		ContactID:              b.ContactID,
		OportunidadeID:         b.OportunidadeID,
		DonoID:                 b.DonoID,
		PoliticaCancelamentoID: b.PoliticaCancelamentoID,
		ValidoAte:              b.ValidoAte,
		Vencido:                b.Vencido,
		ReservaID:              b.ReservaID,
		CriadoEm:               b.CriadoEm,
		UnitTypeID:             b.UnitTypeID,
		CheckIn:                b.CheckIn,
		CheckOut:               b.CheckOut,
		Hospedes:               b.Hospedes,
		IsEvento:               b.IsEvento,
	}, nil
}

// ═══════════════════ O que o `/win` do CRM consome ══════════════════

// OrcamentoVigente devolve o id do último orçamento emitido para a oportunidade,
// ou nil quando não há nenhum. Ver `OrcamentoVigenteDaOportunidade`.
func (s *Servico) OrcamentoVigente(ctx context.Context, casa, oportunidade uuid.UUID) (*uuid.UUID, error) {
	return s.escrita.OrcamentoVigenteDaOportunidade(ctx, casa, oportunidade)
}

// OrcamentoParaGanhar lê o orçamento SEM o recorte `own`.
//
// Sem `own` de propósito: quem chega aqui já passou por `crm.opportunities:editar`
// NESTE card — o middleware e o `TravarOportunidade` decidiram que ele pode
// fechar esta negociação. Reaplicar o escopo de `quotes` faria o gerente que
// assume a carteira de um corretor de férias não conseguir ganhar o card que ele
// mesmo acabou de receber, porque o orçamento tem outro dono.
func (s *Servico) OrcamentoParaGanhar(ctx context.Context, casa, id uuid.UUID) (OrcamentoSalvo, error) {
	bruto, err := s.escrita.BuscarOrcamento(ctx, casa, id, false, uuid.Nil)
	if err != nil {
		return OrcamentoSalvo{}, err
	}
	return s.montarSalvo(ctx, casa, bruto)
}

// VincularOrcamentoAOportunidade e MarcarOrcamentoConvertido são a escrita que o
// `/win` faz na tabela deste módulo. Ficam expostas aqui — e não com o CRM
// falando SQL de `quotes` — porque a tabela tem um dono só.
func (s *Servico) VincularOrcamentoAOportunidade(ctx context.Context, orcamento, oportunidade uuid.UUID) error {
	return s.escrita.VincularAOportunidade(ctx, orcamento, oportunidade)
}

func (s *Servico) MarcarOrcamentoConvertido(ctx context.Context, orcamento, reserva uuid.UUID) error {
	return s.escrita.MarcarConvertido(ctx, orcamento, reserva)
}
