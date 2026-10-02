package crm

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// ListarLeads — GET /crm/leads.
func (s *Servico) ListarLeads(ctx context.Context, f FiltroDeLeads) ([]Lead, int64, error) {
	u, err := ator(ctx)
	if err != nil {
		return nil, 0, err
	}
	f.SomenteMinhas, f.Usuario = somenteMinhas(ctx, RecursoLeads, auth.AcaoVer), u.ID
	return s.repo.ListarLeads(ctx, u.PropertyID, f)
}

// BuscarLead — GET /crm/leads/{id}.
func (s *Servico) BuscarLead(ctx context.Context, id uuid.UUID) (Lead, error) {
	u, err := ator(ctx)
	if err != nil {
		return Lead{}, err
	}
	return s.repo.BuscarLead(ctx, u.PropertyID, id, somenteMinhas(ctx, RecursoLeads, auth.AcaoVer), u.ID)
}

// CriarLead — POST /crm/leads.
func (s *Servico) CriarLead(ctx context.Context, corpo LeadCriar) (Lead, error) {
	u, err := ator(ctx)
	if err != nil {
		return Lead{}, err
	}

	var out Lead
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.repo.ConferirContato(ctx, u.PropertyID, corpo.ContactID); err != nil {
			return err
		}

		g := LeadGravavel{
			ContactID: corpo.ContactID,
			Origem:    strings.TrimSpace(corpo.Origem.Ou("direto")),
			Status:    corpo.Status.Ou(LeadNovo),
			Score:     corpo.Score.Ou(0),
			DonoID:    donoPadrao(ctx, u, RecursoLeads, corpo.DonoID),
		}
		if err := s.preencherInteresse(ctx, u.PropertyID, &g, corpo.CampanhaID, corpo.ProdutoID,
			corpo.CheckIn, corpo.CheckOut); err != nil {
			return err
		}
		if g.DonoID != nil {
			if err := s.repo.ConferirUsuario(ctx, u.PropertyID, *g.DonoID); err != nil {
				return err
			}
		}

		id, err := s.repo.InserirLead(ctx, u.PropertyID, g, u.ID)
		if err != nil {
			return err
		}
		if out, err = s.repo.BuscarLead(ctx, u.PropertyID, id, false, u.ID); err != nil {
			return err
		}
		return audit.Criacao(ctx, s.repo.Pool(), entidadeLead, audit.VerboCriado, id, out)
	})
	return out, err
}

// preencherInteresse resolve os campos de interesse comuns ao POST e ao PATCH.
// `guests_count` e `notes` do lead NÃO passam por aqui: as colunas existem no
// schema, o contrato não as declara, e o `g` já veio da linha gravada — então
// elas atravessam a edição intactas em vez de serem zeradas por omissão. Ver o
// relatório.
func (s *Servico) preencherInteresse(ctx context.Context, propriedade uuid.UUID, g *LeadGravavel,
	campanha, produto httpx.Opt[uuid.UUID], checkIn, checkOut httpx.Opt[string]) error {

	if v, ok := campanha.Definido(); ok {
		g.CampanhaID = &v
	} else if campanha.DeveLimpar() {
		g.CampanhaID = nil
	}
	if v, ok := produto.Definido(); ok {
		if err := s.repo.ConferirProduto(ctx, propriedade, v); err != nil {
			return err
		}
		g.ProdutoID = &v
	} else if produto.DeveLimpar() {
		g.ProdutoID = nil
	}
	g.CheckIn = textoOpcional(g.CheckIn, checkIn)
	g.CheckOut = textoOpcional(g.CheckOut, checkOut)
	return nil
}

// SubstituirLead — PUT: substituição integral dos campos editáveis.
func (s *Servico) SubstituirLead(ctx context.Context, id uuid.UUID, corpo LeadAtualizar) (Lead, error) {
	if _, ok := corpo.ContactID.Definido(); !ok {
		return Lead{}, apperr.Validation(map[string]string{"contact_id": "é obrigatório no PUT."})
	}
	// No PUT o que não veio volta ao padrão — é o que "substituição integral"
	// significa, e é o que diferencia PUT de PATCH.
	if !corpo.Origem.Set {
		corpo.Origem = httpx.De("direto")
	}
	if !corpo.Status.Set {
		corpo.Status = httpx.De(LeadNovo)
	}
	if !corpo.Score.Set {
		corpo.Score = httpx.De(0)
	}
	for _, campo := range []*httpx.Opt[uuid.UUID]{&corpo.CampanhaID, &corpo.ProdutoID, &corpo.DonoID} {
		if !campo.Set {
			*campo = httpx.Nulo[uuid.UUID]()
		}
	}
	if !corpo.CheckIn.Set {
		corpo.CheckIn = httpx.Nulo[string]()
	}
	if !corpo.CheckOut.Set {
		corpo.CheckOut = httpx.Nulo[string]()
	}
	return s.AtualizarLead(ctx, id, corpo)
}

// AtualizarLead — PATCH: ausente não muda, `null` limpa.
func (s *Servico) AtualizarLead(ctx context.Context, id uuid.UUID, corpo LeadAtualizar) (Lead, error) {
	u, err := ator(ctx)
	if err != nil {
		return Lead{}, err
	}

	var out Lead
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, _, err := s.repo.TravarLead(ctx, u.PropertyID, id, somenteMinhas(ctx, RecursoLeads, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		// Lead já convertido não volta atrás por edição: o `converted_at` e o
		// `status` são amarrados por CHECK, e reabrir aqui deixaria a
		// oportunidade nascida dele apontando para uma origem que diz que nunca
		// converteu.
		if antes.Status == LeadConvertido {
			if v, ok := corpo.Status.Definido(); ok && v != LeadConvertido {
				return apperr.Validation(map[string]string{
					"status": "lead convertido não volta a outro estado; a oportunidade dele já existe.",
				})
			}
		}

		g := antes
		if v, ok := corpo.ContactID.Definido(); ok {
			if err := s.repo.ConferirContato(ctx, u.PropertyID, v); err != nil {
				return err
			}
			g.ContactID = v
		}
		if v, ok := corpo.Origem.Definido(); ok {
			g.Origem = strings.TrimSpace(v)
		}
		if v, ok := corpo.Status.Definido(); ok {
			g.Status = v
		}
		if v, ok := corpo.Score.Definido(); ok {
			g.Score = v
		}
		if v, ok := corpo.DonoID.Definido(); ok {
			if err := s.repo.ConferirUsuario(ctx, u.PropertyID, v); err != nil {
				return err
			}
			g.DonoID = &v
		} else if corpo.DonoID.DeveLimpar() {
			g.DonoID = nil
		}
		if err := s.preencherInteresse(ctx, u.PropertyID, &g, corpo.CampanhaID, corpo.ProdutoID,
			corpo.CheckIn, corpo.CheckOut); err != nil {
			return err
		}

		if err := s.repo.AtualizarLead(ctx, id, g); err != nil {
			return err
		}
		if out, err = s.repo.BuscarLead(ctx, u.PropertyID, id, false, u.ID); err != nil {
			return err
		}
		return audit.Alteracao(ctx, s.repo.Pool(), entidadeLead, audit.VerboAlterado, id, antes, g)
	})
	return out, err
}

// ExcluirLead — DELETE /crm/leads/{id}: remoção física, e só do lead que NÃO
// virou oportunidade.
//
// Convertido responde 409: a oportunidade guarda `lead_id` como origem, e é dela
// que sai a conversão por origem no BI. Para descartar sem apagar, o caminho é
// `status: descartado` — que alimenta o funil de perdas antes mesmo de existir
// oportunidade.
func (s *Servico) ExcluirLead(ctx context.Context, id uuid.UUID) error {
	u, err := ator(ctx)
	if err != nil {
		return err
	}

	return s.tx.Do(ctx, func(ctx context.Context) error {
		antes, oportunidade, err := s.repo.TravarLead(ctx, u.PropertyID, id,
			somenteMinhas(ctx, RecursoLeads, auth.AcaoExcluir), u.ID)
		if err != nil {
			return err
		}
		if oportunidade != "" {
			return apperr.ResourceInUse.
				WithMessage("Este lead já virou oportunidade; use `status: descartado` para tirá-lo da fila.").
				WithDetails(map[string]any{"opportunity_id": oportunidade})
		}
		if err := s.repo.ExcluirLead(ctx, id); err != nil {
			return err
		}
		return audit.Exclusao(ctx, s.repo.Pool(), entidadeLead, audit.VerboExcluido, id, antes)
	})
}

// Converter — POST /crm/leads/{id}/convert.
//
// Numa transação: cria a oportunidade com o contato, o interesse e as datas do
// lead, marca o lead como `convertido`, e ACIONA A ENTRADA na primeira etapa —
// inclusive a tarefa automática dela.
//
// O segundo `/convert` é 409 com `details.opportunity_id`: o segundo clique do
// operador abre o card certo em vez de criar um card gêmeo no funil.
func (s *Servico) Converter(ctx context.Context, id uuid.UUID, corpo PedidoDeConversao) (ResultadoDeConversao, error) {
	u, err := ator(ctx)
	if err != nil {
		return ResultadoDeConversao{}, err
	}

	var out ResultadoDeConversao
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		lead, oportunidade, err := s.repo.TravarLead(ctx, u.PropertyID, id,
			somenteMinhas(ctx, RecursoLeads, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		if oportunidade != "" {
			return apperr.LeadAlreadyConverted.WithDetails(map[string]any{"opportunity_id": oportunidade})
		}

		// O corpo SOBREPÕE o lead; o que ele omite é herdado. É o que permite
		// corrigir a data na hora de abrir o card sem ter de editar o lead
		// antes.
		criar := OportunidadeCriar{
			ContactID:      lead.ContactID,
			LeadID:         httpx.De(id),
			FunilID:        corpo.FunilID,
			EtapaID:        corpo.EtapaID,
			ProdutoID:      herdarUUID(corpo.ProdutoID, lead.ProdutoID),
			CheckIn:        herdarTexto(corpo.CheckIn, lead.CheckIn),
			CheckOut:       herdarTexto(corpo.CheckOut, lead.CheckOut),
			Valor:          corpo.Valor,
			FechamentoPrev: corpo.FechamentoPrev,
			// `owner_id` ausente herda o dono do LEAD — trocar de dono na
			// conversão é possível, mas explícito.
			DonoID: herdarUUID(corpo.DonoID, lead.DonoID),
		}

		nova, tarefa, err := s.criarOportunidade(ctx, u, criar)
		if err != nil {
			return err
		}
		if err := s.repo.MarcarLeadConvertido(ctx, id); err != nil {
			return err
		}
		if out.Lead, err = s.repo.BuscarLead(ctx, u.PropertyID, id, false, u.ID); err != nil {
			return err
		}
		out.Oportunidade, out.TarefaAuto = nova, tarefa

		return audit.Alteracao(ctx, s.repo.Pool(), entidadeLead, verboConvertido, id,
			map[string]any{"status": lead.Status},
			map[string]any{"status": LeadConvertido, "opportunity_id": nova.ID})
	})
	return out, err
}

// herdarUUID/herdarTexto/herdarInt implementam o "o corpo sobrepõe, a omissão
// herda" da conversão. `null` explícito no corpo LIMPA — quem manda `null` está
// dizendo "não quero o que o lead tinha".
func herdarUUID(pedido httpx.Opt[uuid.UUID], doLead *uuid.UUID) httpx.Opt[uuid.UUID] {
	if pedido.Set {
		return pedido
	}
	if doLead == nil {
		return httpx.Opt[uuid.UUID]{}
	}
	return httpx.De(*doLead)
}

func herdarTexto(pedido httpx.Opt[string], doLead *string) httpx.Opt[string] {
	if pedido.Set {
		return pedido
	}
	if doLead == nil {
		return httpx.Opt[string]{}
	}
	return httpx.De(*doLead)
}

// textoOpcional resolve a semântica de Opt[string] sobre coluna anulável:
// ausente mantém, valor troca, `null` limpa.
func textoOpcional(atual *string, campo httpx.Opt[string]) *string {
	if v, ok := campo.Definido(); ok {
		return &v
	}
	if campo.DeveLimpar() {
		return nil
	}
	return atual
}
