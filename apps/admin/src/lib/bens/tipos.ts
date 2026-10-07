/**
 * Tipos do inventário de bens por ambiente — espelho da tag `Bens` de
 * `apps/api/openapi/openapi.yaml` (schemas `Ambiente`, `Bem`, `Colocacao`,
 * `Conferencia`, `Avaria` e companhia).
 *
 * Escritos à mão, como os outros espelhos do painel. Campo que o contrato marca
 * como opcional (fora de `required`) fica opcional aqui também: a tela não pode
 * contar com ele, e o tipo é o que a obriga a ter um plano para a ausência.
 *
 * O recurso RBAC destas rotas é `inventory.goods`, **não** `inventory` — aquele é
 * o cadastro comercial (`/units`, `/unit-types`). Ver docs/db.md §11.
 */

// ── Vocabulário fechado (os `CHECK` de 20261007170000) ──────────────────────

export const TIPOS_DE_AMBIENTE = [
  "quarto",
  "banheiro",
  "cozinha",
  "sala",
  "area_externa",
  "lavanderia",
  "varanda",
  "outro",
] as const;
export type TipoDeAmbiente = (typeof TIPOS_DE_AMBIENTE)[number];

export const CATEGORIAS_DE_BEM = [
  "louca",
  "talher",
  "copo",
  "cama",
  "banho",
  "mobilia",
  "eletro",
  "utensilio",
  "decoracao",
  "outro",
] as const;
export type CategoriaDeBem = (typeof CATEGORIAS_DE_BEM)[number];

export const UNIDADES_DE_MEDIDA = ["un", "par", "jogo", "kg", "l", "m"] as const;
export type UnidadeDeMedida = (typeof UNIDADES_DE_MEDIDA)[number];

export const STATUS_DE_CONFERENCIA = ["aberta", "fechada", "cancelada"] as const;
export type StatusDeConferencia = (typeof STATUS_DE_CONFERENCIA)[number];

export const TIPOS_DE_AVARIA = ["quebrado", "faltando", "avariado", "outro"] as const;
export type TipoDeAvaria = (typeof TIPOS_DE_AVARIA)[number];

export const DESFECHOS_DE_AVARIA = ["reposto", "consertado", "cobrado", "perda_aceita", "descartado"] as const;
export type DesfechoDeAvaria = (typeof DESFECHOS_DE_AVARIA)[number];

// ── Leitura ─────────────────────────────────────────────────────────────────

/**
 * `GET /inventory/units` — a unidade vista pelo inventário de bens: só
 * identificação e os números do inventário dela. Nada de tarifa, capacidade ou
 * composição, que são do cadastro comercial (`inventory`).
 */
export type UnidadeDoInventario = {
  id: string;
  code: string;
  name: string;
  active: boolean;
  /** Ambientes ativos. Zero é a unidade que ainda precisa da planta. */
  rooms: number;
  /** Bens distintos colocados em ambientes ativos. */
  items: number;
  /** A conferência aberta, se houver — o seletor leva direto a ela. */
  open_count_id?: string | null;
  last_closed_at?: string | null;
};

/** O rótulo de uma unidade, quando a tela só precisa dizer qual é. */
export type RotuloDeUnidade = Pick<UnidadeDoInventario, "id" | "code" | "name">;

export type Ambiente = {
  id: string;
  unit_id: string;
  unit_code?: string;
  unit_name?: string;
  name: string;
  kind: TipoDeAmbiente;
  /** Ordem de caminhada pela casa. A API desempata por `name` e `id`. */
  sort_order: number;
  active: boolean;
  items_count?: number;
  expected_qty_total?: number;
  open_issues_count?: number;
  created_at?: string;
  updated_at?: string;
};

/** A foto. `url` e `thumb_url` são entregas **autenticadas** da API — o painel
 *  nunca as põe direto num `<img>`: passa pelo intermediário
 *  `/api/inventario/midia/{id}` (ver `lib/bens/midia.ts`). */
export type MidiaDeBem = {
  id: string;
  mime: "image/jpeg" | "image/png" | "image/webp";
  bytes: number;
  width?: number | null;
  height?: number | null;
  original_name?: string;
  url: string;
  thumb_url: string;
  created_at?: string;
};

export type FotoDoBem = { media: MidiaDeBem; sort_order: number };

export type Bem = {
  id: string;
  name: string;
  description?: string | null;
  category: CategoriaDeBem;
  unit_measure: UnidadeDeMedida;
  /** Centavos. `null` = ainda não cotado — diferente de "custa zero". */
  replacement_cost_cents?: number | null;
  active: boolean;
  source_ref?: string | null;
  cover?: MidiaDeBem | null;
  photos_count?: number;
  placements_count?: number;
  expected_qty_total?: number;
  open_issues_count?: number;
  created_at?: string;
  updated_at?: string;
};

export type BemCompleto = Bem & {
  photos?: FotoDoBem[];
  placements?: Colocacao[];
};

export type Colocacao = {
  /** Chave natural `room_id_item_id` (`ChaveDeColocacao`). */
  id: string;
  room_id: string;
  room_name?: string;
  room_kind?: TipoDeAmbiente;
  unit_id?: string;
  unit_code?: string;
  item_id: string;
  item_name?: string;
  item_category?: CategoriaDeBem;
  item_unit_measure?: UnidadeDeMedida;
  cover?: MidiaDeBem | null;
  /** O padrão da casa. Zero é legítimo: "este cômodo não tem, de propósito". */
  expected_qty: number;
  note?: string | null;
  created_at?: string;
  updated_at?: string;
};

export type AmbienteDoInventario = Ambiente & { items: Colocacao[] };

export type InventarioDaUnidade = {
  unit: { id: string; code: string; name: string };
  rooms: AmbienteDoInventario[];
  totals: {
    rooms: number;
    items: number;
    expected_qty: number;
    open_issues: number;
    replacement_cost_cents?: number | null;
    uncosted_items?: number;
  };
  open_count?: Conferencia | null;
  last_closed_count?: Conferencia | null;
};

export type ResultadoDaCopia = {
  dry_run: boolean;
  source?: { unit_id?: string; code?: string };
  rooms_created: { name?: string; kind?: TipoDeAmbiente }[];
  placements_created: { room_name?: string; item_id?: string; item_name?: string; expected_qty?: number }[];
  kept: { room_name?: string; item_name?: string; source_qty?: number; current_qty?: number }[];
};

export type ProgressoDaConferencia = {
  lines: number;
  /** Inclui as contadas em zero. */
  counted: number;
  /** `counted_qty` nula — ainda não contadas. Diferente de contadas em zero. */
  pending: number;
  diverging: number;
  missing_qty?: number;
  surplus_qty?: number;
};

export type Conferencia = {
  id: string;
  unit_id: string;
  unit_code?: string;
  unit_name?: string;
  status: StatusDeConferencia;
  note?: string | null;
  opened_by?: string | null;
  opened_by_name?: string | null;
  opened_at: string;
  closed_by?: string | null;
  closed_by_name?: string | null;
  closed_at?: string | null;
  progress: ProgressoDaConferencia;
  updated_at?: string;
};

export type LinhaDeConferencia = {
  id: string;
  count_id: string;
  room_id: string;
  room_name?: string;
  room_kind?: TipoDeAmbiente;
  item_id: string;
  item_name?: string;
  item_description?: string | null;
  item_category?: CategoriaDeBem;
  item_unit_measure?: UnidadeDeMedida;
  cover?: MidiaDeBem | null;
  /** Congelada na abertura — não muda se o padrão da casa mudar depois. */
  expected_qty: number;
  /** `null` = ainda não contado. `0` = contei e não achei nenhum. */
  counted_qty: number | null;
  diff?: number | null;
  note?: string | null;
  /** Custo de reposição **congelado no fechamento**. `null` enquanto aberta,
   *  na cancelada, e quando o bem não tinha custo cotado ao fechar. */
  replacement_cost_cents?: number | null;
  counted_by?: string | null;
  counted_by_name?: string | null;
  counted_at?: string | null;
};

export type AmbienteDaConferencia = {
  room_id: string;
  room_name: string;
  room_kind?: TipoDeAmbiente;
  progress?: ProgressoDaConferencia;
  lines: LinhaDeConferencia[];
};

export type ConferenciaCompleta = Conferencia & {
  rooms: AmbienteDaConferencia[];
  /** Só em conferência `fechada`: o que o fechamento apurou, com o custo
   *  congelado. `null` em aberta e cancelada. */
  result?: ApuracaoDaConferencia | null;
};

export type LinhaContada = { line: LinhaDeConferencia; progress: ProgressoDaConferencia };

/** `DivergenciaDeConferencia` — uma linha da apuração. */
export type DivergenciaDeConferencia = {
  room_id: string;
  room_name: string;
  item_id: string;
  item_name: string;
  expected_qty: number;
  counted_qty: number;
  /** Negativo é falta; positivo, sobra. */
  diff: number;
  /** O custo congelado no fechamento — não o do catálogo de hoje. */
  replacement_cost_cents?: number | null;
  /** Falta × custo congelado, em centavos — calculado no servidor. `null` sem
   *  custo cotado no fechamento, e na sobra. Nunca zero. */
  loss_cents?: number | null;
  issue_id?: string | null;
};

/** `ApuracaoDaConferencia` — o `result` do `GET` de uma conferência fechada. */
export type ApuracaoDaConferencia = {
  divergences: DivergenciaDeConferencia[];
  issues_created: number;
};

export type ResultadoDoFechamento = ApuracaoDaConferencia & { count: Conferencia };

export type Avaria = {
  id: string;
  room_id: string;
  room_name?: string;
  unit_id?: string;
  unit_code?: string;
  item_id: string;
  item_name?: string;
  item_category?: CategoriaDeBem;
  cover?: MidiaDeBem | null;
  kind: TipoDeAvaria;
  qty: number;
  note?: string | null;
  replacement_cost_cents?: number | null;
  /** `qty × replacement_cost_cents`, calculado no servidor. */
  total_cost_cents?: number | null;
  reservation_id?: string | null;
  reservation_code?: string | null;
  count_id?: string | null;
  /** `null` = pendência **aberta**. */
  resolution?: DesfechoDeAvaria | null;
  reported_by?: string | null;
  reported_by_name?: string | null;
  reported_at: string;
  resolved_by?: string | null;
  resolved_by_name?: string | null;
  resolved_at?: string | null;
  updated_at?: string;
};

// ── Escrita (corpos — `additionalProperties: false` no contrato) ────────────

/** `BemCriar` — corpo do `POST` e do `PUT`. Sem `source_ref`: é do importador. */
export type BemEntrada = {
  name: string;
  description: string | null;
  category: CategoriaDeBem;
  unit_measure: UnidadeDeMedida;
  replacement_cost_cents: number | null;
  active: boolean;
};

export type AmbienteCriarEntrada = {
  unit_id: string;
  name: string;
  kind: TipoDeAmbiente;
  sort_order: number;
  active: boolean;
};

/** `AmbienteSubstituir` — sem `unit_id`: cômodo não muda de unidade.
 *  Escrito por extenso (e não `Omit<>`) para `corpo-de-escrita.test.ts` ler. */
export type AmbienteSubstituirEntrada = {
  name: string;
  kind: TipoDeAmbiente;
  sort_order: number;
  active: boolean;
};

export type ColocacaoCriarEntrada = {
  room_id: string;
  item_id: string;
  expected_qty: number;
  note: string | null;
};

/** `ColocacaoSubstituir` — sem `room_id`/`item_id`: são a identidade da linha. */
export type ColocacaoSubstituirEntrada = {
  expected_qty: number;
  note: string | null;
};

/**
 * `AvariaCriar`. A reserva vai pelo **código** (o que a governanta tem na mão),
 * e nunca junto com `reservation_id` — mandar os dois é `422`. Por isso o
 * painel não declara `reservation_id` aqui, e `reservation_code` só vai no
 * corpo quando foi preenchido.
 */
export type AvariaCriarEntrada = {
  room_id: string;
  item_id: string;
  kind: TipoDeAvaria;
  qty: number;
  note: string | null;
  reservation_code?: string;
  count_id: string | null;
};
