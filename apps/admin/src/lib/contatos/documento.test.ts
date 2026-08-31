import { describe, expect, it } from "vitest";

import { documentoParaApi, ehCNPJ, ehCPF, ehPassaporte, formatarDocumento } from "@/lib/contatos/documento";

/**
 * O dígito verificador não está aqui para "proteger" nada — a API recusa CPF
 * inválido com `422`, e é ela quem manda. Está para o erro aparecer **no
 * campo**, enquanto a pessoa ainda tem o documento na mão. Documento digitado
 * errado no balcão vira uma ficha que nunca casa com a segunda visita do mesmo
 * hóspede.
 */
describe("CPF", () => {
  it("aceita CPF válido, com e sem pontuação", () => {
    expect(ehCPF("12345678909")).toBe(true);
    expect(ehCPF("123.456.789-09")).toBe(true);
  });

  it("recusa dígito verificador errado", () => {
    expect(ehCPF("12345678900")).toBe(false);
  });

  it("recusa os onze dígitos repetidos", () => {
    // `111.111.111-11` PASSA na conta do módulo 11 e não é CPF de ninguém: é o
    // preenchimento de quem quer atravessar o campo. Sem este caso especial, a
    // validação daria certo e a base ganharia uma ficha impossível de casar.
    for (const digito of "0123456789") {
      expect(ehCPF(digito.repeat(11)), `${digito.repeat(11)} passou`).toBe(false);
    }
  });

  it("recusa tamanho errado", () => {
    expect(ehCPF("1234567890")).toBe(false);
    expect(ehCPF("")).toBe(false);
  });
});

describe("CNPJ", () => {
  it("aceita CNPJ válido", () => {
    expect(ehCNPJ("11222333000181")).toBe(true);
    expect(ehCNPJ("11.222.333/0001-81")).toBe(true);
  });

  it("recusa dígito errado e repetição", () => {
    expect(ehCNPJ("11222333000180")).toBe(false);
    expect(ehCNPJ("11111111111111")).toBe(false);
  });
});

describe("passaporte", () => {
  it("aceita alfanumérico de qualquer país", () => {
    // Não há validação de formato de propósito: cada país tem o seu, e recusar
    // o que não se sabe validar barraria hóspede estrangeiro no balcão.
    expect(ehPassaporte("FR1234567")).toBe(true);
    expect(ehPassaporte("AB12345")).toBe(true);
  });

  it("recusa o que nem parece documento", () => {
    expect(ehPassaporte("AB1")).toBe(false);
    expect(ehPassaporte("tem espaço")).toBe(false);
  });
});

describe("fronteira com a API", () => {
  it("CPF e CNPJ vão só com dígitos", () => {
    // Guardar `123.456.789-09` e `12345678909` na mesma coluna é ter a mesma
    // pessoa duas vezes — a busca por documento é igualdade exata.
    expect(documentoParaApi("cpf", "123.456.789-09")).toBe("12345678909");
    expect(documentoParaApi("cnpj", "11.222.333/0001-81")).toBe("11222333000181");
  });

  it("passaporte vai em maiúsculas, sem espaço nas pontas", () => {
    expect(documentoParaApi("passaporte", " fr1234567 ")).toBe("FR1234567");
  });

  it("a exibição devolve a pontuação, porque documento se lê em voz alta", () => {
    expect(formatarDocumento("cpf", "12345678909")).toBe("123.456.789-09");
    expect(formatarDocumento("cnpj", "11222333000181")).toBe("11.222.333/0001-81");
  });

  it("dado torto volta como veio, sem meia formatação", () => {
    // Melhor mostrar o que está gravado do que um CPF com pontos no lugar
    // errado, que parece verdade.
    expect(formatarDocumento("cpf", "123")).toBe("123");
    expect(formatarDocumento(null, null)).toBe("");
  });
});
