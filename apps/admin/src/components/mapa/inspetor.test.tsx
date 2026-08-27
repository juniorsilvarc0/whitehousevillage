import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { janelaDe } from "@/lib/mapa/janela";
import { chaveDePreco, type CelulaSintetica, type IndiceDePrecos, type LinhaDaGrade } from "@/lib/mapa/tipos";
import { forcarTique } from "@/lib/tempo-real/relogio";

import { InspetorDoDia } from "./inspetor";

const JANELA = janelaDe("2026-12-20", 3);
const AGORA = Date.parse("2026-12-01T12:00:00-03:00");

const PRECOS: IndiceDePrecos = new Map([
  [
    chaveDePreco("p-cob", "2026-12-21"),
    { price_cents: 240_000, date_type: "fds" as const, min_nights: 2, available: 1 },
  ],
]);

function linha(celulas: Partial<CelulaSintetica>[]): LinhaDaGrade {
  return {
    chave: "u-cob",
    tipo: "unidade",
    codigo: "COB-01",
    nome: "Cobertura",
    unitId: "u-cob",
    unitTypeId: "p-cob",
    celulas: JANELA.dias.map((date, i) => ({
      date,
      status: "livre" as const,
      date_type: "normal" as const,
      stay_block_id: null,
      reservation_id: null,
      reservation_code: null,
      guest_name: null,
      bloqueadaPor: [],
      ...celulas[i],
    })),
  };
}

describe("InspetorDoDia", () => {
  it("sem cursor, ensina o que a faixa faz em vez de ficar em branco", () => {
    render(<InspetorDoDia alvo={null} janela={JANELA} precos={PRECOS} expiracoes={new Map()} />);
    expect(screen.getByText(/Passe o cursor por uma noite/)).toBeDefined();
  });

  it("mostra a TARIFA da noite, que vem do produto da linha — nunca da unidade", () => {
    // `GET /availability/units` não traz preço, e o contrato diz por quê: a
    // mesma AP-01 custa uma coisa vendida como Apartamento 2 Suítes e outra
    // dentro da White House Completa.
    render(
      <InspetorDoDia
        alvo={{ linha: linha([{}, { date_type: "fds" }]), indice: 1 }}
        janela={JANELA}
        precos={PRECOS}
        expiracoes={new Map()}
      />,
    );

    expect(screen.getByText("R$ 2.400,00")).toBeDefined();
    expect(screen.getByText("Fim de semana")).toBeDefined();
    expect(screen.getByText("mín. 2 noites")).toBeDefined();
    expect(screen.getByText("Livre")).toBeDefined();
  });

  it("noite sem tarifa cadastrada é dita em voz alta — é configuração pela metade", () => {
    // A venda recusaria com `RATE_NOT_FOUND`. Mostrar um preço em branco faria
    // a tela oferecer o que o orçamento nega.
    render(
      <InspetorDoDia
        alvo={{ linha: linha([{}]), indice: 0 }}
        janela={JANELA}
        precos={PRECOS}
        expiracoes={new Map()}
      />,
    );
    expect(screen.getByText("sem tarifa cadastrada")).toBeDefined();
  });

  it("na pré-reserva mostra o vencimento por extenso, não só a barra tracejada", () => {
    forcarTique(AGORA);
    render(
      <InspetorDoDia
        alvo={{
          linha: linha([{ status: "hold", reservation_id: "r1", reservation_code: "WH-2026-0004" }]),
          indice: 0,
        }}
        janela={JANELA}
        precos={PRECOS}
        expiracoes={new Map([["r1", new Date(AGORA + 5 * 3_600_000).toISOString()]])}
      />,
    );

    expect(screen.getByText("WH-2026-0004")).toBeDefined();
    expect(screen.getByText("expira em 5 h 0 min")).toBeDefined();
  });

  it("na linha da casa, o dia `parcial` NOMEIA a unidade que impede a venda", () => {
    // "Indisponível" manda a gestão procurar; "a SP-02 está vendida" resolve a
    // ligação em que o hóspede está esperando na linha.
    render(
      <InspetorDoDia
        alvo={{
          linha: linha([{ status: "parcial", bloqueadaPor: ["SP-02", "COB-01"] }]),
          indice: 0,
        }}
        janela={JANELA}
        precos={PRECOS}
        expiracoes={new Map()}
      />,
    );

    expect(screen.getByText("Casa parcialmente ocupada")).toBeDefined();
    expect(screen.getByText("ocupada em SP-02, COB-01")).toBeDefined();
  });
});
