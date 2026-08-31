"use client";

import * as React from "react";
import { AlertTriangle, Lock } from "lucide-react";
import { Toaster, toast } from "sonner";

import { mensagemDeContato, tituloDeContato, type CodigoContato } from "@/lib/contatos/codigos";
import type { FalhaDeContato } from "@/lib/contatos/api";
import { cn } from "@/lib/utils";

/**
 * O canal de aviso do cadastro: toast para o que falhou no meio de um gesto,
 * faixa fixa para o que impediu a tela de carregar.
 *
 * O `<Toaster/>` está montado aqui, e não na casca, pelo mesmo motivo que o do
 * CRM (`components/crm/avisos.tsx`): **nenhum está montado na casca** — nem em
 * `app/layout.tsx`, nem no `DashboardShell` —, e sem ele `toast.error()` é uma
 * função que não desenha nada. O lugar definitivo é a casca; está no relatório,
 * e no dia em que existir os dois saem daqui sem mudar o `notificar()`.
 */
export function AvisosDeContatos() {
  return (
    <Toaster
      position="top-center"
      richColors
      closeButton
      // A barra superior flutua e come o topo; no celular o notch come mais.
      offset="calc(var(--app-chrome-top) + env(safe-area-inset-top))"
      toastOptions={{ classNames: { toast: "font-sans" } }}
    />
  );
}

export function notificar(falha: FalhaDeContato): void {
  toast.error(tituloDeContato(falha.code), {
    description: mensagemDeContato(falha.code),
    duration: 6_000,
  });
}

export function notificarSucesso(titulo: string, descricao?: string): void {
  toast.success(titulo, descricao ? { description: descricao } : undefined);
}

/**
 * A falha que impediu a tela de carregar, com o **código à vista**.
 *
 * Mesmo papel do `EstadoDeErro` da casca, com o vocabulário de contatos por
 * cima: `CONTACT_DUPLICATE` e `CONTACT_ANONYMIZED` ainda não existem no espelho
 * de `lib/api/codigos.ts`, e `EstadoDeErro` só aceita o tipo de lá.
 *
 * O código aparece na tela de propósito: nesta fase o módulo de contatos da API
 * está sendo escrito em paralelo, e a diferença entre "a rota ainda não existe"
 * (`NOT_FOUND`), "o seu perfil não alcança" (`FORBIDDEN`) e "a API caiu"
 * (`NETWORK_ERROR`) é a diferença entre esperar, pedir permissão e chamar
 * alguém.
 */
export function AvisoDeErro({
  code,
  titulo = "Não foi possível carregar",
  detalhe,
  className,
}: {
  code: CodigoContato;
  titulo?: string;
  detalhe?: React.ReactNode;
  className?: string;
}) {
  const semPermissao = code === "FORBIDDEN";
  const Icone = semPermissao ? Lock : AlertTriangle;
  return (
    <div
      role="alert"
      className={cn(
        "rounded-xl border px-5 py-4",
        semPermissao ? "border-border/60 bg-muted/25" : "border-destructive/30 bg-destructive/8",
        className,
      )}
    >
      <div className="flex items-start gap-3">
        <Icone
          className={cn("mt-0.5 size-5 shrink-0", semPermissao ? "text-muted-foreground" : "text-destructive")}
          aria-hidden="true"
        />
        <div className="min-w-0">
          <h3 className="font-display text-base text-foreground">{titulo}</h3>
          <p className="mt-1 text-sm text-muted-foreground">{mensagemDeContato(code)}</p>
          {detalhe ? <p className="mt-2 text-sm text-muted-foreground">{detalhe}</p> : null}
          <code className="mt-2 inline-block rounded-md bg-card px-2 py-0.5 font-mono text-xs text-muted-foreground">
            {code}
          </code>
        </div>
      </div>
    </div>
  );
}
