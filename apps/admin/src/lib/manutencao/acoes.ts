"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { apiFetch, apiList } from "@/lib/api/client";
import { detalhesDoZod } from "@/lib/acoes/campos";
import { tentar } from "@/lib/acoes/executar";
import { exigir } from "@/lib/acoes/guarda";
import { falha, type Resultado } from "@/lib/acoes/resultado";
import type { Bem, InventarioDaUnidade } from "@/lib/bens/tipos";
import {
  CustoFormulario,
  EdicaoFormulario,
  OrdemFormulario,
  PeriodoFormulario,
  custoParaAtualizar,
  custoParaConcluir,
  edicaoParaSubstituir,
  ordemParaCriar,
  periodoParaEntrada,
} from "@/lib/manutencao/esquemas";
import type { BemDoCatalogo, OpcoesDaUnidade, OrdemDeManutencao } from "@/lib/manutencao/tipos";

/**
 * Escrita das ordens de manutenção.
 *
 * Cada action declara o par recurso × ação que o `x-rbac` da rota pede — a
 * guarda aqui é cortesia, a API recusa de novo e é ela que vale. As transições
 * são **ações nomeadas** (`/start`, `/complete`, `DELETE`), nunca um `PATCH
 * {status}`: a máquina de estados é da API, e o painel só pede o passo.
 *
 * Revalida o módulo **e** o inventário: concluir conserta a avaria de origem
 * (e a tela de avarias mostra "ordem aberta" por avaria), e o mapa lê
 * `stay_blocks` sem cache — o próximo carregamento já vem com o bloqueio novo,
 * e o SSE de `calendar` avisa quem está com ele aberto.
 */

const MODULO = "/(app)/app/manutencao";
const INVENTARIO = "/(app)/app/inventario";

function revalidar(): void {
  revalidatePath(MODULO, "layout");
  revalidatePath(INVENTARIO, "layout");
}

const Id = z.string().uuid();

function idInvalido(): ReturnType<typeof falha> {
  return falha("VALIDATION_ERROR", "Identificador inválido.");
}

// ── Criar ───────────────────────────────────────────────────────────────────

/**
 * Abre a ordem — e, se pedido, bloqueia a unidade **no mesmo commit**. Data
 * ocupada é `409 DATE_CONFLICT` e nada é criado; avaria que já tem ordem é
 * `409 MAINTENANCE_ORDER_ALREADY_OPEN` com `details.maintenance_order_id`. As
 * duas falhas atravessam inteiras, com `details`, para a tela decidir.
 *
 * Sem `Idempotency-Key`, como o contrato decide: os dois duplicados que
 * importam (mesma data, mesma avaria) o banco já recusa.
 */
export async function abrirOrdem(valores: OrdemFormulario): Promise<Resultado<OrdemDeManutencao>> {
  const recusa = await exigir("maintenance", "criar");
  if (recusa) return recusa;

  const analise = OrdemFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira os dados da ordem.", detalhesDoZod(analise.error));
  }

  const resultado = await tentar(() =>
    apiFetch<OrdemDeManutencao>("/maintenance-orders", { method: "POST", body: ordemParaCriar(analise.data) }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

// ── Editar ──────────────────────────────────────────────────────────────────

/**
 * `PUT` dos campos de cadastro. `comAvaria` decide só se cômodo e bem vão no
 * corpo (com avaria, são os dela e não vão); quem confere é a API, que recusa
 * com `422` se divergirem.
 */
export async function salvarOrdem(
  id: string,
  valores: EdicaoFormulario,
  comAvaria: boolean,
): Promise<Resultado<OrdemDeManutencao>> {
  const recusa = await exigir("maintenance", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const analise = EdicaoFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira os dados da ordem.", detalhesDoZod(analise.error));
  }

  const resultado = await tentar(() =>
    apiFetch<OrdemDeManutencao>(`/maintenance-orders/${id}`, {
      method: "PUT",
      body: edicaoParaSubstituir(analise.data, comAvaria),
    }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

/** `PATCH {cost_cents}` — o custo que chega depois da conclusão. Só essa
 *  chave vai no corpo: qualquer outra, na concluída, é `409`. */
export async function lancarCusto(id: string, valores: CustoFormulario): Promise<Resultado<OrdemDeManutencao>> {
  const recusa = await exigir("maintenance", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const analise = CustoFormulario.safeParse(valores);
  if (!analise.success) return falha("VALIDATION_ERROR", "Confira o custo.", detalhesDoZod(analise.error));

  const resultado = await tentar(() =>
    apiFetch<OrdemDeManutencao>(`/maintenance-orders/${id}`, { method: "PATCH", body: custoParaAtualizar(analise.data) }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

// ── Transições nomeadas ─────────────────────────────────────────────────────

/** `aberta → em_andamento`. Segundo toque é `409 INVALID_STATE_TRANSITION`. */
export async function iniciarOrdem(id: string): Promise<Resultado<OrdemDeManutencao>> {
  const recusa = await exigir("maintenance", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const resultado = await tentar(() =>
    apiFetch<OrdemDeManutencao>(`/maintenance-orders/${id}/start`, { method: "POST" }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

/**
 * Conclui: libera o calendário e conserta a avaria de origem, na mesma
 * transação. O custo é opcional — vazio vai **sem corpo**, e o que já estava
 * lançado fica.
 */
export async function concluirOrdem(id: string, valores: CustoFormulario): Promise<Resultado<OrdemDeManutencao>> {
  const recusa = await exigir("maintenance", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const analise = CustoFormulario.safeParse(valores);
  if (!analise.success) return falha("VALIDATION_ERROR", "Confira o custo.", detalhesDoZod(analise.error));

  const corpo = custoParaConcluir(analise.data);
  const resultado = await tentar(() =>
    apiFetch<OrdemDeManutencao>(`/maintenance-orders/${id}/complete`, {
      method: "POST",
      ...(corpo ? { body: corpo } : {}),
    }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

/** Cancela — **não apaga**. `DELETE` responde `200` com a ordem como ficou
 *  (não `204`), para a tela mostrar o que aconteceu com o bloqueio. */
export async function cancelarOrdem(id: string): Promise<Resultado<OrdemDeManutencao>> {
  const recusa = await exigir("maintenance", "excluir");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const resultado = await tentar(() =>
    apiFetch<OrdemDeManutencao>(`/maintenance-orders/${id}`, { method: "DELETE" }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

// ── Bloqueio ────────────────────────────────────────────────────────────────

/** Bloqueia a unidade da ordem, ou estende/encurta o bloqueio — quem decide o
 *  que acontece é `maintenance.Replan`, do outro lado. */
export async function definirBloqueio(id: string, valores: PeriodoFormulario): Promise<Resultado<OrdemDeManutencao>> {
  const recusa = await exigir("maintenance", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const analise = PeriodoFormulario.safeParse(valores);
  if (!analise.success) return falha("VALIDATION_ERROR", "Confira o período.", detalhesDoZod(analise.error));

  const resultado = await tentar(() =>
    apiFetch<OrdemDeManutencao>(`/maintenance-orders/${id}/block`, {
      method: "PUT",
      body: periodoParaEntrada(analise.data),
    }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

/** Solta o bloqueio sem encerrar a ordem. Segundo toque responde `200` igual. */
export async function soltarBloqueio(id: string): Promise<Resultado<OrdemDeManutencao>> {
  const recusa = await exigir("maintenance", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const resultado = await tentar(() =>
    apiFetch<OrdemDeManutencao>(`/maintenance-orders/${id}/block`, { method: "DELETE" }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

// ── Apoio do formulário (leituras pelas rotas de Bens) ──────────────────────

/**
 * Os cômodos ativos da unidade e os bens colocados em cada um — uma chamada
 * (`GET /units/{id}/inventory`, que sem `include_inactive` já vem só com o que
 * está em uso). É Server Action porque o formulário escolhe a unidade no
 * navegador, e o navegador não tem o token.
 *
 * A rota pede `inventory.goods:ver`, não `maintenance`: é o contrato que diz
 * (descrição da tag `Manutenção`), e a guarda daqui pergunta pela mesma coisa
 * para a mensagem sair certa.
 */
export async function opcoesDaUnidade(unitId: string): Promise<Resultado<OpcoesDaUnidade>> {
  const recusa = await exigir("inventory.goods", "ver");
  if (recusa) return recusa;
  if (!Id.safeParse(unitId).success) return idInvalido();

  const r = await tentar(() => apiFetch<InventarioDaUnidade>(`/units/${unitId}/inventory`));
  if (!r.ok) return r;
  return {
    ok: true,
    data: {
      ambientes: r.data.rooms
        .filter((a) => a.active)
        .map((a) => ({
          id: a.id,
          name: a.name,
          itens: a.items.map((c) => ({ id: c.item_id, name: c.item_name ?? "Bem sem nome" })),
        })),
    },
  };
}

/** Busca no catálogo — o defeito pode ser do móvel que ninguém conta, e o bem
 *  não precisa estar colocado no cômodo. */
export async function buscarBens(q: string): Promise<Resultado<BemDoCatalogo[]>> {
  const recusa = await exigir("inventory.goods", "ver");
  if (recusa) return recusa;
  const termo = q.trim().slice(0, 120);
  if (termo.length < 2) return { ok: true, data: [] };

  const r = await tentar(() =>
    apiList<Bem>("/inventory/items", { query: { q: termo, active: true, sort: "name", per_page: 20 } }),
  );
  if (!r.ok) return r;
  return { ok: true, data: r.data.data.map((b) => ({ id: b.id, name: b.name })) };
}
