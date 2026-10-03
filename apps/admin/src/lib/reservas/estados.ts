import type { EstadoDaReserva, Reserva } from "@/lib/reservas/tipos";

/**
 * O vocabulário de estados da reserva, dito em português — e **quais ações
 * cabem em cada um**.
 *
 * Vive num módulo puro (sem React, sem `next/headers`) por dois motivos: a lista
 * e o detalhe precisam responder à mesma pergunta e não podem responder
 * diferente, e a regra "check-out só depois de check-in" é testável em segundos
 * aqui e cara de testar dentro de um componente.
 *
 * **Isto não autoriza nada.** Quem recusa a transição é a API, com
 * `409 INVALID_STATE_TRANSITION`; o que se ganha aqui é não oferecer um botão
 * que já se sabe que vai falhar.
 */

export type DescricaoDeEstado = {
  rotulo: string;
  /** A frase que explica o estado para quem vende, não para quem depura. */
  explicacao: string;
  /** `true` quando as linhas de `stay_blocks` da reserva seguram o calendário. */
  bloqueiaCalendario: boolean;
  /** Estado do qual a reserva não sai mais. */
  terminal: boolean;
  tom: "aberto" | "vivo" | "encerrado" | "perdido";
};

export const ESTADOS: Record<EstadoDaReserva, DescricaoDeEstado> = {
  quote: {
    rotulo: "Rascunho",
    explicacao:
      "Reserva em rascunho: está anotada, mas não guarda as datas. Ainda não é uma pré-reserva.",
    bloqueiaCalendario: false,
    terminal: false,
    tom: "aberto",
  },
  hold: {
    rotulo: "Pré-reserva",
    explicacao:
      "As datas estão guardadas por um prazo. Se o sinal não for pago até lá, a pré-reserva vence e as datas voltam a ficar livres.",
    bloqueiaCalendario: true,
    terminal: false,
    tom: "aberto",
  },
  confirmed: {
    rotulo: "Confirmada",
    explicacao: "Sinal recebido. As datas ficam garantidas, sem prazo para vencer.",
    bloqueiaCalendario: true,
    terminal: false,
    tom: "vivo",
  },
  checked_in: {
    rotulo: "Hospedado",
    explicacao: "O hóspede está na casa. As datas continuam ocupadas até o check-out.",
    bloqueiaCalendario: true,
    terminal: false,
    tom: "vivo",
  },
  checked_out: {
    rotulo: "Estadia cumprida",
    explicacao:
      "O hóspede saiu e os apartamentos já estão livres para venda. A estadia continua visível no mapa como histórico.",
    bloqueiaCalendario: false,
    terminal: true,
    tom: "encerrado",
  },
  closed: {
    rotulo: "Encerrada",
    explicacao: "Contas acertadas e caução devolvida. Este passo é feito no financeiro.",
    bloqueiaCalendario: false,
    terminal: true,
    tom: "encerrado",
  },
  cancelled: {
    rotulo: "Cancelada",
    explicacao: "As datas foram liberadas e o histórico ficou guardado. As regras de cancelamento da época da reserva já foram aplicadas.",
    bloqueiaCalendario: false,
    terminal: true,
    tom: "perdido",
  },
  expired: {
    rotulo: "Expirada",
    explicacao: "A pré-reserva venceu sem o pagamento do sinal e as datas foram liberadas. Para vender de novo, crie outra reserva.",
    bloqueiaCalendario: false,
    terminal: true,
    tom: "perdido",
  },
  no_show: {
    rotulo: "Não compareceu",
    explicacao:
      "O hóspede não apareceu. Fica separado de “cancelada” porque, nos relatórios, quem avisa e quem some sem avisar são casos diferentes.",
    bloqueiaCalendario: false,
    terminal: true,
    tom: "perdido",
  },
};

/** Os estados que a lista oferece como recorte, na ordem do ciclo de vida. */
export const ESTADOS_EM_ORDEM: EstadoDaReserva[] = [
  "quote",
  "hold",
  "confirmed",
  "checked_in",
  "checked_out",
  "closed",
  "cancelled",
  "expired",
  "no_show",
];

export function ehEstadoDeReserva(valor: string): valor is EstadoDaReserva {
  return Object.prototype.hasOwnProperty.call(ESTADOS, valor);
}

export function rotuloDoEstado(estado: EstadoDaReserva): string {
  return ESTADOS[estado].rotulo;
}

// ── Que ação cabe em que estado ────────────────────────────────────────────
//
// Cada predicado espelha uma linha do contrato. O comentário diz QUAL, para que
// a divergência entre tela e API seja encontrável pelo texto.

/** `POST /confirm`: `hold → confirmed`. */
export function podeConfirmar(r: Reserva): boolean {
  return r.status === "hold";
}

/** `POST /extend-hold`: só reserva em `hold`. */
export function podeEstenderHold(r: Reserva): boolean {
  return r.status === "hold";
}

/** `POST /check-in`: só reserva `confirmed`. */
export function podeFazerCheckIn(r: Reserva): boolean {
  return r.status === "confirmed";
}

/** `POST /check-out`: só reserva `checked_in`. */
export function podeFazerCheckOut(r: Reserva): boolean {
  return r.status === "checked_in";
}

/** `POST /reschedule`: só `hold` e `confirmed` remarcam. */
export function podeRemarcar(r: Reserva): boolean {
  return r.status === "hold" || r.status === "confirmed";
}

/**
 * `POST /reassign-unit`: precisa de calendário bloqueado, e produto
 * `all_members` não realoca — ele já ocupa todas as unidades da composição.
 *
 * O painel não recebe `consumes` dentro de `Reserva`, então a leitura é pelo
 * que está alocado: reserva com mais de uma unidade **é** a casa inteira, e não
 * há para onde mover. É proxy, não adivinhação — o contrato define
 * `all_members` como "insere as oito".
 */
export function podeRealocarUnidade(r: Reserva): boolean {
  return ESTADOS[r.status].bloqueiaCalendario && r.units.length === 1;
}

/** `POST /cancel`: recusa em `cancelled`, `expired`, `no_show`, `checked_out` e `closed`. */
export function podeCancelar(r: Reserva): boolean {
  return !ESTADOS[r.status].terminal;
}

/** `DELETE /reservations/{id}`: só apaga o que nunca existiu comercialmente. */
export function podeDescartar(r: Reserva): boolean {
  return r.status === "quote";
}

/**
 * Pré-reserva com prazo já vencido.
 *
 * Continua sendo `hold` na leitura porque o job que expira roda de tempos em
 * tempos — a tela vê o intervalo entre o vencimento e a varredura. Confirmar
 * aqui responde `409 HOLD_EXPIRED`, e é por isso que a linha avisa antes de o
 * operador clicar.
 */
export function holdVencido(r: Reserva, agora: Date = new Date()): boolean {
  if (r.status !== "hold" || !r.hold_expires_at) return false;
  const prazo = Date.parse(r.hold_expires_at);
  return Number.isFinite(prazo) && prazo <= agora.getTime();
}

// A contagem regressiva em si NÃO mora aqui: é `expiracaoDeHold`
// (`lib/mapa/expiracao.ts`), a mesma que o mapa usa. Duplicá-la daria duas
// frases para o mesmo prazo, e a operação deixaria de confiar nas duas.

/**
 * O motivo de cancelamento como a tela fala. O contrato grava o código do
 * catálogo (`desistencia`, `no_show`…) ou, em "outro", o texto livre — que
 * passa como veio.
 */
const MOTIVO_DE_CANCELAMENTO: Record<string, string> = {
  desistencia: "desistência do hóspede",
  no_show: "não compareceu",
  alteracao_de_planos: "alteração de planos",
  problema_de_pagamento: "problema de pagamento",
  erro_de_lancamento: "erro de lançamento",
  remarcacao: "remarcação",
  outro: "outro",
};

export function rotuloDoMotivo(motivo: string): string {
  return MOTIVO_DE_CANCELAMENTO[motivo] ?? motivo;
}
