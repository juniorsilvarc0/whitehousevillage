import { normalizarCodigo, type CodigoDeErro } from "@/lib/api/codigos";
import { mensagemDoErro } from "@/lib/acoes/resultado";

/**
 * Os dois códigos que o módulo de contatos acrescentou ao contrato — e o motivo
 * de este arquivo existir.
 *
 * ## O diagnóstico (o mesmo do CRM, por outra porta)
 *
 * `lib/api/codigos.ts` guarda o vocabulário fechado do painel e ainda **não**
 * conhece `CONTACT_DUPLICATE` nem `CONTACT_ANONYMIZED` (o contrato já os
 * declara; o espelho do painel e o `internal/platform/apperr` ainda não).
 * `normalizarCodigo()` descarta o que não está na lista e cai no status:
 *
 * ```
 * normalizarCodigo("CONTACT_DUPLICATE",  409) === "INTERNAL"   ← aqui
 * normalizarCodigo("CONTACT_ANONYMIZED", 409) === "INTERNAL"
 * ```
 *
 * O estrago é justamente no gesto mais comum do cadastro. Quem digita um
 * telefone que já existe receberia **"erro no servidor"** — e a resposta certa
 * era abrir a ficha que já está lá, com o nome da pessoa. O código é o que
 * carrega `details.contact_id`; perdê-lo transforma o desfecho útil ("é a Ana,
 * quer abrir?") num chamado de suporte.
 *
 * ## O que se faz enquanto isso
 *
 * `lib/contatos/api.ts` recupera o código **cru** do corpo da resposta e o
 * entrega aqui — mesma técnica do `lib/crm/api.ts`, pelo mesmo motivo, e os dois
 * encolhem juntos no dia em que o espelho geral crescer.
 */
export const CODIGOS_DE_CONTATOS = ["CONTACT_DUPLICATE", "CONTACT_ANONYMIZED"] as const;

export type CodigoDeContatos = (typeof CODIGOS_DE_CONTATOS)[number];

/** O vocabulário que a tela de contatos enxerga: o geral mais os dois. */
export type CodigoContato = CodigoDeErro | CodigoDeContatos;

const DE_CONTATOS: readonly string[] = CODIGOS_DE_CONTATOS;

export function ehCodigoDeContatos(code: string): code is CodigoDeContatos {
  return DE_CONTATOS.includes(code);
}

/**
 * Normaliza preservando o vocabulário de contatos. Delega ao `normalizarCodigo`
 * do painel para tudo que ele conhece — não há um segundo mapa de status aqui,
 * que divergiria do primeiro no dia em que só um fosse ajustado.
 */
export function normalizarCodigoDeContato(code: string | undefined, status: number): CodigoContato {
  if (code && ehCodigoDeContatos(code)) return code;
  return normalizarCodigo(code, status);
}

/**
 * Texto por **código**, nunca o `message` da API.
 *
 * `CONTACT_DUPLICATE` tem a frase mais curta do arquivo de propósito: a tela não
 * conta a história por aqui, ela mostra o contato que já existe com um botão
 * para abri-lo. Uma frase longa no lugar do caminho de saída seria o "erro seco"
 * que esta rodada veio consertar.
 */
export function mensagemDeContato(code: CodigoContato, padrao?: string): string {
  switch (code) {
    case "CONTACT_DUPLICATE":
      return "Esta pessoa já está cadastrada.";
    case "CONTACT_ANONYMIZED":
      return "Os dados desta ficha foram apagados a pedido da pessoa e não podem voltar. Cadastre um contato novo.";
    case "RESOURCE_IN_USE":
      // O texto geral fala em "desativar", que não é o gesto daqui: contato com
      // vínculo não se apaga, se anonimiza — e a ficha explica a diferença.
      //
      // É o ÚNICO código do dicionário geral que este arquivo reescreve.
      // `NOT_FOUND` chegou a ter uma versão "deste contato" e foi removida: na
      // tela de lista ela dizia a coisa errada ("este contato não existe mais"
      // sobre uma coleção), e o texto geral já servia. Sobrescrever sem ganho é
      // criar uma segunda verdade para manter.
      return "Este contato já tem histórico e não pode ser apagado. Use a anonimização.";
    default:
      return mensagemDoErro(code, padrao);
  }
}

/** Título curto do aviso — o que vai na primeira linha do toast. */
export function tituloDeContato(code: CodigoContato): string {
  switch (code) {
    case "CONTACT_DUPLICATE":
      return "Contato já existe";
    case "CONTACT_ANONYMIZED":
      return "Ficha anonimizada";
    case "RESOURCE_IN_USE":
      return "Contato com histórico";
    case "FORBIDDEN":
      return "Sem permissão";
    case "VALIDATION_ERROR":
      return "Confira os dados";
    default:
      return "Não foi possível concluir";
  }
}
