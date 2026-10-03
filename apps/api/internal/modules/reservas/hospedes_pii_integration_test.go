//go:build integration

package reservas_test

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// GET /reservations/{id}/full devolve a rooming list com nome e telefone
// CHEIOS — é de onde a recepção liga para quem vai chegar. Até esta rodada a
// leitura não deixava rastro nenhum. O contrato: uma linha de
// `pii_access_log` por hóspede exibido (`reason: rooming_list`), antes da
// resposta, com falha fechada; reserva fora do escopo é 404 sem gravar; e
// reserva sem hóspede não exibiu ninguém.
func TestFullDaReservaRegistraCadaHospedeExibido(t *testing.T) {
	a := subir(t)
	ator, gestor := a.usuario(t, a.perfil(t, "res_rooming",
		"reservations:ver", "reservations:criar", "reservations:editar"))
	produto, titular := a.produto(t, "suite-piscina"), a.contato(t)
	acompanhante := a.contato(t)
	sorteio, err := rand.Int(rand.Reader, big.NewInt(90000000))
	if err != nil {
		t.Fatalf("sorteando telefone: %v", err)
	}
	// Sorteado: o índice único de telefone não pode colidir com o que uma
	// execução anterior interrompida deixou para trás.
	telefone := fmt.Sprintf("+55859%08d", sorteio.Int64()+10000000)
	if _, err := a.pool.Exec(a.ctx, `UPDATE contacts SET phone_e164 = $2 WHERE id = $1`, acompanhante, telefone); err != nil {
		t.Fatalf("dando telefone ao acompanhante: %v", err)
	}
	t.Cleanup(func() {
		a.executar(t, `DELETE FROM pii_access_log WHERE contact_id = ANY($1)`, []uuid.UUID{titular, acompanhante})
	})

	r := a.postarVenda(t, gestor, pedido(produto, titular, a.dia(965), a.dia(969), 2))
	if r.Status != http.StatusCreated {
		t.Fatalf("POST = %d: %s", r.Status, r.Corpo)
	}
	reserva := dado[vendaComCorretor](t, r).ID
	if _, err := a.pool.Exec(a.ctx, `
		INSERT INTO reservation_guests (reservation_id, contact_id, is_lead_guest) VALUES ($1, $2, false)`,
		reserva, acompanhante); err != nil {
		t.Fatalf("acrescentando o acompanhante: %v", err)
	}
	caminho := "/reservations/" + reserva.String() + "/full"

	contar := func() int {
		t.Helper()
		var n int
		if err := a.pool.QueryRow(a.ctx, `
			SELECT count(*) FROM pii_access_log
			 WHERE actor_id = $1 AND reason = 'rooming_list' AND contact_id = ANY($2)`,
			ator, []uuid.UUID{titular, acompanhante}).Scan(&n); err != nil {
			t.Fatalf("contando pii_access_log: %v", err)
		}
		return n
	}

	// Duas leituras, dois hóspedes cada: +2 e +2. A pergunta da LGPD é por
	// pessoa, e cada leitura é um acesso.
	for leitura := 1; leitura <= 2; leitura++ {
		r = a.chamar(t, http.MethodGet, caminho, gestor, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("GET /full = %d: %s", r.Status, r.Corpo)
		}
		if !strings.Contains(string(r.Corpo), telefone) {
			t.Fatalf("a rooming list não trouxe o telefone cheio do acompanhante: %s", r.Corpo)
		}
		if n := contar(); n != 2*leitura {
			t.Fatalf("depois da leitura %d: %d linhas rooming_list, esperado %d (uma por hóspede)", leitura, n, 2*leitura)
		}
	}

	// Fora do escopo: 404 e nada gravado.
	_, estranho := a.usuario(t, a.perfilComEscopo(t, "res_rooming_own", "own", "reservations:ver"))
	if r = a.chamar(t, http.MethodGet, caminho, estranho, nil); r.Status != http.StatusNotFound {
		t.Fatalf("GET /full fora do escopo = %d, esperado 404: %s", r.Status, r.Corpo)
	}
	var total int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM pii_access_log WHERE contact_id = ANY($1)`, []uuid.UUID{titular, acompanhante}).Scan(&total); err != nil {
		t.Fatalf("contando: %v", err)
	}
	if total != 4 {
		t.Fatalf("a leitura recusada gravou acesso: %d linhas, esperado continuar em 4", total)
	}

	// Sem hóspede: a tela sai e não grava nada.
	a.executar(t, `DELETE FROM reservation_guests WHERE reservation_id = $1`, reserva)
	if r = a.chamar(t, http.MethodGet, caminho, gestor, nil); r.Status != http.StatusOK {
		t.Fatalf("GET /full sem hóspede = %d: %s", r.Status, r.Corpo)
	}
	if n := contar(); n != 4 {
		t.Fatalf("reserva sem hóspede gravou %d linha(s) a mais", n-4)
	}
}
