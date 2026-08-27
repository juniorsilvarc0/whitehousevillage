"use client";

import type { FieldValues, Path, UseFormReturn } from "react-hook-form";

import type { FalhaCrm } from "@/lib/crm/api";
import { mensagemCrm, type CodigoCrm } from "@/lib/crm/codigos";

/**
 * Leva a recusa da API para dentro do formulário — **pelo código**, nunca pelo
 * texto.
 *
 * Mesmo contrato de `lib/acoes/formulario.ts`, com o vocabulário do CRM: aquele
 * aceita só `CodigoDeErro`, e os sete códigos novos ainda não estão no espelho
 * do painel. Quando estiverem, esta função sai e a da casca serve aos dois.
 *
 * Devolve `null` quando a mensagem coube em algum campo — aí não há o que dizer
 * no topo do modal, e repetir a frase nos dois lugares só empurra o formulário
 * para fora da tela.
 */
export function aplicarFalhaCrm<T extends FieldValues>(
  falha: FalhaCrm,
  form: UseFormReturn<T>,
  campoPorCodigo: Partial<Record<CodigoCrm, Path<T>>> = {},
): string | null {
  let algumCampoRecebeu = false;

  const campos = Object.keys(form.getValues() as Record<string, unknown>);
  for (const [chave, valor] of Object.entries(falha.details)) {
    if (typeof valor !== "string" || !campos.includes(chave)) continue;
    form.setError(chave as Path<T>, { type: "server", message: valor });
    algumCampoRecebeu = true;
  }

  const alvo = campoPorCodigo[falha.code];
  if (alvo && campos.includes(alvo)) {
    form.setError(alvo, { type: "server", message: mensagemCrm(falha.code) });
    form.setFocus(alvo);
    return null;
  }

  return algumCampoRecebeu ? null : mensagemCrm(falha.code);
}
