import { somarDias } from "@/lib/datas";
import { paredeDaCasa } from "@/lib/fuso";
import { diaDaSemanaISO } from "@/lib/agenda/rotulos";
import { diasDoEvento } from "@/lib/agenda/visoes";
import type { BloqueioDeHorario, EventoDaAgenda, IntervaloDeFuncionamento } from "@/lib/agenda/tipos";

/**
 * A geometria da grade de horários — as visões de dia e de semana
 * (`docs/ui.md` §9, `AgendaGrid`: "grade própria, sem biblioteca de
 * calendário; layout de colisão em colunas").
 *
 * Tudo em **minutos do dia da casa** (0 a 1440). O instante da API é lido no
 * relógio da casa por `lib/fuso.ts`; daqui para baixo é só aritmética de
 * desenho — onde começa, onde termina, quantas colunas dividem o espaço. Não
 * há regra de negócio: o que colide com o quê para **recusar** é da API
 * (`agenda.CheckUnit`); a colisão daqui é visual — dois cartões que se
 * sobrepõem na tela vão lado a lado.
 */

export const MINUTOS_NO_DIA = 1440;

/** Um pedaço de evento dentro de um dia: o evento das 22:00 às 02:00 tem dois. */
export type Trecho = {
  evento: EventoDaAgenda;
  /** Minutos desde a meia-noite da casa. */
  inicio: number;
  /** Exclusivo. 1440 quando segue para o dia seguinte. */
  fim: number;
  /** Começou num dia anterior. */
  continua: boolean;
  /** Segue para um dia seguinte. */
  segue: boolean;
};

/**
 * O trecho do evento no dia — `null` se ele não toca o dia, ou se é de dia
 * inteiro (esse vai para a faixa de cima, não para a grade).
 */
export function trechoNoDia(evento: EventoDaAgenda, dia: string): Trecho | null {
  if (evento.all_day) return null;
  const { primeiro, ultimo } = diasDoEvento(evento);
  if (dia < primeiro || dia > ultimo) return null;
  const inicio = paredeDaCasa(evento.starts_at);
  const fim = paredeDaCasa(evento.ends_at);
  const ini = dia === primeiro && inicio ? inicio.minutos : 0;
  const ate = fim && dia === fim.data ? fim.minutos : MINUTOS_NO_DIA;
  return {
    evento,
    inicio: ini,
    fim: Math.max(ate, ini + 1),
    continua: dia > primeiro,
    segue: dia < ultimo,
  };
}

/** Os eventos de dia inteiro que tocam o dia — a faixa de cima. */
export function diaInteiroNoDia(eventos: readonly EventoDaAgenda[], dia: string): EventoDaAgenda[] {
  return eventos.filter((e) => {
    if (!e.all_day) return false;
    const { primeiro, ultimo } = diasDoEvento(e);
    return primeiro <= dia && dia <= ultimo;
  });
}

export type TrechoDisposto = Trecho & {
  /** A coluna do cartão dentro do grupo de colisão (0…colunas−1). */
  coluna: number;
  /** Quantas colunas dividem o grupo. */
  colunas: number;
};

/**
 * O layout de colisão em colunas.
 *
 * 1. Ordena por início (o mais longo primeiro no empate).
 * 2. Junta em grupos os trechos que se sobrepõem em cadeia — um grupo fecha
 *    quando o próximo começa depois de tudo o que já está nele terminar.
 * 3. Dentro do grupo, cada trecho vai para a primeira coluna livre; o grupo
 *    inteiro divide a largura pelo número de colunas que usou.
 *
 * `minimoVisual` é a altura mínima do cartão, em minutos: o check-out de 30
 * minutos ocupa mais tela do que 30 minutos, e é o tamanho **desenhado** que
 * decide se dois cartões se encostam — senão o de baixo nasceria por cima.
 */
export function dispor(trechos: readonly Trecho[], minimoVisual = 0): TrechoDisposto[] {
  const ordenados = [...trechos].sort(
    (a, b) => a.inicio - b.inicio || b.fim - a.fim || a.evento.id.localeCompare(b.evento.id),
  );
  const fimVisual = (t: Trecho) => Math.max(t.fim, t.inicio + minimoVisual);

  const saida: TrechoDisposto[] = [];
  let grupo: TrechoDisposto[] = [];
  let colunasDoGrupo: number[] = []; // o fim visual do último cartão de cada coluna
  let fimDoGrupo = -1;

  const fechar = () => {
    for (const t of grupo) t.colunas = colunasDoGrupo.length;
    saida.push(...grupo);
    grupo = [];
    colunasDoGrupo = [];
    fimDoGrupo = -1;
  };

  for (const t of ordenados) {
    if (grupo.length > 0 && t.inicio >= fimDoGrupo) fechar();
    let coluna = colunasDoGrupo.findIndex((fim) => fim <= t.inicio);
    if (coluna < 0) {
      coluna = colunasDoGrupo.length;
      colunasDoGrupo.push(fimVisual(t));
    } else {
      colunasDoGrupo[coluna] = fimVisual(t);
    }
    grupo.push({ ...t, coluna, colunas: 1 });
    fimDoGrupo = Math.max(fimDoGrupo, fimVisual(t));
  }
  if (grupo.length > 0) fechar();
  return saida;
}

// ── O fundo da grade: funcionamento e bloqueios de horário ──────────────────

export type Faixa = { inicio: number; fim: number };

function minutosDaHora(hora: string): number {
  if (hora === "24:00") return MINUTOS_NO_DIA;
  const [h, m] = hora.split(":").map(Number);
  return (h ?? 0) * 60 + (m ?? 0);
}

/** Os intervalos de funcionamento do dia, em minutos. Dia sem intervalo é dia
 *  fechado (lista vazia). */
export function abertoNoDia(horarios: readonly IntervaloDeFuncionamento[], dia: string): Faixa[] {
  const iso = diaDaSemanaISO(dia);
  return horarios
    .filter((h) => h.weekday === iso)
    .map((h) => ({ inicio: minutosDaHora(h.start), fim: minutosDaHora(h.end) }))
    .filter((f) => f.fim > f.inicio)
    .sort((a, b) => a.inicio - b.inicio);
}

/** O complemento do funcionamento dentro da faixa desenhada — o que a grade
 *  sombreia como "fora do horário". */
export function fechadoNoDia(horarios: readonly IntervaloDeFuncionamento[], dia: string, faixa: Faixa): Faixa[] {
  const fechado: Faixa[] = [];
  let cursor = faixa.inicio;
  for (const aberto of abertoNoDia(horarios, dia)) {
    if (aberto.inicio > cursor) fechado.push({ inicio: cursor, fim: Math.min(aberto.inicio, faixa.fim) });
    cursor = Math.max(cursor, aberto.fim);
  }
  if (cursor < faixa.fim) fechado.push({ inicio: cursor, fim: faixa.fim });
  return fechado.filter((f) => f.fim > f.inicio);
}

export type BloqueioNoDia = Faixa & { bloqueio: BloqueioDeHorario; diaInteiro: boolean };

/** Os bloqueios de horário da operação que tocam o dia, em minutos. */
export function bloqueiosNoDia(bloqueios: readonly BloqueioDeHorario[], dia: string): BloqueioNoDia[] {
  const saida: BloqueioNoDia[] = [];
  for (const b of bloqueios) {
    const a = paredeDaCasa(b.starts_at);
    const z = paredeDaCasa(b.ends_at);
    if (!a || !z) continue;
    const ultimo = z.minutos === 0 ? somarDias(z.data, -1) : z.data;
    if (dia < a.data || dia > ultimo) continue;
    const inicio = dia === a.data ? a.minutos : 0;
    const fim = dia === z.data ? z.minutos : MINUTOS_NO_DIA;
    saida.push({ bloqueio: b, inicio, fim, diaInteiro: b.all_day || (inicio === 0 && fim === MINUTOS_NO_DIA) });
  }
  return saida;
}

/**
 * As horas que a grade desenha: do começo do funcionamento mais cedo ao fim
 * mais tarde dos dias visíveis — esticadas para caber todo evento e bloqueio
 * que cair fora disso (a visita das 21:00 não pode sumir da tela por estar
 * fora do expediente). Arredondadas para a hora cheia. Sem nada para medir,
 * das 08:00 às 20:00.
 */
export function faixaDeHoras(
  dias: readonly string[],
  horarios: readonly IntervaloDeFuncionamento[],
  trechos: readonly Faixa[],
  bloqueios: readonly Faixa[] = [],
): Faixa {
  let inicio = Number.POSITIVE_INFINITY;
  let fim = Number.NEGATIVE_INFINITY;
  const medir = (f: Faixa) => {
    inicio = Math.min(inicio, f.inicio);
    fim = Math.max(fim, f.fim);
  };
  for (const dia of dias) abertoNoDia(horarios, dia).forEach(medir);
  trechos.forEach(medir);
  bloqueios.forEach(medir);
  if (!Number.isFinite(inicio) || !Number.isFinite(fim) || fim <= inicio) return { inicio: 8 * 60, fim: 20 * 60 };
  return {
    inicio: Math.max(0, Math.floor(inicio / 60) * 60),
    fim: Math.min(MINUTOS_NO_DIA, Math.ceil(fim / 60) * 60),
  };
}
