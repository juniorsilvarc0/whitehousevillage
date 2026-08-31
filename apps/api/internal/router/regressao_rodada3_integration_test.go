//go:build integration

// Regressão da TERCEIRA rodada de revisão — sempre na COSTURA, sempre pela
// porta da frente.
//
// A lição que fez este arquivo nascer: nas duas rodadas anteriores cada achado
// foi fechado dentro da pasta do módulo dono, com teste verde ao lado da
// correção. E dois deles continuaram de pé para quem usa o produto, porque
// `internal/modules/*` monta só as próprias rotas e nunca o `router.New` que
// roda em produção. Aqui a pergunta é feita ao SERVIDOR INTEIRO: mesmo
// middleware, mesmo RBAC, mesmo banco, mesmo corpo que o contrato declara.
//
// Cada teste deste arquivo guarda um achado que foi REPRODUZIDO ao vivo nesta
// rodada, e o cabeçalho de cada um conta o dano em linguagem de negócio — não
// em nome de função — porque quem lê a falha meses depois precisa saber o que
// o hóspede ou o dono da casa perdeu, não qual `if` sumiu.
package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// ─────────────────────────── Apoio ──────────────────────────────────────────

// chamarComCabecalhos é o `chamar` com cabeçalhos escolhidos pelo caso. Existe
// por um motivo só: o cenário do IP forjado É o cabeçalho.
func (a *ambiente) chamarComCabecalhos(t *testing.T, metodo, caminho, token string, corpo any, cabecalhos map[string]string) resposta {
	t.Helper()

	var body io.Reader
	if corpo != nil {
		bruto, err := json.Marshal(corpo)
		if err != nil {
			t.Fatalf("serializando o corpo: %v", err)
		}
		body = bytes.NewReader(bruto)
	}
	req, err := http.NewRequestWithContext(a.ctx, metodo, a.servidor.URL+PrefixoDaAPI+caminho, body)
	if err != nil {
		t.Fatalf("montando a requisição: %v", err)
	}
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range cabecalhos {
		req.Header.Set(k, v)
	}

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", metodo, caminho, err)
	}
	defer resp.Body.Close()

	lido, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("lendo a resposta: %v", err)
	}
	return resposta{Status: resp.StatusCode, Corpo: lido, Headers: resp.Header}
}

// unidadeNova cadastra uma unidade física pela API e a leva embora no fim.
func (a *ambiente) unidadeNova(t *testing.T, token, rotulo string) uuid.UUID {
	t.Helper()

	marca := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:6])
	criada := envelopeDe[struct {
		ID uuid.UUID `json:"id"`
	}](t, a.chamar(t, http.MethodPost, "/units", token, map[string]any{
		"code": "QA-" + marca,
		"name": "QA " + rotulo + " " + marca,
	}), http.StatusCreated, "POST /units")

	t.Cleanup(func() {
		a.executarQA(t, `DELETE FROM unit_type_members WHERE unit_id = $1`, criada.ID)
		a.executarQA(t, `DELETE FROM stay_blocks WHERE unit_id = $1`, criada.ID)
		a.executarQA(t, `DELETE FROM units WHERE id = $1`, criada.ID)
	})
	return criada.ID
}

// idsDaComposicao devolve a composição declarada de um produto, direto do banco
// — é o estado que a guarda tem de preservar quando recusa.
func (a *ambiente) idsDaComposicao(t *testing.T, produto uuid.UUID) []uuid.UUID {
	t.Helper()

	linhas, err := a.pool.Query(a.ctx, `
		SELECT m.unit_id FROM unit_type_members m
		  JOIN units u ON u.id = m.unit_id
		 WHERE m.unit_type_id = $1 ORDER BY u.code`, produto)
	if err != nil {
		t.Fatalf("composição do produto: %v", err)
	}
	defer linhas.Close()

	var ids []uuid.UUID
	for linhas.Next() {
		var id uuid.UUID
		if err := linhas.Scan(&id); err != nil {
			t.Fatalf("lendo membro: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

// consumoDoProduto lê `unit_types.consumes` no banco. A resposta HTTP pode
// mentir por caminhos que nem passam pelo service; a coluna, não.
func (a *ambiente) consumoDoProduto(t *testing.T, produto uuid.UUID) string {
	t.Helper()

	var consome string
	if err := a.pool.QueryRow(a.ctx, `SELECT consumes FROM unit_types WHERE id = $1`, produto).Scan(&consome); err != nil {
		t.Fatalf("consumo do produto: %v", err)
	}
	return consome
}

// varrerVendasExclusivasCapengas procura, no banco inteiro da propriedade, uma
// venda de produto `all_members` cujo conjunto de unidades ocupadas seja
// DIFERENTE do conjunto declarado na composição.
//
// A invariante é de CONJUNTO, e não de contagem: trocar uma unidade por outra
// deixa as contagens iguais e os conjuntos diferentes, que é exatamente o
// caminho mais provável (e o que a varredura por contagem não via).
//
//	`faltando` — unidade da composição que a venda NÃO ocupa. É o dano caro: a
//	casa foi alugada inteira e esse quarto continua vendável para um estranho.
//	`sobrando` — unidade que a venda ocupa e que saiu da composição. A estadia
//	hospeda fora do próprio produto, e toda derivação
//	produto→composição→unidades (limpeza, check-in, mapa) a ignora.
func (a *ambiente) varrerVendasExclusivasCapengas(t *testing.T, propriedade uuid.UUID) int {
	t.Helper()

	linhas, err := a.pool.Query(a.ctx, `
		SELECT r.code,
		       COALESCE((SELECT string_agg(u.code, ',' ORDER BY u.code)
		                   FROM unit_type_members m JOIN units u ON u.id = m.unit_id
		                  WHERE m.unit_type_id = ut.id
		                    AND NOT EXISTS (SELECT 1 FROM reservation_units ru
		                                     WHERE ru.reservation_id = r.id AND ru.unit_id = m.unit_id)), ''),
		       COALESCE((SELECT string_agg(u.code, ',' ORDER BY u.code)
		                   FROM reservation_units ru JOIN units u ON u.id = ru.unit_id
		                  WHERE ru.reservation_id = r.id
		                    AND NOT EXISTS (SELECT 1 FROM unit_type_members m
		                                     WHERE m.unit_type_id = ut.id AND m.unit_id = ru.unit_id)), '')
		  FROM reservations r
		  JOIN unit_types ut ON ut.id = r.unit_type_id AND ut.consumes = 'all_members'
		 WHERE r.property_id = $1
		   AND r.status NOT IN ('quote','cancelled','no_show','expired')
		   AND (EXISTS (SELECT 1 FROM unit_type_members m
		                 WHERE m.unit_type_id = ut.id
		                   AND NOT EXISTS (SELECT 1 FROM reservation_units ru
		                                    WHERE ru.reservation_id = r.id AND ru.unit_id = m.unit_id))
		     OR EXISTS (SELECT 1 FROM reservation_units ru
		                 WHERE ru.reservation_id = r.id
		                   AND NOT EXISTS (SELECT 1 FROM unit_type_members m
		                                    WHERE m.unit_type_id = ut.id AND m.unit_id = ru.unit_id)))
		 ORDER BY r.code`, propriedade)
	if err != nil {
		t.Fatalf("varrendo vendas exclusivas: %v", err)
	}
	defer linhas.Close()

	var capengas int
	for linhas.Next() {
		var codigo, faltando, sobrando string
		if err := linhas.Scan(&codigo, &faltando, &sobrando); err != nil {
			t.Fatalf("lendo venda capenga: %v", err)
		}
		capengas++
		t.Errorf("venda de produto exclusivo %s não bate com a composição do produto: "+
			"unidades da composição que ela NÃO ocupa = [%s] (cada uma é um quarto da casa alugada "+
			"inteira que o sistema ainda acha vago); unidades que ela ocupa e que saíram da composição "+
			"= [%s]", codigo, faltando, sobrando)
	}
	if err := linhas.Err(); err != nil {
		t.Fatalf("varrendo vendas exclusivas: %v", err)
	}
	return capengas
}

// ─────────── CRÍTICO — a composição da casa vendida ─────────────────────────

// TestMexerNaComposicaoDaCasaVendidaNaoAbreNenhumaPorta.
//
// EM LINGUAGEM DE NEGÓCIO: uma família fechou a White House inteira para o
// casamento — as oito unidades, três noites, R$ 20.200,00 pagos. Depois disso
// alguém abre a tela de inventário e mexe na definição do produto "Casa
// Completa". Cinco caminhos diferentes levavam ao mesmo estrago, e todos foram
// executados ao vivo contra a API real nesta rodada:
//
//	A) ACRESCENTAR uma unidade à composição — a unidade nova fica LIVRE dentro
//	   das datas da casa alugada, e um estranho é vendido para dentro dela;
//	B) REMOVER uma unidade da composição — a próxima venda da casa inteira sai
//	   com sete unidades pelo preço de oito, e a oitava é vendida a terceiro;
//	C) trocar `consumes` de `all_members` para `one_member` pelo PATCH — quem
//	   pagou pela casa inteira passa a ocupar UM apartamento, e os outros sete
//	   vão ao mercado;
//	D) o mesmo pelo PUT, reenviando o cadastro — a assimetria de verbo: quem
//	   não consegue pelo PATCH consegue salvando o formulário inteiro;
//	E) trocar `one_member` para `all_members` num produto com venda viva — a
//	   invariante quebra do outro lado: a estadia ocupa uma unidade e o produto
//	   passa a declarar quatro.
//
// Nenhum desses caminhos toca em `stay_blocks`, e por isso a `EXCLUDE` do banco
// — que é a defesa da regra 2 do CLAUDE.md — não tem o que recusar: o dano é
// feito pela AUSÊNCIA de uma linha, e constraint nenhuma enxerga linha que não
// existe. A defesa tem de ser a recusa do cadastro.
//
// O teste também guarda os dois CONTROLES POSITIVOS, e eles não são detalhe:
// sem eles, uma guarda que recusasse toda edição de composição passaria neste
// arquivo e trancaria o inventário da casa para sempre.
func TestMexerNaComposicaoDaCasaVendidaNaoAbreNenhumaPorta(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	u := a.operacao(t)
	contato := a.hospedeDaJornada(t, propriedade)
	completa := a.produtoDoSeed(t, propriedade, "completa")
	apto := a.produtoDoSeed(t, propriedade, "apto-2s")
	de, ate := janelaLonge(120, 3)

	composicaoOriginal := a.idsDaComposicao(t, completa)
	if len(composicaoOriginal) != 8 {
		t.Fatalf("a Casa Completa do seed declara %d unidades, esperado 8 — o cenário não alcança o defeito",
			len(composicaoOriginal))
	}

	// A venda. `hold` já é estadia de pé: ela tirou a data do mercado e o
	// hóspede tem em mãos o número das unidades.
	venda := envelopeDe[reservaQA](t, a.chamarComChave(t, http.MethodPost, "/reservations", u.Token, map[string]any{
		"unit_type_id": completa,
		"contact_id":   contato,
		"check_in":     de,
		"check_out":    ate,
		"guests_count": 10,
	}, a.chaveNova(t, "casa-inteira")), http.StatusCreated, "POST /reservations")

	if len(venda.Unidades) != 8 {
		t.Fatalf("a venda da casa inteira saiu com %d unidades (%s) — o cenário não alcança o defeito",
			len(venda.Unidades), codigosDas(venda))
	}

	nova := a.unidadeNova(t, u.Token, "porta-a")
	caminhoMembros := "/unit-types/" + completa.String() + "/members"

	exigirRecusa := func(t *testing.T, porta string, r resposta, dano string) {
		t.Helper()
		if r.Status == http.StatusOK || r.Status == http.StatusCreated || r.Status == http.StatusNoContent {
			t.Errorf("PORTA %s ABERTA (status %d): %s — resposta: %s", porta, r.Status, dano, r.Corpo)
			return
		}
		if r.Status != http.StatusConflict || r.codigoDeErro(t) != "RESOURCE_IN_USE" {
			t.Errorf("porta %s: status %d / code %s, esperado 409 RESOURCE_IN_USE — corpo: %s",
				porta, r.Status, r.codigoDeErro(t), r.Corpo)
			return
		}
		// A recusa tem de NOMEAR a reserva e as datas: sem isso o operador não
		// tem ação e o 409 vira "não dá, tente de novo" para sempre.
		if !strings.Contains(string(r.Corpo), venda.Codigo) {
			t.Errorf("porta %s: a recusa não nomeou a reserva que impede a mudança (%s) — corpo: %s",
				porta, venda.Codigo, r.Corpo)
		}
	}

	// ── A: acrescentar unidade à composição da casa vendida ────────────────
	t.Run("A: acrescentar unidade", func(t *testing.T) {
		exigirRecusa(t, "A", a.chamar(t, http.MethodPut, caminhoMembros, u.Token,
			map[string]any{"unit_ids": append(append([]uuid.UUID{}, composicaoOriginal...), nova)}),
			"a unidade acrescentada fica livre DENTRO das datas da casa alugada inteira, e a próxima "+
				"venda do apartamento coloca um estranho dormindo lá")
	})

	// ── B: remover unidade da composição da casa vendida ───────────────────
	t.Run("B: remover unidade", func(t *testing.T) {
		exigirRecusa(t, "B", a.chamar(t, http.MethodPut, caminhoMembros, u.Token,
			map[string]any{"unit_ids": composicaoOriginal[:len(composicaoOriginal)-1]}),
			"a próxima venda da casa inteira sai com sete unidades pelo preço de oito, e a oitava vai "+
				"ao mercado para outro hóspede")
	})

	// ── C e D: trocar o `consumes` do produto vendido, pelos DOIS verbos ────
	t.Run("C: consumes pelo PATCH", func(t *testing.T) {
		exigirRecusa(t, "C", a.chamar(t, http.MethodPatch, "/unit-types/"+completa.String(), u.Token,
			map[string]any{"consumes": "one_member"}),
			"quem pagou pela casa inteira passa a ocupar UM apartamento e os outros sete vão ao mercado")
	})

	t.Run("D: consumes pelo PUT", func(t *testing.T) {
		produto := envelopeDe[map[string]any](t,
			a.chamar(t, http.MethodGet, "/unit-types/"+completa.String(), u.Token, nil),
			http.StatusOK, "GET /unit-types/{id}")

		exigirRecusa(t, "D", a.chamar(t, http.MethodPut, "/unit-types/"+completa.String(), u.Token,
			map[string]any{
				"code":               produto["code"],
				"name":               produto["name"],
				"capacity":           produto["capacity"],
				"cleaning_fee_cents": produto["cleaning_fee_cents"],
				"consumes":           "one_member",
			}),
			"a mesma troca do PATCH, feita salvando o formulário inteiro — quem fecha um verbo e "+
				"esquece o outro não fechou nada")
	})

	// ── E: o mesmo defeito ao contrário, no produto `one_member` vendido ───
	t.Run("E: one_member vira all_members com venda viva", func(t *testing.T) {
		deE, ateE := janelaLonge(126, 2)
		vendaApto := envelopeDe[reservaQA](t, a.chamarComChave(t, http.MethodPost, "/reservations", u.Token,
			map[string]any{
				"unit_type_id": apto,
				"contact_id":   contato,
				"check_in":     deE,
				"check_out":    ateE,
				"guests_count": 2,
			}, a.chaveNova(t, "apto-vivo")), http.StatusCreated, "POST /reservations")

		r := a.chamar(t, http.MethodPatch, "/unit-types/"+apto.String(), u.Token,
			map[string]any{"consumes": "all_members"})
		if r.Status == http.StatusOK {
			t.Errorf("PORTA E ABERTA: %s ocupa 1 unidade e o produto passou a declarar %d — a estadia "+
				"vendida deixou de bater com o produto que a vendeu, e limpeza, check-in e mapa passam "+
				"a derivar do conjunto errado", vendaApto.Codigo, len(a.idsDaComposicao(t, apto)))
		} else if r.Status != http.StatusConflict || r.codigoDeErro(t) != "RESOURCE_IN_USE" {
			t.Errorf("porta E: status %d / code %s, esperado 409 RESOURCE_IN_USE — corpo: %s",
				r.Status, r.codigoDeErro(t), r.Corpo)
		}
	})

	// ── O estado no banco tem de estar INTACTO depois das cinco tentativas ──
	//
	// Perguntar só o status HTTP não basta: uma guarda que recusasse DEPOIS de
	// gravar responderia 409 com o estrago feito.
	if agora := a.idsDaComposicao(t, completa); len(agora) != len(composicaoOriginal) {
		t.Errorf("a composição da Casa Completa mudou apesar das recusas: %d unidades, eram %d",
			len(agora), len(composicaoOriginal))
	}
	if c := a.consumoDoProduto(t, completa); c != "all_members" {
		t.Errorf("o `consumes` da Casa Completa virou %q apesar das recusas", c)
	}
	if c := a.consumoDoProduto(t, apto); c != "one_member" {
		t.Errorf("o `consumes` do apto-2s virou %q apesar da recusa", c)
	}

	// ── CONTROLES POSITIVOS ────────────────────────────────────────────────
	//
	// Sem estes dois, uma guarda que recusasse toda edição de composição
	// passaria neste teste inteiro — e trancaria o inventário da casa: a tela
	// não conseguiria nem salvar um formulário que ninguém tocou.
	t.Run("controle: reenviar a MESMA composição passa", func(t *testing.T) {
		invertida := make([]uuid.UUID, 0, len(composicaoOriginal))
		for i := len(composicaoOriginal) - 1; i >= 0; i-- {
			invertida = append(invertida, composicaoOriginal[i])
		}
		if r := a.chamar(t, http.MethodPut, caminhoMembros, u.Token,
			map[string]any{"unit_ids": invertida}); r.Status != http.StatusOK {
			t.Errorf("reenviar o mesmo conjunto (em outra ordem) devolveu %d, esperado 200 — a tela de "+
				"inventário não consegue salvar um formulário que ninguém tocou: %s", r.Status, r.Corpo)
		}
	})

	t.Run("controle: crescer o pool de um one_member passa", func(t *testing.T) {
		original := a.idsDaComposicao(t, apto)
		unidade := a.unidadeNova(t, u.Token, "pool")
		r := a.chamar(t, http.MethodPut, "/unit-types/"+apto.String()+"/members", u.Token,
			map[string]any{"unit_ids": append(append([]uuid.UUID{}, original...), unidade)})
		if r.Status != http.StatusOK {
			t.Errorf("acrescentar unidade ao pool de um produto `one_member` com venda viva devolveu %d, "+
				"esperado 200 — a venda escolheu UMA unidade no ato e a segurou; crescer o pool não mexe "+
				"nela, e recusar aqui trancaria a casa (o apto-2s quase sempre tem venda viva): %s",
				r.Status, r.Corpo)
		}
		// Devolve a composição semeada: o que este teste mede não pode mudar o
		// que o próximo teste da suíte encontra.
		if r := a.chamar(t, http.MethodPut, "/unit-types/"+apto.String()+"/members", u.Token,
			map[string]any{"unit_ids": original}); r.Status != http.StatusOK {
			t.Errorf("devolvendo a composição do apto-2s: %d — %s", r.Status, r.Corpo)
		}
	})

	// ── E a prova final, no banco ──────────────────────────────────────────
	if capengas := a.varrerVendasExclusivasCapengas(t, propriedade); capengas > 0 {
		t.Errorf("%d venda(s) exclusiva(s) capenga(s) no banco depois deste cenário", capengas)
	}
}

// ─────────── ALTO — o calendário prometia o que a venda recusa ──────────────

// produtoNaVitrine é a linha de `GET /availability` com os campos que esta
// rodada acrescentou: o motivo da indisponibilidade e o tamanho REAL da
// composição ativa.
type produtoNaVitrine struct {
	UnitTypeID   uuid.UUID `json:"unit_type_id"`
	UnitTypeCode string    `json:"unit_type_code"`
	Consome      string    `json:"consumes"`
	Total        int       `json:"total_units"`
	Ativas       int       `json:"active_units"`
	Dias         []struct {
		Data       string  `json:"date"`
		Disponivel int     `json:"available"`
		Preco      *int64  `json:"price_cents"`
		Motivo     *string `json:"unavailable_reason"`
	} `json:"days"`
}

func (a *ambiente) vitrine(t *testing.T, token, de, ate string) []produtoNaVitrine {
	t.Helper()

	caminho := fmt.Sprintf("/availability?from=%s&to=%s", de, ate)
	return envelopeDe[[]produtoNaVitrine](t, a.chamar(t, http.MethodGet, caminho, token, nil),
		http.StatusOK, "GET /availability")
}

// orcar pede o orçamento pela porta da frente, com o corpo do contrato.
func (a *ambiente) orcar(t *testing.T, token string, produto uuid.UUID, de, ate string, hospedes int) resposta {
	t.Helper()

	return a.chamar(t, http.MethodPost, "/quotes", token, map[string]any{
		"unit_type_id": produto,
		"check_in":     de,
		"check_out":    ate,
		"guests_count": hospedes,
	})
}

// TestOCalendarioNaoPrometeOQueAVendaRecusa.
//
// EM LINGUAGEM DE NEGÓCIO: o corretor abre o calendário, vê "3 disponíveis" na
// data que o cliente pediu, promete ao telefone, monta o orçamento — e a API
// responde que não dá. Ele liga de volta para desdizer. Foram dois estados
// diferentes com o mesmo desfecho, os dois medidos ao vivo nesta rodada:
//
//	sem tarifa publicada:    calendário "3 disponíveis" · orçamento 422 RATE_NOT_FOUND
//	composição incompleta:   calendário "1 disponível"  · reserva 422 COMPOSITION_INCOMPLETE
//	                         (e, pior, o orçamento cobrava o preço das oito unidades)
//
// `available` não é "a unidade está livre": é "eu vendo isto hoje". Unidade
// livre sem tarifa não é vendável, e casa exclusiva com um quarto fora do ar
// também não. Quem promete é a mesma casa que recusa.
//
// A primeira parte varre a vitrine inteira e cobra a implicação forte:
// prometeu ⟹ a venda não recusa POR CONFIGURAÇÃO (tarifa ausente, composição
// pela metade). Recusa por regra de negócio — mínimo de noites, capacidade — é
// outra conversa: o calendário nunca prometeu isso.
func TestOCalendarioNaoPrometeOQueAVendaRecusa(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	u := a.operacao(t)
	de, ate := janelaLonge(140, 4)

	// ── Parte 1: a vitrine inteira, dia a dia ──────────────────────────────
	vitrine := a.vitrine(t, u.Token, de, ate)
	if len(vitrine) == 0 {
		t.Fatal("GET /availability não devolveu produto nenhum — o cenário não mede nada")
	}

	var prometidos int
	for _, produto := range vitrine {
		for _, dia := range produto.Dias {
			if dia.Disponivel <= 0 {
				continue
			}
			prometidos++

			// Um dia prometido tem de ter preço na vitrine: é o número que o
			// corretor lê no telefone.
			if dia.Preco == nil {
				t.Errorf("%s em %s: `available` = %d e `price_cents` nulo — o calendário oferece a "+
					"noite sem saber por quanto", produto.UnitTypeCode, dia.Data, dia.Disponivel)
			}
			if dia.Motivo != nil {
				t.Errorf("%s em %s: `available` = %d e `unavailable_reason` = %q ao mesmo tempo",
					produto.UnitTypeCode, dia.Data, dia.Disponivel, *dia.Motivo)
			}

			seguinte, err := time.Parse("2006-01-02", dia.Data)
			if err != nil {
				t.Fatalf("data ilegível na vitrine: %q", dia.Data)
			}
			r := a.orcar(t, u.Token, produto.UnitTypeID, dia.Data, seguinte.AddDate(0, 0, 1).Format("2006-01-02"), 2)
			if r.Status == http.StatusOK {
				continue
			}
			if codigo := r.codigoDeErro(t); codigo == "RATE_NOT_FOUND" || codigo == "COMPOSITION_INCOMPLETE" {
				t.Errorf("%s em %s: o calendário prometeu %d disponível(is) e o orçamento recusou com %s — "+
					"o corretor promete ao cliente e liga de volta para desdizer",
					produto.UnitTypeCode, dia.Data, dia.Disponivel, codigo)
			}
		}
	}
	if prometidos == 0 {
		t.Fatal("a vitrine não ofereceu uma noite sequer na janela — o cenário não alcança o defeito")
	}

	// ── Parte 2: produto SEM TARIFA não é oferecido ────────────────────────
	//
	// Produto novo, com composição completa e unidade ativa: o único motivo
	// para não vender é não haver preço publicado.
	t.Run("sem tarifa publicada não é oferta", func(t *testing.T) {
		unidade := a.unidadeNova(t, u.Token, "sem-tarifa")
		marca := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:6])
		produto := envelopeDe[struct {
			ID uuid.UUID `json:"id"`
		}](t, a.chamar(t, http.MethodPost, "/unit-types", u.Token, map[string]any{
			"code": "qa-" + strings.ToLower(marca), "name": "QA Sem Tarifa " + marca,
			"capacity": 2, "consumes": "one_member",
		}), http.StatusCreated, "POST /unit-types")
		t.Cleanup(func() {
			a.executarQA(t, `DELETE FROM unit_type_members WHERE unit_type_id = $1`, produto.ID)
			a.executarQA(t, `DELETE FROM unit_types WHERE id = $1`, produto.ID)
		})

		if r := a.chamar(t, http.MethodPut, "/unit-types/"+produto.ID.String()+"/members", u.Token,
			map[string]any{"unit_ids": []uuid.UUID{unidade}}); r.Status != http.StatusOK {
			t.Fatalf("compondo o produto sem tarifa: %d — %s", r.Status, r.Corpo)
		}

		var achado bool
		for _, linha := range a.vitrine(t, u.Token, de, ate) {
			if linha.UnitTypeID != produto.ID {
				continue
			}
			achado = true
			for _, dia := range linha.Dias {
				if dia.Disponivel != 0 {
					t.Errorf("%s em %s: `available` = %d sem tarifa publicada — a venda vai recusar com "+
						"RATE_NOT_FOUND", linha.UnitTypeCode, dia.Data, dia.Disponivel)
				}
				if dia.Motivo == nil || *dia.Motivo != "sem_tarifa" {
					t.Errorf("%s em %s: `unavailable_reason` = %v, esperado \"sem_tarifa\" — sem o motivo "+
						"a gestão não sabe que basta publicar o preço", linha.UnitTypeCode, dia.Data, dia.Motivo)
				}
			}
		}
		if !achado {
			t.Fatal("o produto sem tarifa não apareceu na vitrine: some da tela em vez de aparecer com o motivo")
		}

		r := a.orcar(t, u.Token, produto.ID, de, ate, 2)
		if r.Status != http.StatusUnprocessableEntity || r.codigoDeErro(t) != "RATE_NOT_FOUND" {
			t.Errorf("orçar produto sem tarifa devolveu %d/%s, esperado 422 RATE_NOT_FOUND — corpo: %s",
				r.Status, r.codigoDeErro(t), r.Corpo)
		}
	})

	// ── Parte 3: composição incompleta não é oferta ────────────────────────
	//
	// O estado é alcançado pelo BANCO, e não pelo PATCH: unidade já inativa é
	// dado legado (importação, ou linha anterior à guarda do inventário), e é
	// justamente ele que a disponibilidade precisa enxergar.
	t.Run("composição incompleta não é oferta", func(t *testing.T) {
		completa := a.produtoDoSeed(t, propriedade, "completa")
		alvo := a.unidadeDoSeed(t, propriedade, "AP-03")
		deC, ateC := janelaLonge(160, 2)

		a.executarQA(t, `UPDATE units SET active = false WHERE id = $1`, alvo)
		t.Cleanup(func() { a.executarQA(t, `UPDATE units SET active = true WHERE id = $1`, alvo) })

		var achado bool
		for _, linha := range a.vitrine(t, u.Token, deC, ateC) {
			if linha.UnitTypeID != completa {
				continue
			}
			achado = true
			if linha.Total != 8 {
				t.Errorf("`total_units` = %d, esperado 8 — o tamanho DECLARADO da composição não pode "+
					"encolher junto com a unidade inativa, senão o defeito fica escondido", linha.Total)
			}
			if linha.Ativas != 7 {
				t.Errorf("`active_units` = %d, esperado 7 — é este campo que denuncia o quarto fora do ar",
					linha.Ativas)
			}
			for _, dia := range linha.Dias {
				if dia.Disponivel != 0 {
					t.Errorf("completa em %s: `available` = %d com AP-03 fora do ar — o orçamento cobraria "+
						"o preço das oito unidades e a reserva recusaria com COMPOSITION_INCOMPLETE",
						dia.Data, dia.Disponivel)
				}
				if dia.Motivo == nil || *dia.Motivo != "composicao_incompleta" {
					t.Errorf("completa em %s: `unavailable_reason` = %v, esperado \"composicao_incompleta\"",
						dia.Data, dia.Motivo)
				}
			}
		}
		if !achado {
			t.Fatal("a Casa Completa sumiu da vitrine em vez de aparecer com o motivo")
		}

		r := a.orcar(t, u.Token, completa, deC, ateC, 2)
		if r.Status != http.StatusUnprocessableEntity || r.codigoDeErro(t) != "COMPOSITION_INCOMPLETE" {
			t.Fatalf("orçar a casa com um quarto fora do ar devolveu %d/%s, esperado 422 "+
				"COMPOSITION_INCOMPLETE — corpo: %s", r.Status, r.codigoDeErro(t), r.Corpo)
		}
		// O `details` é o que dá ação a quem está na tela: qual quarto falta.
		if !strings.Contains(string(r.Corpo), "AP-03") {
			t.Errorf("a recusa não nomeou o quarto que falta (AP-03) — o operador fica sem ação: %s", r.Corpo)
		}

		// CONTROLE: o produto `one_member` continua vendendo as unidades que
		// sobraram. Sem ele, uma implementação que fechasse a casa inteira a
		// cada unidade inativa passaria aqui e esvaziaria o estoque.
		apto := a.produtoDoSeed(t, propriedade, "apto-2s")
		if r := a.orcar(t, u.Token, apto, deC, ateC, 2); r.Status != http.StatusOK {
			t.Errorf("com AP-03 fora do ar, orçar o apto-2s (one_member) devolveu %d — uma unidade "+
				"inativa não pode tirar do mercado as que continuam de pé: %s", r.Status, r.Corpo)
		}
	})
}

// ─────────── ALTO — a trilha com o IP que o AUTOR da escrita escolheu ───────

// TestATrilhaNaoRegistraOIPQueOAutorDaEscritaEscolheu.
//
// EM LINGUAGEM DE NEGÓCIO: a trilha de auditoria existe para responder "quem
// tirou AP-03 do ar na véspera do Réveillon, e de onde". Com o middleware de
// IP que estava montado (`chi/middleware.RealIP`, DEPRECADO por três avisos de
// segurança), a resposta do "de onde" era escrita pela própria pessoa que fez a
// escrita: bastava mandar um cabeçalho `X-Forwarded-File` com o endereço que
// ela quisesse. Trilha com endereço escolhido pelo investigado é pior do que
// trilha sem endereço — a investigação começa por uma mentira, e o mesmo valor
// ainda é a chave do limitador do `/auth/login` (IP forjável = força bruta com
// o contador zerado a cada tentativa).
//
// A regra medida aqui: o proxy APENDA o que ele mesmo viu, então numa cadeia
// `X-Forwarded-For` a entrada mais à DIREITA é a única que um salto confiável
// escreveu. Tudo à esquerda dela pode ter vindo do cliente.
func TestATrilhaNaoRegistraOIPQueOAutorDaEscritaEscolheu(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	u := a.operacao(t)
	unidade := a.unidadeNova(t, u.Token, "ip")

	desde := a.agoraNoBanco(t)
	a.limparTrilhaDe(t, u.ID)

	// O forjado é PÚBLICO de propósito. Endereço privado à esquerda seria
	// descartado por qualquer resolvedor razoável e o teste passaria mesmo com
	// o defeito presente — medido: com a varredura invertida (da esquerda para
	// a direita, que é o que o middleware do chi faz) e um `10.x` forjado, o
	// teste continuava verde. Quem forja escolhe o endereço que quer que apareça
	// na trilha, e escolhe um plausível.
	const forjado = "198.51.100.7"   // o que o autor da escrita escreveu
	const verdadeiro = "203.0.113.9" // o que o salto confiável apendou

	r := a.chamarComCabecalhos(t, http.MethodPatch, "/units/"+unidade.String(), u.Token,
		map[string]any{"name": "QA trilha"}, map[string]string{
			"X-Forwarded-For": forjado + ", " + verdadeiro,
			"X-Real-Ip":       "9.9.9.9",
			"User-Agent":      "Painel/1.0 (QA)",
		})
	if r.Status != http.StatusOK {
		t.Fatalf("PATCH /units: status %d — corpo: %s", r.Status, r.Corpo)
	}

	linhas := a.trilhaDe(t, u.ID, desde)
	if len(linhas) == 0 {
		t.Fatalf("a escrita não deixou linha nenhuma em `audit_log` para o ator %s", u.ID)
	}
	for _, l := range linhas {
		if l.IP == nil {
			t.Errorf("%s: `ip` nulo — a trilha não diz de onde a escrita partiu", l.Acao)
			continue
		}
		if *l.IP == forjado || *l.IP == "9.9.9.9" {
			t.Errorf("%s: `audit_log.ip` = %q — é o endereço que o AUTOR da escrita digitou no cabeçalho. "+
				"A trilha passou a registrar o que o investigado quis que ela registrasse", l.Acao, *l.IP)
		}
		if *l.IP != verdadeiro {
			t.Errorf("%s: `audit_log.ip` = %q, esperado %q (a entrada mais à direita do X-Forwarded-For, "+
				"a única que um salto confiável escreve)", l.Acao, *l.IP, verdadeiro)
		}
		if l.UserAgent == nil || !strings.Contains(*l.UserAgent, "Painel/1.0") {
			t.Errorf("%s: `user_agent` = %v, esperado o do cliente", l.Acao, l.UserAgent)
		}
		if l.PropriedadeID == nil || *l.PropriedadeID != propriedade {
			t.Errorf("%s: `property_id` = %v, esperado %s", l.Acao, l.PropriedadeID, propriedade)
		}
	}
}

// ─────────── ALTO — o dinheiro que evaporava na remarcação ──────────────────

// TestRemarcarParaMaisBaratoNaoFazDinheiroDoHospedeEvaporar.
//
// EM LINGUAGEM DE NEGÓCIO: o hóspede pagou a estadia inteira adiantada,
// R$ 3.580,00. Depois pediu para encurtar — remarcou para uma estadia de
// R$ 1.030,00. O sistema emitia a reserva nova já com o sinal antigo colado
// nela, e no cancelamento seguinte devolvia R$ 1.030,00 declarando a conta
// encerrada. Os R$ 2.550,00 de diferença sumiam: não viravam devolução, não
// viravam crédito, não viravam dívida da casa. E o documento que o sistema
// emitia para o razão AFIRMAVA que o hóspede tinha pago R$ 1.030,00.
//
// A invariante que fecha a conta, e que este teste mede pela porta da frente:
//
//	devolvido + retido + crédito == o que o hóspede entregou à casa
func TestRemarcarParaMaisBaratoNaoFazDinheiroDoHospedeEvaporar(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	u := a.operacao(t)
	contato := a.hospedeDaJornada(t, propriedade)
	cobertura := a.produtoDoSeed(t, propriedade, "cobertura")
	de, ate := janelaLonge(200, 4)
	curtoDe, curtoAte := janelaLonge(230, 1)

	original := envelopeDe[reservaQA](t, a.chamarComChave(t, http.MethodPost, "/reservations", u.Token,
		map[string]any{
			"unit_type_id": cobertura,
			"contact_id":   contato,
			"check_in":     de,
			"check_out":    ate,
			"guests_count": 2,
		}, a.chaveNova(t, "cara")), http.StatusCreated, "POST /reservations")

	// Pagamento INTEGRAL adiantado — é o caso em que a diferença é maior, e é
	// caso real: quem fecha alta temporada costuma pagar tudo na hora.
	pago := original.Total
	if r := a.chamarComChave(t, http.MethodPost, "/reservations/"+original.ID.String()+"/confirm", u.Token,
		map[string]any{"deposit_paid_cents": pago}, a.chaveNova(t, "confirm-cara")); r.Status != http.StatusOK {
		t.Fatalf("POST /confirm: status %d — corpo: %s", r.Status, r.Corpo)
	}

	remarcada := a.chamarComChave(t, http.MethodPost, "/reservations/"+original.ID.String()+"/reschedule", u.Token,
		map[string]any{"check_in": curtoDe, "check_out": curtoAte, "reason": "hóspede encurtou a estadia"},
		a.chaveNova(t, "reschedule"))
	if remarcada.Status != http.StatusCreated && remarcada.Status != http.StatusOK {
		t.Fatalf("POST /reschedule: status %d — corpo: %s", remarcada.Status, remarcada.Corpo)
	}

	var envelope struct {
		Data reservaQA `json:"data"`
		Meta struct {
			TotalAnterior int64 `json:"previous_total_cents"`
			Diferenca     int64 `json:"difference_cents"`
			Credito       int64 `json:"credit_cents"`
		} `json:"meta"`
	}
	remarcada.decodificar(t, &envelope)
	nova := envelope.Data

	if nova.Total >= pago {
		t.Fatalf("a estadia nova custou %d e o hóspede pagou %d — o cenário não alcança o defeito "+
			"(ele precisa de estadia nova MAIS BARATA)", nova.Total, pago)
	}
	sobra := pago - nova.Total
	if envelope.Meta.Credito != sobra {
		t.Errorf("`meta.credit_cents` = %d, esperado %d — o que o hóspede pagou a mais tem de aparecer na "+
			"resposta da remarcação, senão some da tela de quem está atendendo",
			envelope.Meta.Credito, sobra)
	}

	// O crédito também tem de estar na TIMELINE da reserva nova: é onde o
	// atendente que abrir a venda amanhã vai procurar.
	detalhe := envelopeDe[struct {
		Timeline []struct {
			Tipo    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		} `json:"timeline"`
	}](t, a.chamar(t, http.MethodGet, "/reservations/"+nova.ID.String()+"/full", u.Token, nil),
		http.StatusOK, "GET /reservations/{id}/full")

	var achouCredito bool
	for _, e := range detalhe.Timeline {
		if e.Tipo == "credit_issued" {
			achouCredito = true
		}
	}
	if !achouCredito {
		t.Errorf("a timeline da reserva nova não tem o evento `credit_issued` — o dinheiro que sobrou não " +
			"deixou registro nenhum na venda que o herdou")
	}

	// E o fecho da conta, no cancelamento.
	cancelada := a.chamar(t, http.MethodPost, "/reservations/"+nova.ID.String()+"/cancel", u.Token,
		map[string]any{"reason": "desistência"})
	if cancelada.Status != http.StatusOK {
		t.Fatalf("POST /cancel: status %d — corpo: %s", cancelada.Status, cancelada.Corpo)
	}
	var conta struct {
		Data struct {
			Devolucao int64 `json:"refund_cents"`
			Retido    int64 `json:"retained_cents"`
			Credito   int64 `json:"credit_cents"`
			SinalPago int64 `json:"deposit_paid_cents"`
		} `json:"data"`
	}
	cancelada.decodificar(t, &conta)

	soma := conta.Data.Devolucao + conta.Data.Retido + conta.Data.Credito
	if soma != pago {
		t.Errorf("a casa recebeu %d do hóspede e fechou a conta em %d (devolvido %d + retido %d + crédito "+
			"%d): %d centavos evaporaram — não viraram devolução, não viraram crédito e não viraram dívida "+
			"de ninguém", pago, soma, conta.Data.Devolucao, conta.Data.Retido, conta.Data.Credito, pago-soma)
	}
	if conta.Data.Devolucao < 0 || conta.Data.Retido < 0 || conta.Data.Credito < 0 {
		t.Errorf("valor negativo no fecho da conta: devolvido %d, retido %d, crédito %d",
			conta.Data.Devolucao, conta.Data.Retido, conta.Data.Credito)
	}
}

// ─────────── MÉDIO — a década tirada do mercado numa tecla ──────────────────

// TestUmaTeclaNaoTiraADecadaDoMercado.
//
// EM LINGUAGEM DE NEGÓCIO: um bloqueio de manutenção digitado com o ano errado
// — `2040` no lugar de `2030` — tirou 29.224 noites-unidade do mercado com um
// único POST, feito pelo menor privilégio do sistema. A venda de 2045 passava a
// morrer com 409 e desfazer o estrago exigia oito DELETEs, um por unidade. Não
// há aviso, não há confirmação, e a tela não mostra bloqueio a catorze anos de
// distância: ninguém descobre até um cliente ligar.
// janelaAPartirDeHoje devolve uma janela de `noites` a partir de HOJE mais
// `dias`. Usada só onde a regra medida é relativa ao dia da casa.
func janelaAPartirDeHoje(dias, noites int) (string, string) {
	base := time.Now().UTC().AddDate(0, 0, dias)
	return base.Format("2006-01-02"), base.AddDate(0, 0, noites).Format("2006-01-02")
}

func TestUmaTeclaNaoTiraADecadaDoMercado(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	u := a.operacao(t)
	unidade := a.unidadeDoSeed(t, propriedade, "SP-01")

	decada := a.chamar(t, http.MethodPost, "/blocks", u.Token, map[string]any{
		"unit_ids": []uuid.UUID{unidade},
		"from":     "2040-01-01",
		"to":       "2050-01-01",
		"source":   "maintenance",
		"note":     "ano digitado errado",
	})
	if decada.Status == http.StatusCreated {
		t.Errorf("POST /blocks de 2040 a 2050 foi aceito: uma tecla tirou a década inteira do mercado — "+
			"corpo: %s", decada.Corpo)
		var criado struct {
			Data []struct {
				ID uuid.UUID `json:"id"`
			} `json:"data"`
		}
		decada.decodificar(t, &criado)
		for _, b := range criado.Data {
			a.executarQA(t, `DELETE FROM stay_blocks WHERE id = $1`, b.ID)
		}
	} else if decada.Status != http.StatusUnprocessableEntity || decada.codigoDeErro(t) != "VALIDATION_ERROR" {
		t.Errorf("POST /blocks de dez anos devolveu %d/%s, esperado 422 VALIDATION_ERROR — corpo: %s",
			decada.Status, decada.codigoDeErro(t), decada.Corpo)
	}

	// CONTROLE POSITIVO: a quinzena de pintura, que é o uso real do endpoint,
	// continua passando. Sem ele, um teto de zero noites passaria neste teste.
	//
	// A data é RELATIVA a hoje, e não fixa como no resto da suíte, porque o
	// segundo teto (`from` ≤ hoje + 3 anos) se mede contra o dia da CASA: uma
	// data fixa em 2032 seria recusada por estar além do horizonte, e o teste
	// passaria a medir o calendário do laptop em vez da regra.
	de, ate := janelaAPartirDeHoje(90, 15)
	quinzena := a.chamar(t, http.MethodPost, "/blocks", u.Token, map[string]any{
		"unit_ids": []uuid.UUID{unidade},
		"from":     de,
		"to":       ate,
		"source":   "maintenance",
		"note":     "pintura",
	})
	if quinzena.Status != http.StatusCreated {
		t.Fatalf("bloquear quinze dias para pintura devolveu %d — o teto não pode impedir o uso normal "+
			"do calendário: %s", quinzena.Status, quinzena.Corpo)
	}
	var criados struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	quinzena.decodificar(t, &criados)
	for _, b := range criados.Data {
		id := b.ID
		t.Cleanup(func() { a.executarQA(t, `DELETE FROM stay_blocks WHERE id = $1`, id) })
	}
}

// ─────────── ALTO — nenhum decoder recusava campo desconhecido ──────────────

// agoraNoBanco lê o relógio do POSTGRES, e não o do processo de teste.
//
// Todo recorte "o que aconteceu a partir de agora" numa tabela cujo `at` é
// preenchido pelo banco tem de nascer do MESMO relógio, senão qualquer
// diferença entre o relógio do host e o da VM do Docker come as primeiras
// linhas do recorte — e o teste falha dizendo que a escrita não deixou rastro,
// que é uma acusação grave e falsa.
func (a *ambiente) agoraNoBanco(t *testing.T) time.Time {
	t.Helper()

	var agora time.Time
	if err := a.pool.QueryRow(a.ctx, `SELECT now()`).Scan(&agora); err != nil {
		t.Fatalf("lendo o relógio do banco: %v", err)
	}
	return agora
}

// pathsComCorpoNaOpenAPI devolve path → verbo → true para todo verbo que o
// contrato declara COM `requestBody`.
//
// É daqui que a varredura tira a lista do que cobrir, e não de uma lista escrita
// à mão: rota nova com corpo entra na varredura no dia em que entra no contrato,
// sem ninguém precisar lembrar. O scanner é o mesmo de `contract_test.go` — dois
// espaços para o path, quatro para o verbo, seis para `requestBody`.
func pathsComCorpoNaOpenAPI(t *testing.T) map[string]map[string]bool {
	t.Helper()

	bruto, err := os.ReadFile(caminhoDaOpenAPI)
	if err != nil {
		t.Fatalf("lendo o contrato em %s: %v", caminhoDaOpenAPI, err)
	}

	out := map[string]map[string]bool{}
	path, verbo := "", ""
	for _, linha := range strings.Split(string(bruto), "\n") {
		if m := linhaDePath.FindStringSubmatch(linha); m != nil {
			path, verbo = m[1], ""
			continue
		}
		if m := linhaDeVerbo.FindStringSubmatch(linha); m != nil {
			verbo = strings.ToUpper(m[1])
			continue
		}
		if path == "" || verbo == "" {
			continue
		}
		if strings.HasPrefix(linha, "      requestBody:") {
			if out[path] == nil {
				out[path] = map[string]bool{}
			}
			out[path][verbo] = true
		}
	}
	if len(out) == 0 {
		t.Fatalf("nenhum requestBody lido de %s — o scanner ficou defasado do formato do arquivo", caminhoDaOpenAPI)
	}
	return out
}

// TestNenhumaRotaComCorpoAceitaCampoDesconhecido.
//
// EM LINGUAGEM DE NEGÓCIO: até esta rodada, NENHUM decoder da API chamava
// `DisallowUnknownFields`. O efeito prático, medido na API no ar:
//
//	PATCH /units/{id} {"ativa": false}  → 200, e a unidade continuava ATIVA
//	POST  /quotes     {"guests": 4}     → 200, e o orçamento ignorava o campo
//
// Quem está do outro lado vê "salvo" e vai embora. O campo com o nome errado —
// o português no lugar do inglês, o nome antigo depois de um rename, a chave
// que o painel mandou por engano — é exatamente o caso em que o cliente ACHA
// que mandou e o servidor decidiu sozinho que não. Foi assim que o rename
// `guests` → `guests_count` atravessou uma rodada inteira sem ninguém ver.
//
// A varredura não usa lista escrita à mão: ela pergunta ao CONTRATO quais rotas
// têm corpo e cobra cada uma delas pela porta da frente, com um campo que o
// contrato não declara. Rota nova com corpo entra sozinha no dia em que entra
// no contrato.
func TestNenhumaRotaComCorpoAceitaCampoDesconhecido(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	// Um usuário com a matriz INTEIRA: o que se mede aqui é o decoder, e um 403
	// no meio do caminho esconderia a rota atrás da permissão.
	linhas, err := a.pool.Query(a.ctx, `SELECT code FROM resources`)
	if err != nil {
		t.Fatalf("lendo o catálogo de recursos: %v", err)
	}
	var permissoes []auth.Permissao
	for linhas.Next() {
		var recurso string
		if err := linhas.Scan(&recurso); err != nil {
			t.Fatalf("lendo recurso: %v", err)
		}
		for _, acao := range []string{auth.AcaoVer, auth.AcaoCriar, auth.AcaoEditar, auth.AcaoExcluir} {
			permissoes = append(permissoes, auth.Permissao{Resource: recurso, Action: acao, Scope: auth.EscopoAll})
		}
	}
	linhas.Close()
	u := a.criarUsuario(t, "varredura", a.criarPerfil(t, "varredura", permissoes))

	// Alvos reais para os `{id}` da tabela. Um UUID sorteado daria 404 antes de
	// o corpo ser lido, e a rota escaparia da varredura passando verde.
	contato := a.hospedeDaJornada(t, propriedade)
	de, ate := janelaLonge(300, 2)
	reserva := envelopeDe[reservaQA](t, a.chamarComChave(t, http.MethodPost, "/reservations", u.Token,
		map[string]any{
			"unit_type_id": a.produtoDoSeed(t, propriedade, "apto-2s"),
			"contact_id":   contato,
			"check_in":     de,
			"check_out":    ate,
			"guests_count": 2,
		}, a.chaveNova(t, "varredura")), http.StatusCreated, "POST /reservations")

	alvos := map[string]uuid.UUID{
		"/users":        u.ID,
		"/roles":        u.RoleID,
		"/properties":   propriedade,
		"/unit-types":   a.produtoDoSeed(t, propriedade, "apto-2s"),
		"/units":        a.unidadeNova(t, u.Token, "varredura"),
		"/reservations": reserva.ID,
		// O contato do seed serve de alvo para `/contacts/{id}`: a varredura só
		// manda campo desconhecido, então as três rotas com corpo do módulo
		// (PUT, PATCH e /anonymize) param no 422 do decoder sem escrever nada —
		// inclusive a anonimização, que é irreversível.
		"/contacts": contato,
	}
	// O CRM entrou com 45 rotas; sem alvo aqui elas nasceriam fora da varredura,
	// que é exatamente o que esta mensagem de erro existe para impedir.
	// Funil, etapas e motivos de perda vêm do seed; lead, oportunidade e
	// atividade não são semeados, então a varredura cria os seus.
	alvos["/crm/leads"] = a.leadDeVarredura(t, propriedade, contato)
	alvos["/crm/opportunities"] = a.oportunidadeDeVarredura(t, propriedade, contato)
	alvos["/crm/activities"] = a.atividadeDeVarredura(t, propriedade, alvos["/crm/opportunities"])

	for prefixo, tabela := range map[string]string{
		"/rate-tables":      "rate_tables",
		"/rates":            "rates",
		"/holidays":         "holidays",
		"/special-periods":  "special_periods",
		"/min-nights":       "min_nights_rules",
		"/crm/pipelines":    "crm_pipelines",
		"/crm/stages":       "crm_stages",
		"/crm/lost-reasons": "crm_lost_reasons",
	} {
		var id uuid.UUID
		if err := a.pool.QueryRow(a.ctx, `SELECT id FROM `+tabela+` LIMIT 1`).Scan(&id); err != nil {
			t.Fatalf("sem linha em %s para alcançar %s/{id}: %v — o seed precisa ter uma", tabela, prefixo, err)
		}
		alvos[prefixo] = id
	}

	// As rotas públicas de sessão são varridas SEM token: é assim que elas são
	// usadas, e o decoder é o mesmo.
	semToken := map[string]bool{
		"/auth/login": true, "/auth/refresh": true,
		"/auth/password/forgot": true, "/auth/password/reset": true,
	}

	const desconhecido = "campo_que_o_contrato_nao_declara"
	comCorpo := pathsComCorpoNaOpenAPI(t)

	var varridas int
	for _, rota := range tabela(t) {
		if !comCorpo[rota.Path][rota.Metodo] {
			continue
		}

		caminho := rota.Path
		if strings.Contains(caminho, "{id}") {
			// A chave é o primeiro segmento, MAS o CRM agrupa seis coleções sob
			// `/crm` — cada uma com o seu alvo. Por isso tenta-se primeiro o
			// prefixo de dois níveis, e só então a raiz. Sem isso, todo o CRM
			// colapsaria numa chave só e a varredura acharia que falta alvo.
			partes := strings.Split(strings.TrimPrefix(caminho, "/"), "/")
			raiz := "/" + partes[0]
			chave := raiz
			if len(partes) > 1 {
				if _, existe := alvos["/"+partes[0]+"/"+partes[1]]; existe {
					chave = "/" + partes[0] + "/" + partes[1]
				}
			}
			alvo, ok := alvos[chave]
			if !ok {
				t.Errorf("%s %s: a varredura não sabe alcançar esta rota (falta um alvo para %q) — rota "+
					"nova com corpo tem de entrar aqui, senão ela nasce fora da cobertura",
					rota.Metodo, rota.Path, raiz)
				continue
			}
			caminho = strings.ReplaceAll(caminho, "{id}", alvo.String())
		}

		token := u.Token
		if semToken[rota.Path] {
			token = ""
		}
		varridas++

		corpo := any(map[string]any{desconhecido: "x"})
		r := a.chamarComCabecalhos(t, rota.Metodo, caminho, token, corpo,
			map[string]string{"Idempotency-Key": a.chaveNova(t, "varredura")})

		// Rota cujo corpo é uma LISTA (a matriz de permissões é a única hoje)
		// recusa o objeto por TIPO, antes de olhar campo nenhum. Repetir com a
		// forma certa é o que faz a varredura medir o decoder também ali, em vez
		// de aceitar um 422 que não prova nada sobre campo desconhecido.
		if r.Status == http.StatusUnprocessableEntity && strings.Contains(string(r.Corpo), "tipo inválido") {
			corpo = []any{map[string]any{desconhecido: "x"}}
			r = a.chamarComCabecalhos(t, rota.Metodo, caminho, token, corpo,
				map[string]string{"Idempotency-Key": a.chaveNova(t, "varredura")})
		}

		if r.Status == http.StatusOK || r.Status == http.StatusCreated || r.Status == http.StatusNoContent {
			t.Errorf("%s %s respondeu %d para um corpo com o campo %q: o cliente vê \"salvo\" e o servidor "+
				"jogou o campo fora — é assim que um rename atravessa uma rodada inteira sem ninguém ver",
				rota.Metodo, rota.Path, r.Status, desconhecido)
			continue
		}
		if r.Status != http.StatusUnprocessableEntity {
			t.Errorf("%s %s respondeu %d para campo desconhecido, esperado 422 — corpo: %s",
				rota.Metodo, rota.Path, r.Status, r.Corpo)
			continue
		}
		if codigo := r.codigoDeErro(t); codigo != "VALIDATION_ERROR" {
			t.Errorf("%s %s: code %q, esperado VALIDATION_ERROR — corpo: %s", rota.Metodo, rota.Path, codigo, r.Corpo)
		}
		// O NOME do campo tem de sair no `details`: sem ele a tela mostra
		// "dados inválidos" e quem integra não descobre qual chave está errada.
		if !strings.Contains(string(r.Corpo), desconhecido) {
			t.Errorf("%s %s: a recusa não nomeou o campo desconhecido — corpo: %s",
				rota.Metodo, rota.Path, r.Corpo)
		}
	}

	t.Logf("varredura: %d rotas com corpo declarado no contrato, todas recusando campo desconhecido", varridas)

	if varridas < 20 {
		t.Fatalf("a varredura cobriu só %d rotas com corpo — o scanner do contrato ficou defasado e a "+
			"cobertura encolheu sem ninguém notar", varridas)
	}
}

// ─── Alvos de CRM para a varredura de campo desconhecido ────────────────────
// Criados por SQL de propósito: a varredura mede o DECODER, e montar o alvo
// pela própria API acoplaria o teste ao DTO que ele está auditando.

func (a *ambiente) leadDeVarredura(t *testing.T, propriedade, contato uuid.UUID) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO crm_leads (property_id, contact_id, source, status, score)
		VALUES ($1, $2, 'varredura', 'novo', 0)
		RETURNING id`, propriedade, contato).Scan(&id); err != nil {
		t.Fatalf("criando lead para a varredura: %v", err)
	}
	t.Cleanup(func() { a.executarQA(t, `DELETE FROM crm_leads WHERE id = $1`, id) })
	return id
}

func (a *ambiente) oportunidadeDeVarredura(t *testing.T, propriedade, contato uuid.UUID) uuid.UUID {
	t.Helper()

	var funil, etapa uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT p.id, e.id FROM crm_pipelines p
		   JOIN crm_stages e ON e.pipeline_id = p.id
		  ORDER BY e.position LIMIT 1`).Scan(&funil, &etapa); err != nil {
		t.Fatalf("sem funil semeado para a varredura: %v", err)
	}

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO crm_opportunities (property_id, contact_id, pipeline_id, stage_id,
		                               title, amount_cents, probability, status, entered_stage_at)
		VALUES ($1, $2, $3, $4, 'Varredura', 0, 0, 'aberto', now())
		RETURNING id`, propriedade, contato, funil, etapa).Scan(&id); err != nil {
		t.Fatalf("criando oportunidade para a varredura: %v", err)
	}
	t.Cleanup(func() { a.executarQA(t, `DELETE FROM crm_opportunities WHERE id = $1`, id) })
	return id
}

func (a *ambiente) atividadeDeVarredura(t *testing.T, propriedade, oportunidade uuid.UUID) uuid.UUID {
	t.Helper()

	// `crm_activities_tem_vinculo` exige ao menos um vínculo: atividade solta
	// não existe no modelo, e é um bom desenho — tarefa sem dono some.
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO crm_activities (property_id, opportunity_id, type, subject, status, priority, auto)
		VALUES ($1, $2, 'tarefa', 'Varredura', 'pendente', 'normal', false)
		RETURNING id`, propriedade, oportunidade).Scan(&id); err != nil {
		t.Fatalf("criando atividade para a varredura: %v", err)
	}
	t.Cleanup(func() { a.executarQA(t, `DELETE FROM crm_activities WHERE id = $1`, id) })
	return id
}
