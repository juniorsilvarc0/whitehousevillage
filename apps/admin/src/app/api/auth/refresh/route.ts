import { NextResponse } from "next/server";

import { ApiError, apiFetch } from "@/lib/api/client";
import type { ParDeTokens } from "@/lib/api/types";
import { gravarSessao, lerRefreshToken, limparSessao } from "@/lib/auth/session";
import { destinoSeguro, extrairRefresh, respostaDeErro } from "@/lib/auth/upstream";

/**
 * Rotaciona o refresh e reemite o cookie de sessão.
 *
 * Duas portas para o mesmo mecanismo:
 *
 * - **POST** — chamada de dentro da página, devolve JSON.
 * - **GET** — o caminho de navegação: o `middleware.ts` manda o usuário para cá
 *   quando o access token expirou mas o refresh ainda vale, e daqui ele volta
 *   para a página que pediu, já com sessão nova. Sem isso, quinze minutos de
 *   café terminariam em tela de login com o formulário meio preenchido perdido.
 */

type Rotacao = { par: ParDeTokens; refresh: string | undefined };

async function rotacionar(refreshAtual: string): Promise<Rotacao> {
  let refresh: string | undefined;
  const par = await apiFetch<ParDeTokens>("/auth/refresh", {
    method: "POST",
    anonymous: true,
    // O contrato aceita o token no cookie ou no corpo. Como o cookie da API
    // (`Path=/api/v1/auth`) nunca chega ao navegador, o corpo é o caminho do BFF.
    body: { refresh_token: refreshAtual },
    onResponse: (resposta) => {
      refresh = extrairRefresh(resposta);
    },
  });
  return { par, refresh };
}

export async function POST(): Promise<NextResponse> {
  const atual = await lerRefreshToken();
  if (!atual) {
    const vazia = respostaDeErro(new ApiError("TOKEN_INVALID", "Sessão expirada.", 401));
    limparSessao(vazia.cookies);
    return vazia;
  }

  try {
    const { par, refresh } = await rotacionar(atual);
    const resposta = NextResponse.json({ data: { user: par.user } }, { status: 200 });
    gravarSessao(resposta.cookies, par, refresh);
    return resposta;
  } catch (erro) {
    // `TOKEN_REUSED` significa que a API acabou de revogar a família inteira —
    // insistir com o mesmo token só piora. Limpa e manda logar de novo.
    const falha = respostaDeErro(erro);
    limparSessao(falha.cookies);
    return falha;
  }
}

export async function GET(request: Request): Promise<NextResponse> {
  const url = new URL(request.url);
  const destino = destinoSeguro(url.searchParams.get("next"));
  const paraLogin = new URL(`/login?next=${encodeURIComponent(destino)}`, url.origin);

  const atual = await lerRefreshToken();
  if (!atual) return redirecionar(paraLogin, true);

  try {
    const { par, refresh } = await rotacionar(atual);
    const resposta = redirecionar(new URL(destino, url.origin), false);
    gravarSessao(resposta.cookies, par, refresh);
    return resposta;
  } catch {
    return redirecionar(paraLogin, true);
  }
}

function redirecionar(url: URL, limpar: boolean): NextResponse {
  // 303 força o método GET no destino e `no-store` impede que o navegador
  // guarde um redirecionamento que depende de cookie para estar certo.
  const resposta = NextResponse.redirect(url, { status: 303 });
  resposta.headers.set("Cache-Control", "no-store");
  if (limpar) limparSessao(resposta.cookies);
  return resposta;
}
