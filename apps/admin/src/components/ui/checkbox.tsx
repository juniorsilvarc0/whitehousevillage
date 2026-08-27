import * as React from "react";

import { cn } from "@/lib/utils";

/**
 * Caixa de marcação nativa com a moldura da casca.
 *
 * `accent-color` em token faz o navegador desenhar o preenchimento na cor da
 * marca sem substituir o controle por um `div` — o que preservaria a aparência
 * e perderia teclado, leitor de tela e o clique no rótulo de graça.
 */
export function Checkbox({ className, ...props }: React.ComponentProps<"input">) {
  return (
    <input
      type="checkbox"
      className={cn(
        "size-4 shrink-0 cursor-pointer rounded-[0.35rem] border border-input bg-card",
        "accent-[var(--primary)] outline-none",
        "focus-visible:ring-2 focus-visible:ring-ring/40",
        "disabled:cursor-not-allowed disabled:opacity-60",
        className,
      )}
      {...props}
    />
  );
}

/** Caixa + rótulo clicável, que é como ela aparece em 90% dos formulários. */
export function CheckboxCampo({
  id,
  label,
  hint,
  className,
  ...props
}: React.ComponentProps<"input"> & { id: string; label: React.ReactNode; hint?: React.ReactNode }) {
  return (
    <div className={cn("flex items-start gap-2.5", className)}>
      <Checkbox id={id} className="mt-0.5" {...props} />
      <div className="min-w-0">
        <label htmlFor={id} className="cursor-pointer text-sm font-medium leading-tight text-foreground">
          {label}
        </label>
        {hint ? <p className="mt-0.5 text-xs leading-snug text-muted-foreground">{hint}</p> : null}
      </div>
    </div>
  );
}
