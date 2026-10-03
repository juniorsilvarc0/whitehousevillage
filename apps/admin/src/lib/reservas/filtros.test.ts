import { describe, expect, it } from "vitest";

import {
  aplicar,
  lerFiltros,
  ORDENACAO_PADRAO,
  paraConsulta,
  temRecorte,
} from "@/lib/reservas/filtros";

/**
 * A query string é **entrada de usuário**: alguém edita a URL à mão, alguém
 * cola um link antigo depois de um recorte mudar de nome, alguém encaminha o
 * endereço da página 4 de uma busca que agora tem duas páginas.
 *
 * O que se protege aqui é a promessa de que nada disso vira `422` da API nem
 * uma lista vazia sem explicação: o valor impossível é descartado, e o descarte
 * é **dito**.
 */
describe("lerFiltros", () => {
  it("mantém os estados válidos e avisa sobre o que descartou", () => {
    const { filtros, avisos } = lerFiltros({ status: "hold,vendida,confirmed" });
    expect(filtros.status).toBe("hold,confirmed");
    expect(avisos.join(" ")).toContain("vendida");
  });

  it("descarta data fora do formato em vez de mandá-la para a API", () => {
    const { filtros, avisos } = lerFiltros({ from: "20/12/2026" });
    expect(filtros.from).toBe("");
    expect(avisos.join(" ")).toContain("não é uma data válida");
  });

  /**
   * `to` antes de `from` faz o intervalo `[from, to)` ser vazio: a lista viria
   * em branco e o operador leria "nenhuma reserva" para um recorte que não
   * existe. Soltar a ponta final e dizer isso é melhor do que mostrar o vazio.
   */
  it("solta a ponta final quando ela é anterior à inicial", () => {
    const { filtros, avisos } = lerFiltros({ from: "2026-12-20", to: "2026-12-01" });
    expect(filtros.from).toBe("2026-12-20");
    expect(filtros.to).toBe("");
    expect(avisos.join(" ")).toContain("anterior");
  });

  it("recusa identificador de produto que não é UUID", () => {
    const { filtros, avisos } = lerFiltros({ unit_type_id: "cobertura" });
    expect(filtros.unit_type_id).toBe("");
    expect(avisos.join(" ")).toContain("não foi reconhecido");
  });

  it("aceita UUID de produto sem reclamar", () => {
    const id = "e300c73a-59a3-4604-a462-919ec7e04af8";
    const { filtros, avisos } = lerFiltros({ unit_type_id: id });
    expect(filtros.unit_type_id).toBe(id);
    expect(avisos).toHaveLength(0);
  });

  it("cai na primeira página e na ordenação padrão diante de lixo", () => {
    const { filtros, avisos } = lerFiltros({ page: "-3", sort: "preco" });
    expect(filtros.page).toBe(1);
    expect(filtros.sort).toBe(ORDENACAO_PADRAO);
    expect(avisos).toHaveLength(2);
  });

  it("não trata a página como recorte — só filtro conta", () => {
    expect(temRecorte(lerFiltros({ page: "3" }).filtros)).toBe(false);
    expect(temRecorte(lerFiltros({ q: "WH-2026" }).filtros)).toBe(true);
  });
});

describe("paraConsulta", () => {
  it("omite as chaves vazias e sempre manda página, tamanho e ordenação", () => {
    const consulta = paraConsulta(lerFiltros({ status: "hold" }).filtros);
    expect(consulta).toEqual({ page: 1, per_page: 25, sort: ORDENACAO_PADRAO, status: "hold" });
    expect("q" in consulta).toBe(false);
    expect("from" in consulta).toBe(false);
  });
});

describe("aplicar", () => {
  /**
   * Sem isto, quem está na página 4 e aperta "Pré-reservas" cai numa página 4
   * que não existe no recorte novo e lê "nenhuma reserva" — a lista parece
   * vazia justamente quando o filtro acabou de acertar.
   */
  it("volta para a primeira página ao mexer em qualquer filtro", () => {
    const atual = new URLSearchParams("page=4&status=confirmed");
    expect(aplicar(atual, "status", "hold")).toBe("?status=hold");
  });

  it("preserva a página quando é a própria página que muda", () => {
    const atual = new URLSearchParams("page=4&status=confirmed");
    expect(aplicar(atual, "page", "5")).toBe("?page=5&status=confirmed");
  });

  it("valor vazio remove a chave, e sem chave nenhuma a URL fica limpa", () => {
    expect(aplicar(new URLSearchParams("status=hold"), "status", "")).toBe("");
  });
});
