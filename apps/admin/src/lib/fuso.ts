/**
 * Fuso da operação. A casa fica no Ceará: `America/Fortaleza` não tem horário
 * de verão, mas o servidor pode estar em qualquer lugar. Toda formatação de
 * data e hora do painel passa por aqui — data de estadia exibida no fuso do
 * navegador de quem abriu a tela é como uma diária vira dois dias diferentes
 * dependendo de quem olha.
 *
 * É também o **único** lugar do painel que converte entre hora de parede da
 * casa e instante (spec §9, "convertidos num lugar só no Go e num lugar só no
 * painel"): `instanteDaCasa` monta o instante que vai no corpo, `paredeDaCasa`
 * lê a hora de parede de um instante que veio da API. O CRM (`lib/crm/datas.ts`)
 * e a agenda (`lib/agenda/esquemas.ts`) pedem a conversão daqui, e nenhum dos
 * dois guarda um deslocamento próprio.
 */
export const FUSO = "America/Fortaleza";

export function horaLocal(instante: Date = new Date()): number {
  const partes = new Intl.DateTimeFormat("pt-BR", {
    timeZone: FUSO,
    hour: "numeric",
    hour12: false,
  }).format(instante);
  return Number.parseInt(partes, 10);
}

export function saudacao(instante: Date = new Date()): string {
  const hora = horaLocal(instante);
  if (hora < 12) return "Bom dia";
  if (hora < 18) return "Boa tarde";
  return "Boa noite";
}

export function dataPorExtenso(instante: Date = new Date()): string {
  return new Intl.DateTimeFormat("pt-BR", {
    timeZone: FUSO,
    weekday: "long",
    day: "2-digit",
    month: "long",
  }).format(instante);
}

// ── Hora de parede da casa ⇄ instante ───────────────────────────────────────

/** Um instante lido no relógio da casa. */
export type ParedeDaCasa = {
  /** `YYYY-MM-DD` — o dia NA CASA. */
  data: string;
  /** `HH:MM`, de `00:00` a `23:59`. */
  hora: string;
  /** Minutos desde a meia-noite da casa, de 0 a 1439. */
  minutos: number;
};

// `hourCycle: "h23"`, e não `hour12: false`: há motor que escreve a meia-noite
// como "24" com `hour12: false`, e a agenda inteira andaria um dia.
const RELOGIO_DA_CASA = new Intl.DateTimeFormat("en-CA", {
  timeZone: FUSO,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});

const HORA = /^([01]\d|2[0-3]):([0-5]\d)$/;
const DATA = /^\d{4}-\d{2}-\d{2}$/;

/**
 * A hora de parede da casa num instante — o dia, o `HH:MM` e o minuto do dia.
 *
 * Toda data-hora da API já sai no offset da propriedade
 * (`"2026-11-20T14:00:00-03:00"`), mas a leitura não confia nisso: o mesmo
 * instante escrito em UTC (`"2026-11-20T17:00:00Z"`) dá a mesma resposta. Quem
 * agrupa por dia continua usando o `date` que a API manda; isto serve para
 * **posicionar** na grade de horários, que precisa do minuto.
 *
 * `null` para instante ilegível — a tela decide o que mostrar, sem exceção.
 */
export function paredeDaCasa(instante: string | Date): ParedeDaCasa | null {
  const d = typeof instante === "string" ? new Date(instante) : instante;
  if (Number.isNaN(d.getTime())) return null;
  const partes = RELOGIO_DA_CASA.formatToParts(d);
  const pegar = (tipo: string) => partes.find((p) => p.type === tipo)?.value ?? "";
  const hh = pegar("hour") === "24" ? "00" : pegar("hour");
  const mm = pegar("minute");
  const horas = Number.parseInt(hh, 10);
  const minutos = Number.parseInt(mm, 10);
  if (!Number.isFinite(horas) || !Number.isFinite(minutos)) return null;
  return {
    data: `${pegar("year")}-${pegar("month")}-${pegar("day")}`,
    hora: `${hh}:${mm}`,
    minutos: horas * 60 + minutos,
  };
}

/** O deslocamento da casa num instante, em minutos (Fortaleza: −180). */
function deslocamentoEm(instante: Date): number {
  const p = paredeDaCasa(instante);
  if (!p) return 0;
  const comoSeFosseUTC = Date.parse(`${p.data}T00:00:00Z`) + p.minutos * 60_000;
  const noMinuto = Math.floor(instante.getTime() / 60_000) * 60_000;
  return Math.round((comoSeFosseUTC - noMinuto) / 60_000);
}

function escreverDeslocamento(minutos: number): string {
  const sinal = minutos < 0 ? "-" : "+";
  const abs = Math.abs(minutos);
  const hh = String(Math.floor(abs / 60)).padStart(2, "0");
  const mm = String(abs % 60).padStart(2, "0");
  return `${sinal}${hh}:${mm}`;
}

/**
 * O deslocamento da casa em vigor na hora de parede `hora` do dia `data` —
 * `"-03:00"` em Fortaleza.
 *
 * **Perguntado ao Intl**, e não escrito como `-03:00`: Fortaleza não tem
 * horário de verão hoje, e a constante ficaria certa por anos e erraria em
 * silêncio no dia em que a regra mudasse. A pergunta é feita duas vezes — a
 * primeira com a hora de parede lida como UTC, a segunda já no instante
 * corrigido —, o que acerta também a hora vizinha de uma virada de horário.
 */
export function deslocamentoDaCasa(data: string, hora = "12:00"): string {
  const casado = HORA.exec(hora);
  const minutosDoDia = casado ? Number(casado[1]) * 60 + Number(casado[2]) : 12 * 60;
  const ingenuo = Date.parse(`${data}T00:00:00Z`) + minutosDoDia * 60_000;
  if (Number.isNaN(ingenuo)) return escreverDeslocamento(deslocamentoEm(new Date()));
  const primeiro = deslocamentoEm(new Date(ingenuo));
  const refinado = deslocamentoEm(new Date(ingenuo - primeiro * 60_000));
  return escreverDeslocamento(refinado);
}

/**
 * O instante, **no offset da casa**, da hora de parede `hora` (`HH:MM`) do dia
 * `data`: `("2026-11-20", "14:00")` → `"2026-11-20T14:00:00-03:00"`.
 *
 * É o formato que o contrato pede ("com offset — instante sem offset é `422`,
 * não se adivinha o fuso") e o que ele devolve: a hora digitada é a da casa,
 * seja qual for o fuso do navegador de quem marcou.
 */
export function instanteDaCasa(data: string, hora: string): string {
  if (!DATA.test(data) || !HORA.test(hora)) {
    throw new Error(`instanteDaCasa: esperava YYYY-MM-DD e HH:MM, recebeu ${JSON.stringify([data, hora])}`);
  }
  return `${data}T${hora}:00${deslocamentoDaCasa(data, hora)}`;
}

/**
 * A meia-noite **da casa** no começo do dia `data` — a ponta de todo evento de
 * dia inteiro (`all_day: true` exige `starts_at` e `ends_at` à meia-noite da
 * casa, e não à do navegador).
 */
export function meiaNoiteDaCasa(data: string): string {
  return instanteDaCasa(data, "00:00");
}
