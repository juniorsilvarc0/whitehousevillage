import type { Lista } from "@/lib/api/types";

/**
 * A janela inteira, página por página.
 *
 * As visões de calendário pedem `[from, to)` com `per_page=100` e **seguem
 * `meta.total_pages`** (contrato, `GET /agenda/events`): uma semana de alta
 * temporada com a casa cheia passa de cem eventos, e a grade que mostrasse só
 * a primeira página diria "sábado livre" com três limpezas marcadas.
 *
 * O teto existe para a resposta errada não virar laço sem fim (uma API que
 * devolvesse `total_pages` crescendo) — e, quando ele corta, a tela **diz**
 * que cortou (`incompleta`), em vez de desenhar uma agenda parcial como se
 * fosse a inteira.
 *
 * Puro de propósito: quem busca a página é passado por parâmetro, e o teste
 * prova a paginação sem rede.
 */
export const TETO_DE_PAGINAS = 20;

export async function todasAsPaginas<T>(
  buscarPagina: (pagina: number) => Promise<Lista<T>>,
  teto = TETO_DE_PAGINAS,
): Promise<{ itens: T[]; incompleta: boolean }> {
  const itens: T[] = [];
  for (let pagina = 1; pagina <= teto; pagina++) {
    const { data, meta } = await buscarPagina(pagina);
    itens.push(...data);
    const total = Number.isFinite(meta.total_pages) ? meta.total_pages : 1;
    if (pagina >= total || data.length === 0) return { itens, incompleta: false };
  }
  return { itens, incompleta: true };
}
