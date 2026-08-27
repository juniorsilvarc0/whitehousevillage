import { ehDataISO, hojeISO, noitesEntre, somarDias, type DataISO } from "@/lib/datas";

/**
 * A janela visível do mapa: onde começa, quantos dias, e a lista de dias.
 *
 * `to` é **exclusivo**, como toda faixa de estadia do sistema — pedir
 * `from=2026-12-20` com 3 dias devolve 20, 21 e 22, e `to` é 23. Manter a mesma
 * convenção da API aqui evita a classe de bug em que o último dia da tela é o
 * primeiro dia livre.
 */

/** ~90 dias: o horizonte que a gestão olha (o trimestre da temporada). */
export const DIAS_PADRAO = 90;

/**
 * O menor tamanho que a BARRA oferece. Menos que isso não é mapa, é agenda de
 * uma semana — mas o teto mínimo é do filtro, não da primitiva: `janelaDe(x, 1)`
 * é legítimo e é o que os testes e a linha sintética usam para raciocinar sobre
 * um dia só.
 */
export const DIAS_MINIMO_DO_FILTRO = 7;

/**
 * Teto de 366 dias porque é o teto que `GET /availability/units` declara em
 * `422 VALIDATION_ERROR`. A tela recusa antes de a API recusar, para o link
 * compartilhado com `?dias=5000` abrir num mapa e não numa mensagem de erro.
 */
export const DIAS_MAXIMO = 366;

export type Janela = {
  from: DataISO;
  /** Exclusivo. */
  to: DataISO;
  dias: readonly DataISO[];
};

export function janelaDe(from: DataISO, dias: number): Janela {
  const total = Math.min(DIAS_MAXIMO, Math.max(1, Math.trunc(Number.isFinite(dias) ? dias : DIAS_PADRAO)));
  const inicio = ehDataISO(from) ? from : hojeISO();
  const lista: DataISO[] = [];
  for (let i = 0; i < total; i += 1) lista.push(somarDias(inicio, i));
  return { from: inicio, to: somarDias(inicio, total), dias: lista };
}

/** O que a BARRA e a query string aceitam — o piso de uma semana vale aqui, não
 *  na primitiva. */
export function limitarDias(dias: number): number {
  if (!Number.isFinite(dias)) return DIAS_PADRAO;
  return Math.min(DIAS_MAXIMO, Math.max(DIAS_MINIMO_DO_FILTRO, Math.trunc(dias)));
}

/**
 * Índice do dia dentro da janela, ou `-1`.
 *
 * A grade inteira é posicionada por índice — a barra de uma reserva sabe onde
 * começa porque sabe em que coluna cai o `check_in`. Fazer isso por diferença
 * de datas em vez de `indexOf` mantém a conta em O(1) por faixa: com 90 colunas
 * e 9 linhas, um `indexOf` por célula seria 810 varreduras lineares por render.
 */
export function indiceDoDia(janela: Janela, data: DataISO): number {
  if (!ehDataISO(data)) return -1;
  const delta = noitesEntre(janela.from, data);
  return delta >= 0 && delta < janela.dias.length ? delta : -1;
}

/** Desloca a janela mantendo o tamanho — é o que os botões de navegação fazem. */
export function deslocar(janela: Janela, dias: number): Janela {
  return janelaDe(somarDias(janela.from, dias), janela.dias.length);
}

/**
 * Janela centrada em "hoje", com uma semana de passado à mostra.
 *
 * O passado importa: o mapa é operacional, e a limpeza de uma estadia que
 * terminou ontem ainda é assunto de hoje. Sete dias é o que cabe sem empurrar o
 * futuro — que é o que a gestão de fato vende — para fora da primeira tela.
 */
export function janelaPadrao(hoje: DataISO = hojeISO()): Janela {
  return janelaDe(somarDias(hoje, -7), DIAS_PADRAO);
}
