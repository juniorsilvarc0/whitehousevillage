import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { normalizarCodigo } from "@/lib/api/codigos";
import { mensagemDoErro } from "@/lib/acoes/resultado";
import {
  conflitosDe,
  falhaNoFormulario,
  falhaNosHorarios,
  fraseDosReprogramados,
  mensagemDaAgenda,
  pedeRecarga,
} from "@/lib/agenda/mensagens";
import { fraseDoAviso, fraseDoConflito } from "@/lib/agenda/rotulos";

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

describe("AGENDA_EVENT_CLOSED no espelho geral", () => {
  it("o 409 não vira INTERNAL — o código atravessa com o details", () => {
    expect(normalizarCodigo("AGENDA_EVENT_CLOSED", 409)).toBe("AGENDA_EVENT_CLOSED");
  });

  it.skipIf(doContrato === null)("e é o do contrato, letra por letra", () => {
    expect(doContrato).toContain("AGENDA_EVENT_CLOSED");
  });

  it("tem frase própria no dicionário geral", () => {
    expect(mensagemDoErro("AGENDA_EVENT_CLOSED")).not.toBe(mensagemDoErro("INTERNAL"));
    expect(mensagemDoErro("AGENDA_EVENT_CLOSED")).toMatch(/concluído ou desmarcado/);
  });

  it("e a agenda diz o que ainda muda, pelo details.editable", () => {
    expect(mensagemDaAgenda({ code: "AGENDA_EVENT_CLOSED", details: { editable: "so_notas" } })).toMatch(/só a anotação/);
    expect(mensagemDaAgenda({ code: "AGENDA_EVENT_CLOSED", details: { editable: "nada" } })).toMatch(/desmarcado/);
    expect(pedeRecarga({ code: "AGENDA_EVENT_CLOSED" })).toBe(true);
  });
});

describe("409 UNIT_NOT_AVAILABLE vai para junto do horário", () => {
  const falha = {
    code: "UNIT_NOT_AVAILABLE" as const,
    details: {
      conflicts: [
        { unit_id: "u", unit_code: "AP-02", kind: "limpeza", starts_at: "2026-11-20T11:00:00-03:00", ends_at: "2026-11-20T14:00:00-03:00" },
        { unit_id: "u", unit_code: "AP-02", kind: "estadia", starts_at: "2026-11-18T14:00:00-03:00", ends_at: "2026-11-20T11:00:00-03:00" },
        { lixo: true },
      ],
    },
  };

  it("cada conflito numa linha — a unidade, o que a ocupa e quando; nunca quem", () => {
    const leitura = falhaNoFormulario(falha, "criacao", false);
    expect(leitura.geral).toBeNull();
    expect(leitura.campos).toEqual({});
    expect(leitura.quando?.mensagem).toMatch(/nada foi marcado/i);
    expect(leitura.quando?.conflitos).toEqual([
      "AP-02 · limpeza · 20/11, das 11:00 às 14:00",
      "AP-02 · hóspede na unidade · de 18/11 14:00 a 20/11 11:00",
    ]);
  });

  it("linha fora da forma é descartada, não derruba a tela", () => {
    expect(conflitosDe(falha.details)).toHaveLength(2);
    expect(conflitosDe({ conflicts: "nada" })).toEqual([]);
  });

  it("a frase do conflito sai do kind", () => {
    expect(
      fraseDoConflito({ unit_id: "u", unit_code: "AP-01", kind: "manutencao", starts_at: "2026-11-20T00:00:00-03:00", ends_at: "2026-11-21T00:00:00-03:00" }),
    ).toBe("AP-01 · manutenção · 20/11, o dia inteiro");
  });
});

describe("403 assignee_not_actor fixa o responsável", () => {
  it("vai para o campo responsável e pede para fixá-lo no próprio usuário", () => {
    const leitura = falhaNoFormulario(
      { code: "FORBIDDEN", details: { field: "assignee_id", reason: "assignee_not_actor", scope: "own" } },
      "criacao",
      false,
    );
    expect(leitura.fixarResponsavel).toBe(true);
    expect(leitura.campos.assignee_id).toMatch(/sempre você/);
    expect(leitura.geral).toBeNull();
  });

  it("o 403 sem esse motivo continua sendo o 403 de sempre", () => {
    const leitura = falhaNoFormulario({ code: "FORBIDDEN", details: {} }, "criacao", false);
    expect(leitura.fixarResponsavel).toBe(false);
    expect(leitura.geral).toMatch(/permissão/);
  });

  it("own_scope_not_applicable explica por que o corretor não bloqueia horário", () => {
    expect(mensagemDaAgenda({ code: "FORBIDDEN", details: { reason: "own_scope_not_applicable" } }, "bloqueio")).toMatch(/operação inteira/);
  });
});

describe("422 por campo: o DTO fala starts_at, o formulário fala dia e hora", () => {
  it("starts_at cai no dia; ends_at na hora de término — ou no último dia, no dia inteiro", () => {
    const d = { starts_at: "mais de 365 dias no passado.", ends_at: "deve ser posterior a `starts_at`." };
    expect(falhaNoFormulario({ code: "VALIDATION_ERROR", details: d }, "criacao", false).campos).toEqual({
      data_inicio: d.starts_at,
      hora_fim: d.ends_at,
    });
    expect(falhaNoFormulario({ code: "VALIDATION_ERROR", details: d }, "criacao", true).campos).toEqual({
      data_inicio: d.starts_at,
      data_fim: d.ends_at,
    });
  });

  it("o que não coube em campo nenhum vira a mensagem geral, pelo código", () => {
    expect(falhaNoFormulario({ code: "VALIDATION_ERROR", details: { foo: "bar" } }, "criacao", false).geral).toBe(
      mensagemDoErro("VALIDATION_ERROR"),
    );
  });
});

describe("INVALID_STATE_TRANSITION pelo details.reason", () => {
  it("segue_a_reserva e nao_comecou têm frase própria", () => {
    expect(mensagemDaAgenda({ code: "INVALID_STATE_TRANSITION", details: { reason: "segue_a_reserva" } }, "conclusao")).toMatch(/pela reserva/);
    expect(mensagemDaAgenda({ code: "INVALID_STATE_TRANSITION", details: { reason: "segue_a_reserva" } }, "desmarcacao")).toMatch(/cancelada/);
    expect(mensagemDaAgenda({ code: "INVALID_STATE_TRANSITION", details: { reason: "nao_comecou" } }, "conclusao")).toMatch(/a partir do dia/);
  });
});

describe("os horários: todas as violações de uma vez, cada uma no seu campo", () => {
  it("o índice do intervalo é o da lista enviada", () => {
    const leitura = falhaNosHorarios({
      code: "VALIDATION_ERROR",
      details: {
        checkin_time: "o próximo hóspede entra depois que a limpeza termina.",
        "business_hours[2].end": "o fim tem de ser depois do início.",
        "business_hours[3]": "sobrepõe outro intervalo do mesmo dia.",
        business_hours: "no máximo 4 intervalos por dia.",
      },
    });
    expect(leitura.campos).toEqual({ checkin_time: "o próximo hóspede entra depois que a limpeza termina." });
    expect(leitura.intervalos).toEqual({
      2: { end: "o fim tem de ser depois do início." },
      3: { linha: "sobrepõe outro intervalo do mesmo dia." },
    });
    expect(leitura.funcionamento).toBe("no máximo 4 intervalos por dia.");
    expect(leitura.geral).toBeNull();
  });

  it("403 diz que gravar é decisão do dono", () => {
    expect(falhaNosHorarios({ code: "FORBIDDEN", details: {} }).geral).toMatch(/Configurações/);
  });

  it("quantos eventos foram reprogramados", () => {
    expect(fraseDosReprogramados(0)).toMatch(/Nenhum evento/);
    expect(fraseDosReprogramados(1)).toMatch(/1 evento automático foi/);
    expect(fraseDosReprogramados(12)).toMatch(/12 eventos automáticos foram/);
  });
});

describe("o aviso de horário: a frase sai do kind, nunca do message da API", () => {
  it("cada kind tem a sua frase, com a janela quando vier", () => {
    expect(fraseDoAviso({ kind: "fora_do_horario" })).toMatch(/fora do horário de funcionamento/);
    expect(
      fraseDoAviso({ kind: "bloqueio_de_horario", starts_at: "2026-11-20T14:00:00-03:00", ends_at: "2026-11-20T16:00:00-03:00" }),
    ).toBe("Cai num horário bloqueado da operação (20/11, das 14:00 às 16:00).");
    expect(fraseDoAviso({ kind: "responsavel_ocupado" })).toMatch(/outro compromisso/);
  });
});
