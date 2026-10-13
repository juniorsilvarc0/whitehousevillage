import type { CodigoDeErro } from "@/lib/api/codigos";
import { mensagemDoErro, type Falha } from "@/lib/acoes/resultado";
import { fraseDoConflito } from "@/lib/agenda/rotulos";
import { OCUPACOES_DA_UNIDADE, type ConflitoDeUnidade, type EdicaoDoEvento } from "@/lib/agenda/tipos";

/**
 * O que dizer quando a agenda recusa — **pelo código**, nunca pelo texto da
 * API. Os `details` entram só onde o contrato os promete: `conflicts` do
 * `UNIT_NOT_AVAILABLE`, `editable` do `AGENDA_EVENT_CLOSED`, `reason` do
 * `INVALID_STATE_TRANSITION` e do `FORBIDDEN`, e o erro por campo do
 * `VALIDATION_ERROR`.
 */

export const CAMINHO_DA_AGENDA = "/app/agenda";
export const CAMINHO_DOS_BLOQUEIOS = "/app/agenda/bloqueios";
export const CAMINHO_DOS_HORARIOS = "/app/configuracoes/agenda";

/** Onde a recusa aconteceu — o mesmo código quer frases diferentes. */
export type ContextoDaAgenda = "criacao" | "edicao" | "conclusao" | "desmarcacao" | "bloqueio" | "horarios";

function texto(d: Record<string, unknown>, chave: string): string | null {
  const v = d[chave];
  return typeof v === "string" && v !== "" ? v : null;
}

/** `details.editable` do `AGENDA_EVENT_CLOSED`, quando vier no vocabulário. */
function edicaoDe(details: Record<string, unknown>): EdicaoDoEvento | null {
  const v = details?.editable;
  return v === "tudo" || v === "segue_a_reserva" || v === "so_notas" || v === "nada" ? v : null;
}

/** `details.conflicts` lido sem confiar nele: linha fora da forma é descartada. */
export function conflitosDe(details: Record<string, unknown> | undefined): ConflitoDeUnidade[] {
  const lista = details?.conflicts;
  if (!Array.isArray(lista)) return [];
  const saida: ConflitoDeUnidade[] = [];
  for (const item of lista) {
    if (typeof item !== "object" || item === null) continue;
    const c = item as Record<string, unknown>;
    if (typeof c.starts_at !== "string" || typeof c.ends_at !== "string") continue;
    const kind = (OCUPACOES_DA_UNIDADE as readonly string[]).includes(String(c.kind)) ? (c.kind as ConflitoDeUnidade["kind"]) : null;
    if (!kind) continue;
    saida.push({
      unit_id: typeof c.unit_id === "string" ? c.unit_id : "",
      unit_code: typeof c.unit_code === "string" ? c.unit_code : undefined,
      kind,
      starts_at: c.starts_at,
      ends_at: c.ends_at,
    });
  }
  return saida;
}

export function mensagemDaAgenda(falha: Pick<Falha, "code" | "details">, contexto: ContextoDaAgenda = "edicao"): string {
  const d = falha.details ?? {};
  switch (falha.code as CodigoDeErro) {
    case "UNIT_NOT_AVAILABLE":
      return contexto === "criacao"
        ? "A unidade está ocupada nesse horário, e nada foi marcado. Escolha outro horário ou outra unidade."
        : "A unidade está ocupada no horário novo, e nada mudou. Escolha outro horário — ou mova antes o que está no caminho.";
    case "AGENDA_EVENT_CLOSED": {
      const editavel = edicaoDe(d);
      if (editavel === "so_notas") return "Este evento já foi concluído: agora só a anotação pode mudar. A tela foi atualizada.";
      if (editavel === "nada") return "Este evento foi desmarcado e não aceita mais mudanças. A tela foi atualizada.";
      return mensagemDoErro("AGENDA_EVENT_CLOSED");
    }
    case "INVALID_STATE_TRANSITION": {
      const motivo = texto(d, "reason");
      if (motivo === "segue_a_reserva") {
        return contexto === "desmarcacao"
          ? "Este evento segue a reserva e não se desmarca por aqui: ele sai da agenda quando a reserva é cancelada."
          : "Check-in e check-out se concluem pela reserva — registre a entrada ou a saída na tela da reserva.";
      }
      if (motivo === "nao_comecou") return "Ainda não chegou o dia deste evento: ele só pode ser concluído a partir do dia marcado.";
      return "Alguém já mexeu neste evento — ele não aceita mais esse passo. A tela foi atualizada com a situação de agora.";
    }
    case "FORBIDDEN": {
      const motivo = texto(d, "reason");
      if (motivo === "assignee_not_actor") return "No seu perfil, o responsável pelo evento é sempre você.";
      if (motivo === "own_scope_not_applicable") {
        return "O bloqueio de horário vale para a operação inteira, e o seu perfil só mexe nos próprios compromissos. Peça à gestão.";
      }
      if (contexto === "horarios") {
        return "Gravar os horários da operação pede a permissão de editar Configurações — é decisão do dono da casa.";
      }
      return "Seu perfil não tem permissão para mexer na agenda. Se precisar, peça à gestão.";
    }
    case "NOT_FOUND":
      if (contexto === "criacao") return "A unidade, a reserva, a pessoa ou o responsável escolhido não foi encontrado. Feche e abra o formulário de novo.";
      if (contexto === "horarios" || contexto === "bloqueio") return "Esta parte da agenda não respondeu — ela pode ainda não estar no ar.";
      return "Este evento não foi encontrado: ele pode ter saído do seu alcance, ou esta parte do sistema ainda não está no ar.";
    default:
      return mensagemDoErro(falha.code as CodigoDeErro);
  }
}

/** A falha pede recarregar a agenda: o evento mudou por baixo da tela. */
export function pedeRecarga(falha: Pick<Falha, "code">): boolean {
  return falha.code === "AGENDA_EVENT_CLOSED" || falha.code === "INVALID_STATE_TRANSITION" || falha.code === "NOT_FOUND";
}

// ── Erro por campo ──────────────────────────────────────────────────────────

/** Os campos que os formulários de evento e de bloqueio desenham. */
export type CampoDoFormulario =
  | "type"
  | "title"
  | "reason"
  | "all_day"
  | "data_inicio"
  | "hora_inicio"
  | "data_fim"
  | "hora_fim"
  | "unit_id"
  | "reservation_id"
  | "contact_id"
  | "assignee_id"
  | "notes";

export type FalhaNoFormulario = {
  campos: Partial<Record<CampoDoFormulario, string>>;
  /**
   * A recusa sobre o HORÁRIO, mostrada junto dos campos de data e hora — é ali
   * que a pessoa está olhando quando vai trocar o horário. `409
   * UNIT_NOT_AVAILABLE` vem com uma linha por ocupação (`details.conflicts`).
   */
  quando: { mensagem: string; conflitos: string[] } | null;
  /** O que não coube em campo nenhum. `null` quando tudo coube. */
  geral: string | null;
  /** `403 assignee_not_actor`: o perfil só marca para si — o campo responsável
   *  passa a vir fixo no ator. */
  fixarResponsavel: boolean;
};

/**
 * De `details` do contrato para o campo do formulário.
 *
 * O contrato fala a língua do DTO (`starts_at`, `ends_at`), e o formulário a
 * do `<input>` (dia e hora separados). `starts_at` cai no dia de início; o
 * `ends_at`, no término — a hora, ou o último dia quando o evento é de dia
 * inteiro. A revalidação da action devolve os nomes do formulário, que caem
 * direto.
 */
export function falhaNoFormulario(
  falha: Pick<Falha, "code" | "details">,
  contexto: ContextoDaAgenda,
  diaInteiro: boolean,
): FalhaNoFormulario {
  const d = falha.details ?? {};
  const campos: FalhaNoFormulario["campos"] = {};

  if (falha.code === "UNIT_NOT_AVAILABLE") {
    return {
      campos,
      quando: { mensagem: mensagemDaAgenda(falha, contexto), conflitos: conflitosDe(d).map(fraseDoConflito) },
      geral: null,
      fixarResponsavel: false,
    };
  }

  if (falha.code === "FORBIDDEN" && texto(d, "reason") === "assignee_not_actor") {
    return {
      campos: { assignee_id: mensagemDaAgenda(falha, contexto) },
      quando: null,
      geral: null,
      fixarResponsavel: true,
    };
  }

  if (falha.code === "VALIDATION_ERROR") {
    const DIRETOS: [string, CampoDoFormulario][] = [
      ["type", "type"],
      ["title", "title"],
      ["reason", "reason"],
      ["all_day", "all_day"],
      ["starts_at", "data_inicio"],
      ["ends_at", diaInteiro ? "data_fim" : "hora_fim"],
      ["unit_id", "unit_id"],
      ["reservation_id", "reservation_id"],
      ["contact_id", "contact_id"],
      ["assignee_id", "assignee_id"],
      ["notes", "notes"],
      // A revalidação da action devolve os nomes do próprio formulário.
      ["data_inicio", "data_inicio"],
      ["hora_inicio", "hora_inicio"],
      ["data_fim", "data_fim"],
      ["hora_fim", "hora_fim"],
    ];
    for (const [chave, campo] of DIRETOS) {
      const frase = texto(d, chave);
      if (frase && !campos[campo]) campos[campo] = frase;
    }
  }

  const coube = Object.keys(campos).length > 0;
  return { campos, quando: null, geral: coube ? null : mensagemDaAgenda(falha, contexto), fixarResponsavel: false };
}

// ── Os horários da operação ─────────────────────────────────────────────────

export type CampoDosHorarios = "checkin_time" | "checkout_time" | "cleaning_start" | "cleaning_end" | "slot_minutes" | "buffer_minutes";

export type FalhaNosHorarios = {
  campos: Partial<Record<CampoDosHorarios, string>>;
  /** Por índice do intervalo enviado: `business_hours[2].end` → `{2: {end}}`. */
  intervalos: Record<number, { weekday?: string; start?: string; end?: string; linha?: string }>;
  /** `details.business_hours` — o funcionamento inteiro (mais de 4 por dia). */
  funcionamento: string | null;
  geral: string | null;
};

const CHAVE_DO_INTERVALO = /^business_hours\[(\d+)\](?:\.(weekday|start|end))?$/;

/**
 * `agenda.Settings.Validate` devolve **todas** as violações de uma vez, com os
 * nomes do contrato (`checkin_time`, `business_hours[2].end`). Cada uma vai
 * para o seu campo — e o índice do intervalo é o da lista enviada, que é a
 * ordem da tela (`horariosParaEntrada` não reordena).
 */
export function falhaNosHorarios(falha: Pick<Falha, "code" | "details">): FalhaNosHorarios {
  const d = falha.details ?? {};
  const saida: FalhaNosHorarios = { campos: {}, intervalos: {}, funcionamento: null, geral: null };
  if (falha.code !== "VALIDATION_ERROR") {
    saida.geral = mensagemDaAgenda(falha, "horarios");
    return saida;
  }
  const CAMPOS: CampoDosHorarios[] = ["checkin_time", "checkout_time", "cleaning_start", "cleaning_end", "slot_minutes", "buffer_minutes"];
  let algum = false;
  for (const [chave, valor] of Object.entries(d)) {
    if (typeof valor !== "string" || valor === "") continue;
    if ((CAMPOS as string[]).includes(chave)) {
      saida.campos[chave as CampoDosHorarios] = valor;
      algum = true;
      continue;
    }
    if (chave === "business_hours") {
      saida.funcionamento = valor;
      algum = true;
      continue;
    }
    const casado = CHAVE_DO_INTERVALO.exec(chave);
    if (casado) {
      const i = Number(casado[1]);
      const campo = (casado[2] ?? "linha") as "weekday" | "start" | "end" | "linha";
      saida.intervalos[i] = { ...saida.intervalos[i], [campo]: valor };
      algum = true;
    }
  }
  if (!algum) saida.geral = mensagemDaAgenda(falha, "horarios");
  return saida;
}

/** A frase de sucesso do `PUT /agenda/settings` — `meta.retimed_events`. */
export function fraseDosReprogramados(n: number): string {
  if (n === 0) return "Horários gravados. Nenhum evento precisou mudar de horário.";
  if (n === 1) return "Horários gravados. 1 evento automático foi para o horário novo.";
  return `Horários gravados. ${n} eventos automáticos foram para o horário novo.`;
}
