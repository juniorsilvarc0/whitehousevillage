package auth

import (
	"strings"
	"testing"
)

func TestHashEVerify(t *testing.T) {
	const senha = "senha-do-recepcionista-2026"

	codificado, err := Hash(senha)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if !strings.HasPrefix(codificado, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Fatalf("formato codificado fora do padrão: %s", codificado)
	}

	ok, err := Verify(codificado, senha)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("a senha correta deveria verificar")
	}
}

// O salt aleatório é o que impede rainbow table: a mesma senha nunca pode
// produzir duas vezes o mesmo hash.
func TestHashUsaSaltNovoACadaChamada(t *testing.T) {
	const senha = "mesma-senha"

	a, err := Hash(senha)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	b, err := Hash(senha)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if a == b {
		t.Fatal("dois hashes da mesma senha saíram idênticos: o salt não está variando")
	}

	for _, h := range []string{a, b} {
		ok, err := Verify(h, senha)
		if err != nil || !ok {
			t.Fatalf("hash com salt novo deveria verificar (ok=%v err=%v)", ok, err)
		}
	}
}

func TestVerifyRecusaSenhaErrada(t *testing.T) {
	codificado, err := Hash("senha-certa")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	ok, err := Verify(codificado, "senha-errada")
	if err != nil {
		t.Fatalf("Verify não deveria errar com senha só incorreta: %v", err)
	}
	if ok {
		t.Fatal("senha errada não pode verificar")
	}
}

func TestVerifyRecusaHashMalformado(t *testing.T) {
	casos := map[string]string{
		"vazio":                "",
		"sem cifrão":           "argon2id",
		"algoritmo diferente":  "$argon2i$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$aGFzaA",
		"versão incompatível":  "$argon2id$v=16$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$aGFzaA",
		"parâmetros ilegíveis": "$argon2id$v=19$m=abc,t=3,p=4$c2FsdHNhbHRzYWx0$aGFzaA",
		"base64 inválido":      "$argon2id$v=19$m=65536,t=3,p=4$!!!$aGFzaA",
		"parâmetro zerado":     "$argon2id$v=19$m=0,t=3,p=4$c2FsdHNhbHRzYWx0$aGFzaA",
	}

	for nome, codificado := range casos {
		t.Run(nome, func(t *testing.T) {
			ok, err := Verify(codificado, "qualquer")
			if err == nil {
				t.Fatal("hash malformado deveria devolver erro, não apenas falso")
			}
			if ok {
				t.Fatal("hash malformado não pode verificar")
			}
		})
	}
}

// O hash dummy tem de ser um hash de verdade: é ele que faz o caminho
// "e-mail não existe" custar o mesmo tempo que "senha errada".
func TestHashDummyEhVerificavel(t *testing.T) {
	dummy := HashDummy()
	if !strings.HasPrefix(dummy, "$argon2id$") {
		t.Fatalf("hash dummy não é argon2id: %s", dummy)
	}
	if _, err := Verify(dummy, "qualquer-coisa"); err != nil {
		t.Fatalf("hash dummy deveria ser verificável: %v", err)
	}
	if HashDummy() != dummy {
		t.Fatal("hash dummy deveria ser calculado uma única vez")
	}
}
