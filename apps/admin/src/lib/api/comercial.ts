/**
 * Tipos da Fase 1 — inventário, tarifário, calendário comercial, política,
 * disponibilidade e orçamento.
 *
 * Espelho literal de `apps/api/openapi/openapi.yaml`, escrito à mão pelo mesmo
 * motivo que `types.ts`: o contrato é a fonte da verdade e um gerador entraria
 * no caminho sem acrescentar nada enquanto a superfície couber numa leitura.
 *
 * Ficam separados de `types.ts` (auth e RBAC) por assunto, não por acaso: quem
 * mexe em tarifa não deveria abrir o arquivo que descreve sessão.
 */

// ── Vocabulário ────────────────────────────────────────────────────────────

/** Um tipo por noite, resolvido por precedência (spec §3). */
export type TipoDeData = "normal" | "fds" | "feriado" | "alta" | "reveillon" | "carnaval";

/** Quantas unidades da composição uma venda ocupa. */
export type Consumo = "one_member" | "all_members";

export type TipoDePeriodo = "reveillon" | "carnaval" | "alta" | "evento";

/** Alçada do desconto, decidida pela política vigente — o motor devolve pronto. */
export type Alcada = "gestao" | "proprietario" | "negado";

// ── Inventário ─────────────────────────────────────────────────────────────

export type Propriedade = {
  id: string;
  name: string;
  slug: string;
  timezone: string;
  address: string | null;
  city: string | null;
  state: string | null;
  active: boolean;
  created_at: string;
  updated_at: string;
};

/** O que se **vende** (`unit_types`). Não confundir com `Unidade`. */
export type Produto = {
  id: string;
  code: string;
  name: string;
  /** Limite operacional declarado, não somado: a Completa acomoda 24 embora as
   *  unidades somem 40. É decisão dos proprietários, e o sistema não recalcula. */
  capacity: number;
  consumes: Consumo;
  /** Cobrada uma vez por estadia, e o desconto nunca incide sobre ela. */
  cleaning_fee_cents: number;
  description: string | null;
  sort_order: number;
  active: boolean;
  created_at: string;
  updated_at: string;
};

export type ProdutoEntrada = {
  code: string;
  name: string;
  capacity: number;
  consumes: Consumo;
  cleaning_fee_cents: number;
  description: string | null;
  sort_order: number;
  active: boolean;
};

/** O que se **ocupa** (`units`) — o que a constraint `stay_no_overlap` protege. */
export type Unidade = {
  id: string;
  code: string;
  name: string;
  floor: string | null;
  notes: string | null;
  sort_order: number;
  active: boolean;
  created_at: string;
  updated_at: string;
};

export type UnidadeEntrada = {
  code: string;
  name: string;
  floor: string | null;
  notes: string | null;
  sort_order: number;
  active: boolean;
};

/** Item de `unit_type_members`, sempre ordenado por `unit_code`. */
export type UnidadeDaComposicao = {
  unit_id: string;
  unit_code: string;
  unit_name: string;
  active: boolean;
};

// ── Tarifário ──────────────────────────────────────────────────────────────

export type TabelaDeTarifas = {
  id: string;
  name: string;
  valid_from: string;
  valid_to: string | null;
  active: boolean;
  created_at: string;
};

export type TabelaDeTarifasEntrada = {
  name: string;
  valid_from: string;
  valid_to: string | null;
  active: boolean;
};

/** Uma célula da grade produto × tipo de data. */
export type Tarifa = {
  id: string;
  rate_table_id: string;
  unit_type_id: string;
  unit_type_code: string;
  date_type: TipoDeData;
  amount_cents: number;
};

/** Célula no corpo de `POST /rates/bulk` — a tabela vem no nível de cima. */
export type TarifaDaGrade = {
  unit_type_id: string;
  date_type: TipoDeData;
  amount_cents: number;
};

/**
 * O que `POST /rates/bulk` fez, em números.
 *
 * `removed` é o campo que importa: é ele que mostra, para quem salvou, que a
 * gravação apagou célula — em vez de a remoção ser descoberta dias depois,
 * quando a venda daquele produto morre em `RATE_NOT_FOUND`.
 */
export type MetaDaGrade = {
  /** O escopo efetivamente aplicado, para conferir o que a API deduziu. */
  unit_type_ids: string[];
  created: number;
  updated: number;
  unchanged: number;
  /** **Só produto do escopo entra nesta conta.** */
  removed: number;
};

/** Resposta de `POST /rates/bulk`: a tabela inteira como ficou, e o que mudou. */
export type GradeGravada = {
  /** Inclusive os produtos que a chamada não tocou — é o estado a redesenhar. */
  tarifas: Tarifa[];
  meta: MetaDaGrade;
};

export type Feriado = {
  id: string;
  date: string;
  name: string;
  active: boolean;
};

export type FeriadoEntrada = { date: string; name: string; active: boolean };

/** Faixa do calendário comercial — **inclusiva nas duas pontas**. */
export type PeriodoEspecial = {
  id: string;
  name: string;
  kind: TipoDePeriodo;
  starts_on: string;
  ends_on: string;
  active: boolean;
};

export type PeriodoEspecialEntrada = {
  name: string;
  kind: TipoDePeriodo;
  starts_on: string;
  ends_on: string;
  active: boolean;
};

export type MinimoDeNoites = {
  id: string;
  rate_table_id: string;
  date_type: TipoDeData;
  nights: number;
};

export type MinimoDeNoitesEntrada = {
  rate_table_id: string;
  date_type: TipoDeData;
  nights: number;
};

// ── Políticas (versionadas) ────────────────────────────────────────────────

export type PoliticaComercial = {
  id: string;
  /** Só leitura: o servidor atribui `max(version) + 1` a cada `PUT`. */
  version: number;
  deposit_pct: number;
  balance_due_days: number;
  hold_hours: number;
  /** Até aqui a gestão fecha sozinha. */
  discount_auto_pct: number;
  /** Até aqui, com aprovação do proprietário. Acima, negado. */
  discount_approval_pct: number;
  event_deposit_cents: number;
  valid_from: string;
  created_at: string;
  /** Limite de extensões da pré-reserva. Acrescentados pela migration da Fase 1;
   *  a API pode ainda não devolvê-los, por isso opcionais. */
  hold_extension_hours?: number;
  hold_max_extensions?: number;
};

export type PoliticaComercialEntrada = {
  deposit_pct: number;
  balance_due_days: number;
  hold_hours: number;
  discount_auto_pct: number;
  discount_approval_pct: number;
  event_deposit_cents: number;
  valid_from: string;
};

/** `null` nos limites é "sem piso" / "sem teto". */
export type FaixaDeCancelamento = {
  days_before_min: number | null;
  days_before_max: number | null;
  refund_pct: number;
  label: string;
  sort_order: number;
};

export type PoliticaDeCancelamento = {
  id: string;
  version: number;
  name: string;
  valid_from: string;
  /** Ordenadas por `sort_order`, da mais generosa à mais restritiva. */
  tiers: FaixaDeCancelamento[];
};

export type PoliticaDeCancelamentoEntrada = {
  name: string;
  valid_from: string;
  tiers: FaixaDeCancelamento[];
};

// ── Disponibilidade e orçamento ────────────────────────────────────────────

/**
 * Por que a célula do mapa está cinza.
 *
 * Existe porque "0 livre" não diz o que fazer: `ocupado` é assunto do hóspede
 * ("escolha outra data"), enquanto `sem_tarifa` e `composicao_incompleta` são
 * configuração pela metade, e quem age é a gestão. Precedência declarada no
 * contrato: `composicao_incompleta` > `sem_tarifa` > `unidade_inativa` >
 * `ocupado`.
 */
export type MotivoDeIndisponibilidade =
  | "ocupado"
  | "sem_tarifa"
  | "composicao_incompleta"
  | "unidade_inativa";

export type DiaDoProduto = {
  date: string;
  /**
   * Unidades livres **e vendáveis**: ativa, sem bloqueio que conte e com tarifa
   * na tabela vigente. Noite sem tarifa vem como `0`, não como oferta — a tela
   * nunca deve oferecer o que `POST /quotes` recusaria com `RATE_NOT_FOUND`.
   *
   * Para `all_members` é 0 ou 1: a Completa só é livre quando todas estão.
   */
  available: number;
  /** `null` quando `available > 0`. */
  unavailable_reason: MotivoDeIndisponibilidade | null;
  date_type: TipoDeData;
  /** `null` quando a tabela vigente não tem a célula — e aí `available` é 0. */
  price_cents: number | null;
  min_nights: number;
};

export type DisponibilidadeDoProduto = {
  unit_type_id: string;
  unit_type_code: string;
  name: string;
  consumes: Consumo;
  /** Tamanho **declarado** da composição — não encolhe porque alguém desativou
   *  uma unidade. */
  total_units: number;
  /** Quantas dessas estão ativas. Em `all_members`, `active_units <
   *  total_units` é produto que não pode ser entregue: 7 de 8 não é a casa
   *  inteira, e a venda recusa com `COMPOSITION_INCOMPLETE`. */
  active_units: number;
  days: DiaDoProduto[];
};

export type PedidoDeOrcamento = {
  unit_type_id: string;
  check_in: string;
  /** Exclusivo: a noite do check-out não é cobrada. */
  check_out: string;
  /** **Era `guests` até 26/08/2026.** O mesmo dado tinha dois nomes — `guests`
   *  na criação e `guests_count` na edição e em toda resposta —, e o painel
   *  acertava um e errava o outro sem receber erro nenhum. Um dado, um nome. */
  guests_count: number;
  discount_pct: number;
  is_event: boolean;
  rate_table_id?: string;
  policy_version?: number;
};

export type LinhaDoOrcamento = {
  date_type: TipoDeData;
  label: string;
  nights: number;
  unit_price_cents: number;
  subtotal_cents: number;
};

export type NoiteDoOrcamento = {
  date: string;
  date_type: TipoDeData;
  label: string;
  price_cents: number;
};

/**
 * Saída de `booking.Build`, aberta de propósito: a gestão precisa explicar o
 * número ao hóspede, e **o painel nunca recalcula nada** — todo valor exibido
 * vem daqui.
 */
export type Orcamento = {
  night_count: number;
  subtotal_cents: number;
  discount_pct: number;
  /** Incide só sobre as diárias — nunca sobre limpeza nem sobre caução. */
  discount_cents: number;
  cleaning_cents: number;
  event_deposit_cents: number;
  total_cents: number;
  deposit_cents: number;
  balance_cents: number;
  avg_nightly_cents: number;
  /** O **maior** mínimo entre as noites da estadia. */
  min_nights: number;
  discount_authority: Alcada;
  policy_version: number;
  rate_table_id: string;
  lines: LinhaDoOrcamento[];
  nights: NoiteDoOrcamento[];
};
