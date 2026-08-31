import { ehDataISO } from "@/lib/datas";
import { ehEstadoDeReserva, ESTADOS } from "@/lib/reservas/estados";
import type { EstadoDaReserva } from "@/lib/reservas/tipos";

/**
 * O recorte da lista de reservas — **na query string**, sempre.
 *
 * O motivo é operacional, não estético: "me manda as pré-reservas que vencem
 * essa semana" precisa virar um link que reabre exatamente a mesma tela. Estado
 * de filtro em `useState` morre no F5 e não se cola no WhatsApp.
 *
 * Este módulo é puro de propósito. A query string é entrada de usuário — quem
 * edita a URL à mão manda `status=vendida` e `to` antes de `from` —, e mandar
 * isso para a API rende `422` num lugar onde a tela deveria simplesmente
 * explicar o que ignorou. Por isso `lerFiltros` devolve **os avisos junto**:
 * descartar em silêncio é como o operador conclui que "o filtro não funciona".
 */

export type Ordenacao =
  | "check_in"
  | "-check_in"
  | "created_at"
  | "-created_at"
  | "code"
  | "-code";

/** Espelho do enum de `sort` em `GET /reservations`. */
export const ORDENACOES: { valor: Ordenacao; rotulo: string }[] = [
  { valor: "-created_at", rotulo: "Mais recentes primeiro" },
  { valor: "created_at", rotulo: "Mais antigas primeiro" },
  { valor: "check_in", rotulo: "Entrada mais próxima" },
  { valor: "-check_in", rotulo: "Entrada mais distante" },
  { valor: "code", rotulo: "Código crescente" },
  { valor: "-code", rotulo: "Código decrescente" },
];

/**
 * O mesmo padrão do contrato (`-created_at`).
 *
 * A tentação é ordenar por `check_in` — "quem chega primeiro" é a pergunta da
 * operação. Mas sem recorte de data isso põe a estadia de 2019 no topo, e a
 * lista abre no passado. A pergunta operacional é respondida pelo recorte "Em
 * aberto" mais a faixa de datas; o padrão continua sendo o que a API entrega, e
 * divergir dele faria a primeira página da tela discordar da primeira página da
 * API sem nada na URL explicando por quê.
 */
export const ORDENACAO_PADRAO: Ordenacao = "-created_at";

export const POR_PAGINA = 25;

/**
 * Recortes prontos — o que a operação realmente pergunta.
 *
 * "Em aberto" junta `hold` e `confirmed` porque é a resposta a "o que ainda vai
 * acontecer"; `checked_in` fica de fora dele e tem recorte próprio, porque
 * hóspede dentro de casa é a fila do check-out, não a do check-in.
 */
export const RECORTES: { id: string; rotulo: string; status: string }[] = [
  { id: "abertas", rotulo: "Em aberto", status: "hold,confirmed" },
  { id: "hold", rotulo: "Pré-reservas", status: "hold" },
  { id: "hospedados", rotulo: "Na casa", status: "checked_in" },
  { id: "encerradas", rotulo: "Encerradas", status: "checked_out,closed" },
  { id: "perdidas", rotulo: "Perdidas", status: "cancelled,expired,no_show" },
];

export type FiltrosDeReservas = {
  /** Um ou mais estados separados por vírgula, como o contrato pede. */
  status: string;
  from: string;
  to: string;
  unit_type_id: string;
  q: string;
  page: number;
  sort: Ordenacao;
};

export const FILTROS_VAZIOS: FiltrosDeReservas = {
  status: "",
  from: "",
  to: "",
  unit_type_id: "",
  q: "",
  page: 1,
  sort: ORDENACAO_PADRAO,
};

export type ParametrosCrus = Record<string, string | string[] | undefined>;

export type LeituraDeFiltros = {
  filtros: FiltrosDeReservas;
  /** O que foi descartado e por quê — a tela mostra, não engole. */
  avisos: string[];
};

function primeiro(valor: string | string[] | undefined): string {
  if (Array.isArray(valor)) return valor[0] ?? "";
  return (valor ?? "").trim();
}

function ehUUID(valor: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(valor);
}

/** Lê a query string, descarta o que a API recusaria e diz o que descartou. */
export function lerFiltros(params: ParametrosCrus): LeituraDeFiltros {
  const avisos: string[] = [];

  const statusCru = primeiro(params.status);
  const pedidos = statusCru ? statusCru.split(",").map((s) => s.trim()).filter(Boolean) : [];
  const validos: EstadoDaReserva[] = [];
  const invalidos: string[] = [];
  for (const estado of pedidos) {
    if (ehEstadoDeReserva(estado)) {
      if (!validos.includes(estado)) validos.push(estado);
    } else {
      invalidos.push(estado);
    }
  }
  if (invalidos.length > 0) {
    avisos.push(
      `Situação desconhecida ignorada: ${invalidos.join(", ")}. As situações válidas são ${Object.keys(ESTADOS).join(", ")}.`,
    );
  }

  let from = primeiro(params.from);
  let to = primeiro(params.to);
  if (from && !ehDataISO(from)) {
    avisos.push(`Data inicial ignorada: "${from}" não é uma data no formato AAAA-MM-DD.`);
    from = "";
  }
  if (to && !ehDataISO(to)) {
    avisos.push(`Data final ignorada: "${to}" não é uma data no formato AAAA-MM-DD.`);
    to = "";
  }
  if (from && to && to < from) {
    // O contrato lê `[from, to)`: com `to` antes de `from` o intervalo é vazio e
    // a lista viria em branco sem explicação nenhuma. Melhor soltar a ponta
    // final e dizer isso do que mostrar "nenhuma reserva" para um recorte que
    // não existe.
    avisos.push(`A data final (${to}) é anterior à inicial (${from}); o recorte ficou aberto no fim.`);
    to = "";
  }

  const unitTypeId = primeiro(params.unit_type_id);
  let produto = unitTypeId;
  if (produto && !ehUUID(produto)) {
    avisos.push("Produto ignorado: o identificador na URL não é um UUID.");
    produto = "";
  }

  const paginaCrua = primeiro(params.page);
  const pagina = Number.parseInt(paginaCrua, 10);
  const page = Number.isFinite(pagina) && pagina > 0 ? pagina : 1;
  if (paginaCrua && page === 1 && paginaCrua !== "1") {
    avisos.push(`Página "${paginaCrua}" não existe; abrindo a primeira.`);
  }

  const sortCru = primeiro(params.sort);
  const sort = ORDENACOES.some((o) => o.valor === sortCru) ? (sortCru as Ordenacao) : ORDENACAO_PADRAO;
  if (sortCru && sort !== sortCru) {
    avisos.push(`Ordenação "${sortCru}" não existe; usando a padrão.`);
  }

  return {
    filtros: {
      status: validos.join(","),
      from,
      to,
      unit_type_id: produto,
      q: primeiro(params.q),
      page,
      sort,
    },
    avisos,
  };
}

/** `true` quando o usuário recortou alguma coisa — a página não conta. */
export function temRecorte(f: FiltrosDeReservas): boolean {
  return Boolean(f.status || f.from || f.to || f.unit_type_id || f.q) || f.sort !== ORDENACAO_PADRAO;
}

/** Query de `GET /reservations`. Chave vazia fica de fora: `apiFetch` já omite,
 *  mas deixar explícito é o que faz o teste desta função valer alguma coisa. */
export function paraConsulta(f: FiltrosDeReservas, porPagina = POR_PAGINA): Record<string, string | number> {
  const consulta: Record<string, string | number> = {
    page: f.page,
    per_page: porPagina,
    sort: f.sort,
  };
  if (f.status) consulta.status = f.status;
  if (f.from) consulta.from = f.from;
  if (f.to) consulta.to = f.to;
  if (f.unit_type_id) consulta.unit_type_id = f.unit_type_id;
  if (f.q) consulta.q = f.q;
  return consulta;
}

/**
 * Reescreve a URL trocando uma chave.
 *
 * **Mexer em qualquer filtro volta para a página 1.** Sem isso, quem está na
 * página 4 e aperta "Pré-reservas" cai numa página 4 que não existe no recorte
 * novo e lê "nenhuma reserva" — a lista parece vazia justamente quando o filtro
 * acabou de acertar.
 */
export function aplicar(atual: URLSearchParams, chave: string, valor: string): string {
  const proximos = new URLSearchParams(atual.toString());
  if (valor === "") proximos.delete(chave);
  else proximos.set(chave, valor);
  if (chave !== "page") proximos.delete("page");
  const consulta = proximos.toString();
  return consulta ? `?${consulta}` : "";
}
