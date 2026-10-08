import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";

import { TelaDeContagem } from "@/components/bens/contagem";
import { contarLinha } from "@/lib/bens/acoes";
import type { ConferenciaCompleta } from "@/lib/bens/tipos";

/**
 * A contagem otimista no celular.
 *
 * 1. O número muda no toque, antes da resposta — senão quem conta toca de novo.
 * 2. Toques seguidos viram **uma** gravação, com o número final: três `PATCH`
 *    em fila podem chegar fora de ordem e gravar o do meio.
 * 3. Recusa devolve a linha ao último valor gravado **e diz por quê**.
 */

const { refresh } = vi.hoisted(() => ({ refresh: vi.fn() }));

vi.mock("sonner", () => ({ Toaster: () => null, toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() } }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn(), refresh, replace: vi.fn() }) }));
vi.mock("@/lib/bens/acoes", () => ({
  contarLinha: vi.fn(),
  fecharConferencia: vi.fn(),
  cancelarConferencia: vi.fn(),
}));

const contar = vi.mocked(contarLinha);

const CONFERENCIA: ConferenciaCompleta = {
  id: "c1",
  unit_id: "u1",
  status: "aberta",
  opened_at: "2026-10-07T12:00:00Z",
  progress: { lines: 1, counted: 0, pending: 1, diverging: 0 },
  rooms: [
    {
      room_id: "r1",
      room_name: "Cozinha",
      lines: [{ id: "l1", count_id: "c1", room_id: "r1", item_id: "i1", item_name: "Taça", expected_qty: 12, counted_qty: null }],
    },
  ],
};

const campo = () => screen.getByLabelText("Quantidade contada de Taça") as HTMLInputElement;
const mais = () => screen.getByRole("button", { name: "Um a mais de Taça" });

describe("TelaDeContagem — otimista, com volta", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    contar.mockReset();
    refresh.mockReset();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("três toques seguidos: a tela muda na hora e grava uma vez, com o número final", async () => {
    contar.mockResolvedValue({
      ok: true,
      data: {
        line: { ...CONFERENCIA.rooms[0].lines[0], counted_qty: 15 },
        progress: { lines: 1, counted: 1, pending: 0, diverging: 1 },
      },
    });
    render(<TelaDeContagem conferencia={CONFERENCIA} permissoes={{ editar: true, excluir: true }} />);

    fireEvent.click(mais());
    fireEvent.click(mais());
    fireEvent.click(mais());
    expect(campo().value, "o + na pendente parte do esperado: 12 → 13 → 14 → 15").toBe("15");
    expect(contar).not.toHaveBeenCalled();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(400);
    });

    expect(contar).toHaveBeenCalledTimes(1);
    expect(contar).toHaveBeenCalledWith("c1", "l1", 15);
    // O rodapé passa a falar o que o servidor devolveu.
    expect(screen.getByRole("progressbar", { name: "Itens contados" }).getAttribute("aria-valuenow")).toBe("1");
  });

  it("recusa devolve a linha ao que estava gravado — pendente continua pendente — e avisa", async () => {
    contar.mockResolvedValue({ ok: false, code: "COUNT_CLOSED", message: "fechada", details: {} });
    render(<TelaDeContagem conferencia={CONFERENCIA} permissoes={{ editar: true, excluir: true }} />);

    fireEvent.click(screen.getByRole("button", { name: /conferido igual ao esperado/i }));
    expect(campo().value).toBe("12");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(400);
    });

    expect(campo().value, "voltou a pendente: traço, não zero").toBe("");
    expect(vi.mocked(toast.error)).toHaveBeenCalledWith(
      "A contagem não foi salva",
      expect.objectContaining({ description: expect.stringMatching(/encerrada/i) }),
    );
    // Conferência fechada por outra pessoa: a tela recarrega e passa a mostrá-la encerrada.
    expect(refresh).toHaveBeenCalled();
  });

  it("'Desfazer' grava null — a linha volta a pendente no servidor também", async () => {
    contar.mockResolvedValue({
      ok: true,
      data: { line: { ...CONFERENCIA.rooms[0].lines[0], counted_qty: null }, progress: CONFERENCIA.progress },
    });
    const contada = {
      ...CONFERENCIA,
      rooms: [{ ...CONFERENCIA.rooms[0], lines: [{ ...CONFERENCIA.rooms[0].lines[0], counted_qty: 0 }] }],
    };
    render(<TelaDeContagem conferencia={contada} permissoes={{ editar: true, excluir: false }} />);

    fireEvent.click(screen.getByRole("button", { name: /desfazer a contagem de taça/i }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(400);
    });

    expect(contar).toHaveBeenCalledWith("c1", "l1", null);
  });
});

/**
 * A apuração de uma conferência fechada vem do `GET` (`result`), com o custo
 * **congelado no fechamento**. Antes ela só existia na resposta do
 * `POST /close`: recarregar a página fazia o prejuízo sumir da tela.
 */
describe("TelaDeContagem — conferência fechada", () => {
  const fechada: ConferenciaCompleta = {
    ...CONFERENCIA,
    status: "fechada",
    closed_at: "2026-10-07T13:00:00Z",
    progress: { lines: 1, counted: 1, pending: 0, diverging: 1 },
    rooms: [
      {
        ...CONFERENCIA.rooms[0],
        lines: [{ ...CONFERENCIA.rooms[0].lines[0], counted_qty: 9, diff: -3, replacement_cost_cents: 1890 }],
      },
    ],
    result: {
      divergences: [
        {
          room_id: "r1",
          room_name: "Cozinha",
          item_id: "i1",
          item_name: "Taça",
          expected_qty: 12,
          counted_qty: 9,
          diff: -3,
          replacement_cost_cents: 1890,
          loss_cents: 5670,
          issue_id: "a1",
        },
      ],
      issues_created: 1,
    },
  };

  it("mostra a apuração que o GET devolveu, com o prejuízo do custo congelado", () => {
    render(<TelaDeContagem conferencia={fechada} permissoes={{ editar: true, excluir: true }} />);
    const resultado = screen.getByRole("region", { name: "Resultado da conferência" });
    expect(resultado.textContent).toMatch(/1 divergência/);
    expect(resultado.textContent).toMatch(/56,70/);
    expect(resultado.textContent).toMatch(/1 avaria foi aberta/);
  });

  it("a linha fechada mostra o custo da época, não o do catálogo", () => {
    render(<TelaDeContagem conferencia={fechada} permissoes={{ editar: true, excluir: true }} />);
    expect(screen.getByText(/custo no fechamento/i).textContent).toMatch(/18,90/);
  });

  it("aberta não tem apuração", () => {
    render(<TelaDeContagem conferencia={{ ...CONFERENCIA, result: null }} permissoes={{ editar: true, excluir: true }} />);
    expect(screen.queryByRole("region", { name: "Resultado da conferência" })).toBeNull();
  });
});
