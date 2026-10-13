//go:build integration

package agenda_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/agenda"
)

// A agenda que se enche sozinha (F2-20), passo a passo da reserva — sempre
// pela API, na mesma transação do passo. As datas ficam a ~2 anos de hoje, em
// faixas próprias por teste, longe das que os outros pacotes vendem.

// O critério de aceite da spec §9: `/confirm` seguido de
// `GET /agenda/events?reservation_id=` devolve os três — check-in no dia da
// entrada, check-out e limpeza no da saída —, e repetir com a mesma
// Idempotency-Key continua devolvendo três; o quarto automático, direto no
// banco, é recusado pelo índice único.
func TestConfirmarPoeOsTresEventosERepetirAChaveNaoDuplica(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	de, ate := a.hoje.AddDays(700), a.hoje.AddDays(703)

	r := a.criarReserva(t, token, a.produto(t, "cobertura"), contato, de, ate)
	if evs := a.eventosDaReserva(t, token, r.ID); len(evs) != 0 {
		t.Fatalf("pré-reserva não tem agenda; veio %d eventos", len(evs))
	}
	k := chave()
	primeira := a.confirmar(t, token, r.ID, k)
	exigir(t, primeira, http.StatusOK, "confirmando")

	evs := a.eventosDaReserva(t, token, r.ID)
	if len(evs) != 3 {
		t.Fatalf("o /confirm põe três eventos; vieram %d: %+v", len(evs), evs)
	}
	tipos := porTipo(t, evs)
	a.exigirJanela(t, tipos["checkin"], de, "14:00", "14:30", "check-in")
	a.exigirJanela(t, tipos["checkout"], ate, "10:30", "11:00", "check-out")
	a.exigirJanela(t, tipos["limpeza"], ate, "11:00", "14:00", "limpeza")
	for _, e := range evs {
		if e.Status != "agendado" || e.ResponsavelID != nil || e.Editavel != "segue_a_reserva" {
			t.Errorf("%s: status %s, responsável %v, editável %s", e.Tipo, e.Status, e.ResponsavelID, e.Editavel)
		}
		if len(e.Unidades) != 1 || e.Unidades[0].Codigo != "COB-01" || e.UnidadeID != nil {
			t.Errorf("%s: o automático lê a unidade da reserva (COB-01) e não a grava: %+v / %v", e.Tipo, e.Unidades, e.UnidadeID)
		}
		if e.ContatoID == nil || *e.ContatoID != contato || e.ContatoNome == nil {
			t.Errorf("%s: o titular é lido da reserva: %v %v", e.Tipo, e.ContatoID, e.ContatoNome)
		}
		if e.ReservaCodigo == nil || !strings.HasSuffix(e.Titulo, " · "+r.Codigo) {
			t.Errorf("%s: título %q sem o código %s", e.Tipo, e.Titulo, r.Codigo)
		}
		if len(e.Acoes) != 0 {
			t.Errorf("%s: dia futuro e automático — nenhuma ação hoje, veio %v", e.Tipo, e.Acoes)
		}
	}
	if tipos["checkin"].Data != de.String() || tipos["limpeza"].Data != ate.String() {
		t.Errorf("date é o dia da casa: %s e %s", tipos["checkin"].Data, tipos["limpeza"].Data)
	}

	// Repetir a chave devolve a resposta guardada e não cria nada.
	segunda := a.confirmar(t, token, r.ID, k)
	exigir(t, segunda, http.StatusOK, "repetindo a chave")
	if n := a.contar(t, `SELECT count(*) FROM agenda_events WHERE reservation_id = $1`, r.ID); n != 3 {
		t.Fatalf("depois da repetição: %d eventos no banco, esperado 3", n)
	}
	// O índice único recusa o quarto automático — em qualquer estado.
	_, err := a.pool.Exec(a.ctx, `
		INSERT INTO agenda_events (property_id, type, origin, reservation_id, starts_at, ends_at, status, cancelled_at)
		VALUES ($1, 'checkin', 'reservation', $2, $3, $4, 'cancelado', now())`,
		a.propriedade, r.ID, a.em(de, "15:00"), a.em(de, "15:30"))
	if err == nil {
		t.Fatal("um segundo check-in automático entrou no banco")
	}

	// A trilha: três criações, com o ator do /confirm.
	if n := a.contar(t, `SELECT count(*) FROM audit_log WHERE entity = 'agenda_events' AND action = 'agenda_events.criado'
		AND entity_id IN (SELECT id FROM agenda_events WHERE reservation_id = $1) AND actor_id IS NOT NULL`, r.ID); n != 3 {
		t.Errorf("trilha: %d linhas de criação, esperado 3", n)
	}
}

// Cancelar a reserva deixa os três em `cancelado`, sem apagar; a simulação
// (`dry_run`) não toca na agenda.
func TestCancelarAReservaCancelaOsEventosSemApagar(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	de, ate := a.hoje.AddDays(710), a.hoje.AddDays(713)
	r := a.reservaConfirmada(t, token, "cobertura", a.contato(t), de, ate)

	exigir(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/cancel?dry_run=true", token, map[string]any{}),
		http.StatusOK, "simulando")
	for _, e := range a.eventosDaReserva(t, token, r.ID) {
		if e.Status != "agendado" {
			t.Fatalf("dry_run mexeu na agenda: %s %s", e.Tipo, e.Status)
		}
	}

	exigir(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/cancel", token, map[string]any{"reason": "desistência"}),
		http.StatusOK, "cancelando")
	evs := a.eventosDaReserva(t, token, r.ID)
	if len(evs) != 3 {
		t.Fatalf("cancelar não apaga: %d eventos", len(evs))
	}
	for _, e := range evs {
		if e.Status != "cancelado" || e.CanceladoEm == nil || e.Editavel != "nada" || len(e.Acoes) != 0 {
			t.Errorf("%s: %s, cancelled_at %v, editável %s, ações %v", e.Tipo, e.Status, e.CanceladoEm, e.Editavel, e.Acoes)
		}
	}
	// O padrão da lista esconde os cancelados.
	r2 := a.chamar(t, http.MethodGet, "/agenda/events?reservation_id="+r.ID.String(), token, nil)
	exigir(t, r2, http.StatusOK, "lista padrão")
	if evs, _ := lista[eventoVisto](t, r2); len(evs) != 0 {
		t.Errorf("o padrão (agendado,concluido) mostrou %d cancelados", len(evs))
	}
}

// Remarcar é cancelar a original e confirmar a nova: os da original viram
// `cancelado`, a nova nasce com os três dela nas datas novas; o ajustado à mão
// na original não é copiado.
func TestRemarcarCancelaOsDaOriginalECriaOsDaNova(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	de, ate := a.hoje.AddDays(720), a.hoje.AddDays(723)
	r := a.reservaConfirmada(t, token, "cobertura", a.contato(t), de, ate)

	// Um ajuste à mão na original (o hóspede chega às 16:00).
	original := porTipo(t, a.eventosDaReserva(t, token, r.ID))
	exigir(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+original["checkin"].ID.String(), token, map[string]any{
		"starts_at": a.rfc(de, "16:00"), "ends_at": a.rfc(de, "16:30"), "notes": "chega de carro",
	}), http.StatusOK, "ajustando o check-in")

	novoDe, novoAte := a.hoje.AddDays(726), a.hoje.AddDays(729)
	resp := a.chamarCom(t, http.MethodPost, "/reservations/"+r.ID.String()+"/reschedule", token,
		map[string]any{"check_in": novoDe.String(), "check_out": novoAte.String()},
		map[string]string{"Idempotency-Key": chave()})
	exigir(t, resp, http.StatusCreated, "remarcando")
	nova := dado[reservaVista](t, resp)

	for _, e := range a.eventosDaReserva(t, token, r.ID) {
		if e.Status != "cancelado" {
			t.Errorf("original: %s ficou %s", e.Tipo, e.Status)
		}
	}
	novos := porTipo(t, a.eventosDaReserva(t, token, nova.ID))
	if len(novos) != 3 {
		t.Fatalf("a nova nasce confirmada com três eventos; vieram %d", len(novos))
	}
	a.exigirJanela(t, novos["checkin"], novoDe, "14:00", "14:30", "check-in da nova (o ajuste da original não é copiado)")
	a.exigirJanela(t, novos["checkout"], novoAte, "10:30", "11:00", "check-out da nova")
	a.exigirJanela(t, novos["limpeza"], novoAte, "11:00", "14:00", "limpeza da nova")
	if novos["checkin"].Notas != nil {
		t.Errorf("a anotação da original foi copiada: %q", *novos["checkin"].Notas)
	}
}

// Realocar: o automático já aparece na unidade nova (lê da reserva); o manual
// agendado preso à unidade que saiu muda junto; o manual sem unidade segue a
// reserva sozinho; o desmarcado fica onde estava.
func TestRealocarLevaSoOManualPresoAUnidadeQueSaiu(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	de, ate := a.hoje.AddDays(740), a.hoje.AddDays(743)
	r := a.reservaConfirmada(t, token, "suite-piscina", a.contato(t), de, ate)
	if len(r.Unidades) != 1 {
		t.Fatalf("a suíte ocupa uma unidade: %+v", r.Unidades)
	}
	origem := r.Unidades[0].ID
	destino := a.outraUnidadeDoProduto(t, "suite-piscina", r.Unidades[0].Codigo)
	meio := de.AddDays(1)

	preso := a.marcar(t, token, map[string]any{
		"type": "limpeza", "title": "Faxina do meio da estadia", "reservation_id": r.ID, "unit_id": origem,
		"starts_at": a.rfc(meio, "10:00"), "ends_at": a.rfc(meio, "11:00"),
	}).Data
	solto := a.marcar(t, token, map[string]any{
		"type": "evento", "title": "Jantar do grupo", "reservation_id": r.ID,
		"starts_at": a.rfc(meio, "18:00"), "ends_at": a.rfc(meio, "19:00"),
	}).Data
	desmarcado := a.marcar(t, token, map[string]any{
		"type": "visita", "title": "Vistoria", "reservation_id": r.ID, "unit_id": origem,
		"starts_at": a.rfc(meio, "12:00"), "ends_at": a.rfc(meio, "12:30"),
	}).Data
	exigir(t, a.chamar(t, http.MethodDelete, "/agenda/events/"+desmarcado.ID.String(), token, nil), http.StatusOK, "desmarcando")

	exigir(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/reassign-unit", token,
		map[string]any{"to_unit_id": destino}), http.StatusOK, "realocando")

	for _, e := range a.eventosDaReserva(t, token, r.ID) {
		switch e.ID {
		case preso.ID:
			if e.UnidadeID == nil || *e.UnidadeID != destino {
				t.Errorf("o manual preso à unidade que saiu muda junto: %v", e.UnidadeID)
			}
		case solto.ID:
			if e.UnidadeID != nil || len(e.Unidades) != 1 || e.Unidades[0].ID != destino {
				t.Errorf("o manual sem unidade segue a reserva sozinho: %v %+v", e.UnidadeID, e.Unidades)
			}
		case desmarcado.ID:
			if e.UnidadeID == nil || *e.UnidadeID != origem {
				t.Errorf("o desmarcado fica onde estava: %v", e.UnidadeID)
			}
		default:
			if e.Origem != "reservation" || len(e.Unidades) != 1 || e.Unidades[0].ID != destino {
				t.Errorf("automático %s não está na unidade nova: %+v", e.Tipo, e.Unidades)
			}
		}
	}
	if n := a.contar(t, `SELECT count(*) FROM audit_log WHERE action = 'agenda_events.realocado' AND entity_id = $1`, preso.ID); n != 1 {
		t.Errorf("trilha da realocação do evento: %d linhas", n)
	}
}

// A agenda nunca trava a realocação: o manual preso à unidade que saiu, se cai
// na de destino sobre OUTRA marcação à mão no mesmo horário, segue a reserva
// SEM UNIDADE PRÓPRIA (`unit_id` nulo, ocupando as unidades dela) — 200, a
// reserva na unidade nova, e a colisão à vista (`has_unit_conflict`). O outro
// evento preso à unidade que saiu, sem colisão, muda normalmente na mesma
// realocação: a recusa de um não desfaz o outro. A trilha diz que unidade o
// evento perdeu e por quê.
func TestRealocarSobreOutraMarcacaoSegueSemUnidadePropria(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	de, ate := a.hoje.AddDays(745), a.hoje.AddDays(748)
	r := a.reservaConfirmada(t, token, "suite-piscina", a.contato(t), de, ate)
	origem, codigoDaOrigem := r.Unidades[0].ID, r.Unidades[0].Codigo
	destino := a.outraUnidadeDoProduto(t, "suite-piscina", codigoDaOrigem)
	meio := de.AddDays(1)

	colide := a.marcar(t, token, map[string]any{
		"type": "limpeza", "title": "Faxina do meio", "reservation_id": r.ID, "unit_id": origem,
		"starts_at": a.rfc(meio, "10:00"), "ends_at": a.rfc(meio, "11:00"),
	}).Data
	cabe := a.marcar(t, token, map[string]any{
		"type": "visita", "title": "Vistoria do ar-condicionado", "reservation_id": r.ID, "unit_id": origem,
		"starts_at": a.rfc(meio, "16:00"), "ends_at": a.rfc(meio, "16:30"),
	}).Data
	outra := a.marcar(t, token, map[string]any{
		"type": "visita", "title": "Visita de corretor", "unit_id": destino,
		"starts_at": a.rfc(meio, "10:30"), "ends_at": a.rfc(meio, "11:30"),
	}).Data

	resp := a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/reassign-unit", token, map[string]any{"to_unit_id": destino})
	exigir(t, resp, http.StatusOK, "realocando para cima da visita")
	var unidade uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT unit_id FROM reservation_units WHERE reservation_id = $1`, r.ID).Scan(&unidade); err != nil {
		t.Fatal(err)
	}
	if unidade != destino {
		t.Fatalf("a reserva tem de estar na unidade nova: %s", unidade)
	}

	ev := dado[eventoVisto](t, a.chamar(t, http.MethodGet, "/agenda/events/"+colide.ID.String(), token, nil))
	if ev.UnidadeID != nil {
		t.Fatalf("o evento que não coube segue a reserva sem unidade própria: unit_id %v", ev.UnidadeID)
	}
	if ev.Status != "agendado" || len(ev.Unidades) != 1 || ev.Unidades[0].ID != destino {
		t.Fatalf("ele ocupa as unidades da reserva (a nova): %s %+v", ev.Status, ev.Unidades)
	}
	if !ev.ConflitoDeUnidade {
		t.Fatal("a colisão com a outra marcação tem de aparecer em has_unit_conflict")
	}
	if v := dado[eventoVisto](t, a.chamar(t, http.MethodGet, "/agenda/events/"+outra.ID.String(), token, nil)); v.UnidadeID == nil ||
		*v.UnidadeID != destino || v.Status != "agendado" || !v.ConflitoDeUnidade {
		t.Errorf("a outra marcação fica onde estava, também em conflito: %+v", v)
	}
	if c := dado[eventoVisto](t, a.chamar(t, http.MethodGet, "/agenda/events/"+cabe.ID.String(), token, nil)); c.UnidadeID == nil ||
		*c.UnidadeID != destino || c.ConflitoDeUnidade {
		t.Errorf("o evento que cabe muda junto, sem conflito: %+v", c)
	}
	for _, e := range porTipo(t, a.eventosDaReserva(t, token, r.ID)) {
		if len(e.Unidades) != 1 || e.Unidades[0].ID != destino {
			t.Errorf("automático %s fora da unidade nova: %+v", e.Tipo, e.Unidades)
		}
	}

	var antes, depois string
	if err := a.pool.QueryRow(a.ctx, `
		SELECT before::text, after::text FROM audit_log
		 WHERE action = 'agenda_events.realocado_sem_unidade' AND entity_id = $1`, colide.ID).Scan(&antes, &depois); err != nil {
		t.Fatalf("a trilha do evento diz que ele perdeu a unidade: %v", err)
	}
	if !strings.Contains(antes, origem.String()) || !strings.Contains(antes, codigoDaOrigem) ||
		!strings.Contains(depois, "unidade_de_destino_ocupada_por_marcacao_manual") || !strings.Contains(depois, destino.String()) {
		t.Errorf("trilha: antes %s, depois %s", antes, depois)
	}
	if n := a.contar(t, `SELECT count(*) FROM audit_log WHERE action = 'agenda_events.realocado' AND entity_id = $1`, cabe.ID); n != 1 {
		t.Errorf("trilha da mudança normal: %d", n)
	}
}

// Check-in e check-out registrados concluem os automáticos; a saída
// ANTECIPADA traz o check-out e a limpeza para o dia em que a unidade
// esvaziou.
func TestCheckInECheckOutConcluemEAntecipamAAgenda(t *testing.T) {
	a := subir(t)
	quem, token := a.gestor(t)
	de, ate := a.hoje.AddDays(750), a.hoje.AddDays(753)
	r := a.reservaConfirmada(t, token, "cobertura", a.contato(t), de, ate)

	entrada := a.rfc(de, "19:12")
	exigir(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/check-in", token, map[string]any{"at": entrada}),
		http.StatusOK, "check-in")
	tipos := porTipo(t, a.eventosDaReserva(t, token, r.ID))
	ci := tipos["checkin"]
	if ci.Status != "concluido" || ci.ConcluidoEm == nil || !ci.ConcluidoEm.Equal(a.em(de, "19:12")) {
		t.Fatalf("check-in: %s em %v, esperado concluido em %s", ci.Status, ci.ConcluidoEm, entrada)
	}
	if ci.ConcluidoPor == nil || *ci.ConcluidoPor != quem {
		t.Errorf("done_by é quem registrou: %v", ci.ConcluidoPor)
	}
	a.exigirJanela(t, ci, de, "14:00", "14:30", "o atrasado continua no horário combinado")

	// Saída no dia seguinte à entrada: duas noites antes do contratado.
	saida := de.AddDays(1)
	exigir(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/check-out", token,
		map[string]any{"at": a.rfc(saida, "09:40")}), http.StatusOK, "check-out antecipado")
	tipos = porTipo(t, a.eventosDaReserva(t, token, r.ID))
	co, limpeza := tipos["checkout"], tipos["limpeza"]
	if co.Status != "concluido" || co.ConcluidoEm == nil || !co.ConcluidoEm.Equal(a.em(saida, "09:40")) {
		t.Fatalf("check-out: %s em %v", co.Status, co.ConcluidoEm)
	}
	a.exigirJanela(t, co, saida, "10:30", "11:00", "o check-out é desenhado no dia em que aconteceu")
	if limpeza.Status != "agendado" {
		t.Fatalf("a limpeza continua por fazer: %s", limpeza.Status)
	}
	a.exigirJanela(t, limpeza, saida, "11:00", "14:00", "a limpeza vem para o dia em que a unidade esvaziou")
}

// Cancelar com o hóspede DENTRO: o check-out é cancelado (não houve saída
// registrada), a limpeza vem para HOJE e o manual que não começou é cancelado.
func TestCancelarComOHospedeDentroTrazALimpezaParaHoje(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	de, ate := a.hoje.AddDays(756), a.hoje.AddDays(759)
	r := a.reservaConfirmada(t, token, "cobertura", a.contato(t), de, ate)
	exigir(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/check-in", token,
		map[string]any{"at": a.rfc(de, "15:00")}), http.StatusOK, "check-in")
	jantar := a.marcar(t, token, map[string]any{
		"type": "evento", "title": "Jantar", "reservation_id": r.ID,
		"starts_at": a.rfc(de.AddDays(1), "19:00"), "ends_at": a.rfc(de.AddDays(1), "21:00"),
	}).Data

	exigir(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/cancel", token, map[string]any{}),
		http.StatusOK, "cancelando com o hóspede dentro")
	evs := a.eventosDaReserva(t, token, r.ID)
	tipos := porTipo(t, evs)
	if tipos["checkin"].Status != "concluido" || tipos["checkout"].Status != "cancelado" {
		t.Fatalf("check-in %s, check-out %s", tipos["checkin"].Status, tipos["checkout"].Status)
	}
	if tipos["limpeza"].Status != "agendado" {
		t.Fatalf("a limpeza continua por fazer: %s", tipos["limpeza"].Status)
	}
	a.exigirJanela(t, tipos["limpeza"], a.hoje, "11:00", "14:00", "a limpeza vem para hoje")
	for _, e := range evs {
		if e.ID == jantar.ID && e.Status != "cancelado" {
			t.Errorf("o manual que não começou é cancelado: %s", e.Status)
		}
	}
}

// Gravar os horários reprograma o automático agendado, de hoje em diante, que
// ainda estava no horário antigo — e deixa onde está o ajustado à mão.
func TestGravarOsHorariosReprogramaOFuturoEPreservaOAjustado(t *testing.T) {
	a := subir(t)
	quem, token := a.gestor(t)
	de, ate := a.hoje.AddDays(760), a.hoje.AddDays(763)
	r := a.reservaConfirmada(t, token, "cobertura", a.contato(t), de, ate)
	tipos := porTipo(t, a.eventosDaReserva(t, token, r.ID))
	exigir(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+tipos["checkin"].ID.String(), token, map[string]any{
		"starts_at": a.rfc(de, "16:00"), "ends_at": a.rfc(de, "16:30"),
	}), http.StatusOK, "ajustando o check-in à mão")

	// Os horários voltam aos provisórios no fim, pelo banco: os outros testes
	// do pacote contam com eles.
	t.Cleanup(func() {
		a.executar(t, `
			UPDATE agenda_settings
			   SET checkin_time = DEFAULT, checkout_time = DEFAULT, cleaning_start = DEFAULT, cleaning_end = DEFAULT,
			       business_hours = DEFAULT, slot_minutes = DEFAULT, buffer_minutes = DEFAULT, updated_by = NULL
			 WHERE property_id = $1`, a.propriedade)
	})

	horarios := horariosConfirmados("15:00", "12:00", "12:00", "15:00")
	resp := a.chamar(t, http.MethodPut, "/agenda/settings", token, horarios)
	exigir(t, resp, http.StatusOK, "gravando os horários")
	var corpo struct {
		Data struct {
			Entrada       string     `json:"checkin_time"`
			Provisoria    bool       `json:"provisional"`
			AtualizadaPor *uuid.UUID `json:"updated_by"`
		} `json:"data"`
		Meta struct {
			Reprogramados *int `json:"retimed_events"`
		} `json:"meta"`
	}
	if err := jsonDe(resp.Corpo, &corpo); err != nil {
		t.Fatal(err)
	}
	if corpo.Data.Entrada != "15:00" || corpo.Data.Provisoria || corpo.Data.AtualizadaPor == nil || *corpo.Data.AtualizadaPor != quem {
		t.Fatalf("configuração gravada: %+v", corpo.Data)
	}
	if corpo.Meta.Reprogramados == nil || *corpo.Meta.Reprogramados < 2 {
		t.Fatalf("meta.retimed_events: %v (o check-out e a limpeza desta reserva, no mínimo)", corpo.Meta.Reprogramados)
	}

	tipos = porTipo(t, a.eventosDaReserva(t, token, r.ID))
	a.exigirJanela(t, tipos["checkin"], de, "16:00", "16:30", "o ajustado à mão fica")
	a.exigirJanela(t, tipos["checkout"], ate, "11:30", "12:00", "o check-out vai para o horário novo")
	a.exigirJanela(t, tipos["limpeza"], ate, "12:00", "15:00", "a limpeza vai para o horário novo")
	if n := a.contar(t, `SELECT count(*) FROM audit_log WHERE action = 'agenda_events.reprogramado' AND entity_id = $1`,
		tipos["limpeza"].ID); n != 1 {
		t.Errorf("trilha da reprogramação: %d", n)
	}

	// O mesmo corpo de novo: nada mais a reprogramar.
	resp = a.chamar(t, http.MethodPut, "/agenda/settings", token, horarios)
	exigir(t, resp, http.StatusOK, "gravando de novo")
	if err := jsonDe(resp.Corpo, &corpo); err != nil {
		t.Fatal(err)
	}
	if *corpo.Meta.Reprogramados != 0 {
		t.Errorf("a segunda gravação reprogramou %d", *corpo.Meta.Reprogramados)
	}
}

// O preenchimento das reservas confirmadas antes da agenda: o mesmo Plan do
// /confirm, idempotente — a segunda execução cria zero. A reserva já em curso
// nasce com o check-in concluído no instante registrado.
func TestPreenchimentoDasReservasAnterioresCriaUmaVezSo(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	confirmada := a.reservaConfirmada(t, token, "cobertura", contato, a.hoje.AddDays(770), a.hoje.AddDays(773))
	emCurso := a.reservaConfirmada(t, token, "cobertura", contato, a.hoje.AddDays(776), a.hoje.AddDays(779))
	entrada := a.em(a.hoje.AddDays(776), "18:05")
	exigir(t, a.chamar(t, http.MethodPost, "/reservations/"+emCurso.ID.String()+"/check-in", token,
		map[string]any{"at": entrada.Format(time.RFC3339)}), http.StatusOK, "check-in")

	// Numa transação que volta atrás no fim: o preenchimento é da casa
	// inteira, e o teste não deixa nada para os outros.
	tx, err := a.pool.Begin(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// "Antes da agenda": as duas reservas sem os eventos.
	if _, err := tx.Exec(a.ctx, `DELETE FROM agenda_events WHERE reservation_id = ANY($1::uuid[])`,
		[]uuid.UUID{confirmada.ID, emCurso.ID}); err != nil {
		t.Fatal(err)
	}

	criados, err := agenda.PreencherReservasConfirmadas(a.ctx, tx, a.propriedade)
	if err != nil {
		t.Fatalf("primeira execução: %v", err)
	}
	if criados < 6 {
		t.Fatalf("primeira execução criou %d; as duas reservas do teste pedem 6", criados)
	}
	var n, checkins int
	var feitoEm time.Time
	if err := tx.QueryRow(a.ctx, `
		SELECT count(*), count(*) FILTER (WHERE type = 'checkin'),
		       max(done_at) FILTER (WHERE type = 'checkin' AND reservation_id = $2)
		  FROM agenda_events WHERE reservation_id = ANY($1::uuid[]) AND origin = 'reservation'`,
		[]uuid.UUID{confirmada.ID, emCurso.ID}, emCurso.ID).Scan(&n, &checkins, &feitoEm); err != nil {
		t.Fatal(err)
	}
	if n != 6 || checkins != 2 {
		t.Fatalf("%d eventos (%d check-ins), esperado 6 (2)", n, checkins)
	}
	if !feitoEm.Equal(entrada) {
		t.Fatalf("o check-in da reserva em curso nasce concluído em %s; veio %s", entrada, feitoEm)
	}

	segunda, err := agenda.PreencherReservasConfirmadas(a.ctx, tx, a.propriedade)
	if err != nil {
		t.Fatalf("segunda execução: %v", err)
	}
	if segunda != 0 {
		t.Fatalf("a segunda execução criou %d; o preenchimento é idempotente", segunda)
	}
}

// A agenda nunca trava a venda: a visita marcada antes passa a COLIDIR com a
// estadia vendida depois, e a lista mostra (`has_unit_conflict`).
func TestVendaDepoisDaVisitaAparecemComoConflitoNaLista(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	de, ate := a.hoje.AddDays(780), a.hoje.AddDays(783)
	cobertura := a.unidadeDoSeed(t, "COB-01")
	visita := a.marcar(t, token, map[string]any{
		"type": "visita", "title": "Visita com cliente", "unit_id": cobertura,
		"starts_at": a.rfc(de.AddDays(1), "10:00"), "ends_at": a.rfc(de.AddDays(1), "11:00"),
	}).Data
	if visita.ConflitoDeUnidade {
		t.Fatal("a visita nasce sem conflito")
	}

	a.reservaConfirmada(t, token, "cobertura", a.contato(t), de, ate)

	ev := dado[eventoVisto](t, a.chamar(t, http.MethodGet, "/agenda/events/"+visita.ID.String(), token, nil))
	if !ev.ConflitoDeUnidade {
		t.Fatal("a venda chegou depois: a visita agora colide com a estadia, e a leitura tem de mostrar")
	}
	// Anotar a visita que JÁ colide não é recusado pela colisão que ninguém
	// criou agora.
	exigir(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+visita.ID.String(), token, map[string]any{"notes": "remarcar"}),
		http.StatusOK, "anotando a visita em conflito")
}

// O automático só muda o que é da agenda: horário (check-in no mesmo dia),
// responsável e anotação. Reenviar o que foi lido passa; tipo, título,
// contato e unidade diferentes são 422; check-in e check-out não concluem nem
// se desmarcam por aqui.
func TestEventoAutomaticoSoMudaOQueEhDaAgenda(t *testing.T) {
	a := subir(t)
	quem, token := a.gestor(t)
	de, ate := a.hoje.AddDays(786), a.hoje.AddDays(789)
	contato := a.contato(t)
	r := a.reservaConfirmada(t, token, "cobertura", contato, de, ate)
	tipos := porTipo(t, a.eventosDaReserva(t, token, r.ID))
	ci := tipos["checkin"]
	caminho := "/agenda/events/" + ci.ID.String()

	// O formulário que reenvia o evento lido não falha pelo que não tocou.
	eco := a.chamar(t, http.MethodPut, caminho, token, map[string]any{
		"title": ci.Titulo, "starts_at": ci.Inicio.Format(time.RFC3339), "ends_at": ci.Fim.Format(time.RFC3339),
		"all_day": false, "unit_id": ci.Unidades[0].ID, "reservation_id": r.ID, "contact_id": contato,
	})
	exigir(t, eco, http.StatusOK, "reenviando o automático como foi lido")

	for campo, valor := range map[string]any{"title": "Outro título", "contact_id": a.contato(t), "all_day": true} {
		resp := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{campo: valor})
		exigirErro(t, resp, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "mudando "+campo+" do automático")
		if resp.detalhes(t)[campo] == nil {
			t.Errorf("o 422 não nomeia %s: %s", campo, resp.Corpo)
		}
	}
	outroDia := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{
		"starts_at": a.rfc(de.AddDays(1), "14:00"), "ends_at": a.rfc(de.AddDays(1), "14:30"),
	})
	exigirErro(t, outroDia, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "check-in em outro dia")

	ok := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{
		"starts_at": a.rfc(de, "17:00"), "ends_at": a.rfc(de, "17:30"), "assignee_id": quem, "notes": "late check-in",
	})
	exigir(t, ok, http.StatusOK, "mudando hora, responsável e anotação")
	e := escrita(t, ok).Data
	if e.ResponsavelID == nil || *e.ResponsavelID != quem || e.Notas == nil {
		t.Fatalf("responsável e anotação não gravaram: %+v", e)
	}

	exigirErro(t, a.chamar(t, http.MethodPost, caminho+"/complete", token, nil),
		http.StatusConflict, "INVALID_STATE_TRANSITION", "concluindo o check-in por aqui")
	exigirErro(t, a.chamar(t, http.MethodDelete, "/agenda/events/"+tipos["checkout"].ID.String(), token, nil),
		http.StatusConflict, "INVALID_STATE_TRANSITION", "desmarcando o check-out")
	resp := a.chamar(t, http.MethodDelete, "/agenda/events/"+tipos["limpeza"].ID.String(), token, nil)
	exigirErro(t, resp, http.StatusConflict, "INVALID_STATE_TRANSITION", "desmarcando a limpeza da reserva")
	if resp.detalhes(t)["reason"] != "segue_a_reserva" {
		t.Errorf("details.reason: %v", resp.detalhes(t)["reason"])
	}
	// A limpeza não começa antes de o hóspede sair.
	exigirErro(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+tipos["limpeza"].ID.String(), token, map[string]any{
		"starts_at": a.rfc(ate, "09:00"), "ends_at": a.rfc(ate, "10:00"),
	}), http.StatusUnprocessableEntity, "VALIDATION_ERROR", "limpeza antes do check-out")
	// Mover o check-out para depois cruza a limpeza da mesma unidade: 409.
	exigirErro(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+tipos["checkout"].ID.String(), token, map[string]any{
		"starts_at": a.rfc(ate, "12:30"), "ends_at": a.rfc(ate, "13:00"),
	}), http.StatusConflict, "UNIT_NOT_AVAILABLE", "check-out por cima da limpeza")
}

// outraUnidadeDoProduto é uma unidade do produto que não é `exceto`.
func (a *ambiente) outraUnidadeDoProduto(t *testing.T, produto, exceto string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		SELECT u.id FROM unit_type_members m
		  JOIN unit_types ut ON ut.id = m.unit_type_id
		  JOIN units u ON u.id = m.unit_id
		 WHERE ut.property_id = $1 AND ut.code = $2 AND u.code <> $3 AND u.active
		 ORDER BY u.code LIMIT 1`, a.propriedade, produto, exceto).Scan(&id); err != nil {
		t.Fatalf("outra unidade de %s: %v", produto, err)
	}
	return id
}
