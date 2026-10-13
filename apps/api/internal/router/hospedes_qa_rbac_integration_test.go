//go:build integration

package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// ─────────────────────────── Cenário com ids reais ──────────────────────────

// qhCenario é uma reserva de pé com titular e um acompanhante, mais uma ficha
// fora da lista — o alvo REAL de cada `{id}` e `{contactId}` das três
// operações. Com id sorteado, um 404 do handler esconderia a falta de checagem
// de permissão; com id real, só o RBAC separa o 403 do sucesso.
type qhCenario struct {
	Casa         qagCasa
	Produto      qagProduto
	Reserva      qagReserva
	Titular      qhPessoa
	Acompanhante qhPessoa
	Fora         qhPessoa
}

func (a *ambiente) qhMontarCenario(t *testing.T, f *qagFaxina, token string, deslocamento int) qhCenario {
	t.Helper()
	c := qhCenario{Casa: a.qagCasa(t, a.qbPropriedadePadrao(t))}
	c.Produto = f.produto(t, c.Casa.Prop, "one_member", 1)
	c.Titular = f.qhPessoa(t, c.Casa.Prop, "Titular")
	c.Acompanhante = f.qhPessoa(t, c.Casa.Prop, "Acompanhante")
	c.Fora = f.qhPessoa(t, c.Casa.Prop, "Fora")
	d := c.Casa.Hoje.AddDays(deslocamento)
	c.Reserva = a.qhReservar(t, token, c.Produto.ID, c.Titular.ID, d, d.AddDays(2), 4)
	a.qhAcrescentarOK(t, token, c.Reserva.ID, c.Acompanhante.ID)
	return c
}

// caminho e corpo de cada operação: o POST acrescenta quem está fora; o DELETE
// retira o acompanhante. Corpo VÁLIDO: se a permissão faltar no servidor, a
// escrita teria efeito — e é isso que o teste confere no banco depois.
func (c qhCenario) caminho(op qbOperacao) string {
	p := strings.Replace(op.Path, "{id}", c.Reserva.ID.String(), 1)
	p = strings.Replace(p, "{contactId}", c.Acompanhante.ID.String(), 1)
	if strings.Contains(p, "{") {
		panic("parâmetro sem alvo no cenário: " + op.Path)
	}
	return p
}

func (c qhCenario) corpo(op qbOperacao) any {
	if op.Metodo == http.MethodPost {
		return map[string]any{"contact_id": c.Fora.ID}
	}
	return nil
}

// qhEstado é o que uma chamada recusada não pode ter mudado: a lista, a trilha
// da lista, a linha do tempo dela e o registro de leitura das três pessoas.
type qhEstado struct {
	Lista, Trilha, Eventos string
	Leituras               int
}

func (a *ambiente) qhLerEstado(t *testing.T, c qhCenario) qhEstado {
	t.Helper()
	var e qhEstado
	if err := a.pool.QueryRow(a.ctx, `
		SELECT coalesce(string_agg(contact_id::text || ':' || is_lead_guest::text, ',' ORDER BY contact_id), '')
		  FROM reservation_guests WHERE reservation_id = $1`, c.Reserva.ID).Scan(&e.Lista); err != nil {
		t.Fatalf("lendo a lista: %v", err)
	}
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*)::text FROM audit_log WHERE entity = 'reservation_guests' AND entity_id = $1`, c.Reserva.ID).Scan(&e.Trilha); err != nil {
		t.Fatalf("lendo a trilha: %v", err)
	}
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*)::text FROM reservation_events WHERE reservation_id = $1 AND type IN ('guest_added', 'guest_removed')`,
		c.Reserva.ID).Scan(&e.Eventos); err != nil {
		t.Fatalf("lendo a linha do tempo: %v", err)
	}
	e.Leituras = a.qhLeiturasDe(t, c.Titular.ID, c.Acompanhante.ID, c.Fora.ID)
	return e
}

// ─────────────────────────── 1. O que o contrato e o seed dizem ─────────────

// A premissa de todos os testes de permissão abaixo, lida das duas fontes: o
// x-rbac das três operações no CONTRATO (ler é `reservations:ver`; acrescentar e
// retirar é `reservations:editar`, nunca `criar`) e a matriz que o SEED entrega
// (`admin` e `usuario` com `ver` e `editar` em `all`; o `corretor` com `ver` e
// `criar` em `own` e SEM `editar`; a vitrine só com `criar`).
func TestHospedesQAOContratoEOSeedConcordamSobreQuemMexeNaLista(t *testing.T) {
	a := subirAPI(t)
	quer := map[string]string{
		"GET /reservations/{id}/guests":                "reservations:ver",
		"POST /reservations/{id}/guests":               "reservations:editar",
		"DELETE /reservations/{id}/guests/{contactId}": "reservations:editar",
	}
	for _, op := range qhOperacoesDaLista(t) {
		if got := op.Recurso + ":" + op.Acao; quer[op.chave()] != got {
			t.Errorf("%s: x-rbac do contrato = %s, esperado %s", op.chave(), got, quer[op.chave()])
		}
	}

	esperado := map[string]string{
		"admin":    "criar,editar,excluir,ver",
		"usuario":  "criar,editar,excluir,ver",
		"corretor": "criar:own,ver:own",
		"vitrine":  "criar",
	}
	for perfil, matriz := range esperado {
		var got string
		if err := a.pool.QueryRow(a.ctx, `
			SELECT coalesce(string_agg(action || CASE WHEN scope = 'all' THEN '' ELSE ':' || scope END, ',' ORDER BY action), '')
			  FROM role_permissions WHERE role_id = $1 AND resource_code = 'reservations'`,
			a.qbPerfilDoSeed(t, perfil)).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != matriz {
			t.Errorf("perfil %s no seed: reservations = [%s], a premissa do contrato é [%s]", perfil, got, matriz)
		}
	}
}

// ─────────────────────────── 2. Quem não opera a casa ───────────────────────

// A conta de serviço da vitrine (o site) tem só `reservations:criar`: 403 nas
// três operações, com o 403 apontando a permissão do x-rbac — e nada muda,
// nem a leitura fica registrada (a lista não saiu).
func TestHospedesQAVitrineRecebe403NasTresOperacoesENadaMuda(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "hq-admin", a.qbPerfilDoSeed(t, "admin"))
	vitrine := a.criarUsuario(t, "hq-vitrine", a.qbPerfilDoSeed(t, "vitrine"))
	f := a.qagFaxina(t, admin, vitrine)
	c := a.qhMontarCenario(t, f, admin.Token, 400)
	antes := a.qhLerEstado(t, c)

	for _, op := range qhOperacoesDaLista(t) {
		t.Run(op.chave(), func(t *testing.T) {
			e := qbErro(t, a.chamar(t, op.Metodo, c.caminho(op), vitrine.Token, c.corpo(op)),
				http.StatusForbidden, "FORBIDDEN", "vitrine em "+op.chave())
			if e.Details["resource"] != op.Recurso || e.Details["action"] != op.Acao {
				t.Errorf("o 403 diz que faltou %v:%v; o x-rbac do contrato é %s:%s",
					e.Details["resource"], e.Details["action"], op.Recurso, op.Acao)
			}
		})
	}
	if depois := a.qhLerEstado(t, c); depois != antes {
		t.Fatalf("a vitrine recebeu 403 e mesmo assim algo mudou:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// Sem sessão — sem token ou com token que não é JWT —, nenhuma das três
// responde, e nada muda.
func TestHospedesQASemSessaoAsTresOperacoesDao401(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "hq-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qagFaxina(t, admin)
	c := a.qhMontarCenario(t, f, admin.Token, 410)
	antes := a.qhLerEstado(t, c)

	for _, op := range qhOperacoesDaLista(t) {
		t.Run(op.chave(), func(t *testing.T) {
			qbErro(t, a.chamar(t, op.Metodo, c.caminho(op), "", c.corpo(op)), http.StatusUnauthorized, "UNAUTHORIZED",
				"sem token em "+op.chave())
			qbErro(t, a.chamar(t, op.Metodo, c.caminho(op), "nao.e.um.jwt", c.corpo(op)), http.StatusUnauthorized, "UNAUTHORIZED",
				"token inválido em "+op.chave())
		})
	}
	if depois := a.qhLerEstado(t, c); depois != antes {
		t.Fatalf("chamadas sem sessão mudaram o estado:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// ─────────────────────────── 3. Quem opera a casa ───────────────────────────

// `admin` e `usuario`, com o perfil REAL do seed, percorrem as três operações e
// recebem o que o contrato promete: 200 com `data` e `meta`, 201 com Location
// e 204 sem corpo.
func TestHospedesQAAdminEUsuarioDoSeedPercorremAsTresOperacoes(t *testing.T) {
	for i, perfil := range []string{"admin", "usuario"} {
		t.Run(perfil, func(t *testing.T) {
			a := subirAPI(t)
			u := a.criarUsuario(t, "hq-"+perfil, a.qbPerfilDoSeed(t, perfil))
			f := a.qagFaxina(t, u)
			c := a.qhMontarCenario(t, f, u.Token, 420+10*i)

			cobertas := map[string]bool{}
			for _, op := range qhOperacoesDaLista(t) {
				r := a.chamar(t, op.Metodo, c.caminho(op), u.Token, c.corpo(op))
				cobertas[op.chave()] = true
				switch op.Metodo {
				case http.MethodGet:
					exigirStatusQB(t, r, http.StatusOK, perfil+" em "+op.chave())
				case http.MethodPost:
					exigirStatusQB(t, r, http.StatusCreated, perfil+" em "+op.chave())
					quer := PrefixoDaAPI + qhCaminho(c.Reserva.ID) + "/" + c.Fora.ID.String()
					if loc := r.Headers.Get("Location"); loc != quer {
						t.Errorf("Location do 201 = %q, o contrato diz %q", loc, quer)
					}
				case http.MethodDelete:
					exigirStatusQB(t, r, http.StatusNoContent, perfil+" em "+op.chave())
					if len(r.Corpo) != 0 {
						t.Errorf("204 com corpo: %s", r.Corpo)
					}
				}
			}
			if len(cobertas) != 3 {
				t.Fatalf("cobri %d operações, o contrato tem 3", len(cobertas))
			}
			qhExigirLista(t, "depois do POST e do DELETE", a.qhLer(t, u.Token, c.Reserva.ID).ids(), c.Titular.ID, c.Fora.ID)
		})
	}
}

// ─────────────────────────── 4. O corretor em `own` ─────────────────────────

// Em linguagem de negócio: o corretor acompanha quem vai dormir nas vendas DELE
// e não enxerga as dos colegas; quem completa a lista é a recepção. Com o
// perfil REAL do seed (`reservations` em `ver` e `criar`, `own`):
//
//   - lê a lista e o /full da venda dele, e cada leitura registra cada pessoa;
//   - na venda de OUTRO corretor recebe 404, nunca 403, e — o ponto que a LGPD
//     cobra — NENHUMA linha nova em `pii_access_log`, nem a dele, nem de
//     ninguém sobre aquelas pessoas;
//   - acrescentar e retirar é 403 `reservations:editar`, na venda dele e na
//     alheia, e nada muda.
func TestHospedesQACorretorDoSeedLeASuaNaoVeADoColegaENaoEdita(t *testing.T) {
	a := subirAPI(t)
	ana := a.criarUsuario(t, "hq-ana", a.qbPerfilDoSeed(t, "corretor"))
	beto := a.criarUsuario(t, "hq-beto", a.qbPerfilDoSeed(t, "corretor"))
	f := a.qagFaxina(t, ana, beto)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 2)
	clienteDaAna := f.qhPessoa(t, casa.Prop, "Cliente da Ana")
	clienteDoBeto := f.qhPessoa(t, casa.Prop, "Cliente do Beto")
	alguem := f.qhPessoa(t, casa.Prop, "Acompanhante")
	d := casa.Hoje.AddDays(450)

	daAna := a.qhReservar(t, ana.Token, p.ID, clienteDaAna.ID, d, d.AddDays(2), 3)
	doBeto := a.qhReservar(t, beto.Token, p.ID, clienteDoBeto.ID, d, d.AddDays(2), 3)

	// A dela: 200, e uma linha por pessoa exibida.
	lida := a.qhLer(t, ana.Token, daAna.ID)
	qhExigirLista(t, "a corretora lê a lista da venda dela", lida.ids(), clienteDaAna.ID)
	if n := a.qhLeiturasDoAtor(t, ana.ID); n != 1 {
		t.Fatalf("a leitura da corretora gravou %d linha(s) de rooming_list, esperado 1", n)
	}
	completa, _ := a.qhCompleta(t, ana.Token, daAna.ID)
	if len(completa.Hospedes) != 1 || completa.Hospedes[0].ContactID != clienteDaAna.ID {
		t.Fatalf("/full da venda dela: guests = %+v", completa.Hospedes)
	}
	if n := a.qhLeiturasDoAtor(t, ana.ID); n != 2 {
		t.Fatalf("o /full da corretora gravou %d linha(s), esperado mais 1 (total 2)", n-1)
	}

	// A do colega: 404 nas duas leituras, e nenhum rastro de leitura.
	antesDela := a.qhLeiturasDoAtor(t, ana.ID)
	antesDeles := a.qhLeiturasDe(t, clienteDoBeto.ID)
	qbErro(t, a.chamar(t, http.MethodGet, qhCaminho(doBeto.ID), ana.Token, nil), http.StatusNotFound, "NOT_FOUND",
		"corretora lendo a lista do colega")
	qbErro(t, a.chamar(t, http.MethodGet, "/reservations/"+doBeto.ID.String()+"/full", ana.Token, nil), http.StatusNotFound,
		"NOT_FOUND", "corretora lendo o /full do colega")
	if n := a.qhLeiturasDoAtor(t, ana.ID); n != antesDela {
		t.Errorf("a leitura recusada gravou %d linha(s) de PII para a corretora", n-antesDela)
	}
	if n := a.qhLeiturasDe(t, clienteDoBeto.ID); n != antesDeles {
		t.Errorf("a leitura recusada gravou %d linha(s) de PII sobre o cliente do colega", n-antesDeles)
	}

	// Escrever: 403 `reservations:editar`, na dela e na do colega.
	for nome, r := range map[string]resposta{
		"POST na dela":      a.qhAcrescentar(t, ana.Token, daAna.ID, alguem.ID),
		"POST na do Beto":   a.qhAcrescentar(t, ana.Token, doBeto.ID, alguem.ID),
		"DELETE na dela":    a.qhRetirar(t, ana.Token, daAna.ID, clienteDaAna.ID),
		"DELETE na do Beto": a.qhRetirar(t, ana.Token, doBeto.ID, clienteDoBeto.ID),
	} {
		e := qbErro(t, r, http.StatusForbidden, "FORBIDDEN", "corretora: "+nome)
		if e.Details["resource"] != "reservations" || e.Details["action"] != auth.AcaoEditar {
			t.Errorf("%s: o 403 aponta %v:%v; o x-rbac do contrato é reservations:editar", nome, e.Details["resource"], e.Details["action"])
		}
	}
	qhExigirLista(t, "a lista dela não mudou", a.qhLer(t, beto.Token, doBeto.ID).ids(), clienteDoBeto.ID)
	if n := a.qhNomesNoBanco(t, daAna.ID); n != 1 {
		t.Fatalf("o 403 mexeu na lista da corretora: %d nomes", n)
	}
}

// Quem edita reservas só as PRÓPRIAS (um perfil da gestão com
// `reservations:editar` em `own` — a célula existe na tela de perfis) recebe
// 404 ao acrescentar ou retirar na reserva de outro dono: a escrita usa o
// escopo de `editar` no SQL, e o 404 não deixa nada para trás.
func TestHospedesQAEscritaEmOwnNaReservaDeOutroDonoEh404(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "hq-admin", a.qbPerfilDoSeed(t, "admin"))
	recepcao := a.criarUsuario(t, "hq-recepcao-own", a.criarPerfil(t, "rooming_own", []auth.Permissao{
		{Resource: "reservations", Action: auth.AcaoVer, Scope: auth.EscopoOwn},
		{Resource: "reservations", Action: auth.AcaoEditar, Scope: auth.EscopoOwn},
	}))
	f := a.qagFaxina(t, admin, recepcao)
	c := a.qhMontarCenario(t, f, admin.Token, 460)
	antes := a.qhLerEstado(t, c)

	qbErro(t, a.qhAcrescentar(t, recepcao.Token, c.Reserva.ID, c.Fora.ID), http.StatusNotFound, "NOT_FOUND",
		"own acrescentando na reserva do admin")
	qbErro(t, a.qhRetirar(t, recepcao.Token, c.Reserva.ID, c.Acompanhante.ID), http.StatusNotFound, "NOT_FOUND",
		"own retirando da reserva do admin")
	qbErro(t, a.chamar(t, http.MethodGet, qhCaminho(c.Reserva.ID), recepcao.Token, nil), http.StatusNotFound, "NOT_FOUND",
		"own lendo a lista do admin")
	if depois := a.qhLerEstado(t, c); depois != antes {
		t.Fatalf("o 404 de escopo mudou alguma coisa:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// ─────────────────────────── 5. Outra propriedade ───────────────────────────

// A lista de uma casa não vaza para o administrador de outra: 404 nas três
// operações e no /full, nenhuma linha de leitura — e a ficha de lá, posta na
// lista de cá, recebe exatamente a mesma resposta de uma ficha que não existe
// (senão a recusa vira oráculo de quais ids existem nas outras casas).
func TestHospedesQAOutraPropriedadeNaoVaza(t *testing.T) {
	a := subirAPI(t)
	casaB := a.qagOutraCasa(t)
	admin := a.criarUsuario(t, "hq-admin", a.qbPerfilDoSeed(t, "admin"))
	adminDeB := a.qbUsuarioEm(t, "hq-admin-b", a.qbPerfilDoSeed(t, "admin"), casaB)
	f := a.qagFaxina(t, admin)
	c := a.qhMontarCenario(t, f, admin.Token, 470)
	deLa := f.qhPessoa(t, casaB, "Hóspede da Casa B")
	antes := a.qhLerEstado(t, c)

	for _, op := range qhOperacoesDaLista(t) {
		qbErro(t, a.chamar(t, op.Metodo, c.caminho(op), adminDeB.Token, c.corpo(op)), http.StatusNotFound, "NOT_FOUND",
			"admin da casa B em "+op.chave())
	}
	qbErro(t, a.chamar(t, http.MethodGet, "/reservations/"+c.Reserva.ID.String()+"/full", adminDeB.Token, nil),
		http.StatusNotFound, "NOT_FOUND", "admin da casa B no /full")
	if depois := a.qhLerEstado(t, c); depois != antes {
		t.Fatalf("a outra casa mexeu ou leu a lista:\n antes  %+v\n depois %+v", antes, depois)
	}
	if n := a.qhLeiturasDoAtor(t, adminDeB.ID); n != 0 {
		t.Fatalf("o admin da casa B gravou %d leitura(s)", n)
	}

	// A ficha de lá e a ficha que não existe: a MESMA resposta, byte a byte.
	r1 := a.qhAcrescentar(t, admin.Token, c.Reserva.ID, deLa.ID)
	r2 := a.qhAcrescentar(t, admin.Token, c.Reserva.ID, uuid.New())
	e1 := qbErro(t, r1, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "ficha de outra propriedade")
	qbErro(t, r2, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "ficha inexistente")
	if _, ok := e1.Details["contact_id"]; !ok {
		t.Errorf("a recusa não aponta contact_id: %v", e1.Details)
	}
	if string(r1.Corpo) != string(r2.Corpo) {
		t.Errorf("ficha de outra casa e ficha inexistente respondem diferente — é um oráculo:\n outra casa  %s\n inexistente %s", r1.Corpo, r2.Corpo)
	}
	for _, proibido := range []string{deLa.Nome, deLa.Documento, deLa.Telefone, casaB.String()} {
		if strings.Contains(string(r1.Corpo), proibido) {
			t.Errorf("a recusa vazou %q da outra casa: %s", proibido, r1.Corpo)
		}
	}
	if depois := a.qhLerEstado(t, c); depois.Lista != antes.Lista {
		t.Fatalf("a ficha de outra casa entrou na lista: %s", depois.Lista)
	}
}
