import { describe, expect, it } from "vitest";

import {
  abertoNoDia,
  bloqueiosNoDia,
  diaInteiroNoDia,
  dispor,
  faixaDeHoras,
  fechadoNoDia,
  trechoNoDia,
  type Trecho,
} from "@/lib/agenda/grade";
import type { BloqueioDeHorario, EventoDaAgenda, IntervaloDeFuncionamento } from "@/lib/agenda/tipos";

function evento(parcial: Partial<EventoDaAgenda>): EventoDaAgenda {
  return {
    id: "e",
    type: "visita",
    origin: "manual",
    status: "agendado",
    title: "Visita",
    starts_at: "2026-11-20T14:00:00-03:00",
    ends_at: "2026-11-20T14:30:00-03:00",
    all_day: false,
    date: "2026-11-20",
    units: [],
    has_unit_conflict: false,
    allowed_actions: [],
    editable: "tudo",
    ...parcial,
  };
}

function trecho(id: string, inicio: number, fim: number): Trecho {
  return { evento: evento({ id }), inicio, fim, continua: false, segue: false };
}

describe("o trecho de cada dia, em minutos da casa", () => {
  it("o evento das 14:00 às 14:30 ocupa 840–870", () => {
    expect(trechoNoDia(evento({}), "2026-11-20")).toMatchObject({ inicio: 840, fim: 870, continua: false, segue: false });
    expect(trechoNoDia(evento({}), "2026-11-21")).toBeNull();
  });

  it("o que atravessa a meia-noite vira dois trechos, um em cada dia", () => {
    const festa = evento({ starts_at: "2026-11-20T22:00:00-03:00", ends_at: "2026-11-21T02:00:00-03:00" });
    expect(trechoNoDia(festa, "2026-11-20")).toMatchObject({ inicio: 1320, fim: 1440, segue: true });
    expect(trechoNoDia(festa, "2026-11-21")).toMatchObject({ inicio: 0, fim: 120, continua: true });
  });

  it("o de dia inteiro não entra na grade: vai para a faixa de cima, em todo dia que toca", () => {
    const manutencao = evento({
      all_day: true,
      starts_at: "2026-11-20T00:00:00-03:00",
      ends_at: "2026-11-22T00:00:00-03:00",
    });
    expect(trechoNoDia(manutencao, "2026-11-20")).toBeNull();
    expect(diaInteiroNoDia([manutencao], "2026-11-20")).toHaveLength(1);
    expect(diaInteiroNoDia([manutencao], "2026-11-21")).toHaveLength(1);
    expect(diaInteiroNoDia([manutencao], "2026-11-22")).toHaveLength(0);
  });
});

describe("o layout de colisão em colunas", () => {
  it("quem não se sobrepõe fica sozinho, com a largura inteira", () => {
    const d = dispor([trecho("a", 600, 660), trecho("b", 660, 720)]);
    expect(d.map((t) => [t.evento.id, t.coluna, t.colunas])).toEqual([
      ["a", 0, 1],
      ["b", 0, 1],
    ]);
  });

  it("dois que se sobrepõem dividem a largura, lado a lado", () => {
    const d = dispor([trecho("a", 600, 720), trecho("b", 630, 690)]);
    expect(d.map((t) => [t.evento.id, t.coluna, t.colunas])).toEqual([
      ["a", 0, 2],
      ["b", 1, 2],
    ]);
  });

  it("o grupo é a cadeia de sobreposições; a coluna livre é reaproveitada", () => {
    // a 10–12, b 10:30–11, c 11–11:30 (cabe embaixo de b), d 13–14 (outro grupo).
    const d = dispor([trecho("a", 600, 720), trecho("b", 630, 660), trecho("c", 660, 690), trecho("d", 780, 840)]);
    const porId = Object.fromEntries(d.map((t) => [t.evento.id, [t.coluna, t.colunas]]));
    expect(porId).toEqual({ a: [0, 2], b: [1, 2], c: [1, 2], d: [0, 1] });
  });

  it("o tamanho DESENHADO decide: dois cartões curtos colados não se sobrepõem na tela", () => {
    // 10:00–10:10 e 10:10–10:20: sem altura mínima, lado nenhum; com 30 min
    // desenhados, o primeiro invade o segundo — e eles vão lado a lado.
    expect(dispor([trecho("a", 600, 610), trecho("b", 610, 620)]).map((t) => t.colunas)).toEqual([1, 1]);
    expect(dispor([trecho("a", 600, 610), trecho("b", 610, 620)], 30).map((t) => t.colunas)).toEqual([2, 2]);
  });
});

describe("o fundo da grade: funcionamento e bloqueios de horário", () => {
  // Segunda a sexta das 08:00 às 12:00 e das 14:00 às 18:00; sábado até 24:00; domingo fechado.
  const horarios: IntervaloDeFuncionamento[] = [
    ...[1, 2, 3, 4, 5].flatMap((d) => [
      { weekday: d, start: "08:00", end: "12:00" },
      { weekday: d, start: "14:00", end: "18:00" },
    ]),
    { weekday: 6, start: "10:00", end: "24:00" },
  ];

  it("o dia da semana é o ISO do contrato (20/11/2026 é sexta = 5)", () => {
    expect(abertoNoDia(horarios, "2026-11-20")).toEqual([
      { inicio: 480, fim: 720 },
      { inicio: 840, fim: 1080 },
    ]);
    expect(abertoNoDia(horarios, "2026-11-21")).toEqual([{ inicio: 600, fim: 1440 }]);
  });

  it("o sombreado é o complemento do funcionamento dentro da faixa desenhada", () => {
    expect(fechadoNoDia(horarios, "2026-11-20", { inicio: 420, fim: 1200 })).toEqual([
      { inicio: 420, fim: 480 },
      { inicio: 720, fim: 840 },
      { inicio: 1080, fim: 1200 },
    ]);
  });

  it("dia sem intervalo é dia fechado: sombreado inteiro", () => {
    expect(fechadoNoDia(horarios, "2026-11-22", { inicio: 480, fim: 1200 })).toEqual([{ inicio: 480, fim: 1200 }]);
  });

  it("o bloqueio de horário da operação no dia, em minutos — e o de dia inteiro marcado como tal", () => {
    const treino: BloqueioDeHorario = {
      id: "b1",
      starts_at: "2026-11-20T14:00:00-03:00",
      ends_at: "2026-11-20T16:00:00-03:00",
      all_day: false,
      reason: "Equipe em treinamento",
      created_at: "2026-11-01T10:00:00-03:00",
      updated_at: "2026-11-01T10:00:00-03:00",
    };
    const dedetizacao: BloqueioDeHorario = {
      ...treino,
      id: "b2",
      starts_at: "2026-11-21T00:00:00-03:00",
      ends_at: "2026-11-22T00:00:00-03:00",
      all_day: true,
      reason: "Dedetização",
    };
    expect(bloqueiosNoDia([treino, dedetizacao], "2026-11-20")).toMatchObject([{ inicio: 840, fim: 960, diaInteiro: false }]);
    expect(bloqueiosNoDia([treino, dedetizacao], "2026-11-21")).toMatchObject([{ inicio: 0, fim: 1440, diaInteiro: true }]);
    expect(bloqueiosNoDia([treino, dedetizacao], "2026-11-22")).toEqual([]);
  });

  it("a faixa de horas cobre o funcionamento e estica para o evento fora dele", () => {
    expect(faixaDeHoras(["2026-11-20"], horarios, [])).toEqual({ inicio: 480, fim: 1080 });
    // A visita das 20:30 não some da tela por estar fora do expediente.
    expect(faixaDeHoras(["2026-11-20"], horarios, [{ inicio: 1230, fim: 1260 }])).toEqual({ inicio: 480, fim: 1260 });
    expect(faixaDeHoras(["2026-11-22"], horarios, [])).toEqual({ inicio: 480, fim: 1200 });
  });
});
