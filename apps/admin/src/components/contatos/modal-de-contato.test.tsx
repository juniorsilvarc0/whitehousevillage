import * as React from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useControleDeModal } from "@/components/layout/controle-de-modal";
import type { Contato } from "@/lib/contatos/tipos";

import { ModalDeContato } from "./modal-de-contato";

/**
 * **O momento mais importante do cadastro: o telefone já é de alguém.**
 *
 * Quem digita um número que já existe não cometeu um engano — ele quer chegar
 * naquela pessoa e não sabia que ela estava lá. A resposta certa é o caminho até
 * a ficha; a resposta errada, e a que a maioria das telas de cadastro dá, é um
 * "409" seco que manda o operador se virar.
 *
 * Estes casos medem exatamente isso: a recusa vira porta, não parede.
 */

const salvarContato = vi.fn();
const refresh = vi.fn();

vi.mock("@/app/(app)/app/contatos/acoes", () => ({
  salvarContato: (...args: unknown[]) => salvarContato(...args),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh, push: vi.fn(), replace: vi.fn() }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
  Toaster: () => null,
}));

const ANA: Contato = {
  id: "c-ana",
  name: "Ana Silva",
  email: "ana@exemplo.com",
  phone_e164: "+5585999990000",
  doc_type: null,
  doc_number: null,
  birth_date: null,
  city: "Fortaleza",
  state: "CE",
  notes: null,
  lgpd_basis: "contrato",
  marketing_opt_in: false,
  consent_at: null,
  anonymized_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

/** Casca mínima: o modal é imperativo (`controle.abrir`), então o teste precisa
 *  de alguém que o abra — como a tela de verdade faz no clique de "Novo". */
function Bancada() {
  const controle = useControleDeModal<Contato | null>();
  return (
    <>
      <button type="button" onClick={() => controle.abrir(null)}>
        Novo
      </button>
      <ModalDeContato controle={controle.ref} />
    </>
  );
}

function abrirEPreencher(nome: string, telefone: string) {
  fireEvent.click(screen.getByRole("button", { name: "Novo" }));
  fireEvent.change(screen.getByLabelText(/^Nome/), { target: { value: nome } });
  fireEvent.change(screen.getByLabelText(/^Telefone/), { target: { value: telefone } });
  fireEvent.click(screen.getByRole("button", { name: "Salvar" }));
}

beforeEach(() => {
  salvarContato.mockReset();
  refresh.mockReset();
});

describe("telefone que já é de alguém", () => {
  beforeEach(() => {
    salvarContato.mockResolvedValue({
      ok: false,
      duplicata: { campo: "phone_e164", contactId: "c-ana", contato: ANA },
    });
  });

  it("mostra quem é e oferece o caminho até a ficha dela", async () => {
    render(<Bancada />);
    abrirEPreencher("Ana S.", "+5585999990000");

    // O nome da pessoa é o que transforma a recusa em decisão: sem ele, o
    // operador só sabe que "alguém" tem o número e não tem como conferir se é a
    // mesma pessoa que está no telefone com ele.
    expect(await screen.findByText("Ana Silva já está cadastrada")).toBeTruthy();

    const link = screen.getByRole("link", { name: /Abrir a ficha de Ana/ });
    expect(link.getAttribute("href")).toBe("/app/contatos/c-ana");
  });

  it("o aviso fica na tela — não é um toast que some", async () => {
    // O operador precisa poder ler, decidir e clicar. E continuar vendo o que
    // digitou, caso conclua que é outra pessoa mesmo.
    render(<Bancada />);
    abrirEPreencher("Ana S.", "+5585999990000");

    await screen.findByText("Ana Silva já está cadastrada");
    expect((screen.getByLabelText(/^Telefone/) as HTMLInputElement).value).toBe("+5585999990000");
    expect(screen.getByRole("dialog")).toBeTruthy();
  });

  it("marca o campo que colidiu, não o formulário inteiro", async () => {
    render(<Bancada />);
    abrirEPreencher("Ana S.", "+5585999990000");

    await screen.findByText("Já pertence a outro contato.");
    expect(screen.getByLabelText(/^Telefone/).getAttribute("aria-invalid")).toBe("true");
    expect(screen.getByLabelText(/^Nome/).getAttribute("aria-invalid")).toBeNull();
  });

  it("“não é essa pessoa” devolve o foco ao telefone e limpa o aviso", async () => {
    // A segunda saída: quem digitou o número errado corrige e continua. Sem
    // ela, o único caminho seria fechar o modal e recomeçar o cadastro.
    render(<Bancada />);
    abrirEPreencher("Ana S.", "+5585999990000");

    fireEvent.click(await screen.findByRole("button", { name: /Não é essa pessoa/ }));

    await waitFor(() => {
      expect(screen.queryByText("Ana Silva já está cadastrada")).toBeNull();
    });
    expect(document.activeElement).toBe(screen.getByLabelText(/^Telefone/));
  });

  it("não fecha o modal nem recarrega a tela", async () => {
    // Fechar seria perder o que foi digitado; recarregar diria que gravou.
    render(<Bancada />);
    abrirEPreencher("Ana S.", "+5585999990000");

    await screen.findByText("Ana Silva já está cadastrada");
    expect(refresh).not.toHaveBeenCalled();
  });
});

describe("duplicata sem nome descoberto", () => {
  it("ainda leva à ficha, pelo id", async () => {
    // A busca por quem já existe pode falhar (rota fora do ar, campo que o
    // contrato não publicou). Um nome é melhor que um id; um id é muito melhor
    // que nada — e o `contact_id` vem na própria recusa.
    salvarContato.mockResolvedValue({
      ok: false,
      duplicata: { campo: "phone_e164", contactId: "c-ana", contato: null },
    });

    render(<Bancada />);
    abrirEPreencher("Ana S.", "+5585999990000");

    expect(await screen.findByText("Este telefone já está cadastrado")).toBeTruthy();
    expect(screen.getByRole("link", { name: /Abrir o contato existente/ }).getAttribute("href")).toBe(
      "/app/contatos/c-ana",
    );
  });
});

describe("validação antes de gastar requisição", () => {
  it("telefone sem DDI não chega a ser enviado", async () => {
    // O contrato recusa com 422 e **não** normaliza: adivinhar o DDI cria dois
    // registros da mesma pessoa. A tela diz isso no campo, sem a ida ao
    // servidor.
    render(<Bancada />);
    abrirEPreencher("Ana S.", "85999990000");

    expect(await screen.findByText(/formato internacional com DDI/)).toBeTruthy();
    expect(salvarContato).not.toHaveBeenCalled();
  });

  it("aceite de marketing sem data não chega a ser enviado", async () => {
    // Consentimento sem data não é consentimento, é afirmação — e o campo da
    // data só aparece depois que o aceite é ligado.
    render(<Bancada />);
    fireEvent.click(screen.getByRole("button", { name: "Novo" }));
    fireEvent.change(screen.getByLabelText(/^Nome/), { target: { value: "Ana S." } });
    fireEvent.click(screen.getByLabelText(/Aceita receber ofertas/));
    fireEvent.click(screen.getByRole("button", { name: "Salvar" }));

    expect(await screen.findByText(/aceitou em algum dia/)).toBeTruthy();
    expect(salvarContato).not.toHaveBeenCalled();
  });
});
