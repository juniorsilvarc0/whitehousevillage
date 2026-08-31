import { describe, expect, it } from "vitest";

import { lerComposicaoIncompleta, lerConflito } from "@/lib/reservas/conflito";

/**
 * `details.period` chega como o `daterange` do Postgres. A tela não repete a
 * notação do banco para quem vende — e, mais importante, **não mostra um
 * colchete solto** quando o formato muda: sem frase é melhor do que meia frase.
 */
describe("lerConflito", () => {
  it("traduz unidade e período em uma frase em português", () => {
    const { frase } = lerConflito({ unit_code: "AP-03", period: "[2026-12-20,2026-12-23)" });
    expect(frase).toBe("A unidade AP-03 já está ocupada de 20/12/2026 a 23/12/2026.");
  });

  it("com só a unidade, diz só a unidade", () => {
    expect(lerConflito({ unit_code: "AP-03" }).frase).toBe("A unidade em conflito é a AP-03.");
  });

  it("com só o período, diz só as datas", () => {
    expect(lerConflito({ period: "[2026-12-20,2026-12-23)" }).frase).toBe(
      "O conflito é entre 20/12/2026 e 23/12/2026.",
    );
  });

  it("formato inesperado devolve nada em vez de meia tradução", () => {
    expect(lerConflito({ period: "dezembro inteiro" }).frase).toBeNull();
    expect(lerConflito({}).frase).toBeNull();
    expect(lerConflito({ unit_code: 42, period: null }).frase).toBeNull();
  });
});

/**
 * `COMPOSITION_INCOMPLETE` é `422` e não `409` porque a data não está em
 * disputa: nenhuma data resolve enquanto o produto não puder ser entregue
 * inteiro. Quem age é a gestão do inventário — e por isso a frase precisa
 * carregar o **código da unidade**, não uma queixa genérica.
 */
describe("lerComposicaoIncompleta", () => {
  it("nomeia a unidade a reativar", () => {
    const texto = lerComposicaoIncompleta({
      unit_type_code: "White House Completa",
      expected_units: 8,
      active_units: 7,
      missing_unit_codes: ["AP-03"],
    });
    expect(texto).toContain("AP-03");
    expect(texto).toContain("inventário");
  });

  it("sem a lista, cai na contagem", () => {
    expect(lerComposicaoIncompleta({ expected_units: 8, active_units: 7 })).toContain("8");
  });

  it("sem nada aproveitável, não inventa", () => {
    expect(lerComposicaoIncompleta({})).toBeNull();
  });
});
