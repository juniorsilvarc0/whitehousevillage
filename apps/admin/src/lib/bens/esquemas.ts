import { z } from "zod";

import { inteiroDe, paraInteiro, paraTextoOuNulo, textoObrigatorio } from "@/lib/acoes/campos";
import { centavosDeTexto, reaisDeCentavos } from "@/lib/dinheiro";
import {
  CATEGORIAS_DE_BEM,
  TIPOS_DE_AMBIENTE,
  TIPOS_DE_AVARIA,
  UNIDADES_DE_MEDIDA,
  type AmbienteCriarEntrada,
  type AmbienteSubstituirEntrada,
  type Bem,
  type BemEntrada,
  type ColocacaoSubstituirEntrada,
} from "@/lib/bens/tipos";

/**
 * Formulários do inventário de bens — espelho de `BemCriar`, `AmbienteCriar`,
 * `ColocacaoCriar` e `AvariaCriar` da OpenAPI.
 *
 * Mesmo arranjo de `configuracoes/inventario/esquemas.ts`: tudo é `string` no
 * formulário e a conversão acontece num lugar só, na fronteira com o DTO. O
 * schema valida no navegador (para o erro aparecer sem ida ao servidor) e de
 * novo dentro da action (Server Action é endpoint público).
 */

// ── Custo de reposição: reais na tela, centavos no fio ──────────────────────

/**
 * O custo de reposição tem **três** estados, e o formulário precisa dos três:
 *
 * - vazio → `null`, "ainda não cotado";
 * - um valor maior que zero → centavos;
 * - zero → **recusado**. O contrato o recusa (`minimum: 1`) para que zero não
 *   vire um segundo jeito, errado, de escrever "não sei": uma avaria de bem
 *   "custo zero" sairia de graça na cobrança.
 */
export type LeituraDoCusto = { ok: true; centavos: number | null } | { ok: false; motivo: "zero" | "invalido" };

export function lerCusto(texto: string): LeituraDoCusto {
  if (texto.trim() === "") return { ok: true, centavos: null };
  const centavos = centavosDeTexto(texto);
  if (centavos === null) return { ok: false, motivo: "invalido" };
  if (centavos === 0) return { ok: false, motivo: "zero" };
  return { ok: true, centavos };
}

/** Centavos (ou "não cotado") de volta para o `<input>`. */
export function custoParaCampo(centavos: number | null | undefined): string {
  return centavos === null || centavos === undefined ? "" : reaisDeCentavos(centavos);
}

const MENSAGEM_DO_CUSTO = {
  zero: "Custo zero não existe aqui. Se ainda não sabe quanto custa repor, deixe o campo vazio.",
  invalido: "Informe o valor em reais, como 18,90 — ou deixe vazio se ainda não foi cotado.",
} as const;

// ── Bem (catálogo) ──────────────────────────────────────────────────────────

export const BemFormulario = z
  .object({
    name: textoObrigatorio(1, "Informe o nome do bem.").max(160, "Nome com no máximo 160 caracteres."),
    description: z.string().max(2000, "Descrição com no máximo 2000 caracteres."),
    category: z.enum(CATEGORIAS_DE_BEM, { message: "Escolha a categoria." }),
    unit_measure: z.enum(UNIDADES_DE_MEDIDA, { message: "Escolha a unidade de contagem." }),
    replacement_cost: z.string(),
    active: z.boolean(),
  })
  .superRefine((valores, ctx) => {
    const custo = lerCusto(valores.replacement_cost);
    if (!custo.ok) {
      ctx.addIssue({ code: "custom", path: ["replacement_cost"], message: MENSAGEM_DO_CUSTO[custo.motivo] });
    }
  });

export type BemFormulario = z.infer<typeof BemFormulario>;

export function valoresDoBem(bem: Bem | null): BemFormulario {
  return {
    name: bem?.name ?? "",
    description: bem?.description ?? "",
    category: bem?.category ?? "louca",
    unit_measure: bem?.unit_measure ?? "un",
    replacement_cost: custoParaCampo(bem?.replacement_cost_cents),
    active: bem?.active ?? true,
  };
}

/**
 * O formulário vira o corpo de `POST`/`PUT /inventory/items`. Só chega aqui o
 * que passou pelo schema; o custo inválido já foi recusado, e o que sobra é
 * centavos ou `null`.
 */
export function bemParaEntrada(valores: BemFormulario): BemEntrada {
  const custo = lerCusto(valores.replacement_cost);
  return {
    name: valores.name.trim(),
    description: paraTextoOuNulo(valores.description),
    category: valores.category,
    unit_measure: valores.unit_measure,
    replacement_cost_cents: custo.ok ? custo.centavos : null,
    active: valores.active,
  };
}

// ── Ambiente ────────────────────────────────────────────────────────────────

export const AmbienteFormulario = z.object({
  name: textoObrigatorio(1, "Dê um nome ao ambiente — “Cozinha”, “Suíte 1”.").max(120, "Nome com no máximo 120 caracteres."),
  kind: z.enum(TIPOS_DE_AMBIENTE, { message: "Escolha o tipo de ambiente." }),
  sort_order: inteiroDe(0, "A ordem precisa ser um número inteiro (0, 1, 2…)."),
  active: z.boolean(),
});

export type AmbienteFormulario = z.infer<typeof AmbienteFormulario>;

export function ambienteParaSubstituir(valores: AmbienteFormulario): AmbienteSubstituirEntrada {
  return {
    name: valores.name.trim(),
    kind: valores.kind,
    sort_order: paraInteiro(valores.sort_order),
    active: valores.active,
  };
}

export function ambienteParaCriar(unitId: string, valores: AmbienteFormulario): AmbienteCriarEntrada {
  return { unit_id: unitId, ...ambienteParaSubstituir(valores) };
}

// ── Colocação (quanto de cada bem em cada ambiente) ─────────────────────────

export const ColocacaoFormulario = z.object({
  // Zero é legítimo: "este ambiente não tem este item, e é de propósito".
  expected_qty: inteiroDe(0, "Informe a quantidade esperada (0 ou mais)."),
  note: z.string().max(2000, "Observação com no máximo 2000 caracteres."),
});

export type ColocacaoFormulario = z.infer<typeof ColocacaoFormulario>;

export function colocacaoParaEntrada(valores: ColocacaoFormulario): ColocacaoSubstituirEntrada {
  return { expected_qty: paraInteiro(valores.expected_qty), note: paraTextoOuNulo(valores.note) };
}

// ── Avaria ──────────────────────────────────────────────────────────────────

export const AvariaFormulario = z.object({
  room_id: z.string().uuid("Escolha o ambiente."),
  item_id: z.string().uuid("Escolha o bem."),
  kind: z.enum(TIPOS_DE_AVARIA, { message: "Escolha o que aconteceu." }),
  // Avaria de zero peças não é avaria — o contrato exige `qty >= 1`.
  qty: inteiroDe(1, "Quantas peças? Precisa ser pelo menos 1."),
  note: z.string().max(2000, "Observação com no máximo 2000 caracteres."),
  /** O CÓDIGO da reserva (WH-2026-0142), que é o que a governanta tem em mãos.
   *  Vai como `reservation_code`; quem o resolve é a API. Vazio = sem reserva. */
  reservation_code: z.string().max(40, "Código longo demais — confira (ex.: WH-2026-0142)."),
});

export type AvariaFormulario = z.infer<typeof AvariaFormulario>;
