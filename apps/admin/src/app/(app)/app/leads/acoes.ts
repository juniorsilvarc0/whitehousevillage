"use server";

import { revalidatePath } from "next/cache";

import { detalhesDoZod, paraCentavos } from "@/lib/acoes/campos";
import { exigir } from "@/lib/acoes/guarda";
import { chamarCrm, falhaCrm, type ResultadoCrm } from "@/lib/crm/api";
import { ConversaoFormulario } from "@/lib/crm/esquemas";
import type { Lead, PedidoDeConversao, ResultadoDeConversao } from "@/lib/crm/tipos";

const CAMINHO = "/app/leads";

/**
 * Converte o lead em oportunidade.
 *
 * **O que o corpo manda sobrepõe o lead; o que ele omite é herdado.** Por isso
 * todo campo em branco é *removido* do corpo em vez de virar `null`: mandar
 * `null` significaria "limpe este campo", e a conversão de um clique perderia
 * as datas pretendidas que o lead já trazia — exatamente o dado que faz a
 * conversão valer a pena.
 *
 * Converter duas vezes é `409 LEAD_ALREADY_CONVERTED`, com
 * `details.opportunity_id` apontando o card que já existe. A tela usa isso para
 * abrir o card certo, em vez de deixar o operador criar um gêmeo no funil.
 */
export async function converterLead(
  id: string,
  valores: ConversaoFormulario,
): Promise<ResultadoCrm<ResultadoDeConversao>> {
  const recusa = await exigir("crm.leads", "editar");
  if (recusa) return recusa;

  const analise = ConversaoFormulario.safeParse(valores);
  if (!analise.success) {
    return falhaCrm("VALIDATION_ERROR", "Confira os dados da conversão.", detalhesDoZod(analise.error));
  }

  const v = analise.data;
  const corpo: PedidoDeConversao = {
    ...(v.pipeline_id ? { pipeline_id: v.pipeline_id } : {}),
    ...(v.stage_id ? { stage_id: v.stage_id } : {}),
    ...(v.unit_type_id ? { unit_type_id: v.unit_type_id } : {}),
    ...(v.check_in ? { check_in: v.check_in } : {}),
    ...(v.check_out ? { check_out: v.check_out } : {}),
    ...(v.amount ? { amount_cents: paraCentavos(v.amount) } : {}),
    ...(v.expected_close ? { expected_close: v.expected_close } : {}),
  };

  const resultado = await chamarCrm<ResultadoDeConversao>(`/crm/leads/${id}/convert`, {
    method: "POST",
    body: corpo,
  });

  if (resultado.ok) {
    revalidatePath(CAMINHO);
    revalidatePath("/app/funil");
  }
  return resultado;
}

/**
 * Descarta o lead — `status: descartado`, nunca `DELETE`.
 *
 * Apagar tiraria do relatório a perda que aconteceu **antes** do funil, que é
 * justamente o que essa lista mede: quanto interesse chega e quanto morre sem
 * virar negócio. O `DELETE` do contrato existe para engano de digitação, não
 * para o fim natural de um lead.
 */
export async function descartarLead(id: string): Promise<ResultadoCrm<Lead>> {
  const recusa = await exigir("crm.leads", "editar");
  if (recusa) return recusa;

  const resultado = await chamarCrm<Lead>(`/crm/leads/${id}`, {
    method: "PATCH",
    body: { status: "descartado" },
  });

  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}
