import type { Permissao } from "@/lib/api/types";
import { can } from "@/lib/auth/permissions";
import type { OrdemDeManutencao } from "@/lib/manutencao/tipos";

/**
 * O que o perfil pode fazer nas ordens — lido da matriz, sempre em
 * `maintenance`. Esconder não é autorizar: cada action confere de novo e a API
 * recusa com 403.
 */
export type PermissoesDaManutencao = {
  ver: boolean;
  criar: boolean;
  editar: boolean;
  excluir: boolean;
};

export function permissoesDaManutencao(matriz: Permissao[]): PermissoesDaManutencao {
  return {
    ver: can(matriz, "maintenance", "ver"),
    criar: can(matriz, "maintenance", "criar"),
    editar: can(matriz, "maintenance", "editar"),
    excluir: can(matriz, "maintenance", "excluir"),
  };
}

/** Os gestos que a tela de detalhe oferece. */
export type BotoesDaOrdem = {
  iniciar: boolean;
  concluir: boolean;
  cancelar: boolean;
  /** Título, descrição, prioridade, cômodo, bem e custo — `PUT`. */
  editarCadastro: boolean;
  /** `PUT|DELETE /{id}/block`. */
  gerenciarBloqueio: boolean;
  /** Só o custo, por `PATCH {cost_cents}` — a ordem concluída. */
  lancarCusto: boolean;
};

/**
 * Os botões da ordem **a partir do que a API devolveu**: `allowed_actions` para
 * as transições e `editable` para a edição — cruzados com a matriz.
 *
 * O `status` não entra na decisão, de propósito. A máquina de estados mora em
 * `internal/domain/maintenance` (`AllowedActions`, `EditableIn`), e uma segunda
 * cópia aqui ("em andamento não inicia") divergiria da primeira no dia em que
 * só uma mudasse. O teste prova isso com uma ordem `aberta` cujo
 * `allowed_actions` vem vazio: nenhum botão de transição aparece.
 *
 * O par recurso × ação é o `x-rbac` de cada rota: `/start`, `/complete`, `PUT`,
 * `PATCH` e `/block` pedem `editar`; o `DELETE` (cancelar) pede `excluir`.
 */
export function botoesDaOrdem(
  ordem: Pick<OrdemDeManutencao, "allowed_actions" | "editable">,
  permissoes: Pick<PermissoesDaManutencao, "editar" | "excluir">,
): BotoesDaOrdem {
  const pode = (acao: OrdemDeManutencao["allowed_actions"][number]) => ordem.allowed_actions.includes(acao);
  return {
    iniciar: permissoes.editar && pode("start"),
    concluir: permissoes.editar && pode("complete"),
    cancelar: permissoes.excluir && pode("cancel"),
    editarCadastro: permissoes.editar && ordem.editable === "tudo",
    gerenciarBloqueio: permissoes.editar && ordem.editable === "tudo",
    lancarCusto: permissoes.editar && ordem.editable === "so_custo",
  };
}
