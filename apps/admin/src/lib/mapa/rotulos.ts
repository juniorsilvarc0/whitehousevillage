import type { StatusDaCelulaSintetica } from "./tipos";

/**
 * O nome de cada estado **na tela**, que não é o nome no banco.
 *
 * A tradução é obrigatória e o contrato diz por quê: `checked_out` é o nome de
 * apresentação do bloco `completed`, e `maintenance`/`owner_hold`/`ota` vêm do
 * `source`, não do `status`. Traduzir num lugar só é o que impede a tela de
 * mostrar "completed" para a governanta.
 */
export const ROTULO_DO_STATUS: Readonly<Record<StatusDaCelulaSintetica, string>> = {
  livre: "Livre",
  hold: "Pré-reserva",
  confirmed: "Confirmada",
  checked_out: "Estadia cumprida",
  maintenance: "Manutenção",
  owner_hold: "Uso do proprietário",
  ota: "Reserva de canal",
  parcial: "Casa parcialmente ocupada",
};

/**
 * A frase que explica o estado. Vive junto do rótulo porque a legenda do mapa é
 * onde a equipe aprende o vocabulário — e porque `checked_out` continuar
 * desenhado é uma decisão que precisa estar escrita onde se olha.
 */
export const EXPLICACAO_DO_STATUS: Readonly<Record<StatusDaCelulaSintetica, string>> = {
  livre: "Sem bloqueio — a data está vendável.",
  hold: "Data segurada sem sinal. Expira sozinha e volta ao estoque.",
  confirmed: "Sinal recebido. É a venda firme.",
  checked_out: "A estadia aconteceu e terminou; a data já é vendável de novo.",
  maintenance: "Bloqueio operacional. Não é venda e não gera receita.",
  owner_hold: "Uso da casa pelos proprietários.",
  ota: "Veio de canal externo. Alterações são feitas no canal, não aqui.",
  parcial: "Alguma unidade está ocupada — a casa inteira não pode ser vendida neste dia.",
};

/** O texto curto que cabe dentro da barra, quando não há reserva para nomear. */
export function textoDaFaixa(
  status: StatusDaCelulaSintetica,
  reservationCode: string | null,
  guestName: string | null,
): string {
  if (reservationCode && guestName) return `${reservationCode} · ${guestName}`;
  if (reservationCode) return reservationCode;
  if (guestName) return guestName;
  return ROTULO_DO_STATUS[status];
}

/** O que o `source` do bloco significa para quem opera. Só onde há selo. */
export function seloDaFaixa(status: StatusDaCelulaSintetica): string | null {
  if (status === "ota") return "OTA";
  if (status === "owner_hold") return "PROP";
  return null;
}
