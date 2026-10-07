"use client";

import { Toaster, toast } from "sonner";

import type { Falha } from "@/lib/acoes/resultado";
import { mensagemDeBens, type Contexto } from "@/lib/bens/mensagens";

/**
 * O canal de aviso do inventário de bens.
 *
 * O `<Toaster/>` é montado pelo layout do módulo, e não pela casca, pelo mesmo
 * motivo do CRM e de contatos (`components/crm/avisos.tsx`): **nenhum está
 * montado na casca**, e sem ele `toast.error()` não desenha nada — o rollback
 * de uma contagem desfaria o número sem dizer por quê.
 */
export function AvisosDoInventario() {
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
  toast.error(titulo, { description: mensagemDeBens(falha, contexto), duration: 7_000 });
}

export function notificarSucesso(titulo: string, descricao?: string): void {
  toast.success(titulo, descricao ? { description: descricao } : undefined);
}

export function notificarInfo(titulo: string, descricao?: string): void {
  toast.info(titulo, descricao ? { description: descricao, duration: 7_000 } : undefined);
}
