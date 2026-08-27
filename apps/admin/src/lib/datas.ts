import { FUSO } from "@/lib/fuso";

/**
 * Datas de calendário (`date`, sem hora) no painel.
 *
 * **A armadilha que este arquivo existe para evitar:** `new Date("2026-12-20")`
 * é meia-noite **UTC**, e meia-noite UTC em Fortaleza (UTC−3) é dia 19 às 21h.
 * Formatar isso com o fuso do navegador mostra a véspera — a diária de 20/12
 * apareceria como 19/12 para metade da equipe. Por isso toda formatação de data
 * pura aqui declara `timeZone: "UTC"`: a string veio do banco como `date`, não
 * como instante, e não pode ser reinterpretada.
 *
 * Só o "hoje" é diferente: esse depende de onde a casa fica, e sai do fuso da
 * operação (`America/Fortaleza`), nunca do relógio do servidor.
 */

/** `YYYY-MM-DD` — o formato do contrato para `date`. */
export type DataISO = string;

const PADRAO = /^\d{4}-\d{2}-\d{2}$/;

export function ehDataISO(valor: string): boolean {
  if (!PADRAO.test(valor)) return false;
  const d = new Date(`${valor}T00:00:00Z`);
  return !Number.isNaN(d.getTime()) && d.toISOString().slice(0, 10) === valor;
}

/** Hoje **na operação**, não no relógio de quem abriu a tela. */
export function hojeISO(agora: Date = new Date()): DataISO {
  const partes = new Intl.DateTimeFormat("en-CA", {
    timeZone: FUSO,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(agora);
  const pegar = (tipo: string) => partes.find((p) => p.type === tipo)?.value ?? "";
  return `${pegar("year")}-${pegar("month")}-${pegar("day")}`;
}

export function somarDias(data: DataISO, dias: number): DataISO {
  const d = new Date(`${data}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + dias);
  return d.toISOString().slice(0, 10);
}

/** Noites de uma estadia half-open `[check_in, check_out)`: 20→23 são 3. */
export function noitesEntre(entrada: DataISO, saida: DataISO): number {
  const a = Date.parse(`${entrada}T00:00:00Z`);
  const b = Date.parse(`${saida}T00:00:00Z`);
  if (Number.isNaN(a) || Number.isNaN(b)) return 0;
  return Math.round((b - a) / 86_400_000);
}

/** `20/12/2026`. */
export function formatarData(data: DataISO): string {
  if (!ehDataISO(data)) return data;
  return new Intl.DateTimeFormat("pt-BR", { timeZone: "UTC" }).format(new Date(`${data}T00:00:00Z`));
}

/** `sáb, 20 dez` — para a coluna de noites, onde o ano é ruído. */
export function formatarDataCurta(data: DataISO): string {
  if (!ehDataISO(data)) return data;
  return new Intl.DateTimeFormat("pt-BR", {
    timeZone: "UTC",
    weekday: "short",
    day: "2-digit",
    month: "short",
  })
    .format(new Date(`${data}T00:00:00Z`))
    .replace(".", "");
}

/** `20/12/2026 às 14:32` — para `timestamptz`, que é instante e vai no fuso da casa. */
export function formatarInstante(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return new Intl.DateTimeFormat("pt-BR", {
    timeZone: FUSO,
    dateStyle: "short",
    timeStyle: "short",
  }).format(d);
}

/**
 * Faixa **inclusiva nas duas pontas** — é assim que `special_periods` é
 * declarado no contrato, ao contrário da estadia. A diferença tem que aparecer
 * na tela, senão a gestão cadastra o Réveillon com um dia a menos.
 */
export function diasInclusivos(inicio: DataISO, fim: DataISO): number {
  return noitesEntre(inicio, fim) + 1;
}
