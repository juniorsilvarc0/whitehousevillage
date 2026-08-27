import { z } from "zod";

import type { FeriadoEntrada, PeriodoEspecialEntrada } from "@/lib/api/comercial";
import { dataDe, textoObrigatorio } from "@/lib/acoes/campos";

/** Espelho de `FeriadoEntrada`. */
export const FeriadoFormulario = z.object({
  date: dataDe("Informe a data do feriado."),
  name: textoObrigatorio(2, "Informe o nome do feriado."),
  active: z.boolean(),
});

export type FeriadoFormulario = z.infer<typeof FeriadoFormulario>;

export function feriadoParaEntrada(valores: FeriadoFormulario): FeriadoEntrada {
  return { date: valores.date.trim(), name: valores.name.trim(), active: valores.active };
}

/**
 * Espelho de `PeriodoEspecialEntrada`.
 *
 * `ends_on` é **inclusivo** — é a exceção declarada do contrato, e a validação
 * aceita `ends_on == starts_on` justamente por isso: um período de um dia só é
 * legítimo, e recusá-lo seria tratar a faixa como se fosse half-open igual à
 * estadia.
 */
export const PeriodoFormulario = z
  .object({
    name: textoObrigatorio(2, "Informe o nome do período."),
    kind: z.enum(["reveillon", "carnaval", "alta", "evento"]),
    starts_on: dataDe("Informe o início do período."),
    ends_on: dataDe("Informe o fim do período."),
    active: z.boolean(),
  })
  .refine((v) => v.ends_on.trim() >= v.starts_on.trim(), {
    path: ["ends_on"],
    message: "O fim não pode ser anterior ao início.",
  });

export type PeriodoFormulario = z.infer<typeof PeriodoFormulario>;

export function periodoParaEntrada(valores: PeriodoFormulario): PeriodoEspecialEntrada {
  return {
    name: valores.name.trim(),
    kind: valores.kind,
    starts_on: valores.starts_on.trim(),
    ends_on: valores.ends_on.trim(),
    active: valores.active,
  };
}
