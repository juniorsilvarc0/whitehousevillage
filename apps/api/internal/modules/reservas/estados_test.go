package reservas

import (
	"reflect"
	"testing"
)

// A máquina de estados é o que separa "cancelar uma reserva" de "apagar uma
// venda". Estes testes fixam as arestas em que o dinheiro muda de lado.

func TestQuemBloqueiaOCalendario(t *testing.T) {
	// spec §5 e o schema EstadoDaReserva do contrato: hold, confirmed e
	// checked_in ocupam data; o resto não.
	bloqueiam := map[string]bool{
		EstadoQuote: false, EstadoHold: true, EstadoConfirmada: true,
		EstadoCheckIn: true, EstadoCheckOut: false, EstadoFechada: false,
		EstadoCancelada: false, EstadoExpirada: false, EstadoNoShow: false,
	}
	for estado, esperado := range bloqueiam {
		if BloqueiaCalendario(estado) != esperado {
			t.Errorf("BloqueiaCalendario(%q) = %v, esperado %v", estado, !esperado, esperado)
		}
	}
}

func TestStatusDoBlocoAcompanhaOEstadoComercial(t *testing.T) {
	casos := map[string]string{
		EstadoHold:       BlocoHold,
		EstadoConfirmada: BlocoConfirmado,
		// Hóspede dentro do apartamento ocupa exatamente como reserva paga.
		EstadoCheckIn: BlocoConfirmado,
		// A partir daqui a data volta ao estoque vendável e a linha fica — mas
		// não com o mesmo rótulo. A estadia CUMPRIDA vira `completed`, que é
		// história; a venda que não aconteceu vira `cancelled`. Confundir os dois
		// foi o MÉDIO 7: o mapa devolvia os dias da estadia como `livre`, e
		// nenhuma consulta separava receita realizada de venda perdida.
		EstadoCheckOut: BlocoConcluido,
		EstadoFechada:  BlocoConcluido,
		// `no_show` fica em `cancelled` de propósito: quem não apareceu não gerou
		// estadia nenhuma. É venda perdida com retenção, não ocupação.
		EstadoCancelada: BlocoCancelado,
		EstadoNoShow:    BlocoCancelado,
		EstadoExpirada:  BlocoExpirado,
	}
	for estado, esperado := range casos {
		if got := StatusDoBlocoPara(estado); got != esperado {
			t.Errorf("StatusDoBlocoPara(%q) = %q, esperado %q", estado, got, esperado)
		}
	}

	// A invariante que importa: todo estado que bloqueia calendário tem de
	// mapear para um status que a constraint enxerga, e nenhum outro pode.
	// `completed` está de fora — ele preserva a história SEM travar inventário,
	// que é exatamente o ponto da migration 20260826120000.
	dentroDaConstraint := map[string]bool{}
	for _, s := range StatusQueBloqueiam {
		dentroDaConstraint[s] = true
	}
	for estado := range transicoes {
		if dentroDaConstraint[StatusDoBlocoPara(estado)] != BloqueiaCalendario(estado) {
			t.Errorf("estado %q: bloqueia=%v mas o bloco vira %q",
				estado, BloqueiaCalendario(estado), StatusDoBlocoPara(estado))
		}
	}
}

// Os dois predicados não podem virar um. Desde `completed`, "ocupa o
// inventário" e "aparece no mapa" são perguntas diferentes, e o dia em que
// alguém acrescentar `completed` a StatusQueBloqueiam o check-out volta a
// recusar a venda da noite seguinte — overbooking ao contrário.
func TestOsDoisPredicadosDeOcupacaoNaoSeConfundem(t *testing.T) {
	for _, s := range StatusQueBloqueiam {
		if s == BlocoConcluido {
			t.Fatal("`completed` entrou no predicado do INVENTÁRIO: estadia encerrada voltaria a travar data")
		}
	}
	visiveis := map[string]bool{}
	for _, s := range StatusVisiveisNoMapa {
		visiveis[s] = true
	}
	if !visiveis[BlocoConcluido] {
		t.Fatal("`completed` ficou fora do predicado do MAPA: a estadia cumprida some do histórico")
	}
	for _, s := range StatusQueBloqueiam {
		if !visiveis[s] {
			t.Fatalf("%q bloqueia venda mas não aparece no mapa", s)
		}
	}
	// Terminal é terminal: nenhum estado do mapa que ainda bloqueia pode ser
	// tratado como encerrado.
	for _, s := range StatusQueBloqueiam {
		if BlocoTerminal(s) {
			t.Fatalf("%q bloqueia inventário e ainda assim conta como terminal", s)
		}
	}
	if !BlocoTerminal(BlocoConcluido) || !BlocoTerminal(BlocoCancelado) || !BlocoTerminal(BlocoExpirado) {
		t.Fatal("um estado terminal deixou de ser reconhecido: DELETE /blocks o liberaria duas vezes")
	}
}

func TestTransicoesLegitimas(t *testing.T) {
	permitidas := [][2]string{
		{EstadoQuote, EstadoHold},
		{EstadoHold, EstadoConfirmada},
		{EstadoHold, EstadoExpirada},
		{EstadoHold, EstadoCancelada},
		{EstadoConfirmada, EstadoCheckIn},
		{EstadoConfirmada, EstadoNoShow},
		{EstadoCheckIn, EstadoCheckOut},
		{EstadoCheckOut, EstadoFechada},
	}
	for _, p := range permitidas {
		if !PodeTransitar(p[0], p[1]) {
			t.Errorf("%s → %s deveria ser permitida", p[0], p[1])
		}
	}

	proibidas := [][2]string{
		// Pular o sinal seria vender sem receber.
		{EstadoHold, EstadoCheckIn},
		// Reserva encerrada não volta.
		{EstadoCancelada, EstadoConfirmada},
		{EstadoExpirada, EstadoHold},
		{EstadoNoShow, EstadoCheckIn},
		// Check-in de reserva que nem sinal tem.
		{EstadoQuote, EstadoCheckIn},
		// no_show de uma pré-reserva: quem não pagou não "deixou de aparecer".
		{EstadoHold, EstadoNoShow},
	}
	for _, p := range proibidas {
		if PodeTransitar(p[0], p[1]) {
			t.Errorf("%s → %s NÃO deveria ser permitida", p[0], p[1])
		}
	}
}

func TestCancelavelCobreExatamenteOContrato(t *testing.T) {
	// O contrato: cancelled, expired, no_show, checked_out e closed devolvem
	// 409 RESERVATION_NOT_CANCELLABLE. O resto cancela.
	for _, e := range []string{EstadoQuote, EstadoHold, EstadoConfirmada, EstadoCheckIn} {
		if !Cancelavel(e) {
			t.Errorf("%q deveria ser cancelável", e)
		}
	}
	for _, e := range []string{EstadoCancelada, EstadoExpirada, EstadoNoShow, EstadoCheckOut, EstadoFechada} {
		if Cancelavel(e) {
			t.Errorf("%q NÃO deveria ser cancelável", e)
		}
	}

	esperado := []string{EstadoCheckIn, EstadoConfirmada, EstadoHold, EstadoQuote}
	if got := EstadosCancelaveis(); !reflect.DeepEqual(got, esperado) {
		t.Errorf("EstadosCancelaveis() = %v, esperado %v", got, esperado)
	}
}

func TestOrigensDeAjudamAMensagemDeErro(t *testing.T) {
	// "só reserva `hold` confirma" e "só reserva `confirmed` faz check-in" —
	// as duas frases do contrato saem daqui.
	if got := OrigensDe(EstadoConfirmada); !reflect.DeepEqual(got, []string{EstadoHold}) {
		t.Errorf("OrigensDe(confirmed) = %v", got)
	}
	if got := OrigensDe(EstadoCheckIn); !reflect.DeepEqual(got, []string{EstadoConfirmada}) {
		t.Errorf("OrigensDe(checked_in) = %v", got)
	}
	if got := OrigensDe(EstadoCheckOut); !reflect.DeepEqual(got, []string{EstadoCheckIn}) {
		t.Errorf("OrigensDe(checked_out) = %v", got)
	}
}

func TestEstadoConhecidoRecusaInvencao(t *testing.T) {
	for _, e := range []string{"", "pendente", "HOLD", "confirmado"} {
		if EstadoConhecido(e) {
			t.Errorf("%q não é estado do contrato", e)
		}
	}
	// Os nove do enum da OpenAPI, e só eles.
	if len(transicoes) != 9 {
		t.Fatalf("a máquina tem %d estados; o enum do contrato tem 9", len(transicoes))
	}
}
