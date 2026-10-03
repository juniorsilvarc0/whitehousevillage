import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { GradeDeTarifas, type ProdutoDaGrade } from "@/components/comercial/grade-de-tarifas";
import type { GradeGravada, Tarifa, TarifaDaGrade } from "@/lib/api/comercial";
import type { Resultado } from "@/lib/acoes/resultado";

/**
 * A grade de tarifas é o componente de decisão do tarifário: ele decide **o
 * que** vai para o servidor, e o `POST /rates/bulk` substitui a grade inteira.
 *
 * O que se protege:
 *
 * 1. **Manda a grade inteira dos produtos da tela, não o que mudou.** Dentro do
 *    escopo declarado, o endpoint remove o par que não vier no corpo. Enviar só
 *    as células alteradas apagaria todas as outras — e o próximo orçamento
 *    morreria em `RATE_NOT_FOUND` num tipo de noite que ninguém tocou.
 * 1b. **O escopo vai declarado e cobre exatamente as linhas da grade.** É ele
 *    que impede a gravação de um produto apagar a tarifa de outro (medido em
 *    26/08/2026: 6 células salvas, 18 apagadas) e é ele que permite zerar um
 *    produto de propósito.
 * 2. **Célula esvaziada é remoção, e a tela avisa antes — e depois.** É a única
 *    operação destrutiva da tela, e é invisível: a diferença entre "não mexi" e
 *    "apaguei" são dois caracteres. O que o servidor removeu volta em `meta` e
 *    aparece para quem salvou.
 * 3. **Centavo é centavo.** `1.234,56` tem que virar `123456`, não
 *    `123456.00000000001` — um centavo errado numa tabela comercial é uma
 *    reunião com os proprietários.
 * 4. **Nada sai enquanto houver célula inválida**, e a grade inteira vazia é
 *    recusada em vez de apagar o tarifário.
 */

const PRODUTOS: ProdutoDaGrade[] = [
  { id: "p-ap2s", code: "AP2S", name: "Apartamento 2 Suítes", active: true },
  { id: "p-cob", code: "COB", name: "Cobertura", active: true },
];

function tarifa(unitTypeId: string, dateType: Tarifa["date_type"], centavos: number): Tarifa {
  return {
    id: `${unitTypeId}-${dateType}`,
    rate_table_id: "t1",
    unit_type_id: unitTypeId,
    unit_type_code: unitTypeId,
    date_type: dateType,
    amount_cents: centavos,
  };
}

/** A grade V1 reduzida a dois produtos, com uma célula faltando de propósito. */
const TARIFAS: Tarifa[] = [
  tarifa("p-ap2s", "normal", 85_000),
  tarifa("p-ap2s", "fds", 110_000),
  tarifa("p-ap2s", "reveillon", 320_000),
  tarifa("p-cob", "normal", 190_000),
  tarifa("p-cob", "fds", 240_000),
];

function celula(produto: string, tipo: string): HTMLInputElement {
  return screen.getByLabelText(`${produto} — ${tipo}`) as HTMLInputElement;
}

function salvarBotao(): HTMLButtonElement {
  return screen.getByRole("button", { name: /salvar os preços/i }) as HTMLButtonElement;
}

function respostaOk(escopo: string[], celulas: TarifaDaGrade[], removidas = 0): Resultado<GradeGravada> {
  return {
    ok: true,
    data: {
      tarifas: celulas.map((c) => tarifa(c.unit_type_id, c.date_type, c.amount_cents)),
      meta: {
        unit_type_ids: escopo,
        created: 0,
        updated: celulas.length,
        unchanged: 0,
        removed: removidas,
      },
    },
  };
}

function montar(
  aoSalvar = vi.fn(async (_id: string, escopo: string[], c: TarifaDaGrade[]) => respostaOk(escopo, c)),
) {
  render(
    <GradeDeTarifas
      tabelaId="t1"
      produtos={PRODUTOS}
      tarifas={TARIFAS}
      minimosPorTipo={{ fds: 2, reveillon: 4 }}
      podeEditar
      aoSalvar={aoSalvar}
    />,
  );
  return aoSalvar;
}

describe("GradeDeTarifas", () => {
  it("desenha a grade produto × tipo de data com a precedência e o mínimo à vista", () => {
    montar();

    // Seis tipos de data por produto, na ordem decrescente de precedência.
    expect(celula("Apartamento 2 Suítes", "Normal").value).toBe("850,00");
    expect(celula("Cobertura", "Fim de semana").value).toBe("2.400,00");
    // Célula sem tarifa fica vazia, não zerada: zero seria uma diária de graça.
    expect(celula("Cobertura", "Réveillon").value).toBe("");

    const texto = document.body.textContent ?? "";
    expect(texto, "a precedência precisa estar na própria tela").toContain("prioridade 1");
    expect(texto).toContain("mín. 4 noites");
  });

  it("começa sem nada para salvar", () => {
    montar();
    expect(salvarBotao().disabled, "grade intocada não tem o que salvar").toBe(true);
  });

  it("manda a grade INTEIRA, não só o que mudou", async () => {
    const aoSalvar = montar();

    fireEvent.change(celula("Apartamento 2 Suítes", "Normal"), { target: { value: "900,00" } });
    fireEvent.click(salvarBotao());

    await waitFor(() => expect(aoSalvar).toHaveBeenCalled());

    const [tabelaId, escopo, celulas] = aoSalvar.mock.calls[0]!;
    expect(tabelaId).toBe("t1");
    expect(escopo, "o escopo é a grade à vista — sem ele, esvaziar não apaga").toEqual(["p-ap2s", "p-cob"]);
    // Cinco células preenchidas na entrada, uma delas alterada — as cinco vão.
    expect(
      celulas,
      "mandar só a célula alterada apagaria as outras: o bulk substitui a grade inteira",
    ).toHaveLength(5);
    expect(celulas).toContainEqual({ unit_type_id: "p-ap2s", date_type: "normal", amount_cents: 90_000 });
    expect(celulas).toContainEqual({ unit_type_id: "p-cob", date_type: "fds", amount_cents: 240_000 });
  });

  it("converte reais em centavos sem passar por ponto flutuante", async () => {
    const aoSalvar = montar();

    fireEvent.change(celula("Cobertura", "Réveillon"), { target: { value: "7.500,45" } });
    fireEvent.click(salvarBotao());

    await waitFor(() => expect(aoSalvar).toHaveBeenCalled());
    const [, , celulas] = aoSalvar.mock.calls[0]!;
    const nova = celulas.find((c) => c.unit_type_id === "p-cob" && c.date_type === "reveillon");
    expect(nova?.amount_cents).toBe(750_045);
    expect(Number.isInteger(nova?.amount_cents)).toBe(true);
  });

  it("avisa que a célula esvaziada será removida da tabela", async () => {
    montar();

    fireEvent.change(celula("Apartamento 2 Suítes", "Fim de semana"), { target: { value: "" } });

    await waitFor(() => {
      expect(
        document.body.textContent,
        "esvaziar uma célula é apagar a tarifa; a tela precisa dizer isso antes de salvar",
      ).toContain("será removido");
    });
    expect(document.body.textContent).toContain("não poderão ser orçadas");
  });

  it("de fato remove a célula esvaziada do corpo enviado", async () => {
    const aoSalvar = montar();

    fireEvent.change(celula("Apartamento 2 Suítes", "Fim de semana"), { target: { value: "" } });
    fireEvent.click(salvarBotao());

    await waitFor(() => expect(aoSalvar).toHaveBeenCalled());
    const [, , celulas] = aoSalvar.mock.calls[0]!;
    expect(celulas).toHaveLength(4);
    expect(celulas.some((c) => c.unit_type_id === "p-ap2s" && c.date_type === "fds")).toBe(false);
  });

  it("recusa o envio enquanto houver célula inválida", async () => {
    const aoSalvar = montar();

    fireEvent.change(celula("Cobertura", "Normal"), { target: { value: "mil e duzentos" } });
    fireEvent.click(salvarBotao());

    const aviso = await screen.findByRole("alert");
    expect(aviso.textContent).toContain("valor inválido");
    expect(aoSalvar, "grade com célula ilegível não pode virar requisição").not.toHaveBeenCalled();
  });

  it("recusa diária zerada — o mínimo do contrato é um centavo", async () => {
    const aoSalvar = montar();

    fireEvent.change(celula("Cobertura", "Normal"), { target: { value: "0,00" } });
    fireEvent.click(salvarBotao());

    await screen.findByRole("alert");
    expect(aoSalvar).not.toHaveBeenCalled();
  });

  it("recusa apagar a grade inteira", async () => {
    const aoSalvar = montar();

    for (const [produto, tipo] of [
      ["Apartamento 2 Suítes", "Normal"],
      ["Apartamento 2 Suítes", "Fim de semana"],
      ["Apartamento 2 Suítes", "Réveillon"],
      ["Cobertura", "Normal"],
      ["Cobertura", "Fim de semana"],
    ] as const) {
      fireEvent.change(celula(produto, tipo), { target: { value: "" } });
    }

    fireEvent.click(salvarBotao());

    const aviso = await screen.findByRole("alert");
    expect(aviso.textContent).toContain("nenhum orçamento poderia ser feito");
    expect(aoSalvar).not.toHaveBeenCalled();
  });

  it("descarta as alterações e volta ao que o servidor mandou", async () => {
    montar();

    fireEvent.change(celula("Cobertura", "Normal"), { target: { value: "1,00" } });
    fireEvent.click(screen.getByRole("button", { name: /descartar/i }));

    await waitFor(() => expect(celula("Cobertura", "Normal").value).toBe("1.900,00"));
    expect(salvarBotao().disabled).toBe(true);
  });

  it("diz que o passado não muda, e só depois de salvar", async () => {
    montar();
    expect(document.body.textContent).not.toContain("Preços salvos");

    fireEvent.change(celula("Cobertura", "Normal"), { target: { value: "2.000,00" } });
    fireEvent.click(salvarBotao());

    await waitFor(() => {
      expect(document.body.textContent).toContain("Preços salvos");
    });
    expect(document.body.textContent).toContain("já feitos não mudam");
    // A resposta do servidor vira a nova base: não sobra nada por salvar.
    expect(salvarBotao().disabled).toBe(true);
  });

  it("mostra o erro pelo código quando o servidor recusa, e mantém o que foi digitado", async () => {
    const aoSalvar = vi.fn(async (_id: string, _escopo: string[], _c: TarifaDaGrade[]) => ({
      ok: false as const,
      code: "CODE_IN_USE" as const,
      message: "mensagem que a tela deve ignorar",
      details: {},
    }));
    render(
      <GradeDeTarifas
        tabelaId="t1"
        produtos={PRODUTOS}
        tarifas={TARIFAS}
        minimosPorTipo={{}}
        podeEditar
        aoSalvar={aoSalvar}
      />,
    );

    fireEvent.change(celula("Cobertura", "Normal"), { target: { value: "2.000,00" } });
    fireEvent.click(salvarBotao());

    const aviso = await screen.findByRole("alert");
    expect(aviso.textContent).not.toContain("mensagem que a tela deve ignorar");
    expect(celula("Cobertura", "Normal").value, "recusa não pode perder o que o usuário digitou").toBe("2.000,00");
  });

  it("declara o escopo com os produtos da grade — inclusive o que ficou sem nenhuma célula", async () => {
    const aoSalvar = montar();

    // A Cobertura fica sem nenhuma tarifa. Ela precisa continuar no escopo:
    // produto que some do corpo E do escopo não é zerado, é ignorado — e a tela
    // mostraria coluna vazia com a tarifa ainda viva no banco.
    for (const tipo of ["Normal", "Fim de semana"] as const) {
      fireEvent.change(celula("Cobertura", tipo), { target: { value: "" } });
    }
    fireEvent.click(salvarBotao());

    await waitFor(() => expect(aoSalvar).toHaveBeenCalled());
    const [, escopo, celulas] = aoSalvar.mock.calls[0]!;
    expect(escopo).toContain("p-cob");
    expect(celulas.some((c) => c.unit_type_id === "p-cob")).toBe(false);
  });

  it("mostra o que o servidor removeu, e não só que salvou", async () => {
    const aoSalvar = vi.fn(async (_id: string, escopo: string[], c: TarifaDaGrade[]) =>
      respostaOk(escopo, c, 2),
    );
    montar(aoSalvar);

    fireEvent.change(celula("Cobertura", "Normal"), { target: { value: "2.000,00" } });
    fireEvent.click(salvarBotao());

    await waitFor(() => {
      expect(
        document.body.textContent,
        "remoção que só aparece quando a venda falha é remoção invisível",
      ).toContain("2 removidos");
    });
  });

  it("não oferece edição a quem só pode ver", () => {
    render(
      <GradeDeTarifas
        tabelaId="t1"
        produtos={PRODUTOS}
        tarifas={TARIFAS}
        minimosPorTipo={{}}
        podeEditar={false}
        aoSalvar={vi.fn()}
      />,
    );

    expect(screen.queryByRole("button", { name: /salvar os preços/i })).toBeNull();
    expect(celula("Cobertura", "Normal").disabled).toBe(true);
  });
});
