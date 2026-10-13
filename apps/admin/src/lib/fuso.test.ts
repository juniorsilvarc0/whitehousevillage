import { describe, expect, it } from "vitest";

import { deslocamentoDaCasa, instanteDaCasa, meiaNoiteDaCasa, paredeDaCasa } from "@/lib/fuso";

/**
 * O único lugar do painel que converte entre hora de parede da casa e
 * instante. Os casos usam instantes escritos em UTC de propósito: se a
 * conversão usasse o fuso do processo (ou do navegador), o mesmo instante
 * cairia em outro dia ou outra hora conforme a máquina que roda o teste.
 */

describe("instanteDaCasa — a hora digitada é a da casa", () => {
  it("monta o instante com o offset da casa, sem converter para o fuso de quem digitou", () => {
    expect(instanteDaCasa("2026-11-20", "14:00")).toBe("2026-11-20T14:00:00-03:00");
    expect(deslocamentoDaCasa("2026-11-20", "14:00")).toBe("-03:00");
  });

  it("a meia-noite da casa é 00:00 no offset da casa — o que o dia inteiro exige", () => {
    expect(meiaNoiteDaCasa("2026-11-20")).toBe("2026-11-20T00:00:00-03:00");
    // A mesma meia-noite, vista de UTC, já é 03:00 — e não 00:00Z.
    expect(new Date(meiaNoiteDaCasa("2026-11-20")).toISOString()).toBe("2026-11-20T03:00:00.000Z");
  });

  it("recusa entrada fora do formato em vez de montar um instante errado", () => {
    expect(() => instanteDaCasa("20/11/2026", "14:00")).toThrow();
    expect(() => instanteDaCasa("2026-11-20", "14h")).toThrow();
  });
});

describe("paredeDaCasa — o relógio da casa num instante", () => {
  it("lê o instante no offset da casa como ele veio", () => {
    expect(paredeDaCasa("2026-11-20T14:00:00-03:00")).toEqual({ data: "2026-11-20", hora: "14:00", minutos: 840 });
  });

  it("o mesmo instante escrito em UTC dá o mesmo dia e a mesma hora da casa", () => {
    // 01:30 de 21/11 em UTC são 22:30 de 20/11 em Fortaleza.
    expect(paredeDaCasa("2026-11-21T01:30:00Z")).toEqual({ data: "2026-11-20", hora: "22:30", minutos: 1350 });
  });

  it("a meia-noite da casa é 00:00, nunca 24:00", () => {
    expect(paredeDaCasa("2026-11-21T03:00:00Z")).toEqual({ data: "2026-11-21", hora: "00:00", minutos: 0 });
  });

  it("instante ilegível é null, sem exceção", () => {
    expect(paredeDaCasa("ontem")).toBeNull();
  });

  it("ida e volta: o instante montado é lido de volta na mesma hora de parede", () => {
    const i = instanteDaCasa("2027-02-14", "09:45");
    expect(paredeDaCasa(i)).toEqual({ data: "2027-02-14", hora: "09:45", minutos: 585 });
  });
});
