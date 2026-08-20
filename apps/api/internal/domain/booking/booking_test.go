package booking_test

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/booking"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/money"
)

// ───────────────────────── fixtures: a Tabela Comercial V1 real ─────────────

func cobertura() booking.Product {
	return booking.Product{
		ID: "cobertura", Name: "White House Cobertura", Capacity: 10,
		CleaningFee: money.FromReais(350),
		Rates: map[calendar.DateType]money.Cents{
			calendar.Normal:     money.FromReais(1900),
			calendar.Weekend:    money.FromReais(2400),
			calendar.Holiday:    money.FromReais(3100),
			calendar.HighSeason: money.FromReais(3600),
			calendar.NewYear:    money.FromReais(7500),
			calendar.Carnival:   money.FromReais(6000),
		},
	}
}

func policy() booking.Policy {
	return booking.Policy{
		Version: 1, DepositPct: 50, BalanceDueDays: 7, HoldHours: 48,
		DiscountAutoPct: 5, DiscountApprovalPct: 10,
		EventDeposit: money.FromReais(2000),
		MinNights: map[calendar.DateType]int{
			calendar.Normal: 1, calendar.Weekend: 2, calendar.Holiday: 3,
			calendar.HighSeason: 3, calendar.NewYear: 4, calendar.Carnival: 4,
		},
	}
}

func comercial() calendar.Commercial {
	return calendar.NewCommercial(
		map[string]string{
			"2026-09-07": "Independência",
			"2026-10-12": "N. Sra. Aparecida",
			"2026-11-02": "Finados",
			"2026-11-15": "Proclamação",
			"2026-12-25": "Natal",
			"2027-01-01": "Ano Novo",
		},
		[]calendar.Period{
			{Name: "Réveillon", Type: calendar.NewYear, From: calendar.MustParse("2026-12-27"), To: calendar.MustParse("2027-01-02")},
			{Name: "Carnaval", Type: calendar.Carnival, From: calendar.MustParse("2027-02-05"), To: calendar.MustParse("2027-02-10")},
			{Name: "Alta temporada", Type: calendar.HighSeason, From: calendar.MustParse("2026-12-15"), To: calendar.MustParse("2027-01-31")},
		},
	)
}

func ruleCode(t *testing.T, err error) string {
	t.Helper()
	var re *booking.RuleError
	if !errors.As(err, &re) {
		t.Fatalf("esperava RuleError, veio %v", err)
	}
	return re.Code
}

// ───────────────────────── classificação da diária ──────────────────────────

func TestClassificacaoRespeitaPrecedencia(t *testing.T) {
	cal := comercial()
	casos := []struct {
		data string
		want calendar.DateType
		por  string
	}{
		{"2026-11-16", calendar.Normal, "segunda comum"},
		{"2026-11-20", calendar.Weekend, "sexta é fim de semana comercial"},
		{"2026-11-21", calendar.Weekend, "sábado"},
		{"2026-11-22", calendar.Normal, "domingo já é diária normal"},
		{"2026-09-07", calendar.Holiday, "feriado vence diária normal"},
		{"2026-12-20", calendar.HighSeason, "alta temporada vence fim de semana"},
		{"2026-12-25", calendar.Holiday, "Natal vence alta temporada"},
		{"2026-12-31", calendar.NewYear, "réveillon vence alta temporada"},
		{"2027-01-01", calendar.NewYear, "réveillon vence até o feriado de Ano Novo"},
		{"2027-02-07", calendar.Carnival, "carnaval"},
	}
	for _, c := range casos {
		got := cal.Classify(calendar.MustParse(c.data)).Type
		if got != c.want {
			t.Errorf("%s (%s): esperava %s, veio %s", c.data, c.por, c.want, got)
		}
	}
}

// ───────────────────────── orçamento ────────────────────────────────────────

// Reproduz o cenário demonstrado aos proprietários: Cobertura de 20 a 23 de
// novembro — 2 noites de fim de semana + 1 normal, mais a limpeza.
func TestOrcamentoCoberturaFimDeSemana(t *testing.T) {
	q, err := booking.Build(booking.Request{
		Product:  cobertura(),
		CheckIn:  calendar.MustParse("2026-11-20"),
		CheckOut: calendar.MustParse("2026-11-23"),
		Guests:   6,
	}, comercial(), policy())
	if err != nil {
		t.Fatalf("orçamento válido falhou: %v", err)
	}

	if q.NightCount != 3 {
		t.Errorf("noites: esperava 3, veio %d", q.NightCount)
	}
	if want := money.FromReais(6700); q.Subtotal != want {
		t.Errorf("subtotal: esperava %s, veio %s", want, q.Subtotal)
	}
	if want := money.FromReais(7050); q.Total != want {
		t.Errorf("total: esperava %s, veio %s", want, q.Total)
	}
	if want := money.FromReais(3525); q.Deposit != want {
		t.Errorf("sinal: esperava %s, veio %s", want, q.Deposit)
	}
	if q.Balance != q.Total-q.Deposit {
		t.Errorf("saldo não fecha: %s + %s != %s", q.Deposit, q.Balance, q.Total)
	}
	if len(q.Lines) != 2 {
		t.Fatalf("esperava 2 linhas (fds e normal), veio %d", len(q.Lines))
	}
	if q.Lines[0].Type != calendar.Weekend || q.Lines[0].Nights != 2 {
		t.Errorf("primeira linha deveria ser 2× fim de semana, veio %s ×%d", q.Lines[0].Type, q.Lines[0].Nights)
	}
}

// O Réveillon atravessa a virada do ano — todas as noites precisam ser
// classificadas como réveillon, inclusive as de janeiro.
func TestOrcamentoReveillonAtravessaOAno(t *testing.T) {
	q, err := booking.Build(booking.Request{
		Product:  cobertura(),
		CheckIn:  calendar.MustParse("2026-12-28"),
		CheckOut: calendar.MustParse("2027-01-02"),
		Guests:   10,
	}, comercial(), policy())
	if err != nil {
		t.Fatalf("orçamento válido falhou: %v", err)
	}
	if q.NightCount != 5 {
		t.Fatalf("noites: esperava 5, veio %d", q.NightCount)
	}
	for _, n := range q.Nights {
		if n.Type != calendar.NewYear {
			t.Errorf("%s deveria ser réveillon, veio %s", n.Date, n.Type)
		}
	}
	if want := money.FromReais(37850); q.Total != want { // 5×7500 + 350
		t.Errorf("total: esperava %s, veio %s", want, q.Total)
	}
}

func TestEstadiaMinimaUsaANoiteMaisRestritiva(t *testing.T) {
	_, err := booking.Build(booking.Request{
		Product:  cobertura(),
		CheckIn:  calendar.MustParse("2026-12-30"),
		CheckOut: calendar.MustParse("2027-01-01"), // 2 noites de réveillon, mínimo 4
		Guests:   4,
	}, comercial(), policy())
	if got := ruleCode(t, err); got != "MIN_STAY_NOT_MET" {
		t.Errorf("esperava MIN_STAY_NOT_MET, veio %s", got)
	}
}

// A taxa de limpeza não entra na base do desconto — negociar 10% não pode
// baratear a limpeza.
func TestDescontoIncideApenasSobreAsDiarias(t *testing.T) {
	req := booking.Request{
		Product:     cobertura(),
		CheckIn:     calendar.MustParse("2026-11-20"),
		CheckOut:    calendar.MustParse("2026-11-23"),
		Guests:      6,
		DiscountPct: 10,
	}
	q, err := booking.Build(req, comercial(), policy())
	if err != nil {
		t.Fatalf("desconto de 10%% deveria ser permitido com aprovação: %v", err)
	}
	if want := money.FromReais(670); q.Discount != want { // 10% de 6700, não de 7050
		t.Errorf("desconto: esperava %s, veio %s", want, q.Discount)
	}
	if want := money.FromReais(6380); q.Total != want { // 6700 - 670 + 350
		t.Errorf("total: esperava %s, veio %s", want, q.Total)
	}
	if q.Cleaning != money.FromReais(350) {
		t.Errorf("limpeza não pode ser descontada, veio %s", q.Cleaning)
	}
}

func TestAlcadaDeDesconto(t *testing.T) {
	casos := []struct {
		pct  float64
		want booking.DiscountAuthority
	}{
		{0, booking.AuthorityManager},
		{5, booking.AuthorityManager},
		{5.5, booking.AuthorityOwner},
		{10, booking.AuthorityOwner},
		{10.1, booking.AuthorityForbidden},
		{15, booking.AuthorityForbidden},
	}
	for _, c := range casos {
		if got := policy().Authority(c.pct); got != c.want {
			t.Errorf("desconto de %.1f%%: esperava %s, veio %s", c.pct, c.want, got)
		}
	}
}

func TestDescontoAcimaDaPoliticaBloqueia(t *testing.T) {
	_, err := booking.Build(booking.Request{
		Product: cobertura(), CheckIn: calendar.MustParse("2026-11-20"),
		CheckOut: calendar.MustParse("2026-11-23"), Guests: 6, DiscountPct: 12,
	}, comercial(), policy())
	if got := ruleCode(t, err); got != "DISCOUNT_ABOVE_LIMIT" {
		t.Errorf("esperava DISCOUNT_ABOVE_LIMIT, veio %s", got)
	}
}

func TestCapacidadeEDatasInvalidas(t *testing.T) {
	base := booking.Request{
		Product: cobertura(), CheckIn: calendar.MustParse("2026-11-20"),
		CheckOut: calendar.MustParse("2026-11-23"), Guests: 6,
	}

	over := base
	over.Guests = 11
	_, err := booking.Build(over, comercial(), policy())
	if got := ruleCode(t, err); got != "CAPACITY_EXCEEDED" {
		t.Errorf("esperava CAPACITY_EXCEEDED, veio %s", got)
	}

	invertida := base
	invertida.CheckOut = calendar.MustParse("2026-11-20")
	_, err = booking.Build(invertida, comercial(), policy())
	if got := ruleCode(t, err); got != "VALIDATION_ERROR" {
		t.Errorf("esperava VALIDATION_ERROR, veio %s", got)
	}
}

func TestCaucaoDeEventoEntraNoTotalMasNaoNoDesconto(t *testing.T) {
	req := booking.Request{
		Product: cobertura(), CheckIn: calendar.MustParse("2026-11-20"),
		CheckOut: calendar.MustParse("2026-11-23"), Guests: 6, IsEvent: true, DiscountPct: 5,
	}
	q, err := booking.Build(req, comercial(), policy())
	if err != nil {
		t.Fatalf("falhou: %v", err)
	}
	if q.EventDeposit != money.FromReais(2000) {
		t.Errorf("caução: esperava R$ 2.000,00, veio %s", q.EventDeposit)
	}
	if want := money.FromReais(335); q.Discount != want { // 5% de 6700
		t.Errorf("desconto não pode incidir sobre a caução: esperava %s, veio %s", want, q.Discount)
	}
}

// ───────────────────────── cancelamento ─────────────────────────────────────

func TestPoliticaDeCancelamento(t *testing.T) {
	pol := booking.DefaultCancellation()
	sinal := money.FromReais(3525)

	casos := []struct {
		dias   int
		refund money.Cents
	}{
		{60, money.FromReais(3525)},
		{30, money.FromReais(3525)},
		{29, money.Cents(176250)},
		{10, money.Cents(176250)},
		{7, money.Cents(176250)},
		{6, 0},
		{0, 0},
	}
	for _, c := range casos {
		out := pol.Simulate(sinal, c.dias)
		if out.Refund != c.refund {
			t.Errorf("%d dias antes: esperava devolução de %s, veio %s", c.dias, c.refund, out.Refund)
		}
		if out.Refund+out.Retained != sinal {
			t.Errorf("%d dias: devolução + retenção != sinal", c.dias)
		}
	}
}

// ───────────────────────── invariantes ──────────────────────────────────────

// Estadias aleatórias não podem violar as invariantes do orçamento — é o teste
// que pega o caso que ninguém pensou em tabelar.
func TestInvariantesDoOrcamento(t *testing.T) {
	cal, pol, prod := comercial(), policy(), cobertura()
	rng := rand.New(rand.NewSource(42))
	base := calendar.MustParse("2026-08-01")

	for i := 0; i < 500; i++ {
		in := base.AddDays(rng.Intn(400))
		out := in.AddDays(1 + rng.Intn(10))
		desconto := float64(rng.Intn(11))

		q, err := booking.Build(booking.Request{
			Product: prod, CheckIn: in, CheckOut: out,
			Guests: 1 + rng.Intn(prod.Capacity), DiscountPct: desconto,
		}, cal, pol)
		var re *booking.RuleError
		if errors.As(err, &re) && re.Code == "MIN_STAY_NOT_MET" {
			continue // estadia curta em período restrito é recusa legítima
		}
		if err != nil {
			t.Fatalf("%s→%s: erro inesperado %v", in, out, err)
		}

		if q.Total < 0 || q.Deposit < 0 || q.Balance < 0 {
			t.Fatalf("%s→%s: valores negativos %+v", in, out, q)
		}
		if q.Deposit+q.Balance != q.Total {
			t.Fatalf("%s→%s: sinal + saldo != total", in, out)
		}
		if q.Total != q.Subtotal-q.Discount+q.Cleaning+q.EventDeposit {
			t.Fatalf("%s→%s: composição do total não fecha", in, out)
		}
		if q.Discount > q.Subtotal {
			t.Fatalf("%s→%s: desconto maior que as diárias", in, out)
		}
		if q.NightCount != len(q.Nights) || q.NightCount != in.Nights(out) {
			t.Fatalf("%s→%s: contagem de noites divergente", in, out)
		}
		var soma money.Cents
		for _, l := range q.Lines {
			soma += l.Subtotal
		}
		if soma != q.Subtotal {
			t.Fatalf("%s→%s: linhas não somam o subtotal", in, out)
		}
	}
}
