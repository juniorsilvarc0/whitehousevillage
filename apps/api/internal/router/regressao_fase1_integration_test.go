//go:build integration

// Regressão dos achados que reprovaram a Fase 1 — na COSTURA entre os módulos.
//
// Cada módulo já guarda o próprio achado na própria suíte, e guarda bem. O que
// nenhuma delas alcança é o sistema montado: `internal/modules/*` sobe só as
// rotas do próprio módulo, e por isso um módulo pode consertar o defeito no
// lado dele e o defeito continuar de pé para quem usa o produto.
//
// Foi o que aconteceu com dois destes achados, e é por isso que este arquivo
// existe:
//
//   - o MÉDIO 7 é conferido em `reservas` por uma consulta SQL que o próprio
//     teste escreve (`status IN ('hold','confirmed','completed')`). O endpoint
//     do mapa não usa esse predicado. O teste do módulo afirma o predicado que
//     ele GOSTARIA que o mapa usasse — aqui perguntamos ao mapa;
//   - o ALTO 3 é conferido em cada módulo com o `audit.Middleware` montado à
//     mão pelo teste. O `router.New` de verdade não o monta.
//
// A regra vale para o arquivo inteiro: aqui a pergunta é sempre feita pela
// porta da frente, ao servidor real, com o corpo que o contrato declara.
package router

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// ─────────────────────────── Apoio ──────────────────────────────────────────

// operacao é o perfil que faz a casa girar: vende, cuida do inventário, mexe no
// calendário e orça. Escopo `all` de propósito — o que estes cenários medem é a
// REGRA, e escopo estreito esconderia a regra atrás de um 404.
func (a *ambiente) operacao(t *testing.T) usuarioDeTeste {
	t.Helper()

	var permissoes []auth.Permissao
	for _, recurso := range []string{"reservations", "calendar", "quotes", "inventory", "settings"} {
		for _, acao := range []string{auth.AcaoVer, auth.AcaoCriar, auth.AcaoEditar, auth.AcaoExcluir} {
			permissoes = append(permissoes, auth.Permissao{Resource: recurso, Action: acao, Scope: auth.EscopoAll})
		}
	}
	return a.criarUsuario(t, "operacao", a.criarPerfil(t, "operacao", permissoes))
}

// janelaLonge devolve uma janela de `noites` a partir de um deslocamento grande
// em dias. Data distante e determinística: longe de dado real, longe das datas
// da jornada (novembro/2026) e longe das dos outros cenários deste arquivo, para
// dois testes nunca disputarem a mesma unidade na mesma noite.
func janelaLonge(deslocamento, noites int) (string, string) {
	base := time.Date(2032, time.March, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, deslocamento)
	return base.Format("2006-01-02"), base.AddDate(0, 0, noites).Format("2006-01-02")
}

// mapaDeUnidades pergunta ao endpoint do mapa — o de verdade, o que a tela do
// calendário consome — o que aconteceu numa janela.
func (a *ambiente) mapaDeUnidades(t *testing.T, token, de, ate string) []linhaDoMapaQA {
	t.Helper()

	caminho := fmt.Sprintf("/availability/units?from=%s&to=%s", de, ate)
	return envelopeDe[[]linhaDoMapaQA](t, a.chamar(t, http.MethodGet, caminho, token, nil),
		http.StatusOK, "GET /availability/units")
}

// celulaDoMapaEm acha a célula de uma unidade num dia. Devolve `nil` quando a
// unidade não aparece no mapa, que é resposta diferente de "aparece livre".
func celulaDoMapaEm(mapa []linhaDoMapaQA, unidade, dia string) *celulaDoMapa {
	for _, linha := range mapa {
		if linha.UnitCode != unidade {
			continue
		}
		for i := range linha.Dias {
			if linha.Dias[i].Data == dia {
				return &linha.Dias[i]
			}
		}
	}
	return nil
}

// reservaEmCurso leva uma venda de zero a `checked_out` pela porta da frente —
// orçar não, vender, confirmar, entrar e sair. É o caminho que o MÉDIO 7 mede.
func (a *ambiente) reservaEmCurso(t *testing.T, u usuarioDeTeste, produto, contato uuid.UUID, de, ate string, sinal int64) reservaQA {
	t.Helper()

	reserva := envelopeDe[reservaQA](t, a.chamarComChave(t, http.MethodPost, "/reservations", u.Token, map[string]any{
		"unit_type_id": produto,
		"contact_id":   contato,
		"check_in":     de,
		"check_out":    ate,
		"guests_count": 2,
	}, a.chaveNova(t, "em-curso")), http.StatusCreated, "POST /reservations")

	base := "/reservations/" + reserva.ID.String()
	if r := a.chamarComChave(t, http.MethodPost, base+"/confirm", u.Token,
		map[string]any{"deposit_paid_cents": sinal}, a.chaveNova(t, "confirm")); r.Status != http.StatusOK {
		t.Fatalf("POST /confirm: status %d — corpo: %s", r.Status, r.Corpo)
	}
	return reserva
}

// ─────────────── MÉDIO 7 — a estadia cumprida no mapa ───────────────────────

// VERDE desde 26/08/2026 (rodada 3). Ficou vermelho uma rodada inteira porque
// `statusQueBloqueiam = {hold, confirmed}` servia às DUAS consultas — a de
// conflito e a do mapa. Hoje `disponibilidade` tem dois predicados nomeados:
// venda (`hold, confirmed`) e exibição (`hold, confirmed, completed`). O teste
// fica como REGRESSÃO: é o único lugar que impede os dois de voltarem a ser um.
//
// TestOMapaContinuaMostrandoQueHouveHospedeDepoisDoCheckOut.
//
// EM LINGUAGEM DE NEGÓCIO: o hóspede dormiu três noites, pagou e foi embora. No
// dia seguinte a gestão abre o mapa de ocupação daquela semana e as três noites
// aparecem como se ninguém nunca tivesse estado lá. É a fonte de ocupação, de
// ADR e de RevPAR: toda receita realizada da casa some do relatório, e a estadia
// consumada fica indistinguível da venda que o hóspede cancelou sem chegar.
//
// A janela é a mesma coisa vista de dois ângulos, e os dois têm de valer ao
// mesmo tempo:
//
//	inventário (vender de novo)  → a noite está LIVRE
//	mapa       (o que aconteceu) → a noite teve HÓSPEDE
//
// Confundir os dois predicados é o erro nos dois sentidos: com `completed`
// dentro do predicado de venda, o check-out trava a noite seguinte (overbooking
// ao contrário); com `completed` fora do predicado do mapa, a história some.
func TestOMapaContinuaMostrandoQueHouveHospedeDepoisDoCheckOut(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	u := a.operacao(t)
	contato := a.hospedeDaJornada(t, propriedade)
	apto := a.produtoDoSeed(t, propriedade, "apto-2s")

	de, ate := janelaLonge(0, 3)
	reserva := a.reservaEmCurso(t, u, apto, contato, de, ate, 100000)
	base := "/reservations/" + reserva.ID.String()

	if len(reserva.Unidades) != 1 {
		t.Fatalf("a venda de um apartamento alocou %d unidades, esperado 1", len(reserva.Unidades))
	}
	unidade := reserva.Unidades[0].UnitCode

	// O mapa mostra o hóspede ANTES do check-out. Sem este controle o teste
	// passaria num sistema em que o mapa nunca mostra nada.
	antes := celulaDoMapaEm(a.mapaDeUnidades(t, u.Token, de, ate), unidade, de)
	if antes == nil || antes.ReservaCodigo == nil || *antes.ReservaCodigo != reserva.Codigo {
		t.Fatalf("antes do check-out o mapa já não mostrava a reserva %s em %s/%s: %+v",
			reserva.Codigo, unidade, de, antes)
	}

	// O `at` vai explícito porque a janela é distante: a entrada e a saída são
	// validadas contra o período VENDIDO, e o `now()` implícito de hoje cairia
	// fora dele. Entrada na tarde do primeiro dia, saída na manhã do dia da
	// partida — o teto do check-out é inclusivo (a diária do dia da saída não
	// foi vendida, mas o hóspede ainda está na casa de manhã).
	entrada := map[string]any{"at": de + "T15:00:00-03:00"}
	saida := map[string]any{"at": ate + "T10:00:00-03:00"}
	if r := a.chamar(t, http.MethodPost, base+"/check-in", u.Token, entrada); r.Status != http.StatusOK {
		t.Fatalf("POST /check-in: status %d — corpo: %s", r.Status, r.Corpo)
	}
	if r := a.chamar(t, http.MethodPost, base+"/check-out", u.Token, saida); r.Status != http.StatusOK {
		t.Fatalf("POST /check-out: status %d — corpo: %s", r.Status, r.Corpo)
	}

	// ── A metade que o negócio precisa ver: a estadia continua no mapa ──────
	depois := a.mapaDeUnidades(t, u.Token, de, ate)
	inicio, err := time.Parse("2006-01-02", de)
	if err != nil {
		t.Fatalf("data da janela: %v", err)
	}
	for n := 0; n < 3; n++ {
		dia := inicio.AddDate(0, 0, n).Format("2006-01-02")
		celula := celulaDoMapaEm(depois, unidade, dia)
		if celula == nil {
			t.Fatalf("%s: a unidade %s sumiu do mapa depois do check-out", dia, unidade)
		}
		if celula.ReservaCodigo == nil || *celula.ReservaCodigo != reserva.Codigo {
			t.Errorf("%s em %s: o mapa devolveu status %q e reserva %v depois do check-out — "+
				"a estadia CUMPRIDA de %s evaporou do mapa, que é a fonte de ocupação, ADR e RevPAR",
				unidade, dia, celula.Status, textoOuNulo(celula.ReservaCodigo), reserva.Codigo)
		}
	}

	// ── A outra metade: a noite voltou a ser vendável ───────────────────────
	//
	// Sem este controle, "o mapa mostra o hóspede" seria satisfeito por um
	// sistema que simplesmente não libera a data — que é o defeito oposto e
	// custa mais caro, porque some com o estoque.
	revenda := a.chamarComChave(t, http.MethodPost, "/reservations", u.Token, map[string]any{
		"unit_type_id": apto,
		"contact_id":   contato,
		"check_in":     de,
		"check_out":    ate,
		"guests_count": 2,
	}, a.chaveNova(t, "revenda"))
	if revenda.Status != http.StatusCreated {
		t.Fatalf("depois do check-out a mesma janela recusou uma venda nova (status %d): a estadia "+
			"encerrada continua segurando o estoque — corpo: %s", revenda.Status, revenda.Corpo)
	}
}

func textoOuNulo(s *string) string {
	if s == nil {
		return "null"
	}
	return *s
}

// ─────────── CRÍTICO 1 — a exclusividade da casa completa ───────────────────

// TestNenhumCaminhoDeixaHospedeEstranhoDentroDaCasaExclusiva.
//
// EM LINGUAGEM DE NEGÓCIO: a Casa Completa é a casa INTEIRA — capacidade 24,
// R$ 5.500 a diária, oito unidades. Vendê-la com sete significa que alguém
// pagou pela casa toda e vai encontrar um estranho dormindo num dos quartos.
//
// A revisão mediu o caminho de três passos que produzia isso:
//
//  1. desativar AP-03 no inventário            (parecia inofensivo)
//  2. vender a Completa                        (saía com 7 e o preço de 8)
//  3. reativar AP-03                           (a oitava ficava livre para outra venda)
//
// Cada módulo fechou a sua porta. O que ninguém verificou é que as três estão
// fechadas AO MESMO TEMPO no sistema montado — e basta uma aberta para o
// caminho voltar a existir. Este teste percorre os três passos em sequência,
// pela porta da frente, e exige que o primeiro que puder recusar recuse.
//
// Ele reprova se alguém reintroduzir o filtro `u.active` na consulta das
// candidatas: com o filtro de volta, a Completa é vendida com as sete unidades
// ativas em vez de recusada, e o passo 2 responde 201.
func TestNenhumCaminhoDeixaHospedeEstranhoDentroDaCasaExclusiva(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	u := a.operacao(t)
	contato := a.hospedeDaJornada(t, propriedade)
	completa := a.produtoDoSeed(t, propriedade, "completa")
	de, ate := janelaLonge(40, 3)

	membros := a.membrosDoProduto(t, completa)
	if len(membros) != 8 {
		t.Fatalf("a Casa Completa do seed declara %d unidades, esperado 8 — o cenário não alcança o defeito",
			len(membros))
	}
	alvo := a.unidadeDoSeed(t, propriedade, "AP-03")

	// ── Porta 1: o inventário recusa tirar do ar uma unidade que compõe ─────
	desativar := a.chamar(t, http.MethodPatch, "/units/"+alvo.String(), u.Token, map[string]any{"active": false})
	restaurar := func() {
		a.executarQA(t, `UPDATE units SET active = true WHERE id = $1`, alvo)
	}
	t.Cleanup(restaurar)

	if desativar.Status == http.StatusOK || desativar.Status == http.StatusNoContent {
		t.Errorf("PORTA 1 ABERTA: o inventário deixou tirar AP-03 do ar (status %d) mesmo ela compondo "+
			"a Casa Completa — corpo: %s", desativar.Status, desativar.Corpo)
	} else if desativar.Status != http.StatusConflict {
		t.Errorf("PATCH /units {active:false} devolveu %d, esperado 409 RESOURCE_IN_USE — corpo: %s",
			desativar.Status, desativar.Corpo)
	}

	// ── Porta 2: mesmo com a unidade fora do ar, a venda é recusada ─────────
	//
	// Chegamos ao estado pelo BANCO, e não pela API, de propósito: a porta 1
	// pode estar fechada e o dado legado (unidade já inativa antes desta rodada,
	// ou importação de OTA) continuar existindo. É a porta 2 que protege esse
	// dado — e é ela que o CRÍTICO 1 era.
	a.executarQA(t, `UPDATE units SET active = false WHERE id = $1`, alvo)

	venda := a.chamarComChave(t, http.MethodPost, "/reservations", u.Token, map[string]any{
		"unit_type_id": completa,
		"contact_id":   contato,
		"check_in":     de,
		"check_out":    ate,
		"guests_count": 10,
	}, a.chaveNova(t, "completa-incompleta"))

	if venda.Status == http.StatusCreated {
		vendida := envelopeDe[reservaQA](t, venda, http.StatusCreated, "POST /reservations")
		t.Fatalf("PORTA 2 ABERTA — A CASA FOI VENDIDA PELA METADE: a Casa Completa saiu com %d unidades "+
			"(%s) por %d centavos, o preço da casa inteira. Quem pagou vai encontrar AP-03 vazia hoje e "+
			"um estranho dentro dela amanhã", len(vendida.Unidades), codigosDas(vendida), vendida.Total)
	}
	if venda.Status != http.StatusUnprocessableEntity {
		t.Errorf("vender a Completa com uma unidade fora do ar devolveu %d, esperado 422 "+
			"COMPOSITION_INCOMPLETE — corpo: %s", venda.Status, venda.Corpo)
	}
	if !strings.Contains(string(venda.Corpo), "COMPOSITION_INCOMPLETE") {
		t.Errorf("a recusa não usou COMPOSITION_INCOMPLETE — corpo: %s", venda.Corpo)
	}
	// A recusa tem de dizer QUAL unidade falta: sem isso o operador não tem
	// ação, e 422 vira "tente outra data" para sempre — que é justamente o
	// motivo de o código não ser 409.
	if !strings.Contains(string(venda.Corpo), "AP-03") {
		t.Errorf("a recusa não nomeou a unidade que falta (AP-03) — o operador fica sem ação: %s", venda.Corpo)
	}

	// ── Porta 3: reativar não pode legalizar uma venda já emitida sem ela ───
	//
	// Aqui o passo 2 recusou, então não há venda incompleta e a reativação tem
	// de PASSAR — é o controle positivo. Uma guarda que recusasse toda
	// reativação satisfaria o teste sem proteger ninguém.
	reativar := a.chamar(t, http.MethodPatch, "/units/"+alvo.String(), u.Token, map[string]any{"active": true})
	if reativar.Status != http.StatusOK {
		t.Errorf("com a Completa NÃO vendida, reativar AP-03 devolveu %d, esperado 200 — uma guarda que "+
			"recusa toda reativação tranca o inventário sem proteger venda nenhuma: %s",
			reativar.Status, reativar.Corpo)
	}

	// ── E a prova final, no banco: nenhuma venda exclusiva ficou capenga ────
	//
	// A invariante de `all_members` é de CONJUNTO, não de contagem. A versão
	// anterior desta varredura comparava `count(reservation_units)` com
	// `count(unit_type_members)` e era CEGA para o caminho mais provável: trocar
	// uma unidade por outra na composição deixa as contagens IGUAIS e os
	// conjuntos diferentes. Medido neste banco, no mesmo instante:
	//
	//	composicao completa    = AP-01,AP-02,AP-99,COB-01,SP-01,SP-02,SP-03,SP-04
	//	reservation_units 0083 = AP-01,AP-02,AP-03,COB-01,SP-01,SP-02,SP-03,SP-04
	//	varredura por contagem = 0   ← verde num banco quebrado
	//	varredura por conjunto = 1
	//
	// A consulta abaixo devolve as DUAS diferenças, com o código da reserva e as
	// unidades, porque "1 venda capenga" não diz a ninguém onde olhar:
	//
	//	`faltando` — unidade da composição que a venda NÃO ocupa. É o dano caro:
	//	a casa foi alugada inteira e esse quarto continua vendável para um
	//	estranho.
	//	`sobrando` — unidade que a venda ocupa e que saiu da composição. A
	//	estadia hospeda fora do próprio produto, e toda derivação
	//	produto→composição→unidades (limpeza, check-in, mapa) a ignora.
	linhas, err := a.pool.Query(a.ctx, `
		SELECT r.code,
		       COALESCE((SELECT string_agg(u.code, ',' ORDER BY u.code)
		                   FROM unit_type_members m JOIN units u ON u.id = m.unit_id
		                  WHERE m.unit_type_id = ut.id
		                    AND NOT EXISTS (SELECT 1 FROM reservation_units ru
		                                     WHERE ru.reservation_id = r.id AND ru.unit_id = m.unit_id)), ''),
		       COALESCE((SELECT string_agg(u.code, ',' ORDER BY u.code)
		                   FROM reservation_units ru JOIN units u ON u.id = ru.unit_id
		                  WHERE ru.reservation_id = r.id
		                    AND NOT EXISTS (SELECT 1 FROM unit_type_members m
		                                     WHERE m.unit_type_id = ut.id AND m.unit_id = ru.unit_id)), '')
		  FROM reservations r
		  JOIN unit_types ut ON ut.id = r.unit_type_id AND ut.consumes = 'all_members'
		 WHERE r.property_id = $1
		   AND r.status NOT IN ('quote','cancelled','no_show','expired')
		   AND (EXISTS (SELECT 1 FROM unit_type_members m
		                 WHERE m.unit_type_id = ut.id
		                   AND NOT EXISTS (SELECT 1 FROM reservation_units ru
		                                    WHERE ru.reservation_id = r.id AND ru.unit_id = m.unit_id))
		     OR EXISTS (SELECT 1 FROM reservation_units ru
		                 WHERE ru.reservation_id = r.id
		                   AND NOT EXISTS (SELECT 1 FROM unit_type_members m
		                                    WHERE m.unit_type_id = ut.id AND m.unit_id = ru.unit_id)))
		 ORDER BY r.code`, propriedade)
	if err != nil {
		t.Fatalf("varrendo vendas exclusivas: %v", err)
	}
	defer linhas.Close()

	var capengas int
	for linhas.Next() {
		var codigo, faltando, sobrando string
		if err := linhas.Scan(&codigo, &faltando, &sobrando); err != nil {
			t.Fatalf("lendo venda capenga: %v", err)
		}
		capengas++
		t.Errorf("venda de produto exclusivo %s não bate com a composição do produto: "+
			"unidades da composição que ela NÃO ocupa = [%s] (cada uma é um quarto da casa alugada "+
			"inteira que o sistema ainda acha vago); unidades que ela ocupa e que saíram da composição "+
			"= [%s]", codigo, faltando, sobrando)
	}
	if err := linhas.Err(); err != nil {
		t.Fatalf("varrendo vendas exclusivas: %v", err)
	}
	if capengas > 0 {
		t.Errorf("%d venda(s) exclusiva(s) capenga(s) no banco", capengas)
	}
}

func codigosDas(r reservaQA) string {
	var lista []string
	for _, u := range r.Unidades {
		lista = append(lista, u.UnitCode)
	}
	return strings.Join(lista, " ")
}

func (a *ambiente) membrosDoProduto(t *testing.T, produto uuid.UUID) []string {
	t.Helper()

	linhas, err := a.pool.Query(a.ctx, `
		SELECT u.code FROM unit_type_members m
		  JOIN units u ON u.id = m.unit_id
		 WHERE m.unit_type_id = $1 ORDER BY u.code`, produto)
	if err != nil {
		t.Fatalf("membros do produto: %v", err)
	}
	defer linhas.Close()

	var codigos []string
	for linhas.Next() {
		var c string
		if err := linhas.Scan(&c); err != nil {
			t.Fatalf("lendo membro: %v", err)
		}
		codigos = append(codigos, c)
	}
	return codigos
}

func (a *ambiente) unidadeDoSeed(t *testing.T, propriedade uuid.UUID, codigo string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM units WHERE property_id = $1 AND code = $2`, propriedade, codigo).Scan(&id); err != nil {
		t.Fatalf("unidade %q do seed: %v", codigo, err)
	}
	return id
}

// ─────────────── ALTO 3 — a trilha do sistema montado ───────────────────────

// VERDE desde 26/08/2026 (rodada 3): `router.New` passou a montar
// `audit.Middleware`, depois do `httpx.RealIP` — antes dele o IP ainda não foi
// resolvido — e FORA do grupo protegido, porque escrita de rota pública (troca
// de senha) também deixa trilha. O teste fica como REGRESSÃO: nenhuma suíte de
// módulo o substitui, porque todas montam o middleware à mão.
//
// O IP que chega aqui é medido à parte, em
// `TestATrilhaNaoRegistraOIPQueOAutorDaEscritaEscolheu`: registrar a origem só
// vale se a origem não puder ser escolhida por quem faz a escrita.
//
// TestTodaEscritaDaFase1DeixaTrilhaCompletaNoSistemaMontado.
//
// EM LINGUAGEM DE NEGÓCIO: quando alguém pergunta "quem tirou AP-03 do ar na
// véspera do Réveillon?" ou "quem triplicou a diária da Cobertura?", a resposta
// tem de estar em `audit_log` — com nome, hora, o valor de antes, o valor de
// depois, o endereço de onde partiu e o `request_id` que amarra a linha ao log
// da requisição.
//
// Cada módulo já prova que grava a trilha. Nenhum deles prova pelo SERVIDOR
// REAL: as suítes de módulo montam `audit.Middleware` à mão. É a diferença
// entre "o pacote de auditoria funciona" e "o produto audita" — e ela aparece
// exatamente nas duas colunas que só o middleware preenche.
func TestTodaEscritaDaFase1DeixaTrilhaCompletaNoSistemaMontado(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	u := a.operacao(t)
	contato := a.hospedeDaJornada(t, propriedade)
	apto := a.produtoDoSeed(t, propriedade, "apto-2s")
	de, ate := janelaLonge(80, 2)

	// O recorte nasce do relógio do BANCO, e não do processo de teste: `at` é
	// preenchido pelo Postgres, e qualquer diferença entre o relógio do host e o
	// da VM do Docker comeria as primeiras linhas do recorte. O sintoma dessa
	// diferença é uma acusação grave e falsa — "a criação da reserva não deixou
	// rastro" — e ela apareceu uma vez na suíte completa desta rodada sem se
	// reproduzir em execução isolada.
	desde := a.agoraNoBanco(t)
	a.limparTrilhaDe(t, u.ID)

	// Um punhado de escritas da Fase 1, cada uma de um módulo diferente e todas
	// pela porta da frente — é isso que separa este teste dos de módulo.
	reserva := a.reservaEmCurso(t, u, apto, contato, de, ate, 50000)
	if r := a.chamar(t, http.MethodPatch, "/reservations/"+reserva.ID.String(), u.Token,
		map[string]any{"notes": "trilha da Fase 1"}); r.Status != http.StatusOK {
		t.Fatalf("PATCH /reservations: status %d — corpo: %s", r.Status, r.Corpo)
	}

	linhas := a.trilhaDe(t, u.ID, desde)
	if len(linhas) == 0 {
		t.Fatalf("depois de criar, confirmar e editar uma reserva pela API, `audit_log` não tem NENHUMA "+
			"linha do ator %s — as escritas da Fase 1 acontecem sem deixar rastro", u.ID)
	}

	acoes := map[string]bool{}
	for _, l := range linhas {
		acoes[l.Acao] = true
	}
	for _, esperada := range []string{"reservations.criado", "reservations.confirmada", "reservations.alterado"} {
		if !acoes[esperada] {
			t.Errorf("`audit_log` não registrou %q — as ações gravadas foram %v", esperada, chavesDe(acoes))
		}
	}

	// Toda linha tem de responder às três perguntas do auditor: QUEM, SOBRE O
	// QUE, e DE ONDE. Um campo nulo aqui é uma resposta que não existe.
	for _, l := range linhas {
		if l.Entidade == "" || l.EntidadeID == nil {
			t.Errorf("%s: linha sem entidade (entity=%q, entity_id=%v) — não dá para saber sobre o que ela fala",
				l.Acao, l.Entidade, l.EntidadeID)
		}
		if l.PropriedadeID == nil || *l.PropriedadeID != propriedade {
			t.Errorf("%s: property_id = %v, esperado %s — trilha sem propriedade não separa uma casa da outra",
				l.Acao, l.PropriedadeID, propriedade)
		}
		if l.RequestID == nil || *l.RequestID == "" {
			t.Errorf("%s: `request_id` nulo — a linha da trilha não se amarra à requisição que a produziu, "+
				"e investigar um incidente vira leitura de log por horário", l.Acao)
		}
		if l.IP == nil || *l.IP == "" {
			t.Errorf("%s: `ip` nulo — a trilha não diz DE ONDE a escrita partiu. `audit.Middleware` não está "+
				"montado em `router.New`: as suítes de módulo o montam à mão e por isso passam verde", l.Acao)
		}
		if l.UserAgent == nil || *l.UserAgent == "" {
			t.Errorf("%s: `user_agent` nulo — mesma causa do `ip` nulo", l.Acao)
		}
	}

	// E o que a trilha NUNCA pode carregar. O filtro do pacote `audit` é por
	// nome de campo; aqui a pergunta é a do vazamento, feita ao banco: existe
	// alguma linha desta sessão com cara de segredo dentro?
	var vazando int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM audit_log
		 WHERE at >= $1
		   AND (COALESCE(before::text,'') || COALESCE(after::text,'')) ~* $2`,
		desde, `\$argon2|\$2[aby]\$|eyJ[A-Za-z0-9_-]{10,}|"(password|senha|token|secret|api_key)"\s*:\s*"(?!\[redigido\])`,
	).Scan(&vazando); err != nil {
		t.Fatalf("varrendo a trilha atrás de segredo: %v", err)
	}
	if vazando != 0 {
		t.Errorf("%d linha(s) de `audit_log` gravadas nesta sessão contêm hash de senha, token JWT ou campo "+
			"de segredo em claro — a trilha de auditoria virou o lugar mais fácil de roubar credencial", vazando)
	}
}

type linhaDaTrilha struct {
	Acao          string
	Entidade      string
	EntidadeID    *uuid.UUID
	PropriedadeID *uuid.UUID
	IP            *string
	UserAgent     *string
	RequestID     *string
}

func (a *ambiente) trilhaDe(t *testing.T, ator uuid.UUID, desde time.Time) []linhaDaTrilha {
	t.Helper()

	linhas, err := a.pool.Query(a.ctx, `
		SELECT action, entity, entity_id, property_id, host(ip), user_agent, request_id
		  FROM audit_log WHERE actor_id = $1 AND at >= $2 ORDER BY at`, ator, desde)
	if err != nil {
		t.Fatalf("lendo a trilha: %v", err)
	}
	defer linhas.Close()

	var saida []linhaDaTrilha
	for linhas.Next() {
		var l linhaDaTrilha
		if err := linhas.Scan(&l.Acao, &l.Entidade, &l.EntidadeID, &l.PropriedadeID,
			&l.IP, &l.UserAgent, &l.RequestID); err != nil {
			t.Fatalf("lendo linha da trilha: %v", err)
		}
		saida = append(saida, l)
	}
	return saida
}

// limparTrilhaDe também salva o `t.Cleanup` de `criarUsuario`: `audit_log.actor_id`
// referencia `users` SEM `ON DELETE`, então um usuário com rastro guardado não
// se apaga (`23503`) e o teste deixaria conta órfã no banco a cada execução.
func (a *ambiente) limparTrilhaDe(t *testing.T, ator uuid.UUID) {
	t.Helper()

	a.executarQA(t, `DELETE FROM audit_log WHERE actor_id = $1`, ator)
	t.Cleanup(func() { a.executarQA(t, `DELETE FROM audit_log WHERE actor_id = $1`, ator) })
}

func chavesDe(m map[string]bool) []string {
	var saida []string
	for k := range m {
		saida = append(saida, k)
	}
	return saida
}

// ─────────────── ALTO 2 — o sinal e a devolução ─────────────────────────────

// TestACasaNuncaDevolveMaisDinheiroDoQueOHospedePagou.
//
// EM LINGUAGEM DE NEGÓCIO: a revisão registrou um sinal de R$ 99.999.999,99
// numa reserva de R$ 1.880,00 — aceito com 200 — e o cancelamento devolveu
// R$ 50.000.000,00 sobre uma diária de fim de semana. O teto do sinal e o teto
// da devolução são a mesma promessa vista de dois lados, e por isso os dois são
// medidos aqui: um sistema que só validasse a entrada continuaria emitindo
// ordem de devolução absurda para as linhas gravadas antes da correção.
func TestACasaNuncaDevolveMaisDinheiroDoQueOHospedePagou(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	u := a.operacao(t)
	contato := a.hospedeDaJornada(t, propriedade)
	apto := a.produtoDoSeed(t, propriedade, "apto-2s")
	de, ate := janelaLonge(120, 2)

	reserva := envelopeDe[reservaQA](t, a.chamarComChave(t, http.MethodPost, "/reservations", u.Token,
		map[string]any{
			"unit_type_id": apto, "contact_id": contato,
			"check_in": de, "check_out": ate, "guests_count": 2,
		}, a.chaveNova(t, "teto-sinal")), http.StatusCreated, "POST /reservations")
	base := "/reservations/" + reserva.ID.String()

	// ── Acima do total: recusado ────────────────────────────────────────────
	absurdo := reserva.Total*100 + 1
	acima := a.chamarComChave(t, http.MethodPost, base+"/confirm", u.Token,
		map[string]any{"deposit_paid_cents": absurdo}, a.chaveNova(t, "acima"))
	if acima.Status != http.StatusUnprocessableEntity {
		t.Errorf("confirmar com sinal de %d centavos numa reserva de %d devolveu %d, esperado 422 — "+
			"um dígito a mais no caixa vira ordem de devolução de cem vezes o valor da estadia: %s",
			absurdo, reserva.Total, acima.Status, acima.Corpo)
	}

	// ── Zero: recusado. Confirmar sem dinheiro é o que o `hold` já faz ──────
	semDinheiro := a.chamarComChave(t, http.MethodPost, base+"/confirm", u.Token,
		map[string]any{"deposit_paid_cents": 0}, a.chaveNova(t, "zero"))
	if semDinheiro.Status != http.StatusUnprocessableEntity {
		t.Errorf("confirmar com sinal zero devolveu %d, esperado 422 — corpo: %s",
			semDinheiro.Status, semDinheiro.Corpo)
	}

	// ── O total inteiro adiantado: LEGÍTIMO. Este é o controle positivo ─────
	//
	// Sem ele, uma trava que recusasse todo sinal alto passaria no teste e
	// impediria o hóspede de pagar a estadia inteira antes de chegar.
	quitado := a.chamarComChave(t, http.MethodPost, base+"/confirm", u.Token,
		map[string]any{"deposit_paid_cents": reserva.Total}, a.chaveNova(t, "quitado"))
	if quitado.Status != http.StatusOK {
		t.Fatalf("pagar a estadia inteira adiantada (%d de %d) devolveu %d, esperado 200 — é pagamento "+
			"legítimo e a trava do teto não pode recusá-lo: %s",
			reserva.Total, reserva.Total, quitado.Status, quitado.Corpo)
	}

	// ── E a devolução jamais passa do que entrou ────────────────────────────
	simulacao := envelopeDe[cancelamentoQA](t, a.chamar(t, http.MethodPost, base+"/cancel?dry_run=1", u.Token, nil),
		http.StatusOK, "POST /cancel?dry_run=1")

	if simulacao.SinalPago != reserva.Total {
		t.Errorf("o cancelamento leu sinal pago = %d, esperado %d", simulacao.SinalPago, reserva.Total)
	}
	if simulacao.Devolucao > simulacao.SinalPago {
		t.Errorf("o cancelamento devolveria %d centavos sobre um sinal de %d — a casa pagaria para o "+
			"hóspede ter reservado", simulacao.Devolucao, simulacao.SinalPago)
	}
	if simulacao.Devolucao < 0 || simulacao.Retido < 0 {
		t.Errorf("devolução %d / retenção %d: valor negativo em dinheiro", simulacao.Devolucao, simulacao.Retido)
	}
	if soma := simulacao.Devolucao + simulacao.Retido; soma != simulacao.SinalPago {
		t.Errorf("devolvido %d + retido %d = %d, e o hóspede pagou %d — o dinheiro não fecha",
			simulacao.Devolucao, simulacao.Retido, soma, simulacao.SinalPago)
	}
}

// ─────────────── O contrato do orçamento ────────────────────────────────────

// VERDE desde 26/08/2026 (rodada 3): a tag virou `json:"guests_count"` e a tela
// de orçamento voltou a calcular. O teste fica como REGRESSÃO, e a ele se somou
// uma defesa que não depende de ninguém lembrar de conferir:
// `apps/admin/src/lib/api/corpo-de-escrita.test.ts` compara, sem banco, as
// chaves que o painel manda com as tags `json` dos DTOs do Go.
//
// TestOrcamentoAceitaOCampoQueOContratoEOPainelMandam.
//
// EM LINGUAGEM DE NEGÓCIO: o corretor preenche o formulário de orçamento, clica
// em calcular e recebe "Informe ao menos 1 hóspede" — com o campo preenchido. A
// tela de orçamento não funciona.
//
// A causa é uma correção que foi aplicada só de um lado: o mesmo dado tinha dois
// nomes (`guests` na criação, `guests_count` na edição e nas respostas), o
// contrato e o painel foram unificados em `guests_count`, e `POST /quotes`
// continuou exigindo `guests`. Como campo desconhecido agora é recusado, o
// painel manda o nome novo e a API responde 422 pelo nome velho.
//
// A asserção é contra o CONTRATO, não contra o código: `openapi.yaml` declara
// `required: [unit_type_id, check_in, check_out, guests_count]`.
func TestOrcamentoAceitaOCampoQueOContratoEOPainelMandam(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	u := a.operacao(t)
	cobertura := a.produtoDoSeed(t, propriedade, "cobertura")
	de, ate := janelaLonge(160, 3)

	r := a.chamar(t, http.MethodPost, "/quotes", u.Token, map[string]any{
		"unit_type_id": cobertura,
		"check_in":     de,
		"check_out":    ate,
		"guests_count": 4,
	})
	if r.Status != http.StatusOK {
		t.Fatalf("POST /quotes com o corpo que o contrato declara devolveu %d — a tela de orçamento do "+
			"painel manda exatamente este corpo e recebe esta resposta, ou seja, não calcula orçamento "+
			"nenhum. Corpo: %s", r.Status, r.Corpo)
	}

	orcamento := envelopeDe[orcamentoQA](t, r, http.StatusOK, "POST /quotes")
	if orcamento.Noites != 3 {
		t.Errorf("orçamento de %s a %s deu %d noites, esperado 3", de, ate, orcamento.Noites)
	}
	if orcamento.Total <= 0 {
		t.Errorf("orçamento fechou em %d centavos", orcamento.Total)
	}

	// O controle: o nome VELHO tem de ser recusado, e recusado como campo
	// desconhecido. Sem isso, a API poderia aceitar os dois nomes para sempre —
	// que é como a ambiguidade nasceu.
	velho := a.chamar(t, http.MethodPost, "/quotes", u.Token, map[string]any{
		"unit_type_id": cobertura,
		"check_in":     de,
		"check_out":    ate,
		"guests":       4,
	})
	if velho.Status == http.StatusOK {
		t.Errorf("POST /quotes ainda aceita o nome antigo `guests` (200) — dois nomes para o mesmo dado " +
			"é como o campo virou silenciosamente opcional na primeira vez")
	}
}
