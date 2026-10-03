//go:build integration

package tarifario_test

import (
	"net/http"
	"sort"
	"strings"
	"testing"
)

// colunasConhecidasDaPolitica é o catálogo de `commercial_policies` que este
// teste sabe pôr fora do DEFAULT. Coluna nova no banco reprova o teste até
// alguém decidir, aqui, se ela é herdada (entra em politicaForaDoPadrao) ou é
// identidade da linha (entra também na lista de sobrescrita do repositório).
var colunasConhecidasDaPolitica = []string{
	"balance_due_days", "created_at", "deposit_pct", "discount_approval_pct",
	"discount_auto_pct", "event_deposit_cents", "hold_extension_hours",
	"hold_hours", "hold_max_extensions", "id", "property_id",
	"quote_validity_days", "valid_from", "version",
}

// identidadeDaLinha muda de uma versão para a outra por definição.
const identidadeDaLinha = `- 'id' - 'version' - 'created_at'`

// politicaForaDoPadrao tem TODAS as colunas editáveis longe do DEFAULT do
// banco (event_deposit_cents 0, hold_extension_hours 24, hold_max_extensions 1,
// quote_validity_days 7). Valor igual ao DEFAULT não prova herança: a coluna
// que volta ao DEFAULT em silêncio passaria pelo teste.
func politicaForaDoPadrao() map[string]any {
	return map[string]any{
		"deposit_pct": 35.5, "balance_due_days": 12, "hold_hours": 36,
		"discount_auto_pct": 4.25, "discount_approval_pct": 9.75,
		"event_deposit_cents": 123400, "hold_extension_hours": 6,
		"hold_max_extensions": 3, "quote_validity_days": 15,
		"valid_from": hojeMais(-10),
	}
}

// F2-05 / risco R6: publicar uma versão nova não pode devolver ao DEFAULT,
// em silêncio, coluna que o corpo do PUT omitiu.
//
// O teste publica a v1 com tudo fora do padrão; publica a v2 mandando só os
// obrigatórios e mudando UM deles (hold_hours); publica a v3 mudando só
// quote_validity_days. Em cada passo, a linha nova menos a identidade e menos a
// coluna mudada tem de ser IGUAL à anterior — comparada pelo banco
// (`to_jsonb`), o que pega também coluna que a API não serve.
func TestPoliticaNaoPerdeColunaAoRepublicar(t *testing.T) {
	a := subir(t)

	// 0. O catálogo é o que o teste conhece.
	var colunas []string
	linhas, err := a.pool.Query(a.ctx, `
		SELECT column_name FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = 'commercial_policies'
		 ORDER BY column_name`)
	if err != nil {
		t.Fatalf("lendo o catálogo: %v", err)
	}
	for linhas.Next() {
		var c string
		if err := linhas.Scan(&c); err != nil {
			t.Fatalf("lendo coluna: %v", err)
		}
		colunas = append(colunas, c)
	}
	linhas.Close()
	if err := linhas.Err(); err != nil {
		t.Fatalf("lendo o catálogo: %v", err)
	}
	conhecidas := append([]string(nil), colunasConhecidasDaPolitica...)
	sort.Strings(conhecidas)
	if strings.Join(colunas, ",") != strings.Join(conhecidas, ",") {
		t.Fatalf("commercial_policies mudou de colunas:\n  banco: %v\n  teste: %v\n"+
			"decida se a coluna nova é herdada (ponha-a fora do DEFAULT em politicaForaDoPadrao) "+
			"ou é identidade da linha (sobrescreva-a em PublicarPoliticaComercial)", colunas, conhecidas)
	}

	// 1. v1 com tudo fora do padrão — e a resposta já devolve o que foi pedido.
	v1 := politicaForaDoPadrao()
	var publicada struct {
		Versao   int `json:"version"`
		Validade int `json:"quote_validity_days"`
		Extensao int `json:"hold_extension_hours"`
		Max      int `json:"hold_max_extensions"`
		Caucao   int `json:"event_deposit_cents"`
		Hold     int `json:"hold_hours"`
	}
	a.exigir(t, http.StatusCreated, http.MethodPut, "/policies/commercial", v1).dados(t, &publicada)
	if publicada.Versao != 1 || publicada.Validade != 15 || publicada.Extensao != 6 ||
		publicada.Max != 3 || publicada.Caucao != 123400 {
		t.Fatalf("v1 não gravou o que foi pedido: %+v", publicada)
	}

	// 2. v2: só os obrigatórios, mudando hold_hours. Os opcionais OMITIDOS.
	v2 := politicaForaDoPadrao()
	for _, opcional := range []string{"event_deposit_cents", "hold_extension_hours",
		"hold_max_extensions", "quote_validity_days"} {
		delete(v2, opcional)
	}
	v2["hold_hours"] = 72
	a.exigir(t, http.StatusCreated, http.MethodPut, "/policies/commercial", v2).dados(t, &publicada)
	if publicada.Versao != 2 || publicada.Hold != 72 {
		t.Fatalf("v2 saiu como %+v", publicada)
	}
	a.exigirLinhasIguaisMenos(t, 1, 2, "hold_hours")

	// 3. v3: muda só quote_validity_days (e reenvia `null` num opcional, que
	// também é "não mudei" numa coluna NOT NULL).
	v3 := politicaForaDoPadrao()
	delete(v3, "event_deposit_cents")
	delete(v3, "hold_extension_hours")
	v3["hold_max_extensions"] = nil
	v3["hold_hours"] = 72
	v3["quote_validity_days"] = 20
	a.exigir(t, http.StatusCreated, http.MethodPut, "/policies/commercial", v3).dados(t, &publicada)
	if publicada.Versao != 3 || publicada.Validade != 20 {
		t.Fatalf("v3 saiu como %+v", publicada)
	}
	a.exigirLinhasIguaisMenos(t, 2, 3, "quote_validity_days")

	// 4. E a vigente, lida pela API, carrega a validade publicada.
	var vigente struct {
		Versao   int `json:"version"`
		Validade int `json:"quote_validity_days"`
		Caucao   int `json:"event_deposit_cents"`
	}
	a.exigir(t, http.StatusOK, http.MethodGet, "/policies/commercial", nil).dados(t, &vigente)
	if vigente.Versao != 3 || vigente.Validade != 20 || vigente.Caucao != 123400 {
		t.Fatalf("vigente = %+v, esperado v3 com validade 20 e caução 123400 herdada da v1", vigente)
	}
}

// exigirLinhasIguaisMenos compara duas versões no banco, coluna a coluna, menos
// a identidade da linha e a coluna que o PUT mudou. A diferença é listada pelo
// nome, para o vermelho dizer QUAL coluna voltou ao DEFAULT.
func (a *ambiente) exigirLinhasIguaisMenos(t *testing.T, de, para int, mudada string) {
	t.Helper()

	q := `
		WITH v AS (
		    SELECT version, to_jsonb(cp) ` + identidadeDaLinha + ` - $3::text AS linha
		      FROM commercial_policies cp
		     WHERE cp.property_id = $1 AND cp.version IN ($2, $4)
		)
		SELECT coalesce(string_agg(format('%s: v%s=%s v%s=%s', k.chave, $2::int, ant.linha -> k.chave,
		                                  $4::int, nova.linha -> k.chave), '; ' ORDER BY k.chave), '')
		  FROM v ant, v nova,
		       LATERAL jsonb_object_keys(ant.linha) AS k(chave)
		 WHERE ant.version = $2 AND nova.version = $4
		   AND ant.linha -> k.chave IS DISTINCT FROM nova.linha -> k.chave`

	var diferencas string
	if err := a.pool.QueryRow(a.ctx, q, a.propriedade, de, mudada, para).Scan(&diferencas); err != nil {
		t.Fatalf("comparando v%d com v%d: %v", de, para, err)
	}
	if diferencas != "" {
		t.Fatalf("a v%d perdeu coluna que o PUT não mudou (só %q mudou): %s", para, mudada, diferencas)
	}

	var mudou bool
	if err := a.pool.QueryRow(a.ctx, `
		SELECT (SELECT to_jsonb(cp) -> $3::text FROM commercial_policies cp WHERE cp.property_id = $1 AND cp.version = $2)
		       IS DISTINCT FROM
		       (SELECT to_jsonb(cp) -> $3::text FROM commercial_policies cp WHERE cp.property_id = $1 AND cp.version = $4)`,
		a.propriedade, de, mudada, para).Scan(&mudou); err != nil {
		t.Fatalf("conferindo a coluna mudada: %v", err)
	}
	if !mudou {
		t.Fatalf("%q não mudou de v%d para v%d — o PUT foi ignorado", mudada, de, para)
	}
}
