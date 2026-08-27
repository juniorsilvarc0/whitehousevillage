import { describe, expect, it } from "vitest";

import {
  PoliticaDeCancelamentoFormulario,
  buracosNasFaixas,
  politicaDeCancelamentoParaEntrada,
  type FaixaFormulario,
} from "./esquemas";

/**
 * As faixas de cancelamento são a única parte da configuração em que um erro de
 * cadastro **tira dinheiro de um hóspede em silêncio**: sem faixa aplicável, o
 * motor não acha o que devolver e retém tudo por omissão. Ninguém descobre até
 * alguém cancelar e reclamar.
 *
 * Por isso a sobreposição é bloqueada (o servidor também a recusa) e o buraco é
 * avisado.
 */

function faixa(min: string, max: string, pct: string, label = "Faixa"): FaixaFormulario {
  return { days_before_min: min, days_before_max: max, refund_pct: pct, label };
}

/** As faixas do seed: ≥30 devolve tudo, 7–29 retém metade, <7 retém tudo. */
const PADRAO = [
  faixa("30", "", "100", "Devolução integral do sinal"),
  faixa("7", "29", "50", "Retenção de 50% do sinal"),
  faixa("", "6", "0", "Retenção integral do sinal"),
];

describe("buracosNasFaixas", () => {
  it("não acha buraco na política padrão — ela cobre de 0 ao infinito", () => {
    expect(buracosNasFaixas(PADRAO)).toEqual([]);
  });

  it("acha o intervalo descoberto entre duas faixas", () => {
    const comBuraco = [faixa("30", "", "100"), faixa("", "6", "0")];
    expect(buracosNasFaixas(comBuraco)).toEqual(["de 7 a 29 dias"]);
  });

  it("acha o descoberto acima da última faixa", () => {
    // Sem "sem teto" na faixa mais generosa, quem cancela com muita
    // antecedência fica sem regra — e perde tudo.
    const semTeto = [faixa("", "6", "0"), faixa("7", "29", "50")];
    expect(buracosNasFaixas(semTeto)).toEqual(["30 dias ou mais"]);
  });

  it("acha o descoberto perto do check-in", () => {
    const semPiso = [faixa("7", "", "50")];
    expect(buracosNasFaixas(semPiso)).toEqual(["de 0 a 6 dias"]);
  });
});

describe("PoliticaDeCancelamentoFormulario", () => {
  const base = { name: "Padrão White House", valid_from: "2026-09-01" };

  it("aceita a política padrão", () => {
    expect(PoliticaDeCancelamentoFormulario.safeParse({ ...base, tiers: PADRAO }).success).toBe(true);
  });

  it("recusa faixas que se sobrepõem — cada antecedência cai numa faixa só", () => {
    const analise = PoliticaDeCancelamentoFormulario.safeParse({
      ...base,
      tiers: [faixa("7", "", "100"), faixa("5", "29", "50")],
    });
    expect(analise.success).toBe(false);
    expect(JSON.stringify(analise.error?.issues)).toContain("se sobrepõe");
  });

  it("recusa teto menor que piso", () => {
    const analise = PoliticaDeCancelamentoFormulario.safeParse({ ...base, tiers: [faixa("30", "7", "100")] });
    expect(analise.success).toBe(false);
  });

  it("recusa política sem faixa nenhuma", () => {
    expect(PoliticaDeCancelamentoFormulario.safeParse({ ...base, tiers: [] }).success).toBe(false);
  });
});

describe("politicaDeCancelamentoParaEntrada", () => {
  it("traduz campo vazio para null e numera a ordem de avaliação pela ordem da tela", () => {
    const entrada = politicaDeCancelamentoParaEntrada({ ...{ name: "P", valid_from: "2026-09-01" }, tiers: PADRAO });

    expect(entrada.tiers[0]).toEqual({
      days_before_min: 30,
      // `null` é "sem teto": é o -1 do motor traduzido para SQL.
      days_before_max: null,
      refund_pct: 100,
      label: "Devolução integral do sinal",
      sort_order: 0,
    });
    expect(entrada.tiers[2]?.days_before_min).toBeNull();
    // A ordem da tela é a ordem em que o motor procura a primeira faixa
    // aplicável — da mais generosa à mais restritiva.
    expect(entrada.tiers.map((t) => t.sort_order)).toEqual([0, 1, 2]);
  });
});
