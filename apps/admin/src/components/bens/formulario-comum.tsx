"use client";

import { Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";

/**
 * As duas peças que todo modal do inventário repete: o rodapé (cancelar à
 * esquerda, salvar com spinner) e o aviso geral no topo do formulário.
 */
export function RodapeDoModal({
  formulario,
  enviando,
  aoCancelar,
  rotulo = "Salvar",
  desabilitado = false,
}: {
  /** Id do `<form>`. Único por modal: vários ficam montados na mesma árvore. */
  formulario?: string;
  enviando: boolean;
  aoCancelar: () => void;
  rotulo?: string;
  desabilitado?: boolean;
}) {
  return (
    <>
      <Button variant="ghost" onClick={aoCancelar} disabled={enviando}>
        Cancelar
      </Button>
      <Button type="submit" form={formulario} disabled={enviando || desabilitado}>
        {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
        {rotulo}
      </Button>
    </>
  );
}

export function AvisoGeral({ mensagem }: { mensagem: string | null }) {
  if (!mensagem) return null;
  return (
    <p role="alert" className="mb-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
      {mensagem}
    </p>
  );
}
