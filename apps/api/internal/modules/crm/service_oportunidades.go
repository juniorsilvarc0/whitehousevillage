package crm

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// ListarOportunidades — GET /crm/opportunities.
func (s *Servico) ListarOportunidades(ctx context.Context, f FiltroDeOportunidades) ([]Oportunidade, int64, error) {
	u, err := ator(ctx)
	if err != nil {
		return nil, 0, err
	}
	f.SomenteMinhas, f.Usuario = somenteMinhas(ctx, RecursoOportunidades, auth.AcaoVer), u.ID

	lista, total, err := s.repo.ListarOportunidades(ctx, u.PropertyID, f)
	if err != nil {
		return nil, 0, err
	}
	agora, err := s.repo.Agora(ctx)
	if err != nil {
		return nil, 0, err
	}
	// O "agora" é lido UMA vez e aplicado a todas as linhas: sem isso, uma
	// listagem longa julgaria a primeira e a última linha por instantes
	// diferentes, e o mesmo card poderia mudar de cor entre duas páginas.
	for i := range lista {
		lista[i].SLAEstourado = slaEstourado(lista[i].SLAVenceEm, lista[i].Status, agora)
	}
	return lista, total, nil
}

// BuscarOportunidade — GET /crm/opportunities/{id}.
func (s *Servico) BuscarOportunidade(ctx context.Context, id uuid.UUID) (Oportunidade, error) {
	u, err := ator(ctx)
	if err != nil {
		return Oportunidade{}, err
	}
	return s.buscarComSLA(ctx, u, id, somenteMinhas(ctx, RecursoOportunidades, auth.AcaoVer))
}

func (s *Servico) buscarComSLA(ctx context.Context, u *auth.Usuario, id uuid.UUID, restrito bool) (Oportunidade, error) {
	o, err := s.repo.BuscarOportunidade(ctx, u.PropertyID, id, restrito, u.ID)
	if err != nil {
		return o, err
	}
	agora, err := s.repo.Agora(ctx)
	if err != nil {
		return o, err
	}
	o.SLAEstourado = slaEstourado(o.SLAVenceEm, o.Status, agora)
	return o, nil
}

// CriarOportunidade — POST /crm/opportunities.
//
// Não exige `Idempotency-Key`: nada de dinheiro nem de calendário nasce aqui. A
// reserva só aparece em `/win`, e é lá que a chave é obrigatória.
func (s *Servico) CriarOportunidade(ctx context.Context, corpo OportunidadeCriar) (Oportunidade, error) {
	u, err := ator(ctx)
	if err != nil {
		return Oportunidade{}, err
	}

	var out Oportunidade
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		out, _, err = s.criarOportunidade(ctx, u, corpo)
		return err
	})
	return out, err
}

// criarOportunidade é o miolo compartilhado por POST /crm/opportunities e por
// POST /crm/leads/{id}/convert. Roda SEMPRE dentro de transação.
//
// A entrada na etapa inicial não é um caso especial: ela grava
// `crm_stage_history` (com `from_stage_id` nulo, que é o que "primeira entrada"
// significa) e dispara a tarefa automática como qualquer outra entrada. Um
// caminho separado para "a primeira vez" é como a tarefa da etapa "Novo lead"
// deixaria de nascer sem ninguém notar.
func (s *Servico) criarOportunidade(ctx context.Context, u *auth.Usuario, corpo OportunidadeCriar) (Oportunidade, *Atividade, error) {
	if err := s.repo.ConferirContato(ctx, u.PropertyID, corpo.ContactID); err != nil {
		return Oportunidade{}, nil, err
	}

	funil, err := s.resolverFunil(ctx, u.PropertyID, corpo.FunilID)
	if err != nil {
		return Oportunidade{}, nil, err
	}
	etapa, err := s.resolverEtapa(ctx, u.PropertyID, funil, corpo.EtapaID)
	if err != nil {
		return Oportunidade{}, nil, err
	}

	g := OportunidadeGravavel{
		ContactID:     corpo.ContactID,
		FunilID:       funil,
		EtapaID:       etapa.ID,
		Valor:         corpo.Valor.Ou(0),
		Probabilidade: corpo.Probabilidade.Ou(etapa.Probabilidade),
		DonoID:        donoPadrao(ctx, u, RecursoOportunidades, corpo.DonoID),
	}
	if v, ok := corpo.LeadID.Definido(); ok {
		if err := s.repo.ConferirLead(ctx, u.PropertyID, v); err != nil {
			return Oportunidade{}, nil, err
		}
		g.LeadID = &v
	}
	if v, ok := corpo.ProdutoID.Definido(); ok {
		if err := s.repo.ConferirProduto(ctx, u.PropertyID, v); err != nil {
			return Oportunidade{}, nil, err
		}
		g.ProdutoID = &v
	}
	if g.DonoID != nil {
		if err := s.repo.ConferirUsuario(ctx, u.PropertyID, *g.DonoID); err != nil {
			return Oportunidade{}, nil, err
		}
	}
	g.CheckIn = textoOpcional(nil, corpo.CheckIn)
	g.CheckOut = textoOpcional(nil, corpo.CheckOut)
	g.FechamentoPrev = textoOpcional(nil, corpo.FechamentoPrev)

	contato, err := s.repo.ContatoResumido(ctx, u.PropertyID, corpo.ContactID)
	if err != nil {
		return Oportunidade{}, nil, err
	}
	g.Titulo = tituloDoCard(contato.Nome, nil, g.CheckIn, g.CheckOut)

	id, err := s.repo.InserirOportunidade(ctx, u.PropertyID, g, StatusDaEtapa(etapa.Tipo), u.ID)
	if err != nil {
		return Oportunidade{}, nil, err
	}
	if e, ok := corpo.Evento.Definido(); ok {
		if err := s.repo.GravarDetalheDeEvento(ctx, id, detalheDeEvento(DetalheDeEvento{}, e)); err != nil {
			return Oportunidade{}, nil, err
		}
	}

	tarefa, err := s.entrarNaEtapa(ctx, u, id, nil, etapa, g.DonoID, corpo.ContactID, nil)
	if err != nil {
		return Oportunidade{}, nil, err
	}

	nova, err := s.buscarComSLA(ctx, u, id, false)
	if err != nil {
		return Oportunidade{}, nil, err
	}
	if err := audit.Criacao(ctx, s.repo.Pool(), entidadeOportunidade, audit.VerboCriado, id, nova); err != nil {
		return Oportunidade{}, nil, err
	}
	return nova, tarefa, nil
}

// resolverFunil devolve o funil pedido, ou o `is_default` quando não veio.
func (s *Servico) resolverFunil(ctx context.Context, propriedade uuid.UUID, pedido httpx.Opt[uuid.UUID]) (uuid.UUID, error) {
	if v, ok := pedido.Definido(); ok {
		if _, err := s.repo.BuscarFunil(ctx, propriedade, nil, v); err != nil {
			return uuid.Nil, err
		}
		return v, nil
	}
	return s.repo.FunilPadrao(ctx, propriedade)
}

// resolverEtapa devolve a etapa pedida — conferindo que ela é DO FUNIL — ou a de
// menor `position` quando não veio.
func (s *Servico) resolverEtapa(ctx context.Context, propriedade, funil uuid.UUID, pedido httpx.Opt[uuid.UUID]) (Etapa, error) {
	if v, ok := pedido.Definido(); ok {
		etapa, err := s.repo.BuscarEtapa(ctx, propriedade, v)
		if err != nil {
			return Etapa{}, err
		}
		if etapa.FunilID != funil {
			return Etapa{}, EtapaForaDoFunil.WithDetails(map[string]any{
				"stage_id": v, "pipeline_id": funil,
			})
		}
		return etapa, nil
	}
	return s.repo.PrimeiraEtapaDoFunil(ctx, funil)
}

// entrarNaEtapa é O MECANISMO do módulo: registra o movimento e cria a tarefa
// automática. Roda em toda entrada — criação, conversão e `/stage`.
//
// # A tarefa automática, e por que "sair e voltar" tem o comportamento que tem
//
// A criação é condicionada a NÃO existir tarefa automática PENDENTE da mesma
// etapa, e quem decide isso é a parcial única do banco (ver
// `CriarTarefaAutomatica`). As três consequências, escolhidas de propósito:
//
//   - mover o card para fora e de volta no mesmo dia NÃO empilha dois
//     follow-ups do mesmo assunto — e o kanban com arrasto otimista reenvia a
//     mesma transição mais vezes do que se imagina;
//   - voltar à etapa DEPOIS de a tarefa ter sido concluída cria outra: é um
//     ciclo de SLA novo. O cliente que voltou para "Negociação" depois de ter
//     recebido o orçamento precisa de um follow-up novo, não do silêncio de uma
//     tarefa que já foi feita;
//   - a tarefa que o operador APAGOU não renasce por reentrada enquanto não for
//     concluída nem cancelada — porque apagada ela some do predicado, e a
//     próxima entrada cria outra. Quem apagou resolveu por fora; quem reentra
//     está começando de novo.
//
// Devolver `nil` quando não criou é informação, não omissão: a tela precisa
// saber que a idempotência agiu, e não receber a tarefa antiga como se fosse
// nova.
func (s *Servico) entrarNaEtapa(ctx context.Context, u *auth.Usuario, oportunidade uuid.UUID,
	de *uuid.UUID, etapa Etapa, dono *uuid.UUID, contato uuid.UUID, motivo *string) (*Atividade, error) {

	if err := s.repo.RegistrarMovimento(ctx, oportunidade, de, etapa.ID, &u.ID, motivo); err != nil {
		return nil, err
	}
	if !etapa.TemTarefaAutomatica() {
		return nil, nil
	}
	prazoEmDias, ok := etapa.PrazoDaTarefa()
	if !ok {
		return nil, nil
	}

	agora, err := s.repo.Agora(ctx)
	if err != nil {
		return nil, err
	}
	vence := prazoDaTarefa(agora, prazoEmDias)

	// Responsável = DONO da oportunidade (spec §7). Sem dono, cai em quem
	// moveu: tarefa sem responsável é tarefa que ninguém faz, e a coluna
	// `owner_id` de `crm_activities` é o que o escopo `own` compara.
	responsavel := u.ID
	if dono != nil {
		responsavel = *dono
	}

	tipo := AtividadeTarefa
	if etapa.TarefaTipo != nil {
		tipo = *etapa.TarefaTipo
	}

	id, err := s.repo.CriarTarefaAutomatica(ctx, u.PropertyID, AtividadeGravavel{
		Tipo:           tipo,
		Assunto:        *etapa.TarefaAssunto,
		VenceEm:        &vence,
		Prioridade:     PrioridadeNormal,
		OportunidadeID: &oportunidade,
		ContactID:      &contato,
		EtapaID:        &etapa.ID,
		DonoID:         responsavel,
	}, u.ID)
	if err != nil || id == nil {
		return nil, err
	}

	tarefa, err := s.repo.BuscarAtividade(ctx, u.PropertyID, *id, false, u.ID)
	if err != nil {
		return nil, err
	}
	if err := audit.Criacao(ctx, s.repo.Pool(), entidadeAtividade, audit.VerboCriado, *id, tarefa); err != nil {
		return nil, err
	}
	return &tarefa, nil
}

// SubstituirOportunidade — PUT: substituição integral dos campos cadastrais.
func (s *Servico) SubstituirOportunidade(ctx context.Context, id uuid.UUID, corpo OportunidadeAtualizar) (Oportunidade, error) {
	if _, ok := corpo.ContactID.Definido(); !ok {
		return Oportunidade{}, apperr.Validation(map[string]string{"contact_id": "é obrigatório no PUT."})
	}
	for _, campo := range []*httpx.Opt[uuid.UUID]{&corpo.LeadID, &corpo.ProdutoID, &corpo.DonoID} {
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
	if !corpo.FechamentoPrev.Set {
		corpo.FechamentoPrev = httpx.Nulo[string]()
	}
	if !corpo.Valor.Set {
		corpo.Valor = httpx.De(int64(0))
	}
	if !corpo.Evento.Set {
		corpo.Evento = httpx.Nulo[DetalheDeEventoEntrada]()
	}
	return s.AtualizarOportunidade(ctx, id, corpo)
}

// AtualizarOportunidade — PATCH.
//
// Oportunidade já fechada recusa edição com `OPPORTUNITY_ALREADY_CLOSED`: o que
// está fechado é história, e o BI lê essa história. Negócio que renasce é
// oportunidade NOVA, com o mesmo contato — assim a conversão do funil não conta
// a mesma venda duas vezes.
func (s *Servico) AtualizarOportunidade(ctx context.Context, id uuid.UUID, corpo OportunidadeAtualizar) (Oportunidade, error) {
	u, err := ator(ctx)
	if err != nil {
		return Oportunidade{}, err
	}

	var out Oportunidade
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		travada, err := s.repo.TravarOportunidade(ctx, u.PropertyID, id,
			somenteMinhas(ctx, RecursoOportunidades, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		if travada.Status != OportunidadeAberta {
			return OportunidadeFechada.WithDetails(map[string]any{"status": EstadoParaContrato(travada.Status)})
		}

		antes, err := s.repo.BuscarOportunidade(ctx, u.PropertyID, id, false, u.ID)
		if err != nil {
			return err
		}

		g := OportunidadeGravavel{
			ContactID: antes.ContactID,
			LeadID:    antes.LeadID,
			FunilID:   antes.FunilID,
			EtapaID:   antes.EtapaID,
			ProdutoID: antes.ProdutoID,
			CheckIn:   antes.CheckIn,
			CheckOut:  antes.CheckOut,
			// `guests_count` vem da linha TRAVADA, e não do corpo: a coluna
			// existe no schema, o contrato não a expõe, e um UPDATE que a
			// omitisse a zeraria em toda edição. Ver o relatório.
			Hospedes:       travada.Hospedes,
			Valor:          antes.Valor,
			Probabilidade:  antes.Probabilidade,
			FechamentoPrev: antes.FechamentoPrev,
			DonoID:         antes.DonoID,
		}

		if v, ok := corpo.ContactID.Definido(); ok {
			if err := s.repo.ConferirContato(ctx, u.PropertyID, v); err != nil {
				return err
			}
			g.ContactID = v
		}
		if v, ok := corpo.LeadID.Definido(); ok {
			if err := s.repo.ConferirLead(ctx, u.PropertyID, v); err != nil {
				return err
			}
			g.LeadID = &v
		} else if corpo.LeadID.DeveLimpar() {
			g.LeadID = nil
		}
		if v, ok := corpo.ProdutoID.Definido(); ok {
			if err := s.repo.ConferirProduto(ctx, u.PropertyID, v); err != nil {
				return err
			}
			g.ProdutoID = &v
		} else if corpo.ProdutoID.DeveLimpar() {
			g.ProdutoID = nil
		}
		if v, ok := corpo.DonoID.Definido(); ok {
			if err := s.repo.ConferirUsuario(ctx, u.PropertyID, v); err != nil {
				return err
			}
			g.DonoID = &v
		} else if corpo.DonoID.DeveLimpar() {
			g.DonoID = nil
		}
		g.CheckIn = textoOpcional(g.CheckIn, corpo.CheckIn)
		g.CheckOut = textoOpcional(g.CheckOut, corpo.CheckOut)
		g.FechamentoPrev = textoOpcional(g.FechamentoPrev, corpo.FechamentoPrev)
		if v, ok := corpo.Valor.Definido(); ok {
			g.Valor = v
		}
		if v, ok := corpo.Probabilidade.Definido(); ok {
			g.Probabilidade = v
		}

		// A data de saída é EXCLUSIVA, como toda data de saída do sistema: a
		// checagem cruzada precisa ser feita sobre o valor RESULTANTE, senão um
		// PATCH que mexe só no `check_in` passaria por cima da que o POST fez.
		if g.CheckIn != nil && g.CheckOut != nil && *g.CheckOut <= *g.CheckIn {
			return apperr.Validation(map[string]string{"check_out": "deve ser depois de check_in."})
		}

		contato, err := s.repo.ContatoResumido(ctx, u.PropertyID, g.ContactID)
		if err != nil {
			return err
		}
		g.Titulo = tituloDoCard(contato.Nome, nil, g.CheckIn, g.CheckOut)

		if err := s.repo.AtualizarOportunidade(ctx, id, g); err != nil {
			return err
		}
		if e, ok := corpo.Evento.Definido(); ok {
			atual := DetalheDeEvento{}
			if antes.Evento != nil {
				atual = *antes.Evento
			}
			if err := s.repo.GravarDetalheDeEvento(ctx, id, detalheDeEvento(atual, e)); err != nil {
				return err
			}
		} else if corpo.Evento.DeveLimpar() {
			if err := s.repo.ApagarDetalheDeEvento(ctx, id); err != nil {
				return err
			}
		}

		if out, err = s.buscarComSLA(ctx, u, id, false); err != nil {
			return err
		}
		return audit.Alteracao(ctx, s.repo.Pool(), entidadeOportunidade, audit.VerboAlterado, id, antes, out)
	})
	return out, err
}

// detalheDeEvento aplica o Opt sobre o satélite: ausente mantém, valor troca.
func detalheDeEvento(atual DetalheDeEvento, corpo DetalheDeEventoEntrada) DetalheDeEvento {
	atual.TipoDeEvento = textoOpcional(atual.TipoDeEvento, corpo.TipoDeEvento)
	atual.Observacoes = textoOpcional(atual.Observacoes, corpo.Observacoes)
	if v, ok := corpo.ConvidadosPrev.Definido(); ok {
		atual.ConvidadosPrev = &v
	} else if corpo.ConvidadosPrev.DeveLimpar() {
		atual.ConvidadosPrev = nil
	}
	if v, ok := corpo.PrecisaBuffet.Definido(); ok {
		atual.PrecisaBuffet = v
	}
	return atual
}

// ExcluirOportunidade — DELETE: remoção física, e só do que está ABERTO e SEM
// reserva.
//
// Ganha, perdida ou com reserva é 409: é a origem comercial da venda, e é dela
// que saem conversão, motivos de perda e desempenho por corretor. Apagar leva
// junto o histórico de etapas e as atividades — por isso só se apaga o que ainda
// não virou história.
func (s *Servico) ExcluirOportunidade(ctx context.Context, id uuid.UUID) error {
	u, err := ator(ctx)
	if err != nil {
		return err
	}

	return s.tx.Do(ctx, func(ctx context.Context) error {
		travada, err := s.repo.TravarOportunidade(ctx, u.PropertyID, id,
			somenteMinhas(ctx, RecursoOportunidades, auth.AcaoExcluir), u.ID)
		if err != nil {
			return err
		}
		if travada.Status != OportunidadeAberta || travada.ReservaID != nil {
			detalhes := map[string]any{"status": EstadoParaContrato(travada.Status)}
			if travada.ReservaID != nil {
				detalhes["reservation_id"] = *travada.ReservaID
			}
			return RecursoEmUso.
				WithMessage("Oportunidade fechada ou com reserva não se apaga: é a origem comercial da venda.").
				WithDetails(detalhes)
		}

		antes, err := s.repo.BuscarOportunidade(ctx, u.PropertyID, id, false, u.ID)
		if err != nil {
			return err
		}
		if err := s.repo.ExcluirOportunidade(ctx, id); err != nil {
			return err
		}
		return audit.Exclusao(ctx, s.repo.Pool(), entidadeOportunidade, audit.VerboExcluido, id, antes)
	})
}

// ═══════════════════════════ /stage ═════════════════════════════════

// MudarEtapa — POST /crm/opportunities/{id}/stage.
func (s *Servico) MudarEtapa(ctx context.Context, id uuid.UUID, corpo PedidoDeMudancaDeEtapa) (ResultadoDeMudancaDeEtapa, error) {
	u, err := ator(ctx)
	if err != nil {
		return ResultadoDeMudancaDeEtapa{}, err
	}

	var out ResultadoDeMudancaDeEtapa
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		travada, err := s.repo.TravarOportunidade(ctx, u.PropertyID, id,
			somenteMinhas(ctx, RecursoOportunidades, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		if travada.Status != OportunidadeAberta {
			return OportunidadeFechada.WithDetails(map[string]any{"status": EstadoParaContrato(travada.Status)})
		}

		// Guarda otimista do kanban: dois corretores arrastando o mesmo card, e
		// o segundo descobre que o quadro mudou em vez de sobrescrever em
		// silêncio. `details.current_stage_id` diz para onde o card foi.
		if de, ok := corpo.DeEtapaID.Definido(); ok && de != travada.EtapaID {
			return TransicaoInvalida.
				WithMessage("O card já não está nessa etapa; alguém o moveu antes.").
				WithDetails(map[string]any{
					"from_stage_id":    de,
					"current_stage_id": travada.EtapaID,
				})
		}

		etapa, err := s.repo.BuscarEtapa(ctx, u.PropertyID, corpo.EtapaID)
		if err != nil {
			return err
		}
		if etapa.FunilID != travada.FunilID {
			return EtapaForaDoFunil.WithDetails(map[string]any{
				"stage_id": corpo.EtapaID, "pipeline_id": travada.FunilID,
			})
		}
		// Etapa terminal não se alcança por aqui. Não é preciosismo: ganhar cria
		// RESERVA (e por isso exige `Idempotency-Key`) e perder exige MOTIVO.
		// Deixar `/stage` fazer isso significaria criar reserva sem chave de
		// idempotência num arrasto de kanban — o gesto que mais se repete por
		// engano.
		if EtapaTerminal(etapa.Tipo) {
			endpoint := "/crm/opportunities/" + id.String() + "/win"
			if etapa.Tipo == EtapaPerdido {
				endpoint = "/crm/opportunities/" + id.String() + "/lose"
			}
			return TransicaoInvalida.
				WithMessage("Etapa terminal não se alcança por /stage; use o endpoint da ação.").
				WithDetails(map[string]any{
					"stage_type": etapa.Tipo,
					"endpoint":   endpoint,
				})
		}
		if etapa.ID == travada.EtapaID {
			// O CHECK `crm_stage_history_movimento` recusa origem igual a
			// destino. Traduzir aqui evita que o arrasto que solta o card na
			// própria coluna vire 422 do banco — e evita reiniciar o SLA por um
			// gesto que não mudou nada.
			return TransicaoInvalida.
				WithMessage("O card já está nesta etapa.").
				WithDetails(map[string]any{"current_stage_id": travada.EtapaID})
		}

		probabilidade := etapa.Probabilidade
		if v, ok := corpo.Probabilidade.Definido(); ok {
			probabilidade = v
		}
		if err := s.repo.MoverEtapa(ctx, id, etapa.ID, probabilidade); err != nil {
			return err
		}

		de := travada.EtapaID
		motivo := textoOpcional(nil, corpo.Motivo)
		tarefa, err := s.entrarNaEtapa(ctx, u, id, &de, etapa, travada.DonoID, travada.ContactID, motivo)
		if err != nil {
			return err
		}

		if out.Oportunidade, err = s.buscarComSLA(ctx, u, id, false); err != nil {
			return err
		}
		out.TarefaAuto = tarefa
		out.SLA = faixaDeSLA(out.Oportunidade, etapa)

		return audit.Alteracao(ctx, s.repo.Pool(), entidadeOportunidade, verboMovido, id,
			map[string]any{"stage_id": de},
			map[string]any{"stage_id": etapa.ID, "reason": motivo, "auto_task_created": tarefa != nil})
	})
	return out, err
}

// ═══════════════════════════ /win ═══════════════════════════════════

const rotaDeGanho = "POST /crm/opportunities/{id}/win"

// Ganhar — POST /crm/opportunities/{id}/win.
//
// O ponto do módulo inteiro: GANHAR NÃO REDIGITA NADA. Numa transação, o
// orçamento vigente vira reserva `hold` pelo MESMO caminho do
// `POST /reservations` — recalculando pelo motor e congelando
// `reservation_nights`, `rate_table_id`, `policy_version` e
// `cancellation_policy_id` —, a oportunidade vai para a etapa `ganho` com
// histórico, e as tarefas automáticas pendentes são concluídas.
//
// # Nasce `hold`, não `confirmed`
//
// Ganhar no CRM é o ACORDO comercial; `confirmed` é DINHEIRO RECEBIDO (spec §5).
// Nascer `confirmed` faria o financeiro gerar recebível quitado de um sinal que
// ninguém pagou.
//
// # A ordem das operações é a regra 3 da rodada
//
// A reserva é criada ANTES de a oportunidade ser fechada, e as duas estão na
// mesma transação. Se a data foi vendida enquanto se negociava — o caso mais
// comum da alta temporada —, o `23P01` da constraint vira `409 DATE_CONFLICT`,
// a transação inteira volta e a oportunidade continua ABERTA, na etapa em que
// estava. Fechar a oportunidade e falhar a reserva deixaria uma venda ganha sem
// venda.
func (s *Servico) Ganhar(ctx context.Context, id uuid.UUID, chave string, corpo PedidoDeGanho) (Resultado, error) {
	u, err := ator(ctx)
	if err != nil {
		return Resultado{}, err
	}
	hash, err := impressao(struct {
		ID    uuid.UUID     `json:"id"`
		Corpo PedidoDeGanho `json:"corpo"`
	}{id, corpo})
	if err != nil {
		return Resultado{}, err
	}

	var saida Resultado
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		guardada, err := s.repo.reservarChave(ctx, chave, rotaDeGanho, hash, donoDo(u))
		if err != nil {
			return err
		}
		if guardada != nil {
			saida = Resultado{Status: guardada.Status, Bruto: guardada.Corpo}
			return nil
		}

		resultado, status, err := s.ganhar(ctx, u, id, corpo)
		if err != nil {
			return err
		}

		env := envelope{Data: resultado}
		if err := s.repo.guardarResposta(ctx, chave, rotaDeGanho, donoDo(u), status, env); err != nil {
			return err
		}
		saida = Resultado{Status: status, Corpo: env}
		if status == http.StatusCreated {
			saida.Local = "/api/v1/reservations/" + resultado.Reserva.ID.String()
		}
		return nil
	})
	return saida, err
}

func (s *Servico) ganhar(ctx context.Context, u *auth.Usuario, id uuid.UUID, corpo PedidoDeGanho) (ResultadoDeGanho, int, error) {
	travada, err := s.repo.TravarOportunidade(ctx, u.PropertyID, id,
		somenteMinhas(ctx, RecursoOportunidades, auth.AcaoEditar), u.ID)
	if err != nil {
		return ResultadoDeGanho{}, 0, err
	}
	if travada.Status != OportunidadeAberta {
		return ResultadoDeGanho{}, 0, OportunidadeFechada.
			WithDetails(map[string]any{"status": EstadoParaContrato(travada.Status)})
	}

	terminal, err := s.repo.EtapaTerminalDoFunil(ctx, travada.FunilID, EtapaGanho)
	if err != nil {
		return ResultadoDeGanho{}, 0, err
	}

	status := http.StatusOK
	reservaID := travada.ReservaID
	criouReserva := false

	if reservaID == nil {
		// A oportunidade ainda não tem reserva: o orçamento vigente vira uma.
		orcamento, err := s.orcamentoVigente(ctx, u, travada, corpo.OrcamentoID)
		if err != nil {
			return ResultadoDeGanho{}, 0, err
		}

		// A chave de idempotência do `/win` é reaproveitada no
		// `POST /reservations` interno: a mesma chave, do mesmo ator, em duas
		// rotas diferentes são duas linhas independentes em `idempotency_keys`
		// (a PK é `(key, endpoint, actor_id, property_id)`), e é isso que faz o
		// replay do `/win` nunca chegar a criar uma segunda reserva.
		nova, err := s.vendas.Criar(ctx, chaveInternaDaReserva(id), reservas.ReservaCriar{
			UnitTypeID:  orcamento.UnitTypeID,
			CheckIn:     orcamento.CheckIn,
			CheckOut:    orcamento.CheckOut,
			Hospedes:    orcamento.Hospedes,
			DescontoPct: orcamento.DescontoPct,
			IsEvento:    orcamento.IsEvento,
			TipoEvento:  orcamento.TipoEvento,
			ContactID:   orcamento.ContactID,
			Origem:      "crm",
		})
		if err != nil {
			// As recusas do motor valem inteiras aqui — DATE_CONFLICT,
			// MIN_STAY_NOT_MET, CAPACITY_EXCEEDED, COMPOSITION_INCOMPLETE — e
			// nenhuma delas é reescrita: o operador precisa ler o motivo real.
			return ResultadoDeGanho{}, 0, err
		}
		reserva, err := reservaDoResultado(nova)
		if err != nil {
			return ResultadoDeGanho{}, 0, err
		}
		reservaID, criouReserva, status = &reserva.ID, true, http.StatusCreated
	}

	if err := s.repo.Fechar(ctx, id, terminal.ID, OportunidadeGanha, nil, 100, reservaID); err != nil {
		return ResultadoDeGanho{}, 0, err
	}
	de := travada.EtapaID
	nota := textoOpcional(nil, corpo.Nota)
	// Só há MOVIMENTO quando a etapa muda: `crm_stage_history_movimento` recusa
	// origem igual a destino, e é um CHECK certo — uma linha "de Ganho para
	// Ganho" não é história, é ruído que estraga o tempo médio por etapa. O
	// caso aparece quando o card já está na etapa terminal (uma reabertura, ou
	// um funil de uma etapa só) e é fechado de novo.
	if de != terminal.ID {
		if err := s.repo.RegistrarMovimento(ctx, id, &de, terminal.ID, &u.ID, nota); err != nil {
			return ResultadoDeGanho{}, 0, err
		}
	}
	// Cobrar follow-up de negócio fechado é ruído que ensina a operação a
	// ignorar o alerta — e o alerta ignorado é o mesmo que alerta inexistente.
	if err := s.repo.ConcluirTarefasAutomaticas(ctx, id); err != nil {
		return ResultadoDeGanho{}, 0, err
	}

	oportunidade, err := s.buscarComSLA(ctx, u, id, false)
	if err != nil {
		return ResultadoDeGanho{}, 0, err
	}
	reserva, err := s.reservasRepo.Buscar(ctx, u.PropertyID, *reservaID, false, u.ID)
	if err != nil {
		return ResultadoDeGanho{}, 0, err
	}
	if err := audit.Alteracao(ctx, s.repo.Pool(), entidadeOportunidade, verboGanha, id,
		map[string]any{"status": EstadoParaContrato(travada.Status), "stage_id": de},
		map[string]any{
			"status": "ganha", "stage_id": terminal.ID,
			"reservation_id": *reservaID, "reservation_created": criouReserva,
		}); err != nil {
		return ResultadoDeGanho{}, 0, err
	}

	return ResultadoDeGanho{
		Oportunidade:  oportunidade,
		Reserva:       reserva,
		ReservaCriada: criouReserva,
	}, status, nil
}

// orcamentoVigente resolve QUAL orçamento vira reserva.
//
// Sem `quote_id` na oportunidade e sem `quote_id` no corpo, `422
// QUOTE_REQUIRED_TO_WIN`: sem preço congelado não há o que virar reserva, e
// inventar o preço na hora é o que a spec §3 proíbe.
//
// LIMITAÇÃO CONHECIDA: "orçamento DESTA oportunidade" é conferido pelo CONTATO,
// porque `reservations` não tem `opportunity_id` (ver o relatório). O
// `quote_id` já vinculado passa direto; um id vindo do corpo precisa ser uma
// reserva em `quote` do mesmo contato.
func (s *Servico) orcamentoVigente(ctx context.Context, u *auth.Usuario, o OportunidadeTravada, pedido httpx.Opt[uuid.UUID]) (ReservaDaOportunidade, error) {
	escolhido := o.OrcamentoID
	if v, ok := pedido.Definido(); ok {
		escolhido = &v
	}
	if escolhido == nil {
		return ReservaDaOportunidade{}, OrcamentoObrigatorioParaGanhar.WithDetails(map[string]any{
			"hint": "emita o orçamento e vincule-o à oportunidade antes de ganhar, ou informe quote_id no corpo.",
		})
	}

	orcamento, err := s.repo.OrcamentoDaOportunidade(ctx, u.PropertyID, *escolhido)
	if err != nil {
		return ReservaDaOportunidade{}, err
	}
	if o.OrcamentoID == nil || *o.OrcamentoID != orcamento.ID {
		if orcamento.ContactID != o.ContactID {
			return ReservaDaOportunidade{}, apperr.Validation(map[string]string{
				"quote_id": "este orçamento é de outro contato; informe um orçamento desta oportunidade.",
			})
		}
		if orcamento.Status != reservas.EstadoQuote {
			return ReservaDaOportunidade{}, apperr.Validation(map[string]string{
				"quote_id": "esta reserva já saiu de `quote`; ela não é mais um orçamento vigente.",
			})
		}
		// Vincula o orçamento escolhido à oportunidade ANTES de ganhar: sem
		// isso, o card ficaria dizendo que ganhou com um orçamento que ele nunca
		// declarou ter.
		if err := s.repo.VincularOrcamento(ctx, o.ID, orcamento.ID, orcamento.Total); err != nil {
			return ReservaDaOportunidade{}, err
		}
	}
	return orcamento, nil
}

// reservaDoResultado extrai a reserva do `Resultado` do módulo de reservas.
//
// O corpo dele é um envelope de tipo NÃO exportado (é assim que o replay
// idempotente devolve os bytes originais), então o caminho honesto é
// desserializar o mesmo JSON que o cliente receberia. Tentar adivinhar o id por
// outro caminho — reler a última reserva do contato, por exemplo — seria uma
// corrida esperando para acontecer.
func reservaDoResultado(res reservas.Resultado) (reservas.Reserva, error) {
	bruto := res.Bruto
	if bruto == nil {
		var err error
		if bruto, err = json.Marshal(res.Corpo); err != nil {
			return reservas.Reserva{}, apperr.Internal.WithCause(err)
		}
	}
	var env struct {
		Data reservas.Reserva `json:"data"`
	}
	if err := json.Unmarshal(bruto, &env); err != nil {
		return reservas.Reserva{}, apperr.Internal.WithCause(err)
	}
	if env.Data.ID == uuid.Nil {
		return reservas.Reserva{}, apperr.Internal.WithMessage("A reserva criada pelo ganho não voltou identificada.")
	}
	return env.Data, nil
}

// chaveInternaDaReserva monta a `Idempotency-Key` do POST /reservations interno.
//
// É derivada da OPORTUNIDADE, e não da chave do cliente: assim, duas chamadas de
// `/win` com chaves de cliente diferentes (o operador que gerou outra chave
// depois de um timeout) ainda batem na MESMA chave de reserva, e a segunda
// recebe a reserva da primeira em vez de criar uma segunda venda das mesmas
// datas — que seria, na melhor das hipóteses, um 409 contra si mesma.
func chaveInternaDaReserva(oportunidade uuid.UUID) string {
	return "crm-win-" + oportunidade.String()
}

// ═══════════════════════════ /lose ══════════════════════════════════

// Perder — POST /crm/opportunities/{id}/lose.
//
// `lost_reason_id` é OBRIGATÓRIO e tem de ser motivo ATIVO do catálogo. Não é
// burocracia: "motivos de perda" é um dos indicadores do §15, e é o único lugar
// em que a casa descobre que perdeu doze negócios por estadia mínima e não por
// preço.
//
// A pré-reserva atrelada NÃO é cancelada: perder o negócio e liberar a data são
// duas decisões, e a segunda tem política congelada e possível reembolso. A
// resposta traz a reserva para a tela oferecer o `/cancel` em seguida.
func (s *Servico) Perder(ctx context.Context, id uuid.UUID, corpo PedidoDePerda) (ResultadoDePerda, error) {
	u, err := ator(ctx)
	if err != nil {
		return ResultadoDePerda{}, err
	}

	var out ResultadoDePerda
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		travada, err := s.repo.TravarOportunidade(ctx, u.PropertyID, id,
			somenteMinhas(ctx, RecursoOportunidades, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		if travada.Status != OportunidadeAberta {
			return OportunidadeFechada.WithDetails(map[string]any{"status": EstadoParaContrato(travada.Status)})
		}
		if err := s.repo.MotivoAtivo(ctx, u.PropertyID, corpo.MotivoID); err != nil {
			return err
		}

		terminal, err := s.repo.EtapaTerminalDoFunil(ctx, travada.FunilID, EtapaPerdido)
		if err != nil {
			return err
		}
		motivo := corpo.MotivoID
		if err := s.repo.Fechar(ctx, id, terminal.ID, OportunidadePerdida, &motivo, 0, nil); err != nil {
			return err
		}
		de := travada.EtapaID
		nota := textoOpcional(nil, corpo.Nota)
		// Mesma guarda do `/win`: sem movimento de etapa não há linha de
		// histórico (ver o comentário lá).
		if de != terminal.ID {
			if err := s.repo.RegistrarMovimento(ctx, id, &de, terminal.ID, &u.ID, nota); err != nil {
				return err
			}
		}
		// CANCELADA, e não concluída: a tarefa não foi feita, o negócio acabou.
		// Marcar como concluída inflaria a taxa de conclusão do §15 com trabalho
		// que ninguém executou.
		if err := s.repo.CancelarTarefasAutomaticas(ctx, id); err != nil {
			return err
		}

		if out.Oportunidade, err = s.buscarComSLA(ctx, u, id, false); err != nil {
			return err
		}
		if travada.ReservaID != nil {
			reserva, err := s.reservasRepo.Buscar(ctx, u.PropertyID, *travada.ReservaID, false, u.ID)
			if err != nil {
				return err
			}
			out.Reserva = &reserva
		}

		return audit.Alteracao(ctx, s.repo.Pool(), entidadeOportunidade, verboPerdida, id,
			map[string]any{"status": EstadoParaContrato(travada.Status), "stage_id": de},
			map[string]any{
				"status": "perdida", "stage_id": terminal.ID,
				"lost_reason_id": motivo, "note": nota,
			})
	})
	return out, err
}

// ═══════════════════════════ Kanban ═════════════════════════════════

// Kanban — GET /crm/opportunities/kanban.
//
// Uma coluna por etapa, na ordem de `position`, cada uma com os cards da página
// e com o TOTAL DA COLUNA INTEIRA. Etapa vazia vem na lista com contagem zero:
// coluna que some quando esvazia faz o quadro mudar de forma sozinho.
func (s *Servico) Kanban(ctx context.Context, f FiltroDoKanban) (QuadroKanban, error) {
	u, err := ator(ctx)
	if err != nil {
		return QuadroKanban{}, err
	}
	f.SomenteMinhas, f.Usuario = somenteMinhas(ctx, RecursoOportunidades, auth.AcaoVer), u.ID

	funilID := uuid.Nil
	if f.FunilID != nil {
		funilID = *f.FunilID
	} else if funilID, err = s.repo.FunilPadrao(ctx, u.PropertyID); err != nil {
		return QuadroKanban{}, err
	}

	funil, err := s.repo.BuscarFunil(ctx, u.PropertyID, escopoDoContador(ctx, u), funilID)
	if err != nil {
		return QuadroKanban{}, err
	}
	etapas, err := s.repo.EtapasDoFunil(ctx, funilID)
	if err != nil {
		return QuadroKanban{}, err
	}
	cards, totais, err := s.repo.Kanban(ctx, u.PropertyID, funilID, f)
	if err != nil {
		return QuadroKanban{}, err
	}
	agora, err := s.repo.Agora(ctx)
	if err != nil {
		return QuadroKanban{}, err
	}

	quadro := QuadroKanban{Funil: funil, Colunas: make([]ColunaDoKanban, 0, len(etapas))}
	for _, etapa := range etapas {
		coluna := ColunaDoKanban{Etapa: etapa, Cards: []CardDaOportunidade{}}
		if t, ok := totais[etapa.ID]; ok {
			coluna.Total, coluna.Valor = t.Total, t.Valor
		}
		for _, card := range cards[etapa.ID] {
			card.SLAEstourado = slaEstourado(card.SLAVenceEm, card.Status, agora)
			coluna.Cards = append(coluna.Cards, card)
		}
		coluna.TemMais = coluna.Total > len(coluna.Cards)

		quadro.Totais.Total += coluna.Total
		quadro.Totais.Valor += coluna.Valor
		quadro.Colunas = append(quadro.Colunas, coluna)
	}
	return quadro, nil
}

// ═══════════════════════════ /full ══════════════════════════════════

// Completa — GET /crm/opportunities/{id}/full.
//
// Tudo o que a `OpportunityPage` desenha, numa chamada. É um agregador de
// propósito, e não uma economia preguiçosa: a tela montada por sete chamadas em
// paralelo aparece em sete tempos diferentes, e cada uma delas é mais uma chance
// de um pedaço da tela ficar com o escopo errado. Aqui o recorte de permissão é
// resolvido UMA vez, na entrada.
//
// O número de consultas é CONSTANTE (não cresce com a quantidade de atividades,
// notas ou movimentos): oportunidade, contato, etapas do funil, histórico,
// atividades, e no máximo duas leituras de reserva. As notas saem da MESMA
// consulta das atividades, separadas em memória — duas viagens ao banco para o
// mesmo `opportunity_id` seriam a segunda metade de um N+1 disfarçado de
// organização.
func (s *Servico) Completa(ctx context.Context, id uuid.UUID) (OportunidadeCompleta, error) {
	u, err := ator(ctx)
	if err != nil {
		return OportunidadeCompleta{}, err
	}
	restrito := somenteMinhas(ctx, RecursoOportunidades, auth.AcaoVer)

	var c OportunidadeCompleta
	if c.Oportunidade, err = s.buscarComSLA(ctx, u, id, restrito); err != nil {
		return c, err
	}
	if c.Contato, err = s.repo.ContatoResumido(ctx, u.PropertyID, c.Oportunidade.ContactID); err != nil {
		return c, err
	}
	// `stages` vem do funil DA OPORTUNIDADE, não do funil default: o card antigo
	// tem de desenhar a trilha em que ele realmente andou.
	if c.Etapas, err = s.repo.EtapasDoFunil(ctx, c.Oportunidade.FunilID); err != nil {
		return c, err
	}
	if c.Historico, err = s.repo.HistoricoDeEtapas(ctx, id); err != nil {
		return c, err
	}

	todas, err := s.repo.AtividadesDaOportunidade(ctx, id)
	if err != nil {
		return c, err
	}
	c.Atividades, c.Notas = separarNotas(todas)

	// Declarado e VAZIO nesta rodada: o módulo de anexos é de outra fase, e a
	// tela precisa nascer com o formato final. Acrescentar a chave depois
	// obrigaria o painel a tratar `documents` como opcional para sempre.
	c.Documentos = []DocumentoDaOportunidade{}

	etapaAtual := Etapa{ID: c.Oportunidade.EtapaID, Nome: c.Oportunidade.EtapaNome, SLADias: c.Oportunidade.SLADias}
	c.SLA = faixaDeSLA(c.Oportunidade, etapaAtual)

	if c.Oportunidade.OrcamentoID != nil {
		orcamento, err := s.reservasRepo.Buscar(ctx, u.PropertyID, *c.Oportunidade.OrcamentoID, false, u.ID)
		if err != nil {
			return c, err
		}
		c.Orcamento = &orcamento
	}
	if c.Oportunidade.ReservaID != nil {
		reserva, err := s.reservasRepo.Buscar(ctx, u.PropertyID, *c.Oportunidade.ReservaID, false, u.ID)
		if err != nil {
			return c, err
		}
		c.Reserva = &reserva
	}

	agora, err := s.repo.Agora(ctx)
	if err != nil {
		return c, err
	}
	c.Timeline = montarTimeline(c)
	c.Alertas = montarAlertas(c, agora)
	return c, nil
}

// separarNotas divide a lista única em tarefas e notas.
//
// Nota é ATIVIDADE de tipo `nota` (não há tabela separada nem CRUD próprio):
// criar nota é `POST /crm/activities` com `type: nota`, e assim ela entra na
// linha do tempo pelo mesmo caminho de todo o resto.
func separarNotas(todas []Atividade) ([]Atividade, []NotaDaOportunidade) {
	atividades := []Atividade{}
	notas := []NotaDaOportunidade{}
	for _, a := range todas {
		if a.Tipo != AtividadeNota {
			atividades = append(atividades, a)
			continue
		}
		corpo := a.Assunto
		if a.Descricao != nil && strings.TrimSpace(*a.Descricao) != "" {
			corpo = *a.Descricao
		}
		notas = append(notas, NotaDaOportunidade{
			ID: a.ID, Corpo: corpo, AutorID: &a.DonoID, AutorNome: a.DonoNome, CriadaEm: a.CriadoEm,
		})
	}
	return atividades, notas
}
