"use client";

import * as React from "react";
import { AlertTriangle, Lock } from "lucide-react";
import { Toaster, toast } from "sonner";

import { mensagemCrm, tituloCrm, type CodigoCrm } from "@/lib/crm/codigos";
import type { FalhaCrm } from "@/lib/crm/api";
import { cn } from "@/lib/utils";

/**
 * O canal de aviso do CRM: toast para o que falhou no meio de um gesto, faixa
 * fixa para o que impediu a tela de carregar.
 *
 * ## Por que o `Toaster` está aqui e não na casca
 *
 * `sonner` é a escolha da casa (docs/ui.md §7), mas **nenhum `<Toaster/>` está
 * montado** — nem em `app/layout.tsx`, nem no `DashboardShell`. Sem ele,
 * `toast.error()` é uma função que não desenha nada, e o rollback do kanban
 * desfaria o movimento sem dizer por quê: o card voltaria sozinho e o operador
 * concluiria que o sistema "não aceitou o arrasto".
 *
 * Montar aqui resolve para as telas do CRM sem tocar em arquivo de outro
 * agente. O lugar definitivo é a casca — está no relatório desta rodada —, e no
 * dia em que ele existir esta montagem sai e o `notificar()` continua igual.
 */
export function AvisosDoCrm() {
  return (
    <Toaster
      position="top-center"
      richColors
      closeButton
      // A barra superior flutua e come o topo; e no celular o notch come mais.
      offset="calc(var(--app-chrome-top) + env(safe-area-inset-top))"
      toastOptions={{ classNames: { toast: "font-sans" } }}
    />
  );
}

/**
 * Traduz a recusa e a mostra — **pelo código**, nunca pelo texto da API.
 *
 * `DATE_CONFLICT` ganha o detalhe do contrato (`details.unit_code`,
 * `details.period`) na descrição: na alta temporada é a recusa mais comum de
 * `/win`, e "a data já está ocupada" sem dizer qual unidade e qual intervalo
 * manda o operador abrir o mapa e procurar.
 */
export function notificar(falha: FalhaCrm): void {
  toast.error(tituloCrm(falha.code), {
    description: descricaoDaFalha(falha),
    duration: falha.code === "DATE_CONFLICT" ? 10_000 : 6_000,
  });
}

export function descricaoDaFalha(falha: FalhaCrm): string {
  const base = mensagemCrm(falha.code);
  const extra = detalheDoConflito(falha.details);
  return extra ? `${base} ${extra}` : base;
}

/**
 * `details.period` chega como o `daterange` do Postgres — `[2026-12-20,2026-12-23)`.
 * A tela não repete a notação do banco para quem vende: extrai as duas pontas e
 * escreve a frase. Formato inesperado devolve `null` em vez de um texto meio
 * traduzido — melhor dizer só o essencial do que mostrar colchete solto.
 */
export function detalheDoConflito(details: Record<string, unknown>): string | null {
  const unidade = typeof details.unit_code === "string" ? details.unit_code : null;
  const periodo = typeof details.period === "string" ? details.period : null;
  const datas = periodo ? [...periodo.matchAll(/\d{4}-\d{2}-\d{2}/g)].map((m) => m[0]) : [];

  if (unidade && datas.length === 2) {
    return `A unidade ${unidade} já está ocupada de ${brl(datas[0]!)} a ${brl(datas[1]!)}.`;
  }
  if (unidade) return `A unidade em conflito é a ${unidade}.`;
  if (datas.length === 2) return `O conflito é entre ${brl(datas[0]!)} e ${brl(datas[1]!)}.`;
  return null;
}

function brl(data: string): string {
  const [ano, mes, dia] = data.split("-");
  return `${dia}/${mes}/${ano}`;
}

export function notificarSucesso(titulo: string, descricao?: string): void {
  toast.success(titulo, descricao ? { description: descricao } : undefined);
}

/**
 * A falha que impediu a tela de carregar, com o **código à vista**.
 *
 * Mesmo papel do `EstadoDeErro` da casca, com o vocabulário do CRM por cima: os
 * sete códigos novos ainda não existem no espelho de `lib/api/codigos.ts`, e
 * `EstadoDeErro` só aceita o tipo de lá. Quando o espelho crescer, isto vira
 * uma chamada ao componente compartilhado.
 */
export function AvisoDeErro({
  code,
  titulo = "Não foi possível carregar",
  detalhe,
  className,
}: {
  code: CodigoCrm;
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
          <p className="mt-1 text-sm text-muted-foreground">{mensagemCrm(code)}</p>
          {detalhe ? <p className="mt-2 text-sm text-muted-foreground">{detalhe}</p> : null}
          <code className="mt-2 inline-block rounded-md bg-card px-2 py-0.5 font-mono text-xs text-muted-foreground">
            {code}
          </code>
        </div>
      </div>
    </div>
  );
}
