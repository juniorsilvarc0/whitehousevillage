//go:build integration

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Formato e isolamento: o envelope da lista e a paginação, o corpo fora do
// contrato, o filtro inválido, a outra propriedade e o dado pessoal.

// Em linguagem de negócio: a visão de lista do painel pede a agenda página a
// página. Nenhuma linha some e nenhuma aparece duas vezes — nem com a
// manutenção (derivada) no meio, nem com eventos no mesmo instante —, a ordem
// é por início (e, no empate, sempre a mesma), e `meta` diz a verdade.
func TestAgendaQAListaPaginaSemRepetirNemPerder(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	responsavel := a.criarUsuario(t, "ag-responsavel", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u, responsavel)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	tk := u.Token
	unidade, _ := f.unidade(t, casa.Prop)
	d := casa.Hoje.AddDays(200)

	for i, hora := range []string{"13:00", "09:00", "11:00", "10:00", "12:00"} {
		a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": fmt.Sprintf("Visita %d", i), "unit_id": unidade,
			"starts_at": casa.rfc(d, hora), "ends_at": casa.rfc(d, qagSomar(hora, 30))})
	}
	ordem := qbDado[qmOrdem](t, a.chamar(t, http.MethodPost, "/maintenance-orders", tk, map[string]any{
		"unit_id": unidade, "title": "Pintura", "block": qmPeriodo(d.AddDays(1), d.AddDays(2)),
	}), http.StatusCreated, "manutenção no meio da janela")
	f.ordem(ordem.ID)

	base := fmt.Sprintf("/agenda/events?from=%s&to=%s&unit_id=%s", d, d.AddDays(3), unidade)
	inteira, meta := qbLista[qagEvento](t, a.chamar(t, http.MethodGet, base+"&per_page=100", tk, nil), "lista inteira")
	if len(inteira) != 6 || meta.Total != 6 {
		t.Fatalf("cinco visitas e uma manutenção: %d (meta.total %d)", len(inteira), meta.Total)
	}
	for i := 1; i < len(inteira); i++ {
		if inteira[i].Inicio < inteira[i-1].Inicio {
			t.Errorf("a lista vem por início: %s depois de %s", inteira[i].Inicio, inteira[i-1].Inicio)
		}
	}
	qagPaginar(t, a, tk, base, 4, inteira)

	desc, _ := qbLista[qagEvento](t, a.chamar(t, http.MethodGet, base+"&per_page=100&sort=-starts_at", tk, nil), "sort=-starts_at")
	for i := range desc {
		if desc[i].ID != inteira[len(inteira)-1-i].ID {
			t.Fatalf("sort=-starts_at é a ordem inversa: posição %d", i)
		}
	}

	// Empate: cinco eventos no MESMO instante — o desempate é sempre o mesmo.
	dia := d.AddDays(5)
	for i := 0; i < 5; i++ {
		a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": fmt.Sprintf("Empate %d", i), "assignee_id": responsavel.ID,
			"starts_at": casa.rfc(dia, "10:00"), "ends_at": casa.rfc(dia, "11:00")})
	}
	base = fmt.Sprintf("/agenda/events?from=%s&to=%s&assignee_id=%s", dia, dia.AddDays(1), responsavel.ID)
	empate, _ := qbLista[qagEvento](t, a.chamar(t, http.MethodGet, base+"&per_page=100", tk, nil), "empate")
	if len(empate) != 5 {
		t.Fatalf("cinco no mesmo instante: %d", len(empate))
	}
	qagPaginar(t, a, tk, base, 2, empate)
	descEmpate, _ := qbLista[qagEvento](t, a.chamar(t, http.MethodGet, base+"&per_page=100&sort=-starts_at", tk, nil), "empate desc")
	for i := range descEmpate {
		if descEmpate[i].ID != empate[len(empate)-1-i].ID {
			t.Errorf("no empate, a ordem inversa também é estável: posição %d", i)
		}
	}

	// per_page acima do teto do contrato (100) não devolve mais que 100.
	_, meta = qbLista[qagEvento](t, a.chamar(t, http.MethodGet, base+"&per_page=1000", tk, nil), "per_page=1000")
	if meta.PerPage > 100 {
		t.Errorf("per_page vai até 100; meta.per_page = %d", meta.PerPage)
	}
}

// qagPaginar percorre as páginas e confere que a concatenação é a lista
// inteira, na mesma ordem, sem repetir nem perder, com o `meta` coerente.
func qagPaginar(t *testing.T, a *ambiente, tk, base string, porPagina int, inteira []qagEvento) {
	t.Helper()
	var juntas []qagEvento
	vistos := map[uuid.UUID]bool{}
	paginas := (len(inteira) + porPagina - 1) / porPagina
	for p := 1; p <= paginas+1; p++ {
		r := a.chamar(t, http.MethodGet, fmt.Sprintf("%s&per_page=%d&page=%d", base, porPagina, p), tk, nil)
		evs, meta := qbLista[qagEvento](t, r, fmt.Sprintf("página %d", p))
		var env map[string]json.RawMessage
		if err := json.Unmarshal(r.Corpo, &env); err != nil || env["data"] == nil || env["meta"] == nil || len(env) != 2 {
			t.Fatalf("a lista é {data, meta}: %s", r.Corpo)
		}
		if meta.Page != p || meta.PerPage != porPagina || meta.Total != int64(len(inteira)) || meta.TotalPages != paginas {
			t.Errorf("página %d: meta %+v; esperado page %d, per_page %d, total %d, total_pages %d", p, meta, p, porPagina, len(inteira), paginas)
		}
		if p > paginas && len(evs) != 0 {
			t.Errorf("a página além da última vem vazia; vieram %d", len(evs))
		}
		for _, e := range evs {
			if vistos[e.ID] {
				t.Errorf("%s apareceu em duas páginas", e.ID)
			}
			vistos[e.ID] = true
		}
		juntas = append(juntas, evs...)
	}
	if len(juntas) != len(inteira) {
		t.Fatalf("as páginas somam %d linhas; a lista inteira tem %d", len(juntas), len(inteira))
	}
	for i := range juntas {
		if juntas[i].ID != inteira[i].ID {
			t.Fatalf("a paginação muda a ordem na posição %d", i)
		}
	}
}

// Em linguagem de negócio: o cliente com um campo errado (a chave com outra
// caixa, um campo que não existe, inclusive dentro do horário de
// funcionamento) recebe 422 dizendo qual campo — e nada é gravado. O mesmo
// para o filtro da lista: filtro inválido nunca vira a agenda inteira
// fingindo que filtrou.
func TestAgendaQACorpoEFiltroForaDoContratoDao422(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qagFaxina(t, admin)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	tk := admin.Token
	d := casa.Hoje.AddDays(210)
	marca := "Fora do contrato " + qbSufixo()
	unidade, _ := f.unidade(t, casa.Prop)
	ev := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "Visita do formato", "unit_id": unidade,
		"starts_at": casa.rfc(d, "10:00"), "ends_at": casa.rfc(d, "11:00")}).Data
	bl := qbDado[qagBloqueio](t, a.chamar(t, http.MethodPost, "/agenda/blocks", tk, map[string]any{"reason": "Treinamento",
		"starts_at": casa.rfc(d, "14:00"), "ends_at": casa.rfc(d, "15:00")}), http.StatusCreated, "bloqueio")
	horarios := a.qagHorariosDaCasa(t, tk)

	evento := fmt.Sprintf(`"type":"evento","starts_at":%q`, casa.rfc(d, "16:00"))
	janela := fmt.Sprintf(`"starts_at":%q,"ends_at":%q`, casa.rfc(d, "16:00"), casa.rfc(d, "17:00"))
	dias := `{"weekday":1,"start":"08:00","end":"20:00"}`
	cfg := func(intervalo string) string {
		return `{"checkin_time":"14:00","checkout_time":"11:00","cleaning_start":"11:00","cleaning_end":"14:00",` +
			`"slot_minutes":30,"buffer_minutes":15,"business_hours":[` + intervalo + `]}`
	}
	casos := []struct {
		nome, metodo, caminho, corpo, campo string
	}{
		{"campo desconhecido no POST", "POST", "/agenda/events", `{` + evento + `,"title":"` + marca + `","sala":1}`, "sala"},
		{"chave com outra caixa no POST", "POST", "/agenda/events", `{` + evento + `,"Title":"` + marca + `"}`, "Title"},
		{"a mesma chave em duas caixas no POST", "POST", "/agenda/events", `{` + evento + `,"title":"x","Title":"` + marca + `"}`, "Title"},
		{"campo desconhecido no PUT", "PUT", "/agenda/events/" + ev.ID.String(), `{"type":"visita","title":"` + marca + `",` + janela + `,"color":"red"}`, "color"},
		{"chave com outra caixa no PATCH", "PATCH", "/agenda/events/" + ev.ID.String(), `{"Notes":"` + marca + `"}`, "Notes"},
		{"status no PATCH (estado só muda por ação)", "PATCH", "/agenda/events/" + ev.ID.String(), `{"status":"concluido"}`, "status"},
		{"chave com outra caixa no POST do bloqueio", "POST", "/agenda/blocks", `{` + janela + `,"Reason":"` + marca + `"}`, "Reason"},
		{"campo desconhecido no PUT do bloqueio", "PUT", "/agenda/blocks/" + bl.ID.String(), `{` + janela + `,"reason":"` + marca + `","unit_id":null}`, "unit_id"},
		{"null no PATCH do bloqueio", "PATCH", "/agenda/blocks/" + bl.ID.String(), `{"reason":null}`, "reason"},
		{"chave com outra caixa aninhada nos horários", "PUT", "/agenda/settings", cfg(`{"Weekday":1,"start":"08:00","end":"20:00"}`), "business_hours[0].Weekday"},
		{"campo desconhecido aninhado nos horários", "PUT", "/agenda/settings", cfg(`{"weekday":1,"start":"08:00","end":"20:00","closed":false}`), "business_hours[0].closed"},
		{"chave com outra caixa no topo dos horários", "PUT", "/agenda/settings", strings.Replace(cfg(dias), `"checkin_time"`, `"Checkin_Time"`, 1), "Checkin_Time"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			e := qbErro(t, a.qbChamarCru(t, c.metodo, c.caminho, tk, c.corpo), http.StatusUnprocessableEntity, "VALIDATION_ERROR", c.nome)
			// O campo desconhecido aninhado sai do decoder sem o caminho
			// ("closed", e não "business_hours[0].closed"): o contrato só
			// promete o 422, então o nome curto também vale.
			curto := c.campo[strings.LastIndex(c.campo, ".")+1:]
			if e.Details[c.campo] == nil && e.Details[curto] == nil {
				t.Errorf("o 422 não nomeia %q: %v", c.campo, e.Details)
			}
		})
	}
	for _, corpo := range []string{"", "nao é json", "[]", "null"} {
		qbErro(t, a.qbChamarCru(t, http.MethodPost, "/agenda/events", tk, corpo), http.StatusUnprocessableEntity, "VALIDATION_ERROR",
			fmt.Sprintf("POST com corpo %q", corpo))
	}
	if n := a.qagContar(t, `SELECT (SELECT count(*) FROM agenda_events WHERE title = $1 OR notes = $1)
	                             + (SELECT count(*) FROM agenda_blocks WHERE reason = $1)`, marca); n != 0 {
		t.Fatalf("um 422 gravou %d linhas", n)
	}
	if x := a.qagLer(t, tk, ev.ID); x.Status != "agendado" || x.Titulo != "Visita do formato" {
		t.Fatalf("um 422 mudou o evento: %+v", x)
	}
	if h := a.qagHorariosDaCasa(t, tk); h.AtualizadaPor != nil || h.Provisoria != horarios.Provisoria {
		t.Fatalf("um 422 gravou os horários: %+v", h)
	}

	// Filtros.
	de, ate := d.String(), d.AddDays(1).String()
	filtros := []struct{ query, campo string }{
		{"from=" + de, "to"},
		{"to=" + ate, "from"},
		{"", "from"},
		{"from=" + de + "&to=" + de, "to"},
		{"from=" + ate + "&to=" + de, "to"},
		{"from=" + de + "&to=" + d.AddDays(93).String(), "to"},
		{"from=2026-02-30&to=" + ate, "from"},
		{"from=" + de + "&to=" + ate + "&type=reuniao", "type"},
		{"from=" + de + "&to=" + ate + "&type=,", "type"},
		{"from=" + de + "&to=" + ate + "&status=feito", "status"},
		{"from=" + de + "&to=" + ate + "&unassigned=sim", "unassigned"},
		{"from=" + de + "&to=" + ate + "&unassigned=true&assignee_id=" + admin.ID.String(), "unassigned"},
		{"from=" + de + "&to=" + ate + "&sort=title", "sort"},
		{"from=" + de + "&to=" + ate + "&unit_id=AP-01", "unit_id"},
		{"from=" + de + "&to=" + ate + "&assignee_id=eu", "assignee_id"},
		{"reservation_id=WH-2026-0001", "reservation_id"},
	}
	for _, c := range filtros {
		e := qbErro(t, a.chamar(t, http.MethodGet, "/agenda/events?"+c.query, tk, nil), http.StatusUnprocessableEntity, "VALIDATION_ERROR",
			"GET /agenda/events?"+c.query)
		if e.Details[c.campo] == nil {
			t.Errorf("GET /agenda/events?%s: o 422 não nomeia %s: %v", c.query, c.campo, e.Details)
		}
	}
	exigirStatusQB(t, a.chamar(t, http.MethodGet, "/agenda/events?from="+de+"&to="+d.AddDays(92).String(), tk, nil), http.StatusOK,
		"a janela de exatamente 92 dias é aceita")
	for _, q := range []string{"", "from=" + de, "from=" + de + "&to=" + d.AddDays(93).String(), "from=" + ate + "&to=" + de} {
		qbErro(t, a.chamar(t, http.MethodGet, "/agenda/blocks?"+q, tk, nil), http.StatusUnprocessableEntity, "VALIDATION_ERROR", "GET /agenda/blocks?"+q)
	}
}

// O contrato diz "`unassigned` que não seja `true` nem `false`" → 422 — e o
// schema do parâmetro é `boolean`. `1`, `t` e `TRUE` não são nem um nem
// outro: o painel que mandar `unassigned=1` acha que filtrou de um jeito que o
// contrato não promete.
func TestAgendaQAUnassignedSoAceitaTrueOuFalse(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	a.qagFaxina(t, admin)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	q := "/agenda/events?from=" + casa.Hoje.String() + "&to=" + casa.Hoje.AddDays(1).String()
	for _, v := range []string{"true", "false"} {
		exigirStatusQB(t, a.chamar(t, http.MethodGet, q+"&unassigned="+v, admin.Token, nil), http.StatusOK, "unassigned="+v)
	}
	for _, v := range []string{"1", "0", "t", "TRUE", "False"} {
		r := a.chamar(t, http.MethodGet, q+"&unassigned="+v, admin.Token, nil)
		if r.Status != http.StatusUnprocessableEntity {
			t.Errorf("unassigned=%s: status %d; o contrato só aceita true ou false (422 para o resto)", v, r.Status)
		}
	}
}

// Em linguagem de negócio: a agenda de uma casa não vaza para a outra. Quem
// opera a White House não vê, não edita, não conclui nem desmarca o evento da
// outra casa (404, como se não existisse), não enxerga os bloqueios de horário
// dela e não consegue citar a unidade, o cliente ou o funcionário dela num
// evento seu. E a outra casa tem os horários dela, provisórios até alguém de
// lá gravar.
func TestAgendaQAOutraCasaNaoVaza(t *testing.T) {
	a := subirAPI(t)
	casaB := a.qagOutraCasa(t)
	adminA := a.criarUsuario(t, "ag-admin-a", a.qbPerfilDoSeed(t, "admin"))
	adminB := a.qbUsuarioEm(t, "ag-admin-b", a.qbPerfilDoSeed(t, "admin"), casaB)
	f := a.qagFaxina(t, adminA, adminB)
	casaA := a.qagCasa(t, a.qbPropriedadePadrao(t))
	b := a.qagCasa(t, casaB)
	d := casaA.Hoje.AddDays(220)

	unidadeB, _ := f.unidade(t, casaB)
	contatoB := f.contato(t, casaB)
	evB := a.qagMarcar(t, adminB.Token, map[string]any{"type": "visita", "title": "Visita da casa B", "unit_id": unidadeB,
		"contact_id": contatoB.ID, "assignee_id": adminB.ID, "starts_at": b.rfc(d, "10:00"), "ends_at": b.rfc(d, "11:00")}).Data
	blB := qbDado[qagBloqueio](t, a.chamar(t, http.MethodPost, "/agenda/blocks", adminB.Token, map[string]any{"reason": "Treinamento da casa B",
		"starts_at": b.rfc(d, "14:00"), "ends_at": b.rfc(d, "16:00")}), http.StatusCreated, "bloqueio da casa B")
	hB := a.qagHorariosDaCasa(t, adminB.Token)
	if !hB.Provisoria || hB.Entrada != "14:00" {
		t.Errorf("a casa criada depois da migration nasce com os horários provisórios na primeira leitura: %+v", hB)
	}

	tkA := adminA.Token
	unidadeA, _ := f.unidade(t, casaA.Prop)
	evA := a.qagMarcar(t, tkA, map[string]any{"type": "visita", "title": "Visita da casa A", "unit_id": unidadeA,
		"starts_at": casaA.rfc(d, "10:00"), "ends_at": casaA.rfc(d, "11:00")}).Data

	for _, e := range a.qagJanela(t, tkA, d, d.AddDays(1), "") {
		if e.ID == evB.ID {
			t.Fatalf("a lista da casa A trouxe o evento da casa B")
		}
	}
	if evs := a.qagJanela(t, tkA, d, d.AddDays(1), "unit_id="+unidadeB.String()); len(evs) != 0 {
		t.Errorf("filtrar pela unidade da outra casa não abre a agenda dela: %d eventos", len(evs))
	}
	bs, _ := qbLista[qagBloqueio](t, a.chamar(t, http.MethodGet, "/agenda/blocks?per_page=100&from="+d.String()+"&to="+d.AddDays(1).String(), tkA, nil), "bloqueios da casa A")
	for _, x := range bs {
		if x.ID == blB.ID {
			t.Fatalf("a casa A viu o bloqueio de horário da casa B")
		}
	}
	caminho := "/agenda/events/" + evB.ID.String()
	for nome, r := range map[string]resposta{
		"GET":      a.chamar(t, http.MethodGet, caminho, tkA, nil),
		"PATCH":    a.chamar(t, http.MethodPatch, caminho, tkA, map[string]any{"notes": "invasão"}),
		"PATCH {}": a.chamar(t, http.MethodPatch, caminho, tkA, map[string]any{}),
		"PUT": a.chamar(t, http.MethodPut, caminho, tkA, map[string]any{"type": "visita", "title": "invasão",
			"starts_at": b.rfc(d, "10:00"), "ends_at": b.rfc(d, "11:00")}),
		"complete":        a.chamar(t, http.MethodPost, caminho+"/complete", tkA, nil),
		"DELETE":          a.chamar(t, http.MethodDelete, caminho, tkA, nil),
		"GET bloqueio":    a.chamar(t, http.MethodGet, "/agenda/blocks/"+blB.ID.String(), tkA, nil),
		"PATCH bloqueio":  a.chamar(t, http.MethodPatch, "/agenda/blocks/"+blB.ID.String(), tkA, map[string]any{"reason": "invasão"}),
		"PUT bloqueio":    a.chamar(t, http.MethodPut, "/agenda/blocks/"+blB.ID.String(), tkA, map[string]any{"reason": "invasão", "starts_at": b.rfc(d, "14:00"), "ends_at": b.rfc(d, "16:00")}),
		"DELETE bloqueio": a.chamar(t, http.MethodDelete, "/agenda/blocks/"+blB.ID.String(), tkA, nil),
	} {
		qbErro(t, r, http.StatusNotFound, "NOT_FOUND", "casa A em "+nome+" da casa B")
	}
	if x := a.qagLer(t, adminB.Token, evB.ID); x.Status != "agendado" || x.Notas != nil || x.Titulo != "Visita da casa B" {
		t.Fatalf("a casa A mexeu no evento da casa B: %+v", x)
	}
	if x := qbDado[qagBloqueio](t, a.chamar(t, http.MethodGet, "/agenda/blocks/"+blB.ID.String(), adminB.Token, nil), http.StatusOK, "bloqueio B"); x.Motivo != "Treinamento da casa B" {
		t.Fatalf("a casa A mexeu no bloqueio da casa B: %+v", x)
	}

	for campo, valor := range map[string]any{"unit_id": unidadeB, "contact_id": contatoB.ID, "assignee_id": adminB.ID} {
		e := qbErro(t, a.chamar(t, http.MethodPost, "/agenda/events", tkA, map[string]any{"type": "visita", "title": "Citando a outra casa",
			campo: valor, "starts_at": casaA.rfc(d, "15:00")}), http.StatusUnprocessableEntity, "VALIDATION_ERROR", "citando "+campo+" da casa B")
		if e.Details[campo] == nil {
			t.Errorf("o 422 não nomeia %s: %v", campo, e.Details)
		}
	}
	// E a casa B não vê o da casa A.
	for _, e := range a.qagJanela(t, adminB.Token, d, d.AddDays(1), "") {
		if e.ID == evA.ID {
			t.Fatalf("a lista da casa B trouxe o evento da casa A")
		}
	}
	qbErro(t, a.chamar(t, http.MethodGet, "/agenda/events/"+evA.ID.String(), adminB.Token, nil), http.StatusNotFound, "NOT_FOUND", "casa B no evento da A")

	// Gravar os horários da casa A não toca nos da casa B.
	exigirStatusQB(t, a.chamar(t, http.MethodPut, "/agenda/settings", tkA, qagCorpoDosHorarios("15:00", "10:00", "10:00", "15:00", 30, 15)),
		http.StatusOK, "casa A gravando os horários")
	if h := a.qagHorariosDaCasa(t, adminB.Token); !h.Provisoria || h.Entrada != "14:00" {
		t.Errorf("os horários da casa B mudaram com a gravação da casa A: %+v", h)
	}
}

// Em linguagem de negócio: a agenda é tela de meio time. Ela mostra QUEM é o
// cliente da visita e o titular da reserva (id e nome), e nunca o documento, o
// telefone ou o e-mail deles — nem o telefone ou o e-mail do funcionário
// responsável. Por isso ler a agenda não deixa rastro em `pii_access_log`. E
// nenhuma chave da resposta fica fora do schema do contrato.
func TestAgendaQADadoPessoalNaoSaiDaAgenda(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	responsavel := a.criarUsuario(t, "ag-responsavel", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u, responsavel)
	telefoneDoResponsavel := a.qbComTelefone(t, responsavel)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	tk := u.Token
	p := f.produto(t, casa.Prop, "one_member", 1)
	cliente := f.contato(t, casa.Prop)
	titular := f.contato(t, casa.Prop)
	d := casa.Hoje.AddDays(230)
	logAntes := a.qagContar(t, `SELECT count(*) FROM pii_access_log WHERE contact_id = ANY($1::uuid[])`, []uuid.UUID{cliente.ID, titular.ID})

	r := a.qagVendida(t, tk, p.ID, titular.ID, d, d.AddDays(2))
	var corpos []resposta
	criada := a.chamar(t, http.MethodPost, "/agenda/events", tk, map[string]any{"type": "visita", "title": "Visita com cliente",
		"contact_id": cliente.ID, "assignee_id": responsavel.ID, "starts_at": casa.rfc(d.AddDays(5), "10:00"), "ends_at": casa.rfc(d.AddDays(5), "11:00")})
	exigirStatusQB(t, criada, http.StatusCreated, "POST")
	ev := qagEscritaDe(t, criada).Data
	caminho := "/agenda/events/" + ev.ID.String()
	corpos = append(corpos, criada,
		a.chamar(t, http.MethodGet, caminho, tk, nil),
		a.chamar(t, http.MethodPatch, caminho, tk, map[string]any{"notes": "cliente prefere WhatsApp"}),
		a.chamar(t, http.MethodPut, caminho, tk, map[string]any{"type": "visita", "title": "Visita com cliente",
			"contact_id": cliente.ID, "assignee_id": responsavel.ID, "starts_at": casa.rfc(d.AddDays(5), "10:30"), "ends_at": casa.rfc(d.AddDays(5), "11:30")}),
		a.chamar(t, http.MethodGet, "/agenda/events?per_page=100&from="+d.String()+"&to="+d.AddDays(6).String(), tk, nil),
		a.chamar(t, http.MethodGet, "/agenda/events?per_page=100&status=agendado,concluido,cancelado&reservation_id="+r.ID.String(), tk, nil),
		a.chamar(t, http.MethodDelete, caminho, tk, nil),
	)
	for i, c := range corpos {
		if c.Status != http.StatusOK && c.Status != http.StatusCreated {
			t.Fatalf("resposta %d: %d — %s", i, c.Status, c.Corpo)
		}
		texto := string(c.Corpo)
		for nome, segredo := range map[string]string{
			"e-mail do cliente": cliente.Email, "telefone do cliente": cliente.Telefone, "documento do cliente": cliente.Documento,
			"e-mail do titular": titular.Email, "telefone do titular": titular.Telefone, "documento do titular": titular.Documento,
			"e-mail do responsável": responsavel.Email, "telefone do responsável": telefoneDoResponsavel,
		} {
			if strings.Contains(texto, segredo) {
				t.Errorf("resposta %d vazou o %s: %s", i, nome, texto)
			}
		}
		if fora := qbChavesForaDoContrato(t, "EventoDaAgenda", c.Corpo); len(fora) > 0 {
			t.Errorf("resposta %d com chaves fora do schema EventoDaAgenda: %v", i, fora)
		}
	}
	lido := a.qagLer(t, tk, ev.ID)
	if lido.ContatoID == nil || *lido.ContatoID != cliente.ID || lido.ContatoNome == nil || *lido.ContatoNome != cliente.Nome {
		t.Errorf("o evento traz o id e o nome do cliente: %v %v", lido.ContatoID, lido.ContatoNome)
	}
	if lido.ResponsavelNome == nil || *lido.ResponsavelNome == "" {
		t.Errorf("o evento traz o nome do responsável: %v", lido.ResponsavelNome)
	}
	for _, e := range a.qagDaReserva(t, tk, r.ID) {
		if e.ContatoID == nil || *e.ContatoID != titular.ID || e.ContatoNome == nil || *e.ContatoNome != titular.Nome {
			t.Errorf("o automático traz o id e o nome do titular: %v %v", e.ContatoID, e.ContatoNome)
		}
	}
	if depois := a.qagContar(t, `SELECT count(*) FROM pii_access_log WHERE contact_id = ANY($1::uuid[])`, []uuid.UUID{cliente.ID, titular.ID}); depois != logAntes {
		t.Errorf("ler a agenda gravou %d linhas em pii_access_log; o contrato diz que nenhuma rota da agenda grava", depois-logAntes)
	}
	// A agenda guarda o id, nunca o nome.
	var titulo, notas string
	if err := a.pool.QueryRow(a.ctx, `SELECT coalesce(title, ''), coalesce(notes, '') FROM agenda_events WHERE id = $1`, ev.ID).Scan(&titulo, &notas); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(titulo+notas, cliente.Nome) {
		t.Errorf("o nome do cliente foi copiado para a agenda")
	}
}

// Em linguagem de negócio: o cliente de uma visita (mesmo desmarcada) não se
// apaga — a agenda é histórico de operação. A recusa é 409 RESOURCE_IN_USE com
// a contagem da agenda em `details.references`, e não um 422 genérico; e o
// contato e a visita continuam lá.
func TestAgendaQAContatoComVisitaNaoSeApaga(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qagFaxina(t, admin)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	tk := admin.Token
	cliente := f.contato(t, casa.Prop)
	d := casa.Hoje.AddDays(240)

	visita := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "Visita de apresentação", "contact_id": cliente.ID,
		"starts_at": casa.rfc(d, "10:00"), "ends_at": casa.rfc(d, "11:00")}).Data
	for _, momento := range []string{"com a visita agendada", "com a visita desmarcada"} {
		e := qbErro(t, a.chamar(t, http.MethodDelete, "/contacts/"+cliente.ID.String(), tk, nil), http.StatusConflict, "RESOURCE_IN_USE",
			"apagando o cliente da visita "+momento)
		refs, ok := e.Details["references"].(map[string]any)
		if !ok || refs["agenda_events"] != float64(1) {
			t.Errorf("%s: details.references.agenda_events deveria ser 1: %v", momento, e.Details)
		}
		if n := a.qagContar(t, `SELECT count(*) FROM contacts WHERE id = $1`, cliente.ID); n != 1 {
			t.Fatalf("%s: o contato foi apagado", momento)
		}
		if momento == "com a visita agendada" {
			exigirStatusQB(t, a.chamar(t, http.MethodDelete, "/agenda/events/"+visita.ID.String(), tk, nil), http.StatusOK, "desmarcando")
		}
	}
	if x := a.qagLer(t, tk, visita.ID); x.ContatoID == nil || *x.ContatoID != cliente.ID {
		t.Errorf("a visita perdeu o cliente: %+v", x)
	}
}

// Em linguagem de negócio: o bloqueio de horário da operação ("equipe em
// treinamento das 14 às 16") aparece no dia em que cruza, em ordem de início,
// recusa o que não faz sentido e, ao ser removido, some de verdade (204) — com
// o antes guardado na trilha.
func TestAgendaQABloqueiosDeHorarioDaOperacao(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	a.qagFaxina(t, admin)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	tk := admin.Token
	d := casa.Hoje.AddDays(250)

	criar := func(motivo, de, ate string, diaInteiro bool) qagBloqueio {
		r := a.chamar(t, http.MethodPost, "/agenda/blocks", tk, map[string]any{"reason": motivo, "starts_at": de, "ends_at": ate, "all_day": diaInteiro})
		b := qbDado[qagBloqueio](t, r, http.StatusCreated, "POST /agenda/blocks "+motivo)
		if loc := r.Headers.Get("Location"); loc != PrefixoDaAPI+"/agenda/blocks/"+b.ID.String() {
			t.Errorf("Location: %q", loc)
		}
		return b
	}
	cedo := criar("Cedo", casa.rfc(d, "08:00"), casa.rfc(d, "09:00"), false)
	madrugada := criar("Atravessa a meia-noite", casa.rfc(d.AddDays(-1), "22:00"), casa.rfc(d, "02:00"), false)
	outroDia := criar("Outro dia", casa.rfc(d.AddDays(1), "10:00"), casa.rfc(d.AddDays(1), "11:00"), false)
	inteiro := criar("Dedetização", casa.rfc(d.AddDays(2), "00:00"), casa.rfc(d.AddDays(3), "00:00"), true)
	if !inteiro.DiaInteiro || cedo.CriadoPor == nil || *cedo.CriadoPor != admin.ID {
		t.Errorf("bloqueio: all_day %v, created_by %v", inteiro.DiaInteiro, cedo.CriadoPor)
	}

	doDia, meta := qbLista[qagBloqueio](t, a.chamar(t, http.MethodGet, "/agenda/blocks?per_page=100&from="+d.String()+"&to="+d.AddDays(1).String(), tk, nil), "bloqueios do dia")
	var ids []uuid.UUID
	for _, b := range doDia {
		if b.ID == cedo.ID || b.ID == madrugada.ID || b.ID == outroDia.ID {
			ids = append(ids, b.ID)
		}
	}
	if len(ids) != 2 || ids[0] != madrugada.ID || ids[1] != cedo.ID || meta.Total < 2 {
		t.Errorf("o dia traz os que CRUZAM a janela, por início (madrugada, cedo): %v (total %d)", ids, meta.Total)
	}

	for nome, corpo := range map[string]map[string]any{
		"dia inteiro fora da meia-noite": {"reason": "x", "all_day": true, "starts_at": casa.rfc(d, "03:00"), "ends_at": casa.rfc(d.AddDays(1), "03:00")},
		"fim antes do início":            {"reason": "x", "starts_at": casa.rfc(d, "10:00"), "ends_at": casa.rfc(d, "09:00")},
		"mais de 366 dias":               {"reason": "x", "starts_at": casa.rfc(d, "00:00"), "ends_at": casa.rfc(d.AddDays(367), "00:00")},
		"motivo só de espaço":            {"reason": "   ", "starts_at": casa.rfc(d, "10:00"), "ends_at": casa.rfc(d, "11:00")},
		"motivo de 201 caracteres":       {"reason": strings.Repeat("a", 201), "starts_at": casa.rfc(d, "10:00"), "ends_at": casa.rfc(d, "11:00")},
		"instante sem fuso":              {"reason": "x", "starts_at": d.String() + "T10:00:00", "ends_at": d.String() + "T11:00:00"},
	} {
		qbErro(t, a.chamar(t, http.MethodPost, "/agenda/blocks", tk, corpo), http.StatusUnprocessableEntity, "VALIDATION_ERROR", nome)
	}
	for _, campo := range []string{"starts_at", "ends_at", "all_day", "reason"} {
		qbErro(t, a.chamar(t, http.MethodPatch, "/agenda/blocks/"+cedo.ID.String(), tk, map[string]any{campo: nil}),
			http.StatusUnprocessableEntity, "VALIDATION_ERROR", "PATCH "+campo+" null")
	}
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/agenda/blocks/"+cedo.ID.String(), tk, map[string]any{"reason": "Cedo (remarcado)"}),
		http.StatusOK, "PATCH do motivo")
	if x := qbDado[qagBloqueio](t, a.chamar(t, http.MethodGet, "/agenda/blocks/"+cedo.ID.String(), tk, nil), http.StatusOK, "GET"); x.Motivo != "Cedo (remarcado)" || x.Inicio != cedo.Inicio {
		t.Errorf("PATCH muda só o que veio: %+v", x)
	}

	rm := a.chamar(t, http.MethodDelete, "/agenda/blocks/"+outroDia.ID.String(), tk, nil)
	if rm.Status != http.StatusNoContent || len(rm.Corpo) != 0 {
		t.Fatalf("DELETE do bloqueio é 204 sem corpo: %d %s", rm.Status, rm.Corpo)
	}
	qbErro(t, a.chamar(t, http.MethodGet, "/agenda/blocks/"+outroDia.ID.String(), tk, nil), http.StatusNotFound, "NOT_FOUND", "bloqueio removido")
	qbErro(t, a.chamar(t, http.MethodDelete, "/agenda/blocks/"+outroDia.ID.String(), tk, nil), http.StatusNotFound, "NOT_FOUND", "removendo de novo")
	if n := a.qagContar(t, `SELECT count(*) FROM agenda_blocks WHERE id = $1`, outroDia.ID); n != 0 {
		t.Fatalf("o DELETE do bloqueio apaga de verdade")
	}
	if n := a.qagContar(t, `SELECT count(*) FROM audit_log WHERE entity_id = $1 AND before IS NOT NULL AND before::text LIKE '%Outro dia%'`, outroDia.ID); n == 0 {
		t.Errorf("o antes do bloqueio removido fica em audit_log")
	}
}
