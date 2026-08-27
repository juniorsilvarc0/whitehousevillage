import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { QuoteBuilder, type ProdutoParaOrcar } from "@/components/comercial/quote-builder";
import type { Orcamento } from "@/lib/api/comercial";
import type { Resultado } from "@/lib/acoes/resultado";
import type { LimitesDeAlcada } from "@/lib/comercial/alcada";
import type { OrcamentoFormulario } from "@/lib/comercial/orcamento";

/**
 * O `QuoteBuilder` é a tela que decide **quando** perguntar o preço ao servidor
 * e **o que** mostrar enquanto a resposta não chega. O que se protege aqui:
 *
 * 1. **O que está na tela corresponde ao formulário de agora.** Validação e
 *    recorte do resultado são calculados no render; enquanto eram estado
 *    escrito de dentro de um efeito, existia sempre um quadro mostrando o
 *    número (ou o erro) do valor anterior.
 * 2. **Ninguém fica esperando para sempre.** Requisição cancelada não é
 *    sucedida por outra quando o formulário fica inválido — e o indicador de
 *    "calculando" precisa desligar mesmo assim.
 * 3. **Acima do teto não vira requisição.** O servidor responderia
 *    `422 DISCOUNT_ABOVE_LIMIT`, e mandar assim mesmo ensina o vendedor a
 *    tratar a recusa como ruído.
 */

const PRODUTOS: ProdutoParaOrcar[] = [
  { id: "p-cob", code: "COB", name: "Cobertura", capacity: 8, cleaning_fee_cents: 45_000 },
];

const LIMITES: LimitesDeAlcada = { auto: 5, aprovacao: 10 };

const INICIAL: OrcamentoFormulario = {
  unit_type_id: "p-cob",
  check_in: "2030-09-10",
  check_out: "2030-09-13",
  guests_count: "4",
  discount_pct: "0",
  is_event: false,
};

const ORCAMENTO: Orcamento = {
  night_count: 3,
  subtotal_cents: 570_000,
  discount_pct: 0,
  discount_cents: 0,
  cleaning_cents: 45_000,
  event_deposit_cents: 0,
  total_cents: 615_000,
  deposit_cents: 184_500,
  balance_cents: 430_500,
  avg_nightly_cents: 190_000,
  min_nights: 2,
  discount_authority: "gestao",
  policy_version: 3,
  rate_table_id: "t1",
  lines: [{ date_type: "normal", label: "Normal", nights: 3, unit_price_cents: 190_000, subtotal_cents: 570_000 }],
  nights: [
    { date: "2030-09-10", date_type: "normal", label: "Normal", price_cents: 190_000 },
    { date: "2030-09-11", date_type: "normal", label: "Normal", price_cents: 190_000 },
    { date: "2030-09-12", date_type: "normal", label: "Normal", price_cents: 190_000 },
  ],
};

function montar(aoCalcular: (v: OrcamentoFormulario) => Promise<Resultado<Orcamento>>) {
  render(
    <QuoteBuilder
      produtos={PRODUTOS}
      limites={LIMITES}
      limitesConfirmados
      inicial={INICIAL}
      aoCalcular={aoCalcular}
    />,
  );
}

const saida = () => screen.getByLabelText(/^Saída/) as HTMLInputElement;
const desconto = () => screen.getByLabelText(/^Desconto \(%\)/) as HTMLInputElement;

describe("QuoteBuilder", () => {
  it("pede o cálculo ao servidor com os valores do formulário", async () => {
    const aoCalcular = vi.fn(async (_v: OrcamentoFormulario) => ({ ok: true as const, data: ORCAMENTO }));
    montar(aoCalcular);

    await waitFor(() => expect(aoCalcular).toHaveBeenCalledTimes(1));
    // O painel não recalcula nada: o número na tela é o que veio do motor.
    await screen.findByText("R$ 6.150,00");

    const enviado = aoCalcular.mock.calls[0]![0];
    expect(
      enviado.guests_count,
      "o contrato chama este dado de guests_count na entrada, na edição e na resposta",
    ).toBe("4");
    expect(Object.keys(enviado)).not.toContain("guests");
  });

  it("não deixa o painel calculando para sempre quando o formulário fica inválido no meio do cálculo", async () => {
    let liberar: (r: Resultado<Orcamento>) => void = () => {};
    const aoCalcular = vi.fn(
      () =>
        new Promise<Resultado<Orcamento>>((resolve) => {
          liberar = resolve;
        }),
    );
    montar(aoCalcular);

    await waitFor(() => expect(aoCalcular).toHaveBeenCalled());
    // Enquanto a requisição viaja, o painel está girando: o convite some.
    await waitFor(() => expect(screen.queryByText("O cálculo aparece aqui")).toBeNull());

    // A saída é apagada com a requisição ainda em voo: a que estava viajando é
    // descartada e nenhuma outra vem no lugar para desligar o indicador.
    fireEvent.change(saida(), { target: { value: "" } });
    liberar({ ok: true, data: ORCAMENTO });

    await waitFor(() => {
      expect(
        screen.getByText("O cálculo aparece aqui"),
        "sem requisição em voo, a tela precisa voltar ao estado de espera — não ficar girando para sempre",
      ).toBeTruthy();
    });
  });

  it("esconde o cálculo assim que o formulário deixa de poder gerá-lo", async () => {
    const aoCalcular = vi.fn(async () => ({ ok: true as const, data: ORCAMENTO }));
    montar(aoCalcular);

    await screen.findByText("R$ 6.150,00");

    fireEvent.change(saida(), { target: { value: "" } });

    expect(
      screen.queryByText("R$ 6.150,00"),
      "número de uma estadia que o formulário não descreve mais é número errado na tela",
    ).toBeNull();
  });

  it("não transforma desconto acima do teto em requisição", async () => {
    const aoCalcular = vi.fn(async () => ({ ok: true as const, data: ORCAMENTO }));
    montar(aoCalcular);

    await waitFor(() => expect(aoCalcular).toHaveBeenCalledTimes(1));

    fireEvent.change(desconto(), { target: { value: "22" } });

    await waitFor(() => {
      expect(screen.getByText(/acima do teto da política/)).toBeTruthy();
    });
    expect(aoCalcular, "o servidor recusaria com DISCOUNT_ABOVE_LIMIT").toHaveBeenCalledTimes(1);
  });
});
