import { z } from "zod";

import type { TabelaDeTarifasEntrada } from "@/lib/api/comercial";
import { dataDe, inteiroDe, paraInteiro, textoObrigatorio } from "@/lib/acoes/campos";

/** Espelho de `TabelaDeTarifasEntrada`. */
export const TabelaFormulario = z
  .object({
    name: textoObrigatorio(2, "Informe o nome da tabela."),
    valid_from: dataDe("Informe a data de início da vigência."),
    valid_to: z.string(),
    active: z.boolean(),
  })
  .refine(
    (v) => v.valid_to.trim() === "" || v.valid_to.trim() >= v.valid_from.trim(),
    { path: ["valid_to"], message: "O fim da vigência não pode ser anterior ao início." },
  );

export type TabelaFormulario = z.infer<typeof TabelaFormulario>;

export function tabelaParaEntrada(valores: TabelaFormulario): TabelaDeTarifasEntrada {
  return {
    name: valores.name.trim(),
    valid_from: valores.valid_from.trim(),
    // `null` é "sem fim" no contrato — a tabela vigente até segunda ordem.
    valid_to: valores.valid_to.trim() === "" ? null : valores.valid_to.trim(),
    active: valores.active,
  };
}

/** Espelho de `MinimoDeNoitesEntrada`, sem o `rate_table_id` (vem da tela). */
export const MinimoFormulario = z.object({
  date_type: z.enum(["normal", "fds", "feriado", "alta", "reveillon", "carnaval"]),
  nights: inteiroDe(1, "O mínimo precisa ser de pelo menos 1 noite."),
});

export type MinimoFormulario = z.infer<typeof MinimoFormulario>;

export function minimoParaNoites(valores: MinimoFormulario): number {
  return paraInteiro(valores.nights);
}
