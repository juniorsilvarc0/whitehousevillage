import { NextResponse } from "next/server";

import { apiFetch } from "@/lib/api/client";
import { lerAccessToken, lerRefreshToken, limparSessao } from "@/lib/auth/session";

/**
 * Encerra a sessão. O contrato garante que `/auth/logout` é idempotente (204
 * mesmo com a família já revogada), então **os cookies caem de qualquer jeito**:
 * a API fora do ar não pode ser motivo para o usuário continuar logado no
 * navegador de um computador emprestado.
 */
export async function POST(): Promise<NextResponse> {
  const access = await lerAccessToken();
  const refresh = await lerRefreshToken();

  if (access) {
    try {
      await apiFetch<void>("/auth/logout", {
        method: "POST",
        accessToken: access,
        body: refresh ? { refresh_token: refresh } : {},
      });
    } catch {
      // Falhar aqui só significa que a família continua viva do lado da API até
      // expirar. O access token dura 15 min; é o preço de não manter lista negra
      // de JWT, e já está assumido no contrato.
    }
  }

  const resposta = new NextResponse(null, { status: 204 });
  limparSessao(resposta.cookies);
  return resposta;
}
