package booking

import "github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/money"

// Tier é uma faixa da política de cancelamento. Faixas são dado versionado:
// mudar a regra amanhã não pode alterar reserva já feita, por isso a reserva
// congela a versão da política que usou.
type Tier struct {
	MinDaysBefore int     // inclusivo; -1 = sem piso
	MaxDaysBefore int     // inclusivo; -1 = sem teto
	RefundPct     float64 // do sinal
	Label         string
}

// CancellationPolicy é o conjunto de faixas, da mais generosa à mais restritiva.
type CancellationPolicy struct {
	Version int
	Name    string
	Tiers   []Tier
}

// DefaultCancellation reproduz a regra em vigor hoje na White House:
// 30 dias ou mais devolve tudo, de 7 a 29 retém metade, abaixo de 7 retém tudo.
func DefaultCancellation() CancellationPolicy {
	return CancellationPolicy{
		Version: 1,
		Name:    "Padrão White House",
		Tiers: []Tier{
			{MinDaysBefore: 30, MaxDaysBefore: -1, RefundPct: 100, Label: "Devolução integral do sinal"},
			{MinDaysBefore: 7, MaxDaysBefore: 29, RefundPct: 50, Label: "Retenção de 50% do sinal"},
			{MinDaysBefore: -1, MaxDaysBefore: 6, RefundPct: 0, Label: "Retenção integral do sinal"},
		},
	}
}

// Outcome é o resultado de um cancelamento: quanto volta, quanto fica e por quê.
type Outcome struct {
	Tier     Tier        `json:"-"`
	Label    string      `json:"label"`
	Refund   money.Cents `json:"refund_cents"`
	Retained money.Cents `json:"retained_cents"`
}

// Simulate calcula a devolução sem executar nada — é o que alimenta o
// `?dry_run=1` da API, para a gestão ver o valor antes de confirmar.
func (p CancellationPolicy) Simulate(depositPaid money.Cents, daysBefore int) Outcome {
	for _, t := range p.Tiers {
		if t.MinDaysBefore >= 0 && daysBefore < t.MinDaysBefore {
			continue
		}
		if t.MaxDaysBefore >= 0 && daysBefore > t.MaxDaysBefore {
			continue
		}
		refund := depositPaid.Pct(t.RefundPct)
		return Outcome{Tier: t, Label: t.Label, Refund: refund, Retained: depositPaid - refund}
	}
	// Sem faixa aplicável, o conservador é não devolver e deixar a decisão para
	// o humano — nunca devolver por omissão.
	return Outcome{Label: "Sem faixa aplicável — decidir manualmente", Refund: 0, Retained: depositPaid}
}
