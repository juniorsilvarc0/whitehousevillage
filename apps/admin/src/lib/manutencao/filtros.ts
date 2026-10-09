import type { Query } from "@/lib/api/client";
import { PRIORIDADES_DA_ORDEM, STATUS_DA_ORDEM, type PrioridadeDaOrdem, type StatusDaOrdem } from "@/lib/manutencao/tipos";

/**
 * Os recortes da lista de ordens — **na query string**, com os mesmos nomes
 * dos parâmetros da API (`status`, `open`, `unit_id`, `priority`, `q`, `page`):
 * "me manda as urgentes do AP-03" é um link.
 *
 * Puro de propósito: a URL é entrada de usuário, e o que a API recusaria com
 * `422` (status inventado, id que não é UUID) é descartado aqui, com aviso.
 *
 * **Não há `sort` aqui.** A ordem padrão da API (`sort=urgencia`: abertas
 * primeiro, `urgente → baixa`, as mais antigas no topo; depois as encerradas,
 * da mais recente) é a lista de trabalho, e o painel não a reordena nem oferece
 * outra — a escada de prioridade é de `maintenance.ByUrgency()`.
 */

export type ParametrosCrus = Record<string, string | string[] | undefined>;

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export const POR_PAGINA = 25;

export type FiltrosDaLista = {
  status: StatusDaOrdem | "";
  /** `true` = aberta ou em andamento; `false` = concluída ou cancelada. */
  open: boolean | null;
  unit_id: string;
  priority: PrioridadeDaOrdem | "";
  q: string;
  page: number;
};

function texto(cru: ParametrosCrus, chave: string): string {
  const v = cru[chave];
  return (Array.isArray(v) ? v[0] : v)?.trim() ?? "";
}

function deLista<T extends string>(valor: string, lista: readonly T[]): T | "" {
  return (lista as readonly string[]).includes(valor) ? (valor as T) : "";
}

export function lerFiltros(cru: ParametrosCrus): { filtros: FiltrosDaLista; avisos: string[] } {
  const avisos: string[] = [];

  const statusCru = texto(cru, "status");
  const status = deLista(statusCru, STATUS_DA_ORDEM);
  if (statusCru && !status) avisos.push("A situação do endereço não existe e foi ignorada.");

  const prioridadeCrua = texto(cru, "priority");
  const priority = deLista(prioridadeCrua, PRIORIDADES_DA_ORDEM);
  if (prioridadeCrua && !priority) avisos.push("A prioridade do endereço não existe e foi ignorada.");

  const openCru = texto(cru, "open");
  const open = openCru === "true" ? true : openCru === "false" ? false : null;

  let unit_id = texto(cru, "unit_id");
  if (unit_id && !UUID.test(unit_id)) {
    avisos.push("O filtro de unidade do endereço não é válido e foi ignorado.");
    unit_id = "";
  }

  const n = Number.parseInt(texto(cru, "page"), 10);
  const page = Number.isFinite(n) && n > 0 ? n : 1;

  // `maxLength: 200` no contrato — cortar aqui evita um 422 por link colado.
  const q = texto(cru, "q").slice(0, 200);

  return { filtros: { status, open, unit_id, priority, q, page }, avisos };
}

export function consultaDaLista(f: FiltrosDaLista): Query {
  return {
    status: f.status || undefined,
    open: f.open === null ? undefined : f.open,
    unit_id: f.unit_id || undefined,
    priority: f.priority || undefined,
    q: f.q || undefined,
    page: f.page,
    per_page: POR_PAGINA,
  };
}

export function temFiltro(f: FiltrosDaLista): boolean {
  return Boolean(f.status || f.open !== null || f.unit_id || f.priority || f.q);
}

/**
 * A "situação" da barra de filtros é **um** `<select>` só, que escreve em
 * `open` **ou** em `status` — no celular, dois selects para a mesma pergunta
 * ("o que ainda está para fazer?") é um a mais. Estes são os valores dele.
 */
export const SITUACOES = [
  { valor: "", rotulo: "Todas" },
  { valor: "open:true", rotulo: "Em aberto" },
  { valor: "open:false", rotulo: "Encerradas" },
  { valor: "status:aberta", rotulo: "Só abertas (não iniciadas)" },
  { valor: "status:em_andamento", rotulo: "Só em andamento" },
  { valor: "status:concluida", rotulo: "Só concluídas" },
  { valor: "status:cancelada", rotulo: "Só canceladas" },
] as const;

/** O valor do select a partir da URL. Com os dois no endereço, `status` vence
 *  na exibição (é o mais específico); a API aplica os dois (E). */
export function situacaoDaUrl(parametros: URLSearchParams): string {
  const status = parametros.get("status");
  if (status) return `status:${status}`;
  const open = parametros.get("open");
  if (open === "true" || open === "false") return `open:${open}`;
  return "";
}

/** A query string depois de trocar a situação: limpa as duas chaves, escreve
 *  a escolhida e volta à primeira página. */
export function comSituacao(atual: string, valor: string): string {
  const p = new URLSearchParams(atual);
  p.delete("status");
  p.delete("open");
  p.delete("page");
  const [chave, v] = valor.split(":");
  if (chave && v) p.set(chave, v);
  const q = p.toString();
  return q ? `?${q}` : "";
}
