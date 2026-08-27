//go:build integration

package tarifario_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// ═══════════════════ Versionamento das políticas ═══════════════════

// A regra 7 do CLAUDE.md, medida contra o banco: `PUT` PUBLICA uma versão nova e
// a anterior continua exatamente como estava. Reserva emitida guarda
// `policy_version`; reescrever a linha vigente mudaria o sinal e a alçada de
// toda venda que apontou para aquele número.
func TestPutDaPoliticaComercialCriaVersaoNovaSemEditarAAnterior(t *testing.T) {
	a := subir(t)

	primeira := map[string]any{
		"deposit_pct": 50, "balance_due_days": 7, "hold_hours": 48,
		"discount_auto_pct": 5, "discount_approval_pct": 10,
		"event_deposit_cents": 200000, "valid_from": hojeMais(-30),
	}
	var v1 struct {
		Versao   int     `json:"version"`
		Sinal    float64 `json:"deposit_pct"`
		HoldHora int     `json:"hold_hours"`
	}
	a.exigir(t, http.StatusCreated, http.MethodPut, "/policies/commercial", primeira).dados(t, &v1)
	if v1.Versao != 1 || v1.Sinal != 50 {
		t.Fatalf("versão 1 saiu como %+v", v1)
	}

	segunda := map[string]any{
		"deposit_pct": 30, "balance_due_days": 10, "hold_hours": 24,
		"discount_auto_pct": 8, "discount_approval_pct": 15,
		"valid_from": hojeMais(0),
	}
	var v2 struct {
		Versao    int     `json:"version"`
		Sinal     float64 `json:"deposit_pct"`
		Caucao    int64   `json:"event_deposit_cents"`
		Extensoes int     `json:"hold_max_extensions"`
	}
	a.exigir(t, http.StatusCreated, http.MethodPut, "/policies/commercial", segunda).dados(t, &v2)
	if v2.Versao != 2 {
		t.Fatalf("segunda publicação virou versão %d, esperado 2", v2.Versao)
	}
	// Campo omitido no corpo herda da versão anterior em vez de cair no DEFAULT
	// do banco: publicar política nova não pode apagar em silêncio a caução que
	// a gestão configurou.
	if v2.Caucao != 200000 {
		t.Errorf("caução = %d, esperado herdar 200000 da versão 1", v2.Caucao)
	}

	// A versão 1 continua íntegra — é o que a reserva antiga vai reler.
	var relida struct {
		Versao int     `json:"version"`
		Sinal  float64 `json:"deposit_pct"`
		Hold   int     `json:"hold_hours"`
	}
	a.exigir(t, http.StatusOK, http.MethodGet, "/policies/commercial?version=1", nil).dados(t, &relida)
	if relida.Sinal != 50 || relida.Hold != 48 {
		t.Fatalf("a versão 1 foi reescrita: %+v", relida)
	}

	// E a vigente passou a ser a 2.
	var vigente struct {
		Versao int `json:"version"`
	}
	a.exigir(t, http.StatusOK, http.MethodGet, "/policies/commercial", nil).dados(t, &vigente)
	if vigente.Versao != 2 {
		t.Fatalf("vigente = versão %d, esperado 2", vigente.Versao)
	}

	// Duas linhas no banco, não uma editada.
	var linhas int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM commercial_policies WHERE property_id = $1`, a.propriedade).Scan(&linhas); err != nil {
		t.Fatalf("contando políticas: %v", err)
	}
	if linhas != 2 {
		t.Fatalf("gravadas %d políticas, esperado 2", linhas)
	}
}

func TestPoliticaComercialRecusaAntedatarEAlcadaInvertida(t *testing.T) {
	a := subir(t)

	base := map[string]any{
		"deposit_pct": 50, "balance_due_days": 7, "hold_hours": 48,
		"discount_auto_pct": 5, "discount_approval_pct": 10, "valid_from": hojeMais(0),
	}
	a.exigir(t, http.StatusCreated, http.MethodPut, "/policies/commercial", base)

	antedatada := map[string]any{
		"deposit_pct": 50, "balance_due_days": 7, "hold_hours": 48,
		"discount_auto_pct": 5, "discount_approval_pct": 10, "valid_from": hojeMais(-1),
	}
	r := a.exigir(t, http.StatusConflict, http.MethodPut, "/policies/commercial", antedatada)
	if got := r.codigoDoErro(); got != "POLICY_IMMUTABLE" {
		t.Fatalf("código = %q, esperado POLICY_IMMUTABLE", got)
	}

	invertida := map[string]any{
		"deposit_pct": 50, "balance_due_days": 7, "hold_hours": 48,
		"discount_auto_pct": 12, "discount_approval_pct": 6, "valid_from": hojeMais(1),
	}
	r = a.exigir(t, http.StatusUnprocessableEntity, http.MethodPut, "/policies/commercial", invertida)
	if got := r.codigoDoErro(); got != "VALIDATION_ERROR" {
		t.Fatalf("código = %q, esperado VALIDATION_ERROR", got)
	}

	// Nenhuma das duas recusadas entrou.
	var linhas int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM commercial_policies WHERE property_id = $1`, a.propriedade).Scan(&linhas); err != nil {
		t.Fatalf("contando políticas: %v", err)
	}
	if linhas != 1 {
		t.Fatalf("gravadas %d políticas, esperado 1", linhas)
	}
}

// As faixas gravadas são as que `booking.CancellationPolicy` consome: `null` nos
// limites é o `-1` do domínio, e a ordem de leitura é `sort_order`.
func TestPoliticaDeCancelamentoPublicaFaixasEAsReleOrdenadas(t *testing.T) {
	a := subir(t)

	corpo := map[string]any{
		"name": "Padrão White House", "valid_from": hojeMais(0),
		"tiers": []map[string]any{
			{"days_before_min": 30, "days_before_max": nil, "refund_pct": 100, "label": "Devolução integral do sinal", "sort_order": 1},
			{"days_before_min": 7, "days_before_max": 29, "refund_pct": 50, "label": "Retenção de 50% do sinal", "sort_order": 2},
			{"days_before_min": nil, "days_before_max": 6, "refund_pct": 0, "label": "Retenção integral do sinal", "sort_order": 3},
		},
	}

	type faixa struct {
		Min    *int    `json:"days_before_min"`
		Max    *int    `json:"days_before_max"`
		Pct    float64 `json:"refund_pct"`
		Rotulo string  `json:"label"`
		Ordem  int     `json:"sort_order"`
	}
	var publicada struct {
		Versao int     `json:"version"`
		Faixas []faixa `json:"tiers"`
	}
	a.exigir(t, http.StatusCreated, http.MethodPut, "/policies/cancellation", corpo).dados(t, &publicada)

	if publicada.Versao != 1 || len(publicada.Faixas) != 3 {
		t.Fatalf("publicada = %+v", publicada)
	}
	if publicada.Faixas[0].Ordem != 1 || publicada.Faixas[0].Max != nil {
		t.Errorf("a faixa mais generosa precisa vir primeiro e sem teto: %+v", publicada.Faixas[0])
	}
	if publicada.Faixas[2].Min != nil || publicada.Faixas[2].Pct != 0 {
		t.Errorf("a faixa mais restritiva precisa ser sem piso e reter tudo: %+v", publicada.Faixas[2])
	}

	// Publicar de novo cria versão 2 e NÃO mexe nas faixas da 1.
	corpo["valid_from"] = hojeMais(1)
	corpo["tiers"] = []map[string]any{
		{"days_before_min": nil, "days_before_max": nil, "refund_pct": 0, "label": "Sem devolução", "sort_order": 1},
	}
	a.exigir(t, http.StatusCreated, http.MethodPut, "/policies/cancellation", corpo)

	var antigas int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM cancellation_tiers t
		  JOIN cancellation_policies p ON p.id = t.policy_id
		 WHERE p.property_id = $1 AND p.version = 1`, a.propriedade).Scan(&antigas); err != nil {
		t.Fatalf("contando as faixas da versão 1: %v", err)
	}
	if antigas != 3 {
		t.Fatalf("a versão 1 ficou com %d faixas, esperado 3 — publicar reescreveu o passado", antigas)
	}
}

// Buraco entre faixas faria o motor cair no "sem faixa aplicável" e reter o
// sinal inteiro, calado. O endpoint recusa antes de gravar.
func TestPoliticaDeCancelamentoRecusaFaixasComBuraco(t *testing.T) {
	a := subir(t)

	corpo := map[string]any{
		"name": "Com buraco", "valid_from": hojeMais(0),
		"tiers": []map[string]any{
			{"days_before_min": 30, "days_before_max": nil, "refund_pct": 100, "label": "Devolução integral", "sort_order": 1},
			{"days_before_min": nil, "days_before_max": 6, "refund_pct": 0, "label": "Retenção integral", "sort_order": 2},
		},
	}

	r := a.exigir(t, http.StatusUnprocessableEntity, http.MethodPut, "/policies/cancellation", corpo)
	if got := r.codigoDoErro(); got != "VALIDATION_ERROR" {
		t.Fatalf("código = %q, esperado VALIDATION_ERROR", got)
	}

	var linhas int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM cancellation_policies WHERE property_id = $1`, a.propriedade).Scan(&linhas); err != nil {
		t.Fatalf("contando políticas: %v", err)
	}
	if linhas != 0 {
		t.Fatalf("a política com buraco foi gravada (%d linhas)", linhas)
	}
}

// ═══════════════════ Grade transacional ═══════════════════

// O bulk é tudo-ou-nada: a grade que falha no meio não deixa metade gravada.
func TestGradeInteiraEhTransacional(t *testing.T) {
	a := subir(t)
	tabela := a.criarTabela(t, "Grade "+uuid.NewString()[:8], hojeMais(-1), nil)

	// 1. A grade boa entra inteira.
	var grade []struct {
		TipoDeData string `json:"date_type"`
		Valor      int64  `json:"amount_cents"`
	}
	a.exigir(t, http.StatusOK, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela, "rates": gradeCompleta(a.produtoA, 85000),
	}).dados(t, &grade)
	if len(grade) != 6 {
		t.Fatalf("a grade saiu com %d células, esperado 6", len(grade))
	}

	antes := a.tarifasDaTabela(t, tabela)
	if len(antes) != 6 {
		t.Fatalf("gravadas %d células, esperado 6", len(antes))
	}

	// 2. Grade com produto de outra propriedade: recusada ANTES de qualquer
	//    DELETE. A tabela continua com a grade anterior, valor a valor.
	invalida := append(gradeCompleta(a.produtoB, 190000), map[string]any{
		"unit_type_id": uuid.New(), "date_type": "normal", "amount_cents": 1,
	})
	r := a.exigir(t, http.StatusUnprocessableEntity, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela, "rates": invalida,
	})
	if got := r.codigoDoErro(); got != "VALIDATION_ERROR" {
		t.Fatalf("código = %q, esperado VALIDATION_ERROR", got)
	}

	depois := a.tarifasDaTabela(t, tabela)
	if len(depois) != len(antes) {
		t.Fatalf("a grade ficou com %d células depois da recusa, esperado %d", len(depois), len(antes))
	}
	for chave, valor := range antes {
		if depois[chave] != valor {
			t.Fatalf("célula %s mudou de %d para %d numa requisição recusada", chave, valor, depois[chave])
		}
	}

	// 3. Rollback de verdade: o DELETE + INSERT roda dentro da transação do
	//    service, e um erro depois dele devolve a grade antiga inteira. Aqui a
	//    falha é provocada no próprio bloco transacional.
	falhaProposital := errors.New("falha provocada depois de reescrever a grade")
	err := a.tx.Do(a.ctx, func(ctx context.Context) error {
		// Zera o produto A pelo caminho do repositório: escopo = {A}, sem
		// nenhuma célula. É a operação mais destrutiva que o bulk permite.
		atuais, err := a.repo.CelulasDoEscopo(ctx, tabela, []uuid.UUID{a.produtoA})
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(atuais))
		for _, c := range atuais {
			ids = append(ids, c.ID)
		}
		if err := a.repo.AplicarGrade(ctx, tabela, ids, nil); err != nil {
			return err
		}
		return falhaProposital
	})
	if !errors.Is(err, falhaProposital) {
		t.Fatalf("erro = %v, esperado a falha provocada", err)
	}

	aposRollback := a.tarifasDaTabela(t, tabela)
	if len(aposRollback) != len(antes) {
		t.Fatalf("depois do rollback sobraram %d células, esperado %d — a transação não desfez o DELETE",
			len(aposRollback), len(antes))
	}
}

// Substitui, não acumula: o par que não vem no corpo é removido. É a mesma
// semântica do PUT /roles/{id}/permissions — a tela manda o estado completo.
func TestGradeSubstituiEmVezDeAcumular(t *testing.T) {
	a := subir(t)
	tabela := a.criarTabela(t, "Substituição "+uuid.NewString()[:8], hojeMais(-1), nil)

	a.exigir(t, http.StatusOK, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela, "rates": gradeCompleta(a.produtoA, 85000),
	})

	// Segunda gravação só com `normal`: os outros cinco tipos somem.
	a.exigir(t, http.StatusOK, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela,
		"rates":         []map[string]any{{"unit_type_id": a.produtoA, "date_type": "normal", "amount_cents": 99000}},
	})

	grade := a.tarifasDaTabela(t, tabela)
	if len(grade) != 1 {
		t.Fatalf("a grade ficou com %d células, esperado 1 — o bulk acumulou em vez de substituir", len(grade))
	}
	if grade[a.produtoA.String()+":normal"] != 99000 {
		t.Fatalf("o valor gravado foi %d, esperado 99000", grade[a.produtoA.String()+":normal"])
	}
}

// ═══════════════════ O passado não muda ═══════════════════

// O critério de aceite da spec §3: "alterar uma tarifa muda o próximo orçamento
// e NÃO muda nenhum orçamento ou reserva já emitidos".
//
// A prova precisa ser contra o banco porque é lá que o congelamento mora:
// `reservation_nights` guarda a tarifa por noite e `reservation_pricing` guarda
// o total. Um teste de service não veria a diferença entre "não reescreve" e
// "recalcula igual por acaso".
func TestAlterarTarifaNaoMexeEmReservaJaEmitida(t *testing.T) {
	a := subir(t)
	tabela := a.criarTabela(t, "Congelada "+uuid.NewString()[:8], hojeMais(-10), nil)

	var criadas []struct {
		ID         uuid.UUID `json:"id"`
		TipoDeData string    `json:"date_type"`
		Valor      int64     `json:"amount_cents"`
	}
	a.exigir(t, http.StatusOK, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela, "rates": gradeCompleta(a.produtoA, 85000),
	}).dados(t, &criadas)

	var tarifaNormal uuid.UUID
	for _, c := range criadas {
		if c.TipoDeData == "normal" {
			tarifaNormal = c.ID
		}
	}
	if tarifaNormal == uuid.Nil {
		t.Fatal("a célula `normal` não voltou na resposta do bulk")
	}

	// A venda de ontem: duas noites a 85000, com o snapshot gravado.
	reserva := a.reservaEmitida(t, tabela, 85000, 2)

	// A tarifa sobe hoje.
	a.exigir(t, http.StatusOK, http.MethodPatch, "/rates/"+tarifaNormal.String(),
		map[string]any{"amount_cents": 120000})

	// O próximo orçamento vê o valor novo...
	var relida struct {
		Valor int64 `json:"amount_cents"`
	}
	a.exigir(t, http.StatusOK, http.MethodGet, "/rates/"+tarifaNormal.String(), nil).dados(t, &relida)
	if relida.Valor != 120000 {
		t.Fatalf("a tarifa vigente = %d, esperado 120000", relida.Valor)
	}

	// ...e a venda de ontem continua valendo o que foi acordado.
	linhas, err := a.pool.Query(a.ctx,
		`SELECT price_cents FROM reservation_nights WHERE reservation_id = $1 ORDER BY night`, reserva)
	if err != nil {
		t.Fatalf("lendo as noites da reserva: %v", err)
	}
	defer linhas.Close()

	noites := 0
	for linhas.Next() {
		var preco int64
		if err := linhas.Scan(&preco); err != nil {
			t.Fatalf("lendo as noites: %v", err)
		}
		if preco != 85000 {
			t.Fatalf("noite gravada a %d virou %d — o tarifário reescreveu o passado", 85000, preco)
		}
		noites++
	}
	if noites != 2 {
		t.Fatalf("a reserva ficou com %d noites, esperado 2", noites)
	}

	var subtotal, total int64
	if err := a.pool.QueryRow(a.ctx,
		`SELECT subtotal_cents, total_cents FROM reservation_pricing WHERE reservation_id = $1`, reserva).
		Scan(&subtotal, &total); err != nil {
		t.Fatalf("lendo o preço congelado: %v", err)
	}
	if subtotal != 170000 || total != 188000 {
		t.Fatalf("o orçamento emitido virou subtotal=%d total=%d, esperado 170000/188000", subtotal, total)
	}
}

// Apagar a célula da grade também não pode mexer no passado: o histórico está em
// `reservation_nights`, e a FK da reserva é para a TABELA, não para a célula.
func TestExcluirTarifaNaoApagaOHistorico(t *testing.T) {
	a := subir(t)
	tabela := a.criarTabela(t, "Excluída "+uuid.NewString()[:8], hojeMais(-10), nil)

	var criadas []struct {
		ID         uuid.UUID `json:"id"`
		TipoDeData string    `json:"date_type"`
	}
	a.exigir(t, http.StatusOK, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela, "rates": gradeCompleta(a.produtoA, 85000),
	}).dados(t, &criadas)

	reserva := a.reservaEmitida(t, tabela, 85000, 2)

	for _, c := range criadas {
		if c.TipoDeData == "normal" {
			a.exigir(t, http.StatusNoContent, http.MethodDelete, "/rates/"+c.ID.String(), nil)
		}
	}

	var noites int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM reservation_nights WHERE reservation_id = $1`, reserva).Scan(&noites); err != nil {
		t.Fatalf("contando as noites: %v", err)
	}
	if noites != 2 {
		t.Fatalf("sobraram %d noites depois de apagar a célula, esperado 2", noites)
	}
}

// ═══════════════════ Calendário comercial ═══════════════════

// Períodos se sobrepõem DE PROPÓSITO: o Réveillon mora dentro da alta
// temporada, e a precedência de `date_type_rules` resolve qual tipo a noite
// recebe. Uma constraint de exclusão aqui tornaria o calendário da casa
// impossível de cadastrar.
func TestPeriodosPodemSeSobreporDeProposito(t *testing.T) {
	a := subir(t)

	alta := map[string]any{
		"name": "Alta 2026/2027 " + uuid.NewString()[:8], "kind": "alta",
		"starts_on": "2026-12-15", "ends_on": "2027-01-31",
	}
	reveillon := map[string]any{
		"name": "Réveillon 2026/2027 " + uuid.NewString()[:8], "kind": "reveillon",
		"starts_on": "2026-12-28", "ends_on": "2027-01-02",
	}

	a.exigir(t, http.StatusCreated, http.MethodPost, "/special-periods", alta)
	a.exigir(t, http.StatusCreated, http.MethodPost, "/special-periods", reveillon)

	// A janela do fim do ano traz os dois — é isso que o classificador precisa
	// enxergar para o Réveillon vencer a alta por precedência.
	var env struct {
		Data []struct {
			Nome    string `json:"name"`
			Especie string `json:"kind"`
		} `json:"data"`
	}
	r := a.exigir(t, http.StatusOK, http.MethodGet, "/special-periods?from=2026-12-30&to=2026-12-31", nil)
	if err := jsonUnmarshal(r.Corpo, &env); err != nil {
		t.Fatalf("lendo a lista: %v", err)
	}
	if len(env.Data) != 2 {
		t.Fatalf("a janela trouxe %d períodos, esperado 2 (alta + réveillon sobrepostos)", len(env.Data))
	}
}

func TestPeriodoRecusaFimAntesDoComeco(t *testing.T) {
	a := subir(t)

	r := a.exigir(t, http.StatusUnprocessableEntity, http.MethodPost, "/special-periods", map[string]any{
		"name": "Invertido " + uuid.NewString()[:8], "kind": "evento",
		"starts_on": "2027-01-10", "ends_on": "2027-01-05",
	})
	if got := r.codigoDoErro(); got != "VALIDATION_ERROR" {
		t.Fatalf("código = %q, esperado VALIDATION_ERROR", got)
	}
}

// Feriado é chave natural por data: dois "12 de outubro" na mesma casa não
// existem, e a colisão precisa sair com o código estável do contrato.
func TestFeriadoRepetidoDevolveCodeInUse(t *testing.T) {
	a := subir(t)

	corpo := map[string]any{"date": "2026-10-12", "name": "Nossa Senhora Aparecida"}
	a.exigir(t, http.StatusCreated, http.MethodPost, "/holidays", corpo)

	r := a.exigir(t, http.StatusConflict, http.MethodPost, "/holidays", corpo)
	if got := r.codigoDoErro(); got != "CODE_IN_USE" {
		t.Fatalf("código = %q, esperado CODE_IN_USE", got)
	}
}

// ═══════════════════ Tabelas de tarifas ═══════════════════

// Desativar a última tabela vigente pararia a venda: todo orçamento passaria a
// sair como 422 RATE_NOT_FOUND.
func TestDesativarUltimaTabelaVigenteDevolve409(t *testing.T) {
	a := subir(t)
	unica := a.criarTabela(t, "Única "+uuid.NewString()[:8], hojeMais(-1), nil)

	r := a.exigir(t, http.StatusConflict, http.MethodDelete, "/rate-tables/"+unica.String(), nil)
	if got := r.codigoDoErro(); got != "RESOURCE_IN_USE" {
		t.Fatalf("código = %q, esperado RESOURCE_IN_USE", got)
	}

	var ativa bool
	if err := a.pool.QueryRow(a.ctx, `SELECT active FROM rate_tables WHERE id = $1`, unica).Scan(&ativa); err != nil {
		t.Fatalf("relendo a tabela: %v", err)
	}
	if !ativa {
		t.Fatal("a tabela foi desativada mesmo com o 409")
	}

	// Com outra vigente publicada, a desativação passa.
	a.criarTabela(t, "Sucessora "+uuid.NewString()[:8], hojeMais(-1), nil)
	a.exigir(t, http.StatusNoContent, http.MethodDelete, "/rate-tables/"+unica.String(), nil)

	if err := a.pool.QueryRow(a.ctx, `SELECT active FROM rate_tables WHERE id = $1`, unica).Scan(&ativa); err != nil {
		t.Fatalf("relendo a tabela: %v", err)
	}
	if ativa {
		t.Fatal("a tabela continuou ativa depois do DELETE")
	}

	// DESATIVAÇÃO, não remoção: as tarifas seguem lá para as reservas antigas
	// que apontam para `rate_table_id`.
	var existe bool
	if err := a.pool.QueryRow(a.ctx,
		`SELECT EXISTS (SELECT 1 FROM rate_tables WHERE id = $1)`, unica).Scan(&existe); err != nil {
		t.Fatalf("checando a existência: %v", err)
	}
	if !existe {
		t.Fatal("o DELETE removeu a linha fisicamente")
	}
}

// O filtro `?on=` é o que a tela usa para perguntar "qual tabela vale neste
// dia". Vigência que não cobre a data não pode aparecer.
func TestFiltroDeVigenciaRespeitaAJanela(t *testing.T) {
	a := subir(t)

	passada := a.criarTabela(t, "Passada "+uuid.NewString()[:8], "2020-01-01", texto("2020-12-31"))
	atual := a.criarTabela(t, "Atual "+uuid.NewString()[:8], hojeMais(-5), nil)

	var env struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	r := a.exigir(t, http.StatusOK, http.MethodGet, "/rate-tables?on="+hojeMais(0), nil)
	if err := jsonUnmarshal(r.Corpo, &env); err != nil {
		t.Fatalf("lendo a listagem: %v", err)
	}

	achouAtual, achouPassada := false, false
	for _, l := range env.Data {
		if l.ID == atual {
			achouAtual = true
		}
		if l.ID == passada {
			achouPassada = true
		}
	}
	if !achouAtual {
		t.Error("a tabela vigente hoje não apareceu no filtro `on`")
	}
	if achouPassada {
		t.Error("a tabela de 2020 apareceu como vigente hoje")
	}
}

// PATCH cruza a vigência com o que está GRAVADO: mandar só `valid_to` não pode
// deixar passar uma tabela que termina antes de começar.
func TestPatchDeVigenciaCruzaComOValorGravado(t *testing.T) {
	a := subir(t)
	tabela := a.criarTabela(t, "Vigência "+uuid.NewString()[:8], hojeMais(10), nil)

	r := a.exigir(t, http.StatusUnprocessableEntity, http.MethodPatch, "/rate-tables/"+tabela.String(),
		map[string]any{"valid_to": hojeMais(5)})
	if got := r.codigoDoErro(); got != "VALIDATION_ERROR" {
		t.Fatalf("código = %q, esperado VALIDATION_ERROR", got)
	}

	// E `valid_to: null` limpa o fim da vigência — o estado que um ponteiro
	// simples não conseguiria distinguir de "não mandei".
	a.exigir(t, http.StatusOK, http.MethodPatch, "/rate-tables/"+tabela.String(),
		map[string]any{"valid_to": hojeMais(30)})
	a.exigir(t, http.StatusOK, http.MethodPatch, "/rate-tables/"+tabela.String(),
		map[string]any{"valid_to": nil})

	var ate *string
	if err := a.pool.QueryRow(a.ctx,
		`SELECT valid_to::text FROM rate_tables WHERE id = $1`, tabela).Scan(&ate); err != nil {
		t.Fatalf("relendo a vigência: %v", err)
	}
	if ate != nil {
		t.Fatalf("valid_to = %q, esperado nulo depois do `null` explícito", *ate)
	}
}

// A configuração é da propriedade de quem pede: id de outra casa é 404, nunca
// leitura silenciosa do dado do vizinho.
func TestTabelaDeOutraPropriedadeNaoEhVisivel(t *testing.T) {
	a := subir(t)
	b := subir(t)

	daOutra := b.criarTabela(t, "Da outra casa "+uuid.NewString()[:8], hojeMais(-1), nil)

	r := a.exigir(t, http.StatusNotFound, http.MethodGet, "/rate-tables/"+daOutra.String(), nil)
	if got := r.codigoDoErro(); got != "NOT_FOUND" {
		t.Fatalf("código = %q, esperado NOT_FOUND", got)
	}
}

// ═══════════════════ Mínimo de noites ═══════════════════

func TestMinimoDeNoitesEhUnicoPorTipoNaTabela(t *testing.T) {
	a := subir(t)
	tabela := a.criarTabela(t, "Mínimos "+uuid.NewString()[:8], hojeMais(-1), nil)

	corpo := map[string]any{"rate_table_id": tabela, "date_type": "reveillon", "nights": 4}
	a.exigir(t, http.StatusCreated, http.MethodPost, "/min-nights", corpo)

	r := a.exigir(t, http.StatusConflict, http.MethodPost, "/min-nights", corpo)
	if got := r.codigoDoErro(); got != "CODE_IN_USE" {
		t.Fatalf("código = %q, esperado CODE_IN_USE", got)
	}
}

// ═══════════════════ BAIXO 10: o escopo do bulk ═══════════════════

// O teste que a revisão pediu: salvar a grade de UM produto não pode encostar
// nos outros.
//
// Reproduzido antes da correção, nesta mesma árvore e contra Postgres real: a
// tabela tinha 12 células (dois produtos × seis tipos), o bulk de um produto
// mandou 6 e a tabela ficou com 6 — as seis do outro produto foram apagadas por
// um `DELETE FROM rates WHERE rate_table_id = $1`. A venda daquele produto
// passava a responder RATE_NOT_FOUND sem ninguém ter pedido para apagar nada.
func TestBulkDeUmProdutoNaoTocaNoOutro(t *testing.T) {
	a := subir(t)
	tabela := a.criarTabela(t, "Escopo "+uuid.NewString()[:8], hojeMais(-1), nil)

	a.exigir(t, http.StatusOK, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela,
		"rates":         append(gradeCompleta(a.produtoA, 100000), gradeCompleta(a.produtoB, 200000)...),
	})
	antes := a.tarifasDaTabela(t, tabela)
	if len(antes) != 12 {
		t.Fatalf("a grade inicial ficou com %d células, esperado 12", len(antes))
	}

	// A tela salva SÓ o produto A, com preço novo.
	r := a.exigir(t, http.StatusOK, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela, "rates": gradeCompleta(a.produtoA, 150000),
	})

	depois := a.tarifasDaTabela(t, tabela)
	if len(depois) != 12 {
		t.Fatalf("a tabela ficou com %d células, esperado 12 — o bulk de um produto apagou o outro", len(depois))
	}
	// As seis do produto B têm de estar intactas, VALOR A VALOR: contar linhas
	// não bastaria se elas tivessem sido apagadas e regravadas erradas.
	for i, tipo := range []string{"normal", "fds", "feriado", "alta", "reveillon", "carnaval"} {
		chave := a.produtoB.String() + ":" + tipo
		esperado := int64(200000 + i*10000)
		if depois[chave] != esperado {
			t.Fatalf("célula %s do produto NÃO enviado virou %d, esperado %d", tipo, depois[chave], esperado)
		}
	}
	// E as do produto A têm de ter mudado — senão o teste passaria com um bulk
	// que não faz nada.
	if depois[a.produtoA.String()+":normal"] != 150000 {
		t.Fatalf("o produto enviado não foi atualizado: %d", depois[a.produtoA.String()+":normal"])
	}

	// O meta conta o que a chamada fez, e `removed` é zero porque nada saiu.
	var meta struct {
		ProdutoIDs  []uuid.UUID `json:"unit_type_ids"`
		Criadas     int         `json:"created"`
		Atualizadas int         `json:"updated"`
		Inalteradas int         `json:"unchanged"`
		Removidas   int         `json:"removed"`
	}
	var env struct {
		Data []map[string]any `json:"data"`
		Meta json.RawMessage  `json:"meta"`
	}
	if err := jsonUnmarshal(r.Corpo, &env); err != nil {
		t.Fatalf("lendo o envelope: %v (corpo: %s)", err, r.Corpo)
	}
	if err := jsonUnmarshal(env.Meta, &meta); err != nil {
		t.Fatalf("lendo o meta: %v", err)
	}
	if meta.Removidas != 0 {
		t.Fatalf("meta.removed = %d numa chamada que não apagou nada", meta.Removidas)
	}
	if meta.Atualizadas != 6 || meta.Criadas != 0 || meta.Inalteradas != 0 {
		t.Fatalf("meta = created %d / updated %d / unchanged %d, esperado 0/6/0",
			meta.Criadas, meta.Atualizadas, meta.Inalteradas)
	}
	if len(meta.ProdutoIDs) != 1 || meta.ProdutoIDs[0] != a.produtoA {
		t.Fatalf("meta.unit_type_ids = %v, esperado só o produto A", meta.ProdutoIDs)
	}
	// `data` é a tabela INTEIRA, não só o escopo: é o estado que a tela redesenha.
	if len(env.Data) != 12 {
		t.Fatalf("`data` veio com %d células, esperado a tabela inteira (12)", len(env.Data))
	}
}

// Zerar um produto é ato explícito: citá-lo em `unit_type_ids` sem mandar
// célula. É o único caminho para apagar tarifa pelo bulk — e ele continua sem
// tocar em quem não foi citado.
func TestZerarProdutoExigeCitaLoNoEscopo(t *testing.T) {
	a := subir(t)
	tabela := a.criarTabela(t, "Zerar "+uuid.NewString()[:8], hojeMais(-1), nil)

	a.exigir(t, http.StatusOK, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela,
		"rates":         append(gradeCompleta(a.produtoA, 100000), gradeCompleta(a.produtoB, 200000)...),
	})

	a.exigir(t, http.StatusOK, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela,
		"unit_type_ids": []uuid.UUID{a.produtoB},
		"rates":         []map[string]any{},
	})

	grade := a.tarifasDaTabela(t, tabela)
	if len(grade) != 6 {
		t.Fatalf("a tabela ficou com %d células, esperado 6 (só o produto A)", len(grade))
	}
	for chave := range grade {
		if chave[:36] == a.produtoB.String() {
			t.Fatalf("o produto zerado ainda tem a célula %s", chave)
		}
	}
	if grade[a.produtoA.String()+":normal"] != 100000 {
		t.Fatalf("zerar o produto B alterou o produto A: %d", grade[a.produtoA.String()+":normal"])
	}
}

// Célula de produto que não está no escopo declarado é 422: mandar preço de
// quem não foi citado ampliaria em silêncio o raio de ação da remoção.
func TestCelulaForaDoEscopoDeclaradoDevolve422(t *testing.T) {
	a := subir(t)
	tabela := a.criarTabela(t, "Fora do escopo "+uuid.NewString()[:8], hojeMais(-1), nil)

	a.exigir(t, http.StatusOK, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela, "rates": gradeCompleta(a.produtoA, 100000),
	})

	r := a.exigir(t, http.StatusUnprocessableEntity, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela,
		"unit_type_ids": []uuid.UUID{a.produtoA},
		"rates":         []map[string]any{{"unit_type_id": a.produtoB, "date_type": "normal", "amount_cents": 1}},
	})
	if got := r.codigoDoErro(); got != "VALIDATION_ERROR" {
		t.Fatalf("código = %q, esperado VALIDATION_ERROR", got)
	}
	if len(a.tarifasDaTabela(t, tabela)) != 6 {
		t.Fatal("a requisição recusada mexeu na grade")
	}
}

// ═══════════════════ ALTO 3: a trilha de auditoria ═══════════════════

// O ALTO 3 medido: uma execução da suíte escrevia dezenas de linhas nas tabelas
// do tarifário e ZERO em `audit_log`. Aqui a trilha é contada no banco, na
// mesma propriedade, depois de cada escrita.
func TestEscritaDoTarifarioGravaTrilhaNoBanco(t *testing.T) {
	a := subir(t)
	tabela := a.criarTabela(t, "Trilha "+uuid.NewString()[:8], hojeMais(-1), nil)

	// A criação da tabela já tem de estar lá.
	if n := a.linhasDeAuditoria(t, "rate_tables.criado", tabela); n != 1 {
		t.Fatalf("`rate_tables.criado` deixou %d linhas em audit_log, esperado 1", n)
	}

	a.exigir(t, http.StatusOK, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela, "rates": gradeCompleta(a.produtoA, 100000),
	})
	if n := a.linhasDeAuditoria(t, "rate_tables.grade_salva", tabela); n != 1 {
		t.Fatalf("`rate_tables.grade_salva` deixou %d linhas, esperado 1", n)
	}

	// A pergunta do enunciado: quem triplicou a tarifa? O `after` da linha da
	// grade tem de trazer a célula, com o valor velho e o novo, e o ator.
	a.exigir(t, http.StatusOK, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela,
		"rates": []map[string]any{
			{"unit_type_id": a.produtoA, "date_type": "reveillon", "amount_cents": 420000},
		},
		"unit_type_ids": []uuid.UUID{a.produtoA},
	})

	antes, depois, ator := a.ultimaTrilha(t, "rate_tables.grade_salva", tabela)
	rotulo := a.codigoDoProduto(t, a.produtoA) + ".reveillon"
	if antes[rotulo] != float64(140000) {
		t.Fatalf("before[%s] = %v, esperado 140000 (o valor da grade anterior) — before completo: %v", rotulo, antes[rotulo], antes)
	}
	if depois[rotulo] != float64(420000) {
		t.Fatalf("after[%s] = %v, esperado 420000 — after completo: %v", rotulo, depois[rotulo], depois)
	}
	if ator == uuid.Nil {
		t.Fatal("a linha da trilha ficou sem ator: `quem triplicou a tarifa` continua sem resposta")
	}
	// As outras cinco células do produto foram removidas nesta chamada e o
	// `before` tem de dizer quais eram — depois do DELETE ele é a única cópia.
	if len(antes) != 6 {
		t.Fatalf("before trouxe %d células, esperado 6 (a triplicada + as cinco removidas): %v", len(antes), antes)
	}
}

// A trilha entra na TRANSAÇÃO do negócio: escrita recusada não deixa rastro de
// uma alteração que não aconteceu.
func TestTrilhaNaoSobreviveAoRollbackDoTarifario(t *testing.T) {
	a := subir(t)
	tabela := a.criarTabela(t, "Rollback "+uuid.NewString()[:8], hojeMais(-1), nil)

	antes := a.totalDeAuditoria(t)

	// Produto de outra propriedade: a grade inteira é recusada.
	a.exigir(t, http.StatusUnprocessableEntity, http.MethodPost, "/rates/bulk", map[string]any{
		"rate_table_id": tabela,
		"rates":         []map[string]any{{"unit_type_id": uuid.New(), "date_type": "normal", "amount_cents": 1}},
	})

	if depois := a.totalDeAuditoria(t); depois != antes {
		t.Fatalf("audit_log foi de %d para %d linhas numa requisição RECUSADA", antes, depois)
	}
}
