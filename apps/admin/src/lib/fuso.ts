/**
 * Fuso da operação. A casa fica no Ceará: `America/Fortaleza` não tem horário
 * de verão, mas o servidor pode estar em qualquer lugar. Toda formatação de
 * data e hora do painel passa por aqui — data de estadia exibida no fuso do
 * navegador de quem abriu a tela é como uma diária vira dois dias diferentes
 * dependendo de quem olha.
 */
export const FUSO = "America/Fortaleza";

export function horaLocal(instante: Date = new Date()): number {
  const partes = new Intl.DateTimeFormat("pt-BR", {
    timeZone: FUSO,
    hour: "numeric",
    hour12: false,
  }).format(instante);
  return Number.parseInt(partes, 10);
}

export function saudacao(instante: Date = new Date()): string {
  const hora = horaLocal(instante);
  if (hora < 12) return "Bom dia";
  if (hora < 18) return "Boa tarde";
  return "Boa noite";
}

export function dataPorExtenso(instante: Date = new Date()): string {
  return new Intl.DateTimeFormat("pt-BR", {
    timeZone: FUSO,
    weekday: "long",
    day: "2-digit",
    month: "long",
  }).format(instante);
}
