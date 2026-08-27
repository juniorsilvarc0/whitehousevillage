import { describe, expect, it } from "vitest";

import { centavosDeTexto, formatarBRL, reaisDeCentavos } from "@/lib/dinheiro";

/**
 * A leitura de dinheiro digitado é o ponto exato onde um erro de um centavo
 * entra no sistema — e onde ele é mais difícil de notar depois, porque a grade
 * mostra `R$ 1.234,56` de qualquer jeito.
 *
 * O caso que justifica o parser por string: `Number("1234.56") * 100` dá
 * `123455.99999999999` em ponto flutuante, e `Math.round` só esconde o
 * problema até o valor em que ele não arredonda para o lado certo.
 */
describe("centavosDeTexto", () => {
  it("lê o formato brasileiro, com e sem separador de milhar", () => {
    expect(centavosDeTexto("850,00")).toBe(85_000);
    expect(centavosDeTexto("1.234,56")).toBe(123_456);
    expect(centavosDeTexto("21.000,00")).toBe(2_100_000);
  });

  it("lê o ponto como decimal quando não há vírgula — é o teclado do celular", () => {
    expect(centavosDeTexto("1234.56")).toBe(123_456);
    expect(centavosDeTexto("0.05")).toBe(5);
  });

  it("lê o ponto como milhar quando não cabe como decimal", () => {
    expect(centavosDeTexto("1.234")).toBe(123_400);
    expect(centavosDeTexto("21.000")).toBe(2_100_000);
  });

  it("aceita inteiro puro e ignora R$ e espaços", () => {
    expect(centavosDeTexto("850")).toBe(85_000);
    expect(centavosDeTexto("R$ 850,00")).toBe(85_000);
    expect(centavosDeTexto(" 850,00 ")).toBe(85_000);
  });

  it("devolve exatamente o inteiro, sem resíduo de ponto flutuante", () => {
    for (const texto of ["1234.56", "1.234,56", "0,07", "9.999,99"]) {
      const centavos = centavosDeTexto(texto);
      expect(Number.isInteger(centavos), `${texto} não virou inteiro`).toBe(true);
    }
    expect(centavosDeTexto("9.999,99")).toBe(999_999);
  });

  it("recusa o que não é dinheiro, em vez de virar zero silencioso", () => {
    // Zero silencioso viraria uma diária de graça na grade.
    expect(centavosDeTexto("")).toBeNull();
    expect(centavosDeTexto("mil")).toBeNull();
    expect(centavosDeTexto("1,234")).toBeNull();
    expect(centavosDeTexto("-50,00")).toBeNull();
  });
});

describe("ida e volta", () => {
  it("formata e relê sem perder centavo", () => {
    for (const centavos of [0, 1, 5, 85_000, 123_456, 2_100_000, 999_999]) {
      expect(centavosDeTexto(reaisDeCentavos(centavos)), `${centavos} não sobreviveu`).toBe(centavos);
    }
  });

  it("mostra em real brasileiro a partir de centavos", () => {
    expect(formatarBRL(85_000).replace(/ /g, " ")).toBe("R$ 850,00");
    expect(formatarBRL(5).replace(/ /g, " ")).toBe("R$ 0,05");
  });
});
