import { describe, expect, it } from "vitest";

import { diaDaSemanaISO, horarioSemODia, intervaloPorExtenso, janelaPorExtenso } from "@/lib/agenda/rotulos";

/**
 * As frases de horário — formatação, não regra. O que importa provar é que o
 * fim exclusivo é dito do jeito que se lê (o evento de dia inteiro "até 22/11",
 * e não até a meia-noite do dia 23) e que a hora é a da casa.
 */

describe("a janela do evento", () => {
  it("com hora, no mesmo dia", () => {
    expect(janelaPorExtenso({ all_day: false, date: "2026-11-20", starts_at: "2026-11-20T14:00:00-03:00", ends_at: "2026-11-20T14:30:00-03:00" })).toBe(
      "14:00–14:30",
    );
  });

  it("instante escrito em UTC sai na hora da casa", () => {
    expect(janelaPorExtenso({ all_day: false, date: "2026-11-20", starts_at: "2026-11-20T17:00:00Z", ends_at: "2026-11-20T17:30:00Z" })).toBe(
      "14:00–14:30",
    );
  });

  it("até a meia-noite é 24:00 do mesmo dia, não 00:00", () => {
    expect(janelaPorExtenso({ all_day: false, date: "2026-11-20", starts_at: "2026-11-20T22:00:00-03:00", ends_at: "2026-11-21T00:00:00-03:00" })).toBe(
      "22:00–24:00",
    );
  });

  it("atravessando dias, o dia do fim aparece", () => {
    expect(janelaPorExtenso({ all_day: false, date: "2026-11-20", starts_at: "2026-11-20T22:00:00-03:00", ends_at: "2026-11-21T02:00:00-03:00" })).toBe(
      "22:00 → 21/11 02:00",
    );
  });

  it("dia inteiro diz o último dia, e não a meia-noite do seguinte", () => {
    expect(janelaPorExtenso({ all_day: true, date: "2026-11-20", starts_at: "2026-11-20T00:00:00-03:00", ends_at: "2026-11-21T00:00:00-03:00" })).toBe(
      "Dia inteiro",
    );
    expect(janelaPorExtenso({ all_day: true, date: "2026-11-20", starts_at: "2026-11-20T00:00:00-03:00", ends_at: "2026-11-23T00:00:00-03:00" })).toBe(
      "Dia inteiro · até 22/11",
    );
  });
});

describe("intervalos de ocupação e de bloqueio", () => {
  it("com o dia, para o conflito e o aviso", () => {
    expect(intervaloPorExtenso("2026-11-20T11:00:00-03:00", "2026-11-20T14:00:00-03:00")).toBe("20/11, das 11:00 às 14:00");
    expect(intervaloPorExtenso("2026-11-20T00:00:00-03:00", "2026-11-21T00:00:00-03:00")).toBe("20/11, o dia inteiro");
    expect(intervaloPorExtenso("2026-11-18T14:00:00-03:00", "2026-11-23T11:00:00-03:00")).toBe("de 18/11 14:00 a 23/11 11:00");
  });

  it("sem o dia, quando ele já está escrito ao lado", () => {
    expect(horarioSemODia("2026-11-20T15:00:00-03:00", "2026-11-20T16:30:00-03:00")).toBe("das 15:00 às 16:30");
    expect(horarioSemODia("2026-11-24T00:00:00-03:00", "2026-11-25T00:00:00-03:00")).toBe("o dia inteiro");
    expect(horarioSemODia("2026-11-24T00:00:00-03:00", "2026-11-27T00:00:00-03:00")).toBe("o dia inteiro, até 26/11");
    expect(horarioSemODia("2026-11-20T22:00:00-03:00", "2026-11-21T00:00:00-03:00")).toBe("das 22:00 à meia-noite");
  });
});

describe("o dia da semana ISO de uma data civil", () => {
  it("1 = segunda … 7 = domingo, sem fuso nenhum", () => {
    expect(diaDaSemanaISO("2026-11-16")).toBe(1);
    expect(diaDaSemanaISO("2026-11-20")).toBe(5);
    expect(diaDaSemanaISO("2026-11-22")).toBe(7);
  });
});
