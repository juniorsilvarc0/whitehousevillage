import * as React from "react";

import { cn, iniciais } from "@/lib/utils";

/**
 * Avatar por iniciais. Sem imagem por enquanto — o contrato não expõe foto de
 * usuário, e inventar um `<img>` que sempre erra o carregamento só entregaria
 * um quadrado quebrado.
 */
export function Avatar({
  nome,
  className,
  ...props
}: React.ComponentProps<"span"> & { nome: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-flex size-9 shrink-0 items-center justify-center rounded-full",
        "bg-brand-gradient text-[0.7rem] font-semibold tracking-wide text-white",
        "ring-1 ring-white/25",
        className,
      )}
      {...props}
    >
      {iniciais(nome)}
    </span>
  );
}
