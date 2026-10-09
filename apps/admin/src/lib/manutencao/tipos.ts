import type { DesfechoDeAvaria, TipoDeAvaria } from "@/lib/bens/tipos";

/**
 * Tipos das ordens de manutenção — espelho da tag `Manutenção` de
 * `apps/api/openapi/openapi.yaml` (schemas `OrdemDeManutencao`,
 * `OrdemDeManutencaoCriar|Substituir|Atualizar`, `ConclusaoDaOrdem`,
 * `PeriodoDoBloqueio`, `BloqueioDaOrdem`, `AvariaDaOrdem` e os enums).
 *
 * Escritos à mão, como os outros espelhos do painel. Campo fora de `required`
 * no contrato fica opcional aqui: a tela tem de ter um plano para a ausência.
 *
 * O recurso RBAC é `maintenance` — não `inventory.goods` nem `calendar`. Quem
 * pode criar ou editar a ordem bloqueia a unidade dela sem precisar de
 * `calendar:*`; o formulário, porém, escolhe cômodo e bem pelas rotas de `Bens`,
 * que pedem `inventory.goods:ver`.
 */

// ── Vocabulário fechado (os `CHECK` de `maintenance_orders`) ────────────────

export const STATUS_DA_ORDEM = ["aberta", "em_andamento", "concluida", "cancelada"] as const;
export type StatusDaOrdem = (typeof STATUS_DA_ORDEM)[number];

/** Ordem do enum do contrato. A lista de trabalho ordena `urgente → baixa`, e
 *  quem ordena é a API (`sort=urgencia`) — esta constante não ordena nada. */
export const PRIORIDADES_DA_ORDEM = ["baixa", "normal", "alta", "urgente"] as const;
export type PrioridadeDaOrdem = (typeof PRIORIDADES_DA_ORDEM)[number];

/** `start` é `POST /{id}/start`, `complete` é `POST /{id}/complete` e `cancel`
 *  é `DELETE /{id}`. */
export const ACOES_DA_ORDEM = ["start", "complete", "cancel"] as const;
export type AcaoDaOrdem = (typeof ACOES_DA_ORDEM)[number];

/** Como o bloqueio está **hoje** no fuso da propriedade — derivado pela API,
 *  nunca pelo painel. */
export const FASES_DO_BLOQUEIO = ["agendado", "em_curso", "encerrado", "liberado"] as const;
export type FaseDoBloqueio = (typeof FASES_DO_BLOQUEIO)[number];

/** O que `PUT`/`PATCH` e a rota do bloqueio aceitam no estado atual. */
export const EDICOES_DA_ORDEM = ["tudo", "so_custo", "nada"] as const;
export type EdicaoDaOrdem = (typeof EDICOES_DA_ORDEM)[number];

export const ORDENACOES_DA_LISTA = ["urgencia", "opened_at", "-opened_at", "-closed_at"] as const;
export type OrdenacaoDaLista = (typeof ORDENACOES_DA_LISTA)[number];

// ── Leitura ─────────────────────────────────────────────────────────────────

/** Half-open, como toda estadia: `[10/11, 15/11)` bloqueia as noites de 10 a
 *  14 e deixa o dia 15 livre para check-in. */
export type PeriodoDoBloqueio = {
  from: string;
  /** Exclusivo. */
  to: string;
};

/** A linha de `stay_blocks` da ordem (`source = 'maintenance'`). */
export type BloqueioDaOrdem = {
  id: string;
  from: string;
  /** Exclusivo. */
  to: string;
  nights: number;
  status: "confirmed" | "cancelled";
  phase: FaseDoBloqueio;
};

/** Resumo da avaria que originou a ordem. */
export type AvariaDaOrdem = {
  id: string;
  kind: TipoDeAvaria;
  qty: number;
  note?: string | null;
  /** `null` = ainda pendente. */
  resolution?: DesfechoDeAvaria | null;
  reported_at?: string;
};

export type OrdemDeManutencao = {
  id: string;
  unit_id: string;
  unit_code: string;
  unit_name: string;
  room_id?: string | null;
  room_name?: string | null;
  item_id?: string | null;
  item_name?: string | null;
  issue_id?: string | null;
  issue?: AvariaDaOrdem | null;
  title: string;
  description?: string | null;
  priority: PrioridadeDaOrdem;
  status: StatusDaOrdem;
  /** Centavos. `null` = ainda não lançado — zero é recusado pelo contrato. */
  cost_cents?: number | null;
  /** `null` = a ordem nunca bloqueou o calendário. */
  block?: BloqueioDaOrdem | null;
  opened_at: string;
  opened_by?: string | null;
  /** Nome do USUÁRIO do sistema — não é contato, não se mascara. */
  opened_by_name?: string | null;
  started_at?: string | null;
  closed_at?: string | null;
  closed_by?: string | null;
  closed_by_name?: string | null;
  updated_at: string;
  /** As transições que a ordem aceita agora. **Os botões saem daqui** — o
   *  painel não guarda uma segunda cópia da máquina de estados. */
  allowed_actions: AcaoDaOrdem[];
  editable: EdicaoDaOrdem;
};

// ── Escrita (corpos — `additionalProperties: false` no contrato) ────────────

/**
 * `OrdemDeManutencaoCriar`. `room_id`, `item_id` e `issue_id` só vão no corpo
 * quando preenchidos: com avaria, omitidos vêm dela, e informados têm de ser
 * iguais. `block` ausente = sem bloqueio.
 */
export type OrdemCriarEntrada = {
  unit_id: string;
  room_id?: string;
  item_id?: string;
  issue_id?: string;
  title: string;
  description: string | null;
  priority: PrioridadeDaOrdem;
  cost_cents: number | null;
  block?: PeriodoDoBloqueio;
};

/**
 * `OrdemDeManutencaoSubstituir` — corpo do `PUT`. Sem `unit_id`, `issue_id`,
 * `status`, `block` e instantes (mandá-los é `422`). Com avaria ligada,
 * `room_id` e `item_id` **não vão**: ficam os dela; `null` seria `422`.
 */
export type OrdemSubstituirEntrada = {
  room_id?: string | null;
  item_id?: string | null;
  title: string;
  description: string | null;
  priority: PrioridadeDaOrdem;
  cost_cents: number | null;
};

/** `PATCH {cost_cents}` — o único campo que a ordem concluída ainda aceita.
 *  `null` apaga o custo, também na concluída (está no contrato). */
export type CustoDaOrdemEntrada = {
  cost_cents: number | null;
};

/** `ConclusaoDaOrdem` — corpo opcional; ausente não mexe no custo. **Não é
 *  anulável**: `{"cost_cents": null}` é `422`. Para não mexer no custo, o
 *  painel manda sem corpo (`custoParaConcluir`). */
export type ConclusaoEntrada = {
  cost_cents?: number;
};

// ── Apoio do formulário (rotas de `Bens`) ───────────────────────────────────

/** O que o formulário precisa de uma unidade para escolher cômodo e bem. */
export type OpcoesDaUnidade = {
  ambientes: { id: string; name: string; itens: { id: string; name: string }[] }[];
};

export type BemDoCatalogo = { id: string; name: string };
