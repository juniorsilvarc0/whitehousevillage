package bens

import (
	"regexp"
	"strconv"
	"strings"
)

// O `code` do cômodo é a identidade estável dele na unidade (unit_rooms.code,
// 20261007213000): minúsculo, sem acento, separado por hífen, até 60
// caracteres, único na unidade e NÃO editável depois de criado. `name` é o
// rótulo que o gestor edita; é pelo `code` que a cópia entre unidades (e a
// importação do levantamento fotográfico) reencontra o mesmo cômodo depois de
// um rename.
//
// A derivação abaixo é a MESMA do backfill da migration 20261007213000 (e de
// docs/db.md §11), passo a passo: as duas implementações têm de dar o mesmo
// resultado caractere a caractere, senão o cômodo criado pela API e o migrado
// pelo backfill divergem para o mesmo nome. É aritmética pura de string,
// testada em codigo_test.go com os exemplos da migration.

const (
	tamanhoMaximoDoCodigo = 60
	codigoDeNomeVazio     = "comodo"

	// tentativasDeSufixo é quantos candidatos (`base`, `base-2`, …) o POST
	// tenta antes de desistir com 409. Cinquenta cômodos de mesmo nome base na
	// mesma unidade não é planta de casa; é laço de cliente.
	tentativasDeSufixo = 50
)

// padraoDoCodigo é o `pattern` do contrato e o CHECK unit_rooms_code_formato.
var padraoDoCodigo = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// CodigoValido diz se o `code` informado pelo cliente cabe no formato e no
// tamanho. Recusa maiúscula, acento, `_`, espaço, hífen nas pontas e duplo.
func CodigoValido(codigo string) bool {
	return len(codigo) <= tamanhoMaximoDoCodigo && padraoDoCodigo.MatchString(codigo)
}

// traducaoDoCodigo é a tabela FIXA do passo 2 — e nada além dela. Não se usa
// strings.ToLower: a minúscula Unicode do Go e o `lower()` do Postgres
// discordam fora do ASCII, e a regra tem de ser a mesma nos dois.
var traducaoDoCodigo = func() map[rune]rune {
	t := map[rune]rune{}
	for alvo, origens := range map[rune]string{
		'a': "ÀÁÂÃÄàáâãä",
		'e': "ÈÉÊËèéêë",
		'i': "ÌÍÎÏìíîï",
		'o': "ÒÓÔÕÖòóôõö",
		'u': "ÙÚÛÜùúûü",
		'c': "Çç",
		'n': "Ññ",
	} {
		for _, r := range origens {
			t[r] = alvo
		}
	}
	for r := 'A'; r <= 'Z'; r++ {
		t[r] = r - 'A' + 'a'
	}
	return t
}()

// BaseDoCodigo deriva a base do `code` a partir do `name`:
//
//  1. remove as marcas diacríticas combinantes (U+0300–U+036F), que aparecem
//     quando o nome chega decomposto (NFD), senão "Área" viraria "a-rea";
//  2. troca pela tabela fixa (acentos do português e A–Z ASCII);
//  3. cada sequência MÁXIMA fora de [a-z0-9] vira UM hífen — letra que a tabela
//     não traduz ("ø", "ß", emoji) cai aqui como qualquer símbolo;
//  4. tira os hífens das pontas;
//  5. acima de 60, fica com os 60 primeiros e tira o hífen do fim (depois do
//     passo 3 é ASCII puro: caractere = byte);
//  6. vazio vira `comodo`.
func BaseDoCodigo(nome string) string {
	var b strings.Builder
	hifen := false
	for _, r := range nome {
		if r >= 0x0300 && r <= 0x036F { // passo 1
			continue
		}
		if t, ok := traducaoDoCodigo[r]; ok { // passo 2
			r = t
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			hifen = false
			continue
		}
		if !hifen { // passo 3
			b.WriteByte('-')
			hifen = true
		}
	}
	base := strings.Trim(b.String(), "-")  // passo 4
	if len(base) > tamanhoMaximoDoCodigo { // passo 5
		base = strings.TrimRight(base[:tamanhoMaximoDoCodigo], "-")
	}
	if base == "" { // passo 6
		return codigoDeNomeVazio
	}
	return base
}

// CandidatoDoCodigo é o n-ésimo candidato da sequência `base, base-2, base-3…`.
// Com sufixo, a base é antes cortada em 60 − len("-n") e perde o hífen que
// sobrar no fim — o resultado nunca passa de 60.
func CandidatoDoCodigo(base string, n int) string {
	if n <= 1 {
		return base
	}
	sufixo := "-" + strconv.Itoa(n)
	corte := min(len(base), tamanhoMaximoDoCodigo-len(sufixo))
	return strings.TrimRight(base[:corte], "-") + sufixo
}
