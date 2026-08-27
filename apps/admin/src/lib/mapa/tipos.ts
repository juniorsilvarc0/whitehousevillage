import type { Consumo, TipoDeData } from "@/lib/api/comercial";
import type { DataISO } from "@/lib/datas";

/**
 * O vocabulário do mapa de ocupação.
 *
 * Espelho de `LinhaDoMapa` em `apps/api/openapi/openapi.yaml`, mais os tipos de
 * **apresentação** que só existem deste lado. Fica aqui, e não em
 * `lib/api/comercial.ts`, porque aquele arquivo é de outro dono (ownership de
 * pasta, `docs/agents.md` §2) — e porque metade do que o mapa desenha não é
 * campo de resposta nenhum: é derivação.
 *
 * ## O contrato tem TRÊS vocabulários de estado, e confundi-los é o bug clássico
 *
 * 1. `EstadoDaReserva` — a venda (`hold`, `confirmed`, `checked_out`…).
 * 2. `Bloqueio.status` — a linha de calendário (`hold`, `confirmed`,
 *    `completed`, `cancelled`, `expired`).
 * 3. **`StatusDaCelula`** — o que este arquivo declara, e o único que a tela
 *    conhece. É o `LinhaDoMapa.days[].status` do contrato: `livre` é ausência de
 *    linha, `checked_out` é o nome de tela do bloco `completed`, e
 *    `maintenance`/`owner_hold`/`ota` vêm do `source` do bloco, não do `status`.
 *
 * O mapa exibe `hold, confirmed, completed` — predicado diferente do da venda,
 * que é só `hold, confirmed`. Estadia cumprida continua desenhada; ocupação
 * desfeita, não.
 */
export type StatusDaCelula =
  | "livre"
  | "hold"
  | "confirmed"
  | "checked_out"
  | "maintenance"
  | "owner_hold"
  | "ota";

export type CelulaDoMapa = {
  date: DataISO;
  status: StatusDaCelula;
  date_type: TipoDeData;
  stay_block_id: string | null;
  reservation_id: string | null;
  reservation_code: string | null;
  guest_name: string | null;
};

/** Item de `GET /availability/units` — uma unidade física e seus dias. */
export type LinhaDoMapa = {
  unit_id: string;
  unit_code: string;
  unit_name: string;
  days: CelulaDoMapa[];
};

/**
 * Estado exclusivo da linha sintética da White House Completa: parte da casa
 * está ocupada, e por isso a casa inteira **não é vendável** — mas nenhuma
 * reserva da Completa existe para desenhar.
 *
 * Não é um status do contrato e nunca chega da API: é a resposta derivada à
 * única pergunta que a linha sintética existe para responder ("dá para vender a
 * casa inteira neste dia?"). Sem ele a linha teria de mentir entre `livre`
 * (falso: a venda seria recusada pela constraint) e `confirmed` (falso: não há
 * reserva da Completa).
 */
export type StatusDaCelulaSintetica = StatusDaCelula | "parcial";

export type CelulaSintetica = Omit<CelulaDoMapa, "status"> & {
  status: StatusDaCelulaSintetica;
  /** Os códigos das unidades que impedem a venda da casa inteira — só em `parcial`. */
  bloqueadaPor: readonly string[];
};

/**
 * Uma linha da grade. A linha sintética da Completa é uma linha como as outras
 * do ponto de vista do desenho — o que muda é a origem das células (derivadas,
 * não buscadas) e o fato de não ter `unit_id`, que é o que a impede de virar
 * origem de um bloqueio operacional: não existe unidade "casa inteira" para
 * bloquear, existem oito.
 */
export type LinhaDaGrade = {
  chave: string;
  tipo: "unidade" | "sintetica";
  codigo: string;
  nome: string;
  unitId: string | null;
  /** Produto usado para responder o preço da noite no hover. Preço é do
   *  produto, nunca da unidade — a mesma AP-01 custa uma coisa vendida como
   *  Apartamento 2 Suítes e outra dentro da Completa. */
  unitTypeId: string | null;
  celulas: readonly (CelulaDoMapa | CelulaSintetica)[];
};

export type GrupoDoMapa = {
  unitTypeId: string;
  nome: string;
  consumes: Consumo;
  linhas: readonly LinhaDaGrade[];
};

/** Índice de preço/tipo de data por (produto, dia), montado de `GET /availability`. */
export type PrecoDaNoite = {
  price_cents: number | null;
  date_type: TipoDeData;
  min_nights: number;
  available: number;
};

export type IndiceDePrecos = ReadonlyMap<string, PrecoDaNoite>;

export function chaveDePreco(unitTypeId: string, data: DataISO): string {
  return `${unitTypeId}|${data}`;
}

/**
 * `hold_expires_at` por reserva.
 *
 * **Não vem do mapa** — `LinhaDoMapa.days[]` não tem `expires_at`, e a célula
 * `hold` do design system pede contador de expiração. Sai de
 * `GET /reservations?status=hold`, que é onde `hold_expires_at` é publicado.
 * Está anotado no relatório como divergência de contrato: a alternativa seria
 * a tela inventar um prazo de 48 h a partir da política, que é exatamente o
 * número que `extend-hold` existe para mudar.
 */
export type ExpiracaoDeHold = ReadonlyMap<string, string>;

/** O pacote que o servidor entrega à tela, e que o refetch de tempo real repõe. */
export type DadosDoMapa = {
  linhas: readonly LinhaDoMapa[];
  expiracoes: Record<string, string>;
  buscadoEm: string;
};
