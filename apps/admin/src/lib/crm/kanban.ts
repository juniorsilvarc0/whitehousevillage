import type { CardDaOportunidade, ColunaDoKanban, EtapaDoFunil } from "@/lib/crm/tipos";

/**
 * O movimento do card, **puro**.
 *
 * O kanban move o card antes de o servidor responder (é o que faz o arrasto
 * parecer instantâneo) e desfaz o movimento quando a resposta é recusa. Isso
 * são duas transformações de estado que precisam ser exatamente inversas — e
 * "exatamente inversas" é a classe de coisa que se prova em teste, não que se
 * confere lendo um componente com dnd-kit no meio.
 *
 * Por isso a transformação mora aqui, sem React: o componente guarda o array
 * anterior, chama `aplicarMovimento`, e o rollback é repor o array anterior.
 * Não há "desaplicar": desfazer é voltar ao valor de antes, que continua
 * inteiro na pilha da função que o segurou.
 */

export type LocalizacaoDoCard = {
  card: CardDaOportunidade;
  /** `stage.id` da coluna em que o card está agora. */
  origem: string;
};

export function localizarCard(
  colunas: readonly ColunaDoKanban[],
  cardId: string,
): LocalizacaoDoCard | null {
  for (const coluna of colunas) {
    const card = coluna.cards.find((c) => c.id === cardId);
    if (card) return { card, origem: coluna.stage.id };
  }
  return null;
}

/**
 * O que fazer com um arrasto — decidido **antes** de mexer em qualquer estado.
 *
 * A distinção que este tipo existe para carregar: soltar o card numa coluna
 * terminal (`ganho`/`perdido`) **não** é um movimento otimista. Ganhar cria
 * reserva (e exige `Idempotency-Key`); perder exige motivo. `POST /stage`
 * recusa as duas com `409 INVALID_STATE_TRANSITION` justamente para que o
 * arrasto — o gesto que mais se repete por engano — não crie reserva sozinho.
 * Então o card **não sai do lugar**: abre-se o diálogo, e quem move o card é o
 * `/win` ou o `/lose` que vier depois.
 */
export type MovimentoPlanejado =
  | { tipo: "nada" }
  | { tipo: "ganhar"; card: CardDaOportunidade; destino: EtapaDoFunil }
  | { tipo: "perder"; card: CardDaOportunidade; destino: EtapaDoFunil }
  | {
      tipo: "mover";
      card: CardDaOportunidade;
      origem: string;
      destino: EtapaDoFunil;
      /** Já com o card na coluna nova e os totais das duas ajustados. */
      colunas: ColunaDoKanban[];
    };

export function planejarMovimento(
  colunas: readonly ColunaDoKanban[],
  cardId: string,
  destinoId: string,
): MovimentoPlanejado {
  const encontrado = localizarCard(colunas, cardId);
  const destino = colunas.find((c) => c.stage.id === destinoId)?.stage;
  if (!encontrado || !destino) return { tipo: "nada" };
  if (encontrado.origem === destinoId) return { tipo: "nada" };

  if (destino.type === "ganho") return { tipo: "ganhar", card: encontrado.card, destino };
  if (destino.type === "perdido") return { tipo: "perder", card: encontrado.card, destino };

  return {
    tipo: "mover",
    card: encontrado.card,
    origem: encontrado.origem,
    destino,
    colunas: aplicarMovimento(colunas, cardId, destinoId),
  };
}

/**
 * Tira o card de uma coluna e põe na outra, ajustando **contagem e soma** das
 * duas.
 *
 * Os totais têm de andar junto com o card. O total da coluna vem do servidor
 * porque a coluna é paginada — mas deixá-lo parado durante o movimento otimista
 * faria a soma discordar dos cards à vista até o próximo refresh, que é
 * exatamente o instante em que alguém está conferindo o funil.
 *
 * Devolve o **mesmo array** quando não há o que mover: assim o `setState` do
 * chamador não dispara render à toa.
 */
export function aplicarMovimento(
  colunas: readonly ColunaDoKanban[],
  cardId: string,
  destinoId: string,
): ColunaDoKanban[] {
  const encontrado = localizarCard(colunas, cardId);
  if (!encontrado || encontrado.origem === destinoId) return colunas as ColunaDoKanban[];
  if (!colunas.some((c) => c.stage.id === destinoId)) return colunas as ColunaDoKanban[];

  const { card, origem } = encontrado;

  return colunas.map((coluna) => {
    if (coluna.stage.id === origem) {
      return {
        ...coluna,
        count: Math.max(0, coluna.count - 1),
        amount_cents: coluna.amount_cents - card.amount_cents,
        cards: coluna.cards.filter((c) => c.id !== cardId),
      };
    }
    if (coluna.stage.id === destinoId) {
      // Entra no topo: acabou de entrar na etapa, e a ordem definitiva chega no
      // refresh seguinte — o servidor é quem ordena a coluna.
      const movido: CardDaOportunidade = { ...card, probability: coluna.stage.probability };
      return {
        ...coluna,
        count: coluna.count + 1,
        amount_cents: coluna.amount_cents + card.amount_cents,
        cards: [movido, ...coluna.cards],
      };
    }
    return coluna;
  });
}

/** Soma dos cards visíveis — usada só para conferência em teste. O número que a
 *  coluna exibe é `count`/`amount_cents`, que vêm do servidor. */
export function somaVisivel(coluna: ColunaDoKanban): number {
  return coluna.cards.reduce((total, card) => total + card.amount_cents, 0);
}
