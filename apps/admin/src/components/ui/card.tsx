import * as React from "react";

import { cn } from "@/lib/utils";

/**
 * Cartão de conteúdo.
 *
 * `elevated` só quando o cartão flutua **direto sobre o wash**. Dentro de um
 * `panel-float` ele é sub-superfície e vai sem sombra — sombra dentro de sombra
 * suja o painel e achata a hierarquia inteira.
 */
export function Card({
  className,
  elevated = false,
  ...props
}: React.ComponentProps<"div"> & { elevated?: boolean }) {
  return (
    <div
      className={cn(
        "rounded-xl border border-border/60 bg-muted/25",
        elevated && "bg-card shadow-soft",
        className,
      )}
      {...props}
    />
  );
}

export function CardHeader({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={cn("flex flex-col gap-1 p-4 sm:p-5", className)} {...props} />;
}

export function CardTitle({ className, ...props }: React.ComponentProps<"h3">) {
  return <h3 className={cn("font-display text-base leading-tight", className)} {...props} />;
}

export function CardDescription({ className, ...props }: React.ComponentProps<"p">) {
  return <p className={cn("text-sm text-muted-foreground", className)} {...props} />;
}

export function CardContent({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={cn("p-4 pt-0 sm:p-5 sm:pt-0", className)} {...props} />;
}

export function CardFooter({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={cn("flex items-center gap-2 p-4 pt-0 sm:p-5 sm:pt-0", className)} {...props} />;
}
