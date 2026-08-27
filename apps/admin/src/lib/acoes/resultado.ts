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
      return "Seu perfil não alcança esta operação.";
    case "UNAUTHORIZED":
      return "Sua sessão expirou. Entre de novo.";
    case "NOT_FOUND":
      return "Este registro não existe mais — alguém pode tê-lo removido.";
    case "NETWORK_ERROR":
      return "Não foi possível falar com o servidor. Tente novamente em instantes.";
    case "RATE_LIMITED":
      return "Requisições demais. Espere alguns instantes.";
    case "CODE_IN_USE":
      return "Já existe um registro com esse código nesta propriedade.";
    case "RESOURCE_IN_USE":
      return "Ainda há registros usando isto. Encerre-os antes de desativar.";
    case "POLICY_IMMUTABLE":
      return "Versão de política já publicada não se reescreve nem se antedata. Publique uma versão nova.";
    case "RATE_NOT_FOUND":
      return "A tabela vigente não tem tarifa para algum tipo de data desta estadia.";
    case "MIN_STAY_NOT_MET":
      return "A estadia é menor que o mínimo de noites do período.";
    case "CAPACITY_EXCEEDED":
      return "Hóspedes acima da capacidade do produto.";
    case "DISCOUNT_ABOVE_LIMIT":
      return "Desconto acima da alçada. Acima de 10% nem com aprovação.";
    case "DATE_CONFLICT":
      return "A data já está ocupada. O calendário mudou enquanto esta tela estava aberta.";
    case "UNIT_NOT_AVAILABLE":
      return "A unidade escolhida está ocupada nesse período.";
    case "COMPOSITION_INCOMPLETE":
      return "Este produto não pode ser entregue inteiro: falta unidade ativa na composição. Reative a unidade no inventário.";
    case "INVALID_STATE_TRANSITION":
      return "A reserva não está em um estado que aceite esta ação.";
    case "RESERVATION_NOT_CANCELLABLE":
      return "Esta reserva já foi encerrada e não pode ser cancelada.";
    case "HOLD_EXPIRED":
      return "A pré-reserva expirou e a data foi liberada.";
    case "HOLD_LIMIT_REACHED":
      return "Limite de extensões da pré-reserva atingido.";
    case "IDEMPOTENCY_MISMATCH":
      return "Esta operação já foi enviada com outro conteúdo. Recarregue a tela.";
    case "INTERNAL":
      return "Erro no servidor. Tente novamente em instantes.";
    default:
      return padrao;
  }
}
