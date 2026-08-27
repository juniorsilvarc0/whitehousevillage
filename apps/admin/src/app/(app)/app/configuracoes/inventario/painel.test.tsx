import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { Produto, Unidade } from "@/lib/api/comercial";

import { PainelDeInventario } from "./painel";

/**
 * O que se protege aqui é o **estado do modal entre duas aberturas**.
 *
 * Um modal de edição que fica montado (para não perder a animação de saída)
 * guarda o formulário da linha anterior. Se a reposição acontecer tarde — num
 * efeito que só roda depois de renderizar —, existe um quadro mostrando o
 * produto errado; e quem salva rápido salva o produto errado de verdade,
 * porque o `id` do envio vem do mesmo estado atrasado.
 *
 * Aqui a reposição acontece no **evento que abre**, e é isso que estes testes
 * medem: o que está no formulário é sempre a linha em que se clicou.
 */

vi.mock("./acoes", () => ({
  salvarProduto: vi.fn(async () => ({ ok: true as const, data: null })),
  salvarUnidade: vi.fn(async () => ({ ok: true as const, data: null })),
  salvarComposicao: vi.fn(async () => ({ ok: true as const, data: null })),
  desativarProduto: vi.fn(async () => ({ ok: true as const, data: null })),
  desativarUnidade: vi.fn(async () => ({ ok: true as const, data: null })),
}));

function produto(id: string, code: string, name: string, capacity: number): Produto {
  return {
    id,
    code,
    name,
    capacity,
    consumes: "one_member",
    cleaning_fee_cents: 25_000,
    description: null,
    sort_order: 0,
    active: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

const UNIDADES: Unidade[] = [
  {
    id: "u-ap01",
    code: "AP-01",
    name: "Apartamento 01",
    floor: "Térreo",
    notes: null,
    sort_order: 0,
    active: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
];

const PRODUTOS = [produto("p-ap2s", "AP2S", "Apartamento 2 Suítes", 4), produto("p-cob", "COB", "Cobertura", 8)];

function montar() {
  render(
    <PainelDeInventario
      produtos={PRODUTOS}
      unidades={UNIDADES}
      composicoes={{
        "p-ap2s": [{ unit_id: "u-ap01", unit_code: "AP-01", unit_name: "Apartamento 01", active: true }],
      }}
      permissoes={{ criar: true, editar: true, excluir: true }}
    />,
  );
}

const campo = (rotulo: string | RegExp) => screen.getByLabelText(rotulo) as HTMLInputElement;

async function abrirEdicaoDe(nome: string) {
  const cartao = screen.getByText(nome).closest("li")!;
  fireEvent.click(within(cartao).getByRole("button", { name: /editar/i }));
  await screen.findByRole("dialog");
}


describe("PainelDeInventario — modais reabertos", () => {
  it("abre a edição com os valores da linha clicada", async () => {
    montar();
    await abrirEdicaoDe("Apartamento 2 Suítes");

    expect(campo(/^Código/).value).toBe("AP2S");
    expect(campo(/^Capacidade/).value).toBe("4");
  });

  it("não carrega os valores do produto anterior para a abertura seguinte", async () => {
    montar();

    await abrirEdicaoDe("Apartamento 2 Suítes");
    // Rascunho abandonado: o usuário mexe e fecha sem salvar.
    fireEvent.change(campo(/^Capacidade/), { target: { value: "99" } });
    fireEvent.click(screen.getByRole("button", { name: "Cancelar" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    await abrirEdicaoDe("Cobertura");

    expect(campo(/^Código/).value).toBe("COB");
    expect(
      campo(/^Capacidade/).value,
      "capacidade de um produto no formulário de outro é venda com o número errado",
    ).toBe("8");
  });

  it("volta ao formulário em branco quando a abertura é para criar", async () => {
    montar();

    await abrirEdicaoDe("Cobertura");
    fireEvent.click(screen.getByRole("button", { name: "Cancelar" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: /novo produto/i }));
    await screen.findByRole("dialog");

    expect(campo(/^Código/).value).toBe("");
    expect(campo(/^Capacidade/).value, "o padrão de produto novo é 2 hóspedes, não o último editado").toBe("2");
  });

  it("marca a composição do produto em que se clicou", async () => {
    montar();

    const cartao = screen.getByText("Apartamento 2 Suítes").closest("li")!;
    fireEvent.click(within(cartao).getByRole("button", { name: /composição/i }));
    await screen.findByRole("dialog");

    expect((screen.getByRole("checkbox", { name: /AP-01/ }) as HTMLInputElement).checked).toBe(true);
  });
});
