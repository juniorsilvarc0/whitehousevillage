package manutencao

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/maintenance"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Transacionador é o que o service precisa do db.TxManager.
type Transacionador interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

var _ Transacionador = (*db.TxManager)(nil)

// Service orquestra: transação, escopo de propriedade, a decisão do domínio
// sobre o estado TRAVADO, o efeito no calendário e na avaria, e a trilha.
//
// Toda escrita segue o mesmo roteiro, numa transação só:
//
//  1. trava a ordem (FOR UPDATE) — duas ações simultâneas se enfileiram aqui;
//  2. pergunta ao domínio (`maintenance.Next`, `CheckEdit`, `Replan`,
//     `ReleaseOn`, `IssueOnClose`) com "hoje" lido do banco no fuso da casa;
//  3. aplica o que ele decidiu — o bloqueio em `stay_blocks`, a avaria, a
//     ordem —, e quem decide sobreposição de datas é a constraint;
//  4. grava a trilha de cada linha mudada, na mesma transação.
type Service struct {
	repo *Repository
	tx   Transacionador
}

// NewService monta o service.
func NewService(repo *Repository, tx Transacionador) *Service {
	return &Service{repo: repo, tx: tx}
}

// Entidades e verbos da trilha. As entidades são nomes de TABELA; os verbos do
// bloqueio são os de `reservas` (`bloqueada`, `desbloqueada`), para "quem tirou
// esta unidade do ar" ser uma consulta só, venha o bloqueio de onde vier.
const (
	entidadeOrdem    = "maintenance_orders"
	entidadeBloqueio = "stay_blocks"
	entidadeAvaria   = "inventory_issues"

	verboIniciada     = "iniciada"
	verboConcluida    = "concluida"
	verboCancelada    = "cancelada"
	verboBloqueada    = "bloqueada"
	verboDesbloqueada = "desbloqueada"
	verboRemarcada    = "remarcada"
	verboEncurtada    = "encurtada"
	verboResolvida    = "resolvida"
)

// propriedadeDoAtor resolve a casa em que a requisição opera. Sem escopo
// `own`: a ordem é da casa, não de quem a abriu.
func propriedadeDoAtor(ctx context.Context) (uuid.UUID, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return uuid.Nil, apperr.Unauthorized
	}
	if u.PropertyID == uuid.Nil {
		return uuid.Nil, apperr.Internal.WithCause(fmt.Errorf("usuário %s sem property_id: sessão incompleta", u.ID))
	}
	return u.PropertyID, nil
}

// atorOuNulo é o autor das colunas `*_by`. Nulo fora de requisição.
func atorOuNulo(ctx context.Context) *uuid.UUID {
	if id, ok := auth.UsuarioID(ctx); ok {
		return &id
	}
	return nil
}

// ═══════════════════════════ Leitura ══════════════════════════════════════

func (s *Service) Listar(ctx context.Context, f FiltroDeOrdens) ([]OrdemDeManutencao, int64, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarOrdens(ctx, prop, f)
}

func (s *Service) Buscar(ctx context.Context, id uuid.UUID) (OrdemDeManutencao, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return OrdemDeManutencao{}, err
	}
	return s.repo.BuscarOrdem(ctx, prop, id)
}

// ═══════════════════════════ Abertura ═════════════════════════════════════

// Criar abre a ordem — e, se pedido, o bloqueio no MESMO commit: ou nascem as
// duas, ou nenhuma. A segunda ordem da mesma avaria é recusada pelo índice
// parcial (ON CONFLICT), e a data ocupada pela constraint de `stay_blocks`.
func (s *Service) Criar(ctx context.Context, c OrdemDeManutencaoCriar) (OrdemDeManutencao, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return OrdemDeManutencao{}, err
	}
	autor := atorOuNulo(ctx)

	var out OrdemDeManutencao
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if c.Bloqueio != nil {
			// Primeira coisa da transação: as ordens que bloqueiam a mesma
			// unidade entram em fila (ver TravarCalendarioDaUnidade).
			if err := s.repo.TravarCalendarioDaUnidade(ctx, c.UnidadeID); err != nil {
				return err
			}
		}
		unidade, ok, err := s.repo.Unidade(ctx, prop, c.UnidadeID)
		if err != nil {
			return err
		}
		if !ok {
			return apperr.Validation(map[string]string{"unit_id": "unidade não encontrada nesta propriedade."})
		}

		// A avaria, se citada: é ela que dá cômodo e bem, e ela tem de ser
		// desta unidade. O par é validado ANTES do INSERT (422) — com ordem
		// aberta na avaria, o índice parcial venceria a FK e o par divergente
		// sairia como 409, escondendo o erro de campo.
		var origem *avariaDeOrigem
		if c.AvariaID != nil {
			a, ok, err := s.repo.Avaria(ctx, prop, *c.AvariaID)
			if err != nil {
				return err
			}
			switch {
			case !ok:
				return apperr.Validation(map[string]string{"issue_id": "avaria não encontrada nesta propriedade."})
			case a.UnidadeID != c.UnidadeID:
				return apperr.Validation(map[string]string{"issue_id": "a avaria é de um cômodo de outra unidade."})
			}
			origem = &a
		}
		comodo, bem, err := s.vinculos(ctx, prop, c.UnidadeID, origem,
			optDoPost(c.ComodoID), optDoPost(c.BemID), nil, nil)
		if err != nil {
			return err
		}

		// O período, se pedido, antes de qualquer escrita: noite que já passou
		// não se bloqueia, e os tetos são os de `POST /blocks`.
		var periodo *maintenance.Period
		if c.Bloqueio != nil {
			p, err := c.Bloqueio.periodo()
			if err != nil {
				return apperr.Validation(map[string]string{"block": "período ilegível."}).WithCause(err)
			}
			hoje, err := s.repo.Hoje(ctx, prop)
			if err != nil {
				return err
			}
			if _, err := maintenance.Replan(nil, p, hoje); err != nil {
				return traduzirRegra(err, "block.", nil)
			}
			if !unidade.Ativa {
				return apperr.Validation(map[string]string{
					"block": "a unidade está inativa: ela não é vendida, e não há o que bloquear.",
				})
			}
			periodo = &p
		}

		id, criada, err := s.repo.CriarOrdem(ctx, prop, novaOrdem{
			UnidadeID: c.UnidadeID, ComodoID: comodo, BemID: bem, AvariaID: c.AvariaID,
			Titulo: strings.TrimSpace(c.Titulo), Descricao: textoOuNulo(c.Descricao),
			Prioridade: prioridadeOuPadrao(c.Prioridade), CustoCents: c.CustoCents, AbertaPor: autor,
		})
		if err != nil {
			return err
		}
		if !criada {
			return s.avariaJaTemOrdem(ctx, prop, *c.AvariaID)
		}

		gravada, err := s.repo.TravarOrdem(ctx, prop, id)
		if err != nil {
			return err
		}
		if periodo != nil {
			if gravada, err = s.criarBloqueio(ctx, prop, gravada, *periodo, autor); err != nil {
				return err
			}
		}
		if err := audit.Criacao(ctx, s.repo.pool, entidadeOrdem, audit.VerboCriado, id, gravada); err != nil {
			return err
		}
		out, err = s.repo.BuscarOrdem(ctx, prop, id)
		return err
	})
	return out, err
}

// avariaJaTemOrdem monta o 409 com a ordem que já existe: o segundo toque
// vira navegação, não beco.
func (s *Service) avariaJaTemOrdem(ctx context.Context, prop, avaria uuid.UUID) error {
	existente, err := s.repo.OrdemAbertaDaAvaria(ctx, prop, avaria)
	if err != nil {
		return err
	}
	detalhes := map[string]any{}
	if existente != nil {
		detalhes["maintenance_order_id"] = *existente
	}
	return apperr.MaintenanceOrderOpen.WithDetails(detalhes)
}

// ═══════════════════════════ Cadastro ═════════════════════════════════════

// Substituir é o PUT: título, descrição, prioridade, cômodo, bem e custo. É
// sempre "outra coisa além do custo" para o domínio, então concluída e
// cancelada recusam.
func (s *Service) Substituir(ctx context.Context, id uuid.UUID, c OrdemDeManutencaoSubstituir) (OrdemDeManutencao, error) {
	return s.editar(ctx, id, maintenance.Edit{Cost: true, Other: true}, func(ctx context.Context, prop uuid.UUID, o *ordemGravada) error {
		comodo, bem, err := s.vinculos(ctx, prop, o.UnidadeID, origemDaOrdem(*o), c.ComodoID, c.BemID, nil, nil)
		if err != nil {
			return err
		}
		o.ComodoID, o.BemID = comodo, bem
		o.Titulo = strings.TrimSpace(c.Titulo)
		o.Descricao = textoOuNulo(c.Descricao)
		o.Prioridade = c.Prioridade
		o.CustoCents = c.CustoCents
		return nil
	})
}

// Atualizar é o PATCH: ausente não muda, `null` limpa. Corpo vazio responde
// 200 sem escrever, em qualquer estado; na concluída só o custo passa.
func (s *Service) Atualizar(ctx context.Context, id uuid.UUID, c OrdemDeManutencaoAtualizar) (OrdemDeManutencao, error) {
	if c.vazio() {
		return s.Buscar(ctx, id)
	}
	return s.editar(ctx, id, c.edicao(), func(ctx context.Context, prop uuid.UUID, o *ordemGravada) error {
		comodo, bem, err := s.vinculos(ctx, prop, o.UnidadeID, origemDaOrdem(*o), c.ComodoID, c.BemID, o.ComodoID, o.BemID)
		if err != nil {
			return err
		}
		o.ComodoID, o.BemID = comodo, bem
		if v, ok := c.Titulo.Definido(); ok {
			o.Titulo = strings.TrimSpace(v)
		}
		if c.Descricao.Set {
			v, ok := c.Descricao.Definido()
			if ok {
				o.Descricao = textoOuNulo(&v)
			} else {
				o.Descricao = nil
			}
		}
		if v, ok := c.Prioridade.Definido(); ok {
			o.Prioridade = v
		}
		if c.CustoCents.Set {
			v, ok := c.CustoCents.Definido()
			if ok {
				o.CustoCents = &v
			} else {
				o.CustoCents = nil
			}
		}
		return nil
	})
}

// editar é o roteiro comum do PUT e do PATCH: trava, pergunta ao domínio se o
// estado aceita a edição, aplica, grava e audita.
func (s *Service) editar(ctx context.Context, id uuid.UUID, e maintenance.Edit,
	aplicar func(ctx context.Context, prop uuid.UUID, o *ordemGravada) error) (OrdemDeManutencao, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return OrdemDeManutencao{}, err
	}
	var out OrdemDeManutencao
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarOrdem(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := maintenance.CheckEdit(antes.estado(), e); err != nil {
			return traduzirRegra(err, "", &antes)
		}
		depois := antes
		if err := aplicar(ctx, prop, &depois); err != nil {
			return err
		}
		if err := s.repo.GravarCadastro(ctx, depois); err != nil {
			return err
		}
		if err := audit.Alteracao(ctx, s.repo.pool, entidadeOrdem, audit.VerboAlterado, id, antes, depois); err != nil {
			return err
		}
		out, err = s.repo.BuscarOrdem(ctx, prop, id)
		return err
	})
	return out, err
}

// vinculos resolve cômodo e bem da ordem.
//
// Com avaria (`origem`), os dois SÃO os dela: ausente fica o dela, igual
// passa, diferente ou `null` é 422. Sem avaria: ausente fica `atual` (no PATCH,
// o que a ordem tem; no POST e no PUT, nada), `null` limpa, e um valor tem de
// ser cômodo DESTA unidade e bem do catálogo desta casa.
func (s *Service) vinculos(ctx context.Context, prop, unidade uuid.UUID, origem *avariaDeOrigem,
	comodo, bem httpx.Opt[uuid.UUID], comodoAtual, bemAtual *uuid.UUID) (*uuid.UUID, *uuid.UUID, error) {
	falhas := map[string]string{}

	if origem != nil {
		for _, v := range []struct {
			campo, nome string
			pedido      httpx.Opt[uuid.UUID]
			daAvaria    uuid.UUID
		}{
			{"room_id", "o cômodo", comodo, origem.ComodoID},
			{"item_id", "o bem", bem, origem.BemID},
		} {
			if !v.pedido.Set {
				continue
			}
			if valor, ok := v.pedido.Definido(); !ok || valor != v.daAvaria {
				falhas[v.campo] = fmt.Sprintf("a ordem é de uma avaria: %s é o dela (%s).", v.nome, v.daAvaria)
			}
		}
		if len(falhas) > 0 {
			return nil, nil, apperr.Validation(falhas)
		}
		c, b := origem.ComodoID, origem.BemID
		return &c, &b, nil
	}

	resolver := func(pedido httpx.Opt[uuid.UUID], atual *uuid.UUID) *uuid.UUID {
		if !pedido.Set {
			return atual
		}
		if v, ok := pedido.Definido(); ok {
			return &v
		}
		return nil
	}
	novoComodo, novoBem := resolver(comodo, comodoAtual), resolver(bem, bemAtual)

	if v, ok := comodo.Definido(); ok {
		dona, existe, err := s.repo.UnidadeDoComodo(ctx, prop, v)
		if err != nil {
			return nil, nil, err
		}
		switch {
		case !existe:
			falhas["room_id"] = "cômodo não encontrado nesta propriedade."
		case dona != unidade:
			falhas["room_id"] = "o cômodo não é desta unidade."
		}
	}
	if v, ok := bem.Definido(); ok {
		existe, err := s.repo.BemExiste(ctx, prop, v)
		if err != nil {
			return nil, nil, err
		}
		if !existe {
			falhas["item_id"] = "bem não encontrado nesta propriedade."
		}
	}
	if len(falhas) > 0 {
		return nil, nil, apperr.Validation(falhas)
	}
	return novoComodo, novoBem, nil
}

// origemDaOrdem: a ordem ligada a avaria já carrega cômodo e bem DELA — a FK
// composta `(issue_id, room_id, item_id)` garante —, então não é preciso
// reler a avaria.
func origemDaOrdem(o ordemGravada) *avariaDeOrigem {
	if o.AvariaID == nil || o.ComodoID == nil || o.BemID == nil {
		return nil
	}
	return &avariaDeOrigem{ComodoID: *o.ComodoID, BemID: *o.BemID, UnidadeID: o.UnidadeID}
}

// optDoPost: no POST, `null` vale o mesmo que ausente — não há o que limpar.
func optDoPost(v *uuid.UUID) httpx.Opt[uuid.UUID] {
	if v == nil {
		return httpx.Opt[uuid.UUID]{}
	}
	return httpx.De(*v)
}

func prioridadeOuPadrao(p *string) string {
	if p == nil {
		return string(maintenance.DefaultPriority)
	}
	return *p
}

// ═══════════════════════════ Transições ═══════════════════════════════════

// Iniciar é `aberta → em_andamento`. Não mexe no calendário.
func (s *Service) Iniciar(ctx context.Context, id uuid.UUID) (OrdemDeManutencao, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return OrdemDeManutencao{}, err
	}
	var out OrdemDeManutencao
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarOrdem(ctx, prop, id)
		if err != nil {
			return err
		}
		if _, err := maintenance.Next(antes.estado(), maintenance.Start); err != nil {
			return traduzirRegra(err, "", &antes)
		}
		if err := s.repo.Iniciar(ctx, id); err != nil {
			return err
		}
		return s.auditarEResponder(ctx, prop, id, verboIniciada, antes, &out)
	})
	return out, err
}

// Concluir encerra a ordem como feita: libera o bloqueio (ReleaseOn) e
// conserta a avaria ainda aberta (IssueOnClose), tudo no mesmo commit.
func (s *Service) Concluir(ctx context.Context, id uuid.UUID, c ConclusaoDaOrdem) (OrdemDeManutencao, error) {
	var custo *int64
	if v, ok := c.CustoCents.Definido(); ok {
		custo = &v
	}
	return s.encerrar(ctx, id, maintenance.Complete, verboConcluida, custo)
}

// Cancelar é o DELETE: encerra sem serviço. Libera o bloqueio pela mesma
// regra; a avaria NÃO é tocada (IssueOnClose de `cancelada` não resolve).
func (s *Service) Cancelar(ctx context.Context, id uuid.UUID) (OrdemDeManutencao, error) {
	return s.encerrar(ctx, id, maintenance.Cancel, verboCancelada, nil)
}

func (s *Service) encerrar(ctx context.Context, id uuid.UUID, acao maintenance.Action, verbo string, custo *int64) (OrdemDeManutencao, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return OrdemDeManutencao{}, err
	}
	autor := atorOuNulo(ctx)

	var out OrdemDeManutencao
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarOrdem(ctx, prop, id)
		if err != nil {
			return err
		}
		destino, err := maintenance.Next(antes.estado(), acao)
		if err != nil {
			return traduzirRegra(err, "", &antes)
		}
		if _, err := s.liberarBloqueio(ctx, prop, antes); err != nil {
			return err
		}
		if desfecho, resolve := maintenance.IssueOnClose(destino); resolve && antes.AvariaID != nil {
			if err := s.consertarAvaria(ctx, *antes.AvariaID, desfecho, autor, id); err != nil {
				return err
			}
		}
		if err := s.repo.Encerrar(ctx, id, destino, autor, custo); err != nil {
			return err
		}
		return s.auditarEResponder(ctx, prop, id, verbo, antes, &out)
	})
	return out, err
}

// consertarAvaria aplica o desfecho do domínio à avaria AINDA ABERTA; a que já
// foi resolvida por uma pessoa fica como está.
func (s *Service) consertarAvaria(ctx context.Context, avaria uuid.UUID, desfecho string, autor *uuid.UUID, ordem uuid.UUID) error {
	depois, mudou, err := s.repo.ConsertarAvaria(ctx, avaria, desfecho, autor)
	if err != nil || !mudou {
		return err
	}
	// A ordem entra só no `depois`: a linha da trilha diz "resolvida pela
	// ordem X", que é a pergunta de quem a lê.
	depois.OrdemID = &ordem
	return audit.Alteracao(ctx, s.repo.pool, entidadeAvaria, verboResolvida, avaria, avariaAuditada{}, depois)
}

// auditarEResponder relê a ordem travada (o `depois` da trilha sai do banco,
// pelo mesmo caminho do `antes`) e monta a resposta.
func (s *Service) auditarEResponder(ctx context.Context, prop, id uuid.UUID, verbo string, antes ordemGravada, out *OrdemDeManutencao) error {
	depois, err := s.repo.TravarOrdem(ctx, prop, id)
	if err != nil {
		return err
	}
	if err := audit.Alteracao(ctx, s.repo.pool, entidadeOrdem, verbo, id, antes, depois); err != nil {
		return err
	}
	*out, err = s.repo.BuscarOrdem(ctx, prop, id)
	return err
}

// ═══════════════════════════ O bloqueio ═══════════════════════════════════

// Bloquear é o `PUT /{id}/block`: cria, altera ou não mexe, conforme
// `maintenance.Replan` decide com o bloqueio atual e HOJE.
func (s *Service) Bloquear(ctx context.Context, id uuid.UUID, corpo PeriodoDoBloqueio) (OrdemDeManutencao, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return OrdemDeManutencao{}, err
	}
	pedido, err := corpo.periodo()
	if err != nil {
		return OrdemDeManutencao{}, apperr.Validation(map[string]string{"from": "período ilegível."}).WithCause(err)
	}
	autor := atorOuNulo(ctx)

	var out OrdemDeManutencao
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		ordem, err := s.repo.TravarOrdem(ctx, prop, id)
		if err != nil {
			return err
		}
		// O período do bloqueio é "outra coisa além do custo" para o domínio:
		// concluída e cancelada recusam (o encerramento já liberou).
		if err := maintenance.CheckEdit(ordem.estado(), maintenance.Edit{Other: true}); err != nil {
			return traduzirRegra(err, "", &ordem)
		}
		hoje, err := s.repo.Hoje(ctx, prop)
		if err != nil {
			return err
		}

		var (
			atual   *maintenance.Block
			gravado bloqueioGravado
		)
		if ordem.BloqueioID != nil {
			if gravado, err = s.repo.TravarBloqueio(ctx, *ordem.BloqueioID, id); err != nil {
				return err
			}
			d, err := gravado.dominio()
			if err != nil {
				return apperr.Internal.WithCause(err)
			}
			atual = &d
		}

		decisao, err := maintenance.Replan(atual, pedido, hoje)
		if err != nil {
			return traduzirRegra(err, "", &ordem)
		}
		if decisao == maintenance.Create || decisao == maintenance.Change {
			// A fila por unidade de quem escreve período (ver
			// TravarCalendarioDaUnidade). Depois da trava da ordem: quem segura
			// a da unidade nunca espera por uma ordem que já existe.
			if err := s.repo.TravarCalendarioDaUnidade(ctx, ordem.UnidadeID); err != nil {
				return err
			}
		}
		switch decisao {
		case maintenance.NoChange:
		case maintenance.Create:
			unidade, _, err := s.repo.Unidade(ctx, prop, ordem.UnidadeID)
			if err != nil {
				return err
			}
			if !unidade.Ativa {
				return apperr.Validation(map[string]string{
					"block": "a unidade está inativa: ela não é vendida, e não há o que bloquear.",
				})
			}
			depois, err := s.criarBloqueio(ctx, prop, ordem, pedido, autor)
			if err != nil {
				return err
			}
			if err := audit.Alteracao(ctx, s.repo.pool, entidadeOrdem, audit.VerboAlterado, id, ordem, depois); err != nil {
				return err
			}
		case maintenance.Change:
			if err := s.repo.MudarPeriodo(ctx, gravado.ID, ordem.UnidadeCodigo, pedido); err != nil {
				return err
			}
			if err := audit.Alteracao(ctx, s.repo.pool, entidadeBloqueio, verboRemarcada, gravado.ID,
				gravado, gravado.comPeriodo(pedido)); err != nil {
				return err
			}
			if err := s.repo.TocarOrdem(ctx, id); err != nil {
				return err
			}
		default:
			return apperr.Internal.WithCause(fmt.Errorf("manutencao: decisão de remarcação desconhecida %q", decisao))
		}
		out, err = s.repo.BuscarOrdem(ctx, prop, id)
		return err
	})
	return out, err
}

// SoltarBloqueio é o `DELETE /{id}/block`: a regra da liberação ao encerrar,
// sem encerrar. Bloqueio terminado, já liberado ou inexistente: 200 igual.
func (s *Service) SoltarBloqueio(ctx context.Context, id uuid.UUID) (OrdemDeManutencao, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return OrdemDeManutencao{}, err
	}
	var out OrdemDeManutencao
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		ordem, err := s.repo.TravarOrdem(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := maintenance.CheckEdit(ordem.estado(), maintenance.Edit{Other: true}); err != nil {
			return traduzirRegra(err, "", &ordem)
		}
		mudou, err := s.liberarBloqueio(ctx, prop, ordem)
		if err != nil {
			return err
		}
		if mudou {
			if err := s.repo.TocarOrdem(ctx, id); err != nil {
				return err
			}
		}
		out, err = s.repo.BuscarOrdem(ctx, prop, id)
		return err
	})
	return out, err
}

// criarBloqueio insere a linha nova de `stay_blocks` e aponta a ordem para
// ela. A antiga, se havia (terminada ou liberada), fica como história.
func (s *Service) criarBloqueio(ctx context.Context, prop uuid.UUID, ordem ordemGravada, p maintenance.Period, autor *uuid.UUID) (ordemGravada, error) {
	bloco, err := s.repo.CriarBloqueio(ctx, prop, ordem.UnidadeID, ordem.UnidadeCodigo, p, ordem.Titulo, autor)
	if err != nil {
		return ordem, err
	}
	if err := s.repo.ApontarBloqueio(ctx, ordem.ID, bloco); err != nil {
		return ordem, err
	}
	gravado, err := s.repo.TravarBloqueio(ctx, bloco, ordem.ID)
	if err != nil {
		return ordem, err
	}
	if err := audit.Criacao(ctx, s.repo.pool, entidadeBloqueio, verboBloqueada, bloco, gravado); err != nil {
		return ordem, err
	}
	ordem.BloqueioID = &bloco
	return ordem, nil
}

// liberarBloqueio aplica `maintenance.ReleaseOn` no dia de hoje: o que não
// começou é solto inteiro (`cancelled`, a linha fica), o que está em curso tem
// o fim cortado para hoje, o que terminou fica. Devolve se a linha mudou.
func (s *Service) liberarBloqueio(ctx context.Context, prop uuid.UUID, ordem ordemGravada) (bool, error) {
	if ordem.BloqueioID == nil {
		return false, nil
	}
	hoje, err := s.repo.Hoje(ctx, prop)
	if err != nil {
		return false, err
	}
	gravado, err := s.repo.TravarBloqueio(ctx, *ordem.BloqueioID, ordem.ID)
	if err != nil {
		return false, err
	}
	b, err := gravado.dominio()
	if err != nil {
		return false, apperr.Internal.WithCause(err)
	}

	switch decisao := maintenance.ReleaseOn(b, hoje); decisao.Kind {
	case maintenance.Keep:
		return false, nil
	case maintenance.Drop:
		if err := s.repo.SoltarBloqueio(ctx, gravado.ID); err != nil {
			return false, err
		}
		return true, audit.Alteracao(ctx, s.repo.pool, entidadeBloqueio, verboDesbloqueada, gravado.ID,
			gravado, gravado.comStatus(statusBloqueioSolto))
	case maintenance.Truncate:
		if err := s.repo.MudarPeriodo(ctx, gravado.ID, gravado.UnidadeCodigo, decisao.Period); err != nil {
			return false, err
		}
		return true, audit.Alteracao(ctx, s.repo.pool, entidadeBloqueio, verboEncurtada, gravado.ID,
			gravado, gravado.comPeriodo(decisao.Period))
	default:
		return false, apperr.Internal.WithCause(fmt.Errorf("manutencao: liberação desconhecida %q", decisao.Kind))
	}
}

// ═══════════════════════════ Tradução do domínio ══════════════════════════

// traduzirRegra leva o `maintenance.RuleError` ao erro do contrato: o code e
// o status vêm do catálogo (`apperr.PorCodigo`), a frase e os `details` do
// domínio. `prefixo` aninha os campos do período (`block.from`) quando ele
// veio dentro de outro corpo. Ordem encerrada ganha `details.closed_at`.
func traduzirRegra(err error, prefixo string, ordem *ordemGravada) error {
	var regra *maintenance.RuleError
	if !errors.As(err, &regra) {
		return apperr.Internal.WithCause(err)
	}
	base, ok := apperr.PorCodigo(regra.Code)
	if !ok {
		return apperr.Internal.WithCause(err)
	}
	detalhes := make(map[string]any, len(regra.Details)+1)
	for k, v := range regra.Details {
		if regra.Code == apperr.CodeValidationError {
			k = prefixo + k
		}
		detalhes[k] = v
	}
	if regra.Code == apperr.CodeMaintenanceOrderClosed && ordem != nil && ordem.FechadaEm != nil {
		detalhes["closed_at"] = *ordem.FechadaEm
	}
	return base.WithMessage(regra.Message).WithDetails(detalhes).WithCause(err)
}
