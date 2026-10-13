import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { normalizarCodigo } from "@/lib/api/codigos";
import { mensagemDoErro, type Falha } from "@/lib/acoes/resultado";
import {
  cabeAumentar,
  lerListaCheia,
  mensagemDaLista,
  numeroParaMaisUm,
  pedeRecarga,
  podeAbrirLugar,
  podeAcrescentar,
  podeRetirar,
} from "@/lib/reservas/hospedes";

/**
 * A rooming list (F2-22) do lado do painel: os portões dos botões e as frases
 * por código.
 *
 * Os portões não decidem regra — `guests_editable` vem do servidor —, mas são
 * eles que impedem a tela de oferecer o que vai falhar. E as frases são a
 * diferença entre "erro" e "aumente o número de hóspedes e acrescente a Joana".
 */

const AQUI = path.dirname(fileURLToPath(import.meta.url));
const OPENAPI = path.resolve(AQUI, "../../../../api/openapi/openapi.yaml");

function codigosDoContrato(): string[] | null {
  if (!existsSync(OPENAPI)) return null;
  const fonte = readFileSync(OPENAPI, "utf8");
  const bloco = fonte.match(/enum: \[(VALIDATION_ERROR[\s\S]*?)\]/);
  if (!bloco) throw new Error("não achei o enum de error.code na OpenAPI — o contrato mudou de forma?");
  return bloco[1].split(",").map((c) => c.trim()).filter(Boolean);
}

const doContrato = codigosDoContrato();

function recusa(code: Falha["code"], details: Record<string, unknown> = {}): Falha {
  return { ok: false, code, message: "TEXTO CRU DA API QUE NÃO DEVE APARECER", details };
}

const EDITA = { editavel: true, podeEditar: true };

describe("os códigos da lista no espelho geral", () => {
  it("GUEST_LIST_FULL atravessa intacto — não vira VALIDATION_ERROR e não perde o details", () => {
    // Sem ele no espelho, o 422 cairia em VALIDATION_ERROR ("confira os dados")
    // e a tela não teria guests_count/listed/capacity para oferecer o aumento.
    expect(normalizarCodigo("GUEST_LIST_FULL", 422)).toBe("GUEST_LIST_FULL");
  });

  it("CONTACT_DUPLICATE e CONTACT_ANONYMIZED também — o 409 não vira INTERNAL", () => {
    expect(normalizarCodigo("CONTACT_DUPLICATE", 409)).toBe("CONTACT_DUPLICATE");
    expect(normalizarCodigo("CONTACT_ANONYMIZED", 409)).toBe("CONTACT_ANONYMIZED");
  });

  it.skipIf(doContrato === null)("e são os do contrato, letra por letra", () => {
    expect(doContrato).toContain("GUEST_LIST_FULL");
    expect(doContrato).toContain("CONTACT_DUPLICATE");
    expect(doContrato).toContain("CONTACT_ANONYMIZED");
  });

  it("cada um tem frase própria no dicionário geral", () => {
    const padrao = mensagemDoErro("INTERNAL");
    for (const code of ["GUEST_LIST_FULL", "CONTACT_DUPLICATE", "CONTACT_ANONYMIZED"] as const) {
      expect(mensagemDoErro(code), code).not.toBe(padrao);
      expect(mensagemDoErro(code), code).not.toBe(mensagemDoErro("VALIDATION_ERROR"));
    }
    expect(mensagemDoErro("GUEST_LIST_FULL")).toMatch(/aumente/);
  });
});

describe("Acrescentar hóspede — só com tudo junto", () => {
  it("lista editável, permissão e lugar sobrando: aparece", () => {
    expect(podeAcrescentar(EDITA, 2, 6)).toBe(true);
  });

  it("falta qualquer um dos três: não aparece", () => {
    expect(podeAcrescentar({ editavel: false, podeEditar: true }, 2, 6)).toBe(false);
    expect(podeAcrescentar({ editavel: true, podeEditar: false }, 2, 6)).toBe(false);
    // Lista completa: o caminho é aumentar o número antes — o botão seria o 422.
    expect(podeAcrescentar(EDITA, 6, 6)).toBe(false);
    // Lista anterior ao F2-22, acima do número: também não.
    expect(podeAcrescentar(EDITA, 7, 6)).toBe(false);
  });
});

describe("Retirar — só acompanhante", () => {
  it("o titular nunca tem o botão, nem com tudo liberado", () => {
    expect(podeRetirar({ is_lead_guest: true }, EDITA)).toBe(false);
  });

  it("o acompanhante tem, com a lista editável e a permissão", () => {
    expect(podeRetirar({ is_lead_guest: false }, EDITA)).toBe(true);
    expect(podeRetirar({ is_lead_guest: false }, { editavel: false, podeEditar: true })).toBe(false);
    expect(podeRetirar({ is_lead_guest: false }, { editavel: true, podeEditar: false })).toBe(false);
  });
});

describe("abrir lugar com a lista completa", () => {
  it("oferece aumentar quando o vendido está abaixo da capacidade", () => {
    expect(podeAbrirLugar(EDITA, 6, 6, 8)).toBe(true);
  });

  it("não oferece com o produto no máximo — a capacidade já diz que não cabe", () => {
    expect(podeAbrirLugar(EDITA, 8, 8, 8)).toBe(false);
  });

  it("sem a capacidade na mão, oferece — quem responde é o servidor", () => {
    expect(podeAbrirLugar(EDITA, 6, 6, null)).toBe(true);
  });

  it("com lugar sobrando, não é este o botão: é o de acrescentar", () => {
    expect(podeAbrirLugar(EDITA, 4, 6, 8)).toBe(false);
  });

  it("lista fechada ou sem permissão: nada", () => {
    expect(podeAbrirLugar({ editavel: false, podeEditar: true }, 6, 6, 8)).toBe(false);
    expect(podeAbrirLugar({ editavel: true, podeEditar: false }, 6, 6, 8)).toBe(false);
  });

  it("pede lugar para mais um nome — contando os nomes, não o vendido", () => {
    expect(numeroParaMaisUm(6)).toBe(7);
    // Lista anterior ao F2-22 com 8 nomes e 6 vendidos: pedir 7 não abriria
    // lugar nenhum.
    expect(numeroParaMaisUm(8)).toBe(9);
  });
});

describe("GUEST_LIST_FULL — os três números do details", () => {
  it("lê os três e diz se cabe aumentar", () => {
    const lista = lerListaCheia({ guests_count: 6, listed: 6, capacity: 8 });
    expect(lista).toEqual({ guests_count: 6, listed: 6, capacity: 8 });
    expect(cabeAumentar(lista!)).toBe(true);
    expect(cabeAumentar({ guests_count: 8, listed: 8, capacity: 8 })).toBe(false);
  });

  it("falta um número ou vem texto: não oferece nada, em vez de oferecer errado", () => {
    expect(lerListaCheia({ guests_count: 6, listed: 6 })).toBeNull();
    expect(lerListaCheia({ guests_count: "6", listed: 6, capacity: 8 })).toBeNull();
    expect(lerListaCheia(undefined)).toBeNull();
  });
});

describe("a frase da recusa — pelo código, nunca pelo texto da API", () => {
  it("lista cheia com lugar no produto: diz os números e o caminho", () => {
    const frase = mensagemDaLista(recusa("GUEST_LIST_FULL", { guests_count: 6, listed: 6, capacity: 8 }), "acrescentar", "Joana Souza");
    expect(frase).toContain("6 nomes para 6 hóspedes");
    expect(frase).toContain("comporta até 8");
    expect(frase).toContain("aumente o número de hóspedes");
    expect(frase).toContain("Joana Souza");
  });

  it("lista cheia com o produto no máximo: o caminho é retirar alguém", () => {
    const frase = mensagemDaLista(recusa("GUEST_LIST_FULL", { guests_count: 8, listed: 8, capacity: 8 }), "acrescentar");
    expect(frase).toContain("retire antes alguém");
    expect(frase).not.toContain("aumente");
  });

  it("a pessoa já está na lista — e, se é o titular, diz", () => {
    expect(mensagemDaLista(recusa("CODE_IN_USE", { is_lead_guest: true }), "acrescentar", "Ana")).toContain("titular");
    expect(mensagemDaLista(recusa("CODE_IN_USE", { is_lead_guest: false }), "acrescentar", "Ana")).toContain(
      "Ana já está na lista",
    );
  });

  it("estado da reserva: o rótulo em português, não o valor do enum", () => {
    const frase = mensagemDaLista(recusa("INVALID_STATE_TRANSITION", { status: "checked_out" }), "acrescentar");
    expect(frase).toContain("Estadia cumprida");
    expect(frase).not.toContain("checked_out");
  });

  it("ficha anonimizada explica o porquê e a saída", () => {
    expect(mensagemDaLista(recusa("CONTACT_ANONYMIZED"), "acrescentar")).toMatch(/cadastre-a de novo/);
  });

  it("contact_id inválido manda procurar de novo", () => {
    expect(
      mensagemDaLista(recusa("VALIDATION_ERROR", { contact_id: "contato não encontrado" }), "acrescentar"),
    ).toContain("Procure a pessoa de novo");
  });

  it("titular não sai: o caminho é editar a reserva", () => {
    expect(mensagemDaLista(recusa("RESOURCE_IN_USE", { is_lead_guest: true }), "retirar", "Carlos")).toContain(
      "titular",
    );
  });

  it("nunca ecoa o message da API", () => {
    for (const code of ["GUEST_LIST_FULL", "CODE_IN_USE", "CONTACT_ANONYMIZED", "INVALID_STATE_TRANSITION", "INTERNAL"] as const) {
      expect(mensagemDaLista(recusa(code), "acrescentar")).not.toContain("TEXTO CRU");
    }
  });
});

describe("quando recarregar o /full", () => {
  it("só nas recusas que dizem que a tela está velha", () => {
    expect(pedeRecarga({ code: "CODE_IN_USE" })).toBe(true);
    expect(pedeRecarga({ code: "GUEST_LIST_FULL" })).toBe(true);
    expect(pedeRecarga({ code: "INVALID_STATE_TRANSITION" })).toBe(true);
    expect(pedeRecarga({ code: "RESOURCE_IN_USE" })).toBe(true);
  });

  it("e não por reflexo: cada recarga grava uma linha de pii_access_log por hóspede", () => {
    expect(pedeRecarga({ code: "CONTACT_ANONYMIZED" })).toBe(false);
    expect(pedeRecarga({ code: "VALIDATION_ERROR" })).toBe(false);
    expect(pedeRecarga({ code: "NETWORK_ERROR" })).toBe(false);
    expect(pedeRecarga({ code: "FORBIDDEN" })).toBe(false);
  });
});
