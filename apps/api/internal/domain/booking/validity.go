package booking

import (
	"fmt"
	"time"
)

// QuoteValidityMaxDays é o teto de `quote_validity_days` aceito na publicação
// da política (contrato: PoliticaComercialEntrada).
//
// Não é regra comercial — a regra é o número que a gestão publica, e esse é dado
// versionado. É guarda de DIGITAÇÃO: um orçamento cuja validade passa de um ano
// sobrevive à tabela de tarifas que o precificou, e `3650` no lugar de `365`
// congelaria o preço em `quotes.valid_until` por dez anos, sem volta (o
// orçamento emitido não se reescreve). O piso é o `CHECK > 0` do banco.
const QuoteValidityMaxDays = 365

// QuoteValidUntil resolve até quando um orçamento EMITIDO fica de pé.
//
//   - Com `requested`, vale o pedido — desde que posterior à emissão. No
//     passado (ou no instante da emissão) é VALIDATION_ERROR em `valid_until`:
//     proposta que nasce vencida não é proposta, e o motivo merece o nome do
//     campo, não a mensagem genérica do `CHECK quotes_validade` no COMMIT.
//   - Sem `requested`, vale `issued + pol.QuoteValidityDays` dias de
//     calendário, no fuso de `issued`. Quem chama passa o "agora" do banco já
//     em America/Fortaleza; somar DIAS (e não 24 h × N) é o que mantém a hora
//     do dia estável se a casa algum dia voltar a ter horário de verão.
//
// `pol` é a política que PRECIFICOU o orçamento — a mesma de `policy_version`.
// Ler a vigente de novo abriria a janela em que o preço sai de uma versão e a
// validade de outra.
//
// Política sem validade (zero ou negativa) é ERRO, e não RuleError: a coluna é
// `NOT NULL CHECK > 0`, então zero aqui só aparece quando quem montou a Policy
// esqueceu de ler o campo. Devolver `issued` faria o orçamento nascer vencido e
// estourar no CHECK — a falha certa é alta e imediata, no primeiro teste.
func QuoteValidUntil(requested *time.Time, issued time.Time, pol Policy) (time.Time, error) {
	if requested != nil {
		if !requested.After(issued) {
			return time.Time{}, &RuleError{
				Code:    "VALIDATION_ERROR",
				Message: "A validade do orçamento precisa ser no futuro.",
				Details: map[string]any{
					"valid_until": "precisa ser no futuro: proposta que nasce vencida não é proposta.",
				},
			}
		}
		return *requested, nil
	}
	if pol.QuoteValidityDays < 1 {
		return time.Time{}, fmt.Errorf("booking: política v%d sem quote_validity_days (%d) — o campo não foi lido de commercial_policies",
			pol.Version, pol.QuoteValidityDays)
	}
	return issued.AddDate(0, 0, pol.QuoteValidityDays), nil
}
