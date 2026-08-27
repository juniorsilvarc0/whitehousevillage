//go:build integration

// A EXCLUSIVIDADE CAI PELA COMPOSIÇÃO — o crítico desta rodada.
//
// A rodada anterior fechou quatro portas para o mesmo buraco (as duas de
// `units.active`, a de `unit_types.active`, e a da unidade inativa entrando na
// composição). Todas guardam uma COLUNA. O revisor entrou pelo CONJUNTO, e
// mediu, em Postgres 16 e 14:
//
//	Completa vendida 2033-05-10→13 (WH-2026-0011, 8 blocos)
//	POST /units cria AP-99
//	PUT /unit-types/{completa}/members com os 8 atuais + AP-99  → 200
//	banco: composicao = 9, reservation_units = 8, stay_blocks = 8
//	AP-99 somado ao apto-2s, POST /reservations 2033-05-11→12   → 201
//	⇒ um estranho dorme dentro da casa alugada inteira.
//
// A `EXCLUDE` não detecta e não tinha como: a nona linha de stay_blocks NUNCA É
// INSERIDA, e o buraco é a AUSÊNCIA de uma linha. Constraint nenhuma vê
// ausência.
//
// Os testes abaixo montam o estado "casa vendida" DIRETO no banco (é o que os
// helpers `reserva`/`bloqueioDaReserva`/`compor` existem para fazer) e exercitam
// os endpoints DESTE módulo. O passo final do caminho — o `POST /reservations`
// que hospeda o estranho — é do módulo `reservas`, e é reproduzido aqui pelo
// INSERT que aquela venda executaria: se a linha entra, a unidade estava livre
// dentro da estadia exclusiva, e é isso que o caminho todo significa.
package inventario_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// casaVendida monta o cenário: produto `all_members` com `quantas` unidades,
// composição gravada, e uma reserva CONFIRMADA com bloco em cada unidade.
//
// As datas são as dos helpers do ambiente (current_date + 30 → + 33), ou seja
// futuro — que é o que faz a venda "viva" para as guardas.
func (a *ambiente) casaVendida(t *testing.T, consome string, quantas int) (produto uuid.UUID, unidades []uuid.UUID, reserva uuid.UUID) {
	t.Helper()

	produto, _ = a.produtoDireto(t, consome)
	for i := 0; i < quantas; i++ {
		unidades = append(unidades, a.unidadeDireta(t, fmt.Sprintf("IT-V%d-%s", i, sufixo())))
	}
	a.compor(t, produto, unidades...)

	reserva = a.reserva(t, produto, "confirmed")
	for _, unidade := range unidades {
		bloco := a.bloqueioDaReserva(t, unidade, reserva)
		// `reservation_units` é o que a invariante da venda exclusiva compara
		// com a composição; sem ela o cenário não seria o cenário.
		a.vincular(t, reserva, unidade, bloco)
	}
	return produto, unidades, reserva
}

func (a *ambiente) vincular(t *testing.T, reserva, unidade, bloco uuid.UUID) {
	t.Helper()
	if _, err := a.pool.Exec(a.ctx, `
		INSERT INTO reservation_units (reservation_id, unit_id, stay_block_id)
		VALUES ($1, $2, $3)`, reserva, unidade, bloco); err != nil {
		t.Fatalf("vinculando unidade à reserva: %v", err)
	}
	t.Cleanup(func() {
		a.executar(t, `DELETE FROM reservation_units WHERE reservation_id = $1 AND unit_id = $2`,
			reserva, unidade)
	})
}

func (a *ambiente) contarComposicao(t *testing.T, produto uuid.UUID) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM unit_type_members WHERE unit_type_id = $1`, produto).Scan(&n); err != nil {
		t.Fatalf("contando a composição: %v", err)
	}
	return n
}

func (a *ambiente) consumoDoProduto(t *testing.T, produto uuid.UUID) string {
	t.Helper()
	var c string
	if err := a.pool.QueryRow(a.ctx,
		`SELECT consumes FROM unit_types WHERE id = $1`, produto).Scan(&c); err != nil {
		t.Fatalf("lendo consumes: %v", err)
	}
	return c
}

// ═══════════════════════ O crítico, e sua porta gêmea ═══════════════════════

// TestAcrescentarUnidadeNaComposicaoDaCasaVendida é o caminho relatado.
func TestAcrescentarUnidadeNaComposicaoDaCasaVendida(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_vend_add", celulaVer, celulaCriar, celulaEditar))

	produto, unidades, reserva := a.casaVendida(t, "all_members", 3)
	nova := a.unidadeDireta(t, "IT-AP99-"+sufixo())
	caminho := "/unit-types/" + produto.String() + "/members"

	r := a.chamar(t, http.MethodPut, caminho, gestor,
		map[string]any{"unit_ids": append(append([]uuid.UUID{}, unidades...), nova)})

	if r.Status == http.StatusOK {
		t.Fatalf("a composição da casa VENDIDA (%s) aceitou uma unidade nova: composicao=%d, blocos=%d — "+
			"a unidade acrescentada fica livre dentro da estadia exclusiva, e a EXCLUDE não vê ausência de linha",
			a.codigoDaReserva(t, reserva), a.contarComposicao(t, produto), len(unidades))
	}
	if r.Status != http.StatusConflict {
		t.Fatalf("PUT members = %d, esperado 409 (%s)", r.Status, r.Corpo)
	}
	if c := r.codigoDoErro(); c != "RESOURCE_IN_USE" {
		t.Fatalf("code = %q, esperado RESOURCE_IN_USE (%s)", c, r.Corpo)
	}

	d := r.detalhes(t)
	if d["reservations_count"] != float64(1) {
		t.Errorf("details.reservations_count = %v, esperado 1", d["reservations_count"])
	}
	// A recusa é inútil se não disser QUAL reserva resolver.
	vivas, _ := d["live_reservations"].([]any)
	if len(vivas) != 1 {
		t.Fatalf("details.live_reservations = %v, esperado a reserva que segura o conjunto", d["live_reservations"])
	}
	primeira, _ := vivas[0].(map[string]any)
	if primeira["code"] != a.codigoDaReserva(t, reserva) {
		t.Errorf("details.live_reservations[0].code = %v, esperado %s",
			primeira["code"], a.codigoDaReserva(t, reserva))
	}
	if primeira["check_in"] == nil || primeira["check_out"] == nil {
		t.Errorf("a recusa não informa as datas a esperar: %v", primeira)
	}

	// E nada foi gravado: a transação inteira voltou.
	if n := a.contarComposicao(t, produto); n != len(unidades) {
		t.Fatalf("a composição foi gravada apesar da recusa: %d, esperado %d", n, len(unidades))
	}
}

// A REMOÇÃO é o mesmo buraco pela porta oposta, e foi medida ao vivo: com
// AP-03 fora da composição, a venda seguinte da Completa saiu com 7 unidades
// pelo preço de 8 e o apto-2s foi vendido em AP-03 dentro das mesmas datas.
func TestRemoverUnidadeDaComposicaoDaCasaVendida(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_vend_rem", celulaVer, celulaEditar))

	produto, unidades, _ := a.casaVendida(t, "all_members", 3)
	caminho := "/unit-types/" + produto.String() + "/members"

	r := a.chamar(t, http.MethodPut, caminho, gestor,
		map[string]any{"unit_ids": unidades[:2]})

	if r.Status != http.StatusConflict {
		t.Fatalf("PUT members removendo unidade da casa vendida = %d, esperado 409 (%s)", r.Status, r.Corpo)
	}
	if c := r.codigoDoErro(); c != "RESOURCE_IN_USE" {
		t.Fatalf("code = %q, esperado RESOURCE_IN_USE (%s)", c, r.Corpo)
	}
	if n := a.contarComposicao(t, produto); n != 3 {
		t.Fatalf("a composição foi gravada apesar da recusa: %d, esperado 3", n)
	}
}

// A GUARDA PROTEGE A VENDA, NÃO O CADASTRO: sem estadia futura, a composição
// muda normalmente. Sem este teste a correção poderia ser "recusar sempre", que
// tornaria o cadastro imutável.
func TestComposicaoMudaQuandoNaoHaEstadiaFutura(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_vend_livre", celulaVer, celulaEditar))

	produto, _ := a.produtoDireto(t, "all_members")
	u1 := a.unidadeDireta(t, "IT-L1-"+sufixo())
	u2 := a.unidadeDireta(t, "IT-L2-"+sufixo())
	a.compor(t, produto, u1)

	r := a.chamar(t, http.MethodPut, "/unit-types/"+produto.String()+"/members", gestor,
		map[string]any{"unit_ids": []uuid.UUID{u1, u2}})
	if r.Status != http.StatusOK {
		t.Fatalf("composição sem venda viva = %d, esperado 200 (%s)", r.Status, r.Corpo)
	}
	if n := a.contarComposicao(t, produto); n != 2 {
		t.Fatalf("composição = %d, esperado 2", n)
	}
}

// Reenviar o MESMO conjunto é no-op: a tela salva o formulário sem ninguém ter
// tocado na grade, e recusar isso ensinaria o operador a temer o botão.
func TestReenviarAMesmaComposicaoDeCasaVendidaPassa(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_vend_noop", celulaVer, celulaEditar))

	produto, unidades, _ := a.casaVendida(t, "all_members", 3)
	// Ordem invertida de propósito: o que importa é o CONJUNTO.
	invertida := []uuid.UUID{unidades[2], unidades[0], unidades[1]}

	r := a.chamar(t, http.MethodPut, "/unit-types/"+produto.String()+"/members", gestor,
		map[string]any{"unit_ids": invertida})
	if r.Status != http.StatusOK {
		t.Fatalf("reenviar o mesmo conjunto = %d, esperado 200 (%s)", r.Status, r.Corpo)
	}
}

// ═══════════════════════ `consumes`: a mesma falha por outra coluna ═════════

// all_members → one_member com venda viva. Medido ao vivo: a Completa vendida
// por R$ 20.200,00 saiu com UM apartamento, e o apto-2s foi vendido dentro das
// mesmas datas — 201, e corretamente, porque não havia bloco com que colidir.
func TestTrocarConsumesDeProdutoVendidoPeloPatchEhRecusado(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_cons_patch", celulaVer, celulaEditar))

	produto, _, _ := a.casaVendida(t, "all_members", 3)

	r := a.chamar(t, http.MethodPatch, "/unit-types/"+produto.String(), gestor,
		map[string]any{"consumes": "one_member"})
	if r.Status != http.StatusConflict {
		t.Fatalf("PATCH consumes com venda viva = %d, esperado 409 (%s)", r.Status, r.Corpo)
	}
	if c := r.codigoDoErro(); c != "RESOURCE_IN_USE" {
		t.Fatalf("code = %q, esperado RESOURCE_IN_USE (%s)", c, r.Corpo)
	}
	d := r.detalhes(t)
	if d["current_consumes"] != "all_members" || d["requested_consumes"] != "one_member" {
		t.Errorf("details = %v, esperado a transição all_members → one_member", d)
	}
	if c := a.consumoDoProduto(t, produto); c != "all_members" {
		t.Fatalf("o consumo virou %q apesar da recusa", c)
	}
}

// A MESMA decisão pela outra porta. Na rodada passada a guarda de `active`
// existia no DELETE e não no PATCH, e bastava trocar de verbo para contorná-la;
// aqui o PUT reenvia o cadastro inteiro e carregaria `consumes` junto.
func TestTrocarConsumesDeProdutoVendidoPeloPutEhRecusado(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_cons_put", celulaVer, celulaEditar))

	produto, _, _ := a.casaVendida(t, "all_members", 3)
	var codigo string
	if err := a.pool.QueryRow(a.ctx, `SELECT code FROM unit_types WHERE id = $1`, produto).Scan(&codigo); err != nil {
		t.Fatalf("lendo o código do produto: %v", err)
	}

	r := a.chamar(t, http.MethodPut, "/unit-types/"+produto.String(), gestor, map[string]any{
		"code": codigo, "name": "Produto de integração", "capacity": 4,
		"consumes": "one_member", "cleaning_fee_cents": 0, "sort_order": 900, "active": true,
	})
	if r.Status != http.StatusConflict {
		t.Fatalf("PUT trocando consumes com venda viva = %d, esperado 409 (%s)", r.Status, r.Corpo)
	}
	if c := a.consumoDoProduto(t, produto); c != "all_members" {
		t.Fatalf("o consumo virou %q apesar da recusa", c)
	}
}

// O caminho oposto quebra a invariante ao contrário: a venda de UM apartamento
// passaria a pertencer a um produto que declara o conjunto inteiro.
func TestTrocarConsumesDeOneMemberVendidoParaAllMembersEhRecusado(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_cons_inv", celulaVer, celulaEditar))

	produto, _ := a.produtoDireto(t, "one_member")
	u1 := a.unidadeDireta(t, "IT-O1-"+sufixo())
	u2 := a.unidadeDireta(t, "IT-O2-"+sufixo())
	a.compor(t, produto, u1, u2)
	reserva := a.reserva(t, produto, "confirmed")
	bloco := a.bloqueioDaReserva(t, u1, reserva)
	a.vincular(t, reserva, u1, bloco)

	r := a.chamar(t, http.MethodPatch, "/unit-types/"+produto.String(), gestor,
		map[string]any{"consumes": "all_members"})
	if r.Status != http.StatusConflict {
		t.Fatalf("one_member → all_members com venda viva = %d, esperado 409 (%s)", r.Status, r.Corpo)
	}
	if c := a.consumoDoProduto(t, produto); c != "one_member" {
		t.Fatalf("o consumo virou %q apesar da recusa", c)
	}
}

// PATCH que não menciona `consumes` não é tentativa de troca — o Opt ausente é
// o terceiro estado, e confundi-lo com "trocar" faria todo PATCH de
// `sort_order` bater na guarda.
func TestPatchDeSortOrderPassaComVendaViva(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_cons_sort", celulaVer, celulaEditar))

	produto, _, _ := a.casaVendida(t, "all_members", 3)

	r := a.chamar(t, http.MethodPatch, "/unit-types/"+produto.String(), gestor,
		map[string]any{"sort_order": 42})
	if r.Status != http.StatusOK {
		t.Fatalf("PATCH de sort_order com venda viva = %d, esperado 200 (%s)", r.Status, r.Corpo)
	}
}

// ═══════════════════════ `one_member`: só a saída ocupada ═══════════════════

// Crescer o pool de um `one_member` com venda viva é operação NORMAL: a venda
// escolheu uma unidade no ato e a segurou. Recusar isso travaria a casa — o
// apto-2s quase sempre tem estadia futura de pé.
func TestAcrescentarUnidadeEmOneMemberComVendaVivaPassa(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_om_add", celulaVer, celulaEditar))

	produto, unidades, _ := a.casaVendida(t, "one_member", 2)
	nova := a.unidadeDireta(t, "IT-OM-"+sufixo())

	r := a.chamar(t, http.MethodPut, "/unit-types/"+produto.String()+"/members", gestor,
		map[string]any{"unit_ids": append(append([]uuid.UUID{}, unidades...), nova)})
	if r.Status != http.StatusOK {
		t.Fatalf("acrescentar unidade a one_member vendido = %d, esperado 200 (%s)", r.Status, r.Corpo)
	}
	if n := a.contarComposicao(t, produto); n != 3 {
		t.Fatalf("composição = %d, esperado 3", n)
	}
}

// O que o `one_member` recusa é tirar a unidade que uma estadia viva OCUPA: a
// venda continuaria hospedando numa unidade fora do próprio produto, e toda
// derivação produto → composição → unidades (limpeza, check-in, mapa) deixaria
// essa estadia de fora.
func TestRemoverUnidadeOcupadaDeOneMemberEhRecusadoNaAPI(t *testing.T) {
	a := subir(t)
	gestor := a.token(t, a.perfil(t, "inv_om_rem", celulaVer, celulaEditar))

	produto, unidades, reserva := a.casaVendida(t, "one_member", 2)

	r := a.chamar(t, http.MethodPut, "/unit-types/"+produto.String()+"/members", gestor,
		map[string]any{"unit_ids": unidades[1:]})
	if r.Status != http.StatusConflict {
		t.Fatalf("tirar unidade ocupada de one_member = %d, esperado 409 (%s)", r.Status, r.Corpo)
	}
	d := r.detalhes(t)
	ocupadas, _ := d["occupied_unit_codes"].([]any)
	if len(ocupadas) != 1 {
		t.Fatalf("details.occupied_unit_codes = %v, esperado a unidade que a estadia %s ocupa",
			d["occupied_unit_codes"], a.codigoDaReserva(t, reserva))
	}
	if n := a.contarComposicao(t, produto); n != 2 {
		t.Fatalf("a composição foi gravada apesar da recusa: %d, esperado 2", n)
	}
}

// ═══════════ Por que a EXCLUDE não bastava: o mecanismo, medido ═════════════

// TestUnidadeForaDaVendaFicaLivreDentroDaEstadiaExclusiva demonstra POR QUE a
// composição precisa de guarda no service.
//
// Ele monta à mão o estado que a API agora recusa criar — composição com uma
// unidade a mais do que a venda segura — e prova que a `stay_no_overlap` aceita
// uma ocupação concorrente naquela unidade DENTRO das datas da estadia
// exclusiva. É o `POST /reservations` do caminho relatado, reduzido ao INSERT
// que ele executa: a constraint não tem com o que colidir porque a linha que
// faltaria nunca foi inserida.
func TestUnidadeForaDaVendaFicaLivreDentroDaEstadiaExclusiva(t *testing.T) {
	a := subir(t)

	produto, _, reserva := a.casaVendida(t, "all_members", 3)
	// A nona unidade: na composição, sem bloco — o estado exato que o PUT
	// produzia antes da guarda.
	orfa := a.unidadeDireta(t, "IT-ORFA-"+sufixo())
	a.compor(t, produto, orfa)

	var intruso uuid.UUID
	err := a.pool.QueryRow(a.ctx, `
		INSERT INTO stay_blocks (property_id, unit_id, source, status, period)
		VALUES ($1, $2, 'reservation', 'confirmed',
		        daterange(current_date + 31, current_date + 32, '[)'))
		RETURNING id`, a.propriedade, orfa).Scan(&intruso)
	if err != nil {
		t.Fatalf("a inserção concorrente falhou por outro motivo: %v", err)
	}
	a.executar(t, `DELETE FROM stay_blocks WHERE id = $1`, intruso)

	// Não é uma falha do banco: é a demonstração de que o banco NÃO PODE
	// resolver isso sozinho com a EXCLUDE. Por isso a guarda mora no service, e
	// por isso o relatório pede a constraint trigger — a única forma de o banco
	// enxergar a ausência de uma linha é conferir o CONJUNTO no commit.
	t.Logf("a unidade fora da venda aceitou ocupação concorrente dentro de %s: "+
		"a EXCLUDE não vê ausência de linha, e é isso que a guarda de composição impede de existir",
		a.codigoDaReserva(t, reserva))
}
