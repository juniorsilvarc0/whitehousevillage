import { z } from "zod";

import { paraTextoOuNulo, textoObrigatorio } from "@/lib/acoes/campos";
import { custoParaCampo, lerCusto } from "@/lib/bens/esquemas";
import { ehDataISO } from "@/lib/datas";
import {
  PRIORIDADES_DA_ORDEM,
  type ConclusaoEntrada,
  type CustoDaOrdemEntrada,
  type OrdemCriarEntrada,
  type OrdemDeManutencao,
  type OrdemSubstituirEntrada,
  type PeriodoDoBloqueio,
} from "@/lib/manutencao/tipos";

/**
 * Formulários das ordens de manutenção — espelho de `OrdemDeManutencaoCriar`,
 * `OrdemDeManutencaoSubstituir`, `ConclusaoDaOrdem` e `PeriodoDoBloqueio`.
 *
 * Mesmo arranjo dos bens: tudo é `string` no formulário (o `<input>` fala
 * string) e a conversão acontece num lugar só, nas funções `…Para…` daqui de
 * baixo, na fronteira com o DTO. O schema roda no navegador e de novo dentro da
 * action (Server Action é endpoint público).
 *
 * **O período do bloqueio não é validado aqui além da forma.** "Começa hoje ou
 * depois", "fim depois do início", "até 365 noites", "início até daqui a 3
 * anos" e "noite que já passou não muda" são regras de
 * `internal/domain/maintenance.Replan`, e o `422` dele chega em
 * `details["block.from"]`/`details["block.to"]` (ou `from`/`to` na rota do
 * bloqueio) para cair no campo certo. Copiar a regra para cá seria criar a
 * segunda verdade que diverge — e a segunda verdade, num fuso diferente do da
 * casa, recusaria "hoje" às 22h.
 */

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Vazio ou UUID — os campos opcionais de cômodo, bem e avaria. */
const idOpcional = (mensagem: string) => z.string().refine((v) => v === "" || UUID.test(v), mensagem);

// ── Custo: reais na tela, centavos no fio ───────────────────────────────────

/**
 * Três estados, como o custo de reposição do bem: vazio é "ainda não lançado"
 * (`null`), maior que zero vai em centavos, e **zero é recusado** — o contrato
 * o recusa (`CHECK > 0`) para não virar um segundo jeito, errado, de escrever
 * "não sei", que somaria zero na margem da estadia.
 */
export const MENSAGEM_DO_CUSTO = {
  zero: "Custo zero não existe aqui. Se ainda não sabe quanto custou, deixe o campo vazio — dá para lançar depois.",
  invalido: "Informe o valor em reais, como 350,00 — ou deixe vazio se a nota ainda não chegou.",
} as const;

function validarCusto(valor: string, ctx: z.RefinementCtx, campo = "cost"): void {
  const custo = lerCusto(valor);
  if (!custo.ok) ctx.addIssue({ code: "custom", path: [campo], message: MENSAGEM_DO_CUSTO[custo.motivo] });
}

/** Centavos inteiros ou `null`. Só chamado depois do schema, que já recusou o
 *  inválido e o zero. */
export function centavosDoCusto(valor: string): number | null {
  const custo = lerCusto(valor);
  return custo.ok ? custo.centavos : null;
}

export { custoParaCampo };

// ── Criar ───────────────────────────────────────────────────────────────────

export const OrdemFormulario = z
  .object({
    unit_id: z.string().refine((v) => UUID.test(v), "Escolha a unidade."),
    room_id: idOpcional("Escolha o cômodo de novo."),
    item_id: idOpcional("Escolha o bem de novo."),
    issue_id: idOpcional("A avaria de origem não é válida."),
    title: textoObrigatorio(1, "Diga o que precisa ser feito — “Ar da suíte não gela”.").max(
      200,
      "Título com no máximo 200 caracteres.",
    ),
    description: z.string().max(4000, "Descrição com no máximo 4000 caracteres."),
    priority: z.enum(PRIORIDADES_DA_ORDEM, { message: "Escolha a prioridade." }),
    cost: z.string(),
    bloquear: z.boolean(),
    block_from: z.string(),
    block_to: z.string(),
  })
  .superRefine((v, ctx) => {
    validarCusto(v.cost, ctx);
    // Só a forma: com "Bloquear calendário" marcado, as duas datas têm de ser
    // datas. Se o período vale é a API que diz.
    if (v.bloquear) {
      if (!ehDataISO(v.block_from.trim())) {
        ctx.addIssue({ code: "custom", path: ["block_from"], message: "Informe o primeiro dia bloqueado." });
      }
      if (!ehDataISO(v.block_to.trim())) {
        ctx.addIssue({ code: "custom", path: ["block_to"], message: "Informe o dia em que a unidade volta à venda." });
      }
    }
  });

export type OrdemFormulario = z.infer<typeof OrdemFormulario>;

export function ordemVazia(parcial: Partial<OrdemFormulario> = {}): OrdemFormulario {
  return {
    unit_id: "",
    room_id: "",
    item_id: "",
    issue_id: "",
    title: "",
    description: "",
    priority: "normal",
    cost: "",
    bloquear: false,
    block_from: "",
    block_to: "",
    ...parcial,
  };
}

/**
 * O formulário vira o corpo de `POST /maintenance-orders`.
 *
 * Cômodo, bem e avaria só vão quando preenchidos: sem avaria, ausente e `null`
 * são a mesma coisa; com avaria, ausente "vem dela" e qualquer valor informado
 * tem de ser igual ao dela — mandar `null` ao lado de `issue_id` seria pedir
 * um `422` à toa. `block` só vai com a caixa marcada.
 */
export function ordemParaCriar(v: OrdemFormulario): OrdemCriarEntrada {
  const corpo: OrdemCriarEntrada = {
    unit_id: v.unit_id,
    title: v.title.trim(),
    description: paraTextoOuNulo(v.description),
    priority: v.priority,
    cost_cents: centavosDoCusto(v.cost),
  };
  if (v.room_id) corpo.room_id = v.room_id;
  if (v.item_id) corpo.item_id = v.item_id;
  if (v.issue_id) corpo.issue_id = v.issue_id;
  if (v.bloquear) corpo.block = { from: v.block_from.trim(), to: v.block_to.trim() };
  return corpo;
}

// ── Editar (PUT) ────────────────────────────────────────────────────────────

export const EdicaoFormulario = z
  .object({
    room_id: idOpcional("Escolha o cômodo de novo."),
    item_id: idOpcional("Escolha o bem de novo."),
    title: textoObrigatorio(1, "Diga o que precisa ser feito.").max(200, "Título com no máximo 200 caracteres."),
    description: z.string().max(4000, "Descrição com no máximo 4000 caracteres."),
    priority: z.enum(PRIORIDADES_DA_ORDEM, { message: "Escolha a prioridade." }),
    cost: z.string(),
  })
  .superRefine((v, ctx) => validarCusto(v.cost, ctx));

export type EdicaoFormulario = z.infer<typeof EdicaoFormulario>;

export function valoresDaEdicao(ordem: OrdemDeManutencao): EdicaoFormulario {
  return {
    room_id: ordem.room_id ?? "",
    item_id: ordem.item_id ?? "",
    title: ordem.title,
    description: ordem.description ?? "",
    priority: ordem.priority,
    cost: custoParaCampo(ordem.cost_cents),
  };
}

/**
 * Corpo do `PUT`. Num `PUT`, campo anulável omitido vira `null` — **exceto**
 * cômodo e bem com avaria ligada, que ficam os dela. Por isso, com avaria, as
 * duas chaves não vão (mandar `null` seria `422`); sem avaria, vão sempre, e
 * vazio é `null` (limpa).
 */
export function edicaoParaSubstituir(v: EdicaoFormulario, comAvaria: boolean): OrdemSubstituirEntrada {
  const corpo: OrdemSubstituirEntrada = {
    title: v.title.trim(),
    description: paraTextoOuNulo(v.description),
    priority: v.priority,
    cost_cents: centavosDoCusto(v.cost),
  };
  if (!comAvaria) {
    corpo.room_id = v.room_id || null;
    corpo.item_id = v.item_id || null;
  }
  return corpo;
}

// ── Custo depois de concluída (PATCH) e na conclusão ────────────────────────

export const CustoFormulario = z.object({ cost: z.string() }).superRefine((v, ctx) => validarCusto(v.cost, ctx));
export type CustoFormulario = z.infer<typeof CustoFormulario>;

/** `PATCH {cost_cents}` — vazio limpa (`null`): a nota lançada errada sai. */
export function custoParaAtualizar(v: CustoFormulario): CustoDaOrdemEntrada {
  return { cost_cents: centavosDoCusto(v.cost) };
}

/** Corpo de `/complete`: sem custo, **sem corpo** — concluir não apaga o custo
 *  que já estava lançado. */
export function custoParaConcluir(v: CustoFormulario): ConclusaoEntrada | undefined {
  const centavos = centavosDoCusto(v.cost);
  return centavos === null ? undefined : { cost_cents: centavos };
}

// ── Bloqueio (PUT /{id}/block) ──────────────────────────────────────────────

export const PeriodoFormulario = z.object({
  block_from: z.string().refine((v) => ehDataISO(v.trim()), "Informe o primeiro dia bloqueado."),
  block_to: z.string().refine((v) => ehDataISO(v.trim()), "Informe o dia em que a unidade volta à venda."),
});
export type PeriodoFormulario = z.infer<typeof PeriodoFormulario>;

export function periodoParaEntrada(v: PeriodoFormulario): PeriodoDoBloqueio {
  return { from: v.block_from.trim(), to: v.block_to.trim() };
}
