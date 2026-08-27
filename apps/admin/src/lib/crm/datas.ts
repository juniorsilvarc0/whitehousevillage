import { FUSO } from "@/lib/fuso";

/**
 * Data + hora digitadas → instante ISO, no fuso da **operação**.
 *
 * O painel proíbe `datetime-local` (docs/ui.md §10) e pede campos separados de
 * data e hora. Juntá-los com `new Date("2026-12-20T09:00")` interpretaria a
 * hora no fuso de **quem abriu a tela**: a tarefa marcada para as 9h por quem
 * está em Lisboa nasceria às 5h da manhã em Fortaleza, e o alerta de tarefa
 * vencida dispararia meio dia antes.
 *
 * O deslocamento é **perguntado ao Intl** para a data em questão, e não escrito
 * como `-03:00`. Fortaleza não tem horário de verão hoje; a constante ficaria
 * certa por anos e erraria em silêncio no dia em que a regra mudasse — e é
 * justamente por isso que `lib/fuso.ts` existe em vez de um número.
 */
function deslocamentoDoFuso(dataISO: string): string {
  try {
    const partes = new Intl.DateTimeFormat("en-US", {
      timeZone: FUSO,
      timeZoneName: "longOffset",
    }).formatToParts(new Date(`${dataISO}T12:00:00Z`));
    const nome = partes.find((p) => p.type === "timeZoneName")?.value ?? "";
    const casado = /GMT([+-]\d{2}:\d{2})/.exec(nome);
    if (casado) return casado[1]!;
  } catch {
    // Ambiente sem `longOffset` (runtime antigo). Cai no deslocamento atual da
    // casa, que é o comportamento menos errado disponível.
  }
  return "-03:00";
}

/** `("2026-12-20", "09:00")` → `"2026-12-20T09:00:00-03:00"`. Hora vazia é 09:00,
 *  o começo do expediente — tarefa sem hora vira tarefa da meia-noite, que
 *  chega como vencida antes de alguém abrir o painel. */
export function instanteDaOperacao(data: string, hora: string): string {
  const horario = /^\d{2}:\d{2}$/.test(hora) ? hora : "09:00";
  return `${data}T${horario}:00${deslocamentoDoFuso(data)}`;
}
