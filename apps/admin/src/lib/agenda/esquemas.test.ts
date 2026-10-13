import { describe, expect, it } from "vitest";

import {
  HorariosFormulario,
  bloqueioParaEntrada,
  bloqueioVazio,
  esquemaDoEvento,
  eventoParaAtualizar,
  eventoParaCriar,
  eventoParaSubstituir,
  eventoVazio,
  fimDoCampo,
  horariosParaEntrada,
  valoresDoEvento,
  valoresDosHorarios,
} from "@/lib/agenda/esquemas";
import type { ConfiguracaoDaAgenda, EventoDaAgenda } from "@/lib/agenda/tipos";

const UNIDADE = "11111111-1111-4111-8111-111111111111";
const EU = "22222222-2222-4222-8222-222222222222";

describe("o corpo do POST — a hora digitada é a da casa", () => {
  it("evento com hora vai com o offset da casa nas duas pontas", () => {
    const corpo = eventoParaCriar(
      eventoVazio({ title: " Visita da família Andrade ", data_inicio: "2026-11-20", hora_inicio: "14:00", hora_fim: "15:00", unit_id: UNIDADE }),
    );
    expect(corpo).toEqual({
      type: "visita",
      title: "Visita da família Andrade",
      starts_at: "2026-11-20T14:00:00-03:00",
      ends_at: "2026-11-20T15:00:00-03:00",
      all_day: false,
      unit_id: UNIDADE,
      reservation_id: null,
      contact_id: null,
      assignee_id: null,
      notes: null,
    });
  });

  it("sem hora de término, `ends_at` não vai: a API usa um passo da grade", () => {
    const corpo = eventoParaCriar(eventoVazio({ title: "Visita", data_inicio: "2026-11-20", hora_inicio: "14:00" }));
    expect(corpo).not.toHaveProperty("ends_at");
  });

  it("dia inteiro: meia-noite DA CASA no primeiro dia e no dia seguinte ao último", () => {
    const corpo = eventoParaCriar(
      eventoVazio({ type: "evento", title: "Casamento", all_day: true, data_inicio: "2026-11-20", data_fim: "2026-11-22" }),
    );
    expect(corpo.starts_at).toBe("2026-11-20T00:00:00-03:00");
    // O último dia é inclusivo na tela e exclusivo no fio.
    expect(corpo.ends_at).toBe("2026-11-23T00:00:00-03:00");
    expect(corpo.all_day).toBe(true);
  });

  it("dia inteiro de um dia só: o fim é a meia-noite seguinte", () => {
    const corpo = eventoParaCriar(eventoVazio({ type: "limpeza", title: "Faxina geral", all_day: true, data_inicio: "2026-11-20" }));
    expect(corpo.ends_at).toBe("2026-11-21T00:00:00-03:00");
  });

  it("o evento que passa da meia-noite termina no dia escolhido", () => {
    const corpo = eventoParaCriar(
      eventoVazio({ type: "evento", title: "Festa", data_inicio: "2026-11-20", hora_inicio: "22:00", data_fim: "2026-11-21", hora_fim: "02:00" }),
    );
    expect(corpo.ends_at).toBe("2026-11-21T02:00:00-03:00");
  });
});

describe("o corpo da edição sai do editable", () => {
  const manual = valoresDoEvento({
    id: "e",
    type: "visita",
    origin: "manual",
    status: "agendado",
    title: "Visita",
    starts_at: "2026-11-20T14:00:00-03:00",
    ends_at: "2026-11-20T14:30:00-03:00",
    all_day: false,
    date: "2026-11-20",
    units: [{ unit_id: UNIDADE, unit_code: "AP-01" }],
    assignee_id: EU,
    has_unit_conflict: false,
    allowed_actions: ["complete", "cancel"],
    editable: "tudo",
  });

  it("os valores do formulário saem do evento, na hora da casa", () => {
    expect(manual).toMatchObject({ data_inicio: "2026-11-20", hora_inicio: "14:00", hora_fim: "14:30", data_fim: "", unit_id: UNIDADE, assignee_id: EU });
  });

  it("PUT (tudo): o manual inteiro, anulável vazio vira null", () => {
    expect(eventoParaSubstituir({ ...manual, notes: "" })).toEqual({
      type: "visita",
      title: "Visita",
      starts_at: "2026-11-20T14:00:00-03:00",
      ends_at: "2026-11-20T14:30:00-03:00",
      all_day: false,
      unit_id: UNIDADE,
      reservation_id: null,
      contact_id: null,
      assignee_id: EU,
      notes: null,
    });
  });

  it("PATCH (segue a reserva): só horário, responsável e anotação — nunca tipo, título ou unidade", () => {
    const corpo = eventoParaAtualizar({ ...manual, hora_inicio: "15:00", hora_fim: "15:30" }, "segue_a_reserva");
    expect(Object.keys(corpo).sort()).toEqual(["assignee_id", "ends_at", "notes", "starts_at"]);
    expect(corpo.starts_at).toBe("2026-11-20T15:00:00-03:00");
  });

  it("PATCH (só notas): só a anotação — qualquer outra chave no concluído seria 409", () => {
    expect(eventoParaAtualizar({ ...manual, notes: "Chave na portaria" }, "so_notas")).toEqual({ notes: "Chave na portaria" });
  });
});

describe("o schema confere a forma, não a regra", () => {
  it("criar exige tipo manual, título, dia e hora de início — o término é opcional", () => {
    const r = esquemaDoEvento("criar").safeParse(eventoVazio({ type: "checkin", data_inicio: "", hora_inicio: "" }));
    expect(r.success).toBe(false);
    const caminhos = r.success ? [] : r.error.issues.map((i) => i.path.join("."));
    expect(caminhos).toEqual(expect.arrayContaining(["type", "title", "data_inicio", "hora_inicio"]));
    expect(caminhos).not.toContain("hora_fim");
  });

  it("fim antes do início NÃO é recusado aqui: é regra da API, e a recusa dela cai no campo", () => {
    const r = esquemaDoEvento("criar").safeParse(
      eventoVazio({ title: "Visita", data_inicio: "2026-11-20", hora_inicio: "15:00", hora_fim: "14:00" }),
    );
    expect(r.success).toBe(true);
  });

  it("no automático não confere tipo nem título — eles não vão no corpo", () => {
    const r = esquemaDoEvento("segue_a_reserva").safeParse(
      eventoVazio({ type: "checkin", title: "", data_inicio: "2026-11-20", hora_inicio: "14:00", hora_fim: "14:30" }),
    );
    expect(r.success).toBe(true);
  });

  it("só notas confere só a anotação", () => {
    expect(esquemaDoEvento("so_notas").safeParse(eventoVazio({ type: "checkin", title: "" })).success).toBe(true);
  });
});

describe("o bloqueio de horário", () => {
  it("vai com o offset da casa; o de dia inteiro, de meia-noite a meia-noite", () => {
    expect(bloqueioParaEntrada(bloqueioVazio({ reason: "Treino", data_inicio: "2026-11-20", hora_inicio: "14:00", hora_fim: "16:00" }))).toEqual({
      starts_at: "2026-11-20T14:00:00-03:00",
      ends_at: "2026-11-20T16:00:00-03:00",
      all_day: false,
      reason: "Treino",
    });
    expect(bloqueioParaEntrada(bloqueioVazio({ reason: "Dedetização", all_day: true, data_inicio: "2026-11-21" }))).toMatchObject({
      starts_at: "2026-11-21T00:00:00-03:00",
      ends_at: "2026-11-22T00:00:00-03:00",
    });
  });
});

describe("os horários da operação", () => {
  const config: ConfiguracaoDaAgenda = {
    timezone: "America/Sao_Paulo",
    today: "2026-11-20",
    checkin_time: "14:00",
    checkout_time: "11:00",
    cleaning_start: "11:00",
    cleaning_end: "14:00",
    business_hours: [
      { weekday: 1, start: "08:00", end: "20:00" },
      { weekday: 6, start: "18:00", end: "24:00" },
    ],
    slot_minutes: 30,
    buffer_minutes: 15,
    provisional: true,
    updated_at: "2026-10-13T10:00:00-03:00",
  };

  it("ida e volta: o corpo do PUT é a configuração inteira, sem os campos só de leitura", () => {
    const corpo = horariosParaEntrada(valoresDosHorarios(config));
    expect(corpo).toEqual({
      checkin_time: "14:00",
      checkout_time: "11:00",
      cleaning_start: "11:00",
      cleaning_end: "14:00",
      business_hours: config.business_hours,
      slot_minutes: 30,
      buffer_minutes: 15,
    });
    expect(corpo).not.toHaveProperty("today");
    expect(corpo).not.toHaveProperty("provisional");
  });

  it("o seletor de hora não escreve 24:00: 00:00 no FIM do intervalo é a meia-noite que fecha o dia", () => {
    expect(valoresDosHorarios(config).business_hours[1]!.end).toBe("00:00");
    expect(fimDoCampo("00:00")).toBe("24:00");
    expect(fimDoCampo("20:00")).toBe("20:00");
  });

  it("a forma é conferida aqui; a coerência (check-out ≤ limpeza ≤ check-in) é da API", () => {
    const incoerente = { ...valoresDosHorarios(config), checkin_time: "09:00" };
    expect(HorariosFormulario.safeParse(incoerente).success).toBe(true);
    expect(HorariosFormulario.safeParse({ ...valoresDosHorarios(config), checkin_time: "14h" }).success).toBe(false);
  });
});

describe("a unidade que o PUT devolve é a gravada, não a da reserva", () => {
  const base: EventoDaAgenda = {
    id: "e",
    type: "limpeza",
    origin: "manual",
    status: "agendado",
    title: "Faxina do meio da estadia",
    starts_at: "2026-11-20T09:00:00-03:00",
    ends_at: "2026-11-20T11:00:00-03:00",
    all_day: false,
    date: "2026-11-20",
    units: [{ unit_id: UNIDADE, unit_code: "AP-01" }],
    reservation_id: "55555555-5555-4555-8555-555555555555",
    has_unit_conflict: false,
    allowed_actions: ["cancel"],
    editable: "tudo",
  };

  it("com `unit_id` na resposta, vale ele — mesmo nulo (o evento ocupa a reserva inteira)", () => {
    expect(valoresDoEvento({ ...base, unit_id: null }).unit_id).toBe("");
    expect(valoresDoEvento({ ...base, unit_id: UNIDADE }).unit_id).toBe(UNIDADE);
  });

  it("sem ele, lê `units`: a reserva de várias unidades fica 'as da reserva'", () => {
    expect(valoresDoEvento(base).unit_id).toBe(UNIDADE);
    expect(
      valoresDoEvento({ ...base, units: [...base.units, { unit_id: "22222222-2222-4222-8222-222222222222", unit_code: "AP-02" }] }).unit_id,
    ).toBe("");
  });

  it("o automático nunca leva unidade: ela é da reserva", () => {
    expect(valoresDoEvento({ ...base, origin: "reservation", editable: "segue_a_reserva" }).unit_id).toBe("");
  });
});

describe("a edição de dia inteiro mostra o último dia, inclusivo", () => {
  it("de 20 a 22 (ends_at = meia-noite de 23)", () => {
    const valores = valoresDoEvento({
      id: "e",
      type: "evento",
      origin: "manual",
      status: "agendado",
      title: "Casamento",
      starts_at: "2026-11-20T00:00:00-03:00",
      ends_at: "2026-11-23T00:00:00-03:00",
      all_day: true,
      date: "2026-11-20",
      units: [],
      has_unit_conflict: false,
      allowed_actions: [],
      editable: "tudo",
    } satisfies EventoDaAgenda);
    expect(valores).toMatchObject({ all_day: true, data_inicio: "2026-11-20", data_fim: "2026-11-22" });
    expect(eventoParaSubstituir(valores).ends_at).toBe("2026-11-23T00:00:00-03:00");
  });
});
