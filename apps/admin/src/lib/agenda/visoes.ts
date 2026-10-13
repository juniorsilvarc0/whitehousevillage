import type { Query } from "@/lib/api/client";
import { ehDataISO, somarDias } from "@/lib/datas";
import { paredeDaCasa } from "@/lib/fuso";
import { dataLonga, diaDaSemanaISO, diaEMes, mesPorExtenso } from "@/lib/agenda/rotulos";
import { TIPOS_DE_EVENTO, type EventoDaAgenda, type TipoDeEvento } from "@/lib/agenda/tipos";

/**
 * As quatro visões da agenda — dia, semana, mês e lista — e o recorte de cada
 * uma, **na query string**: "a limpeza de sexta no AP-02" é um link.
 *
 * Puro de propósito. Duas coisas que este arquivo NÃO faz:
 *
 * - **Não sabe que dia é hoje.** "Hoje" é o `today` de `GET /agenda/settings`
 *   — o dia da CASA, dito pelo servidor — e chega por parâmetro. Nenhuma
 *   função daqui chama o relógio: o navegador de quem abriu a tela pode estar
 *   em outro fuso, ou com a data errada, e a agenda da operação não pode mudar
 *   de dia por causa disso.
 * - **Não converte fuso para agrupar.** Cada evento vem com `date` (o dia da
 *   casa em que começa) e é por ele que lista e mês agrupam. A hora de parede
 *   (`lib/fuso.ts`) só entra onde o dia não basta: saber até que dia um evento
 *   longo vai.
 *
 * As janelas são half-open `[from, to)` em dias da casa, como a API as pede, e
 * cabem nos 92 dias do contrato (o mês é a grade inteira: até 6 semanas).
 */

export type ParametrosCrus = Record<string, string | string[] | undefined>;

export const VISOES = ["dia", "semana", "mes", "lista"] as const;
export type Visao = (typeof VISOES)[number];

export const ROTULO_DA_VISAO: Record<Visao, string> = {
  dia: "Dia",
  semana: "Semana",
  mes: "Mês",
  lista: "Lista",
};

/** A visão de quem abre a agenda sem escolher: o dia — "o que tem hoje" é a
 *  pergunta de quem limpa e de quem recebe, e cabe no celular. */
export const VISAO_PADRAO: Visao = "dia";

/** A lista cobre uma semana a partir do dia escolhido. */
export const DIAS_DA_LISTA = 7;

/** Quem é o responsável no filtro: todos, só eu, ou ninguém (a limpeza que
 *  ninguém pegou). `eu` vira `assignee_id = <usuário>`; `ninguem`,
 *  `unassigned=true` — nunca os dois juntos (é `422`). */
export const RESPONSAVEIS = ["", "eu", "ninguem"] as const;
export type FiltroDeResponsavel = (typeof RESPONSAVEIS)[number];

export type FiltrosDaAgenda = {
  visao: Visao;
  /** O dia de referência: o mostrado (dia), um da semana (semana), um do mês
   *  (mês) ou o primeiro (lista). Ausente na URL = hoje NA CASA. */
  data: string;
  tipo: TipoDeEvento | "";
  unidade: string;
  responsavel: FiltroDeResponsavel;
  /** Mostra também os desmarcados (o padrão da API os esconde). */
  cancelados: boolean;
};

export type Janela = {
  from: string;
  /** Exclusivo. */
  to: string;
  /** Os dias de `[from, to)`, em ordem. */
  dias: string[];
};

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function texto(cru: ParametrosCrus, chave: string): string {
  const v = cru[chave];
  return (Array.isArray(v) ? v[0] : v)?.trim() ?? "";
}

function deLista<T extends string>(valor: string, lista: readonly T[]): T | "" {
  return (lista as readonly string[]).includes(valor) ? (valor as T) : "";
}

/**
 * Lê a URL com tolerância — parâmetro impossível cai no padrão, com aviso.
 *
 * `hoje` é obrigatório e vem do servidor (`settings.today`). Não há valor
 * padrão de propósito: um `hoje = hojeISO()` aqui seria o atalho por onde o
 * relógio de quem abriu a tela voltaria a decidir o dia da operação.
 */
export function lerFiltros(cru: ParametrosCrus, hoje: string): { filtros: FiltrosDaAgenda; avisos: string[] } {
  const avisos: string[] = [];

  const visaoCrua = texto(cru, "visao");
  const visao = deLista(visaoCrua, VISOES) || VISAO_PADRAO;
  if (visaoCrua && !deLista(visaoCrua, VISOES)) avisos.push("A visão do endereço não existe; abrimos a do dia.");

  const dataCrua = texto(cru, "data");
  const data = dataCrua && ehDataISO(dataCrua) ? dataCrua : hoje;
  if (dataCrua && !ehDataISO(dataCrua)) avisos.push("A data do endereço não é válida; abrimos em hoje.");

  const tipoCru = texto(cru, "tipo");
  const tipo = deLista(tipoCru, TIPOS_DE_EVENTO);
  if (tipoCru && !tipo) avisos.push("O tipo de evento do endereço não existe e foi ignorado.");

  let unidade = texto(cru, "unidade");
  if (unidade && !UUID.test(unidade)) {
    avisos.push("O filtro de unidade do endereço não é válido e foi ignorado.");
    unidade = "";
  }

  const responsavel = deLista(texto(cru, "responsavel"), RESPONSAVEIS);

  return {
    filtros: { visao, data, tipo, unidade, responsavel, cancelados: texto(cru, "cancelados") === "1" },
    avisos,
  };
}

// ── Janelas ─────────────────────────────────────────────────────────────────

/** A segunda-feira da semana da data (semana ISO: segunda a domingo — o fim
 *  de semana fica junto, que é quando a casa troca de hóspede). */
export function inicioDaSemana(data: string): string {
  return somarDias(data, -(diaDaSemanaISO(data) - 1));
}

export function primeiroDoMes(data: string): string {
  return `${data.slice(0, 8)}01`;
}

/** O primeiro dia do mês `n` meses depois (ou antes) do mês da data. */
export function somarMeses(data: string, n: number): string {
  const ano = Number(data.slice(0, 4));
  const mes = Number(data.slice(5, 7)) - 1 + n;
  const d = new Date(Date.UTC(ano, mes, 1));
  return d.toISOString().slice(0, 10);
}

function diasEntre(from: string, to: string): string[] {
  const dias: string[] = [];
  for (let d = from; d < to; d = somarDias(d, 1)) dias.push(d);
  return dias;
}

export function janelaDaVisao(visao: Visao, data: string): Janela {
  let from: string;
  let to: string;
  switch (visao) {
    case "dia":
      from = data;
      to = somarDias(data, 1);
      break;
    case "semana":
      from = inicioDaSemana(data);
      to = somarDias(from, 7);
      break;
    case "mes": {
      // A grade inteira: da segunda-feira da semana do dia 1 ao domingo da
      // semana do último dia — até 6 semanas, dentro dos 92 dias do contrato.
      const primeiro = primeiroDoMes(data);
      const ultimo = somarDias(somarMeses(data, 1), -1);
      from = inicioDaSemana(primeiro);
      to = somarDias(inicioDaSemana(ultimo), 7);
      break;
    }
    case "lista":
      from = data;
      to = somarDias(data, DIAS_DA_LISTA);
      break;
  }
  return { from, to, dias: diasEntre(from, to) };
}

/** A data de referência um passo para trás (−1) ou para a frente (+1). */
export function dataVizinha(visao: Visao, data: string, passo: -1 | 1): string {
  switch (visao) {
    case "dia":
      return somarDias(data, passo);
    case "semana":
      return somarDias(inicioDaSemana(data), 7 * passo);
    case "mes":
      return somarMeses(data, passo);
    case "lista":
      return somarDias(data, DIAS_DA_LISTA * passo);
  }
}

/** O período na barra: `sexta-feira, 20 de novembro de 2026`, `16/11 a
 *  22/11`, `novembro de 2026`, `20/11 a 26/11`. */
export function tituloDaJanela(visao: Visao, data: string, janela: Janela): string {
  const ultimo = somarDias(janela.to, -1);
  switch (visao) {
    case "dia":
      return dataLonga(data);
    case "mes":
      return mesPorExtenso(data);
    case "semana":
    case "lista":
      return `${diaEMes(janela.from)} a ${diaEMes(ultimo)}`;
  }
}

/** A grade do mês em semanas (linhas de 7 dias). */
export function semanasDaJanela(janela: Janela): string[][] {
  const semanas: string[][] = [];
  for (let i = 0; i < janela.dias.length; i += 7) semanas.push(janela.dias.slice(i, i + 7));
  return semanas;
}

// ── A query string ──────────────────────────────────────────────────────────

/**
 * Serializa **omitindo o que é padrão** — a visão do dia e a data de hoje não
 * vão para a URL. Um link sem `data` abre sempre no hoje da casa, que é o que
 * quem manda "olha a agenda" quer dizer.
 */
export function paraQueryString(filtros: FiltrosDaAgenda, hoje: string): string {
  const p = new URLSearchParams();
  if (filtros.visao !== VISAO_PADRAO) p.set("visao", filtros.visao);
  if (filtros.data !== hoje) p.set("data", filtros.data);
  if (filtros.tipo) p.set("tipo", filtros.tipo);
  if (filtros.unidade) p.set("unidade", filtros.unidade);
  if (filtros.responsavel) p.set("responsavel", filtros.responsavel);
  if (filtros.cancelados) p.set("cancelados", "1");
  const q = p.toString();
  return q ? `?${q}` : "";
}

export function temFiltro(f: FiltrosDaAgenda): boolean {
  return Boolean(f.tipo || f.unidade || f.responsavel || f.cancelados);
}

/** Por página, o teto do contrato: as visões pedem a janela inteira e seguem
 *  `meta.total_pages`. */
export const POR_PAGINA = 100;

/**
 * A consulta de `GET /agenda/events` para a janela e os filtros. `eu` é o id
 * do usuário da sessão — só usado no filtro "comigo".
 */
export function consultaDosEventos(filtros: FiltrosDaAgenda, janela: Janela, eu: string): Query {
  return {
    from: janela.from,
    to: janela.to,
    sort: "starts_at",
    per_page: POR_PAGINA,
    type: filtros.tipo || undefined,
    // O padrão da API é `agendado,concluido`; pedir os desmarcados é pedir os três.
    status: filtros.cancelados ? "agendado,concluido,cancelado" : undefined,
    unit_id: filtros.unidade || undefined,
    assignee_id: filtros.responsavel === "eu" ? eu : undefined,
    unassigned: filtros.responsavel === "ninguem" ? true : undefined,
  };
}

// ── Que dias um evento ocupa ────────────────────────────────────────────────

/**
 * O primeiro e o último dia (inclusive) que o evento toca, em dias da casa.
 *
 * O primeiro é o `date` da API. O último sai do `ends_at` lido no relógio da
 * casa — e o fim é **exclusivo**: o evento de dia inteiro termina à meia-noite
 * do dia seguinte ao último, e a visita que acaba às 00:00 não toca o dia
 * seguinte.
 */
export function diasDoEvento(evento: Pick<EventoDaAgenda, "date" | "ends_at">): { primeiro: string; ultimo: string } {
  const fim = paredeDaCasa(evento.ends_at);
  if (!fim) return { primeiro: evento.date, ultimo: evento.date };
  const ultimo = fim.minutos === 0 ? somarDias(fim.data, -1) : fim.data;
  return { primeiro: evento.date, ultimo: ultimo < evento.date ? evento.date : ultimo };
}

/** Os eventos que tocam o dia, na ordem em que a API os entregou. */
export function eventosDoDia<T extends Pick<EventoDaAgenda, "date" | "ends_at">>(eventos: readonly T[], dia: string): T[] {
  return eventos.filter((e) => {
    const { primeiro, ultimo } = diasDoEvento(e);
    return primeiro <= dia && dia <= ultimo;
  });
}

export type GrupoDoDia = {
  data: string;
  eventos: EventoDaAgenda[];
};

/**
 * A lista agrupada **por `date`** — um grupo por dia da janela, inclusive os
 * vazios (o dia livre também é informação para quem escala a limpeza).
 *
 * O evento que começou antes da janela (a manutenção que vai de ontem a
 * sexta) entra no primeiro dia dela: a API o devolve porque ele **cruza** a
 * janela, e escondê-lo num grupo anterior que a tela não desenha seria
 * escondê-lo. Fora isso, a ordem é a da API (`starts_at`, depois `id`).
 */
export function agruparPorDia(eventos: readonly EventoDaAgenda[], janela: Janela): GrupoDoDia[] {
  const grupos = new Map<string, EventoDaAgenda[]>(janela.dias.map((d) => [d, []]));
  for (const e of eventos) {
    const chave = e.date < janela.from ? janela.from : e.date;
    grupos.get(chave)?.push(e);
  }
  return janela.dias.map((data) => ({ data, eventos: grupos.get(data) ?? [] }));
}
