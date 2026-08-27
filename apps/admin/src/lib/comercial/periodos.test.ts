import { describe, expect, it } from "vitest";

import { sobreposicoes } from "@/lib/comercial/periodos";
import type { PeriodoEspecial } from "@/lib/api/comercial";

/**
 * A sobreposição entre períodos é **regra**, não defeito — o Réveillon mora
 * dentro da alta temporada, e a tabela não tem constraint de exclusão por isso.
 *
 * A tela mostra quem cruza com quem para evitar o reflexo errado: alguém
 * "consertar" o cadastro encolhendo a alta temporada para não encostar no
 * Réveillon, e com isso mudar o preço de todas as noites entre os dois.
 *
 * As pontas são **inclusivas nos dois lados**, ao contrário da estadia. Encostar
 * é sobrepor: um período que termina em 20/12 e outro que começa em 20/12
 * disputam a noite de 20/12.
 */

function periodo(
  id: string,
  starts_on: string,
  ends_on: string,
  kind: PeriodoEspecial["kind"] = "alta",
  active = true,
): PeriodoEspecial {
  return { id, name: id, kind, starts_on, ends_on, active };
}

describe("sobreposicoes", () => {
  it("acha o réveillon dentro da alta temporada, dos dois lados", () => {
    const alta = periodo("alta", "2026-12-15", "2027-02-28", "alta");
    const reveillon = periodo("reveillon", "2026-12-28", "2027-01-02", "reveillon");

    const mapa = sobreposicoes([alta, reveillon]);
    expect(mapa.get("alta")?.map((p) => p.id)).toEqual(["reveillon"]);
    expect(mapa.get("reveillon")?.map((p) => p.id)).toEqual(["alta"]);
  });

  it("conta como sobreposição quando as pontas apenas se encostam", () => {
    // Inclusivo nas duas pontas: 20/12 pertence aos dois períodos.
    const mapa = sobreposicoes([
      periodo("a", "2026-12-01", "2026-12-20"),
      periodo("b", "2026-12-20", "2026-12-31"),
    ]);
    expect(mapa.size).toBe(2);
  });

  it("não vê sobreposição entre períodos separados por um dia", () => {
    const mapa = sobreposicoes([
      periodo("a", "2026-12-01", "2026-12-19"),
      periodo("b", "2026-12-21", "2026-12-31"),
    ]);
    expect(mapa.size).toBe(0);
  });

  it("ignora período inativo — ele não classifica noite nenhuma", () => {
    const mapa = sobreposicoes([
      periodo("ativo", "2026-12-15", "2027-02-28"),
      periodo("desligado", "2026-12-28", "2027-01-02", "reveillon", false),
    ]);
    expect(mapa.size).toBe(0);
  });

  it("nunca aponta um período para ele mesmo", () => {
    const mapa = sobreposicoes([periodo("sozinho", "2026-12-01", "2026-12-31")]);
    expect(mapa.size).toBe(0);
  });
});
