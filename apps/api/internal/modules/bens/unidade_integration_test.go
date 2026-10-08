//go:build integration

package bens_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type copiaResp struct {
	DryRun bool `json:"dry_run"`
	Origem struct {
		UnidadeID uuid.UUID `json:"unit_id"`
		Codigo    string    `json:"code"`
	} `json:"source"`
	Ambientes []struct {
		Nome string `json:"name"`
		Tipo string `json:"kind"`
	} `json:"rooms_created"`
	Colocacoes []struct {
		AmbienteNome string    `json:"room_name"`
		BemID        uuid.UUID `json:"item_id"`
		Qtd          int       `json:"expected_qty"`
	} `json:"placements_created"`
	Mantidas []struct {
		AmbienteNome string `json:"room_name"`
		QtdOrigem    int    `json:"source_qty"`
		QtdAtual     int    `json:"current_qty"`
	} `json:"kept"`
}

// A cópia só ACRESCENTA, o dry_run devolve o mesmo plano sem gravar, a segunda
// passada não cria nada, e destino com conferência aberta é 409.
func TestCopiaSoAcrescentaEDryRunNaoGrava(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	origem, codigoOrigem := a.unidade(t)
	destino, _ := a.unidade(t)

	cozinha := a.comodo(t, g, origem, "Cozinha", "cozinha", 1)
	quarto := a.comodo(t, g, origem, "Quarto", "quarto", 2)
	inativo := a.comodo(t, g, origem, "Depósito", "outro", 3)
	prato := a.bem(t, g, "Prato", nil)
	toalha := a.bem(t, g, "Toalha", nil)
	a.colocar(t, g, cozinha.ID, prato.ID, 12)
	a.colocar(t, g, quarto.ID, toalha.ID, 4)
	a.colocar(t, g, inativo.ID, toalha.ID, 9)
	exigir(t, a.chamar(t, http.MethodPatch, "/rooms/"+inativo.ID.String(), g, map[string]any{"active": false}), http.StatusOK, "desativando")

	// O destino já tem "Cozinha" (de outro tipo) com 8 pratos contados.
	cozinhaDestino := a.comodo(t, g, destino, "Cozinha", "sala", 7)
	a.colocar(t, g, cozinhaDestino.ID, prato.ID, 8)

	caminho := fmt.Sprintf("/units/%s/inventory/copy", destino)
	r := a.chamar(t, http.MethodPost, caminho+"?dry_run=1", g, map[string]any{"source_unit_id": origem})
	exigir(t, r, http.StatusOK, "simulando")
	sim := dado[copiaResp](t, r)
	if !sim.DryRun || sim.Origem.Codigo != codigoOrigem {
		t.Fatalf("simulação: %+v", sim)
	}
	if len(sim.Ambientes) != 1 || sim.Ambientes[0].Nome != "Quarto" {
		t.Fatalf("só o Quarto nasce; a Cozinha é reaproveitada e o Depósito inativo fica: %+v", sim.Ambientes)
	}
	if len(sim.Colocacoes) != 1 || sim.Colocacoes[0].AmbienteNome != "Quarto" || sim.Colocacoes[0].Qtd != 4 {
		t.Fatalf("nasce só a toalha do quarto: %+v", sim.Colocacoes)
	}
	if len(sim.Mantidas) != 1 || sim.Mantidas[0].QtdAtual != 8 || sim.Mantidas[0].QtdOrigem != 12 {
		t.Fatalf("os 8 pratos do destino ficam: %+v", sim.Mantidas)
	}
	var comodos int
	if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM unit_rooms WHERE unit_id = $1`, destino).Scan(&comodos); err != nil {
		t.Fatal(err)
	}
	if comodos != 1 {
		t.Fatalf("o dry_run gravou: o destino tem %d cômodos", comodos)
	}

	r = a.chamar(t, http.MethodPost, caminho, g, map[string]any{"source_unit_id": origem})
	exigir(t, r, http.StatusOK, "copiando")
	exec := dado[copiaResp](t, r)
	if exec.DryRun || len(exec.Ambientes) != len(sim.Ambientes) || len(exec.Colocacoes) != len(sim.Colocacoes) || len(exec.Mantidas) != len(sim.Mantidas) {
		t.Fatalf("a execução deveria repetir o plano simulado:\n sim  %+v\n exec %+v", sim, exec)
	}
	r = a.chamar(t, http.MethodGet, "/rooms/"+cozinhaDestino.ID.String(), g, nil)
	if c := dado[ambienteResp](t, r); c.Tipo != "sala" || c.Ordem != 7 || c.QtdEsperadaTotal != 8 {
		t.Fatalf("a Cozinha do destino não pode ser reescrita: %+v", c)
	}

	r = a.chamar(t, http.MethodPost, caminho, g, map[string]any{"source_unit_id": origem})
	exigir(t, r, http.StatusOK, "copiando de novo")
	if seg := dado[copiaResp](t, r); len(seg.Ambientes) != 0 || len(seg.Colocacoes) != 0 || len(seg.Mantidas) != 2 {
		t.Fatalf("a segunda passada não cria nada: %+v", seg)
	}

	exigirErro(t, a.chamar(t, http.MethodPost, caminho, g, map[string]any{"source_unit_id": destino}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "origem igual ao destino")
	vazia, _ := a.unidade(t)
	r = a.chamar(t, http.MethodPost, caminho, g, map[string]any{"source_unit_id": vazia})
	exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "origem sem ambiente")
	if r.detalhes(t)["source_unit_id"] == nil {
		t.Fatalf("o 422 deveria explicar em source_unit_id: %s", r.Corpo)
	}

	conf := a.abrir(t, g, destino)
	r = a.chamar(t, http.MethodPost, caminho, g, map[string]any{"source_unit_id": origem})
	exigirErro(t, r, http.StatusConflict, "COUNT_ALREADY_OPEN", "copiando com conferência aberta")
	if r.detalhes(t)["count_id"] != conf.ID.String() {
		t.Fatalf("details.count_id deveria levar à conferência aberta: %s", r.Corpo)
	}
	exigirErro(t, a.chamar(t, http.MethodPost, fmt.Sprintf("/units/%s/inventory/copy", uuid.New()), g,
		map[string]any{"source_unit_id": origem}), http.StatusNotFound, "NOT_FOUND", "destino inexistente")
}

type telaDaUnidadeResp struct {
	Unidade struct {
		Codigo string `json:"code"`
	} `json:"unit"`
	Ambientes []struct {
		Nome string `json:"name"`
		Bens []struct {
			BemID uuid.UUID `json:"item_id"`
		} `json:"items"`
	} `json:"rooms"`
	Totais struct {
		Ambientes      int    `json:"rooms"`
		Bens           int    `json:"items"`
		QtdEsperada    int64  `json:"expected_qty"`
		Custo          *int64 `json:"replacement_cost_cents"`
		BensSemCusto   int    `json:"uncosted_items"`
		AvariasAbertas int    `json:"open_issues"`
	} `json:"totals"`
	Aberta *struct {
		ID uuid.UUID `json:"id"`
	} `json:"open_count"`
	UltimaFechada *struct{} `json:"last_closed_count"`
}

// A tela da unidade numa chamada: cômodos na ordem de caminhada, totais do que
// foi exibido, conferência aberta; `q` tira o cômodo sem resultado.
func TestTelaDaUnidade(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, codigo := a.unidade(t)
	quarto := a.comodo(t, g, unidade, "Quarto", "quarto", 2)
	cozinha := a.comodo(t, g, unidade, "Cozinha", "cozinha", 1)
	a.comodo(t, g, unidade, "Varanda", "varanda", 3)
	prato := a.bem(t, g, "Prato", ptr[int64](1000))
	taca := a.bem(t, g, "Taça", nil)
	a.colocar(t, g, cozinha.ID, prato.ID, 12)
	a.colocar(t, g, cozinha.ID, taca.ID, 6)
	a.colocar(t, g, quarto.ID, prato.ID, 2)
	a.abrir(t, g, unidade)

	r := a.chamar(t, http.MethodGet, fmt.Sprintf("/units/%s/inventory", unidade), g, nil)
	exigir(t, r, http.StatusOK, "abrindo a tela")
	tela := dado[telaDaUnidadeResp](t, r)
	if tela.Unidade.Codigo != codigo || len(tela.Ambientes) != 3 ||
		tela.Ambientes[0].Nome != "Cozinha" || tela.Ambientes[1].Nome != "Quarto" || tela.Ambientes[2].Nome != "Varanda" {
		t.Fatalf("ordem de caminhada: %+v", tela.Ambientes)
	}
	if tela.Totais.Ambientes != 3 || tela.Totais.Bens != 2 || tela.Totais.QtdEsperada != 20 ||
		tela.Totais.Custo == nil || *tela.Totais.Custo != 14*1000 || tela.Totais.BensSemCusto != 1 {
		t.Fatalf("totais: %+v", tela.Totais)
	}
	if tela.Aberta == nil || tela.UltimaFechada != nil {
		t.Fatalf("conferência aberta e nenhuma fechada: %s", r.Corpo)
	}
	if strings.Contains(string(r.Corpo), "capacity") || strings.Contains(string(r.Corpo), "cleaning_fee") {
		t.Fatalf("a tela de bens não pode abrir o cadastro comercial: %s", r.Corpo)
	}

	r = a.chamar(t, http.MethodGet, fmt.Sprintf("/units/%s/inventory?q=Ta%%C3%%A7a", unidade), g, nil)
	exigir(t, r, http.StatusOK, "buscando")
	busca := dado[struct {
		Ambientes []struct {
			Nome string `json:"name"`
		} `json:"rooms"`
	}](t, r)
	if len(busca.Ambientes) != 1 || busca.Ambientes[0].Nome != "Cozinha" {
		t.Fatalf("cômodo sem resultado sai da resposta: %+v", busca.Ambientes)
	}
}

// CSV para o Excel em português: BOM, `;`, vírgula decimal, nome do arquivo
// com o recorte e a data da casa.
func TestExportacaoCSV(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, codigo := a.unidade(t)
	area := a.comodo(t, g, unidade, "Área da piscina", "area_externa", 1)
	b := a.bem(t, g, "=Toalha de piscina", ptr[int64](4590))
	a.colocar(t, g, area.ID, b.ID, 10)

	r := a.chamar(t, http.MethodGet, "/inventory/export?unit_id="+unidade.String(), g, nil)
	exigir(t, r, http.StatusOK, "exportando")
	if ct := r.Cabecalho.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("Content-Type = %q", ct)
	}
	hoje := time.Now().In(fortaleza(t)).Format(time.DateOnly)
	esperado := fmt.Sprintf(`attachment; filename="inventario-%s-%s.csv"`, strings.ToLower(codigo), hoje)
	if cd := r.Cabecalho.Get("Content-Disposition"); cd != esperado {
		t.Fatalf("Content-Disposition = %q, esperado %q", cd, esperado)
	}
	corpo := string(r.Corpo)
	if !strings.HasPrefix(corpo, "\xEF\xBB\xBF") {
		t.Fatal("sem BOM")
	}
	linhas := strings.Split(strings.TrimSpace(strings.TrimPrefix(corpo, "\xEF\xBB\xBF")), "\r\n")
	if len(linhas) != 2 {
		t.Fatalf("cabeçalho + 1 colocação, veio %d linhas: %q", len(linhas), corpo)
	}
	campos := strings.Split(linhas[1], ";")
	if campos[0] != codigo || campos[1] != "Área da piscina" || campos[2] != "Área externa" ||
		!strings.HasPrefix(campos[3], "'=Toalha") || campos[7] != "10" || campos[8] != "45,90" {
		t.Fatalf("linha exportada: %q", linhas[1])
	}

	exigirErro(t, a.chamar(t, http.MethodGet, "/inventory/export?unit_id="+uuid.NewString(), g, nil),
		http.StatusNotFound, "NOT_FOUND", "unidade inexistente")
}

func fortaleza(t *testing.T) *time.Location {
	t.Helper()
	l, err := time.LoadLocation("America/Fortaleza")
	if err != nil {
		t.Skipf("sem tzdata: %v", err)
	}
	return l
}

// A avaria exige bem colocado no cômodo; resolver carimba quem e quando no
// servidor; `null` reabre; o filtro `open` separa as duas listas.
func TestAvariaResolveEReabre(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	cozinha := a.comodo(t, g, unidade, "Cozinha", "cozinha", 1)
	quarto := a.comodo(t, g, unidade, "Quarto", "quarto", 2)
	copo := a.bem(t, g, "Copo", ptr[int64](1250))
	a.colocar(t, g, cozinha.ID, copo.ID, 8)

	r := a.chamar(t, http.MethodPost, "/inventory/issues", g, map[string]any{
		"room_id": quarto.ID, "item_id": copo.ID, "kind": "quebrado", "qty": 1,
	})
	exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "bem fora do cômodo")

	r = a.chamar(t, http.MethodPost, "/inventory/issues", g, map[string]any{
		"room_id": cozinha.ID, "item_id": copo.ID, "kind": "quebrado", "qty": 3, "note": "caiu da prateleira",
	})
	exigir(t, r, http.StatusCreated, "relatando")
	type avaria struct {
		ID           uuid.UUID  `json:"id"`
		Total        *int64     `json:"total_cost_cents"`
		Desfecho     *string    `json:"resolution"`
		ResolvidaEm  *time.Time `json:"resolved_at"`
		ResolvidaPor *string    `json:"resolved_by_name"`
		Relator      *string    `json:"reported_by_name"`
	}
	av := dado[avaria](t, r)
	if av.Total == nil || *av.Total != 3750 || av.Desfecho != nil || av.Relator == nil {
		t.Fatalf("avaria nova: %+v", av)
	}

	r = a.chamar(t, http.MethodPatch, "/inventory/issues/"+av.ID.String(), g, map[string]any{"resolution": "reposto"})
	exigir(t, r, http.StatusOK, "resolvendo")
	resolvida := dado[avaria](t, r)
	if resolvida.ResolvidaEm == nil || resolvida.ResolvidaPor == nil || *resolvida.Desfecho != "reposto" {
		t.Fatalf("resolver carimba quem e quando: %+v", resolvida)
	}
	r = a.chamar(t, http.MethodGet, "/inventory/issues?open=true&room_id="+cozinha.ID.String(), g, nil)
	if abertas, _ := lista[avaria](t, r); len(abertas) != 0 {
		t.Fatalf("resolvida não aparece em open=true: %+v", abertas)
	}

	exigirErro(t, a.chamar(t, http.MethodPatch, "/inventory/issues/"+av.ID.String(), g, `{"resolved_at":"2026-01-01T00:00:00Z"}`),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "resolved_at do aparelho")

	r = a.chamar(t, http.MethodPatch, "/inventory/issues/"+av.ID.String(), g, `{"resolution": null}`)
	exigir(t, r, http.StatusOK, "reabrindo")
	if reaberta := dado[avaria](t, r); reaberta.Desfecho != nil || reaberta.ResolvidaEm != nil || reaberta.ResolvidaPor != nil {
		t.Fatalf("null reabre e limpa o instante: %+v", reaberta)
	}
	r = a.chamar(t, http.MethodGet, "/inventory/issues?open=true&room_id="+cozinha.ID.String(), g, nil)
	if abertas, _ := lista[avaria](t, r); len(abertas) != 1 {
		t.Fatalf("reaberta volta à lista de trabalho: %+v", abertas)
	}

	hoje := time.Now().In(fortaleza(t)).Format(time.DateOnly)
	r = a.chamar(t, http.MethodGet, "/inventory/issues?from="+hoje+"&to="+hoje+"&item_id="+copo.ID.String(), g, nil)
	if doDia, _ := lista[avaria](t, r); len(doDia) != 1 {
		t.Fatalf("filtro por data local da casa: %s", r.Corpo)
	}
	exigirErro(t, a.chamar(t, http.MethodGet, "/inventory/issues?from=07/10/2026", g, nil),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "data fora do formato")

	exigir(t, a.chamar(t, http.MethodDelete, "/inventory/issues/"+av.ID.String(), g, nil), http.StatusNoContent, "apagando")
	exigirErro(t, a.chamar(t, http.MethodGet, "/inventory/issues/"+av.ID.String(), g, nil), http.StatusNotFound, "NOT_FOUND", "relendo")
}

type unidadeDoInventarioResp struct {
	ID          uuid.UUID  `json:"id"`
	Codigo      string     `json:"code"`
	Ativa       bool       `json:"active"`
	Ambientes   int        `json:"rooms"`
	Bens        int        `json:"items"`
	Aberta      *uuid.UUID `json:"open_count_id"`
	FechadaEm   *time.Time `json:"last_closed_at"`
	Capacidade  *int       `json:"capacity"`
	TaxaLimpeza *int64     `json:"cleaning_fee_cents"`
}

// O seletor de unidade de quem só tem `inventory.goods`: `/units` é do
// cadastro comercial e responde 403 a esse perfil; `/inventory/units` lista
// inclusive a unidade SEM ambiente — é nela que alguém cria o primeiro cômodo.
func TestSeletorDeUnidadesDoInventario(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	contagem := a.token(t, a.perfil(t, "bens_contagem", celulaVer))
	vazia, codigoVazia := a.unidade(t)
	cheia, codigoCheia := a.unidade(t)
	cozinha := a.comodo(t, g, cheia, "Cozinha", "cozinha", 1)
	inativo := a.comodo(t, g, cheia, "Depósito", "outro", 2)
	prato := a.bem(t, g, "Prato", nil)
	taca := a.bem(t, g, "Taça", nil)
	a.colocar(t, g, cozinha.ID, prato.ID, 12)
	a.colocar(t, g, cozinha.ID, taca.ID, 6)
	a.colocar(t, g, inativo.ID, prato.ID, 3)
	exigir(t, a.chamar(t, http.MethodPatch, "/rooms/"+inativo.ID.String(), g, map[string]any{"active": false}), http.StatusOK, "desativando")

	fechada := a.abrir(t, g, cheia)
	for _, l := range fechada.linhas() {
		exigir(t, a.contar(t, g, fechada.ID, l.ID, l.QtdEsperada), http.StatusOK, "contando")
	}
	exigir(t, a.chamar(t, http.MethodPost, fmt.Sprintf("/inventory/counts/%s/close", fechada.ID), g, nil), http.StatusOK, "fechando")
	aberta := a.abrir(t, g, cheia)

	r := a.chamar(t, http.MethodGet, "/inventory/units?q=IT-BENS", contagem, nil)
	exigir(t, r, http.StatusOK, "listando com só inventory.goods:ver")
	unidades, m := lista[unidadeDoInventarioResp](t, r)
	if m.PerPage != 100 {
		t.Fatalf("per_page padrão do seletor é 100, veio %d", m.PerPage)
	}
	porID := map[uuid.UUID]unidadeDoInventarioResp{}
	for i, u := range unidades {
		porID[u.ID] = u
		if i > 0 && unidades[i-1].Codigo > u.Codigo {
			t.Fatalf("ordem por code: %s antes de %s", unidades[i-1].Codigo, u.Codigo)
		}
		if u.Capacidade != nil || u.TaxaLimpeza != nil {
			t.Fatalf("o seletor não abre o cadastro comercial: %s", r.Corpo)
		}
	}
	v, ok := porID[vazia]
	if !ok || v.Codigo != codigoVazia || v.Ambientes != 0 || v.Bens != 0 || v.Aberta != nil || v.FechadaEm != nil {
		t.Fatalf("a unidade sem ambiente aparece, zerada: %+v (achou=%v)", v, ok)
	}
	c := porID[cheia]
	if c.Codigo != codigoCheia || c.Ambientes != 1 || c.Bens != 2 || c.Aberta == nil || *c.Aberta != aberta.ID || c.FechadaEm == nil {
		t.Fatalf("1 ambiente ativo, 2 bens distintos, aberta e última fechada: %+v", c)
	}

	r = a.chamar(t, http.MethodGet, "/inventory/units?q="+codigoVazia, contagem, nil)
	if so, mm := lista[unidadeDoInventarioResp](t, r); len(so) != 1 || mm.Total != 1 || so[0].ID != vazia {
		t.Fatalf("q por código: %s", r.Corpo)
	}
	exigir(t, a.chamar(t, http.MethodGet, "/inventory/units?active=false&q="+codigoCheia, contagem, nil), http.StatusOK, "filtro active")
	if r := a.chamar(t, http.MethodGet, "/inventory/units", "", nil); r.Status != http.StatusUnauthorized {
		t.Fatalf("sem token: %d", r.Status)
	}
}

// A reserva da avaria entra por `reservation_id` OU `reservation_code` — o
// código é o que a governanta tem na folha do check-out. A resposta mostra só
// o código: nada do hóspede.
func TestAvariaPeloCodigoDaReserva(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	quarto := a.comodo(t, g, unidade, "Quarto", "quarto", 1)
	abajur := a.bem(t, g, "Abajur", nil)
	a.colocar(t, g, quarto.ID, abajur.ID, 2)
	reservaID, codigo := a.reserva(t)

	base := map[string]any{"room_id": quarto.ID, "item_id": abajur.ID, "kind": "quebrado", "qty": 1}
	com := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	r := a.chamar(t, http.MethodPost, "/inventory/issues", g, com(map[string]any{"reservation_code": strings.ToLower(codigo)}))
	exigir(t, r, http.StatusCreated, "relatando pelo código")
	type avaria struct {
		ID            uuid.UUID  `json:"id"`
		ReservaID     *uuid.UUID `json:"reservation_id"`
		ReservaCodigo *string    `json:"reservation_code"`
	}
	av := dado[avaria](t, r)
	if av.ReservaID == nil || *av.ReservaID != reservaID || av.ReservaCodigo == nil || *av.ReservaCodigo != codigo {
		t.Fatalf("o código vira o vínculo com a reserva: %+v", av)
	}
	if strings.Contains(string(r.Corpo), "Sigiloso") || strings.Contains(string(r.Corpo), "contact") {
		t.Fatalf("a avaria não pode trazer dado do hóspede: %s", r.Corpo)
	}

	r = a.chamar(t, http.MethodPost, "/inventory/issues", g, com(map[string]any{"reservation_id": reservaID, "reservation_code": codigo}))
	exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "id e código juntos")
	r = a.chamar(t, http.MethodPost, "/inventory/issues", g, com(map[string]any{"reservation_code": "WH-1999-0001"}))
	exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "código inexistente")
	if r.detalhes(t)["reservation_code"] == nil {
		t.Fatalf("o 422 deveria apontar reservation_code: %s", r.Corpo)
	}

	// PATCH: o código em null desvincula; o código de volta vincula; as duas
	// chaves juntas são 422.
	caminho := "/inventory/issues/" + av.ID.String()
	r = a.chamar(t, http.MethodPatch, caminho, g, `{"reservation_code": null}`)
	exigir(t, r, http.StatusOK, "desvinculando pelo código")
	if d := dado[avaria](t, r); d.ReservaID != nil || d.ReservaCodigo != nil {
		t.Fatalf("null desvincula: %+v", d)
	}
	r = a.chamar(t, http.MethodPatch, caminho, g, map[string]any{"reservation_code": codigo})
	exigir(t, r, http.StatusOK, "vinculando pelo código")
	if d := dado[avaria](t, r); d.ReservaID == nil || *d.ReservaID != reservaID {
		t.Fatalf("o código vincula de novo: %+v", d)
	}
	exigirErro(t, a.chamar(t, http.MethodPatch, caminho, g, `{"reservation_id": null, "reservation_code": "X"}`),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "duas portas no PATCH")
	r = a.chamar(t, http.MethodPatch, caminho, g, `{"reservation_id": null}`)
	if d := dado[avaria](t, r); r.Status != http.StatusOK || d.ReservaID != nil {
		t.Fatalf("reservation_id null também desvincula: %d %s", r.Status, r.Corpo)
	}

	// PUT pelo código.
	r = a.chamar(t, http.MethodPut, caminho, g, map[string]any{"kind": "avariado", "qty": 2, "reservation_code": codigo})
	exigir(t, r, http.StatusOK, "substituindo pelo código")
	if d := dado[avaria](t, r); d.ReservaCodigo == nil || *d.ReservaCodigo != codigo {
		t.Fatalf("PUT pelo código: %+v", d)
	}
}

// O nome do CSV só leva a unidade quando o filtro é `unit_id`; o recorte só
// por cômodo leva o slug da propriedade, como o contrato fixa.
func TestNomeDoCSVPorComodoUsaASlugDaCasa(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	sala := a.comodo(t, g, unidade, "Sala", "sala", 1)
	var slug string
	if err := a.pool.QueryRow(a.ctx, `SELECT slug FROM properties WHERE id = $1`, a.propriedade).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	r := a.chamar(t, http.MethodGet, "/inventory/export?room_id="+sala.ID.String(), g, nil)
	exigir(t, r, http.StatusOK, "exportando por cômodo")
	hoje := time.Now().In(fortaleza(t)).Format(time.DateOnly)
	if cd := r.Cabecalho.Get("Content-Disposition"); cd != fmt.Sprintf(`attachment; filename="inventario-%s-%s.csv"`, slug, hoje) {
		t.Fatalf("Content-Disposition = %q", cd)
	}
}
