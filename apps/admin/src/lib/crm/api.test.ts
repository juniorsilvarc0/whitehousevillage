// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, apiFetch } from "@/lib/api/client";
import { chamarCrm } from "@/lib/crm/api";

/**
 * A recuperação do código cru — o mecanismo que `lib/crm/api.ts` inventou, e a
 * prova de que ele funciona.
 *
 * Roda em ambiente **node** (`@vitest-environment node`) de propósito: o
 * cliente da API se recusa a rodar onde existe `window`, porque o token vive em
 * cookie `httpOnly` e não existe no navegador. Testar isto em jsdom mediria a
 * própria guarda, não o mecanismo.
 */

vi.mock("next/headers", () => ({
  cookies: async () => ({ get: () => ({ value: "token-de-teste" }) }),
}));

function respondaCom(status: number, corpo: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      new Response(JSON.stringify(corpo), {
        status,
        headers: { "content-type": "application/json" },
      }),
    ),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

const CONVERTIDO = {
  error: {
    code: "LEAD_ALREADY_CONVERTED",
    message: "lead já convertido",
    details: { opportunity_id: "op-42" },
  },
};

describe("chamarCrm", () => {
  it("o cliente da casca perde o código do CRM — é o problema que se está resolvendo", async () => {
    respondaCom(409, CONVERTIDO);

    // Controle: pelo caminho normal, `LEAD_ALREADY_CONVERTED` some. O 409 não
    // tem ramo em `normalizarCodigo` e cai em INTERNAL, e a tela diria "erro no
    // servidor" para uma recusa que o operador resolve num clique.
    await expect(apiFetch("/crm/leads/l-1/convert", { method: "POST" })).rejects.toMatchObject({
      code: "INTERNAL",
    });
  });

  it("recupera o código cru do corpo, lendo uma cópia da resposta", async () => {
    respondaCom(409, CONVERTIDO);

    const resultado = await chamarCrm("/crm/leads/l-1/convert", { method: "POST" });

    expect(resultado.ok).toBe(false);
    if (resultado.ok) return;
    expect(resultado.code).toBe("LEAD_ALREADY_CONVERTED");
    // O `details` continua chegando inteiro: é ele que abre o card certo em vez
    // de deixar o operador criar um gêmeo no funil.
    expect(resultado.details.opportunity_id).toBe("op-42");
  });

  it("não inventa código: corpo sem `error.code` cai na normalização da casca", async () => {
    respondaCom(500, { error: { message: "boom" } });

    const resultado = await chamarCrm("/crm/opportunities/kanban");
    expect(resultado.ok).toBe(false);
    if (resultado.ok) return;
    expect(resultado.code).toBe("INTERNAL");
  });

  it("código geral atravessa intacto — o mecanismo não sequestra o vocabulário existente", async () => {
    respondaCom(409, {
      error: { code: "DATE_CONFLICT", message: "ocupada", details: { unit_code: "SP-03" } },
    });

    const resultado = await chamarCrm("/crm/opportunities/op-1/win", { method: "POST" });
    expect(resultado.ok).toBe(false);
    if (resultado.ok) return;
    expect(resultado.code).toBe("DATE_CONFLICT");
    expect(resultado.details.unit_code).toBe("SP-03");
  });

  it("o caminho feliz não lê o corpo duas vezes", async () => {
    respondaCom(200, { data: { id: "op-1" } });

    const resultado = await chamarCrm<{ id: string }>("/crm/opportunities/op-1");
    expect(resultado).toEqual({ ok: true, data: { id: "op-1" } });
  });

  it("erro que não é da API não se disfarça de recusa comercial", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => {
      throw new TypeError("fetch failed");
    }));

    const resultado = await chamarCrm("/crm/pipelines");
    expect(resultado.ok).toBe(false);
    if (resultado.ok) return;
    // `NETWORK_ERROR` vem do próprio cliente da casca: a tela precisa saber que
    // o problema é de conexão para oferecer "tentar de novo".
    expect(resultado.code).toBe("NETWORK_ERROR");
  });
});

describe("ApiError", () => {
  it("continua sendo a classe que o cliente lança", () => {
    expect(new ApiError("INTERNAL", "x", 500)).toBeInstanceOf(Error);
  });
});
