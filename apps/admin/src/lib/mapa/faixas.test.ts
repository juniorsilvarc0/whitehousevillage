import { describe, expect, it } from "vitest";

import { faixaNoDia, faixasVisiveis, montarFaixas } from "./faixas";
import type { CelulaDoMapa, StatusDaCelula } from "./tipos";

function celula(
  date: string,
  status: StatusDaCelula,
  extra: Partial<CelulaDoMapa> = {},
): CelulaDoMapa {
  return {
    date,
    status,
    date_type: "normal",
    stay_block_id: null,
    reservation_id: null,
    reservation_code: null,
    guest_name: null,
    ...extra,
  };
}

const bloco = (id: string, reserva?: string, codigo?: string) => ({
  stay_block_id: id,
  reservation_id: reserva ?? null,
  reservation_code: codigo ?? null,
});

describe("montarFaixas", () => {
  it("junta dias do mesmo bloco numa faixa só", () => {
    const faixas = montarFaixas([
      celula("2026-01-01", "livre"),
      celula("2026-01-02", "confirmed", bloco("b1", "r1", "WH-2026-0001")),
      celula("2026-01-03", "confirmed", bloco("b1", "r1", "WH-2026-0001")),
      celula("2026-01-04", "confirmed", bloco("b1", "r1", "WH-2026-0001")),
      celula("2026-01-05", "livre"),
    ]);

    expect(faixas).toHaveLength(1);
    expect(faixas[0]).toMatchObject({ inicio: 1, fim: 4, status: "confirmed", reservationCode: "WH-2026-0001" });
  });

  it("NÃO junta duas reservas encostadas (back-to-back)", () => {
    // A regra que este teste protege: continuidade é identidade de BLOCO, não
    // igualdade de status. Fundir as duas apagaria a troca de hóspede
    // exatamente no dia em que a operação precisa enxergá-la.
    const faixas = montarFaixas([
      celula("2026-01-01", "confirmed", bloco("b1", "r1", "WH-2026-0001")),
      celula("2026-01-02", "confirmed", bloco("b1", "r1", "WH-2026-0001")),
      celula("2026-01-03", "confirmed", bloco("b2", "r2", "WH-2026-0002")),
      celula("2026-01-04", "confirmed", bloco("b2", "r2", "WH-2026-0002")),
    ]);

    expect(faixas).toHaveLength(2);
    expect(faixas[0]).toMatchObject({ inicio: 0, fim: 2, reservationCode: "WH-2026-0001" });
    expect(faixas[1]).toMatchObject({ inicio: 2, fim: 4, reservationCode: "WH-2026-0002" });
  });

  it("marca as pontas que encostam na borda da janela", () => {
    const faixas = montarFaixas([
      celula("2026-01-01", "confirmed", bloco("b1")),
      celula("2026-01-02", "confirmed", bloco("b1")),
      celula("2026-01-03", "livre"),
      celula("2026-01-04", "maintenance", bloco("b2")),
    ]);

    expect(faixas[0]).toMatchObject({ tocaInicio: true, tocaFim: false });
    expect(faixas[1]).toMatchObject({ tocaInicio: false, tocaFim: true });
  });

  it("mantém contínua a linha sintética, que não tem bloco e só tem reserva", () => {
    // A White House Completa ocupa oito unidades: são oito blocos, e a linha
    // derivada não representa nenhum deles. Quem a mantém inteira é a reserva.
    const faixas = montarFaixas([
      celula("2026-01-01", "confirmed", { reservation_id: "r9", reservation_code: "WH-2026-0009" }),
      celula("2026-01-02", "confirmed", { reservation_id: "r9", reservation_code: "WH-2026-0009" }),
    ]);

    expect(faixas).toHaveLength(1);
    expect(faixas[0]).toMatchObject({ inicio: 0, fim: 2, stayBlockId: null, reservationId: "r9" });
  });

  it("ignora dias livres e devolve nada quando a linha está vazia", () => {
    expect(montarFaixas([celula("2026-01-01", "livre"), celula("2026-01-02", "livre")])).toEqual([]);
  });
});

describe("faixasVisiveis", () => {
  const faixas = montarFaixas([
    celula("d0", "confirmed", bloco("b1")),
    celula("d1", "confirmed", bloco("b1")),
    celula("d2", "livre"),
    celula("d3", "hold", bloco("b2")),
    celula("d4", "livre"),
    celula("d5", "maintenance", bloco("b3")),
  ]);

  it("traz só o que intersecta a janela visível", () => {
    expect(faixasVisiveis(faixas, 3, 4).map((f) => f.stayBlockId)).toEqual(["b2"]);
  });

  it("traz a faixa que ATRAVESSA a janela, mesmo começando antes dela", () => {
    expect(faixasVisiveis(faixas, 1, 1).map((f) => f.stayBlockId)).toEqual(["b1"]);
  });
});

describe("faixaNoDia", () => {
  const faixas = montarFaixas([
    celula("d0", "confirmed", bloco("b1")),
    celula("d1", "confirmed", bloco("b1")),
    celula("d2", "livre"),
  ]);

  it("acha a faixa sob o dia clicado", () => {
    expect(faixaNoDia(faixas, 1)?.stayBlockId).toBe("b1");
  });

  it("devolve null no dia livre — é o que separa 'abrir reserva' de 'começar bloqueio'", () => {
    expect(faixaNoDia(faixas, 2)).toBeNull();
  });
});
