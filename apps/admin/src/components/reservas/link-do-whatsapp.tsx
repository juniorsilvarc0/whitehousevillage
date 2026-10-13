import { MessageCircle } from "lucide-react";

import { buttonVariants } from "@/components/ui/button";
import { linkDoWhatsApp } from "@/lib/contatos/whatsapp";
import { cn } from "@/lib/utils";

/**
 * Abre a conversa do WhatsApp com o número do hóspede e uma primeira mensagem
 * citando o código da reserva. Número fora de E.164 não ganha botão.
 *
 * Mora fora de `detalhe.tsx` porque a lista de hóspedes é componente de
 * cliente: importar do detalhe (componente de servidor, que monta as ações da
 * reserva) arrastaria a tela inteira para o pacote do navegador.
 */
export function LinkDoWhatsApp({ telefone, texto, compacto }: { telefone: string; texto: string; compacto?: boolean }) {
  const href = linkDoWhatsApp(telefone, texto);
  if (!href) return null;
  if (compacto) {
    return (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="flex items-center gap-1 text-xs font-medium text-[#1f8f4e] hover:underline"
        aria-label="Abrir conversa no WhatsApp"
      >
        <MessageCircle className="size-3.5" aria-hidden="true" />
        WhatsApp
      </a>
    );
  }
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className={cn(buttonVariants({ size: "md" }), "bg-none bg-[#1f8f4e] text-white hover:brightness-110")}
    >
      <MessageCircle aria-hidden="true" />
      Chamar no WhatsApp
    </a>
  );
}
