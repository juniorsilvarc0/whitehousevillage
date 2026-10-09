"use client";

import * as React from "react";
import { Toaster, toast } from "sonner";

import type { Falha } from "@/lib/acoes/resultado";
import { mensagemDeManutencao, type Contexto } from "@/lib/manutencao/mensagens";

/**
 * O canal de aviso das ordens de manutenção.
 *
 * O `<Toaster/>` é montado pelo layout do módulo, e não pela casca, pelo mesmo
 * motivo do inventário e do CRM: nenhum está montado na casca. Na tela de
 * avarias quem desenha é o `<Toaster/>` do inventário — o `toast` do sonner é
 * global e cai no que estiver montado.
 */
export function AvisosDaManutencao() {
  return (
    <Toaster
      position="top-center"
      richColors
      closeButton
      offset="calc(var(--app-chrome-top) + env(safe-area-inset-top))"
      toastOptions={{ classNames: { toast: "font-sans" } }}
    />
  );
}

export function notificarFalha(titulo: string, falha: Pick<Falha, "code" | "details">, contexto?: Contexto): void {
  toast.error(titulo, { description: mensagemDeManutencao(falha, contexto), duration: 7_000 });
}

export function notificarSucesso(titulo: string, descricao?: string, acao?: React.ReactNode): void {
  toast.success(titulo, {
    ...(descricao ? { description: descricao } : {}),
    ...(acao ? { action: acao, duration: 7_000 } : {}),
  });
}

export function notificarInfo(titulo: string, descricao?: string, acao?: React.ReactNode): void {
  toast.info(titulo, { description: descricao, duration: 9_000, ...(acao ? { action: acao } : {}) });
}
