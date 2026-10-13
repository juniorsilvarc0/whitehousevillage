//go:build integration

package router

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// ─────────────────────────── 3. A avaria ────────────────────────────────────
//
// A avaria é de `Bens` (`inventory.goods`), a ordem é de `Manutenção`. O que se
// cobra aqui é a fronteira, lida pelas DUAS portas: a governanta relata pela
// tela de avarias, o encarregado conserta pela ordem, e cada um vê o efeito do
// outro na própria tela.

type qmAvaria struct {
	ID           uuid.UUID  `json:"id"`
	Desfecho     *string    `json:"resolution"`
	ResolvidaEm  *time.Time `json:"resolved_at"`
	ResolvidaPor *uuid.UUID `json:"resolved_by"`
	OrdemAberta  *uuid.UUID `json:"open_maintenance_order_id"`
}

func (a *ambiente) qmAvariaLida(t *testing.T, token string, id uuid.UUID) qmAvaria {
	t.Helper()
	return qbDado[qmAvaria](t, a.chamar(t, http.MethodGet, "/inventory/issues/"+id.String(), token, nil), http.StatusOK, "GET /inventory/issues/{id}")
}

func (a *ambiente) qmAvariaNaLista(t *testing.T, token string, comodo, id uuid.UUID) qmAvaria {
	t.Helper()
	itens, _ := qbLista[qmAvaria](t, a.chamar(t, http.MethodGet, "/inventory/issues?room_id="+comodo.String(), token, nil), "GET /inventory/issues")
	for _, x := range itens {
		if x.ID == id {
			return x
		}
	}
	t.Fatalf("a avaria %s não está na lista do cômodo", id)
	return qmAvaria{}
}

func idOuNada(v *uuid.UUID) string {
	if v == nil {
		return "<nulo>"
	}
	return v.String()
}

// Em linguagem de negócio: a governanta relata "ar da suíte não gela"; o
// encarregado abre a ordem a partir da avaria. Enquanto a ordem está aberta,
// a lista de avarias diz "ordem em aberto" e leva até ela; o segundo toque em
// "abrir ordem" leva à mesma ordem em vez de criar outra; a avaria não pode
// ser apagada; cancelar a ordem não conserta nada (a avaria segue pendente e
// aceita a ordem do retrabalho); e concluir a ordem fecha a avaria como
// `consertado`, com quem e quando.
func TestManutencaoQAAvariaDaAberturaAoConserto(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "manut-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	a.qmFaxina(t, f)
	tk := u.Token
	unidade, _ := f.qbUnidade(t, a.qbPropriedadePadrao(t), "")
	suite := a.qbComodo(t, tk, unidade, "Suíte 1", "quarto", 1)
	ar := a.qbBem(t, f, tk, "Ar-condicionado QA "+qbSufixo(), ptrInt64(250000))
	a.qbColocar(t, tk, suite, ar, 1)
	avaria := qbDado[qbIDResp](t, a.chamar(t, http.MethodPost, "/inventory/issues", tk, map[string]any{
		"room_id": suite, "item_id": ar, "kind": "avariado", "qty": 1, "note": "não gela",
	}), http.StatusCreated, "relatando a avaria").ID

	if x := a.qmAvariaNaLista(t, tk, suite, avaria); x.OrdemAberta != nil {
		t.Fatalf("avaria sem ordem com open_maintenance_order_id = %s", x.OrdemAberta)
	}

	// A ação "Abrir ordem de manutenção" da tela de avarias: só unidade, avaria e título.
	primeira := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "issue_id": avaria, "title": "Ar não gela"})
	if idOuNada(primeira.ComodoID) != suite.String() || idOuNada(primeira.BemID) != ar.String() {
		t.Fatalf("cômodo e bem vêm da avaria: %s %s", idOuNada(primeira.ComodoID), idOuNada(primeira.BemID))
	}
	for nome, x := range map[string]qmAvaria{
		"lista":   a.qmAvariaNaLista(t, tk, suite, avaria),
		"detalhe": a.qmAvariaLida(t, tk, avaria),
	} {
		if x.OrdemAberta == nil || *x.OrdemAberta != primeira.ID {
			t.Errorf("%s da avaria: open_maintenance_order_id = %s, esperado %s", nome, idOuNada(x.OrdemAberta), primeira.ID)
		}
	}

	// O segundo toque leva à primeira ordem, e não cria outra.
	r := a.chamar(t, http.MethodPost, "/maintenance-orders", tk, map[string]any{"unit_id": unidade, "issue_id": avaria, "title": "Ar não gela (de novo)"})
	e := qbErro(t, r, http.StatusConflict, "MAINTENANCE_ORDER_ALREADY_OPEN", "segunda ordem para a mesma avaria")
	if e.Details["maintenance_order_id"] != primeira.ID.String() {
		t.Errorf("o 409 tem de levar à ordem que já existe (%s): %s", primeira.ID, r.Corpo)
	}
	if n := a.qmContar(t, `SELECT count(*) FROM maintenance_orders WHERE issue_id = $1`, avaria); n != 1 {
		t.Fatalf("o segundo toque criou ordem: %d ordens na avaria", n)
	}

	// Citada pela ordem, a avaria não se apaga — nem o cômodo, nem o bem.
	e = qbErro(t, a.chamar(t, http.MethodDelete, "/inventory/issues/"+avaria.String(), tk, nil), http.StatusConflict, "RESOURCE_IN_USE",
		"apagar a avaria citada pela ordem")
	if e.Details["maintenance_order_id"] != primeira.ID.String() {
		t.Errorf("details.maintenance_order_id = %v, esperado %s", e.Details["maintenance_order_id"], primeira.ID)
	}
	e = qbErro(t, a.chamar(t, http.MethodDelete, "/rooms/"+suite.String(), tk, nil), http.StatusConflict, "RESOURCE_IN_USE", "apagar o cômodo da ordem")
	if n, _ := e.Details["maintenance_orders"].(float64); n < 1 {
		t.Errorf("o 409 do cômodo conta as ordens que o citam (details.maintenance_orders): %v", e.Details)
	}
	e = qbErro(t, a.chamar(t, http.MethodDelete, "/inventory/items/"+ar.String(), tk, nil), http.StatusConflict, "RESOURCE_IN_USE", "apagar o bem da ordem")
	if n, _ := e.Details["maintenance_orders"].(float64); n < 1 {
		t.Errorf("o 409 do bem conta as ordens que o citam (details.maintenance_orders): %v", e.Details)
	}

	// Cancelar não conserta: a avaria segue pendente e deixa de ter ordem aberta.
	c := qbDado[qmOrdem](t, a.chamar(t, http.MethodDelete, "/maintenance-orders/"+primeira.ID.String(), tk, nil), http.StatusOK, "cancelando")
	if c.Avaria == nil || c.Avaria.Desfecho != nil {
		t.Fatalf("a ordem cancelada mostra a avaria intacta: %+v", c.Avaria)
	}
	x := a.qmAvariaLida(t, tk, avaria)
	if x.Desfecho != nil || x.ResolvidaEm != nil || x.ResolvidaPor != nil {
		t.Fatalf("cancelar a ordem mexeu na avaria: %+v", x)
	}
	if x.OrdemAberta != nil {
		t.Fatalf("a ordem cancelada continua como `open_maintenance_order_id`: %s", x.OrdemAberta)
	}
	if l := a.qmAvariaNaLista(t, tk, suite, avaria); l.OrdemAberta != nil {
		t.Fatalf("na lista, a ordem cancelada continua aberta: %s", l.OrdemAberta)
	}

	// Retrabalho: a avaria aceita ordem nova, que vira a aberta.
	segunda := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "issue_id": avaria, "title": "Ar não gela — técnico novo"})
	if l := a.qmAvariaNaLista(t, tk, suite, avaria); l.OrdemAberta == nil || *l.OrdemAberta != segunda.ID {
		t.Fatalf("a ordem do retrabalho é a aberta: %s", idOuNada(l.OrdemAberta))
	}
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/maintenance-orders/"+segunda.ID.String()+"/start", tk, nil), http.StatusOK, "começando")
	antesDeConcluir := time.Now().Add(-time.Minute)
	fim := qbDado[qmOrdem](t, a.chamar(t, http.MethodPost, "/maintenance-orders/"+segunda.ID.String()+"/complete", tk,
		map[string]any{"cost_cents": 42000}), http.StatusOK, "concluindo")
	if fim.Avaria == nil || fim.Avaria.Desfecho == nil || *fim.Avaria.Desfecho != "consertado" {
		t.Fatalf("a ordem concluída mostra a avaria consertada: %+v", fim.Avaria)
	}
	x = a.qmAvariaLida(t, tk, avaria)
	if x.Desfecho == nil || *x.Desfecho != "consertado" {
		t.Fatalf("concluir a ordem fecha a avaria como consertado; a tela de avarias mostra %v", x.Desfecho)
	}
	if x.ResolvidaPor == nil || *x.ResolvidaPor != u.ID || x.ResolvidaEm == nil || x.ResolvidaEm.Before(antesDeConcluir) {
		t.Fatalf("resolved_by/resolved_at da avaria consertada: %s %v", idOuNada(x.ResolvidaPor), x.ResolvidaEm)
	}
	if x.OrdemAberta != nil {
		t.Fatalf("a ordem concluída continua como `open_maintenance_order_id`: %s", x.OrdemAberta)
	}

	// Encerradas, as duas ordens continuam citando a avaria: apagar segue 409.
	e = qbErro(t, a.chamar(t, http.MethodDelete, "/inventory/issues/"+avaria.String(), tk, nil), http.StatusConflict, "RESOURCE_IN_USE",
		"apagar a avaria de ordens encerradas")
	if v := e.Details["maintenance_order_id"]; v != primeira.ID.String() && v != segunda.ID.String() {
		t.Errorf("details.maintenance_order_id = %v, esperado uma das ordens da avaria", v)
	}
}

// Em linguagem de negócio: quem só confere a casa (`inventory.goods:ver`) vê na
// lista de avarias QUE existe uma ordem aberta (o id), mas não abre a ordem —
// o conteúdo dela continua exigindo `maintenance:ver`.
func TestManutencaoQAQuemSoVeAvariasVeQueHaOrdemMasNaoAAbre(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "manut-usuario", a.qbPerfilDoSeed(t, "usuario"))
	conferente := a.criarUsuario(t, "manut-conferente", a.criarPerfil(t, "manut_conferente", []auth.Permissao{
		{Resource: "inventory.goods", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	}))
	f := a.qbFaxina(t, u, conferente)
	a.qmFaxina(t, f)
	unidade, _ := f.qbUnidade(t, a.qbPropriedadePadrao(t), "")
	banheiro := a.qbComodo(t, u.Token, unidade, "Banheiro", "banheiro", 1)
	chuveiro := a.qbBem(t, f, u.Token, "Chuveiro QA "+qbSufixo(), nil)
	a.qbColocar(t, u.Token, banheiro, chuveiro, 1)
	avaria := qbDado[qbIDResp](t, a.chamar(t, http.MethodPost, "/inventory/issues", u.Token, map[string]any{
		"room_id": banheiro, "item_id": chuveiro, "kind": "avariado", "qty": 1,
	}), http.StatusCreated, "relatando").ID
	o := a.qmAbrir(t, u.Token, map[string]any{"unit_id": unidade, "issue_id": avaria, "title": "Chuveiro não esquenta"})

	if x := a.qmAvariaNaLista(t, conferente.Token, banheiro, avaria); x.OrdemAberta == nil || *x.OrdemAberta != o.ID {
		t.Fatalf("o conferente vê que há ordem aberta: %s", idOuNada(x.OrdemAberta))
	}
	qbErro(t, a.chamar(t, http.MethodGet, "/maintenance-orders/"+o.ID.String(), conferente.Token, nil), http.StatusForbidden, "FORBIDDEN",
		"o conferente abrindo a ordem")
}
