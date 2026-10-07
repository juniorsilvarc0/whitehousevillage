import { apiList, type Query } from "@/lib/api/client";
import { tentar } from "@/lib/acoes/executar";
import type { Resultado } from "@/lib/acoes/resultado";
import type { Bem, UnidadeDoInventario } from "@/lib/bens/tipos";

export type { UnidadeDoInventario };

/**
 * Leituras do inventário de bens que precisam de mais de uma chamada —
 * **servidor-only**, como todo acesso à API.
 */

/** Teto de páginas numa leitura completa: 10 × 100 itens. Acima disso a tela
 *  precisa de busca, não de lista — e o teto impede um laço sem fim se a API
 *  algum dia devolver `total_pages` errado. */
const MAXIMO_DE_PAGINAS = 10;

export async function carregarTodas<T>(path: string, query: Query = {}): Promise<Resultado<T[]>> {
  return tentar(async () => {
    const todos: T[] = [];
    for (let page = 1; page <= MAXIMO_DE_PAGINAS; page++) {
      const { data, meta } = await apiList<T>(path, { query: { ...query, page, per_page: 100 } });
      todos.push(...data);
      if (page >= meta.total_pages || data.length === 0) break;
    }
    return todos;
  });
}

/**
 * As unidades que a tela oferece para escolher — `GET /inventory/units`.
 *
 * Rota do próprio inventário de bens (`inventory.goods:ver`), e não `/units`,
 * que é do cadastro comercial e exige `inventory`. Vem com os números de cada
 * unidade (ambientes, bens, conferência aberta, última fechada), e traz também
 * a unidade que ainda não tem ambiente nenhum — que é justamente onde alguém
 * precisa criar o primeiro cômodo.
 */
export function unidadesDoInventario(): Promise<Resultado<UnidadeDoInventario[]>> {
  return carregarTodas<UnidadeDoInventario>("/inventory/units", { active: true });
}

/** O catálogo em uso, inteiro — para escolher o bem a colocar num ambiente. */
export function catalogoAtivo(): Promise<Resultado<Bem[]>> {
  return carregarTodas<Bem>("/inventory/items", { active: true, sort: "name" });
}
