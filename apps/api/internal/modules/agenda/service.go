package agenda

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	regra "github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/agenda"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Transacionador é o que o service precisa do db.TxManager.
type Transacionador interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

var _ Transacionador = (*db.TxManager)(nil)

// Service orquestra a agenda: transação, escopo, a pergunta ao domínio com
// "hoje" e "agora" lidos do banco no fuso da casa, a escrita e a trilha.
//
// Toda escrita manual segue o roteiro da especificação, numa transação só:
//
//  1. a forma do corpo (o DTO, no handler);
//  2. o responsável (`regra.ResolveAssignee`) — em `own`, é o próprio ator;
//  3. a janela (`regra.CheckWindow`);
//  4. a existência do que o evento cita (unidade ativa, reserva viva, unidade
//     da reserva, contato não anonimizado, usuário ativo);
//  5. na edição, o evento travado (FOR UPDATE) e `regra.CheckEdit`;
//  6. a unidade (`regra.CheckUnit` sobre as ocupações carregadas);
//  7. a gravação — e a disputa entre duas marcações à mão é da constraint;
//  8. os avisos de horário (`regra.TimeWarnings`) em `meta.warnings`;
//  9. `audit_log`.
type Service struct {
	repo *Repository
	tx   Transacionador
}

// NewService monta o service.
func NewService(repo *Repository, tx Transacionador) *Service {
	return &Service{repo: repo, tx: tx}
}

// Entidades e verbos da trilha. As entidades são nomes de TABELA.
const (
	entidadeEvento       = "agenda_events"
	entidadeBloqueio     = "agenda_blocks"
	entidadeConfiguracao = "agenda_settings"
	verboConcluido       = "concluido"
	verboCancelado       = "cancelado"
	verboReprogramado    = "reprogramado"
	verboMovido          = "movido"
	verboRealocado       = "realocado"
	// verboRealocadoSemUnidade — a reserva foi realocada e o evento manual
	// preso à unidade que saiu caiu, na de destino, sobre outra marcação à
	// mão: ele seguiu a reserva sem unidade própria.
	verboRealocadoSemUnidade = "realocado_sem_unidade"
	mensagemDeUnidadeTomada  = "A unidade está ocupada nesse horário."
)

// propriedadeDoAtor resolve a casa em que a requisição opera.
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

// atorOuNulo é o autor das colunas `*_by`. Nulo fora de requisição (o seed).
func atorOuNulo(ctx context.Context) *uuid.UUID {
	if id, ok := auth.UsuarioID(ctx); ok {
		return &id
	}
	return nil
}

// escopo lê o alcance do ator NA AÇÃO, da matriz de RBAC — nunca o nome do
// perfil. Sem célula é 403: a rota já passou pelo middleware, então chegar
// aqui sem escopo é a matriz mudando no meio da requisição, e negar é a
// resposta segura.
func escopo(ctx context.Context, acao string) (regra.Scope, error) {
	switch auth.ScopeOf(ctx, Recurso, acao) {
	case auth.EscopoAll:
		return regra.ScopeAll, nil
	case auth.EscopoOwn:
		return regra.ScopeOwn, nil
	}
	return "", apperr.Forbidden.WithDetails(map[string]string{"resource": Recurso, "action": acao})
}

// responsavelDoEscopo é o `assignee_id = <ator>` do escopo `own`, ou nil em
// `all`. É o que vai para o WHERE — nunca filtro em memória.
func responsavelDoEscopo(ctx context.Context, acao string) (*uuid.UUID, error) {
	e, err := escopo(ctx, acao)
	if err != nil || e != regra.ScopeOwn {
		return nil, err
	}
	id, ok := auth.UsuarioID(ctx)
	if !ok {
		return nil, apperr.Unauthorized
	}
	return &id, nil
}

// ═══════════════════════════ Leitura ══════════════════════════════════════

// Listar é o `GET /agenda/events`: os gravados e os derivados da janela, em
// ordem de início (empate por id) e paginados no envelope padrão.
func (s *Service) Listar(ctx context.Context, f FiltroDeEventos) ([]EventoDaAgenda, int64, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, 0, err
	}
	responsavel, err := responsavelDoEscopo(ctx, auth.AcaoVer)
	if err != nil {
		return nil, 0, err
	}
	rel, err := s.repo.Relogio(ctx, prop)
	if err != nil {
		return nil, 0, err
	}
	cfg, err := s.repo.Configuracao(ctx, prop, semTrava)
	if err != nil {
		return nil, 0, err
	}

	consulta := consultaDaLista{FiltroDeEventos: f, Responsavel: responsavel}
	if f.De != nil && f.Ate != nil {
		inicio, fim := rel.zona.StartOfDay(*f.De), rel.zona.StartOfDay(*f.Ate)
		consulta.InicioDaJanela, consulta.FimDaJanela = &inicio, &fim
	}
	linhas, err := s.repo.Linhas(ctx, prop, consulta)
	if err != nil {
		return nil, 0, err
	}
	if err := instantesDosDerivados(linhas, rel.zona); err != nil {
		return nil, 0, err
	}
	ordenar(linhas, f.Decrescente)

	total := int64(len(linhas))
	pagina := linhas[min(httpx.Offset(f.Pagina, f.PorPagina), len(linhas)):]
	pagina = pagina[:min(f.PorPagina, len(pagina))]

	eventos, err := s.montar(ctx, prop, rel, cfg, pagina)
	if err != nil {
		return nil, 0, err
	}
	return eventos, total, nil
}

// Buscar é o detalhe de um evento GRAVADO. O id de um derivado é 404 aqui (a
// tela abre a ordem ou o bloqueio), e em `own` o evento de outro responsável
// também — nunca 403.
func (s *Service) Buscar(ctx context.Context, id uuid.UUID) (EventoDaAgenda, error) {
	return s.buscarNoEscopo(ctx, id, auth.AcaoVer)
}

// buscarNoEscopo lê o evento no escopo da AÇÃO pedida — o `PATCH {}`, que só
// relê, responde 404 no mesmo lugar em que a edição responderia.
func (s *Service) buscarNoEscopo(ctx context.Context, id uuid.UUID, acao string) (EventoDaAgenda, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return EventoDaAgenda{}, err
	}
	responsavel, err := responsavelDoEscopo(ctx, acao)
	if err != nil {
		return EventoDaAgenda{}, err
	}
	rel, err := s.repo.Relogio(ctx, prop)
	if err != nil {
		return EventoDaAgenda{}, err
	}
	cfg, err := s.repo.Configuracao(ctx, prop, semTrava)
	if err != nil {
		return EventoDaAgenda{}, err
	}
	return s.lerEvento(ctx, prop, id, responsavel, rel, cfg)
}

// lerEvento relê um evento gravado pronto para a resposta — o detalhe, e a
// resposta de toda escrita (na transação dela).
func (s *Service) lerEvento(ctx context.Context, prop, id uuid.UUID, responsavel *uuid.UUID, rel relogio, cfg configuracaoGravada) (EventoDaAgenda, error) {
	linhas, err := s.repo.Linhas(ctx, prop, consultaDaLista{SoOEvento: &id, Responsavel: responsavel})
	if err != nil {
		return EventoDaAgenda{}, err
	}
	if len(linhas) == 0 {
		return EventoDaAgenda{}, apperr.NotFound("Evento da agenda")
	}
	eventos, err := s.montar(ctx, prop, rel, cfg, linhas[:1])
	if err != nil {
		return EventoDaAgenda{}, err
	}
	return eventos[0], nil
}

// instantesDosDerivados converte o período de noites do derivado em instantes
// — da meia-noite DA CASA do primeiro dia à do dia seguinte ao último —, pela
// única porta de conversão (`calendar.Zone`).
func instantesDosDerivados(linhas []linhaDaLista, z calendar.Zone) error {
	for i := range linhas {
		if !linhas[i].Derivado {
			continue
		}
		de, ate, err := periodo(linhas[i].De, linhas[i].Ate)
		if err != nil {
			return apperr.Internal.WithCause(err)
		}
		linhas[i].Inicio, linhas[i].Fim = z.StartOfDay(de), z.StartOfDay(ate)
	}
	return nil
}

// ordenar põe as linhas por início e, no empate, por id — a ordem do contrato,
// e a que deixa a paginação estável.
func ordenar(linhas []linhaDaLista, decrescente bool) {
	sort.SliceStable(linhas, func(i, j int) bool {
		a, b := linhas[i], linhas[j]
		if !a.Inicio.Equal(b.Inicio) {
			if decrescente {
				return a.Inicio.After(b.Inicio)
			}
			return a.Inicio.Before(b.Inicio)
		}
		if decrescente {
			return a.ID.String() > b.ID.String()
		}
		return a.ID.String() < b.ID.String()
	})
}

// montar transforma as linhas no schema `EventoDaAgenda`: as unidades (a
// gravada, as da reserva, a do bloqueio), o título composto do automático, o
// dia da casa, as ações e a edição permitidas HOJE e o conflito de unidade —
// calculado pela MESMA regra que recusa a escrita.
func (s *Service) montar(ctx context.Context, prop uuid.UUID, rel relogio, cfg configuracaoGravada, linhas []linhaDaLista) ([]EventoDaAgenda, error) {
	z, hoje := rel.zona, rel.hoje()

	reservas, avulsas := []uuid.UUID{}, []uuid.UUID{}
	vistas := map[uuid.UUID]bool{}
	for _, l := range linhas {
		switch {
		case l.UnidadeID != nil:
			if !vistas[*l.UnidadeID] {
				vistas[*l.UnidadeID] = true
				avulsas = append(avulsas, *l.UnidadeID)
			}
		case l.ReservaID != nil:
			reservas = append(reservas, *l.ReservaID)
		}
	}
	porReserva, err := s.repo.unidadesDasReservas(ctx, reservas)
	if err != nil {
		return nil, err
	}
	codigos, err := s.repo.codigosDasUnidades(ctx, avulsas)
	if err != nil {
		return nil, err
	}

	out := make([]EventoDaAgenda, len(linhas))
	for i, l := range linhas {
		e := EventoDaAgenda{
			ID: l.ID, Tipo: l.Tipo, Origem: l.Origem, Status: l.Status,
			Inicio: z.In(l.Inicio), Fim: z.In(l.Fim), DiaInteiro: l.DiaInteiro,
			Data:      z.DateOf(l.Inicio).String(),
			Unidades:  []UnidadeDoEventoDaAgenda{},
			ReservaID: l.ReservaID, ReservaCodigo: l.ReservaCodigo,
			ContatoID: l.ContatoID, ContatoNome: l.ContatoNome,
			ResponsavelID: l.ResponsavelID, ResponsavelNome: l.ResponsavelNome,
			Notas: l.Notas, ConcluidoEm: noFuso(z, l.ConcluidoEm), ConcluidoPor: l.ConcluidoPor,
			CanceladoEm: noFuso(z, l.CanceladoEm), CriadoPor: l.CriadoPor,
			CriadoEm: noFuso(z, l.CriadoEm), AtualizadoEm: noFuso(z, l.AtualizadoEm),
			AcoesPermitidas: []string{},
		}
		switch {
		case l.UnidadeID != nil:
			e.Unidades = append(e.Unidades, UnidadeDoEventoDaAgenda{ID: *l.UnidadeID, Codigo: codigos[*l.UnidadeID]})
		case l.ReservaID != nil:
			e.Unidades = append(e.Unidades, porReserva[*l.ReservaID]...)
		}

		if l.Derivado {
			// O derivado mexe-se onde nasceu (a ordem, o `/blocks`): nada aqui.
			e.Titulo = tituloDoDerivado(l)
			e.BloqueioID = &l.ID
			e.OrdemDeManutencaoID = l.OrdemID
			e.Editavel = string(regra.EditNone)
			e.CriadoPor = nil
		} else {
			e.UnidadeID = l.UnidadeID
			fatos := regra.Facts{Type: regra.Type(l.Tipo), Origin: regra.Origin(l.Origem), Status: regra.Status(l.Status), Date: z.DateOf(l.Inicio)}
			e.Titulo = tituloDoGravado(l)
			e.AcoesPermitidas = nomesDasAcoes(regra.AllowedActions(fatos, hoje))
			e.Editavel = string(regra.EditableIn(fatos))
		}
		out[i] = e
	}

	if err := s.marcarConflitos(ctx, prop, cfg, z, linhas, out); err != nil {
		return nil, err
	}
	return out, nil
}

// marcarConflitos calcula `has_unit_conflict` dos eventos gravados e agendados
// que ocupam unidade: o evento que já estava na agenda quando a venda, a
// manutenção ou outro ajuste chegou passa a colidir — e a lista mostra, em vez
// de esconder. Fechado e derivado: sempre `false`.
func (s *Service) marcarConflitos(ctx context.Context, prop uuid.UUID, cfg configuracaoGravada, z calendar.Zone,
	linhas []linhaDaLista, eventos []EventoDaAgenda) error {
	var (
		unidades []uuid.UUID
		vistas   = map[uuid.UUID]bool{}
		janela   regra.Window
		algum    bool
	)
	for i, l := range linhas {
		if l.Derivado || l.Status != string(regra.Scheduled) || len(eventos[i].Unidades) == 0 {
			continue
		}
		for _, u := range eventos[i].Unidades {
			if !vistas[u.ID] {
				vistas[u.ID] = true
				unidades = append(unidades, u.ID)
			}
		}
		if !algum || l.Inicio.Before(janela.Start) {
			janela.Start = l.Inicio
		}
		if !algum || l.Fim.After(janela.End) {
			janela.End = l.Fim
		}
		algum = true
	}
	if !algum {
		return nil
	}
	ocupacoes, err := s.repo.Ocupacoes(ctx, prop, unidades, janela, cfg.Regras, z)
	if err != nil {
		return err
	}
	for i, l := range linhas {
		if l.Derivado || l.Status != string(regra.Scheduled) || len(eventos[i].Unidades) == 0 {
			continue
		}
		c := regra.Candidate{Event: l.ID.String(), Units: idsEmTexto(eventos[i].Unidades),
			Window: regra.Window{Start: l.Inicio, End: l.Fim}, AllDay: l.DiaInteiro}
		if l.ReservaID != nil {
			c.Reservation = l.ReservaID.String()
		}
		eventos[i].ConflitoDeUnidade = len(regra.UnitConflicts(c, ocupacoes)) > 0
	}
	return nil
}

// tituloDoGravado: no manual, o gravado; no automático, composto na leitura
// (`regra.AutoTitle`) — a regra de composição muda sem migration.
func tituloDoGravado(l linhaDaLista) string {
	if l.Origem == string(regra.FromReservation) || l.Titulo == nil {
		codigo := ""
		if l.ReservaCodigo != nil {
			codigo = *l.ReservaCodigo
		}
		return regra.AutoTitle(regra.Type(l.Tipo), codigo)
	}
	return *l.Titulo
}

// tituloDoDerivado: na manutenção, o título da ordem; no bloqueio de unidade,
// a nota do bloqueio; sem nenhum dos dois, o nome do tipo.
func tituloDoDerivado(l linhaDaLista) string {
	for _, t := range []*string{l.Titulo, l.Nota} {
		if t != nil && *t != "" {
			return *t
		}
	}
	return regra.Type(l.Tipo).Label()
}

func idsEmTexto(us []UnidadeDoEventoDaAgenda) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.ID.String()
	}
	return out
}

// noFuso devolve o instante no offset da casa — o que a API serializa.
func noFuso(z calendar.Zone, t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := z.In(*t)
	return &v
}

// ═══════════════════════════ Tradução do domínio ══════════════════════════

// traduzirRegra leva o `regra.RuleError` ao erro do contrato: o code e o
// status vêm do catálogo (`apperr.PorCodigo`), a frase e os `details` do
// domínio. No `UNIT_NOT_AVAILABLE`, cada conflito ganha o `unit_code` (o
// domínio só conhece o id). Erro que não é de regra é defeito — 500.
func (s *Service) traduzirRegra(ctx context.Context, err error) error {
	var r *regra.RuleError
	if !errors.As(err, &r) {
		return apperr.Internal.WithCause(err)
	}
	base, ok := apperr.PorCodigo(r.Code)
	if !ok {
		return apperr.Internal.WithCause(err)
	}
	detalhes := make(map[string]any, len(r.Details))
	for k, v := range r.Details {
		detalhes[k] = v
	}
	if r.Code == apperr.CodeUnitNotAvailable {
		if err := s.repo.comCodigosDasUnidades(ctx, detalhes); err != nil {
			return err
		}
	}
	return base.WithMessage(r.Message).WithDetails(detalhes).WithCause(err)
}

// comCodigosDasUnidades acrescenta `unit_code` a cada item de
// `details.conflicts` — o que a tela mostra ("AP-01 em limpeza das 11:00 às
// 14:00").
func (r *Repository) comCodigosDasUnidades(ctx context.Context, detalhes map[string]any) error {
	conflitos, ok := detalhes["conflicts"].([]map[string]any)
	if !ok {
		return nil
	}
	ids := []uuid.UUID{}
	for _, c := range conflitos {
		if texto, ok := c["unit_id"].(string); ok {
			if id, err := uuid.Parse(texto); err == nil {
				ids = append(ids, id)
			}
		}
	}
	codigos, err := r.codigosDasUnidades(ctx, ids)
	if err != nil {
		return err
	}
	copia := make([]map[string]any, len(conflitos))
	for i, c := range conflitos {
		item := make(map[string]any, len(c)+1)
		for k, v := range c {
			item[k] = v
		}
		if texto, ok := c["unit_id"].(string); ok {
			if id, err := uuid.Parse(texto); err == nil {
				item["unit_code"] = codigos[id]
			}
		}
		copia[i] = item
	}
	detalhes["conflicts"] = copia
	return nil
}

// conflitoDepoisDoRollback responde a disputa que a CONSTRAINT decidiu (23P01
// em `agenda_events_unidade_sem_sobreposicao`): a transação abortou, e os
// `details.conflicts` são relidos fora dela, pela mesma regra da conferência
// (`regra.CheckUnit`). Se o outro evento já saiu do caminho entre o rollback e
// a releitura, a resposta continua 409 — a recusa aconteceu —, com a lista
// vazia.
func (s *Service) conflitoDepoisDoRollback(ctx context.Context, prop uuid.UUID, c regra.Candidate, causa error) error {
	rel, err := s.repo.Relogio(ctx, prop)
	if err != nil {
		return err
	}
	cfg, err := s.repo.Configuracao(ctx, prop, semTrava)
	if err != nil {
		return err
	}
	unidades := make([]uuid.UUID, 0, len(c.Units))
	for _, u := range c.Units {
		if id, err := uuid.Parse(u); err == nil {
			unidades = append(unidades, id)
		}
	}
	ocupacoes, err := s.repo.Ocupacoes(ctx, prop, unidades, c.Window, cfg.Regras, rel.zona)
	if err != nil {
		return err
	}
	if err := regra.CheckUnit(c, ocupacoes, rel.zona); err != nil {
		traduzido := s.traduzirRegra(ctx, err)
		var ap *apperr.Error
		if errors.As(traduzido, &ap) {
			return ap.WithCause(causa)
		}
		return traduzido
	}
	return apperr.UnitNotAvailable.WithMessage(mensagemDeUnidadeTomada).
		WithDetails(map[string]any{"conflicts": []map[string]any{}}).WithCause(causa)
}
