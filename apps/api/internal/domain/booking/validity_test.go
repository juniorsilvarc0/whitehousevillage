package booking_test

import (
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/booking"
)

// fortaleza é o fuso da casa. Carregado do tzdata quando existe; sem ele, o
// offset fixo de -3 h é o mesmo — Fortaleza não tem horário de verão desde 2001.
func fortaleza(t *testing.T) *time.Location {
	t.Helper()
	if loc, err := time.LoadLocation("America/Fortaleza"); err == nil {
		return loc
	}
	return time.FixedZone("America/Fortaleza", -3*60*60)
}

func comValidade(dias int) booking.Policy {
	p := policy()
	p.QuoteValidityDays = dias
	return p
}

func TestValidadeDoOrcamentoMesa(t *testing.T) {
	loc := fortaleza(t)
	emissao := time.Date(2026, 10, 2, 17, 45, 0, 0, loc)
	amanha := emissao.Add(24 * time.Hour)
	ontem := emissao.Add(-time.Minute)

	casos := []struct {
		nome    string
		pedido  *time.Time
		dias    int
		quer    time.Time
		recusou bool
	}{
		{nome: "ausente vale a política: 7 dias, a mesma hora do dia", dias: 7, quer: time.Date(2026, 10, 9, 17, 45, 0, 0, loc)},
		{nome: "ausente com política de 15 dias — o motivo da coluna existir", dias: 15, quer: time.Date(2026, 10, 17, 17, 45, 0, 0, loc)},
		{nome: "ausente atravessa o ano sem perder o dia", dias: 90, quer: time.Date(2026, 12, 31, 17, 45, 0, 0, loc)},
		{nome: "pedido explícito no futuro vence a política", pedido: &amanha, dias: 7, quer: amanha},
		{nome: "pedido no passado é recusado em valid_until", pedido: &ontem, dias: 7, recusou: true},
		{nome: "pedido no instante da emissão nasce vencido", pedido: &emissao, dias: 7, recusou: true},
		{nome: "pedido explícito não depende da política estar lida", pedido: &amanha, dias: 0, quer: amanha},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, err := booking.QuoteValidUntil(c.pedido, emissao, comValidade(c.dias))
			if c.recusou {
				if code := ruleCode(t, err); code != "VALIDATION_ERROR" {
					t.Fatalf("code = %s, esperado VALIDATION_ERROR", code)
				}
				var regra *booking.RuleError
				_ = errors.As(err, &regra)
				if _, ok := regra.Details["valid_until"]; !ok {
					t.Errorf("details sem valid_until: %v — a tela não saberia qual campo marcar", regra.Details)
				}
				return
			}
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if !got.Equal(c.quer) {
				t.Errorf("valid_until = %s, esperado %s", got, c.quer)
			}
		})
	}
}

// Política montada sem ler a coluna (zero value) falha alto, e NÃO como
// RuleError: não é o usuário que errou, é quem montou a Policy. Devolver a
// própria emissão faria o orçamento nascer vencido e estourar no CHECK do banco.
func TestValidadeSemPoliticaLidaFalhaComoDefeito(t *testing.T) {
	emissao := time.Date(2026, 10, 2, 12, 0, 0, 0, fortaleza(t))
	for _, dias := range []int{0, -1} {
		got, err := booking.QuoteValidUntil(nil, emissao, comValidade(dias))
		if err == nil {
			t.Fatalf("dias=%d: aceitou e devolveu %s", dias, got)
		}
		var regra *booking.RuleError
		if errors.As(err, &regra) {
			t.Errorf("dias=%d: virou RuleError %s — o front mostraria erro de validação para um defeito de servidor", dias, regra.Code)
		}
	}
}

// Propriedade: para toda validade publicável (1..teto) e toda emissão, o
// orçamento sem `valid_until` vence DEPOIS da emissão, exatamente N dias de
// calendário adiante, na mesma hora local — e mais validade nunca vence antes.
func TestValidadeDoOrcamentoPropriedades(t *testing.T) {
	loc := fortaleza(t)
	rng := rand.New(rand.NewSource(20261002))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)

	for i := 0; i < 5000; i++ {
		emissao := base.Add(time.Duration(rng.Int63n(int64(3 * 365 * 24 * time.Hour))))
		dias := 1 + rng.Intn(booking.QuoteValidityMaxDays)

		vence, err := booking.QuoteValidUntil(nil, emissao, comValidade(dias))
		if err != nil {
			t.Fatalf("emissão %s, %d dias: %v", emissao, dias, err)
		}
		if !vence.After(emissao) {
			t.Fatalf("emissão %s, %d dias: vence em %s, que não é depois", emissao, dias, vence)
		}
		ea, em, ed := emissao.Date()
		if want := time.Date(ea, em, ed+dias, 0, 0, 0, 0, loc); !sameDay(vence, want) {
			t.Fatalf("emissão %s, %d dias: vence no dia %s, esperado %s", emissao, dias, vence.Format("2006-01-02"), want.Format("2006-01-02"))
		}
		if vence.Hour() != emissao.Hour() || vence.Minute() != emissao.Minute() {
			t.Fatalf("emissão %s, %d dias: a hora do dia mudou para %s", emissao, dias, vence.Format("15:04"))
		}

		// Monotonicidade: publicar mais dias nunca encurta o orçamento.
		if dias < booking.QuoteValidityMaxDays {
			mais, _ := booking.QuoteValidUntil(nil, emissao, comValidade(dias+1))
			if !mais.After(vence) {
				t.Fatalf("emissão %s: %d dias vence em %s e %d dias em %s", emissao, dias, vence, dias+1, mais)
			}
		}
	}
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
