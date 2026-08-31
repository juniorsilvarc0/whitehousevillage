import { describe, expect, it } from "vitest";

import { consultaDeBusca, modoDaBusca } from "@/lib/contatos/busca";

/**
 * Uma caixa de busca, três filtros do contrato que **não são intercambiáveis**.
 *
 * O que se protege aqui é a escolha entre eles, porque errá-la custa caro nos
 * dois sentidos:
 *
 * - mandar um número incompleto em `phone` é `422 VALIDATION_ERROR` na cara de
 *   quem ainda estava digitando;
 * - mandar um telefone completo em `q` transforma a pergunta "esta pessoa já
 *   existe?" numa busca por semelhança de texto — e é dessa confusão que nasce
 *   o contato duplicado a cada mensagem de WhatsApp.
 */
describe("modoDaBusca", () => {
  it("só trata como telefone o E.164 completo", () => {
    expect(modoDaBusca("+5585999990000")).toBe("telefone");
    expect(modoDaBusca("+55 (85) 99999-0000")).toBe("telefone");
  });

  it("telefone pela metade continua sendo busca por nome, não erro", () => {
    // Quem digitou `+5585` não errou: não terminou. Virar consulta por telefone
    // aqui devolveria 422 no meio da digitação, e a tela ficaria vermelha
    // enquanto a pessoa escreve.
    expect(modoDaBusca("+55")).toBe("nome");
    expect(modoDaBusca("+5585")).toBe("nome");
  });

  it("nunca inventa DDI: número sem `+` não vira busca por telefone", () => {
    // `85999990000` pode ser Fortaleza ou pode ser outra coisa. Completar o DDI
    // no palpite é o que cria dois registros do mesmo ser humano — exatamente o
    // que a UNIQUE de `phone_e164` existe para impedir. A consulta sai sem
    // `phone`, e isso é o que importa; que ela caia em documento (onze dígitos
    // são CPF **e** celular brasileiro) é a leitura que pode achar alguém, e a
    // barra escreve na tela qual das duas escolheu.
    expect(consultaDeBusca("85999990000").phone).toBeUndefined();
    expect(modoDaBusca("85999990000")).toBe("documento");
  });

  it("onze e catorze dígitos são documento; qualquer outro tamanho, não", () => {
    expect(modoDaBusca("12345678909")).toBe("documento");
    expect(modoDaBusca("123.456.789-09")).toBe("documento");
    expect(modoDaBusca("11222333000181")).toBe("documento");
    expect(modoDaBusca("1234")).toBe("nome");
  });

  it("número no meio de texto não é documento", () => {
    // "Casa 11122233344" tem onze dígitos e é um nome. Buscar por documento
    // devolveria vazio sem dizer por quê.
    expect(modoDaBusca("Casa 11122233344")).toBe("nome");
  });

  it("uma letra só não vira consulta", () => {
    // O contrato exige `minLength: 2` em `q`. Quem digitou "A" está começando,
    // não errando — a busca vazia é mais honesta que um 422.
    expect(consultaDeBusca("A")).toEqual({});
    expect(consultaDeBusca("")).toEqual({});
  });
});

describe("consultaDeBusca", () => {
  it("manda telefone limpo em E.164, não o que foi digitado", () => {
    expect(consultaDeBusca("+55 (85) 99999-0000")).toEqual({ phone: "+5585999990000" });
  });

  it("manda documento só com dígitos, como o banco grava", () => {
    // `123.456.789-09` e `12345678909` são a mesma pessoa; a busca é igualdade
    // exata, então mandar a pontuação devolveria vazio.
    expect(consultaDeBusca("123.456.789-09")).toEqual({ doc_number: "12345678909" });
  });

  it("nome desce em `q`", () => {
    expect(consultaDeBusca("  Ana Silva  ")).toEqual({ q: "Ana Silva" });
  });

  it("os três filtros nunca saem juntos", () => {
    // Mandar `phone` e `q` na mesma requisição faria a API decidir a precedência
    // — e a decisão é da tela, que sabe o que a pessoa quis dizer.
    for (const termo of ["+5585999990000", "12345678909", "Ana"]) {
      expect(Object.keys(consultaDeBusca(termo))).toHaveLength(1);
    }
  });
});
