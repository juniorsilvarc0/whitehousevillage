import type { CodigoDeErro } from "@/lib/api/codigos";
import { mensagemDoErro, type Falha } from "@/lib/acoes/resultado";

/**
 * O que dizer quando o inventário de bens recusa — **pelo código**, nunca pelo
 * texto da API. Os `details` entram só onde o contrato os promete (contagens
 * de `RESOURCE_IN_USE`, `count_id` de `COUNT_ALREADY_OPEN`, `pending` de
 * `COUNT_HAS_PENDING_LINES`, a frase de `details.file` no envio de foto).
 */

export const CAMINHO_DO_INVENTARIO = "/app/inventario";

export function caminhoDaConferencia(id: string): string {
  return `${CAMINHO_DO_INVENTARIO}/conferencias/${id}`;
}

/**
 * `409 COUNT_ALREADY_OPEN` não é beco: `details.count_id` é a conferência que
 * já está aberta, e o segundo toque em "Abrir conferência" tem de levar a ela.
 * Devolve `null` para qualquer outra falha — ou para um 409 que venha sem o id,
 * caso em que a tela mostra a frase e não inventa destino.
 */
export function conferenciaJaAberta(
  falha: Pick<Falha, "code" | "details">,
): { id: string; caminho: string; abertaEm: string | null } | null {
  if (falha.code !== "COUNT_ALREADY_OPEN") return null;
  const id = falha.details.count_id;
  if (typeof id !== "string" || id === "") return null;
  const abertaEm = typeof falha.details.opened_at === "string" ? falha.details.opened_at : null;
  return { id, caminho: caminhoDaConferencia(id), abertaEm };
}

/** Onde a recusa aconteceu — o mesmo código quer frases diferentes. */
export type Contexto = "bem" | "ambiente" | "colocacao" | "conferencia" | "avaria" | "copia" | "foto";

function numero(valor: unknown): number {
  return typeof valor === "number" && Number.isFinite(valor) ? valor : 0;
}

function plural(n: number, um: string, varios: string): string {
  return `${n} ${n === 1 ? um : varios}`;
}

export function mensagemDeBens(falha: Pick<Falha, "code" | "details">, contexto?: Contexto): string {
  const d = falha.details ?? {};
  switch (falha.code as CodigoDeErro) {
    case "RESOURCE_IN_USE": {
      const partes = [
        numero(d.placements) > 0 ? plural(numero(d.placements), "ambiente onde está colocado", "ambientes onde está colocado") : null,
        numero(d.count_lines) > 0 ? plural(numero(d.count_lines), "linha de conferência", "linhas de conferência") : null,
        numero(d.issues) > 0 ? plural(numero(d.issues), "avaria registrada", "avarias registradas") : null,
      ].filter(Boolean);
      const segura = partes.length > 0 ? ` (${partes.join(", ")})` : "";
      const temHistorico = numero(d.count_lines) > 0 || numero(d.issues) > 0;
      if (contexto === "ambiente") {
        return `Este ambiente já passou por conferência ou teve avaria${segura}, e esse histórico não pode sumir. Desative o ambiente em vez de apagar: ele sai da contagem e o histórico continua legível.`;
      }
      if (contexto === "bem" && !temHistorico && numero(d.placements) > 0) {
        return `Este bem ainda está colocado em algum ambiente${segura}. Tire-o dos ambientes antes de apagar — ou desative, se ele só saiu de uso.`;
      }
      return `Este bem já tem histórico${segura} e não pode ser apagado. Desative em vez de apagar: ele sai do catálogo em uso e o histórico continua legível.`;
    }
    case "CODE_IN_USE":
      if (contexto === "colocacao") {
        const ja = typeof d.expected_qty === "number" ? ` (com ${d.expected_qty})` : "";
        return `Este bem já está neste ambiente${ja}. Para mudar a quantidade, edite a linha que já existe.`;
      }
      return "Já existe um ambiente com este nome nesta unidade. Use outro nome — “Suíte 2”, “Quarto do fundo”.";
    case "COUNT_HAS_PENDING_LINES": {
      const pendentes = numero(d.pending);
      return pendentes > 0
        ? `Ainda há ${plural(pendentes, "item sem contagem", "itens sem contagem")}. Conte todos antes de fechar — ou cancele a conferência, se ela não vai ser terminada.`
        : mensagemDoErro("COUNT_HAS_PENDING_LINES");
    }
    case "VALIDATION_ERROR": {
      // O envio de foto e a cópia escrevem a frase já na língua do gestor.
      for (const chave of ["file", "source_unit_id", "unit_id", "media_ids"]) {
        const frase = d[chave];
        if (typeof frase === "string" && frase) return frase;
      }
      if (contexto === "conferencia") {
        return "Não deu para abrir a conferência. Confira se a unidade tem bens colocados nos ambientes — conferência de nada não existe.";
      }
      return mensagemDoErro("VALIDATION_ERROR");
    }
    default:
      return mensagemDoErro(falha.code as CodigoDeErro);
  }
}
