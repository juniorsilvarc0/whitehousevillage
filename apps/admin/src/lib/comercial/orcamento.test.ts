import { describe, expect, it } from "vitest";

import { OrcamentoFormulario, orcamentoParaPedido } from "@/lib/comercial/orcamento";

/**
 * A fronteira formulário → DTO do orçamento.
 *
 * O que se protege é **um dado, um nome**: o número de hóspedes é
 * `guests_count` no pedido, na criação, na edição e na resposta. Enquanto a
 * criação mandava `guests` e o resto falava `guests_count`, um `PATCH` com o
 * nome errado respondia `200` sem gravar nada — o cliente achava que tinha
 * gravado e não havia erro em lugar nenhum para investigar.
 */
describe("orcamentoParaPedido", () => {
  const valores = {
    unit_type_id: "p-cob",
    check_in: "2030-09-10",
    check_out: "2030-09-13",
    guests_count: "4",
    discount_pct: "7,5",
    is_event: true,
  };

  it("manda guests_count — o nome que o contrato usa em toda a superfície", () => {
    const pedido = orcamentoParaPedido(valores);
    expect(pedido.guests_count).toBe(4);
    expect(
      Object.keys(pedido),
      "campo desconhecido no corpo é 422; um nome legado aqui vira recusa lá",
    ).not.toContain("guests");
  });

  it("converte percentual com vírgula, porque é assim que se digita em português", () => {
    expect(orcamentoParaPedido(valores).discount_pct).toBe(7.5);
  });

  it("recusa hóspede zero antes de virar requisição", () => {
    const analise = OrcamentoFormulario.safeParse({ ...valores, guests_count: "0" });
    expect(analise.success).toBe(false);
    if (!analise.success) {
      expect(analise.error.issues[0]!.path).toEqual(["guests_count"]);
    }
  });
});
