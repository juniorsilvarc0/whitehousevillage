//go:build integration

package router

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// O fuso (contrato: "toda data-hora de resposta sai no offset da casa, e o dia
// de cada evento vem pronto (`date`)"). Os casos ficam onde UTC e a casa
// discordam do DIA: entre 21:00 e 23:59 da casa, já é amanhã em UTC.

// qagOffsetsDaResposta percorre o corpo e devolve toda data-hora que NÃO está
// no offset da casa — em qualquer nível e em qualquer chave.
func qagOffsetsDaResposta(t *testing.T, corpo []byte, offset string) []string {
	t.Helper()
	var v any
	if err := json.Unmarshal(corpo, &v); err != nil {
		t.Fatalf("resposta não é JSON: %v", err)
	}
	var fora []string
	var andar func(chave string, v any)
	andar = func(chave string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, f := range x {
				andar(k, f)
			}
		case []any:
			for _, f := range x {
				andar(chave, f)
			}
		case string:
			if _, err := time.Parse(time.RFC3339Nano, x); err == nil && !strings.HasSuffix(x, offset) {
				fora = append(fora, chave+"="+x)
			}
		}
	}
	andar("", v)
	return fora
}

// Em linguagem de negócio: a visita das 22:00 de hoje (em Fortaleza) é de
// HOJE — mesmo que o servidor e o banco pensem em UTC, onde já é amanhã. Ela
// aparece no dia de hoje da agenda, não no de amanhã; pode ser dada por feita
// hoje; e o instante mandado em UTC volta na hora de parede da casa.
func TestAgendaQAEventoDasVinteEDuasCaiNoDiaDaCasa(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	off := qagOffsetDaCasa(casa)
	tk := u.Token
	hoje := casa.Hoje
	d := casa.Hoje.AddDays(190)

	// Hoje às 22:00 da casa: o dia é hoje, e concluir é permitido (não é
	// "amanhã" para quem pensa em UTC).
	criada := a.chamar(t, http.MethodPost, "/agenda/events", tk, map[string]any{"type": "evento", "title": "Reunião das 22h",
		"starts_at": casa.rfc(hoje, "22:00"), "ends_at": casa.rfc(hoje, "22:30")})
	noite := qagEscritaDe(t, criada).Data
	if criada.Status != http.StatusCreated || noite.Data != hoje.String() || noite.Inicio != casa.rfc(hoje, "22:00") {
		t.Fatalf("22:00 de hoje na casa: status %d, date %s, starts_at %s", criada.Status, noite.Data, noite.Inicio)
	}
	if noite.acoes() != "complete,cancel" {
		t.Errorf("o evento de hoje às 22:00 aceita concluir hoje: ações [%s]", noite.acoes())
	}
	feita := a.chamar(t, http.MethodPost, "/agenda/events/"+noite.ID.String()+"/complete", tk, nil)
	if feita.Status != http.StatusOK {
		t.Errorf("concluir o evento de hoje às 22:00 da casa: %d — %s", feita.Status, feita.Corpo)
	}
	if fora := qagOffsetsDaResposta(t, feita.Corpo, off); len(fora) > 0 {
		t.Errorf("data-hora fora do offset da casa no concluído: %v", fora)
	}

	// O mesmo instante mandado em UTC volta na hora da casa, e o dia é o da casa.
	emUTC := casa.em(d, "22:00").UTC().Format(time.RFC3339)
	if !strings.HasSuffix(emUTC, "Z") || strings.HasPrefix(emUTC, d.String()) {
		t.Fatalf("premissa: 22:00 da casa já é outro dia em UTC (%s)", emUTC)
	}
	r := a.chamar(t, http.MethodPost, "/agenda/events", tk, map[string]any{"type": "evento", "title": "Mandada em UTC",
		"starts_at": emUTC, "ends_at": casa.em(d, "22:30").UTC().Format(time.RFC3339)})
	exigirStatusQB(t, r, http.StatusCreated, "POST em UTC")
	utc := qagEscritaDe(t, r).Data
	if utc.Inicio != casa.rfc(d, "22:00") || utc.Fim != casa.rfc(d, "22:30") || utc.Data != d.String() {
		t.Errorf("o instante em UTC volta na hora de parede da casa: %s–%s, date %s", utc.Inicio, utc.Fim, utc.Data)
	}
	if fora := qagOffsetsDaResposta(t, r.Corpo, off); len(fora) > 0 {
		t.Errorf("data-hora fora do offset da casa no POST: %v", fora)
	}

	// Os vizinhos da meia-noite.
	antesDaMeiaNoite := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Véspera às 23:45",
		"starts_at": casa.rfc(d.AddDays(-1), "23:45"), "ends_at": casa.rfc(d.AddDays(-1), "23:59")}).Data
	cruza := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Atravessa a meia-noite",
		"starts_at": casa.rfc(d, "23:30"), "ends_at": casa.rfc(d.AddDays(1), "00:30")}).Data
	if cruza.Data != d.String() {
		t.Errorf("o dia do evento é o dia da casa em que ele começa: %s", cruza.Data)
	}
	ids := func(evs []qagEvento) map[uuid.UUID]bool {
		out := map[uuid.UUID]bool{}
		for _, e := range evs {
			out[e.ID] = true
		}
		return out
	}
	todos := ids(a.qagJanela(t, tk, d, d.AddDays(1), ""))
	seguinte := ids(a.qagJanela(t, tk, d.AddDays(1), d.AddDays(2), ""))
	vespera := ids(a.qagJanela(t, tk, d.AddDays(-1), d, ""))
	for nome, c := range map[string]struct {
		id                     uuid.UUID
		vespera, dia, seguinte bool
	}{
		"22:00 do dia":              {utc.ID, false, true, false},
		"23:45 da véspera":          {antesDaMeiaNoite.ID, true, false, false},
		"23:30 → 00:30 do seguinte": {cruza.ID, false, true, true},
	} {
		if vespera[c.id] != c.vespera || todos[c.id] != c.dia || seguinte[c.id] != c.seguinte {
			t.Errorf("%s: na véspera %v, no dia %v, no seguinte %v; esperado %v/%v/%v (dias da casa, half-open)",
				nome, vespera[c.id], todos[c.id], seguinte[c.id], c.vespera, c.dia, c.seguinte)
		}
	}

	// Dia inteiro é à meia-noite DA CASA — a meia-noite UTC é 21:00 da véspera.
	inteiro := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Dia inteiro", "all_day": true,
		"starts_at": casa.rfc(d.AddDays(5), "00:00"), "ends_at": casa.rfc(d.AddDays(6), "00:00")}).Data
	if inteiro.Data != d.AddDays(5).String() {
		t.Errorf("dia inteiro: date %s", inteiro.Data)
	}
	e := qbErro(t, a.chamar(t, http.MethodPost, "/agenda/events", tk, map[string]any{"type": "evento", "title": "Meia-noite UTC",
		"all_day": true, "starts_at": d.AddDays(7).String() + "T00:00:00Z", "ends_at": d.AddDays(8).String() + "T00:00:00Z"}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "dia inteiro na meia-noite UTC")
	if e.Details["starts_at"] == nil {
		t.Errorf("o 422 do dia inteiro fora da meia-noite da casa nomeia starts_at: %v", e.Details)
	}
	meiaNoiteDaCasaEmUTC := casa.em(d.AddDays(9), "00:00").UTC().Format(time.RFC3339)
	r = a.chamar(t, http.MethodPost, "/agenda/events", tk, map[string]any{"type": "evento", "title": "Meia-noite da casa escrita em UTC",
		"all_day": true, "starts_at": meiaNoiteDaCasaEmUTC, "ends_at": casa.em(d.AddDays(10), "00:00").UTC().Format(time.RFC3339)})
	exigirStatusQB(t, r, http.StatusCreated, "dia inteiro com a meia-noite da casa escrita em UTC")
	if x := qagEscritaDe(t, r).Data; x.Inicio != casa.rfc(d.AddDays(9), "00:00") || x.Data != d.AddDays(9).String() {
		t.Errorf("dia inteiro escrito em UTC volta na meia-noite da casa: %s, date %s", x.Inicio, x.Data)
	}

	// Toda data-hora da lista, do bloqueio e da configuração sai no offset da casa.
	lista := a.chamar(t, http.MethodGet, "/agenda/events?per_page=100&status=agendado,concluido,cancelado&from="+hoje.String()+"&to="+hoje.AddDays(1).String(), tk, nil)
	exigirStatusQB(t, lista, http.StatusOK, "lista de hoje")
	if fora := qagOffsetsDaResposta(t, lista.Corpo, off); len(fora) > 0 {
		t.Errorf("data-hora fora do offset da casa na lista: %v", fora)
	}
	bl := a.chamar(t, http.MethodPost, "/agenda/blocks", tk, map[string]any{"reason": "Treinamento noturno",
		"starts_at": casa.em(d, "21:00").UTC().Format(time.RFC3339), "ends_at": casa.em(d, "23:00").UTC().Format(time.RFC3339)})
	exigirStatusQB(t, bl, http.StatusCreated, "bloqueio mandado em UTC")
	if fora := qagOffsetsDaResposta(t, bl.Corpo, off); len(fora) > 0 {
		t.Errorf("data-hora fora do offset da casa no bloqueio: %v", fora)
	}
	b := qbDado[qagBloqueio](t, bl, http.StatusCreated, "bloqueio")
	if b.Inicio != casa.rfc(d, "21:00") {
		t.Errorf("o bloqueio volta na hora da casa: %s", b.Inicio)
	}
	// O bloqueio das 21:00 às 23:00 é do dia d: a lista do dia o traz, a do dia seguinte não.
	tem := func(de, ate string) bool {
		bs, _ := qbLista[qagBloqueio](t, a.chamar(t, http.MethodGet, "/agenda/blocks?per_page=100&from="+de+"&to="+ate, tk, nil), "bloqueios")
		for _, x := range bs {
			if x.ID == b.ID {
				return true
			}
		}
		return false
	}
	if !tem(d.String(), d.AddDays(1).String()) || tem(d.AddDays(1).String(), d.AddDays(2).String()) {
		t.Errorf("o bloqueio das 21:00–23:00 da casa é do dia da casa, e não do dia seguinte em UTC")
	}
}
