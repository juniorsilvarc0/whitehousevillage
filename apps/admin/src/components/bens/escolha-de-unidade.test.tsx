import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { EscolhaDeUnidade } from "@/components/bens/escolha-de-unidade";
import type { UnidadeDoInventario } from "@/lib/bens/tipos";

/**
 * O seletor de unidade vem de `GET /inventory/units`. O que mudou em relação a
 * derivá-lo dos ambientes: **a unidade sem ambiente aparece** — e é nela que
 * alguém precisa criar o primeiro cômodo —, e a conferência aberta leva direto
 * à contagem.
 */

const ABERTA = "9f1c1e2a-0d3b-4f6a-9a1e-2b7c5d8e0f11";

function unidade(parcial: Partial<UnidadeDoInventario> & Pick<UnidadeDoInventario, "id" | "code">): UnidadeDoInventario {
  return { name: `Apartamento ${parcial.code}`, active: true, rooms: 6, items: 40, ...parcial };
}

describe("EscolhaDeUnidade", () => {
  it("a unidade sem ambiente aparece, com o convite para criar o primeiro cômodo ou copiar", () => {
    render(<EscolhaDeUnidade unidades={[unidade({ id: "u2", code: "AP-02", rooms: 0, items: 0 })]} />);
    const cartao = screen.getByRole("listitem", { name: /AP-02/ });
    const convite = within(cartao).getByRole("link", { name: /crie o primeiro cômodo ou copie de outra unidade/i });
    expect(convite.getAttribute("href")).toBe("/app/inventario?unidade=u2");
  });

  it("a conferência aberta é um link direto para a contagem", () => {
    render(<EscolhaDeUnidade unidades={[unidade({ id: "u1", code: "AP-01", open_count_id: ABERTA })]} />);
    const link = screen.getByRole("link", { name: /conferência em andamento/i });
    expect(link.getAttribute("href")).toBe(`/app/inventario/conferencias/${ABERTA}`);
  });

  it("sem conferência aberta, não há o atalho; os números da unidade estão à vista", () => {
    render(<EscolhaDeUnidade unidades={[unidade({ id: "u1", code: "AP-01", rooms: 6, items: 40, last_closed_at: null })]} />);
    expect(screen.queryByRole("link", { name: /conferência em andamento/i })).toBeNull();
    const cartao = screen.getByRole("listitem", { name: /AP-01/ });
    expect(cartao.textContent).toMatch(/6 ambientes · 40 bens · nunca conferida/);
  });
});
