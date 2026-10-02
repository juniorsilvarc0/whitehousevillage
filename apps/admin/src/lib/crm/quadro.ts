import { z } from "zod";

import { chamarCrm, type ResultadoCrm } from "@/lib/crm/api";
import type { QuadroKanban } from "@/lib/crm/tipos";

/*
 * Módulo de SERVIDOR (a cadeia `@/lib/crm/api` → `@/lib/api/client` →
 * `next/headers` quebra o build de quem o importar de um `"use client"`). O
 * `FiltrosDoFunil` daqui é `import type` no cliente, e tipo não sobrevive à
 * compilação.
 */

/**
 * O recorte do funil — **uma** definição, duas leituras.
 *
 * Quem lê o quadro é a página (`/app/funil`, na primeira carga) e a Server
 * Action que o tempo real dispara a cada evento do tópico `crm`. As duas
 * precisam do MESMO `GET /crm/opportunities/kanban` com o MESMO recorte: se
 * cada uma montasse a própria query, um filtro novo entraria na tela e a
 * atualização automática continuaria trazendo o quadro sem ele — a tela
 * "ao vivo" mostrando um recorte que ninguém pediu, e nenhum erro em lugar
 * nenhum.
 */

/** `?q=a&q=b` chega como array. A barra do funil tem um campo só, então vale o
 *  primeiro; sem isto o objeto inteiro reprovaria na validação e o recorte
 *  sumiria calado, devolvendo o funil inteiro para quem filtrou. */
const parametro = z
  .union([z.string(), z.array(z.string())])
  .transform((valor) => (Array.isArray(valor) ? (valor[0] ?? "") : valor))
  .optional();

/**
 * Os filtros como a URL os entrega: **texto**, inclusive o booleano.
 *
 * Eles atravessam a fronteira servidor → cliente → Server Action (o kanban
 * devolve o mesmo recorte quando pede o quadro de novo), e o que atravessa essa
 * fronteira é o que veio da query string. Converter para `boolean` aqui daria
 * duas representações do mesmo filtro, e a conversão teria de ser desfeita do
 * outro lado.
 */
export const FiltrosDoFunil = z.object({
  pipeline_id: parametro,
  q: parametro,
  owner_id: parametro,
  unit_type_id: parametro,
  from: parametro,
  to: parametro,
  include_closed: parametro,
});
export type FiltrosDoFunil = z.infer<typeof FiltrosDoFunil>;

/**
 * Uma chamada desenha o quadro inteiro: colunas, cards e **totais por coluna**.
 *
 * Os totais vêm do servidor porque a coluna é paginada — somar o que veio na
 * tela daria um valor de funil que muda conforme se rola a página, e é
 * justamente o total que a gestão olha primeiro.
 */
export function carregarQuadro(filtros: FiltrosDoFunil): Promise<ResultadoCrm<QuadroKanban>> {
  return chamarCrm<QuadroKanban>("/crm/opportunities/kanban", {
    query: {
      pipeline_id: filtros.pipeline_id,
      q: filtros.q,
      owner_id: filtros.owner_id,
      unit_type_id: filtros.unit_type_id,
      from: filtros.from,
      to: filtros.to,
      // Só `"true"` liga o histórico fechado. Repassar o texto cru deixaria
      // `include_closed=0` ligar o filtro, porque para a API qualquer valor
      // presente já é o parâmetro presente.
      include_closed: filtros.include_closed === "true" ? "true" : undefined,
    },
  });
}
