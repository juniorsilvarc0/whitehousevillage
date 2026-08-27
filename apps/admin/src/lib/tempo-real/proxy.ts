import { apiBaseUrl } from "@/lib/api/client";
import { lerAccessToken } from "@/lib/auth/session";

import type { Topico } from "./eventos";

/**
 * O repasse do stream: navegador → Route Handler → API Go.
 *
 * ## Por que existe
 *
 * O painel inteiro é um BFF: o JWT vive em cookie `httpOnly` e o navegador
 * nunca fala com a API Go. Para toda rota REST isso é o `apiFetch` do servidor.
 * Para o stream não dá: quem abre a conexão é o `EventSource`, no navegador, e
 * ele **não** manda cabeçalho customizado — não há como pôr o `Bearer` ali. O
 * contrato também recusa token em query string, e com razão: ele pararia
 * escrito no log de acesso de todo proxy do caminho.
 *
 * Sobra este salto. O cookie chega aqui por ser mesma origem, o `Bearer` é
 * injetado deste lado, e o corpo é repassado **sem buffer** — o `ReadableStream`
 * da resposta vai direto para a resposta de saída. Ler e reemitir aqui
 * transformaria tempo real em entrega em lote, que é o defeito que o
 * `X-Accel-Buffering: no` do contrato existe para impedir um salto adiante.
 *
 * ## O que este arquivo NÃO faz
 *
 * Não decide quem pode ouvir o quê. A permissão por tópico é conferida no
 * handshake da API Go (`x-rbac-topicos` no contrato: `calendar` exige
 * `calendar:ver`, `crm` exige `crm.opportunities:ver`), e um `403` de lá
 * atravessa este repasse inteiro. Repetir a regra aqui seria um segundo lugar
 * para errar a matriz — e o que ninguém testa.
 */
const TOPICOS: readonly string[] = ["calendar", "crm"];

export type PedidoDeStream = {
  topicos: Topico[];
  lastEventId: string | null;
};

/**
 * Lê e valida `topics`. Nome desconhecido é recusado em vez de descartado: uma
 * conexão silenciosa por causa de um `?topics=calender` é indistinguível de
 * "não aconteceu nada ainda", e é assim que se perde meia hora.
 */
export function lerPedido(url: URL): PedidoDeStream | { erro: string } {
  const bruto = (url.searchParams.get("topics") ?? "").trim();
  if (bruto === "") return { erro: "topics é obrigatório." };

  const pedidos = bruto.split(",").map((t) => t.trim()).filter(Boolean);
  const desconhecidos = pedidos.filter((t) => !TOPICOS.includes(t));
  if (desconhecidos.length > 0) return { erro: `Tópico desconhecido: ${desconhecidos.join(", ")}.` };

  return {
    topicos: [...new Set(pedidos)] as Topico[],
    lastEventId: url.searchParams.get("last_event_id"),
  };
}

function erro(code: string, message: string, status: number): Response {
  return new Response(JSON.stringify({ error: { code, message, details: {} } }), {
    status,
    headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
  });
}

export async function repassarStream(request: Request): Promise<Response> {
  const pedido = lerPedido(new URL(request.url));
  if ("erro" in pedido) return erro("VALIDATION_ERROR", pedido.erro, 422);

  const token = await lerAccessToken();
  // Sem cookie não adianta abrir a conexão para colher um 401 lá na frente: o
  // `EventSource` fecha de vez em resposta não-2xx, e o hook lê isso como
  // "sessão morta" — que é exatamente o que é.
  if (!token) return erro("UNAUTHORIZED", "Sessão ausente ou expirada.", 401);

  const alvo = new URL(`${apiBaseUrl()}/stream`);
  alvo.searchParams.set("topics", pedido.topicos.join(","));

  const cabecalhos: Record<string, string> = {
    Authorization: `Bearer ${token}`,
    Accept: "text/event-stream",
  };
  // O cursor volta como CABEÇALHO, que é a porta que o contrato declara para
  // navegador. O query param existe para cliente que não é navegador — e é o
  // que o hook usa para chegar até aqui, porque `EventSource` não manda header.
  const cursor = pedido.lastEventId ?? request.headers.get("last-event-id");
  if (cursor) cabecalhos["Last-Event-ID"] = cursor;

  let upstream: Response;
  try {
    upstream = await fetch(alvo, {
      headers: cabecalhos,
      // O abort do cliente tem de chegar até a API: sem isto, fechar a aba
      // deixaria uma conexão longa de pé do outro lado, e um dia de painel
      // aberto e fechado viraria um vazamento de conexões no servidor Go.
      signal: request.signal,
      cache: "no-store",
    });
  } catch {
    return erro("NETWORK_ERROR", "Não foi possível abrir o barramento de eventos.", 502);
  }

  if (!upstream.ok || !upstream.body) {
    const texto = await upstream.text().catch(() => "");
    return new Response(texto || JSON.stringify({ error: { code: "INTERNAL", message: "Stream indisponível." } }), {
      status: upstream.status || 502,
      headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
    });
  }

  return new Response(upstream.body, {
    status: 200,
    headers: {
      "Content-Type": "text/event-stream; charset=utf-8",
      "Cache-Control": "no-store, no-transform",
      // `Connection: keep-alive` NÃO vai aqui: é cabeçalho específico de
      // conexão, o HTTP/1.1 já o assume, e sob HTTP/2 ele é proibido — mandá-lo
      // derrubaria a conexão com erro de protocolo justo onde o painel roda
      // atrás do Traefik.
      //
      // Este outro é repetido de propósito: o cabeçalho da API Go morre neste salto, e
      // é o proxy da frente (Traefik/nginx) que precisa lê-lo. Sem ele o buffer
      // segura os eventos e entrega tudo junto, no fim — tempo real que chega
      // em lote não é tempo real.
      "X-Accel-Buffering": "no",
    },
  });
}
