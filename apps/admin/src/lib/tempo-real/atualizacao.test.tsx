import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useAtualizacaoAoVivo } from "./atualizacao";
import type { EventoDoStream } from "./eventos";
import type { FonteDeEventos } from "./sse";

class FonteFalsa implements FonteDeEventos {
  static ultima: FonteFalsa | null = null;
  readyState = 0;
  private ouvintes = new Map<string, ((evento: MessageEvent<string>) => void)[]>();

  constructor(public readonly url: string) {
    FonteFalsa.ultima = this;
  }

  addEventListener(tipo: string, ouvinte: (evento: MessageEvent<string>) => void): void {
    this.ouvintes.set(tipo, [...(this.ouvintes.get(tipo) ?? []), ouvinte]);
  }

  close(): void {
    this.readyState = 2;
  }

  emitir(tipo: string, data: string): void {
    act(() => {
      for (const ouvinte of this.ouvintes.get(tipo) ?? []) {
        ouvinte(new MessageEvent(tipo, { data }));
      }
    });
  }
}

const criarFonte = (url: string): FonteDeEventos => new FonteFalsa(url);
const fonte = () => FonteFalsa.ultima!;

const bloco = (id: string, v: number) => JSON.stringify({ entity: "stay_block", id, v });

const interessa = (evento: EventoDoStream) =>
  evento.entity === "stay_block" || evento.entity === "reservation";

describe("useAtualizacaoAoVivo", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("junta a rajada de uma venda da Completa numa requisição só", async () => {
    // Oito `stay_block` e um `reservation`, todos na mesma transação. Nove
    // buscas para chegar ao mesmo desenho é o defeito que a janela de
    // agrupamento existe para impedir.
    const buscar = vi.fn().mockResolvedValue({ ok: true, data: "matriz" });
    const aoAtualizar = vi.fn();

    renderHook(() =>
      useAtualizacaoAoVivo({ topicos: ["calendar"], interessa, buscar, aoAtualizar, criarFonte }),
    );

    for (let i = 0; i < 8; i += 1) fonte().emitir("calendar", bloco(`b${i}`, 853));
    fonte().emitir("calendar", JSON.stringify({ entity: "reservation", id: "r1", v: 853 }));

    expect(buscar).not.toHaveBeenCalled();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });

    expect(buscar).toHaveBeenCalledTimes(1);
    expect(aoAtualizar).toHaveBeenCalledWith("matriz");
  });

  it("não gasta requisição com evento de outro assunto", async () => {
    const buscar = vi.fn().mockResolvedValue({ ok: true, data: "x" });
    renderHook(() =>
      useAtualizacaoAoVivo({
        topicos: ["calendar", "crm"],
        interessa,
        buscar,
        aoAtualizar: vi.fn(),
        criarFonte,
      }),
    );

    fonte().emitir("crm", JSON.stringify({ entity: "opportunity", id: "o1", v: 3 }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });

    expect(buscar).not.toHaveBeenCalled();
  });

  it("uma requisição no ar por vez, e o evento da rajada vira exatamente mais uma", async () => {
    let liberar: ((valor: { ok: true; data: string }) => void) | null = null;
    const buscar = vi
      .fn()
      .mockImplementationOnce(
        () => new Promise<{ ok: true; data: string }>((resolve) => (liberar = resolve)),
      )
      .mockResolvedValue({ ok: true, data: "segunda" });

    renderHook(() =>
      useAtualizacaoAoVivo({
        topicos: ["calendar"],
        interessa,
        buscar,
        aoAtualizar: vi.fn(),
        criarFonte,
      }),
    );

    fonte().emitir("calendar", bloco("b1", 1));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(buscar).toHaveBeenCalledTimes(1);

    // Chega evento com a primeira busca ainda no ar.
    fonte().emitir("calendar", bloco("b2", 2));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(buscar).toHaveBeenCalledTimes(1);

    await act(async () => {
      liberar?.({ ok: true, data: "primeira" });
      await vi.advanceTimersByTimeAsync(0);
    });

    await waitFor(() => expect(buscar).toHaveBeenCalledTimes(2));
  });

  it("falha de atualização não apaga o desenho: guarda o código e segue mostrando o anterior", async () => {
    const buscar = vi.fn().mockResolvedValue({ ok: false, code: "NETWORK_ERROR", message: "", details: {} });
    const aoAtualizar = vi.fn();

    const { result } = renderHook(() =>
      useAtualizacaoAoVivo({ topicos: ["calendar"], interessa, buscar, aoAtualizar, criarFonte }),
    );

    fonte().emitir("calendar", bloco("b1", 1));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });

    expect(aoAtualizar).not.toHaveBeenCalled();
    await waitFor(() => expect(result.current.falha).toBe("NETWORK_ERROR"));
  });

  it("`resync` do servidor também dispara a busca", async () => {
    const buscar = vi.fn().mockResolvedValue({ ok: true, data: "x" });
    renderHook(() =>
      useAtualizacaoAoVivo({
        topicos: ["calendar"],
        interessa,
        buscar,
        aoAtualizar: vi.fn(),
        criarFonte,
      }),
    );

    fonte().emitir("resync", "{}");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });

    expect(buscar).toHaveBeenCalledTimes(1);
  });
});
