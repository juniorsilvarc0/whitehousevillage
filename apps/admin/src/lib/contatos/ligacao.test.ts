import { describe, expect, it } from "vitest";

import { linkDeLigacao } from "@/lib/contatos/ligacao";

/**
 * O único lugar do painel que monta `tel:`. A máscara das coleções
 * (`+*********0000`) não é E.164, e um `tel:` com ela disca o nada — foi o
 * botão de ligar do lead até a D11.
 */
describe("linkDeLigacao", () => {
  it("E.164 vira `tel:`", () => {
    expect(linkDeLigacao("+5585999990000")).toBe("tel:+5585999990000");
  });

  it("telefone mascarado não vira link", () => {
    expect(linkDeLigacao("+*********0000")).toBeNull();
  });

  it("sem telefone, ou fora do E.164, também não", () => {
    expect(linkDeLigacao(null)).toBeNull();
    expect(linkDeLigacao(undefined)).toBeNull();
    expect(linkDeLigacao("")).toBeNull();
    expect(linkDeLigacao("85999990000")).toBeNull();
  });
});
