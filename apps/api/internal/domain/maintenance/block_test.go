package maintenance

import (
	"math/rand"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

var d = calendar.MustParse

// O dia D de todos os casos de mesa: 15/11/2026, um domingo.
var hoje = d("2026-11-15")

func per(de, ate string) Period { return Period{From: d(de), To: d(ate)} }

func vivo(de, ate string) *Block { return &Block{Period: per(de, ate), Active: true} }

func liberado(de, ate string) *Block { return &Block{Period: per(de, ate), Active: false} }

// ─────────────────────────── Fase ───────────────────────────────────

func TestPhaseOfMesa(t *testing.T) {
	casos := []struct {
		nome string
		b    Block
		quer Phase
	}{
		{"começa amanhã", *vivo("2026-11-16", "2026-11-20"), Scheduled},
		{"começa hoje: cobre a noite de hoje", *vivo("2026-11-15", "2026-11-20"), Running},
		{"começou ontem", *vivo("2026-11-14", "2026-11-20"), Running},
		{"última noite é hoje", *vivo("2026-11-10", "2026-11-16"), Running},
		{"terminou hoje de manhã (to = D)", *vivo("2026-11-10", "2026-11-15"), Ended},
		{"terminou semana passada", *vivo("2026-11-01", "2026-11-05"), Ended},
		{"liberado, mesmo no futuro", *liberado("2026-11-20", "2026-11-25"), Released},
	}
	for _, c := range casos {
		if got := PhaseOf(c.b, hoje); got != c.quer {
			t.Errorf("%s: %s, esperado %s", c.nome, got, c.quer)
		}
	}
}

// ─────────────────────────── Liberação ──────────────────────────────

// A decisão do dono, caso a caso: não começou → apaga; em curso → corta o fim
// para D (a noite de D volta à venda); terminado → fica.
func TestReleaseOnMesa(t *testing.T) {
	casos := []struct {
		nome string
		b    Block
		tipo ReleaseKind
		fica Period
	}{
		{"não começou: apaga", *vivo("2026-11-20", "2026-11-25"), Drop, Period{}},
		{"começa hoje: apaga inteiro, nenhuma noite passou", *vivo("2026-11-15", "2026-11-18"), Drop, Period{}},
		{"em curso: corta para hoje", *vivo("2026-11-10", "2026-11-20"), Truncate, per("2026-11-10", "2026-11-15")},
		{"começou ontem: fica só ontem", *vivo("2026-11-14", "2026-11-20"), Truncate, per("2026-11-14", "2026-11-15")},
		{"última noite é hoje: ela volta à venda", *vivo("2026-11-10", "2026-11-16"), Truncate, per("2026-11-10", "2026-11-15")},
		{"terminou hoje de manhã: fica", *vivo("2026-11-10", "2026-11-15"), Keep, per("2026-11-10", "2026-11-15")},
		{"terminou antes: fica", *vivo("2026-10-01", "2026-10-05"), Keep, per("2026-10-01", "2026-10-05")},
		{"já liberado: nada a fazer", *liberado("2026-11-20", "2026-11-25"), Keep, per("2026-11-20", "2026-11-25")},
	}
	for _, c := range casos {
		got := ReleaseOn(c.b, hoje)
		if got.Kind != c.tipo || got.Period != c.fica {
			t.Errorf("%s: %s %v→%v, esperado %s %v→%v",
				c.nome, got.Kind, got.Period.From, got.Period.To, c.tipo, c.fica.From, c.fica.To)
		}
	}
}

// Encerrar uma ordem no Réveillon: o corte atravessa o ano sem pular nem
// repetir noite (a aritmética é de data civil, não de instante).
func TestReleaseOnAtravessaOAno(t *testing.T) {
	got := ReleaseOn(*vivo("2026-12-28", "2027-01-03"), d("2027-01-01"))
	if got.Kind != Truncate || got.Period != per("2026-12-28", "2027-01-01") || got.Period.Nights() != 4 {
		t.Fatalf("corte no ano-novo: %+v", got)
	}
}

// Propriedade da liberação, sobre milhares de bloqueios e dias sorteados (seed
// fixa: o teste é determinístico):
//
//  1. depois de liberar, NENHUMA noite de D em diante continua bloqueada;
//  2. NENHUMA noite antes de D deixou de estar bloqueada (o passado não muda);
//  3. o que fica é período válido (lower < upper) ou nada — nunca [D, D);
//  4. só um bloqueio vivo que ainda tem noite de D em diante é tocado.
func TestReleaseOnPropriedades(t *testing.T) {
	rng := rand.New(rand.NewSource(20261009))
	base := d("2026-01-01")

	for i := 0; i < 20000; i++ {
		de := base.AddDays(rng.Intn(800))
		b := Block{Period: Period{From: de, To: de.AddDays(1 + rng.Intn(60))}, Active: rng.Intn(5) > 0}
		dia := base.AddDays(rng.Intn(900))
		r := ReleaseOn(b, dia)

		bloqueadaAntes := func(n calendar.Date) bool {
			return b.Active && !n.Before(b.From) && n.Before(b.To)
		}
		bloqueadaDepois := func(n calendar.Date) bool {
			if r.Kind == Drop || !b.Active {
				return false
			}
			return !n.Before(r.Period.From) && n.Before(r.Period.To)
		}

		for n := b.From.AddDays(-2); n.Before(b.To.AddDays(2)); n = n.AddDays(1) {
			if !n.Before(dia) && bloqueadaDepois(n) {
				t.Fatalf("caso %d (%v→%v, ativo=%v, D=%s): a noite %s continua bloqueada depois de liberar (%+v)",
					i, b.From, b.To, b.Active, dia, n, r)
			}
			if n.Before(dia) && bloqueadaAntes(n) != bloqueadaDepois(n) {
				t.Fatalf("caso %d (%v→%v, ativo=%v, D=%s): a noite passada %s mudou de estado (%+v)",
					i, b.From, b.To, b.Active, dia, n, r)
			}
		}
		if r.Kind != Drop && r.Period.Nights() <= 0 {
			t.Fatalf("caso %d: sobrou período vazio ou invertido %+v", i, r)
		}
		tocado := r.Kind != Keep
		deveriaTocar := b.Active && b.To.After(dia)
		if tocado != deveriaTocar {
			t.Fatalf("caso %d (%v→%v, ativo=%v, D=%s): tocado=%v, esperado %v", i, b.From, b.To, b.Active, dia, tocado, deveriaTocar)
		}
	}
}

// ─────────────────────────── Remarcação ─────────────────────────────

func TestReplanMesa(t *testing.T) {
	casos := []struct {
		nome   string
		atual  *Block
		pedido Period
		quer   ReplanKind
		campo  string // vazio = aceita; senão o campo do VALIDATION_ERROR
	}{
		// Sem bloqueio vivo: cria, desde que comece hoje ou depois.
		{"ordem sem bloqueio, a partir de hoje", nil, per("2026-11-15", "2026-11-18"), Create, ""},
		{"ordem sem bloqueio, no futuro", nil, per("2026-12-01", "2026-12-05"), Create, ""},
		{"ordem sem bloqueio, começando ontem", nil, per("2026-11-14", "2026-11-18"), "", "from"},
		{"bloqueio liberado: cria outro", liberado("2026-11-20", "2026-11-25"), per("2026-11-20", "2026-11-25"), Create, ""},
		{"bloqueio terminado: cria outro, colado", vivo("2026-11-10", "2026-11-15"), per("2026-11-15", "2026-11-17"), Create, ""},
		{"bloqueio terminado: não ressuscita o passado", vivo("2026-11-01", "2026-11-05"), per("2026-11-01", "2026-11-20"), "", "from"},

		// Vivo e ainda não começou: muda à vontade, sem voltar para antes de hoje.
		{"agendado: estende", vivo("2026-11-20", "2026-11-25"), per("2026-11-20", "2026-11-30"), Change, ""},
		{"agendado: encurta", vivo("2026-11-20", "2026-11-25"), per("2026-11-21", "2026-11-23"), Change, ""},
		{"agendado: antecipa para hoje", vivo("2026-11-20", "2026-11-25"), per("2026-11-15", "2026-11-25"), Change, ""},
		{"agendado: antecipa para ontem", vivo("2026-11-20", "2026-11-25"), per("2026-11-14", "2026-11-25"), "", "from"},
		{"agendado: mesmo período (segundo toque)", vivo("2026-11-20", "2026-11-25"), per("2026-11-20", "2026-11-25"), NoChange, ""},
		{"começa hoje conta como não começado", vivo("2026-11-15", "2026-11-18"), per("2026-11-16", "2026-11-18"), Change, ""},

		// Em curso: o início é história; o fim não volta para antes de hoje.
		{"em curso: estende o fim", vivo("2026-11-10", "2026-11-20"), per("2026-11-10", "2026-11-30"), Change, ""},
		{"em curso: encurta até hoje", vivo("2026-11-10", "2026-11-20"), per("2026-11-10", "2026-11-15"), Change, ""},
		{"em curso: encurta até amanhã", vivo("2026-11-10", "2026-11-20"), per("2026-11-10", "2026-11-16"), Change, ""},
		{"em curso: fim antes de hoje", vivo("2026-11-10", "2026-11-20"), per("2026-11-10", "2026-11-13"), "", "to"},
		{"em curso: mexe no início para trás", vivo("2026-11-10", "2026-11-20"), per("2026-11-08", "2026-11-20"), "", "from"},
		{"em curso: mexe no início para hoje", vivo("2026-11-10", "2026-11-20"), per("2026-11-15", "2026-11-20"), "", "from"},
		{"em curso: mesmo período", vivo("2026-11-10", "2026-11-20"), per("2026-11-10", "2026-11-20"), NoChange, ""},

		// Os tetos de calendar valem para todo caminho.
		{"to igual a from", nil, per("2026-11-20", "2026-11-20"), "", "to"},
		{"to antes de from", nil, per("2026-11-20", "2026-11-18"), "", "to"},
		{"exatamente 365 noites", nil, per("2026-11-15", "2027-11-15"), Create, ""},
		{"366 noites", nil, per("2026-11-15", "2027-11-16"), "", "to"},
		{"começa no limite do horizonte", nil, Period{From: hoje.AddDays(calendar.BlockHorizonDays), To: hoje.AddDays(calendar.BlockHorizonDays + 2)}, Create, ""},
		{"começa um dia além do horizonte", nil, Period{From: hoje.AddDays(calendar.BlockHorizonDays + 1), To: hoje.AddDays(calendar.BlockHorizonDays + 3)}, "", "from"},
		{"em curso estendido além de 365 noites no total", vivo("2026-11-10", "2026-11-20"), per("2026-11-10", "2027-11-11"), "", "to"},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, err := Replan(c.atual, c.pedido, hoje)
			if c.campo == "" {
				if err != nil {
					t.Fatalf("recusou: %v", err)
				}
				if got != c.quer {
					t.Fatalf("%s, esperado %s", got, c.quer)
				}
				return
			}
			r := regra(t, err)
			if r.Code != codeValidation {
				t.Fatalf("code %q, esperado %s", r.Code, codeValidation)
			}
			if _, ok := r.Details[c.campo]; !ok || len(r.Details) != 1 {
				t.Fatalf("details %v, esperado só o campo %q", r.Details, c.campo)
			}
		})
	}
}

// Propriedade da remarcação: quando o pedido é ACEITO,
//
//  1. Change preserva o passado do bloqueio vivo — passado(novo) = passado(atual);
//  2. Create nunca começa antes de hoje (nenhuma noite passada é bloqueada);
//  3. o resultado respeita os dois tetos de calendar;
//
// e quando um pedido que mudaria o passado de um bloqueio vivo chega, ele é
// RECUSADO (controle: a propriedade 1 não passa por vacuidade).
func TestReplanPropriedades(t *testing.T) {
	rng := rand.New(rand.NewSource(1511))
	base := d("2026-01-01")
	passado := func(p Period, dia calendar.Date) (Period, bool) {
		if !p.From.Before(dia) {
			return Period{}, false
		}
		fim := p.To
		if dia.Before(fim) {
			fim = dia
		}
		return Period{From: p.From, To: fim}, true
	}

	var aceitos, recusadosPorPassado int
	for i := 0; i < 20000; i++ {
		dia := base.AddDays(200 + rng.Intn(400))
		var atual *Block
		if rng.Intn(4) > 0 {
			de := dia.AddDays(rng.Intn(40) - 20)
			atual = &Block{Period: Period{From: de, To: de.AddDays(1 + rng.Intn(30))}, Active: rng.Intn(6) > 0}
		}
		de := dia.AddDays(rng.Intn(40) - 20)
		pedido := Period{From: de, To: de.AddDays(rng.Intn(40) - 2)}
		if atual != nil && rng.Intn(3) == 0 {
			// Um terço dos pedidos parte do início atual, que é o caso comum de
			// estender ou encurtar um bloqueio em curso.
			pedido.From = atual.From
		}

		got, err := Replan(atual, pedido, dia)
		vivoHoje := atual != nil && atual.Active && atual.To.After(dia)
		pa, temPa := Period{}, false
		if vivoHoje {
			pa, temPa = passado(atual.Period, dia)
		}
		pn, temPn := passado(pedido, dia)
		mudaPassado := vivoHoje && (temPa != temPn || pa != pn)

		if err != nil {
			if regra(t, err).Code != codeValidation {
				t.Fatalf("caso %d: code inesperado %v", i, err)
			}
			if mudaPassado {
				recusadosPorPassado++
			}
			continue
		}
		aceitos++
		if pedido.Nights() <= 0 || pedido.Nights() > calendar.MaxBlockNights {
			t.Fatalf("caso %d: aceitou %d noites", i, pedido.Nights())
		}
		if pedido.From.After(dia.AddDays(calendar.BlockHorizonDays)) {
			t.Fatalf("caso %d: aceitou início além do horizonte", i)
		}
		switch got {
		case Change, NoChange:
			if !vivoHoje {
				t.Fatalf("caso %d: %s sem bloqueio vivo", i, got)
			}
			if mudaPassado {
				t.Fatalf("caso %d (atual %v→%v, pedido %v→%v, D=%s): aceitou mudar o passado",
					i, atual.From, atual.To, pedido.From, pedido.To, dia)
			}
			if got == NoChange && pedido != atual.Period {
				t.Fatalf("caso %d: NoChange com período diferente", i)
			}
		case Create:
			if vivoHoje {
				t.Fatalf("caso %d: Create com bloqueio vivo — sobrariam duas linhas para a mesma ordem", i)
			}
			if pedido.From.Before(dia) {
				t.Fatalf("caso %d: Create começando antes de hoje (%s < %s)", i, pedido.From, dia)
			}
		default:
			t.Fatalf("caso %d: tipo desconhecido %q", i, got)
		}
	}
	if aceitos < 1000 || recusadosPorPassado < 1000 {
		t.Fatalf("amostra fraca: %d aceitos, %d recusados por mudar o passado — a propriedade passaria por vacuidade",
			aceitos, recusadosPorPassado)
	}
}

// Liberar e remarcar concordam: encurtar um bloqueio em curso até hoje é
// exatamente o que a liberação faz com ele.
func TestEncurtarAteHojeEhALiberacao(t *testing.T) {
	b := vivo("2026-11-10", "2026-11-20")
	r := ReleaseOn(*b, hoje)
	got, err := Replan(b, r.Period, hoje)
	if err != nil || got != Change {
		t.Fatalf("remarcar para o que a liberação deixa: (%s, %v)", got, err)
	}
}
