"use client";

import { QuoteBuilder, type ProdutoParaOrcar } from "@/components/comercial/quote-builder";
import type { LimitesDeAlcada } from "@/lib/comercial/alcada";
import type { OrcamentoFormulario } from "@/lib/comercial/orcamento";

import { calcularOrcamento } from "./acoes";

/**
 * Amarra o `QuoteBuilder` à Server Action.
 *
 * Existe por uma razão técnica precisa: a ação entra na lista de dependências
 * do efeito que calcula, e uma ação **passada como prop pelo componente de
 * servidor** ganha identidade nova a cada re-render da página — o efeito
 * dispararia de novo a cada renderização, recalculando um orçamento que não
 * mudou. Importada aqui, a referência é de módulo e não muda nunca.
 */
export function ConstrutorDeOrcamento({
  produtos,
  limites,
  limitesConfirmados,
  inicial,
}: {
  produtos: ProdutoParaOrcar[];
  limites: LimitesDeAlcada;
  limitesConfirmados: boolean;
  inicial: OrcamentoFormulario;
}) {
  return (
    <QuoteBuilder
      produtos={produtos}
      limites={limites}
      limitesConfirmados={limitesConfirmados}
      inicial={inicial}
      aoCalcular={calcularOrcamento}
    />
  );
}
