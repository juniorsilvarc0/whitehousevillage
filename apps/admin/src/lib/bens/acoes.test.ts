import { beforeEach, describe, expect, it, vi } from "vitest";

import { contarLinha, fecharConferencia, registrarAvaria } from "@/lib/bens/acoes";
import type { AvariaFormulario } from "@/lib/bens/esquemas";

/**
 * O CORPO que cada gesto manda à API — onde "campo ausente" e "campo nulo"
 * são coisas diferentes (`Opt[T]` do lado Go) e o contrato depende disso:
 *
 * - `ContagemDaLinha` não tem campo obrigatório: o +/− manda o número, o
 *   "Desfazer" manda `counted_qty: null` — presente, e nulo.
 * - `PedidoDeFechamento.note`: ausente mantém, texto substitui, `null` limpa.
 * - `AvariaCriar`: a reserva vai por `reservation_code`, nunca junto com
 *   `reservation_id` (os dois é `422`), e campo vazio não vai.
 */

const { apiFetch, ErroDaApi } = vi.hoisted(() => {
  class ErroDaApi extends Error {
    constructor(
      readonly code: string,
      readonly details: Record<string, unknown> = {},
    ) {
      super(code);
    }
  }
  return { apiFetch: vi.fn(), ErroDaApi };
});

vi.mock("next/cache", () => ({ revalidatePath: vi.fn() }));
vi.mock("@/lib/acoes/guarda", () => ({ exigir: vi.fn(async () => null) }));
vi.mock("@/lib/api/client", () => ({
  apiFetch,
  isApiError: (e: unknown) => e instanceof ErroDaApi,
}));

const CONFERENCIA = "9f1c1e2a-0d3b-4f6a-9a1e-2b7c5d8e0f11";
const LINHA = "3a8d5c71-6b2e-4f10-9d44-0c15e7b39a62";
const AMBIENTE = "11111111-1111-4111-8111-111111111111";
const BEM = "22222222-2222-4222-8222-222222222222";

/** O corpo exatamente como sai para o fio — `JSON.stringify` é o que distingue
 *  chave ausente de chave com `null`. */
function corpoEnviado(): string {
  const init = apiFetch.mock.calls.at(-1)?.[1] as { body?: unknown } | undefined;
  return JSON.stringify(init?.body);
}

beforeEach(() => {
  apiFetch.mockReset();
  apiFetch.mockResolvedValue({});
});

describe("contar uma linha", () => {
  it("o número vai em counted_qty", async () => {
    await contarLinha(CONFERENCIA, LINHA, 9);
    expect(apiFetch.mock.calls[0][0]).toBe(`/inventory/counts/${CONFERENCIA}/lines/${LINHA}`);
    expect(corpoEnviado()).toBe('{"counted_qty":9}');
  });

  it("zero vai como zero — 'contei e não achei'", async () => {
    await contarLinha(CONFERENCIA, LINHA, 0);
    expect(corpoEnviado()).toBe('{"counted_qty":0}');
  });

  it("'Desfazer' manda counted_qty presente e nulo — e não um corpo vazio, que seria 422", async () => {
    await contarLinha(CONFERENCIA, LINHA, null);
    expect(corpoEnviado()).toBe('{"counted_qty":null}');
  });

  it("número negativo nem sai do painel", async () => {
    const r = await contarLinha(CONFERENCIA, LINHA, -1);
    expect(r.ok).toBe(false);
    expect(apiFetch).not.toHaveBeenCalled();
  });
});

describe("a observação do fechamento", () => {
  it("ausente: o corpo não leva note (mantém a que existe)", async () => {
    await fecharConferencia(CONFERENCIA, true, undefined);
    expect(corpoEnviado()).toBe('{"raise_issues":true}');
  });

  it("texto: substitui", async () => {
    await fecharConferencia(CONFERENCIA, false, "  faltou o cofre ");
    expect(corpoEnviado()).toBe('{"raise_issues":false,"note":"faltou o cofre"}');
  });

  it("null: limpa", async () => {
    await fecharConferencia(CONFERENCIA, true, null);
    expect(corpoEnviado()).toBe('{"raise_issues":true,"note":null}');
  });
});

describe("a reserva da avaria vai pelo código", () => {
  const base: AvariaFormulario = {
    room_id: AMBIENTE,
    item_id: BEM,
    kind: "quebrado",
    qty: "2",
    note: "",
    reservation_code: "",
  };

  it("código preenchido vai em reservation_code, normalizado — e nunca com reservation_id", async () => {
    await registrarAvaria({ ...base, reservation_code: " wh-2026-0142 " });
    const corpo = JSON.parse(corpoEnviado()) as Record<string, unknown>;
    expect(corpo.reservation_code).toBe("WH-2026-0142");
    expect("reservation_id" in corpo, "mandar os dois é 422").toBe(false);
    expect(corpo.qty).toBe(2);
  });

  it("sem código, nenhuma das duas chaves vai", async () => {
    await registrarAvaria(base);
    const corpo = JSON.parse(corpoEnviado()) as Record<string, unknown>;
    expect("reservation_code" in corpo).toBe(false);
    expect("reservation_id" in corpo).toBe(false);
  });

  it("a recusa por código inexistente volta com details.reservation_code, para o formulário pôr no campo", async () => {
    apiFetch.mockRejectedValueOnce(
      new ErroDaApi("VALIDATION_ERROR", { reservation_code: "Nenhuma reserva com este código nesta casa." }),
    );
    const r = await registrarAvaria({ ...base, reservation_code: "WH-1999-0001" });
    expect(r.ok).toBe(false);
    if (!r.ok) {
      expect(r.code).toBe("VALIDATION_ERROR");
      expect(r.details.reservation_code).toBe("Nenhuma reserva com este código nesta casa.");
    }
  });
});
