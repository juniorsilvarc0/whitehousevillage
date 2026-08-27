"use client";

import * as React from "react";

/**
 * O controle de um modal de formulário: quem abre diz **com o quê**.
 *
 * ## Por que imperativo, e não uma prop `aberto` + efeito
 *
 * Um modal de edição precisa nascer limpo a cada abertura — com os valores da
 * linha escolhida agora, sem o erro e sem o rascunho da vez anterior. As três
 * formas de conseguir isso, e por que sobra esta:
 *
 * 1. **Efeito olhando a prop `aberto`** (o que existia): `useEffect(() => { if
 *    (aberto) form.reset(...) })`. É repor estado *depois* de renderizar com o
 *    estado errado — um render em cascata por abertura, e a regra
 *    `react-hooks/set-state-in-effect` reprova com razão.
 * 2. **`key` no chamador**, remontando o modal a cada abertura. Repõe tudo de
 *    graça, mas come a animação de entrada: o Base UI inicia `mounted` com o
 *    valor de `open` (`useTransitionStatus`), então um `Dialog` que **monta**
 *    já aberto nunca recebe `data-starting-style`. No celular isso é a folha
 *    de baixo aparecendo de estalo, sem subir.
 * 3. **Repor no evento que abre** — que é onde o React manda repor. O evento é
 *    o clique, e o clique é do chamador; falta só a linha até o modal, e é essa
 *    linha que este arquivo é.
 *
 * O modal continua montado (animação intacta) e passa a ser dono do próprio
 * `aberto`: o chamador guarda só a intenção, não o estado.
 */
export type ControleDeModal<T> = {
  /** Abre com o alvo — `null` é "criar novo". */
  abrir: (alvo: T) => void;
};

export type Controlador<T> = {
  /** Vai para a prop `controle` do modal. */
  ref: React.RefObject<ControleDeModal<T> | null>;
  /** Vai para o `onClick` de quem abre. Silencioso enquanto o modal não montou:
   *  clique antes da hidratação não pode virar exceção na tela. */
  abrir: (alvo: T) => void;
};

export function useControleDeModal<T>(): Controlador<T> {
  const ref = React.useRef<ControleDeModal<T> | null>(null);
  const abrir = React.useCallback((alvo: T) => {
    ref.current?.abrir(alvo);
  }, []);
  return React.useMemo(() => ({ ref, abrir }), [abrir]);
}
