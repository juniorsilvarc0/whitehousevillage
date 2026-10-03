package booking_test

import (
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/booking"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/money"
)

// grandVilla é a tabela que o dono do negócio passou em 03/10/2026: diária de
// R$ 3.500 no período regular e R$ 6.500 na alta (a imagem dizia 5.500 e foi
// corrigida por ele), com pacotes de 2 e 4 diárias nos dois períodos.
// Réveillon e Carnaval são "sob consulta": sem tarifa.
func grandVilla() booking.Product {
	regular := []calendar.DateType{calendar.Normal, calendar.Weekend}
	alta := []calendar.DateType{calendar.HighSeason}
	return booking.Product{
		ID: "grand-villa", Name: "White House Grand Villa", Capacity: 8,
		Rates: map[calendar.DateType]money.Cents{
			calendar.Normal:     money.FromReais(3500),
			calendar.Weekend:    money.FromReais(3500),
			calendar.HighSeason: money.FromReais(6500),
		},
		Packages: []booking.Package{
			{Nights: 2, Types: regular, Total: money.FromReais(6500)},
			{Nights: 4, Types: regular, Total: money.FromReais(12000)},
			{Nights: 2, Types: alta, Total: money.FromReais(10500)},
			{Nights: 4, Types: alta, Total: money.FromReais(20000)},
		},
	}
}

func orcarGV(t *testing.T, de, ate string) booking.Quote {
	t.Helper()
	q, err := booking.Build(booking.Request{
		Product: grandVilla(), CheckIn: calendar.MustParse(de), CheckOut: calendar.MustParse(ate), Guests: 4,
	}, comercial(), policy())
	if err != nil && ruleCode(t, err) != "MIN_STAY_NOT_MET" {
		t.Fatalf("%s→%s: %v", de, ate, err)
	}
	return q
}

func TestPacoteDaGrandVillaMesa(t *testing.T) {
	casos := []struct {
		nome    string
		de, ate string
		total   int64 // em reais
	}{
		// 05/10/2026 é segunda: noites de segunda a quinta são "normal".
		{"regular, 2 diárias = pacote", "2026-10-05", "2026-10-07", 6500},
		{"regular, 3 diárias = pacote de 2 + 1 avulsa", "2026-10-05", "2026-10-08", 6500 + 3500},
		{"regular, 4 diárias = pacote de 4", "2026-10-05", "2026-10-09", 12000},
		{"regular com fim de semana no meio continua regular", "2026-10-08", "2026-10-12", 12000},
		{"regular, 5 diárias = pacote de 4 + 1 avulsa", "2026-10-05", "2026-10-10", 12000 + 3500},
		// Janeiro de 2027, depois do Réveillon, é alta temporada.
		{"alta, 1 diária avulsa", "2027-01-11", "2027-01-12", 6500},
		{"alta, 2 diárias = pacote", "2027-01-11", "2027-01-13", 10500},
		{"alta, 4 diárias = pacote", "2027-01-11", "2027-01-15", 20000},
		{"alta, 6 diárias = pacote de 4 + pacote de 2", "2027-01-11", "2027-01-17", 20000 + 10500},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			q := orcarGV(t, c.de, c.ate)
			if q.Total != money.FromReais(c.total) {
				t.Fatalf("total = %v, esperado R$ %d", q.Total, c.total)
			}
			var soma money.Cents
			for _, n := range q.Nights {
				soma += n.Price
			}
			if soma != q.Subtotal {
				t.Fatalf("as noites somam %v e o subtotal é %v: o snapshot por noite não fecharia", soma, q.Subtotal)
			}
		})
	}
}

// Uma janela que mistura alta e regular não vira pacote de nenhum dos dois: o
// pacote é "N diárias NO período", não "N diárias quaisquer".
func TestPacoteNaoAtravessaTiposQueNaoCobre(t *testing.T) {
	cal := calendar.NewCommercial(nil, []calendar.Period{
		{Name: "Alta", Type: calendar.HighSeason, From: calendar.MustParse("2026-10-06"), To: calendar.MustParse("2026-10-30")},
	})
	p := grandVilla()
	p.MinNights = map[calendar.DateType]int{calendar.HighSeason: 1} // isola o pacote da estadia mínima
	q, err := booking.Build(booking.Request{
		Product: p, CheckIn: calendar.MustParse("2026-10-05"), CheckOut: calendar.MustParse("2026-10-07"), Guests: 2,
	}, cal, policy())
	if err != nil {
		t.Fatal(err)
	}
	if want := money.FromReais(3500 + 6500); q.Total != want {
		t.Fatalf("segunda regular + terça alta = %v, esperado %v (duas avulsas)", q.Total, want)
	}
}

// Pacote existe para baratear. Se a tabela avulsa ficar mais barata que o
// pacote (alguém baixou a diária e esqueceu o pacote), vale a avulsa.
func TestPacoteMaisCaroQueAvulsoNaoSeAplica(t *testing.T) {
	p := grandVilla()
	p.Packages = []booking.Package{{Nights: 2, Types: []calendar.DateType{calendar.Normal}, Total: money.FromReais(9000)}}
	q, err := booking.Build(booking.Request{
		Product: p, CheckIn: calendar.MustParse("2026-10-05"), CheckOut: calendar.MustParse("2026-10-07"), Guests: 2,
	}, comercial(), policy())
	if err != nil {
		t.Fatal(err)
	}
	if q.Total != money.FromReais(7000) {
		t.Fatalf("total = %v, esperado R$ 7.000 (duas avulsas, mais baratas que o pacote)", q.Total)
	}
}

// O total do pacote é repartido em centavos inteiros e fecha exatamente.
func TestPacoteComTotalIndivisivelFechaNoCentavo(t *testing.T) {
	p := grandVilla()
	p.Packages = []booking.Package{{Nights: 3, Types: []calendar.DateType{calendar.Normal}, Total: 1000001}}
	q, err := booking.Build(booking.Request{
		Product: p, CheckIn: calendar.MustParse("2026-10-05"), CheckOut: calendar.MustParse("2026-10-08"), Guests: 2,
	}, comercial(), policy())
	if err != nil {
		t.Fatal(err)
	}
	if q.Subtotal != 1000001 || q.Nights[0].Price != 333334 || q.Nights[2].Price != 333333 {
		t.Fatalf("repartição errada: subtotal %v, noites %v %v %v", q.Subtotal, q.Nights[0].Price, q.Nights[1].Price, q.Nights[2].Price)
	}
	if len(q.Lines) != 1 || q.Lines[0].Label != "Pacote 3 diárias" || q.Lines[0].Nights != 3 {
		t.Fatalf("linha do pacote: %+v", q.Lines)
	}
}

// Réveillon na Grand Villa é sob consulta: sem tarifa, nada de orçamento.
func TestGrandVillaNoReveillonEhSobConsulta(t *testing.T) {
	_, err := booking.Build(booking.Request{
		Product: grandVilla(), CheckIn: calendar.MustParse("2026-12-28"), CheckOut: calendar.MustParse("2027-01-01"), Guests: 2,
	}, comercial(), policy())
	if ruleCode(t, err) != "RATE_NOT_FOUND" {
		t.Fatalf("esperava RATE_NOT_FOUND, veio %v", err)
	}
}

// A estadia mínima do produto substitui a geral do mesmo tipo de data: a
// política da casa aceita 1 noite comum, a Pool Suíte exige 2.
func TestEstadiaMinimaDoProdutoSubstituiAGeral(t *testing.T) {
	suite := booking.Product{
		ID: "suite-coral", Name: "Pool Suíte Coral", Capacity: 2,
		Rates:     map[calendar.DateType]money.Cents{calendar.Normal: money.FromReais(320)},
		MinNights: map[calendar.DateType]int{calendar.Normal: 2},
	}
	pedido := booking.Request{Product: suite, CheckIn: calendar.MustParse("2026-10-05"), CheckOut: calendar.MustParse("2026-10-06"), Guests: 2}
	if _, err := booking.Build(pedido, comercial(), policy()); ruleCode(t, err) != "MIN_STAY_NOT_MET" {
		t.Fatalf("1 noite na suíte deveria exigir 2, veio %v", err)
	}
	// Controle: sem a regra do produto, a geral (1 noite) aceita.
	suite.MinNights = nil
	pedido.Product = suite
	if _, err := booking.Build(pedido, comercial(), policy()); err != nil {
		t.Fatalf("sem regra do produto, 1 noite comum passa: %v", err)
	}
}
