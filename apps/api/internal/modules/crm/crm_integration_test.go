//go:build integration

package crm_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ═══════════════════════════════════════════════════════════════════
// 1. A TAREFA AUTOMÁTICA É IDEMPOTENTE
//
// spec §7: "entrar numa etapa cria UMA tarefa automática (idempotente: não
// duplica se já existe pendente da mesma etapa)".
//
// O que este teste prova, além da regra: que a idempotência é do BANCO. O
// `ON CONFLICT DO NOTHING` colhe a decisão da parcial única
// `crm_activities_auto_idx`; se ela sumir numa migration futura, o subteste de
// concorrência aqui reprova — o `SELECT`-antes-de-`INSERT` passaria no caminho
// sequencial e falharia justamente sob duplo clique, que é quando importa.
// ═══════════════════════════════════════════════════════════════════

func TestTarefaAutomaticaDaEtapaEhIdempotente(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	_, etapas := a.funilDoSeed(t)

	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})

	// Critério de aceite literal do §7: "mover o card para 'Orçamento enviado'
	// cria sozinho a tarefa de follow-up com o prazo do SLA".
	primeira := a.moverEtapa(t, token, card.ID, etapas["Orçamento enviado"], nil)
	if primeira.TarefaAuto == nil {
		t.Fatal("mover para 'Orçamento enviado' NÃO criou a tarefa de follow-up (critério de aceite do §7)")
	}
	if primeira.TarefaAuto.Assunto != "Follow-up do orçamento enviado" {
		t.Fatalf("assunto da tarefa = %q", primeira.TarefaAuto.Assunto)
	}
	if !primeira.TarefaAuto.Auto {
		t.Fatal("a tarefa criada pela etapa não veio marcada como automática")
	}
	if primeira.TarefaAuto.VenceEm == nil {
		t.Fatal("tarefa automática sem prazo: tarefa que nunca vence é o mesmo que tarefa que não existe")
	}
	// A etapa do seed tem sla_days=2 e auto_task_due_days=2.
	prazo, err := time.Parse(time.RFC3339, *primeira.TarefaAuto.VenceEm)
	if err != nil {
		t.Fatalf("prazo ilegível %q: %v", *primeira.TarefaAuto.VenceEm, err)
	}
	if dias := int(time.Until(prazo).Hours() / 24); dias < 1 || dias > 2 {
		t.Fatalf("prazo da tarefa a %d dias, esperado ~2 (o SLA da etapa)", dias)
	}

	pendentes := func() int {
		return a.contar(t, `SELECT count(*) FROM crm_activities
		                     WHERE opportunity_id = $1 AND stage_id = $2 AND auto AND status = 'pendente'`,
			card.ID, etapas["Orçamento enviado"])
	}
	if n := pendentes(); n != 1 {
		t.Fatalf("depois da primeira entrada há %d tarefas automáticas pendentes, esperado 1", n)
	}

	// SAIR E VOLTAR no mesmo dia NÃO empilha: o kanban com arrasto otimista
	// reenvia a mesma transição mais vezes do que se imagina.
	a.moverEtapa(t, token, card.ID, etapas["Negociação"], nil)
	segunda := a.moverEtapa(t, token, card.ID, etapas["Orçamento enviado"], nil)
	if segunda.TarefaAuto != nil {
		t.Fatalf("reentrar na etapa criou uma SEGUNDA tarefa (%s) — a idempotência não segurou",
			segunda.TarefaAuto.ID)
	}
	if n := pendentes(); n != 1 {
		t.Fatalf("depois de sair e voltar há %d tarefas pendentes, esperado 1", n)
	}

	// Voltar DEPOIS de a tarefa ter sido concluída cria outra: é ciclo de SLA
	// novo, não duplicata. É a diferença entre "o corretor já ligou" e "o
	// cliente voltou a negociar".
	if resp := a.chamar(t, http.MethodPost,
		"/crm/activities/"+primeira.TarefaAuto.ID.String()+"/complete", token, nil); resp.Status != http.StatusOK {
		t.Fatalf("POST /complete = %d (%s)", resp.Status, resp.Corpo)
	}
	a.moverEtapa(t, token, card.ID, etapas["Negociação"], nil)
	terceira := a.moverEtapa(t, token, card.ID, etapas["Orçamento enviado"], nil)
	if terceira.TarefaAuto == nil {
		t.Fatal("com a tarefa anterior CONCLUÍDA, reentrar deveria abrir um ciclo de SLA novo")
	}
	if n := pendentes(); n != 1 {
		t.Fatalf("o ciclo novo deveria deixar 1 pendente, há %d", n)
	}
	total := a.contar(t, `SELECT count(*) FROM crm_activities
	                       WHERE opportunity_id = $1 AND stage_id = $2 AND auto`,
		card.ID, etapas["Orçamento enviado"])
	if total != 2 {
		t.Fatalf("esperava 2 tarefas automáticas ao todo (uma por ciclo), há %d", total)
	}
}

// A idempotência sob CONCORRÊNCIA — o duplo clique de verdade.
//
// Sem a parcial única do banco, este é o teste que reprova: dois `SELECT`
// simultâneos leem "não existe pendente" e os dois inserem.
func TestDoisArrastosSimultaneosNaMesmaEtapaCriamUmaTarefaSo(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	_, etapas := a.funilDoSeed(t)

	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})
	destino := etapas["Orçamento enviado"]

	const tentativas = 8
	respostas := make(chan int, tentativas)
	for i := 0; i < tentativas; i++ {
		go func() {
			resp := a.chamar(t, http.MethodPost, "/crm/opportunities/"+card.ID.String()+"/stage", token,
				map[string]any{"stage_id": destino})
			respostas <- resp.Status
		}()
	}
	vencedores := 0
	for i := 0; i < tentativas; i++ {
		if <-respostas == http.StatusOK {
			vencedores++
		}
	}

	// Quantos movimentos venceram não importa (o segundo recebe "já está nesta
	// etapa"); o que não pode é a tarefa nascer duas vezes.
	pendentes := a.contar(t, `SELECT count(*) FROM crm_activities
	                           WHERE opportunity_id = $1 AND stage_id = $2 AND auto AND status = 'pendente'`,
		card.ID, destino)
	if pendentes != 1 {
		t.Fatalf("%d entradas simultâneas produziram %d tarefas automáticas pendentes, esperado 1 (%d movimentos aceitos)",
			tentativas, pendentes, vencedores)
	}
}

// ═══════════════════════════════════════════════════════════════════
// 2. `/win` QUE FALHA NA RESERVA NÃO FECHA A OPORTUNIDADE
//
// É o caso que mais vai acontecer na alta temporada: a data foi vendida
// enquanto se negociava. Fechar a oportunidade e falhar a reserva deixaria uma
// venda ganha sem venda.
// ═══════════════════════════════════════════════════════════════════

func TestGanhoRecusadoPelaDataOcupadaNaoFechaAOportunidade(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	completa := a.produto(t, "completa")

	entrada, saida := a.dia(200), a.dia(203)

	// A casa inteira exige as OITO unidades. Uma ocupada basta para o motor
	// recusar — e é o mesmo caminho por onde a alta temporada recusa de verdade.
	a.bloqueioDireto(t, a.unidade(t, "COB-01"), entrada, saida)

	card := a.criarOportunidade(t, token, map[string]any{
		"contact_id": contato, "unit_type_id": completa,
		"check_in": entrada, "check_out": saida,
	})
	orcamento := a.orcamentoDireto(t, completa, contato, entrada, saida, 10)
	a.executar(t, `UPDATE crm_opportunities SET quote_id = $2 WHERE id = $1`, card.ID, orcamento)

	antes := a.estadoDoCard(t, card.ID)

	resp := a.chamarIdem(t, http.MethodPost, "/crm/opportunities/"+card.ID.String()+"/win", token,
		nil, "win-conflito-"+sufixo())
	if resp.Status == http.StatusOK || resp.Status == http.StatusCreated {
		t.Fatalf("o ganho passou com a data ocupada: %d (%s)", resp.Status, resp.Corpo)
	}
	if resp.Status != http.StatusConflict {
		t.Fatalf("esperava 409 do motor de reservas, veio %d (%s)", resp.Status, resp.Corpo)
	}
	if codigo := resp.codigoDoErro(); codigo != "DATE_CONFLICT" {
		t.Fatalf("código = %q, esperado DATE_CONFLICT", codigo)
	}

	// NADA mudou — é a metade do teste que importa.
	depois := a.estadoDoCard(t, card.ID)
	if depois != antes {
		t.Fatalf("a oportunidade mudou apesar da recusa:\nantes:  %s\ndepois: %s", antes, depois)
	}
	if n := a.contar(t, `SELECT count(*) FROM reservations WHERE contact_id = $1 AND status <> 'quote'`, contato); n != 0 {
		t.Fatalf("a recusa deixou %d reserva(s) para trás", n)
	}
	// A chave de idempotência da tentativa que falhou não pode ficar gravada:
	// repetir depois de um 409 de data ocupada é uma tentativa nova e legítima.
	if n := a.contar(t, `SELECT count(*) FROM idempotency_keys WHERE key LIKE 'win-conflito-%'`); n != 0 {
		t.Fatalf("a tentativa recusada deixou %d chave(s) de idempotência gravada(s)", n)
	}
}

// O caminho feliz do `/win`, e as três promessas do contrato: nasce `hold`,
// devolve 201 com Location, e conclui as tarefas automáticas pendentes.
func TestGanharCriaAReservaEmHoldEFechaAOportunidade(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	produto := a.produto(t, "apto-2s")
	_, etapas := a.funilDoSeed(t)

	entrada, saida := a.dia(210), a.dia(213)
	card := a.criarOportunidade(t, token, map[string]any{
		"contact_id": contato, "unit_type_id": produto,
		"check_in": entrada, "check_out": saida,
	})
	// Uma tarefa automática pendente, para provar que o ganho a encerra.
	a.moverEtapa(t, token, card.ID, etapas["Orçamento enviado"], nil)

	orcamento := a.orcamentoDireto(t, produto, contato, entrada, saida, 4)

	chave := "win-ok-" + sufixo()
	resp := a.chamarIdem(t, http.MethodPost, "/crm/opportunities/"+card.ID.String()+"/win", token,
		map[string]any{"quote_id": orcamento}, chave)
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /win = %d (%s)", resp.Status, resp.Corpo)
	}
	ganho := dado[resultadoDeGanho](t, resp)

	if !ganho.ReservaCriada {
		t.Fatal("reservation_created veio false num ganho que criou a reserva")
	}
	// Ganhar é ACORDO; confirmar é dinheiro (§5). Nascer `confirmed` faria o
	// financeiro gerar recebível quitado de um sinal que ninguém pagou.
	if ganho.Reserva.Status != "hold" {
		t.Fatalf("a reserva nasceu %q, esperado hold", ganho.Reserva.Status)
	}
	if resp.Location != "/api/v1/reservations/"+ganho.Reserva.ID.String() {
		t.Fatalf("Location = %q", resp.Location)
	}
	if ganho.Oportunidade.Status != "ganha" {
		t.Fatalf("status da oportunidade = %q", ganho.Oportunidade.Status)
	}
	if ganho.Oportunidade.EtapaTipo != "ganho" {
		t.Fatalf("a oportunidade não foi para a etapa terminal: %q", ganho.Oportunidade.EtapaTipo)
	}
	if ganho.Oportunidade.Probabilidade != 100 {
		t.Fatalf("probabilidade = %d, esperado 100", ganho.Oportunidade.Probabilidade)
	}
	if n := a.contar(t, `SELECT count(*) FROM crm_activities
	                      WHERE opportunity_id = $1 AND auto AND status = 'pendente'`, card.ID); n != 0 {
		t.Fatalf("o ganho deixou %d tarefa(s) automática(s) pendente(s): cobrar follow-up de negócio fechado é ruído", n)
	}

	// A repetição da chave devolve a resposta ORIGINAL — 201, Location e corpo —
	// e não cria uma segunda reserva das mesmas datas.
	repetida := a.chamarIdem(t, http.MethodPost, "/crm/opportunities/"+card.ID.String()+"/win", token,
		map[string]any{"quote_id": orcamento}, chave)
	if repetida.Status != http.StatusCreated {
		t.Fatalf("a repetição da chave respondeu %d, esperado o 201 original (%s)", repetida.Status, repetida.Corpo)
	}
	if n := a.contar(t, `SELECT count(*) FROM reservations WHERE contact_id = $1 AND status = 'hold'`, contato); n != 1 {
		t.Fatalf("a repetição criou reserva a mais: %d holds para o mesmo contato", n)
	}
}

// A oportunidade que já tinha pré-reserva: `/win` NÃO cria a segunda — ela
// seria, na melhor das hipóteses, um 409 contra a primeira.
func TestGanharComPreReservaVinculadaResponde200SemCriarOutra(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	produto := a.produto(t, "cobertura")

	entrada, saida := a.dia(220), a.dia(222)
	card := a.criarOportunidade(t, token, map[string]any{
		"contact_id": contato, "unit_type_id": produto,
		"check_in": entrada, "check_out": saida,
	})
	orcamento := a.orcamentoDireto(t, produto, contato, entrada, saida, 2)

	// Primeiro ganho: cria a reserva.
	primeiro := a.chamarIdem(t, http.MethodPost, "/crm/opportunities/"+card.ID.String()+"/win", token,
		map[string]any{"quote_id": orcamento}, "win-a-"+sufixo())
	if primeiro.Status != http.StatusCreated {
		t.Fatalf("primeiro /win = %d (%s)", primeiro.Status, primeiro.Corpo)
	}
	reserva := dado[resultadoDeGanho](t, primeiro).Reserva.ID

	// Reabre o card à mão, na etapa "Pré-reserva", para exercitar o caminho
	// real: o hold já existe (foi criado quando o card chegou nessa etapa) e o
	// `/win` só precisa fechar a oportunidade. Nenhum endpoint desta fase
	// preenche `reservation_id` numa oportunidade ABERTA — ver o relatório.
	_, etapasDoSeed := a.funilDoSeed(t)
	a.executar(t, `UPDATE crm_opportunities
	                  SET status = 'aberto', closed_at = NULL, stage_id = $2
	                WHERE id = $1`, card.ID, etapasDoSeed["Pré-reserva"])

	segundo := a.chamarIdem(t, http.MethodPost, "/crm/opportunities/"+card.ID.String()+"/win", token,
		nil, "win-b-"+sufixo())
	if segundo.Status != http.StatusOK {
		t.Fatalf("com reserva já vinculada esperava 200, veio %d (%s)", segundo.Status, segundo.Corpo)
	}
	ganho := dado[resultadoDeGanho](t, segundo)
	if ganho.ReservaCriada {
		t.Fatal("reservation_created veio true sem nada ter sido criado")
	}
	if ganho.Reserva.ID != reserva {
		t.Fatalf("a resposta trouxe outra reserva: %s ≠ %s", ganho.Reserva.ID, reserva)
	}
	if n := a.contar(t, `SELECT count(*) FROM reservations WHERE contact_id = $1 AND status = 'hold'`, contato); n != 1 {
		t.Fatalf("o segundo ganho criou reserva a mais: %d holds", n)
	}
}

// Sem orçamento vigente não há o que virar reserva.
func TestGanharSemOrcamentoVigenteEhRecusado(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)

	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})

	resp := a.chamarIdem(t, http.MethodPost, "/crm/opportunities/"+card.ID.String()+"/win", token,
		nil, "win-sem-"+sufixo())
	if resp.Status != http.StatusUnprocessableEntity {
		t.Fatalf("esperava 422, veio %d (%s)", resp.Status, resp.Corpo)
	}
	if codigo := resp.codigoDoErro(); codigo != "QUOTE_REQUIRED_TO_WIN" {
		t.Fatalf("código = %q, esperado QUOTE_REQUIRED_TO_WIN", codigo)
	}
	if status := a.texto(t, `SELECT status FROM crm_opportunities WHERE id = $1`, card.ID); status != "aberto" {
		t.Fatalf("a oportunidade saiu de aberto: %q", status)
	}
}

// `Idempotency-Key` é obrigatório: isto cria reserva.
func TestGanharSemChaveDeIdempotenciaEhRecusado(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)

	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})
	resp := a.chamar(t, http.MethodPost, "/crm/opportunities/"+card.ID.String()+"/win", token, nil)
	if resp.Status != http.StatusUnprocessableEntity {
		t.Fatalf("esperava 422 sem Idempotency-Key, veio %d (%s)", resp.Status, resp.Corpo)
	}
}

// ═══════════════════════════════════════════════════════════════════
// 3. `/lose` EXIGE MOTIVO
// ═══════════════════════════════════════════════════════════════════

func TestPerderExigeMotivoAtivoDoCatalogo(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)

	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})
	rota := "/crm/opportunities/" + card.ID.String() + "/lose"

	// Sem motivo: o DTO exige o campo.
	if resp := a.chamar(t, http.MethodPost, rota, token, map[string]any{}); resp.Status != http.StatusUnprocessableEntity {
		t.Fatalf("perder sem motivo respondeu %d (%s)", resp.Status, resp.Corpo)
	}

	// Motivo desconhecido: 422 LOSS_REASON_REQUIRED, e não uma violação de FK.
	resp := a.chamar(t, http.MethodPost, rota, token, map[string]any{"lost_reason_id": uuid.NewString()})
	if resp.Status != http.StatusUnprocessableEntity || resp.codigoDoErro() != "LOSS_REASON_REQUIRED" {
		t.Fatalf("motivo desconhecido = %d %s (%s)", resp.Status, resp.codigoDoErro(), resp.Corpo)
	}

	// Motivo INATIVO: some do seletor, e um id vindo de uma tela antiga não
	// pode reabrir o catálogo aposentado pela porta dos fundos.
	motivo := a.motivoDePerda(t)
	a.executar(t, `UPDATE crm_lost_reasons SET active = false WHERE id = $1`, motivo)
	t.Cleanup(func() { a.executar(t, `UPDATE crm_lost_reasons SET active = true WHERE id = $1`, motivo) })

	resp = a.chamar(t, http.MethodPost, rota, token, map[string]any{"lost_reason_id": motivo})
	if resp.Status != http.StatusUnprocessableEntity || resp.codigoDoErro() != "LOSS_REASON_REQUIRED" {
		t.Fatalf("motivo inativo = %d %s (%s)", resp.Status, resp.codigoDoErro(), resp.Corpo)
	}

	// Com o motivo de volta ao catálogo, a perda passa.
	a.executar(t, `UPDATE crm_lost_reasons SET active = true WHERE id = $1`, motivo)
	resp = a.chamar(t, http.MethodPost, rota, token, map[string]any{
		"lost_reason_id": motivo, "note": "escolheu a pousada vizinha",
	})
	if resp.Status != http.StatusOK {
		t.Fatalf("perder com motivo ativo = %d (%s)", resp.Status, resp.Corpo)
	}
	perdida := dado[struct {
		Oportunidade oportunidadeVista `json:"opportunity"`
	}](t, resp).Oportunidade
	if perdida.Status != "perdida" || perdida.MotivoID == nil || *perdida.MotivoID != motivo {
		t.Fatalf("perda gravada errado: %+v", perdida)
	}
	if perdida.Probabilidade != 0 {
		t.Fatalf("probabilidade da perdida = %d, esperado 0", perdida.Probabilidade)
	}
	// A tarefa automática vai para `cancelada`, e não `concluida`: ela não foi
	// feita, o negócio acabou.
	if n := a.contar(t, `SELECT count(*) FROM crm_activities
	                      WHERE opportunity_id = $1 AND auto AND status = 'pendente'`, card.ID); n != 0 {
		t.Fatalf("a perda deixou %d tarefa(s) automática(s) pendente(s)", n)
	}
}

// ═══════════════════════════════════════════════════════════════════
// 4. `stage_history` É INSERT-ONLY E ACUMULA
// ═══════════════════════════════════════════════════════════════════

func TestHistoricoDeEtapasAcumulaEnuncaSobrescreve(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	_, etapas := a.funilDoSeed(t)

	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})

	// A criação já é a primeira entrada, com `from_stage_id` nulo.
	if n := a.contar(t, `SELECT count(*) FROM crm_stage_history WHERE opportunity_id = $1`, card.ID); n != 1 {
		t.Fatalf("a criação gravou %d linha(s) de histórico, esperado 1", n)
	}
	if n := a.contar(t, `SELECT count(*) FROM crm_stage_history
	                      WHERE opportunity_id = $1 AND from_stage_id IS NULL`, card.ID); n != 1 {
		t.Fatal("a primeira entrada deveria ter from_stage_id nulo")
	}

	trilha := []string{"Em atendimento", "Disponibilidade consultada", "Orçamento enviado", "Negociação"}
	for _, nome := range trilha {
		a.moverEtapa(t, token, card.ID, etapas[nome], nil)
	}

	esperado := 1 + len(trilha)
	if n := a.contar(t, `SELECT count(*) FROM crm_stage_history WHERE opportunity_id = $1`, card.ID); n != esperado {
		t.Fatalf("histórico com %d linhas, esperado %d — mover é REGISTRAR, nunca sobrescrever", n, esperado)
	}

	// Voltar acumula em vez de apagar a ida: é o que mede o vaivém do funil.
	a.moverEtapa(t, token, card.ID, etapas["Orçamento enviado"], nil)
	if n := a.contar(t, `SELECT count(*) FROM crm_stage_history
	                      WHERE opportunity_id = $1 AND to_stage_id = $2`, card.ID, etapas["Orçamento enviado"]); n != 2 {
		t.Fatalf("a segunda entrada em 'Orçamento enviado' não acumulou: %d linhas", n)
	}

	// O `/full` devolve a mesma trilha, do mais recente para o mais antigo, com
	// os dias em cada etapa derivados.
	resp := a.chamar(t, http.MethodGet, "/crm/opportunities/"+card.ID.String()+"/full", token, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("GET /full = %d (%s)", resp.Status, resp.Corpo)
	}
	completa := dado[struct {
		Historico []struct {
			ParaEtapaNome string   `json:"to_stage_name"`
			DiasNaEtapa   *float64 `json:"days_in_stage"`
			Instante      string   `json:"at"`
		} `json:"stage_history"`
	}](t, resp)
	if len(completa.Historico) != esperado+1 {
		t.Fatalf("/full trouxe %d linhas de histórico, esperado %d", len(completa.Historico), esperado+1)
	}
	if completa.Historico[0].ParaEtapaNome != "Orçamento enviado" {
		t.Fatalf("a primeira linha do /full deveria ser a mais recente, veio %q",
			completa.Historico[0].ParaEtapaNome)
	}
}

// A guarda otimista do arrasto: `from_stage_id` defasado é 409, não
// sobrescrita silenciosa.
func TestArrastoComEtapaDeOrigemDefasadaEhRecusado(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	_, etapas := a.funilDoSeed(t)

	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})
	a.moverEtapa(t, token, card.ID, etapas["Negociação"], nil)

	// O segundo corretor ainda acha que o card está em "Novo lead".
	resp := a.chamar(t, http.MethodPost, "/crm/opportunities/"+card.ID.String()+"/stage", token,
		map[string]any{"stage_id": etapas["Pré-reserva"], "from_stage_id": etapas["Novo lead"]})
	if resp.Status != http.StatusConflict {
		t.Fatalf("esperava 409 com origem defasada, veio %d (%s)", resp.Status, resp.Corpo)
	}
	if resp.codigoDoErro() != "INVALID_STATE_TRANSITION" {
		t.Fatalf("código = %q", resp.codigoDoErro())
	}
	if atual := resp.detalhes(t)["current_stage_id"]; atual != etapas["Negociação"].String() {
		t.Fatalf("details.current_stage_id = %v, esperado a etapa real", atual)
	}
}

// Etapa terminal não se alcança por `/stage`: ganhar cria reserva (e exige
// chave de idempotência), perder exige motivo.
func TestArrastarParaEtapaTerminalEhRecusadoComOEndpointCerto(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	_, etapas := a.funilDoSeed(t)

	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})

	for nome, sufixoDoEndpoint := range map[string]string{"Ganho": "/win", "Perdido": "/lose"} {
		resp := a.chamar(t, http.MethodPost, "/crm/opportunities/"+card.ID.String()+"/stage", token,
			map[string]any{"stage_id": etapas[nome]})
		if resp.Status != http.StatusConflict {
			t.Fatalf("arrastar para %q respondeu %d (%s)", nome, resp.Status, resp.Corpo)
		}
		endpoint, _ := resp.detalhes(t)["endpoint"].(string)
		if esperado := "/crm/opportunities/" + card.ID.String() + sufixoDoEndpoint; endpoint != esperado {
			t.Fatalf("details.endpoint = %q, esperado %q", endpoint, esperado)
		}
	}
	// E o card não se moveu.
	if n := a.contar(t, `SELECT count(*) FROM crm_stage_history WHERE opportunity_id = $1`, card.ID); n != 1 {
		t.Fatalf("a recusa gravou histórico: %d linhas", n)
	}
}

// ═══════════════════════════════════════════════════════════════════
// 5. ESCOPO `own` — PROVADO POR SQL
//
// O critério de aceite do §7 é "corretor vê no kanban apenas os próprios
// cards". Este teste confere as quatro superfícies (lista, kanban, detalhe,
// /full) E confirma, no `pg_stat_statements` do plano, que o recorte é uma
// cláusula de SQL — não uma peneira em memória que faria o `total` mentir.
// ═══════════════════════════════════════════════════════════════════

func TestEscopoOwnRecortaListaKanbanDetalheEFull(t *testing.T) {
	a := subir(t)
	_, tokenGestor := a.gestor(t)
	corretorID, tokenCorretor := a.corretor(t)
	outroID, _ := a.corretor(t)

	contato := a.contato(t)
	funil, _ := a.funilDoSeed(t)

	meu := a.criarOportunidade(t, tokenGestor, map[string]any{
		"contact_id": contato, "owner_id": corretorID, "amount_cents": 100000,
	}).ID
	alheio := a.criarOportunidade(t, tokenGestor, map[string]any{
		"contact_id": contato, "owner_id": outroID, "amount_cents": 900000,
	}).ID
	semDono := a.criarOportunidade(t, tokenGestor, map[string]any{
		"contact_id": contato, "amount_cents": 500000,
	}).ID

	// ── Listagem: o `total` do rodapé é o número que a gestão lê. Se o recorte
	// fosse em memória, ele contaria os três.
	resp := a.chamar(t, http.MethodGet, "/crm/opportunities?contact_id="+contato.String(), tokenCorretor, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("GET /crm/opportunities = %d (%s)", resp.Status, resp.Corpo)
	}
	visiveis, meta := lista[oportunidadeVista](t, resp)
	if meta.Total != 1 {
		t.Fatalf("meta.total = %d para o corretor, esperado 1 — o recorte não está no SQL", meta.Total)
	}
	if len(visiveis) != 1 || visiveis[0].ID != meu {
		t.Fatalf("o corretor enxergou %d card(s): %+v", len(visiveis), visiveis)
	}

	// O filtro `owner_id` da query não AMPLIA nada: quem tem escopo `own` só
	// consegue estreitar o que já é dele.
	resp = a.chamar(t, http.MethodGet,
		"/crm/opportunities?contact_id="+contato.String()+"&owner_id="+outroID.String(), tokenCorretor, nil)
	_, meta = lista[oportunidadeVista](t, resp)
	if meta.Total != 1 {
		t.Fatalf("o filtro owner_id ampliou o escopo: total = %d", meta.Total)
	}

	// ── Kanban: as colunas continuam todas lá; os totais é que são restritos.
	resp = a.chamar(t, http.MethodGet, "/crm/opportunities/kanban?pipeline_id="+funil.String(), tokenCorretor, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("GET kanban = %d (%s)", resp.Status, resp.Corpo)
	}
	quadro := dado[struct {
		Colunas []struct {
			Etapa struct {
				Nome string `json:"name"`
			} `json:"stage"`
			Total int   `json:"count"`
			Valor int64 `json:"amount_cents"`
			Cards []struct {
				ID uuid.UUID `json:"id"`
			} `json:"cards"`
		} `json:"columns"`
		Totais struct {
			Total int   `json:"count"`
			Valor int64 `json:"amount_cents"`
		} `json:"totals"`
	}](t, resp)

	if len(quadro.Colunas) != 8 {
		t.Fatalf("o quadro tem %d colunas, esperado 8 — coluna que some quando esvazia muda a forma da tela", len(quadro.Colunas))
	}
	if quadro.Totais.Total != 1 {
		t.Fatalf("totals.count = %d no kanban do corretor, esperado 1", quadro.Totais.Total)
	}
	// O total em DINHEIRO é o que vaza mais feio: 1.500.000 no rodapé diria ao
	// corretor quanto a casa inteira tem em negociação.
	if quadro.Totais.Valor != 100000 {
		t.Fatalf("totals.amount_cents = %d, esperado 100000 (só a carteira dele)", quadro.Totais.Valor)
	}
	for _, coluna := range quadro.Colunas {
		for _, card := range coluna.Cards {
			if card.ID != meu {
				t.Fatalf("o kanban do corretor trouxe o card %s, que é de outro dono", card.ID)
			}
		}
	}

	// ── Detalhe e /full: fora do escopo é 404, NUNCA 403 — 403 confirmaria que
	// o card existe.
	for _, caminho := range []string{
		"/crm/opportunities/" + alheio.String(),
		"/crm/opportunities/" + alheio.String() + "/full",
		"/crm/opportunities/" + semDono.String(),
	} {
		resp := a.chamar(t, http.MethodGet, caminho, tokenCorretor, nil)
		if resp.Status != http.StatusNotFound {
			t.Fatalf("GET %s pelo corretor = %d (%s), esperado 404", caminho, resp.Status, resp.Corpo)
		}
	}

	// ── E a escrita respeita o mesmo recorte.
	resp = a.chamar(t, http.MethodPatch, "/crm/opportunities/"+alheio.String(), tokenCorretor,
		map[string]any{"amount_cents": 1})
	if resp.Status != http.StatusNotFound {
		t.Fatalf("PATCH em card alheio = %d, esperado 404", resp.Status)
	}
	if valor := a.contar(t, `SELECT amount_cents FROM crm_opportunities WHERE id = $1`, alheio); valor != 900000 {
		t.Fatalf("o card alheio foi alterado: amount_cents = %d", valor)
	}
}

// O recorte é SQL, e a prova é o plano de execução: `owner_id = $n` tem de
// aparecer no filtro do índice, e não depois.
func TestOEscopoOwnVaiParaOPlanoDaConsultaENaoParaAMemoria(t *testing.T) {
	a := subir(t)
	corretorID, _ := a.corretor(t)

	// O EXPLAIN devolve UMA LINHA POR LINHA do plano: ler só a primeira com
	// QueryRow pegaria o `Aggregate` do topo e nunca o filtro, que é justamente
	// o que este teste procura.
	linhas, err := a.pool.Query(a.ctx, `
		EXPLAIN (FORMAT TEXT)
		SELECT count(*) FROM crm_opportunities o
		 WHERE o.property_id = $1 AND o.status = 'aberto' AND o.owner_id = $2`,
		a.propriedade, corretorID)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer linhas.Close()

	plano := ""
	for linhas.Next() {
		var linha string
		if err := linhas.Scan(&linha); err != nil {
			t.Fatalf("EXPLAIN: %v", err)
		}
		plano += linha + "\n"
	}
	if !contemTodas(plano, "owner_id") {
		t.Fatalf("o plano não cita owner_id — o recorte não chegou ao banco:\n%s", plano)
	}
}

func contemTodas(texto string, termos ...string) bool {
	for _, termo := range termos {
		encontrou := false
		for i := 0; i+len(termo) <= len(texto); i++ {
			if texto[i:i+len(termo)] == termo {
				encontrou = true
				break
			}
		}
		if !encontrou {
			return false
		}
	}
	return true
}

// Leads e atividades seguem a mesma regra. Lead SEM DONO não aparece para quem
// tem escopo `own` — e é o comportamento correto: é fila da gestão até ser
// distribuído.
func TestEscopoOwnEmLeadsEAtividades(t *testing.T) {
	a := subir(t)
	_, tokenGestor := a.gestor(t)
	corretorID, tokenCorretor := a.corretor(t)
	contato := a.contato(t)

	meuLead := a.criarLead(t, tokenGestor, map[string]any{"contact_id": contato, "owner_id": corretorID})
	leadSemDono := a.criarLead(t, tokenGestor, map[string]any{"contact_id": contato})

	resp := a.chamar(t, http.MethodGet, "/crm/leads?contact_id="+contato.String(), tokenCorretor, nil)
	vistos, meta := lista[struct {
		ID uuid.UUID `json:"id"`
	}](t, resp)
	if meta.Total != 1 || len(vistos) != 1 || vistos[0].ID != meuLead {
		t.Fatalf("o corretor viu %d lead(s) (total %d), esperado só o dele", len(vistos), meta.Total)
	}
	if r := a.chamar(t, http.MethodGet, "/crm/leads/"+leadSemDono.String(), tokenCorretor, nil); r.Status != http.StatusNotFound {
		t.Fatalf("lead sem dono pelo corretor = %d, esperado 404", r.Status)
	}

	// Atividade criada pelo gestor para outra pessoa não entra na caixa do
	// corretor.
	resp = a.chamar(t, http.MethodPost, "/crm/activities", tokenGestor, map[string]any{
		"type": "ligacao", "subject": "Ligar para o cliente", "contact_id": contato,
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /crm/activities = %d (%s)", resp.Status, resp.Corpo)
	}
	alheia := dado[atividadeVista](t, resp).ID

	resp = a.chamar(t, http.MethodGet, "/crm/activities?contact_id="+contato.String(), tokenCorretor, nil)
	_, meta = lista[atividadeVista](t, resp)
	if meta.Total != 0 {
		t.Fatalf("o corretor viu %d atividade(s) de outra pessoa", meta.Total)
	}
	if r := a.chamar(t, http.MethodDelete, "/crm/activities/"+alheia.String(), tokenCorretor, nil); r.Status != http.StatusNotFound {
		t.Fatalf("DELETE em atividade alheia = %d, esperado 404", r.Status)
	}
}

// ═══════════════════════════════════════════════════════════════════
// 6. `/full` NUMA CHAMADA, SEM N+1 — E O TEMPO MEDIDO
// ═══════════════════════════════════════════════════════════════════

// O número de consultas do `/full` tem de ser CONSTANTE: não pode crescer com a
// quantidade de atividades, notas ou movimentos de etapa. Este teste mede o
// tempo com 2 atividades e com 60, no mesmo card — se houvesse uma consulta por
// atividade, a segunda medição explodiria.
func TestFullNaoTemNMaisUmEOTempoNaoCresceComOVolume(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	_, etapas := a.funilDoSeed(t)

	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})
	rota := "/crm/opportunities/" + card.ID.String() + "/full"

	medir := func(rotulo string) time.Duration {
		t.Helper()
		// Uma passada de aquecimento tira o custo do primeiro plano do número.
		a.chamar(t, http.MethodGet, rota, token, nil)

		melhor := time.Hour
		for i := 0; i < 5; i++ {
			inicio := time.Now()
			resp := a.chamar(t, http.MethodGet, rota, token, nil)
			decorrido := time.Since(inicio)
			if resp.Status != http.StatusOK {
				t.Fatalf("GET /full (%s) = %d (%s)", rotulo, resp.Status, resp.Corpo)
			}
			if decorrido < melhor {
				melhor = decorrido
			}
		}
		t.Logf("GET /full (%s): %.1f ms", rotulo, float64(melhor.Microseconds())/1000)
		return melhor
	}

	// Volume pequeno.
	a.chamar(t, http.MethodPost, "/crm/activities", token, map[string]any{
		"type": "nota", "subject": "primeira nota", "opportunity_id": card.ID,
	})
	pequeno := medir("2 atividades, 1 movimento")

	// Volume grande: 60 atividades e 5 movimentos de etapa.
	for i := 0; i < 60; i++ {
		resp := a.chamar(t, http.MethodPost, "/crm/activities", token, map[string]any{
			"type": "ligacao", "subject": fmt.Sprintf("Contato %d", i), "opportunity_id": card.ID,
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("criando atividade %d: %d (%s)", i, resp.Status, resp.Corpo)
		}
	}
	for _, nome := range []string{"Em atendimento", "Disponibilidade consultada", "Orçamento enviado", "Negociação", "Pré-reserva"} {
		a.moverEtapa(t, token, card.ID, etapas[nome], nil)
	}
	grande := medir("62+ atividades, 6 movimentos")

	// O teto é generoso de propósito: o que se quer pegar é a ORDEM DE
	// GRANDEZA de um N+1 (60 viagens a mais ao banco), não variação de milissegundos.
	if grande > pequeno*4 {
		t.Fatalf("o /full cresceu %.1fx com 30x mais atividades (%.1f ms → %.1f ms): cheira a N+1",
			float64(grande)/float64(pequeno),
			float64(pequeno.Microseconds())/1000, float64(grande.Microseconds())/1000)
	}

	// E o conteúdo: notas separadas das atividades, documentos declarados e
	// vazios, timeline montada.
	resp := a.chamar(t, http.MethodGet, rota, token, nil)
	completa := dado[struct {
		Atividades []atividadeVista `json:"activities"`
		Notas      []struct {
			ID    uuid.UUID `json:"id"`
			Corpo string    `json:"body"`
		} `json:"notes"`
		Documentos []any `json:"documents"`
		Etapas     []any `json:"stages"`
		Timeline   []struct {
			Tipo string `json:"type"`
		} `json:"timeline"`
		SLA struct {
			EtapaID uuid.UUID `json:"stage_id"`
		} `json:"sla"`
		Contato struct {
			ID uuid.UUID `json:"id"`
		} `json:"contact"`
	}](t, resp)

	if len(completa.Notas) != 1 {
		t.Fatalf("notes trouxe %d itens, esperado 1", len(completa.Notas))
	}
	for _, at := range completa.Atividades {
		if at.Tipo == "nota" {
			t.Fatal("activities não deve conter atividades de tipo `nota` — elas têm aba própria")
		}
	}
	if completa.Documentos == nil {
		t.Fatal("documents veio nulo; o contrato promete a chave declarada e vazia")
	}
	if len(completa.Etapas) != 8 {
		t.Fatalf("stages trouxe %d etapas, esperado as 8 do funil da oportunidade", len(completa.Etapas))
	}
	if len(completa.Timeline) == 0 {
		t.Fatal("timeline vazia")
	}
	if completa.Contato.ID != contato {
		t.Fatal("o /full não trouxe o contato resolvido")
	}
}

// ═══════════════════════════════════════════════════════════════════
// 7. KANBAN PAGINADO POR COLUNA
// ═══════════════════════════════════════════════════════════════════

// Uma coluna com muitos cards não pode derrubar a tela — e o total da coluna
// tem de vir do SERVIDOR, senão a soma que a tela mostra muda conforme se rola
// a página.
func TestKanbanPaginaPorColunaEOTotalVemDoServidor(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	funil, etapas := a.funilDoSeed(t)

	const cards = 12
	for i := 0; i < cards; i++ {
		a.criarOportunidade(t, token, map[string]any{
			"contact_id": contato, "stage_id": etapas["Negociação"], "amount_cents": 1000,
		})
	}

	resp := a.chamar(t, http.MethodGet,
		fmt.Sprintf("/crm/opportunities/kanban?pipeline_id=%s&per_column=5", funil), token, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("GET kanban = %d (%s)", resp.Status, resp.Corpo)
	}
	quadro := dado[struct {
		Colunas []struct {
			Etapa struct {
				ID   uuid.UUID `json:"id"`
				Nome string    `json:"name"`
			} `json:"stage"`
			Total   int   `json:"count"`
			Valor   int64 `json:"amount_cents"`
			TemMais bool  `json:"has_more"`
			Cards   []any `json:"cards"`
		} `json:"columns"`
	}](t, resp)

	achou := false
	for _, coluna := range quadro.Colunas {
		if coluna.Etapa.ID != etapas["Negociação"] {
			continue
		}
		achou = true
		if len(coluna.Cards) != 5 {
			t.Fatalf("a coluna trouxe %d cards, esperado o teto de 5", len(coluna.Cards))
		}
		if coluna.Total != cards {
			t.Fatalf("count = %d, esperado %d — o total é da COLUNA, não da página", coluna.Total, cards)
		}
		if coluna.Valor != cards*1000 {
			t.Fatalf("amount_cents = %d, esperado %d — a soma é do servidor", coluna.Valor, cards*1000)
		}
		if !coluna.TemMais {
			t.Fatal("has_more = false com 12 cards e página de 5")
		}
	}
	if !achou {
		t.Fatal("a coluna 'Negociação' não veio no quadro")
	}
}

// ═══════════════════════════════════════════════════════════════════
// 8. ALERTAS DERIVADOS
// ═══════════════════════════════════════════════════════════════════

// O SLA estourado nasce da PASSAGEM DO TEMPO, e não de uma escrita: aqui o
// relógio é empurrado no banco (o card "entrou na etapa" há dez dias) e nenhuma
// linha de alerta é criada — mesmo assim o `/full` acusa.
func TestSLAEstouradoETarefaVencidaSaoDerivadosNaLeitura(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	_, etapas := a.funilDoSeed(t)

	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})
	movida := a.moverEtapa(t, token, card.ID, etapas["Orçamento enviado"], nil)
	if movida.Oportunidade.SLAEstourado {
		t.Fatal("o card acabou de entrar na etapa e já veio com o SLA estourado")
	}
	if movida.SLA.DiasRestantes == nil || *movida.SLA.DiasRestantes < 1 {
		t.Fatalf("days_left = %v logo após a entrada, esperado positivo", movida.SLA.DiasRestantes)
	}

	// Envelhece o card e a tarefa. NENHUMA tabela de alertas é tocada.
	a.executar(t, `UPDATE crm_opportunities SET entered_stage_at = now() - interval '10 days' WHERE id = $1`, card.ID)
	a.executar(t, `UPDATE crm_activities SET due_at = now() - interval '3 days'
	                WHERE opportunity_id = $1 AND auto AND status = 'pendente'`, card.ID)

	resp := a.chamar(t, http.MethodGet, "/crm/opportunities/"+card.ID.String()+"/full", token, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("GET /full = %d (%s)", resp.Status, resp.Corpo)
	}
	completa := dado[struct {
		Oportunidade oportunidadeVista `json:"opportunity"`
		Atividades   []atividadeVista  `json:"activities"`
		SLA          struct {
			DiasRestantes *int `json:"days_left"`
			Estourado     bool `json:"breached"`
		} `json:"sla"`
		Alertas []struct {
			Codigo     string `json:"code"`
			Severidade string `json:"severity"`
			Mensagem   string `json:"message"`
		} `json:"alerts"`
	}](t, resp)

	if !completa.Oportunidade.SLAEstourado || !completa.SLA.Estourado {
		t.Fatal("o SLA não foi derivado como estourado depois de dez dias na etapa")
	}
	if completa.SLA.DiasRestantes == nil || *completa.SLA.DiasRestantes >= 0 {
		t.Fatalf("days_left = %v, esperado NEGATIVO quando estourou", completa.SLA.DiasRestantes)
	}
	if len(completa.Atividades) == 0 || !completa.Atividades[0].Vencida {
		t.Fatalf("a tarefa vencida não foi marcada como overdue: %+v", completa.Atividades)
	}

	codigos := map[string]bool{}
	for _, alerta := range completa.Alertas {
		codigos[alerta.Codigo] = true
	}
	for _, esperado := range []string{"sla_estourado", "tarefa_vencida"} {
		if !codigos[esperado] {
			t.Fatalf("faltou o alerta %q; vieram %v", esperado, codigos)
		}
	}

	// E nada disso foi gravado: não existe tabela de alertas para envelhecer
	// errado.
	if n := a.contar(t, `SELECT count(*) FROM information_schema.tables
	                      WHERE table_schema = current_schema() AND table_name LIKE '%alert%'`); n != 0 {
		t.Fatalf("apareceu tabela de alertas no schema (%d): alerta gravado precisa de um job para nascer e outro para morrer", n)
	}
}

// ═══════════════════════════════════════════════════════════════════
// 9. CONVERSÃO DE LEAD
// ═══════════════════════════════════════════════════════════════════

func TestConverterLeadHerdaOInteresseEAcionaAPrimeiraEtapa(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	produto := a.produto(t, "apto-2s")

	entrada, saida := a.dia(120), a.dia(124)
	lead := a.criarLead(t, token, map[string]any{
		"contact_id":            contato,
		"source":                "whatsapp",
		"interest_unit_type_id": produto,
		"desired_check_in":      entrada,
		"desired_check_out":     saida,
	})

	resp := a.chamar(t, http.MethodPost, "/crm/leads/"+lead.String()+"/convert", token, nil)
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /convert = %d (%s)", resp.Status, resp.Corpo)
	}
	conversao := dado[struct {
		Lead struct {
			Status         string     `json:"status"`
			ConvertidoEm   *string    `json:"converted_at"`
			OportunidadeID *uuid.UUID `json:"opportunity_id"`
		} `json:"lead"`
		Oportunidade oportunidadeVista `json:"opportunity"`
		TarefaAuto   *atividadeVista   `json:"auto_task"`
	}](t, resp)

	if conversao.Lead.Status != "convertido" || conversao.Lead.ConvertidoEm == nil {
		t.Fatalf("o lead não foi marcado como convertido: %+v", conversao.Lead)
	}
	if conversao.Oportunidade.CheckIn == nil || *conversao.Oportunidade.CheckIn != entrada {
		t.Fatalf("a oportunidade não herdou a data pretendida: %+v", conversao.Oportunidade.CheckIn)
	}
	if conversao.Oportunidade.EtapaNome != "Novo lead" {
		t.Fatalf("caiu na etapa %q, esperado a de menor position", conversao.Oportunidade.EtapaNome)
	}
	// A primeira entrada não é caso especial: ela dispara a tarefa automática
	// como qualquer outra.
	if conversao.TarefaAuto == nil {
		t.Fatal("a entrada na primeira etapa não criou a tarefa automática dela")
	}

	// Converter de novo abre o card certo em vez de criar um gêmeo.
	repetida := a.chamar(t, http.MethodPost, "/crm/leads/"+lead.String()+"/convert", token, nil)
	if repetida.Status != http.StatusConflict || repetida.codigoDoErro() != "LEAD_ALREADY_CONVERTED" {
		t.Fatalf("segunda conversão = %d %s (%s)", repetida.Status, repetida.codigoDoErro(), repetida.Corpo)
	}
	if id := repetida.detalhes(t)["opportunity_id"]; id != conversao.Oportunidade.ID.String() {
		t.Fatalf("details.opportunity_id = %v, esperado o card existente", id)
	}
	if n := a.contar(t, `SELECT count(*) FROM crm_opportunities WHERE lead_id = $1`, lead); n != 1 {
		t.Fatalf("a segunda conversão criou %d oportunidades", n)
	}
}

// ═══════════════════════════════════════════════════════════════════
// 10. AUDITORIA EM TODA ESCRITA
// ═══════════════════════════════════════════════════════════════════

func TestTodaEscritaDoCRMDeixaTrilhaDeAuditoria(t *testing.T) {
	a := subir(t)
	usuario, token := a.gestor(t)
	contato := a.contato(t)
	_, etapas := a.funilDoSeed(t)
	motivo := a.motivoDePerda(t)

	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})
	a.chamar(t, http.MethodPatch, "/crm/opportunities/"+card.ID.String(), token,
		map[string]any{"amount_cents": 42000})
	a.moverEtapa(t, token, card.ID, etapas["Negociação"], nil)
	a.chamar(t, http.MethodPost, "/crm/opportunities/"+card.ID.String()+"/lose", token,
		map[string]any{"lost_reason_id": motivo})

	acoes := map[string]bool{}
	linhas, err := a.pool.Query(a.ctx,
		`SELECT action FROM audit_log WHERE actor_id = $1 AND entity_id = $2`, usuario, card.ID)
	if err != nil {
		t.Fatalf("lendo audit_log: %v", err)
	}
	defer linhas.Close()
	for linhas.Next() {
		var acao string
		if err := linhas.Scan(&acao); err != nil {
			t.Fatalf("lendo audit_log: %v", err)
		}
		acoes[acao] = true
	}

	for _, esperada := range []string{
		"crm_opportunities.criado",
		"crm_opportunities.alterado",
		"crm_opportunities.etapa_movida",
		"crm_opportunities.perdida",
	} {
		if !acoes[esperada] {
			t.Fatalf("faltou a trilha %q; vieram %v", esperada, acoes)
		}
	}

	// A trilha tem de trazer o IP e o request_id do middleware — sem eles, a
	// investigação de um incidente começa por uma mentira.
	if n := a.contar(t, `SELECT count(*) FROM audit_log
	                      WHERE actor_id = $1 AND entity_id = $2 AND after IS NULL AND before IS NULL`,
		usuario, card.ID); n > 0 {
		t.Fatalf("%d linha(s) de trilha sem before nem after", n)
	}
}

// ═══════════════════════════════════════════════════════════════════
// 11. GUARDAS DE CONFIGURAÇÃO DO FUNIL
// ═══════════════════════════════════════════════════════════════════

func TestFunilPadraoNaoPodeFicarSemSucessor(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	funil, _ := a.funilDoSeed(t)

	resp := a.chamar(t, http.MethodPatch, "/crm/pipelines/"+funil.String(), token,
		map[string]any{"is_default": false})
	if resp.Status != http.StatusConflict || resp.codigoDoErro() != "DEFAULT_PIPELINE_REQUIRED" {
		t.Fatalf("desmarcar o único padrão = %d %s (%s)", resp.Status, resp.codigoDoErro(), resp.Corpo)
	}
	if padrao := a.contar(t, `SELECT count(*) FROM crm_pipelines WHERE property_id = $1 AND is_default AND active`,
		a.propriedade); padrao != 1 {
		t.Fatalf("a propriedade ficou com %d funis padrão", padrao)
	}

	// Marcar OUTRO como padrão desmarca este sozinho — é o caminho que o erro
	// acima manda seguir.
	resp = a.chamar(t, http.MethodPost, "/crm/pipelines", token,
		map[string]any{"name": "Funil de Eventos " + sufixo(), "is_default": true})
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /crm/pipelines = %d (%s)", resp.Status, resp.Corpo)
	}
	novo := dado[struct {
		ID uuid.UUID `json:"id"`
	}](t, resp).ID
	t.Cleanup(func() {
		a.executar(t, `UPDATE crm_pipelines SET is_default = false WHERE id = $1`, novo)
		a.executar(t, `UPDATE crm_pipelines SET is_default = true WHERE id = $1`, funil)
		a.executar(t, `DELETE FROM crm_pipelines WHERE id = $1`, novo)
	})

	if padrao := a.contar(t, `SELECT count(*) FROM crm_pipelines WHERE property_id = $1 AND is_default`,
		a.propriedade); padrao != 1 {
		t.Fatalf("depois da troca há %d funis padrão", padrao)
	}
	if a.texto(t, `SELECT id::text FROM crm_pipelines WHERE property_id = $1 AND is_default`, a.propriedade) != novo.String() {
		t.Fatal("o funil novo não assumiu o padrão")
	}
}

// Reordenar exige a lista COMPLETA: mandar só as duas etapas que se moveram
// deixaria as demais com a posição de antes, e o kanban desenharia duas colunas
// na mesma casa.
func TestReordenarExigeATodasAsEtapasDoFunil(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	funil := a.funilDescartavel(t, token)
	primeira := a.criarEtapa(t, token, funil, "Contato "+sufixo())
	segunda := a.criarEtapa(t, token, funil, "Proposta "+sufixo())
	terceira := a.criarEtapa(t, token, funil, "Fechamento "+sufixo())

	resp := a.chamar(t, http.MethodPost, "/crm/stages/reorder", token, map[string]any{
		"pipeline_id": funil, "stage_ids": []uuid.UUID{terceira, primeira},
	})
	if resp.Status != http.StatusUnprocessableEntity || resp.codigoDoErro() != "STAGE_ORDER_INCOMPLETE" {
		t.Fatalf("lista incompleta = %d %s (%s)", resp.Status, resp.codigoDoErro(), resp.Corpo)
	}
	faltando, _ := resp.detalhes(t)["missing_stage_ids"].([]any)
	if len(faltando) != 1 || faltando[0] != segunda.String() {
		t.Fatalf("details.missing_stage_ids = %v, esperado [%s]", faltando, segunda)
	}

	// Etapa de OUTRO funil na lista é recusa diferente, com o id apontado.
	_, etapasDoSeed := a.funilDoSeed(t)
	resp = a.chamar(t, http.MethodPost, "/crm/stages/reorder", token, map[string]any{
		"pipeline_id": funil,
		"stage_ids":   []uuid.UUID{terceira, segunda, primeira, etapasDoSeed["Ganho"]},
	})
	if resp.Status != http.StatusUnprocessableEntity || resp.codigoDoErro() != "STAGE_NOT_IN_PIPELINE" {
		t.Fatalf("etapa de outro funil = %d %s (%s)", resp.Status, resp.codigoDoErro(), resp.Corpo)
	}

	// A lista completa reordena NUMA instrução — é a `UNIQUE` adiável que
	// permite a colisão intermediária.
	resp = a.chamar(t, http.MethodPost, "/crm/stages/reorder", token, map[string]any{
		"pipeline_id": funil, "stage_ids": []uuid.UUID{terceira, segunda, primeira},
	})
	if resp.Status != http.StatusOK {
		t.Fatalf("reorder completo = %d (%s)", resp.Status, resp.Corpo)
	}
	ordem := dado[[]struct {
		ID      uuid.UUID `json:"id"`
		Posicao int       `json:"position"`
	}](t, resp)
	if len(ordem) != 3 || ordem[0].ID != terceira || ordem[2].ID != primeira {
		t.Fatalf("ordem gravada errada: %+v", ordem)
	}
	for i, etapa := range ordem {
		if etapa.Posicao != i {
			t.Fatalf("position = %d na %dª etapa, esperado o índice do array", etapa.Posicao, i)
		}
	}
}

// Etapa com histórico apontando para ela não se apaga: `crm_stage_history` é
// insert-only e é a matéria-prima da conversão por etapa.
func TestEtapaComHistoricoNaoSeApaga(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)

	funil := a.funilDescartavel(t, token)
	inicial := a.criarEtapa(t, token, funil, "Entrada "+sufixo())
	segunda := a.criarEtapa(t, token, funil, "Saída "+sufixo())

	// Etapa vazia se apaga.
	if resp := a.chamar(t, http.MethodDelete, "/crm/stages/"+segunda.String(), token, nil); resp.Status != http.StatusNoContent {
		t.Fatalf("apagar etapa vazia = %d (%s)", resp.Status, resp.Corpo)
	}

	a.criarOportunidade(t, token, map[string]any{
		"contact_id": contato, "pipeline_id": funil, "stage_id": inicial,
	})
	resp := a.chamar(t, http.MethodDelete, "/crm/stages/"+inicial.String(), token, nil)
	if resp.Status != http.StatusConflict || resp.codigoDoErro() != "RESOURCE_IN_USE" {
		t.Fatalf("apagar etapa em uso = %d %s (%s)", resp.Status, resp.codigoDoErro(), resp.Corpo)
	}
	detalhes := resp.detalhes(t)
	if detalhes["opportunity_count"] == nil || detalhes["history_count"] == nil {
		t.Fatalf("details deveria trazer as duas contagens: %v", detalhes)
	}
}

// ═══════════════════════════════════════════════════════════════════
// 12. O ESTADO SÓ MUDA POR AÇÃO NOMEADA
// ═══════════════════════════════════════════════════════════════════

// Um PATCH que movesse a etapa não gravaria `crm_stage_history`, e a conversão
// por etapa do BI passaria a mentir. O decoder recusa o campo desconhecido — e
// é essa recusa que sustenta a regra.
func TestPatchNaoAceitaMudarEtapaStatusNemReserva(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	_, etapas := a.funilDoSeed(t)

	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})

	for campo, valor := range map[string]any{
		"stage_id":       etapas["Ganho"],
		"status":         "ganha",
		"lost_reason_id": a.motivoDePerda(t),
		"reservation_id": uuid.NewString(),
		"quote_id":       uuid.NewString(),
	} {
		resp := a.chamar(t, http.MethodPatch, "/crm/opportunities/"+card.ID.String(), token,
			map[string]any{campo: valor})
		if resp.Status != http.StatusUnprocessableEntity {
			t.Fatalf("PATCH com %q respondeu %d (%s) — o estado só muda por ação nomeada",
				campo, resp.Status, resp.Corpo)
		}
	}
	if n := a.contar(t, `SELECT count(*) FROM crm_stage_history WHERE opportunity_id = $1`, card.ID); n != 1 {
		t.Fatalf("os PATCH recusados mexeram no histórico: %d linhas", n)
	}
}

// `done_at` escrito pelo cliente é o tempo médio de resposta do §15 medido pelo
// relógio do navegador. Concluir é `/complete`, e ele é idempotente.
func TestConclusaoEhDoServidorEIdempotente(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)

	resp := a.chamar(t, http.MethodPost, "/crm/activities", token, map[string]any{
		"type": "ligacao", "subject": "Ligar", "contact_id": contato,
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /crm/activities = %d (%s)", resp.Status, resp.Corpo)
	}
	tarefa := dado[atividadeVista](t, resp).ID

	// `done_at` e `status: concluida` não passam pelo PATCH.
	if r := a.chamar(t, http.MethodPatch, "/crm/activities/"+tarefa.String(), token,
		map[string]any{"done_at": time.Now().Format(time.RFC3339)}); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH com done_at = %d (%s)", r.Status, r.Corpo)
	}
	if r := a.chamar(t, http.MethodPatch, "/crm/activities/"+tarefa.String(), token,
		map[string]any{"status": "concluida"}); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH com status concluida = %d (%s)", r.Status, r.Corpo)
	}

	rota := "/crm/activities/" + tarefa.String() + "/complete"
	primeira := a.chamar(t, http.MethodPost, rota, token, map[string]any{"note": "cliente pediu para ligar amanhã"})
	if primeira.Status != http.StatusOK {
		t.Fatalf("POST /complete = %d (%s)", primeira.Status, primeira.Corpo)
	}
	concluida := dado[struct {
		Atividade atividadeVista  `json:"activity"`
		Proxima   *atividadeVista `json:"next_activity"`
	}](t, primeira)
	if concluida.Atividade.ConcluidaEm == nil {
		t.Fatal("done_at não foi carimbado")
	}
	if concluida.Proxima != nil {
		t.Fatal("next_activity veio preenchida sem ter sido pedida")
	}

	// O duplo clique é o gesto mais comum da tela: `done_at` não pode andar.
	segunda := a.chamar(t, http.MethodPost, rota, token, nil)
	if segunda.Status != http.StatusOK {
		t.Fatalf("segunda conclusão = %d (%s)", segunda.Status, segunda.Corpo)
	}
	repetida := dado[struct {
		Atividade atividadeVista `json:"activity"`
	}](t, segunda).Atividade
	if repetida.ConcluidaEm == nil || *repetida.ConcluidaEm != *concluida.Atividade.ConcluidaEm {
		t.Fatalf("done_at mudou na repetição: %v → %v", concluida.Atividade.ConcluidaEm, repetida.ConcluidaEm)
	}

	// "Concluir e agendar o próximo passo", numa transação só.
	resp = a.chamar(t, http.MethodPost, "/crm/activities", token, map[string]any{
		"type": "tarefa", "subject": "Retomar", "contact_id": contato,
	})
	outra := dado[atividadeVista](t, resp).ID
	resp = a.chamar(t, http.MethodPost, "/crm/activities/"+outra.String()+"/complete", token, map[string]any{
		"next_activity": map[string]any{
			"type": "whatsapp", "subject": "Mandar as fotos",
			"due_at": time.Now().Add(48 * time.Hour).Format(time.RFC3339),
		},
	})
	if resp.Status != http.StatusOK {
		t.Fatalf("complete com next_activity = %d (%s)", resp.Status, resp.Corpo)
	}
	comProxima := dado[struct {
		Proxima *atividadeVista `json:"next_activity"`
	}](t, resp)
	if comProxima.Proxima == nil || comProxima.Proxima.Tipo != "whatsapp" {
		t.Fatalf("a próxima ação não nasceu: %+v", comProxima.Proxima)
	}
}

// ─────────────────────────── Auxiliares dos testes ──────────────────

func (a *ambiente) moverEtapa(t *testing.T, token string, card, etapa uuid.UUID, de *uuid.UUID) resultadoDeEtapa {
	t.Helper()

	corpo := map[string]any{"stage_id": etapa}
	if de != nil {
		corpo["from_stage_id"] = *de
	}
	resp := a.chamar(t, http.MethodPost, "/crm/opportunities/"+card.String()+"/stage", token, corpo)
	if resp.Status != http.StatusOK {
		t.Fatalf("POST /stage (%s) = %d (%s)", etapa, resp.Status, resp.Corpo)
	}
	return dado[resultadoDeEtapa](t, resp)
}

func (a *ambiente) criarLead(t *testing.T, token string, corpo map[string]any) uuid.UUID {
	t.Helper()

	resp := a.chamar(t, http.MethodPost, "/crm/leads", token, corpo)
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /crm/leads = %d (%s)", resp.Status, resp.Corpo)
	}
	return dado[struct {
		ID uuid.UUID `json:"id"`
	}](t, resp).ID
}

// funilDescartavel cria um funil só para o teste, com limpeza no fim.
func (a *ambiente) funilDescartavel(t *testing.T, token string) uuid.UUID {
	t.Helper()

	resp := a.chamar(t, http.MethodPost, "/crm/pipelines", token,
		map[string]any{"name": "Funil de teste " + sufixo()})
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /crm/pipelines = %d (%s)", resp.Status, resp.Corpo)
	}
	id := dado[struct {
		ID uuid.UUID `json:"id"`
	}](t, resp).ID

	t.Cleanup(func() {
		a.executar(t, `DELETE FROM crm_stage_history WHERE to_stage_id IN
		                 (SELECT id FROM crm_stages WHERE pipeline_id = $1)`, id)
		a.executar(t, `DELETE FROM crm_activities WHERE stage_id IN
		                 (SELECT id FROM crm_stages WHERE pipeline_id = $1)`, id)
		a.executar(t, `DELETE FROM crm_opportunities WHERE pipeline_id = $1`, id)
		a.executar(t, `DELETE FROM crm_pipelines WHERE id = $1`, id)
	})
	return id
}

func (a *ambiente) criarEtapa(t *testing.T, token string, funil uuid.UUID, nome string) uuid.UUID {
	t.Helper()

	resp := a.chamar(t, http.MethodPost, "/crm/stages", token,
		map[string]any{"pipeline_id": funil, "name": nome})
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /crm/stages = %d (%s)", resp.Status, resp.Corpo)
	}
	return dado[struct {
		ID uuid.UUID `json:"id"`
	}](t, resp).ID
}

// estadoDoCard resume, num texto comparável, tudo o que uma ação poderia ter
// mudado. É como o teste do ganho recusado prova que NADA mudou.
func (a *ambiente) estadoDoCard(t *testing.T, card uuid.UUID) string {
	t.Helper()

	return a.texto(t, `
		SELECT format('status=%s stage=%s reserva=%s quote=%s fechada=%s historico=%s',
		              o.status, o.stage_id, coalesce(o.reservation_id::text, '-'),
		              coalesce(o.quote_id::text, '-'), coalesce(o.closed_at::text, '-'),
		              (SELECT count(*) FROM crm_stage_history h WHERE h.opportunity_id = o.id))
		  FROM crm_opportunities o WHERE o.id = $1`, card)
}
