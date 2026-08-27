import { z } from "zod";

import { centavosDeTexto } from "@/lib/dinheiro";
import { ehDataISO } from "@/lib/datas";

/**
 * Peças de validação reaproveitadas pelos formulários.
 *
 * **Todo campo é `string` no formulário, inclusive número e dinheiro.** A
 * tentação é usar `z.coerce.number()` e receber o valor pronto; o preço é que o
 * tipo de entrada do schema deixa de bater com o de saída, e o `react-hook-form`
 * passa a precisar de três parâmetros de tipo para não reclamar. Com string em
 * tudo, o formulário fala a língua do `<input>` e a conversão acontece num
 * lugar só — as funções `para*` daqui de baixo —, na fronteira com o DTO.
 *
 * O que estas funções **não** fazem é conta: quem soma, desconta e arredonda é
 * o motor do servidor. Aqui só se recusa o que nem chega a ser um número.
 */

export const textoObrigatorio = (min: number, mensagem: string) => z.string().trim().min(min, mensagem);

export const inteiroDe = (min: number, mensagem: string) =>
  z.string().refine((v) => /^\d+$/.test(v.trim()) && Number(v.trim()) >= min, mensagem);

/** Dinheiro digitado em reais (`1.234,56`), guardado em centavos. */
export const dinheiroDe = (min: number, mensagem: string) =>
  z.string().refine((v) => {
    const centavos = centavosDeTexto(v);
    return centavos !== null && centavos >= min;
  }, mensagem);

export const dataDe = (mensagem: string) => z.string().refine((v) => ehDataISO(v.trim()), mensagem);

/** Percentual da política — `number` no contrato, com casa decimal permitida. */
export const percentualDe = (mensagem: string) =>
  z.string().refine((v) => {
    const n = Number(v.trim().replace(",", "."));
    return v.trim() !== "" && Number.isFinite(n) && n >= 0 && n <= 100;
  }, mensagem);

export function paraInteiro(valor: string): number {
  return Number.parseInt(valor.trim(), 10);
}

export function paraCentavos(valor: string): number {
  return centavosDeTexto(valor) ?? 0;
}

export function paraNumero(valor: string): number {
  return Number(valor.trim().replace(",", "."));
}

/** `""` vira `null` — a distinção que o contrato usa para "limpar o campo". */
export function paraTextoOuNulo(valor: string): string | null {
  const limpo = valor.trim();
  return limpo === "" ? null : limpo;
}

/**
 * `ZodError` → `details` no formato do contrato (`{ campo: mensagem }`).
 *
 * Mesma forma que a API devolve em `422 VALIDATION_ERROR`, para o formulário
 * não precisar saber se quem recusou foi o servidor de dados ou a revalidação
 * da action. Fica a primeira mensagem por campo: a segunda descreve o mesmo
 * erro por outro ângulo e só empurra a primeira para fora da tela.
 */
export function detalhesDoZod(erro: z.ZodError): Record<string, string> {
  const detalhes: Record<string, string> = {};
  for (const problema of erro.issues) {
    const chave = problema.path.join(".");
    if (chave && !(chave in detalhes)) detalhes[chave] = problema.message;
  }
  return detalhes;
}
