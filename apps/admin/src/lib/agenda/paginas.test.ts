import { describe, expect, it, vi } from "vitest";

import type { Lista } from "@/lib/api/types";
import { todasAsPaginas } from "@/lib/agenda/paginas";

function pagina(n: number, total: number, itens: number[]): Lista<number> {
  return { data: itens, meta: { page: n, per_page: 100, total: 0, total_pages: total } };
}

describe("a janela inteira, seguindo total_pages", () => {
  it("pede da 1 até a última e junta tudo, em ordem", async () => {
    const buscar = vi.fn(async (n: number) => pagina(n, 3, [n * 10, n * 10 + 1]));
    const r = await todasAsPaginas(buscar);
    expect(buscar.mock.calls.map((c) => c[0])).toEqual([1, 2, 3]);
    expect(r).toEqual({ itens: [10, 11, 20, 21, 30, 31], incompleta: false });
  });

  it("uma página só: uma chamada", async () => {
    const buscar = vi.fn(async (n: number) => pagina(n, 1, [1]));
    await todasAsPaginas(buscar);
    expect(buscar).toHaveBeenCalledTimes(1);
  });

  it("página vazia antes do fim encerra (a API mudou por baixo)", async () => {
    const buscar = vi.fn(async (n: number) => pagina(n, 5, n === 2 ? [] : [n]));
    const r = await todasAsPaginas(buscar);
    expect(buscar).toHaveBeenCalledTimes(2);
    expect(r.incompleta).toBe(false);
  });

  it("o teto corta — e diz que cortou, em vez de desenhar meia agenda como se fosse a inteira", async () => {
    const buscar = vi.fn(async (n: number) => pagina(n, 50, [n]));
    const r = await todasAsPaginas(buscar, 3);
    expect(buscar).toHaveBeenCalledTimes(3);
    expect(r).toEqual({ itens: [1, 2, 3], incompleta: true });
  });
});
