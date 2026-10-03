import { z } from "zod";

import type {
  FaixaDeCancelamento,
  PoliticaComercialEntrada,
  PoliticaDeCancelamentoEntrada,
} from "@/lib/api/comercial";
import {
  dataDe,
  dinheiroDe,
  inteiroDe,
  paraCentavos,
  paraInteiro,
  paraNumero,
  percentualDe,
  textoObrigatorio,
} from "@/lib/acoes/campos";

/**
 * Política comercial — espelho de `PoliticaComercialEntrada`.
 *
 * `version` não entra: quem numera é o servidor, e aceitar o número do cliente
 * seria abrir a porta para reescrever uma versão já publicada — exatamente o
 * que `reservations.policy_version` existe para impedir.
 */
export const PoliticaComercialFormulario = z
  .object({
    deposit_pct: percentualDe("O sinal precisa ser um percentual entre 0 e 100."),
    balance_due_days: inteiroDe(0, "Informe em quantos dias antes do check-in o saldo vence."),
    hold_hours: inteiroDe(1, "A pré-reserva precisa durar ao menos 1 hora."),
    discount_auto_pct: percentualDe("Informe o limite de desconto da gestão, entre 0 e 100."),
    discount_approval_pct: percentualDe("Informe o limite de desconto com aprovação do proprietário, entre 0 e 100."),
    event_deposit_cents: dinheiroDe(0, "Informe a caução de evento em reais (0 se não houver)."),
    valid_from: dataDe("Informe a partir de quando esta versão vale."),
  })
  .refine((v) => paraNumero(v.discount_approval_pct) >= paraNumero(v.discount_auto_pct), {
    path: ["discount_approval_pct"],
    message: "O limite com aprovação do proprietário não pode ser menor que o da gestão.",
  });

export type PoliticaComercialFormulario = z.infer<typeof PoliticaComercialFormulario>;

export function politicaComercialParaEntrada(v: PoliticaComercialFormulario): PoliticaComercialEntrada {
  return {
    deposit_pct: paraNumero(v.deposit_pct),
    balance_due_days: paraInteiro(v.balance_due_days),
    hold_hours: paraInteiro(v.hold_hours),
    discount_auto_pct: paraNumero(v.discount_auto_pct),
    discount_approval_pct: paraNumero(v.discount_approval_pct),
    event_deposit_cents: paraCentavos(v.event_deposit_cents),
    valid_from: v.valid_from.trim(),
  };
}

// ─────────────────── Política de cancelamento ──────────────────────────────

const limiteEmDias = z
  .string()
  .refine((v) => v.trim() === "" || /^\d+$/.test(v.trim()), "Use um número inteiro de dias, ou deixe em branco.");

export const FaixaFormulario = z.object({
  days_before_min: limiteEmDias,
  days_before_max: limiteEmDias,
  refund_pct: percentualDe("A devolução precisa ser um percentual entre 0 e 100."),
  label: textoObrigatorio(2, "Escreva o texto que a tela mostrará ao cancelar."),
});

export type FaixaFormulario = z.infer<typeof FaixaFormulario>;

const SEM_PISO = Number.NEGATIVE_INFINITY;
const SEM_TETO = Number.POSITIVE_INFINITY;

function limites(faixa: FaixaFormulario): [number, number] {
  const min = faixa.days_before_min.trim() === "" ? SEM_PISO : Number(faixa.days_before_min.trim());
  const max = faixa.days_before_max.trim() === "" ? SEM_TETO : Number(faixa.days_before_max.trim());
  return [min, max];
}

/**
 * As faixas inteiras, sempre juntas.
 *
 * Faixa avulsa não é política: um buraco entre o teto de uma e o piso da
 * seguinte faz o motor cair no caso "sem faixa aplicável" e reter tudo por
 * omissão — o hóspede perde o sinal por um erro de cadastro. Por isso a
 * validação recusa **sobreposição** (que o servidor também recusa) e a tela
 * avisa sobre **buraco**, que é o defeito silencioso.
 */
export const PoliticaDeCancelamentoFormulario = z
  .object({
    name: textoObrigatorio(2, "Informe o nome da política."),
    valid_from: dataDe("Informe a partir de quando esta versão vale."),
    tiers: z.array(FaixaFormulario).min(1, "A política precisa de ao menos uma faixa."),
  })
  .superRefine((valores, ctx) => {
    valores.tiers.forEach((faixa, indice) => {
      const [min, max] = limites(faixa);
      if (min > max) {
        ctx.addIssue({
          code: "custom",
          path: ["tiers", indice, "days_before_max"],
          message: "O teto não pode ser menor que o piso.",
        });
      }
    });

    for (let a = 0; a < valores.tiers.length; a += 1) {
      for (let b = a + 1; b < valores.tiers.length; b += 1) {
        const [minA, maxA] = limites(valores.tiers[a]!);
        const [minB, maxB] = limites(valores.tiers[b]!);
        if (minA <= maxB && minB <= maxA) {
          ctx.addIssue({
            code: "custom",
            path: ["tiers", b, "days_before_min"],
            message: "Esta faixa se sobrepõe a outra. Cada antecedência tem que cair em uma faixa só.",
          });
        }
      }
    }
  });

export type PoliticaDeCancelamentoFormulario = z.infer<typeof PoliticaDeCancelamentoFormulario>;

export function politicaDeCancelamentoParaEntrada(
  valores: PoliticaDeCancelamentoFormulario,
): PoliticaDeCancelamentoEntrada {
  return {
    name: valores.name.trim(),
    valid_from: valores.valid_from.trim(),
    // `sort_order` é a ordem de avaliação, e a ordem de avaliação é a ordem da
    // tela: o motor pega a primeira faixa aplicável de cima para baixo.
    tiers: valores.tiers.map((faixa, indice) => paraFaixa(faixa, indice)),
  };
}

function paraFaixa(faixa: FaixaFormulario, indice: number): FaixaDeCancelamento {
  return {
    days_before_min: faixa.days_before_min.trim() === "" ? null : paraInteiro(faixa.days_before_min),
    days_before_max: faixa.days_before_max.trim() === "" ? null : paraInteiro(faixa.days_before_max),
    refund_pct: paraNumero(faixa.refund_pct),
    label: faixa.label.trim(),
    sort_order: indice,
  };
}

/**
 * Antecedências que **nenhuma** faixa cobre.
 *
 * Devolve os intervalos em dias que ficaram descobertos, para a tela avisar
 * antes de publicar. Não bloqueia: quem decide é o servidor. Mas avisa, porque
 * o buraco é o defeito silencioso — o motor não acha faixa aplicável e retém
 * tudo por omissão, e ninguém descobre até o primeiro hóspede reclamar.
 *
 * A varredura anda da antecedência 0 (cancelou no dia) para cima. `null` no
 * piso é 0; `null` no teto é infinito.
 */
export function buracosNasFaixas(faixas: FaixaFormulario[]): string[] {
  const intervalos = faixas
    .map((faixa): [number, number] => {
      const [min, max] = limites(faixa);
      return [min === SEM_PISO ? 0 : min, max];
    })
    .filter(([min, max]) => min <= max)
    .sort((a, b) => a[0] - b[0]);

  if (intervalos.length === 0) return [];

  const buracos: string[] = [];
  let proximoDescoberto = 0;

  for (const [min, max] of intervalos) {
    if (min > proximoDescoberto) {
      buracos.push(
        min - 1 === proximoDescoberto
          ? `${proximoDescoberto} dia`
          : `de ${proximoDescoberto} a ${min - 1} dias`,
      );
    }
    proximoDescoberto = Math.max(proximoDescoberto, max === SEM_TETO ? SEM_TETO : max + 1);
  }

  if (proximoDescoberto !== SEM_TETO) buracos.push(`${proximoDescoberto} dias ou mais`);
  return buracos;
}
