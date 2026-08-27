package crm

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// ListarAtividades — GET /crm/activities.
func (s *Servico) ListarAtividades(ctx context.Context, f FiltroDeAtividades) ([]Atividade, int64, error) {
	u, err := ator(ctx)
	if err != nil {
		return nil, 0, err
	}
	f.SomenteMinhas, f.Usuario = somenteMinhas(ctx, RecursoAtividades, auth.AcaoVer), u.ID
	return s.repo.ListarAtividades(ctx, u.PropertyID, f)
}

// BuscarAtividade — GET /crm/activities/{id}.
func (s *Servico) BuscarAtividade(ctx context.Context, id uuid.UUID) (Atividade, error) {
	u, err := ator(ctx)
	if err != nil {
		return Atividade{}, err
	}
	return s.repo.BuscarAtividade(ctx, u.PropertyID, id, somenteMinhas(ctx, RecursoAtividades, auth.AcaoVer), u.ID)
}

// CriarAtividade — POST /crm/activities.
//
// `auto` não é aceito no corpo (nem existe no DTO): automática é a tarefa que o
// SERVIDOR criou ao entrar numa etapa. Deixar o cliente se declarar automática
// entregaria a ele a parcial única do banco — a idempotência da tarefa de etapa
// passaria a ser disputada por linhas que o servidor não controla.
func (s *Servico) CriarAtividade(ctx context.Context, corpo AtividadeCriar) (Atividade, error) {
	u, err := ator(ctx)
	if err != nil {
		return Atividade{}, err
	}

	var out Atividade
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		g := AtividadeGravavel{
			Tipo:       corpo.Tipo,
			Assunto:    strings.TrimSpace(corpo.Assunto),
			Descricao:  textoOpcional(nil, corpo.Descricao),
			Prioridade: corpo.Prioridade.Ou(PrioridadeNormal),
			DonoID:     u.ID,
		}
		// Nota não tem prazo: um `due_at` numa nota criaria uma "nota vencida"
		// na caixa de entrada de alguém, e nota não é trabalho de ninguém.
		if v, ok := corpo.VenceEm.Definido(); ok && corpo.Tipo != AtividadeNota {
			g.VenceEm = &v
		}
		if v, ok := corpo.DonoID.Definido(); ok {
			if err := s.repo.ConferirUsuario(ctx, u.PropertyID, v); err != nil {
				return err
			}
			g.DonoID = v
		}
		if v, ok := corpo.OportunidadeID.Definido(); ok {
			if err := s.repo.ConferirOportunidade(ctx, u.PropertyID, v); err != nil {
				return err
			}
			g.OportunidadeID = &v
		}
		if v, ok := corpo.LeadID.Definido(); ok {
			if err := s.repo.ConferirLead(ctx, u.PropertyID, v); err != nil {
				return err
			}
			g.LeadID = &v
		}
		if v, ok := corpo.ContactID.Definido(); ok {
			if err := s.repo.ConferirContato(ctx, u.PropertyID, v); err != nil {
				return err
			}
			g.ContactID = &v
		}

		id, err := s.repo.InserirAtividade(ctx, u.PropertyID, g, u.ID)
		if err != nil {
			return err
		}
		if out, err = s.repo.BuscarAtividade(ctx, u.PropertyID, id, false, u.ID); err != nil {
			return err
		}
		return audit.Criacao(ctx, s.repo.Pool(), entidadeAtividade, audit.VerboCriado, id, out)
	})
	return out, err
}

// SubstituirAtividade — PUT: substituição integral dos campos editáveis.
func (s *Servico) SubstituirAtividade(ctx context.Context, id uuid.UUID, corpo AtividadeAtualizar) (Atividade, error) {
	if _, ok := corpo.Assunto.Definido(); !ok {
		return Atividade{}, apperr.Validation(map[string]string{"subject": "é obrigatório no PUT."})
	}
	if !corpo.Prioridade.Set {
		corpo.Prioridade = httpx.De(PrioridadeNormal)
	}
	if !corpo.Descricao.Set {
		corpo.Descricao = httpx.Nulo[string]()
	}
	if !corpo.VenceEm.Set {
		corpo.VenceEm = httpx.Nulo[time.Time]()
	}
	// `status` NÃO volta ao padrão no PUT: reabrir uma tarefa cancelada (ou
	// concluída) por uma edição de assunto seria mudar o estado sem passar pela
	// ação — o mesmo defeito que tirou `stage_id` do PATCH da oportunidade.
	return s.AtualizarAtividade(ctx, id, corpo)
}

// AtualizarAtividade — PATCH.
//
// `status: concluida` é recusado no DTO: concluir é `POST /{id}/complete`, que
// carimba `done_at` com o relógio do servidor. `done_at` escrito pelo cliente é
// o tempo médio de resposta do §15 medido pelo relógio do navegador.
func (s *Servico) AtualizarAtividade(ctx context.Context, id uuid.UUID, corpo AtividadeAtualizar) (Atividade, error) {
	u, err := ator(ctx)
	if err != nil {
		return Atividade{}, err
	}

	var out Atividade
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if _, err := s.repo.TravarAtividade(ctx, u.PropertyID, id,
			somenteMinhas(ctx, RecursoAtividades, auth.AcaoEditar), u.ID); err != nil {
			return err
		}
		antes, err := s.repo.BuscarAtividade(ctx, u.PropertyID, id, false, u.ID)
		if err != nil {
			return err
		}

		tipo := antes.Tipo
		if v, ok := corpo.Tipo.Definido(); ok {
			tipo = v
		}
		assunto := antes.Assunto
		if v, ok := corpo.Assunto.Definido(); ok {
			assunto = strings.TrimSpace(v)
		}
		descricao := textoOpcional(antes.Descricao, corpo.Descricao)
		prioridade := antes.Prioridade
		if v, ok := corpo.Prioridade.Definido(); ok {
			prioridade = v
		}
		dono := antes.DonoID
		if v, ok := corpo.DonoID.Definido(); ok {
			// Repassar a tarefa a outra pessoa: quem tem escopo `own` perde a
			// tarefa de vista ao fazer isso — e é o comportamento correto, ela
			// deixou de ser dele.
			if err := s.repo.ConferirUsuario(ctx, u.PropertyID, v); err != nil {
				return err
			}
			dono = v
		}

		venceEm := antes.VenceEm
		if v, ok := corpo.VenceEm.Definido(); ok {
			venceEm = &v
		} else if corpo.VenceEm.DeveLimpar() {
			venceEm = nil
		}
		if tipo == AtividadeNota {
			venceEm = nil
		}

		status := antes.Status
		if v, ok := corpo.Status.Definido(); ok {
			// Reabrir uma tarefa CONCLUÍDA por aqui apagaria o `done_at` — e o
			// `done_at` é o insumo do tempo médio de resposta. Concluída é
			// terminal para o PATCH; o que se reabre é a cancelada.
			if antes.Status == AtividadeConcluida && v != AtividadeConcluida {
				return TransicaoInvalida.
					WithMessage("Atividade concluída não volta a pendente; crie a próxima ação.").
					WithDetails(map[string]any{"status": antes.Status})
			}
			status = v
		}

		if err := s.repo.AtualizarAtividade(ctx, id, tipo, assunto, descricao, venceEm, prioridade, dono, status); err != nil {
			return err
		}
		if out, err = s.repo.BuscarAtividade(ctx, u.PropertyID, id, false, u.ID); err != nil {
			return err
		}
		return audit.Alteracao(ctx, s.repo.Pool(), entidadeAtividade, audit.VerboAlterado, id, antes, out)
	})
	return out, err
}

// ExcluirAtividade — DELETE /crm/activities/{id}: remoção física.
func (s *Servico) ExcluirAtividade(ctx context.Context, id uuid.UUID) error {
	u, err := ator(ctx)
	if err != nil {
		return err
	}

	return s.tx.Do(ctx, func(ctx context.Context) error {
		if _, err := s.repo.TravarAtividade(ctx, u.PropertyID, id,
			somenteMinhas(ctx, RecursoAtividades, auth.AcaoExcluir), u.ID); err != nil {
			return err
		}
		antes, err := s.repo.BuscarAtividade(ctx, u.PropertyID, id, false, u.ID)
		if err != nil {
			return err
		}
		if err := s.repo.ExcluirAtividade(ctx, id); err != nil {
			return err
		}
		return audit.Exclusao(ctx, s.repo.Pool(), entidadeAtividade, audit.VerboExcluido, id, antes)
	})
}

// Concluir — POST /crm/activities/{id}/complete.
//
// Grava `status: concluida` e `done_at` com o RELÓGIO DO SERVIDOR.
//
// É idempotente: concluir de novo devolve 200 com o MESMO `done_at`, sem
// reescrever nada. O duplo clique no botão "Concluir" é o gesto mais comum da
// tela, e mover `done_at` para a frente falsearia o tempo de resposta do §15.
//
// `next_activity` cria, na mesma transação, o "concluir e agendar o próximo
// passo": sem ele a operação conclui e esquece, e o único lembrete passa a ser
// o alerta de cliente parado — que chega quinze dias depois.
func (s *Servico) Concluir(ctx context.Context, id uuid.UUID, corpo PedidoDeConclusao) (ResultadoDeConclusao, error) {
	u, err := ator(ctx)
	if err != nil {
		return ResultadoDeConclusao{}, err
	}

	var out ResultadoDeConclusao
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		travada, err := s.repo.TravarAtividade(ctx, u.PropertyID, id,
			somenteMinhas(ctx, RecursoAtividades, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		if travada.Status == AtividadeCancelada {
			return TransicaoInvalida.
				WithMessage("Atividade cancelada não se conclui; reabra com PATCH (status: pendente) antes.").
				WithDetails(map[string]any{"status": travada.Status})
		}

		if travada.Status == AtividadePendente {
			if err := s.repo.ConcluirAtividade(ctx, id, textoOpcional(nil, corpo.Nota)); err != nil {
				return err
			}
		}
		// Já concluída: nada é reescrito, e o `done_at` original fica de pé.

		if out.Atividade, err = s.repo.BuscarAtividade(ctx, u.PropertyID, id, false, u.ID); err != nil {
			return err
		}

		if prox, ok := corpo.Proxima.Definido(); ok {
			proxima, err := s.agendarProxima(ctx, u, travada, prox)
			if err != nil {
				return err
			}
			out.Proxima = proxima
		}

		if travada.Status != AtividadePendente {
			// Repetição do mesmo `complete`: não há mudança para auditar, e
			// gravar uma linha idêntica a cada duplo clique encheria a trilha de
			// ruído que esconde a alteração de verdade.
			return nil
		}
		return audit.Alteracao(ctx, s.repo.Pool(), entidadeAtividade, verboConcluida, id,
			map[string]any{"status": travada.Status},
			map[string]any{"status": AtividadeConcluida, "done_at": out.Atividade.ConcluidaEm})
	})
	return out, err
}

// agendarProxima cria a próxima ação herdando os vínculos da atividade
// concluída — mesma oportunidade, mesmo lead, mesmo contato, mesmo dono.
//
// Herdar, e não pedir de novo: o operador acabou de dizer com quem está
// falando, e obrigá-lo a repetir os vínculos é o atrito que faz o "próximo
// passo" não ser agendado.
func (s *Servico) agendarProxima(ctx context.Context, u *auth.Usuario, concluida AtividadeTravada, prox ProximaAtividade) (*Atividade, error) {
	dono := concluida.DonoID
	if v, ok := prox.DonoID.Definido(); ok {
		if err := s.repo.ConferirUsuario(ctx, u.PropertyID, v); err != nil {
			return nil, err
		}
		dono = v
	}
	vence := prox.VenceEm

	id, err := s.repo.InserirAtividade(ctx, u.PropertyID, AtividadeGravavel{
		Tipo:           prox.Tipo,
		Assunto:        strings.TrimSpace(prox.Assunto),
		VenceEm:        &vence,
		Prioridade:     prox.Prioridade.Ou(PrioridadeNormal),
		LeadID:         concluida.LeadID,
		OportunidadeID: concluida.OportunidadeID,
		ContactID:      concluida.ContactID,
		DonoID:         dono,
	}, u.ID)
	if err != nil {
		return nil, err
	}
	nova, err := s.repo.BuscarAtividade(ctx, u.PropertyID, id, false, u.ID)
	if err != nil {
		return nil, err
	}
	if err := audit.Criacao(ctx, s.repo.Pool(), entidadeAtividade, audit.VerboCriado, id, nova); err != nil {
		return nil, err
	}
	return &nova, nil
}
