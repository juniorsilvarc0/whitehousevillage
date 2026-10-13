/**
 * Tipos da agenda operacional — espelho da tag `Agenda` de
 * `apps/api/openapi/openapi.yaml` (schemas `EventoDaAgenda`,
 * `EventoDaAgendaCriar|Substituir|Atualizar`, `EscritaNaAgenda`,
 * `AvisoDeAgenda`, `ConflitoDeUnidade`, `BloqueioDeHorario[Criar]`,
 * `ConfiguracaoDaAgenda[Entrada]`, `IntervaloDeFuncionamento` e os enums).
 *
 * Escritos à mão, como os outros espelhos do painel. Campo fora de `required`
 * no contrato fica opcional aqui: a tela tem de ter um plano para a ausência.
 *
 * Recurso RBAC: `agenda` (`supports_own = true`); gravar os horários pede
 * `settings:editar`. O escopo `own` é do servidor (`assignee_id = ator` no
 * SQL): o painel mostra o que vem e não filtra nada.
 */

// ── Vocabulário fechado ─────────────────────────────────────────────────────

/** Os sete da spec §9 (`agenda.Types()`), na ordem em que a tela os lista. */
export const TIPOS_DE_EVENTO = ["checkin", "checkout", "limpeza", "manutencao", "visita", "evento", "bloqueio"] as const;
export type TipoDeEvento = (typeof TIPOS_DE_EVENTO)[number];

/** O que se marca à mão (`agenda.ManualTypes()`). */
export const TIPOS_MANUAIS = ["visita", "evento", "limpeza"] as const;
export type TipoManual = (typeof TIPOS_MANUAIS)[number];

/**
 * `reservation` nasceu da reserva e a segue; `manual` foi marcado aqui;
 * `maintenance` e `stay_block` são **lidos** de `stay_blocks` na hora — mexem-se
 * onde nasceram (a ordem de manutenção, o mapa), nunca por aqui.
 */
export const ORIGENS_DO_EVENTO = ["reservation", "manual", "maintenance", "stay_block"] as const;
export type OrigemDoEvento = (typeof ORIGENS_DO_EVENTO)[number];

export const STATUS_DO_EVENTO = ["agendado", "concluido", "cancelado"] as const;
export type StatusDoEvento = (typeof STATUS_DO_EVENTO)[number];

/** `complete` é `POST /{id}/complete`; `cancel` é `DELETE /{id}` (desmarcar). */
export const ACOES_DO_EVENTO = ["complete", "cancel"] as const;
export type AcaoDoEvento = (typeof ACOES_DO_EVENTO)[number];

/**
 * O que `PUT`/`PATCH` aceitam no estado atual (`agenda.EditableIn`): `tudo` no
 * manual agendado; `segue_a_reserva` no automático agendado (horário,
 * responsável e anotação); `so_notas` no concluído; `nada` no cancelado e no
 * derivado. **Os campos travados do formulário saem daqui.**
 */
export const EDICOES_DO_EVENTO = ["tudo", "segue_a_reserva", "so_notas", "nada"] as const;
export type EdicaoDoEvento = (typeof EDICOES_DO_EVENTO)[number];

export const TIPOS_DE_AVISO = ["fora_do_horario", "bloqueio_de_horario", "responsavel_ocupado"] as const;
export type TipoDeAviso = (typeof TIPOS_DE_AVISO)[number];

export const OCUPACOES_DA_UNIDADE = [
  "estadia",
  "checkin",
  "checkout",
  "limpeza",
  "visita",
  "evento",
  "manutencao",
  "bloqueio",
] as const;
export type OcupacaoDaUnidade = (typeof OCUPACOES_DA_UNIDADE)[number];

// ── Leitura ─────────────────────────────────────────────────────────────────

export type UnidadeDoEvento = {
  unit_id: string;
  unit_code: string;
};

export type EventoDaAgenda = {
  /** `agenda_events.id`; nos derivados, o `stay_blocks.id`. */
  id: string;
  type: TipoDeEvento;
  origin: OrigemDoEvento;
  status: StatusDoEvento;
  title: string;
  /** Instante no offset da casa — a string traz a hora de parede. */
  starts_at: string;
  /** Exclusivo. No dia inteiro, a meia-noite da casa do dia seguinte ao último. */
  ends_at: string;
  all_day: boolean;
  /** O dia da casa em que o evento começa — **é por ele que as visões agrupam**. */
  date: string;
  units: UnidadeDoEvento[];
  /**
   * A unidade GRAVADA no evento manual. **Fora do contrato** em 13/10/2026 (o
   * schema só tem `units`); o módulo Go a manda como campo aditivo. Sem ela,
   * `units` não diz se a unidade veio do evento ou da reserva ligada — e o
   * `PUT` da edição teria de adivinhar. Opcional aqui: ausente, o formulário
   * cai na leitura de `units` (`valoresDoEvento`).
   */
  unit_id?: string | null;
  reservation_id?: string | null;
  reservation_code?: string | null;
  contact_id?: string | null;
  /** Lido na hora; sem documento, telefone nem e-mail. */
  contact_name?: string | null;
  assignee_id?: string | null;
  /** Nome do USUÁRIO responsável. */
  assignee_name?: string | null;
  notes?: string | null;
  /** Só em `origin: maintenance`, quando o bloqueio é de uma ordem. */
  maintenance_order_id?: string | null;
  /** Só nos derivados: a linha de `stay_blocks`. */
  stay_block_id?: string | null;
  done_at?: string | null;
  done_by?: string | null;
  cancelled_at?: string | null;
  /** Calculado na leitura pela mesma regra que recusa a escrita. */
  has_unit_conflict: boolean;
  /** As transições que o evento aceita HOJE. **Os botões saem daqui.** */
  allowed_actions: AcaoDoEvento[];
  editable: EdicaoDoEvento;
  created_by?: string | null;
  created_at?: string | null;
  updated_at?: string | null;
};

/** Conflito de HORÁRIO: o evento foi gravado, e a tela avisa (`meta.warnings`). */
export type AvisoDeAgenda = {
  kind: TipoDeAviso;
  /** Texto da API, para quem depura. A tela escreve a própria frase pelo `kind`. */
  message: string;
  ref_id?: string | null;
  starts_at?: string | null;
  ends_at?: string | null;
};

/** Resposta de `POST`, `PUT` e `PATCH` de evento: o evento e os avisos. */
export type EscritaNaAgenda = {
  evento: EventoDaAgenda;
  avisos: AvisoDeAgenda[];
};

/** Uma linha de `details.conflicts` do `409 UNIT_NOT_AVAILABLE` — a unidade, o
 *  que a ocupa e quando; nunca quem. */
export type ConflitoDeUnidade = {
  unit_id: string;
  unit_code?: string;
  kind: OcupacaoDaUnidade;
  starts_at: string;
  ends_at: string;
};

/** Linha de `agenda_blocks`: o tempo da operação, não uma unidade. */
export type BloqueioDeHorario = {
  id: string;
  starts_at: string;
  /** Exclusivo. */
  ends_at: string;
  all_day: boolean;
  reason: string;
  created_by?: string | null;
  created_at: string;
  updated_at: string;
};

/** Hora de parede da casa, `HH:MM`, de `00:00` a `24:00` (`24:00` só fecha intervalo). */
export type HoraDoDia = string;

export type IntervaloDeFuncionamento = {
  /** ISO: 1 = segunda … 7 = domingo. */
  weekday: number;
  start: HoraDoDia;
  end: HoraDoDia;
};

export type ConfiguracaoDaAgenda = {
  /** `properties.timezone` — só leitura. */
  timezone: string;
  /** O dia de hoje NA CASA — só leitura. **É o "hoje" do painel.** */
  today: string;
  checkin_time: HoraDoDia;
  checkout_time: HoraDoDia;
  cleaning_start: HoraDoDia;
  cleaning_end: HoraDoDia;
  /** Ordenado por dia e início. Dia sem intervalo é dia fechado. */
  business_hours: IntervaloDeFuncionamento[];
  /** Passo da grade das visões de dia e semana. */
  slot_minutes: number;
  /** Folga mínima entre dois compromissos da mesma pessoa. */
  buffer_minutes: number;
  /** `true` enquanto ninguém gravou: os valores estão a confirmar pelo dono. */
  provisional: boolean;
  updated_at: string;
  updated_by?: string | null;
  updated_by_name?: string | null;
};

/** O `PUT /agenda/settings` respondido: a configuração e quantos eventos
 *  automáticos foram para o horário novo. */
export type GravacaoDosHorarios = {
  configuracao: ConfiguracaoDaAgenda;
  reprogramados: number;
};

// ── Escrita (corpos — `additionalProperties: false` no contrato) ────────────
//
// Os tipos de entrada são planos de propósito: `lib/api/corpo-de-escrita.test.ts`
// lê as chaves de cada um e as confere contra as tags `json` do DTO em Go.

/**
 * `EventoDaAgendaCriar` — corpo do `POST /agenda/events`. `ends_at` ausente é
 * `starts_at` + um passo da grade (decidido pela API); `assignee_id` ausente,
 * em escopo `own`, vira o próprio ator.
 */
export type EventoCriarEntrada = {
  type: TipoManual;
  title: string;
  starts_at: string;
  ends_at?: string;
  all_day: boolean;
  unit_id: string | null;
  reservation_id: string | null;
  contact_id: string | null;
  assignee_id?: string | null;
  notes: string | null;
};

/**
 * `EventoDaAgendaSubstituir` — corpo do `PUT`, usado no evento manual agendado
 * (`editable: tudo`). Campo anulável omitido vira `null`, então todos vão.
 */
export type EventoSubstituirEntrada = {
  type: TipoManual;
  title: string;
  starts_at: string;
  ends_at: string;
  all_day: boolean;
  unit_id: string | null;
  reservation_id: string | null;
  contact_id: string | null;
  assignee_id: string | null;
  notes: string | null;
};

/**
 * `EventoDaAgendaAtualizar` — corpo do `PATCH`. Ausente não muda; `null` limpa.
 * O painel o usa no automático agendado (`segue_a_reserva`: horário,
 * responsável e anotação) e no concluído (`so_notas`: só a anotação).
 */
export type EventoAtualizarEntrada = {
  starts_at?: string;
  ends_at?: string;
  assignee_id?: string | null;
  notes?: string | null;
};

/** `BloqueioDeHorarioCriar` — corpo do `POST /agenda/blocks`. */
export type BloqueioDeHorarioEntrada = {
  starts_at: string;
  ends_at: string;
  all_day: boolean;
  reason: string;
};

/** `ConfiguracaoDaAgendaEntrada` — corpo do `PUT /agenda/settings`, inteiro.
 *  `timezone`, `today`, `provisional` e autoria são só leitura (`422`). */
export type ConfiguracaoDaAgendaEntrada = {
  checkin_time: HoraDoDia;
  checkout_time: HoraDoDia;
  cleaning_start: HoraDoDia;
  cleaning_end: HoraDoDia;
  business_hours: IntervaloDeFuncionamento[];
  slot_minutes: number;
  buffer_minutes: number;
};

// ── Apoio do formulário ─────────────────────────────────────────────────────

/** Uma unidade que o formulário oferece. */
export type UnidadeDaAgenda = { id: string; code: string; name: string };

/** Um usuário que pode ser responsável. */
export type PessoaDaAgenda = { id: string; name: string };

/** O que a busca de reserva devolve ao formulário. */
export type ReservaDaAgenda = {
  id: string;
  code: string;
  contact_name: string | null;
  check_in: string;
  check_out: string;
};

/** O que a busca de contato devolve ao formulário — só o nome, que não se
 *  mascara (regra do topo do contrato). */
export type ContatoDaAgenda = { id: string; name: string };
