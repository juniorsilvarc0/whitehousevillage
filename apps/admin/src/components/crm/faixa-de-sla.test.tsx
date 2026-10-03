import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FaixaDeSla, FaixaDeSlaDoCard } from "@/components/crm/faixa-de-sla";
import type { FaixaDeSLA } from "@/lib/crm/tipos";

/**
 * A faixa de SLA.
 *
 * O que se protege aqui é uma coisa só, e é a mais importante de toda a tela do
 * CRM: **a faixa lê o que o servidor calculou e não recalcula nada**. `days_left`
 * e `breached` vêm com o relógio de `America/Fortaleza`; se o componente
 * contasse os dias por conta própria, dois usuários em fusos diferentes veriam
 * estados diferentes do mesmo card — e o filtro `sla=estourado` da listagem, que
 * roda no servidor, discordaria da cor que a tela está pintando.
 *
 * Por isso os testes usam `due_at` no **passado** com `breached: false` e
 * vice-versa: um componente que recalculasse contradiria o payload, e é
 * exatamente isso que estas asserções pegam.
 */

function faixa(parcial: Partial<FaixaDeSLA> = {}): FaixaDeSLA {
  return {
    stage_id: "e-orcamento",
    stage_name: "Orçamento enviado",
    sla_days: 2,
    entered_stage_at: "2026-08-20T12:00:00Z",
    due_at: "2026-08-22T12:00:00Z",
    days_left: 1,
    breached: false,
    ...parcial,
  };
}

describe("FaixaDeSla", () => {
  it("diz há quantos dias estourou, no tom de recusa", () => {
    render(<FaixaDeSla faixa={faixa({ breached: true, days_left: -2 })} />);

    expect(screen.getByRole("status")).toHaveProperty("dataset.tom", "estourado");
    expect(screen.getByText("Estourou há 2 dias")).toBeTruthy();
  });

  it("no singular escreve 'há 1 dia', não 'há 1 dias'", () => {
    render(<FaixaDeSla faixa={faixa({ breached: true, days_left: -1 })} />);
    expect(screen.getByText("Estourou há 1 dia")).toBeTruthy();
  });

  it("estouro no mesmo dia não vira 'há 0 dias'", () => {
    render(<FaixaDeSla faixa={faixa({ breached: true, days_left: 0 })} />);
    expect(screen.getByText("Prazo venceu hoje")).toBeTruthy();
  });

  it("separa 'vence hoje' de 'no prazo' — são decisões diferentes para quem vende", () => {
    const { unmount } = render(<FaixaDeSla faixa={faixa({ days_left: 0 })} />);
    expect(screen.getByRole("status")).toHaveProperty("dataset.tom", "vence_hoje");
    expect(screen.getByText("Vence hoje")).toBeTruthy();
    unmount();

    render(<FaixaDeSla faixa={faixa({ days_left: 3 })} />);
    expect(screen.getByRole("status")).toHaveProperty("dataset.tom", "no_prazo");
    expect(screen.getByText("Faltam 3 dias")).toBeTruthy();
  });

  it("etapa sem SLA diz que não cobra prazo, em vez de fingir um", () => {
    render(<FaixaDeSla faixa={faixa({ sla_days: null, due_at: null, days_left: null })} />);
    expect(screen.getByRole("status")).toHaveProperty("dataset.tom", "sem_sla");
    expect(screen.getByText("Etapa sem prazo")).toBeTruthy();
  });

  it("obedece ao `breached` do servidor mesmo quando o prazo já passou no relógio local", () => {
    // Prazo de 2020 e `breached: false`: um componente que contasse os dias
    // sozinho pintaria vermelho aqui. O servidor é a única fonte do estado.
    render(
      <FaixaDeSla faixa={faixa({ due_at: "2020-01-01T12:00:00Z", days_left: 4, breached: false })} />,
    );
    expect(screen.getByRole("status")).toHaveProperty("dataset.tom", "no_prazo");
    expect(screen.getByText("Faltam 4 dias")).toBeTruthy();
  });

  it("obedece ao `breached` do servidor mesmo com prazo futuro", () => {
    render(
      <FaixaDeSla faixa={faixa({ due_at: "2099-01-01T12:00:00Z", days_left: -3, breached: true })} />,
    );
    expect(screen.getByRole("status")).toHaveProperty("dataset.tom", "estourado");
    expect(screen.getByText("Estourou há 3 dias")).toBeTruthy();
  });
});

describe("FaixaDeSlaDoCard", () => {
  it("não desenha nada quando o card está no prazo", () => {
    // Marca em todo card não distingue nada: o que precisa saltar num quadro de
    // oitenta cards é o punhado que estourou.
    const { container } = render(
      <FaixaDeSlaDoCard card={{ sla_breached: false, sla_due_at: "2026-08-22T12:00:00Z" }} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("desenha a faixa quando o servidor marcou o card como estourado", () => {
    render(<FaixaDeSlaDoCard card={{ sla_breached: true, sla_due_at: "2026-08-22T12:00:00Z" }} />);
    expect(screen.getByText("Prazo vencido")).toBeTruthy();
  });
});
