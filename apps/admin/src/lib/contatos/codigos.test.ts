import { describe, expect, it } from "vitest";

import { normalizarCodigo } from "@/lib/api/codigos";
import { mensagemDeContato, normalizarCodigoDeContato } from "@/lib/contatos/codigos";

/**
 * Este arquivo fixa a **demonstração do defeito** que `lib/contatos/codigos.ts`
 * existe para contornar — e é o que dirá, no dia em que o espelho geral crescer,
 * que o contorno pode sair.
 *
 * `lib/api/codigos.ts` guarda o vocabulário fechado do painel e ainda não
 * conhece os dois códigos do cadastro. `normalizarCodigo()` descarta o que não
 * está na lista e cai no status — e `409` cai em `INTERNAL`.
 */
describe("o espelho geral ainda não conhece os códigos do cadastro", () => {
  it("`CONTACT_DUPLICATE` viraria `INTERNAL`", () => {
    // O estrago é no gesto mais comum do cadastro: quem digita um telefone que
    // já existe leria "erro no servidor" em vez de receber o caminho até a
    // pessoa que já está lá. `INTERNAL` também apagaria o `details.contact_id`
    // da leitura da tela, que é o que abre a ficha certa.
    expect(normalizarCodigo("CONTACT_DUPLICATE", 409)).toBe("INTERNAL");
    expect(normalizarCodigo("CONTACT_ANONYMIZED", 409)).toBe("INTERNAL");
  });

  it("o dicionário de contatos preserva os dois", () => {
    expect(normalizarCodigoDeContato("CONTACT_DUPLICATE", 409)).toBe("CONTACT_DUPLICATE");
    expect(normalizarCodigoDeContato("CONTACT_ANONYMIZED", 409)).toBe("CONTACT_ANONYMIZED");
  });

  it("e não inventa um segundo mapa de status para o resto", () => {
    // Delegar é o que impede as duas tabelas de divergirem no dia em que só uma
    // for ajustada.
    for (const status of [401, 403, 404, 422, 429, 500]) {
      expect(normalizarCodigoDeContato(undefined, status)).toBe(normalizarCodigo(undefined, status));
    }
  });
});

describe("texto por código, nunca pelo `message` da API", () => {
  it("a duplicata tem a frase mais curta do arquivo", () => {
    // De propósito: a tela não conta a história aqui — ela mostra quem já existe
    // com um botão para abrir. Uma frase longa no lugar do caminho de saída
    // seria o "erro seco" que esta rodada veio consertar.
    expect(mensagemDeContato("CONTACT_DUPLICATE")).toBe("Esta pessoa já está cadastrada.");
  });

  it("`RESOURCE_IN_USE` fala de anonimizar, não de desativar", () => {
    // O texto geral fala em "encerre antes de desativar", que não é o gesto
    // daqui: contato com histórico não se apaga, se anonimiza.
    expect(mensagemDeContato("RESOURCE_IN_USE")).toContain("anonimiza");
  });

  it("código que o cadastro não conhece cai no dicionário geral", () => {
    // Assim um código novo do contrato continua tendo uma frase, em vez de
    // deixar a tela muda.
    expect(mensagemDeContato("DATE_CONFLICT")).toContain("data");
  });
});
