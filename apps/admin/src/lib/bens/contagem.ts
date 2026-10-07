import type { AmbienteDaConferencia, LinhaDeConferencia } from "@/lib/bens/tipos";

/**
 * A contagem no celular — regras de TELA, puras e testadas em
 * `contagem.test.ts`.
 *
 * ## `null` não é `0`
 *
 * É a distinção que o módulo inteiro existe para preservar (docs/db.md §11):
 * `counted_qty: null` é "ainda não contei"; `0` é "contei e não achei nenhum".
 * Fechar a conferência com a primeira transformaria "não olhei" em "sumiu", e
 * a segunda é uma perda que alguém vai cobrar de um hóspede. Por isso a tela
 * desenha as duas de jeitos que não se confundem — traço e moldura tracejada
 * para a pendente, número cheio para a contada — e nenhum gesto aqui transforma
 * uma na outra sem querer: o "−" numa linha pendente parte do esperado, nunca
 * de zero, e o caminho de volta a pendente é um botão próprio ("Desfazer").
 *
 * Nada aqui decide divergência para valer: quem apura falta, sobra e prejuízo é
 * o fechamento, no servidor. A tela só lê a linha que já tem para pintar.
 */

export type EstadoDaLinha = "pendente" | "confere" | "falta" | "sobra";

type Contavel = Pick<LinhaDeConferencia, "expected_qty" | "counted_qty">;

export function estadoDaLinha(linha: Contavel): EstadoDaLinha {
  if (linha.counted_qty === null) return "pendente";
  if (linha.counted_qty === linha.expected_qty) return "confere";
  return linha.counted_qty < linha.expected_qty ? "falta" : "sobra";
}

/** O que vai no meio do contador: traço para pendente, o número para contada. */
export function exibicaoDaContagem(contada: number | null): string {
  return contada === null ? "—" : String(contada);
}

/** A etiqueta da linha, em palavras. */
export function rotuloDoEstado(linha: Contavel): string {
  const estado = estadoDaLinha(linha);
  switch (estado) {
    case "pendente":
      return "Não contado";
    case "confere":
      return "Confere";
    case "falta": {
      const falta = linha.expected_qty - (linha.counted_qty ?? 0);
      // Zero contado merece frase própria: é o caso que mais se confunde com
      // "não contado", e a etiqueta é onde a diferença precisa ser lida.
      return linha.counted_qty === 0 ? `Nenhum encontrado (falta ${falta})` : `Falta ${falta}`;
    }
    case "sobra":
      return `Sobra ${(linha.counted_qty ?? 0) - linha.expected_qty}`;
  }
}

/**
 * Um toque no `+` ou no `−`.
 *
 * Na linha **pendente** o passo parte do **esperado**, não de zero: quem
 * esperava 12 e achou 11 dá um toque, não onze. E é também o que impede o "−"
 * de uma linha pendente virar `0` — que seria dizer "não achei nenhum" sem ter
 * contado.
 */
export function aplicarPasso(contada: number | null, esperada: number, passo: 1 | -1): number {
  const base = contada ?? esperada;
  return Math.max(0, base + passo);
}

/**
 * O número digitado no campo do contador. Vazio não é zero: devolve `null`, e
 * quem chama decide que isso é "não mexer" — apagar o campo nunca desfaz a
 * contagem por acidente. Texto que não é inteiro não-negativo é `"invalido"`.
 */
export function lerDigitado(texto: string): number | null | "invalido" {
  const limpo = texto.trim();
  if (limpo === "") return null;
  if (!/^\d{1,6}$/.test(limpo)) return "invalido";
  return Number.parseInt(limpo, 10);
}

/** Pendentes de um ambiente, lidos das linhas que a tela tem agora. */
export function pendentesDoAmbiente(ambiente: Pick<AmbienteDaConferencia, "lines">): number {
  return ambiente.lines.filter((l) => l.counted_qty === null).length;
}

/** O próximo ambiente com pendência depois do atual, na ordem de caminhada —
 *  ou `null` quando a casa inteira já foi contada. Dá a volta: quem começou
 *  pela varanda termina pela sala. */
export function proximoComPendencia(
  ambientes: readonly Pick<AmbienteDaConferencia, "room_id" | "lines">[],
  atual: string,
): string | null {
  const i = ambientes.findIndex((a) => a.room_id === atual);
  for (let passo = 1; passo <= ambientes.length; passo++) {
    const candidato = ambientes[(Math.max(i, 0) + passo) % ambientes.length];
    if (candidato && candidato.room_id !== atual && pendentesDoAmbiente(candidato) > 0) return candidato.room_id;
  }
  return null;
}

/**
 * `details.pending_by_room` do `409 COUNT_HAS_PENDING_LINES`: a lista
 * `[{room_id, room_name, pending}]`, na ordem de caminhada e só com os
 * ambientes que têm pendência. É o que manda quem conta de volta ao cômodo
 * certo. Entrada fora dessa forma é descartada, não vira exceção na tela.
 */
export type PendenciaDoAmbiente = { roomId: string; ambiente: string; pendentes: number };

export function pendenciasPorAmbiente(bruto: unknown): PendenciaDoAmbiente[] {
  if (!Array.isArray(bruto)) return [];
  return bruto.flatMap((item) => {
    if (!item || typeof item !== "object") return [];
    const r = item as Record<string, unknown>;
    if (typeof r.room_id !== "string" || typeof r.pending !== "number") return [];
    return [{ roomId: r.room_id, ambiente: typeof r.room_name === "string" ? r.room_name : "Ambiente", pendentes: r.pending }];
  });
}

/**
 * O que vai em `note` no `POST /close`, a partir do campo (que abre com a
 * observação que a conferência já tem): `undefined` quando não mexeram —
 * mantém —, o texto novo — substitui — ou `null` quando apagaram tudo —
 * limpa. É a regra de `PedidoDeFechamento.note`, a mesma do `PATCH`.
 */
export function notaDoFechamento(digitada: string, atual: string | null): string | null | undefined {
  const limpa = digitada.trim();
  if (limpa === (atual ?? "").trim()) return undefined;
  return limpa === "" ? null : limpa;
}
