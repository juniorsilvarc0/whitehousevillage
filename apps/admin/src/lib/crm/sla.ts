import type { CardDaOportunidade, FaixaDeSLA } from "@/lib/crm/tipos";

/**
 * A leitura da faixa de SLA — **só a redação, nunca a conta**.
 *
 * `days_left` e `breached` vêm calculados do servidor, com o relógio de
 * `America/Fortaleza` (o contrato diz isso em `FaixaDeSLA`). Recalcular aqui
 * daria uma resposta por fuso de quem abriu a tela: o corretor em viagem veria
 * "vence hoje" num card que a gestão vê como estourado, e os dois estariam
 * olhando o mesmo dado.
 *
 * O que sobra para o painel é escolher **a palavra e o tom** — e isso é
 * decisão de produto o bastante para ter teste.
 */

export type TomDeSLA = "sem_sla" | "estourado" | "vence_hoje" | "no_prazo";

export type LeituraDeSLA = {
  tom: TomDeSLA;
  /** A frase curta da faixa: "Estourou há 2 dias", "Vence hoje", "Faltam 3 dias". */
  rotulo: string;
  /** O contexto: qual etapa, e desde quando o card está nela. */
  detalhe: string;
  /** `true` quando a faixa exige a cor de recusa — o que o card mostra. */
  alarme: boolean;
};

function plural(n: number, singular: string, plural_: string): string {
  return `${n} ${n === 1 ? singular : plural_}`;
}

/**
 * A leitura da faixa da tela da oportunidade.
 *
 * `breached` manda sobre `days_left`. Os dois vêm do mesmo cálculo do servidor,
 * mas se um dia divergirem (arredondamento na virada do dia, por exemplo) é o
 * booleano que decide a cor — porque é ele que o filtro `sla=estourado` da
 * listagem usa, e faixa vermelha na tela com card fora do filtro é pior que
 * qualquer imprecisão de texto.
 */
export function lerSLA(faixa: FaixaDeSLA): LeituraDeSLA {
  const naEtapa = `Na etapa ${faixa.stage_name}`;

  if (faixa.sla_days === null) {
    return {
      tom: "sem_sla",
      rotulo: "Etapa sem SLA",
      detalhe: `${naEtapa}. Esta etapa não cobra prazo.`,
      alarme: false,
    };
  }

  const prazo = `${naEtapa}, com ${plural(faixa.sla_days, "dia", "dias")} de prazo.`;

  if (faixa.breached) {
    const atraso = faixa.days_left === null ? null : Math.abs(faixa.days_left);
    return {
      tom: "estourado",
      rotulo: atraso === null || atraso === 0 ? "SLA estourou hoje" : `Estourou há ${plural(atraso, "dia", "dias")}`,
      detalhe: prazo,
      alarme: true,
    };
  }

  if (faixa.days_left === 0) {
    return { tom: "vence_hoje", rotulo: "Vence hoje", detalhe: prazo, alarme: false };
  }

  return {
    tom: "no_prazo",
    rotulo: faixa.days_left === null ? "No prazo" : `${faixa.days_left === 1 ? "Falta" : "Faltam"} ${plural(faixa.days_left, "dia", "dias")}`,
    detalhe: prazo,
    alarme: false,
  };
}

/**
 * A faixa do **card** do kanban.
 *
 * O card não recebe `days_left` — o contrato manda só `sla_due_at` e
 * `sla_breached` em `CardDaOportunidade`. É de propósito: contar dias no
 * navegador para 200 cards é 200 chances de discordar do servidor. A faixa do
 * card é binária, e o número exato mora na tela da oportunidade.
 */
export function faixaDoCard(card: Pick<CardDaOportunidade, "sla_breached" | "sla_due_at">): LeituraDeSLA | null {
  if (!card.sla_breached) return null;
  return {
    tom: "estourado",
    rotulo: "SLA estourado",
    detalhe: "O prazo desta etapa passou.",
    alarme: true,
  };
}

/** As classes de cada tom. Ficam aqui, e não espalhadas nos componentes, para
 *  a faixa do card e a da tela nunca divergirem de cor. */
export const CLASSE_DO_TOM: Record<TomDeSLA, string> = {
  sem_sla: "border-border/60 bg-muted/40 text-muted-foreground",
  estourado: "border-destructive/35 bg-destructive/12 text-destructive",
  vence_hoje: "border-alcada-atencao/40 bg-alcada-atencao/12 text-foreground",
  no_prazo: "border-alcada-livre/35 bg-alcada-livre/10 text-foreground",
};
