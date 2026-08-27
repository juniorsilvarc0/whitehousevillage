/**
 * O contador de expiração da pré-reserva.
 *
 * A célula `hold` do design system é tracejada **com contador** por um motivo
 * comercial: `hold` é data segurada sem dinheiro, e o §5 da spec dá 48 h. Uma
 * barra tracejada sem número diz "alguém está pensando"; com número, diz "esta
 * data volta ao estoque hoje às 16h" — que é a informação sobre a qual alguém
 * liga para o hóspede.
 *
 * Duas regras que este arquivo carrega:
 *
 * - **A tela nunca calcula o prazo.** `expires_at` é o instante que a API
 *   gravou (`now() + política`, e `extend-hold` o move). Reproduzir "48 h a
 *   partir da criação" aqui erraria em toda pré-reserva estendida.
 * - **Expirada não some.** Entre o vencimento e a passagem do job (que roda a
 *   cada minuto) a data ainda aparece ocupada, porque ela ainda está: o bloco
 *   só vira `expired` quando o job passar. Mostrar "expirada" é o que separa
 *   "ocupada" de "prestes a liberar".
 */
export type UrgenciaDoHold = "expirada" | "critica" | "atencao" | "tranquila";

export type Expiracao = {
  urgencia: UrgenciaDoHold;
  restanteMs: number;
  /** `expira em 3 h 12 min` — para o detalhe e o `title`. */
  texto: string;
  /** `3h12` — para caber dentro da barra, onde há centímetros e não frases. */
  curto: string;
};

const MINUTO = 60_000;
const HORA = 60 * MINUTO;
const DIA = 24 * HORA;

/** Menos de 2 h é "vai vencer no meu turno". */
const CRITICA = 2 * HORA;
/** Menos de 6 h ainda dá para ligar hoje. */
const ATENCAO = 6 * HORA;

export function expiracaoDeHold(expiresAt: string | null | undefined, agora: number): Expiracao | null {
  if (!expiresAt) return null;
  const alvo = Date.parse(expiresAt);
  if (Number.isNaN(alvo)) return null;

  const restanteMs = alvo - agora;
  if (restanteMs <= 0) {
    return { urgencia: "expirada", restanteMs, texto: "pré-reserva expirada", curto: "expirada" };
  }

  const dias = Math.floor(restanteMs / DIA);
  const horas = Math.floor((restanteMs % DIA) / HORA);
  // Arredonda o minuto para CIMA: faltando 30 s, "expira em 1 min" é verdade
  // por mais tempo do que "expira em 0 min", que fica errado no instante
  // seguinte e parece defeito.
  const minutos = Math.ceil((restanteMs % HORA) / MINUTO);

  const urgencia: UrgenciaDoHold =
    restanteMs < CRITICA ? "critica" : restanteMs < ATENCAO ? "atencao" : "tranquila";

  if (dias > 0) {
    return { urgencia, restanteMs, texto: `expira em ${dias} d ${horas} h`, curto: `${dias}d${horas}` };
  }
  if (horas > 0) {
    return { urgencia, restanteMs, texto: `expira em ${horas} h ${minutos} min`, curto: `${horas}h${String(minutos).padStart(2, "0")}` };
  }
  return { urgencia, restanteMs, texto: `expira em ${minutos} min`, curto: `${minutos}min` };
}
