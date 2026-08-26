import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

/**
 * Botão em pílula — `rounded-full` em **todos** os tamanhos, inclusive o
 * pequeno. A forma é assinatura da casca; deixar o `sm` quadrado quebraria o
 * ritmo justo nas barras de ferramenta, onde há mais botão por centímetro.
 */
const botao = cva(
  cn(
    "inline-flex shrink-0 select-none items-center justify-center gap-2 whitespace-nowrap rounded-full",
    "font-medium transition-[background,color,box-shadow,opacity] outline-none",
    "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
    "disabled:pointer-events-none disabled:opacity-55",
    "[&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
  ),
  {
    variants: {
      variant: {
        // A ação primária carrega o gradiente da marca. A sombra é curta: o
        // botão vive dentro de um `panel-float`, e sombra dentro de sombra é
        // o anti-padrão nº 1 da casca.
        default: "bg-brand-gradient text-white shadow-[0_6px_16px_-8px_oklch(0.40_0.05_120_/_55%)] hover:brightness-110 active:brightness-95",
        secondary: "bg-secondary text-secondary-foreground hover:bg-accent hover:text-accent-foreground",
        outline: "border border-border bg-card text-foreground hover:bg-muted",
        ghost: "text-foreground hover:bg-muted",
        /** Sobre a barra de marca: fundo escuro, então o realce é o branco translúcido. */
        onBrand: "text-white/85 hover:bg-white/15 hover:text-white",
        destructive: "bg-destructive text-white hover:brightness-110",
        link: "text-primary underline-offset-4 hover:underline",
      },
      size: {
        sm: "h-8 px-3 text-xs",
        md: "h-10 px-4 text-sm",
        lg: "h-12 px-6 text-base",
        icon: "size-10",
        iconSm: "size-8",
      },
    },
    defaultVariants: { variant: "default", size: "md" },
  },
);

export type ButtonProps = React.ComponentProps<"button"> & VariantProps<typeof botao>;

export function Button({ className, variant, size, type = "button", ...props }: ButtonProps) {
  // `type` padrão "button": dentro de um <form>, o padrão do HTML é "submit", e
  // um botão de ação secundária submetendo o formulário é bug clássico.
  return <button type={type} className={cn(botao({ variant, size }), className)} {...props} />;
}

export { botao as buttonVariants };
