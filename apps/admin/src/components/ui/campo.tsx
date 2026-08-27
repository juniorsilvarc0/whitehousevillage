import * as React from "react";

import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

/**
 * Rótulo + controle + auxílio + erro, amarrados pelos ids certos.
 *
 * O trabalho que este componente faz não é visual: é ligar `aria-describedby` à
 * dica e à mensagem de erro. Sem isso o leitor de tela anuncia "Capacidade,
 * caixa de edição" e cala a parte que importa — que o campo está inválido e por
 * quê. Fazer isso à mão em vinte formulários é errar em pelo menos um.
 */
export function Campo({
  id,
  label,
  hint,
  erro,
  obrigatorio,
  className,
  children,
}: {
  id: string;
  label: string;
  hint?: React.ReactNode;
  erro?: string;
  obrigatorio?: boolean;
  className?: string;
  children: (props: {
    id: string;
    "aria-describedby": string | undefined;
    invalid: boolean;
  }) => React.ReactNode;
}) {
  const idDica = hint ? `${id}-dica` : undefined;
  const idErro = erro ? `${id}-erro` : undefined;
  const descrito = [idDica, idErro].filter(Boolean).join(" ") || undefined;

  return (
    <div className={cn("flex flex-col gap-1.5", className)}>
      <Label htmlFor={id}>
        {label}
        {obrigatorio ? <span className="ml-0.5 text-destructive">*</span> : null}
      </Label>
      {children({ id, "aria-describedby": descrito, invalid: Boolean(erro) })}
      {hint ? (
        <p id={idDica} className="text-xs leading-snug text-muted-foreground">
          {hint}
        </p>
      ) : null}
      {erro ? (
        <p id={idErro} role="alert" className="text-xs font-medium text-destructive">
          {erro}
        </p>
      ) : null}
    </div>
  );
}
