import { describe, expect, it } from "vitest";

import {
  ESTADOS,
  holdVencido,
  podeCancelar,
  podeConfirmar,
  podeDescartar,
  podeFazerCheckIn,
  podeFazerCheckOut,
  podeRealocarUnidade,
  podeRemarcar,
} from "@/lib/reservas/estados";
import type { EstadoDaReserva, Reserva } from "@/lib/reservas/tipos";

/**
 * Os predicados de estado são a mesma resposta que a lista e o detalhe dão à
 * mesma pergunta. Responder diferente nos dois é pior do que não oferecer a
 * ação em um deles: o operador aprende a regra pela tela em que está e depois
 * erra na outra.
 *
 * **Isto não autoriza nada** — quem recusa a transição é a API, com
 * `409 INVALID_STATE_TRANSITION`. O que se mede aqui é não oferecer um botão
 * que já se sabe que vai falhar.
 */

function reserva(status: EstadoDaReserva, extras: Partial<Reserva> = {}): Reserva {
  return {
    id: "r-1",
    code: "WH-2026-0001",
    status,
    unit_type_id: "p-1",
    unit_type_name: "White House Cobertura",
    contact_id: "c-1",
    contact_name: "Hóspede de Demonstração",
    contact_phone_e164: null,
    broker_id: null,
    source: "direto",
    check_in: "2026-11-20",
    check_out: "2026-11-23",
    night_count: 3,
    guests_count: 4,
    is_event: false,
    event_type: null,
    subtotal_cents: 670_000,
    discount_pct: 0,
    discount_cents: 0,
    cleaning_cents: 35_000,
    event_deposit_cents: 0,
    total_cents: 705_000,
    deposit_cents: 352_500,
    balance_cents: 352_500,
    rate_table_id: "t-1",
    policy_version: 1,
    cancellation_policy_id: "pc-1",
    hold_expires_at: null,
    confirmed_at: null,
    cancelled_at: null,
    cancel_reason: null,
    rebooked_from_id: null,
    notes: null,
    created_at: "2026-08-27T15:31:48-03:00",
    updated_at: "2026-08-27T15:31:48-03:00",
    units: [{ unit_id: "u-1", unit_code: "COB-01", unit_name: "Cobertura", stay_block_id: "b-1", locked: false }],
    ...extras,
  };
}

describe("o ciclo de vida oferece uma ação por estado", () => {
  it("confirmar e estender só existem em pré-reserva", () => {
    expect(podeConfirmar(reserva("hold"))).toBe(true);
    expect(podeConfirmar(reserva("confirmed"))).toBe(false);
    expect(podeConfirmar(reserva("quote"))).toBe(false);
  });

  it("check-in exige confirmada, e check-out exige hospedado", () => {
    expect(podeFazerCheckIn(reserva("confirmed"))).toBe(true);
    expect(podeFazerCheckIn(reserva("hold"))).toBe(false);
    expect(podeFazerCheckOut(reserva("checked_in"))).toBe(true);
    expect(podeFazerCheckOut(reserva("confirmed"))).toBe(false);
  });

  it("remarcar vale só para hold e confirmada", () => {
    expect(podeRemarcar(reserva("hold"))).toBe(true);
    expect(podeRemarcar(reserva("confirmed"))).toBe(true);
    expect(podeRemarcar(reserva("checked_in"))).toBe(false);
    expect(podeRemarcar(reserva("cancelled"))).toBe(false);
  });

  /**
   * Produto `all_members` — a White House Completa — já ocupa todas as unidades
   * da composição: não há para onde mover. O painel não recebe `consumes` na
   * `Reserva`, e lê isso pelo que está alocado.
   */
  it("realocar exige calendário bloqueado e uma unidade só", () => {
    expect(podeRealocarUnidade(reserva("confirmed"))).toBe(true);
    expect(podeRealocarUnidade(reserva("checked_out"))).toBe(false);
    const casaInteira = reserva("confirmed", {
      units: [
        { unit_id: "u-1", unit_code: "AP-01", unit_name: "AP 01", stay_block_id: "b-1", locked: false },
        { unit_id: "u-2", unit_code: "AP-02", unit_name: "AP 02", stay_block_id: "b-2", locked: false },
      ],
    });
    expect(podeRealocarUnidade(casaInteira)).toBe(false);
  });

  it("cancelar vale em tudo que ainda não terminou; descartar, só no rascunho", () => {
    for (const status of ["quote", "hold", "confirmed", "checked_in"] as EstadoDaReserva[]) {
      expect(podeCancelar(reserva(status))).toBe(true);
    }
    for (const status of ["checked_out", "closed", "cancelled", "expired", "no_show"] as EstadoDaReserva[]) {
      expect(podeCancelar(reserva(status))).toBe(false);
      expect(ESTADOS[status].terminal).toBe(true);
    }
    expect(podeDescartar(reserva("quote"))).toBe(true);
    expect(podeDescartar(reserva("hold"))).toBe(false);
  });
});

/**
 * O bloqueio do calendário não é o estado comercial: é o status das linhas de
 * `stay_blocks`, e é ele que a constraint enxerga. A tabela abaixo é a mesma do
 * contrato, e existe para a tela não inventar que "cancelada" ainda segura data.
 */
describe("quem segura o calendário", () => {
  it("bloqueiam hold, confirmed e checked_in — e mais ninguém", () => {
    const bloqueiam = (Object.keys(ESTADOS) as EstadoDaReserva[]).filter(
      (estado) => ESTADOS[estado].bloqueiaCalendario,
    );
    expect(bloqueiam.sort()).toEqual(["checked_in", "confirmed", "hold"]);
  });
});

describe("holdVencido", () => {
  /**
   * A reserva continua sendo `hold` na leitura porque o job que expira roda de
   * tempos em tempos — a tela vê o intervalo entre o vencimento e a varredura.
   * Confirmar aí responde `409 HOLD_EXPIRED`, e a linha avisa antes do clique.
   */
  it("marca o prazo já vencido mesmo com a reserva ainda em hold", () => {
    const agora = new Date("2026-08-27T18:00:00-03:00");
    expect(holdVencido(reserva("hold", { hold_expires_at: "2026-08-27T12:00:00-03:00" }), agora)).toBe(true);
    expect(holdVencido(reserva("hold", { hold_expires_at: "2026-08-29T12:00:00-03:00" }), agora)).toBe(false);
  });

  it("não fala de prazo onde não há prazo", () => {
    expect(holdVencido(reserva("confirmed", { hold_expires_at: "2020-01-01T00:00:00-03:00" }))).toBe(false);
    expect(holdVencido(reserva("hold"))).toBe(false);
  });
});
