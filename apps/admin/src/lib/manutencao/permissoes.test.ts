import { describe, expect, it } from "vitest";

import type { Acao, Permissao } from "@/lib/api/types";
import { botoesDaOrdem, permissoesDaManutencao, type BotoesDaOrdem } from "@/lib/manutencao/permissoes";
import type { OrdemDeManutencao } from "@/lib/manutencao/tipos";

/**
 * Os botões da ordem saem de `allowed_actions` e `editable` — **da resposta da
 * API** —, cruzados com a matriz em `maintenance`. Nunca do `status`.
 *
 * Os quatro estados abaixo usam exatamente o que o contrato descreve para cada
 * um (`maintenance.AllowedActions` / `EditableIn`). O último bloco prova o
 * contrário: se o painel lesse o `status`, uma ordem "aberta" com
 * `allowed_actions` vazio ainda mostraria "Iniciar".
 */

function matriz(acoes: Acao[], recurso = "maintenance"): Permissao[] {
  return acoes.map((action) => ({ resource: recurso, action, scope: "all" }));
}

const TUDO = permissoesDaManutencao(matriz(["ver", "criar", "editar", "excluir"]));

type Estado = Pick<OrdemDeManutencao, "status" | "allowed_actions" | "editable">;
const ABERTA: Estado = { status: "aberta", allowed_actions: ["start", "complete", "cancel"], editable: "tudo" };
const EM_ANDAMENTO: Estado = { status: "em_andamento", allowed_actions: ["complete", "cancel"], editable: "tudo" };
const CONCLUIDA: Estado = { status: "concluida", allowed_actions: [], editable: "so_custo" };
const CANCELADA: Estado = { status: "cancelada", allowed_actions: [], editable: "nada" };

const NENHUM: BotoesDaOrdem = {
  iniciar: false,
  concluir: false,
  cancelar: false,
  editarCadastro: false,
  gerenciarBloqueio: false,
  lancarCusto: false,
};

describe("os botões a partir de allowed_actions e editable", () => {
  it("aberta: inicia, conclui direto, cancela, edita e mexe no bloqueio", () => {
    expect(botoesDaOrdem(ABERTA, TUDO)).toEqual({
      iniciar: true,
      concluir: true,
      cancelar: true,
      editarCadastro: true,
      gerenciarBloqueio: true,
      lancarCusto: false,
    });
  });

  it("em andamento: não inicia de novo — o resto continua", () => {
    expect(botoesDaOrdem(EM_ANDAMENTO, TUDO)).toEqual({
      iniciar: false,
      concluir: true,
      cancelar: true,
      editarCadastro: true,
      gerenciarBloqueio: true,
      lancarCusto: false,
    });
  });

  it("concluída: só o custo — nem editar, nem bloqueio, nem transição", () => {
    expect(botoesDaOrdem(CONCLUIDA, TUDO)).toEqual({ ...NENHUM, lancarCusto: true });
  });

  it("cancelada: nada", () => {
    expect(botoesDaOrdem(CANCELADA, TUDO)).toEqual(NENHUM);
  });
});

describe("a máquina de estados é da API, não do painel", () => {
  it("ordem 'aberta' com allowed_actions vazio não ganha 'Iniciar' — o status não decide", () => {
    const b = botoesDaOrdem({ allowed_actions: [], editable: "tudo" }, TUDO);
    expect(b.iniciar).toBe(false);
    expect(b.concluir).toBe(false);
    expect(b.cancelar).toBe(false);
  });

  it("e o contrário: se a API um dia permitir iniciar em outro estado, o botão aparece sem mudar o painel", () => {
    expect(botoesDaOrdem({ allowed_actions: ["start"], editable: "nada" }, TUDO).iniciar).toBe(true);
  });

  it("editable manda na edição, independente das transições", () => {
    expect(botoesDaOrdem({ allowed_actions: ["start", "complete", "cancel"], editable: "so_custo" }, TUDO)).toMatchObject({
      editarCadastro: false,
      gerenciarBloqueio: false,
      lancarCusto: true,
    });
  });
});

describe("a matriz recorta o que a ordem permite", () => {
  it("só ver: nenhum botão, nem na ordem aberta", () => {
    expect(botoesDaOrdem(ABERTA, permissoesDaManutencao(matriz(["ver"])))).toEqual(NENHUM);
  });

  it("editar sem excluir: inicia, conclui e edita, mas não cancela (DELETE pede excluir)", () => {
    const b = botoesDaOrdem(ABERTA, permissoesDaManutencao(matriz(["ver", "editar"])));
    expect(b).toMatchObject({ iniciar: true, concluir: true, editarCadastro: true, cancelar: false });
  });

  it("excluir sem editar: só cancela", () => {
    expect(botoesDaOrdem(ABERTA, permissoesDaManutencao(matriz(["ver", "excluir"])))).toEqual({ ...NENHUM, cancelar: true });
  });

  it("inventory.goods inteiro não acende nada aqui — o recurso é maintenance", () => {
    expect(permissoesDaManutencao(matriz(["ver", "criar", "editar", "excluir"], "inventory.goods"))).toEqual({
      ver: false,
      criar: false,
      editar: false,
      excluir: false,
    });
  });
});
