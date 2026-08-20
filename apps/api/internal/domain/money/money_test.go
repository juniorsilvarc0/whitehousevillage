package money_test

import (
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/money"
)

func TestFormatacaoBrasileira(t *testing.T) {
	casos := map[money.Cents]string{
		0:       "R$ 0,00",
		350:     "R$ 3,50",
		190000:  "R$ 1.900,00",
		705000:  "R$ 7.050,00",
		3785000: "R$ 37.850,00",
		-176250: "-R$ 1.762,50",
	}
	for in, want := range casos {
		if got := in.String(); got != want {
			t.Errorf("%d centavos: esperava %q, veio %q", int64(in), want, got)
		}
	}
}

func TestPorcentagemArredondaMeioParaCima(t *testing.T) {
	if got := money.Cents(705000).Pct(50); got != 352500 {
		t.Errorf("sinal de 50%%: veio %d", int64(got))
	}
	if got := money.Cents(333).Pct(50); got != 167 { // 166,5 → 167
		t.Errorf("arredondamento: veio %d", int64(got))
	}
}
