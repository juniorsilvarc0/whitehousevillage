import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { DetalheDoEvento } from "@/components/agenda/detalhe-do-evento";
import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { concluirEvento } from "@/lib/agenda/acoes";
import type { EventoDaAgenda } from "@/lib/agenda/tipos";

/**
 * O detalhe desenha os botões **a partir da resposta** — `allowed_actions`
 * para Concluir e Desmarcar, `editable` para Editar —, nunca do `status`, do
 * `type` ou da `origin`.
 */

const { refresh } = vi.hoisted(() => ({ refresh: vi.fn() }));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh, replace: vi.fn() }),
}));
vi.mock("sonner", () => ({ Toaster: () => null, toast: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warning: vi.fn() } }));
vi.mock("@/lib/agenda/acoes", () => ({
  concluirEvento: vi.fn(),
  desmarcarEvento: vi.fn(),
}));

const TODAS = { editar: true, excluir: true };
const DESTINOS = { reservas: true, manutencao: true, mapa: true };

function evento(parcial: Partial<EventoDaAgenda>): EventoDaAgenda {
  return {
    id: "00000000-0000-4000-8000-000000000001",
    type: "visita",
    origin: "manual",
    status: "agendado",
    title: "Visita da família Andrade",
    starts_at: "2026-11-20T15:00:00-03:00",
    ends_at: "2026-11-20T16:00:00-03:00",
    all_day: false,
    date: "2026-11-20",
    units: [{ unit_id: "u1", unit_code: "AP-02" }],
    has_unit_conflict: false,
    allowed_actions: ["complete", "cancel"],
    editable: "tudo",
    ...parcial,
  };
}

function Abrir({ alvo, permissoes = TODAS, aoEditar = vi.fn() }: { alvo: EventoDaAgenda; permissoes?: typeof TODAS; aoEditar?: () => void }) {
  const detalhe = useControleDeModal<EventoDaAgenda>();
  return (
    <>
      <button type="button" onClick={() => detalhe.abrir(alvo)}>
        abrir
      </button>
      <DetalheDoEvento controle={detalhe.ref} permissoes={permissoes} destinos={DESTINOS} aoEditar={aoEditar} />
    </>
  );
}

async function abrir(alvo: EventoDaAgenda, permissoes = TODAS) {
  render(<Abrir alvo={alvo} permissoes={permissoes} />);
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "abrir" }));
  });
  return screen.findByRole("dialog");
}

function botoes(): string[] {
  const grupo = screen.queryByRole("group", { name: /ações do evento/i });
  return grupo ? within(grupo).queryAllByRole("button").map((b) => b.textContent?.trim() ?? "") : [];
}

describe("os botões saem de allowed_actions e editable", () => {
  it("manual de hoje: Desmarcar, Editar e Concluir", async () => {
    await abrir(evento({}));
    expect(botoes()).toEqual(["Desmarcar", "Editar", "Concluir"]);
  });

  it("check-in automático (allowed_actions vazio, segue a reserva): só Editar — conclui pela reserva", async () => {
    await abrir(
      evento({ type: "checkin", origin: "reservation", title: "Check-in · WH-2026-0007", allowed_actions: [], editable: "segue_a_reserva", reservation_id: "r1", reservation_code: "WH-2026-0007" }),
    );
    expect(botoes()).toEqual(["Editar"]);
    expect(screen.getByRole("link", { name: "WH-2026-0007" }).getAttribute("href")).toBe("/app/reservas/r1");
  });

  it("concluído: só a anotação", async () => {
    await abrir(evento({ status: "concluido", allowed_actions: [], editable: "so_notas", done_at: "2026-11-20T16:05:00-03:00" }));
    expect(botoes()).toEqual(["Editar anotação"]);
  });

  it("controle negativo: 'agendado' com allowed_actions vazio e editable nada não ganha botão nenhum", async () => {
    // Se o painel decidisse pelo status, um evento agendado mostraria
    // Concluir e Desmarcar aqui.
    await abrir(evento({ status: "agendado", allowed_actions: [], editable: "nada" }));
    expect(botoes()).toEqual([]);
  });

  it("e o contrário: o que a API permitir aparece, mesmo num tipo que hoje não conclui por aqui", async () => {
    await abrir(evento({ type: "checkin", origin: "reservation", allowed_actions: ["complete"], editable: "nada" }));
    expect(botoes()).toEqual(["Concluir"]);
  });

  it("a matriz corta o que allowed_actions oferece", async () => {
    await abrir(evento({}), { editar: false, excluir: false });
    expect(botoes()).toEqual([]);
  });
});

describe("o derivado é só leitura e leva a onde ele se mexe", () => {
  it("manutenção abre a ordem; sem botão nenhum", async () => {
    await abrir(
      evento({
        type: "manutencao",
        origin: "maintenance",
        title: "Pintura da sala",
        all_day: true,
        starts_at: "2026-11-20T00:00:00-03:00",
        ends_at: "2026-11-22T00:00:00-03:00",
        allowed_actions: [],
        editable: "nada",
        maintenance_order_id: "ordem-1",
        stay_block_id: "sb-1",
      }),
    );
    expect(botoes()).toEqual([]);
    expect(screen.getByRole("link", { name: /abrir a ordem de manutenção/i }).getAttribute("href")).toBe("/app/manutencao/ordem-1");
  });

  it("bloqueio de unidade abre o mapa no dia dele", async () => {
    await abrir(evento({ type: "bloqueio", origin: "stay_block", all_day: true, allowed_actions: [], editable: "nada", stay_block_id: "sb-2" }));
    expect(screen.getByRole("link", { name: /abrir o bloqueio no mapa/i }).getAttribute("href")).toBe("/app/mapa?from=2026-11-20");
  });
});

describe("os selos", () => {
  it("conflito de unidade e status", async () => {
    const dialogo = await abrir(evento({ has_unit_conflict: true }));
    expect(within(dialogo).getByText("Conflito de unidade")).toBeTruthy();
    expect(within(dialogo).getByText("Agendado")).toBeTruthy();
  });

  it("desmarcado", async () => {
    const dialogo = await abrir(evento({ status: "cancelado", allowed_actions: [], editable: "nada", cancelled_at: "2026-11-19T10:00:00-03:00" }));
    expect(within(dialogo).getByText("Desmarcado")).toBeTruthy();
  });
});

describe("concluir antes do dia", () => {
  beforeEach(() => {
    refresh.mockReset();
    vi.mocked(concluirEvento).mockReset();
  });

  it("nao_comecou explica pelo details.reason, dentro do modal, e recarrega a agenda", async () => {
    vi.mocked(concluirEvento).mockResolvedValue({
      ok: false,
      code: "INVALID_STATE_TRANSITION",
      message: "not started",
      details: { reason: "nao_comecou", allowed: ["cancel"] },
    });
    await abrir(evento({}));
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Concluir" }));
    });
    await waitFor(() => expect(screen.getByRole("alert").textContent).toMatch(/a partir do dia marcado/));
    expect(refresh).toHaveBeenCalled();
  });
});
