import { render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AgendaOperacional } from "@/components/agenda/agenda-operacional";
import { permissoesDaAgenda } from "@/lib/agenda/permissoes";
import type { ConfiguracaoDaAgenda, EventoDaAgenda } from "@/lib/agenda/tipos";
import { lerFiltros, type Visao } from "@/lib/agenda/visoes";

/**
 * As quatro visões sobre a mesma leitura da janela — e o "hoje" que vem do
 * servidor (`settings.today`), nunca do relógio do navegador.
 */

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/app/agenda",
}));
vi.mock("sonner", () => ({ Toaster: () => null, toast: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warning: vi.fn() } }));
vi.mock("@/lib/agenda/acoes", () => ({
  marcarEvento: vi.fn(),
  salvarEvento: vi.fn(),
  concluirEvento: vi.fn(),
  desmarcarEvento: vi.fn(),
  buscarReservas: vi.fn(),
  buscarContatos: vi.fn(),
}));

/** O hoje DA CASA, como `GET /agenda/settings` o devolve. */
const HOJE = "2026-11-20"; // sexta-feira

const HORARIOS: ConfiguracaoDaAgenda = {
  timezone: "America/Sao_Paulo",
  today: HOJE,
  checkin_time: "14:00",
  checkout_time: "11:00",
  cleaning_start: "11:00",
  cleaning_end: "14:00",
  business_hours: [1, 2, 3, 4, 5, 6, 7].map((weekday) => ({ weekday, start: "08:00", end: "20:00" })),
  slot_minutes: 30,
  buffer_minutes: 15,
  provisional: true,
  updated_at: "2026-10-13T10:00:00-03:00",
};

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
    units: [{ unit_id: "u1", unit_code: "AP-02" }],
    has_unit_conflict: false,
    allowed_actions: ["complete", "cancel"],
    editable: "tudo",
    ...parcial,
  };
}

const EVENTOS: EventoDaAgenda[] = [
  evento({}),
  evento({
    id: "00000000-0000-4000-8000-000000000002",
    type: "checkin",
    origin: "reservation",
    title: "Check-in · WH-2026-0007",
    starts_at: "2026-11-21T14:00:00-03:00",
    ends_at: "2026-11-21T14:30:00-03:00",
    date: "2026-11-21",
    allowed_actions: [],
    editable: "segue_a_reserva",
    has_unit_conflict: true,
  }),
  evento({
    id: "00000000-0000-4000-8000-000000000003",
    type: "manutencao",
    origin: "maintenance",
    title: "Pintura da sala",
    all_day: true,
    starts_at: "2026-11-23T00:00:00-03:00",
    ends_at: "2026-11-25T00:00:00-03:00",
    date: "2026-11-23",
    allowed_actions: [],
    editable: "nada",
  }),
];

const TODAS = permissoesDaAgenda(
  (["ver", "criar", "editar", "excluir"] as const).map((action) => ({ resource: "agenda", action, scope: "all" as const })),
);

function renderizar(visao: Visao, data = HOJE, eventos = EVENTOS) {
  const { filtros } = lerFiltros({ visao, data }, HOJE);
  return render(
    <AgendaOperacional
      filtros={filtros}
      hoje={HOJE}
      horarios={HORARIOS}
      eventosIniciais={eventos}
      bloqueiosIniciais={[
        {
          id: "b1",
          starts_at: "2026-11-20T09:00:00-03:00",
          ends_at: "2026-11-20T10:00:00-03:00",
          all_day: false,
          reason: "Equipe em treinamento",
          created_at: "2026-11-01T10:00:00-03:00",
          updated_at: "2026-11-01T10:00:00-03:00",
        },
      ]}
      unidades={[{ id: "u1", code: "AP-02", name: "Apartamento 02" }]}
      pessoas={[]}
      eu={{ id: "eu", name: "Ana" }}
      permissoes={TODAS}
      destinos={{ reservas: true, manutencao: true, mapa: true }}
      buscas={{ reservas: false, contatos: false }}
      aoVivo={false}
      aoRecarregar={vi.fn()}
    />,
  );
}

beforeEach(() => {
  // O relógio da máquina diz OUTRO dia (e outro mês). Se alguma visão olhar
  // para ele em vez do `hoje` do servidor, os casos abaixo apontam.
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-12-31T23:30:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
});

describe("visão do dia", () => {
  it("uma coluna, com o evento posicionado e o bloqueio de horário no fundo", () => {
    const { container } = renderizar("dia");
    const colunas = container.querySelectorAll("section.agenda-pista");
    expect(colunas).toHaveLength(1);
    const coluna = screen.getByRole("region", { name: /sexta-feira, 20 de novembro de 2026 \(hoje\)/ });
    expect(within(coluna).getByRole("button", { name: /Visita da família Andrade, 15:00–16:00, AP-02, Agendado/ })).toBeTruthy();
    expect(coluna.querySelector("[data-bloqueio-de-horario]")?.textContent).toContain("Equipe em treinamento");
    // O evento de outro dia não entra na coluna do dia.
    expect(within(coluna).queryByRole("button", { name: /Check-in/ })).toBeNull();
  });

  it("o dia de hoje é o do servidor", () => {
    const { container } = renderizar("dia");
    expect(container.querySelector("[aria-current='date']")?.getAttribute("data-dia")).toBe(HOJE);
    expect(container.querySelector("section[data-hoje='1']")?.getAttribute("data-dia")).toBe(HOJE);
  });
});

describe("visão da semana", () => {
  it("sete colunas, de segunda a domingo, com o hoje do servidor marcado", () => {
    const { container } = renderizar("semana");
    const dias = [...container.querySelectorAll("section.agenda-pista")].map((s) => s.getAttribute("data-dia"));
    expect(dias).toEqual(["2026-11-16", "2026-11-17", "2026-11-18", "2026-11-19", "2026-11-20", "2026-11-21", "2026-11-22"]);
    const hoje = container.querySelectorAll("[aria-current='date']");
    expect(hoje).toHaveLength(1);
    expect(hoje[0]!.getAttribute("data-dia")).toBe(HOJE);
  });

  it("o selo de conflito de unidade chega à grade, e o cabeçalho do dia leva à visão do dia", () => {
    renderizar("semana");
    const sabado = screen.getByRole("region", { name: /sábado, 21 de novembro/ });
    expect(within(sabado).getByRole("button", { name: /Check-in · WH-2026-0007.*conflito de unidade/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /Abrir sábado, 21 de novembro/ }).getAttribute("href")).toBe("/app/agenda?data=2026-11-21");
  });

  it("o de dia inteiro vai para a faixa de cima, em cada dia que toca", () => {
    const { container } = renderizar("semana", "2026-11-23");
    const faixas = [...container.querySelectorAll("[data-faixa-dia-inteiro]")].filter((f) => f.textContent?.includes("Pintura da sala"));
    expect(faixas.map((f) => f.getAttribute("data-faixa-dia-inteiro"))).toEqual(["2026-11-23", "2026-11-24"]);
  });
});

describe("visão do mês", () => {
  it("a grade inteira de novembro (6 semanas), com o hoje do servidor e os eventos em cada dia que tocam", () => {
    const { container } = renderizar("mes");
    const celulas = container.querySelectorAll("[data-dia]");
    expect(celulas).toHaveLength(42);
    expect(container.querySelector("[aria-current='date']")?.getAttribute("data-dia")).toBe(HOJE);
    const dia24 = container.querySelector("[data-dia='2026-11-24']") as HTMLElement;
    expect(within(dia24).getByRole("button", { name: /Pintura da sala/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /sexta-feira, 20 de novembro de 2026 \(hoje\): 1 evento/ }).getAttribute("href")).toBe(
      "/app/agenda",
    );
  });
});

describe("visão da lista", () => {
  it("um grupo por dia, agrupado pelo date, com o dia vazio dito", () => {
    renderizar("lista");
    const hoje = screen.getByRole("region", { name: /sexta-feira, 20 de novembro de 2026 \(hoje\)/ });
    expect(within(hoje).getByRole("article", { name: "Visita da família Andrade" })).toBeTruthy();
    expect(within(hoje).getByText("Hoje")).toBeTruthy();
    const domingo = screen.getByRole("region", { name: /domingo, 22 de novembro/ });
    expect(within(domingo).getByText("Nada marcado.")).toBeTruthy();
    // "Concluir" direto no cartão, porque a API deixou (`allowed_actions`).
    expect(within(hoje).getByRole("button", { name: "Concluir" })).toBeTruthy();
    // O check-in automático não tem `complete`: nada de botão no cartão dele.
    const sabado = screen.getByRole("region", { name: /sábado, 21 de novembro/ });
    expect(within(sabado).queryByRole("button", { name: "Concluir" })).toBeNull();
  });

  it("o cabeçalho 'Hoje' fica no hoje do servidor, mesmo com o relógio em outro mês", () => {
    const { container } = renderizar("lista", "2026-11-18");
    const marcado = container.querySelectorAll("[aria-current='date']");
    expect(marcado).toHaveLength(1);
    expect(marcado[0]!.closest("section")?.getAttribute("data-dia")).toBe(HOJE);
  });
});
