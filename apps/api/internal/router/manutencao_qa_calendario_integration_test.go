//go:build integration

package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// ─────────────────────────── 2. O calendário de verdade ─────────────────────
//
// A ordem bloqueia pela MESMA tabela e pela MESMA constraint da venda. Os
// testes do módulo provam bloqueio contra bloqueio; aqui a ordem encontra a
// RESERVA, o mapa de ocupação e a disponibilidade de venda — a fronteira entre
// módulos, que nenhum teste de módulo atravessa.

// Em linguagem de negócio: a casa vendeu o AP de 15 a 18 e alguém tenta
// marcar a pintura de 16 a 19. A ordem é recusada com "data ocupada" e NADA
// nasce — nem a ordem sem bloqueio, nem bloqueio solto. O mesmo vale para a
// pré-reserva (hold) que ainda não pagou. E colado (pintura começando no dia
// do check-out) passa, porque a estadia é half-open.
func TestManutencaoQAOrdemSobreReservaRecusadaSemDeixarNada(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	admin := a.criarUsuario(t, "manut-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qbFaxina(t, admin)
	m := a.qmFaxina(t, f)
	prop := a.qbPropriedadePadrao(t)
	d := a.qmHoje(t, prop)
	p := m.qmProdutoVendavel(t, prop)
	hospede := m.qmHospede(t, prop)
	tk := admin.Token

	vendida := a.qmVendida(t, tk, p.ID, hospede, d.AddDays(5), d.AddDays(8))
	var statusDaVenda string
	if err := a.pool.QueryRow(a.ctx, `SELECT status FROM reservations WHERE id = $1`, vendida.ID).Scan(&statusDaVenda); err != nil || statusDaVenda != "confirmed" {
		t.Fatalf("premissa: a reserva está confirmada (%s %v)", statusDaVenda, err)
	}
	segurada := qbDado[reservaQA](t, a.qmReservar(t, tk, p.ID, hospede, d.AddDays(20), d.AddDays(22)), http.StatusCreated, "pré-reserva")
	if segurada.Status != "hold" {
		t.Fatalf("premissa: a pré-reserva nasce hold, veio %s", segurada.Status)
	}

	for _, c := range []struct {
		nome     string
		de, ate  calendar.Date
		contexto string
	}{
		{"sobre a reserva confirmada", d.AddDays(6), d.AddDays(9), "ordem sobre reserva confirmada"},
		{"cobrindo a reserva inteira", d.AddDays(4), d.AddDays(9), "ordem que engloba a reserva"},
		{"sobre a pré-reserva", d.AddDays(21), d.AddDays(23), "ordem sobre pré-reserva"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			r := a.chamar(t, http.MethodPost, "/maintenance-orders", tk, map[string]any{
				"unit_id": p.Unidade, "title": "Pintura " + c.nome, "block": qmPeriodo(c.de, c.ate),
			})
			e := qbErro(t, r, http.StatusConflict, "DATE_CONFLICT", c.contexto)
			if e.Details["unit_code"] != p.Codigo {
				t.Errorf("details.unit_code = %v, esperado %s", e.Details["unit_code"], p.Codigo)
			}
			if pedido := fmt.Sprintf("[%s, %s)", c.de, c.ate); e.Details["period"] != pedido {
				t.Errorf("details.period = %v; o contrato manda o período PEDIDO, %s", e.Details["period"], pedido)
			}
		})
	}
	if n := a.qmContar(t, `SELECT count(*) FROM maintenance_orders WHERE unit_id = $1`, p.Unidade); n != 0 {
		t.Fatalf("a ordem recusada por data ocupada nasceu mesmo assim (%d ordem(ns)): o contrato diz que nada é criado", n)
	}
	if n := a.qmContar(t, `SELECT count(*) FROM stay_blocks WHERE unit_id = $1 AND source = 'maintenance'`, p.Unidade); n != 0 {
		t.Fatalf("sobrou bloqueio de manutenção da ordem recusada: %d", n)
	}

	// Colado na saída da reserva confirmada: passa (half-open).
	colada := a.qmAbrir(t, tk, map[string]any{"unit_id": p.Unidade, "title": "Pintura colada", "block": qmPeriodo(d.AddDays(8), d.AddDays(10))})
	if colada.Bloqueio == nil || colada.Bloqueio.Status != "confirmed" {
		t.Fatalf("o bloqueio colado no check-out nasce: %+v", colada.Bloqueio)
	}
	// Estender para trás, por cima da última noite vendida: 409 e nada muda.
	r := a.chamar(t, http.MethodPut, "/maintenance-orders/"+colada.ID.String()+"/block", tk, qmPeriodo(d.AddDays(7), d.AddDays(10)))
	qbErro(t, r, http.StatusConflict, "DATE_CONFLICT", "estender o bloqueio sobre a reserva")
	if b := a.qmLer(t, tk, colada.ID).Bloqueio; b == nil || b.De != d.AddDays(8).String() || b.Ate != d.AddDays(10).String() {
		t.Fatalf("o conflito não pode mexer no bloqueio: %+v", b)
	}
}

// Em linguagem de negócio: quem abre o mapa de ocupação vê a unidade "em
// manutenção" exatamente nas noites da ordem — e a venda (a disponibilidade do
// produto) mostra zero nessas noites. Soltar o bloqueio pela ordem devolve as
// duas leituras ao "livre".
func TestManutencaoQABloqueioDaOrdemApareceOcupadoNoMapaENaVenda(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	admin := a.criarUsuario(t, "manut-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qbFaxina(t, admin)
	m := a.qmFaxina(t, f)
	prop := a.qbPropriedadePadrao(t)
	d := a.qmHoje(t, prop)
	p := m.qmProdutoVendavel(t, prop)
	tk := admin.Token

	o := a.qmAbrir(t, tk, map[string]any{"unit_id": p.Unidade, "title": "Troca do forro", "block": qmPeriodo(d.AddDays(3), d.AddDays(6))})
	de, ate := d.AddDays(2), d.AddDays(7)

	dentro := map[string]bool{d.AddDays(3).String(): true, d.AddDays(4).String(): true, d.AddDays(5).String(): true}
	conferirMapa := func(t *testing.T, ocupado bool) {
		t.Helper()
		celulas := a.qmCelulas(t, tk, p.Unidade, de, ate)
		for dia := de; dia.Before(ate); dia = dia.AddDays(1) {
			c, ok := celulas[dia.String()]
			if !ok {
				t.Errorf("o mapa não trouxe o dia %s", dia)
				continue
			}
			querOcupado := ocupado && dentro[dia.String()]
			switch {
			case querOcupado && (c.Status != "maintenance" || c.StayBlockID == nil || *c.StayBlockID != o.Bloqueio.ID):
				t.Errorf("%s: o mapa mostra %q (bloco %v); esperado `maintenance` com o bloqueio %s da ordem", dia, c.Status, c.StayBlockID, o.Bloqueio.ID)
			case !querOcupado && c.Status != "livre":
				t.Errorf("%s: o mapa mostra %q; esperado `livre`", dia, c.Status)
			}
		}
		caminho := fmt.Sprintf("/availability?from=%s&to=%s&unit_type_id=%s", de, ate, p.ID)
		linhas := envelopeDe[[]linhaDaDisponibilidade](t, a.chamar(t, http.MethodGet, caminho, tk, nil), http.StatusOK, "GET /availability")
		if len(linhas) != 1 {
			t.Fatalf("GET /availability do produto devolveu %d linhas", len(linhas))
		}
		for _, c := range linhas[0].Dias {
			quer := 1
			if ocupado && dentro[c.Data] {
				quer = 0
			}
			if c.Disponivel != quer {
				t.Errorf("%s: disponível para venda = %d, esperado %d", c.Data, c.Disponivel, quer)
			}
		}
	}

	conferirMapa(t, true)
	exigirStatusQB(t, a.chamar(t, http.MethodDelete, "/maintenance-orders/"+o.ID.String()+"/block", tk, nil), http.StatusOK, "soltando")
	conferirMapa(t, false)
}

// Em linguagem de negócio (o critério de aceite da spec §12): a ordem bloqueou
// de anteontem a depois de amanhã. Concluir HOJE deixa bloqueadas só as noites
// que já passaram (anteontem e ontem); a noite de hoje volta à venda, e uma
// reserva com check-in hoje é ACEITA. Cancelar, a mesma coisa. Antes de
// encerrar, a mesma reserva é recusada — é ela que prova que a noite estava
// presa e foi solta.
func TestManutencaoQAEncerrarHojeDevolveANoiteDeHojeAVenda(t *testing.T) {
	for _, encerramento := range []struct {
		nome, metodo, sufixo, status string
	}{
		{"concluir", http.MethodPost, "/complete", "concluida"},
		{"cancelar", http.MethodDelete, "", "cancelada"},
	} {
		t.Run(encerramento.nome, func(t *testing.T) {
			a := subirAPI(t)
			exigirSeed(t, a)
			admin := a.criarUsuario(t, "manut-admin", a.qbPerfilDoSeed(t, "admin"))
			f := a.qbFaxina(t, admin)
			m := a.qmFaxina(t, f)
			prop := a.qbPropriedadePadrao(t)
			d := a.qmHoje(t, prop)
			p := m.qmProdutoVendavel(t, prop)
			hospede := m.qmHospede(t, prop)
			tk := admin.Token

			o := a.qmAbrir(t, tk, map[string]any{"unit_id": p.Unidade, "title": "Conserto do telhado", "block": qmPeriodo(d, d.AddDays(2))})
			// O relógio andou: o bloqueio foi pedido anteontem, até depois de amanhã.
			a.qmMoverBloqueio(t, o.Bloqueio.ID, d.AddDays(-2), d.AddDays(2))

			qbErro(t, a.qmReservar(t, tk, p.ID, hospede, d, d.AddDays(2)), http.StatusConflict, "DATE_CONFLICT",
				"vender hoje com a unidade em manutenção")

			r := a.chamar(t, encerramento.metodo, "/maintenance-orders/"+o.ID.String()+encerramento.sufixo, tk, nil)
			e := qbDado[qmOrdem](t, r, http.StatusOK, encerramento.nome)
			if e.Status != encerramento.status {
				t.Fatalf("status depois de %s: %s", encerramento.nome, e.Status)
			}
			b := e.Bloqueio
			if b == nil || b.ID != o.Bloqueio.ID || b.De != d.AddDays(-2).String() || b.Ate != d.String() ||
				b.Noites != 2 || b.Status != "confirmed" || b.Fase != "encerrado" {
				t.Fatalf("o bloqueio em curso termina hoje, com as duas noites passadas: %+v", b)
			}

			// A noite de hoje e a de amanhã são vendáveis — check-in HOJE.
			venda := qbDado[reservaQA](t, a.qmReservar(t, tk, p.ID, hospede, d, d.AddDays(2)), http.StatusCreated,
				"reserva com check-in hoje depois de "+encerramento.nome)
			if venda.CheckIn != d.String() || venda.Status != "hold" {
				t.Fatalf("a reserva de hoje: %+v", venda)
			}

			celulas := a.qmCelulas(t, tk, p.Unidade, d.AddDays(-2), d.AddDays(2))
			for dia, quer := range map[string]string{
				d.AddDays(-2).String(): "maintenance", d.AddDays(-1).String(): "maintenance",
				d.String(): "hold", d.AddDays(1).String(): "hold",
			} {
				if c := celulas[dia]; c.Status != quer {
					t.Errorf("mapa em %s: %q, esperado %q", dia, c.Status, quer)
				}
			}
			if n := a.qmContar(t, `
				SELECT count(*) FROM stay_blocks x JOIN stay_blocks y
				    ON x.unit_id = y.unit_id AND x.id < y.id AND x.period && y.period
				 WHERE x.unit_id = $1 AND x.status IN ('hold','confirmed') AND y.status IN ('hold','confirmed')`, p.Unidade); n != 0 {
				t.Fatalf("o banco terminou com %d sobreposição(ões) na unidade", n)
			}
		})
	}
}

// Em linguagem de negócio: o bloqueio de uma ordem só se mexe pela ordem.
// Quem tem a chave do calendário (`DELETE /blocks/{id}`) e tenta soltar a
// unidade que está em conserto recebe 409 apontando a ordem — e a unidade
// continua fora da venda. Depois de a ordem encerrar com o bloqueio cortado em
// hoje, as noites que passaram bloqueadas também não se soltam por lá.
func TestManutencaoQABloqueioDaOrdemNaoSeSoltaPeloCalendario(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	admin := a.criarUsuario(t, "manut-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qbFaxina(t, admin)
	m := a.qmFaxina(t, f)
	prop := a.qbPropriedadePadrao(t)
	d := a.qmHoje(t, prop)
	p := m.qmProdutoVendavel(t, prop)
	hospede := m.qmHospede(t, prop)
	tk := admin.Token

	o := a.qmAbrir(t, tk, map[string]any{"unit_id": p.Unidade, "title": "Elétrica", "block": qmPeriodo(d.AddDays(4), d.AddDays(6))})
	soltar := func(t *testing.T, contexto string) {
		t.Helper()
		e := qbErro(t, a.chamar(t, http.MethodDelete, "/blocks/"+o.Bloqueio.ID.String(), tk, nil), http.StatusConflict, "", contexto)
		if e.Details["maintenance_order_id"] != o.ID.String() {
			t.Errorf("%s: details.maintenance_order_id = %v, esperado %s", contexto, e.Details["maintenance_order_id"], o.ID)
		}
	}
	soltar(t, "DELETE /blocks sobre o bloqueio agendado da ordem")
	var status string
	if err := a.pool.QueryRow(a.ctx, `SELECT status FROM stay_blocks WHERE id = $1`, o.Bloqueio.ID).Scan(&status); err != nil || status != "confirmed" {
		t.Fatalf("o bloqueio da ordem continua ocupando: %s %v", status, err)
	}
	qbErro(t, a.qmReservar(t, tk, p.ID, hospede, d.AddDays(4), d.AddDays(5)), http.StatusConflict, "DATE_CONFLICT",
		"vender a noite em conserto depois do DELETE /blocks recusado")

	// Em curso, concluída hoje: o resto [from, D) é história e continua da ordem.
	a.qmMoverBloqueio(t, o.Bloqueio.ID, d.AddDays(-1), d.AddDays(3))
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/maintenance-orders/"+o.ID.String()+"/complete", tk, nil), http.StatusOK, "concluindo")
	soltar(t, "DELETE /blocks sobre o que sobrou do bloqueio da ordem concluída")
	var periodo string
	if err := a.pool.QueryRow(a.ctx, `SELECT status || ' ' || period::text FROM stay_blocks WHERE id = $1`, o.Bloqueio.ID).Scan(&periodo); err != nil ||
		periodo != fmt.Sprintf("confirmed [%s,%s)", d.AddDays(-1), d) {
		t.Fatalf("a noite que passou bloqueada não se desfaz: %q %v", periodo, err)
	}
}

// ─────────────────────────── A disputa entre venda e manutenção ─────────────

// qmPost é o POST seguro para goroutine: devolve o erro em vez de chamar
// t.Fatal fora da goroutine do teste.
func (a *ambiente) qmPost(caminho, token, chave string, corpo any) (resposta, error) {
	bruto, err := json.Marshal(corpo)
	if err != nil {
		return resposta{}, err
	}
	req, err := http.NewRequestWithContext(a.ctx, http.MethodPost, a.servidor.URL+PrefixoDaAPI+caminho, bytes.NewReader(bruto))
	if err != nil {
		return resposta{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if chave != "" {
		req.Header.Set("Idempotency-Key", chave)
	}
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

// Em linguagem de negócio: no mesmo instante, o corretor tenta vender as duas
// noites e o encarregado tenta bloquear as mesmas duas noites para conserto.
// Exatamente UM dos pedidos leva a data — venda ou manutenção —, todos os
// outros ouvem "data ocupada" (409 DATE_CONFLICT), ninguém recebe erro interno,
// e o banco termina sem sobreposição e sem ordem órfã (ordem perdedora que
// nasceu sem o bloqueio).
//
// O nome casa com o regex `it-concorrencia` (Disputa): roda repetido no CI.
func TestDisputaEntreVendaEOrdemDeManutencaoPelaMesmaNoite(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	admin := a.criarUsuario(t, "manut-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qbFaxina(t, admin)
	m := a.qmFaxina(t, f)
	prop := a.qbPropriedadePadrao(t)
	d := a.qmHoje(t, prop)
	p := m.qmProdutoVendavel(t, prop)
	hospede := m.qmHospede(t, prop)
	de, ate := d.AddDays(40), d.AddDays(42)

	const n = 10
	chaves := make([]string, n)
	for i := range chaves {
		chaves[i] = a.chaveNova(t, fmt.Sprintf("disputa-%d", i))
	}

	type resultado struct {
		r       resposta
		err     error
		ehVenda bool
	}
	var (
		wg      sync.WaitGroup
		largada = make(chan struct{})
		out     = make([]resultado, n)
	)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-largada
			if i%2 == 0 {
				r, err := a.qmPost("/reservations", admin.Token, chaves[i], map[string]any{
					"unit_type_id": p.ID, "contact_id": hospede, "check_in": de.String(), "check_out": ate.String(), "guests_count": 2,
				})
				out[i] = resultado{r: r, err: err, ehVenda: true}
				return
			}
			r, err := a.qmPost("/maintenance-orders", admin.Token, "", map[string]any{
				"unit_id": p.Unidade, "title": fmt.Sprintf("Disputa %d", i), "block": qmPeriodo(de, ate),
			})
			out[i] = resultado{r: r, err: err}
		}()
	}
	close(largada)
	wg.Wait()

	vencedores, conflitos := 0, 0
	ordensVencedoras := 0
	for i, x := range out {
		if x.err != nil {
			t.Errorf("pedido %d: %v", i, x.err)
			continue
		}
		switch x.r.Status {
		case http.StatusCreated:
			vencedores++
			if !x.ehVenda {
				ordensVencedoras++
			}
		case http.StatusConflict:
			conflitos++
			qbEnvelopeDeErro(t, x.r, "DATE_CONFLICT", fmt.Sprintf("perdedor %d", i))
		default:
			t.Errorf("pedido %d (venda=%v): %d — nenhuma resposta da disputa pode ser outra coisa que 201 ou 409 — %s",
				i, x.ehVenda, x.r.Status, x.r.Corpo)
		}
	}
	if vencedores != 1 || conflitos != n-1 {
		t.Errorf("%d vencedor(es) e %d conflito(s); esperado exatamente 1 e %d — zero vencedores é a data presa para todo mundo sem dono",
			vencedores, conflitos, n-1)
	}

	if s := a.qmContar(t, `
		SELECT count(*) FROM stay_blocks x JOIN stay_blocks y
		    ON x.unit_id = y.unit_id AND x.id < y.id AND x.period && y.period
		 WHERE x.unit_id = $1 AND x.status IN ('hold','confirmed') AND y.status IN ('hold','confirmed')`, p.Unidade); s != 0 {
		t.Fatalf("OVERBOOKING: o banco terminou com %d sobreposição(ões) na unidade", s)
	}
	if o := a.qmContar(t, `SELECT count(*) FROM maintenance_orders WHERE unit_id = $1`, p.Unidade); o != ordensVencedoras {
		t.Fatalf("%d ordem(ns) no banco para %d ordem(ns) vencedora(s): ordem perdedora ficou órfã", o, ordensVencedoras)
	}
	if o := a.qmContar(t, `SELECT count(*) FROM maintenance_orders WHERE unit_id = $1 AND stay_block_id IS NULL`, p.Unidade); o != 0 {
		t.Fatalf("%d ordem(ns) sem o bloqueio pedido", o)
	}
}

// Em linguagem de negócio: o encarregado estende o conserto por mais duas
// noites no mesmo instante em que o corretor tenta vender exatamente essas
// duas noites — várias vezes, de vários celulares. Quem leva as noites é UM
// só: ou a extensão (e nenhuma venda), ou uma venda (e o bloqueio fica como
// estava). Ninguém recebe erro interno e o banco termina sem sobreposição.
//
// O nome casa com o regex `it-concorrencia` (Disputa): roda repetido no CI.
func TestDisputaEntreVendaEExtensaoDoBloqueioDaOrdem(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	admin := a.criarUsuario(t, "manut-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qbFaxina(t, admin)
	m := a.qmFaxina(t, f)
	prop := a.qbPropriedadePadrao(t)
	d := a.qmHoje(t, prop)
	p := m.qmProdutoVendavel(t, prop)
	hospede := m.qmHospede(t, prop)
	x := d.AddDays(50)

	o := a.qmAbrir(t, admin.Token, map[string]any{"unit_id": p.Unidade, "title": "Reforma do deck", "block": qmPeriodo(x, x.AddDays(2))})

	const vendas = 7
	chaves := make([]string, vendas)
	for i := range chaves {
		chaves[i] = a.chaveNova(t, fmt.Sprintf("extensao-%d", i))
	}
	var (
		wg        sync.WaitGroup
		largada   = make(chan struct{})
		respostas = make([]resposta, vendas+1)
		erros     = make([]error, vendas+1)
	)
	for i := 0; i <= vendas; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-largada
			if i == vendas {
				req, err := http.NewRequestWithContext(a.ctx, http.MethodPut,
					a.servidor.URL+PrefixoDaAPI+"/maintenance-orders/"+o.ID.String()+"/block",
					bytes.NewReader([]byte(fmt.Sprintf(`{"from": %q, "to": %q}`, x, x.AddDays(4)))))
				if err != nil {
					erros[i] = err
					return
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+admin.Token)
				resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
				if err != nil {
					erros[i] = err
					return
				}
				defer func() { _ = resp.Body.Close() }()
				lido, err := io.ReadAll(resp.Body)
				respostas[i], erros[i] = resposta{Status: resp.StatusCode, Corpo: lido}, err
				return
			}
			respostas[i], erros[i] = a.qmPost("/reservations", admin.Token, chaves[i], map[string]any{
				"unit_type_id": p.ID, "contact_id": hospede,
				"check_in": x.AddDays(2).String(), "check_out": x.AddDays(4).String(), "guests_count": 2,
			})
		}()
	}
	close(largada)
	wg.Wait()

	vencedores, extensaoVenceu := 0, false
	for i, r := range respostas {
		if erros[i] != nil {
			t.Errorf("pedido %d: %v", i, erros[i])
			continue
		}
		ok := (i == vendas && r.Status == http.StatusOK) || (i < vendas && r.Status == http.StatusCreated)
		switch {
		case ok:
			vencedores++
			extensaoVenceu = i == vendas
		case r.Status == http.StatusConflict:
			qbEnvelopeDeErro(t, r, "DATE_CONFLICT", fmt.Sprintf("perdedor %d", i))
		default:
			t.Errorf("pedido %d (extensão=%v): %d — só sucesso ou 409 — %s", i, i == vendas, r.Status, r.Corpo)
		}
	}
	if vencedores != 1 {
		t.Errorf("%d pedido(s) levaram as noites; esperado exatamente 1", vencedores)
	}
	b := a.qmLer(t, admin.Token, o.ID).Bloqueio
	quer := x.AddDays(2)
	if extensaoVenceu {
		quer = x.AddDays(4)
	}
	if b == nil || b.Ate != quer.String() {
		t.Errorf("o bloqueio termina em %v; esperado %s (extensão venceu = %v)", b, quer, extensaoVenceu)
	}
	if s := a.qmContar(t, `
		SELECT count(*) FROM stay_blocks x JOIN stay_blocks y
		    ON x.unit_id = y.unit_id AND x.id < y.id AND x.period && y.period
		 WHERE x.unit_id = $1 AND x.status IN ('hold','confirmed') AND y.status IN ('hold','confirmed')`, p.Unidade); s != 0 {
		t.Fatalf("OVERBOOKING: o banco terminou com %d sobreposição(ões) na unidade", s)
	}
}

// Em linguagem de negócio: o site de vendas pergunta à API e mostra "ocupado"
// nas noites em conserto — sem dizer que é manutenção, sem o título da ordem e
// sem o id do bloqueio. Rota pública não carrega vocabulário interno.
func TestManutencaoQAOSiteVeOcupadoSemSaberQueEhManutencao(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	admin := a.criarUsuario(t, "manut-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qbFaxina(t, admin)
	m := a.qmFaxina(t, f)
	prop := a.qbPropriedadePadrao(t)
	d := a.qmHoje(t, prop)
	p := m.qmProdutoVendavel(t, prop)
	if _, err := a.pool.Exec(a.ctx, `UPDATE unit_types SET public_name = 'Chalé do QA' WHERE id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	titulo := "Infiltração sigilosa " + qbSufixo()
	o := a.qmAbrir(t, admin.Token, map[string]any{"unit_id": p.Unidade, "title": titulo, "block": qmPeriodo(d.AddDays(3), d.AddDays(5))})

	r := a.chamar(t, http.MethodGet, fmt.Sprintf("/public/availability?unit_type_id=%s&from=%s&to=%s", p.ID, d.AddDays(2), d.AddDays(6)), "", nil)
	dias := envelopeDe[[]struct {
		Data        string `json:"date"`
		Disponivel  bool   `json:"available"`
		SobConsulta bool   `json:"on_request"`
	}](t, r, http.StatusOK, "GET /public/availability")
	for _, dia := range dias {
		ocupado := dia.Data == d.AddDays(3).String() || dia.Data == d.AddDays(4).String()
		if dia.Disponivel == ocupado {
			t.Errorf("%s: available=%v no site; esperado %v", dia.Data, dia.Disponivel, !ocupado)
		}
		if ocupado && dia.SobConsulta {
			t.Errorf("%s: o site diz 'sob consulta' (falta de tarifa) numa noite que está ocupada", dia.Data)
		}
	}
	corpo := strings.ToLower(string(r.Corpo))
	for _, proibido := range []string{"maintenance", "manuten", strings.ToLower(titulo), o.Bloqueio.ID.String(), o.ID.String(), strings.ToLower(p.Codigo)} {
		if strings.Contains(corpo, proibido) {
			t.Errorf("o calendário público vaza %q: %s", proibido, r.Corpo)
		}
	}
}
