import { formatarData } from "@/lib/datas";

/**
 * `DATE_CONFLICT` e `UNIT_NOT_AVAILABLE` traduzidos — **informação, não falha**.
 *
 * Remarcar para uma data ocupada é o desfecho mais comum da alta temporada, e a
 * transação garante que nada mudou: a reserva antiga continua de pé, exatamente
 * como estava. Tratar isso como erro vermelho ensina o operador a temer o botão
 * que ele mais precisa usar.
 *
 * `details.period` chega como o `daterange` do Postgres — `[2026-12-20,2026-12-23)`.
 * A tela não repete a notação do banco para quem vende: extrai as duas pontas e
 * escreve a frase. Formato inesperado devolve `null` em vez de um texto meio
 * traduzido — melhor dizer só o essencial do que mostrar colchete solto.
 */

export type Conflito = {
  unidade: string | null;
  /** Datas já em formato brasileiro. */
  de: string | null;
  ate: string | null;
  /** A frase pronta, ou `null` quando `details` não trouxe nada aproveitável. */
  frase: string | null;
};

export function lerConflito(details: Record<string, unknown>): Conflito {
  const unidade = typeof details.unit_code === "string" && details.unit_code !== "" ? details.unit_code : null;
  const periodo = typeof details.period === "string" ? details.period : null;
  const datas = periodo ? [...periodo.matchAll(/\d{4}-\d{2}-\d{2}/g)].map((m) => m[0]!) : [];

  const de = datas[0] ? formatarData(datas[0]) : null;
  const ate = datas[1] ? formatarData(datas[1]) : null;

  let frase: string | null = null;
  if (unidade && de && ate) frase = `A unidade ${unidade} já está ocupada de ${de} a ${ate}.`;
  else if (unidade) frase = `A unidade em conflito é a ${unidade}.`;
  else if (de && ate) frase = `O conflito é entre ${de} e ${ate}.`;

  return { unidade, de, ate, frase };
}

/**
 * `COMPOSITION_INCOMPLETE` — qual unidade reativar.
 *
 * O contrato manda `unit_type_code`, `expected_units`, `active_units` e
 * `missing_unit_codes` exatamente para a tela poder dizer o nome da unidade em
 * vez de "produto mal configurado". Quem age é a gestão do inventário, e ela
 * precisa do código na mão.
 */
export function lerComposicaoIncompleta(details: Record<string, unknown>): string | null {
  const faltando = Array.isArray(details.missing_unit_codes)
    ? details.missing_unit_codes.filter((c): c is string => typeof c === "string")
    : [];
  const produto = typeof details.unit_type_code === "string" ? details.unit_type_code : null;
  const esperadas = typeof details.expected_units === "number" ? details.expected_units : null;
  const ativas = typeof details.active_units === "number" ? details.active_units : null;

  if (faltando.length > 0) {
    return `Reative ${faltando.length === 1 ? "a unidade" : "as unidades"} ${faltando.join(", ")} no inventário${
      produto ? ` para voltar a vender a ${produto}` : ""
    }.`;
  }
  if (esperadas !== null && ativas !== null) {
    return `O produto tem ${esperadas} ${esperadas === 1 ? "unidade" : "unidades"} na composição e só ${ativas} ${
      ativas === 1 ? "está ativa" : "estão ativas"
    }. Reative no inventário antes de vender.`;
  }
  return null;
}
