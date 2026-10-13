import type { LinhaDoOrcamento, TipoDeData } from "@/lib/api/comercial";
import type { TipoDeDocumento } from "@/lib/contatos/tipos";

/**
 * Tipos do módulo de reservas — espelho literal de `apps/api/openapi/openapi.yaml`
 * (tag `Reservas`), escrito à mão pelo mesmo motivo de `lib/api/types.ts` e
 * `lib/crm/tipos.ts`: o contrato é a fonte da verdade e um gerador entraria no
 * caminho sem acrescentar nada enquanto a superfície couber numa leitura.
 *
 * O que **não** está aqui, de propósito: nenhum campo derivado. `balance_cents`,
 * `night_count` e `total_cents` vêm do servidor congelados na venda, e o painel
 * não recalcula nada — mostrar o preço de hoje para uma venda de ontem é
 * exatamente o que o snapshot de `reservation_nights` existe para impedir.
 */

/**
 * O estado **comercial** da venda.
 *
 * Não confundir com `Bloqueio.status`, que é o estado da linha de calendário: a
 * mesma estadia cumprida é `checked_out` aqui e `completed` lá, e o contrato
 * marca a diferença como deliberada. Quem bloqueia a data é o bloco, não este
 * campo — `hold`, `confirmed` e `checked_in` seguram; o resto não.
 */
export type EstadoDaReserva =
  | "quote"
  | "hold"
  | "confirmed"
  | "checked_in"
  | "checked_out"
  | "closed"
  | "cancelled"
  | "expired"
  | "no_show";

/** Linha de `reservation_units` — a unidade física que a venda ocupa. */
export type UnidadeAlocada = {
  unit_id: string;
  unit_code: string;
  unit_name: string;
  /** A linha de `stay_blocks` que segura a data desta unidade. */
  stay_block_id: string | null;
  /** Escolha travada pela gestão: sai da realocação automática. */
  locked: boolean;
};

export type Reserva = {
  id: string;
  /** Legível, sequencial por ano e **imutável** — é por ele que a gestão fala da
   *  reserva ao telefone, e por isso ele é a primeira coluna da lista. */
  code: string;
  status: EstadoDaReserva;
  unit_type_id: string;
  unit_type_name: string;
  contact_id: string;
  contact_name: string;
  /** Telefone do cliente. Só a lista (`GET /reservations`) preenche; nas demais respostas vem `null`. */
  contact_phone_e164: string | null;
  broker_id: string | null;
  source: string;
  check_in: string;
  /** Exclusivo: a noite do check-out não é cobrada e a data já fica livre. */
  check_out: string;
  night_count: number;
  guests_count: number;
  is_event: boolean;
  event_type: string | null;
  subtotal_cents: number;
  discount_pct: number;
  discount_cents: number;
  cleaning_cents: number;
  event_deposit_cents: number;
  total_cents: number;
  deposit_cents: number;
  balance_cents: number;
  /** **Snapshot**: a tabela de tarifas usada no cálculo. */
  rate_table_id: string | null;
  /** **Snapshot** da política comercial vigente na criação. */
  policy_version: number | null;
  /** **Snapshot** da política de cancelamento — a versão que `/cancel` aplica. */
  cancellation_policy_id: string | null;
  hold_expires_at: string | null;
  confirmed_at: string | null;
  cancelled_at: string | null;
  cancel_reason: string | null;
  rebooked_from_id: string | null;
  notes: string | null;
  created_at: string;
  updated_at: string;
  units: UnidadeAlocada[];
};

/** Linha de `reservation_nights`: o preço que **foi** aplicado, não o de hoje. */
export type NoiteDaReserva = {
  night: string;
  date_type: TipoDeData;
  price_cents: number;
};

/**
 * Rooming list (F2-22). Não confundir com `guests_count`, que é quantos foram
 * vendidos.
 *
 * A lista guarda o `contact_id`; o resto é lido da ficha a cada leitura —
 * corrigir a ficha corrige a lista. Documento, telefone e e-mail chegam
 * **cheios** (é a tela de trabalho da recepção no check-in), e cada leitura
 * grava uma linha de `pii_access_log` por hóspede. Por isso nada daqui vai
 * para `localStorage`, para a URL nem para o console. Ficha anonimizada chega
 * como "Contato anonimizado #…", sem documento nem canal.
 */
export type HospedeDaReserva = {
  contact_id: string;
  name: string;
  doc_type: TipoDeDocumento | null;
  /** Cheio — é com ele que a recepção confere quem entra. */
  doc_number: string | null;
  phone_e164: string | null;
  email: string | null;
  /** O titular — sempre o `contact_id` da reserva, um só, garantido pelo
   *  banco. Não sai da lista: troca-se pela edição da reserva. */
  is_lead_guest: boolean;
};

/** Corpo do `POST /reservations/{id}/guests`. Só o `contact_id`: a ficha nasce
 *  antes, em `POST /contacts`, e o titular não entra por aqui. */
export type HospedeAcrescentar = {
  contact_id: string;
};

/**
 * Corpo do `PATCH /reservations/{id}` que o painel manda para abrir lugar na
 * lista: **só** `guests_count`. É o caminho que o contrato dá quando chega mais
 * gente — aumenta-se o número de hóspedes (o servidor confere contra a
 * capacidade) e depois se acrescenta o nome. O DTO do Go é o
 * `ReservaAtualizar` inteiro; chave ausente não muda nada.
 */
export type NumeroDeHospedes = {
  guests_count: number;
};

/** `details` do `422 GUEST_LIST_FULL` — os três números que a tela usa para
 *  dizer se ainda dá para aumentar o número de hóspedes. */
export type ListaCheia = {
  /** O número **vendido** — o teto da lista. */
  guests_count: number;
  /** Quantos nomes a lista já tem. */
  listed: number;
  /** A capacidade declarada do produto: até onde `guests_count` pode subir. */
  capacity: number;
};

/** Linha de `reservation_events` — append-only, nada é editado nem apagado. */
export type EventoDaReserva = {
  id: string;
  /** Vocabulário **aberto**, gravado pelo módulo. Nada de `switch` exaustivo:
   *  tipo desconhecido tem que aparecer na linha do tempo, não sumir dela. */
  type: string;
  payload: Record<string, unknown> | null;
  /** `null` quando o autor é o job de expiração, não uma pessoa. */
  actor_id: string | null;
  at: string;
};

/** Saída de `booking.CancellationPolicy.Simulate` — o que a tela mostra ANTES. */
export type ResultadoDeCancelamento = {
  /** Texto da faixa aplicada, vindo da política congelada na reserva. */
  label: string;
  refund_cents: number;
  retained_cents: number;
  /** Base do cálculo: o sinal efetivamente recebido. */
  deposit_paid_cents: number;
  /** Antecedência em dias até o `check_in`. Negativa se o check-in já passou. */
  days_before: number;
  policy_version: number;
  /**
   * Dinheiro do hóspede que este cancelamento **não liquida** — crédito de
   * remarcação para estadia mais barata. Zero é o caso normal, e a invariante
   * que fecha é `refund + retained + credit == o que o hóspede pagou`.
   */
  credit_cents: number;
  /** `true` quando nada foi executado. */
  dry_run: boolean;
  /** Onde a reserva termina: `cancelled` ou `no_show`. */
  status: EstadoDaReserva;
};

export type ReservaCompleta = {
  reservation: Reserva;
  nights: NoiteDaReserva[];
  /** As noites agrupadas por tipo de tarifa, como no orçamento. */
  lines: LinhaDoOrcamento[];
  units: UnidadeAlocada[];
  /** As **pessoas** — titular primeiro, acompanhantes por nome. Pode ter menos
   *  nomes que `reservation.guests_count`, de propósito. */
  guests: HospedeDaReserva[];
  /**
   * A lista aceita acrescentar, retirar e trocar o titular **agora**
   * (`guests.Editable` no domínio). A tela desenha os botões a partir daqui, sem
   * uma segunda cópia da regra. A permissão é à parte (`reservations:editar`).
   */
  guests_editable: boolean;
  /** Do mais recente para o mais antigo. */
  timeline: EventoDaReserva[];
  /** O mesmo cálculo do `?dry_run=1`, já pronto. `null` quando não é cancelável. */
  cancellation_preview: ResultadoDeCancelamento | null;
};

export type ConfirmacaoDeReserva = {
  /** Ausente assume `deposit_cents`. Validado em `[1, total_cents]`. */
  deposit_paid_cents?: number;
  method?: MeioDePagamento;
  paid_at?: string;
  external_ref?: string | null;
  note?: string | null;
};

export type MeioDePagamento = "pix" | "cartao" | "transferencia" | "dinheiro";

export type PedidoDeCancelamento = {
  /** Texto livre. `no_show` leva a reserva ao estado `no_show`, com retenção
   *  integral; `remarcacao` é o que a própria API grava ao remarcar. */
  reason?: string;
};

export type PedidoDeRemarcacao = {
  check_in: string;
  check_out: string;
  unit_type_id?: string;
  guests_count?: number;
  discount_pct?: number;
  reason?: string | null;
};

export type MetaDaRemarcacao = {
  previous_reservation_id: string;
  previous_code: string;
  previous_total_cents: number;
  /** `total novo − total antigo`. Positivo é o que falta cobrar. */
  difference_cents: number;
  /** Sinal já pago que não coube na reserva nova. Zero na maioria dos casos. */
  credit_cents: number;
};

export type RegistroDeEstadia = {
  at?: string;
  note?: string | null;
};

export type PedidoDeRealocacao = {
  /** Pode ser omitido quando a reserva ocupa uma só unidade. */
  from_unit_id?: string | null;
  to_unit_id: string;
  locked?: boolean;
  reason?: string | null;
};

export type PedidoDeExtensaoDeHold = {
  /** Ausente usa `hold_hours` da política congelada. Conta a partir de agora. */
  hours?: number;
  reason?: string | null;
};

export type ResultadoDeExtensaoDeHold = {
  id: string;
  hold_expires_at: string;
  extensions_count: number;
  max_extensions: number;
};
