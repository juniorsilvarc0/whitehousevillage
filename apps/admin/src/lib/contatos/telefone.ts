/**
 * Telefone em E.164 — a chave de deduplicação do cadastro.
 *
 * ## A regra que este arquivo protege: **não se adivinha DDI**
 *
 * O contrato é explícito (`ContatoCriar.phone_e164`): formato errado é
 * `422 VALIDATION_ERROR`, **não** uma normalização silenciosa. O motivo é a
 * unicidade: `85999990000` pode ser Fortaleza ou pode ser outra coisa, e um
 * palpite errado cria dois registros do mesmo ser humano — que é exatamente o
 * que a `UNIQUE (phone_e164)` existe para impedir. Duplicata nascida de palpite
 * é pior que recusa, porque ninguém a percebe.
 *
 * O que a tela **pode** fazer, e faz aqui, é limpar a pontuação que o dedo
 * digita — espaço, parêntese, traço, ponto. Isso não é adivinhar: `+55 (85)
 * 99999-0000` e `+5585999990000` são a mesma sequência de dígitos com a mesma
 * origem declarada. O que não se acrescenta é o que o usuário não escreveu.
 *
 * O `+` inicial fica visível no campo por isso mesmo: ele é o pedaço que
 * carrega a decisão, e escondê-lo atrás de uma máscara faria o painel decidir
 * pelo usuário.
 */

/**
 * E.164: `+` seguido de 8 a 15 dígitos, o primeiro diferente de zero.
 *
 * O piso de 8 não é do padrão (a ITU só fixa o teto de 15); é o que separa um
 * número de telefone plausível de um DDI digitado pela metade. Brasil celular
 * com DDI tem 13.
 */
const E164 = /^\+[1-9]\d{7,14}$/;

export function ehE164(valor: string): boolean {
  return E164.test(valor);
}

/**
 * Tira a pontuação e nada mais. **Não acrescenta DDI, não acrescenta `+`.**
 *
 * Devolve o texto limpo mesmo quando ele não é E.164 válido — quem decide se
 * isso é erro é o schema do formulário, que tem como apontar o campo. Uma função
 * de limpeza que também valida esconderia do usuário qual dos dois problemas ele
 * tem.
 */
export function limparTelefone(valor: string): string {
  return valor.replace(/[\s().\-–—]/g, "");
}

/**
 * `+5585999990000` → `+55 (85) 99999-0000`.
 *
 * Só formata o que reconhece — número brasileiro (`+55`) com 10 ou 11 dígitos
 * depois do DDD. Qualquer outro país volta como veio: inventar agrupamento para
 * um formato que não se conhece é pior que não agrupar, porque parece verdade.
 */
export function formatarTelefone(e164: string | null | undefined): string {
  if (!e164) return "";
  if (!ehE164(e164)) return e164;

  const digitos = e164.slice(1);
  if (!digitos.startsWith("55") || (digitos.length !== 12 && digitos.length !== 13)) return e164;

  const ddd = digitos.slice(2, 4);
  const assinante = digitos.slice(4);
  const corte = assinante.length - 4;
  return `+55 (${ddd}) ${assinante.slice(0, corte)}-${assinante.slice(corte)}`;
}

/**
 * O que a busca por telefone manda para a API.
 *
 * A rota `GET /contacts?phone=` é **igualdade exata em E.164** e devolve zero ou
 * um registro; número mal formatado é `422`, não busca vazia. Então a barra de
 * busca só usa o campo `phone` quando o que foi digitado já é E.164 — caso
 * contrário o termo vai para `q`, que é a busca humana por nome e e-mail.
 * Mandar "85 99999" em `phone` gastaria uma requisição para receber um erro de
 * validação em cima de uma pessoa que só está procurando alguém.
 */
export function pareceTelefone(termo: string): boolean {
  const limpo = limparTelefone(termo.trim());
  return limpo.startsWith("+") && ehE164(limpo);
}
