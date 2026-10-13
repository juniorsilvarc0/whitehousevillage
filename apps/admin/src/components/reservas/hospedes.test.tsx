import * as React from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { HospedeDaReserva } from "@/lib/reservas/tipos";

import { SecaoDeHospedes, type PermissoesDaLista, type ReservaDaLista } from "./hospedes";

/**
 * A seção "Hóspedes" — a rooming list (F2-22) na tela da reserva.
 *
 * O que se mede:
 *
 * 1. **Os botões aparecem só quando cabem.** "Acrescentar" com a lista
 *    editável (`guests_editable`), `reservations:editar` e lugar sobrando;
 *    "Retirar" só nos acompanhantes — o titular nunca tem o botão.
 * 2. **O fluxo do balcão.** Procura pelo documento; achou, acrescenta pelo id;
 *    não achou, cadastra com o formulário de contato e acrescenta; o cadastro
 *    colidiu, segue com o `details.contact_id` — depois de mostrar quem é.
 * 3. **As recusas viram caminho.** `GUEST_LIST_FULL` com o vendido abaixo da
 *    capacidade oferece aumentar o número; o `404` do retirar é "já saiu".
 * 4. **Dado pessoal não vaza para o navegador.** O CPF digitado não vai para
 *    `localStorage` nem para o console.
 */

const acoes = vi.hoisted(() => ({
  procurarPorDocumento: vi.fn(),
  acrescentarHospede: vi.fn(),
  cadastrarEAcrescentar: vi.fn(),
  retirarHospede: vi.fn(),
  aumentarHospedes: vi.fn(),
}));

vi.mock("@/lib/reservas/acoes-de-hospedes", () => acoes);

vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh: vi.fn(), push: vi.fn(), replace: vi.fn() }),
}));

const RESERVA: ReservaDaLista = { id: "r-1", code: "WH-2026-0042", status: "confirmed", guests_count: 4 };

const CARLOS: HospedeDaReserva = {
  contact_id: "c-carlos",
  name: "Carlos Andrade",
  doc_type: "cpf",
  doc_number: "11144477735",
  phone_e164: "+5585988887777",
  email: "carlos@exemplo.com",
  is_lead_guest: true,
};

const BIA: HospedeDaReserva = {
  contact_id: "c-bia",
  name: "Beatriz Andrade",
  doc_type: "passaporte",
  doc_number: "AB123456",
  phone_e164: null,
  email: null,
  is_lead_guest: false,
};

const TUDO: PermissoesDaLista = { editar: true, procurarPessoas: true, cadastrarPessoas: true };

/** CPF válido, só para o balcão digitar. */
const CPF_DA_JOANA = "529.982.247-25";

function desenhar({
  reserva = RESERVA,
  hospedes = [CARLOS, BIA],
  editavel = true,
  permissoes = TUDO,
  capacidade = 6,
}: {
  reserva?: ReservaDaLista;
  hospedes?: HospedeDaReserva[];
  editavel?: boolean;
  permissoes?: PermissoesDaLista;
  capacidade?: number | null;
} = {}) {
  return render(
    <SecaoDeHospedes
      reserva={reserva}
      hospedes={hospedes}
      editavel={editavel}
      permissoes={permissoes}
      capacidade={capacidade}
    />,
  );
}

function botaoAcrescentar() {
  return screen.queryByRole("button", { name: /Acrescentar hóspede/ });
}

function linhaDe(nome: string): HTMLElement {
  const lista = screen.getByRole("list", { name: "Lista de hóspedes" });
  const item = within(lista)
    .getAllByRole("listitem")
    .find((li) => li.textContent?.includes(nome));
  if (!item) throw new Error(`sem linha para ${nome}`);
  return item;
}

async function procurar(numero = CPF_DA_JOANA) {
  fireEvent.click(botaoAcrescentar()!);
  fireEvent.change(await screen.findByLabelText(/^Número/), { target: { value: numero } });
  fireEvent.click(screen.getByRole("button", { name: /^Procurar$/ }));
}

beforeEach(() => {
  for (const fn of Object.values(acoes)) fn.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

// ── 1. Os botões ───────────────────────────────────────────────────────────

describe("a lista na tela", () => {
  it("mostra nome, documento cheio, telefone, o selo de titular e o contador contra o vendido", () => {
    desenhar();
    expect(screen.getByText(/2/, { selector: "[data-contador] span" })).toBeTruthy();
    expect(document.querySelector("[data-contador]")?.textContent?.replace(/\s+/g, " ")).toContain(
      "2 de 4 com nome na lista",
    );

    const carlos = linhaDe("Carlos Andrade");
    // O documento sai cheio e pontuado — é com ele que a recepção confere quem entra.
    expect(carlos.textContent).toContain("CPF 111.444.777-35");
    expect(carlos.textContent).toContain("+55 (85) 98888-7777");
    expect(within(carlos).getByText("Titular")).toBeTruthy();

    const bia = linhaDe("Beatriz Andrade");
    expect(bia.textContent).toContain("Passaporte AB123456");
    expect(within(bia).queryByText("Titular")).toBeNull();
  });

  it("acompanhante sem documento na ficha diz isso, em vez de uma linha em branco", () => {
    desenhar({ hospedes: [CARLOS, { ...BIA, doc_type: null, doc_number: null }] });
    expect(linhaDe("Beatriz Andrade").textContent).toContain("Sem documento na ficha");
  });
});

describe("Acrescentar hóspede — só quando cabe", () => {
  it("aparece com a lista editável, a permissão e lugar sobrando", () => {
    desenhar();
    expect(botaoAcrescentar()).toBeTruthy();
  });

  it("some com a lista fechada (guests_editable falso)", () => {
    desenhar({ editavel: false, reserva: { ...RESERVA, status: "checked_out" } });
    expect(botaoAcrescentar()).toBeNull();
    // E diz por quê, com o estado em português.
    expect(screen.getByText(/Estadia cumprida/)).toBeTruthy();
  });

  it("some sem reservations:editar", () => {
    desenhar({ permissoes: { ...TUDO, editar: false } });
    expect(botaoAcrescentar()).toBeNull();
  });

  it("some com a lista completa — e no lugar aparece o caminho: aumentar o número", () => {
    desenhar({ reserva: { ...RESERVA, guests_count: 2 } });
    expect(botaoAcrescentar()).toBeNull();
    expect(screen.getByRole("button", { name: /Aumentar para 3 hóspedes/ })).toBeTruthy();
  });

  it("com o produto no máximo, não oferece aumentar: diz para retirar alguém", () => {
    desenhar({ reserva: { ...RESERVA, guests_count: 2 }, capacidade: 2 });
    expect(screen.queryByRole("button", { name: /Aumentar para/ })).toBeNull();
    expect(screen.getByText(/retire antes um nome/)).toBeTruthy();
  });
});

describe("Retirar — só acompanhante", () => {
  it("o titular não tem o botão; o acompanhante tem", () => {
    desenhar();
    expect(within(linhaDe("Carlos Andrade")).queryByRole("button", { name: /Retirar/ })).toBeNull();
    expect(within(linhaDe("Beatriz Andrade")).getByRole("button", { name: /Retirar/ })).toBeTruthy();
  });

  it("ninguém tem o botão com a lista fechada ou sem permissão", () => {
    const { unmount } = desenhar({ editavel: false });
    expect(screen.queryAllByRole("button", { name: /Retirar/ })).toHaveLength(0);
    unmount();
    desenhar({ permissoes: { ...TUDO, editar: false } });
    expect(screen.queryAllByRole("button", { name: /Retirar/ })).toHaveLength(0);
  });

  it("retirar pede confirmação e chama a API com o contact_id do acompanhante", async () => {
    acoes.retirarHospede.mockResolvedValueOnce({ ok: true, data: { jaSaiu: false } });
    desenhar();
    fireEvent.click(within(linhaDe("Beatriz Andrade")).getByRole("button", { name: /Retirar/ }));
    expect(acoes.retirarHospede).not.toHaveBeenCalled();

    const dialogo = await screen.findByRole("dialog");
    fireEvent.click(within(dialogo).getByRole("button", { name: "Retirar" }));
    await waitFor(() => expect(acoes.retirarHospede).toHaveBeenCalledWith("r-1", "c-bia"));
    expect(await screen.findByText("Beatriz Andrade saiu da lista.")).toBeTruthy();
  });

  it("404 no retirar é “já saiu”, não um erro", async () => {
    // A action traduz o 404 (`retirarHospede` → `jaSaiu: true`); a tela diz o
    // que aconteceu, sem vermelho: o colega retirou primeiro, ou foi o segundo
    // clique.
    acoes.retirarHospede.mockResolvedValueOnce({ ok: true, data: { jaSaiu: true } });
    desenhar();
    fireEvent.click(within(linhaDe("Beatriz Andrade")).getByRole("button", { name: /Retirar/ }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Retirar" }));

    expect(await screen.findByText("Beatriz Andrade já tinha saído da lista.")).toBeTruthy();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.queryByText(/Não encontramos/)).toBeNull();
  });

  it("o titular que virou titular no meio do caminho: a recusa diz o porquê, no diálogo", async () => {
    acoes.retirarHospede.mockResolvedValueOnce({
      ok: false,
      code: "RESOURCE_IN_USE",
      message: "TEXTO CRU",
      details: { is_lead_guest: true },
    });
    desenhar();
    fireEvent.click(within(linhaDe("Beatriz Andrade")).getByRole("button", { name: /Retirar/ }));
    const dialogo = await screen.findByRole("dialog");
    fireEvent.click(within(dialogo).getByRole("button", { name: "Retirar" }));
    expect(await within(dialogo).findByText(/é o titular da reserva e não sai da lista/)).toBeTruthy();
    expect(within(dialogo).queryByText(/TEXTO CRU/)).toBeNull();
  });
});

// ── 2. O fluxo do balcão ───────────────────────────────────────────────────

describe("achou pelo documento", () => {
  it("mostra a máscara, escolhe pelo id e acrescenta", async () => {
    acoes.procurarPorDocumento.mockResolvedValueOnce({
      ok: true,
      data: [{ id: "c-joana", name: "Joana Souza", doc_type: "cpf", doc_number: "***.***.247-25" }],
    });
    acoes.acrescentarHospede.mockResolvedValueOnce({ ok: true, data: { contact_id: "c-joana", name: "Joana Souza" } });
    desenhar();

    await procurar();
    expect(acoes.procurarPorDocumento).toHaveBeenCalledWith("cpf", CPF_DA_JOANA);

    // A linha da busca vem mascarada, e é assim que aparece.
    const encontrados = await screen.findByRole("list", { name: "Pessoas encontradas" });
    expect(encontrados.textContent).toContain("***.***.247-25");

    fireEvent.click(within(encontrados).getByRole("button", { name: /Acrescentar Joana/ }));
    await waitFor(() => expect(acoes.acrescentarHospede).toHaveBeenCalledWith("r-1", "c-joana"));
    expect(await screen.findByText("Joana Souza entrou na lista.")).toBeTruthy();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("quem a busca acha e já está na lista não ganha o botão", async () => {
    acoes.procurarPorDocumento.mockResolvedValueOnce({
      ok: true,
      data: [{ id: "c-carlos", name: "Carlos Andrade", doc_type: "cpf", doc_number: "***.***.777-35" }],
    });
    desenhar();
    await procurar("111.444.777-35");
    const encontrados = await screen.findByRole("list", { name: "Pessoas encontradas" });
    expect(within(encontrados).getByText("Titular desta reserva")).toBeTruthy();
    expect(within(encontrados).queryByRole("button", { name: /Acrescentar/ })).toBeNull();
  });

  it("CPF com dígito errado nem sai: o erro aparece no campo", async () => {
    desenhar();
    await procurar("529.982.247-26");
    expect(await screen.findByText(/CPF inválido/)).toBeTruthy();
    expect(acoes.procurarPorDocumento).not.toHaveBeenCalled();
  });

  it("sem acesso a Contatos, o diálogo diz o que pedir em vez de uma busca que daria 403", async () => {
    desenhar({ permissoes: { ...TUDO, procurarPessoas: false } });
    fireEvent.click(botaoAcrescentar()!);
    expect(await screen.findByText(/não tem acesso a/)).toBeTruthy();
    expect(screen.queryByLabelText(/^Número/)).toBeNull();
  });
});

describe("não achou: cadastrar pessoa", () => {
  beforeEach(() => {
    acoes.procurarPorDocumento.mockResolvedValue({ ok: true, data: [] });
  });

  it("abre o formulário de contato com o documento já preenchido, e cadastra e acrescenta numa ida", async () => {
    acoes.cadastrarEAcrescentar.mockResolvedValueOnce({ ok: true, data: { contact_id: "c-nova", name: "Joana Souza" } });
    desenhar();
    await procurar();

    expect(await screen.findByText(/Ninguém com o CPF/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Cadastrar pessoa/ }));

    // O formulário é o do cadastro de contatos (mesmos campos, mesma validação).
    expect((screen.getByLabelText(/^Número/) as HTMLInputElement).value).toBe(CPF_DA_JOANA);
    expect((screen.getByLabelText(/^Documento/) as HTMLSelectElement).value).toBe("cpf");
    expect(screen.getByLabelText(/Por que podemos guardar esta ficha/)).toBeTruthy();

    fireEvent.change(screen.getByLabelText(/^Nome/), { target: { value: "Joana Souza" } });
    fireEvent.click(screen.getByRole("button", { name: /Cadastrar e acrescentar/ }));

    await waitFor(() => expect(acoes.cadastrarEAcrescentar).toHaveBeenCalledTimes(1));
    const [reservaId, valores] = acoes.cadastrarEAcrescentar.mock.calls[0];
    expect(reservaId).toBe("r-1");
    expect(valores).toMatchObject({ name: "Joana Souza", doc_type: "cpf", doc_number: CPF_DA_JOANA });
    expect(await screen.findByText("Ficha criada, e Joana Souza entrou na lista.")).toBeTruthy();
  });

  it("sem contacts:criar, não oferece o cadastro — e diz o caminho", async () => {
    desenhar({ permissoes: { ...TUDO, cadastrarPessoas: false } });
    await procurar();
    expect(await screen.findByText(/O seu perfil não cadastra pessoas/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Cadastrar pessoa/ })).toBeNull();
  });

  it("a ficha nasceu e a lista recusou: não recadastra — tenta com o id da ficha nova", async () => {
    acoes.cadastrarEAcrescentar.mockResolvedValueOnce({
      ok: false,
      cadastrada: { contact_id: "c-nova", name: "Joana Souza" },
      falha: { ok: false, code: "GUEST_LIST_FULL", message: "", details: { guests_count: 4, listed: 4, capacity: 6 } },
    });
    acoes.aumentarHospedes.mockResolvedValueOnce({
      ok: true,
      data: { guests_count: 5, acrescentada: { contact_id: "c-nova", name: "Joana Souza" } },
    });
    desenhar();
    await procurar();
    fireEvent.click(await screen.findByRole("button", { name: /Cadastrar pessoa/ }));
    fireEvent.change(screen.getByLabelText(/^Nome/), { target: { value: "Joana Souza" } });
    fireEvent.click(screen.getByRole("button", { name: /Cadastrar e acrescentar/ }));

    expect(await screen.findByText(/A ficha de Joana Souza foi criada/)).toBeTruthy();
    // O formulário saiu: mandar de novo criaria uma segunda ficha (ou um 409).
    expect(screen.queryByRole("button", { name: /Cadastrar e acrescentar/ })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /Aumentar para 5 hóspedes e acrescentar Joana/ }));
    await waitFor(() => expect(acoes.aumentarHospedes).toHaveBeenCalledWith("r-1", 5, "c-nova"));
    expect(acoes.cadastrarEAcrescentar).toHaveBeenCalledTimes(1);
  });
});

describe("o cadastro colidiu (CONTACT_DUPLICATE)", () => {
  async function ateADuplicata() {
    acoes.procurarPorDocumento.mockResolvedValueOnce({ ok: true, data: [] });
    acoes.cadastrarEAcrescentar.mockResolvedValueOnce({
      ok: false,
      duplicata: {
        contactId: "c-ana",
        campo: "phone_e164",
        pessoa: { id: "c-ana", name: "Ana Silva", doc_type: "cpf", doc_number: "***.***.777-35", anonimizada: false },
      },
    });
    desenhar();
    await procurar();
    fireEvent.click(await screen.findByRole("button", { name: /Cadastrar pessoa/ }));
    fireEvent.change(screen.getByLabelText(/^Nome/), { target: { value: "Joana Souza" } });
    fireEvent.change(screen.getByLabelText(/^Telefone/), { target: { value: "+5585999990000" } });
    fireEvent.click(screen.getByRole("button", { name: /Cadastrar e acrescentar/ }));
    return screen.findByText("Este telefone já é de Ana Silva");
  }

  it("mostra quem já tem o telefone e segue com o details.contact_id — depois do toque", async () => {
    acoes.acrescentarHospede.mockResolvedValueOnce({ ok: true, data: { contact_id: "c-ana", name: "Ana Silva" } });
    await ateADuplicata();
    // Seguir sozinho poria na lista quem tem o número, não quem está no balcão.
    expect(acoes.acrescentarHospede).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: /Acrescentar Ana/ }));
    await waitFor(() => expect(acoes.acrescentarHospede).toHaveBeenCalledWith("r-1", "c-ana"));
    expect(await screen.findByText("Ana Silva entrou na lista.")).toBeTruthy();
  });

  it("“não é essa pessoa” volta ao formulário com o que foi digitado, e o campo marcado", async () => {
    await ateADuplicata();
    fireEvent.click(screen.getByRole("button", { name: /Não é essa pessoa/ }));
    const telefone = (await screen.findByLabelText(/^Telefone/)) as HTMLInputElement;
    expect(telefone.value).toBe("+5585999990000");
    expect((screen.getByLabelText(/^Nome/) as HTMLInputElement).value).toBe("Joana Souza");
    expect(await screen.findByText("Já pertence a outro cadastro.")).toBeTruthy();
    expect(acoes.acrescentarHospede).not.toHaveBeenCalled();
  });
});

// ── 3. As recusas ──────────────────────────────────────────────────────────

describe("GUEST_LIST_FULL", () => {
  async function acrescentarJoanaComRecusa(details: Record<string, unknown>) {
    acoes.procurarPorDocumento.mockResolvedValueOnce({
      ok: true,
      data: [{ id: "c-joana", name: "Joana Souza", doc_type: "cpf", doc_number: "***.***.247-25" }],
    });
    acoes.acrescentarHospede.mockResolvedValueOnce({ ok: false, code: "GUEST_LIST_FULL", message: "", details });
    desenhar();
    await procurar();
    fireEvent.click(await screen.findByRole("button", { name: /Acrescentar Joana/ }));
    return screen.findByText(/A lista já tem/);
  }

  it("com o vendido abaixo da capacidade, oferece aumentar e acrescentar — numa ida", async () => {
    acoes.aumentarHospedes.mockResolvedValueOnce({
      ok: true,
      data: { guests_count: 5, acrescentada: { contact_id: "c-joana", name: "Joana Souza" } },
    });
    await acrescentarJoanaComRecusa({ guests_count: 4, listed: 4, capacity: 6 });

    const oferta = screen.getByRole("button", { name: /Aumentar para 5 hóspedes e acrescentar Joana/ });
    fireEvent.click(oferta);
    await waitFor(() => expect(acoes.aumentarHospedes).toHaveBeenCalledWith("r-1", 5, "c-joana"));
    expect(await screen.findByText("A reserva agora é para 5 hóspedes, e Joana Souza entrou na lista.")).toBeTruthy();
  });

  it("com o produto no máximo, não oferece aumentar — diz para retirar alguém", async () => {
    await acrescentarJoanaComRecusa({ guests_count: 6, listed: 6, capacity: 6 });
    expect(screen.queryByRole("button", { name: /Aumentar para/ })).toBeNull();
    expect(screen.getByText(/retire antes alguém/)).toBeTruthy();
  });

  it("o número subiu e a pessoa não entrou: a tela diz que o número mudou", async () => {
    acoes.aumentarHospedes.mockResolvedValueOnce({
      ok: false,
      code: "CODE_IN_USE",
      message: "",
      details: { is_lead_guest: false },
      etapa: "lista",
    });
    await acrescentarJoanaComRecusa({ guests_count: 4, listed: 4, capacity: 6 });
    fireEvent.click(screen.getByRole("button", { name: /Aumentar para 5 hóspedes/ }));
    expect(await screen.findByText(/A reserva agora é para 5 hóspedes, mas Joana Souza ainda não está na lista/)).toBeTruthy();
    expect(screen.getByText(/Joana Souza já está na lista/)).toBeTruthy();
  });
});

describe("as outras recusas do acrescentar, pelo código", () => {
  async function recusar(code: string, details: Record<string, unknown> = {}) {
    acoes.procurarPorDocumento.mockResolvedValueOnce({
      ok: true,
      data: [{ id: "c-joana", name: "Joana Souza", doc_type: "cpf", doc_number: "***.***.247-25" }],
    });
    acoes.acrescentarHospede.mockResolvedValueOnce({ ok: false, code, message: "TEXTO CRU DA API", details });
    desenhar();
    await procurar();
    fireEvent.click(await screen.findByRole("button", { name: /Acrescentar Joana/ }));
    return (await screen.findByText(/./, { selector: "[data-codigo] p" })).closest("[data-codigo]")!;
  }

  it("CODE_IN_USE: a pessoa já está na lista", async () => {
    const aviso = await recusar("CODE_IN_USE", { contact_id: "c-joana", is_lead_guest: false });
    expect(aviso.textContent).toContain("Joana Souza já está na lista");
  });

  it("CONTACT_ANONYMIZED: explica a LGPD e a saída", async () => {
    const aviso = await recusar("CONTACT_ANONYMIZED", { contact_id: "c-joana" });
    expect(aviso.textContent).toContain("apagados a pedido dela");
  });

  it("INVALID_STATE_TRANSITION: a lista já não se edita, com o estado em português", async () => {
    const aviso = await recusar("INVALID_STATE_TRANSITION", { status: "checked_out", allowed: ["hold", "confirmed", "checked_in"] });
    expect(aviso.textContent).toContain("Estadia cumprida");
  });

  it("VALIDATION_ERROR em contact_id: manda procurar de novo", async () => {
    const aviso = await recusar("VALIDATION_ERROR", { contact_id: "contato não encontrado nesta propriedade." });
    expect(aviso.textContent).toContain("Procure a pessoa de novo");
    expect(aviso.textContent).not.toContain("TEXTO CRU");
  });
});

// ── 4. Dado pessoal ────────────────────────────────────────────────────────

/** Um armazenamento que só anota o que recebeu. Fica no lugar do de verdade
 *  porque o Node 25 traz um `localStorage` global sem métodos, que esconde o do
 *  jsdom: sem isto, uma gravação de CPF quebraria a tela no teste — e um
 *  `try/catch` em volta dela passaria em silêncio. */
function armazenamentoQueAnota() {
  const dados = new Map<string, string>();
  return {
    setItem: vi.fn((chave: string, valor: string) => void dados.set(chave, String(valor))),
    getItem: vi.fn((chave: string) => dados.get(chave) ?? null),
    removeItem: vi.fn((chave: string) => void dados.delete(chave)),
    clear: vi.fn(() => dados.clear()),
    key: vi.fn(() => null),
    get length() {
      return dados.size;
    },
  };
}

describe("o CPF digitado não sai da tela", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("nada em localStorage nem sessionStorage, nada no console, e o campo fora do autocompletar", async () => {
    const local = armazenamentoQueAnota();
    const sessao = armazenamentoQueAnota();
    vi.stubGlobal("localStorage", local);
    vi.stubGlobal("sessionStorage", sessao);
    const consoles = (["log", "info", "warn", "error", "debug"] as const).map((m) =>
      vi.spyOn(console, m).mockImplementation(() => {}),
    );
    acoes.procurarPorDocumento.mockResolvedValueOnce({ ok: true, data: [] });
    acoes.cadastrarEAcrescentar.mockResolvedValueOnce({ ok: true, data: { contact_id: "c-nova", name: "Joana Souza" } });

    desenhar();
    await procurar();
    expect((screen.getByLabelText(/^Número/) as HTMLInputElement).getAttribute("autocomplete")).toBe("off");
    fireEvent.click(await screen.findByRole("button", { name: /Cadastrar pessoa/ }));
    expect((screen.getByLabelText(/^Número/) as HTMLInputElement).getAttribute("autocomplete")).toBe("off");
    fireEvent.change(screen.getByLabelText(/^Nome/), { target: { value: "Joana Souza" } });
    fireEvent.click(screen.getByRole("button", { name: /Cadastrar e acrescentar/ }));
    await screen.findByText(/entrou na lista/);

    const digitos = "52998224725";
    const vazou = (args: unknown[]) => args.some((a) => String(a).replace(/\D/g, "").includes(digitos));
    expect(local.setItem.mock.calls.some(vazou)).toBe(false);
    expect(sessao.setItem.mock.calls.some(vazou)).toBe(false);
    for (const espiao of consoles) expect(espiao.mock.calls.some(vazou)).toBe(false);
  });
});
