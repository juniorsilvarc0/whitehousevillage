import { describe, expect, it } from "vitest";

import { Deduplicador, lerEvento } from "./eventos";

describe("lerEvento", () => {
  it("lê o envelope magro do contrato", () => {
    expect(lerEvento('{"entity":"stay_block","id":"3f1c","unit_id":"a07b","v":7}')).toEqual({
      entity: "stay_block",
      id: "3f1c",
      unit_id: "a07b",
      v: 7,
    });
  });

  it("`unit_id` ausente vira null — só `stay_block` o traz", () => {
    expect(lerEvento('{"entity":"reservation","id":"r1","v":3}')?.unit_id).toBeNull();
  });

  it("envelope irreconhecível é descartado, nunca lançado", () => {
    // O barramento é uma segunda porta de entrada de dados. Uma exceção dentro
    // de um ouvinte de evento não é alcançada por error boundary nenhum: o mapa
    // inteiro cairia por um JSON torto.
    expect(lerEvento("não é json")).toBeNull();
    expect(lerEvento("null")).toBeNull();
    expect(lerEvento('{"entity":"planeta","id":"x","v":1}')).toBeNull();
    expect(lerEvento('{"entity":"lead","id":"x"}')).toBeNull();
    expect(lerEvento('{"entity":"lead","id":"","v":1}')).toBeNull();
  });
});

describe("Deduplicador", () => {
  const evento = (id: string, v: number) => ({ entity: "opportunity" as const, id, v });

  it("passa a primeira vez e barra a reentrega do mesmo `v`", () => {
    const dedup = new Deduplicador();
    expect(dedup.novidade(evento("a", 5))).toBe(true);
    expect(dedup.novidade(evento("a", 5))).toBe(false);
  });

  it("barra o `v` MENOR — replay fora de ordem não faz a tela voltar no tempo", () => {
    const dedup = new Deduplicador();
    dedup.novidade(evento("a", 9));
    expect(dedup.novidade(evento("a", 4))).toBe(false);
    expect(dedup.novidade(evento("a", 10))).toBe(true);
  });

  it("os nove eventos de uma venda da Completa passam todos na primeira entrega", () => {
    // Eles compartilham o `v` (é o id da transação) mas têm ids diferentes.
    // Quem os junta num refetch só é o agrupamento do hook, não o dedup.
    const dedup = new Deduplicador();
    const venda = Array.from({ length: 9 }, (_, i) => evento(`bloco-${i}`, 853));
    expect(venda.every((e) => dedup.novidade(e))).toBe(true);
    // Repostos numa reconexão, nenhum deles passa de novo.
    expect(venda.some((e) => dedup.novidade(e))).toBe(false);
  });

  it("`esquecer` devolve tudo à condição de novidade — é o que o resync significa", () => {
    const dedup = new Deduplicador();
    dedup.novidade(evento("a", 5));
    dedup.esquecer();
    expect(dedup.novidade(evento("a", 5))).toBe(true);
  });
});
