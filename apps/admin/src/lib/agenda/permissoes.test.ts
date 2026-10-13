import { describe, expect, it } from "vitest";

import type { Acao, Escopo, Permissao } from "@/lib/api/types";
import { botoesDoEvento, camposDoEvento, permissoesDaAgenda, verboDaEdicao } from "@/lib/agenda/permissoes";
import type { EventoDaAgenda } from "@/lib/agenda/tipos";

/**
 * Os botões do evento saem de `allowed_actions` e `editable` — **da resposta
 * da API** —, cruzados com a matriz em `agenda`. Nunca do `status`, do `type`
 * ou da `origin`.
 */

function matriz(acoes: Acao[], escopo: Escopo = "all", recurso = "agenda"): Permissao[] {
  return acoes.map((action) => ({ resource: recurso, action, scope: escopo }));
}

const TUDO = permissoesDaAgenda(matriz(["ver", "criar", "editar", "excluir"]));

type Estado = Pick<EventoDaAgenda, "allowed_actions" | "editable">;
const MANUAL_HOJE: Estado = { allowed_actions: ["complete", "cancel"], editable: "tudo" };
const MANUAL_AMANHA: Estado = { allowed_actions: ["cancel"], editable: "tudo" };
const CHECKIN_AUTOMATICO: Estado = { allowed_actions: [], editable: "segue_a_reserva" };
const LIMPEZA_AUTOMATICA_HOJE: Estado = { allowed_actions: ["complete"], editable: "segue_a_reserva" };
const CONCLUIDO: Estado = { allowed_actions: [], editable: "so_notas" };
const DESMARCADO: Estado = { allowed_actions: [], editable: "nada" };

describe("os botões a partir de allowed_actions e editable", () => {
  it("manual de hoje: conclui, desmarca e edita", () => {
    expect(botoesDoEvento(MANUAL_HOJE, TUDO)).toEqual({ concluir: true, desmarcar: true, editar: true });
  });

  it("manual de amanhã: a API ainda não deixa concluir — o painel não decide isso", () => {
    expect(botoesDoEvento(MANUAL_AMANHA, TUDO)).toEqual({ concluir: false, desmarcar: true, editar: true });
  });

  it("check-in automático: nada de concluir nem desmarcar (segue a reserva); edita o horário", () => {
    expect(botoesDoEvento(CHECKIN_AUTOMATICO, TUDO)).toEqual({ concluir: false, desmarcar: false, editar: true });
  });

  it("limpeza automática de hoje: conclui (quem limpa marca no celular), não desmarca", () => {
    expect(botoesDoEvento(LIMPEZA_AUTOMATICA_HOJE, TUDO)).toEqual({ concluir: true, desmarcar: false, editar: true });
  });

  it("concluído edita (a anotação); desmarcado e derivado, nada", () => {
    expect(botoesDoEvento(CONCLUIDO, TUDO)).toEqual({ concluir: false, desmarcar: false, editar: true });
    expect(botoesDoEvento(DESMARCADO, TUDO)).toEqual({ concluir: false, desmarcar: false, editar: false });
  });

  it("a matriz corta: sem editar não conclui nem edita; sem excluir não desmarca", () => {
    const soCriar = permissoesDaAgenda(matriz(["ver", "criar"], "own"));
    expect(botoesDoEvento(MANUAL_HOJE, soCriar)).toEqual({ concluir: false, desmarcar: false, editar: false });
    const semExcluir = permissoesDaAgenda(matriz(["ver", "editar"]));
    expect(botoesDoEvento(MANUAL_HOJE, semExcluir).desmarcar).toBe(false);
  });
});

describe("a máquina de estados é da API, não do painel", () => {
  it("evento 'agendado' com allowed_actions vazio não ganha botão nenhum de transição", () => {
    const e: Pick<EventoDaAgenda, "status" | "type" | "origin" | "allowed_actions" | "editable"> = {
      status: "agendado",
      type: "visita",
      origin: "manual",
      allowed_actions: [],
      editable: "tudo",
    };
    const b = botoesDoEvento(e, TUDO);
    expect(b.concluir).toBe(false);
    expect(b.desmarcar).toBe(false);
  });

  it("e o contrário: se a API um dia deixar concluir o check-in por aqui, o botão aparece sem mudar o painel", () => {
    expect(botoesDoEvento({ allowed_actions: ["complete"], editable: "nada" }, TUDO).concluir).toBe(true);
  });
});

describe("os campos abertos saem de editable", () => {
  it("tudo: todos", () => {
    expect([...camposDoEvento("tudo")].sort()).toEqual(
      ["all_day", "assignee_id", "contact_id", "notes", "quando", "reservation_id", "title", "type", "unit_id"].sort(),
    );
  });

  it("segue a reserva: horário, responsável e anotação — tipo, unidade, reserva e pessoa são dela", () => {
    expect([...camposDoEvento("segue_a_reserva")].sort()).toEqual(["assignee_id", "notes", "quando"]);
  });

  it("só notas: só a anotação; nada: nenhum", () => {
    expect([...camposDoEvento("so_notas")]).toEqual(["notes"]);
    expect([...camposDoEvento("nada")]).toEqual([]);
  });

  it("o verbo: PUT substitui o manual inteiro; PATCH muda o que o automático e o concluído aceitam", () => {
    expect(verboDaEdicao("tudo")).toBe("PUT");
    expect(verboDaEdicao("segue_a_reserva")).toBe("PATCH");
    expect(verboDaEdicao("so_notas")).toBe("PATCH");
    expect(verboDaEdicao("nada")).toBeNull();
  });
});

describe("o escopo só muda o que o formulário oferece", () => {
  it("o corretor (agenda em own, ver+criar): marca para si; não bloqueia horário da operação", () => {
    const p = permissoesDaAgenda(matriz(["ver", "criar"], "own"));
    expect(p.criar).toBe(true);
    expect(p.escopoAoCriar).toBe("own");
    expect(p.bloquearHorario).toBe(false);
    expect(p.gravarHorarios).toBe(false);
  });

  it("gravar os horários pede settings:editar — agenda:editar não basta", () => {
    expect(permissoesDaAgenda(matriz(["ver", "criar", "editar", "excluir"])).gravarHorarios).toBe(false);
    expect(
      permissoesDaAgenda([...matriz(["ver"]), { resource: "settings", action: "editar", scope: "all" }]).gravarHorarios,
    ).toBe(true);
  });

  it("a operação em all bloqueia e remove bloqueio de horário", () => {
    expect(TUDO.bloquearHorario).toBe(true);
    expect(TUDO.removerBloqueio).toBe(true);
  });
});
