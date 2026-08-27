import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { SemaforoDeAlcada } from "@/components/comercial/semaforo-de-alcada";

/**
 * O semáforo é o componente de decisão do orçamento: é ele que diz ao vendedor
 * se o número que está prestes a sair da boca dele pode sair.
 *
 * O que se protege:
 *
 * 1. **As três faixas aparecem sempre.** Quem negocia precisa ver onde termina
 *    o próprio poder de decisão, não só onde está agora.
 * 2. **A faixa ativa é a certa nas bordas** — 5% ainda é da gestão, 10% ainda é
 *    aprovável, 10,01% já não é.
 * 3. **O servidor tem a última palavra.** Depois do cálculo vale o
 *    `discount_authority` que ele devolveu, mesmo que a tela esteja com limites
 *    assumidos por não alcançar a política vigente.
 */

const POLITICA = { auto: 5, aprovacao: 10 };

function faixaAtiva(): string | null {
  const ativa = document.querySelector('[data-ativa="true"]');
  return ativa?.getAttribute("data-alcada") ?? null;
}

describe("SemaforoDeAlcada", () => {
  it("mostra as três faixas, com os limites da política vigente", () => {
    render(<SemaforoDeAlcada pct={0} limites={POLITICA} />);

    expect(document.querySelectorAll("[data-alcada]")).toHaveLength(3);
    const texto = document.body.textContent ?? "";
    expect(texto).toContain("Pode fechar");
    expect(texto).toContain("Exige aprovação do proprietário");
    expect(texto).toContain("Não autorizado");
    // Os números são da política, não constantes do componente.
    expect(texto).toContain("até 5%");
    expect(texto).toContain("5% a 10%");
    expect(texto).toContain("acima de 10%");
  });

  it("fica verde até 5% inclusive", () => {
    render(<SemaforoDeAlcada pct={5} limites={POLITICA} />);
    expect(faixaAtiva(), "5% exato é alçada da gestão").toBe("gestao");
    expect(screen.getByRole("status").textContent).toContain("Pode fechar");
  });

  it("vira âmbar acima do teto automático e continua até 10% inclusive", () => {
    const { unmount } = render(<SemaforoDeAlcada pct={7} limites={POLITICA} />);
    expect(faixaAtiva()).toBe("proprietario");
    expect(screen.getByRole("status").textContent).toContain("aprovação");
    unmount();

    render(<SemaforoDeAlcada pct={10} limites={POLITICA} />);
    expect(faixaAtiva(), "10% exato ainda é aprovável pelo proprietário").toBe("proprietario");
  });

  it("fica vermelho acima do teto de aprovação e diz que nem aprovação resolve", () => {
    render(<SemaforoDeAlcada pct={10.5} limites={POLITICA} />);
    expect(faixaAtiva()).toBe("negado");
    expect(screen.getByRole("status").textContent).toContain("Não autorizado");
    expect(document.body.textContent).toContain("DISCOUNT_ABOVE_LIMIT");
  });

  it("acompanha limites diferentes sem depender dos números da spec", () => {
    render(<SemaforoDeAlcada pct={12} limites={{ auto: 8, aprovacao: 15 }} />);
    expect(faixaAtiva()).toBe("proprietario");
    expect(document.body.textContent).toContain("até 8%");
  });

  it("obedece à alçada que o servidor devolveu, quando ela discorda do cálculo local", () => {
    // O cálculo local diria "gestao" para 3%. Se o motor devolveu
    // "proprietario" — porque a política vigente é outra —, quem vale é ele:
    // a tela pode estar com limites assumidos.
    render(<SemaforoDeAlcada pct={3} limites={POLITICA} alcada="proprietario" />);
    expect(faixaAtiva()).toBe("proprietario");
    expect(screen.getByRole("status").textContent).toContain("3% de desconto");
  });

  it("anuncia a mudança de faixa para leitor de tela", () => {
    // O texto muda enquanto o controle é arrastado; sem `role="status"` quem
    // usa leitor de tela mudaria de faixa sem saber.
    render(<SemaforoDeAlcada pct={2} limites={POLITICA} />);
    expect(screen.getByRole("status")).toBeTruthy();
  });
});
