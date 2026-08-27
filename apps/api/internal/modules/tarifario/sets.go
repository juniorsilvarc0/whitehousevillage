package tarifario

import (
	"fmt"
	"strings"
)

// sets monta o `SET` do PATCH a partir dos campos realmente enviados.
//
// Existe para que "campo ausente" continue significando "não mexer" até o SQL:
// um UPDATE com todas as colunas gravaria o zero value de tudo que o cliente
// omitiu, que é exatamente o bug que o `Opt[T]` foi criado para impedir.
//
// Os nomes de coluna são literais deste pacote — nada do que o cliente digitou
// entra na consulta.
type sets struct {
	colunas []string
	refs    map[string]string
	args    []any
}

// novoSets recebe os argumentos que já ocupam posições fixas no WHERE
// (tipicamente $1 = propriedade e $2 = id).
func novoSets(fixos ...any) *sets {
	return &sets{refs: map[string]string{}, args: append([]any{}, fixos...)}
}

func (s *sets) add(coluna string, valor any) {
	s.args = append(s.args, valor)
	ref := fmt.Sprintf("$%d", len(s.args))
	s.refs[coluna] = ref
	s.colunas = append(s.colunas, coluna+" = "+ref)
}

// refDe devolve o placeholder de uma coluna já adicionada, para o WHERE poder
// conferir o valor novo (é assim que a troca de produto de uma tarifa checa se o
// produto é da mesma propriedade).
func (s *sets) refDe(coluna string) string { return s.refs[coluna] }

func (s *sets) vazio() bool { return len(s.colunas) == 0 }

func (s *sets) update(tabela, onde string) string {
	return `UPDATE ` + tabela + ` SET ` + strings.Join(s.colunas, ", ") + ` WHERE ` + onde
}
