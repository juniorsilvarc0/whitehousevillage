import { z } from "zod";

import { DIAS_MAXIMO } from "@/lib/mapa/janela";

/**
 * Espelho em zod do que o contrato aceita — validação do lado de cá para o erro
 * chegar no campo certo antes de gastar uma requisição.
 *
 * **Não substitui o servidor**: os mesmos tetos existem no DTO do Go, e é lá que
 * eles valem. O que se ganha aqui é a mensagem em português apontando para o
 * campo que a pessoa encurta.
 */

const DATA = z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "Data inválida.");

/** `maintenance` e `owner_hold`, e só. `ota` é do importador de canais e
 *  `reservation` nasce com a reserva — nenhum dos dois se cria por esta rota. */
export const OrigemDoBloqueio = z.enum(["maintenance", "owner_hold"]);
export type OrigemDoBloqueio = z.infer<typeof OrigemDoBloqueio>;

export const BloqueioFormulario = z
  .object({
    unit_id: z.string().uuid("Escolha a unidade."),
    from: DATA,
    to: DATA,
    source: OrigemDoBloqueio,
    note: z.string().trim().max(500, "No máximo 500 caracteres.").optional(),
  })
  .refine((v) => v.to > v.from, {
    // Half-open `[from, to)`: bloquear 10 → 12 ocupa 10 e 11 e deixa 12 livre
    // para check-in. Aceitar `to === from` criaria um bloqueio de zero noite.
    message: "A saída tem de ser posterior à entrada.",
    path: ["to"],
  })
  .refine(
    (v) => {
      const noites = (Date.parse(`${v.to}T00:00:00Z`) - Date.parse(`${v.from}T00:00:00Z`)) / 86_400_000;
      return noites <= 365;
    },
    {
      // O teto é do contrato (`422 VALIDATION_ERROR` em `details.to`) e existe
      // por um erro medido: um bloqueio de catorze anos criado com uma tecla
      // errada. Reforma de temporada inteira se parte em bloqueios de até um
      // ano; unidade que sai do catálogo é `active = false` no inventário.
      message: "Um bloqueio vale no máximo 365 noites. Divida em períodos menores.",
      path: ["to"],
    },
  );

export type BloqueioFormulario = z.infer<typeof BloqueioFormulario>;

export const JanelaFormulario = z.object({
  from: DATA,
  dias: z.number().int().min(7).max(DIAS_MAXIMO),
});
