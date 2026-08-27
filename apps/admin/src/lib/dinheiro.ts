/**
 * Dinheiro no painel.
 *
 * O sistema inteiro fala **centavos inteiros** (CLAUDE.md §4). Aqui só se
 * formata para o olho e se lê de volta o que o dedo digitou — nenhuma conta
 * acontece neste arquivo, e nenhuma acontece no painel: quem soma, desconta e
 * arredonda é o motor `internal/domain/booking`, do outro lado.
 *
 * A leitura é feita por string, sem passar por `Number` no meio: `1234,56` em
 * ponto flutuante vira `123456.00000000001` com frequência suficiente para
 * gravar uma tarifa um centavo errada, e um centavo errado numa tabela
 * comercial é uma reunião com os proprietários.
 */

const BRL = new Intl.NumberFormat("pt-BR", {
  style: "currency",
  currency: "BRL",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

/** `85000` → `R$ 850,00`. */
export function formatarBRL(centavos: number): string {
  return BRL.format(centavos / 100);
}

/** `85000` → `850,00` — para dentro de `<input>`, onde o "R$" é do rótulo. */
export function reaisDeCentavos(centavos: number): string {
  const sinal = centavos < 0 ? "-" : "";
  const abs = Math.abs(Math.trunc(centavos));
  const inteiro = Math.trunc(abs / 100);
  const resto = String(abs % 100).padStart(2, "0");
  return `${sinal}${inteiro.toLocaleString("pt-BR")},${resto}`;
}

/**
 * Lê o que o usuário digitou. Aceita `1.234,56`, `1234,56`, `1234.56` e `1234`.
 *
 * O ponto é ambíguo em pt-BR: separador de milhar na escrita ("1.234") e
 * decimal em teclado numérico de celular ("1234.56"). A regra que resolve sem
 * adivinhação: **se há vírgula, o ponto é milhar**; sem vírgula, um ponto
 * seguido de uma ou duas casas é decimal, e qualquer outro é milhar.
 *
 * Devolve `null` para entrada que não é dinheiro — quem chama decide se isso é
 * "campo vazio" ou "valor inválido".
 */
export function centavosDeTexto(texto: string): number | null {
  const limpo = texto.replace(/[\s ]|R\$/g, "");
  if (limpo === "") return null;

  let normalizado: string;
  if (limpo.includes(",")) {
    normalizado = limpo.replace(/\./g, "").replace(",", ".");
  } else if (/^\d+\.\d{1,2}$/.test(limpo)) {
    normalizado = limpo;
  } else {
    normalizado = limpo.replace(/\./g, "");
  }

  if (!/^\d+(\.\d{1,2})?$/.test(normalizado)) return null;

  const [inteiro, decimal = ""] = normalizado.split(".");
  return Number(inteiro) * 100 + Number(`${decimal}00`.slice(0, 2));
}

/** `5` → `5%`; `7.5` → `7,5%`. Percentual da política é `number`, não centavo. */
export function formatarPct(pct: number): string {
  return `${pct.toLocaleString("pt-BR", { maximumFractionDigits: 2 })}%`;
}
