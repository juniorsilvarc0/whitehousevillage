import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";

import { ModalDeEvento, type AlvoDoEvento } from "@/components/agenda/modal-de-evento";
import { useControleDeModal } from "@/components/layout/controle-de-modal";
import type { Escopo } from "@/lib/api/types";
import { marcarEvento, salvarEvento } from "@/lib/agenda/acoes";
import type { EventoDaAgenda } from "@/lib/agenda/tipos";

/**
 * O formulário de evento: o aviso de horário depois do 201, o 409 de unidade
 * junto do horário, o 403 que fixa o responsável e os campos abertos pelo
 * `editable`.
 */

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn(), replace: vi.fn() }),
}));
vi.mock("sonner", () => ({ Toaster: () => null, toast: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warning: vi.fn() } }));
vi.mock("@/lib/agenda/acoes", () => ({
  marcarEvento: vi.fn(),
  salvarEvento: vi.fn(),
  buscarReservas: vi.fn(),
  buscarContatos: vi.fn(),
}));

const EU = { id: "33333333-3333-4333-8333-333333333333", name: "Ana" };
const COLEGA = { id: "44444444-4444-4444-8444-444444444444", name: "Bruno" };
const UNIDADE = "11111111-1111-4111-8111-111111111111";

function evento(parcial: Partial<EventoDaAgenda>): EventoDaAgenda {
  return {
    id: "00000000-0000-4000-8000-000000000001",
    type: "visita",
    origin: "manual",
    status: "agendado",
    title: "Visita da família Andrade",
    starts_at: "2026-11-20T15:00:00-03:00",
    ends_at: "2026-11-20T16:00:00-03:00",
    all_day: false,
    date: "2026-11-20",
    units: [{ unit_id: UNIDADE, unit_code: "AP-02" }],
    has_unit_conflict: false,
    allowed_actions: ["complete", "cancel"],
    editable: "tudo",
    ...parcial,
  };
}

function Abrir({ alvo, escopo = "all" }: { alvo: AlvoDoEvento; escopo?: Escopo }) {
  const modal = useControleDeModal<AlvoDoEvento>();
  return (
    <>
      <button type="button" onClick={() => modal.abrir(alvo)}>
        abrir
      </button>
      <ModalDeEvento
        controle={modal.ref}
        unidades={[{ id: UNIDADE, code: "AP-02", name: "Apartamento 02" }]}
        pessoas={[COLEGA]}
        eu={EU}
        escopoAoCriar={escopo}
        escopoAoEditar={escopo}
        passoEmMinutos={30}
        buscas={{ reservas: false, contatos: false }}
      />
    </>
  );
}

async function abrir(alvo: AlvoDoEvento, escopo: Escopo = "all") {
  render(<Abrir alvo={alvo} escopo={escopo} />);
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "abrir" }));
  });
  return screen.findByRole("dialog");
}

async function preencherVisitaEMarcar() {
  await abrir({ modo: "criar", data: "2026-11-20" });
  fireEvent.change(screen.getByLabelText(/^Título/), { target: { value: "Visita da família Andrade" } });
  fireEvent.change(screen.getByLabelText(/^Começa às/), { target: { value: "21:00" } });
  fireEvent.change(screen.getByLabelText(/^Unidade/), { target: { value: UNIDADE } });
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Marcar" }));
  });
}

const marcar = vi.mocked(marcarEvento);
const salvar = vi.mocked(salvarEvento);

beforeEach(() => {
  marcar.mockReset();
  salvar.mockReset();
  vi.mocked(toast.warning).mockReset();
  vi.mocked(toast.success).mockReset();
});

describe("o aviso de horário — depois do 201, sem impedir", () => {
  it("marcado fora do funcionamento: grava, fecha e avisa pelo kind", async () => {
    marcar.mockResolvedValue({
      ok: true,
      data: {
        evento: evento({ starts_at: "2026-11-20T21:00:00-03:00", ends_at: "2026-11-20T21:30:00-03:00" }),
        avisos: [
          { kind: "fora_do_horario", message: "texto da API que a tela não usa" },
          { kind: "responsavel_ocupado", message: "idem", ref_id: "x", starts_at: "2026-11-20T20:45:00-03:00", ends_at: "2026-11-20T21:15:00-03:00" },
        ],
      },
    });
    await preencherVisitaEMarcar();

    await waitFor(() => expect(toast.warning).toHaveBeenCalledTimes(1));
    const [titulo, opcoes] = vi.mocked(toast.warning).mock.calls[0]!;
    expect(titulo).toMatch(/Visita marcada — com aviso de horário/);
    const descricao = String((opcoes as { description?: unknown }).description);
    expect(descricao).toMatch(/fora do horário de funcionamento/);
    expect(descricao).toMatch(/outro compromisso \(20\/11, das 20:45 às 21:15\)/);
    expect(descricao).not.toMatch(/texto da API/);
    // Aviso não é erro: nada de alerta no formulário, e o pedido saiu uma vez só.
    expect(screen.queryByRole("alert")).toBeNull();
    expect(marcar).toHaveBeenCalledTimes(1);
  });

  it("na edição, o 200 com aviso também avisa — o horário novo foi gravado", async () => {
    salvar.mockResolvedValue({
      ok: true,
      data: {
        evento: evento({}),
        avisos: [{ kind: "bloqueio_de_horario", message: "x", ref_id: "b1", starts_at: "2026-11-20T15:00:00-03:00", ends_at: "2026-11-20T16:30:00-03:00" }],
      },
    });
    await abrir({ modo: "editar", evento: evento({}) });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Salvar" }));
    });
    await waitFor(() => expect(toast.warning).toHaveBeenCalledTimes(1));
    const [titulo, opcoes] = vi.mocked(toast.warning).mock.calls[0]!;
    expect(titulo).toMatch(/Evento atualizado — com aviso de horário/);
    expect(String((opcoes as { description?: unknown }).description)).toBe(
      "Cai num horário bloqueado da operação (20/11, das 15:00 às 16:30).",
    );
    expect(salvar.mock.calls[0]![1]).toBe("tudo");
  });

  it("sem aviso, é o sucesso comum", async () => {
    marcar.mockResolvedValue({ ok: true, data: { evento: evento({}), avisos: [] } });
    await preencherVisitaEMarcar();
    await waitFor(() => expect(toast.success).toHaveBeenCalled());
    expect(toast.warning).not.toHaveBeenCalled();
  });
});

describe("409 UNIT_NOT_AVAILABLE no campo certo", () => {
  it("cada ocupação aparece junto dos campos de horário — e o modal não fecha", async () => {
    marcar.mockResolvedValue({
      ok: false,
      code: "UNIT_NOT_AVAILABLE",
      message: "unit busy",
      details: {
        conflicts: [
          { unit_id: UNIDADE, unit_code: "AP-02", kind: "limpeza", starts_at: "2026-11-20T11:00:00-03:00", ends_at: "2026-11-20T14:00:00-03:00" },
          { unit_id: UNIDADE, unit_code: "AP-02", kind: "estadia", starts_at: "2026-11-20T14:00:00-03:00", ends_at: "2026-11-23T11:00:00-03:00" },
        ],
      },
    });
    await preencherVisitaEMarcar();

    const quando = screen.getByRole("group", { name: "Quando" });
    await waitFor(() => expect(within(quando).getByRole("alert").textContent).toMatch(/nada foi marcado/i));
    const linhas = within(within(quando).getByRole("list", { name: /o que ocupa a unidade/i })).getAllByRole("listitem");
    expect(linhas.map((l) => l.textContent)).toEqual([
      "AP-02 · limpeza · 20/11, das 11:00 às 14:00",
      "AP-02 · hóspede na unidade · de 20/11 14:00 a 23/11 11:00",
    ]);
    // Um alerta só, e é o do horário — nada no topo do modal.
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(toast.success).not.toHaveBeenCalled();
  });
});

describe("403 assignee_not_actor: o corretor só agenda para si", () => {
  it("o responsável escolhido é recusado e o campo passa a vir fixo no próprio usuário", async () => {
    marcar.mockResolvedValue({
      ok: false,
      code: "FORBIDDEN",
      message: "forbidden",
      details: { field: "assignee_id", reason: "assignee_not_actor", scope: "own" },
    });
    await abrir({ modo: "criar", data: "2026-11-20" });
    fireEvent.change(screen.getByLabelText(/^Título/), { target: { value: "Visita" } });
    fireEvent.change(screen.getByLabelText(/^Começa às/), { target: { value: "15:00" } });
    fireEvent.change(screen.getByLabelText(/^Responsável/), { target: { value: COLEGA.id } });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Marcar" }));
    });

    const campo = await screen.findByLabelText(/^Responsável/);
    await waitFor(() => expect((campo as HTMLSelectElement).disabled).toBe(true));
    expect((campo as HTMLSelectElement).value).toBe(EU.id);
    expect(document.getElementById("evento-responsavel-erro")?.textContent).toMatch(/sempre você/);
  });

  it("em escopo own o campo já nasce fixo — e o pedido leva o próprio usuário", async () => {
    marcar.mockResolvedValue({ ok: true, data: { evento: evento({}), avisos: [] } });
    await abrir({ modo: "criar", data: "2026-11-20" }, "own");
    const campo = screen.getByLabelText(/^Responsável/) as HTMLSelectElement;
    expect(campo.disabled).toBe(true);
    expect(campo.value).toBe(EU.id);
    fireEvent.change(screen.getByLabelText(/^Título/), { target: { value: "Visita" } });
    fireEvent.change(screen.getByLabelText(/^Começa às/), { target: { value: "15:00" } });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Marcar" }));
    });
    await waitFor(() => expect(marcar).toHaveBeenCalled());
    expect(marcar.mock.calls[0]![0]).toMatchObject({ assignee_id: EU.id });
  });
});

describe("os campos abertos saem de editable", () => {
  function estado(rotulo: RegExp): boolean {
    return (screen.getByLabelText(rotulo) as HTMLInputElement).disabled;
  }

  it("tudo (manual agendado): todos abertos", async () => {
    await abrir({ modo: "editar", evento: evento({ assignee_id: EU.id, assignee_name: EU.name }) });
    expect([/^O que é/, /^Título/, /^Dia\*?$/, /^Começa às/, /^Termina às/, /^Unidade/, /^Responsável/, /^Anotação$/].map(estado)).toEqual(
      Array(8).fill(false),
    );
  });

  it("segue a reserva (automático): só horário, responsável e anotação", async () => {
    await abrir({
      modo: "editar",
      evento: evento({
        type: "checkin",
        origin: "reservation",
        title: "Check-in · WH-2026-0007",
        starts_at: "2026-11-20T14:00:00-03:00",
        ends_at: "2026-11-20T14:30:00-03:00",
        allowed_actions: [],
        editable: "segue_a_reserva",
        reservation_id: "55555555-5555-4555-8555-555555555555",
        reservation_code: "WH-2026-0007",
      }),
    });
    expect(estado(/^O que é/)).toBe(true);
    expect(estado(/^Título/)).toBe(true);
    expect(estado(/^Unidade/)).toBe(true);
    expect(estado(/^Dia\*?$/)).toBe(false);
    expect(estado(/^Começa às/)).toBe(false);
    expect(estado(/^Termina às/)).toBe(false);
    expect(estado(/^Responsável/)).toBe(false);
    expect(estado(/^Anotação$/)).toBe(false);
    expect((screen.getByLabelText(/^Título/) as HTMLInputElement).value).toBe("Check-in · WH-2026-0007");
  });

  it("só notas (concluído): só a anotação", async () => {
    await abrir({ modo: "editar", evento: evento({ status: "concluido", allowed_actions: [], editable: "so_notas" }) });
    expect([/^O que é/, /^Título/, /^Dia\*?$/, /^Começa às/, /^Termina às/, /^Unidade/, /^Responsável/].map(estado)).toEqual(Array(7).fill(true));
    expect(estado(/^Anotação$/)).toBe(false);
  });

  it("e grava pelo verbo do editable — o automático vai por PATCH com o editable que a API mandou", async () => {
    salvar.mockResolvedValue({ ok: true, data: { evento: evento({}), avisos: [] } });
    await abrir({
      modo: "editar",
      evento: evento({ type: "limpeza", origin: "reservation", title: "Limpeza · WH-2026-0007", allowed_actions: ["complete"], editable: "segue_a_reserva" }),
    });
    fireEvent.change(screen.getByLabelText(/^Começa às/), { target: { value: "15:00" } });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Salvar" }));
    });
    await waitFor(() => expect(salvar).toHaveBeenCalled());
    expect(salvar.mock.calls[0]![1]).toBe("segue_a_reserva");
    expect(salvar.mock.calls[0]![2]).toMatchObject({ hora_inicio: "15:00", type: "limpeza" });
  });
});
