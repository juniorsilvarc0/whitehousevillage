import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { EventoDaReserva } from "@/lib/reservas/tipos";

import { LinhaDoTempo } from "./linha-do-tempo";

/**
 * Os eventos da rooming list (F2-22) na linha do tempo.
 *
 * `guest_added` e `guest_removed` carregam `{contact_id}` e nada mais: a linha
 * do tempo diz QUE a lista mudou e quem mexeu, nunca quem entrou ou saiu. A
 * tela segue a mesma regra — rótulo em português, e o id não vira texto.
 */

const CONTATO = "3b2d6f7a-1c4e-4a8b-9d0f-5e6a7b8c9d01";

function evento(type: string, payload: Record<string, unknown> | null): EventoDaReserva {
  return { id: `e-${type}`, type, payload, actor_id: "u-1", at: "2026-10-13T15:00:00-03:00" };
}

describe("LinhaDoTempo — a lista de hóspedes", () => {
  it("acrescentar e retirar têm rótulo, e o contact_id não aparece", () => {
    const { container } = render(
      <LinhaDoTempo
        eventos={[evento("guest_added", { contact_id: CONTATO }), evento("guest_removed", { contact_id: CONTATO })]}
      />,
    );
    expect(screen.getByText("Hóspede acrescentado à lista")).toBeTruthy();
    expect(screen.getByText("Hóspede retirado da lista")).toBeTruthy();
    expect(container.textContent).not.toContain(CONTATO);
    expect(container.textContent).not.toContain("guest_added");
  });

  it("o aumento do número de hóspedes fica no `updated`, com o número que ficou", () => {
    render(<LinhaDoTempo eventos={[evento("updated", { guests_count: 7, source: "direto" })]} />);
    expect(screen.getByText("Dados da reserva alterados")).toBeTruthy();
    expect(screen.getByText(/hóspedes 7/)).toBeTruthy();
  });

  it("tipo que a tela não conhece continua aparecendo, com o próprio nome", () => {
    render(<LinhaDoTempo eventos={[evento("tipo_novo_do_servidor", null)]} />);
    expect(screen.getByText("tipo_novo_do_servidor")).toBeTruthy();
  });
});
