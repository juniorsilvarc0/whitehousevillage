package main

import (
	"regexp"
	"strings"
	"testing"
)

// Conferências da declaração dos corretores, sem banco — mesma família de
// acesso_test.go: o erro aparece no `go test`, não no primeiro `make seed`.

// Toda conta de perfil corretor em usuariosSeed tem cadastro em corretoresSeed,
// e só elas. A falta é silenciosa no banco (o JOIN da etapa descartaria a
// conta), e a sobra cria cadastro comercial para a conta da gestão — que, pela
// FK composta, passaria a "ser corretor" para o escopo `own` das reservas.
func TestTodaContaDeCorretorDoSeedTemCadastroComercial(t *testing.T) {
	perfilDa := map[string]string{}
	for _, u := range usuariosSeed {
		perfilDa[strings.ToLower(u.email)] = u.perfil
	}

	cadastradas := map[string]bool{}
	for _, cs := range corretoresSeed {
		email := strings.ToLower(cs.email)
		if cadastradas[email] {
			t.Errorf("%s tem dois cadastros em corretoresSeed — brokers_user_id_key recusaria o segundo", cs.email)
		}
		cadastradas[email] = true

		perfil, existe := perfilDa[email]
		switch {
		case !existe:
			t.Errorf("%s está em corretoresSeed e não em usuariosSeed: o cadastro não teria conta para ligar", cs.email)
		case perfil != "corretor":
			t.Errorf("%s tem cadastro de corretor com perfil %q: a conta passaria a gravar venda no próprio nome", cs.email, perfil)
		}
	}

	var corretores int
	for _, u := range usuariosSeed {
		if u.perfil != "corretor" {
			continue
		}
		corretores++
		if !cadastradas[strings.ToLower(u.email)] {
			t.Errorf("%s é corretor em usuariosSeed e não tem cadastro em corretoresSeed: "+
				"nasce com broker_id nulo e não consegue atribuir venda a si", u.email)
		}
	}
	if corretores == 0 {
		t.Fatal("usuariosSeed sem nenhum corretor — teste que não confere nada passa verde por engano")
	}
}

// Faixa reservada e chave natural livre. Telefone repetido com um contato de
// demonstração não dá erro: as duas etapas gravariam a MESMA ficha, cada uma
// com os próprios dados, e a linha voltaria como "atualizada" em toda execução
// — a idempotência morre sem nenhuma mensagem.
func TestContatoDoCorretorNaoColideComContatoDeDemonstracao(t *testing.T) {
	// +55, DDD de dois dígitos, e o número `9 0000 xxxx`, que não é atribuível
	// a celular no Brasil.
	faixaReservada := regexp.MustCompile(`^\+55\d{2}90000\d{4}$`)

	donoDo := map[string]string{}
	for _, c := range contatosDemo {
		donoDo[c.telefone] = "contato de demonstração " + c.nome
	}

	for _, cs := range corretoresSeed {
		c := cs.contato
		if dono, repetido := donoDo[c.telefone]; repetido {
			t.Errorf("o contato do corretor %s usa %s, que já é do %s", cs.email, c.telefone, dono)
		}
		donoDo[c.telefone] = "corretor " + cs.email

		if !faixaReservada.MatchString(c.telefone) {
			t.Errorf("telefone %q do corretor %s fora da faixa 9 0000 xxxx: pode ser de uma pessoa real", c.telefone, cs.email)
		}
		if !strings.HasSuffix(c.email, ".invalid") {
			t.Errorf("e-mail %q do corretor %s não termina em .invalid (RFC 2606): um disparo acidental sairia", c.email, cs.email)
		}
		if c.optIn != (c.consenti != nil) {
			t.Errorf("contato do corretor %s com optIn=%v e consent_at=%v: os dois andam juntos", cs.email, c.optIn, c.consenti)
		}
	}
}
