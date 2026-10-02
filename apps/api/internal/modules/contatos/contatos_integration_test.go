//go:build integration

package contatos_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/contatos"
)

// ═══════════════════════ A dívida que este módulo paga ═══════════════

// A jornada que não existia: identificar a pessoa e vender para ela. Antes
// deste módulo, `POST /reservations` exigia `contact_id` e não havia rota que
// devolvesse um — medido no stack no ar, `GET /contacts` respondia 404.
func TestJornadaDeCadastroEBusca(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	telefone := telefoneDeTeste(t)
	resp := a.chamar(t, http.MethodPost, "/contacts", token, map[string]any{
		"name":       "Joana Ribeiro " + sufixo(),
		"email":      "joana_" + sufixo() + "@exemplo.invalid",
		"phone_e164": telefone,
		"lgpd_basis": contatos.BaseContrato,
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /contacts = %d, corpo %s", resp.Status, resp.Corpo)
	}
	criado := dado[contatos.Contato](t, resp)
	a.limparContato(t, criado.ID)

	if resp.Location != "/api/v1/contacts/"+criado.ID.String() {
		t.Errorf("Location = %q", resp.Location)
	}
	if criado.Telefone == nil || *criado.Telefone != telefone {
		t.Errorf("telefone gravado = %v, esperado %s", criado.Telefone, telefone)
	}

	// O caminho do inbound de WhatsApp: pergunta pelo número EXATO e recebe
	// zero ou um. Nunca uma lista parecida.
	busca := a.chamar(t, http.MethodGet, "/contacts?phone="+urlescape(telefone), token, nil)
	if busca.Status != http.StatusOK {
		t.Fatalf("GET /contacts?phone = %d: %s", busca.Status, busca.Corpo)
	}
	achados, meta := lista[contatos.Contato](t, busca)
	if meta.Total != 1 || len(achados) != 1 || achados[0].ID != criado.ID {
		t.Fatalf("busca por telefone devolveu %d registros (total %d), esperado exatamente o recém-criado",
			len(achados), meta.Total)
	}

	// E o contato serve de fato para vender: a FK NOT NULL de reservations
	// aceita o id que a API acabou de devolver.
	r := a.reserva(t, criado.ID, "confirmed", 30, 3)
	if r.Code == "" {
		t.Fatal("a reserva sobre o contato criado pela API não nasceu")
	}
}

// ═══════════════════════ Deduplicação ════════════════════════════════

// O teste que o módulo existe para passar: a MESMA pessoa, escrita de dois
// jeitos, é recusada como duplicata — e o erro traz o id do registro existente,
// porque quem tenta cadastrar de novo quer justamente chegar nela.
func TestTelefoneDuplicadoEmFormatosDiferentesEhRecusado(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	// +5585 9 XXXX XXXX; a segunda tentativa manda o MESMO número na forma que
	// o balcão digita: com DDD entre parênteses, hífen e sem DDI.
	canonico := telefoneDeTeste(t)
	ddd := canonico[3:5]
	assinante := canonico[5:]
	comoOBalcaoDigita := "(" + ddd + ") " + assinante[:5] + "-" + assinante[5:]

	primeiro := a.chamar(t, http.MethodPost, "/contacts", token, map[string]any{
		"name":       "Marcos Duarte " + sufixo(),
		"phone_e164": canonico,
	})
	if primeiro.Status != http.StatusCreated {
		t.Fatalf("primeiro POST = %d: %s", primeiro.Status, primeiro.Corpo)
	}
	existente := dado[contatos.Contato](t, primeiro)
	a.limparContato(t, existente.ID)

	segundo := a.chamar(t, http.MethodPost, "/contacts", token, map[string]any{
		"name":       "Marcos D. " + sufixo(),
		"phone_e164": comoOBalcaoDigita,
	})
	if segundo.Status != http.StatusConflict {
		t.Fatalf("%q depois de %q devolveu %d (corpo %s) — a deduplicação não enxergou a mesma pessoa",
			comoOBalcaoDigita, canonico, segundo.Status, segundo.Corpo)
	}
	if got := segundo.codigoDoErro(); got != "CONTACT_DUPLICATE" {
		t.Fatalf("código = %q, esperado CONTACT_DUPLICATE", got)
	}

	d := segundo.detalhes(t)
	if d["field"] != "phone_e164" {
		t.Errorf("details.field = %v, esperado phone_e164", d["field"])
	}
	if d["contact_id"] != existente.ID.String() {
		t.Fatalf("details.contact_id = %v, esperado %s — sem o id o atendente recebe um 409 seco e não acha a pessoa",
			d["contact_id"], existente.ID)
	}

	// E não sobrou lixo: o índice único recusou, então existe UMA linha.
	if n := a.contar(t, `SELECT count(*) FROM contacts WHERE phone_e164 = $1`, canonico); n != 1 {
		t.Fatalf("%d linhas com o telefone %s; esperado 1", n, canonico)
	}
}

// A deduplicação por documento não tem índice único (contacts_doc_idx é comum):
// a garantia é a trava de transação do repositório. O comportamento externo tem
// de ser o mesmo do telefone, máscara incluída.
func TestDocumentoDuplicadoEmFormatosDiferentesEhRecusado(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	primeiro := a.chamar(t, http.MethodPost, "/contacts", token, map[string]any{
		"name":       "Carla Nunes " + sufixo(),
		"phone_e164": telefoneDeTeste(t),
		"doc_type":   contatos.DocCPF,
		"doc_number": "529.982.247-25",
	})
	if primeiro.Status != http.StatusCreated {
		t.Fatalf("primeiro POST = %d: %s", primeiro.Status, primeiro.Corpo)
	}
	existente := dado[contatos.Contato](t, primeiro)
	a.limparContato(t, existente.ID)

	if existente.Documento == nil || *existente.Documento != "52998224725" {
		t.Fatalf("documento gravado = %v, esperado só os dígitos", existente.Documento)
	}

	segundo := a.chamar(t, http.MethodPost, "/contacts", token, map[string]any{
		"name":       "Carla N. " + sufixo(),
		"phone_e164": telefoneDeTeste(t), // telefone diferente: só o documento colide
		"doc_type":   contatos.DocCPF,
		"doc_number": "52998224725",
	})
	if segundo.Status != http.StatusConflict || segundo.codigoDoErro() != "CONTACT_DUPLICATE" {
		t.Fatalf("CPF repetido devolveu %d/%s: %s", segundo.Status, segundo.codigoDoErro(), segundo.Corpo)
	}
	d := segundo.detalhes(t)
	if d["field"] != "doc_number" || d["contact_id"] != existente.ID.String() {
		t.Fatalf("details = %v, esperado field=doc_number e contact_id=%s", d, existente.ID)
	}
}

// A trava de documento é de TRANSAÇÃO e serializa pedidos simultâneos. Sem ela
// a checagem seria SELECT-antes-de-INSERT puro: os dois consultam, nenhum acha,
// os dois gravam.
func TestCorridaDeDocumentoNaoCriaDuasPessoas(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	const cpf = "529.982.247-25"
	tipo := "cpf"

	resultados := make(chan resposta, 2)
	telefones := []string{telefoneDeTeste(t), telefoneDeTeste(t)}
	for i := 0; i < 2; i++ {
		go func(tel string) {
			resultados <- a.chamar(t, http.MethodPost, "/contacts", token, map[string]any{
				"name":       "Disputa " + sufixo(),
				"phone_e164": tel,
				"doc_type":   tipo,
				"doc_number": cpf,
			})
		}(telefones[i])
	}

	criados, conflitos := 0, 0
	for i := 0; i < 2; i++ {
		r := <-resultados
		switch r.Status {
		case http.StatusCreated:
			criados++
			a.limparContato(t, dado[contatos.Contato](t, r).ID)
		case http.StatusConflict:
			conflitos++
		default:
			t.Errorf("resposta inesperada %d: %s", r.Status, r.Corpo)
		}
	}
	if criados != 1 || conflitos != 1 {
		t.Fatalf("%d criados e %d conflitos; esperado 1 e 1 — duas fichas do mesmo CPF é a duplicata que o módulo existe para impedir",
			criados, conflitos)
	}
	if n := a.contar(t, `SELECT count(*) FROM contacts WHERE doc_number = '52998224725' AND property_id = $1`, a.propriedade); n != 1 {
		t.Fatalf("%d linhas com o mesmo CPF; esperado 1", n)
	}
}

// ═══════════════════════ LGPD ════════════════════════════════════════

// O segundo teste obrigatório: anonimizar elimina a PII e NÃO toca no
// financeiro. Reserva, preço congelado e noites continuam de pé, apontando para
// o mesmo contact_id.
func TestAnonimizarMantemAReservaEOsValoresCongelados(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	contato := a.contatoDireto(t, "Titular Que Vai Sumir "+sufixo())
	// Estadia ENCERRADA: quem está hospedado hoje não é anonimizável, e é outro
	// teste. Aqui o contrato já acabou e o razão precisa sobreviver.
	r := a.reserva(t, contato, "checked_out", -40, 3)

	antes := a.contar(t, `SELECT count(*) FROM reservation_nights WHERE reservation_id = $1`, r.ID)
	if antes != 3 {
		t.Fatalf("a reserva de prova nasceu com %d noites, esperado 3", antes)
	}

	resp := a.chamar(t, http.MethodPost, "/contacts/"+contato.String()+"/anonymize", token,
		map[string]any{"reason": "pedido do titular em 20/08/2026, protocolo 4471"})
	if resp.Status != http.StatusOK {
		t.Fatalf("POST /anonymize = %d: %s", resp.Status, resp.Corpo)
	}
	res := dado[contatos.ResultadoDeAnonimizacao](t, resp)

	// A pessoa sumiu.
	if res.Contato.AnonimizadoEm == nil {
		t.Fatal("anonymized_at não foi carimbado")
	}
	if res.Contato.Email != nil || res.Contato.Telefone != nil || res.Contato.Documento != nil {
		t.Fatalf("PII sobreviveu na resposta: %+v", res.Contato)
	}
	if res.Contato.OptInMarketing || res.Contato.ConsentimentoEm != nil {
		t.Error("o opt-in de marketing e a data de aceite deveriam ter sido zerados")
	}

	// A venda ficou — e a resposta prova isso na hora.
	if res.Preservado.Reservas != 1 {
		t.Errorf("preserved.reservations = %d, esperado 1", res.Preservado.Reservas)
	}

	var (
		codigo    string
		contatoFK uuid.UUID
		total     int64
		noites    int
	)
	if err := a.pool.QueryRow(a.ctx, `
		SELECT r.code, r.contact_id, p.total_cents,
		       (SELECT count(*) FROM reservation_nights n WHERE n.reservation_id = r.id)
		  FROM reservations r JOIN reservation_pricing p ON p.reservation_id = r.id
		 WHERE r.id = $1`, r.ID).Scan(&codigo, &contatoFK, &total, &noites); err != nil {
		t.Fatalf("a reserva sumiu com a anonimização: %v", err)
	}
	if codigo != r.Code || contatoFK != contato {
		t.Fatalf("reserva %s aponta para %s; esperado %s — a FK nunca é rompida", codigo, contatoFK, contato)
	}
	if total != r.Total {
		t.Fatalf("total_cents = %d, esperado %d: a anonimização reescreveu o preço acordado", total, r.Total)
	}
	if noites != 3 {
		t.Fatalf("%d noites depois da anonimização, esperado 3", noites)
	}

	// E o dado eliminado não ficou guardado na trilha, que é a tabela que meio
	// time consegue ler.
	linhas := a.contar(t, `
		SELECT count(*) FROM audit_log
		 WHERE entity = 'contacts' AND entity_id = $1
		   AND (before::text ILIKE '%Titular Que Vai Sumir%' OR after::text ILIKE '%Titular Que Vai Sumir%')`, contato)
	if linhas != 0 {
		t.Fatalf("%d linhas de audit_log guardam o nome do titular: a eliminação foi cumprida na tabela que ele vê e descumprida na que ele não vê", linhas)
	}
	if a.contar(t, `SELECT count(*) FROM audit_log WHERE entity='contacts' AND entity_id=$1 AND action='contacts.anonimizado'`, contato) != 1 {
		t.Error("a anonimização não deixou linha na trilha — não há como provar quando e por quem foi feita")
	}
}

// Repetir a chamada responde 200 com a mesma ficha e NÃO regrava
// `anonymized_at`: reescrever a data faria a segunda chamada mentir sobre
// quando a eliminação aconteceu.
func TestAnonimizarEhIdempotente(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contatoDireto(t, "Repetido "+sufixo())

	corpo := map[string]any{"reason": "pedido do titular"}
	primeira := a.chamar(t, http.MethodPost, "/contacts/"+contato.String()+"/anonymize", token, corpo)
	if primeira.Status != http.StatusOK {
		t.Fatalf("primeira = %d: %s", primeira.Status, primeira.Corpo)
	}
	umaVez := dado[contatos.ResultadoDeAnonimizacao](t, primeira)

	segunda := a.chamar(t, http.MethodPost, "/contacts/"+contato.String()+"/anonymize", token, corpo)
	if segunda.Status != http.StatusOK {
		t.Fatalf("segunda = %d: %s", segunda.Status, segunda.Corpo)
	}
	duasVezes := dado[contatos.ResultadoDeAnonimizacao](t, segunda)

	if !umaVez.Contato.AnonimizadoEm.Equal(*duasVezes.Contato.AnonimizadoEm) {
		t.Fatalf("anonymized_at mudou de %v para %v na segunda chamada",
			umaVez.Contato.AnonimizadoEm, duasVezes.Contato.AnonimizadoEm)
	}
	if n := a.contar(t, `SELECT count(*) FROM audit_log WHERE entity='contacts' AND entity_id=$1 AND action='contacts.anonimizado'`, contato); n != 1 {
		t.Fatalf("%d linhas de anonimização na trilha; esperado 1", n)
	}
}

// Não se elimina o dado de quem está hospedado hoje: a operação precisa do nome
// para entregar a chave, e a base legal enquanto o contrato corre é a execução
// do contrato.
func TestAnonimizarRecusaComReservaViva(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	contato := a.contatoDireto(t, "Hospede De Hoje "+sufixo())
	r := a.reserva(t, contato, "confirmed", 5, 2)

	resp := a.chamar(t, http.MethodPost, "/contacts/"+contato.String()+"/anonymize", token,
		map[string]any{"reason": "pedido do titular"})
	if resp.Status != http.StatusConflict || resp.codigoDoErro() != "RESOURCE_IN_USE" {
		t.Fatalf("= %d/%s, esperado 409 RESOURCE_IN_USE: %s", resp.Status, resp.codigoDoErro(), resp.Corpo)
	}
	codigos, _ := resp.detalhes(t)["reservations"].([]any)
	if len(codigos) != 1 || codigos[0] != r.Code {
		t.Fatalf("details.reservations = %v, esperado [%s]", codigos, r.Code)
	}
	if a.contar(t, `SELECT count(*) FROM contacts WHERE id=$1 AND anonymized_at IS NOT NULL`, contato) != 0 {
		t.Fatal("a ficha foi anonimizada apesar da recusa")
	}
}

// Ficha anonimizada não volta a receber dado pessoal: seria `anonymized_at`
// preenchido com PII nova dentro.
func TestFichaAnonimizadaRecusaEscrita(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contatoDireto(t, "Ex Cliente "+sufixo())

	a.chamar(t, http.MethodPost, "/contacts/"+contato.String()+"/anonymize", token,
		map[string]any{"reason": "pedido do titular"})

	patch := a.chamar(t, http.MethodPatch, "/contacts/"+contato.String(), token,
		map[string]any{"name": "Nome De Volta"})
	if patch.Status != http.StatusConflict || patch.codigoDoErro() != "CONTACT_ANONYMIZED" {
		t.Fatalf("PATCH = %d/%s, esperado 409 CONTACT_ANONYMIZED: %s", patch.Status, patch.codigoDoErro(), patch.Corpo)
	}
	put := a.chamar(t, http.MethodPut, "/contacts/"+contato.String(), token,
		map[string]any{"name": "Nome De Volta"})
	if put.Status != http.StatusConflict || put.codigoDoErro() != "CONTACT_ANONYMIZED" {
		t.Fatalf("PUT = %d/%s, esperado 409 CONTACT_ANONYMIZED", put.Status, put.codigoDoErro())
	}
}

// A ficha anonimizada some do seletor de negócio novo, mas continua auditável.
func TestAnonimizadoSaiDaListaPorPadrao(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	nome := "Sumido " + sufixo()
	contato := a.contatoDireto(t, nome)
	a.chamar(t, http.MethodPost, "/contacts/"+contato.String()+"/anonymize", token,
		map[string]any{"reason": "pedido do titular"})

	padrao := a.chamar(t, http.MethodGet, "/contacts?q="+urlescape(nome), token, nil)
	if achados, _ := lista[contatos.Contato](t, padrao); len(achados) != 0 {
		t.Fatalf("a ficha anonimizada apareceu na lista padrão: %+v", achados)
	}

	// A ficha continua existindo — só não é oferecida.
	ficha := a.chamar(t, http.MethodGet, "/contacts/"+contato.String(), token, nil)
	if ficha.Status != http.StatusOK {
		t.Fatalf("GET da ficha anonimizada = %d, esperado 200: sumir com ela faria o painel exibir 'contato inexistente' numa reserva que existe", ficha.Status)
	}
}

// A tabela existia desde 20260820120000 e NENHUMA linha de Go escrevia nela.
// Ler dado pessoal identificável deixa rastro (spec §16).
func TestLeituraDeFichaGravaPIIAccessLog(t *testing.T) {
	a := subir(t)
	ator, token := a.gestor(t)
	contato := a.contatoDireto(t, "Ficha Lida "+sufixo())

	if n := a.contar(t, `SELECT count(*) FROM pii_access_log WHERE contact_id = $1`, contato); n != 0 {
		t.Fatalf("a ficha nasceu com %d acessos registrados", n)
	}

	// A LISTA não grava porque não serve a ficha: documento, telefone e e-mail
	// saem mascarados (F2-23 — ver lista_mascarada_integration_test.go).
	a.chamar(t, http.MethodGet, "/contacts?q=Ficha", token, nil)
	if n := a.contar(t, `SELECT count(*) FROM pii_access_log WHERE contact_id = $1`, contato); n != 0 {
		t.Fatalf("a listagem gravou %d acessos; só a ficha individual deve gravar", n)
	}

	if r := a.chamar(t, http.MethodGet, "/contacts/"+contato.String(), token, nil); r.Status != http.StatusOK {
		t.Fatalf("GET ficha = %d: %s", r.Status, r.Corpo)
	}
	if n := a.contar(t,
		`SELECT count(*) FROM pii_access_log WHERE contact_id=$1 AND actor_id=$2 AND reason='detail'`,
		contato, ator); n != 1 {
		t.Fatalf("%d linhas de acesso com reason='detail'; esperado 1", n)
	}

	if r := a.chamar(t, http.MethodGet, "/contacts/"+contato.String()+"/export", token, nil); r.Status != http.StatusOK {
		t.Fatalf("GET export = %d: %s", r.Status, r.Corpo)
	}
	if n := a.contar(t,
		`SELECT count(*) FROM pii_access_log WHERE contact_id=$1 AND actor_id=$2 AND reason='export'`,
		contato, ator); n != 1 {
		t.Fatalf("%d linhas de acesso com reason='export'; esperado 1 — exportar é a maior leitura de dado pessoal que o sistema faz numa chamada só", n)
	}
}

// Portabilidade em JSON, com a reserva e as noites que o titular pagou.
func TestExportacaoTrazOVinculoFinanceiro(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	contato := a.contatoDireto(t, "Portabilidade "+sufixo())
	r := a.reserva(t, contato, "checked_out", -20, 2)

	resp := a.chamar(t, http.MethodGet, "/contacts/"+contato.String()+"/export", token, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("export = %d: %s", resp.Status, resp.Corpo)
	}
	pacote := dado[contatos.Exportacao](t, resp)

	if pacote.Contato.ID != contato {
		t.Fatalf("o pacote é de %s, esperado %s", pacote.Contato.ID, contato)
	}
	var reservas []struct {
		Code   string `json:"code"`
		Nights []struct {
			Night      string `json:"night"`
			PriceCents int64  `json:"price_cents"`
		} `json:"nights"`
		Pricing struct {
			TotalCents int64 `json:"total_cents"`
		} `json:"pricing"`
	}
	if err := json.Unmarshal(pacote.Reservas, &reservas); err != nil {
		t.Fatalf("lendo reservations do pacote: %v (%s)", err, pacote.Reservas)
	}
	if len(reservas) != 1 || reservas[0].Code != r.Code {
		t.Fatalf("reservations = %+v, esperado a reserva %s", reservas, r.Code)
	}
	if len(reservas[0].Nights) != 2 {
		t.Fatalf("%d noites no pacote, esperado 2: portabilidade sem o detalhe do que foi pago não é portabilidade", len(reservas[0].Nights))
	}
	if reservas[0].Pricing.TotalCents != r.Total {
		t.Errorf("total do pacote = %d, esperado %d", reservas[0].Pricing.TotalCents, r.Total)
	}
	// As coleções vazias saem como `[]`, nunca `null`: o `.map()` do painel
	// estoura justamente no caso mais comum.
	for nome, bruto := range map[string]json.RawMessage{
		"leads": pacote.Leads, "opportunities": pacote.Oportunidades, "activities": pacote.Atividades,
	} {
		if string(bruto) != "[]" {
			t.Errorf("%s = %s, esperado []", nome, bruto)
		}
	}
}

// ═══════════════════════ DELETE ══════════════════════════════════════

// Apaga de verdade, e só o que nunca existiu comercialmente.
func TestExcluirContatoSemVinculo(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	resp := a.chamar(t, http.MethodPost, "/contacts", token, map[string]any{
		"name": "Erro De Digitacao " + sufixo(), "phone_e164": telefoneDeTeste(t),
	})
	criado := dado[contatos.Contato](t, resp)

	if r := a.chamar(t, http.MethodDelete, "/contacts/"+criado.ID.String(), token, nil); r.Status != http.StatusNoContent {
		t.Fatalf("DELETE = %d: %s", r.Status, r.Corpo)
	}
	if n := a.contar(t, `SELECT count(*) FROM contacts WHERE id = $1`, criado.ID); n != 0 {
		t.Fatal("o contato sem vínculo deveria ter sido apagado de verdade")
	}
	// A trilha guarda o que deixou de existir — sem a PII, que acabou de ser
	// apagada e que a trilha não pode ressuscitar.
	a.executar(t, `DELETE FROM audit_log WHERE entity='contacts' AND entity_id=$1`, criado.ID)
}

// Com qualquer vínculo, a resposta é 409 com a contagem e o caminho para
// /anonymize: apagar aqui derrubaria a FK NOT NULL de reservations — o preço de
// "sumir com o cadastro" seria sumir com a venda.
func TestExcluirContatoComReservaRecusaEApontaAnonymize(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	contato := a.contatoDireto(t, "Cliente Com Historia "+sufixo())
	a.reserva(t, contato, "checked_out", -60, 2)

	resp := a.chamar(t, http.MethodDelete, "/contacts/"+contato.String(), token, nil)
	if resp.Status != http.StatusConflict || resp.codigoDoErro() != "RESOURCE_IN_USE" {
		t.Fatalf("= %d/%s, esperado 409 RESOURCE_IN_USE: %s", resp.Status, resp.codigoDoErro(), resp.Corpo)
	}
	d := resp.detalhes(t)
	refs, _ := d["references"].(map[string]any)
	if refs == nil || refs["reservations"] != float64(1) {
		t.Fatalf("details.references = %v, esperado reservations = 1", d["references"])
	}
	dica, _ := d["hint"].(string)
	if dica == "" || !contemAnonymize(dica) {
		t.Fatalf("details.hint = %q, deveria apontar /anonymize", dica)
	}
	if a.contar(t, `SELECT count(*) FROM contacts WHERE id=$1`, contato) != 1 {
		t.Fatal("o contato foi apagado apesar do 409")
	}
}

// ═══════════════════════ Consentimento ═══════════════════════════════

// Consentimento sem data não é consentimento, é afirmação.
func TestOptInDeMarketingExigeDataDeConsentimento(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	semData := a.chamar(t, http.MethodPost, "/contacts", token, map[string]any{
		"name": "Sem Data " + sufixo(), "marketing_opt_in": true,
	})
	if semData.Status != http.StatusUnprocessableEntity {
		t.Fatalf("POST com opt-in sem consent_at = %d, esperado 422: %s", semData.Status, semData.Corpo)
	}

	criado := dado[contatos.Contato](t, a.chamar(t, http.MethodPost, "/contacts", token, map[string]any{
		"name": "Com Data " + sufixo(), "phone_e164": telefoneDeTeste(t),
		"marketing_opt_in": true, "consent_at": time.Now().UTC().Format(time.RFC3339),
	}))
	a.limparContato(t, criado.ID)
	if !criado.OptInMarketing || criado.ConsentimentoEm == nil {
		t.Fatalf("opt-in gravado sem data: %+v", criado)
	}

	// Revogar limpa a data na MESMA escrita: data sobrevivente faria o
	// relatório dizer que a pessoa aceitou.
	revogado := dado[contatos.Contato](t, a.chamar(t, http.MethodPatch, "/contacts/"+criado.ID.String(), token,
		map[string]any{"marketing_opt_in": false}))
	if revogado.OptInMarketing || revogado.ConsentimentoEm != nil {
		t.Fatalf("revogação deixou consent_at vivo: %+v", revogado)
	}

	// Religar sem data e sem data gravada volta a ser 422.
	religar := a.chamar(t, http.MethodPatch, "/contacts/"+criado.ID.String(), token,
		map[string]any{"marketing_opt_in": true})
	if religar.Status != http.StatusUnprocessableEntity {
		t.Fatalf("religar o opt-in sem consent_at = %d, esperado 422: %s", religar.Status, religar.Corpo)
	}
}

// ═══════════════════════ Contrato de entrada ═════════════════════════

func TestCampoDesconhecidoEhRecusado(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	r := a.chamarBruto(t, http.MethodPost, "/contacts", token,
		`{"name":"Ana Silva","telefone":"+5585999990000"}`)
	if r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("campo desconhecido = %d, esperado 422: %s", r.Status, r.Corpo)
	}
	if d := r.detalhes(t); d["telefone"] == nil {
		t.Fatalf("details = %v, esperado apontar o campo `telefone`", d)
	}
}

// "Não achei essa pessoa" e "você perguntou errado" são respostas diferentes, e
// o robô que confunde as duas cria um contato novo a cada mensagem.
func TestBuscaPorTelefoneMalFormatadoEh422(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)

	r := a.chamar(t, http.MethodGet, "/contacts?phone=99999", token, nil)
	if r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("phone inválido = %d, esperado 422: %s", r.Status, r.Corpo)
	}
	if d := r.detalhes(t); d["phone"] == nil {
		t.Fatalf("details = %v, esperado apontar `phone`", d)
	}
}

// ═══════════════════════ RBAC ════════════════════════════════════════

// `contacts` é o único recurso do grupo Comercial com supports_own = false: a
// tabela não tem coluna de dono. Tratar `own` como `all` entregaria a base
// inteira de dados pessoais a quem foi restringido — nega-se, dizendo o porquê.
func TestEscopoOwnEmContatosEhNegado(t *testing.T) {
	a := subir(t)
	_, token := a.usuario(t, a.perfilComEscopo(t, "contatos_own", auth.EscopoOwn, celulasDeContatos()...))

	r := a.chamar(t, http.MethodGet, "/contacts", token, nil)
	if r.Status != http.StatusForbidden || r.codigoDoErro() != "FORBIDDEN" {
		t.Fatalf("= %d/%s, esperado 403 FORBIDDEN: %s", r.Status, r.codigoDoErro(), r.Corpo)
	}
	if d := r.detalhes(t); d["scope"] != "own" {
		t.Fatalf("details = %v, deveria dizer qual escopo é inaplicável", d)
	}
}

// O corretor do seed tem `ver` e `criar` e NÃO tem `editar` nem `excluir`:
// ninguém corrige, anonimiza ou apaga o contato alheio.
func TestCorretorNaoEditaNemAnonimiza(t *testing.T) {
	a := subir(t)
	perfil := a.perfil(t, "contatos_corretor",
		contatos.RecursoContatos+":"+auth.AcaoVer,
		contatos.RecursoContatos+":"+auth.AcaoCriar)
	_, token := a.usuario(t, perfil)

	criado := dado[contatos.Contato](t, a.chamar(t, http.MethodPost, "/contacts", token, map[string]any{
		"name": "Lead Do Corretor " + sufixo(), "phone_e164": telefoneDeTeste(t),
	}))
	a.limparContato(t, criado.ID)

	for _, caso := range []struct {
		metodo, caminho string
		corpo           any
	}{
		{http.MethodPatch, "/contacts/" + criado.ID.String(), map[string]any{"name": "Outro Nome"}},
		{http.MethodDelete, "/contacts/" + criado.ID.String(), nil},
		{http.MethodPost, "/contacts/" + criado.ID.String() + "/anonymize", map[string]any{"reason": "qualquer"}},
	} {
		if r := a.chamar(t, caso.metodo, caso.caminho, token, caso.corpo); r.Status != http.StatusForbidden {
			t.Errorf("%s %s = %d, esperado 403", caso.metodo, caso.caminho, r.Status)
		}
	}
}

// ═══════════════════════ Auxiliares ══════════════════════════════════

func urlescape(s string) string {
	out := make([]byte, 0, len(s)*3)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			out = append(out, c)
			continue
		}
		const hex = "0123456789ABCDEF"
		out = append(out, '%', hex[c>>4], hex[c&0x0f])
	}
	return string(out)
}

func contemAnonymize(s string) bool {
	for i := 0; i+len("/anonymize") <= len(s); i++ {
		if s[i:i+len("/anonymize")] == "/anonymize" {
			return true
		}
	}
	return false
}

// ═══════════════════════ Trilha de auditoria ═════════════════════════

// Toda escrita grava trilha (spec §16) — e, NESTE módulo, sem copiar a PII para
// dentro dela.
//
// `audit.Redigir` protege segredo, não dado pessoal: "name" e "phone_e164" não
// casam com nenhum termo do filtro dele, e não deveriam — numa reserva esses
// valores são justamente o que a auditoria precisa mostrar. Aqui, se a edição
// gravasse `{"name":"Maria"}` em `before`, anonimizar a ficha deixaria a cópia
// do dado eliminado numa tabela que todo perfil com auditoria consegue ler.
func TestEscritaGravaTrilhaSemCopiarAPII(t *testing.T) {
	a := subir(t)
	ator, token := a.gestor(t)

	nome := "Auditada " + sufixo()
	telefone := telefoneDeTeste(t)
	criado := dado[contatos.Contato](t, a.chamar(t, http.MethodPost, "/contacts", token, map[string]any{
		"name": nome, "phone_e164": telefone, "lgpd_basis": contatos.BaseLegitimoInteresse,
	}))
	a.limparContato(t, criado.ID)

	if n := a.contar(t, `
		SELECT count(*) FROM audit_log
		 WHERE entity='contacts' AND entity_id=$1 AND action='contacts.criado' AND actor_id=$2
		   AND ip IS NOT NULL`, criado.ID, ator); n != 1 {
		t.Fatalf("%d linhas de criação na trilha com ator e IP; esperado 1", n)
	}

	outroNome := "Renomeada " + sufixo()
	a.chamar(t, http.MethodPatch, "/contacts/"+criado.ID.String(), token, map[string]any{"name": outroNome})

	// O QUE mudou fica registrado…
	var depois string
	if err := a.pool.QueryRow(a.ctx, `
		SELECT after::text FROM audit_log
		 WHERE entity='contacts' AND entity_id=$1 AND action='contacts.alterado'
		 ORDER BY at DESC LIMIT 1`, criado.ID).Scan(&depois); err != nil {
		t.Fatalf("a alteração não deixou linha na trilha: %v", err)
	}
	if !contem(depois, `"name"`) {
		t.Fatalf("after = %s, deveria registrar que `name` mudou", depois)
	}
	if !contem(depois, "[redigido]") {
		t.Fatalf("after = %s, o valor de `name` deveria estar mascarado", depois)
	}

	// …e o VALOR, não. Nem o antigo, nem o novo, nem o telefone.
	for _, segredo := range []string{nome, outroNome, telefone} {
		n := a.contar(t, `
			SELECT count(*) FROM audit_log
			 WHERE entity='contacts' AND entity_id=$1
			   AND (coalesce(before::text,'') || coalesce(after::text,'')) LIKE '%' || $2 || '%'`,
			criado.ID, segredo)
		if n != 0 {
			t.Errorf("%d linhas de audit_log carregam %q — dado pessoal copiado para a trilha", n, segredo)
		}
	}

	// O que NÃO é PII continua com valor: é o que uma fiscalização pergunta.
	a.chamar(t, http.MethodPatch, "/contacts/"+criado.ID.String(), token,
		map[string]any{"lgpd_basis": contatos.BaseContrato})
	if n := a.contar(t, `
		SELECT count(*) FROM audit_log
		 WHERE entity='contacts' AND entity_id=$1 AND after::text LIKE '%contrato%'`, criado.ID); n == 0 {
		t.Error("a base legal foi mascarada à toa: ela não identifica ninguém e é o que a ANPD pergunta")
	}
}

func contem(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
