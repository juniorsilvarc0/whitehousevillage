import { normalizarCodigo, type CodigoDeErro } from "@/lib/api/codigos";
import { mensagemDoErro } from "@/lib/acoes/resultado";

/**
 * Os sete códigos que o CRM acrescentou ao contrato — e o motivo de este
 * arquivo existir.
 *
 * ## O diagnóstico
 *
 * `lib/api/codigos.ts` guarda o vocabulário fechado do painel e ainda **não**
 * conhece os códigos do CRM (o contrato já os declara; o espelho do painel e o
 * `internal/platform/apperr` ainda não). `normalizarCodigo()` descarta o que
 * não está na lista e cai no status:
 *
 * ```
 * normalizarCodigo("STAGE_NOT_IN_PIPELINE",     422) === "VALIDATION_ERROR"
 * normalizarCodigo("LEAD_ALREADY_CONVERTED",    409) === "INTERNAL"   ← aqui
 * normalizarCodigo("OPPORTUNITY_ALREADY_CLOSED",409) === "INTERNAL"
 * ```
 *
 * Os 422 sobrevivem como "confira os dados"; os **409 viram `INTERNAL`**, e a
 * tela passa a dizer "erro no servidor" para "este lead já foi convertido" e
 * para "esta oportunidade já está fechada" — duas recusas que o operador
 * resolve sozinho em dois segundos, se alguém disser a ele o que houve.
 * `codigos.test.ts` fixa essa demonstração em teste, para o dia em que o
 * espelho for atualizado e este arquivo puder encolher.
 *
 * ## O que se faz enquanto isso
 *
 * `lib/crm/api.ts` recupera o código **cru** do corpo da resposta e o entrega
 * aqui. Este módulo é o dicionário do CRM: texto por código, nunca por
 * mensagem da API — a mesma regra da casa, com um vocabulário a mais.
 */
export const CODIGOS_DO_CRM = [
  "STAGE_NOT_IN_PIPELINE",
  "STAGE_ORDER_INCOMPLETE",
  "DEFAULT_PIPELINE_REQUIRED",
  "OPPORTUNITY_ALREADY_CLOSED",
  "LOSS_REASON_REQUIRED",
  "QUOTE_REQUIRED_TO_WIN",
  "LEAD_ALREADY_CONVERTED",
] as const;

export type CodigoDoCrm = (typeof CODIGOS_DO_CRM)[number];

/** O vocabulário que a tela do CRM enxerga: o geral mais os sete. */
export type CodigoCrm = CodigoDeErro | CodigoDoCrm;

const DO_CRM: readonly string[] = CODIGOS_DO_CRM;

export function ehCodigoDoCrm(code: string): code is CodigoDoCrm {
  return DO_CRM.includes(code);
}

/**
 * Normaliza preservando o vocabulário do CRM.
 *
 * Delega ao `normalizarCodigo` do painel para tudo que ele conhece — não há um
 * segundo mapa de status aqui, que divergiria do primeiro no dia em que só um
 * fosse ajustado.
 */
export function normalizarCodigoCrm(code: string | undefined, status: number): CodigoCrm {
  if (code && ehCodigoDoCrm(code)) return code;
  return normalizarCodigo(code, status);
}

/**
 * Texto por **código**, nunca o `message` da API.
 *
 * A API escreve para quem depura; a tela escreve para quem vende. Código que
 * não é do CRM cai no dicionário geral — assim um código novo do contrato
 * continua tendo uma frase, em vez de deixar a tela muda.
 */
export function mensagemCrm(code: CodigoCrm, padrao?: string): string {
  switch (code) {
    case "STAGE_NOT_IN_PIPELINE":
      return "Esta etapa é de outro funil. Recarregue a tela: o funil mudou enquanto ela estava aberta.";
    case "STAGE_ORDER_INCOMPLETE":
      return "A reordenação precisa listar todas as etapas do funil. Recarregue e tente de novo.";
    case "DEFAULT_PIPELINE_REQUIRED":
      return "Tem de existir um funil padrão. Marque outro como padrão antes de desmarcar este.";
    case "OPPORTUNITY_ALREADY_CLOSED":
      return "Esta oportunidade já foi ganha ou perdida, e negócio fechado não volta a ser editado. Negócio que renasce é oportunidade nova, com o mesmo contato.";
    case "LOSS_REASON_REQUIRED":
      return "Escolha um motivo de perda ativo. É dele que sai o relatório de por que a casa perde negócio.";
    case "QUOTE_REQUIRED_TO_WIN":
      return "Não há orçamento vigente para virar reserva. Emita o orçamento antes de ganhar — o preço não se inventa no fechamento.";
    case "LEAD_ALREADY_CONVERTED":
      return "Este lead já virou oportunidade. Abra o card existente em vez de criar um segundo.";
    default:
      return mensagemDoErro(code, padrao);
  }
}

/**
 * O título curto que acompanha a mensagem no toast.
 *
 * Existe porque toast sem título vira um parágrafo solto: quem está arrastando
 * card precisa ler em meio segundo **o que** falhou, e só depois o porquê.
 */
export function tituloCrm(code: CodigoCrm): string {
  switch (code) {
    case "DATE_CONFLICT":
      return "A data já está ocupada";
    case "OPPORTUNITY_ALREADY_CLOSED":
      return "Oportunidade já fechada";
    case "INVALID_STATE_TRANSITION":
      return "O card já tinha sido movido";
    case "FORBIDDEN":
      return "Sem permissão";
    case "NOT_FOUND":
      return "Registro não encontrado";
    case "NETWORK_ERROR":
      return "Sem conexão com o servidor";
    default:
      return "Não foi possível concluir";
  }
}

/**
 * O código do CRM traduzido para o vocabulário **geral** do painel.
 *
 * Existe para as poucas peças da casca que só aceitam `CodigoDeErro` — o
 * `ModalDeConfirmacao`, por exemplo. A tradução perde a frase específica (é o
 * preço), mas nunca perde a **classe** da recusa: 422 continua sendo "confira
 * os dados" e 409 continua sendo "o registro não está em estado de aceitar
 * isso". Quando os sete códigos entrarem em `lib/api/codigos.ts`, esta função
 * some junto com a necessidade dela.
 */
export function codigoGeral(code: CodigoCrm): CodigoDeErro {
  switch (code) {
    case "STAGE_NOT_IN_PIPELINE":
    case "STAGE_ORDER_INCOMPLETE":
    case "LOSS_REASON_REQUIRED":
    case "QUOTE_REQUIRED_TO_WIN":
      return "VALIDATION_ERROR";
    case "DEFAULT_PIPELINE_REQUIRED":
    case "OPPORTUNITY_ALREADY_CLOSED":
    case "LEAD_ALREADY_CONVERTED":
      return "INVALID_STATE_TRANSITION";
    default:
      return code;
  }
}
