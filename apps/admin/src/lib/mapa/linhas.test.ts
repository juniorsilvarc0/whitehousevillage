import { describe, expect, it } from "vitest";

import type { Produto, UnidadeDaComposicao } from "@/lib/api/comercial";

import { janelaDe } from "./janela";
import { alinharCelulas, derivarLinhaSintetica, filtrarOcupadas, montarGrupos } from "./linhas";
import type { CelulaDoMapa, LinhaDoMapa, StatusDaCelula } from "./tipos";

const JANELA = janelaDe("2026-01-01", 4);

function celula(date: string, status: StatusDaCelula, extra: Partial<CelulaDoMapa> = {}): CelulaDoMapa {
  return {
    date,
    status,
    date_type: "normal",
    stay_block_id: null,
    reservation_id: null,
    reservation_code: null,
    guest_name: null,
    ...extra,
  };
}

function produto(id: string, code: string, consumes: Produto["consumes"], sort: number): Produto {
  return {
    id,
    code,
    name: code,
    capacity: 6,
    consumes,
    cleaning_fee_cents: 0,
    description: null,
    sort_order: sort,
    active: true,
    created_at: "",
    updated_at: "",
  };
}

const membro = (unit_id: string, unit_code: string): UnidadeDaComposicao => ({
  unit_id,
  unit_code,
  unit_name: unit_code,
  active: true,
});

function linhaDoMapa(unit_id: string, unit_code: string, dias: CelulaDoMapa[]): LinhaDoMapa {
  return { unit_id, unit_code, unit_name: unit_code, days: dias };
}

describe("alinharCelulas", () => {
  it("alinha por DATA, não por posição — é o que impede o mapa de sair torto", () => {
    // O refetch do tempo real pode voltar com a janela que o usuário tinha meio
    // segundo atrás. Alinhado por índice, o dia 3 apareceria pintado com a
    // ocupação do dia 1: desalinhamento silencioso, o defeito mais caro
    // possível numa grade.
    const alinhadas = alinharCelulas(
      [celula("2026-01-03", "confirmed"), celula("2026-01-01", "hold")],
      JANELA,
    );

    expect(alinhadas.map((c) => c.status)).toEqual(["hold", "livre", "confirmed", "livre"]);
    expect(alinhadas.map((c) => c.date)).toEqual([
      "2026-01-01",
      "2026-01-02",
      "2026-01-03",
      "2026-01-04",
    ]);
  });

  it("descarta o dia que caiu fora da janela em vez de empurrar os outros", () => {
    const alinhadas = alinharCelulas([celula("2025-12-30", "confirmed")], JANELA);
    expect(alinhadas.every((c) => c.status === "livre")).toBe(true);
  });
});

describe("derivarLinhaSintetica — a linha da casa inteira", () => {
  const membros = (statuses: [StatusDaCelula, string | null][][]) =>
    statuses.map((porDia, i) => ({
      codigo: `U-0${i + 1}`,
      celulas: porDia.map(([status, reserva], dia) =>
        celula(JANELA.dias[dia]!, status, { reservation_id: reserva, reservation_code: reserva }),
      ),
    }));

  it("as oito contando a mesma história viram a venda da casa inteira", () => {
    const linha = derivarLinhaSintetica(
      membros([
        [["confirmed", "r1"], ["confirmed", "r1"], ["livre", null], ["livre", null]],
        [["confirmed", "r1"], ["confirmed", "r1"], ["livre", null], ["livre", null]],
      ]),
      JANELA,
    );

    expect(linha[0]).toMatchObject({ status: "confirmed", reservation_id: "r1", bloqueadaPor: [] });
    expect(linha[2]).toMatchObject({ status: "livre" });
  });

  it("uma unidade ocupada por outro hóspede fecha a casa — com o NOME de quem fecha", () => {
    // A resposta que a gestão precisa antes de prometer a casa a um evento não
    // é "indisponível": é "a U-02 está vendida".
    const linha = derivarLinhaSintetica(
      membros([
        [["livre", null]],
        [["confirmed", "r7"]],
      ]),
      janelaDe("2026-01-01", 1),
    );

    expect(linha[0]).toMatchObject({ status: "parcial", bloqueadaPor: ["U-02"] });
  });

  it("`checked_out` conta como LIVRE: a estadia terminou e a data voltou ao estoque", () => {
    // Contá-la como ocupada fecharia para a venda uma noite que a venda aceita
    // — `completed` está fora da EXCLUDE do banco justamente por isso.
    const linha = derivarLinhaSintetica(
      membros([
        [["checked_out", "r1"]],
        [["livre", null]],
      ]),
      janelaDe("2026-01-01", 1),
    );

    expect(linha[0]?.status).toBe("livre");
  });

  it("a casa inteira em manutenção é manutenção, não 'parcial'", () => {
    const linha = derivarLinhaSintetica(
      membros([
        [["maintenance", null]],
        [["maintenance", null]],
      ]),
      janelaDe("2026-01-01", 1),
    );

    expect(linha[0]?.status).toBe("maintenance");
  });
});

describe("montarGrupos", () => {
  const produtos = [
    produto("p-ap", "AP2S", "one_member", 1),
    produto("p-cob", "COB", "one_member", 2),
    produto("p-casa", "COMPLETA", "all_members", 9),
  ];

  const composicoes = {
    "p-ap": [membro("u-ap2", "AP-02"), membro("u-ap1", "AP-01")],
    "p-cob": [membro("u-cob", "COB-01")],
    "p-casa": [membro("u-ap1", "AP-01"), membro("u-ap2", "AP-02"), membro("u-cob", "COB-01")],
  };

  const linhas = [
    linhaDoMapa("u-ap1", "AP-01", [celula("2026-01-01", "confirmed", { stay_block_id: "b1", reservation_id: "r1" })]),
    linhaDoMapa("u-ap2", "AP-02", []),
    linhaDoMapa("u-cob", "COB-01", []),
    linhaDoMapa("u-orfa", "XX-99", []),
  ];

  const grupos = montarGrupos({ produtos, composicoes, linhas, janela: JANELA });

  it("agrupa por produto, em sort_order, com as unidades em ordem de código", () => {
    expect(grupos.map((g) => g.nome)).toEqual(["AP2S", "COB", "Fora de qualquer produto", "COMPLETA"]);
    expect(grupos[0]?.linhas.map((l) => l.codigo)).toEqual(["AP-01", "AP-02"]);
  });

  it("a unidade que está em dois produtos aparece UMA vez, no primeiro deles", () => {
    // AP-01 pertence ao Apartamento 2 Suítes E à Completa, de propósito. Como
    // LINHA ela é uma só — a Completa é a linha derivada, não uma repetição.
    const todasAsLinhas = grupos.flatMap((g) => g.linhas.map((l) => l.codigo));
    expect(todasAsLinhas.filter((c) => c === "AP-01")).toHaveLength(1);
  });

  it("unidade fora de toda composição continua no mapa, num grupo que a denuncia", () => {
    // Ela continua ocupável (manutenção, uso do proprietário); some do mapa
    // seria pior. O nome do grupo é o aviso de inventário incompleto.
    const orfas = grupos.find((g) => g.unitTypeId === "sem-produto");
    expect(orfas?.linhas.map((l) => l.codigo)).toEqual(["XX-99"]);
  });

  it("a Completa vira UMA linha sintética, sem unit_id — não há o que bloquear nela", () => {
    const casa = grupos.at(-1);
    expect(casa?.linhas).toHaveLength(1);
    expect(casa?.linhas[0]).toMatchObject({ tipo: "sintetica", unitId: null, unitTypeId: "p-casa" });
    // AP-01 vendida em 01/01 ⇒ a casa inteira não é vendável nesse dia.
    expect(casa?.linhas[0]?.celulas[0]?.status).toBe("parcial");
  });

  it("cada linha carrega o PRODUTO dela, que é de onde sai a tarifa do hover", () => {
    // Preço é do produto, nunca da unidade: a mesma AP-01 custa uma coisa
    // vendida como Apartamento 2 Suítes e outra dentro da Completa.
    expect(grupos[0]?.linhas[0]?.unitTypeId).toBe("p-ap");
  });
});

describe("filtrarOcupadas", () => {
  it("esconde as linhas sem ocupação e os grupos que ficaram vazios", () => {
    const grupos = montarGrupos({
      produtos: [produto("p", "AP", "one_member", 1)],
      composicoes: { p: [membro("u1", "AP-01"), membro("u2", "AP-02")] },
      linhas: [
        linhaDoMapa("u1", "AP-01", [celula("2026-01-02", "hold", { stay_block_id: "b" })]),
        linhaDoMapa("u2", "AP-02", []),
      ],
      janela: JANELA,
    });

    const filtrados = filtrarOcupadas(grupos);
    expect(filtrados[0]?.linhas.map((l) => l.codigo)).toEqual(["AP-01"]);
  });
});
