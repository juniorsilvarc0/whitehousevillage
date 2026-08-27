import { describe, expect, it } from "vitest";

import { normalizarCodigo } from "@/lib/api/codigos";
import { CODIGOS_DO_CRM, codigoGeral, mensagemCrm, normalizarCodigoCrm } from "@/lib/crm/codigos";

/**
 * A prova do diagnóstico que fez `lib/crm/codigos.ts` e `lib/crm/api.ts`
 * existirem — e o teste que fica **vermelho** no dia em que o problema for
 * resolvido, que é quando os dois arquivos podem ser apagados.
 */
describe("o espelho de códigos do painel ainda não conhece o CRM", () => {
  it("apaga o código do CRM: os 422 viram VALIDATION_ERROR e os 409 viram INTERNAL", () => {
    // 422 sobrevive como classe: "confira os dados" é uma leitura aceitável.
    expect(normalizarCodigo("STAGE_NOT_IN_PIPELINE", 422)).toBe("VALIDATION_ERROR");
    expect(normalizarCodigo("QUOTE_REQUIRED_TO_WIN", 422)).toBe("VALIDATION_ERROR");

    // 409 não sobrevive de jeito nenhum: `normalizarCodigo` não tem ramo para
    // esse status e cai no padrão. "Este lead já virou oportunidade" chega à
    // tela como "erro no servidor" — uma recusa que o operador resolve sozinho
    // virando um chamado para a equipe técnica.
    expect(normalizarCodigo("LEAD_ALREADY_CONVERTED", 409)).toBe("INTERNAL");
    expect(normalizarCodigo("OPPORTUNITY_ALREADY_CLOSED", 409)).toBe("INTERNAL");
  });

  it("o vocabulário do CRM preserva os sete e delega o resto", () => {
    for (const codigo of CODIGOS_DO_CRM) {
      expect(normalizarCodigoCrm(codigo, 409)).toBe(codigo);
    }
    expect(normalizarCodigoCrm("DATE_CONFLICT", 409)).toBe("DATE_CONFLICT");
    expect(normalizarCodigoCrm("QUALQUER_COISA", 404)).toBe("NOT_FOUND");
  });

  it("todo código do CRM tem frase própria, e nenhuma é o texto padrão", () => {
    const padrao = mensagemCrm("INTERNAL");
    for (const codigo of CODIGOS_DO_CRM) {
      const frase = mensagemCrm(codigo);
      expect(frase.length, `${codigo} sem frase`).toBeGreaterThan(20);
      expect(frase, `${codigo} caiu no texto genérico`).not.toBe(padrao);
    }
  });

  it("traduz para o vocabulário geral preservando a classe da recusa", () => {
    // As peças da casca que só aceitam `CodigoDeErro` continuam dizendo algo
    // verdadeiro: 422 é dado errado, 409 é estado que não aceita a operação.
    expect(codigoGeral("QUOTE_REQUIRED_TO_WIN")).toBe("VALIDATION_ERROR");
    expect(codigoGeral("LEAD_ALREADY_CONVERTED")).toBe("INVALID_STATE_TRANSITION");
    // Código que já é geral atravessa intacto.
    expect(codigoGeral("DATE_CONFLICT")).toBe("DATE_CONFLICT");
  });
});
