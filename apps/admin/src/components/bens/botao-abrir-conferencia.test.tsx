import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";

import { BotaoAbrirConferencia } from "@/components/bens/botao-abrir-conferencia";
import { abrirConferencia } from "@/lib/bens/acoes";

/**
 * O segundo toque em "Abrir conferência" **não vira beco**.
 *
 * O banco garante uma conferência aberta por unidade; a segunda abertura é
 * `409 COUNT_ALREADY_OPEN` com `details.count_id`. É o caso comum (duas pessoas
 * no plantão, ou a tela velha no celular), e a resposta certa é levar à
 * conferência que já existe — avisando que ela já estava aberta.
 */

const { push } = vi.hoisted(() => ({ push: vi.fn() }));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, refresh: vi.fn(), replace: vi.fn() }),
}));

vi.mock("sonner", () => ({
  Toaster: () => null,
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

vi.mock("@/lib/bens/acoes", () => ({ abrirConferencia: vi.fn() }));

const abrir = vi.mocked(abrirConferencia);
const UNIDADE = "11111111-1111-4111-8111-111111111111";
const ABERTA = "9f1c1e2a-0d3b-4f6a-9a1e-2b7c5d8e0f11";

async function tocarEmAbrir() {
  render(<BotaoAbrirConferencia unitId={UNIDADE} unidadeRotulo="AP-01" />);
  fireEvent.click(screen.getByRole("button", { name: /abrir conferência/i }));
  await screen.findByRole("dialog");
  // `act` assíncrono: a action devolve uma promessa, e o que ela muda na tela
  // (fechar o modal, mostrar a recusa) tem de acontecer dentro do teste.
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: /abrir e começar a contar/i }));
  });
}

describe("BotaoAbrirConferencia", () => {
  beforeEach(() => {
    push.mockReset();
    abrir.mockReset();
  });

  it("abre e leva à conferência nova", async () => {
    abrir.mockResolvedValue({ ok: true, data: { id: "nova" } });
    await tocarEmAbrir();
    await waitFor(() => expect(push).toHaveBeenCalledWith("/app/inventario/conferencias/nova"));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("COUNT_ALREADY_OPEN leva à conferência que já estava aberta, e avisa", async () => {
    abrir.mockResolvedValue({
      ok: false,
      code: "COUNT_ALREADY_OPEN",
      message: "já aberta",
      details: { count_id: ABERTA, opened_at: "2026-10-07T14:32:00Z" },
    });
    await tocarEmAbrir();

    await waitFor(() => expect(push).toHaveBeenCalledWith(`/app/inventario/conferencias/${ABERTA}`));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(vi.mocked(toast.info)).toHaveBeenCalledWith(
      expect.stringMatching(/já tinha uma conferência em andamento/i),
      expect.objectContaining({ description: expect.stringMatching(/abrindo ela/i) }),
    );
  });

  it("outra recusa fica no modal, com a frase pelo código — sem navegar", async () => {
    abrir.mockResolvedValue({ ok: false, code: "VALIDATION_ERROR", message: "x", details: {} });
    await tocarEmAbrir();

    expect(await screen.findByRole("alert")).toHaveProperty("textContent", expect.stringMatching(/bens colocados/i));
    await waitFor(() =>
      expect((screen.getByRole("button", { name: /abrir e começar a contar/i }) as HTMLButtonElement).disabled).toBe(false),
    );
    expect(push).not.toHaveBeenCalled();
  });

  it("com a conferência aberta já conhecida, o botão continua em vez de abrir", () => {
    render(<BotaoAbrirConferencia unitId={UNIDADE} unidadeRotulo="AP-01" abertaId={ABERTA} />);
    const link = screen.getByRole("link", { name: /continuar conferência/i });
    expect(link.getAttribute("href")).toBe(`/app/inventario/conferencias/${ABERTA}`);
    expect(screen.queryByRole("button", { name: /abrir conferência/i })).toBeNull();
  });
});
