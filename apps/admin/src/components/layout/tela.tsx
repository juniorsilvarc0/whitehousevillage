import * as React from "react";
import Link from "next/link";
import { ChevronLeft, Info, TriangleAlert } from "lucide-react";

import { cn } from "@/lib/utils";

/**
 * A moldura de uma tela de configuração: respiro, cabeçalho, seções.
 *
 * O painel branco (`panel-float`) já vem do `DashboardShell` e rola por dentro;
 * o que falta a cada tela é só o preenchimento e o ritmo vertical. Ficam aqui
 * para não haver seis versões do mesmo `p-4 sm:p-6 lg:p-8`.
 */
export function Tela({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn("flex flex-col gap-6 p-4 sm:p-6 lg:p-8", className)}>{children}</div>;
}

export function CabecalhoDeTela({
  titulo,
  descricao,
  voltar,
  acoes,
}: {
  titulo: string;
  descricao?: React.ReactNode;
  voltar?: { href: string; rotulo: string };
  acoes?: React.ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        {voltar ? (
          <Link
            href={voltar.href}
            className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            <ChevronLeft className="size-3.5" aria-hidden="true" />
            {voltar.rotulo}
          </Link>
        ) : null}
        <h1 className="font-display mt-1 text-2xl leading-tight sm:text-3xl">{titulo}</h1>
        {descricao ? <p className="mt-2 max-w-prose text-sm text-muted-foreground">{descricao}</p> : null}
      </div>
      {acoes ? <div className="flex shrink-0 flex-wrap items-center gap-2">{acoes}</div> : null}
    </header>
  );
}

export function Secao({
  titulo,
  descricao,
  acoes,
  children,
  className,
}: {
  titulo: string;
  descricao?: React.ReactNode;
  acoes?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    // Sub-superfície dentro do `panel-float`: borda e fundo, nunca sombra —
    // sombra dentro de sombra é o anti-padrão nº 1 da casca.
    //
    // `aria-label` em vez de `aria-labelledby`: `Secao` é renderizada em
    // componente de servidor, onde `useId()` não existe, e id fabricado do
    // título quebraria no dia em que dois títulos coincidissem.
    <section
      aria-label={titulo}
      className={cn("rounded-xl border border-border/60 bg-muted/20 p-4 sm:p-5", className)}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="font-display text-base leading-tight">
            {titulo}
          </h2>
          {descricao ? (
            <p className="mt-1 max-w-prose text-sm text-muted-foreground">{descricao}</p>
          ) : null}
        </div>
        {acoes ? <div className="flex shrink-0 flex-wrap items-center gap-2">{acoes}</div> : null}
      </div>
      <div className="mt-4">{children}</div>
    </section>
  );
}

/**
 * A frase que explica a regra na própria tela.
 *
 * Existe porque estas telas são o material da reunião com os proprietários:
 * "períodos se sobrepõem de propósito" e "salvar publica uma versão nova" são
 * decisões de negócio que não podem morar só na documentação — quem está
 * clicando precisa lê-las no momento do clique.
 */
export function Nota({
  variante = "info",
  children,
  className,
}: {
  variante?: "info" | "atencao";
  children: React.ReactNode;
  className?: string;
}) {
  const Icone = variante === "atencao" ? TriangleAlert : Info;
  return (
    <p
      className={cn(
        "flex items-start gap-2 rounded-lg px-3 py-2 text-xs leading-relaxed",
        variante === "atencao"
          ? "bg-alcada-atencao/12 text-foreground"
          : "bg-card/70 text-muted-foreground",
        className,
      )}
    >
      <Icone
        aria-hidden="true"
        className={cn("mt-0.5 size-3.5 shrink-0", variante === "atencao" ? "text-alcada-atencao" : "text-primary")}
      />
      <span className="min-w-0">{children}</span>
    </p>
  );
}
