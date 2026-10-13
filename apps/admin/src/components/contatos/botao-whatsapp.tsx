import { MessageCircle, Phone } from "lucide-react";

import { buttonVariants } from "@/components/ui/button";
import { linkDeLigacao } from "@/lib/contatos/ligacao";
import { formatarTelefone } from "@/lib/contatos/telefone";
import { linkDoWhatsApp, mensagemDaReserva } from "@/lib/contatos/whatsapp";
import { cn } from "@/lib/utils";

const VERDE = "bg-none bg-[#1f8f4e] text-white hover:brightness-110";

/**
 * Abre a conversa do WhatsApp com o número do cliente e uma primeira mensagem
 * citando o código da reserva. Número fora do formato internacional não ganha
 * botão — abrir conversa com um número errado é pior do que não oferecer.
 *
 * `tamanho`: `grande` é o botão do cartão do cliente; `pequeno`, o das listas.
 */
export function BotaoWhatsApp({
  telefone,
  nome,
  codigo,
  tamanho = "pequeno",
}: {
  telefone: string | null | undefined;
  nome: string;
  codigo: string;
  tamanho?: "grande" | "pequeno";
}) {
  const href = linkDoWhatsApp(telefone, mensagemDaReserva(nome, codigo));
  if (!href) return null;
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={`Chamar ${nome} no WhatsApp`}
      className={cn(buttonVariants({ size: tamanho === "grande" ? "md" : "sm" }), VERDE)}
    >
      <MessageCircle aria-hidden="true" />
      {tamanho === "grande" ? "Chamar no WhatsApp" : "WhatsApp"}
    </a>
  );
}

/**
 * O telefone formatado, clicável para ligar. Nada quando não há telefone.
 *
 * Só vira link o que é E.164 (`linkDeLigacao`): valor mascarado
 * (`+*********0000`, que é o que toda coleção de contato devolve) aparece como
 * texto. Um `tel:` de asteriscos foi o defeito da D11 no card do lead.
 */
export function TelefoneClicavel({ telefone, className }: { telefone: string | null | undefined; className?: string }) {
  if (!telefone) return null;
  const href = linkDeLigacao(telefone);
  if (!href) {
    return (
      <span className={cn("inline-flex items-center gap-1.5 font-mono", className)}>
        <Phone className="size-3.5" aria-hidden="true" />
        {telefone}
      </span>
    );
  }
  return (
    <a
      href={href}
      className={cn("inline-flex items-center gap-1.5 font-mono hover:text-foreground", className)}
    >
      <Phone className="size-3.5" aria-hidden="true" />
      {formatarTelefone(telefone)}
    </a>
  );
}
