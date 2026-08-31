package crm

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/disponibilidade"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Entidades da trilha de auditoria. Os nomes são os das TABELAS: é assim que a
// tela de auditoria agrupa, e é assim que `audit.Acao` monta `<entidade>.<verbo>`.
const (
	entidadeFunil        = "crm_pipelines"
	entidadeEtapa        = "crm_stages"
	entidadeMotivo       = "crm_lost_reasons"
	entidadeLead         = "crm_leads"
	entidadeOportunidade = "crm_opportunities"
	entidadeAtividade    = "crm_activities"
)

// Verbos próprios do módulo, além dos três genéricos do `audit`. Existem porque
// "alterado" não responde o que a auditoria precisa saber de um card: mover,
// ganhar e perder são fatos diferentes com consequências diferentes.
const (
	verboMovido     = "etapa_movida"
	verboGanha      = "ganha"
	verboPerdida    = "perdida"
	verboConvertido = "convertido"
	verboConcluida  = "concluida"
	verboReordenado = "reordenado"
)

// Servico orquestra o funil.
//
// Ele NÃO cria reserva sozinho: `/win` delega ao módulo `reservas`, que já
// carrega a política, chama o motor e congela o snapshot. Reimplementar aqui
// faria a mesma estadia custar uma coisa no `POST /reservations` e outra no
// ganho da oportunidade — o defeito que a regra 1 do CLAUDE.md existe para
// impedir.
type Servico struct {
	repo *Repository
	// vendas é o módulo de reservas. Os repositórios dele usam db.From, então
	// as consultas e escritas dele entram NA MESMA TRANSAÇÃO do ganho — é o que
	// faz "reserva falhou, oportunidade não fecha" ser uma propriedade da
	// transação, e não uma sequência de `if` que alguém pode esquecer.
	vendas *reservas.Servico
	// reservasRepo lê a reserva no formato do contrato (schema `Reserva`) para
	// o `/full`, o `/win` e o `/lose` devolverem exatamente o que o módulo dono
	// dela devolve.
	reservasRepo *reservas.Repository
	// orcamentos é o módulo dono da tabela `quotes`. O CRM NÃO fala SQL de
	// orçamento: ele pergunta qual é o vigente, lê o snapshot e manda gravar a
	// conversão. Uma tabela, um dono — e o `/win` continua sem saber calcular
	// preço, que é o ponto da regra 1 do CLAUDE.md.
	orcamentos *disponibilidade.Servico
	tx         *db.TxManager
}

func NovoServico(repo *Repository, vendas *reservas.Servico, reservasRepo *reservas.Repository,
	orcamentos *disponibilidade.Servico, tx *db.TxManager) *Servico {
	return &Servico{repo: repo, vendas: vendas, reservasRepo: reservasRepo, orcamentos: orcamentos, tx: tx}
}

// Resultado é o que o handler escreve nas rotas idempotentes. Existe pela mesma
// razão do homônimo em `reservas`: a repetição de uma chave devolve o CORPO
// ORIGINAL, e não um corpo remontado a partir do estado de agora.
type Resultado struct {
	Status int
	Corpo  any
	Bruto  []byte
	Local  string
}

type envelope struct {
	Data any `json:"data"`
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

// somenteMinhas traduz o escopo do RBAC do recurso pedido. `own` vira
// `AND owner_id = $usuario` NO SQL — nunca filtro em memória.
func somenteMinhas(ctx context.Context, recurso, acao string) bool {
	return auth.SomenteProprios(ctx, recurso, acao)
}

// donoPadrao decide o `owner_id` de um registro que nasce sem dono explícito.
//
// Com escopo `own`, ausente assume o requisitante: criar um registro órfão seria
// criar um registro que o próprio autor não enxerga um segundo depois. Com
// escopo `all`, ausente significa SEM DONO — a fila da gestão, que é um estado
// legítimo do lead que entrou por WhatsApp antes de alguém assumir.
func donoPadrao(ctx context.Context, u *auth.Usuario, recurso string, pedido httpx.Opt[uuid.UUID]) *uuid.UUID {
	if v, ok := pedido.Definido(); ok {
		return &v
	}
	if pedido.DeveLimpar() {
		return nil
	}
	if somenteMinhas(ctx, recurso, auth.AcaoCriar) || somenteMinhas(ctx, recurso, auth.AcaoVer) {
		id := u.ID
		return &id
	}
	return nil
}

// ═══════════════════════════ Funis ══════════════════════════════════

// escopoDoContador é o `owner_id` com que os contadores derivados do funil são
// restritos. Nulo (escopo `all`) conta a casa inteira.
func escopoDoContador(ctx context.Context, u *auth.Usuario) *uuid.UUID {
	if somenteMinhas(ctx, RecursoOportunidades, auth.AcaoVer) {
		id := u.ID
		return &id
	}
	return nil
}

// ListarFunis — GET /crm/pipelines.
func (s *Servico) ListarFunis(ctx context.Context, ativo *bool, busca string, pagina, porPagina int) ([]Funil, int64, error) {
	u, err := ator(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarFunis(ctx, u.PropertyID, escopoDoContador(ctx, u), ativo, busca, pagina, porPagina)
}

// BuscarFunilComEtapas — GET /crm/pipelines/{id}.
func (s *Servico) BuscarFunilComEtapas(ctx context.Context, id uuid.UUID) (FunilComEtapas, error) {
	u, err := ator(ctx)
	if err != nil {
		return FunilComEtapas{}, err
	}
	funil, err := s.repo.BuscarFunil(ctx, u.PropertyID, escopoDoContador(ctx, u), id)
	if err != nil {
		return FunilComEtapas{}, err
	}
	etapas, err := s.repo.EtapasDoFunil(ctx, id)
	if err != nil {
		return FunilComEtapas{}, err
	}
	return FunilComEtapas{Funil: funil, Etapas: etapas}, nil
}

// CriarFunil — POST /crm/pipelines. Nasce SEM etapas: funil vazio não recebe
// oportunidade, e etapa se cria em /crm/stages.
func (s *Servico) CriarFunil(ctx context.Context, corpo FunilEntrada) (Funil, error) {
	u, err := ator(ctx)
	if err != nil {
		return Funil{}, err
	}

	var out Funil
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		padrao, ativo := corpo.Padrao.Ou(false), corpo.Ativo.Ou(true)
		if padrao && !ativo {
			return apperr.Validation(map[string]string{
				"is_default": "funil inativo não pode ser o padrão; a criação de oportunidade cairia num funil desligado.",
			})
		}

		if padrao {
			// ANTES do INSERT, e não depois: `crm_pipelines_default_idx` é um
			// índice parcial único e NÃO é adiável, então o instante com dois
			// defaults nem chega a existir — o INSERT estouraria 23505 na hora.
			// Medido: sem esta inversão, `POST /crm/pipelines {is_default:true}`
			// respondia 422 "default já está em uso" numa operação legítima.
			// `uuid.Nil` é o "nenhum id a preservar": ainda não há linha nova.
			if err := s.repo.DesmarcarOutrosPadroes(ctx, u.PropertyID, uuid.Nil); err != nil {
				return err
			}
		}
		id, err := s.repo.InserirFunil(ctx, u.PropertyID, strings.TrimSpace(corpo.Nome), padrao, ativo)
		if err != nil {
			return err
		}
		if out, err = s.repo.BuscarFunil(ctx, u.PropertyID, escopoDoContador(ctx, u), id); err != nil {
			return err
		}
		return audit.Criacao(ctx, s.repo.Pool(), entidadeFunil, audit.VerboCriado, id, out)
	})
	return out, err
}

// SubstituirFunil — PUT: substituição integral. Campo ausente volta ao padrão.
func (s *Servico) SubstituirFunil(ctx context.Context, id uuid.UUID, corpo FunilEntrada) (Funil, error) {
	return s.gravarFunil(ctx, id, FunilAtualizar{
		Nome:   httpx.De(corpo.Nome),
		Padrao: httpx.De(corpo.Padrao.Ou(false)),
		Ativo:  httpx.De(corpo.Ativo.Ou(true)),
	})
}

// AtualizarFunil — PATCH: ausente não muda.
func (s *Servico) AtualizarFunil(ctx context.Context, id uuid.UUID, corpo FunilAtualizar) (Funil, error) {
	return s.gravarFunil(ctx, id, corpo)
}

func (s *Servico) gravarFunil(ctx context.Context, id uuid.UUID, corpo FunilAtualizar) (Funil, error) {
	u, err := ator(ctx)
	if err != nil {
		return Funil{}, err
	}

	var out Funil
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarFunil(ctx, u.PropertyID, id)
		if err != nil {
			return err
		}

		nome := strings.TrimSpace(corpo.Nome.Ou(antes.Nome))
		padrao := corpo.Padrao.Ou(antes.Padrao)
		ativo := corpo.Ativo.Ou(antes.Ativo)

		if padrao && !ativo {
			return apperr.Validation(map[string]string{
				"is_default": "funil inativo não pode ser o padrão.",
			})
		}
		// Deixar de ser o padrão (por desmarcar OU por desativar) só é
		// permitido se sobrar outro padrão ativo. Sem isso, `POST
		// /crm/opportunities` sem `pipeline_id` passaria a falhar num endpoint
		// que ninguém tocou — o erro apareceria longe daqui.
		if antes.Padrao && !(padrao && ativo) {
			outro, err := s.repo.OutroPadraoAtivo(ctx, u.PropertyID, id)
			if err != nil {
				return err
			}
			if !outro {
				return FunilPadraoObrigatorio
			}
		}
		if !ativo && antes.Ativo {
			abertas, err := s.repo.OportunidadesAbertasNoFunil(ctx, id)
			if err != nil {
				return err
			}
			if abertas > 0 {
				return RecursoEmUso.
					WithMessage("Há oportunidade aberta neste funil; mova ou feche os cards antes de desativá-lo.").
					WithDetails(map[string]any{"open_opportunity_count": abertas})
			}
		}

		if padrao {
			if err := s.repo.DesmarcarOutrosPadroes(ctx, u.PropertyID, id); err != nil {
				return err
			}
		}
		if err := s.repo.AtualizarFunil(ctx, id, nome, padrao, ativo); err != nil {
			return err
		}
		if out, err = s.repo.BuscarFunil(ctx, u.PropertyID, escopoDoContador(ctx, u), id); err != nil {
			return err
		}
		return audit.Alteracao(ctx, s.repo.Pool(), entidadeFunil, audit.VerboAlterado, id,
			funilAuditado{Nome: antes.Nome, Padrao: antes.Padrao, Ativo: antes.Ativo},
			funilAuditado{Nome: nome, Padrao: padrao, Ativo: ativo})
	})
	return out, err
}

type funilAuditado struct {
	Nome   string `json:"name"`
	Padrao bool   `json:"is_default"`
	Ativo  bool   `json:"active"`
}

// DesativarFunil — DELETE: soft delete.
//
// Funil é HISTÓRICO: as oportunidades ganhas e perdidas apontam para as etapas
// dele, e é delas que sai a conversão por etapa do BI. Apagar de verdade
// apagaria o passado.
func (s *Servico) DesativarFunil(ctx context.Context, id uuid.UUID) error {
	_, err := s.gravarFunil(ctx, id, FunilAtualizar{Ativo: httpx.De(false)})
	return err
}

// ═══════════════════════════ Etapas ═════════════════════════════════

// ListarEtapas — GET /crm/stages.
func (s *Servico) ListarEtapas(ctx context.Context, funil *uuid.UUID, tipo string, pagina, porPagina int) ([]Etapa, int64, error) {
	u, err := ator(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarEtapas(ctx, u.PropertyID, funil, tipo, pagina, porPagina)
}

// BuscarEtapa — GET /crm/stages/{id}.
func (s *Servico) BuscarEtapa(ctx context.Context, id uuid.UUID) (Etapa, error) {
	u, err := ator(ctx)
	if err != nil {
		return Etapa{}, err
	}
	return s.repo.BuscarEtapa(ctx, u.PropertyID, id)
}

// CriarEtapa — POST /crm/stages.
func (s *Servico) CriarEtapa(ctx context.Context, corpo EtapaEntrada) (Etapa, error) {
	u, err := ator(ctx)
	if err != nil {
		return Etapa{}, err
	}

	var out Etapa
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if _, err := s.repo.TravarFunil(ctx, u.PropertyID, corpo.FunilID); err != nil {
			return err
		}

		e := Etapa{
			FunilID:       corpo.FunilID,
			Nome:          strings.TrimSpace(corpo.Nome),
			Probabilidade: corpo.Probabilidade.Ou(0),
			Cor:           corpo.Cor.Ou("#8FA36B"),
			Tipo:          corpo.Tipo.Ou(EtapaAberta),
			Notificar:     corpo.Notificar.Ou(true),
		}
		aplicarTarefaAutomatica(&e, corpo.SLADias, corpo.TarefaAssunto, corpo.TarefaTipo, corpo.TarefaPrazoDias)

		if v, ok := corpo.Posicao.Definido(); ok {
			e.Posicao = v
		} else if e.Posicao, err = s.repo.ProximaPosicaoDoFunil(ctx, corpo.FunilID); err != nil {
			return err
		}

		if err := s.conferirTerminalUnico(ctx, corpo.FunilID, e.Tipo, uuid.Nil); err != nil {
			return err
		}

		id, err := s.repo.InserirEtapa(ctx, e)
		if err != nil {
			return err
		}
		// Posição repetida é normalizada na gravação: a `UNIQUE` adiável
		// recusaria o commit, e recusar a criação por causa de um número que a
		// tela nem mostra seria hostil. A ordem canônica se estabelece com
		// `/reorder`.
		if err := s.repo.RecompactarPosicoes(ctx, corpo.FunilID); err != nil {
			return err
		}
		if out, err = s.repo.BuscarEtapa(ctx, u.PropertyID, id); err != nil {
			return err
		}
		return audit.Criacao(ctx, s.repo.Pool(), entidadeEtapa, audit.VerboCriado, id, out)
	})
	return out, err
}

// conferirTerminalUnico recusa a segunda etapa `ganho` (ou `perdido`) do funil.
// São elas que `/win` e `/lose` procuram: duas candidatas transformariam
// "ganhar" numa escolha ambígua do servidor.
func (s *Servico) conferirTerminalUnico(ctx context.Context, funil uuid.UUID, tipo string, exceto uuid.UUID) error {
	if !EtapaTerminal(tipo) {
		return nil
	}
	existe, err := s.repo.TerminalJaExiste(ctx, funil, tipo, exceto)
	if err != nil {
		return err
	}
	if existe {
		return NomeEmUso.
			WithMessage("Este funil já tem uma etapa de tipo " + tipo + "; é ela que /win e /lose procuram.").
			WithDetails(map[string]any{"type": tipo})
	}
	return nil
}

// aplicarTarefaAutomatica resolve os quatro campos que andam juntos.
//
// O CHECK `crm_stages_auto_task_completa` exige que assunto, tipo e prazo sejam
// os três ou nenhum. Traduzir isso aqui — e não deixar o 23514 subir — é o que
// faz o operador ler "informe o tipo da tarefa" em vez de "valor fora do
// permitido pela regra do banco".
func aplicarTarefaAutomatica(e *Etapa, slaDias httpx.Opt[int], assunto, tipo httpx.Opt[string], prazo httpx.Opt[int]) {
	if v, ok := slaDias.Definido(); ok {
		e.SLADias = &v
	} else if slaDias.DeveLimpar() {
		e.SLADias = nil
	}

	if v, ok := assunto.Definido(); ok && strings.TrimSpace(v) != "" {
		texto := strings.TrimSpace(v)
		e.TarefaAssunto = &texto

		// Tipo ausente assume `tarefa`: é o valor que o contrato promete, e é o
		// que impede a etapa de ficar meio configurada.
		tipoDaTarefa := AtividadeTarefa
		if v, ok := tipo.Definido(); ok {
			tipoDaTarefa = v
		}
		e.TarefaTipo = &tipoDaTarefa

		// Prazo ausente usa o SLA: o prazo da tarefa e o do SLA são a mesma
		// promessa. Sem nenhum dos dois, a tarefa nasceria sem vencimento — e
		// tarefa que nunca vence é o mesmo que tarefa que não existe, só que
		// ocupando a lista do corretor. Nesse caso o padrão é HOJE (0).
		prazoDaTarefa := 0
		if v, ok := prazo.Definido(); ok {
			prazoDaTarefa = v
		} else if e.SLADias != nil {
			prazoDaTarefa = *e.SLADias
		}
		e.TarefaPrazoDias = &prazoDaTarefa
		return
	}

	if assunto.DeveLimpar() || !assunto.Set {
		// Sem assunto não há tarefa automática, e os três campos caem juntos.
		e.TarefaAssunto, e.TarefaTipo, e.TarefaPrazoDias = nil, nil, nil
	}
}

// SubstituirEtapa — PUT.
func (s *Servico) SubstituirEtapa(ctx context.Context, id uuid.UUID, corpo EtapaEntrada) (Etapa, error) {
	u, err := ator(ctx)
	if err != nil {
		return Etapa{}, err
	}
	atual, err := s.repo.BuscarEtapa(ctx, u.PropertyID, id)
	if err != nil {
		return Etapa{}, err
	}
	// Etapa não troca de funil: mudar arrastaria as oportunidades dela junto,
	// sem uma linha de histórico dizendo que foram para outro lugar.
	if corpo.FunilID != atual.FunilID {
		return Etapa{}, EtapaForaDoFunil.
			WithMessage("A etapa não muda de funil; isso moveria as oportunidades dela junto, sem histórico.").
			WithDetails(map[string]any{"stage_id": id, "pipeline_id": atual.FunilID})
	}

	return s.gravarEtapa(ctx, id, EtapaAtualizar{
		Nome:          httpx.De(corpo.Nome),
		Posicao:       corpo.Posicao,
		Probabilidade: httpx.De(corpo.Probabilidade.Ou(0)),
		Cor:           httpx.De(corpo.Cor.Ou("#8FA36B")),
		Tipo:          httpx.De(corpo.Tipo.Ou(EtapaAberta)),
		// No PUT o que não veio volta ao padrão — e o padrão da tarefa
		// automática é NÃO TER. É o que "substituição integral" significa.
		SLADias:         opcionalOuNulo(corpo.SLADias),
		TarefaAssunto:   opcionalOuNulo(corpo.TarefaAssunto),
		TarefaTipo:      opcionalOuNulo(corpo.TarefaTipo),
		TarefaPrazoDias: opcionalOuNulo(corpo.TarefaPrazoDias),
		Notificar:       httpx.De(corpo.Notificar.Ou(true)),
	})
}

// AtualizarEtapa — PATCH.
func (s *Servico) AtualizarEtapa(ctx context.Context, id uuid.UUID, corpo EtapaAtualizar) (Etapa, error) {
	return s.gravarEtapa(ctx, id, corpo)
}

func (s *Servico) gravarEtapa(ctx context.Context, id uuid.UUID, corpo EtapaAtualizar) (Etapa, error) {
	u, err := ator(ctx)
	if err != nil {
		return Etapa{}, err
	}

	var out Etapa
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarEtapa(ctx, u.PropertyID, id)
		if err != nil {
			return err
		}
		if _, err := s.repo.TravarFunil(ctx, u.PropertyID, antes.FunilID); err != nil {
			return err
		}

		e := antes
		if v, ok := corpo.Nome.Definido(); ok {
			e.Nome = strings.TrimSpace(v)
		}
		if v, ok := corpo.Probabilidade.Definido(); ok {
			e.Probabilidade = v
		}
		if v, ok := corpo.Cor.Definido(); ok {
			e.Cor = v
		}
		if v, ok := corpo.Tipo.Definido(); ok {
			e.Tipo = v
		}
		if v, ok := corpo.Notificar.Definido(); ok {
			e.Notificar = v
		}
		// Só reescreve a tarefa automática quando algum dos campos dela veio: um
		// PATCH que só muda a cor não pode desligar o follow-up da etapa.
		if corpo.SLADias.Set || corpo.TarefaAssunto.Set || corpo.TarefaTipo.Set || corpo.TarefaPrazoDias.Set {
			assunto := corpo.TarefaAssunto
			if !assunto.Set && antes.TarefaAssunto != nil {
				assunto = httpx.De(*antes.TarefaAssunto)
			}
			tipo := corpo.TarefaTipo
			if !tipo.Set && antes.TarefaTipo != nil {
				tipo = httpx.De(*antes.TarefaTipo)
			}
			prazo := corpo.TarefaPrazoDias
			if !prazo.Set && antes.TarefaPrazoDias != nil {
				prazo = httpx.De(*antes.TarefaPrazoDias)
			}
			sla := corpo.SLADias
			if !sla.Set && antes.SLADias != nil {
				sla = httpx.De(*antes.SLADias)
			}
			aplicarTarefaAutomatica(&e, sla, assunto, tipo, prazo)
		}

		if e.Tipo != antes.Tipo {
			if err := s.conferirTerminalUnico(ctx, e.FunilID, e.Tipo, id); err != nil {
				return err
			}
		}
		if v, ok := corpo.Posicao.Definido(); ok {
			e.Posicao = v
		}

		if err := s.repo.AtualizarEtapa(ctx, e); err != nil {
			return err
		}
		if _, ok := corpo.Posicao.Definido(); ok {
			if err := s.repo.RecompactarPosicoes(ctx, e.FunilID); err != nil {
				return err
			}
		}
		if out, err = s.repo.BuscarEtapa(ctx, u.PropertyID, id); err != nil {
			return err
		}
		return audit.Alteracao(ctx, s.repo.Pool(), entidadeEtapa, audit.VerboAlterado, id, antes, out)
	})
	return out, err
}

// opcionalOuNulo traduz o "ausente volta ao padrão" do PUT sobre coluna
// anulável: ausente vira `null` explícito, e não "não mexer".
func opcionalOuNulo[T any](o httpx.Opt[T]) httpx.Opt[T] {
	if o.Set {
		return o
	}
	return httpx.Nulo[T]()
}

// Reordenar — POST /crm/stages/reorder.
//
// Reordenar é UM ATO. Uma sequência de PATCH deixaria o funil visivelmente
// errado entre uma chamada e outra, e a lista incompleta é recusada justamente
// para que ninguém tente reordenar mandando só as duas etapas que se moveram.
func (s *Servico) Reordenar(ctx context.Context, corpo PedidoDeReordenacao) ([]Etapa, error) {
	u, err := ator(ctx)
	if err != nil {
		return nil, err
	}

	var out []Etapa
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if _, err := s.repo.TravarFunil(ctx, u.PropertyID, corpo.FunilID); err != nil {
			return err
		}

		doFunil, err := s.repo.IDsDasEtapasDoFunil(ctx, corpo.FunilID)
		if err != nil {
			return err
		}

		pertence := map[uuid.UUID]bool{}
		for _, id := range doFunil {
			pertence[id] = true
		}
		enviadas := map[uuid.UUID]bool{}
		for _, id := range corpo.EtapaIDs {
			if !pertence[id] {
				return EtapaForaDoFunil.WithDetails(map[string]any{"stage_id": id})
			}
			enviadas[id] = true
		}
		faltando := []uuid.UUID{}
		for _, id := range doFunil {
			if !enviadas[id] {
				faltando = append(faltando, id)
			}
		}
		if len(faltando) > 0 {
			// Sem esta recusa, as etapas ausentes ficariam com a posição de
			// antes e o kanban desenharia duas colunas na mesma casa.
			return OrdemIncompleta.WithDetails(map[string]any{"missing_stage_ids": faltando})
		}

		if err := s.repo.Reordenar(ctx, corpo.FunilID, corpo.EtapaIDs); err != nil {
			return err
		}
		if out, err = s.repo.EtapasDoFunil(ctx, corpo.FunilID); err != nil {
			return err
		}
		return audit.Alteracao(ctx, s.repo.Pool(), entidadeFunil, verboReordenado, corpo.FunilID,
			map[string]any{"stage_ids": doFunil}, map[string]any{"stage_ids": corpo.EtapaIDs})
	})
	return out, err
}

// ExcluirEtapa — DELETE /crm/stages/{id}: remoção FÍSICA, e só da etapa vazia.
func (s *Servico) ExcluirEtapa(ctx context.Context, id uuid.UUID) error {
	u, err := ator(ctx)
	if err != nil {
		return err
	}

	return s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarEtapa(ctx, u.PropertyID, id)
		if err != nil {
			return err
		}
		if _, err := s.repo.TravarFunil(ctx, u.PropertyID, antes.FunilID); err != nil {
			return err
		}

		oportunidades, historico, err := s.repo.UsoDaEtapa(ctx, id)
		if err != nil {
			return err
		}
		if oportunidades > 0 || historico > 0 {
			// `crm_stage_history` é insert-only e é a matéria-prima da conversão
			// por etapa: apagar a etapa deixaria o histórico apontando para o
			// nada, e o relatório perderia a etapa em que o funil trava.
			return RecursoEmUso.
				WithMessage("A etapa ainda é usada por oportunidades ou pelo histórico de etapas.").
				WithDetails(map[string]any{
					"opportunity_count": oportunidades,
					"history_count":     historico,
				})
		}
		if err := s.repo.ExcluirEtapa(ctx, antes.FunilID, id); err != nil {
			return err
		}
		// `before` é a ÚNICA cópia que sobra: depois do DELETE a linha não está
		// em lugar nenhum, e a trilha é o que responde o que foi apagado.
		return audit.Exclusao(ctx, s.repo.Pool(), entidadeEtapa, audit.VerboExcluido, id, antes)
	})
}

// ═══════════════════════════ Motivos de perda ═══════════════════════

// ListarMotivos — GET /crm/lost-reasons.
func (s *Servico) ListarMotivos(ctx context.Context, ativo *bool, pagina, porPagina int) ([]MotivoDePerda, int64, error) {
	u, err := ator(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarMotivos(ctx, u.PropertyID, ativo, pagina, porPagina)
}

// BuscarMotivo — GET /crm/lost-reasons/{id}.
func (s *Servico) BuscarMotivo(ctx context.Context, id uuid.UUID) (MotivoDePerda, error) {
	u, err := ator(ctx)
	if err != nil {
		return MotivoDePerda{}, err
	}
	return s.repo.BuscarMotivo(ctx, u.PropertyID, id)
}

// CriarMotivo — POST /crm/lost-reasons.
func (s *Servico) CriarMotivo(ctx context.Context, corpo MotivoDePerdaEntrada) (MotivoDePerda, error) {
	u, err := ator(ctx)
	if err != nil {
		return MotivoDePerda{}, err
	}

	var out MotivoDePerda
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		id, err := s.repo.InserirMotivo(ctx, u.PropertyID, strings.TrimSpace(corpo.Rotulo), corpo.Ativo.Ou(true))
		if err != nil {
			return err
		}
		if out, err = s.repo.BuscarMotivo(ctx, u.PropertyID, id); err != nil {
			return err
		}
		return audit.Criacao(ctx, s.repo.Pool(), entidadeMotivo, audit.VerboCriado, id, out)
	})
	return out, err
}

// SubstituirMotivo — PUT.
func (s *Servico) SubstituirMotivo(ctx context.Context, id uuid.UUID, corpo MotivoDePerdaEntrada) (MotivoDePerda, error) {
	return s.gravarMotivo(ctx, id, MotivoDePerdaAtualizar{
		Rotulo: httpx.De(corpo.Rotulo),
		Ativo:  httpx.De(corpo.Ativo.Ou(true)),
	})
}

// AtualizarMotivo — PATCH.
func (s *Servico) AtualizarMotivo(ctx context.Context, id uuid.UUID, corpo MotivoDePerdaAtualizar) (MotivoDePerda, error) {
	return s.gravarMotivo(ctx, id, corpo)
}

func (s *Servico) gravarMotivo(ctx context.Context, id uuid.UUID, corpo MotivoDePerdaAtualizar) (MotivoDePerda, error) {
	u, err := ator(ctx)
	if err != nil {
		return MotivoDePerda{}, err
	}

	var out MotivoDePerda
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarMotivo(ctx, u.PropertyID, id)
		if err != nil {
			return err
		}
		rotulo := strings.TrimSpace(corpo.Rotulo.Ou(antes.Rotulo))
		ativo := corpo.Ativo.Ou(antes.Ativo)

		if err := s.repo.AtualizarMotivo(ctx, id, rotulo, ativo); err != nil {
			return err
		}
		if out, err = s.repo.BuscarMotivo(ctx, u.PropertyID, id); err != nil {
			return err
		}
		return audit.Alteracao(ctx, s.repo.Pool(), entidadeMotivo, audit.VerboAlterado, id, antes, out)
	})
	return out, err
}

// DesativarMotivo — DELETE: soft delete.
//
// Oportunidades perdidas continuam apontando para ele, e é isso que o relatório
// de motivos de perda lê. Inativo some do seletor do `/lose` e continua legível
// no card antigo.
func (s *Servico) DesativarMotivo(ctx context.Context, id uuid.UUID) error {
	_, err := s.gravarMotivo(ctx, id, MotivoDePerdaAtualizar{Ativo: httpx.De(false)})
	return err
}

// ═══════════════════════════ Auxiliares compartilhados ══════════════

// tituloDoCard compõe o `title` que o schema exige e que o contrato não expõe.
//
// Contato + produto + datas é como o card se identifica na tela, e é o que a
// busca procura. Deixar o cliente digitar daria "Fernanda – dezembro" em vinte
// grafias e nenhuma consulta possível.
func tituloDoCard(contato string, produto *string, checkIn, checkOut *string) string {
	partes := []string{strings.TrimSpace(contato)}
	if produto != nil && strings.TrimSpace(*produto) != "" {
		partes = append(partes, strings.TrimSpace(*produto))
	}
	if checkIn != nil && *checkIn != "" {
		periodo := *checkIn
		if checkOut != nil && *checkOut != "" {
			periodo += " a " + *checkOut
		}
		partes = append(partes, periodo)
	}
	titulo := strings.Join(partes, " · ")
	if titulo == "" {
		return "Oportunidade"
	}
	return titulo
}

// prazoDaTarefa converte o prazo em dias da etapa num instante, a partir de
// `agora` — que é o `now()` do BANCO, e não o relógio do processo.
func prazoDaTarefa(agora time.Time, dias int) time.Time {
	return agora.AddDate(0, 0, dias)
}
