// Package auth concentra identidade e acesso: senha, token de acesso, refresh
// rotativo e a autorização por dados (papel × recurso × ação × escopo).
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// Parâmetros do argon2id. 64 MiB × 3 iterações é o custo que torna o ataque de
// dicionário caro em GPU sem inviabilizar o login (fica na casa de dezenas de
// milissegundos por verificação num servidor comum).
//
// Estão gravados TAMBÉM dentro do hash: subir o custo no futuro não invalida as
// senhas antigas, porque o Verify lê os parâmetros da própria string.
const (
	argonMemoria     uint32 = 64 * 1024 // KiB
	argonIteracoes   uint32 = 3
	argonParalelismo uint8  = 4
	argonTamanhoSalt uint32 = 16
	argonTamanhoHash uint32 = 32
	argonVersao             = argon2.Version // 19
)

var (
	// ErrHashInvalido é falha de formato do hash guardado, não senha errada.
	ErrHashInvalido = errors.New("hash de senha em formato inválido")
	// ErrVersaoIncompativel protege contra hash gerado por outra versão do algoritmo.
	ErrVersaoIncompativel = errors.New("hash de senha em versão incompatível de argon2")
)

// Hash devolve a senha no formato codificado padrão do argon2:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt-b64>$<hash-b64>
//
// O salt é novo a cada chamada — é por isso que a mesma senha nunca produz duas
// vezes o mesmo hash, e é o que impede rainbow table.
func Hash(senha string) (string, error) {
	salt := make([]byte, argonTamanhoSalt)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("gerando salt: %w", err)
	}

	chave := argon2.IDKey([]byte(senha), salt, argonIteracoes, argonMemoria, argonParalelismo, argonTamanhoHash)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argonVersao, argonMemoria, argonIteracoes, argonParalelismo,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(chave),
	), nil
}

// Verify recalcula o hash com os parâmetros gravados na própria string e compara
// em tempo constante. Comparar com == vazaria, pelo tempo de resposta, quantos
// bytes iniciais o atacante já acertou.
func Verify(codificado, senha string) (bool, error) {
	p, salt, esperado, err := decodificar(codificado)
	if err != nil {
		return false, err
	}

	calculado := argon2.IDKey([]byte(senha), salt, p.iteracoes, p.memoria, p.paralelismo, uint32(len(esperado)))
	return subtle.ConstantTimeCompare(calculado, esperado) == 1, nil
}

type parametrosArgon struct {
	memoria     uint32
	iteracoes   uint32
	paralelismo uint8
}

func decodificar(codificado string) (parametrosArgon, []byte, []byte, error) {
	var p parametrosArgon

	partes := strings.Split(codificado, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	if len(partes) != 6 || partes[0] != "" || partes[1] != "argon2id" {
		return p, nil, nil, ErrHashInvalido
	}

	var versao int
	if _, err := fmt.Sscanf(partes[2], "v=%d", &versao); err != nil {
		return p, nil, nil, ErrHashInvalido
	}
	if versao != argonVersao {
		return p, nil, nil, ErrVersaoIncompativel
	}

	if _, err := fmt.Sscanf(partes[3], "m=%d,t=%d,p=%d", &p.memoria, &p.iteracoes, &p.paralelismo); err != nil {
		return p, nil, nil, ErrHashInvalido
	}
	if p.memoria == 0 || p.iteracoes == 0 || p.paralelismo == 0 {
		return p, nil, nil, ErrHashInvalido
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(partes[4])
	if err != nil {
		return p, nil, nil, ErrHashInvalido
	}
	hash, err := base64.RawStdEncoding.Strict().DecodeString(partes[5])
	if err != nil || len(hash) == 0 {
		return p, nil, nil, ErrHashInvalido
	}
	return p, salt, hash, nil
}

// HashDummy é um hash real de uma senha que ninguém usa, calculado uma vez.
//
// Serve para o login gastar o mesmo tempo quando o e-mail NÃO existe: sem isso,
// "usuário inexistente" responde em microssegundos e "senha errada" em dezenas
// de milissegundos, e a diferença entrega a lista de quem tem conta — o
// oráculo de enumeração que a resposta idêntica do contrato tenta fechar.
var HashDummy = sync.OnceValue(func() string {
	h, err := Hash("senha-inexistente-para-igualar-o-tempo-de-resposta")
	if err != nil {
		// Falha aqui é falta de entropia no sistema; devolver string vazia faria
		// o Verify sair cedo demais e reabrir o canal de timing.
		panic(fmt.Sprintf("auth: não foi possível preparar o hash dummy: %v", err))
	}
	return h
})

// GastarTempoDeVerificacao roda a verificação contra o hash dummy e descarta o
// resultado. É chamada no caminho "e-mail não existe".
func GastarTempoDeVerificacao(senha string) {
	_, _ = Verify(HashDummy(), senha)
}
