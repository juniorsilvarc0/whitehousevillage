/**
 * Virtualização das **colunas**, e só delas.
 *
 * As linhas são nove. Virtualizá-las custaria mais código do que o que
 * economizaria, e nove `<div>` não são um problema em navegador nenhum. As
 * colunas são ~90 e podem chegar a 366 pelo filtro — aí sim, 366 × 9 = 3.294
 * células de fundo repintadas a cada evento de tempo real derrubam a aba, que é
 * o defeito que o design system nomeia em `docs/ui.md` §9.
 *
 * A janela visível é aritmética pura sobre `scrollLeft`, sem observar elemento
 * nenhum: a grade é uniforme (toda coluna tem a mesma largura), então a posição
 * de cada coluna é conhecida sem medir. É o que permite a este módulo não
 * depender do DOM e ser testado como função.
 */
export type IntervaloVisivel = { primeiro: number; ultimo: number };

/**
 * `overscan` é margem de segurança em colunas: rolar não pode revelar branco
 * antes do próximo render. Cinco colunas a 2,25 rem cada dão ~180 px de folga
 * de cada lado, o que cobre um "flick" de trackpad entre dois quadros.
 */
export const OVERSCAN_PADRAO = 5;

export function intervaloVisivel({
  scrollLeft,
  largura,
  larguraDoDia,
  total,
  overscan = OVERSCAN_PADRAO,
}: {
  scrollLeft: number;
  largura: number;
  larguraDoDia: number;
  total: number;
  overscan?: number;
}): IntervaloVisivel {
  if (total <= 0 || larguraDoDia <= 0) return { primeiro: 0, ultimo: -1 };

  // Largura zero é o primeiro render, antes de o layout existir. Devolver um
  // intervalo vazio deixaria a grade em branco até o segundo quadro e faria a
  // medição de desempenho mentir para melhor; devolver tudo pinta 366 colunas.
  // O meio-termo honesto é uma tela cheia de colunas a partir do começo.
  const visiveis = largura > 0 ? Math.ceil(largura / larguraDoDia) : Math.min(total, 40);

  const primeiro = Math.max(0, Math.floor(scrollLeft / larguraDoDia) - overscan);
  const ultimo = Math.min(total - 1, primeiro + visiveis + overscan * 2);
  return { primeiro, ultimo };
}

/** Os índices renderizados, prontos para o `map` do JSX. */
export function indicesDe({ primeiro, ultimo }: IntervaloVisivel): number[] {
  const lista: number[] = [];
  for (let i = primeiro; i <= ultimo; i += 1) lista.push(i);
  return lista;
}
