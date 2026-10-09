/**
 * O vocabulário fechado de códigos de erro do contrato.
 *
 * Vive num módulo **sem dependência nenhuma** por um motivo prático: componente
 * de cliente precisa do código para escolher a mensagem, e o cliente da API
 * importa `next/headers` — que não existe no navegador. Deixar os dois juntos
 * arrastava metade do BFF para dentro do bundle e quebrava o build.
 *
 * Espelho de `components/responses/Erro` na OpenAPI e de
 * `internal/platform/apperr`: acrescentar código lá obriga a acrescentar aqui.
 */
export type CodigoDeErro =
  | "VALIDATION_ERROR"
  | "UNAUTHORIZED"
  | "FORBIDDEN"
  | "NOT_FOUND"
  | "DATE_CONFLICT"
  | "MIN_STAY_NOT_MET"
  | "CAPACITY_EXCEEDED"
  | "DISCOUNT_ABOVE_LIMIT"
  | "HOLD_EXPIRED"
  | "IDEMPOTENCY_MISMATCH"
  | "RATE_LIMITED"
  | "INVALID_CREDENTIALS"
  | "TOKEN_INVALID"
  | "TOKEN_REUSED"
  | "EMAIL_IN_USE"
  | "ROLE_IMMUTABLE"
  | "ROLE_IN_USE"
  // Fase 1 — o núcleo de reservas. Mesma ordem do enum da OpenAPI.
  | "RATE_NOT_FOUND"
  | "POLICY_IMMUTABLE"
  | "CODE_IN_USE"
  | "RESOURCE_IN_USE"
  | "INVALID_STATE_TRANSITION"
  | "RESERVATION_NOT_CANCELLABLE"
  | "UNIT_NOT_AVAILABLE"
  // Produto que a casa não consegue entregar inteiro (`all_members` com unidade
  // membro inativa, ou composição sem nenhuma unidade ativa). É `422` e não
  // `409` de propósito: a data não está em disputa e nenhuma outra data
  // resolve — quem age é a gestão do inventário, não o hóspede.
  | "COMPOSITION_INCOMPLETE"
  | "HOLD_LIMIT_REACHED"
  // Inventário de bens (`inventory.goods`). Os três são 409 e é por isso que
  // entram aqui, e não num dicionário à parte como os do CRM e de contatos:
  // `normalizarCodigo` não tem ramo para 409 e os jogaria em `INTERNAL`, e
  // `COUNT_ALREADY_OPEN` carrega em `details.count_id` o caminho até a
  // conferência que já está aberta — perder o código é perder a saída.
  | "COUNT_ALREADY_OPEN"
  | "COUNT_CLOSED"
  | "COUNT_HAS_PENDING_LINES"
  // Ordens de manutenção (spec §12). Os dois são 409 — sem eles aqui,
  // `normalizarCodigo` os jogaria em `INTERNAL`, e o segundo toque em "Abrir
  // ordem de manutenção" perderia `details.maintenance_order_id`, que é o
  // caminho até a ordem que já existe.
  | "MAINTENANCE_ORDER_CLOSED"
  | "MAINTENANCE_ORDER_ALREADY_OPEN"
  | "INTERNAL"
  // Fora do contrato: a API não respondeu (caiu, DNS, timeout). É do BFF, não
  // do servidor, e por isso não pode se disfarçar de INTERNAL — a tela precisa
  // saber que o problema é de conexão para oferecer "tentar de novo".
  | "NETWORK_ERROR";

const CONHECIDOS: readonly string[] = [
  "VALIDATION_ERROR", "UNAUTHORIZED", "FORBIDDEN", "NOT_FOUND", "DATE_CONFLICT",
  "MIN_STAY_NOT_MET", "CAPACITY_EXCEEDED", "DISCOUNT_ABOVE_LIMIT", "HOLD_EXPIRED",
  "IDEMPOTENCY_MISMATCH", "RATE_LIMITED", "INVALID_CREDENTIALS", "TOKEN_INVALID",
  "TOKEN_REUSED", "EMAIL_IN_USE", "ROLE_IMMUTABLE", "ROLE_IN_USE",
  "RATE_NOT_FOUND", "POLICY_IMMUTABLE", "CODE_IN_USE", "RESOURCE_IN_USE",
  "INVALID_STATE_TRANSITION", "RESERVATION_NOT_CANCELLABLE", "UNIT_NOT_AVAILABLE",
  "COMPOSITION_INCOMPLETE", "HOLD_LIMIT_REACHED",
  "COUNT_ALREADY_OPEN", "COUNT_CLOSED", "COUNT_HAS_PENDING_LINES",
  "MAINTENANCE_ORDER_CLOSED", "MAINTENANCE_ORDER_ALREADY_OPEN",
  "INTERNAL",
];

/** Código desconhecido não pode virar `undefined` no meio da tela: mapeia pelo
 *  status, que é o que sobra de estável quando o corpo foge do contrato. */
export function normalizarCodigo(code: string | undefined, status: number): CodigoDeErro {
  if (code && CONHECIDOS.includes(code)) return code as CodigoDeErro;
  if (status === 401) return "UNAUTHORIZED";
  if (status === 403) return "FORBIDDEN";
  if (status === 404) return "NOT_FOUND";
  if (status === 422) return "VALIDATION_ERROR";
  if (status === 429) return "RATE_LIMITED";
  return "INTERNAL";
}
