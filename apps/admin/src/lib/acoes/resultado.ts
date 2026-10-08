import type { CodigoDeErro } from "@/lib/api/codigos";

/**
 * O resultado que atravessa a fronteira servidor → cliente.
 *
 * Server Action não pode **lançar** para o cliente: em produção o Next apaga a
 * mensagem e entrega um digest opaco, e a tela perde justamente o `code` — que é
 * a única coisa a que ela tem o direito de reagir (regra da casa: nunca ao
 * texto). Por isso todo caminho de dados devolve este objeto, e o erro do
 * contrato chega inteiro, com `details` para o erro por campo.
 */
export type Falha = {
  ok: false;
  code: CodigoDeErro;
  message: string;
  details: Record<string, unknown>;
};

export type Resultado<T> = { ok: true; data: T } | Falha;

export function falha(code: CodigoDeErro, message: string, details: Record<string, unknown> = {}): Falha {
  return { ok: false, code, message, details };
}

/**
 * Texto por **código**, nunca o `message` da API.
 *
 * A API escreve para quem depura; a tela escreve para quem vende. Os dois textos
 * divergem de propósito — e um código novo no contrato cai no padrão em vez de
 * deixar a tela muda.
 */
export function mensagemDoErro(code: CodigoDeErro, padrao = "Não foi possível concluir a operação."): string {
  switch (code) {
    case "VALIDATION_ERROR":
      return "Confira os dados informados.";
    case "FORBIDDEN":
      return "Seu perfil não tem permissão para fazer isso. Se precisar, peça à gestão.";
    case "UNAUTHORIZED":
      return "Sua sessão expirou. Entre de novo.";
    case "NOT_FOUND":
      return "Não encontramos este item. Ele pode ter sido removido.";
    case "NETWORK_ERROR":
      return "Sem conexão com o sistema agora. Verifique a internet e tente de novo em instantes.";
    case "RATE_LIMITED":
      return "Muitas tentativas seguidas. Espere alguns instantes e tente de novo.";
    case "CODE_IN_USE":
      return "Já existe um cadastro com esse código.";
    case "RESOURCE_IN_USE":
      return "Ainda há reservas ou cadastros usando isto. Resolva-os antes de desativar.";
    case "POLICY_IMMUTABLE":
      return "Uma política já publicada não pode ser alterada nem valer para trás. Publique uma versão nova.";
    case "RATE_NOT_FOUND":
      return "Falta o preço de alguma noite desta estadia na tabela de preços. Preencha em Configurações → Tarifário.";
    case "MIN_STAY_NOT_MET":
      return "A estadia tem menos noites que o mínimo exigido para esse período.";
    case "CAPACITY_EXCEEDED":
      return "Há mais hóspedes do que o produto comporta.";
    case "DISCOUNT_ABOVE_LIMIT":
      return "Desconto acima do permitido, mesmo com aprovação do proprietário.";
    case "DATE_CONFLICT":
      return "Essas datas acabaram de ser ocupadas por outra reserva. Escolha outras datas.";
    case "UNIT_NOT_AVAILABLE":
      return "O apartamento escolhido está ocupado nesse período.";
    case "COMPOSITION_INCOMPLETE":
      return "Este produto não pode ser vendido inteiro porque um dos apartamentos dele está desativado. Reative o apartamento em Configurações → Unidades e produtos.";
    case "INVALID_STATE_TRANSITION":
      return "A situação atual da reserva não permite esta ação. Recarregue a página para ver como ela está.";
    case "RESERVATION_NOT_CANCELLABLE":
      return "Esta reserva já foi encerrada e não pode ser cancelada.";
    case "HOLD_EXPIRED":
      return "A pré-reserva venceu e as datas foram liberadas.";
    case "HOLD_LIMIT_REACHED":
      return "Esta pré-reserva já foi estendida o máximo de vezes permitido.";
    case "COUNT_ALREADY_OPEN":
      return "Esta unidade já tem uma conferência em andamento. Continue por ela em vez de abrir outra.";
    case "COUNT_CLOSED":
      return "Esta conferência já foi encerrada e não aceita mais alterações. Recarregue a página para ver como ela ficou.";
    case "COUNT_HAS_PENDING_LINES":
      return "Ainda há itens sem contagem. Conte todos antes de fechar — ou cancele a conferência, se ela não vai ser terminada.";
    case "IDEMPOTENCY_MISMATCH":
      return "Esta ação já foi enviada antes com outros dados. Recarregue a página e confira.";
    case "INTERNAL":
      return "Algo deu errado do nosso lado. Tente novamente em instantes.";
    default:
      return padrao;
  }
}
