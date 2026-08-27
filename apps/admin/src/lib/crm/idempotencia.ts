/**
 * A chave de idempotência de `POST /crm/opportunities/{id}/win`.
 *
 * Nasce **no navegador**, no instante em que o diálogo abre, e sobrevive a
 * todas as tentativas daquele diálogo. É essa permanência que faz a chave
 * valer alguma coisa: gerada por tentativa, ela seria um identificador novo a
 * cada clique — o mesmo que não existir —, e o duplo clique no botão "Ganhar"
 * criaria duas reservas das mesmas datas, com a segunda morrendo em
 * `409 DATE_CONFLICT` contra a primeira.
 *
 * O contrato pede 8..255 caracteres.
 */
export function novaChaveDeIdempotencia(): string {
  const cripto = globalThis.crypto;
  if (cripto && typeof cripto.randomUUID === "function") return cripto.randomUUID();
  if (cripto && typeof cripto.getRandomValues === "function") {
    const bytes = cripto.getRandomValues(new Uint8Array(16));
    return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  }
  // Último recurso (jsdom antigo, contexto não seguro). Colisão aqui devolve
  // `409 IDEMPOTENCY_MISMATCH`, que é recusa visível — nunca reserva duplicada.
  return `whv-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
}
