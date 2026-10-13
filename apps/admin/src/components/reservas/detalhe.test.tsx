import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { Produto } from "@/lib/api/comercial";
import type { ReservaCompleta } from "@/lib/reservas/tipos";

import { DetalheDaReserva } from "./detalhe";

/**
 * A ligação entre o `/full` e a seção "Hóspedes": o detalhe repassa
 * `guests_editable` (a regra é do servidor, a tela não tem cópia) e a
 * capacidade do produto (do catálogo que a página já carrega). Sem estes casos,
 * um `editavel={true}` esquecido no detalhe passaria por todos os testes da
 * seção.
 */

vi.mock("@/lib/reservas/acoes", () => ({
  cancelarReserva: vi.fn(),
  confirmarReserva: vi.fn(),
  descartarReserva: vi.fn(),
  estenderHold: vi.fn(),
  realocarUnidade: vi.fn(),
  registrarCheckIn: vi.fn(),
  registrarCheckOut: vi.fn(),
  remarcarReserva: vi.fn(),
  simularCancelamento: vi.fn(),
}));
vi.mock("@/lib/reservas/acoes-de-hospedes", () => ({
  procurarPorDocumento: vi.fn(),
  acrescentarHospede: vi.fn(),
  cadastrarEAcrescentar: vi.fn(),
  retirarHospede: vi.fn(),
  aumentarHospedes: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh: vi.fn(), push: vi.fn(), replace: vi.fn() }),
}));

const PRODUTO = "7a1f0c2e-1111-4111-8111-111111111111";

function completo(guestsEditable: boolean, guestsCount = 3): ReservaCompleta {
  return {
    reservation: {
      id: "r-1",
      code: "WH-2026-0042",
      status: "confirmed",
      unit_type_id: PRODUTO,
      unit_type_name: "Apartamento",
      contact_id: "c-carlos",
      contact_name: "Carlos Andrade",
      contact_phone_e164: null,
      broker_id: null,
      source: "direto",
      check_in: "2026-11-20",
      check_out: "2026-11-23",
      night_count: 3,
      guests_count: guestsCount,
      is_event: false,
      event_type: null,
      subtotal_cents: 300_000,
      discount_pct: 0,
      discount_cents: 0,
      cleaning_cents: 20_000,
      event_deposit_cents: 0,
      total_cents: 320_000,
      deposit_cents: 160_000,
      balance_cents: 160_000,
      rate_table_id: null,
      policy_version: 1,
      cancellation_policy_id: null,
      hold_expires_at: null,
      confirmed_at: "2026-10-01T12:00:00-03:00",
      cancelled_at: null,
      cancel_reason: null,
      rebooked_from_id: null,
      notes: null,
      created_at: "2026-10-01T12:00:00-03:00",
      updated_at: "2026-10-01T12:00:00-03:00",
      units: [],
    },
    nights: [],
    lines: [],
    units: [],
    guests: [
      {
        contact_id: "c-carlos",
        name: "Carlos Andrade",
        doc_type: "cpf",
        doc_number: "11144477735",
        phone_e164: "+5585988887777",
        email: null,
        is_lead_guest: true,
      },
      {
        contact_id: "c-bia",
        name: "Beatriz Andrade",
        doc_type: null,
        doc_number: null,
        phone_e164: null,
        email: null,
        is_lead_guest: false,
      },
    ],
    guests_editable: guestsEditable,
    timeline: [],
    cancellation_preview: null,
  };
}

const PERMISSOES = { editar: true, excluir: false, procurarPessoas: true, cadastrarPessoas: true };

function produto(capacity: number): Produto {
  return {
    id: PRODUTO,
    code: "AP",
    name: "Apartamento",
    capacity,
    consumes: "one_member",
    cleaning_fee_cents: 20_000,
    description: null,
    sort_order: 1,
    active: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function secao() {
  return screen.getByRole("region", { name: "Hóspedes" });
}

describe("DetalheDaReserva → seção Hóspedes", () => {
  it("guests_editable verdadeiro: a seção oferece acrescentar e retirar", () => {
    render(<DetalheDaReserva completo={completo(true)} permissoes={PERMISSOES} produtos={[]} composicao={[]} />);
    expect(within(secao()).getByRole("button", { name: /Acrescentar hóspede/ })).toBeTruthy();
    expect(within(secao()).getByRole("button", { name: /Retirar Beatriz/ })).toBeTruthy();
  });

  it("guests_editable falso: nenhum dos dois, mesmo com a permissão", () => {
    render(<DetalheDaReserva completo={completo(false)} permissoes={PERMISSOES} produtos={[]} composicao={[]} />);
    expect(within(secao()).queryByRole("button", { name: /Acrescentar hóspede/ })).toBeNull();
    expect(within(secao()).queryByRole("button", { name: /Retirar/ })).toBeNull();
  });

  it("a capacidade do produto chega à seção: no máximo, não oferece aumentar", () => {
    const { unmount } = render(
      <DetalheDaReserva completo={completo(true, 2)} permissoes={PERMISSOES} produtos={[produto(4)]} composicao={[]} />,
    );
    expect(within(secao()).getByRole("button", { name: /Aumentar para 3 hóspedes/ })).toBeTruthy();
    unmount();

    render(<DetalheDaReserva completo={completo(true, 2)} permissoes={PERMISSOES} produtos={[produto(2)]} composicao={[]} />);
    expect(within(secao()).queryByRole("button", { name: /Aumentar para/ })).toBeNull();
  });
});
