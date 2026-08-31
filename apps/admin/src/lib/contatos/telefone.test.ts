import { describe, expect, it } from "vitest";

import { ehE164, formatarTelefone, limparTelefone } from "@/lib/contatos/telefone";

/**
 * O telefone é a **chave de deduplicação** do cadastro
 * (`UNIQUE (phone_e164) WHERE phone_e164 IS NOT NULL`). O que se protege aqui é
 * a regra que sustenta essa unicidade: não se adivinha DDI.
 */
describe("E.164", () => {
  it("aceita o formato do contrato", () => {
    expect(ehE164("+5585999990000")).toBe(true);
    expect(ehE164("+351912345678")).toBe(true);
  });

  it("recusa o que não declara o país", () => {
    expect(ehE164("85999990000")).toBe(false);
    expect(ehE164("(85) 99999-0000")).toBe(false);
  });

  it("recusa `+0…` e número curto demais para ser telefone", () => {
    expect(ehE164("+0585999990000")).toBe(false);
    expect(ehE164("+55")).toBe(false);
  });
});

describe("limparTelefone", () => {
  it("tira pontuação e NÃO acrescenta nada", () => {
    // A distinção que o arquivo inteiro existe para manter: limpar o que a
    // pessoa digitou é uma coisa; completar o que ela não digitou é outra, e a
    // segunda cria dois cadastros do mesmo ser humano.
    expect(limparTelefone("+55 (85) 99999-0000")).toBe("+5585999990000");
    expect(limparTelefone("85 99999-0000")).toBe("85999990000");
    expect(ehE164(limparTelefone("85 99999-0000"))).toBe(false);
  });
});

describe("formatarTelefone", () => {
  it("agrupa o número brasileiro", () => {
    expect(formatarTelefone("+5585999990000")).toBe("+55 (85) 99999-0000");
    expect(formatarTelefone("+558533334444")).toBe("+55 (85) 3333-4444");
  });

  it("devolve como veio o que não sabe agrupar", () => {
    // Inventar agrupamento para um país cujo plano de numeração não se conhece
    // é pior que não agrupar: parece verdade.
    expect(formatarTelefone("+351912345678")).toBe("+351912345678");
    expect(formatarTelefone(null)).toBe("");
  });
});
