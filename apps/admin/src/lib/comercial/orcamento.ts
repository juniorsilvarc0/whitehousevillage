import { z } from "zod";

import type { PedidoDeOrcamento } from "@/lib/api/comercial";
import { dataDe, inteiroDe, paraInteiro, paraNumero, percentualDe } from "@/lib/acoes/campos";

/**
 * Espelho de `PedidoDeOrcamento`.
 *
 * O nome do campo é `guests_count`, igual ao do pedido, ao da reserva e ao da
 * resposta. Era `guests` só aqui até 26/08/2026, e a assimetria custou uma
 * edição que respondia `200` sem gravar nada.
 *
 * **Nenhum valor de dinheiro entra por aqui.** O pedido leva produto, datas,
 * hóspedes e desconto; quem calcula tudo o resto é o motor do servidor. Um
 * campo de preço neste formulário seria a porta pela qual o painel começaria a
 * ter opinião sobre quanto custa a diária — e duas opiniões sobre preço é uma
 * a mais do que o sistema aguenta.
 */
export const OrcamentoFormulario = z
  .object({
    unit_type_id: z.string().min(1, "Escolha o produto."),
    check_in: dataDe("Informe a data de entrada."),
    check_out: dataDe("Informe a data de saída."),
    guests_count: inteiroDe(1, "Informe ao menos 1 hóspede."),
    discount_pct: percentualDe("O desconto precisa ser um percentual entre 0 e 100."),
    is_event: z.boolean(),
  })
  .refine((v) => v.check_out.trim() > v.check_in.trim(), {
    path: ["check_out"],
    message: "A saída precisa ser depois da entrada — a noite do check-out não é cobrada.",
  });

export type OrcamentoFormulario = z.infer<typeof OrcamentoFormulario>;

export function orcamentoParaPedido(valores: OrcamentoFormulario): PedidoDeOrcamento {
  return {
    unit_type_id: valores.unit_type_id,
    check_in: valores.check_in.trim(),
    check_out: valores.check_out.trim(),
    guests_count: paraInteiro(valores.guests_count),
    discount_pct: paraNumero(valores.discount_pct),
    is_event: valores.is_event,
  };
}
