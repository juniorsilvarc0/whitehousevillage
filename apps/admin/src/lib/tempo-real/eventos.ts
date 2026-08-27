/**
 * O vocabulário do barramento (`GET /stream`) do lado do painel.
 *
 * O envelope é magro **de propósito**: ele carrega a notícia de que algo mudou,
 * nunca o que mudou. Quem quer o dado refaz o fetch autenticado pela rota REST
 * de sempre — e é isso que mantém o RBAC num lugar só e o nome do hóspede fora
 * de um canal que não tem permissão.
 *
 * Consequência prática, e é ela que desenha o hook: **não há verbo**
 * (`created`/`deleted`). Criação, alteração e remoção têm o mesmo tratamento —
 * refazer o fetch da faixa visível — e por isso um código de cliente só.
 */
export type Topico = "calendar" | "crm";

export type EntidadeDoStream = "stay_block" | "reservation" | "opportunity" | "lead" | "activity";

export type EventoDoStream = {
  entity: EntidadeDoStream;
  id: string;
  /** Só em `stay_block`. É o que permitiria repintar uma linha em vez da matriz. */
  unit_id?: string | null;
  /** Versão da **entidade**. Não confundir com o `id:` do envelope SSE, que é o
   *  cursor do barramento e é o que volta em `Last-Event-ID`. */
  v: number;
};

const ENTIDADES: readonly string[] = ["stay_block", "reservation", "opportunity", "lead", "activity"];

/**
 * Interpreta o `data:` sem confiar nele.
 *
 * O barramento é uma segunda porta de entrada de dados na tela, e uma porta que
 * `JSON.parse` sem conferir derruba o mapa inteiro com uma exceção não tratada
 * dentro de um ouvinte de evento — onde nenhum error boundary do React alcança.
 * Envelope irreconhecível vira `null` e é descartado em silêncio; o
 * `resync` da reconexão cobre o que tiver se perdido.
 */
export function lerEvento(bruto: string): EventoDoStream | null {
  let cru: unknown;
  try {
    cru = JSON.parse(bruto);
  } catch {
    return null;
  }
  if (typeof cru !== "object" || cru === null) return null;

  const obj = cru as Record<string, unknown>;
  if (typeof obj.entity !== "string" || !ENTIDADES.includes(obj.entity)) return null;
  if (typeof obj.id !== "string" || obj.id === "") return null;
  if (typeof obj.v !== "number" || !Number.isFinite(obj.v)) return null;

  return {
    entity: obj.entity as EntidadeDoStream,
    id: obj.id,
    unit_id: typeof obj.unit_id === "string" ? obj.unit_id : null,
    v: obj.v,
  };
}

/**
 * Descarta o que a tela já desenhou.
 *
 * O `v` é a versão da entidade, e o replay de uma reconexão repõe eventos que
 * já chegaram. Sem esta guarda, voltar de dez minutos de aba em segundo plano
 * dispararia um refetch por evento reposto — dezenas de requisições para
 * chegar exatamente ao mesmo desenho.
 *
 * O `<=` (e não `<`) é o que faz os **nove** eventos de uma venda da White
 * House Completa — oito `stay_block` e uma `reservation`, todos com o mesmo
 * `v`, porque `v` é o id da transação — valerem por um refetch só quando
 * repostos. Na primeira entrega eles têm ids diferentes e passam todos; é o
 * debounce do hook, não o dedup, que os junta.
 */
export class Deduplicador {
  /** Teto de memória: o mapa cresce com entidades distintas vistas na sessão,
   *  e uma aba deixada aberta a semana inteira não pode virar um vazamento. */
  private static readonly TETO = 2_000;

  private vistos = new Map<string, number>();

  /** `true` quando o evento traz novidade. */
  novidade(evento: EventoDoStream): boolean {
    const anterior = this.vistos.get(evento.id);
    if (anterior !== undefined && evento.v <= anterior) return false;
    if (this.vistos.size >= Deduplicador.TETO) this.vistos.clear();
    this.vistos.set(evento.id, evento.v);
    return true;
  }

  /** `resync` significa que o servidor não sabe o que perdemos. O que a tela
   *  desenhou deixa de ser referência, e tudo volta a ser novidade. */
  esquecer(): void {
    this.vistos.clear();
  }
}
