"use server";

import { apiFetch } from "@/lib/api/client";
import type { Orcamento } from "@/lib/api/comercial";
import { detalhesDoZod } from "@/lib/acoes/campos";
import { exigir } from "@/lib/acoes/guarda";
import { tentar } from "@/lib/acoes/executar";
import { falha, type Resultado } from "@/lib/acoes/resultado";
import { OrcamentoFormulario, orcamentoParaPedido } from "@/lib/comercial/orcamento";

/**
 * Calcula o orçamento — e **não bloqueia data nenhuma**.
 *
 * `POST /quotes` não grava e não insere em `stay_blocks`; só a pré-reserva faz
 * isso. É por essa razão que um orçamento pode virar `409 DATE_CONFLICT` na
 * hora de virar reserva, e isso está certo: entre calcular e vender, a data
 * pode ter sido tomada, e quem impede a dupla venda é a constraint do banco, não
 * a resposta desta chamada.
 *
 * Sem `revalidatePath`: nada mudou no servidor.
 */
export async function calcularOrcamento(
  valores: OrcamentoFormulario,
): Promise<Resultado<Orcamento>> {
  const recusa = await exigir("quotes", "criar");
  if (recusa) return recusa;

  const analise = OrcamentoFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira os dados do orçamento.", detalhesDoZod(analise.error));
  }

  return tentar(() =>
    apiFetch<Orcamento>("/quotes", { method: "POST", body: orcamentoParaPedido(analise.data) }),
  );
}
