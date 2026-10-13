import { describe, expect, it } from "vitest";

import {
  CustoFormulario,
  EdicaoFormulario,
  OrdemFormulario,
  custoParaAtualizar,
  custoParaConcluir,
  edicaoParaSubstituir,
  ordemParaCriar,
  ordemVazia,
} from "@/lib/manutencao/esquemas";

const UNIDADE = "11111111-1111-4111-8111-111111111111";
const COMODO = "22222222-2222-4222-8222-222222222222";
const BEM = "33333333-3333-4333-8333-333333333333";
const AVARIA = "44444444-4444-4444-8444-444444444444";

const base = ordemVazia({ unit_id: UNIDADE, title: "Ar da suíte não gela" });

function problemasEm(resultado: ReturnType<typeof OrdemFormulario.safeParse>, campo: string): string[] {
  if (resultado.success) return [];
  return resultado.error.issues.filter((i) => i.path.join(".") === campo).map((i) => i.message);
}

describe("o formulário de criação", () => {
  it("título só com espaços é recusado — o contrato conta sem as pontas", () => {
    const r = OrdemFormulario.safeParse({ ...base, title: "    " });
    expect(r.success).toBe(false);
    expect(problemasEm(r, "title")).toHaveLength(1);
  });

  it("título com espaço nas pontas passa e vai aparado", () => {
    const r = OrdemFormulario.safeParse({ ...base, title: "  Pintar a GV-01  " });
    expect(r.success).toBe(true);
    if (r.success) expect(ordemParaCriar(r.data).title).toBe("Pintar a GV-01");
  });

  it("unidade é obrigatória", () => {
    expect(problemasEm(OrdemFormulario.safeParse({ ...base, unit_id: "" }), "unit_id")).toHaveLength(1);
  });

  it("custo zero é recusado, em qualquer grafia, com a saída escrita", () => {
    for (const zero of ["0", "0,00", "R$ 0,00"]) {
      const msgs = problemasEm(OrdemFormulario.safeParse({ ...base, cost: zero }), "cost");
      expect(msgs, zero).toHaveLength(1);
      expect(msgs[0]).toMatch(/deixe o campo vazio/i);
    }
  });

  it("custo que não é dinheiro é recusado em vez de virar zero ou null calado", () => {
    expect(problemasEm(OrdemFormulario.safeParse({ ...base, cost: "trezentos" }), "cost")).toHaveLength(1);
  });

  it("o custo vai em centavos INTEIROS — sem ponto flutuante no meio", () => {
    for (const [digitado, centavos] of [
      ["350", 35_000],
      ["350,00", 35_000],
      ["1.234,56", 123_456],
      ["0,10", 10],
      ["19.99", 1_999],
    ] as const) {
      const corpo = ordemParaCriar({ ...base, cost: digitado });
      expect(corpo.cost_cents, digitado).toBe(centavos);
      expect(Number.isInteger(corpo.cost_cents), digitado).toBe(true);
    }
  });

  it("custo vazio vai como null — 'ainda não lançado', não zero", () => {
    expect(ordemParaCriar(base).cost_cents).toBeNull();
  });

  it("prioridade padrão é normal", () => {
    expect(ordemParaCriar(base).priority).toBe("normal");
  });
});

describe("o corpo do POST", () => {
  it("cômodo, bem e avaria vazios NÃO vão — com avaria, ausente vem dela; null seria 422", () => {
    const corpo = ordemParaCriar(base);
    expect("room_id" in corpo).toBe(false);
    expect("item_id" in corpo).toBe(false);
    expect("issue_id" in corpo).toBe(false);
  });

  it("vindo da avaria, os quatro ids vão", () => {
    const corpo = ordemParaCriar({ ...base, room_id: COMODO, item_id: BEM, issue_id: AVARIA });
    expect(corpo).toMatchObject({ unit_id: UNIDADE, room_id: COMODO, item_id: BEM, issue_id: AVARIA });
  });

  it("sem 'Bloquear calendário', não há block no corpo — mesmo com datas digitadas", () => {
    const corpo = ordemParaCriar({ ...base, bloquear: false, block_from: "2026-11-10", block_to: "2026-11-15" });
    expect("block" in corpo).toBe(false);
  });

  it("com a caixa marcada, o período vai como {from, to}", () => {
    const corpo = ordemParaCriar({ ...base, bloquear: true, block_from: "2026-11-10", block_to: "2026-11-15" });
    expect(corpo.block).toEqual({ from: "2026-11-10", to: "2026-11-15" });
  });

  it("descrição vazia vai como null", () => {
    expect(ordemParaCriar({ ...base, description: "   " }).description).toBeNull();
  });
});

describe("o período: só a forma — a regra é da API", () => {
  it("com a caixa marcada, as duas datas têm de ser datas", () => {
    const r = OrdemFormulario.safeParse({ ...base, bloquear: true, block_from: "", block_to: "amanhã" });
    expect(problemasEm(r, "block_from")).toHaveLength(1);
    expect(problemasEm(r, "block_to")).toHaveLength(1);
  });

  it("data no passado, fim antes do início e 2 anos de bloqueio PASSAM no painel — quem recusa é o Replan", () => {
    // Se isto reprovar, alguém copiou a regra do domínio para o front: a
    // segunda cópia diverge da primeira (e o "hoje" do navegador não é o da casa).
    for (const [de, ate] of [
      ["2020-01-01", "2020-01-05"],
      ["2026-11-15", "2026-11-10"],
      ["2026-11-10", "2028-11-10"],
    ]) {
      expect(OrdemFormulario.safeParse({ ...base, bloquear: true, block_from: de, block_to: ate }).success, `${de}→${ate}`).toBe(
        true,
      );
    }
  });
});

describe("editar, lançar custo e concluir", () => {
  const edicao = { room_id: COMODO, item_id: BEM, title: "Trocar o chuveiro", description: "", priority: "alta" as const, cost: "" };

  it("PUT com avaria ligada não manda cômodo nem bem — são os dela", () => {
    const corpo = edicaoParaSubstituir(EdicaoFormulario.parse(edicao), true);
    expect("room_id" in corpo).toBe(false);
    expect("item_id" in corpo).toBe(false);
  });

  it("PUT sem avaria manda os dois — vazio é null, que limpa", () => {
    const corpo = edicaoParaSubstituir(EdicaoFormulario.parse({ ...edicao, room_id: "", item_id: BEM }), false);
    expect(corpo.room_id).toBeNull();
    expect(corpo.item_id).toBe(BEM);
  });

  it("PATCH do custo leva só cost_cents (qualquer outra chave, na concluída, é 409)", () => {
    expect(custoParaAtualizar(CustoFormulario.parse({ cost: "420,50" }))).toEqual({ cost_cents: 42_050 });
    expect(custoParaAtualizar(CustoFormulario.parse({ cost: "" }))).toEqual({ cost_cents: null });
  });

  it("concluir sem custo não manda corpo — o custo já lançado fica", () => {
    expect(custoParaConcluir({ cost: "" })).toBeUndefined();
    expect(custoParaConcluir({ cost: "99,90" })).toEqual({ cost_cents: 9_990 });
  });

  it("custo zero na conclusão também é recusado", () => {
    expect(CustoFormulario.safeParse({ cost: "0,00" }).success).toBe(false);
  });
});
