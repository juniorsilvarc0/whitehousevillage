import { describe, expect, it } from "vitest";

import { normalizarCodigo } from "@/lib/api/codigos";
import { mensagemDeContato, normalizarCodigoDeContato } from "@/lib/contatos/codigos";

/**
 * Este arquivo fixava a **demonstração do defeito** que `lib/contatos/codigos.ts`
 * existe para contornar, e avisou — ficando vermelho — no dia em que o espelho
 * geral cresceu: a rooming list (F2-22, 13/10/2026) pôs os dois códigos do
 * cadastro em `lib/api/codigos.ts`.
 *
 * Antes disso, `normalizarCodigo()` descartava o que não estava na lista e caía
 * no status — e `409` caía em `INTERNAL`. O primeiro caso agora prova o
 * contrário, e é o que impede o espelho de encolher de volta sem ninguém notar.
 */
describe("o espelho geral conhece os códigos do cadastro", () => {
  it("`CONTACT_DUPLICATE` e `CONTACT_ANONYMIZED` atravessam intactos, não viram `INTERNAL`", () => {
    // O estrago, quando viravam `INTERNAL`, era no gesto mais comum do
    // cadastro: quem digita um telefone que já existe lia "erro no servidor" em
    // vez de receber o caminho até a pessoa que já está lá — e a tela da
    // reserva, que segue com o `details.contact_id` no "Cadastrar pessoa" da
    // rooming list, perderia o caminho do mesmo jeito.
    expect(normalizarCodigo("CONTACT_DUPLICATE", 409)).toBe("CONTACT_DUPLICATE");
    expect(normalizarCodigo("CONTACT_ANONYMIZED", 409)).toBe("CONTACT_ANONYMIZED");
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
