import { describe, expect, it } from "vitest";

import { normalizarCodigo } from "@/lib/api/codigos";
import { mensagemDoErro } from "@/lib/acoes/resultado";
import { caminhoDaConferencia, conferenciaJaAberta, mensagemDeBens } from "@/lib/bens/mensagens";

const ID = "9f1c1e2a-0d3b-4f6a-9a1e-2b7c5d8e0f11";

describe("os três códigos da conferência estão no espelho geral", () => {
  it("o 409 não vira INTERNAL — o código atravessa e leva o details junto", () => {
    // Sem isto, `normalizarCodigo` não tem ramo para 409 e o segundo toque em
    // "Abrir conferência" diria "erro no servidor" em vez de levar à aberta.
    expect(normalizarCodigo("COUNT_ALREADY_OPEN", 409)).toBe("COUNT_ALREADY_OPEN");
    expect(normalizarCodigo("COUNT_CLOSED", 409)).toBe("COUNT_CLOSED");
    expect(normalizarCodigo("COUNT_HAS_PENDING_LINES", 409)).toBe("COUNT_HAS_PENDING_LINES");
  });

  it("cada um tem frase própria no dicionário geral", () => {
    const padrao = mensagemDoErro("INTERNAL");
    for (const codigo of ["COUNT_ALREADY_OPEN", "COUNT_CLOSED", "COUNT_HAS_PENDING_LINES"] as const) {
      expect(mensagemDoErro(codigo)).not.toBe(padrao);
    }
  });
});

describe("COUNT_ALREADY_OPEN leva à conferência que já está aberta", () => {
  it("devolve o caminho da conferência do details.count_id", () => {
    const aberta = conferenciaJaAberta({
      code: "COUNT_ALREADY_OPEN",
      details: { count_id: ID, opened_at: "2026-10-07T14:32:00Z" },
    });
    expect(aberta).toEqual({ id: ID, caminho: `/app/inventario/conferencias/${ID}`, abertaEm: "2026-10-07T14:32:00Z" });
    expect(aberta?.caminho).toBe(caminhoDaConferencia(ID));
  });

  it("outro código não inventa destino", () => {
    expect(conferenciaJaAberta({ code: "COUNT_CLOSED", details: { count_id: ID } })).toBeNull();
    expect(conferenciaJaAberta({ code: "VALIDATION_ERROR", details: {} })).toBeNull();
  });

  it("409 sem count_id também não inventa — a tela mostra a frase", () => {
    expect(conferenciaJaAberta({ code: "COUNT_ALREADY_OPEN", details: {} })).toBeNull();
    expect(conferenciaJaAberta({ code: "COUNT_ALREADY_OPEN", details: { count_id: 42 } })).toBeNull();
  });
});

describe("a frase do inventário, pelo código", () => {
  it("RESOURCE_IN_USE manda desativar, não fala de reserva", () => {
    const frase = mensagemDeBens({ code: "RESOURCE_IN_USE", details: { count_lines: 3, issues: 1 } }, "ambiente");
    expect(frase).toMatch(/desative/i);
    expect(frase).not.toMatch(/reserva/i);
  });

  it("bem preso só por colocação manda tirar dos ambientes", () => {
    const frase = mensagemDeBens({ code: "RESOURCE_IN_USE", details: { placements: 2 } }, "bem");
    expect(frase).toMatch(/tire-o dos ambientes/i);
  });

  it("CODE_IN_USE na colocação diz quanto já há ali", () => {
    expect(mensagemDeBens({ code: "CODE_IN_USE", details: { expected_qty: 12 } }, "colocacao")).toMatch(/com 12/);
  });

  it("COUNT_HAS_PENDING_LINES diz quantos faltam", () => {
    expect(mensagemDeBens({ code: "COUNT_HAS_PENDING_LINES", details: { pending: 4 } })).toMatch(/4 itens sem contagem/);
  });

  it("a frase de details.file do envio de foto atravessa", () => {
    expect(
      mensagemDeBens({ code: "VALIDATION_ERROR", details: { file: "A foto precisa ser JPG, PNG ou WebP, até 15 MB." } }, "foto"),
    ).toBe("A foto precisa ser JPG, PNG ou WebP, até 15 MB.");
  });
});
