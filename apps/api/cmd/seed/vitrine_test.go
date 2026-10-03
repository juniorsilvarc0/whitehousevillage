package main

import (
	"strings"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// O perfil da vitrine serve a uma rota sem login: a matriz dele é exatamente
// contacts:criar e reservations:criar, em `all`. Qualquer célula a mais aqui é
// concedida à internet inteira.
func TestPerfilDaVitrineTemExatamenteDuasCelulas(t *testing.T) {
	linhas, err := matrizDaVitrine()
	if err != nil {
		t.Fatalf("matriz da vitrine inválida: %v", err)
	}
	esperado := map[string]bool{
		"contacts:" + auth.AcaoCriar:     true,
		"reservations:" + auth.AcaoCriar: true,
	}
	if len(linhas) != len(esperado) {
		t.Fatalf("perfil da vitrine com %d células, esperado %d: %+v", len(linhas), len(esperado), linhas)
	}
	for _, l := range linhas {
		if l.perfil != perfilVitrine {
			t.Errorf("linha do perfil %q na matriz da vitrine", l.perfil)
		}
		if !esperado[l.recurso+":"+l.acao] {
			t.Errorf("perfil da vitrine concede %s:%s, fora das duas permitidas", l.recurso, l.acao)
		}
		if l.escopo != escopoAll {
			t.Errorf("perfil da vitrine com escopo %q em %s", l.escopo, l.recurso)
		}
	}
}

// A vitrine não pode entrar pela porta dos perfis de gente: lá o seed não
// apaga acréscimo, e a garantia de "só duas células" deixaria de valer.
func TestPerfilDaVitrineNaoEhPerfilDeGente(t *testing.T) {
	for _, p := range perfisSeed {
		if p.codigo == perfilVitrine {
			t.Fatal("vitrine está em perfisSeed — deve viver só em vitrine.go")
		}
	}
	if _, ok := matrizSeed[perfilVitrine]; ok {
		t.Fatal("vitrine está em matrizSeed — deve viver só em vitrine.go")
	}
	for _, u := range usuariosSeed {
		if strings.EqualFold(u.email, emailContaVitrine) || u.perfil == perfilVitrine {
			t.Fatal("a conta da vitrine não é conta de desenvolvimento e não tem senha conhecida")
		}
	}
}

// O hash precisa ser argon2id VÁLIDO — hash fora do formato faz o login
// responder 500 em vez de INVALID_CREDENTIALS — e não pode verificar com
// nenhuma senha que alguém tentaria.
func TestHashDaVitrineEhValidoENaoAbreComSenhaNenhuma(t *testing.T) {
	h1, err := hashDeSenhaDescartada()
	if err != nil {
		t.Fatal(err)
	}
	h2, err := hashDeSenhaDescartada()
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Fatal("dois hashes iguais — o segredo não é aleatório")
	}
	if !strings.HasPrefix(h1, "$argon2id$") {
		t.Fatalf("hash fora do formato argon2id: %q", h1[:min(12, len(h1))])
	}
	for _, tentativa := range []string{"", senhaDeDesenvolvimento, "vitrine", emailContaVitrine, perfilVitrine} {
		ok, err := auth.Verify(h1, tentativa)
		if err != nil {
			t.Fatalf("Verify devolveu erro — o login responderia 500: %v", err)
		}
		if ok {
			t.Fatalf("a conta da vitrine abre com %q", tentativa)
		}
	}
}
