import { describe, expect, it } from "vitest";

import { alcadaDe, faixasDaAlcada, SEMAFORO } from "@/lib/comercial/alcada";

/**
 * A alçada é a única conta que o painel faz sobre dinheiro — e faz porque
 * precisa responder antes de existir requisição, enquanto o controle de
 * desconto ainda está sendo arrastado.
 *
 * O que se protege aqui são as **bordas**. Um `<` no lugar de um `<=` move o
 * limite em um centésimo de ponto percentual e transforma "a gestão fecha
 * sozinha" em "precisa ligar para o proprietário" no caso exato de 5%, que é o
 * mais comum de todos.
 */

const POLITICA = { auto: 5, aprovacao: 10 };

describe("alcadaDe", () => {
  it("libera a gestão até o teto automático, inclusive", () => {
    expect(alcadaDe(0, POLITICA)).toBe("gestao");
    expect(alcadaDe(4.99, POLITICA)).toBe("gestao");
    expect(
      alcadaDe(5, POLITICA),
      "5% exato é a alçada da gestão: a spec diz '≤ 5% a gestão fecha'",
    ).toBe("gestao");
  });

  it("exige aprovação do proprietário na faixa do meio, incluindo o teto", () => {
    expect(alcadaDe(5.01, POLITICA)).toBe("proprietario");
    expect(alcadaDe(7.5, POLITICA)).toBe("proprietario");
    expect(
      alcadaDe(10, POLITICA),
      "10% exato ainda é aprovável: a spec diz '6–10% exige aprovação'",
    ).toBe("proprietario");
  });

  it("nega acima do teto de aprovação", () => {
    expect(alcadaDe(10.01, POLITICA)).toBe("negado");
    expect(alcadaDe(40, POLITICA)).toBe("negado");
  });

  it("segue os limites da política, não os números da spec", () => {
    // Os 5 e 10 são o que a política vigente diz hoje; a tela de política pode
    // publicar outros amanhã, e a decisão tem que acompanhar sem deploy.
    const outra = { auto: 8, aprovacao: 15 };
    expect(alcadaDe(7, outra)).toBe("gestao");
    expect(alcadaDe(12, outra)).toBe("proprietario");
    expect(alcadaDe(16, outra)).toBe("negado");
  });

  it("nega o que não é número, em vez de liberar por omissão", () => {
    // Campo com texto inválido não pode virar desconto autorizado: a falha
    // segura é travar a venda.
    expect(alcadaDe(Number.NaN, POLITICA)).toBe("negado");
    expect(alcadaDe(Number.POSITIVE_INFINITY, POLITICA)).toBe("negado");
  });

  it("só a faixa vermelha desabilita a emissão", () => {
    expect(SEMAFORO.gestao.podeFechar).toBe(true);
    expect(SEMAFORO.proprietario.podeFechar).toBe(true);
    expect(SEMAFORO.negado.podeFechar).toBe(false);
  });
});

describe("faixasDaAlcada", () => {
  it("descreve as três faixas com os números da política", () => {
    expect(faixasDaAlcada(POLITICA)).toEqual([
      { alcada: "gestao", de: 0, ate: 5 },
      { alcada: "proprietario", de: 5, ate: 10 },
      { alcada: "negado", de: 10, ate: null },
    ]);
  });
});
