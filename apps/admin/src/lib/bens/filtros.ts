import type { Query } from "@/lib/api/client";
import { ROTULO_DA_CATEGORIA } from "@/lib/bens/rotulos";
import {
  CATEGORIAS_DE_BEM,
  STATUS_DE_CONFERENCIA,
  TIPOS_DE_AVARIA,
  type CategoriaDeBem,
  type StatusDeConferencia,
  type TipoDeAvaria,
} from "@/lib/bens/tipos";

/**
 * Os recortes do inventário — **na query string**, como no resto do painel:
 * "me manda o que ainda está sem foto na cozinha do AP-02" precisa virar um
 * link que reabre a mesma tela.
 *
 * Puro de propósito: a URL é entrada de usuário, e o que a API recusaria com
 * `422` (categoria inventada, id que não é UUID) é descartado aqui, com aviso.
 */

export type ParametrosCrus = Record<string, string | string[] | undefined>;

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function texto(cru: ParametrosCrus, chave: string): string {
  const v = cru[chave];
  return (Array.isArray(v) ? v[0] : v)?.trim() ?? "";
}

function uuid(cru: ParametrosCrus, chave: string, avisos: string[], rotulo: string): string {
  const v = texto(cru, chave);
  if (v === "") return "";
  if (UUID.test(v)) return v;
  avisos.push(`O filtro de ${rotulo} do endereço não é válido e foi ignorado.`);
  return "";
}

function pagina(cru: ParametrosCrus): number {
  const n = Number.parseInt(texto(cru, "page"), 10);
  return Number.isFinite(n) && n > 0 ? n : 1;
}

function deLista<T extends string>(valor: string, lista: readonly T[]): T | "" {
  return (lista as readonly string[]).includes(valor) ? (valor as T) : "";
}

export const POR_PAGINA = 24;

// ── Catálogo ────────────────────────────────────────────────────────────────

export const ORDENS_DO_CATALOGO = [
  { valor: "name", rotulo: "Nome (A–Z)" },
  { valor: "category", rotulo: "Categoria, depois nome" },
  { valor: "-replacement_cost_cents", rotulo: "Mais caros primeiro" },
  { valor: "-created_at", rotulo: "Cadastrados por último" },
] as const;

export type OrdemDoCatalogo = (typeof ORDENS_DO_CATALOGO)[number]["valor"];

export type FiltrosDoCatalogo = {
  q: string;
  categoria: CategoriaDeBem | "";
  /** Padrão `ativos`: o catálogo em uso. Inativo é bem que saiu de linha. */
  situacao: "ativos" | "inativos" | "todos";
  /** `sem` é a fila de trabalho do cadastro (`has_photo=false`). */
  foto: "sem" | "com" | "";
  unidade: string;
  ambiente: string;
  ordem: OrdemDoCatalogo;
  page: number;
};

export function lerFiltrosDoCatalogo(cru: ParametrosCrus): { filtros: FiltrosDoCatalogo; avisos: string[] } {
  const avisos: string[] = [];
  const categoriaCrua = texto(cru, "categoria");
  const categoria = deLista(categoriaCrua, CATEGORIAS_DE_BEM);
  if (categoriaCrua && !categoria) avisos.push("A categoria do endereço não existe e foi ignorada.");

  const situacaoCrua = texto(cru, "situacao");
  const situacao = situacaoCrua === "inativos" || situacaoCrua === "todos" ? situacaoCrua : "ativos";
  const fotoCrua = texto(cru, "foto");
  const foto = fotoCrua === "sem" || fotoCrua === "com" ? fotoCrua : "";
  const ordemCrua = texto(cru, "ordem");
  const ordem = (ORDENS_DO_CATALOGO.find((o) => o.valor === ordemCrua)?.valor ?? "name") as OrdemDoCatalogo;

  const unidade = uuid(cru, "unidade", avisos, "unidade");
  // Ambiente sem unidade não faz sentido na barra (a lista de ambientes é da
  // unidade escolhida); a API aceitaria, mas a tela não teria como mostrá-lo.
  const ambiente = unidade ? uuid(cru, "ambiente", avisos, "ambiente") : "";

  return {
    filtros: { q: texto(cru, "q"), categoria, situacao, foto, unidade, ambiente, ordem, page: pagina(cru) },
    avisos,
  };
}

export function consultaDoCatalogo(f: FiltrosDoCatalogo): Query {
  return {
    q: f.q || undefined,
    category: f.categoria || undefined,
    active: f.situacao === "ativos" ? true : f.situacao === "inativos" ? false : undefined,
    has_photo: f.foto === "sem" ? false : f.foto === "com" ? true : undefined,
    unit_id: f.unidade || undefined,
    room_id: f.ambiente || undefined,
    sort: f.ordem,
    page: f.page,
    per_page: POR_PAGINA,
  };
}

// ── Exportação ──────────────────────────────────────────────────────────────

/**
 * O link da planilha com o mesmo recorte da tela. `GET /inventory/export` só
 * conhece unidade, ambiente, categoria e `include_inactive` — busca por texto e
 * "sem foto" não existem lá, e por isso não vão no link (a planilha é por
 * colocação, não por item do catálogo).
 */
export function linkDeExportacao(r: {
  unidade?: string;
  ambiente?: string;
  categoria?: string;
  incluirInativos?: boolean;
}): string {
  const p = new URLSearchParams();
  if (r.unidade) p.set("unit_id", r.unidade);
  if (r.ambiente) p.set("room_id", r.ambiente);
  if (r.categoria) p.set("category", r.categoria);
  if (r.incluirInativos) p.set("include_inactive", "true");
  const q = p.toString();
  return q ? `/api/inventario/exportar?${q}` : "/api/inventario/exportar";
}

// ── Unidade ─────────────────────────────────────────────────────────────────

export type FiltrosDaUnidade = {
  unidade: string;
  q: string;
  categoria: CategoriaDeBem | "";
  inativos: boolean;
};

export function lerFiltrosDaUnidade(cru: ParametrosCrus): { filtros: FiltrosDaUnidade; avisos: string[] } {
  const avisos: string[] = [];
  const categoriaCrua = texto(cru, "categoria");
  const categoria = deLista(categoriaCrua, CATEGORIAS_DE_BEM);
  if (categoriaCrua && !categoria) avisos.push("A categoria do endereço não existe e foi ignorada.");
  return {
    filtros: {
      unidade: uuid(cru, "unidade", avisos, "unidade"),
      q: texto(cru, "q"),
      categoria,
      inativos: texto(cru, "inativos") === "1",
    },
    avisos,
  };
}

/**
 * O filtro da tela da unidade, dito em palavras — ou `null` sem filtro.
 *
 * Existe porque os `totals` de `GET /units/{id}/inventory` somam **o que a
 * resposta mostra** (busca, categoria e desativados incluídos): com filtro, o
 * número é do recorte, e a tela tem de dizer qual recorte é.
 */
export function descricaoDoRecorte(f: Pick<FiltrosDaUnidade, "q" | "categoria" | "inativos">): string | null {
  const partes = [
    f.q ? `busca “${f.q}”` : null,
    f.categoria ? `categoria ${ROTULO_DA_CATEGORIA[f.categoria]}` : null,
    f.inativos ? "incluindo desativados" : null,
  ].filter((p): p is string => p !== null);
  return partes.length > 0 ? partes.join(", ") : null;
}

// ── Conferências ────────────────────────────────────────────────────────────

export type FiltrosDeConferencias = { unidade: string; status: StatusDeConferencia | ""; page: number };

export function lerFiltrosDeConferencias(cru: ParametrosCrus): { filtros: FiltrosDeConferencias; avisos: string[] } {
  const avisos: string[] = [];
  return {
    filtros: {
      unidade: uuid(cru, "unidade", avisos, "unidade"),
      status: deLista(texto(cru, "status"), STATUS_DE_CONFERENCIA),
      page: pagina(cru),
    },
    avisos,
  };
}

// ── Avarias ─────────────────────────────────────────────────────────────────

export type FiltrosDeAvarias = {
  /** Padrão `abertas` (`open=true`): a lista de trabalho. */
  situacao: "abertas" | "resolvidas" | "todas";
  unidade: string;
  tipo: TipoDeAvaria | "";
  conferencia: string;
  page: number;
};

export function lerFiltrosDeAvarias(cru: ParametrosCrus): { filtros: FiltrosDeAvarias; avisos: string[] } {
  const avisos: string[] = [];
  const s = texto(cru, "situacao");
  return {
    filtros: {
      situacao: s === "resolvidas" || s === "todas" ? s : "abertas",
      unidade: uuid(cru, "unidade", avisos, "unidade"),
      tipo: deLista(texto(cru, "tipo"), TIPOS_DE_AVARIA),
      conferencia: uuid(cru, "conferencia", avisos, "conferência"),
      page: pagina(cru),
    },
    avisos,
  };
}

export function consultaDeAvarias(f: FiltrosDeAvarias): Query {
  return {
    open: f.situacao === "abertas" ? true : f.situacao === "resolvidas" ? false : undefined,
    unit_id: f.unidade || undefined,
    kind: f.tipo || undefined,
    count_id: f.conferencia || undefined,
    sort: "-reported_at",
    page: f.page,
    per_page: POR_PAGINA,
  };
}

/** Troca um parâmetro e volta para a primeira página — filtro novo com
 *  `page=4` mostra uma lista vazia que parece "não achei nada". */
export function comParametro(atual: string, chave: string, valor: string): string {
  const p = new URLSearchParams(atual);
  if (valor === "") p.delete(chave);
  else p.set(chave, valor);
  if (chave !== "page") p.delete("page");
  const q = p.toString();
  return q ? `?${q}` : "?";
}
