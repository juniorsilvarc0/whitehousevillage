import { apiList } from "@/lib/api/client";
import { carregar } from "@/lib/api/carregar";
import { tentar } from "@/lib/acoes/executar";
import type { Resultado } from "@/lib/acoes/resultado";
import type { Lista } from "@/lib/api/types";
import { consultaDaLista, type FiltrosDaLista } from "@/lib/manutencao/filtros";
import type { OrdemDeManutencao } from "@/lib/manutencao/tipos";

/**
 * Leituras das ordens de manutenção — **servidor-only**, como todo acesso à API
 * (o JWT vive em cookie `httpOnly`).
 *
 * "Esta avaria já tem ordem aberta?" não passa por aqui: a resposta vem na
 * própria avaria (`open_maintenance_order_id`), sem chamada por linha.
 */

/** A lista de trabalho, na ordem da API (`sort=urgencia`, o padrão). */
export function listarOrdens(filtros: FiltrosDaLista): Promise<Resultado<Lista<OrdemDeManutencao>>> {
  return tentar(() => apiList<OrdemDeManutencao>("/maintenance-orders", { query: consultaDaLista(filtros) }));
}

export function carregarOrdem(id: string): Promise<Resultado<OrdemDeManutencao>> {
  return carregar<OrdemDeManutencao>(`/maintenance-orders/${encodeURIComponent(id)}`);
}
