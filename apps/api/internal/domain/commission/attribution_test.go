package commission

import (
	"errors"
	"fmt"
	"testing"
)

// Os ids dos testes são inteiros: o domínio é genérico justamente para não
// depender de uuid, e inteiro deixa a tabela legível.
const (
	eu     = 1 // o corretor da conta de quem escreve
	colega = 2 // outro corretor que existe
	nenhum = 9 // um id que não é de ninguém (o UUID inventado do WH-2026-0008)
)

func ptr(v int) *int { return &v }

func recusa(t *testing.T, err error) Reason {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("esperava *Refusal, veio %v", err)
	}
	return r.Reason
}

// Teste de mesa: cada linha é um caso que alguém vai perguntar na revisão, e as
// duas primeiras são as medidas que abriram o F2-09.
func TestResolveBrokerMesa(t *testing.T) {
	casos := []struct {
		nome   string
		w      BrokerWrite[int]
		quer   *int
		mudou  bool
		recusa Reason // vazio = aceita
	}{
		{
			nome:   "WH-2026-0009: own cria a venda no id do colega",
			w:      BrokerWrite[int]{Write: Create, Scope: ScopeOwn, ActorBroker: ptr(eu), Requested: Set(colega)},
			recusa: ReasonNotActorBroker,
		},
		{
			nome:   "WH-2026-0008 em own: id inventado é 403, não 422 — não se ensina quais ids existem",
			w:      BrokerWrite[int]{Write: Create, Scope: ScopeOwn, ActorBroker: ptr(eu), Requested: Set(nenhum)},
			recusa: ReasonNotActorBroker,
		},
		{
			nome:  "WH-2026-0008 em all: o domínio aceita, quem recusa o id inventado é a FK (422)",
			w:     BrokerWrite[int]{Write: Create, Scope: ScopeAll, Requested: Set(nenhum)},
			quer:  ptr(nenhum),
			mudou: true,
		},
		{
			nome:  "own cria sem dizer nada: a venda é dele",
			w:     BrokerWrite[int]{Write: Create, Scope: ScopeOwn, ActorBroker: ptr(eu), Requested: Absent[int]()},
			quer:  ptr(eu),
			mudou: true,
		},
		{
			nome: "own sem cadastro de corretor cria sem dizer nada: venda direta",
			w:    BrokerWrite[int]{Write: Create, Scope: ScopeOwn, Requested: Absent[int]()},
			quer: nil,
		},
		{
			nome:   "own sem cadastro de corretor não grava ninguém",
			w:      BrokerWrite[int]{Write: Create, Scope: ScopeOwn, Requested: Set(eu)},
			recusa: ReasonNotActorBroker,
		},
		{
			nome: "own cria como venda direta, de propósito",
			w:    BrokerWrite[int]{Write: Create, Scope: ScopeOwn, ActorBroker: ptr(eu), Requested: Null[int]()},
			quer: nil,
		},
		{
			nome:  "own cria em nome próprio, explícito",
			w:     BrokerWrite[int]{Write: Create, Scope: ScopeOwn, ActorBroker: ptr(eu), Requested: Set(eu)},
			quer:  ptr(eu),
			mudou: true,
		},
		{
			nome: "all cria sem dizer nada: venda direta",
			w:    BrokerWrite[int]{Write: Create, Scope: ScopeAll, ActorBroker: ptr(eu), Requested: Absent[int]()},
			quer: nil,
		},
		{
			nome:  "all atribui ao colega",
			w:     BrokerWrite[int]{Write: Create, Scope: ScopeAll, Requested: Set(colega)},
			quer:  ptr(colega),
			mudou: true,
		},
		{
			nome: "PATCH sem o campo não muda nada, nem em reserva confirmada de outro",
			w:    BrokerWrite[int]{Write: Patch, Scope: ScopeOwn, ActorBroker: ptr(eu), Current: ptr(colega), BeyondHold: true, Requested: Absent[int]()},
			quer: ptr(colega),
		},
		{
			nome: "own reenvia o corretor do colega que já estava lá: não é troca",
			w:    BrokerWrite[int]{Write: Patch, Scope: ScopeOwn, ActorBroker: ptr(eu), Current: ptr(colega), BeyondHold: true, Requested: Set(colega)},
			quer: ptr(colega),
		},
		{
			nome:   "own toma para si a venda que a gestão deu ao colega",
			w:      BrokerWrite[int]{Write: Patch, Scope: ScopeOwn, ActorBroker: ptr(eu), Current: ptr(colega), Requested: Set(eu)},
			recusa: ReasonReplacesOtherBroker,
		},
		{
			nome:   "own tira o colega da venda (null)",
			w:      BrokerWrite[int]{Write: Patch, Scope: ScopeOwn, ActorBroker: ptr(eu), Current: ptr(colega), Requested: Null[int]()},
			recusa: ReasonReplacesOtherBroker,
		},
		{
			nome:  "own se atribui venda direta ainda em hold",
			w:     BrokerWrite[int]{Write: Patch, Scope: ScopeOwn, ActorBroker: ptr(eu), Requested: Set(eu)},
			quer:  ptr(eu),
			mudou: true,
		},
		{
			nome:   "own se atribui venda direta já confirmada: é comissão que a casa não combinou",
			w:      BrokerWrite[int]{Write: Patch, Scope: ScopeOwn, ActorBroker: ptr(eu), BeyondHold: true, Requested: Set(eu)},
			recusa: ReasonReservationConfirmed,
		},
		{
			nome:   "own abre mão da própria comissão depois de confirmada: também é troca de dinheiro",
			w:      BrokerWrite[int]{Write: Patch, Scope: ScopeOwn, ActorBroker: ptr(eu), Current: ptr(eu), BeyondHold: true, Requested: Null[int]()},
			recusa: ReasonReservationConfirmed,
		},
		{
			nome:  "own abre mão da própria venda ainda em hold",
			w:     BrokerWrite[int]{Write: Patch, Scope: ScopeOwn, ActorBroker: ptr(eu), Current: ptr(eu), Requested: Null[int]()},
			quer:  nil,
			mudou: true,
		},
		{
			nome:  "all troca o corretor de venda confirmada: aceita e marca a mudança (F2-13 estorna)",
			w:     BrokerWrite[int]{Write: Patch, Scope: ScopeAll, Current: ptr(eu), BeyondHold: true, Requested: Set(colega)},
			quer:  ptr(colega),
			mudou: true,
		},
		{
			nome: "PUT de own sem o campo, na venda própria: volta ao padrão, que é ele mesmo",
			w:    BrokerWrite[int]{Write: Replace, Scope: ScopeOwn, ActorBroker: ptr(eu), Current: ptr(eu), BeyondHold: true, Requested: Absent[int]()},
			quer: ptr(eu),
		},
		{
			nome:   "PUT de own sem o campo, na venda do colega: o padrão tiraria o colega",
			w:      BrokerWrite[int]{Write: Replace, Scope: ScopeOwn, ActorBroker: ptr(eu), Current: ptr(colega), Requested: Absent[int]()},
			recusa: ReasonReplacesOtherBroker,
		},
		{
			nome:  "PUT de all sem o campo: volta a venda direta",
			w:     BrokerWrite[int]{Write: Replace, Scope: ScopeAll, Current: ptr(colega), Requested: Absent[int]()},
			quer:  nil,
			mudou: true,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			d, err := ResolveBroker(c.w)
			if c.recusa != "" {
				if got := recusa(t, err); got != c.recusa {
					t.Fatalf("recusa = %q, esperado %q", got, c.recusa)
				}
				return
			}
			if err != nil {
				t.Fatalf("recusou (%v), esperado aceitar", err)
			}
			if !same(d.Broker, c.quer) {
				t.Errorf("broker = %v, esperado %v", show(d.Broker), show(c.quer))
			}
			if d.Changed != c.mudou {
				t.Errorf("changed = %v, esperado %v", d.Changed, c.mudou)
			}
		})
	}
}

// Entrada fora do vocabulário falha fechada — e não como recusa de negócio, que
// o service traduziria para 403 escondendo o defeito de quem chamou.
func TestResolveBrokerEntradaInvalidaFalhaFechada(t *testing.T) {
	for _, w := range []BrokerWrite[int]{
		{Write: Create, Scope: "", Requested: Set(colega)},
		{Write: Create, Scope: "tudo", Requested: Set(colega)},
		{Write: 0, Scope: ScopeAll, Requested: Set(colega)},
	} {
		d, err := ResolveBroker(w)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%+v: err = %v, esperado ErrInvalidInput", w, err)
		}
		var r *Refusal
		if errors.As(err, &r) {
			t.Errorf("%+v: entrada inválida virou recusa de negócio (%q)", w, r.Reason)
		}
		if d.Broker != nil {
			t.Errorf("%+v: devolveu corretor %d junto com o erro", w, *d.Broker)
		}
	}
}

// A mensagem de recusa não carrega id nem nome de ninguém (F2-13, PII em erro).
func TestRefusalNaoCitaNinguem(t *testing.T) {
	_, err := ResolveBroker(BrokerWrite[int]{Write: Create, Scope: ScopeOwn, ActorBroker: ptr(eu), Requested: Set(colega)})
	if err == nil {
		t.Fatal("esperava recusa")
	}
	for _, id := range []int{eu, colega} {
		if containsDigit(err.Error(), id) {
			t.Errorf("mensagem %q cita o id %d", err.Error(), id)
		}
	}
}

// ── Teste de propriedade ──────────────────────────────────────────────
//
// O universo é pequeno o bastante para ser varrido INTEIRO, e varrer inteiro é
// mais forte que amostrar: 3 verbos × 2 escopos × 3 corretores do ator × 4
// valores gravados × 2 estados × 6 pedidos = 864 combinações, todas conferidas
// contra as invariantes que a regra existe para garantir. Uma regressão que só
// aparece numa combinação rara aparece aqui.

var (
	verbos    = []Write{Create, Replace, Patch}
	escopos   = []Scope{ScopeAll, ScopeOwn}
	doAtor    = []*int{nil, ptr(eu), ptr(colega)}
	gravados  = []*int{nil, ptr(eu), ptr(colega), ptr(nenhum)}
	estados   = []bool{false, true}
	pedidosDe = func() []Field[int] {
		return []Field[int]{Absent[int](), Null[int](), Set(eu), Set(colega), Set(nenhum), Set(3)}
	}
	combinacao = func(f func(BrokerWrite[int])) int {
		n := 0
		for _, v := range verbos {
			for _, s := range escopos {
				for _, a := range doAtor {
					for _, c := range gravados {
						for _, b := range estados {
							for _, p := range pedidosDe() {
								f(BrokerWrite[int]{Write: v, Scope: s, ActorBroker: a, Current: c, BeyondHold: b, Requested: p})
								n++
							}
						}
					}
				}
			}
		}
		return n
	}
)

func TestResolveBrokerPropriedades(t *testing.T) {
	n := combinacao(func(w BrokerWrite[int]) {
		d, err := ResolveBroker(w)
		atual := w.Current
		if w.Write == Create {
			atual = nil
		}

		// P0 — recusa é sempre Refusal, e só o escopo `own` recusa.
		if err != nil {
			var r *Refusal
			if !errors.As(err, &r) {
				t.Errorf("%s: erro que não é Refusal: %v", show3(w), err)
			}
			if w.Scope != ScopeOwn {
				t.Errorf("%s: escopo all recusou: %v", show3(w), err)
			}
			if d.Broker != nil || d.Changed {
				t.Errorf("%s: recusa devolveu decisão %+v", show3(w), d)
			}
			return
		}

		// P1 — Changed é exatamente "o final difere do gravado".
		if d.Changed == same(d.Broker, atual) {
			t.Errorf("%s: changed=%v mas final=%s e atual=%s", show3(w), d.Changed, show(d.Broker), show(atual))
		}

		// P2 — PATCH sem o campo nunca muda nada, em escopo nenhum.
		if w.Write == Patch && !w.Requested.Present && (d.Changed || !same(d.Broker, w.Current)) {
			t.Errorf("%s: PATCH sem broker_id mudou para %s", show3(w), show(d.Broker))
		}

		// P3 — campo presente é gravado como veio; o domínio não troca o pedido
		// por outro valor em silêncio.
		if w.Requested.Present && !same(d.Broker, w.Requested.Value) {
			t.Errorf("%s: pediu %s e gravaria %s", show3(w), show(w.Requested.Value), show(d.Broker))
		}

		if w.Scope != ScopeOwn {
			return
		}

		// P4 — own nunca ATRIBUI a terceiro: toda mudança aceita termina em
		// nil ou no corretor do próprio ator. É o WH-2026-0009, generalizado.
		if d.Changed && !ownOrNil(d.Broker, w.ActorBroker) {
			t.Errorf("%s: own atribuiu a venda a %s", show3(w), show(d.Broker))
		}
		// P5 — own nunca TIRA a venda de terceiro.
		if d.Changed && !ownOrNil(atual, w.ActorBroker) {
			t.Errorf("%s: own tirou a venda de %s", show3(w), show(atual))
		}
		// P6 — own nunca mexe no corretor de reserva que passou de hold.
		if d.Changed && w.Write != Create && w.BeyondHold {
			t.Errorf("%s: own trocou o corretor de reserva confirmada", show3(w))
		}
	})
	if n != 864 {
		t.Fatalf("varreu %d combinações, esperado 864 — o universo do teste encolheu e a propriedade ficou mais fraca", n)
	}
}

// P7 — reenviar o valor gravado nunca é recusado nem conta como troca, em
// qualquer verbo de edição, escopo e estado.
func TestResolveBrokerReenviarNaoETroca(t *testing.T) {
	for _, v := range []Write{Replace, Patch} {
		for _, s := range escopos {
			for _, a := range doAtor {
				for _, c := range gravados {
					for _, b := range estados {
						pedido := Null[int]()
						if c != nil {
							pedido = Set(*c)
						}
						w := BrokerWrite[int]{Write: v, Scope: s, ActorBroker: a, Current: c, BeyondHold: b, Requested: pedido}
						d, err := ResolveBroker(w)
						if err != nil || d.Changed || !same(d.Broker, c) {
							t.Errorf("%s: reenviar o gravado deu (%+v, %v)", show3(w), d, err)
						}
					}
				}
			}
		}
	}
}

// P8 — a regra é monotônica no escopo: tudo que `own` aceita, `all` aceita com
// a MESMA decisão quando o campo vem explícito. Escopo maior nunca restringe.
func TestResolveBrokerAllAceitaTudoQueOwnAceita(t *testing.T) {
	combinacao(func(w BrokerWrite[int]) {
		if w.Scope != ScopeOwn || !w.Requested.Present {
			return
		}
		dOwn, errOwn := ResolveBroker(w)
		if errOwn != nil {
			return
		}
		w.Scope = ScopeAll
		dAll, errAll := ResolveBroker(w)
		if errAll != nil || !same(dAll.Broker, dOwn.Broker) || dAll.Changed != dOwn.Changed {
			t.Errorf("%s: own decidiu %+v e all decidiu (%+v, %v)", show3(w), dOwn, dAll, errAll)
		}
	})
}

func show(p *int) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprint(*p)
}

func show3(w BrokerWrite[int]) string {
	pedido := "ausente"
	if w.Requested.Present {
		pedido = show(w.Requested.Value)
	}
	return fmt.Sprintf("[verbo=%d escopo=%s ator=%s atual=%s passouDoHold=%v pedido=%s]",
		w.Write, w.Scope, show(w.ActorBroker), show(w.Current), w.BeyondHold, pedido)
}

func containsDigit(s string, d int) bool {
	for _, r := range s {
		if r == rune('0'+d) {
			return true
		}
	}
	return false
}
