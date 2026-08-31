"use client";

import type { FieldValues, Path, UseFormReturn } from "react-hook-form";

import { aplicarFalha } from "@/lib/acoes/formulario";
import type { FalhaDeContato } from "@/lib/contatos/api";
import { ehCodigoDeContatos, mensagemDeContato } from "@/lib/contatos/codigos";

/**
 * Leva a falha para dentro do formulário — **pelo código**, nunca pelo texto da
 * API.
 *
 * Existe porque `lib/acoes/formulario.ts#aplicarFalha` só aceita o vocabulário
 * fechado de `lib/api/codigos.ts`, e os dois códigos do cadastro
 * (`CONTACT_DUPLICATE`, `CONTACT_ANONYMIZED`) ainda não estão lá. Em vez de
 * reescrever a função inteira, esta trata os dois casos que ela não conhece e
 * **delega o resto** — não há um segundo mapa de `details` → campo, que
 * divergiria do primeiro no dia em que só um fosse ajustado.
 *
 * No dia em que os códigos entrarem no espelho geral, este arquivo some e a
 * chamada volta a ser `aplicarFalha` direto.
 */
export function aplicarFalhaDeContato<T extends FieldValues>(
  falha: FalhaDeContato,
  form: UseFormReturn<T>,
  campoPorCodigo: Partial<Record<"CONTACT_DUPLICATE" | "CONTACT_ANONYMIZED", Path<T>>> = {},
): string | null {
  if (ehCodigoDeContatos(falha.code)) {
    const alvo = campoPorCodigo[falha.code];
    const campos = Object.keys(form.getValues() as Record<string, unknown>);
    if (alvo && campos.includes(alvo)) {
      form.setError(alvo, { type: "server", message: mensagemDeContato(falha.code) });
      form.setFocus(alvo);
      return null;
    }
    return mensagemDeContato(falha.code);
  }

  // `falha.code` já está estreitado para o vocabulário geral aqui — o `code`
  // explícito depois do spread é o que carrega esse estreitamento.
  return aplicarFalha({ ...falha, code: falha.code }, form);
}
