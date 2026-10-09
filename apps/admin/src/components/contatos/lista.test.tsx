import * as React from "react";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { FalhaDeContato, ResultadoDeContato } from "@/lib/contatos/api";
import type { Contato, ContatoNaLista } from "@/lib/contatos/tipos";

import { ListaDeContatos } from "./lista";

/**
 * **Editar pela lista abre a ficha, nunca a linha** (dívida D11).
 *
 * Desde o F2-23, `GET /contacts` devolve `ContatoNaLista`: documento, telefone
 * e e-mail mascarados, e sem `birth_date` nem `notes`. O "Editar" da lista
 * entregava essa linha ao formulário, e o salvamento é `PUT` com o formulário
 * inteiro. Dois efeitos, os dois em produção:
 *
 * 1. `422` em campos que o operador não tocou — a API recusa e-mail com `*`, e
 *    `+*********0000` não é E.164;
 * 2. **perda silenciosa**: num contato só com nome e anotação nada recusa, e o
 *    `PUT` grava a anotação vazia, porque a lista não a trouxe.
 *
 * O que estes casos exigem: o formulário **só monta depois que a ficha chega**
 * (`GET /contacts/{id}`, via `lerFichaDoContato`), e monta **com os valores
 * dela**. O controle negativo é passar a linha direto ao modal — com ela, o
 * primeiro e o segundo caso ficam vermelhos.
 */

const salvarContato = vi.fn();
const lerFichaDoContato = vi.fn();
const refresh = vi.fn();

vi.mock("@/app/(app)/app/contatos/acoes", () => ({
  salvarContato: (...args: unknown[]) => salvarContato(...args),
  lerFichaDoContato: (...args: unknown[]) => lerFichaDoContato(...args),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh, push: vi.fn(), replace: vi.fn() }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
  Toaster: () => null,
}));

// ── A pessoa, dos dois lados ───────────────────────────────────────────────

/** Como `GET /contacts` a devolve: máscara da OpenAPI ("Dado pessoal na
 *  resposta") e sem as chaves `birth_date` e `notes`. */
const FERNANDA_NA_LISTA: ContatoNaLista = {
  id: "c-fernanda",
  name: "Fernanda Lima",
  email: "f***@gmail.com",
  phone_e164: "+*********0000",
  doc_type: "cpf",
  doc_number: "***.***.247-25",
  city: "Fortaleza",
  state: "CE",
  lgpd_basis: "contrato",
  marketing_opt_in: false,
  consent_at: null,
  anonymized_at: null,
  created_at: "2026-01-10T12:00:00Z",
  updated_at: "2026-09-01T12:00:00Z",
};

/** Como `GET /contacts/{id}` a devolve: cheia. */
const FERNANDA_NA_FICHA: Contato = {
  ...FERNANDA_NA_LISTA,
  email: "fernanda.lima@gmail.com",
  phone_e164: "+5585999990000",
  doc_number: "52998224725",
  birth_date: "1988-04-12",
  notes: "Prefere o apartamento térreo.",
};

/** O caso da perda silenciosa: sem e-mail, telefone nem documento, nada na
 *  linha é recusado pela API — e a anotação, que a lista não traz, sumiria. */
const JOAO_NA_LISTA: ContatoNaLista = {
  id: "c-joao",
  name: "João Pedro",
  email: null,
  phone_e164: null,
  doc_type: null,
  doc_number: null,
  city: null,
  state: null,
  lgpd_basis: "legitimo_interesse",
  marketing_opt_in: false,
  consent_at: null,
  anonymized_at: null,
  created_at: "2026-02-01T12:00:00Z",
  updated_at: "2026-02-01T12:00:00Z",
};

const JOAO_NA_FICHA: Contato = {
  ...JOAO_NA_LISTA,
  birth_date: "1975-11-30",
  notes: "Indicado pelo Carlos. Quer a casa inteira no réveillon de 2027.",
};

// ── Bancada ────────────────────────────────────────────────────────────────

/** Promessa que o teste resolve quando quiser — é o que permite olhar a tela
 *  no intervalo entre o clique e a chegada da ficha. */
function adiada<T>() {
  let resolver!: (valor: T) => void;
  const promessa = new Promise<T>((r) => {
    resolver = r;
  });
  return { promessa, resolver };
}

function falha(code: FalhaDeContato["code"]): FalhaDeContato {
  return { ok: false, code, message: "", details: {} };
}

function desenhar(contatos: readonly ContatoNaLista[]) {
  return render(
    <ListaDeContatos contatos={contatos} permissoes={{ criar: true, editar: true }} temFiltro={false} />,
  );
}

function valor(rotulo: RegExp): string {
  return (screen.getByLabelText(rotulo) as HTMLInputElement | HTMLTextAreaElement).value;
}

/** Nenhum campo do formulário pode carregar máscara — é o que viraria `422` ou,
 *  pior, seria gravado por cima do valor verdadeiro. */
function camposComMascara(): string[] {
  const dialogo = screen.getByRole("dialog");
  return Array.from(dialogo.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>("input, textarea"))
    .map((campo) => campo.value)
    .filter((v) => v.includes("*"));
}

const formulario = () => document.getElementById("form-contato");

beforeEach(() => {
  salvarContato.mockReset();
  lerFichaDoContato.mockReset();
  refresh.mockReset();
});

// ── Os casos ───────────────────────────────────────────────────────────────

describe("Editar pela lista", () => {
  it("só monta o formulário depois que a ficha chega — e com os valores da ficha", async () => {
    const ficha = adiada<ResultadoDeContato<Contato>>();
    lerFichaDoContato.mockReturnValue(ficha.promessa);

    desenhar([FERNANDA_NA_LISTA]);
    fireEvent.click(screen.getByRole("button", { name: "Editar Fernanda Lima" }));

    // Quem é editado vai pelo id; a linha não sai da lista.
    expect(lerFichaDoContato).toHaveBeenCalledTimes(1);
    expect(lerFichaDoContato).toHaveBeenCalledWith("c-fernanda");

    // Enquanto a ficha não chega: o modal está aberto, diz o que está fazendo,
    // e NÃO existe formulário — nem campo vazio, nem campo com a máscara.
    const dialogo = await screen.findByRole("dialog");
    expect(within(dialogo).getByRole("status").textContent).toContain("Abrindo a ficha de Fernanda Lima");
    expect(formulario()).toBeNull();
    expect(screen.queryByLabelText(/^Telefone/)).toBeNull();
    expect(screen.queryByLabelText(/^Observações/)).toBeNull();
    expect((screen.getByRole("button", { name: "Salvar" }) as HTMLButtonElement).disabled).toBe(true);

    await act(async () => {
      ficha.resolver({ ok: true, data: FERNANDA_NA_FICHA });
    });

    // A ficha chegou: agora sim o formulário, preenchido com ELA.
    await waitFor(() => expect(formulario()).not.toBeNull());
    expect(valor(/^Nome/)).toBe("Fernanda Lima");
    expect(valor(/^Telefone/)).toBe("+5585999990000");
    expect(valor(/^E-mail/)).toBe("fernanda.lima@gmail.com");
    expect(valor(/^Número/)).toBe("52998224725");
    expect(valor(/^Nascimento/)).toBe("1988-04-12");
    expect(valor(/^Observações/)).toBe("Prefere o apartamento térreo.");
    expect(camposComMascara()).toEqual([]);
    expect(screen.queryByRole("status")).toBeNull();
    expect((screen.getByRole("button", { name: "Salvar" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("salvar depois de abrir pela lista manda a ficha inteira — a anotação não some", async () => {
    lerFichaDoContato.mockResolvedValue({ ok: true, data: JOAO_NA_FICHA });
    salvarContato.mockResolvedValue({ ok: true, data: JOAO_NA_FICHA });

    desenhar([JOAO_NA_LISTA]);
    fireEvent.click(screen.getByRole("button", { name: "Editar João Pedro" }));
    await waitFor(() => expect(formulario()).not.toBeNull());

    // O operador só quer corrigir a cidade.
    fireEvent.change(screen.getByLabelText(/^Cidade/), { target: { value: "Aquiraz" } });
    fireEvent.click(screen.getByRole("button", { name: "Salvar" }));

    await waitFor(() => expect(salvarContato).toHaveBeenCalledTimes(1));
    const [id, enviado] = salvarContato.mock.calls[0];
    expect(id).toBe("c-joao");
    // O que a lista não traz e o `PUT` gravaria vazio, em silêncio:
    expect(enviado.notes).toBe("Indicado pelo Carlos. Quer a casa inteira no réveillon de 2027.");
    expect(enviado.birth_date).toBe("1975-11-30");
    expect(enviado.city).toBe("Aquiraz");
  });

  it("falha ao buscar a ficha: não monta o formulário, diz o porquê e deixa tentar de novo", async () => {
    lerFichaDoContato
      .mockResolvedValueOnce(falha("NETWORK_ERROR"))
      .mockResolvedValueOnce({ ok: true, data: FERNANDA_NA_FICHA });

    desenhar([FERNANDA_NA_LISTA]);
    fireEvent.click(screen.getByRole("button", { name: "Editar Fernanda Lima" }));

    const aviso = await screen.findByRole("alert");
    expect(aviso.textContent).toContain("Não foi possível abrir a ficha de Fernanda Lima");
    // Pelo código, não pelo texto da API.
    expect(aviso.getAttribute("data-codigo")).toBe("NETWORK_ERROR");
    expect(formulario()).toBeNull();
    expect((screen.getByRole("button", { name: "Salvar" }) as HTMLButtonElement).disabled).toBe(true);
    expect(salvarContato).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: /Tentar de novo/ }));

    await waitFor(() => expect(formulario()).not.toBeNull());
    expect(lerFichaDoContato).toHaveBeenCalledTimes(2);
    expect(valor(/^Telefone/)).toBe("+5585999990000");
  });

  it("ficha que não existe mais: avisa, sem formulário e sem oferecer tentar de novo", async () => {
    lerFichaDoContato.mockResolvedValue(falha("NOT_FOUND"));

    desenhar([FERNANDA_NA_LISTA]);
    fireEvent.click(screen.getByRole("button", { name: "Editar Fernanda Lima" }));

    expect((await screen.findByRole("alert")).getAttribute("data-codigo")).toBe("NOT_FOUND");
    expect(formulario()).toBeNull();
    expect(screen.queryByRole("button", { name: /Tentar de novo/ })).toBeNull();
  });

  it("ficha anonimizada depois de a lista ser desenhada: não abre formulário", async () => {
    // A lista esconde "Editar" de quem já está anonimizado — mas a lista pode
    // ser de minutos atrás, e a ficha lida agora é a verdade.
    lerFichaDoContato.mockResolvedValue({
      ok: true,
      data: { ...FERNANDA_NA_FICHA, anonymized_at: "2026-10-08T10:00:00Z" },
    });

    desenhar([FERNANDA_NA_LISTA]);
    fireEvent.click(screen.getByRole("button", { name: "Editar Fernanda Lima" }));

    expect((await screen.findByRole("alert")).getAttribute("data-codigo")).toBe("CONTACT_ANONYMIZED");
    expect(formulario()).toBeNull();
  });

  it("a ficha que chega depois de fechar e abrir outro contato é descartada", async () => {
    // Sem esta guarda, a resposta atrasada da primeira abertura montaria o
    // formulário com os dados de uma pessoa e o título — e o `id` — de outra.
    const daFernanda = adiada<ResultadoDeContato<Contato>>();
    lerFichaDoContato.mockImplementation((id: string) =>
      id === "c-fernanda" ? daFernanda.promessa : Promise.resolve({ ok: true, data: JOAO_NA_FICHA }),
    );

    desenhar([FERNANDA_NA_LISTA, JOAO_NA_LISTA]);
    fireEvent.click(screen.getByRole("button", { name: "Editar Fernanda Lima" }));
    await screen.findByRole("status");
    fireEvent.click(screen.getByRole("button", { name: "Cancelar" }));

    fireEvent.click(screen.getByRole("button", { name: "Editar João Pedro" }));
    await waitFor(() => expect(valor(/^Observações/)).toBe(JOAO_NA_FICHA.notes));

    await act(async () => {
      daFernanda.resolver({ ok: true, data: FERNANDA_NA_FICHA });
    });

    expect(valor(/^Nome/)).toBe("João Pedro");
    expect(valor(/^Observações/)).toBe(JOAO_NA_FICHA.notes);
  });
});

describe("a linha mascarada na tela", () => {
  it("aparece como a API devolveu, e o telefone mascarado não vira link de ligação", () => {
    const { container } = desenhar([FERNANDA_NA_LISTA]);

    expect(screen.getByText("+*********0000")).toBeTruthy();
    expect(screen.getByText("***.***.247-25")).toBeTruthy();
    expect(container.querySelector('a[href^="tel:"]')).toBeNull();
    // Desenhar a lista não lê ficha nenhuma: a leitura com rastro é do gesto.
    expect(lerFichaDoContato).not.toHaveBeenCalled();
  });

  it("“Novo contato” abre o formulário em branco, sem buscar ficha", async () => {
    desenhar([FERNANDA_NA_LISTA]);
    fireEvent.click(screen.getByRole("button", { name: /Novo contato/ }));

    await waitFor(() => expect(formulario()).not.toBeNull());
    expect(valor(/^Nome/)).toBe("");
    expect(lerFichaDoContato).not.toHaveBeenCalled();
  });
});
