"use server";

import { revalidatePath } from "next/cache";

import { apiFetch } from "@/lib/api/client";
import type { Feriado, PeriodoEspecial } from "@/lib/api/comercial";
import { detalhesDoZod } from "@/lib/acoes/campos";
import { exigir } from "@/lib/acoes/guarda";
import { tentar } from "@/lib/acoes/executar";
import { falha, type Resultado } from "@/lib/acoes/resultado";
import {
  FeriadoFormulario,
  PeriodoFormulario,
  feriadoParaEntrada,
  periodoParaEntrada,
} from "./esquemas";

/**
 * Calendário comercial: feriados e períodos especiais.
 *
 * Nada aqui reescreve preço nenhum já emitido. Marcar 12/10 como feriado muda a
 * classificação das noites **dos próximos cálculos**; a reserva de ontem
 * continua com o tipo e o preço que gravou em `reservation_nights`.
 */

const CAMINHO = "/app/configuracoes/calendario";

function revalidar() {
  revalidatePath(CAMINHO);
  // A classificação da noite muda o orçamento seguinte, e a tela de orçamento
  // mostra o tipo de cada noite.
  revalidatePath("/app/orcamento");
}

export async function salvarFeriado(
  id: string | null,
  valores: FeriadoFormulario,
): Promise<Resultado<Feriado>> {
  const recusa = await exigir("settings", id ? "editar" : "criar");
  if (recusa) return recusa;

  const analise = FeriadoFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira os dados do feriado.", detalhesDoZod(analise.error));
  }

  const resultado = await tentar(() =>
    apiFetch<Feriado>(id ? `/holidays/${id}` : "/holidays", {
      method: id ? "PUT" : "POST",
      body: feriadoParaEntrada(analise.data),
    }),
  );

  if (resultado.ok) revalidar();
  return resultado;
}

export async function removerFeriado(id: string): Promise<Resultado<null>> {
  const recusa = await exigir("settings", "excluir");
  if (recusa) return recusa;

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/holidays/${id}`, { method: "DELETE" });
    return null;
  });

  if (resultado.ok) revalidar();
  return resultado;
}

export async function salvarPeriodo(
  id: string | null,
  valores: PeriodoFormulario,
): Promise<Resultado<PeriodoEspecial>> {
  const recusa = await exigir("settings", id ? "editar" : "criar");
  if (recusa) return recusa;

  const analise = PeriodoFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira os dados do período.", detalhesDoZod(analise.error));
  }

  const resultado = await tentar(() =>
    apiFetch<PeriodoEspecial>(id ? `/special-periods/${id}` : "/special-periods", {
      method: id ? "PUT" : "POST",
      body: periodoParaEntrada(analise.data),
    }),
  );

  if (resultado.ok) revalidar();
  return resultado;
}

export async function removerPeriodo(id: string): Promise<Resultado<null>> {
  const recusa = await exigir("settings", "excluir");
  if (recusa) return recusa;

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/special-periods/${id}`, { method: "DELETE" });
    return null;
  });

  if (resultado.ok) revalidar();
  return resultado;
}
