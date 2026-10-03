import { NextResponse } from "next/server";

import { apiBaseUrl } from "@/lib/api/client";
import { exigir } from "@/lib/acoes/guarda";
import { lerAccessToken } from "@/lib/auth/session";

/**
 * `POST /api/site/midia` — envio de foto ou vídeo do menu "Site".
 *
 * Existe porque Server Action recusa corpo acima de ~1 MB e um vídeo de capa
 * passa de 100 MB (docs/site-cms.md §6). O corpo `multipart/form-data` do
 * navegador é **repassado em fluxo** para `POST /site/media` da API, com o
 * mesmo `Content-Type` (é nele que está o `boundary`): o arquivo não é montado
 * na memória do painel. Tipo e tamanho quem confere é a API, pelos bytes.
 *
 * Fora do matcher do `proxy.ts` (`/app/:path*`) de propósito: quando o proxy
 * roda, o Next copia o corpo para a memória e corta em 10 MB.
 *
 * A guarda de permissão aqui é cortesia (resposta rápida e sem gastar o envio);
 * quem barra de verdade é a API, que responde 403 sem `site:editar`.
 */
export const dynamic = "force-dynamic";
export const runtime = "nodejs";

function erro(status: number, code: string, message: string, details: Record<string, unknown> = {}) {
  return NextResponse.json({ error: { code, message, details } }, { status });
}

export async function POST(request: Request): Promise<Response> {
  const recusa = await exigir("site", "editar");
  if (recusa) return erro(recusa.code === "UNAUTHORIZED" ? 401 : 403, recusa.code, recusa.message, recusa.details);

  const token = await lerAccessToken();
  if (!token) return erro(401, "UNAUTHORIZED", "Sessão ausente ou expirada.");

  const tipo = request.headers.get("content-type") ?? "";
  if (!tipo.toLowerCase().startsWith("multipart/form-data") || !request.body) {
    return erro(422, "VALIDATION_ERROR", "Envio sem arquivo.", { file: "Escolha um arquivo para enviar." });
  }

  const cabecalhos: Record<string, string> = {
    Accept: "application/json",
    Authorization: `Bearer ${token}`,
    "Content-Type": tipo,
  };

  let resposta: Response;
  try {
    resposta = await fetch(`${apiBaseUrl()}/site/media`, {
      method: "POST",
      headers: cabecalhos,
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
    return erro(413, "VALIDATION_ERROR", "Arquivo grande demais.", { reason: "too_large", file: "Arquivo grande demais." });
  }

  const texto = await resposta.text();
  try {
    return NextResponse.json(texto ? JSON.parse(texto) : {}, { status: resposta.status });
  } catch {
    return erro(resposta.ok ? 502 : resposta.status, "INTERNAL", "Resposta ilegível da API.");
  }
}
