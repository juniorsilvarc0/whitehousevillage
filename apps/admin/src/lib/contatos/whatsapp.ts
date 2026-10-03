import { ehE164 } from "@/lib/contatos/telefone";

/**
 * Link que abre a conversa do WhatsApp **com o número do contato** já
 * escolhido — `wa.me` quer só dígitos, com DDI e sem `+`.
 *
 * Devolve `null` para o que não é E.164: um link de WhatsApp para um número
 * mal gravado abre uma conversa com um desconhecido, que é pior do que não
 * oferecer o botão.
 */
export function linkDoWhatsApp(e164: string | null | undefined, texto?: string): string | null {
  if (!e164 || !ehE164(e164)) return null;
  const base = `https://wa.me/${e164.slice(1)}`;
  return texto ? `${base}?text=${encodeURIComponent(texto)}` : base;
}

/** Primeiro nome, para a saudação não soar como cobrança de cartório. */
export function primeiroNome(nome: string): string {
  return nome.trim().split(/\s+/)[0] ?? "";
}

/** A mensagem inicial que a gestão manda a partir da ficha da reserva. */
export function mensagemDaReserva(nome: string, codigo: string): string {
  const saudacao = primeiroNome(nome);
  return `Olá${saudacao ? `, ${saudacao}` : ""}! Aqui é da White House Village, sobre a sua reserva ${codigo}.`;
}
