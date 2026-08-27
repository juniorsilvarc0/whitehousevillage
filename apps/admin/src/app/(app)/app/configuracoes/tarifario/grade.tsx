"use client";

import { GradeDeTarifas, type ProdutoDaGrade } from "@/components/comercial/grade-de-tarifas";
import type { MinimoDeNoites, Tarifa, TipoDeData } from "@/lib/api/comercial";

import { salvarGrade } from "./acoes";

/**
 * Casa a grade (componente puro, testável sem servidor) com a Server Action que
 * grava. É a única razão de este arquivo existir: `GradeDeTarifas` recebe
 * `aoSalvar` por parâmetro para poder ser testada com um duplo, e alguém
 * precisa amarrar o parâmetro à ação de verdade.
 */
export function GradeDaTabela({
  tabelaId,
  produtos,
  tarifas,
  minimos,
  podeEditar,
}: {
  tabelaId: string;
  produtos: ProdutoDaGrade[];
  tarifas: Tarifa[];
  minimos: MinimoDeNoites[];
  podeEditar: boolean;
}) {
  const minimosPorTipo: Partial<Record<TipoDeData, number>> = {};
  for (const regra of minimos) minimosPorTipo[regra.date_type] = regra.nights;

  return (
    <GradeDeTarifas
      tabelaId={tabelaId}
      produtos={produtos}
      tarifas={tarifas}
      minimosPorTipo={minimosPorTipo}
      podeEditar={podeEditar}
      aoSalvar={salvarGrade}
    />
  );
}
