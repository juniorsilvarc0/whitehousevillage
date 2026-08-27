import { apiFetch, apiList, isApiError, type ApiFetchInit, type Query } from "@/lib/api/client";
import type { Lista } from "@/lib/api/types";
import { ehCodigoDoCrm, type CodigoCrm } from "@/lib/crm/codigos";

/**
 * A fronteira do CRM com a API — **servidor-only**, como todo o resto: o JWT
 * vive em cookie `httpOnly` e quem o injeta é o RSC ou a Server Action.
 *
 * ## Por que não usar `tentar()` direto
 *
 * `tentar()` devolve o `code` que o `ApiError` carrega, e o `ApiError` já
 * nasceu com o código **normalizado**: `apiRequest` chama `normalizarCodigo()`
 * antes de lançar, e o código cru do corpo é descartado ali. Como o espelho de
 * códigos do painel ainda não conhece o vocabulário do CRM (ver
 * `lib/crm/codigos.ts`), `LEAD_ALREADY_CONVERTED` chegaria à tela como
 * `INTERNAL` e o operador leria "erro no servidor" em cima de uma recusa que
 * ele resolve sozinho.
 *
 * ## Como se recupera o código cru sem duplicar o cliente
 *
 * `ApiFetchInit.onResponse` entrega a `Response` **antes** do parse. Uma cópia
 * (`clone()`) lida em paralelo devolve o `error.code` original; o corpo
 * verdadeiro segue intacto para o `apiRequest`, que continua sendo o único
 * lugar que monta URL, injeta token e trata cache. Só há clone quando a
 * resposta **não** é ok: no caminho feliz não se lê corpo duas vezes.
 *
 * Quando os sete códigos entrarem em `lib/api/codigos.ts`, este arquivo perde a
 * recuperação e vira um `tentar()` com o tipo mais largo.
 */

export type FalhaCrm = {
  ok: false;
  code: CodigoCrm;
  message: string;
  details: Record<string, unknown>;
};

export type ResultadoCrm<T> = { ok: true; data: T } | FalhaCrm;

export function falhaCrm(
  code: CodigoCrm,
  message: string,
  details: Record<string, unknown> = {},
): FalhaCrm {
  return { ok: false, code, message, details };
}

async function codigoCruDoCorpo(resposta: Response): Promise<string | undefined> {
  try {
    const corpo = (await resposta.json()) as { error?: { code?: string } };
    return corpo?.error?.code;
  } catch {
    // Corpo ilegível é problema de infraestrutura, e o `apiRequest` já o trata.
    // Aqui só significa "não há código cru a recuperar".
    return undefined;
  }
}

export async function chamarCrm<T>(path: string, init: ApiFetchInit = {}): Promise<ResultadoCrm<T>> {
  // Holder em objeto, e não `let`: a atribuição acontece dentro do callback, e
  // o TypeScript estreitaria uma variável solta para `null` no ponto da leitura.
  const capturado: { cru?: Promise<string | undefined> } = {};

  try {
    const data = await apiFetch<T>(path, {
      ...init,
      onResponse: (resposta) => {
        init.onResponse?.(resposta);
        if (!resposta.ok) capturado.cru = codigoCruDoCorpo(resposta.clone());
      },
    });
    return { ok: true, data };
  } catch (erro) {
    if (!isApiError(erro)) {
      // Erro que não veio da API é defeito do painel. Não se disfarça de erro
      // de negócio: o texto genérico avisa sem inventar uma causa comercial.
      return falhaCrm("INTERNAL", "Erro inesperado no painel.");
    }
    const cru = capturado.cru ? await capturado.cru : undefined;
    return {
      ok: false,
      code: cru && ehCodigoDoCrm(cru) ? cru : erro.code,
      message: erro.message,
      details: erro.details,
    };
  }
}

/** Coleção de cadastro que cabe numa página (funis, etapas, motivos). */
export const PAGINA_CHEIA = 100;

export async function listarCrm<T>(path: string, query?: Query): Promise<ResultadoCrm<T[]>> {
  const resultado = await chamarCrm<T[]>(path, { query: { per_page: PAGINA_CHEIA, ...query } });
  // `apiFetch` devolve só o `data` do envelope; lista sem `data` (rota ainda
  // não registrada devolvendo 200 vazio) vira lista vazia, não `undefined`.
  return resultado.ok ? { ok: true, data: resultado.data ?? [] } : resultado;
}

/** Lista **com** `meta` — para as telas paginadas de verdade (leads). */
export async function paginarCrm<T>(path: string, query?: Query): Promise<ResultadoCrm<Lista<T>>> {
  const capturado: { cru?: Promise<string | undefined> } = {};
  try {
    const lista = await apiList<T>(path, {
      query,
      onResponse: (resposta) => {
        if (!resposta.ok) capturado.cru = codigoCruDoCorpo(resposta.clone());
      },
    });
    return { ok: true, data: lista };
  } catch (erro) {
    if (!isApiError(erro)) return falhaCrm("INTERNAL", "Erro inesperado no painel.");
    const cru = capturado.cru ? await capturado.cru : undefined;
    return {
      ok: false,
      code: cru && ehCodigoDoCrm(cru) ? cru : erro.code,
      message: erro.message,
      details: erro.details,
    };
  }
}
