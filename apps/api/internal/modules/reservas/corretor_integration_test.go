//go:build integration

package reservas_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Corretor da venda (F2-09/F2-13). Medido em 31/08/2026: `corretor@wh.local`,
// com `reservations:criar` em `own`, gravou a venda no `broker_id` de outro
// corretor e recebeu 201 (WH-2026-0009); e um UUID inventado também entrava
// (WH-2026-0008). A partir do F2-13 a comissão nasce deste campo.
//
// Os testes usam as contas e a MATRIZ do seed (`corretor@wh.local`,
// `admin@wh.local`, o perfil `corretor`), e não perfis montados para o teste:
// é a medida do backlog que precisa ficar vermelha, não uma parecida.

// corretorVinculado cria uma conta com o perfil dado, ligada a um cadastro de
// corretor (contato + brokers + users.broker_id, na ordem que a FK composta
// `users_broker_id_fkey` exige). Devolve conta, corretor e token.
func (a *ambiente) corretorVinculado(t *testing.T, perfil uuid.UUID) (uuid.UUID, uuid.UUID, string) {
	t.Helper()
	conta, token := a.usuario(t, perfil)

	var contato, corretor uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO contacts (property_id, name) VALUES ($1, $2) RETURNING id`,
		a.propriedade, "Corretor QA "+sufixo()).Scan(&contato); err != nil {
		t.Fatalf("criando o contato do corretor: %v", err)
	}
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO brokers (property_id, contact_id, user_id) VALUES ($1, $2, $3) RETURNING id`,
		a.propriedade, contato, conta).Scan(&corretor); err != nil {
		t.Fatalf("criando o cadastro de corretor: %v", err)
	}
	if _, err := a.pool.Exec(a.ctx, `UPDATE users SET broker_id = $2 WHERE id = $1`, conta, corretor); err != nil {
		t.Fatalf("ligando a conta ao corretor: %v", err)
	}
	// Roda ANTES da limpeza da conta (LIFO): a FK composta e a RESTRICT de
	// reservations.broker_id exigem desligar, apagar as vendas dele e só
	// então o cadastro e o contato.
	t.Cleanup(func() {
		a.executar(t, `UPDATE users SET broker_id = NULL WHERE id = $1`, conta)
		a.executar(t, `DELETE FROM reservations WHERE broker_id = $1`, corretor)
		a.executar(t, `DELETE FROM brokers WHERE id = $1`, corretor)
		a.executar(t, `DELETE FROM contacts WHERE id = $1`, contato)
	})
	return conta, corretor, token
}

// sessaoDoSeed assina um token para uma conta do SEED, com o perfil dela.
func (a *ambiente) sessaoDoSeed(t *testing.T, email string) (uuid.UUID, *uuid.UUID, string) {
	t.Helper()
	var (
		id       uuid.UUID
		corretor *uuid.UUID
		codigo   string
	)
	if err := a.pool.QueryRow(a.ctx, `
		SELECT u.id, u.broker_id, r.code FROM users u JOIN roles r ON r.id = u.role_id WHERE u.email = $1`,
		email).Scan(&id, &corretor, &codigo); err != nil {
		t.Fatalf("conta %s do seed (rodou cmd/seed?): %v", email, err)
	}
	assinado, _, err := a.emissor.Issue(id, codigo)
	if err != nil {
		t.Fatalf("assinando token: %v", err)
	}
	return id, corretor, assinado
}

func (a *ambiente) perfilDoSeed(t *testing.T, codigo string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT id FROM roles WHERE code = $1`, codigo).Scan(&id); err != nil {
		t.Fatalf("perfil %s do seed: %v", codigo, err)
	}
	return id
}

type vendaComCorretor struct {
	ID       uuid.UUID  `json:"id"`
	Status   string     `json:"status"`
	BrokerID *uuid.UUID `json:"broker_id"`
}

// postarVenda manda o POST com chave nova e devolve a resposta crua.
func (a *ambiente) postarVenda(t *testing.T, token string, corpo map[string]any) resposta {
	t.Helper()
	chave := "it-corretor-" + sufixo() + "-" + sufixo()
	a.limparChaves(t, chave)
	return a.chamarIdem(t, http.MethodPost, "/reservations", token, corpo, chave)
}

func exigirRecusaDoCorretor(t *testing.T, r resposta, motivo, contexto string) {
	t.Helper()
	if r.Status != http.StatusForbidden || r.codigoDoErro() != "FORBIDDEN" {
		t.Fatalf("%s = %d/%s, esperado 403 FORBIDDEN: %s", contexto, r.Status, r.codigoDoErro(), r.Corpo)
	}
	d := r.detalhes(t)
	if d["field"] != "broker_id" || d["scope"] != "own" || d["reason"] != motivo {
		t.Fatalf("%s: details = %v, esperado field=broker_id scope=own reason=%s", contexto, d, motivo)
	}
}

func (a *ambiente) corretorGravado(t *testing.T, reserva uuid.UUID) *uuid.UUID {
	t.Helper()
	var c *uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT broker_id FROM reservations WHERE id = $1`, reserva).Scan(&c); err != nil {
		t.Fatalf("lendo broker_id: %v", err)
	}
	return c
}

func mesmo(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// A medida do WH-2026-0009, com a conta e a matriz do seed: o corretor tenta
// pôr a venda no corretor de um colega (que existe) e num id inventado. As
// duas são 403 not_actor_broker — 422 "não existe" no segundo caso ensinaria,
// por tentativa, quais ids existem. E nenhuma reserva nasce.
func TestCorretorOwnNaoAtribuiAVendaAoColega(t *testing.T) {
	a := subir(t)
	_, _, token := a.sessaoDoSeed(t, "corretor@wh.local")
	_, colega, _ := a.corretorVinculado(t, a.perfilDoSeed(t, "corretor"))
	produto, contato := a.produto(t, "suite-piscina"), a.contato(t)

	for nome, valor := range map[string]any{
		"o corretor do colega": colega,
		"um id inventado":      uuid.New(),
	} {
		corpo := pedido(produto, contato, a.dia(930), a.dia(934), 2)
		corpo["broker_id"] = valor
		exigirRecusaDoCorretor(t, a.postarVenda(t, token, corpo), "not_actor_broker", "POST com "+nome)
	}
	var n int
	if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM reservations WHERE contact_id = $1`, contato).Scan(&n); err != nil {
		t.Fatalf("contando reservas: %v", err)
	}
	if n != 0 {
		t.Fatalf("a recusa deixou %d reserva(s) gravada(s)", n)
	}
}

// Em escopo `own`, ausente grava o corretor da PRÓPRIA conta; `null` grava venda
// direta; o próprio id passa.
func TestCorretorOwnOmitindoGravaOProprio(t *testing.T) {
	a := subir(t)
	_, proprio, token := a.sessaoDoSeed(t, "corretor@wh.local")
	if proprio == nil {
		t.Fatal("corretor@wh.local sem users.broker_id: o seed (corretores_de_desenvolvimento) não ligou a conta")
	}
	produto, contato := a.produto(t, "suite-piscina"), a.contato(t)

	casos := []struct {
		nome     string
		ajustar  func(map[string]any)
		esperado *uuid.UUID
		de       int
	}{
		{"omitido", func(map[string]any) {}, proprio, 935},
		{"null", func(c map[string]any) { c["broker_id"] = nil }, nil, 940},
		{"o próprio", func(c map[string]any) { c["broker_id"] = *proprio }, proprio, 945},
	}
	for _, c := range casos {
		corpo := pedido(produto, contato, a.dia(c.de), a.dia(c.de+4), 2)
		c.ajustar(corpo)
		r := a.postarVenda(t, token, corpo)
		if r.Status != http.StatusCreated {
			t.Fatalf("%s: POST = %d: %s", c.nome, r.Status, r.Corpo)
		}
		v := dado[vendaComCorretor](t, r)
		if !mesmo(v.BrokerID, c.esperado) || !mesmo(a.corretorGravado(t, v.ID), c.esperado) {
			t.Fatalf("%s: broker_id = %v (banco %v), esperado %v", c.nome, v.BrokerID, a.corretorGravado(t, v.ID), c.esperado)
		}
	}
}

// Escopo `all` (admin do seed): corretor inexistente é 422 em
// `details.broker_id` — pela FK, sem SELECT antes —; existente é 201, e a
// escrita entra em audit_log com o corretor.
func TestAdminAtribuiCorretorExistenteEOInexistenteEh422(t *testing.T) {
	a := subir(t)
	_, _, token := a.sessaoDoSeed(t, "admin@wh.local")
	_, colega, _ := a.corretorVinculado(t, a.perfilDoSeed(t, "corretor"))
	produto, contato := a.produto(t, "suite-piscina"), a.contato(t)

	corpo := pedido(produto, contato, a.dia(950), a.dia(954), 2)
	corpo["broker_id"] = uuid.New()
	r := a.postarVenda(t, token, corpo)
	if r.Status != http.StatusUnprocessableEntity || r.codigoDoErro() != "VALIDATION_ERROR" {
		t.Fatalf("POST com corretor inexistente = %d/%s, esperado 422 VALIDATION_ERROR: %s", r.Status, r.codigoDoErro(), r.Corpo)
	}
	if d := r.detalhes(t); d["broker_id"] == nil {
		t.Fatalf("details sem broker_id: %v", d)
	}

	corpo["broker_id"] = colega
	r = a.postarVenda(t, token, corpo)
	if r.Status != http.StatusCreated {
		t.Fatalf("POST com corretor existente = %d: %s", r.Status, r.Corpo)
	}
	v := dado[vendaComCorretor](t, r)
	if !mesmo(v.BrokerID, &colega) {
		t.Fatalf("broker_id = %v, esperado %v", v.BrokerID, colega)
	}
	var auditado int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM audit_log
		 WHERE entity = 'reservations' AND entity_id = $1 AND after ->> 'broker_id' = $2`,
		v.ID, colega.String()).Scan(&auditado); err != nil {
		t.Fatalf("lendo a trilha: %v", err)
	}
	if auditado != 1 {
		t.Fatalf("%d linha(s) de audit_log com o corretor da venda; esperado 1", auditado)
	}

	// Trocar pelo PATCH, em `all`, também é 422 para o inexistente e entra
	// na trilha com antes e depois.
	r = a.chamar(t, http.MethodPatch, "/reservations/"+v.ID.String(), token, map[string]any{"broker_id": uuid.New()})
	if r.Status != http.StatusUnprocessableEntity || r.detalhes(t)["broker_id"] == nil {
		t.Fatalf("PATCH com corretor inexistente = %d: %s", r.Status, r.Corpo)
	}
	r = a.chamar(t, http.MethodPatch, "/reservations/"+v.ID.String(), token, map[string]any{"broker_id": nil})
	if r.Status != http.StatusOK || a.corretorGravado(t, v.ID) != nil {
		t.Fatalf("PATCH broker_id=null = %d: %s", r.Status, r.Corpo)
	}
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM audit_log
		 WHERE entity = 'reservations' AND entity_id = $1
		   AND before ->> 'broker_id' = $2 AND after -> 'broker_id' = 'null'::jsonb`,
		v.ID, colega.String()).Scan(&auditado); err != nil {
		t.Fatalf("lendo a trilha da troca: %v", err)
	}
	if auditado != 1 {
		t.Fatalf("a troca do corretor não entrou em audit_log com antes e depois (%d linhas)", auditado)
	}
}

// Edição em escopo `own`: trocar só entre `null` e o próprio, só sobre venda
// que não é de outro corretor, só em quote/hold. Reenviar o gravado passa.
func TestCorretorOwnNaEdicao(t *testing.T) {
	a := subir(t)
	perfil := a.perfilComEscopo(t, "res_corretor_editor", "own",
		"reservations:ver", "reservations:criar", "reservations:editar")
	_, proprio, token := a.corretorVinculado(t, perfil)
	_, colega, _ := a.corretorVinculado(t, a.perfilDoSeed(t, "corretor"))
	_, _, admin := a.sessaoDoSeed(t, "admin@wh.local")
	produto, contato := a.produto(t, "suite-piscina"), a.contato(t)

	r := a.postarVenda(t, token, pedido(produto, contato, a.dia(955), a.dia(959), 2))
	if r.Status != http.StatusCreated {
		t.Fatalf("POST = %d: %s", r.Status, r.Corpo)
	}
	v := dado[vendaComCorretor](t, r)
	if !mesmo(v.BrokerID, &proprio) {
		t.Fatalf("POST omitido gravou %v, esperado o próprio %v", v.BrokerID, proprio)
	}
	caminho := "/reservations/" + v.ID.String()

	exigirRecusaDoCorretor(t, a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"broker_id": colega}),
		"not_actor_broker", "PATCH para o colega")
	if r := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"broker_id": nil}); r.Status != http.StatusOK || a.corretorGravado(t, v.ID) != nil {
		t.Fatalf("PATCH para null = %d: %s", r.Status, r.Corpo)
	}
	// PUT sem broker_id volta ao padrão do escopo: o próprio, e não null.
	if r := a.chamar(t, http.MethodPut, caminho, token, map[string]any{"contact_id": contato, "guests_count": 2}); r.Status != http.StatusOK || !mesmo(a.corretorGravado(t, v.ID), &proprio) {
		t.Fatalf("PUT sem broker_id = %d, gravou %v; esperado o próprio: %s", r.Status, a.corretorGravado(t, v.ID), r.Corpo)
	}

	// A gestão atribui a venda ao colega; o corretor não a toma de volta, mas
	// reenviar o valor gravado não é troca.
	if r := a.chamar(t, http.MethodPatch, caminho, admin, map[string]any{"broker_id": colega}); r.Status != http.StatusOK {
		t.Fatalf("PATCH do admin = %d: %s", r.Status, r.Corpo)
	}
	exigirRecusaDoCorretor(t, a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"broker_id": nil}),
		"replaces_other_broker", "PATCH tirando o colega")
	if r := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"broker_id": colega, "guests_count": 3}); r.Status != http.StatusOK {
		t.Fatalf("reenviar o corretor gravado = %d, esperado 200: %s", r.Status, r.Corpo)
	}

	// Confirmada, a venda gera comissão: trocar exige `all`.
	if r := a.chamar(t, http.MethodPatch, caminho, admin, map[string]any{"broker_id": proprio}); r.Status != http.StatusOK {
		t.Fatalf("PATCH do admin de volta = %d: %s", r.Status, r.Corpo)
	}
	if _, err := a.pool.Exec(a.ctx,
		`UPDATE reservations SET status = 'confirmed', confirmed_at = now(), hold_expires_at = NULL WHERE id = $1`, v.ID); err != nil {
		t.Fatalf("confirmando por SQL: %v", err)
	}
	exigirRecusaDoCorretor(t, a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"broker_id": nil}),
		"reservation_confirmed", "PATCH em reserva confirmada")
	if r := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"broker_id": proprio}); r.Status != http.StatusOK {
		t.Fatalf("reenviar o próprio em confirmada = %d, esperado 200: %s", r.Status, r.Corpo)
	}
}

// A guarda do BANCO, sem o service no caminho. O service decide com o
// `users.broker_id` que leu; a instrução que grava confere de novo, no mesmo
// SQL. Este teste é o cenário que só a segunda conferência pega: a conta foi
// desvinculada (ou trocada) entre a leitura e a escrita — TOCTOU.
//
// Tudo numa transação desfeita no fim: nenhuma reserva de verdade nasce.
func TestGuardaDoCorretorNoSQLRecusaContaQueMudou(t *testing.T) {
	a := subir(t)
	perfil := a.perfilComEscopo(t, "res_guarda", "own", "reservations:criar", "reservations:editar")
	conta, proprio, _ := a.corretorVinculado(t, perfil)
	_, colega, _ := a.corretorVinculado(t, a.perfilDoSeed(t, "corretor"))
	produto, contato := a.produto(t, "suite-piscina"), a.contato(t)

	repo := reservas.NewRepository(a.pool)
	tx := db.NewTxManager(a.pool)
	desfazer := errors.New("desfaz a transação do teste")

	nova := func(corretor uuid.UUID) reservas.NovaReserva {
		return reservas.NovaReserva{
			PropertyID: a.propriedade, UnitTypeID: produto, ContactID: contato,
			BrokerID: &corretor, OwnerID: &conta, Origem: "direto", Status: "quote",
			CheckIn: a.dia(960), CheckOut: a.dia(964), Hospedes: 2, CriadaPor: &conta,
			CorretorRestritoA: &conta,
		}
	}

	casos := []struct {
		nome   string
		antes  string // o que muda na conta "entre a leitura e a escrita"
		pedido uuid.UUID
	}{
		{"o corretor de outra conta", ``, colega},
		{"a conta desvinculada no meio", `UPDATE users SET broker_id = NULL WHERE id = $1`, proprio},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			err := tx.Do(a.ctx, func(ctx context.Context) error {
				if c.antes != "" {
					if _, err := db.From(ctx, a.pool).Exec(ctx, c.antes, conta); err != nil {
						t.Fatalf("mudando a conta: %v", err)
					}
				}
				if _, _, _, err := repo.InserirReserva(ctx, nova(c.pedido)); err != nil {
					return err
				}
				return desfazer
			})
			ae := apperr.From(err)
			if errors.Is(err, desfazer) || ae.Code != "FORBIDDEN" {
				t.Fatalf("INSERT com %s: err = %v, esperado 403 FORBIDDEN da guarda do SQL", c.nome, err)
			}
			if d, ok := ae.Details.(map[string]string); !ok || d["reason"] != "not_actor_broker" {
				t.Fatalf("details = %#v", ae.Details)
			}
		})
	}

	// O mesmo no UPDATE: a venda é do próprio; trocar para o colega com a
	// guarda de `own` não grava.
	var reserva uuid.UUID
	err := tx.Do(a.ctx, func(ctx context.Context) error {
		id, _, _, err := repo.InserirReserva(ctx, nova(proprio))
		if err != nil {
			t.Fatalf("o próprio corretor foi recusado no INSERT: %v", err)
		}
		reserva = id
		return repo.AtualizarCadastro(ctx, id, contato,
			reservas.AtribuicaoDoCorretor{Corretor: &colega, RestritoA: &conta}, 2, false, nil, "direto", nil)
	})
	if apperr.From(err).Code != "FORBIDDEN" {
		t.Fatalf("UPDATE para o colega: err = %v, esperado 403 FORBIDDEN da guarda do SQL", err)
	}
	var sobrou int
	if qerr := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM reservations WHERE id = $1`, reserva).Scan(&sobrou); qerr != nil || sobrou != 0 {
		t.Fatalf("a transação recusada deixou a reserva (%d, %v)", sobrou, qerr)
	}
}
