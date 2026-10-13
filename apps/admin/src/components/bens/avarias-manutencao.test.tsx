import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";

import { ListaDeAvarias, tituloSugerido, type ManutencaoDasAvarias } from "@/components/bens/avarias";
import type { Avaria } from "@/lib/bens/tipos";
import { abrirOrdem } from "@/lib/manutencao/acoes";

/**
 * "Abrir ordem de manutenção" a partir de uma avaria.
 *
 * Aparece com `maintenance:criar` — outro recurso, não `inventory.goods` — e
 * só para pendência aberta (`resolution === null`) **sem** ordem aberta
 * (`open_maintenance_order_id`, que vem na própria avaria). Abre o modal com os
 * quatro ids da avaria (unidade, cômodo, bem e a própria avaria) e um título
 * sugerido. A corrida (`409 MAINTENANCE_ORDER_ALREADY_OPEN`, a tela estava
 * velha) vira um link para a ordem que já existe, não um erro seco.
 */

const { push, refresh } = vi.hoisted(() => ({ push: vi.fn(), refresh: vi.fn() }));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, refresh, replace: vi.fn() }),
}));
vi.mock("sonner", () => ({ Toaster: () => null, toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() } }));
vi.mock("@/lib/bens/acoes", () => ({
  registrarAvaria: vi.fn(),
  resolverAvaria: vi.fn(),
  apagarAvaria: vi.fn(),
}));
vi.mock("@/lib/manutencao/acoes", () => ({
  abrirOrdem: vi.fn(),
  opcoesDaUnidade: vi.fn(),
  buscarBens: vi.fn(),
}));

const UNIDADE = "11111111-1111-4111-8111-111111111111";
const COMODO = "22222222-2222-4222-8222-222222222222";
const BEM = "33333333-3333-4333-8333-333333333333";
const AVARIA = "44444444-4444-4444-8444-444444444444";
const ORDEM = "9f1c1e2a-0d3b-4f6a-9a1e-2b7c5d8e0f11";

const PENDENTE: Avaria = {
  id: AVARIA,
  unit_id: UNIDADE,
  unit_code: "AP-03",
  room_id: COMODO,
  room_name: "Cozinha",
  item_id: BEM,
  item_name: "Geladeira duplex",
  kind: "avariado",
  qty: 1,
  reported_at: "2026-10-08T10:00:00Z",
  resolution: null,
  open_maintenance_order_id: null,
};

const PERMISSOES = { criar: false, editar: false, excluir: false };
const COM_CRIAR: ManutencaoDasAvarias = { podeAbrir: true, podeVerOrdens: true };

function montar(avaria: Avaria, manutencao?: ManutencaoDasAvarias) {
  render(<ListaDeAvarias avarias={[avaria]} permissoes={PERMISSOES} unidade={null} temFiltro={false} manutencao={manutencao} />);
}

const botao = () => screen.queryByRole("button", { name: /abrir ordem de manutenção/i });

describe("quem vê a ação", () => {
  it("com maintenance:criar e avaria pendente, aparece", () => {
    montar(PENDENTE, COM_CRIAR);
    expect(botao()).toBeTruthy();
  });

  it("sem a permissão, some", () => {
    montar(PENDENTE, { podeAbrir: false, podeVerOrdens: true });
    expect(botao()).toBeNull();
  });

  it("sem a ponte informada (padrão), some", () => {
    montar(PENDENTE);
    expect(botao()).toBeNull();
  });

  it("avaria já resolvida não pede conserto — some", () => {
    montar({ ...PENDENTE, resolution: "reposto", resolved_at: "2026-10-09T10:00:00Z" }, COM_CRIAR);
    expect(botao()).toBeNull();
  });

  it("com ordem já aberta (open_maintenance_order_id), o lugar da ação é um link para ela", () => {
    montar({ ...PENDENTE, open_maintenance_order_id: ORDEM }, COM_CRIAR);
    expect(botao()).toBeNull();
    const link = screen.getByRole("link", { name: /ordem de manutenção em aberto/i });
    expect(link.getAttribute("href")).toBe(`/app/manutencao/${ORDEM}`);
  });

  it("sem maintenance:ver, o aviso fica — mas não é link para uma tela que diria 'sem acesso'", () => {
    // O id vem na avaria para quem só tem inventory.goods:ver.
    montar({ ...PENDENTE, open_maintenance_order_id: ORDEM }, { podeAbrir: false, podeVerOrdens: false });
    expect(screen.getByText(/ordem de manutenção em aberto/i)).toBeTruthy();
    expect(screen.queryByRole("link", { name: /ordem de manutenção em aberto/i })).toBeNull();
  });

  it("campo ausente (API anterior ao campo) conta como sem ordem: a ação aparece e o 409 cobre a corrida", () => {
    const { open_maintenance_order_id: _ausente, ...semCampo } = PENDENTE;
    void _ausente;
    montar(semCampo, COM_CRIAR);
    expect(botao()).toBeTruthy();
  });

  it("avaria resolvida não mostra o aviso, mesmo com o id preenchido", () => {
    montar({ ...PENDENTE, resolution: "consertado", open_maintenance_order_id: ORDEM }, COM_CRIAR);
    expect(screen.queryByText(/ordem de manutenção em aberto/i)).toBeNull();
  });
});

async function abrirEEnviar() {
  montar(PENDENTE, COM_CRIAR);
  await act(async () => {
    fireEvent.click(botao()!);
  });
  await screen.findByRole("dialog");
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: /^abrir ordem$/i }));
  });
}

describe("o modal nasce preenchido pela avaria", () => {
  beforeEach(() => {
    vi.mocked(abrirOrdem).mockReset();
    push.mockReset();
    refresh.mockReset();
    vi.mocked(toast.info).mockReset();
  });

  it("os quatro ids e o título sugerido vão para a ordem — cômodo e bem travados", async () => {
    vi.mocked(abrirOrdem).mockResolvedValue({ ok: true, data: { id: ORDEM, block: null } as never });
    await abrirEEnviar();

    await waitFor(() => expect(abrirOrdem).toHaveBeenCalled());
    expect(vi.mocked(abrirOrdem).mock.calls[0][0]).toMatchObject({
      unit_id: UNIDADE,
      room_id: COMODO,
      item_id: BEM,
      issue_id: AVARIA,
      title: tituloSugerido(PENDENTE),
    });
    // Da tela de avarias não se navega: a pessoa continua na lista dela.
    expect(push).not.toHaveBeenCalled();
  });

  it("o título sugerido é editável, e o cômodo e o bem não aparecem como escolha", async () => {
    montar(PENDENTE, COM_CRIAR);
    await act(async () => {
      fireEvent.click(botao()!);
    });
    await screen.findByRole("dialog");
    const titulo = screen.getByLabelText(/o que precisa ser feito/i) as HTMLInputElement;
    expect(titulo.value).toBe("Avariado: Geladeira duplex (Cozinha)");
    expect(titulo.disabled).toBe(false);
    expect(screen.queryByLabelText(/^cômodo/i)).toBeNull();
    expect(screen.queryByLabelText(/^bem/i)).toBeNull();
    expect(screen.getByText(/unidade, cômodo e bem não mudam/i)).toBeTruthy();
  });

  it("409 MAINTENANCE_ORDER_ALREADY_OPEN vira link para a ordem existente, não erro seco", async () => {
    vi.mocked(abrirOrdem).mockResolvedValue({
      ok: false,
      code: "MAINTENANCE_ORDER_ALREADY_OPEN",
      message: "já existe",
      details: { maintenance_order_id: ORDEM },
    });
    await abrirEEnviar();

    await waitFor(() => expect(toast.info).toHaveBeenCalled());
    const [titulo, opcoes] = vi.mocked(toast.info).mock.calls[0] as [string, { action: ReactElement<{ href: string }> }];
    expect(titulo).toMatch(/já tem uma ordem aberta/i);
    expect(opcoes.action.props.href).toBe(`/app/manutencao/${ORDEM}`);
    // O modal fecha (nada de erro vermelho dentro dele) e a lista se atualiza
    // para mostrar o link na própria avaria.
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.queryByRole("alert")).toBeNull();
    expect(refresh).toHaveBeenCalled();
  });
});
