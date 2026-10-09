import { describe, expect, it } from "vitest";

import { comSituacao, consultaDaLista, lerFiltros, situacaoDaUrl, temFiltro } from "@/lib/manutencao/filtros";

const UNIDADE = "11111111-1111-4111-8111-111111111111";

describe("os filtros da lista moram na URL, com os nomes da API", () => {
  it("lê status, open, unit_id, priority, q e page", () => {
    const { filtros, avisos } = lerFiltros({
      status: "em_andamento",
      open: "true",
      unit_id: UNIDADE,
      priority: "urgente",
      q: "chuveiro",
      page: "3",
    });
    expect(avisos).toEqual([]);
    expect(consultaDaLista(filtros)).toEqual({
      status: "em_andamento",
      open: true,
      unit_id: UNIDADE,
      priority: "urgente",
      q: "chuveiro",
      page: 3,
      per_page: 25,
    });
  });

  it("nunca manda sort — a ordem da lista de trabalho é a da API (sort=urgencia)", () => {
    const { filtros } = lerFiltros({ sort: "-opened_at" });
    expect("sort" in consultaDaLista(filtros)).toBe(false);
  });

  it("o que a API recusaria com 422 é descartado aqui, com aviso", () => {
    const { filtros, avisos } = lerFiltros({ status: "pausada", priority: "altissima", unit_id: "AP-03" });
    expect(filtros).toMatchObject({ status: "", priority: "", unit_id: "" });
    expect(avisos).toHaveLength(3);
  });

  it("open=false é filtro (encerradas); ausente não é", () => {
    expect(consultaDaLista(lerFiltros({ open: "false" }).filtros).open).toBe(false);
    expect(consultaDaLista(lerFiltros({}).filtros).open).toBeUndefined();
    expect(temFiltro(lerFiltros({}).filtros)).toBe(false);
    expect(temFiltro(lerFiltros({ open: "false" }).filtros)).toBe(true);
  });
});

describe("a situação é um select só, escrevendo em open ou em status", () => {
  it("lê da URL — status vence na exibição", () => {
    expect(situacaoDaUrl(new URLSearchParams("open=true"))).toBe("open:true");
    expect(situacaoDaUrl(new URLSearchParams("open=true&status=aberta"))).toBe("status:aberta");
    expect(situacaoDaUrl(new URLSearchParams(""))).toBe("");
  });

  it("trocar limpa as duas chaves, escreve uma e volta à página 1", () => {
    expect(comSituacao(`open=true&page=4&unit_id=${UNIDADE}`, "status:concluida")).toBe(`?unit_id=${UNIDADE}&status=concluida`);
    expect(comSituacao("status=aberta&q=ar", "")).toBe("?q=ar");
    expect(comSituacao("status=aberta", "")).toBe("");
  });
});
