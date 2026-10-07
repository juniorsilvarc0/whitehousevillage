import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { LinhaDaContagem } from "@/components/bens/linha-da-contagem";

/**
 * A linha da conferência no celular. O que se protege é a diferença entre
 * **não contado** (`null`) e **contei e não achei** (`0`): na tela, e no que
 * cada toque grava. Fechar uma conferência que confundiu os dois transforma
 * "não olhei" numa perda cobrada de um hóspede.
 */

function linha(contada: number | null, esperada = 12) {
  return {
    id: "l1",
    item_name: "Taça de vinho",
    item_description: null,
    item_unit_measure: "un" as const,
    cover: null,
    expected_qty: esperada,
    counted_qty: contada,
  };
}

function montar(contada: number | null, esperada = 12) {
  const aoMudar = vi.fn();
  render(<LinhaDaContagem linha={linha(contada, esperada)} podeContar aoMudar={aoMudar} />);
  return { aoMudar, campo: screen.getByLabelText("Quantidade contada de Taça de vinho") as HTMLInputElement };
}

describe("null e zero não se parecem", () => {
  it("pendente: traço no lugar do número, etiqueta 'Não contado', moldura de pendente", () => {
    const { campo } = montar(null);
    expect(campo.value, "pendente não pode exibir número nenhum").toBe("");
    expect(campo.placeholder).toBe("—");
    expect(screen.getByText("Não contado")).toBeTruthy();
    expect(screen.getByRole("article").dataset.estado).toBe("pendente");
  });

  it("zero: o número 0 à vista, etiqueta de falta, moldura de falta", () => {
    const { campo } = montar(0);
    expect(campo.value).toBe("0");
    expect(screen.getByText("Nenhum encontrado (falta 12)")).toBeTruthy();
    expect(screen.getByRole("article").dataset.estado).toBe("falta");
    expect(screen.queryByText("Não contado")).toBeNull();
  });

  it("as duas linhas não têm nenhum rótulo em comum", () => {
    const { unmount } = render(<LinhaDaContagem linha={linha(null)} podeContar aoMudar={vi.fn()} />);
    const pendente = screen.getByRole("article").textContent;
    unmount();
    render(<LinhaDaContagem linha={linha(0)} podeContar aoMudar={vi.fn()} />);
    const zero = screen.getByRole("article").textContent;
    expect(pendente).not.toBe(zero);
  });
});

describe("cada gesto grava o que diz", () => {
  it("'Confere' grava o esperado", () => {
    const { aoMudar } = montar(null);
    fireEvent.click(screen.getByRole("button", { name: /conferido igual ao esperado/i }));
    expect(aoMudar).toHaveBeenCalledWith(12);
  });

  it("o − numa linha pendente parte do esperado (11), nunca vira 0", () => {
    const { aoMudar } = montar(null);
    fireEvent.click(screen.getByRole("button", { name: "Um a menos de Taça de vinho" }));
    expect(aoMudar).toHaveBeenCalledWith(11);
  });

  it("o + numa linha contada soma ao contado", () => {
    const { aoMudar } = montar(3);
    fireEvent.click(screen.getByRole("button", { name: "Um a mais de Taça de vinho" }));
    expect(aoMudar).toHaveBeenCalledWith(4);
  });

  it("'Desfazer' devolve a linha a pendente (null), e só existe em linha contada", () => {
    const { aoMudar } = montar(0);
    fireEvent.click(screen.getByRole("button", { name: /desfazer a contagem/i }));
    expect(aoMudar).toHaveBeenCalledWith(null);
  });

  it("linha pendente não oferece 'Desfazer'", () => {
    montar(null);
    expect(screen.queryByRole("button", { name: /desfazer/i })).toBeNull();
  });

  it("apagar o campo para redigitar não grava nada — vazio não é zero nem desfazer", () => {
    const { aoMudar, campo } = montar(7);
    fireEvent.focus(campo);
    fireEvent.change(campo, { target: { value: "" } });
    expect(aoMudar).not.toHaveBeenCalled();
    fireEvent.change(campo, { target: { value: "0" } });
    expect(aoMudar).toHaveBeenCalledWith(0);
  });
});

describe("sem permissão de contar", () => {
  it("mostra o contado e nenhum controle", () => {
    render(<LinhaDaContagem linha={linha(null)} podeContar={false} aoMudar={vi.fn()} />);
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText("—")).toBeTruthy();
  });
});
