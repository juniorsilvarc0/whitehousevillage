import { mensagemDoErro, type Falha } from "@/lib/acoes/resultado";

/**
 * O que dizer ao gestor quando salvar ou enviar não deu certo — pelo código,
 * nunca pelo texto da API. A exceção é `details` da validação: o contrato
 * (docs/site-cms.md §5) escreve ali a frase já na língua do gestor
 * ("A foto precisa ser JPG, PNG ou WebP, até 15 MB.").
 */
export function mensagemDoSite(falha: Pick<Falha, "code" | "details">, status?: number): string {
  if (status === 413 || falha.details?.reason === "too_large") {
    return "Arquivo grande demais. Fotos podem ter até 15 MB e vídeos até 300 MB.";
  }
  switch (falha.code) {
    case "VALIDATION_ERROR": {
      const frase = primeiraFrase(falha.details);
      return frase ?? "Não foi possível salvar. Confira o que foi digitado ou escolhido.";
    }
    case "NOT_FOUND":
      return "Este campo não existe mais no site. Recarregue a página.";
    case "NETWORK_ERROR":
      return "Sem conexão agora. Confira a internet e tente de novo.";
    default:
      return mensagemDoErro(falha.code);
  }
}

/** A frase de erro por campo: `file` no envio, `value` ao salvar, ou a primeira
 *  que houver. Só texto; qualquer outra coisa é ignorada. */
function primeiraFrase(details: Record<string, unknown> | undefined): string | null {
  if (!details) return null;
  for (const chave of ["file", "value"]) {
    if (typeof details[chave] === "string" && details[chave]) return details[chave] as string;
  }
  for (const v of Object.values(details)) {
    if (typeof v === "string" && v) return v;
  }
  return null;
}
