"use client";

import * as React from "react";

import { cn } from "@/lib/utils";

/**
 * Abas com a semântica de abas — `tablist`, `tab`, `tabpanel` e as setas do
 * teclado.
 *
 * Vive no CRM e não em `components/ui` de propósito: `components/ui` é
 * território compartilhado, e criar primitivo lá no meio de uma rodada em que
 * outro agente também escreve no painel é a colisão anunciada. Se uma segunda
 * tela pedir abas, isto sobe para a casca num movimento só.
 *
 * O estado **não** vai para a query string. Filtro de lista vai (é o que se
 * manda por link); aba de detalhe não, porque cada troca de aba viraria uma
 * navegação e uma recarga da árvore de servidor inteira para mostrar dados que
 * já estão na memória.
 */
export type Aba = {
  id: string;
  rotulo: string;
  /** Contador ao lado do rótulo (tarefas, notas). `null` não desenha nada. */
  contagem?: number | null;
  conteudo: React.ReactNode;
};

export function Abas({ abas, inicial, className }: { abas: Aba[]; inicial?: string; className?: string }) {
  const [ativa, setAtiva] = React.useState(inicial ?? abas[0]?.id ?? "");
  const refs = React.useRef<Record<string, HTMLButtonElement | null>>({});

  const indiceAtivo = Math.max(0, abas.findIndex((aba) => aba.id === ativa));

  function navegar(evento: React.KeyboardEvent) {
    const passo = evento.key === "ArrowRight" ? 1 : evento.key === "ArrowLeft" ? -1 : 0;
    if (passo === 0) return;
    evento.preventDefault();
    const proxima = abas[(indiceAtivo + passo + abas.length) % abas.length];
    if (!proxima) return;
    setAtiva(proxima.id);
    refs.current[proxima.id]?.focus();
  }

  return (
    <div className={cn("flex flex-col gap-4", className)}>
      <div role="tablist" aria-label="Seções da oportunidade" onKeyDown={navegar} className="flex flex-wrap gap-1 border-b border-border/60">
        {abas.map((aba) => {
          const selecionada = aba.id === ativa;
          return (
            <button
              key={aba.id}
              ref={(elemento) => {
                refs.current[aba.id] = elemento;
              }}
              type="button"
              role="tab"
              id={`aba-${aba.id}`}
              aria-selected={selecionada}
              aria-controls={`painel-${aba.id}`}
              // Só a aba ativa entra na ordem de tabulação: é o padrão de
              // "roving tabindex", e sem ele o teclado percorre as cinco abas
              // antes de chegar ao conteúdo.
              tabIndex={selecionada ? 0 : -1}
              onClick={() => setAtiva(aba.id)}
              className={cn(
                "-mb-px rounded-t-lg border-b-2 px-3 py-2 text-sm outline-none transition-colors",
                "focus-visible:ring-2 focus-visible:ring-ring/40",
                selecionada
                  ? "border-primary font-medium text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground",
              )}
            >
              {aba.rotulo}
              {typeof aba.contagem === "number" ? (
                <span className="ml-1.5 font-mono text-[0.68rem] tabular-nums text-muted-foreground">
                  {aba.contagem}
                </span>
              ) : null}
            </button>
          );
        })}
      </div>

      {abas.map((aba) =>
        aba.id === ativa ? (
          <div key={aba.id} role="tabpanel" id={`painel-${aba.id}`} aria-labelledby={`aba-${aba.id}`} tabIndex={0} className="outline-none">
            {aba.conteudo}
          </div>
        ) : null,
      )}
    </div>
  );
}
