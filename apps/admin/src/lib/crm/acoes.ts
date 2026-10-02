"use server";

import { revalidatePath } from "next/cache";

import { exigir } from "@/lib/acoes/guarda";
import { detalhesDoZod } from "@/lib/acoes/campos";
import { chamarCrm, falhaCrm, type ResultadoCrm } from "@/lib/crm/api";
import { GanhoFormulario, PerdaFormulario } from "@/lib/crm/esquemas";
import { carregarQuadro, FiltrosDoFunil } from "@/lib/crm/quadro";
import type {
  Atividade,
  Oportunidade,
  OportunidadeAtualizar,
  QuadroKanban,
  ResultadoDeGanho,
  ResultadoDeMudancaDeEtapa,
  ResultadoDePerda,
} from "@/lib/crm/tipos";

/**
 * As ações da oportunidade — compartilhadas pelo kanban e pela tela do card.
 *
 * Ficam em `lib/crm/` e não na pasta de uma das telas porque as duas chamam
 * exatamente as mesmas quatro operações. Duplicá-las seria manter duas versões
 * da mesma guarda de permissão e da mesma invalidação de cache, e a segunda
 * envelheceria calada.
 *
 * **Estado só muda por ação nomeada.** Não existe aqui um "salvar oportunidade"
 * que aceite `stage_id`, `status` ou `lost_reason_id`: mover é `/stage`, ganhar
 * é `/win`, perder é `/lose`. Um `PATCH` que movesse a etapa não gravaria
 * `crm_stage_history`, e a conversão por etapa do relatório passaria a mentir.
 */

const FUNIL = "/app/funil";

function revalidar(oportunidadeId: string): void {
  revalidatePath(FUNIL);
  revalidatePath(`/app/oportunidades/${oportunidadeId}`);
}

/**
 * A busca que o tempo real dispara — o quadro do recorte que está na tela.
 *
 * É Server Action, e não uma chamada do navegador, pelo mesmo motivo do resto
 * do painel: o JWT vive em cookie `httpOnly` e o navegador não o lê. O evento
 * do tópico `crm` chega magro ("mudou a oportunidade X") e a tela refaz o fetch
 * autenticado — que é onde o `scope='own'` do corretor é aplicado. Um evento
 * que carregasse o card seria um segundo caminho de leitura, sem permissão.
 *
 * Não usa `router.refresh()` por uma razão de custo medida na própria página:
 * ela carrega TRÊS coleções em paralelo (funis, quadro, motivos de perda), e
 * das três só o quadro muda quando alguém arrasta um card. Refazer a árvore de
 * RSC a cada evento de colega seriam duas requisições jogadas fora por evento,
 * mais a rolagem horizontal do quadro de volta ao começo.
 *
 * Não revalida caminho nenhum: esta ação **lê**. `revalidatePath` aqui
 * marcaria a rota como suja em toda atualização automática, e a próxima
 * navegação do usuário pagaria uma recarga que ninguém pediu.
 */
export async function buscarQuadro(filtros: FiltrosDoFunil): Promise<ResultadoCrm<QuadroKanban>> {
  const recusa = await exigir("crm.opportunities", "ver");
  if (recusa) return recusa;

  // Server Action é endpoint público (ver `lib/acoes/guarda.ts`): o recorte
  // chega do cliente e passa pelo mesmo schema que a página usa. Chave
  // desconhecida o zod descarta, e é o que impede um filtro inventado de virar
  // querystring na chamada à API.
  const analise = FiltrosDoFunil.safeParse(filtros);
  if (!analise.success) {
    return falhaCrm("VALIDATION_ERROR", "Recorte do funil inválido.", detalhesDoZod(analise.error));
  }

  return carregarQuadro(analise.data);
}

/**
 * Move o card de etapa.
 *
 * `from_stage_id` vai sempre que a tela sabe de onde o card saiu: é a guarda
 * otimista contra dois corretores arrastando o mesmo card. Sem ela, o segundo
 * arrasto vence em silêncio e ninguém fica sabendo que houve disputa.
 */
export async function moverEtapa(
  id: string,
  destinoId: string,
  origemId?: string,
): Promise<ResultadoCrm<ResultadoDeMudancaDeEtapa>> {
  const recusa = await exigir("crm.opportunities", "editar");
  if (recusa) return recusa;

  const resultado = await chamarCrm<ResultadoDeMudancaDeEtapa>(`/crm/opportunities/${id}/stage`, {
    method: "POST",
    body: { stage_id: destinoId, ...(origemId ? { from_stage_id: origemId } : {}) },
  });

  if (resultado.ok) revalidar(id);
  return resultado;
}

/**
 * Ganha a oportunidade e cria a reserva a partir do orçamento vigente.
 *
 * `Idempotency-Key` é **obrigatório** porque isto cria reserva, e a chave é
 * gerada pela tela — não aqui. Gerar no servidor daria uma chave nova a cada
 * tentativa, o que é o mesmo que não ter chave nenhuma: dois cliques no botão
 * criariam duas reservas das mesmas datas, e a segunda morreria em
 * `409 DATE_CONFLICT` contra a primeira.
 */
export async function ganharOportunidade(
  id: string,
  chaveDeIdempotencia: string,
  valores: GanhoFormulario,
  quoteId?: string,
): Promise<ResultadoCrm<ResultadoDeGanho>> {
  const recusa = await exigir("crm.opportunities", "editar");
  if (recusa) return recusa;

  if (chaveDeIdempotencia.trim().length < 8) {
    // O contrato pede 8..255. Chave curta é bug da tela, não recusa comercial.
    return falhaCrm("VALIDATION_ERROR", "Chave de idempotência ausente. Recarregue a tela.");
  }

  const analise = GanhoFormulario.safeParse(valores);
  if (!analise.success) {
    return falhaCrm("VALIDATION_ERROR", "Confira os dados.", detalhesDoZod(analise.error));
  }

  const resultado = await chamarCrm<ResultadoDeGanho>(`/crm/opportunities/${id}/win`, {
    method: "POST",
    headers: { "Idempotency-Key": chaveDeIdempotencia },
    body: {
      ...(quoteId ? { quote_id: quoteId } : {}),
      ...(analise.data.note.trim() ? { note: analise.data.note.trim() } : {}),
    },
  });

  if (resultado.ok) {
    revalidar(id);
    // A reserva nasce `hold` e entra no calendário na hora: quem estiver com o
    // mapa aberto precisa ver o bloco aparecer.
    revalidatePath("/app/mapa");
    revalidatePath("/app/reservas");
  }
  return resultado;
}

/** Perde a oportunidade. O motivo é obrigatório e tem de ser um do catálogo
 *  ativo — `422 LOSS_REASON_REQUIRED` quando não é. */
export async function perderOportunidade(
  id: string,
  valores: PerdaFormulario,
): Promise<ResultadoCrm<ResultadoDePerda>> {
  const recusa = await exigir("crm.opportunities", "editar");
  if (recusa) return recusa;

  const analise = PerdaFormulario.safeParse(valores);
  if (!analise.success) {
    return falhaCrm("VALIDATION_ERROR", "Escolha o motivo da perda.", detalhesDoZod(analise.error));
  }

  const resultado = await chamarCrm<ResultadoDePerda>(`/crm/opportunities/${id}/lose`, {
    method: "POST",
    body: {
      lost_reason_id: analise.data.lost_reason_id,
      ...(analise.data.note.trim() ? { note: analise.data.note.trim() } : {}),
    },
  });

  if (resultado.ok) revalidar(id);
  return resultado;
}

/**
 * Edição inline da oportunidade — o `PATCH` que salva no `blur`.
 *
 * Recebe **um campo por vez**, e é isso que faz o `PATCH` ser o verbo certo:
 * `Opt[T]` do lado do Go distingue "campo ausente" de "campo nulo", e mandar o
 * objeto inteiro apagaria o que outra pessoa gravou entre a abertura da tela e
 * o `blur` deste campo.
 */
export async function atualizarOportunidade(
  id: string,
  campos: OportunidadeAtualizar,
): Promise<ResultadoCrm<Oportunidade>> {
  const recusa = await exigir("crm.opportunities", "editar");
  if (recusa) return recusa;

  if (Object.keys(campos).length === 0) {
    return falhaCrm("VALIDATION_ERROR", "Nada a salvar.");
  }

  const resultado = await chamarCrm<Oportunidade>(`/crm/opportunities/${id}`, {
    method: "PATCH",
    body: campos,
  });

  if (resultado.ok) revalidar(id);
  return resultado;
}

/** Cria atividade ou nota. Nota é atividade de `type: nota` — não há tabela nem
 *  CRUD separado, e por isso ela entra na linha do tempo pelo mesmo caminho. */
export async function criarAtividade(
  oportunidadeId: string,
  corpo: Record<string, unknown>,
): Promise<ResultadoCrm<Atividade>> {
  const recusa = await exigir("crm.activities", "criar");
  if (recusa) return recusa;

  const resultado = await chamarCrm<Atividade>("/crm/activities", {
    method: "POST",
    body: { ...corpo, opportunity_id: oportunidadeId },
  });

  if (resultado.ok) revalidar(oportunidadeId);
  return resultado;
}

/** Conclui a tarefa. `done_at` é carimbado pelo **servidor**: tempo médio de
 *  resposta medido pelo relógio do navegador não é indicador, é opinião. */
export async function concluirAtividade(
  atividadeId: string,
  oportunidadeId: string,
  nota?: string,
): Promise<ResultadoCrm<unknown>> {
  const recusa = await exigir("crm.activities", "editar");
  if (recusa) return recusa;

  const resultado = await chamarCrm<unknown>(`/crm/activities/${atividadeId}/complete`, {
    method: "POST",
    body: nota?.trim() ? { note: nota.trim() } : {},
  });

  if (resultado.ok) revalidar(oportunidadeId);
  return resultado;
}
