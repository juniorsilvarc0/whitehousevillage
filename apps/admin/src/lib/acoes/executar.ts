import { isApiError } from "@/lib/api/client";
import { falha, type Resultado } from "@/lib/acoes/resultado";

/**
 * Executa e traduz. Vale tanto para carregar (RSC) quanto para gravar (action):
 * os dois precisam do mesmo envelope, e ter dois seria ter duas verdades sobre
 * como um erro se parece.
 *
 * Fica separado de `resultado.ts` porque **só o servidor** pode importá-lo:
 * `isApiError` vem do cliente da API, que lê o cookie por `next/headers`.
 * `resultado.ts` continua livre para o navegador, que precisa do tipo e da
 * mensagem por código.
 */
export async function tentar<T>(fn: () => Promise<T>): Promise<Resultado<T>> {
  try {
    return { ok: true, data: await fn() };
  } catch (erro) {
    if (isApiError(erro)) {
      return { ok: false, code: erro.code, message: erro.message, details: erro.details };
    }
    // Erro que não veio da API é defeito do painel. Não se disfarça de erro de
    // negócio: o texto genérico avisa sem inventar uma causa comercial.
    return falha("INTERNAL", "Erro inesperado no painel.");
  }
}
