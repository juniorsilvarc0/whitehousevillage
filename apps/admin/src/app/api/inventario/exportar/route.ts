import { NextResponse } from "next/server";

import { apiBaseUrl } from "@/lib/api/client";
import { exigir } from "@/lib/acoes/guarda";
import { lerAccessToken } from "@/lib/auth/session";

/**
 * `GET /api/inventario/exportar` — a planilha do inventário (`GET /inventory/export`).
 *
 * Existe pelo mesmo motivo das outras rotas daqui: o navegador não tem o token.
 * O CSV é repassado **em fluxo e intacto** — separador `;`, UTF-8 com BOM e
 * decimal com vírgula são escolhas do servidor para abrir no Excel em
 * português, e mexer no corpo aqui (decodificar e recodificar) é o jeito mais
 * curto de perder o BOM e devolver o "Pôr" escrito errado.
 *
 * Só os quatro filtros que o contrato conhece atravessam; qualquer outro
 * parâmetro do endereço é descartado em vez de virar `422` no meio do download.
 */
export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const FILTROS = ["unit_id", "room_id", "category", "include_inactive"] as const;

export async function GET(request: Request): Promise<Response> {
  const recusa = await exigir("inventory.goods", "ver");
  if (recusa) {
    return NextResponse.json(
      { error: { code: recusa.code, message: recusa.message, details: recusa.details } },
      { status: recusa.code === "UNAUTHORIZED" ? 401 : 403 },
    );
  }
  const token = await lerAccessToken();
  if (!token) return new Response(null, { status: 401 });

  const origem = new URL(request.url).searchParams;
  const destino = new URLSearchParams();
  for (const chave of FILTROS) {
    const v = origem.get(chave);
    if (v) destino.set(chave, v);
  }
  const consulta = destino.toString();

  let resposta: Response;
  try {
    resposta = await fetch(`${apiBaseUrl()}/inventory/export${consulta ? `?${consulta}` : ""}`, {
      headers: { Authorization: `Bearer ${token}`, Accept: "text/csv" },
      cache: "no-store",
      signal: request.signal,
    });
  } catch {
    return NextResponse.json(
      { error: { code: "NETWORK_ERROR", message: "Sem conexão com o sistema agora.", details: {} } },
      { status: 502 },
    );
  }

  const saida = new Headers();
  for (const nome of ["content-type", "content-disposition", "content-length"]) {
    const v = resposta.headers.get(nome);
    if (v) saida.set(nome, v);
  }
  saida.set("cache-control", "private, no-store");
  return new Response(resposta.body, { status: resposta.status, headers: saida });
}
