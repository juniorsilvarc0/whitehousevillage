//go:build integration

// O orçamento EMITIDO, contra Postgres real.
//
// O que estes testes provam é a única razão de a tabela `quotes` existir: um
// orçamento reaberto amanhã devolve o preço de ontem. Sem isso, persistir seria
// só um cache do que o motor calcularia agora — e um cache que reprecifica
// sozinho é pior que não ter cache nenhum, porque parece um documento.
package disponibilidade_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/disponibilidade"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// dia devolve a data ISO de hoje + n, no fuso da CASA. Relativo, e não fixo
// como o resto deste pacote, porque estes testes emitem orçamento com validade —
// e validade é medida contra `now()`.
func (a *ambiente) dia(t *testing.T, n int) string {
	t.Helper()
	var iso string
	if err := a.pool.QueryRow(a.ctx, `
		SELECT ((now() AT TIME ZONE timezone)::date + $2::int)::text
		  FROM properties WHERE id = $1`, a.propriedade, n).Scan(&iso); err != nil {
		t.Fatalf("data da casa: %v", err)
	}
	return iso
}

// pedido monta o corpo de POST /quotes já normalizado.
func pedido(t *testing.T, produto uuid.UUID, de, ate string, hospedes int, ajustar func(*disponibilidade.Pedido)) disponibilidade.Entrada {
	t.Helper()

	p := disponibilidade.Pedido{
		UnitTypeID: produto,
		CheckIn:    de,
		CheckOut:   ate,
		Hospedes:   hospedes,
	}
	if ajustar != nil {
		ajustar(&p)
	}
	e, err := p.Normalizar()
	if err != nil {
		t.Fatalf("normalizando o pedido: %v", err)
	}
	return e
}

// emitir grava um orçamento e agenda a limpeza. `quote_nights` cascateia.
func (a *ambiente) emitir(t *testing.T, e disponibilidade.Entrada) disponibilidade.OrcamentoSalvo {
	t.Helper()

	salvo, err := a.svc.Emitir(a.ctx, e)
	if err != nil {
		t.Fatalf("emitindo o orçamento: %v", err)
	}
	t.Cleanup(func() {
		if _, err := a.pool.Exec(a.ctx, `DELETE FROM quotes WHERE id = $1`, salvo.ID); err != nil {
			t.Errorf("limpando o orçamento: %v", err)
		}
	})
	return salvo
}

// ═══════════════════════════════════════════════════════════════════
// O TESTE QUE JUSTIFICA A TABELA: reajustar a tarifa não reescreve o
// orçamento já emitido (CLAUDE.md regra 7, critério de aceite do §3).
// ═══════════════════════════════════════════════════════════════════

func TestOrcamentoEmitidoNaoMudaDeValorDepoisDeReajusteDeTarifa(t *testing.T) {
	a := subir(t, auth.EscopoAll)
	produto := a.produto(t, "apto-2s")
	de, ate := a.dia(t, 300), a.dia(t, 303)

	entrada := pedido(t, produto, de, ate, 2, func(p *disponibilidade.Pedido) { p.Persistir = true })
	salvo := a.emitir(t, entrada)

	if salvo.ID == uuid.Nil {
		t.Fatal("o orçamento emitido voltou sem id")
	}
	if salvo.Total <= 0 || len(salvo.Diarias) != 3 {
		t.Fatalf("orçamento sem substância: total=%d noites=%d", salvo.Total, len(salvo.Diarias))
	}

	// O REAJUSTE. Mexe nas MESMAS linhas de `rates` que precificaram — é o pior
	// caso de propósito: se alguma leitura ainda buscasse o preço na tabela em
	// vez do snapshot, ela veria o número novo.
	if _, err := a.pool.Exec(a.ctx,
		`UPDATE rates SET amount_cents = amount_cents + 100000
		  WHERE rate_table_id = $1 AND unit_type_id = $2`, salvo.RateTableID, produto); err != nil {
		t.Fatalf("reajustando a tarifa: %v", err)
	}
	t.Cleanup(func() {
		if _, err := a.pool.Exec(a.ctx,
			`UPDATE rates SET amount_cents = amount_cents - 100000
			  WHERE rate_table_id = $1 AND unit_type_id = $2`, salvo.RateTableID, produto); err != nil {
			t.Errorf("desfazendo o reajuste: %v", err)
		}
	})

	// Meia prova: o reajuste PEGOU. Sem isto, um teste que só confere que o
	// orçamento não mudou passaria verde com um UPDATE que não afetou nada.
	simulado, err := a.svc.Orcar(a.ctx, pedido(t, produto, de, ate, 2, nil))
	if err != nil {
		t.Fatalf("simulando depois do reajuste: %v", err)
	}
	if simulado.Total == salvo.Total {
		t.Fatalf("o reajuste não teve efeito: simulação continua em %d centavos", simulado.Total)
	}
	if esperado := salvo.Subtotal + 300000; simulado.Subtotal != esperado {
		t.Fatalf("subtotal da simulação = %d, esperado %d (3 noites × R$ 1.000,00 a mais)", simulado.Subtotal, esperado)
	}

	// A outra metade: reabrir devolve o preço de ONTEM, centavo a centavo.
	relido, err := a.svc.BuscarOrcamento(a.ctx, salvo.ID)
	if err != nil {
		t.Fatalf("reabrindo o orçamento: %v", err)
	}

	for _, caso := range []struct {
		campo          string
		emitido, agora int64
	}{
		{"subtotal_cents", salvo.Subtotal, relido.Subtotal},
		{"discount_cents", salvo.Desconto, relido.Desconto},
		{"cleaning_cents", salvo.Limpeza, relido.Limpeza},
		{"event_deposit_cents", salvo.CaucaoEvento, relido.CaucaoEvento},
		{"total_cents", salvo.Total, relido.Total},
		{"deposit_cents", salvo.Sinal, relido.Sinal},
		{"balance_cents", salvo.Saldo, relido.Saldo},
		{"avg_nightly_cents", salvo.MediaPorNoite, relido.MediaPorNoite},
	} {
		if caso.emitido != caso.agora {
			t.Errorf("%s mudou depois do reajuste: emitido %d, relido %d", caso.campo, caso.emitido, caso.agora)
		}
	}
	if relido.RateTableID != salvo.RateTableID || relido.PolicyVersion != salvo.PolicyVersion {
		t.Errorf("o orçamento trocou de tabela/política: %s v%d → %s v%d",
			salvo.RateTableID, salvo.PolicyVersion, relido.RateTableID, relido.PolicyVersion)
	}

	// Noite a noite — é aqui que um recálculo silencioso apareceria mesmo se os
	// totais coincidissem por acaso.
	if len(relido.Diarias) != len(salvo.Diarias) {
		t.Fatalf("o detalhe mudou de tamanho: %d → %d noites", len(salvo.Diarias), len(relido.Diarias))
	}
	for i, n := range salvo.Diarias {
		r := relido.Diarias[i]
		if r.Data != n.Data || r.Tipo != n.Tipo || r.Preco != n.Preco {
			t.Errorf("noite %s mudou: %s/%s/%d → %s/%s/%d",
				n.Data, n.Data, n.Tipo, n.Preco, r.Data, r.Tipo, r.Preco)
		}
	}
}

// Os dois derivados que NÃO são coluna. Guardá-los seria criar a segunda
// verdade; derivá-los errado seria mostrar um saldo que não fecha com o total.
func TestOrcamentoRelidoDerivaSaldoEDiariaMediaDoSnapshot(t *testing.T) {
	a := subir(t, auth.EscopoAll)
	produto := a.produto(t, "apto-2s")

	salvo := a.emitir(t, pedido(t, produto, a.dia(t, 310), a.dia(t, 314), 2,
		func(p *disponibilidade.Pedido) { p.Persistir = true }))

	relido, err := a.svc.BuscarOrcamento(a.ctx, salvo.ID)
	if err != nil {
		t.Fatalf("reabrindo: %v", err)
	}
	if esperado := relido.Total - relido.Sinal; relido.Saldo != esperado {
		t.Errorf("balance_cents = %d, esperado total − sinal = %d", relido.Saldo, esperado)
	}
	if esperado := relido.Total / int64(relido.Noites); relido.MediaPorNoite != esperado {
		t.Errorf("avg_nightly_cents = %d, esperado total ÷ noites = %d", relido.MediaPorNoite, esperado)
	}
	if relido.Noites != 4 {
		t.Errorf("night_count = %d, esperado 4", relido.Noites)
	}
	// A validade nasce dos 7 dias padrão e o orçamento nasce DE PÉ.
	if relido.Vencido {
		t.Error("o orçamento nasceu vencido")
	}
	if relido.ReservaID != nil {
		t.Errorf("o orçamento nasceu convertido: reservation_id = %s", relido.ReservaID)
	}
}

// ═══════════════════════════════════════════════════════════════════
// ORÇAMENTO NÃO BLOQUEIA DATA (spec §4).
// ═══════════════════════════════════════════════════════════════════

func TestOrcamentoEmitidoNaoBloqueiaOCalendario(t *testing.T) {
	a := subir(t, auth.EscopoAll)
	// A Completa é o caso extremo: ela consome as OITO unidades, então um
	// orçamento que segurasse data travaria a casa inteira a cada simulação de
	// preço do Réveillon.
	produto := a.produto(t, "completa")
	de, ate := a.dia(t, 320), a.dia(t, 324)

	antes := a.blocosNoPeriodo(t, de, ate)
	salvo := a.emitir(t, pedido(t, produto, de, ate, 8,
		func(p *disponibilidade.Pedido) { p.Persistir = true }))

	if depois := a.blocosNoPeriodo(t, de, ate); depois != antes {
		t.Fatalf("a emissão criou %d bloqueio(s) em stay_blocks: orçamento não bloqueia data", depois-antes)
	}

	// Dois orçamentos para a MESMA estadia convivem — é o funil emitindo
	// proposta e contraproposta, e nenhum dos dois é dono do calendário.
	segundo := a.emitir(t, pedido(t, produto, de, ate, 8,
		func(p *disponibilidade.Pedido) { p.Persistir = true }))
	if segundo.ID == salvo.ID {
		t.Fatal("a segunda emissão devolveu o mesmo orçamento")
	}
	if depois := a.blocosNoPeriodo(t, de, ate); depois != antes {
		t.Fatalf("dois orçamentos criaram %d bloqueio(s)", depois-antes)
	}
}

func (a *ambiente) blocosNoPeriodo(t *testing.T, de, ate string) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM stay_blocks
		 WHERE property_id = $1 AND period && daterange($2::date, $3::date, '[)')`,
		a.propriedade, de, ate).Scan(&n); err != nil {
		t.Fatalf("contando bloqueios: %v", err)
	}
	return n
}

// ═══════════════════════════════════════════════════════════════════
// SIMULAR ≠ EMITIR
// ═══════════════════════════════════════════════════════════════════

func TestSimulacaoNaoGravaLinhaNenhuma(t *testing.T) {
	a := subir(t, auth.EscopoAll)
	produto := a.produto(t, "apto-2s")

	antes := a.contarOrcamentos(t)
	if _, err := a.svc.Orcar(a.ctx, pedido(t, produto, a.dia(t, 330), a.dia(t, 334), 2, nil)); err != nil {
		t.Fatalf("simulando: %v", err)
	}
	if depois := a.contarOrcamentos(t); depois != antes {
		t.Fatalf("a simulação gravou %d orçamento(s): o QuoteBuilder dispara a cada tecla", depois-antes)
	}
}

func (a *ambiente) contarOrcamentos(t *testing.T) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM quotes WHERE property_id = $1`, a.propriedade).Scan(&n); err != nil {
		t.Fatalf("contando orçamentos: %v", err)
	}
	return n
}

// Os campos que só existem no orçamento emitido são RECUSADOS na simulação, e
// não ignorados: aceitar em silêncio um vínculo que não vai ser gravado deixa o
// cliente achando que vinculou.
func TestVinculoSemPersistEhRecusado(t *testing.T) {
	a := subir(t, auth.EscopoAll)
	produto := a.produto(t, "apto-2s")

	oportunidade := uuid.New()
	validade := time.Now().Add(48 * time.Hour).Format(time.RFC3339)

	for nome, ajuste := range map[string]func(*disponibilidade.Pedido){
		"opportunity_id": func(p *disponibilidade.Pedido) { p.OportunidadeID = &oportunidade },
		"valid_until":    func(p *disponibilidade.Pedido) { p.ValidoAte = &validade },
	} {
		t.Run(nome, func(t *testing.T) {
			p := disponibilidade.Pedido{
				UnitTypeID: produto, CheckIn: a.dia(t, 340), CheckOut: a.dia(t, 344), Hospedes: 2,
			}
			ajuste(&p)
			_, err := p.Normalizar()
			if err == nil {
				t.Fatal("o pedido passou sem persist: true")
			}
			if e := apperr.From(err); e.Code != "VALIDATION_ERROR" {
				t.Fatalf("código = %q, esperado VALIDATION_ERROR", e.Code)
			}
		})
	}
}

// Validade no passado é 422 com o nome do campo — não um CHECK do banco
// estourando no COMMIT com mensagem genérica.
func TestValidadeNoPassadoEhRecusada(t *testing.T) {
	a := subir(t, auth.EscopoAll)
	produto := a.produto(t, "apto-2s")

	ontem := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	entrada := pedido(t, produto, a.dia(t, 350), a.dia(t, 354), 2, func(p *disponibilidade.Pedido) {
		p.Persistir = true
		p.ValidoAte = &ontem
	})

	_, err := a.svc.Emitir(a.ctx, entrada)
	if err == nil {
		t.Fatal("orçamento nasceu vencido")
	}
	e := apperr.From(err)
	if e.Code != "VALIDATION_ERROR" {
		t.Fatalf("código = %q, esperado VALIDATION_ERROR", e.Code)
	}
	campos, ok := e.Details.(map[string]string)
	if !ok {
		t.Fatalf("details não é o mapa de campos: %#v", e.Details)
	}
	if _, apontado := campos["valid_until"]; !apontado {
		t.Fatalf("o erro não aponta o campo: %v", campos)
	}
}

// A regra que o motor aplica na simulação continua valendo na emissão — e o
// orçamento recusado não deixa linha para trás.
func TestEmissaoRecusadaPelaAlcadaNaoGravaNada(t *testing.T) {
	a := subir(t, auth.EscopoAll)
	produto := a.produto(t, "apto-2s")

	antes := a.contarOrcamentos(t)
	_, err := a.svc.Emitir(a.ctx, pedido(t, produto, a.dia(t, 360), a.dia(t, 363), 2,
		func(p *disponibilidade.Pedido) {
			p.Persistir = true
			p.DescontoPct = 90 // muito acima da alçada do proprietário
		}))
	if err == nil {
		t.Fatal("desconto de 90% foi emitido")
	}
	if e := apperr.From(err); e.Code != "DISCOUNT_ABOVE_LIMIT" {
		t.Fatalf("código = %q, esperado DISCOUNT_ABOVE_LIMIT", e.Code)
	}
	if depois := a.contarOrcamentos(t); depois != antes {
		t.Fatalf("a recusa deixou %d orçamento(s) gravado(s)", depois-antes)
	}
}
