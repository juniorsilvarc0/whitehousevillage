import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { InventarioDaUnidadeView } from "@/components/bens/unidade";
import { descricaoDoRecorte } from "@/lib/bens/filtros";
import type { InventarioDaUnidade } from "@/lib/bens/tipos";

/**
 * Os `totals` de `GET /units/{id}/inventory` somam **o que a resposta mostra**:
 * com filtro, "120 peças" é a louça, não o apartamento. A tela tem de dizer que
 * o número é do recorte — e calar quando não há filtro.
 */

vi.mock("sonner", () => ({ Toaster: () => null, toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() } }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn(), refresh: vi.fn(), replace: vi.fn() }) }));
vi.mock("@/lib/bens/acoes", () => ({}));

const INVENTARIO: InventarioDaUnidade = {
  unit: { id: "u1", code: "AP-01", name: "Duplex 01" },
  rooms: [{ id: "r1", unit_id: "u1", name: "Cozinha", kind: "cozinha", sort_order: 0, active: true, items: [] }],
  totals: { rooms: 1, items: 8, expected_qty: 120, open_issues: 0 },
};

const LER = { criar: false, editar: false, excluir: false };

describe("os totais da unidade e o recorte", () => {
  it("com filtro, a legenda diz que os números são do recorte", () => {
    const recorte = descricaoDoRecorte({ q: "", categoria: "louca", inativos: false });
    render(<InventarioDaUnidadeView inventario={INVENTARIO} catalogo={[]} unidades={[]} permissoes={LER} filtrado recorte={recorte} />);
    expect(screen.getByTestId("legenda-do-recorte").textContent).toMatch(/categoria Louça.*não do apartamento inteiro/);
    expect(screen.getByText("Valor cotado do recorte")).toBeTruthy();
  });

  it("sem filtro, não há legenda — o número é o do apartamento", () => {
    render(<InventarioDaUnidadeView inventario={INVENTARIO} catalogo={[]} unidades={[]} permissoes={LER} filtrado={false} />);
    expect(screen.queryByTestId("legenda-do-recorte")).toBeNull();
    expect(screen.getByText("Valor do enxoval cotado")).toBeTruthy();
  });
});

describe("descricaoDoRecorte", () => {
  it("diz em palavras cada filtro ativo, e nada sem filtro", () => {
    expect(descricaoDoRecorte({ q: "", categoria: "", inativos: false })).toBeNull();
    expect(descricaoDoRecorte({ q: "taça", categoria: "copo", inativos: true })).toBe(
      "busca “taça”, categoria Copos e taças, incluindo desativados",
    );
  });
});
