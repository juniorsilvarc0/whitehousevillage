import * as React from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useControleDeModal } from "@/components/layout/controle-de-modal";
import type { Resultado } from "@/lib/acoes/resultado";
import type { ResultadoDeCancelamento } from "@/lib/reservas/tipos";

import { DialogoDeCancelamento, type AlvoDeCancelamento } from "./dialogo-de-cancelamento";

/**
 * O que se protege aqui é **o número que a tela mostra antes de confirmar**.
 *
 * Cancelar é a única ação do painel que move o saldo do hóspede sem ninguém
 * digitar um valor: quem calcula é a política congelada na reserva, e quem
 * descobre o resultado depois de executar não descobriu — cancelou no escuro.
 *
 * Os três comportamentos medidos:
 *
 * 1. A simulação chega e a tela diz, em BRL, quanto volta e quanto fica.
 * 2. **Trocar o motivo re-simula.** `no_show` aplica a faixa de menor
 *    antecedência e termina a reserva em `no_show`, não em `cancelled` — medido
 *    na API em 27/08/2026: a mesma `WH-2026-0001`, a 85 dias do check-in,
 *    devolve `refund 352500` sem motivo e `refund 0 / retained 352500` com
 *    `no_show`. Uma tela que simulasse uma vez e executasse outra coisa
 *    mostraria R$ 3.525,00 e devolveria R$ 0,00.
 * 3. **Não se confirma sem número.** Enquanto a simulação não chega — ou se ela
 *    falha —, o botão de executar fica desligado.
 */

const PREVISAO_INTEGRAL: ResultadoDeCancelamento = {
  label: "Devolução integral do sinal",
  refund_cents: 352_500,
  retained_cents: 0,
  deposit_paid_cents: 352_500,
  days_before: 85,
  policy_version: 1,
  credit_cents: 0,
  dry_run: true,
  status: "cancelled",
};

const PREVISAO_NO_SHOW: ResultadoDeCancelamento = {
  ...PREVISAO_INTEGRAL,
  label: "Retenção integral do sinal",
  refund_cents: 0,
  retained_cents: 352_500,
  status: "no_show",
};

const ALVO: AlvoDeCancelamento = {
  id: "r-1",
  code: "WH-2026-0001",
  contato: "Hóspede de Demonstração",
  check_in: "2026-11-20",
  check_out: "2026-11-23",
  previa: null,
};

/**
 * O diálogo é aberto por `ControleDeModal` (repor no evento que abre, não num
 * efeito), então o teste precisa de um chamador de verdade — é o mesmo caminho
 * que a linha da lista e o detalhe usam.
 */
function Bancada({
  aoSimular,
  aoCancelar = vi.fn(async () => ({ ok: true as const, data: PREVISAO_INTEGRAL })),
}: {
  aoSimular: (id: string, motivo: string) => Promise<Resultado<ResultadoDeCancelamento>>;
  aoCancelar?: (id: string, motivo: string) => Promise<Resultado<ResultadoDeCancelamento>>;
}) {
  const controle = useControleDeModal<AlvoDeCancelamento>();
  return (
    <>
      <button type="button" onClick={() => controle.abrir(ALVO)}>
        abrir
      </button>
      <DialogoDeCancelamento controle={controle.ref} aoSimular={aoSimular} aoCancelar={aoCancelar} />
    </>
  );
}

function abrir() {
  fireEvent.click(screen.getByText("abrir"));
}

/**
 * `Intl.NumberFormat("pt-BR", { currency: "BRL" })` separa o símbolo do número
 * com **espaço inquebrável** (U+00A0), não com espaço comum. Comparar contra
 * `"R$ 3.525,00"` digitado no teste falha por um caractere invisível — e o
 * autor do teste passa meia hora olhando para duas strings idênticas. Normalizar
 * aqui mantém a expectativa legível e continua medindo o valor.
 */
function texto(elemento: Element | null): string {
  return (elemento?.textContent ?? "").replace(/\u00A0/g, " ");
}

function escolherMotivo(rotulo: string) {
  fireEvent.change(screen.getByLabelText(/Motivo/), {
    target: { value: rotulo },
  });
}

describe("DialogoDeCancelamento", () => {
  it("mostra em reais quanto volta para o hóspede e quanto fica com a casa", async () => {
    const simular = vi.fn(async () => ({ ok: true as const, data: PREVISAO_INTEGRAL }));
    render(<Bancada aoSimular={simular} />);
    abrir();

    expect(await screen.findByText("Devolução integral do sinal")).toBeDefined();

    expect(texto(screen.getByText("Volta para o hóspede").closest("div"))).toContain("R$ 3.525,00");
    expect(texto(screen.getByText("Fica com a casa").closest("div"))).toContain("R$ 0,00");

    // A base do cálculo aparece: sem ela, "volta R$ 3.525,00" é um número sem
    // origem, e é justamente a base que um sinal errado corrompe.
    expect(texto(screen.getByText(/Base do cálculo/))).toContain("R$ 3.525,00");
    expect(screen.getByText(/85 dias de antecedência/)).toBeDefined();
  });

  it("re-simula ao trocar o motivo: no-show retém tudo e muda o estado final", async () => {
    const simular = vi.fn(async (_id: string, motivo: string) => ({
      ok: true as const,
      data: motivo === "no_show" ? PREVISAO_NO_SHOW : PREVISAO_INTEGRAL,
    }));
    render(<Bancada aoSimular={simular} />);
    abrir();

    await screen.findByText("Devolução integral do sinal");

    escolherMotivo("no_show");

    expect(await screen.findByText("Retenção integral do sinal")).toBeDefined();
    expect(texto(screen.getByText("Volta para o hóspede").closest("div"))).toContain("R$ 0,00");
    expect(texto(screen.getByText("Fica com a casa").closest("div"))).toContain("R$ 3.525,00");

    // O estado final muda junto — e a diferença importa no relatório: hóspede
    // que avisou e hóspede que sumiu não são a mesma linha.
    expect(screen.getByText(/A reserva termina em/).textContent).toContain("Não compareceu");
    expect(screen.getByRole("button", { name: /Registrar não comparecimento/ })).toBeDefined();

    await waitFor(() => expect(simular).toHaveBeenCalledWith("r-1", "no_show"));
  });

  it("executa o cancelamento com o mesmo motivo que simulou", async () => {
    const simular = vi.fn(async (_id: string, motivo: string) => ({
      ok: true as const,
      data: motivo === "no_show" ? PREVISAO_NO_SHOW : PREVISAO_INTEGRAL,
    }));
    const cancelar = vi.fn(async () => ({ ok: true as const, data: PREVISAO_NO_SHOW }));
    render(<Bancada aoSimular={simular} aoCancelar={cancelar} />);
    abrir();

    escolherMotivo("no_show");
    await screen.findByText("Retenção integral do sinal");

    fireEvent.click(screen.getByRole("button", { name: /Registrar não comparecimento/ }));

    await waitFor(() => expect(cancelar).toHaveBeenCalledWith("r-1", "no_show"));
  });

  it("não deixa confirmar enquanto não há motivo escolhido", async () => {
    const simular = vi.fn(async () => ({ ok: true as const, data: PREVISAO_INTEGRAL }));
    render(<Bancada aoSimular={simular} />);
    abrir();

    await screen.findByText("Devolução integral do sinal");

    // O número já está na tela, mas o motivo ainda não foi dito — e é ele que
    // decide se a conta é esta ou a retenção integral.
    const botao = screen.getByRole("button", { name: /Cancelar a reserva/ }) as HTMLButtonElement;
    expect(botao.disabled).toBe(true);

    escolherMotivo("desistencia");
    await waitFor(() => expect(botao.disabled).toBe(false));
  });

  it("recusa da simulação vira aviso com o código, e o botão continua desligado", async () => {
    const simular = vi.fn(async () => ({
      ok: false as const,
      code: "RESERVATION_NOT_CANCELLABLE" as const,
      message: "não cancelável",
      details: { status: "checked_out" },
    }));
    render(<Bancada aoSimular={simular} />);
    abrir();
    escolherMotivo("desistencia");

    expect(await screen.findByText(/A reserva está como "Estadia cumprida"/)).toBeDefined();
    expect(screen.queryByText("RESERVATION_NOT_CANCELLABLE")).toBeNull();
    // A tela reage ao código, não ao texto da API: a frase exibida é a da casa.
    expect(screen.getByText(/já foi encerrada e não pode ser cancelada/)).toBeDefined();

    const botao = screen.getByRole("button", { name: /Cancelar a reserva/ }) as HTMLButtonElement;
    expect(botao.disabled).toBe(true);
  });

  it("mostra o crédito em aberto quando a devolução não fecha a conta", async () => {
    const simular = vi.fn(async () => ({
      ok: true as const,
      data: { ...PREVISAO_INTEGRAL, refund_cents: 103_000, deposit_paid_cents: 103_000, credit_cents: 255_000 },
    }));
    render(<Bancada aoSimular={simular} />);
    abrir();

    const aviso = await screen.findByText(/crédito em aberto/i);
    // Sem esta linha, `refund + retained` se lê como "conta encerrada" e
    // R$ 2.550,00 do hóspede somem sem nenhum registro os cobrando.
    expect(texto(aviso.closest("p"))).toContain("R$ 2.550,00");
  });
});
