package maintenance

import (
	"errors"
	"reflect"
	"testing"
)

func regra(t *testing.T, err error) *RuleError {
	t.Helper()
	var r *RuleError
	if !errors.As(err, &r) {
		t.Fatalf("esperava *RuleError, veio %v", err)
	}
	return r
}

// A máquina inteira, linha por linha: cada par (estado, ação) que alguém vai
// perguntar na revisão.
func TestNextMesa(t *testing.T) {
	casos := []struct {
		de     Status
		acao   Action
		para   Status
		codigo string // vazio = aceita
	}{
		{Open, Start, InProgress, ""},
		{Open, Complete, Done, ""}, // lâmpada: concluir sem dois toques
		{Open, Cancel, Cancelled, ""},
		{InProgress, Complete, Done, ""},
		{InProgress, Cancel, Cancelled, ""},
		{InProgress, Start, "", codeInvalidTransition}, // segundo toque em "iniciar"
		{Done, Start, "", codeOrderClosed},
		{Done, Complete, "", codeOrderClosed},
		{Done, Cancel, "", codeOrderClosed}, // concluída não vira cancelada
		{Cancelled, Start, "", codeOrderClosed},
		{Cancelled, Complete, "", codeOrderClosed}, // encerrada não reabre
		{Cancelled, Cancel, "", codeOrderClosed},
	}

	for _, c := range casos {
		t.Run(string(c.de)+"/"+string(c.acao), func(t *testing.T) {
			got, err := Next(c.de, c.acao)
			if c.codigo == "" {
				if err != nil {
					t.Fatalf("recusou: %v", err)
				}
				if got != c.para {
					t.Fatalf("foi para %q, esperado %q", got, c.para)
				}
				return
			}
			r := regra(t, err)
			if r.Code != c.codigo {
				t.Fatalf("code %q, esperado %q", r.Code, c.codigo)
			}
			if r.Details["status"] != string(c.de) {
				t.Fatalf("details.status = %v, esperado %q", r.Details["status"], c.de)
			}
		})
	}
}

// A recusa de transição diz o que É permitido — é o que leva o segundo toque
// ao botão certo em vez de a um beco.
func TestTransicaoRecusadaDizOQueEhPermitido(t *testing.T) {
	r := regra(t, func() error { _, err := Next(InProgress, Start); return err }())
	got, ok := r.Details["allowed"].([]string)
	if !ok || !reflect.DeepEqual(got, []string{"complete", "cancel"}) {
		t.Fatalf("details.allowed = %#v, esperado [complete cancel]", r.Details["allowed"])
	}
}

// Encerrada não reabre: a partir de um estado fechado NENHUMA ação passa.
// Propriedade sobre a tabela inteira, para uma ação nova não nascer com porta
// de volta.
func TestEncerradaNaoReabrePorNenhumaAcao(t *testing.T) {
	for _, s := range ClosedStatuses() {
		if got := AllowedActions(s); len(got) != 0 {
			t.Errorf("%s oferece %v; encerrada não oferece ação nenhuma", s, got)
		}
		for _, a := range actions {
			if _, err := Next(s, a); regra(t, err).Code != codeOrderClosed {
				t.Errorf("%s + %s não respondeu %s", s, a, codeOrderClosed)
			}
		}
	}
	// E nenhuma transição da tabela LEVA de volta a um estado aberto vindo de
	// um fechado, nem sai de um fechado.
	for de, saidas := range transitions {
		if de.Closed() {
			t.Errorf("a tabela tem saída a partir de %s", de)
		}
		for _, para := range saidas {
			if !para.Valid() {
				t.Errorf("%s leva a estado inválido %q", de, para)
			}
		}
	}
}

// AllowedActions e Next são a mesma tabela: o que a tela oferece é exatamente
// o que a API aceita, em todo estado.
func TestAcoesPermitidasBatemComNext(t *testing.T) {
	for _, s := range []Status{Open, InProgress, Done, Cancelled} {
		oferecidas := map[Action]bool{}
		for _, a := range AllowedActions(s) {
			oferecidas[a] = true
		}
		for _, a := range actions {
			_, err := Next(s, a)
			if (err == nil) != oferecidas[a] {
				t.Errorf("%s/%s: Next aceita=%v, AllowedActions oferece=%v", s, a, err == nil, oferecidas[a])
			}
		}
	}
	if got := AllowedActions(Done); got == nil {
		t.Fatal("AllowedActions devolveu nil: a resposta sairia `null` em vez de `[]`")
	}
}

// Estado ou ação fora do vocabulário é defeito de programação: erro comum (500),
// nunca um 409 que mandaria o operador tentar de novo.
func TestVocabularioDesconhecidoNaoViraRegra(t *testing.T) {
	for nome, err := range map[string]error{
		"estado": func() error { _, e := Next("reaberta", Complete); return e }(),
		"ação":   func() error { _, e := Next(Open, "reopen"); return e }(),
		"edição": CheckEdit("reaberta", Edit{Other: true}),
	} {
		var r *RuleError
		if err == nil || errors.As(err, &r) {
			t.Errorf("%s desconhecido: esperava erro comum, veio %v", nome, err)
		}
	}
}

func TestCheckEditMesa(t *testing.T) {
	casos := []struct {
		s      Status
		e      Edit
		recusa bool
	}{
		{Open, Edit{Other: true}, false},
		{Open, Edit{Cost: true, Other: true}, false},
		{InProgress, Edit{Other: true}, false},
		{Done, Edit{Cost: true}, false}, // a nota do encanador chega depois
		{Done, Edit{Other: true}, true},
		{Done, Edit{Cost: true, Other: true}, true}, // PUT em concluída: o corpo inteiro
		{Done, Edit{}, false},                       // PATCH vazio não mexe em nada
		{Cancelled, Edit{Cost: true}, true},
		{Cancelled, Edit{Other: true}, true},
		{Cancelled, Edit{}, false},
	}
	for _, c := range casos {
		err := CheckEdit(c.s, c.e)
		if !c.recusa {
			if err != nil {
				t.Errorf("%s %+v recusado: %v", c.s, c.e, err)
			}
			continue
		}
		r := regra(t, err)
		if r.Code != codeOrderClosed {
			t.Errorf("%s %+v: code %q", c.s, c.e, r.Code)
		}
		if r.Details["editable"] != string(EditableIn(c.s)) {
			t.Errorf("%s %+v: details.editable = %v", c.s, c.e, r.Details["editable"])
		}
	}
}

func TestPrioridadesPorUrgencia(t *testing.T) {
	got := ByUrgency()
	want := []Priority{Urgent, High, Normal, Low}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ByUrgency = %v", got)
	}
	for _, p := range got {
		if !p.Valid() {
			t.Errorf("%q fora do vocabulário", p)
		}
	}
	if Priority("critica").Valid() {
		t.Error("prioridade inventada aceita")
	}
	// Devolver cópia: o repositório pode montar o parâmetro sem medo de
	// alterar a ordem de todo mundo.
	got[0] = Low
	if ByUrgency()[0] != Urgent {
		t.Fatal("ByUrgency devolve a fatia compartilhada")
	}
}

func TestAvariaSoEhConsertadaAoConcluir(t *testing.T) {
	if r, ok := IssueOnClose(Done); !ok || r != "consertado" {
		t.Fatalf("concluir: (%q, %v), esperado (consertado, true)", r, ok)
	}
	for _, s := range []Status{Cancelled, Open, InProgress} {
		if r, ok := IssueOnClose(s); ok || r != "" {
			t.Errorf("%s mexeu na avaria: (%q, %v)", s, r, ok)
		}
	}
}
