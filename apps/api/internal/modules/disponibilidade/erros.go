package disponibilidade

import (
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// Os códigos estáveis que a emissão do orçamento acrescentou ao contrato.
//
// Eles PERTENCEM a `internal/platform/apperr` — é lá que o vocabulário estável
// da API mora, e o enum de `components.responses.Erro` já os lista. Estão aqui
// porque `internal/platform` não é pasta deste agente nesta rodada, e editar o
// mesmo arquivo em paralelo é a colisão que a divisão por pasta existe para
// evitar. São construídos pela mesma forma que o pacote usa (código + mensagem +
// status), então mudam de casa sem mudar de comportamento.
//
// PARA O INTEGRADOR: ver o relatório — `QUOTE_NOT_PENDING` e `CONTACT_ANONYMIZED`
// são irmãos de `DISCOUNT_ABOVE_LIMIT` e deviam morar ao lado dele.
func erro(codigo, mensagem string, status int) *apperr.Error {
	return (&apperr.Error{Code: codigo, Message: mensagem}).WithStatus(status)
}

var (
	// OrcamentoNaoEstaDePe — o orçamento apontado venceu (`valid_until` no
	// passado) ou já virou venda (`reservation_id` preenchido).
	//
	// 422 e não 409: nenhuma repetição resolve, e o caminho de saída é emitir
	// OUTRO. Preço vencido não se renova sozinho — reusá-lo é vender pelo número
	// que a casa já retirou de circulação.
	OrcamentoNaoEstaDePe = erro("QUOTE_NOT_PENDING",
		"Este orçamento não está mais de pé: emita outro.", http.StatusUnprocessableEntity)

	// ContatoAnonimizado — o contato exerceu o direito de eliminação da LGPD.
	// Não se emite proposta nova para quem pediu para ser esquecido.
	ContatoAnonimizado = erro("CONTACT_ANONYMIZED",
		"Este contato foi anonimizado; não é possível emitir proposta para ele.", http.StatusConflict)
)
