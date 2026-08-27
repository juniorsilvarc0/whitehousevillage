import { describe, expect, it } from "vitest";

import { filtrosDosParametros, paraQueryString } from "./filtros";
import { DIAS_MAXIMO, DIAS_PADRAO, deslocar, indiceDoDia, janelaDe, limitarDias } from "./janela";
import { indicesDe, intervaloVisivel } from "./virtualizacao";

const HOJE = "2026-08-27";

describe("janelaDe", () => {
  it("`to` é EXCLUSIVO, como toda faixa de estadia do sistema", () => {
    const janela = janelaDe("2026-12-20", 3);
    expect(janela.dias).toEqual(["2026-12-20", "2026-12-21", "2026-12-22"]);
    expect(janela.to).toBe("2026-12-23");
  });

  it("atravessa a virada do ano sem perder um dia", () => {
    expect(janelaDe("2026-12-30", 4).dias).toEqual([
      "2026-12-30",
      "2026-12-31",
      "2027-01-01",
      "2027-01-02",
    ]);
  });

  it("limita a faixa ao teto que a API declara — o link com ?dias=5000 abre um mapa", () => {
    expect(limitarDias(5000)).toBe(DIAS_MAXIMO);
    expect(limitarDias(1)).toBe(7);
    expect(limitarDias(Number.NaN)).toBe(DIAS_PADRAO);
  });
});

describe("indiceDoDia", () => {
  const janela = janelaDe("2026-01-01", 10);

  it("acha a coluna por diferença de datas, não por varredura", () => {
    expect(indiceDoDia(janela, "2026-01-05")).toBe(4);
  });

  it("devolve -1 fora da janela", () => {
    expect(indiceDoDia(janela, "2025-12-31")).toBe(-1);
    expect(indiceDoDia(janela, "2026-01-11")).toBe(-1);
  });
});

describe("deslocar", () => {
  it("move mantendo o tamanho", () => {
    const janela = deslocar(janelaDe("2026-01-01", 30), 30);
    expect(janela.from).toBe("2026-01-31");
    expect(janela.dias).toHaveLength(30);
  });
});

describe("filtros na query string", () => {
  it("parâmetro ausente cai no padrão: uma semana de passado à mostra", () => {
    const filtros = filtrosDosParametros({}, HOJE);
    expect(filtros).toEqual({ from: "2026-08-20", dias: 90, unitTypeId: null, somenteOcupadas: false });
  });

  it("parâmetro impossível não derruba a tela — vira o padrão", () => {
    // Um mapa que responde erro porque alguém colou `?dias=abc` num Slack é um
    // mapa que ninguém compartilha.
    expect(filtrosDosParametros({ dias: "abc", from: "ontem" }, HOJE)).toMatchObject({
      from: "2026-08-20",
      dias: 90,
    });
  });

  it("omite o que é padrão — link com `?dias=90` envelhece mal", () => {
    expect(paraQueryString(filtrosDosParametros({}, HOJE), HOJE)).toBe("");
  });

  it("dá a volta: serializar e reler devolve os mesmos filtros", () => {
    const filtros = { from: "2026-12-20", dias: 30, unitTypeId: "p-1", somenteOcupadas: true };
    const busca = paraQueryString(filtros, HOJE);
    const params = Object.fromEntries(new URLSearchParams(busca));
    expect(filtrosDosParametros(params, HOJE)).toEqual(filtros);
  });
});

describe("intervaloVisivel", () => {
  it("pinta só a janela de rolagem, com overscan dos dois lados", () => {
    const { primeiro, ultimo } = intervaloVisivel({
      scrollLeft: 360,
      largura: 720,
      larguraDoDia: 36,
      total: 366,
    });
    expect(primeiro).toBe(5);
    expect(ultimo).toBe(35);
    expect(indicesDe({ primeiro, ultimo })).toHaveLength(31);
  });

  it("não passa do total nem entra em índice negativo", () => {
    expect(intervaloVisivel({ scrollLeft: 0, largura: 4000, larguraDoDia: 36, total: 20 })).toEqual({
      primeiro: 0,
      ultimo: 19,
    });
  });

  it("sem layout ainda pinta uma tela — grade em branco mediria melhor do que é", () => {
    const { primeiro, ultimo } = intervaloVisivel({
      scrollLeft: 0,
      largura: 0,
      larguraDoDia: 36,
      total: 90,
    });
    expect(primeiro).toBe(0);
    expect(ultimo).toBe(50);
  });
});
