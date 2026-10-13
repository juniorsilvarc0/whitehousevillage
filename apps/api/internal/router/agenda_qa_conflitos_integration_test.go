//go:build integration

package router

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// Conflito de UNIDADE bloqueia; de HORÁRIO avisa (spec §9, de propósito).

// qagSemIdentificar reprova o conflito que diz QUEM ou O QUÊ ocupa a unidade:
// o contrato promete a unidade, o tipo e a janela — "nunca id, título ou
// pessoa" —, porque o ator pode não ter escopo para ver o evento alheio.
func qagSemIdentificar(t *testing.T, c map[string]any, contexto string) {
	t.Helper()
	for k := range c {
		switch k {
		case "unit_id", "unit_code", "kind", "starts_at", "ends_at":
		default:
			t.Errorf("%s: details.conflicts traz %q, fora de ConflitoDeUnidade (unit_id, unit_code, kind, starts_at, ends_at): %v", contexto, k, c)
		}
	}
}

// Em linguagem de negócio: a operação não consegue marcar uma visita no
// apartamento com hóspede dentro, durante a limpeza, no check-in, na
// manutenção, no uso do proprietário ou em cima de outra visita — 409 com o
// que ocupa a unidade e quando, sem dizer quem, e nada é criado. Encostada
// (half-open) passa. O evento ligado à reserva não colide com a estadia dela.
func TestAgendaQAConflitoDeUnidadeRecusaENadaNasce(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	corretor := a.criarUsuario(t, "ag-corretor", a.qbPerfilDoSeed(t, "corretor"))
	f := a.qagFaxina(t, u, corretor)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	h := a.qagHorariosDaCasa(t, u.Token)
	p := f.produto(t, casa.Prop, "one_member", 1)
	unidade, codigo := p.Unidades[0], p.Codigos[0]
	hospede := f.contato(t, casa.Prop)
	tk := u.Token
	d := casa.Hoje.AddDays(150)

	r := a.qagVendida(t, tk, p.ID, hospede.ID, d, d.AddDays(3))
	ordem := qbDado[qmOrdem](t, a.chamar(t, http.MethodPost, "/maintenance-orders", tk, map[string]any{
		"unit_id": unidade, "title": "Troca do piso", "block": qmPeriodo(d.AddDays(10), d.AddDays(12)),
	}), http.StatusCreated, "POST /maintenance-orders")
	f.ordem(ordem.ID)
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/blocks", tk, map[string]any{
		"unit_ids": []uuid.UUID{unidade}, "from": d.AddDays(14).String(), "to": d.AddDays(15).String(), "source": "owner_hold",
		"note": "Uso do proprietário",
	}), http.StatusCreated, "POST /blocks")
	primeira := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "Primeira visita", "unit_id": unidade,
		"starts_at": casa.rfc(d.AddDays(20), "10:00"), "ends_at": casa.rfc(d.AddDays(20), "11:00")}).Data

	contar := func() int {
		return a.qagContar(t, `SELECT count(*) FROM agenda_events WHERE unit_id = $1`, unidade)
	}
	type conflito struct{ kind, de, ate string }
	casos := []struct {
		nome   string
		inicio string
		fim    string
		quer   []conflito
	}{
		{"com hóspede dentro", casa.rfc(d.AddDays(1), "10:00"), casa.rfc(d.AddDays(1), "11:00"),
			[]conflito{{"estadia", casa.rfc(d, h.Entrada), casa.rfc(d.AddDays(3), h.Saida)}}},
		{"durante a limpeza da saída", casa.rfc(d.AddDays(3), "12:00"), casa.rfc(d.AddDays(3), "12:30"),
			[]conflito{{"limpeza", casa.rfc(d.AddDays(3), h.LimpezaDe), casa.rfc(d.AddDays(3), h.LimpezaAte)}}},
		{"no check-in", casa.rfc(d, qagSomar(h.Entrada, 10)), casa.rfc(d, qagSomar(h.Entrada, 40)),
			[]conflito{{"checkin", casa.rfc(d, h.Entrada), casa.rfc(d, qagSomar(h.Entrada, h.Passo))},
				{"estadia", casa.rfc(d, h.Entrada), casa.rfc(d.AddDays(3), h.Saida)}}},
		{"na manutenção", casa.rfc(d.AddDays(11), "09:00"), casa.rfc(d.AddDays(11), "10:00"),
			[]conflito{{"manutencao", casa.rfc(d.AddDays(10), "00:00"), casa.rfc(d.AddDays(12), "00:00")}}},
		{"no uso do proprietário", casa.rfc(d.AddDays(14), "10:00"), casa.rfc(d.AddDays(14), "11:00"),
			[]conflito{{"bloqueio", casa.rfc(d.AddDays(14), "00:00"), casa.rfc(d.AddDays(15), "00:00")}}},
		{"em cima de outra visita", casa.rfc(d.AddDays(20), "10:30"), casa.rfc(d.AddDays(20), "11:30"),
			[]conflito{{"visita", primeira.Inicio, primeira.Fim}}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			antes := contar()
			resp := a.chamar(t, http.MethodPost, "/agenda/events", tk, map[string]any{"type": "visita", "title": "Visita " + c.nome,
				"unit_id": unidade, "starts_at": c.inicio, "ends_at": c.fim})
			qbErro(t, resp, http.StatusConflict, "UNIT_NOT_AVAILABLE", c.nome)
			cs := qagConflitos(t, resp)
			if len(cs) != len(c.quer) {
				t.Fatalf("%d conflitos, esperado %d: %v", len(cs), len(c.quer), cs)
			}
			// A ordem de details.conflicts não é contrato: confere como conjunto.
			porTipo := map[string]map[string]any{}
			for _, x := range cs {
				qagSemIdentificar(t, x, c.nome)
				porTipo[x["kind"].(string)] = x
			}
			for _, q := range c.quer {
				x, ok := porTipo[q.kind]
				if !ok || x["unit_id"] != unidade.String() || x["unit_code"] != codigo || x["starts_at"] != q.de || x["ends_at"] != q.ate {
					t.Errorf("conflito %s: %v; esperado %s → %s em %s", q.kind, x, q.de, q.ate, codigo)
				}
			}
			if depois := contar(); depois != antes {
				t.Fatalf("o 409 criou %d eventos", depois-antes)
			}
		})
	}

	// Encostada passa (half-open), e mover para cima da outra é 409 sem mudar.
	encostada := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "Encostada", "unit_id": unidade,
		"starts_at": casa.rfc(d.AddDays(20), "11:00"), "ends_at": casa.rfc(d.AddDays(20), "12:00")}).Data
	qbErro(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+encostada.ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(d.AddDays(20), "10:30")}), http.StatusConflict, "UNIT_NOT_AVAILABLE", "PATCH para cima da primeira")
	qbErro(t, a.chamar(t, http.MethodPut, "/agenda/events/"+encostada.ID.String(), tk, map[string]any{"type": "visita",
		"title": "Encostada", "unit_id": unidade, "starts_at": casa.rfc(d.AddDays(20), "10:30"), "ends_at": casa.rfc(d.AddDays(20), "11:30")}),
		http.StatusConflict, "UNIT_NOT_AVAILABLE", "PUT para cima da primeira")
	if e := a.qagLer(t, tk, encostada.ID); e.Inicio != encostada.Inicio || e.Fim != encostada.Fim {
		t.Fatalf("o 409 mudou a visita: %s–%s", e.Inicio, e.Fim)
	}

	// O evento ligado à reserva não colide com a estadia dela.
	faxina := a.qagMarcar(t, tk, map[string]any{"type": "limpeza", "title": "Faxina do meio da estadia", "reservation_id": r.ID,
		"unit_id": unidade, "starts_at": casa.rfc(d.AddDays(1), "10:00"), "ends_at": casa.rfc(d.AddDays(1), "11:00")}).Data
	if faxina.Conflito {
		t.Errorf("a faxina do meio da estadia não colide com a estadia da própria reserva")
	}

	// O corretor que bate na visita da operação recebe o 409 sem saber de quem.
	resp := a.chamar(t, http.MethodPost, "/agenda/events", corretor.Token, map[string]any{"type": "visita", "title": "Visita do corretor",
		"unit_id": unidade, "starts_at": casa.rfc(d.AddDays(20), "10:15"), "ends_at": casa.rfc(d.AddDays(20), "10:45")})
	qbErro(t, resp, http.StatusConflict, "UNIT_NOT_AVAILABLE", "corretor em cima da visita da operação")
	for _, c := range qagConflitos(t, resp) {
		qagSemIdentificar(t, c, "409 do corretor")
	}
}

// Em linguagem de negócio: conflito de HORÁRIO nunca recusa — a operação sabe
// o que faz com o próprio tempo. A visita fora do expediente, a que cai no
// treinamento da equipe e a do responsável sem a folga são criadas, e a
// resposta diz por que merecem atenção. Sem nada a avisar, `warnings` é `[]`.
func TestAgendaQAConflitoDeHorarioAvisaECria(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	h := a.qagHorariosDaCasa(t, u.Token)
	tk := u.Token
	d := casa.Hoje.AddDays(160)

	limpo := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Reunião no expediente",
		"starts_at": casa.rfc(d, "09:00"), "ends_at": casa.rfc(d, "09:30")})
	if limpo.Meta.Avisos == nil || len(limpo.Meta.Avisos) != 0 {
		t.Errorf("sem aviso, meta.warnings é [] (não null, não ausente): %+v", limpo.Meta.Avisos)
	}

	fora := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "Visita à noite",
		"starts_at": casa.rfc(d, "21:00"), "ends_at": casa.rfc(d, "21:30")})
	if fora.tipos() != "fora_do_horario" || fora.Meta.Avisos[0].RefID != nil || fora.Meta.Avisos[0].Mensagem == "" {
		t.Errorf("fora do horário: %+v", fora.Meta.Avisos)
	}

	bl := qbDado[qagBloqueio](t, a.chamar(t, http.MethodPost, "/agenda/blocks", tk, map[string]any{
		"starts_at": casa.rfc(d, "14:00"), "ends_at": casa.rfc(d, "16:00"), "reason": "Equipe em treinamento",
	}), http.StatusCreated, "POST /agenda/blocks")
	noBloqueio := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Na hora do treinamento",
		"starts_at": casa.rfc(d, "15:00"), "ends_at": casa.rfc(d, "15:30")})
	if noBloqueio.tipos() != "bloqueio_de_horario" {
		t.Fatalf("no bloqueio de horário: %+v", noBloqueio.Meta.Avisos)
	}
	if av := noBloqueio.Meta.Avisos[0]; av.RefID == nil || *av.RefID != bl.ID || av.Inicio == nil || *av.Inicio != casa.rfc(d, "14:00") ||
		av.Fim == nil || *av.Fim != casa.rfc(d, "16:00") {
		t.Errorf("o aviso aponta o bloqueio e a janela dele, no offset da casa: %+v", av)
	}

	primeira := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "Visita das 10", "assignee_id": u.ID,
		"starts_at": casa.rfc(d, "10:00"), "ends_at": casa.rfc(d, "11:00")}).Data
	semFolga := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "Visita sem folga", "assignee_id": u.ID,
		"starts_at": casa.rfc(d, qagSomar("11:00", h.Folga-5)), "ends_at": casa.rfc(d, "12:00")})
	if semFolga.tipos() != "responsavel_ocupado" || semFolga.Meta.Avisos[0].RefID == nil || *semFolga.Meta.Avisos[0].RefID != primeira.ID {
		t.Errorf("responsável sem a folga: %+v", semFolga.Meta.Avisos)
	}
	comFolga := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "Visita com a folga", "assignee_id": u.ID,
		"starts_at": casa.rfc(d.AddDays(1), qagSomar("11:00", h.Folga)), "ends_at": casa.rfc(d.AddDays(1), "12:00")})
	if len(comFolga.Meta.Avisos) != 0 {
		t.Errorf("em outro dia, sem compromisso perto: %+v", comFolga.Meta.Avisos)
	}

	// Os três de uma vez — e mesmo assim criado.
	noite := d.AddDays(2)
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/agenda/blocks", tk, map[string]any{
		"starts_at": casa.rfc(noite, "20:30"), "ends_at": casa.rfc(noite, "22:00"), "reason": "Dedetização",
	}), http.StatusCreated, "bloqueio da noite")
	a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Jantar", "assignee_id": u.ID,
		"starts_at": casa.rfc(noite, "20:00"), "ends_at": casa.rfc(noite, "20:30")})
	tres := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Tudo junto", "assignee_id": u.ID,
		"starts_at": casa.rfc(noite, "20:35"), "ends_at": casa.rfc(noite, "21:05")})
	if tres.tipos() != "bloqueio_de_horario,fora_do_horario,responsavel_ocupado" {
		t.Errorf("os três avisos juntos: [%s]", tres.tipos())
	}

	// Dia inteiro não tem hora: não recebe fora_do_horario (mas o bloqueio, sim).
	diaInteiro := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Feira de turismo", "all_day": true,
		"starts_at": casa.rfc(d, "00:00"), "ends_at": casa.rfc(d.AddDays(1), "00:00")})
	if diaInteiro.tipos() != "bloqueio_de_horario" {
		t.Errorf("dia inteiro: [%s]", diaInteiro.tipos())
	}

	// PATCH e PUT que mudam a janela avisam; o que só anota, não.
	r := a.chamar(t, http.MethodPatch, "/agenda/events/"+limpo.Data.ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(d, "15:30"), "ends_at": casa.rfc(d, "16:30")})
	exigirStatusQB(t, r, http.StatusOK, "PATCH para dentro do treinamento")
	if e := qagEscritaDe(t, r); e.tipos() != "bloqueio_de_horario" {
		t.Errorf("PATCH: [%s]", e.tipos())
	}
	r = a.chamar(t, http.MethodPatch, "/agenda/events/"+limpo.Data.ID.String(), tk, map[string]any{"notes": "levar contrato"})
	exigirStatusQB(t, r, http.StatusOK, "PATCH só da anotação")
	if e := qagEscritaDe(t, r); e.Meta.Avisos == nil || len(e.Meta.Avisos) != 0 {
		t.Errorf("anotar não muda janela, não avisa (e warnings é []): %+v", e.Meta.Avisos)
	}
	r = a.chamar(t, http.MethodPut, "/agenda/events/"+fora.Data.ID.String(), tk, map[string]any{"type": "visita", "title": "Visita à noite",
		"starts_at": casa.rfc(d, "22:00"), "ends_at": casa.rfc(d, "22:30")})
	exigirStatusQB(t, r, http.StatusOK, "PUT continuando fora do horário")
	if e := qagEscritaDe(t, r); e.tipos() != "fora_do_horario" {
		t.Errorf("PUT: [%s]", e.tipos())
	}
}

// Em linguagem de negócio: a agenda MOSTRA a manutenção e o uso do
// proprietário lidos do calendário (sem cópia), o dia inteiro e sem botões — e
// o que já estava marcado quando a manutenção ou a venda chegou aparece em
// conflito, dos dois lados, em vez de sumir.
func TestAgendaQAListaMostraDerivadosEConflitosQueChegaramDepois(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	tk := u.Token
	p := f.produto(t, casa.Prop, "one_member", 1)
	unidade := p.Unidades[0]
	hospede := f.contato(t, casa.Prop)
	d := casa.Hoje.AddDays(170)

	visitaAntes := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "Visita marcada antes da manutenção", "unit_id": unidade,
		"starts_at": casa.rfc(d.AddDays(1), "10:00"), "ends_at": casa.rfc(d.AddDays(1), "11:00")}).Data
	ordem := qbDado[qmOrdem](t, a.chamar(t, http.MethodPost, "/maintenance-orders", tk, map[string]any{
		"unit_id": unidade, "title": "Troca do piso", "block": qmPeriodo(d, d.AddDays(2)),
	}), http.StatusCreated, "manutenção que chegou depois da visita")
	f.ordem(ordem.ID)
	if ordem.Bloqueio == nil {
		t.Fatalf("a ordem nasceu sem bloqueio: %+v", ordem)
	}
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/blocks", tk, map[string]any{
		"unit_ids": []uuid.UUID{unidade}, "from": d.AddDays(3).String(), "to": d.AddDays(4).String(), "source": "owner_hold",
		"note": "Uso do proprietário",
	}), http.StatusCreated, "POST /blocks com nota")
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/blocks", tk, map[string]any{
		"unit_ids": []uuid.UUID{unidade}, "from": d.AddDays(4).String(), "to": d.AddDays(5).String(), "source": "owner_hold",
	}), http.StatusCreated, "POST /blocks sem nota")

	lista := a.qagJanela(t, tk, d.AddDays(-1), d.AddDays(6), "unit_id="+unidade.String())
	var manut, comNota, semNota *qagEvento
	for i, e := range lista {
		switch {
		case e.Tipo == "manutencao":
			manut = &lista[i]
		case e.Tipo == "bloqueio" && e.Data == d.AddDays(3).String():
			comNota = &lista[i]
		case e.Tipo == "bloqueio" && e.Data == d.AddDays(4).String():
			semNota = &lista[i]
		}
	}
	if manut == nil || comNota == nil || semNota == nil {
		t.Fatalf("a lista traz a manutenção e os dois bloqueios lidos de stay_blocks: %+v", lista)
	}
	if manut.Origem != "maintenance" || manut.Titulo != "Troca do piso" || !manut.DiaInteiro || manut.Status != "agendado" ||
		manut.Editavel != "nada" || len(manut.Acoes) != 0 || manut.Conflito || manut.ID != ordem.Bloqueio.ID ||
		manut.BloqueioID == nil || *manut.BloqueioID != ordem.Bloqueio.ID || manut.OrdemID == nil || *manut.OrdemID != ordem.ID ||
		manut.CriadoEm != nil || manut.AtualizadoEm != nil || manut.CriadoPor != nil {
		t.Errorf("manutenção derivada fora do contrato: %+v", *manut)
	}
	qagExigirJanela(t, *manut, casa.rfc(d, "00:00"), casa.rfc(d.AddDays(2), "00:00"), "manutenção: da meia-noite da casa do primeiro dia à do dia seguinte ao último")
	if manut.Data != d.String() || len(manut.Unidades) != 1 || manut.Unidades[0].ID != unidade {
		t.Errorf("manutenção: date %s, units %+v", manut.Data, manut.Unidades)
	}
	if comNota.Origem != "stay_block" || comNota.Titulo != "Uso do proprietário" || comNota.OrdemID != nil || comNota.Editavel != "nada" {
		t.Errorf("bloqueio com nota: %+v", *comNota)
	}
	if semNota.Titulo != "Bloqueio" {
		t.Errorf("bloqueio sem nota se chama \"Bloqueio\": %q", semNota.Titulo)
	}
	qbErro(t, a.chamar(t, http.MethodGet, "/agenda/events/"+manut.ID.String(), tk, nil), http.StatusNotFound, "NOT_FOUND",
		"o detalhe não serve o derivado")
	qbErro(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+manut.ID.String(), tk, map[string]any{"notes": "x"}), http.StatusNotFound,
		"NOT_FOUND", "o derivado não se edita pela agenda")

	if v, ok := qagAchar(lista, visitaAntes.ID); !ok || !v.Conflito {
		t.Errorf("a visita marcada antes da manutenção aparece em conflito: %+v", v)
	}

	// Os filtros alcançam (ou não) o derivado como o contrato diz.
	janela := "&from=" + d.AddDays(-1).String() + "&to=" + d.AddDays(6).String() + "&unit_id=" + unidade.String()
	for filtro, tipos := range map[string]string{
		"type=manutencao":              "manutencao",
		"type=bloqueio":                "bloqueio,bloqueio",
		"status=cancelado":             "",
		"unassigned=false":             "",
		"assignee_id=" + u.ID.String(): "",
	} {
		evs, _ := qbLista[qagEvento](t, a.chamar(t, http.MethodGet, "/agenda/events?per_page=100&"+filtro+janela, tk, nil), filtro)
		got := []string{}
		for _, e := range evs {
			if e.Origem == "maintenance" || e.Origem == "stay_block" {
				got = append(got, e.Tipo)
			}
		}
		if s := joinSorted(got); s != tipos {
			t.Errorf("filtro %s: derivados [%s], esperado [%s]", filtro, s, tipos)
		}
	}

	// A venda que chegou depois da visita: os dois lados aparecem em conflito.
	dia := d.AddDays(10)
	h := a.qagHorariosDaCasa(t, tk)
	visitaDaTarde := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "Visita na hora do check-in", "unit_id": unidade,
		"starts_at": casa.rfc(dia, h.Entrada), "ends_at": casa.rfc(dia, qagSomar(h.Entrada, 60))}).Data
	r := a.qagVendida(t, tk, p.ID, hospede.ID, dia, dia.AddDays(2))
	if v := a.qagLer(t, tk, visitaDaTarde.ID); !v.Conflito || v.Status != "agendado" {
		t.Errorf("a visita que a venda atropelou continua agendada e em conflito: %+v", v)
	}
	if ci := qagPorTipo(t, a.qagDaReserva(t, tk, r.ID))["checkin"]; !ci.Conflito {
		t.Errorf("o check-in da venda nova também mostra o conflito com a visita: %+v", ci)
	}
}

// A disputa que a constraint decide, pela porta da frente: oito pedidos
// simultâneos da MESMA visita na MESMA unidade e hora. Exatamente um vence,
// os outros recebem 409 UNIT_NOT_AVAILABLE, nenhum 500 — e o banco termina sem
// duas marcações agendadas sobrepostas na unidade.
func TestAgendaQADisputaDeVisitasPelaPortaDaFrente(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	unidade, _ := f.unidade(t, casa.Prop)
	d := casa.Hoje.AddDays(180)

	const n = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		status  = map[int]int{}
		codigos = map[string]int{}
	)
	comeca := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-comeca
			r, err := a.qmPost("/agenda/events", u.Token, "", map[string]any{"type": "visita",
				"title": "Visita disputada", "unit_id": unidade,
				"starts_at": casa.rfc(d, "10:00"), "ends_at": casa.rfc(d, "11:00")})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				status[-1]++
				return
			}
			status[r.Status]++
			if r.Status != http.StatusCreated {
				var env struct {
					Erro struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				_ = json.Unmarshal(r.Corpo, &env)
				codigos[env.Erro.Code]++
			}
		}(i)
	}
	close(comeca)
	wg.Wait()

	if status[http.StatusCreated] != 1 || status[http.StatusConflict] != n-1 || codigos["UNIT_NOT_AVAILABLE"] != n-1 {
		t.Fatalf("exatamente um vence e os outros recebem 409 UNIT_NOT_AVAILABLE: status %v, codes %v", status, codigos)
	}
	if sobrepostos := a.qagContar(t, `
		SELECT count(*) FROM agenda_events x JOIN agenda_events y ON x.id < y.id AND x.unit_id = y.unit_id
		 WHERE x.unit_id = $1 AND x.status = 'agendado' AND y.status = 'agendado'
		   AND x.origin = 'manual' AND y.origin = 'manual'
		   AND tstzrange(x.starts_at, x.ends_at, '[)') && tstzrange(y.starts_at, y.ends_at, '[)')`, unidade); sobrepostos != 0 {
		t.Fatalf("o banco terminou com %d pares de marcações sobrepostas na unidade", sobrepostos)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM agenda_events WHERE unit_id = $1`, unidade); n != 1 {
		t.Fatalf("%d eventos na unidade, esperado 1", n)
	}
}

func joinSorted(xs []string) string {
	s := sortedCopy(xs)
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}
