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

// qbCenario é uma casa montada pela API com um alvo REAL para cada `{id}` das
// 40 operações. Com id sorteado, um 404 do handler poderia esconder a ausência
// de checagem de permissão — com id real, só o RBAC separa o 403 do 200.
type qbCenario struct {
	Unidade, Destino uuid.UUID // Destino é o alvo da cópia e de uma segunda conferência
	CodigoUnidade    string
	Cozinha          uuid.UUID
	Deposito         uuid.UUID // cômodo sem histórico: alvo de DELETE /rooms
	Prato            uuid.UUID
	Solto            uuid.UUID // bem sem colocação: alvo de DELETE e de POST placement
	Colocacao        string
	Midia            uuid.UUID
	Conferencia      uuid.UUID
	Linha            uuid.UUID
	Avaria           uuid.UUID
}

func (a *ambiente) qbMontarCenario(t *testing.T, f *qbFaxina, token string) qbCenario {
	t.Helper()
	prop := a.qbPropriedadePadrao(t)
	var c qbCenario
	c.Unidade, c.CodigoUnidade = f.qbUnidade(t, prop, "")
	c.Destino, _ = f.qbUnidade(t, prop, "")
	c.Cozinha = a.qbComodo(t, token, c.Unidade, "Cozinha", "cozinha", 1)
	c.Deposito = a.qbComodo(t, token, c.Unidade, "Depósito", "outro", 9)
	c.Prato = a.qbBem(t, f, token, "Prato raso QA "+qbSufixo(), ptrInt64(1890))
	c.Solto = a.qbBem(t, f, token, "Travessa QA "+qbSufixo(), nil)
	c.Colocacao = a.qbColocar(t, token, c.Cozinha, c.Prato, 12)

	r := a.qbEnviarFoto(t, token, "file", "prato.png", "image/png", qbPNG(t, 64, 48))
	c.Midia = qbDado[qbIDResp](t, r, http.StatusCreated, "POST /inventory/media").ID
	exigirStatusQB(t, a.chamar(t, http.MethodPut, "/inventory/items/"+c.Prato.String()+"/photos", token,
		map[string]any{"media_ids": []uuid.UUID{c.Midia}}), http.StatusOK, "galeria")

	conf := a.qbAbrir(t, token, c.Unidade)
	c.Conferencia = conf.ID
	c.Linha = conf.linhaDo(t, c.Prato).ID

	r = a.chamar(t, http.MethodPost, "/inventory/issues", token, map[string]any{
		"room_id": c.Cozinha, "item_id": c.Prato, "kind": "quebrado", "qty": 1,
	})
	c.Avaria = qbDado[qbIDResp](t, r, http.StatusCreated, "POST /inventory/issues").ID
	return c
}

func ptrInt64(v int64) *int64 { return &v }

func exigirStatusQB(t *testing.T, r resposta, status int, contexto string) {
	t.Helper()
	if r.Status != status {
		t.Fatalf("%s: status %d, esperado %d — %s", contexto, r.Status, status, r.Corpo)
	}
}

// caminho troca os parâmetros do path do contrato pelos alvos do cenário.
func (c qbCenario) caminho(op qbOperacao) string {
	p := op.Path
	switch {
	case strings.HasPrefix(p, "/rooms/{id}"):
		alvo := c.Cozinha
		if op.Metodo == http.MethodDelete {
			alvo = c.Deposito
		}
		p = strings.Replace(p, "{id}", alvo.String(), 1)
	case strings.HasPrefix(p, "/inventory/items/{id}"):
		alvo := c.Prato
		if op.Metodo == http.MethodDelete {
			alvo = c.Solto
		}
		p = strings.Replace(p, "{id}", alvo.String(), 1)
	case strings.HasPrefix(p, "/inventory/placements/{id}"):
		p = strings.Replace(p, "{id}", c.Colocacao, 1)
	case strings.HasPrefix(p, "/inventory/media/{id}"):
		p = strings.Replace(p, "{id}", c.Midia.String(), 1)
	case p == "/units/{id}/inventory":
		p = strings.Replace(p, "{id}", c.Unidade.String(), 1)
	case p == "/units/{id}/inventory/copy":
		p = strings.Replace(p, "{id}", c.Destino.String(), 1)
	case strings.HasPrefix(p, "/inventory/counts/{id}"):
		p = strings.Replace(p, "{id}", c.Conferencia.String(), 1)
		p = strings.Replace(p, "{lineId}", c.Linha.String(), 1)
	case strings.HasPrefix(p, "/inventory/issues/{id}"):
		p = strings.Replace(p, "{id}", c.Avaria.String(), 1)
	}
	if strings.Contains(p, "{") {
		panic("parâmetro sem alvo no cenário: " + op.Path)
	}
	return p
}

// corpo devolve um corpo VÁLIDO para a operação: se a permissão faltar no
// servidor, a escrita teria efeito — e é isso que o teste depois confere.
func (c qbCenario) corpo(op qbOperacao) any {
	switch op.chave() {
	case "POST /rooms":
		return map[string]any{"unit_id": c.Unidade, "name": "Invasão " + qbSufixo(), "kind": "sala"}
	case "PUT /rooms/{id}":
		return map[string]any{"name": "Renomeada", "kind": "sala"}
	case "PATCH /rooms/{id}":
		return map[string]any{"name": "Renomeada"}
	case "POST /inventory/items":
		return map[string]any{"name": "Invasão " + qbSufixo(), "category": "louca"}
	case "PUT /inventory/items/{id}":
		return map[string]any{"name": "Renomeado", "category": "louca"}
	case "PATCH /inventory/items/{id}":
		return map[string]any{"name": "Renomeado"}
	case "PUT /inventory/items/{id}/photos":
		return map[string]any{"media_ids": []uuid.UUID{}}
	case "POST /inventory/placements":
		return map[string]any{"room_id": c.Cozinha, "item_id": c.Solto, "expected_qty": 1}
	case "PUT /inventory/placements/{id}", "PATCH /inventory/placements/{id}":
		return map[string]any{"expected_qty": 99}
	case "POST /units/{id}/inventory/copy":
		return map[string]any{"source_unit_id": c.Unidade}
	case "POST /inventory/counts":
		return map[string]any{"unit_id": c.Destino}
	case "PUT /inventory/counts/{id}", "PATCH /inventory/counts/{id}":
		return map[string]any{"note": "invasão"}
	case "PATCH /inventory/counts/{id}/lines/{lineId}":
		return map[string]any{"counted_qty": 1}
	case "POST /inventory/counts/{id}/close":
		return map[string]any{"raise_issues": false}
	case "POST /inventory/issues":
		return map[string]any{"room_id": c.Cozinha, "item_id": c.Prato, "kind": "quebrado", "qty": 7}
	case "PUT /inventory/issues/{id}":
		return map[string]any{"kind": "quebrado", "qty": 7}
	case "PATCH /inventory/issues/{id}":
		return map[string]any{"qty": 7}
	}
	return nil
}

// executar faz a operação com o token dado (vazio = sem Authorization).
func (a *ambiente) qbExecutar(t *testing.T, c qbCenario, op qbOperacao, token string) resposta {
	t.Helper()
	caminho := c.caminho(op)
	if op.chave() == "POST /inventory/media" {
		return a.qbEnviarFoto(t, token, "file", "invasao.png", "image/png", qbPNG(t, 8, 8))
	}
	return a.chamar(t, op.Metodo, caminho, token, c.corpo(op))
}

// estado lê o que as escritas recusadas não podem ter mudado.
type qbEstado struct {
	NomeCozinha, NomePrato string
	QtdColocada            int
	StatusConferencia      string
	LinhaContada           bool
	QtdAvaria              int
	ComodosNoDestino       int
	ConferenciasNoDestino  int
	Bens, Midias, Avarias  int
	GaleriaDoPrato         int
	DepositoExiste         bool
	SoltoExiste            bool
}

func (a *ambiente) qbLerEstado(t *testing.T, c qbCenario) qbEstado {
	t.Helper()
	var e qbEstado
	q := func(sql string, dest any, args ...any) {
		if err := a.pool.QueryRow(a.ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("lendo estado (%s): %v", sql, err)
		}
	}
	q(`SELECT name FROM unit_rooms WHERE id = $1`, &e.NomeCozinha, c.Cozinha)
	q(`SELECT name FROM inventory_items WHERE id = $1`, &e.NomePrato, c.Prato)
	q(`SELECT expected_qty FROM room_inventory WHERE room_id = $1 AND item_id = $2`, &e.QtdColocada, c.Cozinha, c.Prato)
	q(`SELECT status FROM inventory_counts WHERE id = $1`, &e.StatusConferencia, c.Conferencia)
	q(`SELECT counted_qty IS NOT NULL FROM inventory_count_lines WHERE id = $1`, &e.LinhaContada, c.Linha)
	q(`SELECT qty FROM inventory_issues WHERE id = $1`, &e.QtdAvaria, c.Avaria)
	q(`SELECT count(*) FROM unit_rooms WHERE unit_id = $1`, &e.ComodosNoDestino, c.Destino)
	q(`SELECT count(*) FROM inventory_counts WHERE unit_id = $1`, &e.ConferenciasNoDestino, c.Destino)
	q(`SELECT count(*) FROM room_inventory WHERE room_id = $1`, &e.Bens, c.Cozinha)
	q(`SELECT count(*) FROM inventory_media`, &e.Midias)
	q(`SELECT count(*) FROM inventory_issues WHERE room_id = $1`, &e.Avarias, c.Cozinha)
	q(`SELECT count(*) FROM inventory_item_media WHERE item_id = $1`, &e.GaleriaDoPrato, c.Prato)
	q(`SELECT EXISTS (SELECT 1 FROM unit_rooms WHERE id = $1)`, &e.DepositoExiste, c.Deposito)
	q(`SELECT EXISTS (SELECT 1 FROM inventory_items WHERE id = $1)`, &e.SoltoExiste, c.Solto)
	return e
}

// ─────────────────────────── 1. RBAC pela matriz do seed ────────────────────

// Em linguagem de negócio: o corretor vende estadia; ele não entra na casa para
// contar taça, não vê a foto do quarto por dentro e não apaga avaria. Logado com
// o perfil REAL do seed, ele bate em porta fechada nas 40 operações — e a porta
// diz exatamente qual permissão faltou, a mesma que o contrato declara.
func TestBensQACorretorDoSeedRecebe403NasQuarentaOperacoes(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "bens-admin", a.qbPerfilDoSeed(t, "admin"))
	corretor := a.criarUsuario(t, "bens-corretor", a.qbPerfilDoSeed(t, "corretor"))
	f := a.qbFaxina(t, admin, corretor)
	c := a.qbMontarCenario(t, f, admin.Token)
	antes := a.qbLerEstado(t, c)

	for _, op := range qbOperacoesDeBens(t) {
		t.Run(op.chave(), func(t *testing.T) {
			r := a.qbExecutar(t, c, op, corretor.Token)
			e := qbErro(t, r, http.StatusForbidden, "FORBIDDEN", "corretor em "+op.chave())
			if e.Details["resource"] != op.Recurso || e.Details["action"] != op.Acao {
				t.Errorf("o 403 diz que faltou %v:%v; o x-rbac do contrato é %s:%s",
					e.Details["resource"], e.Details["action"], op.Recurso, op.Acao)
			}
		})
	}
	if depois := a.qbLerEstado(t, c); depois != antes {
		t.Fatalf("o corretor recebeu 403 e mesmo assim algo mudou:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// Em linguagem de negócio: a foto do inventário mostra o interior da casa — a
// TV, o bar, a fechadura. Sem sessão, NENHUMA das 40 operações responde, nem a
// foto com id real, nem com token inválido, nem com token na query string.
func TestBensQASemSessaoAsQuarentaOperacoesDao401(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "bens-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qbFaxina(t, admin)
	c := a.qbMontarCenario(t, f, admin.Token)
	antes := a.qbLerEstado(t, c)

	for _, op := range qbOperacoesDeBens(t) {
		t.Run(op.chave(), func(t *testing.T) {
			qbErro(t, a.qbExecutar(t, c, op, ""), http.StatusUnauthorized, "UNAUTHORIZED", "sem token em "+op.chave())
			qbErro(t, a.qbExecutar(t, c, op, "nao.e.um.jwt"), http.StatusUnauthorized, "UNAUTHORIZED", "token inválido em "+op.chave())
		})
	}

	// A foto, pelas URLs que a própria API devolve, e por atalhos que um <img>
	// poderia tentar: token na query e esquema Basic.
	foto := "/api/v1/inventory/media/" + c.Midia.String()
	for _, url := range []string{foto, foto + "?size=thumb", foto + "?access_token=" + admin.Token, foto + "?token=" + admin.Token} {
		r := a.qbBaixar(t, "", url)
		if r.Status != http.StatusUnauthorized {
			t.Errorf("GET %s sem Authorization: %d — a foto do interior da casa saiu sem sessão", strings.Replace(url, admin.Token, "<token>", 1), r.Status)
		}
		if strings.HasPrefix(r.Headers.Get("Content-Type"), "image/") {
			t.Errorf("GET %s sem sessão devolveu bytes de imagem", strings.Replace(url, admin.Token, "<token>", 1))
		}
	}
	req, _ := http.NewRequestWithContext(a.ctx, http.MethodGet, a.servidor.URL+foto, nil)
	req.SetBasicAuth(admin.Email, senhaDeIntegracao)
	if r := a.fazer(t, req); r.Status != http.StatusUnauthorized {
		t.Errorf("GET da foto com Basic auth: %d, esperado 401", r.Status)
	}

	if depois := a.qbLerEstado(t, c); depois != antes {
		t.Fatalf("chamadas sem sessão mudaram o estado:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// Em linguagem de negócio: `usuario` e `admin` são quem opera a casa. Com o
// perfil REAL do seed, cada um percorre as 40 operações e recebe o status de
// sucesso que o contrato promete em cada uma — 200, 201 ou 204. Não basta "não
// deu 403": uma rota que responde 500 para quem pode é tão inútil quanto uma
// fechada.
func TestBensQAUsuarioEAdminDoSeedPercorremAsQuarentaOperacoes(t *testing.T) {
	for _, perfil := range []string{"usuario", "admin"} {
		t.Run(perfil, func(t *testing.T) {
			a := subirAPI(t)
			u := a.criarUsuario(t, "bens-"+perfil, a.qbPerfilDoSeed(t, perfil))
			f := a.qbFaxina(t, u)
			cobertas := a.qbJornadaCompleta(t, f, u.Token)

			for _, op := range qbOperacoesDeBens(t) {
				if !cobertas[op.chave()] {
					t.Errorf("%s não foi exercida pela jornada de %s", op.chave(), perfil)
				}
			}
		})
	}
}

// qbJornadaCompleta executa as 40 operações numa ordem possível e confere o
// status de sucesso de cada uma. Devolve o conjunto das que rodaram.
func (a *ambiente) qbJornadaCompleta(t *testing.T, f *qbFaxina, tk string) map[string]bool {
	t.Helper()
	prop := a.qbPropriedadePadrao(t)
	u1, codigo := f.qbUnidade(t, prop, "")
	u2, _ := f.qbUnidade(t, prop, "")
	cobertas := map[string]bool{}

	passo := func(chave string, r resposta, esperado int) resposta {
		t.Helper()
		cobertas[chave] = true
		if r.Status != esperado {
			t.Fatalf("%s: status %d, o contrato promete %d — %s", chave, r.Status, esperado, r.Corpo)
		}
		return r
	}
	id := func(r resposta) uuid.UUID { return qbDado[qbIDResp](t, r, r.Status, "lendo id").ID }

	passo("GET /inventory/units", a.chamar(t, http.MethodGet, "/inventory/units?q="+codigo, tk, nil), 200)

	r1 := id(passo("POST /rooms", a.chamar(t, http.MethodPost, "/rooms", tk,
		map[string]any{"unit_id": u1, "name": "Cozinha", "kind": "cozinha", "sort_order": 1}), 201))
	passo("GET /rooms", a.chamar(t, http.MethodGet, "/rooms?unit_id="+u1.String(), tk, nil), 200)
	passo("GET /rooms/{id}", a.chamar(t, http.MethodGet, "/rooms/"+r1.String(), tk, nil), 200)
	passo("PUT /rooms/{id}", a.chamar(t, http.MethodPut, "/rooms/"+r1.String(), tk,
		map[string]any{"name": "Cozinha", "kind": "cozinha", "sort_order": 1, "active": true}), 200)
	passo("PATCH /rooms/{id}", a.chamar(t, http.MethodPatch, "/rooms/"+r1.String(), tk, map[string]any{"sort_order": 2}), 200)

	nome := "Prato QA " + qbSufixo()
	i1 := id(passo("POST /inventory/items", a.chamar(t, http.MethodPost, "/inventory/items", tk,
		map[string]any{"name": nome, "category": "louca", "replacement_cost_cents": 1890}), 201))
	f.item(i1)
	passo("GET /inventory/items", a.chamar(t, http.MethodGet, "/inventory/items?q="+nome[len(nome)-8:], tk, nil), 200)
	passo("GET /inventory/items/{id}", a.chamar(t, http.MethodGet, "/inventory/items/"+i1.String(), tk, nil), 200)
	passo("PUT /inventory/items/{id}", a.chamar(t, http.MethodPut, "/inventory/items/"+i1.String(), tk,
		map[string]any{"name": nome, "category": "louca", "replacement_cost_cents": 1890}), 200)
	passo("PATCH /inventory/items/{id}", a.chamar(t, http.MethodPatch, "/inventory/items/"+i1.String(), tk,
		map[string]any{"description": "borda dourada"}), 200)

	m1 := id(passo("POST /inventory/media", a.qbEnviarFoto(t, tk, "file", "prato.png", "image/png", qbPNG(t, 40, 30)), 201))
	passo("GET /inventory/media/{id}", a.qbBaixar(t, tk, "/api/v1/inventory/media/"+m1.String()), 200)
	passo("PUT /inventory/items/{id}/photos", a.chamar(t, http.MethodPut, "/inventory/items/"+i1.String()+"/photos", tk,
		map[string]any{"media_ids": []uuid.UUID{m1}}), 200)
	passo("GET /inventory/items/{id}/photos", a.chamar(t, http.MethodGet, "/inventory/items/"+i1.String()+"/photos", tk, nil), 200)

	col := r1.String() + "_" + i1.String()
	passo("POST /inventory/placements", a.chamar(t, http.MethodPost, "/inventory/placements", tk,
		map[string]any{"room_id": r1, "item_id": i1, "expected_qty": 12}), 201)
	passo("GET /inventory/placements", a.chamar(t, http.MethodGet, "/inventory/placements?room_id="+r1.String(), tk, nil), 200)
	passo("GET /inventory/placements/{id}", a.chamar(t, http.MethodGet, "/inventory/placements/"+col, tk, nil), 200)
	passo("PUT /inventory/placements/{id}", a.chamar(t, http.MethodPut, "/inventory/placements/"+col, tk,
		map[string]any{"expected_qty": 12, "note": "padrão da casa"}), 200)
	passo("PATCH /inventory/placements/{id}", a.chamar(t, http.MethodPatch, "/inventory/placements/"+col, tk,
		map[string]any{"note": nil}), 200)

	passo("GET /units/{id}/inventory", a.chamar(t, http.MethodGet, "/units/"+u1.String()+"/inventory", tk, nil), 200)
	passo("POST /units/{id}/inventory/copy", a.chamar(t, http.MethodPost, "/units/"+u2.String()+"/inventory/copy", tk,
		map[string]any{"source_unit_id": u1}), 200)
	passo("GET /inventory/export", a.chamar(t, http.MethodGet, "/inventory/export?unit_id="+u1.String(), tk, nil), 200)

	conf := qbDado[qbConferencia](t, passo("POST /inventory/counts", a.chamar(t, http.MethodPost, "/inventory/counts", tk,
		map[string]any{"unit_id": u1}), 201), 201, "abrindo")
	cid := conf.ID.String()
	passo("GET /inventory/counts", a.chamar(t, http.MethodGet, "/inventory/counts?unit_id="+u1.String(), tk, nil), 200)
	passo("GET /inventory/counts/{id}", a.chamar(t, http.MethodGet, "/inventory/counts/"+cid, tk, nil), 200)
	passo("PUT /inventory/counts/{id}", a.chamar(t, http.MethodPut, "/inventory/counts/"+cid, tk, map[string]any{"note": "plantão"}), 200)
	passo("PATCH /inventory/counts/{id}", a.chamar(t, http.MethodPatch, "/inventory/counts/"+cid, tk, map[string]any{"note": "manhã"}), 200)
	passo("PATCH /inventory/counts/{id}/lines/{lineId}", a.qbContar(t, tk, conf.ID, conf.linhaDo(t, i1).ID, 11), 200)
	passo("POST /inventory/counts/{id}/close", a.chamar(t, http.MethodPost, "/inventory/counts/"+cid+"/close", tk, map[string]any{}), 200)

	outra := a.qbAbrir(t, tk, u2)
	passo("DELETE /inventory/counts/{id}", a.chamar(t, http.MethodDelete, "/inventory/counts/"+outra.ID.String(), tk, nil), 204)

	x1 := id(passo("POST /inventory/issues", a.chamar(t, http.MethodPost, "/inventory/issues", tk,
		map[string]any{"room_id": r1, "item_id": i1, "kind": "quebrado", "qty": 1}), 201))
	passo("GET /inventory/issues", a.chamar(t, http.MethodGet, "/inventory/issues?room_id="+r1.String(), tk, nil), 200)
	passo("GET /inventory/issues/{id}", a.chamar(t, http.MethodGet, "/inventory/issues/"+x1.String(), tk, nil), 200)
	passo("PUT /inventory/issues/{id}", a.chamar(t, http.MethodPut, "/inventory/issues/"+x1.String(), tk,
		map[string]any{"kind": "quebrado", "qty": 2}), 200)
	passo("PATCH /inventory/issues/{id}", a.chamar(t, http.MethodPatch, "/inventory/issues/"+x1.String(), tk,
		map[string]any{"resolution": "reposto"}), 200)
	passo("DELETE /inventory/issues/{id}", a.chamar(t, http.MethodDelete, "/inventory/issues/"+x1.String(), tk, nil), 204)

	passo("DELETE /inventory/placements/{id}", a.chamar(t, http.MethodDelete, "/inventory/placements/"+col, tk, nil), 204)
	r3 := a.qbComodo(t, tk, u1, "Depósito", "outro", 9)
	passo("DELETE /rooms/{id}", a.chamar(t, http.MethodDelete, "/rooms/"+r3.String(), tk, nil), 204)
	i3 := a.qbBem(t, f, tk, "Bem errado QA "+qbSufixo(), nil)
	passo("DELETE /inventory/items/{id}", a.chamar(t, http.MethodDelete, "/inventory/items/"+i3.String(), tk, nil), 204)

	return cobertas
}

// Em linguagem de negócio: a casa quer um perfil "só de olhar" — quem confere
// a lista pelo celular sem poder mudar nada. Com só `inventory.goods:ver`, ele
// lê as 15 rotas de leitura (inclusive o seletor de unidades), não abre o
// cadastro comercial (`/units` é de `inventory`), e não consegue escrever em
// nenhuma das 25 — e nada muda no banco.
func TestBensQAPerfilSoDeVerLeTudoENaoEscreveNada(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "bens-admin", a.qbPerfilDoSeed(t, "admin"))
	leitor := a.criarUsuario(t, "bens-leitor", a.criarPerfil(t, "bens_so_ver", []auth.Permissao{
		{Resource: "inventory.goods", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	}))
	f := a.qbFaxina(t, admin, leitor)
	c := a.qbMontarCenario(t, f, admin.Token)
	antes := a.qbLerEstado(t, c)

	leituras, escritas := 0, 0
	for _, op := range qbOperacoesDeBens(t) {
		t.Run(op.chave(), func(t *testing.T) {
			r := a.qbExecutar(t, c, op, leitor.Token)
			if op.Acao == auth.AcaoVer {
				leituras++
				if r.Status != http.StatusOK {
					t.Fatalf("leitura negada a quem tem inventory.goods:ver: %d — %s", r.Status, r.Corpo)
				}
				return
			}
			escritas++
			e := qbErro(t, r, http.StatusForbidden, "FORBIDDEN", "escrita de quem só vê")
			if e.Details["action"] != op.Acao {
				t.Errorf("o 403 aponta a ação %v; o contrato exige %s", e.Details["action"], op.Acao)
			}
		})
	}
	if leituras != 15 || escritas != 25 {
		t.Errorf("o contrato tem 15 leituras e 25 escritas na tag Bens; contei %d e %d", leituras, escritas)
	}

	// O seletor de unidade responde, e a unidade do cenário está nele.
	r := a.chamar(t, http.MethodGet, "/inventory/units?q="+c.CodigoUnidade, leitor.Token, nil)
	unidades, _ := qbLista[qbIDResp](t, r, "GET /inventory/units")
	if len(unidades) != 1 || unidades[0].ID != c.Unidade {
		t.Fatalf("o seletor de quem só tem inventory.goods:ver deveria trazer a unidade %s: %s", c.CodigoUnidade, r.Corpo)
	}
	// O cadastro comercial continua fechado para ele.
	for _, caminho := range []string{"/units", "/units/" + c.Unidade.String(), "/unit-types", "/properties"} {
		e := qbErro(t, a.chamar(t, http.MethodGet, caminho, leitor.Token, nil), http.StatusForbidden, "FORBIDDEN", "GET "+caminho)
		if e.Details["resource"] != "inventory" {
			t.Errorf("GET %s: o 403 deveria apontar o recurso `inventory` (cadastro comercial); apontou %v", caminho, e.Details["resource"])
		}
	}

	if depois := a.qbLerEstado(t, c); depois != antes {
		t.Fatalf("quem só vê recebeu 403 e mesmo assim algo mudou:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// Em linguagem de negócio: o perfil que cadastra unidades e produtos (`inventory`,
// as quatro ações) NÃO ganha por tabela a tela de contagem, a foto do interior
// nem as avarias. São dois recursos de propósito, e as 40 operações de bens
// respondem 403 apontando `inventory.goods`.
func TestBensQACadastroComercialNaoAlcancaNenhumaRotaDeBens(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "bens-admin", a.qbPerfilDoSeed(t, "admin"))
	var permissoes []auth.Permissao
	for _, acao := range auth.AcoesValidas {
		permissoes = append(permissoes, auth.Permissao{Resource: "inventory", Action: acao, Scope: auth.EscopoAll})
	}
	comercial := a.criarUsuario(t, "bens-comercial", a.criarPerfil(t, "so_cadastro", permissoes))
	f := a.qbFaxina(t, admin, comercial)
	c := a.qbMontarCenario(t, f, admin.Token)
	antes := a.qbLerEstado(t, c)

	// A conta funciona no recurso dela — o 403 abaixo não é sessão quebrada.
	exigirStatusQB(t, a.chamar(t, http.MethodGet, "/units", comercial.Token, nil), http.StatusOK, "GET /units do cadastro comercial")

	for _, op := range qbOperacoesDeBens(t) {
		t.Run(op.chave(), func(t *testing.T) {
			e := qbErro(t, a.qbExecutar(t, c, op, comercial.Token), http.StatusForbidden, "FORBIDDEN", "cadastro comercial em "+op.chave())
			if e.Details["resource"] != "inventory.goods" {
				t.Errorf("o 403 deveria apontar inventory.goods; apontou %v", e.Details["resource"])
			}
		})
	}
	if depois := a.qbLerEstado(t, c); depois != antes {
		t.Fatalf("o cadastro comercial recebeu 403 e mesmo assim algo mudou:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// A matriz do seed, lida do banco: corretor sem nenhuma célula de bens;
// usuario e admin com as quatro. É a premissa dos testes acima — se o seed
// mudar, o motivo do vermelho aparece aqui primeiro, em vez de num 403 sem dono.
func TestBensQAMatrizDoSeedParaInventoryGoods(t *testing.T) {
	a := subirAPI(t)
	for perfil, esperado := range map[string]string{"admin": "criar,editar,excluir,ver", "usuario": "criar,editar,excluir,ver", "corretor": ""} {
		id := a.qbPerfilDoSeed(t, perfil)
		var acoes string
		if err := a.pool.QueryRow(a.ctx, `
			SELECT coalesce(string_agg(action || CASE WHEN scope = 'all' THEN '' ELSE ':' || scope END, ',' ORDER BY action), '')
			  FROM role_permissions WHERE role_id = $1 AND resource_code = 'inventory.goods'`, id).Scan(&acoes); err != nil {
			t.Fatal(err)
		}
		if acoes != esperado {
			t.Errorf("perfil %s no seed: inventory.goods = [%s], esperado [%s]", perfil, acoes, esperado)
		}
	}
}
