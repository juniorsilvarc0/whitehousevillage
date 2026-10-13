//go:build integration

package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// As corridas que o desenho da agenda diz ter fechado com trava, e não com
// SELECT antes do INSERT. Os nomes levam "Corrida" para entrarem na repetição
// de `make it-concorrencia`.

// qagChamar é o `chamar` seguro para goroutine: devolve o erro em vez de
// t.Fatal fora da goroutine do teste.
func (a *ambiente) qagChamar(metodo, caminho, token, chave string, corpo any) (resposta, error) {
	if metodo == http.MethodPost {
		return a.qmPost(caminho, token, chave, corpo)
	}
	return resposta{}, fmt.Errorf("método %s sem apoio", metodo)
}

func qagCodigo(r resposta) string {
	var env struct {
		Erro struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.Corpo, &env)
	return env.Erro.Code
}

// Em linguagem de negócio: no mesmo instante em que a gestão cancela a
// reserva, a equipe marca na agenda três eventos ligados a ela. Cada marcação
// ou nasce e é cancelada junto com a reserva, ou é recusada porque a reserva
// já caiu — nunca sobra evento AGENDADO pendurado numa reserva cancelada, e
// ninguém recebe 500.
func TestAgendaQACorridaEntreCancelarAReservaEMarcarNela(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	hospede := f.contato(t, casa.Prop)
	tk := u.Token

	for rodada := 0; rodada < 3; rodada++ {
		d := casa.Hoje.AddDays(260 + rodada*5)
		r := a.qagVendida(t, tk, p.ID, hospede.ID, d, d.AddDays(3))

		var (
			wg     sync.WaitGroup
			mu     sync.Mutex
			status = map[string]int{}
		)
		comeca := make(chan struct{})
		wg.Add(4)
		go func() {
			defer wg.Done()
			<-comeca
			resp, err := a.qagChamar(http.MethodPost, "/reservations/"+r.ID.String()+"/cancel", tk, "", map[string]any{"reason": "desistência"})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				status["cancel:erro"]++
				return
			}
			status[fmt.Sprintf("cancel:%d", resp.Status)]++
		}()
		for i := 0; i < 3; i++ {
			go func(i int) {
				defer wg.Done()
				<-comeca
				resp, err := a.qagChamar(http.MethodPost, "/agenda/events", tk, "", map[string]any{"type": "evento",
					"title": fmt.Sprintf("Ligado à reserva %d", i), "reservation_id": r.ID,
					"starts_at": casa.rfc(d.AddDays(1), fmt.Sprintf("%02d:00", 9+2*i)), "ends_at": casa.rfc(d.AddDays(1), fmt.Sprintf("%02d:30", 9+2*i))})
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					status["marcar:erro"]++
					return
				}
				status[fmt.Sprintf("marcar:%d:%s", resp.Status, qagCodigo(resp))]++
			}(i)
		}
		close(comeca)
		wg.Wait()

		if status["cancel:200"] != 1 {
			t.Fatalf("rodada %d: o cancelamento tem de passar: %v", rodada, status)
		}
		for k, v := range status {
			switch k {
			case "cancel:200", "marcar:201:", "marcar:422:VALIDATION_ERROR":
			default:
				t.Errorf("rodada %d: resposta fora do esperado %s (×%d) — %v", rodada, k, v, status)
			}
		}
		if pendurados := a.qagContar(t, `SELECT count(*) FROM agenda_events WHERE reservation_id = $1 AND status = 'agendado'`, r.ID); pendurados != 0 {
			t.Fatalf("rodada %d: %d eventos AGENDADOS pendurados na reserva cancelada (%v)", rodada, pendurados, status)
		}
		if criados := a.qagContar(t, `SELECT count(*) FROM agenda_events WHERE reservation_id = $1 AND origin = 'manual'`, r.ID); criados != status["marcar:201:"] {
			t.Errorf("rodada %d: %d manuais no banco, %d respostas 201", rodada, criados, status["marcar:201:"])
		}
	}
}

// Em linguagem de negócio: o dono confirma os horários novos no mesmo
// instante em que a gestão confirma o sinal de seis reservas. Ao final, a
// agenda das seis está TODA no horário novo — quem confirmou antes foi
// reprogramado pela gravação; quem confirmou depois já nasceu no novo. Nenhum
// check-in fica para trás no horário provisório, e ninguém recebe 500.
func TestAgendaQACorridaEntreConfirmarEGravarOsHorarios(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qagFaxina(t, admin)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	qagExigirProvisorio(t, a.qagHorariosDaCasa(t, admin.Token))
	p := f.produto(t, casa.Prop, "one_member", 1)
	hospede := f.contato(t, casa.Prop)
	tk := admin.Token

	const n = 6
	reservas := make([]qagReserva, n)
	chaves := make([]string, n)
	for i := range reservas {
		d := casa.Hoje.AddDays(300 + 4*i)
		reservas[i] = a.qagReservar(t, tk, p.ID, hospede.ID, d, d.AddDays(2))
		chaves[i] = a.chaveNova(t, "agenda-corrida")
	}
	novos := qagCorpoDosHorarios("15:00", "10:00", "10:00", "15:00", 30, 15)

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		status = map[string]int{}
	)
	comeca := make(chan struct{})
	wg.Add(n + 1)
	for i := range reservas {
		go func(i int) {
			defer wg.Done()
			<-comeca
			resp, err := a.qmPost("/reservations/"+reservas[i].ID.String()+"/confirm", tk, chaves[i],
				map[string]any{"deposit_paid_cents": reservas[i].Sinal})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				status["confirm:erro"]++
				return
			}
			status[fmt.Sprintf("confirm:%d", resp.Status)]++
		}(i)
	}
	go func() {
		defer wg.Done()
		<-comeca
		resp, err := a.qagPut("/agenda/settings", tk, novos)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			status["settings:erro"]++
			return
		}
		status[fmt.Sprintf("settings:%d", resp.Status)]++
	}()
	close(comeca)
	wg.Wait()

	if status["confirm:200"] != n || status["settings:200"] != 1 {
		t.Fatalf("todos passam: %v", status)
	}
	for _, r := range reservas {
		ev := qagPorTipo(t, a.qagDaReserva(t, tk, r.ID))
		de, ate := calendar.MustParse(r.CheckIn), calendar.MustParse(r.CheckOut)
		qagExigirJanela(t, ev["checkin"], casa.rfc(de, "15:00"), casa.rfc(de, "15:30"), r.Codigo+" check-in no horário novo")
		qagExigirJanela(t, ev["checkout"], casa.rfc(ate, "09:30"), casa.rfc(ate, "10:00"), r.Codigo+" check-out no horário novo")
		qagExigirJanela(t, ev["limpeza"], casa.rfc(ate, "10:00"), casa.rfc(ate, "15:00"), r.Codigo+" limpeza no horário novo")
	}
}

// qagPut e qagPatch são seguros para goroutine: devolvem o erro em vez de
// t.Fatal fora da goroutine do teste.
func (a *ambiente) qagPut(caminho, token string, corpo any) (resposta, error) {
	return a.qagFazer(http.MethodPut, caminho, token, corpo)
}

func (a *ambiente) qagPatch(caminho, token string, corpo any) (resposta, error) {
	return a.qagFazer(http.MethodPatch, caminho, token, corpo)
}

func (a *ambiente) qagFazer(metodo, caminho, token string, corpo any) (resposta, error) {
	bruto, err := json.Marshal(corpo)
	if err != nil {
		return resposta{}, err
	}
	req, err := http.NewRequestWithContext(a.ctx, metodo, a.servidor.URL+PrefixoDaAPI+caminho, bytes.NewReader(bruto))
	if err != nil {
		return resposta{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return resposta{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	lido, err := io.ReadAll(resp.Body)
	if err != nil {
		return resposta{}, err
	}
	return resposta{Status: resp.StatusCode, Corpo: lido, Headers: resp.Header}, nil
}

// Em linguagem de negócio: a gestão troca a reserva do AP-1 para o AP-2 no
// mesmo instante em que três pessoas marcam visitas no AP-2, no horário da
// faxina que a reserva tinha no AP-1. A troca nunca é recusada por causa da
// agenda; das visitas, no máximo uma entra; ninguém recebe 500; e o banco
// termina coerente: ou a faxina foi junto para o AP-2 e nenhuma visita entrou,
// ou uma visita entrou antes e a faxina seguiu a reserva sem unidade própria,
// em conflito à vista.
func TestAgendaQACorridaEntreRealocarEMarcarNaUnidadeDeDestino(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 2)
	tk := u.Token

	for rodada := 0; rodada < 4; rodada++ {
		d := casa.Hoje.AddDays(400 + rodada*5)
		r := a.qagVendida(t, tk, p.ID, f.contato(t, casa.Prop).ID, d, d.AddDays(3))
		origem := r.Unidades[0].ID
		destino := p.Unidades[0]
		if destino == origem {
			destino = p.Unidades[1]
		}
		meio := d.AddDays(1)
		faxina := a.qagMarcar(t, tk, map[string]any{"type": "limpeza", "title": "Faxina do meio", "reservation_id": r.ID,
			"unit_id": origem, "starts_at": casa.rfc(meio, "10:00"), "ends_at": casa.rfc(meio, "11:00")}).Data

		var (
			wg     sync.WaitGroup
			mu     sync.Mutex
			status = map[string]int{}
		)
		comeca := make(chan struct{})
		wg.Add(4)
		go func() {
			defer wg.Done()
			<-comeca
			resp, err := a.qmPost("/reservations/"+r.ID.String()+"/reassign-unit", tk, "", map[string]any{"to_unit_id": destino})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				status["realocar:erro"]++
				return
			}
			status[fmt.Sprintf("realocar:%d:%s", resp.Status, qagCodigo(resp))]++
		}()
		for i := 0; i < 3; i++ {
			go func(i int) {
				defer wg.Done()
				<-comeca
				resp, err := a.qmPost("/agenda/events", tk, "", map[string]any{"type": "visita", "title": fmt.Sprintf("Visita %d", i),
					"unit_id": destino, "starts_at": casa.rfc(meio, "10:00"), "ends_at": casa.rfc(meio, "11:00")})
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					status["visita:erro"]++
					return
				}
				status[fmt.Sprintf("visita:%d:%s", resp.Status, qagCodigo(resp))]++
			}(i)
		}
		close(comeca)
		wg.Wait()

		if status["realocar:200:"] != 1 {
			t.Fatalf("rodada %d: a agenda nunca trava a realocação: %v", rodada, status)
		}
		for k, v := range status {
			switch k {
			case "realocar:200:", "visita:201:", "visita:409:UNIT_NOT_AVAILABLE":
			default:
				t.Errorf("rodada %d: resposta fora do esperado %s (×%d) — %v", rodada, k, v, status)
			}
		}
		visitas := status["visita:201:"]
		t.Logf("rodada %d: %v", rodada, status)
		e := a.qagLer(t, tk, faxina.ID)
		switch {
		case visitas == 0 && e.UnidadeID != nil && *e.UnidadeID == destino && !e.Conflito:
		case visitas == 1 && e.UnidadeID == nil && e.Conflito && len(e.Unidades) == 1 && e.Unidades[0].ID == destino:
		default:
			t.Errorf("rodada %d: estado incoerente — %d visitas entraram; a faxina ficou com unit_id %v, units %+v, conflito %v (%v)",
				rodada, visitas, e.UnidadeID, e.Unidades, e.Conflito, status)
		}
		if n := a.qagContar(t, `
			SELECT count(*) FROM agenda_events x JOIN agenda_events y ON x.id < y.id AND x.unit_id = y.unit_id
			 WHERE x.unit_id = $1 AND x.status = 'agendado' AND y.status = 'agendado' AND x.origin = 'manual' AND y.origin = 'manual'
			   AND tstzrange(x.starts_at, x.ends_at, '[)') && tstzrange(y.starts_at, y.ends_at, '[)')`, destino); n != 0 {
			t.Fatalf("rodada %d: duas marcações à mão sobrepostas no AP de destino", rodada)
		}
	}
}

// Em linguagem de negócio: duas pessoas movem eventos diferentes para o mesmo
// horário da mesma unidade — e a de fora ainda não gravou quando a da API
// confere. Quem decide é o BANCO (constraint EXCLUDE), não a conferência: a
// remarcação que chega depois recebe 409 UNIT_NOT_AVAILABLE com o conflito
// relido, nunca 500, e o evento dela fica onde estava.
func TestAgendaQADisputaDaRemarcacaoDecididaPelaConstraint(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	unidade, codigo := f.unidade(t, casa.Prop)
	tk := u.Token
	d := casa.Hoje.AddDays(420)

	movida := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "A que vai ser movida", "unit_id": unidade,
		"starts_at": casa.rfc(d, "15:00"), "ends_at": casa.rfc(d, "16:00")}).Data

	tx, err := a.pool.Begin(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(a.ctx, `
		INSERT INTO agenda_events (property_id, type, origin, unit_id, title, starts_at, ends_at, created_by)
		VALUES ($1, 'visita', 'manual', $2, 'A de fora', $3, $4, $5)`,
		casa.Prop, unidade, casa.em(d, "10:00"), casa.em(d, "11:00"), u.ID); err != nil {
		t.Fatal(err)
	}

	resultado := make(chan resposta, 1)
	go func() {
		r, err := a.qagPatch("/agenda/events/"+movida.ID.String(), tk, map[string]any{
			"starts_at": casa.rfc(d, "10:30"), "ends_at": casa.rfc(d, "11:30")})
		if err != nil {
			r = resposta{Status: -1, Corpo: []byte(err.Error())}
		}
		resultado <- r
	}()
	esperando := func() bool {
		return a.qagContar(t, `SELECT count(*) FROM pg_stat_activity
			 WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%UPDATE agenda_events%'`) > 0
	}
	for i := 0; i < 500 && !esperando(); i++ {
		a.qagContar(t, `SELECT count(*) FROM pg_sleep(0.01)`)
	}
	if !esperando() {
		t.Log("o UPDATE da API não chegou a esperar a de fora: a recusa veio da conferência, não da constraint")
	}
	if err := tx.Commit(a.ctx); err != nil {
		t.Fatal(err)
	}
	r := <-resultado
	qbErro(t, r, http.StatusConflict, "UNIT_NOT_AVAILABLE", "remarcação para cima da de fora")
	cs := qagConflitos(t, r)
	if len(cs) != 1 || cs[0]["kind"] != "visita" || cs[0]["unit_code"] != codigo || cs[0]["starts_at"] != casa.rfc(d, "10:00") {
		t.Errorf("os conflitos são relidos depois do rollback: %v", cs)
	}
	if e := a.qagLer(t, tk, movida.ID); e.Inicio != movida.Inicio || e.Fim != movida.Fim {
		t.Fatalf("o 409 mudou o evento: %s–%s", e.Inicio, e.Fim)
	}
}
