// Package money representa dinheiro em centavos inteiros.
//
// Float em dinheiro é bug: 0,1 + 0,2 não dá 0,3, e um erro de arredondamento
// por noite vira divergência de centavos no fechamento do mês.
package money

import (
	"fmt"
	"math"
	"strings"
)

// Cents é um valor monetário em centavos de real.
type Cents int64

// FromReais converte reais inteiros (utilitário de seed e teste).
func FromReais(v int64) Cents { return Cents(v * 100) }

// Pct calcula uma porcentagem do valor, arredondando meio para cima.
// Aplique sobre o total, nunca noite a noite, para não acumular arredondamento.
func (c Cents) Pct(pct float64) Cents {
	return Cents(math.Round(float64(c) * pct / 100))
}

// Times multiplica por uma quantidade (número de noites, por exemplo).
func (c Cents) Times(n int) Cents { return c * Cents(n) }

// String formata no padrão brasileiro: R$ 7.050,00.
func (c Cents) String() string {
	neg := c < 0
	if neg {
		c = -c
	}
	reais := int64(c) / 100
	cent := int64(c) % 100

	s := fmt.Sprintf("%d", reais)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)

	out := fmt.Sprintf("R$ %s,%02d", strings.Join(parts, "."), cent)
	if neg {
		return "-" + out
	}
	return out
}
