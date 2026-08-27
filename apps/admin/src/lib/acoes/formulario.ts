"use client";

import type { FieldValues, Path, UseFormReturn } from "react-hook-form";

import type { CodigoDeErro } from "@/lib/api/codigos";
import { mensagemDoErro, type Falha } from "@/lib/acoes/resultado";

/**
 * Leva a falha da API para dentro do formulário — **pelo código**, nunca pelo
 * texto.
 *
 * Dois caminhos, nesta ordem:
 *
 * 1. `details` traz erro por campo (é o que `422 VALIDATION_ERROR` devolve no
 *    contrato). Cada chave que existir no formulário vira mensagem naquele
 *    campo, que é onde o usuário está olhando.
 * 2. O `code` aponta para um campo específico — `CODE_IN_USE` é sempre sobre a
 *    chave natural, e mostrar "já existe um registro com esse código" no topo
 *    do modal faz o usuário procurar qual dos oito campos errou.
 *
 * O que sobra vira mensagem geral. Devolve `null` quando tudo coube em algum
 * campo: aí não há o que dizer no topo.
 */
export function aplicarFalha<T extends FieldValues>(
  falhaDaApi: Falha,
  form: UseFormReturn<T>,
  campoPorCodigo: Partial<Record<CodigoDeErro, Path<T>>> = {},
): string | null {
  let algumCampoRecebeu = false;

  const campos = Object.keys(form.getValues() as Record<string, unknown>);
  for (const [chave, valor] of Object.entries(falhaDaApi.details)) {
    if (typeof valor !== "string" || !campos.includes(chave)) continue;
    form.setError(chave as Path<T>, { type: "server", message: valor });
    algumCampoRecebeu = true;
  }

  const alvo = campoPorCodigo[falhaDaApi.code];
  if (alvo && campos.includes(alvo)) {
    form.setError(alvo, { type: "server", message: mensagemDoErro(falhaDaApi.code) });
    form.setFocus(alvo);
    return null;
  }

  return algumCampoRecebeu ? null : mensagemDoErro(falhaDaApi.code);
}
