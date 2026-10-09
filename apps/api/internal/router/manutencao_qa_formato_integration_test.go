//go:build integration

package router

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ─────────────────────────── 4. Imutabilidade ───────────────────────────────

// qmFoto é o que a imutabilidade confere no banco: a ordem e o bloqueio dela.
type qmFoto struct {
	Status, Titulo, Prioridade, Descricao, Custo string
	BloqueioStatus, BloqueioPeriodo              string
	Atualizada                                   string
}

func (a *ambiente) qmFotografar(t *testing.T, id uuid.UUID) qmFoto {
	t.Helper()
	var f qmFoto
	if err := a.pool.QueryRow(a.ctx, `
		SELECT mo.status, mo.title, mo.priority, coalesce(mo.description, '∅'), coalesce(mo.cost_cents::text, '∅'),
		       coalesce(sb.status, '∅'), coalesce(sb.period::text, '∅'), mo.updated_at::text
		  FROM maintenance_orders mo LEFT JOIN stay_blocks sb ON sb.id = mo.stay_block_id
		 WHERE mo.id = $1`, id).Scan(&f.Status, &f.Titulo, &f.Prioridade, &f.Descricao, &f.Custo,
		&f.BloqueioStatus, &f.BloqueioPeriodo, &f.Atualizada); err != nil {
		t.Fatalf("fotografando a ordem: %v", err)
	}
	return f
}

// Em linguagem de negócio: ordem cancelada é desistência registrada — não
// aceita custo, não muda de título, não volta a bloquear a unidade, não
// "conclui" nem reabre. Cada tentativa recebe 409 MAINTENANCE_ORDER_CLOSED,
// dizendo o estado, que nada é editável e quando foi encerrada; e o banco fica
// exatamente como estava. Corpo vazio no PATCH é a única coisa que passa (200
// sem escrever), como o contrato manda.
func TestManutencaoQAOrdemCanceladaRecusaTudo(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "manut-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	a.qmFaxina(t, f)
	prop := a.qbPropriedadePadrao(t)
	d := a.qmHoje(t, prop)
	unidade, _ := f.qbUnidade(t, prop, "")
	tk := u.Token

	o := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "title": "Portão", "description": "range",
		"cost_cents": 1000, "block": qmPeriodo(d.AddDays(3), d.AddDays(5))})
	base := "/maintenance-orders/" + o.ID.String()
	c := qbDado[qmOrdem](t, a.chamar(t, http.MethodDelete, base, tk, nil), http.StatusOK, "cancelando")
	if c.Status != "cancelada" || c.Editavel != "nada" || len(c.Acoes) != 0 || c.FechadaEm == nil {
		t.Fatalf("cancelada: status %s, editable %s, ações %v, closed_at %v", c.Status, c.Editavel, c.Acoes, c.FechadaEm)
	}
	antes := a.qmFotografar(t, o.ID)

	for _, caso := range []struct {
		nome, metodo, caminho string
		corpo                 any
		editavel              bool // o 409 de edição traz details.editable
	}{
		{"PUT", http.MethodPut, base, map[string]any{"title": "Portão", "priority": "normal"}, true},
		{"PATCH do custo", http.MethodPatch, base, map[string]any{"cost_cents": 5000}, true},
		{"PATCH apagando o custo", http.MethodPatch, base, map[string]any{"cost_cents": nil}, true},
		{"PATCH do título", http.MethodPatch, base, map[string]any{"title": "Outro"}, true},
		{"PATCH da descrição nula", http.MethodPatch, base, map[string]any{"description": nil}, true},
		{"cancelar de novo", http.MethodDelete, base, nil, false},
		{"começar", http.MethodPost, base + "/start", nil, false},
		{"concluir", http.MethodPost, base + "/complete", nil, false},
		{"concluir com custo", http.MethodPost, base + "/complete", map[string]any{"cost_cents": 5000}, false},
		{"bloquear de novo", http.MethodPut, base + "/block", qmPeriodo(d.AddDays(3), d.AddDays(5)), false},
		{"soltar o bloqueio", http.MethodDelete, base + "/block", nil, false},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			e := qbErro(t, a.chamar(t, caso.metodo, caso.caminho, tk, caso.corpo), http.StatusConflict, "MAINTENANCE_ORDER_CLOSED", caso.nome+" na cancelada")
			if e.Details["status"] != "cancelada" || e.Details["closed_at"] == nil {
				t.Errorf("o 409 diz o estado e quando encerrou (details.status, details.closed_at): %v", e.Details)
			}
			if caso.editavel && e.Details["editable"] != "nada" {
				t.Errorf("o 409 de edição na cancelada diz details.editable = nada: %v", e.Details)
			}
		})
	}
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, base, tk, map[string]any{}), http.StatusOK, "PATCH vazio na cancelada")
	if depois := a.qmFotografar(t, o.ID); depois != antes {
		t.Fatalf("a ordem cancelada mudou:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// Em linguagem de negócio: a nota do encanador chega dias depois. A ordem
// concluída aceita lançar, corrigir e apagar o custo — e mais nada: começar de
// novo, "concluir" outra vez (gravando outro custo), cancelar, editar o título
// ou mexer no bloqueio são 409 MAINTENANCE_ORDER_CLOSED. Custo zero continua
// sendo erro de campo (422), não "encerrada".
func TestManutencaoQAOrdemConcluidaSoAceitaOCusto(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "manut-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	a.qmFaxina(t, f)
	prop := a.qbPropriedadePadrao(t)
	d := a.qmHoje(t, prop)
	unidade, _ := f.qbUnidade(t, prop, "")
	tk := u.Token

	o := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "title": "Lâmpada da varanda"})
	base := "/maintenance-orders/" + o.ID.String()
	// Concluir direto de `aberta`: trocar uma lâmpada não pede dois toques.
	c := qbDado[qmOrdem](t, a.chamar(t, http.MethodPost, base+"/complete", tk, nil), http.StatusOK, "concluindo direto de aberta")
	if c.Status != "concluida" || c.IniciadaEm != nil || c.Editavel != "so_custo" || len(c.Acoes) != 0 || c.Custo != nil {
		t.Fatalf("concluída direto: status %s started_at %v editable %s ações %v custo %v", c.Status, c.IniciadaEm, c.Editavel, c.Acoes, c.Custo)
	}

	if v := qbDado[qmOrdem](t, a.chamar(t, http.MethodPatch, base, tk, map[string]any{"cost_cents": 12345}), http.StatusOK, "lançando o custo").Custo; v == nil || *v != 12345 {
		t.Fatalf("custo lançado depois: %v", v)
	}
	if v := qbDado[qmOrdem](t, a.chamar(t, http.MethodPatch, base, tk, map[string]any{"cost_cents": nil}), http.StatusOK, "apagando o custo").Custo; v != nil {
		t.Fatalf("`cost_cents: null` apaga o custo da concluída; ficou %d", *v)
	}
	if e := qbErro(t, a.chamar(t, http.MethodPatch, base, tk, map[string]any{"cost_cents": 0}), http.StatusUnprocessableEntity, "VALIDATION_ERROR",
		"custo zero na concluída"); e.Details["cost_cents"] == nil {
		t.Errorf("o 422 aponta cost_cents: %v", e.Details)
	}
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, base, tk, map[string]any{"cost_cents": 9900}), http.StatusOK, "custo final")
	antes := a.qmFotografar(t, o.ID)

	for _, caso := range []struct {
		nome, metodo, caminho string
		corpo                 any
	}{
		{"PUT só com o custo", http.MethodPut, base, map[string]any{"title": "Lâmpada da varanda", "priority": "normal", "cost_cents": 9900}},
		{"PATCH da prioridade", http.MethodPatch, base, map[string]any{"priority": "baixa"}},
		{"PATCH do cômodo nulo", http.MethodPatch, base, map[string]any{"room_id": nil}},
		{"começar", http.MethodPost, base + "/start", nil},
		{"concluir de novo com outro custo", http.MethodPost, base + "/complete", map[string]any{"cost_cents": 1}},
		{"cancelar a concluída", http.MethodDelete, base, nil},
		{"bloquear", http.MethodPut, base + "/block", qmPeriodo(d.AddDays(1), d.AddDays(2))},
		{"soltar o bloqueio", http.MethodDelete, base + "/block", nil},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			e := qbErro(t, a.chamar(t, caso.metodo, caso.caminho, tk, caso.corpo), http.StatusConflict, "MAINTENANCE_ORDER_CLOSED", caso.nome+" na concluída")
			if e.Details["status"] != "concluida" {
				t.Errorf("details.status = %v, esperado concluida", e.Details["status"])
			}
		})
	}
	if depois := a.qmFotografar(t, o.ID); depois != antes {
		t.Fatalf("a concluída aceitou mais que o custo:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// ─────────────────────────── 5. Formato ─────────────────────────────────────

// Em linguagem de negócio: o painel desenha os botões a partir de
// `allowed_actions` e trava os campos por `editable`, sem uma segunda máquina
// de estados. Então os dois têm de dizer a verdade em cada estado — na lista e
// no detalhe —, e o botão que o painel NÃO desenha tem de ser recusado pela
// API com as ações que ainda valem.
func TestManutencaoQAAcoesEEditavelDizemAVerdadeEmCadaEstado(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "manut-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	a.qmFaxina(t, f)
	unidade, _ := f.qbUnidade(t, a.qbPropriedadePadrao(t), "")
	tk := u.Token

	d := a.qmHoje(t, a.qbPropriedadePadrao(t))
	aberta := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "title": "Aberta"})
	andamento := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "title": "Em andamento", "block": qmPeriodo(d.AddDays(4), d.AddDays(6))})
	iniciada := qbDado[qmOrdem](t, a.chamar(t, http.MethodPost, "/maintenance-orders/"+andamento.ID.String()+"/start", tk, nil), http.StatusOK, "começando")
	// `/start` não mexe no calendário: o bloqueio já valia desde que foi pedido.
	if b := iniciada.Bloqueio; b == nil || *b != *andamento.Bloqueio {
		t.Fatalf("começar mexeu no bloqueio: antes %+v, depois %+v", andamento.Bloqueio, b)
	}
	concluida := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "title": "Concluída"})
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/maintenance-orders/"+concluida.ID.String()+"/complete", tk, nil), http.StatusOK, "concluindo")
	cancelada := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "title": "Cancelada"})
	exigirStatusQB(t, a.chamar(t, http.MethodDelete, "/maintenance-orders/"+cancelada.ID.String(), tk, nil), http.StatusOK, "cancelando")

	esperado := map[uuid.UUID]struct {
		status, editavel string
		acoes            []string
	}{
		aberta.ID:    {"aberta", "tudo", []string{"start", "complete", "cancel"}},
		andamento.ID: {"em_andamento", "tudo", []string{"complete", "cancel"}},
		concluida.ID: {"concluida", "so_custo", []string{}},
		cancelada.ID: {"cancelada", "nada", []string{}},
	}
	conferir := func(t *testing.T, onde string, o qmOrdem) {
		t.Helper()
		e, ok := esperado[o.ID]
		if !ok {
			return
		}
		if o.Status != e.status || o.Editavel != e.editavel || !slices.Equal(o.Acoes, e.acoes) || o.Acoes == nil {
			t.Errorf("%s, %s: status %s editable %s allowed_actions %v; esperado %s %s %v (e `[]`, nunca null)",
				onde, e.status, o.Status, o.Editavel, o.Acoes, e.status, e.editavel, e.acoes)
		}
		if (e.status == "concluida" || e.status == "cancelada") != (o.FechadaEm != nil) {
			t.Errorf("%s, %s: closed_at = %v — nulo se e somente se não encerrada", onde, e.status, o.FechadaEm)
		}
		if (e.status == "em_andamento") != (o.IniciadaEm != nil) && e.status != "concluida" && e.status != "cancelada" {
			t.Errorf("%s, %s: started_at = %v", onde, e.status, o.IniciadaEm)
		}
	}
	for id := range esperado {
		conferir(t, "detalhe", a.qmLer(t, tk, id))
	}
	itens, _ := qbLista[qmOrdem](t, a.chamar(t, http.MethodGet, "/maintenance-orders?unit_id="+unidade.String(), tk, nil), "lista")
	if len(itens) != 4 {
		t.Fatalf("lista da unidade com %d ordens, esperado 4", len(itens))
	}
	for _, o := range itens {
		conferir(t, "lista", o)
	}

	// O botão que não aparece é recusado com as ações que valem.
	r := a.chamar(t, http.MethodPost, "/maintenance-orders/"+andamento.ID.String()+"/start", tk, nil)
	e := qbErro(t, r, http.StatusConflict, "INVALID_STATE_TRANSITION", "começar o que já está em andamento")
	if fmt.Sprint(e.Details["allowed"]) != "[complete cancel]" || e.Details["status"] != "em_andamento" {
		t.Errorf("details do 409 (status, allowed = allowed_actions): %v", e.Details)
	}
}

// Em linguagem de negócio: a lista de trabalho do celular abre com o que é
// mais urgente e está há mais tempo esperando; depois vêm as encerradas, da
// mais recente. Paginar não repete nem perde ordem, e o envelope é o de toda
// lista do contrato. Nenhuma chave da resposta foge do schema
// `OrdemDeManutencao` — é assim que dado de hóspede vazaria sem ninguém
// decidir.
func TestManutencaoQAListaNoEnvelopeENaOrdemDoContrato(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "manut-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	a.qmFaxina(t, f)
	prop := a.qbPropriedadePadrao(t)
	d := a.qmHoje(t, prop)
	unidade, _ := f.qbUnidade(t, prop, "")
	tk := u.Token

	abrir := func(titulo, prioridade string, horasAtras int) uuid.UUID {
		o := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "title": titulo, "priority": prioridade})
		if _, err := a.pool.Exec(a.ctx, `UPDATE maintenance_orders SET opened_at = opened_at - $2::int * interval '1 hour' WHERE id = $1`,
			o.ID, horasAtras); err != nil {
			t.Fatal(err)
		}
		return o.ID
	}
	baixaAntiga := abrir("Trocar capacho", "baixa", 50)
	normalAntiga := abrir("Porta rangendo", "normal", 40)
	altaEmAndamento := abrir("Vazamento sob a pia", "alta", 5)
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/maintenance-orders/"+altaEmAndamento.String()+"/start", tk, nil), http.StatusOK, "começando")
	urgenteNova := abrir("Disjuntor desarmando", "urgente", 1)
	normalNova := abrir("Torneira pingando", "normal", 2)
	urgenteAntiga := abrir("Sem água quente", "urgente", 30)
	concluidaAntes := abrir("Chuveiro", "urgente", 60)
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/maintenance-orders/"+concluidaAntes.String()+"/complete", tk, nil), http.StatusOK, "concluindo")
	if _, err := a.pool.Exec(a.ctx, `UPDATE maintenance_orders SET closed_at = closed_at - interval '3 hours' WHERE id = $1`, concluidaAntes); err != nil {
		t.Fatal(err)
	}
	canceladaDepois := abrir("Pintura", "alta", 70)
	exigirStatusQB(t, a.chamar(t, http.MethodDelete, "/maintenance-orders/"+canceladaDepois.String(), tk, nil), http.StatusOK, "cancelando")
	// Uma com bloqueio, para a chave `block` aparecer na varredura de schema.
	comBloqueio := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "title": "Reboco", "priority": "baixa",
		"block": qmPeriodo(d.AddDays(2), d.AddDays(3))}).ID

	esperado := []uuid.UUID{urgenteAntiga, urgenteNova, altaEmAndamento, normalAntiga, normalNova, baixaAntiga, comBloqueio,
		canceladaDepois, concluidaAntes}

	r := a.chamar(t, http.MethodGet, "/maintenance-orders?unit_id="+unidade.String(), tk, nil)
	itens, meta := qbLista[qmOrdem](t, r, "lista padrão")
	ids := make([]uuid.UUID, len(itens))
	for i, o := range itens {
		ids[i] = o.ID
	}
	if !slices.Equal(ids, esperado) {
		t.Errorf("ordem da lista de trabalho:\n got  %v\n want %v", ids, esperado)
	}
	if meta.Page != 1 || meta.PerPage != 25 || meta.Total != int64(len(esperado)) || meta.TotalPages != 1 {
		t.Errorf("meta = %+v", meta)
	}
	if fora := qbChavesForaDoContrato(t, "OrdemDeManutencao", r.Corpo); len(fora) > 0 {
		t.Errorf("chaves fora do schema OrdemDeManutencao na lista: %v", fora)
	}

	// Paginando de 2 em 2: a mesma sequência, sem repetir nem perder.
	var paginadas []uuid.UUID
	for pagina := 1; pagina <= 5; pagina++ {
		itens, m := qbLista[qmOrdem](t, a.chamar(t, http.MethodGet,
			fmt.Sprintf("/maintenance-orders?unit_id=%s&per_page=2&page=%d", unidade, pagina), tk, nil), "página")
		if m.TotalPages != 5 || m.Total != int64(len(esperado)) || m.PerPage != 2 || m.Page != pagina {
			t.Errorf("meta da página %d: %+v", pagina, m)
		}
		for _, o := range itens {
			paginadas = append(paginadas, o.ID)
		}
	}
	if !slices.Equal(paginadas, esperado) {
		t.Errorf("paginar mudou a sequência:\n got  %v\n want %v", paginadas, esperado)
	}

	det := a.chamar(t, http.MethodGet, "/maintenance-orders/"+comBloqueio.String(), tk, nil)
	if fora := qbChavesForaDoContrato(t, "OrdemDeManutencao", det.Corpo); len(fora) > 0 {
		t.Errorf("chaves fora do schema OrdemDeManutencao no detalhe: %v", fora)
	}
}

// Em linguagem de negócio: a ordem criada diz onde mora (`Location`), e esse
// endereço abre a mesma ordem. A escrita deixa trilha com autor e request_id
// no sistema MONTADO (o middleware de auditoria é o do router, não o do teste
// do módulo).
func TestManutencaoQALocationNo201ETrilhaNoSistemaMontado(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "manut-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	a.qmFaxina(t, f)
	prop := a.qbPropriedadePadrao(t)
	d := a.qmHoje(t, prop)
	unidade, _ := f.qbUnidade(t, prop, "")

	r := a.chamar(t, http.MethodPost, "/maintenance-orders", u.Token, map[string]any{"unit_id": unidade, "title": "Calha",
		"block": qmPeriodo(d.AddDays(1), d.AddDays(2))})
	o := qbDado[qmOrdem](t, r, http.StatusCreated, "abrindo")
	loc := r.Headers.Get("Location")
	if loc != PrefixoDaAPI+"/maintenance-orders/"+o.ID.String() {
		t.Fatalf("Location = %q, esperado %s/maintenance-orders/%s", loc, PrefixoDaAPI, o.ID)
	}
	lida := qbDado[qmOrdem](t, a.qbBaixar(t, u.Token, loc), http.StatusOK, "GET do Location")
	if lida.ID != o.ID {
		t.Fatalf("o Location abriu outra ordem: %s", lida.ID)
	}
	// `opened_by_name` é nome de USUÁRIO: ler a ordem não grava pii_access_log.
	exigirStatusQB(t, a.chamar(t, http.MethodGet, "/maintenance-orders?unit_id="+unidade.String(), u.Token, nil), http.StatusOK, "lista")
	if n := a.qmContar(t, `SELECT count(*) FROM pii_access_log WHERE actor_id = $1`, u.ID); n != 0 {
		t.Errorf("ler ordens gravou %d linha(s) em pii_access_log: a ordem não carrega dado pessoal", n)
	}
	for acao, entidade := range map[string]uuid.UUID{"maintenance_orders.criado": o.ID, "stay_blocks.bloqueada": o.Bloqueio.ID} {
		var ator *uuid.UUID
		var requisicao *string
		if err := a.pool.QueryRow(a.ctx, `SELECT actor_id, request_id FROM audit_log WHERE action = $1 AND entity_id = $2`, acao, entidade).
			Scan(&ator, &requisicao); err != nil {
			t.Errorf("trilha %s ausente no sistema montado: %v", acao, err)
			continue
		}
		if ator == nil || *ator != u.ID || requisicao == nil || *requisicao == "" {
			t.Errorf("trilha %s sem autor ou request_id: ator %v request_id %v", acao, ator, requisicao)
		}
	}
}

// Em linguagem de negócio: todo erro sai no envelope do contrato com um `code`
// do vocabulário fechado — o painel reage ao code. Inclui os dois codes que
// nasceram com esta fatia.
func TestManutencaoQAErrosNoEnvelopeDoContrato(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "manut-usuario", a.qbPerfilDoSeed(t, "usuario"))
	corretor := a.criarUsuario(t, "manut-corretor", a.qbPerfilDoSeed(t, "corretor"))
	f := a.qbFaxina(t, u, corretor)
	a.qmFaxina(t, f)
	prop := a.qbPropriedadePadrao(t)
	d := a.qmHoje(t, prop)
	unidade, _ := f.qbUnidade(t, prop, "")
	tk := u.Token
	for _, c := range []string{"MAINTENANCE_ORDER_CLOSED", "MAINTENANCE_ORDER_ALREADY_OPEN", "DATE_CONFLICT", "INVALID_STATE_TRANSITION", "RESOURCE_IN_USE"} {
		if !qbEnum(t)[c] {
			t.Errorf("o enum de error.code do contrato não tem %s", c)
		}
	}

	o := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "title": "Fechadura", "block": qmPeriodo(d.AddDays(1), d.AddDays(3))})
	base := "/maintenance-orders/" + o.ID.String()
	qbErro(t, a.chamar(t, http.MethodGet, "/maintenance-orders/"+uuid.NewString(), tk, nil), http.StatusNotFound, "NOT_FOUND", "ordem inexistente")
	qbErro(t, a.chamar(t, http.MethodGet, "/maintenance-orders?sort=prioridade", tk, nil), http.StatusUnprocessableEntity, "VALIDATION_ERROR", "sort fora")
	qbErro(t, a.chamar(t, http.MethodPost, "/maintenance-orders", tk, map[string]any{"unit_id": unidade, "title": "Outra",
		"block": qmPeriodo(d.AddDays(2), d.AddDays(4))}), http.StatusConflict, "DATE_CONFLICT", "bloqueio sobreposto")
	qbErro(t, a.chamar(t, http.MethodPost, "/maintenance-orders", tk, map[string]any{"unit_id": unidade, "title": "  "}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "título em branco")
	qbErro(t, a.chamar(t, http.MethodPost, base+"/complete", tk, map[string]any{"cost_cents": nil}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "concluir com custo nulo")
	// Verbo fora do contrato: o router responde 404 de propósito (router.go,
	// MethodNotAllowed → NotFound), no envelope.
	qbErro(t, a.chamar(t, http.MethodPost, base, tk, nil), http.StatusNotFound, "NOT_FOUND", "verbo fora do contrato")
	qbErro(t, a.chamar(t, http.MethodGet, base, corretor.Token, nil), http.StatusForbidden, "FORBIDDEN", "corretor")
	exigirStatusQB(t, a.chamar(t, http.MethodPost, base+"/start", tk, nil), http.StatusOK, "começando")
	qbErro(t, a.chamar(t, http.MethodPost, base+"/start", tk, nil), http.StatusConflict, "INVALID_STATE_TRANSITION", "começar duas vezes")
	exigirStatusQB(t, a.chamar(t, http.MethodDelete, base, tk, nil), http.StatusOK, "cancelando")
	qbErro(t, a.chamar(t, http.MethodDelete, base, tk, nil), http.StatusConflict, "MAINTENANCE_ORDER_CLOSED", "cancelar duas vezes")
}

// Em linguagem de negócio: um nome de campo errado não pode virar "salvo" em
// silêncio. Com alvo REAL e corpo VÁLIDO mais um campo fora do contrato, cada
// rota com corpo responde 422 nomeando o campo — inclusive o `/complete`, que
// é irreversível — e o banco fica como estava.
func TestManutencaoQACampoDesconhecidoComAlvoRealNaoGravaNada(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "manut-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	a.qmFaxina(t, f)
	c := a.qmMontarCenario(t, f, u.Token)
	antes := a.qmLerEstado(t, c)

	const intruso = "campo_fora_do_contrato"
	varridas := 0
	for _, op := range qmOperacoesDeManutencao(t) {
		corpo, ok := c.corpo(op).(map[string]any)
		if !ok {
			if m, ok2 := c.corpo(op).(map[string]string); ok2 {
				corpo = map[string]any{}
				for k, v := range m {
					corpo[k] = v
				}
			} else {
				continue
			}
		}
		varridas++
		t.Run(op.chave(), func(t *testing.T) {
			com := map[string]any{intruso: true}
			for k, v := range corpo {
				com[k] = v
			}
			e := qbErro(t, a.chamar(t, op.Metodo, c.caminho(op), u.Token, com), http.StatusUnprocessableEntity, "VALIDATION_ERROR",
				op.chave()+" com campo desconhecido")
			if e.Details[intruso] == nil {
				t.Errorf("o 422 tem de nomear o campo: %v", e.Details)
			}
		})
	}
	if varridas != 5 {
		t.Errorf("esperava 5 operações com corpo JSON (POST, PUT, PATCH, complete, PUT block); varri %d", varridas)
	}
	if depois := a.qmLerEstado(t, c); !depois.igual(antes) {
		t.Fatalf("um corpo com campo desconhecido gravou alguma coisa:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// Em linguagem de negócio: JSON diferencia maiúscula de minúscula, e todos os
// schemas de entrada desta tag são `additionalProperties: false` — inclusive o
// `PeriodoDoBloqueio` que vai DENTRO do POST como `block`. `"Title"` não é
// `title`, e `"From"` não é `from`: é campo que o contrato não conhece, 422
// como qualquer outro. Aceitar a chave "quase certa" é o cliente achar que
// mandou uma coisa e o servidor decidir outra — e, no `block`, é bloquear o
// calendário da casa com um corpo que o contrato recusa.
func TestManutencaoQAChaveComOutraCaixaNaoEhOCampoDoContrato(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "manut-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	a.qmFaxina(t, f)
	prop := a.qbPropriedadePadrao(t)
	d := a.qmHoje(t, prop)
	unidade, _ := f.qbUnidade(t, prop, "")
	tk := u.Token
	o := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "title": "Alvo"})
	base := "/maintenance-orders/" + o.ID.String()
	de, ate := d.AddDays(9), d.AddDays(11)

	casos := []struct{ nome, metodo, caminho, corpo string }{
		{"Title no POST", http.MethodPost, "/maintenance-orders", fmt.Sprintf(`{"unit_id": %q, "Title": "x"}`, unidade)},
		{"Cost_Cents no PATCH", http.MethodPatch, base, `{"Cost_Cents": 5}`},
		{"PRIORITY no PUT", http.MethodPut, base, `{"title": "x", "PRIORITY": "alta"}`},
		{"COST_CENTS no complete", http.MethodPost, base + "/complete", `{"COST_CENTS": 5}`},
		{"FROM no PUT do bloqueio", http.MethodPut, base + "/block", fmt.Sprintf(`{"FROM": %q, "to": %q}`, de, ate)},
		{"From/To dentro do block do POST", http.MethodPost, "/maintenance-orders",
			fmt.Sprintf(`{"unit_id": %q, "title": "Caixa aninhada", "block": {"From": %q, "To": %q}}`, unidade, de, ate)},
		// A mesma chave duas vezes, com caixas diferentes: um leitor de JSON que
		// segue o contrato lê `from` = D+20; o servidor fica com a ÚLTIMA que
		// casou sem caixa (D+21) — e bloqueia outro período.
		{"from e From dentro do block do POST", http.MethodPost, "/maintenance-orders",
			fmt.Sprintf(`{"unit_id": %q, "title": "Caixa duplicada", "block": {"from": %q, "From": %q, "to": %q}}`,
				unidade, d.AddDays(20), d.AddDays(21), d.AddDays(23))},
		{"campo desconhecido dentro do block do POST", http.MethodPost, "/maintenance-orders",
			fmt.Sprintf(`{"unit_id": %q, "title": "Intruso aninhado", "block": {"from": %q, "to": %q, "note": "x"}}`, unidade, de, ate)},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			r := a.qbChamarCru(t, c.metodo, c.caminho, tk, c.corpo)
			if r.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s %s %s → %d (esperado 422: a chave não existe no contrato) — %s", c.metodo, c.caminho, c.corpo, r.Status, r.Corpo)
				return
			}
			qbEnvelopeDeErro(t, r, "VALIDATION_ERROR", c.nome)
		})
	}
	if n := a.qmContar(t, `SELECT count(*) FROM maintenance_orders WHERE unit_id = $1`, unidade); n != 1 {
		t.Errorf("corpo fora do contrato criou ordem: %d ordens na unidade (esperado só o alvo)", n)
	}
	if n := a.qmContar(t, `SELECT count(*) FROM stay_blocks WHERE unit_id = $1 AND status = 'confirmed'`, unidade); n != 0 {
		t.Errorf("corpo fora do contrato bloqueou a unidade: %d bloqueio(s) ocupando o calendário", n)
	}
	if v := a.qmFotografar(t, o.ID); v.Status != "aberta" || v.Custo != "∅" || v.Prioridade != "normal" || v.Titulo != "Alvo" {
		t.Errorf("corpo fora do contrato mexeu na ordem: %+v", v)
	}
}

// Em linguagem de negócio: o filtro `open` é booleano no contrato. Quem pede
// "só as abertas" com um valor que não é booleano (`open=sim`, `open=abertas`)
// não pode receber a lista inteira — com as encerradas misturadas — como se o
// filtro tivesse sido aplicado. É a regra que o próprio módulo escreve para
// `status`, `priority` e `sort` ("devolver a lista inteira fingindo que filtrou
// seria pior"), e o 422 que o contrato promete para filtro fora do vocabulário.
func TestManutencaoQAFiltroOpenForaDoVocabularioNaoFingeQueFiltrou(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "manut-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	a.qmFaxina(t, f)
	unidade, _ := f.qbUnidade(t, a.qbPropriedadePadrao(t), "")
	tk := u.Token
	a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "title": "Aberta"})
	fechada := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "title": "Encerrada"})
	exigirStatusQB(t, a.chamar(t, http.MethodDelete, "/maintenance-orders/"+fechada.ID.String(), tk, nil), http.StatusOK, "cancelando")

	for _, valor := range []string{"sim", "abertas", "yes"} {
		t.Run(valor, func(t *testing.T) {
			r := a.chamar(t, http.MethodGet, "/maintenance-orders?unit_id="+unidade.String()+"&open="+valor, tk, nil)
			if r.Status == http.StatusOK {
				itens, _ := qbLista[qmOrdem](t, r, "lista")
				var status []string
				for _, o := range itens {
					status = append(status, o.Status)
				}
				t.Fatalf("open=%s respondeu 200 com %d ordem(ns) [%s] — o filtro foi ignorado em silêncio; esperado 422 VALIDATION_ERROR em details.open",
					valor, len(itens), strings.Join(status, ", "))
			}
			if e := qbErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "open="+valor); e.Details["open"] == nil {
				t.Errorf("o 422 aponta details.open: %v", e.Details)
			}
		})
	}
}
