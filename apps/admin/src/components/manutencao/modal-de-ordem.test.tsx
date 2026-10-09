import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { BotaoNovaOrdem } from "@/components/manutencao/botao-nova-ordem";
import { abrirOrdem } from "@/lib/manutencao/acoes";

/**
 * O formulário de criação leva a recusa da API **para o campo do período**:
 * `details["block.from"]`/`["block.to"]` embaixo de cada data, e o
 * `DATE_CONFLICT` (que não tem campo em `details`) embaixo das duas, com a
 * unidade — é ali que a pessoa está olhando quando vai trocar a data.
 */

const { push } = vi.hoisted(() => ({ push: vi.fn() }));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, refresh: vi.fn(), replace: vi.fn() }),
}));
vi.mock("sonner", () => ({ Toaster: () => null, toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() } }));
vi.mock("@/lib/manutencao/acoes", () => ({
  abrirOrdem: vi.fn(),
  opcoesDaUnidade: vi.fn(async () => ({ ok: true, data: { ambientes: [] } })),
  buscarBens: vi.fn(),
}));

const UNIDADE = "11111111-1111-4111-8111-111111111111";
const abrir = vi.mocked(abrirOrdem);

async function preencherComBloqueio() {
  render(<BotaoNovaOrdem unidades={[{ id: UNIDADE, code: "AP-03", name: "Duplex 03" }]} unidadeInicial={UNIDADE} />);
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: /nova ordem/i }));
  });
  await screen.findByRole("dialog");
  fireEvent.change(screen.getByLabelText(/o que precisa ser feito/i), { target: { value: "Pintar a sala" } });
  fireEvent.click(screen.getByLabelText(/bloquear calendário/i));
  fireEvent.change(await screen.findByLabelText(/primeiro dia bloqueado/i), { target: { value: "2026-11-10" } });
  fireEvent.change(screen.getByLabelText(/volta à venda em/i), { target: { value: "2026-11-15" } });
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: /abrir ordem/i }));
  });
}

describe("o período no campo certo", () => {
  beforeEach(() => {
    abrir.mockReset();
    push.mockReset();
  });

  it("envia o bloqueio pedido e, criada, abre a ordem", async () => {
    abrir.mockResolvedValue({ ok: true, data: { id: "nova", block: null } as never });
    await preencherComBloqueio();
    await waitFor(() => expect(abrir).toHaveBeenCalled());
    expect(abrir.mock.calls[0][0]).toMatchObject({
      unit_id: UNIDADE,
      title: "Pintar a sala",
      bloquear: true,
      block_from: "2026-11-10",
      block_to: "2026-11-15",
    });
    await waitFor(() => expect(push).toHaveBeenCalledWith("/app/manutencao/nova"));
  });

  it("DATE_CONFLICT aparece embaixo das datas, com a unidade — e o modal não fecha", async () => {
    abrir.mockResolvedValue({
      ok: false,
      code: "DATE_CONFLICT",
      message: "conflict",
      details: { unit_code: "AP-03", period: "[2026-11-10,2026-11-15)" },
    });
    await preencherComBloqueio();

    const grupo = screen.getByRole("group", { name: /período do bloqueio/i });
    await waitFor(() => expect(grupo.textContent).toMatch(/AP-03/));
    expect(grupo.textContent).toMatch(/nada foi criado/i);
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(push).not.toHaveBeenCalled();
  });

  it("block.from e block.to caem cada um embaixo da sua data", async () => {
    abrir.mockResolvedValue({
      ok: false,
      code: "VALIDATION_ERROR",
      message: "invalid",
      details: { "block.from": "O bloqueio começa hoje ou depois.", "block.to": "No máximo 365 noites." },
    });
    await preencherComBloqueio();

    await waitFor(() => expect(screen.getByText("O bloqueio começa hoje ou depois.")).toBeTruthy());
    const de = screen.getByLabelText(/primeiro dia bloqueado/i);
    const ate = screen.getByLabelText(/volta à venda em/i);
    expect(de.getAttribute("aria-describedby")).toContain("erro");
    expect(document.getElementById(`${de.id}-erro`)?.textContent).toBe("O bloqueio começa hoje ou depois.");
    expect(document.getElementById(`${ate.id}-erro`)?.textContent).toBe("No máximo 365 noites.");
  });
});
