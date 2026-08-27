import { describe, expect, it } from "vitest";

import { aplicarMovimento, localizarCard, planejarMovimento, somaVisivel } from "@/lib/crm/kanban";
import type { CardDaOportunidade, ColunaDoKanban, EtapaDoFunil } from "@/lib/crm/tipos";

/**
 * A transformação pura por trás do arrasto.
 *
 * O componente prova o comportamento visível; aqui se prova a **invariante**: o
 * dinheiro e a contagem não nascem nem somem no caminho. Um bug de sinal em
 * `amount_cents` daria uma coluna com total negativo, e ninguém revisando um
 * kanban nota isso até a reunião de fechamento do mês.
 */

function etapa(id: string, position: number, tipo: EtapaDoFunil["type"] = "aberto"): EtapaDoFunil {
  return {
    id, pipeline_id: "f", name: id, position, probability: 40, color: "#8FA36B", type: tipo,
    sla_days: null, auto_task_subject: null, auto_task_type: null, auto_task_due_days: null, auto_notify: false,
  };
}

function card(id: string, centavos: number): CardDaOportunidade {
  return {
    id, contact_id: "c", contact_name: id, unit_type_name: null, check_in: null, check_out: null,
    amount_cents: centavos, probability: 10, expected_close: null, owner_id: null, owner_name: null,
    status: "aberta", entered_stage_at: "2026-08-20T12:00:00Z", sla_due_at: null, sla_breached: false,
    pending_task_count: 0, next_due_at: null, reservation_code: null,
  };
}

function coluna(stage: EtapaDoFunil, cards: CardDaOportunidade[]): ColunaDoKanban {
  return {
    stage,
    count: cards.length,
    amount_cents: cards.reduce((t, c) => t + c.amount_cents, 0),
    has_more: false,
    cards,
  };
}

const A = etapa("a", 0);
const B = etapa("b", 1);
const GANHO = etapa("ganho", 2, "ganho");

const QUADRO: ColunaDoKanban[] = [
  coluna(A, [card("op-1", 1_000_00), card("op-2", 500_00)]),
  coluna(B, [card("op-3", 250_00)]),
  coluna(GANHO, []),
];

const total = (colunas: ColunaDoKanban[]) => colunas.reduce((t, c) => t + c.amount_cents, 0);
const contagem = (colunas: ColunaDoKanban[]) => colunas.reduce((t, c) => t + c.count, 0);

describe("aplicarMovimento", () => {
  it("não cria nem destrói dinheiro nem cards", () => {
    const depois = aplicarMovimento(QUADRO, "op-1", "b");
    expect(total(depois)).toBe(total(QUADRO));
    expect(contagem(depois)).toBe(contagem(QUADRO));
  });

  it("mantém a soma da coluna igual à soma dos cards que ela mostra", () => {
    // Só vale porque o quadro do teste cabe numa página; no produto a coluna é
    // paginada e `amount_cents` é maior que a soma visível. O que se checa aqui
    // é o ajuste, não a igualdade em si.
    for (const c of aplicarMovimento(QUADRO, "op-2", "b")) {
      expect(c.amount_cents).toBe(somaVisivel(c));
      expect(c.count).toBe(c.cards.length);
    }
  });

  it("devolve o mesmo array quando não há o que mover", () => {
    // Identidade referencial: é o que evita um render a cada arrasto que
    // termina na coluna de origem — o desfecho mais comum de um arrasto.
    expect(aplicarMovimento(QUADRO, "op-1", "a")).toBe(QUADRO);
    expect(aplicarMovimento(QUADRO, "inexistente", "b")).toBe(QUADRO);
    expect(aplicarMovimento(QUADRO, "op-1", "coluna-que-nao-existe")).toBe(QUADRO);
  });

  it("não muda o array de origem — o rollback depende disso", () => {
    aplicarMovimento(QUADRO, "op-1", "b");
    expect(localizarCard(QUADRO, "op-1")?.origem).toBe("a");
    expect(QUADRO[0]!.cards).toHaveLength(2);
  });

  it("herda a probabilidade da etapa de destino", () => {
    const depois = aplicarMovimento(QUADRO, "op-1", "b");
    expect(localizarCard(depois, "op-1")?.card.probability).toBe(B.probability);
  });
});

describe("planejarMovimento", () => {
  it("etapa terminal vira diálogo, não movimento", () => {
    const plano = planejarMovimento(QUADRO, "op-1", "ganho");
    expect(plano.tipo).toBe("ganhar");
    // Nenhuma coluna nova é calculada: o card não sai do lugar até o `/win`
    // responder, porque ganhar cria reserva e pode falhar.
    expect(plano).not.toHaveProperty("colunas");
  });

  it("mesma coluna é 'nada' — arrasto que volta ao ponto de partida não é evento", () => {
    expect(planejarMovimento(QUADRO, "op-1", "a").tipo).toBe("nada");
  });

  it("carrega a origem, que é a guarda contra dois corretores no mesmo card", () => {
    const plano = planejarMovimento(QUADRO, "op-3", "a");
    expect(plano.tipo === "mover" && plano.origem).toBe("b");
  });
});
