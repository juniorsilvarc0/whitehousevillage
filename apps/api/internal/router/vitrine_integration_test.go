//go:build integration

// A vitrine pública (rotas /public/*) pela porta da frente, sem token.
//
// O critério de pronto do passo A2 de docs/unificacao-site-crm.md é "trocar uma
// tarifa no painel muda o preço que o site mostra, sem editar arquivo nenhum".
// Estes testes medem as duas metades disso: o site recebe o MESMO número que o
// painel (mesmo motor), e o que o painel muda chega ao site. E medem as travas
// que uma rota sem sessão precisa: nada de dado de terceiro, nada de desconto,
// janela curta e limite por IP.
package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/inventario"
)

// noitesDaVitrine é a estadia dos orçamentos destes testes: quatro noites
// cumprem o maior mínimo do tarifário (Réveillon e Carnaval), então o teste
// não fica vermelho no dia em que "daqui a 120 dias" cair num deles.
const noitesDaVitrine = 4

type produtoPublicoQA struct {
	ID      uuid.UUID `json:"unit_type_id"`
	Codigo  string    `json:"code"`
	Lotacao int       `json:"capacity"`
	Limpeza int64     `json:"cleaning_cents"`
	APartir *int64    `json:"from_price_cents"`
	Tarifas []struct {
		Tipo  string `json:"date_type"`
		Preco int64  `json:"price_cents"`
	} `json:"rates"`
}

type orcamentoPublicoQA struct {
	Noites   int   `json:"night_count"`
	Subtotal int64 `json:"subtotal_cents"`
	Limpeza  int64 `json:"cleaning_cents"`
	Total    int64 `json:"total_cents"`
	Sinal    int64 `json:"deposit_cents"`
	Saldo    int64 `json:"balance_cents"`
}

// produtoPublico procura o produto pelo código no catálogo público.
func (a *ambiente) produtoPublico(t *testing.T, codigo string) produtoPublicoQA {
	t.Helper()
	lista := envelopeDe[[]produtoPublicoQA](t, a.chamar(t, http.MethodGet, "/public/products", "", nil),
		http.StatusOK, "GET /public/products")
	for _, p := range lista {
		if p.Codigo == codigo {
			return p
		}
	}
	t.Fatalf("o catálogo público não tem %q", codigo)
	return produtoPublicoQA{}
}

func TestVitrineCatalogoServeOSeedSemCampoInterno(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)

	r := a.chamar(t, http.MethodGet, "/public/products", "", nil)
	lista := envelopeDe[[]produtoPublicoQA](t, r, http.StatusOK, "GET /public/products sem token")

	codigos := map[string]produtoPublicoQA{}
	for _, p := range lista {
		codigos[p.Codigo] = p
	}
	for _, c := range []string{"apto-2s", "suite-piscina", "cobertura", "completa"} {
		p, ok := codigos[c]
		if !ok {
			t.Fatalf("o catálogo público não tem %q: o site ficaria sem o card", c)
		}
		if len(p.Tarifas) == 0 || p.APartir == nil {
			t.Errorf("%s sem tarifa no catálogo público — a tabela de preços do site viria vazia", c)
		}
		if p.Lotacao <= 0 {
			t.Errorf("%s com lotação %d", c, p.Lotacao)
		}
	}

	// Vocabulário interno não sai pela porta pública.
	for _, proibido := range []string{"consumes", "rate_table_id", "policy_version", "discount", "owner", "unit_id"} {
		if bytes.Contains(r.Corpo, []byte(proibido)) {
			t.Errorf("GET /public/products expõe %q: %s", proibido, r.Corpo)
		}
	}
}

// O site recebe o MESMO orçamento que o painel, centavo a centavo. Se um dia
// alguém escrever um cálculo próprio para o público, este teste é quem vê.
func TestVitrineOrcamentoEhOMesmoDoPainel(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	painel := a.vendedor(t)
	produto := a.produtoPublico(t, "cobertura")
	entrada, saida := janelaAPartirDeHoje(120, noitesDaVitrine)

	pedido := map[string]any{"unit_type_id": produto.ID, "check_in": entrada, "check_out": saida, "guests_count": 4}
	publico := envelopeDe[orcamentoPublicoQA](t, a.chamar(t, http.MethodPost, "/public/quotes", "", pedido),
		http.StatusOK, "POST /public/quotes")
	interno := envelopeDe[orcamentoPublicoQA](t, a.chamar(t, http.MethodPost, "/quotes", painel.Token, pedido),
		http.StatusOK, "POST /quotes")

	if publico != interno {
		t.Fatalf("o site e o painel discordam do preço da mesma estadia:\n  site:   %+v\n  painel: %+v", publico, interno)
	}
	if publico.Noites != noitesDaVitrine || publico.Total <= 0 || publico.Sinal+publico.Saldo != publico.Total {
		t.Fatalf("orçamento público incoerente: %+v", publico)
	}
}

// O critério de pronto do A2: o painel muda, o site muda — sem arquivo nenhum.
// Mede pela taxa de limpeza, que o painel edita por PATCH /unit-types/{id}.
func TestVitrineMudarAoPainelMudaOPrecoDoSite(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	produto := a.produtoPublico(t, "suite-piscina")

	gestor := a.criarUsuario(t, "vitrine-inv", a.criarPerfil(t, "vitrine-inv", []auth.Permissao{
		{Resource: inventario.Recurso, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: inventario.Recurso, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	}))
	original := produto.Limpeza
	t.Cleanup(func() {
		a.chamar(t, http.MethodPatch, "/unit-types/"+produto.ID.String(), gestor.Token,
			map[string]any{"cleaning_fee_cents": original})
	})

	entrada, saida := janelaAPartirDeHoje(150, noitesDaVitrine)
	pedido := map[string]any{"unit_type_id": produto.ID, "check_in": entrada, "check_out": saida, "guests_count": 2}
	antes := envelopeDe[orcamentoPublicoQA](t, a.chamar(t, http.MethodPost, "/public/quotes", "", pedido),
		http.StatusOK, "orçamento antes")

	const acrescimo = 3700
	envelopeDe[json.RawMessage](t, a.chamar(t, http.MethodPatch, "/unit-types/"+produto.ID.String(), gestor.Token,
		map[string]any{"cleaning_fee_cents": original + acrescimo}), http.StatusOK, "PATCH /unit-types/{id}")

	depois := envelopeDe[orcamentoPublicoQA](t, a.chamar(t, http.MethodPost, "/public/quotes", "", pedido),
		http.StatusOK, "orçamento depois")
	if depois.Total-antes.Total != acrescimo || depois.Limpeza != original+acrescimo {
		t.Fatalf("o painel subiu a limpeza em %d centavos e o site foi de %d para %d (limpeza %d): "+
			"o preço do site não sai do banco", acrescimo, antes.Total, depois.Total, depois.Limpeza)
	}
	if catalogo := a.produtoPublico(t, "suite-piscina"); catalogo.Limpeza != original+acrescimo {
		t.Fatalf("o catálogo público continua com a limpeza antiga (%d)", catalogo.Limpeza)
	}
}

// O público não negocia: desconto e campos internos são recusados, não
// ignorados — ignorar em silêncio deixaria o site achar que pediu desconto.
func TestVitrineRecusaDescontoECamposInternos(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	produto := a.produtoPublico(t, "apto-2s")
	entrada, saida := janelaAPartirDeHoje(120, noitesDaVitrine)

	for campo, valor := range map[string]any{
		"discount_pct":   10,
		"persist":        true,
		"rate_table_id":  uuid.New(),
		"policy_version": 1,
		"contact_id":     uuid.New(),
	} {
		pedido := map[string]any{"unit_type_id": produto.ID, "check_in": entrada, "check_out": saida, "guests_count": 2, campo: valor}
		r := a.chamar(t, http.MethodPost, "/public/quotes", "", pedido)
		if r.Status != http.StatusUnprocessableEntity || r.codigoDeErro(t) != "VALIDATION_ERROR" {
			t.Errorf("POST /public/quotes com %q devolveu %d — esperado 422 VALIDATION_ERROR: %s", campo, r.Status, r.Corpo)
		}
	}

	// Controle positivo: o mesmo pedido sem o campo passa.
	pedido := map[string]any{"unit_type_id": produto.ID, "check_in": entrada, "check_out": saida, "guests_count": 2}
	if r := a.chamar(t, http.MethodPost, "/public/quotes", "", pedido); r.Status != http.StatusOK {
		t.Fatalf("o pedido limpo deveria passar: %d %s", r.Status, r.Corpo)
	}
}

// Dia ocupado é só `available: false`: nem quem, nem por quê, nem quantas
// unidades sobram — e sem preço.
func TestVitrineDiaOcupadoNaoContaNada(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)
	completa := a.produtoPublico(t, "completa")

	// Uma suíte bloqueada fecha a casa inteira (a Completa consome as oito
	// unidades) nas duas noites do bloqueio.
	op := a.operacao(t)
	unidade := a.unidadeDoSeed(t, propriedade, "SP-01")
	de, ate := janelaAPartirDeHoje(40, 2)
	bloqueio := a.chamar(t, http.MethodPost, "/blocks", op.Token, map[string]any{
		"unit_ids": []uuid.UUID{unidade}, "from": de, "to": ate, "source": "maintenance", "note": "vitrine",
	})
	var criados struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	if bloqueio.Status != http.StatusCreated {
		t.Fatalf("POST /blocks: %d %s", bloqueio.Status, bloqueio.Corpo)
	}
	bloqueio.decodificar(t, &criados)
	for _, b := range criados.Data {
		id := b.ID
		t.Cleanup(func() { a.executarQA(t, `DELETE FROM stay_blocks WHERE id = $1`, id) })
	}

	inicio, fim := janelaAPartirDeHoje(38, 6)
	r := a.chamar(t, http.MethodGet, "/public/availability?unit_type_id="+completa.ID.String()+"&from="+inicio+"&to="+fim, "", nil)
	var envelope struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if r.Status != http.StatusOK {
		t.Fatalf("GET /public/availability: %d %s", r.Status, r.Corpo)
	}
	r.decodificar(t, &envelope)
	if len(envelope.Data) != 6 {
		t.Fatalf("esperava 6 noites, vieram %d", len(envelope.Data))
	}

	permitidas := map[string]bool{"date": true, "available": true, "date_type": true, "price_cents": true, "min_nights": true}
	ocupados := 0
	for _, dia := range envelope.Data {
		for chave := range dia {
			if !permitidas[chave] {
				t.Errorf("o calendário público carrega %q: %s", chave, r.Corpo)
			}
		}
		var data string
		var livre bool
		_ = json.Unmarshal(dia["date"], &data)
		_ = json.Unmarshal(dia["available"], &livre)
		dentro := data >= de && data < ate
		if dentro {
			ocupados++
			if livre {
				t.Errorf("%s: a Completa aparece livre com a SP-01 bloqueada", data)
			}
			if string(dia["price_cents"]) != "null" {
				t.Errorf("%s: dia ocupado com preço (%s)", data, dia["price_cents"])
			}
		}
	}
	if ocupados != 2 {
		t.Fatalf("esperava as 2 noites do bloqueio na janela, achei %d", ocupados)
	}
	for _, proibido := range []string{"guest", "reservation", "reason", "maintenance", "SP-01", "vitrine"} {
		if strings.Contains(string(r.Corpo), proibido) {
			t.Errorf("o calendário público vaza %q: %s", proibido, r.Corpo)
		}
	}
}

func TestVitrineJanelaPublicaEhCurtaEPertoDeHoje(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	produto := a.produtoPublico(t, "cobertura")
	base := "/public/availability?unit_type_id=" + produto.ID.String()

	longa1, longa2 := janelaAPartirDeHoje(10, 120)
	distante1, distante2 := janelaAPartirDeHoje(900, 10)
	passado1, passado2 := janelaAPartirDeHoje(-120, 10)
	for nome, caminho := range map[string]string{
		"janela de 120 dias": base + "&from=" + longa1 + "&to=" + longa2,
		"daqui a 900 dias":   base + "&from=" + distante1 + "&to=" + distante2,
		"quatro meses atrás": base + "&from=" + passado1 + "&to=" + passado2,
		"sem produto":        "/public/availability?from=" + passado2 + "&to=" + longa1,
	} {
		r := a.chamar(t, http.MethodGet, caminho, "", nil)
		if r.Status != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d, esperado 422 — %s", nome, r.Status, r.Corpo)
		}
	}

	// Controle positivo: dois meses a partir de hoje, que é o que o site pede.
	ok1, ok2 := janelaAPartirDeHoje(0, 62)
	if r := a.chamar(t, http.MethodGet, base+"&from="+ok1+"&to="+ok2, "", nil); r.Status != http.StatusOK {
		t.Fatalf("a janela que o site usa foi recusada: %d %s", r.Status, r.Corpo)
	}
}

// 120 por minuto por IP: a 121ª responde 429 RATE_LIMITED. O teste usa a
// rota mais barata; o limitador é o mesmo para as quatro.
func TestVitrineLimitaPorIP(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)

	for i := 1; i <= 120; i++ {
		if r := a.chamar(t, http.MethodGet, "/public/policy", "", nil); r.Status != http.StatusOK {
			t.Fatalf("pedido %d de 120 recusado (%d): o limite cortou um visitante normal", i, r.Status)
		}
	}
	r := a.chamar(t, http.MethodGet, "/public/policy", "", nil)
	if r.Status != http.StatusTooManyRequests || r.codigoDeErro(t) != "RATE_LIMITED" {
		t.Fatalf("o 121º pedido do mesmo IP devolveu %d — esperado 429 RATE_LIMITED", r.Status)
	}
}
