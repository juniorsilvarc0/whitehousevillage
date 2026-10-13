//go:build integration

package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// ─────────────────────────── Cenário com ids reais ──────────────────────────

// qagCenario é um evento manual de HOJE (concluir funcionaria) e um bloqueio de
// horário — o alvo real de cada `{id}` das 15 operações. Com id sorteado, um
// 404 do handler esconderia a falta de checagem de permissão; com id real, só
// o RBAC separa o 403 do 200.
type qagCenario struct {
	Casa     qagCasa
	Unidade  uuid.UUID
	Evento   uuid.UUID
	Bloqueio uuid.UUID
	Marca    string
}

func (a *ambiente) qagMontarCenario(t *testing.T, f *qagFaxina, token string) qagCenario {
	t.Helper()
	c := qagCenario{Casa: a.qagCasa(t, a.qbPropriedadePadrao(t)), Marca: "Invasão " + qbSufixo()}
	c.Unidade, _ = f.unidade(t, c.Casa.Prop)
	hoje := c.Casa.Hoje
	c.Evento = a.qagMarcar(t, token, map[string]any{
		"type": "visita", "title": "Visita do cenário", "unit_id": c.Unidade,
		"starts_at": c.Casa.rfc(hoje, "10:00"), "ends_at": c.Casa.rfc(hoje, "10:30"),
	}).Data.ID
	c.Bloqueio = qbDado[qagBloqueio](t, a.chamar(t, http.MethodPost, "/agenda/blocks", token, map[string]any{
		"starts_at": c.Casa.rfc(hoje.AddDays(3), "14:00"), "ends_at": c.Casa.rfc(hoje.AddDays(3), "16:00"),
		"reason": "Equipe em treinamento",
	}), http.StatusCreated, "POST /agenda/blocks").ID
	return c
}

func (c qagCenario) caminho(op qbOperacao) string {
	p := op.Path
	switch {
	case strings.HasPrefix(p, "/agenda/events/{id}"):
		p = strings.Replace(p, "{id}", c.Evento.String(), 1)
	case strings.HasPrefix(p, "/agenda/blocks/{id}"):
		p = strings.Replace(p, "{id}", c.Bloqueio.String(), 1)
	}
	if strings.Contains(p, "{") {
		panic("parâmetro sem alvo no cenário: " + op.Path)
	}
	if op.Metodo == http.MethodGet && (p == "/agenda/events" || p == "/agenda/blocks") {
		p += "?from=" + c.Casa.Hoje.String() + "&to=" + c.Casa.Hoje.AddDays(30).String()
	}
	return p
}

// corpo devolve um corpo VÁLIDO: se a permissão faltar no servidor, a escrita
// teria efeito — e é isso que o teste depois confere no banco.
func (c qagCenario) corpo(op qbOperacao) any {
	d := c.Casa.Hoje.AddDays(40)
	switch op.chave() {
	case "POST /agenda/events":
		return map[string]any{"type": "evento", "title": c.Marca, "starts_at": c.Casa.rfc(d, "10:00")}
	case "PUT /agenda/events/{id}":
		return map[string]any{"type": "visita", "title": c.Marca, "unit_id": c.Unidade,
			"starts_at": c.Casa.rfc(c.Casa.Hoje, "10:00"), "ends_at": c.Casa.rfc(c.Casa.Hoje, "10:30")}
	case "PATCH /agenda/events/{id}":
		return map[string]any{"notes": c.Marca}
	case "POST /agenda/blocks":
		return map[string]any{"starts_at": c.Casa.rfc(d, "08:00"), "ends_at": c.Casa.rfc(d, "09:00"), "reason": c.Marca}
	case "PUT /agenda/blocks/{id}":
		return map[string]any{"starts_at": c.Casa.rfc(d, "08:00"), "ends_at": c.Casa.rfc(d, "09:00"), "reason": c.Marca}
	case "PATCH /agenda/blocks/{id}":
		return map[string]any{"reason": c.Marca}
	case "PUT /agenda/settings":
		return qagCorpoDosHorarios("14:00", "11:00", "11:00", "14:00", 30, 15)
	}
	return nil
}

func (a *ambiente) qagExecutar(t *testing.T, c qagCenario, op qbOperacao, token string) resposta {
	t.Helper()
	return a.chamar(t, op.Metodo, c.caminho(op), token, c.corpo(op))
}

// qagEstado é o que uma escrita recusada não pode ter mudado.
type qagEstado struct {
	Evento, Bloqueio, Horarios string
	ComAMarca                  int
}

func (a *ambiente) qagLerEstado(t *testing.T, c qagCenario) qagEstado {
	t.Helper()
	var e qagEstado
	if err := a.pool.QueryRow(a.ctx, `
		SELECT concat_ws('|', type, title, status, starts_at, ends_at, unit_id, assignee_id, notes, done_at, cancelled_at)
		  FROM agenda_events WHERE id = $1`, c.Evento).Scan(&e.Evento); err != nil {
		t.Fatalf("lendo o evento do cenário: %v", err)
	}
	if err := a.pool.QueryRow(a.ctx, `
		SELECT coalesce((SELECT concat_ws('|', starts_at, ends_at, all_day, reason) FROM agenda_blocks WHERE id = $1), 'APAGADO')`,
		c.Bloqueio).Scan(&e.Bloqueio); err != nil {
		t.Fatalf("lendo o bloqueio do cenário: %v", err)
	}
	if err := a.pool.QueryRow(a.ctx, `
		SELECT concat_ws('|', checkin_time, checkout_time, cleaning_start, cleaning_end, business_hours::text,
		                 slot_minutes, buffer_minutes, coalesce(updated_by::text, 'ninguém'))
		  FROM agenda_settings WHERE property_id = $1`, c.Casa.Prop).Scan(&e.Horarios); err != nil {
		t.Fatalf("lendo os horários: %v", err)
	}
	e.ComAMarca = a.qagContar(t, `
		SELECT (SELECT count(*) FROM agenda_events WHERE title = $1 OR notes = $1)
		     + (SELECT count(*) FROM agenda_blocks WHERE reason = $1)`, c.Marca)
	return e
}

// ─────────────────────────── 1. A matriz do seed ────────────────────────────

// A premissa dos testes abaixo, lida do banco: `admin` e `usuario` com as
// quatro ações de `agenda` em `all`; o `corretor` com as quatro em `own`; a
// vitrine sem nenhuma. Gravar os horários pede `settings:editar`, que só o
// `admin` tem. E o painel lê o mesmo em `/auth/me`.
func TestAgendaQAMatrizDoSeed(t *testing.T) {
	a := subirAPI(t)
	esperado := map[string]map[string]string{
		"admin":    {"agenda": "criar,editar,excluir,ver", "settings.editar": "all"},
		"usuario":  {"agenda": "criar,editar,excluir,ver", "settings.editar": ""},
		"corretor": {"agenda": "criar:own,editar:own,excluir:own,ver:own", "settings.editar": ""},
		"vitrine":  {"agenda": "", "settings.editar": ""},
	}
	for perfil, quer := range esperado {
		id := a.qbPerfilDoSeed(t, perfil)
		var acoes, editar string
		if err := a.pool.QueryRow(a.ctx, `
			SELECT coalesce(string_agg(action || CASE WHEN scope = 'all' THEN '' ELSE ':' || scope END, ',' ORDER BY action)
			                FILTER (WHERE resource_code = 'agenda'), ''),
			       coalesce(max(scope) FILTER (WHERE resource_code = 'settings' AND action = 'editar'), '')
			  FROM role_permissions WHERE role_id = $1`, id).Scan(&acoes, &editar); err != nil {
			t.Fatal(err)
		}
		if acoes != quer["agenda"] {
			t.Errorf("perfil %s no seed: agenda = [%s], o contrato pede [%s]", perfil, acoes, quer["agenda"])
		}
		if editar != quer["settings.editar"] {
			t.Errorf("perfil %s no seed: settings:editar = %q, esperado %q", perfil, editar, quer["settings.editar"])
		}
	}
	var suportaOwn bool
	if err := a.pool.QueryRow(a.ctx, `SELECT supports_own FROM resources WHERE code = 'agenda'`).Scan(&suportaOwn); err != nil || !suportaOwn {
		t.Errorf("o recurso agenda tem escopo own no contrato (supports_own = true): %v %v", suportaOwn, err)
	}

	corretor := a.criarUsuario(t, "ag-me-corretor", a.qbPerfilDoSeed(t, "corretor"))
	var me struct {
		Data struct {
			Permissoes []auth.Permissao `json:"permissions"`
		} `json:"data"`
	}
	r := a.chamar(t, http.MethodGet, "/auth/me", corretor.Token, nil)
	exigirStatusQB(t, r, http.StatusOK, "GET /auth/me")
	r.decodificar(t, &me)
	n := 0
	for _, p := range me.Data.Permissoes {
		if p.Resource == "agenda" {
			n++
			if p.Scope != auth.EscopoOwn {
				t.Errorf("/auth/me do corretor traz agenda:%s em %s; o contrato diz own", p.Action, p.Scope)
			}
		}
	}
	if n != 4 {
		t.Errorf("/auth/me do corretor traz %d ações de agenda, esperado 4", n)
	}
}

// ─────────────────────────── 2. Quem não opera a casa ───────────────────────

// A conta de serviço da vitrine (o site) não opera a casa: 403 nas 15
// operações, com o 403 dizendo a permissão do x-rbac — e nada muda.
func TestAgendaQAContaDaVitrineRecebe403NasQuinzeOperacoes(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	vitrine := a.criarUsuario(t, "ag-vitrine", a.qbPerfilDoSeed(t, "vitrine"))
	f := a.qagFaxina(t, admin, vitrine)
	c := a.qagMontarCenario(t, f, admin.Token)
	antes := a.qagLerEstado(t, c)

	for _, op := range qagOperacoesDaAgenda(t) {
		t.Run(op.chave(), func(t *testing.T) {
			e := qbErro(t, a.qagExecutar(t, c, op, vitrine.Token), http.StatusForbidden, "FORBIDDEN", "vitrine em "+op.chave())
			if e.Details["resource"] != op.Recurso || e.Details["action"] != op.Acao {
				t.Errorf("o 403 diz que faltou %v:%v; o x-rbac do contrato é %s:%s",
					e.Details["resource"], e.Details["action"], op.Recurso, op.Acao)
			}
		})
	}
	if depois := a.qagLerEstado(t, c); depois != antes {
		t.Fatalf("a vitrine recebeu 403 e mesmo assim algo mudou:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// Sem sessão — sem token ou com token que não é JWT —, nenhuma das 15 responde.
func TestAgendaQASemSessaoAsQuinzeOperacoesDao401(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qagFaxina(t, admin)
	c := a.qagMontarCenario(t, f, admin.Token)
	antes := a.qagLerEstado(t, c)

	for _, op := range qagOperacoesDaAgenda(t) {
		t.Run(op.chave(), func(t *testing.T) {
			qbErro(t, a.qagExecutar(t, c, op, ""), http.StatusUnauthorized, "UNAUTHORIZED", "sem token em "+op.chave())
			qbErro(t, a.qagExecutar(t, c, op, "nao.e.um.jwt"), http.StatusUnauthorized, "UNAUTHORIZED", "token inválido em "+op.chave())
		})
	}
	if depois := a.qagLerEstado(t, c); depois != antes {
		t.Fatalf("chamadas sem sessão mudaram o estado:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// ─────────────────────────── 3. Quem opera a casa ───────────────────────────

// `admin` e `usuario` operam a agenda em `all`: com o perfil REAL do seed,
// cada um percorre as 15 operações e recebe o sucesso que o contrato promete —
// o 201 com Location, o 200 (e não 204) do DELETE do evento, o 204 do DELETE do
// bloqueio. A exceção é a do contrato: gravar os horários pede
// `settings:editar`, e o `usuario` ("operação sem configurações") recebe 403
// apontando `settings:editar` — e os horários não mudam.
func TestAgendaQAAdminEUsuarioDoSeedPercorremAsQuinzeOperacoes(t *testing.T) {
	for _, perfil := range []string{"admin", "usuario"} {
		t.Run(perfil, func(t *testing.T) {
			a := subirAPI(t)
			u := a.criarUsuario(t, "ag-"+perfil, a.qbPerfilDoSeed(t, perfil))
			f := a.qagFaxina(t, u)
			casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
			unidade, _ := f.unidade(t, casa.Prop)
			hoje := casa.Hoje
			tk := u.Token

			cobertas := map[string]bool{}
			passo := func(chave string, r resposta, esperado int) resposta {
				t.Helper()
				cobertas[chave] = true
				if r.Status != esperado {
					t.Fatalf("%s: status %d, o contrato promete %d — %s", chave, r.Status, esperado, r.Corpo)
				}
				return r
			}

			criada := passo("POST /agenda/events", a.chamar(t, http.MethodPost, "/agenda/events", tk, map[string]any{
				"type": "visita", "title": "Visita do " + perfil, "unit_id": unidade,
				"starts_at": casa.rfc(hoje, "09:00"), "ends_at": casa.rfc(hoje, "09:30"),
			}), http.StatusCreated)
			ev := qagEscritaDe(t, criada).Data
			if loc := criada.Headers.Get("Location"); loc != PrefixoDaAPI+"/agenda/events/"+ev.ID.String() {
				t.Errorf("Location do 201: %q", loc)
			}
			base := "/agenda/events/" + ev.ID.String()
			passo("GET /agenda/events", a.chamar(t, http.MethodGet, "/agenda/events?from="+hoje.String()+"&to="+hoje.AddDays(1).String(), tk, nil), http.StatusOK)
			passo("GET /agenda/events/{id}", a.chamar(t, http.MethodGet, base, tk, nil), http.StatusOK)
			passo("PUT /agenda/events/{id}", a.chamar(t, http.MethodPut, base, tk, map[string]any{
				"type": "visita", "title": "Visita remarcada", "unit_id": unidade,
				"starts_at": casa.rfc(hoje, "09:30"), "ends_at": casa.rfc(hoje, "10:00"),
			}), http.StatusOK)
			passo("PATCH /agenda/events/{id}", a.chamar(t, http.MethodPatch, base, tk, map[string]any{"notes": "cliente chega de táxi"}), http.StatusOK)
			concluida := passo("POST /agenda/events/{id}/complete", a.chamar(t, http.MethodPost, base+"/complete", tk, nil), http.StatusOK)
			if e := qbDado[qagEvento](t, concluida, http.StatusOK, "complete"); e.Status != "concluido" || e.ConcluidoPor == nil || *e.ConcluidoPor != u.ID {
				t.Errorf("concluída por %s: %+v", perfil, e)
			}
			outra := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Reunião", "starts_at": casa.rfc(hoje.AddDays(2), "10:00")}).Data
			desmarcada := passo("DELETE /agenda/events/{id}", a.chamar(t, http.MethodDelete, "/agenda/events/"+outra.ID.String(), tk, nil), http.StatusOK)
			if e := qbDado[qagEvento](t, desmarcada, http.StatusOK, "DELETE"); e.Status != "cancelado" {
				t.Errorf("o DELETE devolve o evento como ficou (cancelado): %+v", e)
			}

			bl := passo("POST /agenda/blocks", a.chamar(t, http.MethodPost, "/agenda/blocks", tk, map[string]any{
				"starts_at": casa.rfc(hoje.AddDays(5), "14:00"), "ends_at": casa.rfc(hoje.AddDays(5), "16:00"), "reason": "Dedetização",
			}), http.StatusCreated)
			b := qbDado[qagBloqueio](t, bl, http.StatusCreated, "POST /agenda/blocks")
			if loc := bl.Headers.Get("Location"); loc != PrefixoDaAPI+"/agenda/blocks/"+b.ID.String() {
				t.Errorf("Location do 201 do bloqueio: %q", loc)
			}
			bb := "/agenda/blocks/" + b.ID.String()
			passo("GET /agenda/blocks", a.chamar(t, http.MethodGet, "/agenda/blocks?from="+hoje.String()+"&to="+hoje.AddDays(10).String(), tk, nil), http.StatusOK)
			passo("GET /agenda/blocks/{id}", a.chamar(t, http.MethodGet, bb, tk, nil), http.StatusOK)
			passo("PUT /agenda/blocks/{id}", a.chamar(t, http.MethodPut, bb, tk, map[string]any{
				"starts_at": casa.rfc(hoje.AddDays(5), "13:00"), "ends_at": casa.rfc(hoje.AddDays(5), "16:00"), "reason": "Dedetização geral",
			}), http.StatusOK)
			passo("PATCH /agenda/blocks/{id}", a.chamar(t, http.MethodPatch, bb, tk, map[string]any{"reason": "Dedetização da área comum"}), http.StatusOK)
			apagado := passo("DELETE /agenda/blocks/{id}", a.chamar(t, http.MethodDelete, bb, tk, nil), http.StatusNoContent)
			if len(apagado.Corpo) != 0 {
				t.Errorf("204 com corpo: %s", apagado.Corpo)
			}
			qbErro(t, a.chamar(t, http.MethodGet, bb, tk, nil), http.StatusNotFound, "NOT_FOUND", "bloqueio apagado")

			passo("GET /agenda/settings", a.chamar(t, http.MethodGet, "/agenda/settings", tk, nil), http.StatusOK)
			antes := a.qagHorariosDaCasa(t, tk)
			corpo := qagCorpoDosHorarios("14:00", "11:00", "11:00", "14:00", 30, 15)
			if perfil == "admin" {
				passo("PUT /agenda/settings", a.chamar(t, http.MethodPut, "/agenda/settings", tk, corpo), http.StatusOK)
			} else {
				cobertas["PUT /agenda/settings"] = true
				e := qbErro(t, a.chamar(t, http.MethodPut, "/agenda/settings", tk, corpo), http.StatusForbidden, "FORBIDDEN",
					"usuario gravando os horários")
				if e.Details["resource"] != "settings" || e.Details["action"] != "editar" {
					t.Errorf("o 403 do usuario aponta %v:%v; o contrato exige settings:editar", e.Details["resource"], e.Details["action"])
				}
				if depois := a.qagHorariosDaCasa(t, tk); depois.AtualizadaPor != nil || depois.Provisoria != antes.Provisoria {
					t.Errorf("o usuario recebeu 403 e os horários mudaram: %+v", depois)
				}
			}

			for _, op := range qagOperacoesDaAgenda(t) {
				if !cobertas[op.chave()] {
					t.Errorf("%s não foi exercida pela jornada de %s", op.chave(), perfil)
				}
			}
		})
	}
}

// ─────────────────────────── 4. O corretor em `own` ─────────────────────────

// Em linguagem de negócio: o corretor marca as PRÓPRIAS visitas, e só. Com o
// perfil REAL do seed (as quatro ações de agenda em `own`):
//
//   - a visita marcada sem responsável é dele; marcar para outra pessoa — um
//     colega, o admin ou ninguém (`null`) — é 403 `assignee_not_actor`, e nada
//     nasce;
//   - ele vê só o que é dele, na lista e no detalhe; a visita do colega é 404,
//     nunca 403 — na leitura, na edição, no concluir e no desmarcar;
//   - o que não tem responsável (o evento da casa, o automático da reserva, a
//     manutenção lida de stay_blocks) não aparece;
//   - ele desmarca, conclui e edita o que é dele, mas não o entrega a outro.
func TestAgendaQACorretorDoSeedSoAlcancaAsProprias(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	ana := a.criarUsuario(t, "ag-ana", a.qbPerfilDoSeed(t, "corretor"))
	beto := a.criarUsuario(t, "ag-beto", a.qbPerfilDoSeed(t, "corretor"))
	f := a.qagFaxina(t, admin, ana, beto)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	hoje := casa.Hoje
	d := hoje.AddDays(20)
	unidade, _ := f.unidade(t, casa.Prop)

	// Ausente vira o próprio ator.
	visita := a.qagMarcar(t, ana.Token, map[string]any{"type": "visita", "title": "Visita da Ana", "unit_id": unidade,
		"starts_at": casa.rfc(d, "09:00"), "ends_at": casa.rfc(d, "10:00")}).Data
	if visita.ResponsavelID == nil || *visita.ResponsavelID != ana.ID {
		t.Fatalf("em own, o responsável ausente vira o ator: %v", visita.ResponsavelID)
	}
	// Explícito e igual ao ator passa.
	deHoje := a.qagMarcar(t, ana.Token, map[string]any{"type": "visita", "title": "Visita de hoje da Ana",
		"assignee_id": ana.ID, "starts_at": casa.rfc(hoje, "09:00"), "ends_at": casa.rfc(hoje, "09:30")}).Data

	// Agendar para outra pessoa: 403 assignee_not_actor, e nada nasce.
	for nome, pedido := range map[string]any{"o colega": beto.ID, "o admin": admin.ID, "ninguém (null)": nil} {
		r := a.chamar(t, http.MethodPost, "/agenda/events", ana.Token, map[string]any{"type": "visita",
			"title": "Para outra pessoa", "assignee_id": pedido, "starts_at": casa.rfc(d, "15:00")})
		e := qbErro(t, r, http.StatusForbidden, "FORBIDDEN", "corretor agendando para "+nome)
		if e.Details["reason"] != "assignee_not_actor" || e.Details["field"] != "assignee_id" {
			t.Errorf("agendando para %s: details %v; o contrato pede field assignee_id, reason assignee_not_actor", nome, e.Details)
		}
	}
	if n := a.qagContar(t, `SELECT count(*) FROM agenda_events WHERE title = 'Para outra pessoa'`); n != 0 {
		t.Fatalf("o 403 criou %d eventos", n)
	}

	// O que não é dela: o evento da casa, o do colega, a manutenção.
	daCasa := a.qagMarcar(t, admin.Token, map[string]any{"type": "evento", "title": "Reunião da casa",
		"starts_at": casa.rfc(d, "12:00")}).Data
	doBeto := a.qagMarcar(t, admin.Token, map[string]any{"type": "visita", "title": "Visita do Beto",
		"assignee_id": beto.ID, "starts_at": casa.rfc(d, "16:00")}).Data
	ordem := qbDado[qmOrdem](t, a.chamar(t, http.MethodPost, "/maintenance-orders", admin.Token, map[string]any{
		"unit_id": unidade, "title": "Troca do chuveiro", "block": qmPeriodo(d.AddDays(2), d.AddDays(3)),
	}), http.StatusCreated, "POST /maintenance-orders")
	f.ordem(ordem.ID)

	janela := "/agenda/events?from=" + hoje.String() + "&to=" + d.AddDays(5).String()
	daAna, meta := qbLista[qagEvento](t, a.chamar(t, http.MethodGet, janela, ana.Token, nil), "lista da Ana")
	if meta.Total != 2 || len(daAna) != 2 {
		t.Fatalf("a Ana vê só as duas visitas dela (meta.total %d): %+v", meta.Total, daAna)
	}
	for _, e := range daAna {
		if e.ID != visita.ID && e.ID != deHoje.ID {
			t.Errorf("a Ana viu o que não é dela: %s %q", e.Origem, e.Titulo)
		}
	}
	doBetoNaLista, meta := qbLista[qagEvento](t, a.chamar(t, http.MethodGet, janela, beto.Token, nil), "lista do Beto")
	if meta.Total != 1 || len(doBetoNaLista) != 1 || doBetoNaLista[0].ID != doBeto.ID {
		t.Fatalf("o Beto vê só a visita dele (meta.total %d): %+v", meta.Total, doBetoNaLista)
	}
	// Filtrar pelo responsável alheio não abre a porta.
	if evs, meta := qbLista[qagEvento](t, a.chamar(t, http.MethodGet, janela+"&assignee_id="+ana.ID.String(), beto.Token, nil),
		"Beto filtrando pela Ana"); meta.Total != 0 || len(evs) != 0 {
		t.Fatalf("o Beto filtrou pela Ana e viu %d eventos", meta.Total)
	}

	// A visita da colega é 404 — na leitura e em toda escrita.
	antes := a.qagLer(t, ana.Token, visita.ID)
	caminho := "/agenda/events/" + visita.ID.String()
	for nome, r := range map[string]resposta{
		"GET":      a.chamar(t, http.MethodGet, caminho, beto.Token, nil),
		"PATCH":    a.chamar(t, http.MethodPatch, caminho, beto.Token, map[string]any{"notes": "invasão"}),
		"PATCH {}": a.chamar(t, http.MethodPatch, caminho, beto.Token, map[string]any{}),
		"PUT": a.chamar(t, http.MethodPut, caminho, beto.Token, map[string]any{"type": "visita", "title": "invasão",
			"starts_at": casa.rfc(d, "09:00"), "ends_at": casa.rfc(d, "10:00")}),
		"DELETE":   a.chamar(t, http.MethodDelete, caminho, beto.Token, nil),
		"complete": a.chamar(t, http.MethodPost, "/agenda/events/"+deHoje.ID.String()+"/complete", beto.Token, nil),
	} {
		qbErro(t, r, http.StatusNotFound, "NOT_FOUND", "Beto na visita da Ana ("+nome+")")
	}
	if depois := a.qagLer(t, ana.Token, visita.ID); depois.Notas != antes.Notas || depois.Status != "agendado" || depois.Titulo != antes.Titulo {
		t.Fatalf("o 404 mudou a visita: %+v", depois)
	}
	// O que não tem responsável também é 404 para ela.
	qbErro(t, a.chamar(t, http.MethodGet, "/agenda/events/"+daCasa.ID.String(), ana.Token, nil), http.StatusNotFound, "NOT_FOUND",
		"evento sem responsável visto pela corretora")
	if ordem.Bloqueio != nil {
		qbErro(t, a.chamar(t, http.MethodGet, "/agenda/events/"+ordem.Bloqueio.ID.String(), ana.Token, nil), http.StatusNotFound,
			"NOT_FOUND", "derivado da manutenção visto pela corretora")
	}

	// O que é dela: edita, conclui e desmarca — sem entregar a outro.
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, caminho, ana.Token, map[string]any{"notes": "cliente confirmou"}),
		http.StatusOK, "PATCH próprio")
	for nome, pedido := range map[string]any{"o colega": beto.ID, "ninguém (null)": nil} {
		e := qbErro(t, a.chamar(t, http.MethodPatch, caminho, ana.Token, map[string]any{"assignee_id": pedido}),
			http.StatusForbidden, "FORBIDDEN", "corretora entregando a visita a "+nome)
		if e.Details["reason"] != "assignee_not_actor" {
			t.Errorf("details.reason: %v", e.Details["reason"])
		}
	}
	put := qagEscritaDe(t, a.chamar(t, http.MethodPut, caminho, ana.Token, map[string]any{"type": "visita",
		"title": "Visita da Ana (remarcada)", "unit_id": unidade, "starts_at": casa.rfc(d, "10:00"), "ends_at": casa.rfc(d, "11:00")}))
	if put.Data.ResponsavelID == nil || *put.Data.ResponsavelID != ana.ID {
		t.Errorf("no PUT em own, o responsável omitido é o próprio ator: %v", put.Data.ResponsavelID)
	}
	e := qbDado[qagEvento](t, a.chamar(t, http.MethodPost, "/agenda/events/"+deHoje.ID.String()+"/complete", ana.Token, nil),
		http.StatusOK, "corretora concluindo a visita de hoje")
	if e.Status != "concluido" || e.ConcluidoPor == nil || *e.ConcluidoPor != ana.ID {
		t.Errorf("concluída: %+v", e)
	}
	if e := qbDado[qagEvento](t, a.chamar(t, http.MethodDelete, caminho, ana.Token, nil), http.StatusOK, "corretora desmarcando"); e.Status != "cancelado" {
		t.Errorf("desmarcada: %+v", e)
	}

	// O automático da limpeza entregue a ela pela gestão passa a ser dela —
	// e continua fora do alcance do colega.
	p := f.produto(t, casa.Prop, "one_member", 1)
	hospede := f.contato(t, casa.Prop)
	r := a.qagVendida(t, admin.Token, p.ID, hospede.ID, d.AddDays(10), d.AddDays(12))
	auto := qagPorTipo(t, a.qagDaReserva(t, admin.Token, r.ID))
	if evs := a.qagDaReserva(t, ana.Token, r.ID); len(evs) != 0 {
		t.Fatalf("os automáticos nascem sem responsável e ficam fora do own: a Ana viu %d", len(evs))
	}
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+auto["limpeza"].ID.String(), admin.Token,
		map[string]any{"assignee_id": ana.ID}), http.StatusOK, "gestão entregando a limpeza à Ana")
	if evs := a.qagDaReserva(t, ana.Token, r.ID); len(evs) != 1 || evs[0].ID != auto["limpeza"].ID {
		t.Fatalf("a limpeza entregue à Ana aparece para ela: %+v", evs)
	}
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+auto["limpeza"].ID.String(), ana.Token,
		map[string]any{"notes": "levar o kit de enxoval"}), http.StatusOK, "Ana anotando a limpeza dela")
	qbErro(t, a.chamar(t, http.MethodGet, "/agenda/events/"+auto["limpeza"].ID.String(), beto.Token, nil),
		http.StatusNotFound, "NOT_FOUND", "a limpeza da Ana vista pelo Beto")
}

// O bloqueio de horário é da operação inteira e não tem responsável: o
// corretor LÊ (precisa dele para marcar a visita), mas em `own` toda escrita é
// 403 `own_scope_not_applicable` — e não "um bloqueio só dele". Os horários,
// ele lê; gravar pede `settings:editar`.
func TestAgendaQACorretorLeBloqueiosEHorariosENaoEscreve(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	corretor := a.criarUsuario(t, "ag-corretor", a.qbPerfilDoSeed(t, "corretor"))
	f := a.qagFaxina(t, admin, corretor)
	c := a.qagMontarCenario(t, f, admin.Token)
	antes := a.qagLerEstado(t, c)

	lidos, _ := qbLista[qagBloqueio](t, a.chamar(t, http.MethodGet,
		"/agenda/blocks?from="+c.Casa.Hoje.String()+"&to="+c.Casa.Hoje.AddDays(10).String(), corretor.Token, nil), "corretor listando bloqueios")
	achou := false
	for _, b := range lidos {
		achou = achou || b.ID == c.Bloqueio
	}
	if !achou {
		t.Errorf("o corretor lê os bloqueios de horário em qualquer escopo; o do cenário não veio: %+v", lidos)
	}
	exigirStatusQB(t, a.chamar(t, http.MethodGet, "/agenda/blocks/"+c.Bloqueio.String(), corretor.Token, nil), http.StatusOK,
		"corretor lendo o bloqueio")
	if h := a.qagHorariosDaCasa(t, corretor.Token); h.Fuso == "" || h.Entrada == "" {
		t.Errorf("o corretor lê os horários da casa: %+v", h)
	}

	for _, op := range qagOperacoesDaAgenda(t) {
		escritaDeBloqueio := strings.HasPrefix(op.Path, "/agenda/blocks") && op.Metodo != http.MethodGet
		if !escritaDeBloqueio && op.chave() != "PUT /agenda/settings" {
			continue
		}
		t.Run(op.chave(), func(t *testing.T) {
			e := qbErro(t, a.qagExecutar(t, c, op, corretor.Token), http.StatusForbidden, "FORBIDDEN", "corretor em "+op.chave())
			if escritaDeBloqueio && e.Details["reason"] != "own_scope_not_applicable" {
				t.Errorf("a escrita de bloqueio em own é 403 own_scope_not_applicable; details: %v", e.Details)
			}
			if op.chave() == "PUT /agenda/settings" && (e.Details["resource"] != "settings" || e.Details["action"] != "editar") {
				t.Errorf("gravar os horários pede settings:editar; o 403 aponta %v:%v", e.Details["resource"], e.Details["action"])
			}
		})
	}
	if depois := a.qagLerEstado(t, c); depois != antes {
		t.Fatalf("o corretor recebeu 403 e algo mudou:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// Em linguagem de negócio: a casa quer um perfil "só acompanha a agenda" (o
// recepcionista que olha o dia no celular). Com só `agenda:ver`, ele lê as
// cinco rotas de leitura e recebe 403 nas dez escritas — cada 403 apontando a
// ação que o x-rbac do contrato exige (concluir é `editar`, desmarcar é
// `excluir`, gravar os horários é `settings:editar`) —, e nada muda.
func TestAgendaQAPerfilSoDeVerLeENaoEscreve(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	leitor := a.criarUsuario(t, "ag-leitor", a.criarPerfil(t, "agenda_so_ver", []auth.Permissao{
		{Resource: "agenda", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	}))
	f := a.qagFaxina(t, admin, leitor)
	c := a.qagMontarCenario(t, f, admin.Token)
	antes := a.qagLerEstado(t, c)

	leituras, escritas := 0, 0
	for _, op := range qagOperacoesDaAgenda(t) {
		t.Run(op.chave(), func(t *testing.T) {
			r := a.qagExecutar(t, c, op, leitor.Token)
			if op.Acao == auth.AcaoVer {
				leituras++
				if r.Status != http.StatusOK {
					t.Fatalf("leitura negada a quem tem agenda:ver: %d — %s", r.Status, r.Corpo)
				}
				return
			}
			escritas++
			e := qbErro(t, r, http.StatusForbidden, "FORBIDDEN", "escrita de quem só vê")
			if e.Details["resource"] != op.Recurso || e.Details["action"] != op.Acao {
				t.Errorf("o 403 aponta %v:%v; o x-rbac do contrato exige %s:%s", e.Details["resource"], e.Details["action"], op.Recurso, op.Acao)
			}
		})
	}
	if leituras != 5 || escritas != 10 {
		t.Errorf("o contrato tem 5 leituras e 10 escritas na tag Agenda; contei %d e %d", leituras, escritas)
	}
	if depois := a.qagLerEstado(t, c); depois != antes {
		t.Fatalf("quem só vê recebeu 403 e algo mudou:\n antes  %+v\n depois %+v", antes, depois)
	}
}
