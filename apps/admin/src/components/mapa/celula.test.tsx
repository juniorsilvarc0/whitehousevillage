import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import type { Faixa } from "@/lib/mapa/faixas";
import { forcarTique } from "@/lib/tempo-real/relogio";

import { ColunaDeFundo, FaixaDaGrade, resumoDaLinha } from "./celula";

const AGORA = Date.parse("2026-01-10T12:00:00-03:00");

function faixa(parcial: Partial<Faixa> = {}): Faixa {
  return {
    chave: "f1",
    inicio: 2,
    fim: 7,
    status: "confirmed",
    stayBlockId: "b1",
    reservationId: "r1",
    reservationCode: "WH-2026-0001",
    guestName: "Maria de Souza",
    tocaInicio: false,
    tocaFim: false,
    ...parcial,
  };
}

function classes(): string {
  // A faixa é `aria-hidden` de propósito — a leitura acessível é o resumo da
  // linha —, então o teste a alcança pelo `data-status`, que é o mesmo gancho
  // que o CSS usa.
  return document.querySelector("[data-status]")?.className ?? "";
}

beforeEach(() => {
  forcarTique(AGORA);
});

describe("FaixaDaGrade — um estado por vez", () => {
  it("confirmada: sólida, com código e hóspede escritos UMA vez", () => {
    render(<FaixaDaGrade faixa={faixa()} />);
    expect(screen.getByText("WH-2026-0001 · Maria de Souza")).toBeDefined();
    expect(classes()).toContain("mapa-faixa--confirmed");
  });

  it("pré-reserva: tracejada, com o contador de expiração dentro da barra", () => {
    // `hold` é data segurada sem dinheiro. Uma barra tracejada sem número diz
    // "alguém está pensando"; com número, diz "esta data volta ao estoque hoje".
    render(
      <FaixaDaGrade
        faixa={faixa({ status: "hold" })}
        expiraEm={new Date(AGORA + 3 * 3_600_000 + 12 * 60_000).toISOString()}
      />,
    );

    expect(classes()).toContain("mapa-faixa--hold");
    expect(screen.getByText("3h12")).toBeDefined();
  });

  it("pré-reserva a menos de 2 h é crítica — a barra muda de tom, não só o texto", () => {
    render(
      <FaixaDaGrade
        faixa={faixa({ status: "hold" })}
        expiraEm={new Date(AGORA + 40 * 60_000).toISOString()}
      />,
    );

    expect(document.querySelector("[data-urgencia='critica']")).not.toBeNull();
    expect(screen.getByText("40min")).toBeDefined();
  });

  it("pré-reserva vencida continua desenhada, marcada como expirada", () => {
    // Entre o vencimento e a passagem do job a data ainda está ocupada.
    render(
      <FaixaDaGrade faixa={faixa({ status: "hold" })} expiraEm={new Date(AGORA - 60_000).toISOString()} />,
    );
    expect(screen.getByText("expirada")).toBeDefined();
  });

  it("estadia cumprida fica no mapa, em tom de histórico", () => {
    // Apagá-la levaria a receita realizada — e com ela ADR e RevPAR — do mapa
    // junto. `completed` no banco, `checked_out` na tela.
    render(<FaixaDaGrade faixa={faixa({ status: "checked_out" })} />);
    expect(classes()).toContain("mapa-faixa--checked_out");
  });

  it("manutenção é hachurada e não nomeia hóspede nenhum", () => {
    render(
      <FaixaDaGrade
        faixa={faixa({ status: "maintenance", reservationId: null, reservationCode: null, guestName: null })}
      />,
    );
    expect(classes()).toContain("mapa-faixa--maintenance");
    expect(screen.getByText("Manutenção")).toBeDefined();
  });

  it("uso do proprietário leva selo — a ação da gestão é outra", () => {
    render(
      <FaixaDaGrade
        faixa={faixa({ status: "owner_hold", reservationId: null, reservationCode: null, guestName: null })}
      />,
    );
    expect(screen.getByText("PROP")).toBeDefined();
  });

  it("reserva de canal leva o selo OTA: ela não se cancela por aqui", () => {
    render(<FaixaDaGrade faixa={faixa({ status: "ota" })} />);
    expect(screen.getByText("OTA")).toBeDefined();
    expect(classes()).toContain("mapa-faixa--ota");
  });

  it("`parcial` é da linha sintética: a casa não é vendável, e não há reserva a mostrar", () => {
    render(
      <FaixaDaGrade
        faixa={faixa({
          status: "parcial",
          stayBlockId: null,
          reservationId: null,
          reservationCode: null,
          guestName: null,
        })}
      />,
    );
    expect(screen.getByText("Casa parcialmente ocupada")).toBeDefined();
  });

  it("faixa curta esconde o texto e mantém o selo — três letras e reticências não informam", () => {
    render(<FaixaDaGrade faixa={faixa({ status: "ota", inicio: 0, fim: 1 })} />);
    expect(screen.getByText("OTA")).toBeDefined();
    expect(screen.queryByText("WH-2026-0001 · Maria de Souza")).toBeNull();
  });

  it("ponta que encosta na borda da janela fica reta — 'corta aqui', não 'termina aqui'", () => {
    render(<FaixaDaGrade faixa={faixa({ tocaInicio: true })} />);
    expect(document.querySelector("[data-toca-inicio='1']")).not.toBeNull();
    expect(document.querySelector("[data-toca-fim='1']")).toBeNull();
  });

  it("ocupa exatamente as colunas da estadia", () => {
    render(<FaixaDaGrade faixa={faixa({ inicio: 2, fim: 7 })} />);
    const estilo = document.querySelector<HTMLElement>("[data-status]")?.style;
    expect(estilo?.left).toBe("calc(var(--mapa-dia) * 2 + 2px)");
    expect(estilo?.width).toBe("calc(var(--mapa-dia) * 5 - 4px)");
  });
});

describe("ColunaDeFundo", () => {
  it("carrega o tipo de data, que é a precedência do tarifário", () => {
    render(<ColunaDeFundo indice={3} tipo="reveillon" hoje={false} apontada={false} />);
    const coluna = document.querySelector<HTMLElement>(".mapa-coluna");
    expect(coluna?.dataset.tipo).toBe("reveillon");
    expect(coluna?.style.left).toBe("calc(var(--mapa-dia) * 3)");
  });

  it("marca hoje e a coluna sob o cursor por atributo, não por classe nova", () => {
    render(<ColunaDeFundo indice={0} tipo="fds" hoje apontada />);
    const coluna = document.querySelector<HTMLElement>(".mapa-coluna");
    expect(coluna?.dataset.hoje).toBe("1");
    expect(coluna?.dataset.apontada).toBe("1");
  });
});

describe("resumoDaLinha — o que o leitor de tela recebe no lugar do desenho", () => {
  const dias = ["2026-01-01", "2026-01-02", "2026-01-03", "2026-01-04"];

  it("linha vazia diz que está vazia, em vez de calar", () => {
    expect(resumoDaLinha("COB-01", [], dias)).toBe("COB-01: livre em toda a faixa mostrada.");
  });

  it("descreve cada ocupação com período, estado e quem é", () => {
    const texto = resumoDaLinha("COB-01", [faixa({ inicio: 1, fim: 3 })], dias);
    expect(texto).toBe("COB-01: 2026-01-02 a 2026-01-03: Confirmada, WH-2026-0001 · Maria de Souza.");
  });

  it("bloqueio operacional não inventa hóspede", () => {
    const texto = resumoDaLinha(
      "AP-01",
      [faixa({ inicio: 0, fim: 1, status: "maintenance", reservationCode: null, guestName: null })],
      dias,
    );
    expect(texto).toBe("AP-01: 2026-01-01 a 2026-01-01: Manutenção.");
  });
});
