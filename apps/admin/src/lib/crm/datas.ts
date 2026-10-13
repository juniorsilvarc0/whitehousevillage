import { deslocamentoDaCasa } from "@/lib/fuso";

/**
 * Data + hora digitadas → instante ISO, no fuso da **operação**.
 *
 * O painel proíbe `datetime-local` (docs/ui.md §10) e pede campos separados de
 * data e hora. Juntá-los com `new Date("2026-12-20T09:00")` interpretaria a
 * hora no fuso de **quem abriu a tela**: a tarefa marcada para as 9h por quem
 * está em Lisboa nasceria às 5h da manhã em Fortaleza, e o alerta de tarefa
 * vencida dispararia meio dia antes.
 *
 * O deslocamento vem de `lib/fuso.ts` (`deslocamentoDaCasa`), que o pergunta ao
 * Intl em vez de escrever `-03:00`. Até 13/10/2026 este arquivo tinha a própria
 * cópia da conversa com o Intl; a agenda precisou da mesma conversão, e o
 * helper de fuso passou a ser um só para os dois (spec §9).
 */

/** `("2026-12-20", "09:00")` → `"2026-12-20T09:00:00-03:00"`. Hora vazia é 09:00,
 *  o começo do expediente — tarefa sem hora vira tarefa da meia-noite, que
 *  chega como vencida antes de alguém abrir o painel. */
export function instanteDaOperacao(data: string, hora: string): string {
  const horario = /^\d{2}:\d{2}$/.test(hora) ? hora : "09:00";
  return `${data}T${horario}:00${deslocamentoDaCasa(data, horario)}`;
}
