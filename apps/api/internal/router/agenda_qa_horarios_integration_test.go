//go:build integration

package router

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// Os horários são DADO (spec §9, `GET|PUT /agenda/settings`): nascem
// provisórios, a confirmar pelo dono; gravar pede `settings:editar`; e gravar
// reprograma o que a agenda já tinha enchido com o horário antigo — sem tocar
// no ajustado à mão, no passado e no concluído.

// qagExigirProvisorio reprova o banco em que alguém deixou os horários
// gravados: os testes desta fatia partem dos provisórios do contrato.
func qagExigirProvisorio(t *testing.T, h qagHorarios) {
	t.Helper()
	if !h.Provisoria || h.AtualizadaPor != nil {
		t.Fatalf("os horários da casa já foram gravados por alguém (provisional=%v, updated_by=%v): "+
			"ambiente sujo — a limpeza de um teste anterior falhou", h.Provisoria, h.AtualizadaPor)
	}
}

// Em linguagem de negócio: enquanto o dono não confirma, a casa trabalha com
// os horários provisórios — entrada às 14:00, saída às 11:00, limpeza das
// 11:00 às 14:00, funcionamento das 08:00 às 20:00 nos sete dias, passo de 30
// e folga de 15 — e o painel sabe que eles são provisórios. "Hoje" é o dia da
// casa, e não o do navegador. Os três perfis que operam leem a mesma coisa.
func TestAgendaQAHorariosNascemProvisorios(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	usuario := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	corretor := a.criarUsuario(t, "ag-corretor", a.qbPerfilDoSeed(t, "corretor"))
	a.qagFaxina(t, admin, usuario, corretor)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))

	for nome, tk := range map[string]string{"admin": admin.Token, "usuario": usuario.Token, "corretor": corretor.Token} {
		r := a.chamar(t, http.MethodGet, "/agenda/settings", tk, nil)
		h := qbDado[qagHorarios](t, r, http.StatusOK, "GET /agenda/settings do "+nome)
		qagExigirProvisorio(t, h)
		if h.Entrada != "14:00" || h.Saida != "11:00" || h.LimpezaDe != "11:00" || h.LimpezaAte != "14:00" ||
			h.Passo != 30 || h.Folga != 15 {
			t.Errorf("%s: os provisórios são 14:00 / 11:00 / 11:00–14:00, passo 30, folga 15; vieram %+v", nome, h)
		}
		if len(h.Funcionamento) != 7 {
			t.Errorf("%s: funcionamento nos sete dias; vieram %d intervalos", nome, len(h.Funcionamento))
		}
		for i, iv := range h.Funcionamento {
			if iv.Dia != i+1 || iv.Inicio != "08:00" || iv.Fim != "20:00" {
				t.Errorf("%s: business_hours[%d] = %+v, esperado dia %d das 08:00 às 20:00 (ordenado por dia)", nome, i, iv, i+1)
			}
		}
		if h.Fuso != casa.Fuso || h.Hoje != casa.Hoje.String() {
			t.Errorf("%s: timezone %q / today %q; a casa é %q e hoje lá é %s", nome, h.Fuso, h.Hoje, casa.Fuso, casa.Hoje)
		}
		if _, off := qagOffset(t, h.AtualizadaEm); off != qagOffsetDaCasa(casa) {
			t.Errorf("%s: updated_at %q fora do offset da casa", nome, h.AtualizadaEm)
		}
		if fora := qbChavesForaDoContrato(t, "ConfiguracaoDaAgenda", r.Corpo); len(fora) > 0 {
			t.Errorf("%s: GET /agenda/settings com chaves fora do contrato: %v", nome, fora)
		}
	}
}

// Em linguagem de negócio: o dono digitou um horário que não fecha — a
// limpeza começando antes de o hóspede sair, o próximo entrando antes de a
// limpeza acabar, um passo que não divide o dia. A casa recusa com 422 e diz
// TODOS os campos de uma vez, e nada é gravado. Hora em outro formato ("9:00",
// "14h") e campo só de leitura (timezone, today) também são 422.
func TestAgendaQAHorariosIncoerentesDao422ENadaMuda(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	a.qagFaxina(t, admin)
	antes := a.qagHorariosDaCasa(t, admin.Token)
	qagExigirProvisorio(t, antes)

	comHorario := func(mudar func(c map[string]any)) map[string]any {
		c := qagCorpoDosHorarios("14:00", "11:00", "11:00", "14:00", 30, 15)
		mudar(c)
		return c
	}
	casos := []struct {
		nome   string
		corpo  map[string]any
		campos []string
	}{
		{"limpeza antes do check-out", comHorario(func(c map[string]any) { c["checkout_time"] = "12:00" }), []string{"cleaning_start"}},
		{"limpeza que termina antes de começar", comHorario(func(c map[string]any) {
			c["cleaning_end"] = "10:00"
			c["checkout_time"] = "09:00"
			c["cleaning_start"] = "10:00"
		}), []string{"cleaning_end"}},
		{"entrada antes do fim da limpeza", comHorario(func(c map[string]any) { c["checkin_time"] = "13:00" }), []string{"checkin_time"}},
		{"passo que não divide o dia", comHorario(func(c map[string]any) { c["slot_minutes"] = 7 }), []string{"slot_minutes"}},
		{"passo zero", comHorario(func(c map[string]any) { c["slot_minutes"] = 0 }), []string{"slot_minutes"}},
		{"folga negativa", comHorario(func(c map[string]any) { c["buffer_minutes"] = -1 }), []string{"buffer_minutes"}},
		{"folga acima do teto", comHorario(func(c map[string]any) { c["buffer_minutes"] = 241 }), []string{"buffer_minutes"}},
		{"funcionamento invertido", comHorario(func(c map[string]any) {
			c["business_hours"] = []map[string]any{{"weekday": 1, "start": "20:00", "end": "08:00"}}
		}), []string{"business_hours[0].end"}},
		{"dia da semana fora de 1–7", comHorario(func(c map[string]any) {
			c["business_hours"] = []map[string]any{{"weekday": 8, "start": "08:00", "end": "20:00"}}
		}), []string{"business_hours[0].weekday"}},
		{"intervalos sobrepostos no mesmo dia", comHorario(func(c map[string]any) {
			c["business_hours"] = []map[string]any{{"weekday": 2, "start": "08:00", "end": "12:00"}, {"weekday": 2, "start": "11:00", "end": "18:00"}}
		}), []string{"business_hours[1]"}},
		{"hora sem o zero", comHorario(func(c map[string]any) { c["checkin_time"] = "9:00" }), []string{"checkin_time"}},
		{"hora com h", comHorario(func(c map[string]any) { c["checkout_time"] = "11h" }), []string{"checkout_time"}},
		{"hora com segundos", comHorario(func(c map[string]any) { c["cleaning_start"] = "11:00:00" }), []string{"cleaning_start"}},
		{"timezone é só leitura", comHorario(func(c map[string]any) { c["timezone"] = "America/Sao_Paulo" }), []string{"timezone"}},
		{"today é só leitura", comHorario(func(c map[string]any) { c["today"] = "2026-01-01" }), []string{"today"}},
		{"provisional é só leitura", comHorario(func(c map[string]any) { c["provisional"] = false }), []string{"provisional"}},
		{"faltou o passo", comHorario(func(c map[string]any) { delete(c, "slot_minutes") }), []string{"slot_minutes"}},
		{"todas as violações de uma vez", comHorario(func(c map[string]any) {
			c["checkout_time"] = "12:00"
			c["checkin_time"] = "13:00"
			c["slot_minutes"] = 7
			c["buffer_minutes"] = 500
		}), []string{"cleaning_start", "checkin_time", "slot_minutes", "buffer_minutes"}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			e := qbErro(t, a.chamar(t, http.MethodPut, "/agenda/settings", admin.Token, c.corpo), http.StatusUnprocessableEntity,
				"VALIDATION_ERROR", c.nome)
			for _, campo := range c.campos {
				if e.Details[campo] == nil {
					t.Errorf("o 422 não nomeia %s; details: %v", campo, e.Details)
				}
			}
		})
	}
	if depois := a.qagHorariosDaCasa(t, admin.Token); !depois.Provisoria || depois.AtualizadaPor != nil || depois.Entrada != antes.Entrada {
		t.Fatalf("um 422 gravou os horários: %+v", depois)
	}
}

// qagSnapshot é o instantâneo das janelas dos eventos da casa, para contar
// quem o PUT mexeu sem saber nada de como ele decide.
func (a *ambiente) qagSnapshot(t *testing.T, prop uuid.UUID) map[uuid.UUID]string {
	t.Helper()
	linhas, err := a.pool.Query(a.ctx, `SELECT id, starts_at::text || '/' || ends_at::text FROM agenda_events WHERE property_id = $1`, prop)
	if err != nil {
		t.Fatal(err)
	}
	defer linhas.Close()
	out := map[uuid.UUID]string{}
	for linhas.Next() {
		var (
			id     uuid.UUID
			janela string
		)
		if err := linhas.Scan(&id, &janela); err != nil {
			t.Fatal(err)
		}
		out[id] = janela
	}
	return out
}

// Em linguagem de negócio: o dono confirma os horários da casa (entrada às
// 15:00, saída às 10:00, limpeza das 10:00 às 15:00, passo de uma hora). Tudo
// o que a agenda já tinha marcado nos horários provisórios, de HOJE em
// diante, passa para os horários novos — e só isso: o check-in que a equipe
// combinou às 16:00, o check-out antecipado à mão, o check-in que já
// aconteceu, o de ontem e o da reserva cancelada ficam onde estão.
// `meta.retimed_events` diz exatamente quantos mudaram; a partir daí os
// horários deixam de ser provisórios, valem para a reserva confirmada depois
// e para a duração padrão do evento novo; e gravar de novo não mexe em nada.
func TestAgendaQAGravarHorariosReprogramaOFuturoEPreservaOResto(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qagFaxina(t, admin)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	qagExigirProvisorio(t, a.qagHorariosDaCasa(t, admin.Token))
	tk := admin.Token
	p := f.produto(t, casa.Prop, "one_member", 1)
	hospede := f.contato(t, casa.Prop)
	d := casa.Hoje.AddDays(130)
	ontem := casa.Hoje.AddDays(-1)

	ajustadoCheckIn := a.qagVendida(t, tk, p.ID, hospede.ID, d, d.AddDays(2))
	ajustadoCheckOut := a.qagVendida(t, tk, p.ID, hospede.ID, d.AddDays(4), d.AddDays(6))
	jaEntrou := a.qagVendida(t, tk, p.ID, hospede.ID, d.AddDays(8), d.AddDays(10))
	deOntem := a.qagVendida(t, tk, p.ID, hospede.ID, d.AddDays(12), d.AddDays(14))
	cancelada := a.qagVendida(t, tk, p.ID, hospede.ID, d.AddDays(16), d.AddDays(18))
	deHoje := a.qagVendida(t, tk, p.ID, hospede.ID, casa.Hoje, casa.Hoje.AddDays(2))

	ev := func(r qagReserva) map[string]qagEvento { return qagPorTipo(t, a.qagDaReserva(t, tk, r.ID)) }
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+ev(ajustadoCheckIn)["checkin"].ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(d, "16:00"), "ends_at": casa.rfc(d, "16:30")}), http.StatusOK, "check-in combinado às 16:00")
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+ev(ajustadoCheckOut)["checkout"].ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(d.AddDays(6), "09:30"), "ends_at": casa.rfc(d.AddDays(6), "10:00")}), http.StatusOK, "check-out antecipado à mão")
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+jaEntrou.ID.String()+"/check-in", tk,
		map[string]any{"at": casa.rfc(d.AddDays(8), "14:05")}), http.StatusOK, "check-in registrado")
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+cancelada.ID.String()+"/cancel", tk, map[string]any{}),
		http.StatusOK, "cancelando")
	// O relógio andou: o check-in desta reserva foi ONTEM, no horário provisório.
	// A API não aceita marcar o passado da reserva; é o tempo que o empurra
	// para trás, como o `qmMoverBloqueio` faz com a manutenção.
	checkinDeOntem := ev(deOntem)["checkin"].ID
	if _, err := a.pool.Exec(a.ctx, `UPDATE agenda_events SET starts_at = $2, ends_at = $3 WHERE id = $1`,
		checkinDeOntem, casa.em(ontem, "14:00"), casa.em(ontem, "14:30")); err != nil {
		t.Fatalf("empurrando o check-in para ontem: %v", err)
	}

	antes := a.qagSnapshot(t, casa.Prop)
	novos := qagCorpoDosHorarios("15:00", "10:00", "10:00", "15:00", 60, 10)
	r := a.chamar(t, http.MethodPut, "/agenda/settings", tk, novos)
	exigirStatusQB(t, r, http.StatusOK, "PUT /agenda/settings")
	var gravado struct {
		Data qagHorarios `json:"data"`
		Meta struct {
			Reprogramados *int `json:"retimed_events"`
		} `json:"meta"`
	}
	r.decodificar(t, &gravado)
	if gravado.Data.Provisoria || gravado.Data.AtualizadaPor == nil || *gravado.Data.AtualizadaPor != admin.ID ||
		gravado.Data.AtualizadaPorNome == nil || gravado.Data.Entrada != "15:00" || gravado.Data.Passo != 60 {
		t.Errorf("depois de gravar: provisional %v, updated_by %v (%v), %+v", gravado.Data.Provisoria,
			gravado.Data.AtualizadaPor, gravado.Data.AtualizadaPorNome, gravado.Data)
	}
	depois := a.qagSnapshot(t, casa.Prop)
	mudaram := 0
	for id, janela := range depois {
		if antes[id] != janela {
			mudaram++
		}
	}
	if gravado.Meta.Reprogramados == nil {
		t.Fatalf("meta.retimed_events é obrigatório na resposta: %s", r.Corpo)
	}
	if *gravado.Meta.Reprogramados != mudaram {
		t.Errorf("meta.retimed_events = %d, mas %d eventos mudaram de janela no banco", *gravado.Meta.Reprogramados, mudaram)
	}

	type esperado struct {
		r          qagReserva
		tipo       string
		dia        calendar.Date
		de, ate    string
		porque     string
		reprograma bool
	}
	in, out := func(r qagReserva) calendar.Date { return calendar.MustParse(r.CheckIn) }, func(r qagReserva) calendar.Date { return calendar.MustParse(r.CheckOut) }
	casos := []esperado{
		{ajustadoCheckIn, "checkin", in(ajustadoCheckIn), "16:00", "16:30", "o combinado à mão fica", false},
		{ajustadoCheckIn, "checkout", out(ajustadoCheckIn), "09:00", "10:00", "o check-out vai para o horário novo", true},
		{ajustadoCheckIn, "limpeza", out(ajustadoCheckIn), "10:00", "15:00", "a limpeza vai para o horário novo", true},
		{ajustadoCheckOut, "checkin", in(ajustadoCheckOut), "15:00", "16:00", "o check-in vai para o horário novo (e o passo novo)", true},
		{ajustadoCheckOut, "checkout", out(ajustadoCheckOut), "09:30", "10:00", "o antecipado à mão fica", false},
		{ajustadoCheckOut, "limpeza", out(ajustadoCheckOut), "10:00", "15:00", "a limpeza vai para o horário novo", true},
		{jaEntrou, "checkin", in(jaEntrou), "14:00", "14:30", "o concluído fica", false},
		{jaEntrou, "checkout", out(jaEntrou), "09:00", "10:00", "o check-out ainda agendado muda", true},
		{deOntem, "checkin", ontem, "14:00", "14:30", "o de ontem fica", false},
		{deOntem, "checkout", out(deOntem), "09:00", "10:00", "o futuro da mesma reserva muda", true},
		{cancelada, "checkin", in(cancelada), "14:00", "14:30", "o cancelado fica", false},
		{cancelada, "limpeza", out(cancelada), "11:00", "14:00", "o cancelado fica", false},
		{deHoje, "checkin", casa.Hoje, "15:00", "16:00", "o de HOJE também muda", true},
		{deHoje, "limpeza", out(deHoje), "10:00", "15:00", "a limpeza vai para o horário novo", true},
	}
	for _, c := range casos {
		e := ev(c.r)[c.tipo]
		qagExigirJanela(t, e, casa.rfc(c.dia, c.de), casa.rfc(c.dia, c.ate), c.r.Codigo+" "+c.tipo+": "+c.porque)
	}

	// Os horários valem para o que nasce depois.
	nova := a.qagVendida(t, tk, p.ID, hospede.ID, d.AddDays(20), d.AddDays(22))
	novosEv := ev(nova)
	qagExigirJanela(t, novosEv["checkin"], casa.rfc(d.AddDays(20), "15:00"), casa.rfc(d.AddDays(20), "16:00"), "check-in da reserva nova")
	qagExigirJanela(t, novosEv["checkout"], casa.rfc(d.AddDays(22), "09:00"), casa.rfc(d.AddDays(22), "10:00"), "check-out da reserva nova")
	qagExigirJanela(t, novosEv["limpeza"], casa.rfc(d.AddDays(22), "10:00"), casa.rfc(d.AddDays(22), "15:00"), "limpeza da reserva nova")
	visita := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Reunião sem fim informado",
		"starts_at": casa.rfc(d.AddDays(30), "10:00")}).Data
	qagExigirJanela(t, visita, casa.rfc(d.AddDays(30), "10:00"), casa.rfc(d.AddDays(30), "11:00"), "ends_at ausente é um passo NOVO da grade")

	// O mesmo corpo de novo: nada mais a reprogramar.
	r = a.chamar(t, http.MethodPut, "/agenda/settings", tk, novos)
	exigirStatusQB(t, r, http.StatusOK, "PUT /agenda/settings de novo")
	r.decodificar(t, &gravado)
	if gravado.Meta.Reprogramados == nil || *gravado.Meta.Reprogramados != 0 {
		t.Errorf("a segunda gravação igual reprogramou %v", gravado.Meta.Reprogramados)
	}
}

// Em linguagem de negócio: a casa fecha aos domingos e, no sábado, vai até a
// meia-noite. Uma visita sábado às 21:30 (que em UTC já é domingo) está dentro
// do horário; domingo às 10:00 está fora. O dia da semana que vale é o da
// casa, e não o do servidor.
func TestAgendaQAFuncionamentoEhPeloDiaDaSemanaDaCasa(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	a.qagFaxina(t, admin)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	qagExigirProvisorio(t, a.qagHorariosDaCasa(t, admin.Token))

	horario := []map[string]any{}
	for dia := 1; dia <= 5; dia++ {
		horario = append(horario, map[string]any{"weekday": dia, "start": "08:00", "end": "20:00"})
	}
	horario = append(horario, map[string]any{"weekday": 6, "start": "08:00", "end": "24:00"})
	corpo := qagCorpoDosHorarios("14:00", "11:00", "11:00", "14:00", 30, 15)
	corpo["business_hours"] = horario
	exigirStatusQB(t, a.chamar(t, http.MethodPut, "/agenda/settings", admin.Token, corpo), http.StatusOK, "domingo fechado")

	sabado := casa.Hoje.AddDays(40)
	for sabado.Weekday() != time.Saturday {
		sabado = sabado.AddDays(1)
	}
	domingo := sabado.AddDays(1)
	casos := []struct {
		nome       string
		inicio     string
		fim        string
		querAvisos string
	}{
		{"sábado 21:30 da casa (domingo em UTC)", casa.rfc(sabado, "21:30"), casa.rfc(sabado, "22:00"), ""},
		{"sábado até a meia-noite da casa", casa.rfc(sabado, "23:00"), casa.rfc(domingo, "00:00"), ""},
		{"domingo 10:00 (fechado)", casa.rfc(domingo, "10:00"), casa.rfc(domingo, "10:30"), "fora_do_horario"},
		{"sábado atravessando a meia-noite", casa.rfc(sabado, "23:30"), casa.rfc(domingo, "00:30"), "fora_do_horario"},
	}
	for _, c := range casos {
		e := a.qagMarcar(t, admin.Token, map[string]any{"type": "evento", "title": c.nome, "starts_at": c.inicio, "ends_at": c.fim})
		if e.tipos() != c.querAvisos {
			t.Errorf("%s: avisos [%s], esperado [%s]", c.nome, e.tipos(), c.querAvisos)
		}
	}
	// E a resposta devolve o funcionamento como gravado, por dia e início.
	h := a.qagHorariosDaCasa(t, admin.Token)
	if len(h.Funcionamento) != 6 || h.Funcionamento[5].Dia != 6 || h.Funcionamento[5].Fim != "24:00" {
		t.Errorf("funcionamento lido: %+v", h.Funcionamento)
	}
	for _, iv := range h.Funcionamento {
		if iv.Dia == 7 {
			t.Errorf("domingo fechado não tem intervalo: %+v", h.Funcionamento)
		}
	}
}

// qagOffset devolve o instante e o offset ("-03:00") de uma string RFC 3339.
func qagOffset(t *testing.T, s string) (time.Time, string) {
	t.Helper()
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("instante fora de RFC 3339: %q (%v)", s, err)
	}
	if strings.HasSuffix(s, "Z") {
		return ts, "Z"
	}
	return ts, s[len(s)-6:]
}

// qagOffsetDaCasa é o offset da casa hoje, no formato da string.
func qagOffsetDaCasa(c qagCasa) string {
	return c.em(c.Hoje, "12:00").Format("-07:00")
}

// Em linguagem de negócio: a equipe tinha empurrado à mão a limpeza de uma
// saída para as 11:30. O dono então muda a saída da casa para as 12:00: o
// check-out (que estava no horário antigo) vai para 11:30–12:00 e passa a
// cruzar a limpeza ajustada. Gravar os horários não recusa nada — mas o
// conflito que a gravação criou tem de APARECER, dos dois lados, e não ficar
// escondido.
func TestAgendaQAReprogramarNaoEscondeOConflitoQueCria(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qagFaxina(t, admin)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	qagExigirProvisorio(t, a.qagHorariosDaCasa(t, admin.Token))
	tk := admin.Token
	p := f.produto(t, casa.Prop, "one_member", 1)
	d := casa.Hoje.AddDays(135)

	r := a.qagVendida(t, tk, p.ID, f.contato(t, casa.Prop).ID, d, d.AddDays(2))
	ev := qagPorTipo(t, a.qagDaReserva(t, tk, r.ID))
	saida := d.AddDays(2)
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+ev["limpeza"].ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(saida, "11:30"), "ends_at": casa.rfc(saida, "14:00")}), http.StatusOK, "limpeza empurrada para as 11:30")

	exigirStatusQB(t, a.chamar(t, http.MethodPut, "/agenda/settings", tk, qagCorpoDosHorarios("15:00", "12:00", "12:00", "15:00", 30, 15)),
		http.StatusOK, "saída da casa às 12:00")
	ev = qagPorTipo(t, a.qagDaReserva(t, tk, r.ID))
	qagExigirJanela(t, ev["checkout"], casa.rfc(saida, "11:30"), casa.rfc(saida, "12:00"), "o check-out vai para o horário novo")
	qagExigirJanela(t, ev["limpeza"], casa.rfc(saida, "11:30"), casa.rfc(saida, "14:00"), "a limpeza ajustada fica")
	if !ev["checkout"].Conflito || !ev["limpeza"].Conflito {
		t.Errorf("a gravação dos horários pôs o check-out em cima da limpeza: os dois mostram o conflito — check-out %v, limpeza %v",
			ev["checkout"].Conflito, ev["limpeza"].Conflito)
	}
}
