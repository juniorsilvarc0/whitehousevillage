import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { normalizarCodigo } from "@/lib/api/codigos";
import { mensagemDoErro } from "@/lib/acoes/resultado";
import {
  caminhoDaOrdem,
  falhaNosCampos,
  mensagemDeManutencao,
  ordemJaAberta,
  pedeRecarga,
} from "@/lib/manutencao/mensagens";
import { avisoDoEncerramento, faseCurta, periodoPorExtenso } from "@/lib/manutencao/rotulos";

const ORDEM = "9f1c1e2a-0d3b-4f6a-9a1e-2b7c5d8e0f11";

const AQUI = path.dirname(fileURLToPath(import.meta.url));
const OPENAPI = path.resolve(AQUI, "../../../../api/openapi/openapi.yaml");

/** O enum de `components/responses/Erro.error.code`, lido do contrato. */
function codigosDoContrato(): string[] | null {
  if (!existsSync(OPENAPI)) return null;
  const fonte = readFileSync(OPENAPI, "utf8");
  const bloco = fonte.match(/enum: \[(VALIDATION_ERROR[\s\S]*?)\]/);
  if (!bloco) throw new Error("não achei o enum de error.code na OpenAPI — o contrato mudou de forma?");
  return bloco[1].split(",").map((c) => c.trim()).filter(Boolean);
}

const doContrato = codigosDoContrato();

describe("os dois códigos novos estão no espelho geral", () => {
  it("o 409 não vira INTERNAL — o código atravessa com o details", () => {
    // Sem isto, `normalizarCodigo` não tem ramo para 409: o segundo toque em
    // "Abrir ordem de manutenção" diria "erro no servidor" e perderia
    // `details.maintenance_order_id`, que é o caminho até a ordem existente.
    expect(normalizarCodigo("MAINTENANCE_ORDER_ALREADY_OPEN", 409)).toBe("MAINTENANCE_ORDER_ALREADY_OPEN");
    expect(normalizarCodigo("MAINTENANCE_ORDER_CLOSED", 409)).toBe("MAINTENANCE_ORDER_CLOSED");
  });

  it.skipIf(doContrato === null)("e são os do contrato, letra por letra", () => {
    expect(doContrato).toContain("MAINTENANCE_ORDER_ALREADY_OPEN");
    expect(doContrato).toContain("MAINTENANCE_ORDER_CLOSED");
  });

  it("cada um tem frase própria no dicionário geral", () => {
    const padrao = mensagemDoErro("INTERNAL");
    expect(mensagemDoErro("MAINTENANCE_ORDER_ALREADY_OPEN")).not.toBe(padrao);
    expect(mensagemDoErro("MAINTENANCE_ORDER_CLOSED")).not.toBe(padrao);
  });
});

describe("MAINTENANCE_ORDER_ALREADY_OPEN leva à ordem que já existe", () => {
  it("devolve o caminho do details.maintenance_order_id", () => {
    expect(ordemJaAberta({ code: "MAINTENANCE_ORDER_ALREADY_OPEN", details: { maintenance_order_id: ORDEM } })).toEqual({
      id: ORDEM,
      caminho: `/app/manutencao/${ORDEM}`,
    });
    expect(caminhoDaOrdem(ORDEM)).toBe(`/app/manutencao/${ORDEM}`);
  });

  it("outro código não inventa destino", () => {
    expect(ordemJaAberta({ code: "COUNT_ALREADY_OPEN", details: { maintenance_order_id: ORDEM } })).toBeNull();
    expect(ordemJaAberta({ code: "MAINTENANCE_ORDER_CLOSED", details: { maintenance_order_id: ORDEM } })).toBeNull();
  });

  it("409 sem o id também não inventa — a tela mostra a frase", () => {
    expect(ordemJaAberta({ code: "MAINTENANCE_ORDER_ALREADY_OPEN", details: {} })).toBeNull();
    expect(ordemJaAberta({ code: "MAINTENANCE_ORDER_ALREADY_OPEN", details: { maintenance_order_id: 7 } })).toBeNull();
  });
});

describe("o período no campo certo", () => {
  it("criação: block.from e block.to caem nas duas datas; block (unidade inativa) no período", () => {
    const r = falhaNosCampos(
      {
        code: "VALIDATION_ERROR",
        details: {
          "block.from": "O bloqueio começa hoje ou depois.",
          "block.to": "No máximo 365 noites.",
        },
      },
      "criacao",
    );
    expect(r.campos).toEqual({ block_from: "O bloqueio começa hoje ou depois.", block_to: "No máximo 365 noites." });
    expect(r.periodo).toBeNull();
    expect(r.geral).toBeNull();

    const inativa = falhaNosCampos({ code: "VALIDATION_ERROR", details: { block: "Unidade inativa não se bloqueia." } }, "criacao");
    expect(inativa.periodo).toBe("Unidade inativa não se bloqueia.");
    expect(inativa.geral).toBeNull();
  });

  it("PUT /block: from e to caem nas mesmas duas datas", () => {
    const r = falhaNosCampos(
      { code: "VALIDATION_ERROR", details: { from: "O início de um bloqueio em curso não muda.", to: "O fim não volta para antes de hoje." } },
      "bloqueio",
    );
    expect(r.campos).toEqual({
      block_from: "O início de um bloqueio em curso não muda.",
      block_to: "O fim não volta para antes de hoje.",
    });
  });

  it("os nomes não se misturam: block.from no PUT /block não é campo — vira mensagem geral", () => {
    const r = falhaNosCampos({ code: "VALIDATION_ERROR", details: { "block.from": "x" } }, "bloqueio");
    expect(r.campos).toEqual({});
    expect(r.geral).not.toBeNull();
  });

  it("DATE_CONFLICT vai para o período, com a unidade e as datas — e não para o topo", () => {
    const r = falhaNosCampos(
      { code: "DATE_CONFLICT", details: { unit_code: "AP-03", period: "[2026-11-10,2026-11-15)" } },
      "criacao",
    );
    expect(r.geral).toBeNull();
    expect(r.campos).toEqual({});
    expect(r.periodo).toContain("AP-03");
    // `details.period` é o período PEDIDO, dito com os nomes dos campos — o
    // `to` exclusivo é "volta à venda", não uma noite bloqueada.
    expect(r.periodo).toMatch(/^O período pedido \(primeiro dia 10\/11\/2026, volta à venda em 15\/11\/2026\) cruza/);
    expect(r.periodo).toMatch(/nada foi criado/i);
  });

  it("DATE_CONFLICT sem details ainda fala do período, sem colchete solto", () => {
    const r = falhaNosCampos({ code: "DATE_CONFLICT", details: {} }, "bloqueio");
    expect(r.periodo).toMatch(/bloqueio continua como estava/i);
    expect(r.periodo).not.toMatch(/[[)]/);
  });

  it("cost_cents do contrato cai no campo 'cost' do formulário", () => {
    const r = falhaNosCampos({ code: "VALIDATION_ERROR", details: { cost_cents: "Maior que zero." } }, "criacao");
    expect(r.campos).toEqual({ cost: "Maior que zero." });
  });

  it("422 sem nada aproveitável vira mensagem geral pelo código", () => {
    const r = falhaNosCampos({ code: "VALIDATION_ERROR", details: { unknown_field: "x" } }, "criacao");
    expect(r.geral).toBe(mensagemDoErro("VALIDATION_ERROR"));
  });
});

describe("a frase pelo código", () => {
  it("MAINTENANCE_ORDER_CLOSED explica pelo details.editable", () => {
    expect(mensagemDeManutencao({ code: "MAINTENANCE_ORDER_CLOSED", details: { editable: "so_custo" } })).toMatch(/só aceita o custo/);
    expect(mensagemDeManutencao({ code: "MAINTENANCE_ORDER_CLOSED", details: { editable: "nada" } })).toMatch(/cancelada/);
  });

  it("os dois 409 de estado pedem recarga; o resto não", () => {
    expect(pedeRecarga({ code: "MAINTENANCE_ORDER_CLOSED" })).toBe(true);
    expect(pedeRecarga({ code: "INVALID_STATE_TRANSITION" })).toBe(true);
    expect(pedeRecarga({ code: "DATE_CONFLICT" })).toBe(false);
  });

  it("INVALID_STATE_TRANSITION aqui não fala de reserva", () => {
    expect(mensagemDeManutencao({ code: "INVALID_STATE_TRANSITION", details: {} }, "transicao")).not.toMatch(/reserva/i);
  });
});

describe("o half-open por extenso", () => {
  it("to é exclusivo: a última noite é a véspera, e o to fica livre", () => {
    const frase = periodoPorExtenso({ from: "2026-11-10", to: "2026-11-15", nights: 5 });
    expect(frase).toContain("10/11/2026 a 14/11/2026");
    expect(frase).toContain("5 noites");
    expect(frase).toContain("livre a partir de 15/11/2026");
  });

  it("uma noite só", () => {
    expect(periodoPorExtenso({ from: "2026-11-10", to: "2026-11-11", nights: 1 })).toContain("noite de 10/11/2026 (1 noite)");
  });

  it("a fase vem da API — a frase só a escreve", () => {
    const bloqueio = { from: "2026-11-10", to: "2026-11-15" };
    expect(faseCurta({ ...bloqueio, phase: "agendado" })).toMatch(/^Bloqueio agendado/);
    expect(faseCurta({ ...bloqueio, phase: "em_curso" })).toBe("Bloqueio em curso até a noite de 14/11/2026");
    expect(faseCurta({ ...bloqueio, phase: "encerrado" })).toMatch(/^Bloqueio encerrado/);
    expect(faseCurta({ ...bloqueio, phase: "liberado" })).toMatch(/^Bloqueio liberado/);
  });

  it("encerrar sem bloqueio não promete mexer no calendário", () => {
    expect(avisoDoEncerramento(null)).toMatch(/nada muda/i);
  });

  it("a liberação não promete 'termina hoje' — o bloqueio que começa hoje sai inteiro", () => {
    // Um bloqueio com `from` = hoje aparece como `em_curso`, e encerrar a
    // ordem nesse dia o solta INTEIRO (`maintenance.ReleaseOn`). Dizer
    // "o que está em curso termina hoje" mentiria justamente nesse caso.
    const frase = avisoDoEncerramento({ phase: "em_curso" });
    expect(frase).not.toMatch(/termina hoje/i);
    expect(frase).toMatch(/começa hoje — sai inteiro/i);
    expect(frase).toMatch(/começou antes de hoje/i);
    expect(frase).toMatch(/noite de hoje volta à venda/i);
  });
});
