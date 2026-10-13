"use client";

import { Toaster, toast } from "sonner";

import type { Falha } from "@/lib/acoes/resultado";
import { mensagemDaAgenda, type ContextoDaAgenda } from "@/lib/agenda/mensagens";
import { fraseDoAviso } from "@/lib/agenda/rotulos";
import type { AvisoDeAgenda } from "@/lib/agenda/tipos";

/**
 * O canal de aviso da agenda. O `<Toaster/>` é montado pelo layout do módulo, e
 * não pela casca, pelo mesmo motivo do inventário, da manutenção e do CRM:
 * nenhum está montado na casca.
 */
export function AvisosDaAgenda() {
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

export function notificarFalha(titulo: string, falha: Pick<Falha, "code" | "details">, contexto: ContextoDaAgenda): void {
  toast.error(titulo, { description: mensagemDaAgenda(falha, contexto), duration: 7_000 });
}

export function notificarSucesso(titulo: string, descricao?: string): void {
  toast.success(titulo, descricao ? { description: descricao } : undefined);
}

/**
 * O conflito de HORÁRIO depois do `201`/`200` — **aviso, não erro**: o evento
 * foi gravado e continua gravado (spec §9, "a operação sabe o que está
 * fazendo"). Fica na tela mais tempo que o sucesso comum, porque "o
 * responsável já tem outro compromisso" é o tipo de coisa que se lê duas vezes.
 *
 * A frase sai do `kind` (`fraseDoAviso`), nunca do `message` da API.
 */
export function notificarAvisosDeHorario(titulo: string, avisos: readonly AvisoDeAgenda[]): void {
  if (avisos.length === 0) return;
  toast.warning(titulo, {
    description: avisos.map(fraseDoAviso).join(" "),
    duration: 15_000,
  });
}
