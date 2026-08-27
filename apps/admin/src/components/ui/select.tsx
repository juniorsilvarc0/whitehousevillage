import * as React from "react";
import { ChevronDown } from "lucide-react";

import { cn } from "@/lib/utils";

/**
 * Select **nativo**, com a moldura do `Input`.
 *
 * Nativo de propósito: no celular ele abre a roleta do sistema, que é o
 * componente de escolha que o usuário já sabe usar, e sobrevive a formulário
 * dentro de drawer sem disputar portal, foco e rolagem com o `ModalShell`. O
 * que a casca acrescenta é só a seta — a do agente do usuário não acompanha a
 * borda arredondada.
 */
export type SelectProps = React.ComponentProps<"select"> & { invalid?: boolean };

export function Select({ className, invalid, children, ...props }: SelectProps) {
  return (
    <div className="relative">
      <select
        aria-invalid={invalid || undefined}
        className={cn(
          "flex h-11 w-full appearance-none rounded-xl border border-input bg-card px-3.5 py-2 pr-10 text-sm text-foreground",
          "transition-[border-color,box-shadow] outline-none",
          "focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/35",
          "disabled:cursor-not-allowed disabled:opacity-60",
          "aria-invalid:border-destructive aria-invalid:focus-visible:ring-destructive/30",
          className,
        )}
        {...props}
      >
        {children}
      </select>
      <ChevronDown
        aria-hidden="true"
        className="pointer-events-none absolute right-3.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
      />
    </div>
  );
}
