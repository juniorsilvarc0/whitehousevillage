import type { CelulaDoMapa, CelulaSintetica, StatusDaCelulaSintetica } from "./tipos";

/**
 * De 90 células por linha para um punhado de **faixas**.
 *
 * Este arquivo é o que torna o mapa barato. Uma estadia de doze noites não são
 * doze retângulos com borda arredondada e o nome do hóspede repetido doze
 * vezes: é **uma** barra que atravessa doze colunas, com o nome escrito uma vez
 * e as pontas arredondadas só onde a estadia de fato começa e termina.
 *
 * A conta, medida no caso real das 9 linhas × 90 dias: 810 células viram algo
 * entre 20 e 60 faixas. O que sobra de nó no DOM é o fundo (a cor do tipo de
 * data), que é virtualizado por coluna.
 *
 * **Continuidade é por identidade de bloco, nunca por igualdade de status.**
 * Duas reservas encostadas (back-to-back — o check-out de uma é o check-in da
 * outra, e é caso comum) são as duas `confirmed`, e mesclá-las numa barra só
 * apagaria a troca de hóspede exatamente no dia em que a operação precisa
 * enxergá-la.
 */
export type Faixa = {
  chave: string;
  /** Índice do primeiro dia na janela. */
  inicio: number;
  /** Exclusivo, como toda faixa de estadia. */
  fim: number;
  status: Exclude<StatusDaCelulaSintetica, "livre">;
  stayBlockId: string | null;
  reservationId: string | null;
  reservationCode: string | null;
  guestName: string | null;
  /**
   * A faixa encosta na borda da janela. **Não** afirma que a estadia continua
   * fora dela — a resposta só cobre o intervalo pedido, e afirmar mais do que
   * se sabe seria inventar. O que a tela faz com isso é honesto: desenha a
   * ponta reta em vez de arredondada, que se lê como "corta aqui", e não como
   * "termina aqui".
   */
  tocaInicio: boolean;
  tocaFim: boolean;
};

type Celula = CelulaDoMapa | CelulaSintetica;

/**
 * Identidade de uma célula ocupada, na ordem em que a informação é confiável:
 *
 * 1. `stay_block_id` — a linha de calendário. É *a* identidade: uma manutenção
 *    de três dias é um bloco só, e uma reserva ocupa um bloco por unidade.
 * 2. `reservation_id` — a linha sintética da Completa não tem bloco (tem oito),
 *    e é a reserva que a mantém contínua.
 * 3. o status — sobra para `parcial`, que é derivado e não tem registro nenhum
 *    por trás.
 */
function identidade(celula: Celula): string | null {
  if (celula.status === "livre") return null;
  if (celula.stay_block_id) return `b:${celula.stay_block_id}`;
  if (celula.reservation_id) return `r:${celula.reservation_id}`;
  return `s:${celula.status}`;
}

export function montarFaixas(celulas: readonly Celula[]): Faixa[] {
  const faixas: Faixa[] = [];
  let atual: Faixa | null = null;
  let idAtual: string | null = null;

  for (let i = 0; i < celulas.length; i += 1) {
    const celula = celulas[i]!;
    const id = identidade(celula);

    if (id === null) {
      atual = null;
      idAtual = null;
      continue;
    }

    if (atual && id === idAtual) {
      atual.fim = i + 1;
      continue;
    }

    atual = {
      chave: `${id}@${i}`,
      inicio: i,
      fim: i + 1,
      status: celula.status as Exclude<StatusDaCelulaSintetica, "livre">,
      stayBlockId: celula.stay_block_id,
      reservationId: celula.reservation_id,
      reservationCode: celula.reservation_code,
      guestName: celula.guest_name,
      tocaInicio: i === 0,
      tocaFim: false,
    };
    idAtual = id;
    faixas.push(atual);
  }

  for (const faixa of faixas) faixa.tocaFim = faixa.fim === celulas.length;
  return faixas;
}

/** As faixas que intersectam `[primeiro, ultimo]` — o resto nem vira nó no DOM. */
export function faixasVisiveis(faixas: readonly Faixa[], primeiro: number, ultimo: number): Faixa[] {
  return faixas.filter((faixa) => faixa.inicio <= ultimo && faixa.fim > primeiro);
}

/** A faixa que cobre um dia, ou `null`. É o que o clique e o hover consultam. */
export function faixaNoDia(faixas: readonly Faixa[], indice: number): Faixa | null {
  return faixas.find((faixa) => faixa.inicio <= indice && faixa.fim > indice) ?? null;
}
