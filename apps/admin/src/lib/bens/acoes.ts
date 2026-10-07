"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { apiFetch } from "@/lib/api/client";
import { detalhesDoZod } from "@/lib/acoes/campos";
import { tentar } from "@/lib/acoes/executar";
import { exigir } from "@/lib/acoes/guarda";
import { falha, type Resultado } from "@/lib/acoes/resultado";
import {
  AmbienteFormulario,
  AvariaFormulario,
  BemFormulario,
  ColocacaoFormulario,
  ambienteParaCriar,
  ambienteParaSubstituir,
  bemParaEntrada,
  colocacaoParaEntrada,
} from "@/lib/bens/esquemas";
import {
  DESFECHOS_DE_AVARIA,
  type Ambiente,
  type Avaria,
  type AvariaCriarEntrada,
  type Bem,
  type Colocacao,
  type ConferenciaCompleta,
  type DesfechoDeAvaria,
  type FotoDoBem,
  type LinhaContada,
  type ResultadoDaCopia,
  type ResultadoDoFechamento,
} from "@/lib/bens/tipos";

/**
 * Escrita do inventário de bens por ambiente.
 *
 * Cada action declara o par recurso × ação que o `x-rbac` da operação pede —
 * sempre `inventory.goods`, nunca `inventory` (que é o cadastro comercial). A
 * guarda aqui é cortesia: a API recusa de novo, e é ela que vale.
 *
 * Editar usa `PUT` onde o modal carrega o registro inteiro (bem, ambiente,
 * colocação) e `PATCH` onde o gesto muda **um** campo sem conhecer os outros:
 * ativar/desativar, contar uma linha, dar desfecho a uma avaria. É a mesma
 * divisão do resto do painel.
 */

/** O layout do módulo: revalidá-lo atualiza todas as telas de baixo dele. */
const MODULO = "/(app)/app/inventario";

function revalidar(): void {
  revalidatePath(MODULO, "layout");
}

const Id = z.string().uuid();
const ChaveDeColocacao = z
  .string()
  .regex(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);

function idInvalido(): ReturnType<typeof falha> {
  return falha("VALIDATION_ERROR", "Identificador inválido.");
}

// ─────────────────────────────── Catálogo ────────────────────────────────────

export async function salvarBem(id: string | null, valores: BemFormulario): Promise<Resultado<Bem>> {
  const recusa = await exigir("inventory.goods", id ? "editar" : "criar");
  if (recusa) return recusa;
  if (id !== null && !Id.safeParse(id).success) return idInvalido();

  const analise = BemFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira os dados do bem.", detalhesDoZod(analise.error));
  }

  const resultado = await tentar(() =>
    apiFetch<Bem>(id ? `/inventory/items/${id}` : "/inventory/items", {
      method: id ? "PUT" : "POST",
      body: bemParaEntrada(analise.data),
    }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

/** Tirar de linha (ou voltar) — o caminho certo para bem com histórico. */
export async function alternarAtivoDoBem(id: string, ativo: boolean): Promise<Resultado<Bem>> {
  const recusa = await exigir("inventory.goods", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const resultado = await tentar(() =>
    apiFetch<Bem>(`/inventory/items/${id}`, { method: "PATCH", body: { active: ativo } }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

/**
 * Apaga **de verdade** — só o bem que nunca foi conferido, nunca deu avaria e
 * não está colocado. Com histórico, a API responde `409 RESOURCE_IN_USE` e a
 * tela oferece desativar.
 */
export async function apagarBem(id: string): Promise<Resultado<null>> {
  const recusa = await exigir("inventory.goods", "excluir");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/inventory/items/${id}`, { method: "DELETE" });
    return null;
  });
  if (resultado.ok) revalidar();
  return resultado;
}

/**
 * Substitui a galeria inteira — a ordem do array é a ordem de exibição e o
 * primeiro é a capa. Lista vazia é aceita (bem sem foto é bem mal cadastrado,
 * não é erro).
 */
export async function salvarGaleria(itemId: string, mediaIds: string[]): Promise<Resultado<FotoDoBem[]>> {
  const recusa = await exigir("inventory.goods", "editar");
  if (recusa) return recusa;

  const analise = z
    .object({ itemId: Id, mediaIds: z.array(Id).max(24, "No máximo 24 fotos por bem.") })
    .safeParse({ itemId, mediaIds });
  if (!analise.success) return falha("VALIDATION_ERROR", "Confira as fotos.", detalhesDoZod(analise.error));
  if (new Set(mediaIds).size !== mediaIds.length) {
    return falha("VALIDATION_ERROR", "Foto repetida na galeria.", { media_ids: "A mesma foto aparece duas vezes." });
  }

  const resultado = await tentar(() =>
    apiFetch<FotoDoBem[]>(`/inventory/items/${itemId}/photos`, { method: "PUT", body: { media_ids: mediaIds } }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

// ─────────────────────────────── Ambientes ───────────────────────────────────

export async function salvarAmbiente(
  unitId: string,
  id: string | null,
  valores: AmbienteFormulario,
): Promise<Resultado<Ambiente>> {
  const recusa = await exigir("inventory.goods", id ? "editar" : "criar");
  if (recusa) return recusa;
  if (!Id.safeParse(unitId).success || (id !== null && !Id.safeParse(id).success)) return idInvalido();

  const analise = AmbienteFormulario.safeParse(valores);
  if (!analise.success) {
    return falha("VALIDATION_ERROR", "Confira os dados do ambiente.", detalhesDoZod(analise.error));
  }

  // `PUT` sem `unit_id`: cômodo não muda de unidade (levaria o histórico junto),
  // e mandar o campo é `422` no contrato.
  const resultado = await tentar(() =>
    id
      ? apiFetch<Ambiente>(`/rooms/${id}`, { method: "PUT", body: ambienteParaSubstituir(analise.data) })
      : apiFetch<Ambiente>("/rooms", { method: "POST", body: ambienteParaCriar(unitId, analise.data) }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

/** `PATCH {active}` — como se tira de linha um cômodo que tem histórico. */
export async function alternarAtivoDoAmbiente(id: string, ativo: boolean): Promise<Resultado<Ambiente>> {
  const recusa = await exigir("inventory.goods", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const resultado = await tentar(() => apiFetch<Ambiente>(`/rooms/${id}`, { method: "PATCH", body: { active: ativo } }));
  if (resultado.ok) revalidar();
  return resultado;
}

/** Apaga o cômodo criado por engano. Com histórico: `409 RESOURCE_IN_USE`. */
export async function apagarAmbiente(id: string): Promise<Resultado<null>> {
  const recusa = await exigir("inventory.goods", "excluir");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/rooms/${id}`, { method: "DELETE" });
    return null;
  });
  if (resultado.ok) revalidar();
  return resultado;
}

// ─────────────────────────────── Colocações ──────────────────────────────────

export async function colocarBem(
  roomId: string,
  itemId: string,
  valores: ColocacaoFormulario,
): Promise<Resultado<Colocacao>> {
  const recusa = await exigir("inventory.goods", "criar");
  if (recusa) return recusa;
  if (!Id.safeParse(roomId).success) return falha("VALIDATION_ERROR", "Escolha o ambiente.", { room_id: "Escolha o ambiente." });
  if (!Id.safeParse(itemId).success) return falha("VALIDATION_ERROR", "Escolha o bem.", { item_id: "Escolha o bem." });

  const analise = ColocacaoFormulario.safeParse(valores);
  if (!analise.success) return falha("VALIDATION_ERROR", "Confira a quantidade.", detalhesDoZod(analise.error));

  const resultado = await tentar(() =>
    apiFetch<Colocacao>("/inventory/placements", {
      method: "POST",
      body: { room_id: roomId, item_id: itemId, ...colocacaoParaEntrada(analise.data) },
    }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

/** Muda o padrão da casa — da próxima conferência em diante. */
export async function salvarColocacao(chave: string, valores: ColocacaoFormulario): Promise<Resultado<Colocacao>> {
  const recusa = await exigir("inventory.goods", "editar");
  if (recusa) return recusa;
  if (!ChaveDeColocacao.safeParse(chave).success) return idInvalido();

  const analise = ColocacaoFormulario.safeParse(valores);
  if (!analise.success) return falha("VALIDATION_ERROR", "Confira a quantidade.", detalhesDoZod(analise.error));

  const resultado = await tentar(() =>
    apiFetch<Colocacao>(`/inventory/placements/${chave}`, { method: "PUT", body: colocacaoParaEntrada(analise.data) }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

export async function tirarBem(chave: string): Promise<Resultado<null>> {
  const recusa = await exigir("inventory.goods", "excluir");
  if (recusa) return recusa;
  if (!ChaveDeColocacao.safeParse(chave).success) return idInvalido();

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/inventory/placements/${chave}`, { method: "DELETE" });
    return null;
  });
  if (resultado.ok) revalidar();
  return resultado;
}

/**
 * Copia ambientes e colocações de outra unidade. Só **acrescenta** — a API
 * nunca sobrescreve quantidade — e é idempotente por desenho, por isso o
 * contrato não pede `Idempotency-Key`. `simular` é o `?dry_run=1`, que a tela
 * mostra antes de o gestor confirmar.
 */
export async function copiarInventario(
  destinoId: string,
  origemId: string,
  soAmbientes: boolean,
  simular: boolean,
): Promise<Resultado<ResultadoDaCopia>> {
  const recusa = await exigir("inventory.goods", "criar");
  if (recusa) return recusa;
  if (!Id.safeParse(destinoId).success) return idInvalido();
  if (!Id.safeParse(origemId).success) {
    return falha("VALIDATION_ERROR", "Escolha a unidade de origem.", { source_unit_id: "Escolha a unidade de origem." });
  }

  const resultado = await tentar(() =>
    apiFetch<ResultadoDaCopia>(`/units/${destinoId}/inventory/copy`, {
      method: "POST",
      query: { dry_run: simular ? true : undefined },
      body: { source_unit_id: origemId, rooms_only: soAmbientes },
    }),
  );
  if (resultado.ok && !simular) revalidar();
  return resultado;
}

// ─────────────────────────────── Conferência ─────────────────────────────────

/**
 * Abre a conferência da unidade. A segunda abertura é `409 COUNT_ALREADY_OPEN`
 * com `details.count_id` — a falha atravessa inteira, e a tela usa o id para
 * levar à conferência que já está aberta.
 */
export async function abrirConferencia(unitId: string, nota = ""): Promise<Resultado<{ id: string }>> {
  const recusa = await exigir("inventory.goods", "criar");
  if (recusa) return recusa;
  if (!Id.safeParse(unitId).success) return idInvalido();

  const resultado = await tentar(() =>
    apiFetch<ConferenciaCompleta>("/inventory/counts", {
      method: "POST",
      body: { unit_id: unitId, note: nota.trim() === "" ? null : nota.trim() },
    }),
  );
  if (!resultado.ok) return resultado;
  revalidar();
  return { ok: true, data: { id: resultado.data.id } };
}

/**
 * O gesto do celular. `null` **desfaz** a contagem (volta a pendente); `0` é
 * "contei e não achei". Sem `revalidatePath` de propósito: a resposta já traz a
 * linha e o progresso, e redesenhar a conferência inteira a cada toque é o que
 * faria a tela engasgar no sinal de dentro do apartamento.
 */
export async function contarLinha(
  countId: string,
  lineId: string,
  contada: number | null,
): Promise<Resultado<LinhaContada>> {
  const recusa = await exigir("inventory.goods", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(countId).success || !Id.safeParse(lineId).success) return idInvalido();
  if (contada !== null && !(Number.isInteger(contada) && contada >= 0)) {
    return falha("VALIDATION_ERROR", "Quantidade inválida.", { counted_qty: "A contagem é um número inteiro, 0 ou mais." });
  }

  return tentar(() =>
    apiFetch<LinhaContada>(`/inventory/counts/${countId}/lines/${lineId}`, {
      method: "PATCH",
      body: { counted_qty: contada },
    }),
  );
}

/**
 * Fecha e apura. Linha pendente é `409 COUNT_HAS_PENDING_LINES` — a falha
 * volta com `details.pending` e `details.pending_by_room` para a tela dizer
 * onde falta contar. `abrirAvarias=false` é a contagem de aferição.
 *
 * A observação segue a regra do `PATCH`: `undefined` não vai no corpo (mantém a
 * que a conferência já tem), texto **substitui**, `null` **limpa**. Quem decide
 * qual dos três é a tela, que sabe se o campo foi mexido.
 */
export async function fecharConferencia(
  countId: string,
  abrirAvarias: boolean,
  nota?: string | null,
): Promise<Resultado<ResultadoDoFechamento>> {
  const recusa = await exigir("inventory.goods", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(countId).success) return idInvalido();
  if (typeof nota === "string" && nota.length > 2000) {
    return falha("VALIDATION_ERROR", "Observação longa demais.", { note: "No máximo 2000 caracteres." });
  }

  const corpo: { raise_issues: boolean; note?: string | null } = { raise_issues: abrirAvarias };
  if (nota !== undefined) corpo.note = nota === null || nota.trim() === "" ? null : nota.trim();

  const resultado = await tentar(() =>
    apiFetch<ResultadoDoFechamento>(`/inventory/counts/${countId}/close`, { method: "POST", body: corpo }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

/** Cancela — não apaga. As linhas contadas ficam gravadas. */
export async function cancelarConferencia(countId: string): Promise<Resultado<null>> {
  const recusa = await exigir("inventory.goods", "excluir");
  if (recusa) return recusa;
  if (!Id.safeParse(countId).success) return idInvalido();

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/inventory/counts/${countId}`, { method: "DELETE" });
    return null;
  });
  if (resultado.ok) revalidar();
  return resultado;
}

// ─────────────────────────────── Avarias ─────────────────────────────────────

/**
 * Registra a avaria. A reserva vai pelo **código** (`reservation_code`), que é
 * o que a governanta tem na mão — a API o resolve, sem exigir de quem conta a
 * permissão de consultar reservas. Código que não existe volta `422` com
 * `details.reservation_code`, e o formulário põe a frase no campo.
 *
 * Campo vazio não vai no corpo: avaria sem reserva é desgaste ou achado de
 * rotina, e mandar `reservation_code` junto com `reservation_id` é `422` — o
 * painel nunca manda o id.
 */
export async function registrarAvaria(
  valores: AvariaFormulario,
  countId: string | null = null,
): Promise<Resultado<Avaria>> {
  const recusa = await exigir("inventory.goods", "criar");
  if (recusa) return recusa;
  if (countId !== null && !Id.safeParse(countId).success) return idInvalido();

  const analise = AvariaFormulario.safeParse(valores);
  if (!analise.success) return falha("VALIDATION_ERROR", "Confira os dados da avaria.", detalhesDoZod(analise.error));

  const codigo = analise.data.reservation_code.trim().toUpperCase();
  const corpo: AvariaCriarEntrada = {
    room_id: analise.data.room_id,
    item_id: analise.data.item_id,
    kind: analise.data.kind,
    qty: Number.parseInt(analise.data.qty.trim(), 10),
    note: analise.data.note.trim() === "" ? null : analise.data.note.trim(),
    count_id: countId,
    ...(codigo === "" ? {} : { reservation_code: codigo }),
  };

  const resultado = await tentar(() => apiFetch<Avaria>("/inventory/issues", { method: "POST", body: corpo }));
  if (resultado.ok) revalidar();
  return resultado;
}

/**
 * Dá o desfecho — é ele que fecha a pendência. `null` **reabre** (o prato
 * "reposto" que não chegou). `resolved_at`/`resolved_by` são do servidor.
 */
export async function resolverAvaria(id: string, desfecho: DesfechoDeAvaria | null): Promise<Resultado<Avaria>> {
  const recusa = await exigir("inventory.goods", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();
  if (desfecho !== null && !(DESFECHOS_DE_AVARIA as readonly string[]).includes(desfecho)) {
    return falha("VALIDATION_ERROR", "Desfecho inválido.", { resolution: "Escolha o desfecho." });
  }

  const resultado = await tentar(() =>
    apiFetch<Avaria>(`/inventory/issues/${id}`, { method: "PATCH", body: { resolution: desfecho } }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

/** Apaga o registro que nunca devia ter nascido. Resolver não é apagar. */
export async function apagarAvaria(id: string): Promise<Resultado<null>> {
  const recusa = await exigir("inventory.goods", "excluir");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/inventory/issues/${id}`, { method: "DELETE" });
    return null;
  });
  if (resultado.ok) revalidar();
  return resultado;
}
