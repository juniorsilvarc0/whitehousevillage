import { ehDataISO, formatarData, somarDias } from "@/lib/datas";
import { paredeDaCasa } from "@/lib/fuso";
import type {
  AcaoDoEvento,
  AvisoDeAgenda,
  ConflitoDeUnidade,
  OcupacaoDaUnidade,
  OrigemDoEvento,
  StatusDoEvento,
  TipoDeAviso,
  TipoDeEvento,
} from "@/lib/agenda/tipos";

/**
 * O vocabulário da agenda, dito na língua de quem limpa, recebe e mostra a
 * casa.
 *
 * `Record<…>` e não `switch`: valor novo no enum do contrato vira erro de
 * compilação aqui, em vez de um rótulo vazio na tela do celular.
 *
 * **Nada aqui decide coisa alguma.** São frases e formatação — a data civil
 * (`date`) com `timeZone: "UTC"` para não ser reinterpretada, e o instante lido
 * no relógio da casa por `lib/fuso.ts`.
 */

export const ROTULO_DO_TIPO: Record<TipoDeEvento, string> = {
  checkin: "Check-in",
  checkout: "Check-out",
  limpeza: "Limpeza",
  manutencao: "Manutenção",
  visita: "Visita",
  evento: "Evento",
  bloqueio: "Bloqueio",
};

export const ROTULO_DO_STATUS: Record<StatusDoEvento, string> = {
  agendado: "Agendado",
  concluido: "Concluído",
  cancelado: "Desmarcado",
};

export const ROTULO_DA_ORIGEM: Record<OrigemDoEvento, string> = {
  reservation: "Da reserva",
  manual: "Marcado à mão",
  maintenance: "Da ordem de manutenção",
  stay_block: "Do bloqueio de unidade",
};

/** O aviso de sucesso de `/complete`, com o gênero de cada tipo. */
export const ROTULO_DA_CONCLUSAO: Record<TipoDeEvento, string> = {
  checkin: "Check-in concluído",
  checkout: "Check-out concluído",
  limpeza: "Limpeza concluída",
  manutencao: "Manutenção concluída",
  visita: "Visita concluída",
  evento: "Evento concluído",
  bloqueio: "Bloqueio concluído",
};

/** O aviso de sucesso do `POST`, com o gênero de cada tipo. */
export const ROTULO_DA_MARCACAO: Record<TipoDeEvento, string> = {
  checkin: "Check-in marcado",
  checkout: "Check-out marcado",
  limpeza: "Limpeza marcada",
  manutencao: "Manutenção marcada",
  visita: "Visita marcada",
  evento: "Evento marcado",
  bloqueio: "Bloqueio marcado",
};

export const ROTULO_DA_ACAO: Record<AcaoDoEvento, string> = {
  complete: "Concluir",
  cancel: "Desmarcar",
};

/** Exemplo do título por tipo — o `placeholder` do formulário. */
export const EXEMPLO_DE_TITULO: Record<"visita" | "evento" | "limpeza", string> = {
  visita: "Visita com a família Andrade",
  evento: "Casamento no jardim",
  limpeza: "Faxina do meio da estadia",
};

/** O que ocupa a unidade, numa linha de `details.conflicts`. */
export const ROTULO_DA_OCUPACAO: Record<OcupacaoDaUnidade, string> = {
  estadia: "hóspede na unidade",
  checkin: "check-in",
  checkout: "check-out",
  limpeza: "limpeza",
  visita: "visita",
  evento: "evento",
  manutencao: "manutenção",
  bloqueio: "bloqueio da unidade",
};

/** Dias da semana ISO (1 = segunda … 7 = domingo), como o contrato os numera. */
export const DIAS_DA_SEMANA: readonly { iso: number; curto: string; longo: string }[] = [
  { iso: 1, curto: "seg", longo: "Segunda" },
  { iso: 2, curto: "ter", longo: "Terça" },
  { iso: 3, curto: "qua", longo: "Quarta" },
  { iso: 4, curto: "qui", longo: "Quinta" },
  { iso: 5, curto: "sex", longo: "Sexta" },
  { iso: 6, curto: "sáb", longo: "Sábado" },
  { iso: 7, curto: "dom", longo: "Domingo" },
];

// ── Datas civis (o `date` da API) ───────────────────────────────────────────

function emUTC(data: string): Date {
  return new Date(`${data}T00:00:00Z`);
}

/** Dia da semana ISO de uma data civil — 1 = segunda … 7 = domingo. A data é
 *  `date`, não instante: lida em UTC, sem fuso nenhum para deslocá-la. */
export function diaDaSemanaISO(data: string): number {
  const d = emUTC(data).getUTCDay();
  return d === 0 ? 7 : d;
}

const LONGA = new Intl.DateTimeFormat("pt-BR", {
  timeZone: "UTC",
  weekday: "long",
  day: "numeric",
  month: "long",
  year: "numeric",
});
const MES_E_ANO = new Intl.DateTimeFormat("pt-BR", { timeZone: "UTC", month: "long", year: "numeric" });
const DIA_E_MES = new Intl.DateTimeFormat("pt-BR", { timeZone: "UTC", day: "2-digit", month: "2-digit" });
const SEMANA_CURTA = new Intl.DateTimeFormat("pt-BR", { timeZone: "UTC", weekday: "short" });

/** `sexta-feira, 20 de novembro de 2026`. */
export function dataLonga(data: string): string {
  return ehDataISO(data) ? LONGA.format(emUTC(data)) : data;
}

/** `novembro de 2026`. */
export function mesPorExtenso(data: string): string {
  return ehDataISO(data) ? MES_E_ANO.format(emUTC(data)) : data;
}

/** `20/11`. */
export function diaEMes(data: string): string {
  return ehDataISO(data) ? DIA_E_MES.format(emUTC(data)) : data;
}

/** `sex` — sem o ponto que o Intl põe em alguns motores. */
export function semanaCurta(data: string): string {
  return ehDataISO(data) ? SEMANA_CURTA.format(emUTC(data)).replace(".", "") : data;
}

/** O número do dia (`20`), para o cabeçalho da coluna e a célula do mês. */
export function numeroDoDia(data: string): string {
  return String(Number(data.slice(8, 10)));
}

// ── Instantes (no relógio da casa) ──────────────────────────────────────────

/** `14:00`, no relógio da casa. */
export function horaDe(instante: string): string {
  return paredeDaCasa(instante)?.hora ?? "";
}

/**
 * A janela de um evento por extenso, como o cartão e o detalhe a dizem:
 *
 * - dia inteiro de um dia: `Dia inteiro`;
 * - dia inteiro de vários: `Dia inteiro · até 22/11` (o último dia, e não o
 *   `ends_at`, que é a meia-noite do dia seguinte);
 * - mesmo dia: `14:00–14:30`;
 * - atravessando dias: `22:00 → 21/11 02:00`.
 */
export function janelaPorExtenso(evento: { starts_at: string; ends_at: string; all_day: boolean; date: string }): string {
  const fim = paredeDaCasa(evento.ends_at);
  if (evento.all_day) {
    if (!fim) return "Dia inteiro";
    // `ends_at` é a meia-noite do dia seguinte ao último.
    const ultimo = somarDias(fim.data, -1);
    return ultimo > evento.date ? `Dia inteiro · até ${diaEMes(ultimo)}` : "Dia inteiro";
  }
  const inicio = paredeDaCasa(evento.starts_at);
  if (!inicio || !fim) return "";
  const terminaNoMesmoDia = fim.data === inicio.data || (fim.minutos === 0 && somarDias(inicio.data, 1) === fim.data);
  if (terminaNoMesmoDia) return `${inicio.hora}–${fim.minutos === 0 && fim.data !== inicio.data ? "24:00" : fim.hora}`;
  return `${inicio.hora} → ${diaEMes(fim.data)} ${fim.hora}`;
}

/** A janela de uma ocupação ou de um aviso: `20/11, das 11:00 às 14:00`, ou
 *  `de 18/11 14:00 a 22/11 11:00` quando atravessa dias. */
export function intervaloPorExtenso(inicio: string, fim: string): string {
  const a = paredeDaCasa(inicio);
  const b = paredeDaCasa(fim);
  if (!a || !b) return "";
  if (a.data === b.data) return `${formatarData(a.data).slice(0, 5)}, das ${a.hora} às ${b.hora}`;
  if (b.minutos === 0 && somarDias(a.data, 1) === b.data) {
    return a.minutos === 0 ? `${formatarData(a.data).slice(0, 5)}, o dia inteiro` : `${formatarData(a.data).slice(0, 5)}, das ${a.hora} à meia-noite`;
  }
  return `de ${diaEMes(a.data)} ${a.hora} a ${diaEMes(b.data)} ${b.hora}`;
}

/**
 * O horário sem repetir o dia — para quando o dia já está escrito ao lado:
 * `das 15:00 às 16:30`, `o dia inteiro`, `até 26/11 11:00` (atravessa dias),
 * `até 25/11` (dias inteiros).
 */
export function horarioSemODia(inicio: string, fim: string): string {
  const a = paredeDaCasa(inicio);
  const b = paredeDaCasa(fim);
  if (!a || !b) return "";
  if (a.data === b.data) return `das ${a.hora} às ${b.hora}`;
  if (a.minutos === 0 && b.minutos === 0) {
    const ultimo = somarDias(b.data, -1);
    return ultimo === a.data ? "o dia inteiro" : `o dia inteiro, até ${diaEMes(ultimo)}`;
  }
  if (b.minutos === 0 && somarDias(a.data, 1) === b.data) return `das ${a.hora} à meia-noite`;
  return `das ${a.hora} até ${diaEMes(b.data)} ${b.hora}`;
}

// ── Avisos e conflitos — a frase sai do `kind`, nunca do `message` ─────────

/**
 * O aviso de horário (`meta.warnings`) numa frase. **Pelo `kind`**, como a tela
 * faz com `error.code`: o `message` da API é para quem depura.
 */
export function fraseDoAviso(aviso: Pick<AvisoDeAgenda, "kind" | "starts_at" | "ends_at">): string {
  const quando = aviso.starts_at && aviso.ends_at ? intervaloPorExtenso(aviso.starts_at, aviso.ends_at) : "";
  const FRASES: Record<TipoDeAviso, string> = {
    fora_do_horario: "Fica fora do horário de funcionamento da operação.",
    bloqueio_de_horario: quando
      ? `Cai num horário bloqueado da operação (${quando}).`
      : "Cai num horário bloqueado da operação.",
    responsavel_ocupado: quando
      ? `O responsável tem outro compromisso (${quando}), sem a folga entre um e outro.`
      : "O responsável tem outro compromisso nesse horário, sem a folga entre um e outro.",
  };
  return FRASES[aviso.kind] ?? "Há um aviso de horário para este evento.";
}

/** Uma linha de `details.conflicts`: `AP-02 · limpeza · 20/11, das 11:00 às 14:00`. */
export function fraseDoConflito(conflito: ConflitoDeUnidade): string {
  const onde = conflito.unit_code || "A unidade";
  const oque = ROTULO_DA_OCUPACAO[conflito.kind] ?? "outra ocupação";
  const quando = intervaloPorExtenso(conflito.starts_at, conflito.ends_at);
  return [onde, oque, quando].filter(Boolean).join(" · ");
}
