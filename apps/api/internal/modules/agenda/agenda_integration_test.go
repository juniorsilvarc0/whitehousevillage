//go:build integration

package agenda_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// ─────────────────────────── Marcar à mão ───────────────────────────

// Conflito de HORÁRIO avisa (201 com `meta.warnings`); a visita nasce
// `agendado`, `origin: manual`, com quem marcou em `created_by`, e o Location
// aponta para ela.
func TestConflitoDeHorarioCriaComAviso(t *testing.T) {
	a := subir(t)
	quem, token := a.gestor(t)
	unidade, codigo := a.unidade(t)
	dia := a.hoje.AddDays(10)

	resp := a.chamar(t, http.MethodPost, "/agenda/events", token, map[string]any{
		"type": "visita", "title": "  Visita com a família Andrade  ", "unit_id": unidade,
		"starts_at": a.rfc(dia, "21:00"), "notes": "trazer a planta",
	})
	exigir(t, resp, http.StatusCreated, "visita fora do horário")
	e := escrita(t, resp)
	if !strings.HasSuffix(resp.Cabecalho.Get("Location"), "/api/v1/agenda/events/"+e.Data.ID.String()) {
		t.Errorf("Location: %q", resp.Cabecalho.Get("Location"))
	}
	if len(e.Meta.Avisos) != 1 || e.Meta.Avisos[0].Tipo != "fora_do_horario" || e.Meta.Avisos[0].RefID != nil {
		t.Fatalf("avisos: %+v", e.Meta.Avisos)
	}
	ev := e.Data
	if ev.Origem != "manual" || ev.Status != "agendado" || ev.Titulo != "Visita com a família Andrade" ||
		ev.Editavel != "tudo" || ev.CriadoPor == nil || *ev.CriadoPor != quem || ev.ResponsavelID != nil {
		t.Fatalf("evento: %+v", ev)
	}
	// `ends_at` ausente é um passo da grade (30 min).
	a.exigirJanela(t, ev, dia, "21:00", "21:30", "janela padrão")
	if len(ev.Unidades) != 1 || ev.Unidades[0].Codigo != codigo || ev.UnidadeID == nil || *ev.UnidadeID != unidade {
		t.Errorf("unidade: %+v %v", ev.Unidades, ev.UnidadeID)
	}
	if len(ev.Acoes) != 1 || ev.Acoes[0] != "cancel" {
		t.Errorf("visita futura: só desmarcar, veio %v", ev.Acoes)
	}
	// A string sai no offset da casa.
	if !strings.Contains(string(resp.Corpo), `"starts_at":"`+a.rfc(dia, "21:00")+`"`) {
		t.Errorf("starts_at fora do offset da casa: %s", resp.Corpo)
	}
	if n := a.contar(t, `SELECT count(*) FROM audit_log WHERE action = 'agenda_events.criado' AND entity_id = $1 AND actor_id = $2`,
		ev.ID, quem); n != 1 {
		t.Errorf("trilha da criação: %d", n)
	}
}

// Conflito de UNIDADE recusa: a visita no apartamento em manutenção, ou em
// cima de outra visita, é 409 UNIT_NOT_AVAILABLE com `details.conflicts` (a
// unidade, o que a ocupa e a janela — sem id nem pessoa), e nada é criado.
func TestConflitoDeUnidadeRecusaENadaECriado(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	unidade, codigo := a.unidade(t)
	dia := a.hoje.AddDays(12)
	a.bloqueio(t, unidade, "maintenance", dia, dia.AddDays(2), nil)

	resp := a.chamar(t, http.MethodPost, "/agenda/events", token, map[string]any{
		"type": "visita", "title": "Visita no apartamento em obra", "unit_id": unidade,
		"starts_at": a.rfc(dia.AddDays(1), "10:00"), "ends_at": a.rfc(dia.AddDays(1), "11:00"),
	})
	exigirErro(t, resp, http.StatusConflict, "UNIT_NOT_AVAILABLE", "visita na manutenção")
	conflitos, _ := resp.detalhes(t)["conflicts"].([]any)
	if len(conflitos) != 1 {
		t.Fatalf("conflitos: %s", resp.Corpo)
	}
	c := conflitos[0].(map[string]any)
	if c["kind"] != "manutencao" || c["unit_code"] != codigo || c["unit_id"] != unidade.String() ||
		c["starts_at"] != a.rfc(dia, "00:00") || c["ends_at"] != a.rfc(dia.AddDays(2), "00:00") {
		t.Errorf("conflito: %v", c)
	}
	for _, proibido := range []string{"title", "event_id", "assignee_id", "contact_id"} {
		if _, ok := c[proibido]; ok {
			t.Errorf("o conflito não identifica o evento nem a pessoa (%s): %v", proibido, c)
		}
	}

	// Em cima de outra visita na mesma unidade: o domínio recusa antes do banco.
	outroDia := a.hoje.AddDays(20)
	a.marcar(t, token, map[string]any{"type": "visita", "title": "Primeira", "unit_id": unidade,
		"starts_at": a.rfc(outroDia, "09:00"), "ends_at": a.rfc(outroDia, "10:00")})
	exigirErro(t, a.chamar(t, http.MethodPost, "/agenda/events", token, map[string]any{
		"type": "visita", "title": "Segunda", "unit_id": unidade,
		"starts_at": a.rfc(outroDia, "09:30"), "ends_at": a.rfc(outroDia, "10:30"),
	}), http.StatusConflict, "UNIT_NOT_AVAILABLE", "visita em cima de visita")
	// Encostada (half-open) passa.
	a.marcar(t, token, map[string]any{"type": "visita", "title": "Encostada", "unit_id": unidade,
		"starts_at": a.rfc(outroDia, "10:00"), "ends_at": a.rfc(outroDia, "11:00")})

	if n := a.contar(t, `SELECT count(*) FROM agenda_events WHERE unit_id = $1`, unidade); n != 2 {
		t.Fatalf("o 409 não cria nada: %d eventos na unidade, esperado 2", n)
	}
}

// Os avisos que dependem de outro compromisso: o bloqueio de horário da
// operação e o responsável sem a folga — `ref_id` aponta para o compromisso.
func TestAvisosDoBloqueioDeHorarioEDoResponsavelOcupado(t *testing.T) {
	a := subir(t)
	quem, token := a.gestor(t)
	dia := a.hoje.AddDays(14)

	bloqueio := dado[struct {
		ID uuid.UUID `json:"id"`
	}](t, a.chamar(t, http.MethodPost, "/agenda/blocks", token, map[string]any{
		"starts_at": a.rfc(dia, "14:00"), "ends_at": a.rfc(dia, "16:00"), "reason": "Equipe em treinamento",
	}))
	primeira := a.marcar(t, token, map[string]any{"type": "evento", "title": "Reunião com fornecedor",
		"assignee_id": quem, "starts_at": a.rfc(dia, "10:00"), "ends_at": a.rfc(dia, "11:00")})
	if len(primeira.Meta.Avisos) != 0 {
		t.Fatalf("a primeira não tem aviso: %+v", primeira.Meta.Avisos)
	}

	// 11:05, com 15 minutos de folga: o responsável está ocupado.
	segunda := a.marcar(t, token, map[string]any{"type": "evento", "title": "Outra reunião",
		"assignee_id": quem, "starts_at": a.rfc(dia, "11:05"), "ends_at": a.rfc(dia, "11:35")})
	if len(segunda.Meta.Avisos) != 1 || segunda.Meta.Avisos[0].Tipo != "responsavel_ocupado" ||
		segunda.Meta.Avisos[0].RefID == nil || *segunda.Meta.Avisos[0].RefID != primeira.Data.ID {
		t.Fatalf("avisos da segunda: %+v", segunda.Meta.Avisos)
	}

	terceira := a.marcar(t, token, map[string]any{"type": "evento", "title": "Na hora do treinamento",
		"starts_at": a.rfc(dia, "15:00"), "ends_at": a.rfc(dia, "15:30")})
	if len(terceira.Meta.Avisos) != 1 || terceira.Meta.Avisos[0].Tipo != "bloqueio_de_horario" ||
		terceira.Meta.Avisos[0].RefID == nil || *terceira.Meta.Avisos[0].RefID != bloqueio.ID {
		t.Fatalf("avisos da terceira: %+v", terceira.Meta.Avisos)
	}

	// Mudar a janela da primeira para cima do bloqueio avisa no PATCH.
	resp := a.chamar(t, http.MethodPatch, "/agenda/events/"+primeira.Data.ID.String(), token, map[string]any{
		"starts_at": a.rfc(dia, "15:30"), "ends_at": a.rfc(dia, "16:30"),
	})
	exigir(t, resp, http.StatusOK, "movendo a reunião")
	if av := escrita(t, resp).Meta.Avisos; len(av) == 0 || av[0].Tipo != "bloqueio_de_horario" {
		t.Fatalf("avisos do PATCH: %+v", av)
	}
	// Só a anotação: nada de aviso.
	resp = a.chamar(t, http.MethodPatch, "/agenda/events/"+primeira.Data.ID.String(), token, map[string]any{"notes": "levar contrato"})
	exigir(t, resp, http.StatusOK, "anotando")
	if av := escrita(t, resp).Meta.Avisos; len(av) != 0 {
		t.Fatalf("anotação não muda janela, não avisa: %+v", av)
	}
}

// Validações que o corpo e o domínio recusam com 422 no campo.
func TestMarcarRecusaOQueNaoFazSentido(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	dia := a.hoje.AddDays(30)
	inativa, _ := a.unidade(t)
	if _, err := a.pool.Exec(a.ctx, `UPDATE units SET active = false WHERE id = $1`, inativa); err != nil {
		t.Fatal(err)
	}
	anonimo := a.contato(t)
	if _, err := a.pool.Exec(a.ctx, `UPDATE contacts SET anonymized_at = now() WHERE id = $1`, anonimo); err != nil {
		t.Fatal(err)
	}

	casos := []struct {
		nome  string
		corpo map[string]any
		campo string
	}{
		{"check-in à mão", map[string]any{"type": "checkin", "title": "x", "starts_at": a.rfc(dia, "10:00")}, "type"},
		{"título só espaço", map[string]any{"type": "visita", "title": "   ", "starts_at": a.rfc(dia, "10:00")}, "title"},
		{"instante sem fuso", map[string]any{"type": "visita", "title": "x", "starts_at": dia.String() + "T10:00:00"}, "starts_at"},
		{"fim antes do início", map[string]any{"type": "visita", "title": "x", "starts_at": a.rfc(dia, "10:00"),
			"ends_at": a.rfc(dia, "09:00")}, "ends_at"},
		{"dia inteiro fora da meia-noite da casa", map[string]any{"type": "evento", "title": "x", "all_day": true,
			"starts_at": a.rfc(dia, "03:00"), "ends_at": a.rfc(dia.AddDays(1), "03:00")}, "starts_at"},
		{"além do horizonte", map[string]any{"type": "visita", "title": "x", "starts_at": a.rfc(a.hoje.AddDays(1200), "10:00")}, "starts_at"},
		{"unidade inativa", map[string]any{"type": "visita", "title": "x", "unit_id": inativa, "starts_at": a.rfc(dia, "10:00")}, "unit_id"},
		{"unidade inexistente", map[string]any{"type": "visita", "title": "x", "unit_id": uuid.New(), "starts_at": a.rfc(dia, "10:00")}, "unit_id"},
		{"contato anonimizado", map[string]any{"type": "visita", "title": "x", "contact_id": anonimo, "starts_at": a.rfc(dia, "10:00")}, "contact_id"},
		{"responsável inexistente", map[string]any{"type": "visita", "title": "x", "assignee_id": uuid.New(), "starts_at": a.rfc(dia, "10:00")}, "assignee_id"},
		{"reserva inexistente", map[string]any{"type": "visita", "title": "x", "reservation_id": uuid.New(), "starts_at": a.rfc(dia, "10:00")}, "reservation_id"},
		{"campo desconhecido", map[string]any{"type": "visita", "title": "x", "starts_at": a.rfc(dia, "10:00"), "sala": 1}, "sala"},
		{"ends_at nulo", map[string]any{"type": "visita", "title": "x", "starts_at": a.rfc(dia, "10:00"), "ends_at": nil}, "ends_at"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			resp := a.chamar(t, http.MethodPost, "/agenda/events", token, c.corpo)
			exigirErro(t, resp, http.StatusUnprocessableEntity, "VALIDATION_ERROR", c.nome)
			if resp.detalhes(t)[c.campo] == nil {
				t.Errorf("o 422 não nomeia %s: %s", c.campo, resp.Corpo)
			}
		})
	}
	if n := a.contar(t, `SELECT count(*) FROM agenda_events WHERE created_by IN (SELECT id FROM users WHERE id = ANY($1::uuid[]))`,
		a.usuarios); n != 0 {
		t.Fatalf("422 não cria nada: %d", n)
	}
}

// ─────────────────────────── Transições e edição ─────────────────────

// Concluir e desmarcar seguem a máquina do domínio; o fechado não reabre; o
// concluído só aceita anotação; o corpo vazio passa em qualquer estado.
func TestConcluirEDesmarcarSeguemAMaquina(t *testing.T) {
	a := subir(t)
	quem, token := a.gestor(t)
	unidade, _ := a.unidade(t)

	ontem := a.hoje.AddDays(-1)
	feita := a.marcar(t, token, map[string]any{"type": "limpeza", "title": "Faxina geral", "unit_id": unidade,
		"starts_at": a.rfc(ontem, "09:00"), "ends_at": a.rfc(ontem, "12:00")}).Data
	if len(feita.Acoes) != 2 {
		t.Fatalf("ontem: concluir e desmarcar, veio %v", feita.Acoes)
	}
	caminho := "/agenda/events/" + feita.ID.String()
	resp := a.chamar(t, http.MethodPost, caminho+"/complete", token, nil)
	exigir(t, resp, http.StatusOK, "concluindo")
	ev := dado[eventoVisto](t, resp)
	if ev.Status != "concluido" || ev.ConcluidoEm == nil || ev.ConcluidoPor == nil || *ev.ConcluidoPor != quem ||
		ev.Editavel != "so_notas" || len(ev.Acoes) != 0 {
		t.Fatalf("concluído: %+v", ev)
	}
	exigirErro(t, a.chamar(t, http.MethodPost, caminho+"/complete", token, nil),
		http.StatusConflict, "AGENDA_EVENT_CLOSED", "concluindo de novo")
	exigirErro(t, a.chamar(t, http.MethodDelete, caminho, token, nil),
		http.StatusConflict, "AGENDA_EVENT_CLOSED", "desmarcando o concluído")
	exigir(t, a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"notes": "faltou toalha no AP-02"}),
		http.StatusOK, "anotando o concluído")
	fechado := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"starts_at": a.rfc(ontem, "08:00")})
	exigirErro(t, fechado, http.StatusConflict, "AGENDA_EVENT_CLOSED", "mudando o horário do concluído")
	if fechado.detalhes(t)["editable"] != "so_notas" {
		t.Errorf("details.editable: %v", fechado.detalhes(t)["editable"])
	}

	amanha := a.hoje.AddDays(1)
	futura := a.marcar(t, token, map[string]any{"type": "visita", "title": "Visita", "unit_id": unidade,
		"starts_at": a.rfc(amanha, "09:00"), "ends_at": a.rfc(amanha, "10:00")}).Data
	cedo := a.chamar(t, http.MethodPost, "/agenda/events/"+futura.ID.String()+"/complete", token, nil)
	exigirErro(t, cedo, http.StatusConflict, "INVALID_STATE_TRANSITION", "concluindo a visita de amanhã")
	if cedo.detalhes(t)["reason"] != "nao_comecou" {
		t.Errorf("details.reason: %v", cedo.detalhes(t)["reason"])
	}
	resp = a.chamar(t, http.MethodDelete, "/agenda/events/"+futura.ID.String(), token, nil)
	exigir(t, resp, http.StatusOK, "desmarcando")
	if ev := dado[eventoVisto](t, resp); ev.Status != "cancelado" || ev.CanceladoEm == nil || ev.Editavel != "nada" {
		t.Fatalf("desmarcado: %+v", ev)
	}
	exigir(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+futura.ID.String(), token, map[string]any{}),
		http.StatusOK, "corpo vazio no cancelado")
	exigirErro(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+futura.ID.String(), token, map[string]any{"notes": "x"}),
		http.StatusConflict, "AGENDA_EVENT_CLOSED", "anotando o cancelado")
	// Desmarcado não ocupa mais: outra visita no mesmo horário passa.
	a.marcar(t, token, map[string]any{"type": "visita", "title": "No lugar", "unit_id": unidade,
		"starts_at": a.rfc(amanha, "09:00"), "ends_at": a.rfc(amanha, "10:00")})
	if n := a.contar(t, `SELECT count(*) FROM agenda_events WHERE id = $1`, futura.ID); n != 1 {
		t.Fatal("desmarcar não apaga")
	}
}

// O PUT do manual substitui tudo: `type` e `title` obrigatórios no agendado,
// anulável omitido vira `null`.
func TestSubstituirOManualTrocaTudo(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	unidade, _ := a.unidade(t)
	contato := a.contato(t)
	dia := a.hoje.AddDays(40)
	ev := a.marcar(t, token, map[string]any{"type": "visita", "title": "Visita", "unit_id": unidade, "contact_id": contato,
		"notes": "x", "starts_at": a.rfc(dia, "09:00"), "ends_at": a.rfc(dia, "10:00")}).Data
	caminho := "/agenda/events/" + ev.ID.String()

	falta := a.chamar(t, http.MethodPut, caminho, token, map[string]any{
		"starts_at": a.rfc(dia, "09:00"), "ends_at": a.rfc(dia, "10:00")})
	exigirErro(t, falta, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "PUT sem type e title")
	if d := falta.detalhes(t); d["type"] == nil || d["title"] == nil {
		t.Fatalf("details: %v", d)
	}

	resp := a.chamar(t, http.MethodPut, caminho, token, map[string]any{
		"type": "evento", "title": "Reunião", "starts_at": a.rfc(dia, "11:00"), "ends_at": a.rfc(dia, "12:00")})
	exigir(t, resp, http.StatusOK, "PUT")
	depois := escrita(t, resp).Data
	if depois.Tipo != "evento" || depois.Titulo != "Reunião" || depois.UnidadeID != nil || depois.ContatoID != nil ||
		depois.Notas != nil || len(depois.Unidades) != 0 {
		t.Fatalf("o PUT substitui tudo (omitido vira null): %+v", depois)
	}
}

// ─────────────────────────── Escopo `own` ───────────────────────────

// O corretor (escopo `own`) vê e edita só os eventos de que é o responsável;
// o do colega é 404 — na lista, no detalhe, na edição e nas ações —, nunca
// 403. Em `own`, toda escrita tem o próprio ator como responsável.
func TestEscopoOwnSoAlcancaOsEventosDoProprioResponsavel(t *testing.T) {
	a := subir(t)
	_, gestor := a.gestor(t)
	ana, tokenAna := a.emOwn(t, "Ana Corretora")
	beto, tokenBeto := a.emOwn(t, "Beto Corretor")
	unidade, _ := a.unidade(t)
	dia := a.hoje.AddDays(16)

	visita := a.marcar(t, tokenAna, map[string]any{"type": "visita", "title": "Visita da Ana", "unit_id": unidade,
		"starts_at": a.rfc(dia, "09:00"), "ends_at": a.rfc(dia, "10:00")}).Data
	if visita.ResponsavelID == nil || *visita.ResponsavelID != ana {
		t.Fatalf("em own, o responsável ausente vira o ator: %v", visita.ResponsavelID)
	}
	for _, pedido := range []any{beto, nil} {
		resp := a.chamar(t, http.MethodPost, "/agenda/events", tokenAna, map[string]any{"type": "visita", "title": "x",
			"assignee_id": pedido, "starts_at": a.rfc(dia, "15:00")})
		exigirErro(t, resp, http.StatusForbidden, "FORBIDDEN", "own entregando a visita a outro")
		if resp.detalhes(t)["reason"] != "assignee_not_actor" {
			t.Errorf("details.reason: %v", resp.detalhes(t)["reason"])
		}
	}
	// Um evento sem responsável e uma manutenção na janela: em own, nada disso
	// aparece.
	a.marcar(t, gestor, map[string]any{"type": "evento", "title": "Da casa", "starts_at": a.rfc(dia, "12:00")})
	a.bloqueio(t, unidade, "maintenance", dia.AddDays(2), dia.AddDays(3), nil)

	janela := "/agenda/events?" + janelaDaQuery(dia, dia.AddDays(5))
	daAna, meta := lista[eventoVisto](t, a.chamar(t, http.MethodGet, janela, tokenAna, nil))
	if meta.Total != 1 || len(daAna) != 1 || daAna[0].ID != visita.ID {
		t.Fatalf("a Ana vê só a própria visita (total %d): %+v", meta.Total, daAna)
	}
	doBeto, meta := lista[eventoVisto](t, a.chamar(t, http.MethodGet, janela, tokenBeto, nil))
	if meta.Total != 0 || len(doBeto) != 0 {
		t.Fatalf("o Beto não vê a visita da colega (total %d): %+v", meta.Total, doBeto)
	}

	caminho := "/agenda/events/" + visita.ID.String()
	exigirErro(t, a.chamar(t, http.MethodGet, caminho, tokenBeto, nil), http.StatusNotFound, "NOT_FOUND", "detalhe alheio")
	exigirErro(t, a.chamar(t, http.MethodPatch, caminho, tokenBeto, map[string]any{"notes": "x"}), http.StatusNotFound, "NOT_FOUND", "PATCH alheio")
	exigirErro(t, a.chamar(t, http.MethodPut, caminho, tokenBeto, map[string]any{"type": "visita", "title": "x",
		"starts_at": a.rfc(dia, "09:00"), "ends_at": a.rfc(dia, "10:00")}), http.StatusNotFound, "NOT_FOUND", "PUT alheio")
	exigirErro(t, a.chamar(t, http.MethodDelete, caminho, tokenBeto, nil), http.StatusNotFound, "NOT_FOUND", "DELETE alheio")
	exigirErro(t, a.chamar(t, http.MethodPost, caminho+"/complete", tokenBeto, nil), http.StatusNotFound, "NOT_FOUND", "complete alheio")

	exigir(t, a.chamar(t, http.MethodPatch, caminho, tokenAna, map[string]any{"notes": "cliente confirmou"}), http.StatusOK, "PATCH próprio")
	exigirErro(t, a.chamar(t, http.MethodPatch, caminho, tokenAna, map[string]any{"assignee_id": nil}),
		http.StatusForbidden, "FORBIDDEN", "own tirando o próprio responsável")
	exigir(t, a.chamar(t, http.MethodGet, caminho, tokenAna, nil), http.StatusOK, "detalhe próprio")
}

// O corretor em `own` só liga o evento a uma reserva que ele ENXERGA pelo
// escopo dele em `reservations:ver` — a dele. A do colega é 422 em
// `reservation_id` COMO SE NÃO EXISTISSE (a mesma frase da reserva
// inexistente), no POST, no PUT e no PATCH, e nada dela — código, unidades —
// aparece no corpo: ligada, a resposta do evento devolveria os dois.
func TestEscopoOwnNaoLigaOEventoAReservaDoColega(t *testing.T) {
	a := subir(t)
	_, gestor := a.gestor(t)
	celulas := append([]string{"reservations:ver", "reservations:criar"}, celulasDaAgenda...)
	ana, tokenAna := a.usuario(t, a.perfil(t, auth.EscopoOwn, celulas...), "Ana Corretora")
	beto, _ := a.usuario(t, a.perfil(t, auth.EscopoOwn, celulas...), "Beto Corretor")
	contato := a.contato(t)
	daAna := a.criarReserva(t, gestor, a.produto(t, "cobertura"), contato, a.hoje.AddDays(790), a.hoje.AddDays(793))
	doBeto := a.criarReserva(t, gestor, a.produto(t, "cobertura"), contato, a.hoje.AddDays(796), a.hoje.AddDays(799))
	// Cada corretor é dono da sua venda — o eixo do `own` de reservations.
	for reserva, dono := range map[uuid.UUID]uuid.UUID{daAna.ID: ana, doBeto.ID: beto} {
		if _, err := a.pool.Exec(a.ctx, `UPDATE reservations SET owner_id = $2 WHERE id = $1`, reserva, dono); err != nil {
			t.Fatal(err)
		}
	}
	diaDoBeto := a.hoje.AddDays(797)

	semNadaDoColega := func(t *testing.T, r resposta, contexto string) {
		t.Helper()
		exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", contexto)
		if r.detalhes(t)["reservation_id"] == nil {
			t.Fatalf("%s: o 422 é no reservation_id: %s", contexto, r.Corpo)
		}
		for _, segredo := range []string{doBeto.Codigo, "COB-01", doBeto.ID.String()} {
			if strings.Contains(string(r.Corpo), segredo) {
				t.Errorf("%s: o corpo vazou %q da reserva do colega: %s", contexto, segredo, r.Corpo)
			}
		}
	}

	// POST: a reserva do colega é a mesma resposta da reserva que não existe.
	doColega := a.chamar(t, http.MethodPost, "/agenda/events", tokenAna, map[string]any{
		"type": "visita", "title": "Visita na venda do Beto", "reservation_id": doBeto.ID,
		"starts_at": a.rfc(diaDoBeto, "10:00"), "ends_at": a.rfc(diaDoBeto, "11:00")})
	semNadaDoColega(t, doColega, "POST na reserva do colega")
	inexistente := a.chamar(t, http.MethodPost, "/agenda/events", tokenAna, map[string]any{
		"type": "visita", "title": "x", "reservation_id": uuid.New(), "starts_at": a.rfc(diaDoBeto, "10:00")})
	exigirErro(t, inexistente, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "POST em reserva inexistente")
	if doColega.detalhes(t)["reservation_id"] != inexistente.detalhes(t)["reservation_id"] {
		t.Errorf("a do colega e a inexistente respondem diferente — um oráculo: %v × %v",
			doColega.detalhes(t)["reservation_id"], inexistente.detalhes(t)["reservation_id"])
	}

	// A dela, sim.
	ligada := a.marcar(t, tokenAna, map[string]any{
		"type": "visita", "title": "Visita na venda da Ana", "reservation_id": daAna.ID,
		"starts_at": a.rfc(a.hoje.AddDays(791), "10:00"), "ends_at": a.rfc(a.hoje.AddDays(791), "11:00")}).Data
	if ligada.ReservaCodigo == nil || *ligada.ReservaCodigo != daAna.Codigo {
		t.Fatalf("ligada à reserva dela: %+v", ligada.ReservaCodigo)
	}

	// PATCH e PUT da visita dela para a reserva do colega: o mesmo 422.
	caminho := "/agenda/events/" + ligada.ID.String()
	semNadaDoColega(t, a.chamar(t, http.MethodPatch, caminho, tokenAna, map[string]any{"reservation_id": doBeto.ID}),
		"PATCH para a reserva do colega")
	semNadaDoColega(t, a.chamar(t, http.MethodPut, caminho, tokenAna, map[string]any{
		"type": "visita", "title": "Visita", "reservation_id": doBeto.ID,
		"starts_at": a.rfc(diaDoBeto, "10:00"), "ends_at": a.rfc(diaDoBeto, "11:00")}), "PUT para a reserva do colega")

	if n := a.contar(t, `SELECT count(*) FROM agenda_events WHERE reservation_id = $1`, doBeto.ID); n != 0 {
		t.Fatalf("%d eventos ligados à reserva do colega", n)
	}
	// Em `all`, a gestão liga a qualquer uma.
	a.marcar(t, gestor, map[string]any{"type": "visita", "title": "Visita da gestão", "reservation_id": doBeto.ID,
		"starts_at": a.rfc(diaDoBeto, "15:00"), "ends_at": a.rfc(diaDoBeto, "16:00")})
}

// ─────────────────────────── A lista ────────────────────────────────

// A lista traz, no mesmo envelope, os eventos gravados e a manutenção e o
// bloqueio de unidade LIDOS de `stay_blocks` — o dia inteiro, `editable:
// nada`, sem ações, com o id da linha de `stay_blocks` (e da ordem).
func TestListaTrazManutencaoEBloqueioDerivados(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	unidade, codigo := a.unidade(t)
	dia := a.hoje.AddDays(50)
	nota := "Uso do proprietário"
	daOrdem := a.bloqueio(t, unidade, "maintenance", dia, dia.AddDays(2), &nota)
	var ordem uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO maintenance_orders (property_id, unit_id, title, stay_block_id) VALUES ($1, $2, 'Trocar o ar-condicionado', $3)
		RETURNING id`, a.propriedade, unidade, daOrdem).Scan(&ordem); err != nil {
		t.Fatal(err)
	}
	doDono := a.bloqueio(t, unidade, "owner_hold", dia.AddDays(3), dia.AddDays(5), &nota)
	visita := a.marcar(t, token, map[string]any{"type": "visita", "title": "Visita", "unit_id": unidade,
		"starts_at": a.rfc(dia.AddDays(6), "09:00"), "ends_at": a.rfc(dia.AddDays(6), "10:00")}).Data

	resp := a.chamar(t, http.MethodGet, "/agenda/events?unit_id="+unidade.String()+"&"+janelaDaQuery(dia, dia.AddDays(7)), token, nil)
	exigir(t, resp, http.StatusOK, "lista da unidade")
	evs, meta := lista[eventoVisto](t, resp)
	if meta.Total != 3 || len(evs) != 3 {
		t.Fatalf("manutenção, bloqueio e visita; veio total %d: %+v", meta.Total, evs)
	}
	man, blq, vis := evs[0], evs[1], evs[2]
	if man.Tipo != "manutencao" || man.Origem != "maintenance" || man.Titulo != "Trocar o ar-condicionado" ||
		!man.DiaInteiro || man.Editavel != "nada" || len(man.Acoes) != 0 || man.BloqueioID == nil || *man.BloqueioID != daOrdem ||
		man.OrdemID == nil || *man.OrdemID != ordem || man.ID != daOrdem || man.Status != "agendado" || man.ConflitoDeUnidade {
		t.Errorf("manutenção derivada: %+v", man)
	}
	if !man.Inicio.Equal(a.em(dia, "00:00")) || !man.Fim.Equal(a.em(dia.AddDays(2), "00:00")) || man.Data != dia.String() {
		t.Errorf("manutenção vai da meia-noite de %s à de %s: %s–%s (%s)", dia, dia.AddDays(2), man.Inicio, man.Fim, man.Data)
	}
	if blq.Tipo != "bloqueio" || blq.Origem != "stay_block" || blq.Titulo != nota || blq.ID != doDono || blq.OrdemID != nil ||
		len(blq.Unidades) != 1 || blq.Unidades[0].Codigo != codigo {
		t.Errorf("bloqueio derivado: %+v", blq)
	}
	if vis.ID != visita.ID {
		t.Errorf("a visita fecha a lista, em ordem de início: %+v", vis)
	}

	// O detalhe não serve o derivado: a tela abre a ordem.
	exigirErro(t, a.chamar(t, http.MethodGet, "/agenda/events/"+daOrdem.String(), token, nil),
		http.StatusNotFound, "NOT_FOUND", "detalhe do derivado")
	// Filtros: tipo, decrescente, sem responsável.
	soManutencao, _ := lista[eventoVisto](t, a.chamar(t, http.MethodGet,
		"/agenda/events?type=manutencao&unit_id="+unidade.String()+"&"+janelaDaQuery(dia, dia.AddDays(7)), token, nil))
	if len(soManutencao) != 1 || soManutencao[0].Tipo != "manutencao" {
		t.Errorf("type=manutencao: %+v", soManutencao)
	}
	decrescente, _ := lista[eventoVisto](t, a.chamar(t, http.MethodGet,
		"/agenda/events?sort=-starts_at&unit_id="+unidade.String()+"&"+janelaDaQuery(dia, dia.AddDays(7)), token, nil))
	if len(decrescente) != 3 || decrescente[0].ID != visita.ID {
		t.Errorf("-starts_at: %+v", decrescente)
	}
	comResponsavel, _ := lista[eventoVisto](t, a.chamar(t, http.MethodGet,
		"/agenda/events?unassigned=false&unit_id="+unidade.String()+"&"+janelaDaQuery(dia, dia.AddDays(7)), token, nil))
	if len(comResponsavel) != 0 {
		t.Errorf("unassigned=false: nenhum dos três tem responsável: %+v", comResponsavel)
	}
	// Paginação no envelope padrão.
	pagina, meta := lista[eventoVisto](t, a.chamar(t, http.MethodGet,
		"/agenda/events?per_page=2&page=2&unit_id="+unidade.String()+"&"+janelaDaQuery(dia, dia.AddDays(7)), token, nil))
	if meta.Total != 3 || meta.TotalPages != 2 || len(pagina) != 1 || pagina[0].ID != visita.ID {
		t.Errorf("página 2 de 2: %+v %+v", meta, pagina)
	}
}

// Filtro inválido é 422 — nunca a agenda inteira fingindo que filtrou (D12).
func TestFiltroInvalidoDaListaEh422(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	dia := a.hoje.AddDays(5)
	janela := janelaDaQuery(dia, dia.AddDays(7))
	casos := map[string]string{
		"unassigned=sim": "unassigned",
		"unassigned=true&assignee_id=" + uuid.NewString(): "unassigned",
		"type=xpto":       "type",
		"type=,":          "type",
		"status=pendente": "status",
		"sort=title":      "sort",
		"unit_id=abc":     "unit_id",
	}
	for consulta, campo := range casos {
		resp := a.chamar(t, http.MethodGet, "/agenda/events?"+janela+"&"+consulta, token, nil)
		exigirErro(t, resp, http.StatusUnprocessableEntity, "VALIDATION_ERROR", consulta)
		if resp.detalhes(t)[campo] == nil {
			t.Errorf("%s: o 422 não nomeia %s: %s", consulta, campo, resp.Corpo)
		}
	}
	for consulta, campo := range map[string]string{
		"":                                  "from",
		"from=" + dia.String():              "to",
		janelaDaQuery(dia, dia):             "to",
		janelaDaQuery(dia, dia.AddDays(93)): "to",
	} {
		resp := a.chamar(t, http.MethodGet, "/agenda/events?"+consulta, token, nil)
		exigirErro(t, resp, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "janela "+consulta)
		if resp.detalhes(t)[campo] == nil {
			t.Errorf("janela %q: o 422 não nomeia %s: %s", consulta, campo, resp.Corpo)
		}
	}
	// Com reservation_id, a janela é dispensável.
	exigir(t, a.chamar(t, http.MethodGet, "/agenda/events?reservation_id="+uuid.NewString(), token, nil), http.StatusOK, "por reserva")
	exigir(t, a.chamar(t, http.MethodGet, "/agenda/events?"+janelaDaQuery(dia, dia.AddDays(92)), token, nil), http.StatusOK, "92 dias")
}

// ─────────────────────────── Bloqueios de horário ───────────────────

// O bloqueio de horário da operação: CRUD em `all`, leitura em qualquer
// escopo, escrita em `own` recusada (403 `own_scope_not_applicable`), DELETE
// apaga de verdade e o antes fica na trilha.
func TestBloqueiosDeHorarioDaOperacao(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	_, tokenOwn := a.emOwn(t, "Corretora")
	dia := a.hoje.AddDays(18)

	resp := a.chamar(t, http.MethodPost, "/agenda/blocks", token, map[string]any{
		"starts_at": a.rfc(dia, "00:00"), "ends_at": a.rfc(dia.AddDays(1), "00:00"), "all_day": true, "reason": "Dedetização",
	})
	exigir(t, resp, http.StatusCreated, "criando o bloqueio")
	type bloqueioVisto struct {
		ID         uuid.UUID `json:"id"`
		Inicio     time.Time `json:"starts_at"`
		Fim        time.Time `json:"ends_at"`
		DiaInteiro bool      `json:"all_day"`
		Motivo     string    `json:"reason"`
	}
	b := dado[bloqueioVisto](t, resp)
	if !strings.HasSuffix(resp.Cabecalho.Get("Location"), "/api/v1/agenda/blocks/"+b.ID.String()) || !b.DiaInteiro {
		t.Fatalf("bloqueio: %+v (Location %q)", b, resp.Cabecalho.Get("Location"))
	}
	caminho := "/agenda/blocks/" + b.ID.String()

	exigirErro(t, a.chamar(t, http.MethodPost, "/agenda/blocks", token, map[string]any{
		"starts_at": a.rfc(dia, "03:00"), "ends_at": a.rfc(dia.AddDays(1), "03:00"), "all_day": true, "reason": "x",
	}), http.StatusUnprocessableEntity, "VALIDATION_ERROR", "dia inteiro fora da meia-noite da casa")

	// Em own: lê, não escreve.
	exigir(t, a.chamar(t, http.MethodGet, caminho, tokenOwn, nil), http.StatusOK, "own lê o bloqueio")
	daLista, _ := lista[bloqueioVisto](t, a.chamar(t, http.MethodGet, "/agenda/blocks?"+janelaDaQuery(dia, dia.AddDays(1)), tokenOwn, nil))
	if len(daLista) != 1 || daLista[0].ID != b.ID {
		t.Fatalf("own lista os bloqueios: %+v", daLista)
	}
	for _, c := range []struct {
		metodo, caminho string
		corpo           any
	}{
		{http.MethodPost, "/agenda/blocks", map[string]any{"starts_at": a.rfc(dia, "10:00"), "ends_at": a.rfc(dia, "11:00"), "reason": "x"}},
		{http.MethodPut, caminho, map[string]any{"starts_at": a.rfc(dia, "10:00"), "ends_at": a.rfc(dia, "11:00"), "reason": "x"}},
		{http.MethodPatch, caminho, map[string]any{"reason": "x"}},
		{http.MethodDelete, caminho, nil},
	} {
		resp := a.chamar(t, c.metodo, c.caminho, tokenOwn, c.corpo)
		exigirErro(t, resp, http.StatusForbidden, "FORBIDDEN", "own escrevendo "+c.metodo)
		if resp.detalhes(t)["reason"] != "own_scope_not_applicable" {
			t.Errorf("%s: details.reason %v", c.metodo, resp.detalhes(t)["reason"])
		}
	}

	resp = a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"reason": "  Dedetização geral  "})
	exigir(t, resp, http.StatusOK, "PATCH")
	if d := dado[bloqueioVisto](t, resp); d.Motivo != "Dedetização geral" || !d.DiaInteiro {
		t.Fatalf("PATCH: %+v", d)
	}
	exigirErro(t, a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"reason": nil}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "PATCH com null")
	resp = a.chamar(t, http.MethodPut, caminho, token, map[string]any{
		"starts_at": a.rfc(dia, "14:00"), "ends_at": a.rfc(dia, "16:00"), "reason": "Treinamento"})
	exigir(t, resp, http.StatusOK, "PUT")
	if d := dado[bloqueioVisto](t, resp); d.DiaInteiro || d.Motivo != "Treinamento" {
		t.Fatalf("PUT (all_day omitido volta a false): %+v", d)
	}

	exigir(t, a.chamar(t, http.MethodDelete, caminho, token, nil), http.StatusNoContent, "DELETE")
	exigirErro(t, a.chamar(t, http.MethodGet, caminho, token, nil), http.StatusNotFound, "NOT_FOUND", "depois do DELETE")
	var antes []byte
	if err := a.pool.QueryRow(a.ctx, `SELECT before FROM audit_log WHERE action = 'agenda_blocks.excluido' AND entity_id = $1`,
		b.ID).Scan(&antes); err != nil {
		t.Fatalf("o antes do bloqueio apagado fica na trilha: %v", err)
	}
	if !strings.Contains(string(antes), "Treinamento") {
		t.Errorf("trilha do DELETE: %s", antes)
	}
}

// ─────────────────────────── Horários da casa ───────────────────────

// A configuração nasce provisória; o "hoje" e o fuso são os da casa; gravar
// pede `settings:editar` e recusa a incoerência com todas as violações.
func TestConfiguracaoDaAgenda(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	_, soAgenda := a.usuario(t, a.perfil(t, auth.EscopoAll, celulasDaAgenda...), "Operadora")

	resp := a.chamar(t, http.MethodGet, "/agenda/settings", soAgenda, nil)
	exigir(t, resp, http.StatusOK, "GET /agenda/settings")
	type configuracaoVista struct {
		Fuso          string `json:"timezone"`
		Hoje          string `json:"today"`
		Entrada       string `json:"checkin_time"`
		Saida         string `json:"checkout_time"`
		Funcionamento []struct {
			Dia    int    `json:"weekday"`
			Inicio string `json:"start"`
			Fim    string `json:"end"`
		} `json:"business_hours"`
		Passo      int  `json:"slot_minutes"`
		Folga      int  `json:"buffer_minutes"`
		Provisoria bool `json:"provisional"`
	}
	cfg := dado[configuracaoVista](t, resp)
	if cfg.Fuso != a.zona.Name() || cfg.Hoje != a.hoje.String() || cfg.Entrada != "14:00" || cfg.Saida != "11:00" ||
		cfg.Passo != 30 || cfg.Folga != 15 || !cfg.Provisoria || len(cfg.Funcionamento) != 7 || cfg.Funcionamento[0].Dia != 1 {
		t.Fatalf("configuração provisória: %+v", cfg)
	}

	exigirErro(t, a.chamar(t, http.MethodPut, "/agenda/settings", soAgenda, horariosConfirmados("14:00", "11:00", "11:00", "14:00")),
		http.StatusForbidden, "FORBIDDEN", "agenda:editar não grava os horários da casa")

	ruim := horariosConfirmados("13:00", "11:00", "10:30", "14:00")
	ruim["slot_minutes"] = 25
	resp = a.chamar(t, http.MethodPut, "/agenda/settings", token, ruim)
	exigirErro(t, resp, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "horários incoerentes")
	for _, campo := range []string{"cleaning_start", "checkin_time", "slot_minutes"} {
		if resp.detalhes(t)[campo] == nil {
			t.Errorf("o 422 marca todos os campos de uma vez; faltou %s: %s", campo, resp.Corpo)
		}
	}
	for corpo, campo := range map[string]string{
		`{"checkin_time":"14h"}`: "checkin_time",
		`{"timezone":"UTC"}`:     "timezone",
	} {
		resp := a.chamar(t, http.MethodPut, "/agenda/settings", token, corpo)
		exigirErro(t, resp, http.StatusUnprocessableEntity, "VALIDATION_ERROR", corpo)
		if resp.detalhes(t)[campo] == nil {
			t.Errorf("%s: o 422 não nomeia %s: %s", corpo, campo, resp.Corpo)
		}
	}
	if n := a.contar(t, `SELECT count(*) FROM agenda_settings WHERE property_id = $1 AND updated_by IS NULL`, a.propriedade); n != 1 {
		t.Fatal("nenhuma gravação recusada pode ter tocado na configuração")
	}
}

// ─────────────────────────── O contato ──────────────────────────────

// O contato que é cliente de uma visita não se apaga: 409 RESOURCE_IN_USE com
// a contagem — e não o 23503 da FK virando 422 genérico.
func TestContatoComVisitaNaAgendaNaoSeApaga(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	a.marcar(t, token, map[string]any{"type": "visita", "title": "Visita", "contact_id": contato,
		"starts_at": a.rfc(a.hoje.AddDays(3), "10:00")})

	_, tokenContatos := a.usuario(t, a.perfil(t, auth.EscopoAll, "contacts:ver", "contacts:excluir"), "Gestora de contatos")
	resp := a.chamar(t, http.MethodDelete, "/contacts/"+contato.String(), tokenContatos, nil)
	exigirErro(t, resp, http.StatusConflict, "RESOURCE_IN_USE", "apagando o cliente da visita")
	refs, _ := resp.detalhes(t)["references"].(map[string]any)
	if refs["agenda_events"] != float64(1) {
		t.Fatalf("details.references.agenda_events: %v", refs)
	}
}

// ─────────────────────────── Auxiliares ─────────────────────────────

// horariosConfirmados é o corpo de `PUT /agenda/settings` com o funcionamento
// das 08:00 às 20:00 nos sete dias, passo de 30 e folga de 15.
func horariosConfirmados(entrada, saida, limpeza, fimDaLimpeza string) map[string]any {
	horas := []map[string]any{}
	for dia := 1; dia <= 7; dia++ {
		horas = append(horas, map[string]any{"weekday": dia, "start": "08:00", "end": "20:00"})
	}
	return map[string]any{
		"checkin_time": entrada, "checkout_time": saida, "cleaning_start": limpeza, "cleaning_end": fimDaLimpeza,
		"business_hours": horas, "slot_minutes": 30, "buffer_minutes": 15,
	}
}

func jsonDe(bruto []byte, alvo any) error { return json.Unmarshal(bruto, alvo) }
