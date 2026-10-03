"use server";

import { revalidatePath } from "next/cache";

import { apiFetch, apiFetchComMeta } from "@/lib/api/client";
import type {
  GradeGravada,
  MetaDaGrade,
  MinimoDeNoites,
  TabelaDeTarifas,
  Tarifa,
  TarifaDaGrade,
  TipoDeData,
} from "@/lib/api/comercial";
import { detalhesDoZod } from "@/lib/acoes/campos";
import { exigir } from "@/lib/acoes/guarda";
import { tentar } from "@/lib/acoes/executar";
import { falha, type Resultado } from "@/lib/acoes/resultado";
import { MinimoFormulario, TabelaFormulario, minimoParaNoites, tabelaParaEntrada } from "./esquemas";

const CAMINHO = "/app/configuracoes/tarifario";

const TIPOS: readonly TipoDeData[] = ["normal", "fds", "feriado", "alta", "reveillon", "carnaval"];

/**
 * Salva, numa transação, a grade **dos produtos que a tela abriu**.
 *
 * `escopo` viaja separado das células porque são coisas diferentes:
 * `unit_type_ids` é *o que esta chamada reescreve* e `rates` é *o que fica*.
 * Produto no escopo sem nenhuma célula é zerado — e essa é a única forma de
 * apagar tarifa, de propósito: remoção tem que exigir nomear o alvo, nunca
 * acontecer por omissão. Produto fora do escopo não é tocado.
 *
 * O escopo é enviado **sempre**, mesmo quando seria dedutível de `rates`. Sem
 * ele, um produto cujas células foram todas esvaziadas simplesmente não
 * apareceria no corpo, e a API o deixaria como estava — a tela mostraria a
 * coluna vazia e a tarifa continuaria viva no banco.
 *
 * A revalidação **não** se limita a esta tela: mudar a tarifa muda o próximo
 * orçamento, e a tela de orçamento lê a tabela vigente. Revalidar as duas evita
 * o caso em que a gestão corrige um preço, abre o orçamento na aba ao lado e
 * fecha a venda pelo valor antigo.
 */
export async function salvarGrade(
  tabelaId: string,
  escopo: string[],
  celulas: TarifaDaGrade[],
): Promise<Resultado<GradeGravada>> {
  const recusa = await exigir("settings", "editar");
  if (recusa) return recusa;

  if (escopo.length === 0) {
    return falha("VALIDATION_ERROR", "Nenhum produto para salvar.", {
      unit_type_ids: "Escolha ao menos um produto na tabela de preços.",
    });
  }

  const noEscopo = new Set(escopo);

  // Par repetido é `422` do outro lado; recusar aqui dá a mensagem no lugar
  // certo em vez de um erro genérico com índice.
  const vistos = new Set<string>();
  for (const celula of celulas) {
    const chave = `${celula.unit_type_id}:${celula.date_type}`;
    if (vistos.has(chave)) {
      return falha("VALIDATION_ERROR", "Há dois preços para o mesmo produto e tipo de data.", { rates: chave });
    }
    vistos.add(chave);
    if (!TIPOS.includes(celula.date_type) || !Number.isInteger(celula.amount_cents) || celula.amount_cents < 1) {
      return falha("VALIDATION_ERROR", "Há um preço inválido na tabela.", { rates: chave });
    }
    // Célula de produto fora do escopo é `422` na API, e com razão: mandar
    // preço de quem não foi declarado não pode ampliar o escopo em silêncio.
    if (!noEscopo.has(celula.unit_type_id)) {
      return falha("VALIDATION_ERROR", "Há preço de um produto que não está nesta tabela.", { rates: chave });
    }
  }

  const resultado = await tentar(async () => {
    const { data, meta } = await apiFetchComMeta<Tarifa[], MetaDaGrade>("/rates/bulk", {
      method: "POST",
      body: { rate_table_id: tabelaId, unit_type_ids: escopo, rates: celulas },
    });
    return {
      tarifas: data ?? [],
      // API antiga (sem `meta`) não pode virar `undefined` no meio da tela: o
      // escopo é o que pedimos, e as contagens ficam em zero em vez de mentir.
      meta: meta ?? { unit_type_ids: escopo, created: 0, updated: 0, unchanged: 0, removed: 0 },
    } satisfies GradeGravada;
  });

  if (resultado.ok) {
    revalidatePath(CAMINHO);
    revalidatePath("/app/orcamento");
  }
  return resultado;
}

export async function salvarTabela(
  id: string | null,
  valores: TabelaFormulario,
): Promise<Resultado<TabelaDeTarifas>> {
  const recusa = await exigir("settings", id ? "editar" : "criar");
  if (recusa) return recusa;

  const analise = TabelaFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira os dados da tabela.", detalhesDoZod(analise.error));
  }

  const resultado = await tentar(() =>
    apiFetch<TabelaDeTarifas>(id ? `/rate-tables/${id}` : "/rate-tables", {
      method: id ? "PUT" : "POST",
      body: tabelaParaEntrada(analise.data),
    }),
  );

  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}

/** Desativação, não remoção: apagar levaria junto as tarifas que reservas
 *  antigas referenciam por `rate_table_id`. */
export async function desativarTabela(id: string): Promise<Resultado<null>> {
  const recusa = await exigir("settings", "excluir");
  if (recusa) return recusa;

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/rate-tables/${id}`, { method: "DELETE" });
    return null;
  });

  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}

/**
 * Estadia mínima por tipo de data.
 *
 * Não há endpoint em lote para isto, e a chave natural é
 * `(rate_table_id, date_type)` — por isso o chamador manda o `id` quando a
 * regra já existe (`PATCH`) e `null` quando não (`POST`). Criar de novo daria
 * `409 CODE_IN_USE`, que é a constraint fazendo o trabalho dela.
 */
export async function salvarMinimoDeNoites(
  tabelaId: string,
  id: string | null,
  valores: MinimoFormulario,
): Promise<Resultado<MinimoDeNoites>> {
  const recusa = await exigir("settings", id ? "editar" : "criar");
  if (recusa) return recusa;

  const analise = MinimoFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira o mínimo de noites.", detalhesDoZod(analise.error));
  }

  const noites = minimoParaNoites(analise.data);
  const resultado = await tentar(() =>
    id
      ? apiFetch<MinimoDeNoites>(`/min-nights/${id}`, { method: "PATCH", body: { nights: noites } })
      : apiFetch<MinimoDeNoites>("/min-nights", {
          method: "POST",
          body: { rate_table_id: tabelaId, date_type: analise.data.date_type, nights: noites },
        }),
  );

  if (resultado.ok) {
    revalidatePath(CAMINHO);
    revalidatePath("/app/orcamento");
  }
  return resultado;
}

/** Sem regra para o tipo de data, o mínimo daquele tipo volta a ser 1 noite. */
export async function removerMinimoDeNoites(id: string): Promise<Resultado<null>> {
  const recusa = await exigir("settings", "excluir");
  if (recusa) return recusa;

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/min-nights/${id}`, { method: "DELETE" });
    return null;
  });

  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}
