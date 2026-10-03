import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Falha } from "@/lib/acoes/resultado";

import { Recusa } from "./recusa";

/**
 * **`DATE_CONFLICT` na remarcação é o caso comum, não a exceção.**
 *
 * As duas mudanças da remarcação acontecem na mesma transação: se a data nova
 * estiver ocupada, nada muda e a reserva antiga continua de pé. Pintar isso de
 * vermelho de falha ensina o operador a temer o botão que ele mais usa em
 * dezembro — e a garantia ("nada foi alterado") é justamente o que ele precisa
 * ler para tentar outra data em vez de abrir chamado.
 *
 * O outro comportamento medido aqui é a regra da casa: a tela reage ao **código**
 * e escreve a própria frase. A mensagem que a API mandou nunca aparece.
 */

function falha(code: Falha["code"], details: Record<string, unknown> = {}): Falha {
  return { ok: false, code, message: "TEXTO CRU DA API QUE NÃO DEVE APARECER", details };
}

describe("Recusa", () => {
  it("conflito de data é informativo e traz a garantia de que nada mudou", () => {
    const { container } = render(
      <Recusa
        falha={falha("DATE_CONFLICT", { unit_code: "AP-03", period: "[2026-12-20,2026-12-23)" })}
        garantia="Nada foi alterado: WH-2026-0001 continua de pé."
      />,
    );

    expect(screen.getByText(/A unidade AP-03 já está ocupada de 20\/12\/2026 a 23\/12\/2026/)).toBeDefined();
    expect(screen.getByText(/Nada foi alterado: WH-2026-0001 continua de pé/)).toBeDefined();
    // O código fica para o suporte, fora do texto visível.
    const alerta = container.querySelector("[role=alert]")!;
    expect(alerta.getAttribute("data-codigo")).toBe("DATE_CONFLICT");
    expect(screen.queryByText("DATE_CONFLICT")).toBeNull();

    // Tom de aviso, não de falha: sem a borda destrutiva.
    expect(alerta.className).toContain("alcada-atencao");
    expect(alerta.className).not.toContain("border-destructive");
  });

  it("recusa de verdade continua vermelha, e a garantia não é oferecida", () => {
    const { container } = render(
      <Recusa falha={falha("HOLD_EXPIRED")} garantia="isto não deveria aparecer" />,
    );
    const alerta = container.querySelector("[role=alert]")!;
    expect(alerta.className).toContain("border-destructive");
    expect(screen.queryByText(/isto não deveria aparecer/)).toBeNull();
  });

  it("nunca ecoa o texto da API — a frase é a da casa, escolhida pelo código", () => {
    render(<Recusa falha={falha("UNIT_NOT_AVAILABLE", { unit_code: "AP-02" })} />);
    expect(screen.queryByText(/TEXTO CRU DA API/)).toBeNull();
    expect(screen.getByText(/O apartamento escolhido está ocupado nesse período/)).toBeDefined();
  });

  it("teto do sinal aparece em reais, com o motivo de existir", () => {
    render(<Recusa falha={falha("VALIDATION_ERROR", { field: "deposit_paid_cents", max_cents: 298_000 })} />);
    const texto = screen.getByText(/não pode passar do total/).textContent!.replace(/ /g, " ");
    expect(texto).toContain("R$ 2.980,00");
    expect(texto).toContain("base do reembolso");
  });

  it("limite de extensões diz quantas foram e devolve a decisão a uma pessoa", () => {
    render(<Recusa falha={falha("HOLD_LIMIT_REACHED", { extensions_count: 3, max_extensions: 3 })} />);
    expect(screen.getByText(/Já foram 3 de 3 extensões/)).toBeDefined();
  });

  it("composição incompleta nomeia a unidade a reativar", () => {
    render(
      <Recusa
        falha={falha("COMPOSITION_INCOMPLETE", {
          unit_type_code: "White House Completa",
          expected_units: 8,
          active_units: 7,
          missing_unit_codes: ["AP-03"],
        })}
      />,
    );
    expect(screen.getByText(/Reative a unidade AP-03 no inventário/)).toBeDefined();
  });
});
