//go:build integration

package users_test

import (
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// MÉDIO da 3ª rodada — corrida (TOCTOU) na trava do "último administrador".
//
// `ContarAdministradoresAtivos` rodava FORA da transação e sem trava de linha, e
// a contagem nunca era reconferida dentro do `tx.Do` que grava. Com dois
// administradores X e Y, dois PATCH simultâneos (X rebaixa Y, Y rebaixa X)
// respondiam 200 os dois e a contagem caía para ZERO: a instalação ficava sem
// ninguém capaz de consertar acesso pela API, e a saída era SQL na mão.
//
// Rode com `-count=20`: corrida que só se testa uma vez é corrida que ninguém
// testou.

// duasContasDeAdministrador prepara a instalação com EXATAMENTE dois
// administradores ativos. Desativar o resto é o que torna a contagem
// determinística — a trava só tem o que provar quando o segundo administrador é
// o último.
func duasContasDeAdministrador(t *testing.T, a *ambiente) (idX, idY uuid.UUID, sessaoX, sessaoY sessao, papelRaso uuid.UUID) {
	t.Helper()

	silenciarPopulacao(t, a)

	papelAdmin := a.criarPerfilComCatalogoInteiro(t, "admin_corrida")
	papelRaso = a.criarPerfil(t, "raso", "dashboard:ver:all")

	idX, emailX := a.criarUsuario(t, "x", papelAdmin)
	idY, emailY := a.criarUsuario(t, "y", papelAdmin)

	if n := a.administradoresAtivos(t); n != 2 {
		t.Fatalf("administradores ativos = %d, esperado exatamente 2", n)
	}
	return idX, idY, a.login(t, emailX), a.login(t, emailY), papelRaso
}

// emParalelo dispara as duas requisições ao mesmo tempo e devolve as respostas.
func emParalelo(t *testing.T, primeira, segunda func() resposta) (resposta, resposta) {
	t.Helper()

	var (
		wg     sync.WaitGroup
		largar = make(chan struct{})
		r1, r2 resposta
	)
	wg.Add(2)
	go func() { defer wg.Done(); <-largar; r1 = primeira() }()
	go func() { defer wg.Done(); <-largar; r2 = segunda() }()
	close(largar)
	wg.Wait()
	return r1, r2
}

// O achado: X rebaixa Y e Y rebaixa X no mesmo instante.
func TestRebaixamentoSimultaneoNaoDeixaAInstalacaoSemAdministrador(t *testing.T) {
	a := subir(t)
	idX, idY, sx, sy, papelRaso := duasContasDeAdministrador(t, a)

	corpo := map[string]string{"role_id": papelRaso.String()}
	rX, rY := emParalelo(t,
		func() resposta { return a.chamar(t, http.MethodPatch, "/users/"+idY.String(), sx.Token, corpo) },
		func() resposta { return a.chamar(t, http.MethodPatch, "/users/"+idX.String(), sy.Token, corpo) },
	)

	conferirCorrida(t, a, rX, rY)
}

// O DELETE do contrato tem o mesmo efeito do PATCH {active:false} e, por isso,
// a mesma corrida: X desativa Y enquanto Y desativa X.
func TestDesativacaoSimultaneaNaoDeixaAInstalacaoSemAdministrador(t *testing.T) {
	a := subir(t)
	idX, idY, sx, sy, _ := duasContasDeAdministrador(t, a)

	rX, rY := emParalelo(t,
		func() resposta { return a.chamar(t, http.MethodDelete, "/users/"+idY.String(), sx.Token, nil) },
		func() resposta { return a.chamar(t, http.MethodDelete, "/users/"+idX.String(), sy.Token, nil) },
	)

	conferirCorrida(t, a, rX, rY)
}

// conferirCorrida cobra as três coisas que importam: sobrou administrador, o
// perdedor recebeu 422 (e não 500), e no máximo uma das duas passou.
func conferirCorrida(t *testing.T, a *ambiente, rX, rY resposta) {
	t.Helper()

	restantes := a.administradoresAtivos(t)
	if restantes < 1 {
		t.Fatalf("administradores ativos = %d após as duas requisições (X=%d %s | Y=%d %s)",
			restantes, rX.Status, rX.Corpo, rY.Status, rY.Corpo)
	}

	aceitas := 0
	for _, r := range []resposta{rX, rY} {
		switch {
		case r.Status == http.StatusOK, r.Status == http.StatusNoContent:
			aceitas++
		case r.Status == http.StatusUnprocessableEntity && r.codigoDoErro() == "VALIDATION_ERROR":
			// O perdedor da corrida: a trava do último administrador falou.
		case r.Status == http.StatusUnauthorized:
			// O outro desfecho legítimo, e só no DELETE: o vencedor desativou a
			// conta do perdedor ANTES de a requisição do perdedor carregar a
			// sessão, e o middleware recusou a conta inativa. A operação também
			// não aconteceu — o que importa é que ninguém gravou.
		case r.Status == http.StatusForbidden:
			// O terceiro desfecho legítimo, e o que fazia este teste piscar de
			// vermelho sob carga (1 falha em 10 repetições, medido em
			// 26/08/2026): o vencedor COMMITOU o rebaixamento antes de o RBAC do
			// perdedor ler a matriz, e o perdedor chegou ao autorizador já sem
			// `users:editar`. A trava do último administrador nem chegou a ser
			// consultada — quem recusou foi a autorização, uma camada acima.
			//
			// Em linguagem de negócio é o mesmo desfecho: a escrita não
			// aconteceu e a instalação continua com administrador. Recusar este
			// caso fazia a suíte acusar defeito onde houve só uma ordem
			// diferente de commit — e teste que reprova por sorte é teste que
			// se aprende a ignorar.
			//
			// E não enfraquece a asserção: no OUTRO entrelaçamento, em que as
			// duas requisições são autorizadas antes de qualquer commit, a trava
			// é a única defesa e os dois 200 continuam derrubando o teste. É
			// exatamente por isso que a corrida roda com `-count=10`.
		default:
			t.Fatalf("resposta inesperada: %d %s", r.Status, r.Corpo)
		}
	}
	if aceitas > 1 {
		t.Fatalf("as DUAS requisições foram aceitas: a trava perdeu a corrida (X=%d | Y=%d)",
			rX.Status, rY.Status)
	}
}
