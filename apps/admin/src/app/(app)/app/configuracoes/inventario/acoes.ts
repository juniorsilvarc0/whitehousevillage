"use server";

import { revalidatePath } from "next/cache";

import { apiFetch } from "@/lib/api/client";
import type { Produto, Unidade, UnidadeDaComposicao } from "@/lib/api/comercial";
import { detalhesDoZod } from "@/lib/acoes/campos";
import { exigir } from "@/lib/acoes/guarda";
import { tentar } from "@/lib/acoes/executar";
import { falha, type Resultado } from "@/lib/acoes/resultado";
import {
  ComposicaoFormulario,
  ProdutoFormulario,
  UnidadeFormulario,
  produtoParaEntrada,
  unidadeParaEntrada,
} from "./esquemas";

/**
 * Escrita do inventário.
 *
 * Editar usa `PUT`, não `PATCH`: o formulário carrega o cadastro inteiro, então
 * mandar o estado completo é o que ele de fato representa. `PATCH` existe para
 * quem quer mudar um campo sem conhecer os outros — não é o caso de um modal
 * que abriu com todos preenchidos, e usá-lo esconderia o campo que o usuário
 * apagou de propósito.
 */

const CAMINHO = "/app/configuracoes/inventario";

export async function salvarProduto(
  id: string | null,
  valores: ProdutoFormulario,
): Promise<Resultado<Produto>> {
  const recusa = await exigir("inventory", id ? "editar" : "criar");
  if (recusa) return recusa;

  const analise = ProdutoFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira os dados do produto.", detalhesDoZod(analise.error));
  }

  const resultado = await tentar(() =>
    apiFetch<Produto>(id ? `/unit-types/${id}` : "/unit-types", {
      method: id ? "PUT" : "POST",
      body: produtoParaEntrada(analise.data),
    }),
  );

  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}

/**
 * Desativa — nunca apaga. Produto referenciado por reserva não pode sumir sem
 * levar o histórico junto, e o `409 RESOURCE_IN_USE` do contrato é o que
 * acontece quando ainda há reserva viva.
 */
export async function desativarProduto(id: string): Promise<Resultado<null>> {
  const recusa = await exigir("inventory", "excluir");
  if (recusa) return recusa;

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/unit-types/${id}`, { method: "DELETE" });
    return null;
  });

  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}

export async function salvarUnidade(
  id: string | null,
  valores: UnidadeFormulario,
): Promise<Resultado<Unidade>> {
  const recusa = await exigir("inventory", id ? "editar" : "criar");
  if (recusa) return recusa;

  const analise = UnidadeFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira os dados da unidade.", detalhesDoZod(analise.error));
  }

  const resultado = await tentar(() =>
    apiFetch<Unidade>(id ? `/units/${id}` : "/units", {
      method: id ? "PUT" : "POST",
      body: unidadeParaEntrada(analise.data),
    }),
  );

  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}

export async function desativarUnidade(id: string): Promise<Resultado<null>> {
  const recusa = await exigir("inventory", "excluir");
  if (recusa) return recusa;

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/units/${id}`, { method: "DELETE" });
    return null;
  });

  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}

/**
 * Substitui a composição inteira — o que não vier deixa de fazer parte.
 *
 * A lista sai **ordenada pelos códigos das unidades** porque é essa a ordem em
 * que o servidor insere as linhas de `stay_blocks`, e duas transações
 * concorrentes que travem as mesmas unidades em ordens diferentes fecham em
 * deadlock. Aqui a ordem não muda o resultado gravado; ela mantém o painel
 * falando a mesma língua do resto do sistema, e é de graça.
 */
export async function salvarComposicao(
  produtoId: string,
  selecionadas: { id: string; code: string }[],
): Promise<Resultado<UnidadeDaComposicao[]>> {
  const recusa = await exigir("inventory", "editar");
  if (recusa) return recusa;

  const porCodigo = [...selecionadas].sort((a, b) => a.code.localeCompare(b.code, "pt-BR"));
  const ordenadas = [...new Set(porCodigo.map((u) => u.id))];

  const analise = ComposicaoFormulario.safeParse({ unit_ids: ordenadas });
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira a composição.", detalhesDoZod(analise.error));
  }

  const resultado = await tentar(() =>
    apiFetch<UnidadeDaComposicao[]>(`/unit-types/${produtoId}/members`, {
      method: "PUT",
      body: { unit_ids: analise.data.unit_ids },
    }),
  );

  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}
