import * as React from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";

import { discar } from "@/lib/contatos/ligacao";
import type { Contato } from "@/lib/contatos/tipos";
import type { Lead } from "@/lib/crm/tipos";

import { PainelDeLeads, type PermissoesDeLeads } from "./painel";

/**
 * **O lead liga pelo telefone da ficha, não pela máscara** (dívida D11).
 *
 * `contact_phone_e164` do lead sai sempre mascarado (`+*********0000`): o
 * telefone é do contato, e a API só o entrega cheio na ficha
 * (`GET /contacts/{id}`), que grava `pii_access_log`. O card montava `tel:` com
 * a máscara e o botão discava asteriscos.
 *
 * O que estes casos exigem: o número entregue ao discador é o da **ficha**;
 * nenhum `tel:` da tela carrega `*`; sem telefone, não há botão.
 */

const lerFichaDoContato = vi.fn();

vi.mock("@/app/(app)/app/contatos/acoes", () => ({
  lerFichaDoContato: (...args: unknown[]) => lerFichaDoContato(...args),
}));

vi.mock("./acoes", () => ({
  converterLead: vi.fn(),
  descartarLead: vi.fn(),
}));

// O jsdom não navega: troca-se só o discador, e `linkDeLigacao` continua o de
// verdade — é ele que decide o que vira `tel:`.
vi.mock("@/lib/contatos/ligacao", async (original) => ({
  ...(await original<typeof import("@/lib/contatos/ligacao")>()),
  discar: vi.fn(() => true),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh: vi.fn(), push: vi.fn(), replace: vi.fn() }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
  Toaster: () => null,
}));

const discarMock = vi.mocked(discar);

const LEAD: Lead = {
  id: "l-1",
  contact_id: "c-ana",
  contact_name: "Ana Souza",
  contact_phone_e164: "+*********0000",
  source: "whatsapp",
  campaign_id: null,
  status: "novo",
  score: 0,
  interest_unit_type_id: null,
  interest_unit_type_name: "White House Completa",
  desired_check_in: "2026-12-28",
  desired_check_out: "2027-01-02",
  owner_id: null,
  owner_name: null,
  converted_at: null,
  opportunity_id: null,
  created_at: "2026-10-01T12:00:00Z",
  updated_at: "2026-10-01T12:00:00Z",
};

const FICHA_DA_ANA: Contato = {
  id: "c-ana",
  name: "Ana Souza",
  email: "ana.souza@gmail.com",
  phone_e164: "+5585999990000",
  doc_type: null,
  doc_number: null,
  birth_date: null,
  city: "Fortaleza",
  state: "CE",
  notes: null,
  lgpd_basis: "legitimo_interesse",
  marketing_opt_in: false,
  consent_at: null,
  anonymized_at: null,
  created_at: "2026-10-01T12:00:00Z",
  updated_at: "2026-10-01T12:00:00Z",
};

const TODAS: PermissoesDeLeads = { editar: true, excluir: true, verFicha: true };

function desenhar(leads: Lead[], permissoes: PermissoesDeLeads = TODAS) {
  return render(<PainelDeLeads leads={leads} funis={[]} etapas={[]} produtos={[]} permissoes={permissoes} />);
}

function hrefsDeLigacao(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll<HTMLAnchorElement>('a[href^="tel:"]')).map(
    (a) => a.getAttribute("href") ?? "",
  );
}

beforeEach(() => {
  lerFichaDoContato.mockReset();
  discarMock.mockClear();
});

describe("ligar para o lead", () => {
  it("disca o telefone da ficha do contato, não a máscara do lead", async () => {
    lerFichaDoContato.mockResolvedValue({ ok: true, data: FICHA_DA_ANA });
    const { container } = desenhar([LEAD]);

    // Antes do clique: a máscara aparece como texto, e nenhum `tel:` existe —
    // nem o de asteriscos que o card montava.
    expect(screen.getByText("+*********0000")).toBeTruthy();
    expect(hrefsDeLigacao(container)).toEqual([]);
    // Desenhar a lista não lê ficha nenhuma: seriam 25 leituras com rastro sem
    // ninguém ter pedido para ligar.
    expect(lerFichaDoContato).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Ligar para Ana Souza" }));

    await waitFor(() => expect(discarMock).toHaveBeenCalledTimes(1));
    expect(lerFichaDoContato).toHaveBeenCalledWith("c-ana");
    expect(discarMock).toHaveBeenCalledWith("+5585999990000");

    // Depois: o número da ficha fica à vista como `tel:` comum, para o caso de
    // o navegador não ter aberto o discador sozinho.
    expect(hrefsDeLigacao(container)).toEqual(["tel:+5585999990000"]);
  });

  it("sem telefone no contato, o botão não aparece", () => {
    const { container } = desenhar([{ ...LEAD, contact_phone_e164: null }]);

    expect(screen.queryByRole("button", { name: /Ligar/ })).toBeNull();
    expect(hrefsDeLigacao(container)).toEqual([]);
    expect(lerFichaDoContato).not.toHaveBeenCalled();
  });

  it("sem acesso à ficha (`contacts:ver`), o botão não aparece", () => {
    desenhar([LEAD], { ...TODAS, verFicha: false });

    expect(screen.queryByRole("button", { name: /Ligar/ })).toBeNull();
  });

  it("ficha sem telefone (tirado depois): não disca e diz por quê", async () => {
    lerFichaDoContato.mockResolvedValue({ ok: true, data: { ...FICHA_DA_ANA, phone_e164: null } });
    const { container } = desenhar([LEAD]);

    fireEvent.click(screen.getByRole("button", { name: "Ligar para Ana Souza" }));

    expect(await screen.findByText("Sem telefone na ficha")).toBeTruthy();
    expect(discarMock).not.toHaveBeenCalled();
    expect(hrefsDeLigacao(container)).toEqual([]);
  });

  it("ficha recusada: não disca, avisa pelo código e o botão continua", async () => {
    lerFichaDoContato.mockResolvedValue({ ok: false, code: "FORBIDDEN", message: "x", details: {} });
    desenhar([LEAD]);

    fireEvent.click(screen.getByRole("button", { name: "Ligar para Ana Souza" }));

    await waitFor(() => expect(vi.mocked(toast.error)).toHaveBeenCalledTimes(1));
    expect(vi.mocked(toast.error).mock.calls[0][0]).toBe("Sem permissão");
    expect(discarMock).not.toHaveBeenCalled();
    await waitFor(() =>
      expect((screen.getByRole("button", { name: "Ligar para Ana Souza" }) as HTMLButtonElement).disabled).toBe(
        false,
      ),
    );
  });
});
