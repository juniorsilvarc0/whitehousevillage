import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";

import { DetalheDaOrdem } from "@/components/manutencao/detalhe-da-ordem";
import { iniciarOrdem } from "@/lib/manutencao/acoes";
import type { OrdemDeManutencao } from "@/lib/manutencao/tipos";

/**
 * A tela de detalhe desenha os botões **a partir da resposta** — os quatro
 * estados do contrato, cada um com o `allowed_actions`/`editable` que a API
 * devolve para ele. E o segundo toque em "Iniciar" (409
 * `INVALID_STATE_TRANSITION`) recarrega a ordem em vez de virar erro mudo.
 */

const { refresh } = vi.hoisted(() => ({ refresh: vi.fn() }));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh, replace: vi.fn() }),
}));
vi.mock("sonner", () => ({ Toaster: () => null, toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() } }));
vi.mock("@/lib/manutencao/acoes", () => ({
  abrirOrdem: vi.fn(),
  salvarOrdem: vi.fn(),
  lancarCusto: vi.fn(),
  iniciarOrdem: vi.fn(),
  concluirOrdem: vi.fn(),
  cancelarOrdem: vi.fn(),
  definirBloqueio: vi.fn(),
  soltarBloqueio: vi.fn(),
  opcoesDaUnidade: vi.fn(async () => ({ ok: true, data: { ambientes: [] } })),
  buscarBens: vi.fn(),
}));

const TUDO = { editar: true, excluir: true };

function ordem(parcial: Partial<OrdemDeManutencao>): OrdemDeManutencao {
  return {
    id: "9f1c1e2a-0d3b-4f6a-9a1e-2b7c5d8e0f11",
    unit_id: "11111111-1111-4111-8111-111111111111",
    unit_code: "AP-03",
    unit_name: "Duplex 03",
    title: "Ar-condicionado da suíte não gela",
    priority: "alta",
    status: "aberta",
    opened_at: "2026-10-09T12:00:00Z",
    updated_at: "2026-10-09T12:00:00Z",
    allowed_actions: ["start", "complete", "cancel"],
    editable: "tudo",
    block: null,
    ...parcial,
  };
}

function botoes(): string[] {
  const grupo = screen.queryByRole("group", { name: /ações da ordem/i });
  const daBarra = grupo ? within(grupo).queryAllByRole("button").map((b) => b.textContent?.trim() ?? "") : [];
  const doCalendario = screen
    .queryAllByRole("button", { name: /bloquear calendário|mudar período|soltar bloqueio/i })
    .map((b) => b.textContent?.trim() ?? "");
  return [...daBarra, ...doCalendario];
}

describe("os botões saem de allowed_actions e editable", () => {
  it("aberta: Iniciar, Concluir, Editar, Cancelar ordem e Bloquear calendário", () => {
    render(<DetalheDaOrdem ordem={ordem({})} permissoes={TUDO} podeVerInventario />);
    expect(botoes()).toEqual(["Iniciar", "Concluir", "Editar", "Cancelar ordem", "Bloquear calendário"]);
  });

  it("em andamento: sem Iniciar; com bloqueio em curso, muda o período ou solta", () => {
    render(
      <DetalheDaOrdem
        ordem={ordem({
          status: "em_andamento",
          allowed_actions: ["complete", "cancel"],
          block: { id: "b", from: "2026-10-08", to: "2026-10-12", nights: 4, status: "confirmed", phase: "em_curso" },
        })}
        permissoes={TUDO}
        podeVerInventario
      />,
    );
    expect(botoes()).toEqual(["Concluir", "Editar", "Cancelar ordem", "Mudar período", "Soltar bloqueio"]);
  });

  it("concluída: só o custo", () => {
    render(
      <DetalheDaOrdem
        ordem={ordem({ status: "concluida", allowed_actions: [], editable: "so_custo", closed_at: "2026-10-09T15:00:00Z", cost_cents: null })}
        permissoes={TUDO}
        podeVerInventario
      />,
    );
    expect(botoes()).toEqual(["Lançar custo"]);
    expect(screen.getByText(/só o custo ainda pode/i)).toBeTruthy();
  });

  it("concluída com custo já lançado: o mesmo botão vira 'Corrigir custo'", () => {
    render(
      <DetalheDaOrdem
        ordem={ordem({ status: "concluida", allowed_actions: [], editable: "so_custo", cost_cents: 35_000 })}
        permissoes={TUDO}
        podeVerInventario
      />,
    );
    expect(botoes()).toEqual(["Corrigir custo"]);
    expect(screen.getByText(/R\$\s350,00/)).toBeTruthy();
  });

  it("cancelada: nada", () => {
    render(
      <DetalheDaOrdem
        ordem={ordem({ status: "cancelada", allowed_actions: [], editable: "nada", closed_at: "2026-10-09T15:00:00Z" })}
        permissoes={TUDO}
        podeVerInventario
      />,
    );
    expect(botoes()).toEqual([]);
    expect(screen.queryByRole("group", { name: /ações da ordem/i })).toBeNull();
  });

  it("a fase do bloqueio sai de block.phase, com a última noite escrita como véspera do to", () => {
    render(
      <DetalheDaOrdem
        ordem={ordem({ block: { id: "b", from: "2026-11-10", to: "2026-11-15", nights: 5, status: "confirmed", phase: "agendado" } })}
        permissoes={TUDO}
        podeVerInventario
      />,
    );
    expect(screen.getAllByText(/bloqueio agendado/i).length).toBeGreaterThan(0);
    expect(screen.getByText(/noites de 10\/11\/2026 a 14\/11\/2026 \(5 noites\) · livre a partir de 15\/11\/2026/)).toBeTruthy();
  });

  it("sem permissão de editar, nenhum botão — mesmo com a ordem aberta", () => {
    render(<DetalheDaOrdem ordem={ordem({})} permissoes={{ editar: false, excluir: false }} podeVerInventario={false} />);
    expect(botoes()).toEqual([]);
  });
});

describe("o segundo toque em Iniciar", () => {
  beforeEach(() => {
    refresh.mockReset();
    vi.mocked(iniciarOrdem).mockReset();
  });

  it("INVALID_STATE_TRANSITION recarrega a ordem e explica pelo código", async () => {
    vi.mocked(iniciarOrdem).mockResolvedValue({
      ok: false,
      code: "INVALID_STATE_TRANSITION",
      message: "já em andamento",
      details: { status: "em_andamento", allowed: ["complete", "cancel"] },
    });
    render(<DetalheDaOrdem ordem={ordem({})} permissoes={TUDO} podeVerInventario />);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Iniciar" }));
    });
    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(vi.mocked(toast.error)).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ description: expect.stringMatching(/já mexeu nesta ordem/i) }),
    );
  });
});
