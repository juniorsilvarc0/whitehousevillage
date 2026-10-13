import { z } from "zod";

import { paraTextoOuNulo } from "@/lib/acoes/campos";
import { ehDataISO, somarDias } from "@/lib/datas";
import { instanteDaCasa, meiaNoiteDaCasa, paredeDaCasa } from "@/lib/fuso";
import { diasDoEvento } from "@/lib/agenda/visoes";
import {
  TIPOS_MANUAIS,
  type BloqueioDeHorarioEntrada,
  type ConfiguracaoDaAgenda,
  type ConfiguracaoDaAgendaEntrada,
  type EdicaoDoEvento,
  type EventoAtualizarEntrada,
  type EventoCriarEntrada,
  type EventoDaAgenda,
  type EventoSubstituirEntrada,
  type TipoManual,
} from "@/lib/agenda/tipos";

/**
 * Os formulários da agenda — espelho de `EventoDaAgendaCriar|Substituir|
 * Atualizar`, `BloqueioDeHorarioCriar` e `ConfiguracaoDaAgendaEntrada`.
 *
 * Mesmo arranjo dos outros formulários do painel: tudo é `string` no
 * formulário (o `<input>` fala string), e a conversão acontece num lugar só, nas
 * funções `…Para…` daqui de baixo, na fronteira com o DTO. O schema roda no
 * navegador e de novo dentro da action (Server Action é endpoint público).
 *
 * **Data e hora em campos separados** (`docs/ui.md` §10 proíbe
 * `datetime-local`), juntados pela hora de parede **da casa**
 * (`lib/fuso.ts`): a visita marcada para as 14:00 por quem está em Lisboa é às
 * 14:00 em Fortaleza. O dia inteiro vai com a meia-noite da casa nas duas
 * pontas, e o último dia é **inclusivo** na tela e exclusivo no fio — o
 * `ends_at` é a meia-noite do dia seguinte, como o contrato pede.
 *
 * **O que NÃO é validado aqui:** fim depois do início, até 31 dias, um ano para
 * trás e três à frente, "check-in muda de hora e não de dia", "a limpeza nunca
 * antes do fim do check-out", a coerência dos horários da operação. Tudo isso é
 * de `internal/domain/agenda`, e a recusa da API (`422` por campo) cai no campo
 * certo. Copiar para cá seria a segunda verdade que diverge.
 */

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const HORA = /^([01]\d|2[0-3]):[0-5]\d$/;
/** `HoraDoDia` do contrato: `00:00` a `24:00`. */
const HORA_DO_DIA = /^(([01]\d|2[0-3]):[0-5]\d|24:00)$/;

const idOpcional = (mensagem: string) => z.string().refine((v) => v === "" || UUID.test(v), mensagem);

// ── Evento ──────────────────────────────────────────────────────────────────

/**
 * Em que o formulário está: criando, ou editando com o `editable` que a API
 * mandou. Cada modo valida só o que manda — o título de um check-in não é
 * conferido, porque não vai no corpo.
 */
export type ModoDoEvento = "criar" | Exclude<EdicaoDoEvento, "nada">;

export type EventoFormulario = {
  type: string;
  title: string;
  all_day: boolean;
  data_inicio: string;
  hora_inicio: string;
  /** Dia inteiro: o último dia (inclusivo). Com hora: o dia do término —
   *  vazio é o mesmo dia do início. */
  data_fim: string;
  /** Vazio, na criação: a API usa um passo da grade (`slot_minutes`). */
  hora_fim: string;
  unit_id: string;
  reservation_id: string;
  contact_id: string;
  /** Vazio = ninguém. */
  assignee_id: string;
  notes: string;
};

export function esquemaDoEvento(modo: ModoDoEvento) {
  const comCadastro = modo === "criar" || modo === "tudo";
  const comHorario = modo !== "so_notas";
  return z
    .object({
      type: z.string(),
      title: z.string(),
      all_day: z.boolean(),
      data_inicio: z.string(),
      hora_inicio: z.string(),
      data_fim: z.string(),
      hora_fim: z.string(),
      unit_id: idOpcional("Escolha a unidade de novo."),
      reservation_id: idOpcional("Escolha a reserva de novo."),
      contact_id: idOpcional("Escolha a pessoa de novo."),
      assignee_id: idOpcional("Escolha o responsável de novo."),
      notes: z.string().max(2000, "Anotação com no máximo 2000 caracteres."),
    })
    .superRefine((v, ctx) => {
      if (comCadastro) {
        if (!(TIPOS_MANUAIS as readonly string[]).includes(v.type)) {
          ctx.addIssue({ code: "custom", path: ["type"], message: "Escolha o que é: visita, evento ou limpeza." });
        }
        const titulo = v.title.trim();
        if (titulo.length === 0) ctx.addIssue({ code: "custom", path: ["title"], message: "Dê um nome — “Visita com a família Andrade”." });
        else if (titulo.length > 200) ctx.addIssue({ code: "custom", path: ["title"], message: "Título com no máximo 200 caracteres." });
      }
      if (!comHorario) return;
      if (!ehDataISO(v.data_inicio.trim())) {
        ctx.addIssue({ code: "custom", path: ["data_inicio"], message: v.all_day ? "Informe o primeiro dia." : "Informe o dia." });
      }
      if (v.data_fim.trim() !== "" && !ehDataISO(v.data_fim.trim())) {
        ctx.addIssue({ code: "custom", path: ["data_fim"], message: "Informe uma data válida, ou deixe vazio." });
      }
      if (v.all_day) return;
      if (!HORA.test(v.hora_inicio.trim())) {
        ctx.addIssue({ code: "custom", path: ["hora_inicio"], message: "Informe a hora de início." });
      }
      const fimObrigatorio = modo !== "criar";
      const fim = v.hora_fim.trim();
      if (fim === "" ? fimObrigatorio : !HORA.test(fim)) {
        ctx.addIssue({ code: "custom", path: ["hora_fim"], message: "Informe a hora do término." });
      }
    });
}

export function eventoVazio(parcial: Partial<EventoFormulario> = {}): EventoFormulario {
  return {
    type: "visita",
    title: "",
    all_day: false,
    data_inicio: "",
    hora_inicio: "",
    data_fim: "",
    hora_fim: "",
    unit_id: "",
    reservation_id: "",
    contact_id: "",
    assignee_id: "",
    notes: "",
    ...parcial,
  };
}

/**
 * A unidade gravada no evento manual, para o `PUT` devolver o que estava.
 *
 * Com `unit_id` na resposta (campo aditivo do módulo Go, fora do contrato),
 * é ele. Sem ele, `units` tem de ser lido: o manual **sem** reserva ocupa só a
 * unidade gravada (zero ou uma); o manual **com** reserva ocupa a gravada ou,
 * sem ela, as da reserva — e aí uma unidade só é ambígua. A leitura escolhe
 * essa unidade, que ocupa exatamente o mesmo lugar; com mais de uma, são as
 * da reserva, e o campo fica vazio ("as da reserva").
 */
function unidadeGravada(evento: EventoDaAgenda): string {
  if (evento.origin !== "manual") return "";
  if (evento.unit_id !== undefined) return evento.unit_id ?? "";
  return evento.units.length === 1 ? evento.units[0]!.unit_id : "";
}

/** O formulário de edição a partir do evento como a API o devolveu. */
export function valoresDoEvento(evento: EventoDaAgenda): EventoFormulario {
  const base = {
    type: evento.type,
    title: evento.title,
    all_day: evento.all_day,
    unit_id: unidadeGravada(evento),
    reservation_id: evento.reservation_id ?? "",
    contact_id: evento.contact_id ?? "",
    assignee_id: evento.assignee_id ?? "",
    notes: evento.notes ?? "",
  };
  if (evento.all_day) {
    const { ultimo } = diasDoEvento(evento);
    return { ...base, data_inicio: evento.date, hora_inicio: "", data_fim: ultimo === evento.date ? "" : ultimo, hora_fim: "" };
  }
  const inicio = paredeDaCasa(evento.starts_at);
  const fim = paredeDaCasa(evento.ends_at);
  const dataInicio = inicio?.data ?? evento.date;
  return {
    ...base,
    data_inicio: dataInicio,
    hora_inicio: inicio?.hora ?? "",
    data_fim: fim && fim.data !== dataInicio ? fim.data : "",
    hora_fim: fim?.hora ?? "",
  };
}

/**
 * A janela do formulário em instantes da casa.
 *
 * - Dia inteiro: meia-noite do primeiro dia → meia-noite do dia seguinte ao
 *   último (o último dia é inclusivo na tela).
 * - Com hora: o dia e a hora de início; o término no dia do término (vazio =
 *   o mesmo dia). Sem hora de término, `ends_at` não vai — a API usa um passo.
 */
export function janelaDoFormulario(v: Pick<EventoFormulario, "all_day" | "data_inicio" | "hora_inicio" | "data_fim" | "hora_fim">): {
  starts_at: string;
  ends_at?: string;
} {
  const inicio = v.data_inicio.trim();
  const ultimoOuFim = v.data_fim.trim() || inicio;
  if (v.all_day) {
    return { starts_at: meiaNoiteDaCasa(inicio), ends_at: meiaNoiteDaCasa(somarDias(ultimoOuFim, 1)) };
  }
  const starts_at = instanteDaCasa(inicio, v.hora_inicio.trim());
  const horaFim = v.hora_fim.trim();
  return horaFim ? { starts_at, ends_at: instanteDaCasa(ultimoOuFim, horaFim) } : { starts_at };
}

/** `""` vira `null` — a distinção que o contrato usa para "sem" / "limpar". */
function idOuNulo(v: string): string | null {
  return v.trim() === "" ? null : v.trim();
}

/** Corpo de `POST /agenda/events`. */
export function eventoParaCriar(v: EventoFormulario): EventoCriarEntrada {
  const janela = janelaDoFormulario(v);
  const corpo: EventoCriarEntrada = {
    type: v.type as TipoManual,
    title: v.title.trim(),
    starts_at: janela.starts_at,
    all_day: v.all_day,
    unit_id: idOuNulo(v.unit_id),
    reservation_id: idOuNulo(v.reservation_id),
    contact_id: idOuNulo(v.contact_id),
    assignee_id: idOuNulo(v.assignee_id),
    notes: paraTextoOuNulo(v.notes),
  };
  if (janela.ends_at) corpo.ends_at = janela.ends_at;
  return corpo;
}

/** Corpo do `PUT` — o manual agendado inteiro. Anulável vazio vai `null`. */
export function eventoParaSubstituir(v: EventoFormulario): EventoSubstituirEntrada {
  const janela = janelaDoFormulario(v);
  return {
    type: v.type as TipoManual,
    title: v.title.trim(),
    starts_at: janela.starts_at,
    // No modo `tudo` a hora de término é obrigatória, então `ends_at` existe;
    // o `??` só cobre o dia inteiro, que sempre o tem.
    ends_at: janela.ends_at ?? janela.starts_at,
    all_day: v.all_day,
    unit_id: idOuNulo(v.unit_id),
    reservation_id: idOuNulo(v.reservation_id),
    contact_id: idOuNulo(v.contact_id),
    assignee_id: idOuNulo(v.assignee_id),
    notes: paraTextoOuNulo(v.notes),
  };
}

/**
 * Corpo do `PATCH`, pelo `editable`:
 *
 * - `segue_a_reserva`: horário, responsável e anotação — os quatro campos que
 *   o automático aceita. Repetir o que já estava passa.
 * - `so_notas`: só a anotação. Qualquer outra chave no concluído seria `409`.
 */
export function eventoParaAtualizar(v: EventoFormulario, editable: "segue_a_reserva" | "so_notas"): EventoAtualizarEntrada {
  if (editable === "so_notas") return { notes: paraTextoOuNulo(v.notes) };
  const janela = janelaDoFormulario(v);
  return {
    starts_at: janela.starts_at,
    ends_at: janela.ends_at ?? janela.starts_at,
    assignee_id: idOuNulo(v.assignee_id),
    notes: paraTextoOuNulo(v.notes),
  };
}

// ── Bloqueio de horário da operação ─────────────────────────────────────────

export const BloqueioFormulario = z
  .object({
    reason: z.string(),
    all_day: z.boolean(),
    data_inicio: z.string(),
    hora_inicio: z.string(),
    data_fim: z.string(),
    hora_fim: z.string(),
  })
  .superRefine((v, ctx) => {
    const motivo = v.reason.trim();
    if (motivo.length === 0) ctx.addIssue({ code: "custom", path: ["reason"], message: "Diga o motivo — “Equipe em treinamento”." });
    else if (motivo.length > 200) ctx.addIssue({ code: "custom", path: ["reason"], message: "Motivo com no máximo 200 caracteres." });
    if (!ehDataISO(v.data_inicio.trim())) ctx.addIssue({ code: "custom", path: ["data_inicio"], message: "Informe o dia." });
    if (v.data_fim.trim() !== "" && !ehDataISO(v.data_fim.trim())) {
      ctx.addIssue({ code: "custom", path: ["data_fim"], message: "Informe uma data válida, ou deixe vazio." });
    }
    if (v.all_day) return;
    if (!HORA.test(v.hora_inicio.trim())) ctx.addIssue({ code: "custom", path: ["hora_inicio"], message: "Informe a hora de início." });
    if (!HORA.test(v.hora_fim.trim())) ctx.addIssue({ code: "custom", path: ["hora_fim"], message: "Informe a hora do término." });
  });

export type BloqueioFormulario = z.infer<typeof BloqueioFormulario>;

export function bloqueioVazio(parcial: Partial<BloqueioFormulario> = {}): BloqueioFormulario {
  return { reason: "", all_day: false, data_inicio: "", hora_inicio: "", data_fim: "", hora_fim: "", ...parcial };
}

/** Corpo de `POST /agenda/blocks` — a janela pela mesma conversão do evento. */
export function bloqueioParaEntrada(v: BloqueioFormulario): BloqueioDeHorarioEntrada {
  const janela = janelaDoFormulario(v);
  return {
    starts_at: janela.starts_at,
    ends_at: janela.ends_at ?? janela.starts_at,
    all_day: v.all_day,
    reason: v.reason.trim(),
  };
}

// ── Horários da operação (Configurações → Agenda) ───────────────────────────

/**
 * O formulário dos horários. O funcionamento é uma lista plana de intervalos,
 * cada um com o seu dia — é a forma do contrato, e é o que faz o índice de
 * `details` (`business_hours[2].end`) apontar para o mesmo intervalo da tela.
 */
export const HorariosFormulario = z.object({
  checkin_time: z.string().refine((v) => HORA_DO_DIA.test(v.trim()), "Use HH:MM, como 14:00."),
  checkout_time: z.string().refine((v) => HORA_DO_DIA.test(v.trim()), "Use HH:MM, como 11:00."),
  cleaning_start: z.string().refine((v) => HORA_DO_DIA.test(v.trim()), "Use HH:MM, como 11:00."),
  cleaning_end: z.string().refine((v) => HORA_DO_DIA.test(v.trim()), "Use HH:MM, como 14:00."),
  slot_minutes: z.string().refine((v) => /^\d{1,3}$/.test(v.trim()), "Informe os minutos, como 30."),
  buffer_minutes: z.string().refine((v) => /^\d{1,3}$/.test(v.trim()), "Informe os minutos, como 15 — ou 0."),
  business_hours: z.array(
    z.object({
      weekday: z.number().int().min(1).max(7),
      start: z.string().refine((v) => HORA_DO_DIA.test(v.trim()), "Use HH:MM."),
      end: z.string().refine((v) => HORA_DO_DIA.test(v.trim()), "Use HH:MM."),
    }),
  ),
});

export type HorariosFormulario = z.infer<typeof HorariosFormulario>;

/**
 * O fim de um intervalo de funcionamento: o seletor de hora do navegador não
 * escreve `24:00`, então **`00:00` no fim é a meia-noite que fecha o dia**. É
 * a única leitura possível — um intervalo que termina às 00:00 do mesmo dia
 * seria vazio —, e é ela que deixa "das 18:00 à meia-noite" ser digitável.
 */
export function fimParaCampo(hora: string): string {
  return hora === "24:00" ? "00:00" : hora;
}

export function fimDoCampo(hora: string): string {
  return hora.trim() === "00:00" ? "24:00" : hora.trim();
}

export function valoresDosHorarios(c: ConfiguracaoDaAgenda): HorariosFormulario {
  return {
    checkin_time: c.checkin_time,
    checkout_time: c.checkout_time,
    cleaning_start: c.cleaning_start,
    cleaning_end: c.cleaning_end,
    slot_minutes: String(c.slot_minutes),
    buffer_minutes: String(c.buffer_minutes),
    business_hours: c.business_hours.map((h) => ({ weekday: h.weekday, start: h.start, end: fimParaCampo(h.end) })),
  };
}

/** Corpo do `PUT /agenda/settings` — a configuração inteira, na ordem da tela. */
export function horariosParaEntrada(v: HorariosFormulario): ConfiguracaoDaAgendaEntrada {
  return {
    checkin_time: v.checkin_time.trim(),
    checkout_time: v.checkout_time.trim(),
    cleaning_start: v.cleaning_start.trim(),
    cleaning_end: v.cleaning_end.trim(),
    business_hours: v.business_hours.map((h) => ({ weekday: h.weekday, start: h.start.trim(), end: fimDoCampo(h.end) })),
    slot_minutes: Number.parseInt(v.slot_minutes.trim(), 10),
    buffer_minutes: Number.parseInt(v.buffer_minutes.trim(), 10),
  };
}
