// Package booking é o motor comercial: classifica as noites, calcula o
// orçamento e aplica a política.
//
// É código PURO — sem SQL, sem HTTP, sem relógio implícito. Toda regra
// comercial da White House vive aqui e em lugar nenhum mais; é o que impede a
// regra de se fragmentar em cinco camadas.
package booking

import (
	"fmt"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/money"
)

// Product é o que se vende: apartamento, suíte, cobertura ou a casa completa.
type Product struct {
	ID          string
	Name        string
	Capacity    int
	Rates       map[calendar.DateType]money.Cents
	CleaningFee money.Cents
}

// Policy é a política comercial vigente. Toda reserva congela a versão que usou.
type Policy struct {
	Version             int
	DepositPct          float64 // 50 = sinal de 50%
	BalanceDueDays      int     // saldo até N dias antes do check-in
	HoldHours           int     // validade da pré-reserva
	DiscountAutoPct     float64 // até aqui a gestão fecha sozinha
	DiscountApprovalPct float64 // até aqui, com aprovação do proprietário
	MinNights           map[calendar.DateType]int
	EventDeposit        money.Cents
}

// DiscountAuthority diz até onde a negociação pode ir sem consultar ninguém.
type DiscountAuthority string

const (
	AuthorityManager   DiscountAuthority = "gestao"       // fecha agora
	AuthorityOwner     DiscountAuthority = "proprietario" // precisa de aprovação
	AuthorityForbidden DiscountAuthority = "negado"       // fora da política
)

// Authority classifica um desconto pedido.
func (p Policy) Authority(pct float64) DiscountAuthority {
	switch {
	case pct <= p.DiscountAutoPct:
		return AuthorityManager
	case pct <= p.DiscountApprovalPct:
		return AuthorityOwner
	default:
		return AuthorityForbidden
	}
}

// RuleError é a violação de uma regra de negócio. O Code é estável e vira o
// código de erro da API — o front reage a ele, não ao texto.
type RuleError struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *RuleError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// Request é o pedido de orçamento.
type Request struct {
	Product     Product
	CheckIn     calendar.Date
	CheckOut    calendar.Date
	Guests      int
	DiscountPct float64
	IsEvent     bool
}

// Night é uma diária precificada — o que vira snapshot em reservation_nights.
type Night struct {
	Date  calendar.Date     `json:"date"`
	Type  calendar.DateType `json:"date_type"`
	Label string            `json:"label"`
	Price money.Cents       `json:"price_cents"`
}

// Line agrupa as noites por tipo de tarifa, como aparece no orçamento.
type Line struct {
	Type      calendar.DateType `json:"date_type"`
	Label     string            `json:"label"`
	Nights    int               `json:"nights"`
	UnitPrice money.Cents       `json:"unit_price_cents"`
	Subtotal  money.Cents       `json:"subtotal_cents"`
}

// Quote é o orçamento calculado.
type Quote struct {
	Nights       []Night           `json:"nights"`
	Lines        []Line            `json:"lines"`
	NightCount   int               `json:"night_count"`
	Subtotal     money.Cents       `json:"subtotal_cents"`
	DiscountPct  float64           `json:"discount_pct"`
	Discount     money.Cents       `json:"discount_cents"`
	Cleaning     money.Cents       `json:"cleaning_cents"`
	EventDeposit money.Cents       `json:"event_deposit_cents"`
	Total        money.Cents       `json:"total_cents"`
	Deposit      money.Cents       `json:"deposit_cents"`
	Balance      money.Cents       `json:"balance_cents"`
	AvgNightly   money.Cents       `json:"avg_nightly_cents"`
	MinNights    int               `json:"min_nights"`
	Authority    DiscountAuthority `json:"discount_authority"`
	PolicyVer    int               `json:"policy_version"`
}

// Build calcula o orçamento e aplica as regras que bloqueiam a emissão.
//
// A ordem importa: validações estruturais primeiro (datas, capacidade), depois
// o cálculo, e por último as regras que dependem do resultado (estadia mínima
// do período, alçada do desconto).
func Build(req Request, cal calendar.Commercial, pol Policy) (Quote, error) {
	if !req.CheckIn.Before(req.CheckOut) {
		return Quote{}, &RuleError{
			Code:    "VALIDATION_ERROR",
			Message: "O check-out precisa ser depois do check-in.",
			Details: map[string]any{"check_in": req.CheckIn.String(), "check_out": req.CheckOut.String()},
		}
	}
	if req.Guests < 1 {
		return Quote{}, &RuleError{Code: "VALIDATION_ERROR", Message: "Informe ao menos um hóspede."}
	}
	if req.Guests > req.Product.Capacity {
		return Quote{}, &RuleError{
			Code:    "CAPACITY_EXCEEDED",
			Message: fmt.Sprintf("%s acomoda até %d hóspedes.", req.Product.Name, req.Product.Capacity),
			Details: map[string]any{"capacity": req.Product.Capacity, "requested": req.Guests},
		}
	}

	days := calendar.Range(req.CheckIn, req.CheckOut)
	q := Quote{
		Nights:      make([]Night, 0, len(days)),
		NightCount:  len(days),
		DiscountPct: req.DiscountPct,
		Cleaning:    req.Product.CleaningFee,
		MinNights:   1,
		PolicyVer:   pol.Version,
	}

	// Agrupa preservando a ordem de aparição, para o orçamento sair na ordem
	// em que o hóspede vive a estadia.
	index := map[calendar.DateType]int{}
	for _, d := range days {
		cls := cal.Classify(d)
		price, ok := req.Product.Rates[cls.Type]
		if !ok {
			return Quote{}, &RuleError{
				Code:    "RATE_NOT_FOUND",
				Message: fmt.Sprintf("Sem tarifa cadastrada para %s em %s.", cls.Type, req.Product.Name),
				Details: map[string]any{"date": d.String(), "date_type": string(cls.Type)},
			}
		}

		q.Nights = append(q.Nights, Night{Date: d, Type: cls.Type, Label: cls.Label, Price: price})
		q.Subtotal += price

		if min, ok := pol.MinNights[cls.Type]; ok && min > q.MinNights {
			q.MinNights = min // vale a noite mais restritiva da estadia
		}

		if i, seen := index[cls.Type]; seen {
			q.Lines[i].Nights++
			q.Lines[i].Subtotal += price
		} else {
			index[cls.Type] = len(q.Lines)
			q.Lines = append(q.Lines, Line{
				Type: cls.Type, Label: cls.Label, Nights: 1, UnitPrice: price, Subtotal: price,
			})
		}
	}

	// Desconto incide SÓ sobre as diárias — nunca sobre limpeza ou caução.
	q.Discount = q.Subtotal.Pct(req.DiscountPct)
	if req.IsEvent {
		q.EventDeposit = pol.EventDeposit
	}
	q.Total = q.Subtotal - q.Discount + q.Cleaning + q.EventDeposit
	q.Deposit = q.Total.Pct(pol.DepositPct)
	q.Balance = q.Total - q.Deposit
	q.AvgNightly = money.Cents(int64(q.Total) / int64(q.NightCount))
	q.Authority = pol.Authority(req.DiscountPct)

	if q.NightCount < q.MinNights {
		return q, &RuleError{
			Code:    "MIN_STAY_NOT_MET",
			Message: fmt.Sprintf("Este período exige no mínimo %d noites.", q.MinNights),
			Details: map[string]any{"required": q.MinNights, "requested": q.NightCount},
		}
	}
	if q.Authority == AuthorityForbidden {
		return q, &RuleError{
			Code:    "DISCOUNT_ABOVE_LIMIT",
			Message: fmt.Sprintf("Desconto de %.0f%% acima do limite de %.0f%% da política.", req.DiscountPct, pol.DiscountApprovalPct),
			Details: map[string]any{"requested_pct": req.DiscountPct, "max_pct": pol.DiscountApprovalPct},
		}
	}

	return q, nil
}
