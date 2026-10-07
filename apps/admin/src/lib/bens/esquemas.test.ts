import { describe, expect, it } from "vitest";

import { BemFormulario, bemParaEntrada, custoParaCampo, lerCusto, valoresDoBem } from "@/lib/bens/esquemas";
import type { Bem } from "@/lib/bens/tipos";

/**
 * O custo de reposição: **reais na tela, centavos no fio** — e três estados,
 * não dois. Vazio é "não cotado" (`null`); zero é recusado, porque seria um
 * segundo jeito, errado, de dizer "não sei" e faria a avaria de um bem sair de
 * graça na cobrança.
 */
describe("custo de reposição: reais ↔ centavos", () => {
  it("lê reais em centavos inteiros, sem ponto flutuante no meio", () => {
    expect(lerCusto("18,90")).toEqual({ ok: true, centavos: 1890 });
    expect(lerCusto("1.234,56")).toEqual({ ok: true, centavos: 123_456 });
    expect(lerCusto("0,05")).toEqual({ ok: true, centavos: 5 });
    expect(lerCusto("R$ 400")).toEqual({ ok: true, centavos: 40_000 });
  });

  it("vazio é 'não cotado' (null) — e não zero", () => {
    expect(lerCusto("")).toEqual({ ok: true, centavos: null });
    expect(lerCusto("   ")).toEqual({ ok: true, centavos: null });
  });

  it("zero é recusado, em qualquer grafia", () => {
    for (const zero of ["0", "0,00", "0.00", "R$ 0,00"]) {
      expect(lerCusto(zero), zero).toEqual({ ok: false, motivo: "zero" });
    }
  });

  it("o que não é dinheiro é recusado, em vez de virar zero ou null em silêncio", () => {
    expect(lerCusto("dezoito")).toEqual({ ok: false, motivo: "invalido" });
    expect(lerCusto("-5,00")).toEqual({ ok: false, motivo: "invalido" });
  });

  it("ida e volta: centavos → campo → centavos sem perder centavo", () => {
    for (const centavos of [1, 5, 1890, 123_456, 2_100_000]) {
      const volta = lerCusto(custoParaCampo(centavos));
      expect(volta, String(centavos)).toEqual({ ok: true, centavos });
    }
    expect(custoParaCampo(null)).toBe("");
    expect(custoParaCampo(undefined)).toBe("");
  });
});

describe("o formulário do bem vira o corpo do contrato", () => {
  const base = valoresDoBem(null);

  it("custo vazio vai como null; preenchido, em centavos", () => {
    expect(bemParaEntrada({ ...base, name: "Taça", replacement_cost: "" }).replacement_cost_cents).toBeNull();
    expect(bemParaEntrada({ ...base, name: "Taça", replacement_cost: "18,90" }).replacement_cost_cents).toBe(1890);
  });

  it("o schema recusa custo zero no campo certo, com a saída escrita", () => {
    const r = BemFormulario.safeParse({ ...base, name: "Taça", replacement_cost: "0,00" });
    expect(r.success).toBe(false);
    if (!r.success) {
      const problema = r.error.issues.find((i) => i.path.join(".") === "replacement_cost");
      expect(problema?.message).toMatch(/deixe o campo vazio/i);
    }
  });

  it("o corpo não carrega source_ref — é a chave do importador, e o contrato recusa", () => {
    const corpo = bemParaEntrada({ ...base, name: "Prato raso", replacement_cost: "" });
    expect(Object.keys(corpo).sort()).toEqual(
      ["active", "category", "description", "name", "replacement_cost_cents", "unit_measure"].sort(),
    );
  });

  it("descrição vazia vai como null (limpa), nunca como string vazia", () => {
    expect(bemParaEntrada({ ...base, name: "Prato", description: "  ", replacement_cost: "" }).description).toBeNull();
  });

  it("editar um bem não cotado abre com o campo vazio, não com 0,00", () => {
    const bem: Bem = { id: "b", name: "Prato", category: "louca", unit_measure: "un", active: true, replacement_cost_cents: null };
    expect(valoresDoBem(bem).replacement_cost).toBe("");
    expect(valoresDoBem({ ...bem, replacement_cost_cents: 1890 }).replacement_cost).toBe("18,90");
  });
});
