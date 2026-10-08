import type { Permissao } from "@/lib/api/types";
import { can } from "@/lib/auth/permissions";

/**
 * O que o perfil pode fazer no inventário de bens — lido da matriz, **sempre**
 * em `inventory.goods`.
 *
 * O recurso vizinho, `inventory`, é o cadastro comercial (unidades, produtos,
 * composição). Confundir os dois é o defeito que este módulo nasceu evitando:
 * a permissão que deixa a diarista salvar "contei 9 taças" não pode ser a que
 * apaga um produto que a casa vende (docs/db.md §11). Por isso a leitura mora
 * num lugar só, e `permissoes.test.tsx` prova que `inventory` sozinho não
 * acende botão nenhum aqui.
 *
 * Esconder não é autorizar: cada action confere de novo, e a API recusa com 403.
 */
export type PermissoesDoInventario = {
  ver: boolean;
  criar: boolean;
  editar: boolean;
  excluir: boolean;
};

export function permissoesDoInventario(matriz: Permissao[]): PermissoesDoInventario {
  return {
    ver: can(matriz, "inventory.goods", "ver"),
    criar: can(matriz, "inventory.goods", "criar"),
    editar: can(matriz, "inventory.goods", "editar"),
    excluir: can(matriz, "inventory.goods", "excluir"),
  };
}
