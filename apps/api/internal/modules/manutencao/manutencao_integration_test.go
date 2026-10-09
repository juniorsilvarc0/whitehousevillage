//go:build integration

package manutencao_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// O caminho inteiro de uma ordem que nasceu de uma avaria: abre com bloqueio
// no mesmo commit, começa, conclui com custo — e a conclusão solta o calendário
// e conserta a avaria. Depois, encerrada só aceita o custo (e apagá-lo).
func TestOrdemDaAvariaDaAberturaAConclusao(t *testing.T) {
	a := subir(t)
	token, autor := a.gestor(t)
	unidade, codigo := a.unidade(t)
	suite := a.comodo(t, unidade, "Suíte 1")
	ar := a.bem(t, "Ar-condicionado")
	avaria := a.avaria(t, suite, ar)
	d := a.hoje(t)

	r := a.chamar(t, http.MethodPost, "/maintenance-orders", token, map[string]any{
		"unit_id": unidade, "issue_id": avaria, "title": "  Ar da suíte não gela  ", "priority": "alta",
		"block": periodo(d.AddDays(2), d.AddDays(5)),
	})
	exigir(t, r, http.StatusCreated, "abrindo a ordem da avaria")
	o := dado[ordemResp](t, r)
	if loc := r.Cabecalho.Get("Location"); loc != "/api/v1/maintenance-orders/"+o.ID.String() {
		t.Fatalf("Location = %q", loc)
	}
	if o.Status != "aberta" || o.Titulo != "Ar da suíte não gela" || o.Prioridade != "alta" ||
		o.UnidadeCodigo != codigo || o.UnidadeNome != codigo {
		t.Fatalf("ordem aberta: %+v", o)
	}
	if idTexto(o.ComodoID) != suite.String() || idTexto(o.BemID) != ar.String() || textoDe(o.ComodoNome) != "Suíte 1" {
		t.Fatalf("cômodo e bem vêm da avaria: %s %s", idTexto(o.ComodoID), idTexto(o.BemID))
	}
	if o.Avaria == nil || o.Avaria.ID != avaria || o.Avaria.Desfecho != nil || o.Avaria.Tipo != "avariado" {
		t.Fatalf("resumo da avaria: %+v", o.Avaria)
	}
	if fmt.Sprint(o.Acoes) != "[start complete cancel]" || o.Editavel != "tudo" {
		t.Fatalf("aberta: ações %v, editável %q", o.Acoes, o.Editavel)
	}
	if o.AbertaPor == nil || *o.AbertaPor != autor || textoDe(o.AbertaPorNome) != "Encarregada de Teste" {
		t.Fatalf("quem abriu: %s %s", idTexto(o.AbertaPor), textoDe(o.AbertaPorNome))
	}
	b := o.Bloqueio
	if b == nil || b.De != d.AddDays(2).String() || b.Ate != d.AddDays(5).String() || b.Noites != 3 ||
		b.Status != "confirmed" || b.Fase != "agendado" {
		t.Fatalf("bloqueio: %+v", b)
	}
	var origem, status, nota string
	var dono, criador uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT source, status, note, owner_id, created_by FROM stay_blocks WHERE id = $1`, b.ID).
		Scan(&origem, &status, &nota, &dono, &criador); err != nil {
		t.Fatal(err)
	}
	if origem != "maintenance" || status != "confirmed" || nota != o.Titulo || dono != autor || criador != autor {
		t.Fatalf("linha de stay_blocks: %s %s %q %s %s", origem, status, nota, dono, criador)
	}
	if a.vendavel(t, unidade, d.AddDays(3), d.AddDays(4)) {
		t.Fatal("a noite dentro do bloqueio continua vendável")
	}

	exigir(t, a.chamar(t, http.MethodGet, "/maintenance-orders/"+o.ID.String(), token, nil), http.StatusOK, "detalhe")

	// Começar; o segundo toque é 409 com as ações que restam.
	r = a.chamar(t, http.MethodPost, "/maintenance-orders/"+o.ID.String()+"/start", token, nil)
	exigir(t, r, http.StatusOK, "começando")
	o = dado[ordemResp](t, r)
	if o.Status != "em_andamento" || o.IniciadaEm == nil || fmt.Sprint(o.Acoes) != "[complete cancel]" {
		t.Fatalf("em andamento: %s %v %v", o.Status, o.IniciadaEm, o.Acoes)
	}
	r = a.chamar(t, http.MethodPost, "/maintenance-orders/"+o.ID.String()+"/start", token, nil)
	exigirErro(t, r, http.StatusConflict, "INVALID_STATE_TRANSITION", "começando duas vezes")
	if fmt.Sprint(r.detalhes(t)["allowed"]) != "[complete cancel]" {
		t.Fatalf("details.allowed: %s", r.Corpo)
	}

	// Concluir com o custo: o bloqueio (que ainda não começou) sai inteiro, e a
	// avaria vira `consertado`.
	r = a.chamar(t, http.MethodPost, "/maintenance-orders/"+o.ID.String()+"/complete", token, map[string]any{"cost_cents": 35000})
	exigir(t, r, http.StatusOK, "concluindo")
	o = dado[ordemResp](t, r)
	if o.Status != "concluida" || o.FechadaEm == nil || o.FechadaPor == nil || *o.FechadaPor != autor ||
		o.CustoCents == nil || *o.CustoCents != 35000 || len(o.Acoes) != 0 || o.Editavel != "so_custo" {
		t.Fatalf("concluída: %+v", o)
	}
	if o.Bloqueio == nil || o.Bloqueio.Status != "cancelled" || o.Bloqueio.Fase != "liberado" || o.Bloqueio.ID != b.ID {
		t.Fatalf("o bloqueio que não começou é liberado inteiro (a linha fica): %+v", o.Bloqueio)
	}
	if !a.vendavel(t, unidade, d.AddDays(2), d.AddDays(5)) {
		t.Fatal("o bloqueio liberado continua tirando as noites da venda")
	}
	if o.Avaria == nil || textoDe(o.Avaria.Desfecho) != "consertado" {
		t.Fatalf("a avaria de origem é consertada: %+v", o.Avaria)
	}
	var resolvidaPor uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT resolved_by FROM inventory_issues WHERE id = $1 AND resolved_at IS NOT NULL`, avaria).
		Scan(&resolvidaPor); err != nil || resolvidaPor != autor {
		t.Fatalf("resolved_by/resolved_at da avaria: %s %v", resolvidaPor, err)
	}

	// Concluída: só o custo — inclusive apagá-lo.
	caminho := "/maintenance-orders/" + o.ID.String()
	r = a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"cost_cents": 41000})
	exigir(t, r, http.StatusOK, "lançando o custo depois")
	if c := dado[ordemResp](t, r).CustoCents; c == nil || *c != 41000 {
		t.Fatalf("custo depois: %v", c)
	}
	r = a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"cost_cents": nil})
	exigir(t, r, http.StatusOK, "apagando o custo")
	if c := dado[ordemResp](t, r).CustoCents; c != nil {
		t.Fatalf("`cost_cents: null` apaga o custo, ficou %d", *c)
	}
	for nome, corpo := range map[string]map[string]any{
		"título":           {"title": "outro"},
		"descrição nula":   {"description": nil},
		"custo e título":   {"cost_cents": 100, "title": "x"},
		"prioridade igual": {"priority": "alta"},
	} {
		r = a.chamar(t, http.MethodPatch, caminho, token, corpo)
		exigirErro(t, r, http.StatusConflict, "MAINTENANCE_ORDER_CLOSED", "PATCH de "+nome+" na concluída")
		det := r.detalhes(t)
		if det["editable"] != "so_custo" || det["status"] != "concluida" || det["closed_at"] == nil {
			t.Fatalf("details do 409 (%s): %s", nome, r.Corpo)
		}
	}
	exigir(t, a.chamar(t, http.MethodPatch, caminho, token, map[string]any{}), http.StatusOK, "PATCH vazio na concluída")
	exigirErro(t, a.chamar(t, http.MethodPut, caminho, token, map[string]any{"title": "x", "priority": "normal"}),
		http.StatusConflict, "MAINTENANCE_ORDER_CLOSED", "PUT na concluída")
	exigirErro(t, a.chamar(t, http.MethodPut, caminho+"/block", token, periodo(d.AddDays(1), d.AddDays(2))),
		http.StatusConflict, "MAINTENANCE_ORDER_CLOSED", "bloquear a concluída")
	exigirErro(t, a.chamar(t, http.MethodDelete, caminho+"/block", token, nil),
		http.StatusConflict, "MAINTENANCE_ORDER_CLOSED", "soltar o bloqueio da concluída")
	exigirErro(t, a.chamar(t, http.MethodDelete, caminho, token, nil),
		http.StatusConflict, "MAINTENANCE_ORDER_CLOSED", "cancelar a concluída")
	exigirErro(t, a.chamar(t, http.MethodPost, caminho+"/complete", token, nil),
		http.StatusConflict, "MAINTENANCE_ORDER_CLOSED", "concluir de novo")

	// A trilha: a ordem, o bloqueio e a avaria.
	for acao, entidade := range map[string]uuid.UUID{
		"maintenance_orders.criado":    o.ID,
		"maintenance_orders.iniciada":  o.ID,
		"maintenance_orders.concluida": o.ID,
		"stay_blocks.bloqueada":        b.ID,
		"stay_blocks.desbloqueada":     b.ID,
		"inventory_issues.resolvida":   avaria,
	} {
		if n := a.contarTrilha(t, acao, entidade); n != 1 {
			t.Errorf("audit_log %s: %d linha(s), esperado 1", acao, n)
		}
	}
	if n := a.contarTrilha(t, "maintenance_orders.alterado", o.ID); n != 2 {
		t.Errorf("os dois PATCH do custo deixam duas linhas de alteração, vieram %d", n)
	}
}

// Liberar ao encerrar, nos três casos da tabela do contrato — e a noite de
// HOJE volta à venda quando o bloqueio estava em curso.
func TestLiberacaoAoEncerrarNosTresCasos(t *testing.T) {
	a := subir(t)
	token, _ := a.gestor(t)
	d := a.hoje(t)

	abrirComBloqueio := func(t *testing.T) (ordemResp, uuid.UUID) {
		t.Helper()
		unidade, _ := a.unidade(t)
		o := a.abrir(t, token, map[string]any{"unit_id": unidade, "title": "Pintura", "block": periodo(d.AddDays(1), d.AddDays(4))})
		return o, unidade
	}

	t.Run("não começou: liberado inteiro (cancelar)", func(t *testing.T) {
		o, unidade := abrirComBloqueio(t)
		r := a.chamar(t, http.MethodDelete, "/maintenance-orders/"+o.ID.String(), token, nil)
		exigir(t, r, http.StatusOK, "cancelando")
		c := dado[ordemResp](t, r)
		if c.Status != "cancelada" || c.FechadaEm == nil || c.Editavel != "nada" || c.Bloqueio == nil ||
			c.Bloqueio.Status != "cancelled" || c.Bloqueio.Fase != "liberado" {
			t.Fatalf("cancelada: %+v %+v", c, c.Bloqueio)
		}
		if !a.vendavel(t, unidade, d.AddDays(1), d.AddDays(4)) {
			t.Fatal("as noites do bloqueio liberado não voltaram à venda")
		}
		exigirErro(t, a.chamar(t, http.MethodPatch, "/maintenance-orders/"+o.ID.String(), token, map[string]any{"cost_cents": 100}),
			http.StatusConflict, "MAINTENANCE_ORDER_CLOSED", "custo na cancelada")
		exigirErro(t, a.chamar(t, http.MethodDelete, "/maintenance-orders/"+o.ID.String(), token, nil),
			http.StatusConflict, "MAINTENANCE_ORDER_CLOSED", "cancelar duas vezes")
	})

	t.Run("em curso: o fim é cortado para hoje (concluir)", func(t *testing.T) {
		o, unidade := abrirComBloqueio(t)
		a.moverBloqueio(t, o.Bloqueio.ID, d.AddDays(-2), d.AddDays(3))
		if a.vendavel(t, unidade, d, d.AddDays(1)) {
			t.Fatal("antes de concluir, a noite de hoje está bloqueada")
		}
		r := a.chamar(t, http.MethodPost, "/maintenance-orders/"+o.ID.String()+"/complete", token, nil)
		exigir(t, r, http.StatusOK, "concluindo")
		c := dado[ordemResp](t, r)
		if c.Bloqueio == nil || c.Bloqueio.De != d.AddDays(-2).String() || c.Bloqueio.Ate != d.String() ||
			c.Bloqueio.Status != "confirmed" || c.Bloqueio.Fase != "encerrado" || c.Bloqueio.Noites != 2 {
			t.Fatalf("o bloqueio em curso termina hoje: %+v", c.Bloqueio)
		}
		if c.CustoCents != nil {
			t.Fatalf("concluir sem corpo não mexe no custo: %d", *c.CustoCents)
		}
		if !a.vendavel(t, unidade, d, d.AddDays(3)) {
			t.Fatal("a noite de hoje (e as seguintes) não voltaram à venda")
		}
		if a.vendavel(t, unidade, d.AddDays(-1), d) {
			t.Fatal("a noite de ontem já passou bloqueada: é história, não muda")
		}
	})

	t.Run("terminado: fica como está (cancelar)", func(t *testing.T) {
		o, _ := abrirComBloqueio(t)
		a.moverBloqueio(t, o.Bloqueio.ID, d.AddDays(-5), d.AddDays(-2))
		r := a.chamar(t, http.MethodDelete, "/maintenance-orders/"+o.ID.String(), token, nil)
		exigir(t, r, http.StatusOK, "cancelando")
		c := dado[ordemResp](t, r)
		if c.Bloqueio == nil || c.Bloqueio.De != d.AddDays(-5).String() || c.Bloqueio.Ate != d.AddDays(-2).String() ||
			c.Bloqueio.Status != "confirmed" || c.Bloqueio.Fase != "encerrado" {
			t.Fatalf("o bloqueio terminado não muda: %+v", c.Bloqueio)
		}
		if n := a.contarTrilha(t, "stay_blocks.desbloqueada", o.Bloqueio.ID) + a.contarTrilha(t, "stay_blocks.encurtada", o.Bloqueio.ID); n != 0 {
			t.Fatalf("nada mudou no bloqueio terminado, e a trilha registrou %d mudança(s)", n)
		}
	})

	t.Run("soltar sem encerrar, e bloquear de novo cria linha nova", func(t *testing.T) {
		o, unidade := abrirComBloqueio(t)
		antigo := o.Bloqueio.ID
		a.moverBloqueio(t, antigo, d.AddDays(-2), d.AddDays(3))
		caminho := "/maintenance-orders/" + o.ID.String() + "/block"

		r := a.chamar(t, http.MethodDelete, caminho, token, nil)
		exigir(t, r, http.StatusOK, "soltando")
		s := dado[ordemResp](t, r)
		if s.Status != "aberta" || s.Bloqueio == nil || s.Bloqueio.Ate != d.String() || s.Bloqueio.Fase != "encerrado" {
			t.Fatalf("soltar corta o fim para hoje e a ordem segue aberta: %s %+v", s.Status, s.Bloqueio)
		}
		exigir(t, a.chamar(t, http.MethodDelete, caminho, token, nil), http.StatusOK, "soltar duas vezes")
		if !a.vendavel(t, unidade, d, d.AddDays(1)) {
			t.Fatal("soltar devolveu a noite de hoje à venda")
		}

		r = a.chamar(t, http.MethodPut, caminho, token, periodo(d, d.AddDays(2)))
		exigir(t, r, http.StatusOK, "bloqueando de novo")
		n := dado[ordemResp](t, r)
		if n.Bloqueio == nil || n.Bloqueio.ID == antigo || n.Bloqueio.De != d.String() || n.Bloqueio.Fase != "em_curso" {
			t.Fatalf("o bloqueio terminado não se reabre: linha nova, %+v", n.Bloqueio)
		}
		var status, ate string
		if err := a.pool.QueryRow(a.ctx, `SELECT status, upper(period)::text FROM stay_blocks WHERE id = $1`, antigo).
			Scan(&status, &ate); err != nil || status != "confirmed" || ate != d.String() {
			t.Fatalf("a linha antiga fica como história: %s %s %v", status, ate, err)
		}
	})
}

// PUT /block: criar, repetir (nada), estender, conflitar (nada muda), os tetos,
// o em curso que só mexe no fim, e a unidade inativa.
func TestRemarcacaoDoBloqueio(t *testing.T) {
	a := subir(t)
	token, _ := a.gestor(t)
	unidade, codigo := a.unidade(t)
	d := a.hoje(t)

	o := a.abrir(t, token, map[string]any{"unit_id": unidade, "title": "Troca do piso"})
	if o.Bloqueio != nil {
		t.Fatalf("sem `block`, a ordem nasce sem bloqueio: %+v", o.Bloqueio)
	}
	caminho := "/maintenance-orders/" + o.ID.String() + "/block"

	r := a.chamar(t, http.MethodPut, caminho, token, periodo(d.AddDays(-1), d.AddDays(2)))
	exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "bloqueio novo começando ontem")
	if r.detalhes(t)["from"] == nil {
		t.Fatalf("o erro de período sai em details.from: %s", r.Corpo)
	}

	r = a.chamar(t, http.MethodPut, caminho, token, periodo(d, d.AddDays(3)))
	exigir(t, r, http.StatusOK, "criando o bloqueio")
	b := dado[ordemResp](t, r).Bloqueio
	if b == nil || b.Fase != "em_curso" || b.Noites != 3 {
		t.Fatalf("bloqueio que começa hoje está em curso: %+v", b)
	}
	r = a.chamar(t, http.MethodPut, caminho, token, periodo(d, d.AddDays(3)))
	exigir(t, r, http.StatusOK, "segundo toque")
	if dado[ordemResp](t, r).Bloqueio.ID != b.ID {
		t.Fatal("o mesmo período não cria linha")
	}
	r = a.chamar(t, http.MethodPut, caminho, token, periodo(d, d.AddDays(6)))
	exigir(t, r, http.StatusOK, "estendendo")
	if e := dado[ordemResp](t, r).Bloqueio; e.ID != b.ID || e.Noites != 6 {
		t.Fatalf("estender altera a mesma linha: %+v", e)
	}

	// Outra ordem ocupa [D+10, D+12): estender sobre ela é 409, e nada muda.
	a.abrir(t, token, map[string]any{"unit_id": unidade, "title": "Dedetização", "block": periodo(d.AddDays(10), d.AddDays(12))})
	r = a.chamar(t, http.MethodPut, caminho, token, periodo(d, d.AddDays(11)))
	exigirErro(t, r, http.StatusConflict, "DATE_CONFLICT", "estendendo sobre outra ocupação")
	det := r.detalhes(t)
	if det["unit_code"] != codigo || det["period"] != fmt.Sprintf("[%s, %s)", d, d.AddDays(11)) {
		t.Fatalf("details do DATE_CONFLICT: %s", r.Corpo)
	}
	atual := dado[ordemResp](t, a.chamar(t, http.MethodGet, "/maintenance-orders/"+o.ID.String(), token, nil)).Bloqueio
	if atual.Ate != d.AddDays(6).String() {
		t.Fatalf("o conflito não mexe no bloqueio: %+v", atual)
	}

	// Os tetos de `POST /blocks`.
	r = a.chamar(t, http.MethodPut, caminho, token, periodo(d, d.AddDays(366)))
	exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "366 noites")
	if r.detalhes(t)["to"] == nil {
		t.Fatalf("o teto de duração sai em details.to: %s", r.Corpo)
	}

	// Em curso: o início não muda, o fim não volta para antes de hoje, e `to`
	// = hoje solta a noite de hoje.
	a.moverBloqueio(t, b.ID, d.AddDays(-2), d.AddDays(6))
	exigirErro(t, a.chamar(t, http.MethodPut, caminho, token, periodo(d.AddDays(-1), d.AddDays(6))),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "mudar o início do bloqueio em curso")
	exigirErro(t, a.chamar(t, http.MethodPut, caminho, token, periodo(d.AddDays(-2), d.AddDays(-1))),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "fim antes de hoje")
	r = a.chamar(t, http.MethodPut, caminho, token, periodo(d.AddDays(-2), d))
	exigir(t, r, http.StatusOK, "encurtando para hoje")
	if e := dado[ordemResp](t, r).Bloqueio; e.ID != b.ID || e.Ate != d.String() || e.Fase != "encerrado" {
		t.Fatalf("encurtar até hoje: %+v", e)
	}
	if !a.vendavel(t, unidade, d, d.AddDays(1)) {
		t.Fatal("`to` = hoje devolve a noite de hoje à venda")
	}

	// Criar bloqueio em unidade inativa é 422 em `block`.
	inativa, _ := a.unidade(t)
	semBloqueio := a.abrir(t, token, map[string]any{"unit_id": inativa, "title": "Reforma"})
	if _, err := a.pool.Exec(a.ctx, `UPDATE units SET active = false WHERE id = $1`, inativa); err != nil {
		t.Fatal(err)
	}
	r = a.chamar(t, http.MethodPut, "/maintenance-orders/"+semBloqueio.ID.String()+"/block", token, periodo(d, d.AddDays(1)))
	exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "bloquear unidade inativa")
	if r.detalhes(t)["block"] == nil {
		t.Fatalf("unidade inativa sai em details.block: %s", r.Corpo)
	}

	if n := a.contarTrilha(t, "stay_blocks.remarcada", b.ID); n != 2 {
		t.Errorf("estender e encurtar deixam duas linhas `remarcada`, vieram %d", n)
	}
}

// Cancelar não toca a avaria, e ela aceita uma ordem nova (retrabalho);
// concluir conserta só a avaria AINDA ABERTA.
func TestAvariaConsertadaAoConcluirEIntactaAoCancelar(t *testing.T) {
	a := subir(t)
	token, _ := a.gestor(t)
	unidade, _ := a.unidade(t)
	cozinha := a.comodo(t, unidade, "Cozinha")
	geladeira := a.bem(t, "Geladeira")
	avaria := a.avaria(t, cozinha, geladeira)

	primeira := a.abrir(t, token, map[string]any{"unit_id": unidade, "issue_id": avaria, "title": "Geladeira"})
	exigir(t, a.chamar(t, http.MethodDelete, "/maintenance-orders/"+primeira.ID.String(), token, nil), http.StatusOK, "cancelando")
	var desfecho *string
	if err := a.pool.QueryRow(a.ctx, `SELECT resolution FROM inventory_issues WHERE id = $1`, avaria).Scan(&desfecho); err != nil || desfecho != nil {
		t.Fatalf("cancelar não mexe na avaria: %s %v", textoDe(desfecho), err)
	}

	segunda := a.abrir(t, token, map[string]any{"unit_id": unidade, "issue_id": avaria, "title": "Geladeira, de novo"})
	exigir(t, a.chamar(t, http.MethodPost, "/maintenance-orders/"+segunda.ID.String()+"/complete", token, nil), http.StatusOK, "concluindo")
	if err := a.pool.QueryRow(a.ctx, `SELECT resolution FROM inventory_issues WHERE id = $1`, avaria).Scan(&desfecho); err != nil ||
		textoDe(desfecho) != "consertado" {
		t.Fatalf("concluir conserta a avaria: %s %v", textoDe(desfecho), err)
	}

	// Avaria que uma pessoa já resolveu de outro jeito não é tocada.
	outra := a.avaria(t, cozinha, geladeira)
	terceira := a.abrir(t, token, map[string]any{"unit_id": unidade, "issue_id": outra, "title": "Porta da geladeira"})
	if _, err := a.pool.Exec(a.ctx, `UPDATE inventory_issues SET resolution = 'reposto', resolved_at = now() WHERE id = $1`, outra); err != nil {
		t.Fatal(err)
	}
	r := a.chamar(t, http.MethodPost, "/maintenance-orders/"+terceira.ID.String()+"/complete", token, nil)
	exigir(t, r, http.StatusOK, "concluindo sobre avaria já resolvida")
	if d := dado[ordemResp](t, r).Avaria; d == nil || textoDe(d.Desfecho) != "reposto" {
		t.Fatalf("o desfecho escolhido por uma pessoa vence: %+v", d)
	}
	if n := a.contarTrilha(t, "inventory_issues.resolvida", outra); n != 0 {
		t.Fatalf("avaria intocada não ganha trilha de resolução (%d)", n)
	}
}

// Unidade, cômodo, bem e avaria têm de concordar; o formulário erra em
// português e no campo certo.
func TestCoerenciaDaOrdemComUnidadeComodoBemEAvaria(t *testing.T) {
	a := subir(t)
	token, _ := a.gestor(t)
	unidade, _ := a.unidade(t)
	outraUnidade, _ := a.unidade(t)
	sala := a.comodo(t, unidade, "Sala")
	salaDaOutra := a.comodo(t, outraUnidade, "Sala")
	tv := a.bem(t, "TV")
	sofa := a.bem(t, "Sofá")
	avaria := a.avaria(t, sala, tv)
	avariaDaOutra := a.avaria(t, salaDaOutra, tv)
	d := a.hoje(t)

	for _, c := range []struct {
		nome  string
		corpo map[string]any
		campo string
	}{
		{"unidade inexistente", map[string]any{"unit_id": uuid.New(), "title": "x"}, "unit_id"},
		{"sem unidade", map[string]any{"title": "x"}, "unit_id"},
		{"cômodo de outra unidade", map[string]any{"unit_id": unidade, "room_id": salaDaOutra, "title": "x"}, "room_id"},
		{"bem inexistente", map[string]any{"unit_id": unidade, "item_id": uuid.New(), "title": "x"}, "item_id"},
		{"avaria de outra unidade", map[string]any{"unit_id": unidade, "issue_id": avariaDaOutra, "title": "x"}, "issue_id"},
		{"avaria com outro bem", map[string]any{"unit_id": unidade, "issue_id": avaria, "item_id": sofa, "title": "x"}, "item_id"},
		{"título em branco", map[string]any{"unit_id": unidade, "title": "   "}, "title"},
		{"custo zero", map[string]any{"unit_id": unidade, "title": "x", "cost_cents": 0}, "cost_cents"},
		{"prioridade fora", map[string]any{"unit_id": unidade, "title": "x", "priority": "altissima"}, "priority"},
		{"bloqueio começando ontem", map[string]any{"unit_id": unidade, "title": "x", "block": periodo(d.AddDays(-1), d.AddDays(1))}, "block.from"},
		{"bloqueio além do horizonte", map[string]any{"unit_id": unidade, "title": "x", "block": periodo(d.AddDays(3*365+1), d.AddDays(3*365+2))}, "block.from"},
		{"bloqueio sem fim", map[string]any{"unit_id": unidade, "title": "x", "block": map[string]any{"from": d.String()}}, "block.to"},
		{"campo desconhecido", map[string]any{"unit_id": unidade, "title": "x", "status": "concluida"}, "status"},
	} {
		r := a.chamar(t, http.MethodPost, "/maintenance-orders", token, c.corpo)
		exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", c.nome)
		if r.detalhes(t)[c.campo] == nil {
			t.Errorf("%s: o erro tem de sair em details[%q]: %s", c.nome, c.campo, r.Corpo)
		}
	}
	var n int
	if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM maintenance_orders WHERE unit_id = ANY($1)`,
		[]uuid.UUID{unidade, outraUnidade}).Scan(&n); err != nil || n != 0 {
		t.Fatalf("nenhuma recusa deixa ordem: %d %v", n, err)
	}

	// Avaria com o mesmo cômodo e bem passa; PUT mantém o par da avaria.
	o := a.abrir(t, token, map[string]any{"unit_id": unidade, "issue_id": avaria, "room_id": sala, "item_id": tv, "title": "TV"})
	caminho := "/maintenance-orders/" + o.ID.String()
	r := a.chamar(t, http.MethodPut, caminho, token, map[string]any{"title": "TV sem imagem", "priority": "urgente"})
	exigir(t, r, http.StatusOK, "PUT sem cômodo e bem numa ordem de avaria")
	if s := dado[ordemResp](t, r); idTexto(s.ComodoID) != sala.String() || idTexto(s.BemID) != tv.String() || s.Prioridade != "urgente" {
		t.Fatalf("omitidos ficam os da avaria: %+v", s)
	}
	exigirErro(t, a.chamar(t, http.MethodPut, caminho, token, map[string]any{"title": "x", "priority": "normal", "room_id": nil}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "PUT tirando o cômodo da ordem de avaria")
	exigirErro(t, a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"item_id": sofa}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "PATCH trocando o bem da ordem de avaria")
	exigirErro(t, a.chamar(t, http.MethodPut, caminho, token, map[string]any{"title": "x", "priority": "normal", "unit_id": outraUnidade}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "PUT mudando a unidade (campo fora do corpo)")

	// Sem avaria: PUT omitido limpa; PATCH ausente mantém e null limpa.
	livre := a.abrir(t, token, map[string]any{"unit_id": unidade, "room_id": sala, "item_id": sofa, "title": "Sofá rasgado",
		"description": "  costura  ", "cost_cents": 9000})
	if textoDe(livre.Descricao) != "costura" || livre.Prioridade != "normal" {
		t.Fatalf("descrição aparada e prioridade padrão: %+v", livre)
	}
	caminho = "/maintenance-orders/" + livre.ID.String()
	r = a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"title": "Sofá rasgado no braço"})
	exigir(t, r, http.StatusOK, "PATCH só do título")
	if p := dado[ordemResp](t, r); idTexto(p.ComodoID) != sala.String() || p.CustoCents == nil || textoDe(p.Descricao) != "costura" {
		t.Fatalf("PATCH não mexe no que não veio: %+v", p)
	}
	r = a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"room_id": nil, "description": nil})
	exigir(t, r, http.StatusOK, "PATCH limpando")
	if p := dado[ordemResp](t, r); p.ComodoID != nil || p.Descricao != nil || idTexto(p.BemID) != sofa.String() {
		t.Fatalf("null limpa só o que veio: %+v", p)
	}
	exigirErro(t, a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"title": nil}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "título null")
	r = a.chamar(t, http.MethodPut, caminho, token, map[string]any{"title": "Sofá", "priority": "baixa"})
	exigir(t, r, http.StatusOK, "PUT mínimo")
	if p := dado[ordemResp](t, r); p.BemID != nil || p.CustoCents != nil || p.Descricao != nil {
		t.Fatalf("no PUT, anulável omitido vira null: %+v", p)
	}
	exigirErro(t, a.chamar(t, http.MethodGet, "/maintenance-orders/"+uuid.NewString(), token, nil),
		http.StatusNotFound, "NOT_FOUND", "ordem inexistente")
	exigirErro(t, a.chamar(t, http.MethodGet, "/maintenance-orders/nao-e-uuid", token, nil),
		http.StatusNotFound, "NOT_FOUND", "id malformado")
}

// A lista de trabalho: abertas primeiro, da mais urgente; na mesma prioridade,
// a mais antiga; encerradas depois, da mais recente. Filtros fechados.
func TestListaDeTrabalho(t *testing.T) {
	a := subir(t)
	token, _ := a.gestor(t)
	unidade, _ := a.unidade(t)

	baixa := a.abrir(t, token, map[string]any{"unit_id": unidade, "title": "Trocar lâmpada", "priority": "baixa"})
	urgente := a.abrir(t, token, map[string]any{"unit_id": unidade, "title": "Vazamento no banheiro", "priority": "urgente"})
	normalAntiga := a.abrir(t, token, map[string]any{"unit_id": unidade, "title": "Porta rangendo"})
	normalNova := a.abrir(t, token, map[string]any{"unit_id": unidade, "title": "Torneira 100% pingando", "description": "cozinha"})
	concluidaAntes := a.abrir(t, token, map[string]any{"unit_id": unidade, "title": "Chuveiro", "priority": "urgente"})
	concluidaDepois := a.abrir(t, token, map[string]any{"unit_id": unidade, "title": "Fechadura"})
	exigir(t, a.chamar(t, http.MethodPost, "/maintenance-orders/"+concluidaAntes.ID.String()+"/complete", token, nil), http.StatusOK, "concluindo")
	exigir(t, a.chamar(t, http.MethodPost, "/maintenance-orders/"+urgente.ID.String()+"/start", token, nil), http.StatusOK, "começando")
	// As instantes vêm do banco; a ordem dos encerramentos tem de ser visível.
	if _, err := a.pool.Exec(a.ctx, `UPDATE maintenance_orders SET closed_at = closed_at - interval '1 hour' WHERE id = $1`, concluidaAntes.ID); err != nil {
		t.Fatal(err)
	}
	exigir(t, a.chamar(t, http.MethodDelete, "/maintenance-orders/"+concluidaDepois.ID.String(), token, nil), http.StatusOK, "cancelando")
	if _, err := a.pool.Exec(a.ctx, `UPDATE maintenance_orders SET opened_at = opened_at - interval '1 hour' WHERE id = $1`, normalAntiga.ID); err != nil {
		t.Fatal(err)
	}

	listar := func(t *testing.T, consulta string) []uuid.UUID {
		t.Helper()
		r := a.chamar(t, http.MethodGet, "/maintenance-orders?unit_id="+unidade.String()+consulta, token, nil)
		exigir(t, r, http.StatusOK, "listando "+consulta)
		itens, m := lista[ordemResp](t, r)
		if m.Page != 1 || (m.PerPage == 25 && int(m.Total) != len(itens)) {
			t.Fatalf("meta %+v com %d itens", m, len(itens))
		}
		ids := make([]uuid.UUID, len(itens))
		for i, o := range itens {
			ids[i] = o.ID
		}
		return ids
	}

	esperado := []uuid.UUID{urgente.ID, normalAntiga.ID, normalNova.ID, baixa.ID, concluidaDepois.ID, concluidaAntes.ID}
	if got := listar(t, ""); !slices.Equal(got, esperado) {
		t.Fatalf("ordem de urgência:\n got %v\nwant %v", got, esperado)
	}
	if got := listar(t, "&open=true"); !slices.Equal(got, esperado[:4]) {
		t.Fatalf("open=true: %v", got)
	}
	if got := listar(t, "&open=false&sort=-closed_at"); !slices.Equal(got, esperado[4:]) {
		t.Fatalf("encerradas, da mais recente: %v", got)
	}
	if got := listar(t, "&status=em_andamento"); !slices.Equal(got, []uuid.UUID{urgente.ID}) {
		t.Fatalf("status=em_andamento: %v", got)
	}
	if got := listar(t, "&priority=urgente&open=true"); !slices.Equal(got, []uuid.UUID{urgente.ID}) {
		t.Fatalf("priority=urgente E open=true: %v", got)
	}
	if got := listar(t, "&q="+url.QueryEscape("100%")); !slices.Equal(got, []uuid.UUID{normalNova.ID}) {
		t.Fatalf("q=100%% procura o texto: %v", got)
	}
	if got := listar(t, "&q=COZINHA"); !slices.Equal(got, []uuid.UUID{normalNova.ID}) {
		t.Fatalf("q na descrição, sem caixa: %v", got)
	}
	if got := listar(t, "&sort=opened_at&per_page=2"); !slices.Equal(got, []uuid.UUID{normalAntiga.ID, baixa.ID}) {
		t.Fatalf("sort=opened_at paginado: %v", got)
	}
	r := a.chamar(t, http.MethodGet, "/maintenance-orders?unit_id="+unidade.String()+"&page=9", token, nil)
	exigir(t, r, http.StatusOK, "página além do fim")
	if itens, m := lista[ordemResp](t, r); len(itens) != 0 || m.Total != 6 {
		t.Fatalf("página além do fim: %d itens, total %d", len(itens), m.Total)
	}

	for _, consulta := range []string{"sort=prioridade", "status=fechada", "priority=altissima", "unit_id=xyz", "issue_id=1", "open=sim"} {
		exigirErro(t, a.chamar(t, http.MethodGet, "/maintenance-orders?"+consulta, token, nil),
			http.StatusUnprocessableEntity, "VALIDATION_ERROR", consulta)
	}
}

// O corretor não tem `maintenance` (a ordem bloquearia o calendário pela porta
// dos fundos do `calendar` em `own`): 403 em toda rota. Quem só vê, não abre.
func TestCorretorNaoAlcancaAsOrdens(t *testing.T) {
	a := subir(t)
	gestor, _ := a.gestor(t)
	unidade, _ := a.unidade(t)
	o := a.abrir(t, gestor, map[string]any{"unit_id": unidade, "title": "Portão"})

	corretor, _ := a.sessao(t, a.perfilDoSeed(t, "corretor"))
	caminho := "/maintenance-orders/" + o.ID.String()
	for _, c := range []struct {
		metodo, caminho string
		corpo           any
	}{
		{http.MethodGet, "/maintenance-orders", nil},
		{http.MethodPost, "/maintenance-orders", map[string]any{"unit_id": unidade, "title": "x"}},
		{http.MethodGet, caminho, nil},
		{http.MethodPut, caminho, map[string]any{"title": "x", "priority": "normal"}},
		{http.MethodPatch, caminho, map[string]any{"title": "x"}},
		{http.MethodDelete, caminho, nil},
		{http.MethodPost, caminho + "/start", nil},
		{http.MethodPost, caminho + "/complete", nil},
		{http.MethodPut, caminho + "/block", map[string]any{"from": "2030-01-01", "to": "2030-01-02"}},
		{http.MethodDelete, caminho + "/block", nil},
	} {
		exigirErro(t, a.chamar(t, c.metodo, c.caminho, corretor, c.corpo), http.StatusForbidden, "FORBIDDEN",
			"corretor em "+c.metodo+" "+c.caminho)
	}

	leitor, _ := a.sessao(t, a.perfil(t, "ver"))
	exigir(t, a.chamar(t, http.MethodGet, caminho, leitor, nil), http.StatusOK, "leitor vê")
	exigirErro(t, a.chamar(t, http.MethodPost, "/maintenance-orders", leitor, map[string]any{"unit_id": unidade, "title": "x"}),
		http.StatusForbidden, "FORBIDDEN", "leitor abrindo")
	exigirErro(t, a.chamar(t, http.MethodDelete, caminho, leitor, nil), http.StatusForbidden, "FORBIDDEN", "leitor cancelando")
}

// O calendário não solta o que é da ordem: nem o bloqueio que ela cita, nem o
// ANTIGO de uma remarcação — terminado, sem ordem que o cite, mas histórico do
// conserto ("noite que já passou não muda").
func TestBloqueioDaOrdemENaoSeSoltaPeloCalendario(t *testing.T) {
	a := subir(t)
	token, _ := a.gestor(t)
	calendario, _ := a.sessao(t, a.perfilDe(t, "calendar", "excluir"))
	unidade, _ := a.unidade(t)
	d := a.hoje(t)

	o := a.abrir(t, token, map[string]any{"unit_id": unidade, "title": "Telhado", "block": periodo(d.AddDays(1), d.AddDays(3))})
	antigo := o.Bloqueio.ID
	a.moverBloqueio(t, antigo, d.AddDays(-5), d.AddDays(-2))
	r := a.chamar(t, http.MethodPut, "/maintenance-orders/"+o.ID.String()+"/block", token, periodo(d, d.AddDays(2)))
	exigir(t, r, http.StatusOK, "remarcando o bloqueio terminado")
	novo := dado[ordemResp](t, r).Bloqueio.ID
	if novo == antigo {
		t.Fatal("remarcar o terminado cria linha nova")
	}

	r = a.chamar(t, http.MethodDelete, "/blocks/"+antigo.String(), calendario, nil)
	exigirErro(t, r, http.StatusConflict, "INVALID_STATE_TRANSITION", "soltando o antigo da remarcação pelo calendário")
	if p := r.detalhes(t)["period"]; p != fmt.Sprintf("[%s, %s)", d.AddDays(-5), d.AddDays(-2)) {
		t.Fatalf("details.period: %s", r.Corpo)
	}
	r = a.chamar(t, http.MethodDelete, "/blocks/"+novo.String(), calendario, nil)
	exigirErro(t, r, http.StatusConflict, "INVALID_STATE_TRANSITION", "soltando o bloqueio da ordem pelo calendário")
	if r.detalhes(t)["maintenance_order_id"] != o.ID.String() {
		t.Fatalf("details.maintenance_order_id: %s", r.Corpo)
	}
	var status string
	if err := a.pool.QueryRow(a.ctx, `SELECT string_agg(status, ',' ORDER BY lower(period)) FROM stay_blocks WHERE id = ANY($1)`,
		[]uuid.UUID{antigo, novo}).Scan(&status); err != nil || status != "confirmed,confirmed" {
		t.Fatalf("os dois bloqueios continuam confirmed: %s %v", status, err)
	}
}
