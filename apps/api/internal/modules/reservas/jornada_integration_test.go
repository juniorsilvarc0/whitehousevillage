//go:build integration

package reservas_test

import (
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// ─────────────────────────── 1. A jornada ───────────────────────────

// Orçamento → pré-reserva → sinal → confirmada, com os valores da Tabela V1.
//
// Cobertura, 20 a 23 de novembro de 2026: sexta e sábado são fim de semana
// (R$ 2.400 cada) e domingo é diária normal (R$ 1.900) — na White House o
// hóspede vai embora no domingo, então domingo NÃO é fim de semana. Subtotal
// R$ 6.700, limpeza R$ 350, total R$ 7.050, sinal metade.
func TestJornadaDoOrcamentoAoSinalComOsValoresDaTabelaV1(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)
	cobertura := a.produto(t, "cobertura")

	reserva := a.criarReserva(t, token, pedido(cobertura, contato, "2026-11-20", "2026-11-23", 4))

	if reserva.Status != reservas.EstadoHold {
		t.Fatalf("a reserva nasce em %q, esperado hold", reserva.Status)
	}
	if reserva.Noites != 3 {
		t.Fatalf("noites = %d, esperado 3 (o intervalo é half-open)", reserva.Noites)
	}
	for _, caso := range []struct {
		campo string
		got   int64
		quer  int64
	}{
		{"subtotal_cents", reserva.Subtotal, 670000},
		{"cleaning_cents", reserva.Limpeza, 35000},
		{"discount_cents", reserva.Desconto, 0},
		{"total_cents", reserva.Total, 705000},
		{"deposit_cents", reserva.Sinal, 352500},
		{"balance_cents", reserva.Saldo, 352500},
	} {
		if caso.got != caso.quer {
			t.Errorf("%s = %d, esperado %d", caso.campo, caso.got, caso.quer)
		}
	}

	// O código é legível, sequencial por ano e gerado pelo banco.
	if len(reserva.Codigo) != 12 || reserva.Codigo[:3] != "WH-" {
		t.Errorf("code = %q, esperado o formato WH-AAAA-NNNN", reserva.Codigo)
	}
	// O que a reserva CONGELA — sem isto, mudar o tarifário amanhã reescreveria
	// esta venda.
	if reserva.RateTableID == nil || reserva.PolicyVer == nil || reserva.CancelPolicy == nil {
		t.Fatalf("snapshot incompleto: tabela=%v politica=%v cancelamento=%v",
			reserva.RateTableID, reserva.PolicyVer, reserva.CancelPolicy)
	}
	if reserva.HoldExpiraEm == nil {
		t.Fatal("a pré-reserva nasceu sem prazo: o job nunca a expiraria")
	}
	// Cobertura é `one_member` com uma única unidade na composição.
	if got := reserva.codigosDasUnidades(); len(got) != 1 || got[0] != "COB-01" {
		t.Fatalf("unidades alocadas = %v, esperado [COB-01]", got)
	}
	if a.statusDosBlocos(t, reserva.ID)[reservas.BlocoHold] != 1 {
		t.Fatalf("blocos = %v, esperado 1 em hold", a.statusDosBlocos(t, reserva.ID))
	}

	// O snapshot noite a noite é o que faz o passado não mudar.
	completa := dado[struct {
		Noites []struct {
			Noite string `json:"night"`
			Tipo  string `json:"date_type"`
			Preco int64  `json:"price_cents"`
		} `json:"nights"`
		Linhas []struct {
			Tipo   string `json:"date_type"`
			Noites int    `json:"nights"`
		} `json:"lines"`
	}](t, a.chamar(t, http.MethodGet, "/reservations/"+reserva.ID.String()+"/full", token, nil))

	esperadas := []struct {
		noite string
		tipo  string
		preco int64
	}{
		{"2026-11-20", "fds", 240000},
		{"2026-11-21", "fds", 240000},
		{"2026-11-22", "normal", 190000},
	}
	if len(completa.Noites) != 3 {
		t.Fatalf("noites gravadas = %d", len(completa.Noites))
	}
	for i, e := range esperadas {
		n := completa.Noites[i]
		if n.Noite != e.noite || n.Tipo != e.tipo || n.Preco != e.preco {
			t.Errorf("noite %d = %s/%s/%d, esperado %s/%s/%d", i, n.Noite, n.Tipo, n.Preco, e.noite, e.tipo, e.preco)
		}
	}
	if len(completa.Linhas) != 2 {
		t.Errorf("linhas agrupadas = %d, esperado 2 (fds e normal)", len(completa.Linhas))
	}

	// ─── O sinal ───
	chave := "it-confirm-" + sufixo()
	a.limparChaves(t, chave)
	resp := a.chamarIdem(t, http.MethodPost, "/reservations/"+reserva.ID.String()+"/confirm", token,
		map[string]any{"deposit_paid_cents": 352500, "method": "pix"}, chave)
	if resp.Status != http.StatusOK {
		t.Fatalf("POST /confirm = %d (%s)", resp.Status, resp.Corpo)
	}

	confirmada := dado[reservaVista](t, resp)
	if confirmada.Status != reservas.EstadoConfirmada {
		t.Fatalf("status = %q, esperado confirmed", confirmada.Status)
	}
	if confirmada.ConfirmadaEm == nil {
		t.Error("confirmed_at ficou vazio")
	}
	// A data continua bloqueada; o que ela perde é o prazo de validade.
	if confirmada.HoldExpiraEm != nil {
		t.Errorf("hold_expires_at = %v, esperado nulo depois do sinal", *confirmada.HoldExpiraEm)
	}
	if blocos := a.statusDosBlocos(t, reserva.ID); blocos[reservas.BlocoConfirmado] != 1 || blocos[reservas.BlocoHold] != 0 {
		t.Fatalf("blocos = %v, esperado 1 confirmado e 0 em hold", blocos)
	}
	// O preço NÃO é recalculado na confirmação.
	if confirmada.Total != 705000 {
		t.Errorf("total mudou na confirmação: %d", confirmada.Total)
	}
}

// ─────────────────────────── 2. A Completa nos dois sentidos ────────

// A White House Completa consome as OITO unidades: vendê-la trava a casa
// inteira, e qualquer unidade ocupada a torna invendável. É a exclusividade que
// cai de graça da constraint — sem uma linha de código a decidindo.
func TestCompletaTravaAsOitoEUmaUnidadeOcupadaADerruba(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	completa := a.produto(t, "completa")
	apto := a.produto(t, "apto-2s")

	de, ate := a.dia(400), a.dia(404)

	t.Run("a Completa trava as oito unidades", func(t *testing.T) {
		contato := a.contato(t)
		reserva := a.criarReserva(t, token, pedido(completa, contato, de, ate, 12))

		codigos := reserva.codigosDasUnidades()
		if len(codigos) != 8 {
			t.Fatalf("unidades = %v (%d), esperado as 8", codigos, len(codigos))
		}
		// Ordenadas por `units.code`: é a ordem de inserção que evita impasse.
		esperado := []string{"AP-01", "AP-02", "AP-03", "COB-01", "SP-01", "SP-02", "SP-03", "SP-04"}
		for i := range esperado {
			if codigos[i] != esperado[i] {
				t.Fatalf("unidades = %v, esperado %v", codigos, esperado)
			}
		}
		if a.statusDosBlocos(t, reserva.ID)[reservas.BlocoHold] != 8 {
			t.Fatalf("blocos = %v, esperado 8 em hold", a.statusDosBlocos(t, reserva.ID))
		}

		// Com a casa travada, nem um apartamento sozinho se vende.
		chave := "it-conflito-" + sufixo()
		a.limparChaves(t, chave)
		resp := a.chamarIdem(t, http.MethodPost, "/reservations", token,
			pedido(apto, contato, de, ate, 2), chave)
		if resp.Status != http.StatusConflict {
			t.Fatalf("vender apartamento com a casa travada = %d (%s)", resp.Status, resp.Corpo)
		}
		if resp.codigoDoErro() != "DATE_CONFLICT" {
			t.Fatalf("code = %s, esperado DATE_CONFLICT", resp.codigoDoErro())
		}
	})

	t.Run("uma unidade ocupada derruba a Completa", func(t *testing.T) {
		outroDe, outroAte := a.dia(410), a.dia(414)
		contato := a.contato(t)

		// Uma única suíte da piscina ocupada — a menos importante das oito.
		a.bloqueioDireto(t, a.unidade(t, "SP-03"), outroDe, outroAte)

		chave := "it-completa-" + sufixo()
		a.limparChaves(t, chave)
		resp := a.chamarIdem(t, http.MethodPost, "/reservations", token,
			pedido(completa, contato, outroDe, outroAte, 10), chave)

		if resp.Status != http.StatusConflict {
			t.Fatalf("vender a Completa com SP-03 ocupada = %d (%s)", resp.Status, resp.Corpo)
		}
		if resp.codigoDoErro() != "DATE_CONFLICT" {
			t.Fatalf("code = %s, esperado DATE_CONFLICT", resp.codigoDoErro())
		}
		// O contrato promete dizer QUAL unidade derrubou a venda.
		if unidade, _ := resp.detalhes(t)["unit_code"].(string); unidade != "SP-03" {
			t.Errorf("details.unit_code = %q, esperado SP-03", unidade)
		}
		// Bloqueio parcial não existe: nada foi criado.
		var criadas int
		if err := a.pool.QueryRow(a.ctx,
			`SELECT count(*) FROM reservations WHERE contact_id = $1`, contato).Scan(&criadas); err != nil {
			t.Fatal(err)
		}
		if criadas != 0 {
			t.Fatalf("a transação deixou %d reserva(s) para trás", criadas)
		}
	})
}

// ─────────────────────────── 3. Concorrência ────────────────────────

// spec §5: "50 goroutines tentando reservar a mesma data: exatamente uma vence,
// as demais recebem 409 DATE_CONFLICT, nenhuma 500".
//
// É o teste que prova que a defesa é o BANCO. Nenhum SELECT precede a inserção;
// quem perde a corrida perde na constraint.
func TestCorridaPelaMesmaDataTemExatamenteUmVencedorPorUnidade(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)

	casos := []struct {
		nome        string
		produto     string
		hospedes    int
		vencedores  int
		concorrente int
		dia         int
	}{
		// Cobertura tem UMA unidade: um vencedor.
		{"cobertura", "cobertura", 2, 1, 50, 500},
		// Apartamento 2 Suítes tem TRÊS: três vendas legítimas na mesma data,
		// e a quarta em diante recebe 409 (spec §2).
		{"apto-2s", "apto-2s", 2, 3, 50, 520},
		// A Completa consome as oito: um vencedor, mesmo com oito unidades.
		{"completa", "completa", 4, 1, 50, 540},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			produto := a.produto(t, caso.produto)
			de, ate := a.dia(caso.dia), a.dia(caso.dia+4)

			var (
				espera   sync.WaitGroup
				mutex    sync.Mutex
				criadas  int
				conflito int
				outros   []int
			)
			// Todas as goroutines partem juntas: sem a barreira, a primeira
			// comitaria antes de a última nascer e não haveria corrida.
			largada := make(chan struct{})

			for i := 0; i < caso.concorrente; i++ {
				espera.Add(1)
				go func(n int) {
					defer espera.Done()
					<-largada

					chave := "it-corrida-" + sufixo() + "-" + sufixo()
					resp := a.chamarIdem(t, http.MethodPost, "/reservations", token,
						pedido(produto, contato, de, ate, caso.hospedes), chave)

					mutex.Lock()
					defer mutex.Unlock()
					switch {
					case resp.Status == http.StatusCreated:
						criadas++
					case resp.Status == http.StatusConflict && resp.codigoDoErro() == "DATE_CONFLICT":
						conflito++
					default:
						outros = append(outros, resp.Status)
						t.Errorf("resposta inesperada %d: %s", resp.Status, resp.Corpo)
					}
				}(i)
			}
			close(largada)
			espera.Wait()

			a.limparChaves(t, "it-corrida-")

			if criadas != caso.vencedores {
				t.Fatalf("vencedores = %d, esperado %d (conflitos %d, outros %v)",
					criadas, caso.vencedores, conflito, outros)
			}
			if conflito != caso.concorrente-caso.vencedores {
				t.Fatalf("409 = %d, esperado %d", conflito, caso.concorrente-caso.vencedores)
			}
			if len(outros) != 0 {
				t.Fatalf("houve respostas fora de {201, 409}: %v", outros)
			}

			// Limpa para o `-count=N` continuar valendo.
			a.executar(t, `DELETE FROM reservations WHERE contact_id = $1 AND check_in = $2::date`, contato, de)
		})
	}
}

// ─────────────────────────── 4. Expiração ───────────────────────────

// A pré-reserva vencida libera a data sozinha — e a Completa libera as oito
// unidades JUNTAS, na mesma transação. Meio-livre não existe: três unidades
// vendáveis e cinco presas num hold que já não existe seria pior que o hold.
func TestHoldVencidoLiberaADataSozinho(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	expirador := reservas.NovoExpirador(a.pool, db.NewTxManager(a.pool))

	casos := []struct {
		nome     string
		produto  string
		hospedes int
		blocos   int
		dia      int
	}{
		{"produto simples", "apto-2s", 2, 1, 600},
		{"a Completa libera as oito juntas", "completa", 8, 8, 620},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			contato := a.contato(t)
			produto := a.produto(t, caso.produto)
			de, ate := a.dia(caso.dia), a.dia(caso.dia+4)

			reserva := a.criarReserva(t, token, pedido(produto, contato, de, ate, caso.hospedes))
			if n := a.statusDosBlocos(t, reserva.ID)[reservas.BlocoHold]; n != caso.blocos {
				t.Fatalf("blocos em hold = %d, esperado %d", n, caso.blocos)
			}

			// Envelhece o prazo pelo BANCO: o job compara com o now() do
			// Postgres, e mexer no relógio do processo não o convenceria.
			a.executar(t, `UPDATE reservations SET hold_expires_at = now() - interval '1 minute' WHERE id = $1`, reserva.ID)
			a.executar(t, `UPDATE stay_blocks SET expires_at = now() - interval '1 minute' WHERE reservation_id = $1`, reserva.ID)

			n, err := expirador.Rodar(a.ctx)
			if err != nil {
				t.Fatalf("job de expiração: %v", err)
			}
			if n < 1 {
				t.Fatalf("o job não expirou nada")
			}

			if got := a.statusDaReserva(t, reserva.ID); got != reservas.EstadoExpirada {
				t.Fatalf("status = %q, esperado expired", got)
			}
			blocos := a.statusDosBlocos(t, reserva.ID)
			if blocos[reservas.BlocoExpirado] != caso.blocos || blocos[reservas.BlocoHold] != 0 {
				t.Fatalf("blocos = %v, esperado %d expirados e 0 em hold", blocos, caso.blocos)
			}

			// A prova de que a data VOLTOU ao estoque: a mesma venda passa agora.
			outra := a.criarReserva(t, token, pedido(produto, contato, de, ate, caso.hospedes))
			if outra.ID == reserva.ID {
				t.Fatal("a segunda venda reaproveitou a reserva expirada")
			}

			// E o fato ficou registrado, com autor nulo — quem expirou foi o
			// relógio, não uma pessoa.
			var eventos int
			if err := a.pool.QueryRow(a.ctx,
				`SELECT count(*) FROM reservation_events WHERE reservation_id = $1 AND type = $2 AND actor_id IS NULL`,
				reserva.ID, reservas.EventoExpirada).Scan(&eventos); err != nil {
				t.Fatal(err)
			}
			if eventos != 1 {
				t.Fatalf("eventos `expired` sem autor = %d, esperado 1", eventos)
			}
		})
	}
}

// O advisory lock impede duas réplicas de varrerem ao mesmo tempo. A segunda
// devolve na hora — não é erro, é "alguém já está fazendo isso".
func TestExpiracaoNaoRodaEmDuasReplicasAoMesmoTempo(t *testing.T) {
	a := subir(t)

	conexao, err := a.pool.Acquire(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conexao.Release()

	var peguei bool
	if err := conexao.QueryRow(a.ctx, `SELECT pg_try_advisory_lock($1)`, reservas.ChaveDaTravaDeExpiracao).Scan(&peguei); err != nil {
		t.Fatal(err)
	}
	if !peguei {
		t.Fatal("a trava já estava tomada antes do teste")
	}
	defer func() {
		_, _ = conexao.Exec(a.ctx, `SELECT pg_advisory_unlock($1)`, reservas.ChaveDaTravaDeExpiracao)
	}()

	n, err := reservas.NovoExpirador(a.pool, db.NewTxManager(a.pool)).Rodar(a.ctx)
	if err != nil {
		t.Fatalf("com a trava tomada o job deveria devolver em silêncio, veio: %v", err)
	}
	if n != 0 {
		t.Fatalf("expirou %d com a trava de outra réplica tomada", n)
	}
}

// ─────────────────────────── 6. Idempotência ────────────────────────

// Mesma chave duas vezes cria UMA reserva — e a segunda resposta é a original,
// byte a byte, com o mesmo 201 e o mesmo Location.
func TestMesmaChaveDeIdempotenciaCriaUmaSoReserva(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)
	produto := a.produto(t, "suite-piscina")

	chave := "it-idem-" + sufixo() + "-" + sufixo()
	a.limparChaves(t, chave)
	corpo := pedido(produto, contato, a.dia(700), a.dia(704), 2)

	primeira := a.chamarIdem(t, http.MethodPost, "/reservations", token, corpo, chave)
	if primeira.Status != http.StatusCreated {
		t.Fatalf("primeira = %d (%s)", primeira.Status, primeira.Corpo)
	}
	segunda := a.chamarIdem(t, http.MethodPost, "/reservations", token, corpo, chave)
	if segunda.Status != http.StatusCreated {
		t.Fatalf("a repetição devolveu %d, esperado o mesmo 201 (%s)", segunda.Status, segunda.Corpo)
	}
	// Igualdade de CONTEÚDO, não de bytes: a resposta guardada volta do `jsonb`
	// de `idempotency_keys`, que normaliza a ordem das chaves. O que o cliente
	// lê é o mesmo documento.
	if !mesmoJSON(t, primeira.Corpo, segunda.Corpo) {
		t.Fatalf("a repetição devolveu outro corpo:\n%s\n%s", primeira.Corpo, segunda.Corpo)
	}
	if segunda.Location == "" || segunda.Location != primeira.Location {
		t.Errorf("Location da repetição = %q, esperado %q", segunda.Location, primeira.Location)
	}

	var criadas int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM reservations WHERE contact_id = $1`, contato).Scan(&criadas); err != nil {
		t.Fatal(err)
	}
	if criadas != 1 {
		t.Fatalf("reservas criadas = %d, esperado 1", criadas)
	}

	// Mesma chave, corpo diferente: 409 IDEMPOTENCY_MISMATCH. Sem isso, um
	// retry com o corpo trocado devolveria a resposta antiga como se fosse a da
	// requisição nova.
	outro := pedido(produto, contato, a.dia(700), a.dia(704), 3)
	divergente := a.chamarIdem(t, http.MethodPost, "/reservations", token, outro, chave)
	if divergente.Status != http.StatusConflict || divergente.codigoDoErro() != "IDEMPOTENCY_MISMATCH" {
		t.Fatalf("corpo divergente = %d/%s", divergente.Status, divergente.codigoDoErro())
	}

	// E a chave é obrigatória: criar reserva é irreversível sob concorrência.
	sem := a.chamar(t, http.MethodPost, "/reservations", token, corpo)
	if sem.Status != http.StatusUnprocessableEntity {
		t.Fatalf("sem Idempotency-Key = %d, esperado 422", sem.Status)
	}
}

// A idempotência do /confirm protege dinheiro: dois cliques no botão "registrar
// sinal" não podem virar dois pagamentos na timeline.
func TestConfirmarDuasVezesComAMesmaChaveRegistraUmSinalSo(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)
	reserva := a.criarReserva(t, token, pedido(a.produto(t, "apto-2s"), contato, a.dia(720), a.dia(724), 2))

	chave := "it-confirm2-" + sufixo()
	a.limparChaves(t, chave)
	caminho := "/reservations/" + reserva.ID.String() + "/confirm"
	corpo := map[string]any{"deposit_paid_cents": 100000, "method": "pix"}

	primeira := a.chamarIdem(t, http.MethodPost, caminho, token, corpo, chave)
	segunda := a.chamarIdem(t, http.MethodPost, caminho, token, corpo, chave)
	if primeira.Status != http.StatusOK || segunda.Status != http.StatusOK {
		t.Fatalf("respostas = %d/%d (%s)", primeira.Status, segunda.Status, segunda.Corpo)
	}
	if !mesmoJSON(t, primeira.Corpo, segunda.Corpo) {
		t.Fatalf("a repetição do /confirm devolveu outro corpo:\n%s\n%s", primeira.Corpo, segunda.Corpo)
	}

	var pagamentos int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM reservation_events WHERE reservation_id = $1 AND type = $2`,
		reserva.ID, reservas.EventoConfirmada).Scan(&pagamentos); err != nil {
		t.Fatal(err)
	}
	if pagamentos != 1 {
		t.Fatalf("eventos de sinal = %d, esperado 1", pagamentos)
	}
}

// Sanidade: `uuid.UUID` é usado no arquivo; mantém o import honesto.
var _ = uuid.Nil
