import { z } from "zod";

import type { ProdutoEntrada, UnidadeEntrada } from "@/lib/api/comercial";
import {
  dinheiroDe,
  inteiroDe,
  paraCentavos,
  paraInteiro,
  paraTextoOuNulo,
  textoObrigatorio,
} from "@/lib/acoes/campos";

/**
 * Espelho dos DTOs `ProdutoCriar` e `UnidadeCriar` da OpenAPI.
 *
 * Vive fora do arquivo de actions de propósito: o mesmo schema valida no
 * navegador (para o erro aparecer sem ida ao servidor) e dentro da action (para
 * a validação existir de verdade — Server Action é endpoint público, e confiar
 * no formulário seria confiar em quem chamou).
 */

export const ProdutoFormulario = z.object({
  code: textoObrigatorio(2, "Informe o código do produto."),
  name: textoObrigatorio(2, "Informe o nome do produto."),
  capacity: inteiroDe(1, "A capacidade precisa ser de pelo menos 1 hóspede."),
  consumes: z.enum(["one_member", "all_members"]),
  cleaning_fee_cents: dinheiroDe(0, "Informe a taxa de limpeza em reais (0 se não houver)."),
  description: z.string(),
  sort_order: inteiroDe(0, "A ordem precisa ser um número inteiro."),
  active: z.boolean(),
});

export type ProdutoFormulario = z.infer<typeof ProdutoFormulario>;

export function produtoParaEntrada(valores: ProdutoFormulario): ProdutoEntrada {
  return {
    code: valores.code.trim().toUpperCase(),
    name: valores.name.trim(),
    capacity: paraInteiro(valores.capacity),
    consumes: valores.consumes,
    cleaning_fee_cents: paraCentavos(valores.cleaning_fee_cents),
    description: paraTextoOuNulo(valores.description),
    sort_order: paraInteiro(valores.sort_order),
    active: valores.active,
  };
}

export const UnidadeFormulario = z.object({
  code: textoObrigatorio(2, "Informe o código da unidade (AP-01, SP-04, COB-01…)."),
  name: textoObrigatorio(2, "Informe o nome da unidade."),
  floor: z.string(),
  notes: z.string(),
  sort_order: inteiroDe(0, "A ordem precisa ser um número inteiro."),
  active: z.boolean(),
});

export type UnidadeFormulario = z.infer<typeof UnidadeFormulario>;

export function unidadeParaEntrada(valores: UnidadeFormulario): UnidadeEntrada {
  return {
    code: valores.code.trim().toUpperCase(),
    name: valores.name.trim(),
    floor: paraTextoOuNulo(valores.floor),
    notes: paraTextoOuNulo(valores.notes),
    sort_order: paraInteiro(valores.sort_order),
    active: valores.active,
  };
}

/**
 * Composição: **lista vazia é recusada** aqui e no servidor.
 *
 * Não é rigor por rigor. Produto vendável sem unidade nenhuma é um overbooking
 * silencioso: a venda não teria o que inserir em `stay_blocks`, e a constraint
 * que impede duas reservas na mesma data simplesmente não teria linha para
 * comparar. O `422` do contrato diz a mesma coisa.
 */
export const ComposicaoFormulario = z.object({
  unit_ids: z.array(z.string().uuid()).min(1, "Escolha ao menos uma unidade — produto sem composição não pode ser vendido."),
});

export type ComposicaoFormulario = z.infer<typeof ComposicaoFormulario>;
