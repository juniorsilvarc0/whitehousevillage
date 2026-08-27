/**
 * Tipos do CRM — espelho de `apps/api/openapi/openapi.yaml` (tags `CRM`).
 *
 * Escritos à mão pelo mesmo motivo de `lib/api/types.ts` e `lib/api/comercial.ts`:
 * o contrato é a fonte da verdade e um gerador entraria no caminho sem
 * acrescentar nada enquanto a superfície couber numa leitura.
 *
 * Ficam em `lib/crm/` e não em `lib/api/` por ownership de pasta: o CRM é
 * escrito em paralelo com o mapa e com o tempo real, e um arquivo compartilhado
 * seria o único ponto de colisão entre três agentes.
 */

import type { Orcamento } from "@/lib/api/comercial";
import type { DataISO } from "@/lib/datas";

// ── Funil e etapas ─────────────────────────────────────────────────────────

/** `ganho` e `perdido` são **terminais**: só se chega neles por `/win` e `/lose`. */
export type TipoDeEtapa = "aberto" | "ganho" | "perdido";

export type Funil = {
  id: string;
  name: string;
  is_default: boolean;
  active: boolean;
  stage_count: number;
  open_opportunity_count: number;
};

export type EtapaDoFunil = {
  id: string;
  pipeline_id: string;
  name: string;
  position: number;
  probability: number;
  /** Tinge a **coluna** do kanban; o card fica neutro (docs/ui.md §9). */
  color: string;
  type: TipoDeEtapa;
  sla_days: number | null;
  auto_task_subject: string | null;
  auto_task_type: string | null;
  auto_task_due_days: number | null;
  auto_notify: boolean;
};

export type FunilComEtapas = Funil & { stages: EtapaDoFunil[] };

export type MotivoDePerda = {
  id: string;
  label: string;
  active: boolean;
  usage_count: number;
};

// ── Contato e lead ─────────────────────────────────────────────────────────

export type ContatoResumo = {
  id: string;
  name: string;
  email: string | null;
  phone_e164: string | null;
  city: string | null;
  state: string | null;
};

export type EstadoDoLead = "novo" | "em_atendimento" | "qualificado" | "convertido" | "descartado";

export type Lead = {
  id: string;
  contact_id: string;
  contact_name: string;
  contact_phone_e164: string | null;
  source: string;
  campaign_id: string | null;
  status: EstadoDoLead;
  score: number;
  interest_unit_type_id: string | null;
  interest_unit_type_name: string | null;
  desired_check_in: DataISO | null;
  /** Exclusivo, como toda data de saída no sistema. */
  desired_check_out: DataISO | null;
  owner_id: string | null;
  owner_name: string | null;
  converted_at: string | null;
  opportunity_id: string | null;
  created_at: string;
  updated_at: string;
};

/** Corpo do `POST /crm/leads/{id}/convert`. O que vier aqui sobrepõe o lead. */
export type PedidoDeConversao = {
  pipeline_id?: string;
  stage_id?: string;
  unit_type_id?: string | null;
  check_in?: DataISO | null;
  check_out?: DataISO | null;
  amount_cents?: number | null;
  expected_close?: DataISO | null;
  owner_id?: string | null;
};

export type ResultadoDeConversao = {
  lead: Lead;
  opportunity: Oportunidade;
  auto_task: Atividade | null;
};

// ── Oportunidade ───────────────────────────────────────────────────────────

export type EstadoDaOportunidade = "aberta" | "ganha" | "perdida";

export type DetalheDeEvento = {
  event_type: string | null;
  guests_expected: number | null;
  needs_catering: boolean;
  notes: string | null;
};

/**
 * **Não existe campo `title`**, de propósito: o card se identifica por contato,
 * produto e datas. Título livre viraria vinte grafias da mesma coisa.
 */
export type Oportunidade = {
  id: string;
  contact_id: string;
  contact_name: string;
  lead_id: string | null;
  pipeline_id: string;
  pipeline_name: string;
  stage_id: string;
  stage_name: string;
  stage_type: TipoDeEtapa;
  unit_type_id: string | null;
  unit_type_name: string | null;
  check_in: DataISO | null;
  check_out: DataISO | null;
  quote_id: string | null;
  reservation_id: string | null;
  reservation_code: string | null;
  amount_cents: number;
  probability: number;
  expected_close: DataISO | null;
  owner_id: string | null;
  owner_name: string | null;
  status: EstadoDaOportunidade;
  lost_reason_id: string | null;
  lost_reason_label: string | null;
  entered_stage_at: string;
  sla_days: number | null;
  sla_due_at: string | null;
  sla_breached: boolean;
  pending_task_count: number;
  event: DetalheDeEvento | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
};

/**
 * Corpo do `PATCH /crm/opportunities/{id}`.
 *
 * `stage_id`, `status`, `lost_reason_id`, `quote_id` e `reservation_id` estão
 * **fora** de propósito: cada um tem uma ação com efeito colateral obrigatório
 * (`/stage`, `/win`, `/lose`). Um `PATCH` que movesse a etapa não gravaria
 * `crm_stage_history`, e a conversão por etapa do BI passaria a mentir.
 */
export type OportunidadeAtualizar = {
  contact_id?: string;
  lead_id?: string | null;
  unit_type_id?: string | null;
  check_in?: DataISO | null;
  check_out?: DataISO | null;
  amount_cents?: number;
  probability?: number;
  expected_close?: DataISO | null;
  owner_id?: string | null;
  event?: DetalheDeEvento | null;
};

// ── Kanban ─────────────────────────────────────────────────────────────────

export type CardDaOportunidade = {
  id: string;
  contact_id: string;
  contact_name: string;
  unit_type_name: string | null;
  check_in: DataISO | null;
  check_out: DataISO | null;
  amount_cents: number;
  probability: number;
  expected_close: DataISO | null;
  owner_id: string | null;
  owner_name: string | null;
  status: EstadoDaOportunidade;
  entered_stage_at: string;
  sla_due_at: string | null;
  /** Derivado **no servidor**, com o relógio de `America/Fortaleza`. */
  sla_breached: boolean;
  pending_task_count: number;
  next_due_at: string | null;
  reservation_code: string | null;
};

export type ColunaDoKanban = {
  stage: EtapaDoFunil;
  /** Total de cards **na etapa**, não na página — a coluna é paginada. */
  count: number;
  amount_cents: number;
  has_more: boolean;
  cards: CardDaOportunidade[];
};

export type QuadroKanban = {
  pipeline: Funil;
  columns: ColunaDoKanban[];
  totals: { count: number; amount_cents: number };
};

// ── SLA, histórico, alertas ────────────────────────────────────────────────

/**
 * Tudo derivado **no servidor**, com o "hoje" de `America/Fortaleza`. A mesma
 * conta feita no navegador daria uma resposta por fuso de quem abriu a tela —
 * por isso `dias_restantes` nunca é recalculado aqui.
 */
export type FaixaDeSLA = {
  stage_id: string;
  stage_name: string;
  sla_days: number | null;
  entered_stage_at: string;
  due_at: string | null;
  /** Negativo quando estourou. `null` quando a etapa não tem SLA. */
  days_left: number | null;
  breached: boolean;
};

export type EventoDeEtapa = {
  id: string;
  from_stage_id: string | null;
  from_stage_name: string | null;
  to_stage_id: string;
  to_stage_name: string;
  user_id: string | null;
  user_name: string | null;
  reason: string | null;
  at: string;
  days_in_stage: number | null;
};

export type CodigoDeAlerta =
  | "sla_estourado"
  | "tarefa_vencida"
  | "parado_ha_n_dias"
  | "hold_expirando"
  | "saldo_a_vencer";

export type AlertaDaOportunidade = {
  code: CodigoDeAlerta;
  severity: "info" | "atencao" | "critico";
  message: string;
  due_at: string | null;
  entity_id: string | null;
};

export type EventoDaOportunidade = {
  id: string;
  /** Vocabulário **aberto**, montado na leitura: `created`, `stage_changed`,
   *  `activity_created`, `activity_completed`, `note_added`, `quote_issued`,
   *  `reservation_created`, `won`, `lost`… `chat_message` quando o chat existir. */
  type: string;
  at: string;
  actor_id: string | null;
  actor_name: string | null;
  /** A frase já montada pelo servidor — a tela não remonta texto de evento. */
  title: string;
  payload: Record<string, unknown> | null;
};

export type NotaDaOportunidade = {
  id: string;
  body: string;
  author_id: string | null;
  author_name: string | null;
  created_at: string;
};

/** Declarado e **vazio** nesta rodada: o módulo de anexos é de outra fase, e a
 *  tela nasce com o formato final para `documents` não virar opcional eterno. */
export type DocumentoDaOportunidade = {
  id: string;
  name: string;
  mime: string;
  size_bytes: number;
  url: string;
  uploaded_by: string | null;
  uploaded_by_name: string | null;
  created_at: string;
};

// ── Atividades ─────────────────────────────────────────────────────────────

export type TipoDeAtividade = "tarefa" | "ligacao" | "reuniao" | "email" | "whatsapp" | "nota";

export type EstadoDaAtividade = "pendente" | "concluida" | "cancelada";

export type Atividade = {
  id: string;
  type: TipoDeAtividade;
  subject: string;
  description: string | null;
  due_at: string | null;
  done_at: string | null;
  status: EstadoDaAtividade;
  /** Derivado no servidor: `pendente` com `due_at` no passado. */
  overdue: boolean;
  priority: "baixa" | "normal" | "alta";
  lead_id: string | null;
  opportunity_id: string | null;
  contact_id: string | null;
  contact_name: string | null;
  owner_id: string;
  owner_name: string | null;
  stage_id: string | null;
  auto: boolean;
  created_at: string;
  updated_at: string;
};

// ── Reserva vinculada ──────────────────────────────────────────────────────

/**
 * A reserva como a tela da oportunidade precisa dela.
 *
 * É uma **projeção** do schema `Reserva` do contrato, não uma segunda verdade:
 * o painel ainda não tem o módulo de reservas (`lib/api/reservas.ts` não
 * existe), e declarar as 30 chaves aqui seria fundar o catálogo de reservas
 * dentro do CRM. Quando o módulo chegar, este alias vira `Pick<Reserva, …>`.
 */
export type ReservaVinculada = {
  id: string;
  code: string;
  status: string;
  check_in: DataISO;
  check_out: DataISO;
  night_count: number;
  guests_count: number;
  total_cents: number;
  deposit_cents: number;
  balance_cents: number;
  hold_expires_at: string | null;
};

// ── A tela inteira numa chamada ────────────────────────────────────────────

export type OportunidadeCompleta = {
  opportunity: Oportunidade;
  contact: ContatoResumo;
  /** As etapas do funil **da oportunidade**, incluindo as que ela não alcançou. */
  stages: EtapaDoFunil[];
  sla: FaixaDeSLA;
  stage_history: EventoDeEtapa[];
  /** Tudo que **não** é nota. Pendentes primeiro, por prazo. */
  activities: Atividade[];
  notes: NotaDaOportunidade[];
  documents: DocumentoDaOportunidade[];
  alerts: AlertaDaOportunidade[];
  timeline: EventoDaOportunidade[];
  quote: Orcamento | null;
  reservation: ReservaVinculada | null;
};

// ── Corpos e resultados das ações nomeadas ─────────────────────────────────

export type PedidoDeMudancaDeEtapa = {
  stage_id: string;
  /** Guarda otimista: diferente da etapa atual devolve `409` com
   *  `details.current_stage_id`. É o que impede dois corretores arrastando o
   *  mesmo card de sobrescreverem um ao outro em silêncio. */
  from_stage_id?: string;
  reason?: string | null;
  probability?: number | null;
};

export type ResultadoDeMudancaDeEtapa = {
  opportunity: Oportunidade;
  /** `null` quando a etapa não tem tarefa **ou** quando já existia uma pendente
   *  da mesma etapa — a idempotência não cria a segunda. */
  auto_task: Atividade | null;
  sla: FaixaDeSLA;
};

export type PedidoDeGanho = { quote_id?: string; note?: string | null };

export type ResultadoDeGanho = {
  opportunity: Oportunidade;
  /** Criada como `hold`: ganhar é acordo, confirmar é dinheiro (spec §5). */
  reservation: ReservaVinculada;
  /** `false` quando a oportunidade já tinha a pré-reserva e `/win` só a vinculou. */
  reservation_created: boolean;
};

export type PedidoDePerda = { lost_reason_id: string; note?: string | null };

export type ResultadoDePerda = {
  opportunity: Oportunidade;
  /** Perder **não** cancela a pré-reserva: liberar a data tem política
   *  congelada e possível reembolso, e é decisão separada. Vem preenchida para
   *  a tela oferecer o cancelamento em seguida. */
  reservation: ReservaVinculada | null;
};
