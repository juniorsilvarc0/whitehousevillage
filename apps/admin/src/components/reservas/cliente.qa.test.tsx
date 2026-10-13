import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { HospedeDaReserva, ReservaCompleta } from "@/lib/reservas/tipos";

import { DetalheDaReserva } from "./detalhe";

/**
 * QA do F2-22 — o cartão "Cliente" do detalhe da reserva.
 *
 * O defeito que abriu o F2-22 (spec §6, medido em 13/10/2026) foi o cartão
 * "Cliente" mostrar o nome e o telefone de quem JÁ NÃO ERA o cliente. A regra
 * que o fecha: "O titular tem uma fonte só: `reservations.contact_id`". O banco
 * garante que a linha marcada como titular, quando existe, é a do contato da
 * reserva; "ao menos um titular" é só da aplicação (db.md §5), e uma lista sem
 * a linha do titular é aceita pelo banco.
 *
 * Nesse caso o cartão não pode promover um ACOMPANHANTE a cliente: tem de cair
 * no contato da reserva (`contact_name`/`contact_id`), que é a fonte.
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

const CARLOS: HospedeDaReserva = {
  contact_id: "c-carlos",
  name: "Carlos Andrade",
  doc_type: "cpf",
  doc_number: "11144477735",
  phone_e164: "+5585988887777",
  email: "carlos@exemplo.invalid",
  is_lead_guest: true,
};

const BEATRIZ: HospedeDaReserva = {
  contact_id: "c-bia",
  name: "Beatriz Andrade",
  doc_type: "cpf",
  doc_number: "52998224725",
  phone_e164: "+5585977776666",
  email: "bia@exemplo.invalid",
  is_lead_guest: false,
};

function completo(guests: HospedeDaReserva[]): ReservaCompleta {
  return {
    reservation: {
      id: "r-1",
      code: "WH-2026-0042",
      status: "confirmed",
      unit_type_id: "7a1f0c2e-1111-4111-8111-111111111111",
      unit_type_name: "Apartamento",
      contact_id: "c-carlos",
      contact_name: "Carlos Andrade",
      contact_phone_e164: null,
      broker_id: null,
      source: "direto",
      check_in: "2026-11-20",
      check_out: "2026-11-23",
      night_count: 3,
      guests_count: 3,
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
    guests,
    guests_editable: true,
    timeline: [],
    cancellation_preview: null,
  };
}

const PERMISSOES = { editar: true, excluir: false, procurarPessoas: true, cadastrarPessoas: true };

function cartao() {
  return screen.getByRole("region", { name: "Cliente" });
}

describe("Cartão Cliente — o titular tem uma fonte só (spec §6, F2-22)", () => {
  it("com a linha do titular na lista, mostra o titular — e não o acompanhante", () => {
    render(<DetalheDaReserva completo={completo([CARLOS, BEATRIZ])} permissoes={PERMISSOES} produtos={[]} composicao={[]} />);
    expect(within(cartao()).getByText("Carlos Andrade")).toBeTruthy();
    expect(within(cartao()).queryByText("Beatriz Andrade")).toBeNull();
    expect(within(cartao()).getByRole("link", { name: "Ver ficha" }).getAttribute("href")).toBe("/app/contatos/c-carlos");
  });

  it("sem a linha do titular na lista, mostra o contato da reserva — nunca promove um acompanhante a cliente", () => {
    render(<DetalheDaReserva completo={completo([BEATRIZ])} permissoes={PERMISSOES} produtos={[]} composicao={[]} />);
    const texto = cartao().textContent ?? "";
    expect(texto).toContain("Carlos Andrade");
    expect(texto).not.toContain("Beatriz Andrade");
    expect(texto).not.toContain(BEATRIZ.email ?? "");
    expect(within(cartao()).getByRole("link", { name: "Ver ficha" }).getAttribute("href")).toBe("/app/contatos/c-carlos");
    for (const link of within(cartao()).queryAllByRole("link")) {
      expect(link.getAttribute("href") ?? "").not.toContain("5585977776666");
    }
  });
});
