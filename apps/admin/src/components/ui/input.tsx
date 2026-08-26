import * as React from "react";

import { cn } from "@/lib/utils";

export type InputProps = React.ComponentProps<"input"> & {
  /** Marca o campo como inválido para o CSS **e** para o leitor de tela. */
  invalid?: boolean;
};

export function Input({ className, invalid, type = "text", ...props }: InputProps) {
  return (
    <input
      type={type}
      aria-invalid={invalid || undefined}
      className={cn(
        "flex h-11 w-full rounded-xl border border-input bg-card px-3.5 py-2 text-sm text-foreground",
        "placeholder:text-muted-foreground/70",
        "transition-[border-color,box-shadow] outline-none",
        "focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/35",
        "disabled:cursor-not-allowed disabled:opacity-60",
        "aria-invalid:border-destructive aria-invalid:focus-visible:ring-destructive/30",
        className,
      )}
      {...props}
    />
  );
}
