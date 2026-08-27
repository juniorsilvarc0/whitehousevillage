import { z } from "zod";

import { dataDe, dinheiroDe, textoObrigatorio } from "@/lib/acoes/campos";

/**
 * Os formulários do CRM, em zod — espelhando o DTO do contrato.
 *
 * **Todo campo é `string`**, inclusive dinheiro e data, pela mesma razão de
 * `lib/acoes/campos.ts`: o formulário fala a língua do `<input>` e a conversão
 * acontece num lugar só, na fronteira com o DTO. Campo opcional é `""`, e é a
 * função `para*` que decide se `""` vira `null` (limpar) ou some do corpo.
 */

/** `POST /crm/opportunities/{id}/lose`. O motivo é obrigatório por decisão de
 *  produto (spec §7): é dele que sai o relatório de motivos de perda. */
export const PerdaFormulario = z.object({
  lost_reason_id: textoObrigatorio(1, "Escolha o motivo da perda."),
  note: z.string().max(500, "No máximo 500 caracteres."),
});
export type PerdaFormulario = z.infer<typeof PerdaFormulario>;

/** `POST /crm/opportunities/{id}/win`. `quote_id` vazio deixa o servidor usar o
 *  orçamento vigente da oportunidade. */
export const GanhoFormulario = z.object({
  note: z.string().max(500, "No máximo 500 caracteres."),
});
export type GanhoFormulario = z.infer<typeof GanhoFormulario>;

/**
 * `POST /crm/leads/{id}/convert`. Tudo opcional: o que ficar em branco é
 * **herdado do lead**, e é assim que a conversão de um clique funciona.
 */
export const ConversaoFormulario = z
  .object({
    pipeline_id: z.string(),
    stage_id: z.string(),
    unit_type_id: z.string(),
    check_in: z.union([z.literal(""), dataDe("Data inválida.")]),
    check_out: z.union([z.literal(""), dataDe("Data inválida.")]),
    amount: z.union([z.literal(""), dinheiroDe(0, "Valor inválido.")]),
    expected_close: z.union([z.literal(""), dataDe("Data inválida.")]),
  })
  .refine((v) => !(v.check_in && v.check_out) || v.check_out > v.check_in, {
    // A saída é exclusiva: 20→21 é uma noite, e 20→20 é nenhuma. O servidor
    // recusa com 422, mas deixar o clique sair para colher a recusa ensina o
    // operador a ignorar mensagem de erro.
    path: ["check_out"],
    message: "A saída tem de ser depois da entrada — a noite do check-out não é cobrada.",
  });
export type ConversaoFormulario = z.infer<typeof ConversaoFormulario>;

/**
 * Os campos que a tela da oportunidade edita **inline**, um de cada vez.
 *
 * Cada campo é validado sozinho porque o `blur` salva sozinho: um schema do
 * objeto inteiro reprovaria a edição de um campo por causa de outro que o
 * usuário nem tocou.
 */
export const CampoInline = {
  amount: dinheiroDe(0, "Valor inválido."),
  probability: z
    .string()
    .refine((v) => /^\d+$/.test(v.trim()) && Number(v.trim()) <= 100, "De 0 a 100."),
  data: z.union([z.literal(""), dataDe("Data inválida.")]),
  texto: z.string(),
} as const;

/** `POST /crm/activities` com `type: nota`. Nota não tem prazo nem conclusão. */
export const NotaFormulario = z.object({
  body: textoObrigatorio(1, "Escreva a nota."),
});
export type NotaFormulario = z.infer<typeof NotaFormulario>;

/** `POST /crm/activities` — a tarefa criada à mão na tela da oportunidade. */
export const TarefaFormulario = z.object({
  type: z.enum(["tarefa", "ligacao", "reuniao", "email", "whatsapp"]),
  subject: textoObrigatorio(1, "Escreva o assunto."),
  due_date: z.union([z.literal(""), dataDe("Data inválida.")]),
  due_time: z.union([z.literal(""), z.string().regex(/^\d{2}:\d{2}$/, "Hora inválida.")]),
  priority: z.enum(["baixa", "normal", "alta"]),
});
export type TarefaFormulario = z.infer<typeof TarefaFormulario>;
