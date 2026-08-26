package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// tamanhoRefresh é o número de bytes aleatórios do refresh token. 32 bytes
// (256 bits) tornam a adivinhação impossível na prática.
const tamanhoRefresh = 32

// NovoRefreshToken devolve o token em claro (que vai para o cookie do cliente e
// some da memória do servidor) e o sha256 dele (o único que o banco guarda).
//
// Guardar só o hash é o mesmo raciocínio da senha: um dump da tabela
// refresh_tokens não dá sessão a ninguém. Aqui basta sha256 sem custo de
// derivação — o segredo tem 256 bits de entropia real, então não há dicionário
// a proteger, ao contrário da senha escolhida por gente.
func NovoRefreshToken() (claro string, hash string, err error) {
	bruto := make([]byte, tamanhoRefresh)
	if _, err := rand.Read(bruto); err != nil {
		return "", "", fmt.Errorf("gerando refresh token: %w", err)
	}
	claro = base64.RawURLEncoding.EncodeToString(bruto)
	return claro, HashRefreshToken(claro), nil
}

// HashRefreshToken é a função usada tanto na emissão quanto na busca: o
// repositório procura pelo hash, nunca pelo token em claro.
func HashRefreshToken(claro string) string {
	soma := sha256.Sum256([]byte(claro))
	return hex.EncodeToString(soma[:])
}

// NovoTokenDeReset gera o token de recuperação de senha, com a mesma mecânica
// do refresh: 32 bytes aleatórios, hash no banco, claro só no e-mail.
func NovoTokenDeReset() (claro string, hash string, err error) {
	return NovoRefreshToken()
}
