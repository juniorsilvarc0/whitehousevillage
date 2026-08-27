//go:build integration

package inventario_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/inventario"
)

// Perfis do teste, montados a partir do MESMO catálogo que o seed grava
// (`inventory`, cmd/seed/acesso.go). Não há escopo `own` aqui: apartamento não
// tem dono no sentido do RBAC.
const (
	celulaVer     = inventario.Recurso + ":ver"
	celulaCriar   = inventario.Recurso + ":criar"
	celulaEditar  = inventario.Recurso + ":editar"
	celulaExcluir = inventario.Recurso + ":excluir"
)

// ─────────────────────────── Permissão ──────────────────────────────

// Os três perfis da instalação, no que interessa a este módulo: quem
// administra o inventário, quem só o consulta e quem não tem nada com ele.
//
// A barreira real é o middleware; o que o painel esconde a partir de /auth/me é
// conveniência visual. É por isso que o teste bate na porta em vez de perguntar
// ao service.
func TestPermissaoNosTresPerfis(t *testing.T) {
	a := subir(t)

	gestor := a.token(t, a.perfil(t, "inv_gestor", celulaVer, celulaCriar, celulaEditar, celulaExcluir))
	consulta := a.token(t, a.perfil(t, "inv_consulta", celulaVer))
	// O corretor do seed tem reservas e orçamentos, e NÃO tem inventário.
	corretor := a.token(t, a.perfil(t, "inv_corretor", "reservations:ver", "quotes:criar"))

	produtoID, _ := a.produtoDireto(t, "one_member")

	casos := []struct {
		nome    string
		metodo  string
		caminho string
		corpo   any
		gestor  int
		leitura int
		sem     int
	}{
		{"listar produtos", http.MethodGet, "/unit-types", nil,
			http.StatusOK, http.StatusOK, http.StatusForbidden},
		{"ver produto", http.MethodGet, "/unit-types/" + produtoID.String(), nil,
			http.StatusOK, http.StatusOK, http.StatusForbidden},
		{"criar unidade", http.MethodPost, "/units",
			map[string]any{"code": "IT-" + sufixo(), "name": "Unidade de integração"},
			http.StatusCreated, http.StatusForbidden, http.StatusForbidden},
		{"editar produto", http.MethodPatch, "/unit-types/" + produtoID.String(),
			map[string]any{"sort_order": 901},
			http.StatusOK, http.StatusForbidden, http.StatusForbidden},
		{"listar unidades", http.MethodGet, "/units", nil,
			http.StatusOK, http.StatusOK, http.StatusForbidden},
		{"ver propriedade", http.MethodGet, "/properties", nil,
			http.StatusOK, http.StatusOK, http.StatusForbidden},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if r := a.chamar(t, c.metodo, c.caminho, gestor, c.corpo); r.Status != c.gestor {
				t.Errorf("gestor: status %d, esperado %d — %s", r.Status, c.gestor, r.Corpo)
			} else if c.metodo == http.MethodPost && r.Status == http.StatusCreated {
				// Limpa o que o gestor acabou de criar.
				criada := dado[struct {
					ID uuid.UUID `json:"id"`
				}](t, r)
				a.executar(t, `DELETE FROM units WHERE id = $1`, criada.ID)
			}
			if r := a.chamar(t, c.metodo, c.caminho, consulta, c.corpo); r.Status != c.leitura {
				t.Errorf("só leitura: status %d, esperado %d — %s", r.Status, c.leitura, r.Corpo)
			}
			if r := a.chamar(t, c.metodo, c.caminho, corretor, c.corpo); r.Status != c.sem {
				t.Errorf("sem inventário: status %d, esperado %d — %s", r.Status, c.sem, r.Corpo)
			}
		})
	}
}

func TestSemTokenNaoEntra(t *testing.T) {
	a := subir(t)

	if r := a.chamar(t, http.MethodGet, "/units", "", nil); r.Status != http.StatusUnauthorized {
		t.Fatalf("status %d, esperado 401 — %s", r.Status, r.Corpo)
	}
}

// ─────────────────────────── Paginação ──────────────────────────────

// A listagem é paginada e ordenada por dado, não por acaso: `meta` precisa
// bater com o que veio, e duas páginas seguidas não podem repetir nem pular
// linha — sem ordem determinística, uma unidade some sem nunca ter aparecido.
func TestPaginacaoDaListagem(t *testing.T) {
	a := subir(t)
	leitor := a.token(t, a.perfil(t, "inv_pag", celulaVer))

	type unidade struct {
		ID     uuid.UUID `json:"id"`
		Codigo string    `json:"code"`
	}

	primeira := a.chamar(t, http.MethodGet, "/units?per_page=3&page=1", leitor, nil)
	if primeira.Status != http.StatusOK {
		t.Fatalf("status %d — %s", primeira.Status, primeira.Corpo)
	}
	pagina1, meta1 := lista[unidade](t, primeira)

	if len(pagina1) != 3 {
		t.Fatalf("página cheia deveria trazer 3 itens; veio %d", len(pagina1))
	}
	if meta1.PerPage != 3 || meta1.Page != 1 {
		t.Fatalf("meta não reflete o pedido: %+v", meta1)
	}
	if meta1.Total < 8 {
		t.Fatalf("o seed tem 8 unidades; total = %d", meta1.Total)
	}
	esperadas := int((meta1.Total + 2) / 3)
	if meta1.TotalPages != esperadas {
		t.Fatalf("total_pages = %d, esperado %d para total %d", meta1.TotalPages, esperadas, meta1.Total)
	}

	segunda := a.chamar(t, http.MethodGet, "/units?per_page=3&page=2", leitor, nil)
	pagina2, _ := lista[unidade](t, segunda)

	vistos := map[uuid.UUID]bool{}
	for _, u := range pagina1 {
		vistos[u.ID] = true
	}
	for _, u := range pagina2 {
		if vistos[u.ID] {
			t.Fatalf("a unidade %s apareceu nas duas páginas: a ordenação não é determinística", u.Codigo)
		}
	}
}

// A ordem do catálogo comercial é dado (`sort_order`), não alfabética.
func TestProdutosSaemNaOrdemDoCatalogo(t *testing.T) {
	a := subir(t)
	leitor := a.token(t, a.perfil(t, "inv_ordem", celulaVer))

	type produto struct {
		Codigo string `json:"code"`
		Ordem  int    `json:"sort_order"`
	}
	r := a.chamar(t, http.MethodGet, "/unit-types?per_page=100", leitor, nil)
	produtos, _ := lista[produto](t, r)

	if len(produtos) < 4 {
		t.Fatalf("o seed tem 4 produtos; vieram %d", len(produtos))
	}
	for i := 1; i < len(produtos); i++ {
		if produtos[i].Ordem < produtos[i-1].Ordem {
			t.Fatalf("ordem quebrada entre %s (%d) e %s (%d)",
				produtos[i-1].Codigo, produtos[i-1].Ordem, produtos[i].Codigo, produtos[i].Ordem)
		}
	}
}

// A capacidade da Completa é DECLARADA (24) e não somada — a soma das oito
// unidades daria 40. Este teste existe para ninguém "corrigir" isso depois.
func TestCapacidadeDaCompletaEhDeclaradaENaoSomada(t *testing.T) {
	a := subir(t)
	leitor := a.token(t, a.perfil(t, "inv_cap", celulaVer))

	type produto struct {
		Codigo     string `json:"code"`
		Capacidade int    `json:"capacity"`
	}
	r := a.chamar(t, http.MethodGet, "/unit-types?q=completa", leitor, nil)
	produtos, _ := lista[produto](t, r)

	if len(produtos) == 0 {
		t.Fatal("a White House Completa não apareceu na busca")
	}
	if produtos[0].Capacidade != 24 {
		t.Fatalf("capacity = %d; a Completa é 24 declarado, não 40 somado", produtos[0].Capacidade)
	}
}

// ─────────────────────────── Composição ─────────────────────────────

type membro struct {
	UnidadeID uuid.UUID `json:"unit_id"`
	Codigo    string    `json:"unit_code"`
	Ativa     bool      `json:"active"`
}

func membros(t *testing.T, r resposta) []membro {
	t.Helper()
	return dado[[]membro](t, r)
}

// PUT substitui o conjunto inteiro: o que não vier deixa de fazer parte. É a
// mesma semântica do PUT /roles/{id}/permissions — a tela manda o estado
// completo da grade e não calcula diferença.
func TestPutDeMembrosSubstituiOConjunto(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_comp", celulaVer, celulaEditar))

	produtoID, _ := a.produtoDireto(t, "all_members")
	u1 := a.unidadeDireta(t, "IT-A-"+sufixo())
	u2 := a.unidadeDireta(t, "IT-B-"+sufixo())
	u3 := a.unidadeDireta(t, "IT-C-"+sufixo())
	caminho := "/unit-types/" + produtoID.String() + "/members"

	r := a.chamar(t, http.MethodPut, caminho, gestor,
		map[string]any{"unit_ids": []uuid.UUID{u1, u2}})
	if r.Status != http.StatusOK {
		t.Fatalf("primeira gravação: status %d — %s", r.Status, r.Corpo)
	}
	if got := len(membros(t, r)); got != 2 {
		t.Fatalf("composição inicial = %d itens, esperado 2", got)
	}

	// Segunda gravação: u1 sai, u3 entra.
	r = a.chamar(t, http.MethodPut, caminho, gestor,
		map[string]any{"unit_ids": []uuid.UUID{u3, u2}})
	if r.Status != http.StatusOK {
		t.Fatalf("segunda gravação: status %d — %s", r.Status, r.Corpo)
	}

	depois := membros(t, r)
	if len(depois) != 2 {
		t.Fatalf("composição final = %d itens, esperado 2 (acumulou em vez de substituir)", len(depois))
	}
	for _, m := range depois {
		if m.UnidadeID == u1 {
			t.Fatal("a unidade ausente do corpo continuou na composição")
		}
	}

	// E o GET devolve o mesmo, ordenado por code — a MESMA ordem em que o motor
	// de reservas trava as unidades. Ordens divergentes causam deadlock.
	lido := membros(t, a.chamar(t, http.MethodGet, caminho, gestor, nil))
	for i := 1; i < len(lido); i++ {
		if lido[i].Codigo < lido[i-1].Codigo {
			t.Fatalf("composição fora de ordem: %s antes de %s", lido[i-1].Codigo, lido[i].Codigo)
		}
	}
}

// Composição vazia é erro de validação, não sucesso silencioso: produto
// vendável que não consome unidade nenhuma passa pela constraint sem inserir
// linha em stay_blocks — é overbooking que ninguém vê acontecer.
func TestComposicaoVaziaEhRecusadaNoAllMembers(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_vazia", celulaVer, celulaEditar))

	produtoID, _ := a.produtoDireto(t, "all_members")
	unidade := a.unidadeDireta(t, "IT-V-"+sufixo())
	caminho := "/unit-types/" + produtoID.String() + "/members"

	if r := a.chamar(t, http.MethodPut, caminho, gestor,
		map[string]any{"unit_ids": []uuid.UUID{unidade}}); r.Status != http.StatusOK {
		t.Fatalf("preparo: status %d — %s", r.Status, r.Corpo)
	}

	r := a.chamar(t, http.MethodPut, caminho, gestor, map[string]any{"unit_ids": []string{}})
	if r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, esperado 422 — %s", r.Status, r.Corpo)
	}
	if codigo := r.codigoDoErro(); codigo != "VALIDATION_ERROR" {
		t.Fatalf("code = %s, esperado VALIDATION_ERROR", codigo)
	}

	// E a composição anterior continua de pé: a recusa não pode ter apagado o
	// que já estava lá.
	if got := len(membros(t, a.chamar(t, http.MethodGet, caminho, gestor, nil))); got != 1 {
		t.Fatalf("a composição foi alterada por um pedido recusado: %d itens", got)
	}
}

func TestComposicaoRecusaUnidadeInexistente(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_fora", celulaVer, celulaEditar))

	produtoID, _ := a.produtoDireto(t, "one_member")
	unidade := a.unidadeDireta(t, "IT-F-"+sufixo())

	r := a.chamar(t, http.MethodPut, "/unit-types/"+produtoID.String()+"/members", gestor,
		map[string]any{"unit_ids": []uuid.UUID{unidade, uuid.New()}})
	if r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, esperado 422 — %s", r.Status, r.Corpo)
	}
	if _, apontou := r.detalhes(t)["unit_ids[1]"]; !apontou {
		t.Fatalf("details deveria apontar o índice inválido; veio %v", r.detalhes(t))
	}
}

func TestComposicaoRecusaUnidadeRepetida(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_rep", celulaVer, celulaEditar))

	produtoID, _ := a.produtoDireto(t, "one_member")
	unidade := a.unidadeDireta(t, "IT-R-"+sufixo())

	r := a.chamar(t, http.MethodPut, "/unit-types/"+produtoID.String()+"/members", gestor,
		map[string]any{"unit_ids": []uuid.UUID{unidade, unidade}})
	if r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, esperado 422 — %s", r.Status, r.Corpo)
	}
}

// ─────────────────────────── Exclusão bloqueada ─────────────────────

// Não deixar apagar o chão de quem está de pé: produto com reserva viva não sai
// do ar, e a recusa diz quantas reservas o seguram.
func TestExclusaoDeProdutoComReservaAtivaEhBloqueada(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_del_prod", celulaVer, celulaExcluir))

	produtoID, _ := a.produtoDireto(t, "one_member")
	reservaID := a.reserva(t, produtoID, "confirmed")
	caminho := "/unit-types/" + produtoID.String()

	r := a.chamar(t, http.MethodDelete, caminho, gestor, nil)
	if r.Status != http.StatusConflict {
		t.Fatalf("status %d, esperado 409 — %s", r.Status, r.Corpo)
	}
	if codigo := r.codigoDoErro(); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE", codigo)
	}
	if n, _ := r.detalhes(t)["reservations_count"].(float64); n != 1 {
		t.Fatalf("details.reservations_count = %v, esperado 1", r.detalhes(t)["reservations_count"])
	}

	if a.ativoDoProduto(t, produtoID) != true {
		t.Fatal("o produto foi desativado apesar da recusa")
	}

	// Encerrada a reserva, a desativação passa — e é desativação, não remoção:
	// a linha continua legível para o histórico.
	a.executar(t, `UPDATE reservations SET status = 'cancelled' WHERE id = $1`, reservaID)
	if r := a.chamar(t, http.MethodDelete, caminho, gestor, nil); r.Status != http.StatusNoContent {
		t.Fatalf("status %d, esperado 204 — %s", r.Status, r.Corpo)
	}
	if a.ativoDoProduto(t, produtoID) {
		t.Fatal("o produto continuou ativo depois do DELETE")
	}
}

// Reserva encerrada (checked_out/cancelled) não segura nada: a operação
// terminou, e o cadastro precisa poder ser aposentado.
func TestExclusaoDeProdutoComReservaEncerradaPassa(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_del_ok", celulaVer, celulaExcluir))

	produtoID, _ := a.produtoDireto(t, "one_member")
	a.reserva(t, produtoID, "checked_out")

	if r := a.chamar(t, http.MethodDelete, "/unit-types/"+produtoID.String(), gestor, nil); r.Status != http.StatusNoContent {
		t.Fatalf("status %d, esperado 204 — %s", r.Status, r.Corpo)
	}
}

// Unidade com estadia futura não sai do ar: a operação deixaria de saber qual
// quarto preparar para uma reserva de pé.
func TestExclusaoDeUnidadeComOcupacaoFuturaEhBloqueada(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_del_un", celulaVer, celulaExcluir))

	unidade := a.unidadeDireta(t, "IT-O-"+sufixo())
	a.bloqueio(t, unidade, "confirmed")

	r := a.chamar(t, http.MethodDelete, "/units/"+unidade.String(), gestor, nil)
	if r.Status != http.StatusConflict {
		t.Fatalf("status %d, esperado 409 — %s", r.Status, r.Corpo)
	}
	if codigo := r.codigoDoErro(); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE", codigo)
	}
	if n, _ := r.detalhes(t)["blocks_count"].(float64); n != 1 {
		t.Fatalf("details.blocks_count = %v, esperado 1", r.detalhes(t)["blocks_count"])
	}
}

// Unidade que ainda compõe produto também é recusada: a venda seguinte daquele
// produto travaria menos unidades do que promete.
func TestExclusaoDeUnidadeQueCompoeProdutoEhBloqueada(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_del_comp", celulaVer, celulaEditar, celulaExcluir))

	produtoID, codigoDoProduto := a.produtoDireto(t, "all_members")
	unidade := a.unidadeDireta(t, "IT-M-"+sufixo())
	caminho := "/unit-types/" + produtoID.String() + "/members"

	if r := a.chamar(t, http.MethodPut, caminho, gestor,
		map[string]any{"unit_ids": []uuid.UUID{unidade}}); r.Status != http.StatusOK {
		t.Fatalf("preparo: status %d — %s", r.Status, r.Corpo)
	}

	r := a.chamar(t, http.MethodDelete, "/units/"+unidade.String(), gestor, nil)
	if r.Status != http.StatusConflict {
		t.Fatalf("status %d, esperado 409 — %s", r.Status, r.Corpo)
	}
	produtos, _ := r.detalhes(t)["unit_types"].([]any)
	if len(produtos) != 1 || produtos[0] != codigoDoProduto {
		t.Fatalf("details.unit_types = %v, esperado [%s]", r.detalhes(t)["unit_types"], codigoDoProduto)
	}

	// Tirada da composição, a unidade pode ser aposentada. A ordem importa: a
	// decisão de mudar o que o produto consome fica explícita e registrada.
	unidadeSobra := a.unidadeDireta(t, "IT-N-"+sufixo())
	if r := a.chamar(t, http.MethodPut, caminho, gestor,
		map[string]any{"unit_ids": []uuid.UUID{unidadeSobra}}); r.Status != http.StatusOK {
		t.Fatalf("trocando a composição: status %d — %s", r.Status, r.Corpo)
	}
	if r := a.chamar(t, http.MethodDelete, "/units/"+unidade.String(), gestor, nil); r.Status != http.StatusNoContent {
		t.Fatalf("status %d, esperado 204 — %s", r.Status, r.Corpo)
	}
}

// ─────────────────────────── CRUD e chave natural ───────────────────

// Quem decide a colisão de `code` é a constraint UNIQUE (property_id, code) —
// não um SELECT antes do INSERT, que perderia a corrida contra outra
// requisição no mesmo instante.
func TestCodigoRepetidoEh409CodeInUse(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_dup", celulaVer, celulaCriar))

	codigo := "IT-D-" + sufixo()
	corpo := map[string]any{"code": codigo, "name": "Unidade de integração"}

	primeira := a.chamar(t, http.MethodPost, "/units", gestor, corpo)
	if primeira.Status != http.StatusCreated {
		t.Fatalf("status %d, esperado 201 — %s", primeira.Status, primeira.Corpo)
	}
	criada := dado[struct {
		ID uuid.UUID `json:"id"`
	}](t, primeira)
	t.Cleanup(func() { a.executar(t, `DELETE FROM units WHERE id = $1`, criada.ID) })

	segunda := a.chamar(t, http.MethodPost, "/units", gestor, corpo)
	if segunda.Status != http.StatusConflict {
		t.Fatalf("status %d, esperado 409 — %s", segunda.Status, segunda.Corpo)
	}
	if codigoDoErro := segunda.codigoDoErro(); codigoDoErro != "CODE_IN_USE" {
		t.Fatalf("code = %s, esperado CODE_IN_USE", codigoDoErro)
	}
}

// PUT é substituição integral: o opcional ausente volta ao padrão do schema.
// PATCH é o contrário — ausente não mexe, e `null` limpa.
func TestPutSubstituiEPatchPreserva(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_put", celulaVer, celulaCriar, celulaEditar))

	type unidade struct {
		ID     uuid.UUID `json:"id"`
		Codigo string    `json:"code"`
		Andar  *string   `json:"floor"`
		Ordem  int       `json:"sort_order"`
	}

	codigo := "IT-P-" + sufixo()
	criada := dado[unidade](t, a.chamar(t, http.MethodPost, "/units", gestor, map[string]any{
		"code": codigo, "name": "Unidade de integração", "floor": "Térreo", "sort_order": 42,
	}))
	t.Cleanup(func() { a.executar(t, `DELETE FROM units WHERE id = $1`, criada.ID) })

	if criada.Andar == nil || *criada.Andar != "Térreo" || criada.Ordem != 42 {
		t.Fatalf("o POST não gravou o que veio: %+v", criada)
	}

	caminho := "/units/" + criada.ID.String()

	// PATCH sem `floor`: o andar continua lá.
	depoisDoPatch := dado[unidade](t, a.chamar(t, http.MethodPatch, caminho, gestor,
		map[string]any{"sort_order": 43}))
	if depoisDoPatch.Andar == nil || depoisDoPatch.Ordem != 43 {
		t.Fatalf("PATCH mexeu no que não veio no corpo: %+v", depoisDoPatch)
	}

	// PATCH com `floor: null`: limpa.
	depoisDoNulo := dado[unidade](t, a.chamar(t, http.MethodPatch, caminho, gestor,
		map[string]any{"floor": nil}))
	if depoisDoNulo.Andar != nil {
		t.Fatalf("floor: null deveria limpar; veio %v", *depoisDoNulo.Andar)
	}

	// PUT sem `sort_order`: volta ao padrão 0.
	depoisDoPut := dado[unidade](t, a.chamar(t, http.MethodPut, caminho, gestor,
		map[string]any{"code": codigo, "name": "Unidade renomeada"}))
	if depoisDoPut.Ordem != 0 {
		t.Fatalf("PUT deveria devolver sort_order ao padrão; veio %d", depoisDoPut.Ordem)
	}
}

// Filtro pela composição: "quais unidades este produto consome" é a pergunta do
// mapa de ocupação.
func TestFiltroDeUnidadesPorProduto(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_filtro", celulaVer, celulaEditar))

	produtoID, _ := a.produtoDireto(t, "all_members")
	u1 := a.unidadeDireta(t, "IT-G-"+sufixo())
	u2 := a.unidadeDireta(t, "IT-H-"+sufixo())
	a.unidadeDireta(t, "IT-I-"+sufixo()) // fora da composição

	if r := a.chamar(t, http.MethodPut, "/unit-types/"+produtoID.String()+"/members", gestor,
		map[string]any{"unit_ids": []uuid.UUID{u1, u2}}); r.Status != http.StatusOK {
		t.Fatalf("preparo: status %d — %s", r.Status, r.Corpo)
	}

	type unidade struct {
		ID uuid.UUID `json:"id"`
	}
	r := a.chamar(t, http.MethodGet,
		"/units?per_page=100&unit_type_id="+produtoID.String(), gestor, nil)
	unidades, m := lista[unidade](t, r)

	if m.Total != 2 || len(unidades) != 2 {
		t.Fatalf("o filtro deveria trazer exatamente as 2 da composição; veio total=%d itens=%d", m.Total, len(unidades))
	}
}

func TestIdDesconhecidoEh404(t *testing.T) {
	a := subir(t)
	leitor := a.token(t, a.perfil(t, "inv_404", celulaVer))

	for _, caminho := range []string{
		"/units/" + uuid.NewString(),
		"/unit-types/" + uuid.NewString(),
		"/unit-types/" + uuid.NewString() + "/members",
		"/units/nao-e-uuid",
	} {
		if r := a.chamar(t, http.MethodGet, caminho, leitor, nil); r.Status != http.StatusNotFound {
			t.Errorf("%s: status %d, esperado 404 — %s", caminho, r.Status, r.Corpo)
		}
	}
}

// ─────────────────────────── Propriedade ────────────────────────────

func TestPatchDePropriedadeRecusaFusoForaDeAmerica(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_fuso", celulaVer, celulaEditar))

	caminho := "/properties/" + a.propriedade.String()
	r := a.chamar(t, http.MethodPatch, caminho, gestor, map[string]any{"timezone": "Europe/Lisbon"})
	if r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, esperado 422 — %s", r.Status, r.Corpo)
	}

	// E o fuso da casa continua o que era: mudá-lo reinterpreta "hoje" no
	// sistema inteiro.
	type propriedade struct {
		Fuso string `json:"timezone"`
	}
	atual := dado[propriedade](t, a.chamar(t, http.MethodGet, caminho, gestor, nil))
	if atual.Fuso != "America/Fortaleza" {
		t.Fatalf("timezone = %s, esperado America/Fortaleza", atual.Fuso)
	}
}

func (a *ambiente) ativoDoProduto(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var ativo bool
	if err := a.pool.QueryRow(a.ctx, `SELECT active FROM unit_types WHERE id = $1`, id).Scan(&ativo); err != nil {
		t.Fatalf("lendo active do produto: %v", err)
	}
	return ativo
}

// ─────────────────── Desativação por PATCH/PUT (CRÍTICO 1) ──────────

// A outra metade do DELETE: `active:false` por PATCH e por PUT é a MESMA
// decisão de tirar a unidade do ar, e precisa das mesmas guardas.
//
// Sem isso, a revisão adversarial mediu o caminho inteiro: PATCH desativa a
// unidade que compõe a White House Completa (all_members), e a venda seguinte
// da casa inteira trava sete unidades em vez de oito — porque a consulta de
// candidatas filtra `u.active`. A oitava linha nunca é inserida, então a
// `EXCLUDE` não tem sobreposição para detectar: a casa "exclusiva" é vendida
// pelo preço de oito com um estranho dormindo na oitava unidade.
func TestPatchNaoDesativaUnidadeQueODeleteRecusa(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_patch_off", celulaVer, celulaEditar, celulaExcluir))

	produtoID, codigoDoProduto := a.produtoDireto(t, "all_members")
	unidade := a.unidadeDireta(t, "IT-P-"+sufixo())
	if r := a.chamar(t, http.MethodPut, "/unit-types/"+produtoID.String()+"/members", gestor,
		map[string]any{"unit_ids": []uuid.UUID{unidade}}); r.Status != http.StatusOK {
		t.Fatalf("preparo da composição: status %d — %s", r.Status, r.Corpo)
	}

	caminho := "/units/" + unidade.String()

	// O DELETE já recusava. É o mesmo pedido, por outro verbo.
	if r := a.chamar(t, http.MethodDelete, caminho, gestor, nil); r.Status != http.StatusConflict {
		t.Fatalf("controle — DELETE deveria recusar: status %d — %s", r.Status, r.Corpo)
	}

	r := a.chamar(t, http.MethodPatch, caminho, gestor, map[string]any{"active": false})
	if r.Status != http.StatusConflict {
		t.Fatalf("PATCH active=false: status %d, esperado 409 — %s", r.Status, r.Corpo)
	}
	if codigo := r.codigoDoErro(); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE — %s", codigo, r.Corpo)
	}
	produtos, _ := r.detalhes(t)["unit_types"].([]any)
	if len(produtos) != 1 || produtos[0] != codigoDoProduto {
		t.Fatalf("details.unit_types = %v, esperado [%s]", r.detalhes(t)["unit_types"], codigoDoProduto)
	}
	if !a.ativoDaUnidade(t, unidade) {
		t.Fatal("a unidade foi desativada pelo PATCH apesar da recusa")
	}

	// PUT é substituição integral e carrega `active` como qualquer outro campo:
	// a mesma guarda vale, senão o buraco só muda de verbo.
	corpoDoPut := map[string]any{"code": "IT-P2-" + sufixo(), "name": "Unidade de integração", "active": false}
	if r := a.chamar(t, http.MethodPut, caminho, gestor, corpoDoPut); r.Status != http.StatusConflict {
		t.Fatalf("PUT active=false: status %d, esperado 409 — %s", r.Status, r.Corpo)
	}
	if !a.ativoDaUnidade(t, unidade) {
		t.Fatal("a unidade foi desativada pelo PUT apesar da recusa")
	}
}

// PATCH de produto com reserva viva: mesma simetria do DELETE.
func TestPatchNaoDesativaProdutoComReservaAtiva(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_patch_prod", celulaVer, celulaEditar))

	produtoID, _ := a.produtoDireto(t, "one_member")
	a.reserva(t, produtoID, "confirmed")

	r := a.chamar(t, http.MethodPatch, "/unit-types/"+produtoID.String(), gestor,
		map[string]any{"active": false})
	if r.Status != http.StatusConflict {
		t.Fatalf("status %d, esperado 409 — %s", r.Status, r.Corpo)
	}
	if codigo := r.codigoDoErro(); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE — %s", codigo, r.Corpo)
	}
	if !a.ativoDoProduto(t, produtoID) {
		t.Fatal("o produto foi desativado pelo PATCH apesar da recusa")
	}
}

// O caminho de VOLTA do CRÍTICO 1, montado como a revisão o mediu.
//
// Estado de partida (o que a Fase 1 deixou nos bancos): uma unidade INATIVA que
// continua na composição de um produto `all_members`, e uma venda dessa casa
// inteira fechada nesse intervalo — com blocos só nas unidades que estavam
// ativas. A oitava ficou sem `stay_blocks`.
//
// Reativá-la a devolve ao inventário vendável DENTRO das datas de uma estadia
// exclusiva já vendida, e a `EXCLUDE` não tem como defender: não existe linha
// com que a próxima venda possa colidir. O desfecho medido pela revisão foi um
// hóspede de outro produto dormindo dentro da casa alugada por inteiro.
// TestBancoRecusaVenderACasaInteiraIncompleta é a prova permanente do crítico
// que atravessou três revisões adversariais.
//
// O cenário original: uma unidade fora do ar, a casa inteira vendida entregando
// N-1 unidades pelo preço de N, e a unidade que faltou vendida depois a um
// estranho. Nenhuma constraint via o problema, porque o defeito é a AUSÊNCIA de
// uma linha em `reservation_units`, e constraint nenhuma vê ausência.
//
// A versão anterior deste teste montava esse estado à mão para provar que
// reativar a unidade precisava ser recusado pelo service. Hoje o estado é
// inalcançável: a constraint trigger adiável confere o CONJUNTO no commit. O
// teste passa a cobrar a garantia na origem — e por SQL direto, sem a aplicação
// no caminho, que é onde uma garantia de banco precisa se sustentar.
func TestBancoRecusaVenderACasaInteiraIncompleta(t *testing.T) {
	a := subir(t)

	produtoID, _ := a.produtoDireto(t, "all_members")
	dentro := a.unidadeDireta(t, "IT-RA-"+sufixo())
	deFora := a.unidadeDireta(t, "IT-RB-"+sufixo())
	a.compor(t, produtoID, dentro, deFora)

	contato := a.contato(t)

	// Tudo numa transação, como a produção faz — a constraint é adiada até o
	// commit, então é ele quem precisa recusar.
	tx, err := a.pool.Begin(a.ctx)
	if err != nil {
		t.Fatalf("abrindo transação: %v", err)
	}
	defer func() { _ = tx.Rollback(a.ctx) }()

	var reservaID uuid.UUID
	if err := tx.QueryRow(a.ctx, `
		INSERT INTO reservations (property_id, unit_type_id, contact_id, status,
		                          check_in, check_out, guests_count)
		VALUES ($1, $2, $3, 'confirmed', current_date + 30, current_date + 33, 2)
		RETURNING id`, a.propriedade, produtoID, contato).Scan(&reservaID); err != nil {
		t.Fatalf("criando a reserva: %v", err)
	}

	// Só UMA das duas unidades da composição: a casa vendida sem estar inteira.
	var bloco uuid.UUID
	if err := tx.QueryRow(a.ctx, `
		INSERT INTO stay_blocks (property_id, unit_id, reservation_id, source, status, period)
		VALUES ($1, $2, $3, 'reservation', 'confirmed',
		        daterange(current_date + 30, current_date + 33, '[)'))
		RETURNING id`, a.propriedade, dentro, reservaID).Scan(&bloco); err != nil {
		t.Fatalf("criando o bloco: %v", err)
	}
	if _, err := tx.Exec(a.ctx, `
		INSERT INTO reservation_units (reservation_id, unit_id, stay_block_id)
		VALUES ($1, $2, $3)`, reservaID, dentro, bloco); err != nil {
		t.Fatalf("vinculando a unidade: %v", err)
	}

	err = tx.Commit(a.ctx)
	if err == nil {
		a.executar(t, `DELETE FROM reservations WHERE id = $1`, reservaID)
		t.Fatal("o banco aceitou vender a casa inteira segurando 1 de 2 unidades: " +
			"a unidade que faltou fica vendável a um estranho dentro da estadia " +
			"exclusiva — a invariante da casa inteira não está valendo")
	}
	if !strings.Contains(err.Error(), "sem estar inteira") {
		t.Fatalf("a recusa veio por outro motivo: %v", err)
	}
	t.Logf("o banco recusou no commit, como deve: %v", err)
	_ = deFora
}

// A porta oposta: em vez de desativar a unidade que compõe, compor com uma
// unidade já inativa. O resultado seria o mesmo — produto declarando N unidades
// e venda alocando N-1 —, então a recusa é a mesma.
func TestComposicaoRecusaUnidadeInativa(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_comp_inativa", celulaVer, celulaEditar))

	produtoID, _ := a.produtoDireto(t, "all_members")
	viva := a.unidadeDireta(t, "IT-CV-"+sufixo())
	morta := a.unidadeDiretaAssim(t, "IT-CM-"+sufixo(), false)

	r := a.chamar(t, http.MethodPut, "/unit-types/"+produtoID.String()+"/members", gestor,
		map[string]any{"unit_ids": []uuid.UUID{viva, morta}})
	if r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, esperado 422 — %s", r.Status, r.Corpo)
	}
	if c := r.codigoDoErro(); c != "VALIDATION_ERROR" {
		t.Fatalf("code = %s, esperado VALIDATION_ERROR — %s", c, r.Corpo)
	}
	if _, apontou := r.detalhes(t)["unit_ids[1]"]; !apontou {
		t.Fatalf("details deveria apontar o índice da unidade inativa; veio %v", r.detalhes(t))
	}

	var membros int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM unit_type_members WHERE unit_type_id = $1`, produtoID).Scan(&membros); err != nil {
		t.Fatalf("contando membros: %v", err)
	}
	if membros != 0 {
		t.Fatalf("a composição foi gravada apesar da recusa: %d membros", membros)
	}
}

// ─────────────────────── ALTO 3: trilha de auditoria ────────────────

// "Quem desativou AP-03?" precisa ter resposta. A Fase 1 escreveu 1626 tuplas
// nas tabelas de negócio e ZERO linhas em `audit_log`; este teste bate no
// endpoint de verdade e vai ler a trilha no banco.
func TestEscritaDeUnidadeGravaTrilhaComAutorEOrigem(t *testing.T) {
	a := subir(t)
	perfilID := a.perfil(t, "inv_trilha", celulaVer, celulaCriar, celulaEditar, celulaExcluir)
	gestor, autor := a.tokenComUsuario(t, perfilID)

	criada := dado[struct {
		ID uuid.UUID `json:"id"`
	}](t, a.chamar(t, http.MethodPost, "/units", gestor,
		map[string]any{"code": "IT-T-" + sufixo(), "name": "Unidade auditada"}))
	t.Cleanup(func() {
		a.executar(t, `DELETE FROM audit_log WHERE entity_id = $1`, criada.ID)
		a.executar(t, `DELETE FROM units WHERE id = $1`, criada.ID)
	})
	caminho := "/units/" + criada.ID.String()

	if r := a.chamar(t, http.MethodPatch, caminho, gestor, map[string]any{"sort_order": 42}); r.Status != http.StatusOK {
		t.Fatalf("PATCH: status %d — %s", r.Status, r.Corpo)
	}
	if r := a.chamar(t, http.MethodDelete, caminho, gestor, nil); r.Status != http.StatusNoContent {
		t.Fatalf("DELETE: status %d — %s", r.Status, r.Corpo)
	}

	linhas := a.trilhaDaEntidade(t, "units", criada.ID)
	acoes := []string{}
	for _, l := range linhas {
		acoes = append(acoes, l.Acao)
	}
	esperadas := []string{"units.criado", "units.alterado", "units.desativado"}
	if len(acoes) != len(esperadas) {
		t.Fatalf("trilha = %v, esperadas %v", acoes, esperadas)
	}
	for i, esperada := range esperadas {
		if acoes[i] != esperada {
			t.Fatalf("trilha = %v, esperadas %v", acoes, esperadas)
		}
	}

	// A desativação é a linha que a revisão foi procurar e não achou.
	desativacao := linhas[2]
	if desativacao.Ator != autor {
		t.Fatalf("actor_id = %s, esperado %s", desativacao.Ator, autor)
	}
	if desativacao.IP == nil || *desativacao.IP == "" {
		t.Fatal("ip nulo: o middleware de origem não chegou à trilha")
	}
	if desativacao.RequestID == nil || *desativacao.RequestID == "" {
		t.Fatal("request_id nulo: a linha não é rastreável até a requisição")
	}
	if campo(t, desativacao.Antes, "active") != true {
		t.Fatalf("o `before` da desativação não guarda o estado que saiu do ar: %s", desativacao.Antes)
	}
	if campo(t, desativacao.Depois, "active") != false {
		t.Fatalf("o `after` da desativação não registra a saída do ar: %s", desativacao.Depois)
	}

	// PATCH que só mexe em sort_order não vira "desativado": o verbo separa as
	// decisões, e é ele que torna a consulta da auditoria uma linha só.
	if campo(t, linhas[1].Depois, "sort_order") != float64(42) {
		t.Fatalf("o `after` da alteração não traz o campo que mudou: %s", linhas[1].Depois)
	}
}

// A trilha entra na MESMA transação do negócio: escrita recusada não deixa
// rastro de algo que não aconteceu.
func TestEscritaRecusadaNaoDeixaTrilha(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_trilha_rec", celulaVer, celulaEditar))

	produtoID, _ := a.produtoDireto(t, "all_members")
	unidade := a.unidadeDireta(t, "IT-TR-"+sufixo())
	a.compor(t, produtoID, unidade)
	t.Cleanup(func() { a.executar(t, `DELETE FROM audit_log WHERE entity_id = $1`, unidade) })

	if r := a.chamar(t, http.MethodPatch, "/units/"+unidade.String(), gestor,
		map[string]any{"active": false}); r.Status != http.StatusConflict {
		t.Fatalf("status %d, esperado 409 — %s", r.Status, r.Corpo)
	}
	if linhas := a.trilhaDaEntidade(t, "units", unidade); len(linhas) != 0 {
		t.Fatalf("a recusa deixou %d linha(s) em audit_log: %v", len(linhas), linhas[0].Acao)
	}
}

// campo lê um campo do documento jsonb da trilha. O acesso é por chave, e não
// por substring: o Postgres devolve o jsonb reformatado, e `"active":true`
// simplesmente não existe no texto que volta.
func campo(t *testing.T, documento []byte, chave string) any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(documento, &m); err != nil {
		t.Fatalf("lendo o documento da trilha: %v (%s)", err, documento)
	}
	return m[chave]
}

// O ataque da revisão, contra o inventário REAL do seed — não contra fixtures.
//
// `PATCH /units/{AP-03}` com `{"active": false}` devolvia 200. A partir daí a
// White House Completa (all_members, capacidade 24, R$ 5.500/noite) passava a
// ter 7 candidatas ativas para 8 membros na composição, e era vendida por
// inteiro entregando sete apartamentos. Este teste fixa o desfecho no dado que
// vai para produção.
func TestAP03NaoSaiDoArPeloPatchNoInventarioDoSeed(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_ap03", celulaVer, celulaEditar))

	var (
		unidade  uuid.UUID
		completa uuid.UUID
	)
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM units WHERE property_id = $1 AND code = 'AP-03'`, a.propriedade).Scan(&unidade); err != nil {
		t.Skipf("AP-03 não está no banco (seed do inventário não rodou): %v", err)
	}
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM unit_types WHERE property_id = $1 AND code = 'completa'`, a.propriedade).Scan(&completa); err != nil {
		t.Skipf("produto `completa` não está no banco: %v", err)
	}

	r := a.chamar(t, http.MethodPatch, "/units/"+unidade.String(), gestor, map[string]any{"active": false})
	if r.Status != http.StatusConflict {
		// Se isto passar, a Completa já pode ser vendida com sete unidades.
		a.executar(t, `UPDATE units SET active = true WHERE id = $1`, unidade)
		t.Fatalf("status %d, esperado 409 — %s", r.Status, r.Corpo)
	}
	exclusivos, _ := r.detalhes(t)["exclusive_unit_types"].([]any)
	if len(exclusivos) != 1 || exclusivos[0] != "completa" {
		t.Fatalf("details.exclusive_unit_types = %v, esperado [completa]", r.detalhes(t)["exclusive_unit_types"])
	}

	// A composição continua com as oito, e as oito continuam vendáveis.
	var membros, ativas int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*), count(*) FILTER (WHERE u.active)
		  FROM unit_type_members m JOIN units u ON u.id = m.unit_id
		 WHERE m.unit_type_id = $1`, completa).Scan(&membros, &ativas); err != nil {
		t.Fatalf("contando a composição da Completa: %v", err)
	}
	if membros != ativas {
		t.Fatalf("a Completa declara %d unidades e só %d estão ativas — é a venda de 8 entregando %d",
			membros, ativas, ativas)
	}
}
