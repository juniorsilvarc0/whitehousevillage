import { formatarData, somarDias } from "@/lib/datas";
import type {
  AcaoDaOrdem,
  BloqueioDaOrdem,
  FaseDoBloqueio,
  PrioridadeDaOrdem,
  StatusDaOrdem,
} from "@/lib/manutencao/tipos";

/**
 * O vocabulário das ordens de manutenção, dito na língua de quem conserta.
 *
 * `Record<…>` e não `switch`: valor novo no enum do contrato vira erro de
 * compilação aqui, em vez de um rótulo vazio na tela do celular.
 */

export const ROTULO_DO_STATUS: Record<StatusDaOrdem, string> = {
  aberta: "Aberta",
  em_andamento: "Em andamento",
  concluida: "Concluída",
  cancelada: "Cancelada",
};

export const ROTULO_DA_PRIORIDADE: Record<PrioridadeDaOrdem, string> = {
  baixa: "Baixa",
  normal: "Normal",
  alta: "Alta",
  urgente: "Urgente",
};

export const ROTULO_DA_ACAO: Record<AcaoDaOrdem, string> = {
  start: "Iniciar",
  complete: "Concluir",
  cancel: "Cancelar ordem",
};

export const ROTULO_DA_FASE: Record<FaseDoBloqueio, string> = {
  agendado: "Bloqueio agendado",
  em_curso: "Bloqueio em curso",
  encerrado: "Bloqueio encerrado",
  liberado: "Bloqueio liberado",
};

/**
 * O período do bloqueio por extenso, a partir do que a API devolveu.
 *
 * **Nada aqui decide coisa alguma.** A fase vem pronta (`block.phase`, derivada
 * pela API no fuso da propriedade) e o número de noites também (`nights`). O
 * que esta função faz é só escrever o half-open de um jeito que ninguém leia
 * errado: `to` é o dia em que a unidade **volta à venda**, então a última noite
 * bloqueada é a véspera dele — e é isso que a frase diz, em vez de mostrar
 * "10/11 a 15/11" e deixar a operação achar que o dia 15 está tomado.
 */
export function periodoPorExtenso(bloqueio: Pick<BloqueioDaOrdem, "from" | "to" | "nights">): string {
  const ultima = somarDias(bloqueio.to, -1);
  const noites = `${bloqueio.nights} ${bloqueio.nights === 1 ? "noite" : "noites"}`;
  const quais =
    ultima === bloqueio.from
      ? `noite de ${formatarData(bloqueio.from)}`
      : `noites de ${formatarData(bloqueio.from)} a ${formatarData(ultima)}`;
  return `${quais} (${noites}) · livre a partir de ${formatarData(bloqueio.to)}`;
}

/**
 * A frase curta do cartão da lista: "Bloqueio em curso até a noite de 14/11".
 * Uma linha, para caber no celular ao lado do selo de prioridade.
 */
export function faseCurta(bloqueio: Pick<BloqueioDaOrdem, "from" | "to" | "phase">): string {
  const ultima = formatarData(somarDias(bloqueio.to, -1));
  switch (bloqueio.phase) {
    case "agendado":
      return `Bloqueio agendado: de ${formatarData(bloqueio.from)} até a noite de ${ultima}`;
    case "em_curso":
      return `Bloqueio em curso até a noite de ${ultima}`;
    case "encerrado":
      return `Bloqueio encerrado em ${formatarData(bloqueio.to)}`;
    case "liberado":
      return "Bloqueio liberado — as datas voltaram à venda";
  }
}

/**
 * Como o bloqueio é liberado — ao encerrar a ordem e ao soltá-lo. A regra é da
 * API (`maintenance.ReleaseOn`); a frase só a anuncia, de forma genérica, sem
 * olhar datas nem fase, para não virar uma segunda cópia que diverge no dia em
 * que só a primeira mudar.
 *
 * Não diz "o que está em curso termina hoje": um bloqueio que **começa hoje**
 * aparece com a fase `em_curso`, e liberá-lo hoje o solta inteiro, como se não
 * tivesse começado. A frase fala em "começou antes de hoje", que é o caso em
 * que sobra alguma noite.
 */
export const COMO_O_BLOQUEIO_E_LIBERADO =
  "O bloqueio que ainda não começou — ou que começa hoje — sai inteiro. O que começou antes de hoje fica só com as noites que já passaram, e a noite de hoje volta à venda.";

/** O que acontece com o calendário ao encerrar (concluir ou cancelar). */
export function avisoDoEncerramento(bloqueio: unknown): string {
  if (!bloqueio) return "Esta ordem não bloqueou o calendário: nada muda nas datas da unidade.";
  return `O calendário é liberado. ${COMO_O_BLOQUEIO_E_LIBERADO}`;
}
