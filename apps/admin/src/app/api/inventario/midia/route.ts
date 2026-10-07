import { NextResponse } from "next/server";

import { apiBaseUrl } from "@/lib/api/client";
import { exigir } from "@/lib/acoes/guarda";
import { lerAccessToken } from "@/lib/auth/session";

/**
 * `POST /api/inventario/midia` — envio da foto de um bem do inventário.
 *
 * Mesmo desenho de `/api/site/midia`: Server Action recusa corpo acima de ~1 MB
 * e a foto do celular passa disso, então o `multipart/form-data` do navegador é
 * **repassado em fluxo** para `POST /inventory/media`, com o mesmo
 * `Content-Type` (é nele que está o `boundary`). O arquivo não é montado na
 * memória do painel; tipo e tamanho quem confere é a API, pelos bytes.
 *
 * Fora do matcher do `proxy.ts` (`/app/:path*`) de propósito: quando o proxy
 * roda, o Next copia o corpo para a memória e corta em 10 MB — e a foto pode ter
 * 15.
 *
 * A guarda aqui é cortesia (resposta rápida, sem gastar o envio pelo sinal do
 * celular); quem barra é a API, que responde 403 sem `inventory.goods:criar`.
 */
export const dynamic = "force-dynamic";
export const runtime = "nodejs";

function erro(status: number, code: string, message: string, details: Record<string, unknown> = {}) {
  return NextResponse.json({ error: { code, message, details } }, { status });
}

export async function POST(request: Request): Promise<Response> {
  const recusa = await exigir("inventory.goods", "criar");
  if (recusa) return erro(recusa.code === "UNAUTHORIZED" ? 401 : 403, recusa.code, recusa.message, recusa.details);

  const token = await lerAccessToken();
  if (!token) return erro(401, "UNAUTHORIZED", "Sessão ausente ou expirada.");

  const tipo = request.headers.get("content-type") ?? "";
  if (!tipo.toLowerCase().startsWith("multipart/form-data") || !request.body) {
    return erro(422, "VALIDATION_ERROR", "Envio sem arquivo.", { file: "Escolha uma foto para enviar." });
  }

  let resposta: Response;
  try {
    resposta = await fetch(`${apiBaseUrl()}/inventory/media`, {
      method: "POST",
      headers: { Accept: "application/json", Authorization: `Bearer ${token}`, "Content-Type": tipo },
      body: request.body,
      // Corpo em fluxo exige `duplex: "half"` no fetch do Node (undici).
      duplex: "half",
      cache: "no-store",
      signal: request.signal,
    } as RequestInit & { duplex: "half" });
  } catch {
    return erro(502, "NETWORK_ERROR", "Sem conexão com o sistema agora.");
  }

  if (resposta.status === 413) {
    return erro(413, "VALIDATION_ERROR", "Arquivo grande demais.", {
      reason: "too_large",
      file: "A foto passa de 15 MB. Reduza a resolução e tente de novo.",
    });
  }

  const texto = await resposta.text();
  try {
    return NextResponse.json(texto ? JSON.parse(texto) : {}, { status: resposta.status });
  } catch {
    return erro(resposta.ok ? 502 : resposta.status, "INTERNAL", "Resposta ilegível da API.");
  }
}
