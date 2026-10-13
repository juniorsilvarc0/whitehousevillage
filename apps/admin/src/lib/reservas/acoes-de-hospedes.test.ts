import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  acrescentarHospede,
  aumentarHospedes,
  cadastrarEAcrescentar,
  procurarPorDocumento,
  retirarHospede,
} from "@/lib/reservas/acoes-de-hospedes";
import { valoresDoContato } from "@/components/contatos/campos-do-contato";

/**
 * O que cada gesto da rooming list manda ao fio — verbo, rota e corpo — e
 * quando ele recarrega a tela.
 *
 * Recarregar aqui não é detalhe: o `/full` grava uma linha de `pii_access_log`
 * por hóspede a cada leitura. Cada gesto que muda a lista recarrega **uma**
 * vez (o `revalidatePath` desenha a rota na própria resposta da action); a
 * busca por documento, que só lê a coleção mascarada, não recarrega nunca.
 */

const { apiFetch, apiList, revalidatePath, exigir, salvarContato, ErroDaApi } = vi.hoisted(() => {
  class ErroDaApi extends Error {
    constructor(
      readonly code: string,
      readonly details: Record<string, unknown> = {},
    ) {
      super(code);
    }
  }
  return {
    apiFetch: vi.fn(),
    apiList: vi.fn(),
    revalidatePath: vi.fn(),
    // A guarda devolve `null` (passa) ou a falha que recusou.
    exigir: vi.fn<(recurso: string, acao: string) => Promise<unknown>>(async () => null),
    salvarContato: vi.fn(),
    ErroDaApi,
  };
});

vi.mock("next/cache", () => ({ revalidatePath }));
vi.mock("@/lib/acoes/guarda", () => ({ exigir }));
vi.mock("@/lib/api/client", () => ({
  apiFetch,
  apiList,
  isApiError: (e: unknown) => e instanceof ErroDaApi,
}));
vi.mock("@/app/(app)/app/contatos/acoes", () => ({ salvarContato }));

const RESERVA = "9f1c1e2a-0d3b-4f6a-9a1e-2b7c5d8e0f11";
const JOANA = "3b2d6f7a-1c4e-4a8b-9d0f-5e6a7b8c9d01";
const ANA = "11111111-1111-4111-8111-111111111111";

/** Como a API devolve a pessoa acrescentada: cheia (documento, telefone). */
const JOANA_CHEIA = {
  contact_id: JOANA,
  name: "Joana Souza",
  doc_type: "cpf",
  doc_number: "52998224725",
  phone_e164: "+5585999990000",
  email: "joana@exemplo.com",
  is_lead_guest: false,
};

function chamada(n = -1): { rota: string; metodo?: string; corpo: unknown; temCorpo: boolean } {
  const [rota, init] = apiFetch.mock.calls.at(n) as [string, { method?: string; body?: unknown }];
  return { rota, metodo: init.method, corpo: init.body, temCorpo: "body" in init };
}

const DETALHE = `/app/reservas/${RESERVA}`;

beforeEach(() => {
  apiFetch.mockReset();
  apiList.mockReset();
  revalidatePath.mockReset();
  exigir.mockReset();
  exigir.mockResolvedValue(null);
  salvarContato.mockReset();
});

describe("acrescentar quem já tem ficha", () => {
  it("é POST /reservations/{id}/guests com só o contact_id — o titular não entra por aqui", async () => {
    apiFetch.mockResolvedValueOnce(JOANA_CHEIA);
    await acrescentarHospede(RESERVA, JOANA);
    expect(chamada()).toEqual({
      rota: `/reservations/${RESERVA}/guests`,
      metodo: "POST",
      corpo: { contact_id: JOANA },
      temCorpo: true,
    });
    expect(exigir).toHaveBeenCalledWith("reservations", "editar");
  });

  it("recarrega o detalhe uma vez — e devolve ao navegador só o nome e o id, não o documento", async () => {
    apiFetch.mockResolvedValueOnce(JOANA_CHEIA);
    const r = await acrescentarHospede(RESERVA, JOANA);
    expect(r).toEqual({ ok: true, data: { contact_id: JOANA, name: "Joana Souza" } });
    expect(JSON.stringify(r)).not.toContain("52998224725");
    expect(JSON.stringify(r)).not.toContain("+5585999990000");
    expect(revalidatePath).toHaveBeenCalledTimes(1);
    expect(revalidatePath).toHaveBeenCalledWith(DETALHE);
  });

  it("GUEST_LIST_FULL atravessa com o details intacto — é dele que sai a oferta de aumentar", async () => {
    apiFetch.mockRejectedValueOnce(new ErroDaApi("GUEST_LIST_FULL", { guests_count: 6, listed: 6, capacity: 8 }));
    const r = await acrescentarHospede(RESERVA, JOANA);
    expect(r).toMatchObject({ ok: false, code: "GUEST_LIST_FULL", details: { guests_count: 6, listed: 6, capacity: 8 } });
    // A lista na tela estava velha (alguém ocupou o último lugar): recarrega.
    expect(revalidatePath).toHaveBeenCalledWith(DETALHE);
  });

  it("recusa que não diz nada da tela não recarrega — seria uma leitura de dado pessoal à toa", async () => {
    apiFetch.mockRejectedValueOnce(new ErroDaApi("CONTACT_ANONYMIZED", { contact_id: JOANA }));
    const r = await acrescentarHospede(RESERVA, JOANA);
    expect(r).toMatchObject({ ok: false, code: "CONTACT_ANONYMIZED" });
    expect(revalidatePath).not.toHaveBeenCalled();
  });

  it("id que não é GUID nem sai do painel", async () => {
    const r = await acrescentarHospede(RESERVA, "../../contacts");
    expect(r.ok).toBe(false);
    expect(apiFetch).not.toHaveBeenCalled();
  });

  it("sem reservations:editar, nem tenta", async () => {
    exigir.mockResolvedValueOnce({ ok: false, code: "FORBIDDEN", message: "", details: {} });
    const r = await acrescentarHospede(RESERVA, JOANA);
    expect(r).toMatchObject({ ok: false, code: "FORBIDDEN" });
    expect(apiFetch).not.toHaveBeenCalled();
  });
});

describe("retirar", () => {
  it("é DELETE no vínculo, sem corpo, e recarrega", async () => {
    apiFetch.mockResolvedValueOnce(undefined);
    const r = await retirarHospede(RESERVA, JOANA);
    expect(chamada()).toMatchObject({ rota: `/reservations/${RESERVA}/guests/${JOANA}`, metodo: "DELETE", temCorpo: false });
    expect(r).toEqual({ ok: true, data: { jaSaiu: false } });
    expect(revalidatePath).toHaveBeenCalledWith(DETALHE);
  });

  it("404 é “já saiu”: volta como sucesso, e a tela recarrega para tirar a pessoa da vista", async () => {
    apiFetch.mockRejectedValueOnce(new ErroDaApi("NOT_FOUND", { contact_id: JOANA }));
    const r = await retirarHospede(RESERVA, JOANA);
    expect(r).toEqual({ ok: true, data: { jaSaiu: true } });
    expect(revalidatePath).toHaveBeenCalledWith(DETALHE);
  });

  it("o titular não sai: RESOURCE_IN_USE atravessa, e a tela recarrega para mostrar quem é o titular agora", async () => {
    apiFetch.mockRejectedValueOnce(new ErroDaApi("RESOURCE_IN_USE", { is_lead_guest: true }));
    const r = await retirarHospede(RESERVA, JOANA);
    expect(r).toMatchObject({ ok: false, code: "RESOURCE_IN_USE", details: { is_lead_guest: true } });
    expect(revalidatePath).toHaveBeenCalledWith(DETALHE);
  });
});

describe("procurar pelo documento", () => {
  it("manda o CPF só com dígitos, como é gravado — a pontuação digitada não vai", async () => {
    apiList.mockResolvedValueOnce({ data: [], meta: { page: 1, per_page: 10, total: 0, total_pages: 0 } });
    await procurarPorDocumento("cpf", "529.982.247-25");
    const [rota, init] = apiList.mock.calls[0] as [string, { query: Record<string, unknown> }];
    expect(rota).toBe("/contacts");
    expect(init.query).toMatchObject({ doc_number: "52998224725" });
    // Ficha anonimizada fica de fora, como a rota faz por padrão: ela não volta
    // a uma estadia.
    expect(init.query).not.toHaveProperty("include_anonymized");
    expect(exigir).toHaveBeenCalledWith("contacts", "ver");
  });

  it("devolve a linha mascarada, e só o que serve para escolher pelo id", async () => {
    apiList.mockResolvedValueOnce({
      data: [
        {
          id: JOANA,
          name: "Joana Souza",
          doc_type: "cpf",
          doc_number: "***.***.247-25",
          phone_e164: "+*********0000",
          email: "j***@exemplo.com",
          city: "Fortaleza",
        },
      ],
      meta: { page: 1, per_page: 10, total: 1, total_pages: 1 },
    });
    const r = await procurarPorDocumento("cpf", "52998224725");
    expect(r).toEqual({ ok: true, data: [{ id: JOANA, name: "Joana Souza", doc_type: "cpf", doc_number: "***.***.247-25" }] });
  });

  it("não recarrega a reserva: uma pergunta não pode gravar 24 linhas de pii_access_log", async () => {
    apiList.mockResolvedValueOnce({ data: [], meta: { page: 1, per_page: 10, total: 0, total_pages: 0 } });
    await procurarPorDocumento("cpf", "52998224725");
    expect(revalidatePath).not.toHaveBeenCalled();
  });

  it("CPF com dígito errado nem sai do painel — o erro volta no campo", async () => {
    const r = await procurarPorDocumento("cpf", "529.982.247-26");
    expect(r).toMatchObject({ ok: false, code: "VALIDATION_ERROR", details: { doc_number: expect.stringContaining("CPF") } });
    expect(apiList).not.toHaveBeenCalled();
  });

  it("passaporte vai em maiúsculas, como é gravado", async () => {
    apiList.mockResolvedValueOnce({ data: [], meta: { page: 1, per_page: 10, total: 0, total_pages: 0 } });
    await procurarPorDocumento("passaporte", "ab123456");
    const [, init] = apiList.mock.calls[0] as [string, { query: Record<string, unknown> }];
    expect(init.query).toMatchObject({ doc_number: "AB123456" });
  });
});

describe("cadastrar e acrescentar", () => {
  const VALORES = { ...valoresDoContato(null), name: "Joana Souza", doc_type: "cpf", doc_number: "52998224725" };

  it("o cadastro é o de sempre (salvarContato), e o POST de hóspede segue com o id da ficha nova", async () => {
    salvarContato.mockResolvedValueOnce({ ok: true, data: { id: JOANA, name: "Joana Souza" } });
    apiFetch.mockResolvedValueOnce(JOANA_CHEIA);
    const r = await cadastrarEAcrescentar(RESERVA, VALORES);
    expect(salvarContato).toHaveBeenCalledWith(null, VALORES);
    expect(chamada()).toMatchObject({ rota: `/reservations/${RESERVA}/guests`, metodo: "POST", corpo: { contact_id: JOANA } });
    expect(r).toEqual({ ok: true, data: { contact_id: JOANA, name: "Joana Souza" } });
  });

  it("CONTACT_DUPLICATE devolve quem é, e NÃO acrescenta sozinho — a tela pergunta antes", async () => {
    salvarContato.mockResolvedValueOnce({
      ok: false,
      duplicata: {
        campo: "phone_e164",
        contactId: ANA,
        contato: { id: ANA, name: "Ana Silva", doc_type: null, doc_number: null, anonymized_at: null },
      },
    });
    const r = await cadastrarEAcrescentar(RESERVA, VALORES);
    expect(r).toEqual({
      ok: false,
      duplicata: {
        contactId: ANA,
        campo: "phone_e164",
        pessoa: { id: ANA, name: "Ana Silva", doc_type: null, doc_number: null, anonimizada: false },
      },
    });
    expect(apiFetch).not.toHaveBeenCalled();
  });

  it("a ficha nasceu e a lista recusou: devolve quem foi cadastrada, para tentar de novo sem recadastrar", async () => {
    salvarContato.mockResolvedValueOnce({ ok: true, data: { id: JOANA, name: "Joana Souza" } });
    apiFetch.mockRejectedValueOnce(new ErroDaApi("GUEST_LIST_FULL", { guests_count: 4, listed: 4, capacity: 6 }));
    const r = await cadastrarEAcrescentar(RESERVA, VALORES);
    expect(r).toMatchObject({
      ok: false,
      cadastrada: { contact_id: JOANA, name: "Joana Souza" },
      falha: { code: "GUEST_LIST_FULL", details: { guests_count: 4, listed: 4, capacity: 6 } },
    });
  });

  it("sem reservations:editar não cadastra ninguém — a ficha nasceria sem ter onde entrar", async () => {
    exigir.mockResolvedValueOnce({ ok: false, code: "FORBIDDEN", message: "", details: {} });
    const r = await cadastrarEAcrescentar(RESERVA, VALORES);
    expect(r).toMatchObject({ ok: false, code: "FORBIDDEN" });
    expect(salvarContato).not.toHaveBeenCalled();
  });

  it("recusa do cadastro (campo) volta como está, com o details para o formulário", async () => {
    salvarContato.mockResolvedValueOnce({
      ok: false,
      code: "VALIDATION_ERROR",
      message: "",
      details: { doc_number: "CPF inválido." },
    });
    const r = await cadastrarEAcrescentar(RESERVA, VALORES);
    expect(r).toMatchObject({ ok: false, code: "VALIDATION_ERROR", details: { doc_number: "CPF inválido." } });
    expect(apiFetch).not.toHaveBeenCalled();
  });
});

describe("abrir lugar: aumentar o número de hóspedes", () => {
  it("PATCH na reserva com SÓ guests_count — chave a mais é 422, e o PATCH não mexe em preço", async () => {
    apiFetch.mockResolvedValueOnce({ id: RESERVA, guests_count: 7 });
    const r = await aumentarHospedes(RESERVA, 7);
    expect(chamada()).toEqual({ rota: `/reservations/${RESERVA}`, metodo: "PATCH", corpo: { guests_count: 7 }, temCorpo: true });
    expect(r).toEqual({ ok: true, data: { guests_count: 7, acrescentada: null } });
    expect(revalidatePath).toHaveBeenCalledWith(DETALHE);
  });

  it("com quem esperava lugar: PATCH e depois o POST, na mesma ida", async () => {
    apiFetch.mockResolvedValueOnce({ id: RESERVA, guests_count: 7 }).mockResolvedValueOnce(JOANA_CHEIA);
    const r = await aumentarHospedes(RESERVA, 7, JOANA);
    expect(chamada(0)).toMatchObject({ metodo: "PATCH", corpo: { guests_count: 7 } });
    expect(chamada(1)).toMatchObject({ rota: `/reservations/${RESERVA}/guests`, metodo: "POST", corpo: { contact_id: JOANA } });
    expect(r).toEqual({ ok: true, data: { guests_count: 7, acrescentada: { contact_id: JOANA, name: "Joana Souza" } } });
  });

  it("acima da capacidade: para no número, e ninguém é acrescentado", async () => {
    apiFetch.mockRejectedValueOnce(new ErroDaApi("CAPACITY_EXCEEDED", { capacity: 6, requested: 7 }));
    const r = await aumentarHospedes(RESERVA, 7, JOANA);
    expect(r).toMatchObject({ ok: false, code: "CAPACITY_EXCEEDED", etapa: "numero" });
    expect(apiFetch).toHaveBeenCalledTimes(1);
  });

  it("o número subiu e a pessoa não entrou: diz em que passo parou", async () => {
    apiFetch
      .mockResolvedValueOnce({ id: RESERVA, guests_count: 7 })
      .mockRejectedValueOnce(new ErroDaApi("CODE_IN_USE", { contact_id: JOANA, is_lead_guest: false }));
    const r = await aumentarHospedes(RESERVA, 7, JOANA);
    expect(r).toMatchObject({ ok: false, code: "CODE_IN_USE", etapa: "lista" });
  });

  it("número que não é inteiro positivo nem sai do painel", async () => {
    const r = await aumentarHospedes(RESERVA, 0);
    expect(r).toMatchObject({ ok: false, etapa: "numero" });
    expect(apiFetch).not.toHaveBeenCalled();
  });
});
