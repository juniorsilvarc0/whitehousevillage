import { apiFetch, apiList, type Query } from "@/lib/api/client";
import { tentar } from "@/lib/acoes/executar";
import type { Resultado } from "@/lib/acoes/resultado";

/**
 * Carregamento de dados em componente de servidor, **sem lançar**.
 *
 * Uma tela de configuração mostra meia dúzia de coleções lado a lado. Se a
 * primeira que falhar derrubar a árvore inteira no boundary de erro, a gestão
 * perde as outras cinco por causa de uma — e, nesta fase, é garantido que
 * alguma vai faltar: os módulos da API estão sendo escritos em paralelo e uma
 * rota ainda não registrada responde `404`. Cada bloco carrega e falha sozinho,
 * mostrando o código na própria seção.
 */

/** O contrato pagina tudo; estas coleções são de cadastro e cabem numa página. */
export const PAGINA_CHEIA = 100;

export function carregar<T>(path: string, query?: Query): Promise<Resultado<T>> {
  return tentar(() => apiFetch<T>(path, { query }));
}

/** Lista já sem o envelope. `meta` fica de fora de propósito: onde o painel
 *  precisa de paginação de verdade, a tela pede `apiList` diretamente. */
export function carregarLista<T>(path: string, query?: Query): Promise<Resultado<T[]>> {
  return tentar(async () => (await apiList<T>(path, { query: { per_page: PAGINA_CHEIA, ...query } })).data);
}
