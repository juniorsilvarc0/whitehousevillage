import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  abrirOrdem,
  cancelarOrdem,
  concluirOrdem,
  definirBloqueio,
  iniciarOrdem,
  lancarCusto,
  salvarOrdem,
  soltarBloqueio,
} from "@/lib/manutencao/acoes";
import { ordemVazia } from "@/lib/manutencao/esquemas";

/**
 * O que cada gesto manda ao fio: verbo, rota e corpo — onde "chave ausente" e
 * "chave com null" são coisas diferentes (`Opt[T]` do lado Go) e o contrato
 * depende disso. As transições são ações nomeadas, nunca `PATCH {status}`.
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
  apiList: vi.fn(),
  isApiError: (e: unknown) => e instanceof ErroDaApi,
}));

const ORDEM = "9f1c1e2a-0d3b-4f6a-9a1e-2b7c5d8e0f11";
const UNIDADE = "11111111-1111-4111-8111-111111111111";

function chamada(): { rota: string; metodo?: string; corpo: string | undefined; temCorpo: boolean } {
  const [rota, init] = apiFetch.mock.calls.at(-1) as [string, { method?: string; body?: unknown }];
  return { rota, metodo: init.method, corpo: JSON.stringify(init.body), temCorpo: "body" in init };
}

beforeEach(() => {
  apiFetch.mockReset();
  apiFetch.mockResolvedValue({ id: ORDEM });
});

describe("as transições são ações nomeadas", () => {
  it("iniciar é POST /start, sem corpo", async () => {
    await iniciarOrdem(ORDEM);
    expect(chamada()).toMatchObject({ rota: `/maintenance-orders/${ORDEM}/start`, metodo: "POST", temCorpo: false });
  });

  it("concluir sem custo é POST /complete SEM corpo — o custo já lançado fica", async () => {
    await concluirOrdem(ORDEM, { cost: "" });
    expect(chamada()).toMatchObject({ rota: `/maintenance-orders/${ORDEM}/complete`, metodo: "POST", temCorpo: false });
  });

  it("concluir com custo manda só cost_cents, em centavos inteiros", async () => {
    await concluirOrdem(ORDEM, { cost: "1.250,00" });
    expect(chamada().corpo).toBe('{"cost_cents":125000}');
  });

  it("cancelar é DELETE na ordem — e a resposta (200 com a ordem) volta para a tela", async () => {
    apiFetch.mockResolvedValueOnce({ id: ORDEM, status: "cancelada", block: { phase: "liberado" } });
    const r = await cancelarOrdem(ORDEM);
    expect(chamada()).toMatchObject({ rota: `/maintenance-orders/${ORDEM}`, metodo: "DELETE" });
    expect(r.ok && r.data).toMatchObject({ status: "cancelada" });
  });
});

describe("o custo depois de concluída", () => {
  it("PATCH leva só cost_cents — qualquer outra chave seria 409", async () => {
    await lancarCusto(ORDEM, { cost: "89,90" });
    expect(chamada()).toMatchObject({ rota: `/maintenance-orders/${ORDEM}`, metodo: "PATCH", corpo: '{"cost_cents":8990}' });
  });

  it("vazio limpa: cost_cents presente e nulo", async () => {
    await lancarCusto(ORDEM, { cost: "" });
    expect(chamada().corpo).toBe('{"cost_cents":null}');
  });

  it("zero nem sai do painel", async () => {
    const r = await lancarCusto(ORDEM, { cost: "0" });
    expect(r.ok).toBe(false);
    expect(apiFetch).not.toHaveBeenCalled();
  });
});

describe("criar", () => {
  it("POST com o período só quando a caixa está marcada", async () => {
    await abrirOrdem(ordemVazia({ unit_id: UNIDADE, title: "Pintar", bloquear: true, block_from: "2026-11-10", block_to: "2026-11-15" }));
    const { rota, metodo, corpo } = chamada();
    expect(rota).toBe("/maintenance-orders");
    expect(metodo).toBe("POST");
    expect(JSON.parse(corpo!)).toEqual({
      unit_id: UNIDADE,
      title: "Pintar",
      description: null,
      priority: "normal",
      cost_cents: null,
      block: { from: "2026-11-10", to: "2026-11-15" },
    });
  });

  it("a recusa atravessa com o details intacto — é dele que sai o link para a ordem existente", async () => {
    apiFetch.mockRejectedValueOnce(new ErroDaApi("MAINTENANCE_ORDER_ALREADY_OPEN", { maintenance_order_id: ORDEM }));
    const r = await abrirOrdem(ordemVazia({ unit_id: UNIDADE, title: "Trocar a lâmpada" }));
    expect(r).toMatchObject({ ok: false, code: "MAINTENANCE_ORDER_ALREADY_OPEN", details: { maintenance_order_id: ORDEM } });
  });

  it("título só com espaço nem sai do painel", async () => {
    const r = await abrirOrdem(ordemVazia({ unit_id: UNIDADE, title: "   " }));
    expect(r.ok).toBe(false);
    expect(apiFetch).not.toHaveBeenCalled();
  });
});

describe("editar e o bloqueio", () => {
  it("PUT com avaria ligada não manda cômodo nem bem", async () => {
    await salvarOrdem(ORDEM, { room_id: "", item_id: "", title: "Ar", description: "", priority: "alta", cost: "" }, true);
    const corpo = JSON.parse(chamada().corpo!) as Record<string, unknown>;
    expect(chamada().metodo).toBe("PUT");
    expect(Object.keys(corpo).sort()).toEqual(["cost_cents", "description", "priority", "title"]);
  });

  it("PUT /block leva {from, to}; DELETE /block solta", async () => {
    await definirBloqueio(ORDEM, { block_from: "2026-12-01", block_to: "2026-12-03" });
    expect(chamada()).toMatchObject({ rota: `/maintenance-orders/${ORDEM}/block`, metodo: "PUT", corpo: '{"from":"2026-12-01","to":"2026-12-03"}' });
    await soltarBloqueio(ORDEM);
    expect(chamada()).toMatchObject({ rota: `/maintenance-orders/${ORDEM}/block`, metodo: "DELETE", temCorpo: false });
  });

  it("id que não é UUID nem sai do painel", async () => {
    const r = await iniciarOrdem("../../users");
    expect(r.ok).toBe(false);
    expect(apiFetch).not.toHaveBeenCalled();
  });
});
