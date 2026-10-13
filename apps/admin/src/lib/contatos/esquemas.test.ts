import { describe, expect, it } from "vitest";

import { ContatoFormulario } from "@/lib/contatos/esquemas";

/**
 * A máscara da lista nunca volta como entrada (OpenAPI, "Dado pessoal na
 * resposta"): é `422` na API, e o formulário recusa antes, no campo.
 *
 * Telefone e documento mascarados já caíam nos validadores de formato; o
 * e-mail não — `f***@gmail.com` é endereço válido pela RFC. É o caso que o DTO
 * do Go recusa explicitamente (`validarEmail`), e o espelho tem de recusar
 * igual.
 */

const BASE: ContatoFormulario = {
  name: "Fernanda Lima",
  email: "",
  phone_e164: "",
  doc_type: "",
  doc_number: "",
  birth_date: "",
  city: "",
  state: "",
  notes: "",
  lgpd_basis: "",
  marketing_opt_in: false,
  consent_at: "",
};

function errosDe(valores: Partial<ContatoFormulario>): Record<string, string> {
  const analise = ContatoFormulario.safeParse({ ...BASE, ...valores });
  if (analise.success) return {};
  return Object.fromEntries(analise.error.issues.map((i) => [i.path.join("."), i.message]));
}

describe("valor mascarado não volta como entrada", () => {
  it("e-mail com `*` é recusado, mesmo sendo endereço válido pela RFC", () => {
    expect(errosDe({ email: "f***@gmail.com" }).email).toMatch(/mascarado/);
  });

  it("telefone mascarado é recusado", () => {
    expect(errosDe({ phone_e164: "+*********0000" }).phone_e164).toBeTruthy();
  });

  it("documento mascarado é recusado, nos três tipos", () => {
    expect(errosDe({ doc_type: "cpf", doc_number: "***.***.247-25" }).doc_number).toBeTruthy();
    expect(errosDe({ doc_type: "cnpj", doc_number: "**.***.***/0001-81" }).doc_number).toBeTruthy();
    expect(errosDe({ doc_type: "passaporte", doc_number: "******567" }).doc_number).toBeTruthy();
  });

  it("os valores cheios da ficha passam", () => {
    expect(
      errosDe({
        email: "fernanda.lima@gmail.com",
        phone_e164: "+5585999990000",
        doc_type: "cpf",
        doc_number: "52998224725",
      }),
    ).toEqual({});
  });
});
