"use server";

import { revalidatePath } from "next/cache";

import { apiFetch } from "@/lib/api/client";
import type { PoliticaComercial, PoliticaDeCancelamento } from "@/lib/api/comercial";
import { detalhesDoZod } from "@/lib/acoes/campos";
import { exigir } from "@/lib/acoes/guarda";
import { tentar } from "@/lib/acoes/executar";
import { falha, type Resultado } from "@/lib/acoes/resultado";
import {
  PoliticaComercialFormulario,
  PoliticaDeCancelamentoFormulario,
  politicaComercialParaEntrada,
  politicaDeCancelamentoParaEntrada,
} from "./esquemas";

/**
 * Publicação de política.
 *
 * **`PUT` cria versão nova; nunca edita a vigente.** O servidor atribui
 * `version = max(version) + 1` e ignora qualquer número que venha no corpo.
 * Editar a linha vigente reescreveria a regra sob os pés das reservas que
 * congelaram aquele número — que é exatamente o que `policy_version` e
 * `cancellation_policy_id` existem para impedir.
 *
 * Por isso não há ação de excluir aqui: política é histórico, não cadastro.
 * Voltar atrás é publicar de novo a versão antiga, que entra como versão nova e
 * deixa rastro.
 */

const CAMINHO = "/app/configuracoes/politica";

export async function publicarPoliticaComercial(
  valores: PoliticaComercialFormulario,
): Promise<Resultado<PoliticaComercial>> {
  const recusa = await exigir("settings", "editar");
  if (recusa) return recusa;

  const analise = PoliticaComercialFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira a política comercial.", detalhesDoZod(analise.error));
  }

  const resultado = await tentar(() =>
    apiFetch<PoliticaComercial>("/policies/commercial", {
      method: "PUT",
      body: politicaComercialParaEntrada(analise.data),
    }),
  );

  if (resultado.ok) {
    revalidatePath(CAMINHO);
    // A alçada do semáforo e o sinal do orçamento saem daqui.
    revalidatePath("/app/orcamento");
  }
  return resultado;
}

export async function publicarPoliticaDeCancelamento(
  valores: PoliticaDeCancelamentoFormulario,
): Promise<Resultado<PoliticaDeCancelamento>> {
  const recusa = await exigir("settings", "editar");
  if (recusa) return recusa;

  const analise = PoliticaDeCancelamentoFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira as faixas de cancelamento.", detalhesDoZod(analise.error));
  }

  const resultado = await tentar(() =>
    apiFetch<PoliticaDeCancelamento>("/policies/cancellation", {
      method: "PUT",
      body: politicaDeCancelamentoParaEntrada(analise.data),
    }),
  );

  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}
