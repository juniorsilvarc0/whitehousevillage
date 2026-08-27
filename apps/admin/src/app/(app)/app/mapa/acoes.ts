"use server";

import { revalidatePath } from "next/cache";

import { apiFetch } from "@/lib/api/client";
import { detalhesDoZod } from "@/lib/acoes/campos";
import { exigir } from "@/lib/acoes/guarda";
import { tentar } from "@/lib/acoes/executar";
import { falha, type Resultado } from "@/lib/acoes/resultado";
import { janelaDe } from "@/lib/mapa/janela";
import type { DadosDoMapa } from "@/lib/mapa/tipos";

import { carregarOcupacao } from "./dados";
import { BloqueioFormulario, JanelaFormulario } from "./esquemas";

const CAMINHO = "/app/mapa";

/**
 * A busca que o tempo real dispara.
 *
 * É Server Action, e não uma rota de dados no navegador, pelo mesmo motivo que
 * o resto do painel: o JWT vive em cookie `httpOnly` e o navegador não o lê. O
 * evento SSE chega magro ("mudou o bloco X"), a tela chama isto, e a resposta
 * volta com a matriz inteira da faixa visível — que é o desenho de que a tela
 * precisa e o único lugar onde o RBAC do usuário é aplicado.
 *
 * Não usa `router.refresh()` por uma razão concreta: refazer a árvore de RSC
 * repõe também a barra de filtros, a rolagem horizontal e o card aberto. A
 * gestão fica com o mapa rolado em fevereiro e um clique num evento de janeiro
 * a jogaria de volta ao começo.
 */
export async function buscarOcupacao(from: string, dias: number): Promise<Resultado<DadosDoMapa>> {
  const recusa = await exigir("calendar", "ver");
  if (recusa) return recusa;

  const analise = JanelaFormulario.safeParse({ from, dias });
  if (!analise.success) return falha("VALIDATION_ERROR", "Faixa inválida.", detalhesDoZod(analise.error));

  return carregarOcupacao(janelaDe(analise.data.from, analise.data.dias));
}

/**
 * Bloqueio operacional criado direto no mapa — o gesto do arrasto.
 *
 * Uma unidade por chamada, de propósito: o arrasto é numa linha, e uma linha é
 * uma unidade. O contrato aceita várias (`unit_ids`) porque a manutenção da casa
 * inteira existe, e ela ganha um botão próprio quando alguém pedir — não um
 * arrasto de nove linhas, que ninguém consegue fazer sem errar.
 *
 * `409 DATE_CONFLICT` aqui é resposta normal, não defeito: entre a tela desenhar
 * a data livre e o `INSERT` acontecer, alguém pode ter vendido. Quem recusa é a
 * constraint do banco, e a tela mostra a recusa e recarrega.
 */
export async function criarBloqueio(valores: BloqueioFormulario): Promise<Resultado<null>> {
  const recusa = await exigir("calendar", "criar");
  if (recusa) return recusa;

  const analise = BloqueioFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira o período do bloqueio.", detalhesDoZod(analise.error));
  }

  const { unit_id, from, to, source, note } = analise.data;
  const resultado = await tentar(async () => {
    await apiFetch<unknown>("/blocks", {
      method: "POST",
      body: { unit_ids: [unit_id], from, to, source, note: note?.trim() ? note.trim() : null },
    });
    return null;
  });

  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}

/**
 * Libera um bloqueio operacional. Par simétrico da criação — quem pode tirar a
 * data do estoque tem de poder devolvê-la, e é por isso que a ação exigida é
 * `calendar:excluir` e não `admin`.
 *
 * Bloco de reserva não passa por aqui: o contrato responde
 * `409 INVALID_STATE_TRANSITION`, porque quem solta a data de uma reserva é
 * `/cancel`, `/check-out` ou o job de expiração. A tela nem oferece o botão.
 */
export async function liberarBloqueio(stayBlockId: string): Promise<Resultado<null>> {
  const recusa = await exigir("calendar", "excluir");
  if (recusa) return recusa;

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/blocks/${stayBlockId}`, { method: "DELETE" });
    return null;
  });

  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}
