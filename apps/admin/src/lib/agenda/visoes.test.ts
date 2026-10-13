import { afterEach, describe, expect, it, vi } from "vitest";

import type { EventoDaAgenda } from "@/lib/agenda/tipos";
import {
  agruparPorDia,
  consultaDosEventos,
  dataVizinha,
  diasDoEvento,
  janelaDaVisao,
  lerFiltros,
  paraQueryString,
  semanasDaJanela,
  tituloDaJanela,
} from "@/lib/agenda/visoes";

function evento(parcial: Partial<EventoDaAgenda>): EventoDaAgenda {
  return {
    id: "00000000-0000-4000-8000-000000000001",
    type: "visita",
    origin: "manual",
    status: "agendado",
    title: "Visita",
    starts_at: "2026-11-20T14:00:00-03:00",
    ends_at: "2026-11-20T14:30:00-03:00",
    all_day: false,
    date: "2026-11-20",
    units: [],
    has_unit_conflict: false,
    allowed_actions: ["complete", "cancel"],
    editable: "tudo",
    ...parcial,
  };
}

afterEach(() => {
  vi.useRealTimers();
});

describe("'hoje' é o do servidor, nunca o relógio de quem abriu a tela", () => {
  it("sem data na URL, a agenda abre no `today` que veio de GET /agenda/settings", () => {
    // O relógio da máquina diz outro dia — e não pode ganhar.
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-12-31T12:00:00Z"));
    const { filtros } = lerFiltros({}, "2026-11-20");
    expect(filtros.data).toBe("2026-11-20");
    expect(janelaDaVisao(filtros.visao, filtros.data).from).toBe("2026-11-20");
  });

  it("data inválida na URL cai no hoje do servidor, com aviso", () => {
    const { filtros, avisos } = lerFiltros({ data: "20/11/2026" }, "2026-11-20");
    expect(filtros.data).toBe("2026-11-20");
    expect(avisos).toHaveLength(1);
  });

  it("o link sem `data` é o hoje: a URL só carrega a data quando ela não é hoje", () => {
    const { filtros } = lerFiltros({}, "2026-11-20");
    expect(paraQueryString(filtros, "2026-11-20")).toBe("");
    expect(paraQueryString({ ...filtros, data: "2026-11-21" }, "2026-11-20")).toBe("?data=2026-11-21");
  });
});

describe("as quatro janelas — half-open, em dias da casa, dentro dos 92 do contrato", () => {
  it("dia: [d, d+1)", () => {
    expect(janelaDaVisao("dia", "2026-11-20")).toEqual({ from: "2026-11-20", to: "2026-11-21", dias: ["2026-11-20"] });
  });

  it("semana: de segunda a domingo, qualquer que seja o dia escolhido", () => {
    // 20/11/2026 é sexta.
    const j = janelaDaVisao("semana", "2026-11-20");
    expect(j.from).toBe("2026-11-16");
    expect(j.to).toBe("2026-11-23");
    expect(j.dias).toHaveLength(7);
    // Domingo continua na semana que começou na segunda anterior.
    expect(janelaDaVisao("semana", "2026-11-22").from).toBe("2026-11-16");
  });

  it("mês: a grade inteira, da segunda da semana do dia 1 ao domingo da semana do último", () => {
    const j = janelaDaVisao("mes", "2026-11-20");
    // 01/11/2026 é domingo: a grade começa em 26/10; 30/11 é segunda: acaba em 06/12.
    expect(j.from).toBe("2026-10-26");
    expect(j.to).toBe("2026-12-07");
    expect(j.dias).toHaveLength(42);
    expect(semanasDaJanela(j)).toHaveLength(6);
    expect(j.dias.length).toBeLessThanOrEqual(92);
  });

  it("lista: uma semana a partir do dia escolhido", () => {
    const j = janelaDaVisao("lista", "2026-11-20");
    expect(j.from).toBe("2026-11-20");
    expect(j.to).toBe("2026-11-27");
  });

  it("andar para trás e para a frente respeita a visão", () => {
    expect(dataVizinha("dia", "2026-11-20", 1)).toBe("2026-11-21");
    expect(dataVizinha("semana", "2026-11-20", -1)).toBe("2026-11-09");
    expect(dataVizinha("mes", "2026-01-31", 1)).toBe("2026-02-01");
    expect(dataVizinha("mes", "2026-01-15", -1)).toBe("2025-12-01");
    expect(dataVizinha("lista", "2026-11-20", 1)).toBe("2026-11-27");
  });

  it("o título do período", () => {
    expect(tituloDaJanela("dia", "2026-11-20", janelaDaVisao("dia", "2026-11-20"))).toMatch(/sexta-feira, 20 de novembro de 2026/);
    expect(tituloDaJanela("mes", "2026-11-20", janelaDaVisao("mes", "2026-11-20"))).toBe("novembro de 2026");
    expect(tituloDaJanela("semana", "2026-11-20", janelaDaVisao("semana", "2026-11-20"))).toBe("16/11 a 22/11");
  });
});

describe("a lista agrupa pelo `date` da API", () => {
  const janela = janelaDaVisao("lista", "2026-11-20");

  it("um grupo por dia da janela, inclusive os vazios, na ordem da API", () => {
    const a = evento({ id: "a", date: "2026-11-20" });
    const b = evento({ id: "b", date: "2026-11-22", starts_at: "2026-11-22T10:00:00-03:00", ends_at: "2026-11-22T11:00:00-03:00" });
    const grupos = agruparPorDia([a, b], janela);
    expect(grupos.map((g) => g.data)).toEqual(janela.dias);
    expect(grupos[0]!.eventos.map((e) => e.id)).toEqual(["a"]);
    expect(grupos[1]!.eventos).toEqual([]);
    expect(grupos[2]!.eventos.map((e) => e.id)).toEqual(["b"]);
  });

  it("agrupa pelo `date`, e não pelo instante convertido no navegador", () => {
    // Um `starts_at` em UTC que, lido no fuso do navegador de quem está em
    // Lisboa, cairia no dia seguinte. O `date` da API diz 20 — e é 20.
    const tarde = evento({ id: "t", date: "2026-11-20", starts_at: "2026-11-21T00:30:00Z", ends_at: "2026-11-21T01:00:00Z" });
    const grupos = agruparPorDia([tarde], janela);
    expect(grupos[0]!.eventos.map((e) => e.id)).toEqual(["t"]);
  });

  it("o que começou antes da janela aparece no primeiro dia dela, em vez de sumir", () => {
    const manutencao = evento({
      id: "m",
      type: "manutencao",
      origin: "maintenance",
      all_day: true,
      date: "2026-11-18",
      starts_at: "2026-11-18T00:00:00-03:00",
      ends_at: "2026-11-23T00:00:00-03:00",
    });
    const grupos = agruparPorDia([manutencao], janela);
    expect(grupos[0]!.eventos.map((e) => e.id)).toEqual(["m"]);
  });
});

describe("os dias que um evento toca — o fim é exclusivo", () => {
  it("dia inteiro de três dias termina à meia-noite do quarto, e o quarto não é dele", () => {
    expect(
      diasDoEvento({ date: "2026-11-20", ends_at: "2026-11-23T00:00:00-03:00" }),
    ).toEqual({ primeiro: "2026-11-20", ultimo: "2026-11-22" });
  });

  it("a visita que acaba às 00:00 não toca o dia seguinte; a que passa da meia-noite, toca", () => {
    expect(diasDoEvento({ date: "2026-11-20", ends_at: "2026-11-21T00:00:00-03:00" }).ultimo).toBe("2026-11-20");
    expect(diasDoEvento({ date: "2026-11-20", ends_at: "2026-11-21T02:00:00-03:00" }).ultimo).toBe("2026-11-21");
  });
});

describe("a consulta de GET /agenda/events", () => {
  const janela = janelaDaVisao("semana", "2026-11-20");
  const base = lerFiltros({}, "2026-11-20").filtros;

  it("pede a janela inteira, 100 por página, na ordem de início", () => {
    expect(consultaDosEventos(base, janela, "eu")).toMatchObject({
      from: "2026-11-16",
      to: "2026-11-23",
      per_page: 100,
      sort: "starts_at",
      status: undefined,
    });
  });

  it("os desmarcados só vêm quando pedidos — o padrão da API os esconde", () => {
    expect(consultaDosEventos({ ...base, cancelados: true }, janela, "eu").status).toBe("agendado,concluido,cancelado");
  });

  it("'comigo' e 'sem responsável' nunca vão juntos (seria 422)", () => {
    const comigo = consultaDosEventos({ ...base, responsavel: "eu" }, janela, "u-1");
    expect(comigo.assignee_id).toBe("u-1");
    expect(comigo.unassigned).toBeUndefined();
    const ninguem = consultaDosEventos({ ...base, responsavel: "ninguem" }, janela, "u-1");
    expect(ninguem.unassigned).toBe(true);
    expect(ninguem.assignee_id).toBeUndefined();
  });

  it("filtro inventado na URL não vira consulta", () => {
    const { filtros, avisos } = lerFiltros({ tipo: "festa", unidade: "AP-01", visao: "ano" }, "2026-11-20");
    expect(filtros.tipo).toBe("");
    expect(filtros.unidade).toBe("");
    expect(filtros.visao).toBe("dia");
    expect(avisos).toHaveLength(3);
  });
});
