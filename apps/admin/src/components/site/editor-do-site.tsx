"use client";

import * as React from "react";
import { ChevronDown } from "lucide-react";
import { Toaster } from "sonner";

import { CampoDoSiteEditor } from "@/components/site/campo-do-site";
import { Badge } from "@/components/ui/badge";
import type { SecaoDoSite } from "@/lib/site/tipos";

/**
 * As seções do site, na ordem do catálogo, cada uma recolhível.
 *
 * `<details>` nativo: abre e fecha sem JavaScript, o teclado funciona de graça
 * e a busca do navegador (Ctrl+F) encontra texto dentro de seção fechada.
 */
export function EditorDoSite({ secoes, podeEditar }: { secoes: SecaoDoSite[]; podeEditar: boolean }) {
  return (
    <div className="flex flex-col gap-3">
      {/* Nenhum <Toaster/> está montado na casca (ver components/contatos/avisos.tsx). */}
      <Toaster
        position="top-center"
        richColors
        closeButton
        offset="calc(var(--app-chrome-top) + env(safe-area-inset-top))"
        toastOptions={{ classNames: { toast: "font-sans" } }}
      />
      {!podeEditar ? (
        <p className="rounded-xl border border-border/60 bg-muted/25 px-4 py-3 text-sm text-muted-foreground">
          Seu perfil pode ver o conteúdo do site, mas não alterar. Para mudar, peça à gestão.
        </p>
      ) : null}
      {secoes.map((secao) => (
        <SecaoDoEditor key={secao.key} secao={secao} podeEditar={podeEditar} />
      ))}
    </div>
  );
}

function SecaoDoEditor({ secao, podeEditar }: { secao: SecaoDoSite; podeEditar: boolean }) {
  const [editados, setEditados] = React.useState<Set<string>>(
    () => new Set(secao.fields.filter((f) => !f.is_default).map((f) => f.key)),
  );

  const aoMudarEstado = React.useCallback((chave: string, editado: boolean) => {
    setEditados((atual) => {
      const novo = new Set(atual);
      if (editado) novo.add(chave);
      else novo.delete(chave);
      return novo;
    });
  }, []);

  const n = secao.fields.length;
  return (
    // Sub-superfície dentro do panel-float: borda e fundo, sem sombra.
    <details className="group rounded-xl border border-border/60 bg-muted/20" data-secao={secao.key}>
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3 rounded-xl px-4 py-3.5 hover:bg-muted/40 sm:px-5 [&::-webkit-details-marker]:hidden">
        <span className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="font-display text-base leading-tight">{secao.label}</span>
          <span className="text-xs text-muted-foreground">
            {n} {n === 1 ? "item" : "itens"}
          </span>
          {editados.size > 0 ? (
            <Badge variant="accent">
              {editados.size} {editados.size === 1 ? "editado" : "editados"}
            </Badge>
          ) : null}
        </span>
        <ChevronDown className="size-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-180" aria-hidden="true" />
      </summary>
      <div className="px-4 pb-2 sm:px-5">
        {secao.fields.map((campo) => (
          <CampoDoSiteEditor key={campo.key} campo={campo} podeEditar={podeEditar} aoMudarEstado={aoMudarEstado} />
        ))}
      </div>
    </details>
  );
}
