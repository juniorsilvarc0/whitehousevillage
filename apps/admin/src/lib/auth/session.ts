import { cache } from "react";
import { cookies, headers } from "next/headers";
import { redirect } from "next/navigation";

import { ApiError, apiFetch, isApiError } from "@/lib/api/client";
import type { ParDeTokens, Sessao } from "@/lib/api/types";
import {
  HEADER_PATHNAME,
  REFRESH_COOKIE_NAME,
  SESSION_COOKIE_NAME,
  opcoesDaSessao,
  opcoesDeRemocao,
  opcoesDoRefresh,
} from "@/lib/auth/cookies";

export { HEADER_PATHNAME, REFRESH_COOKIE_NAME, SESSION_COOKIE_NAME };

/** Interface mínima do jar mutável — `cookies()` em Route Handler e o
 *  `NextResponse.cookies` atendem os dois métodos. Tipar assim evita amarrar
 *  este módulo a um deles. */
type JarGravavel = {
  set(name: string, value: string, options?: Record<string, unknown>): unknown;
  delete?(name: string): unknown;
};

// ── Leitura ────────────────────────────────────────────────────────────────

export async function lerAccessToken(): Promise<string | undefined> {
  return (await cookies()).get(SESSION_COOKIE_NAME)?.value;
}

export async function lerRefreshToken(): Promise<string | undefined> {
  return (await cookies()).get(REFRESH_COOKIE_NAME)?.value;
}

// ── Escrita ────────────────────────────────────────────────────────────────

/**
 * Grava o par recém-emitido. O access vale o que a API disse (`expires_in`); o
 * refresh, 30 dias. Refresh ausente **apaga** o cookie antigo em vez de mantê-lo:
 * a rotação já matou o token anterior e reapresentá-lo dispararia
 * `TOKEN_REUSED`, derrubando a família inteira de sessões do usuário.
 */
export function gravarSessao(jar: JarGravavel, par: ParDeTokens, refreshToken?: string): void {
  jar.set(SESSION_COOKIE_NAME, par.access_token, opcoesDaSessao(par.expires_in));
  if (refreshToken) jar.set(REFRESH_COOKIE_NAME, refreshToken, opcoesDoRefresh());
  else jar.set(REFRESH_COOKIE_NAME, "", opcoesDeRemocao());
}

export function limparSessao(jar: JarGravavel): void {
  jar.set(SESSION_COOKIE_NAME, "", opcoesDeRemocao());
  jar.set(REFRESH_COOKIE_NAME, "", opcoesDeRemocao());
}

// ── Sessão ─────────────────────────────────────────────────────────────────

/**
 * Usuário autenticado e a matriz de permissões do perfil, direto de `/auth/me`.
 *
 * Envolvido em `cache()` do React: uma árvore de RSC pode perguntar pela sessão
 * em cinco componentes diferentes e ainda assim sai uma requisição só por
 * render. O cache é por requisição — não vaza sessão entre usuários.
 *
 * Devolve `null` em vez de lançar quando não há sessão válida: "não logado" é
 * estado esperado, não exceção. Erro de rede também vira `null`, senão a API
 * fora do ar transformaria toda tela numa página de erro em vez de mandar
 * para o login.
 */
export const getSession = cache(async (): Promise<Sessao | null> => {
  const token = await lerAccessToken();
  if (!token) return null;

  try {
    return await apiFetch<Sessao>("/auth/me", { accessToken: token });
  } catch (erro) {
    if (isApiError(erro) && (erro.code === "UNAUTHORIZED" || erro.code === "NETWORK_ERROR")) return null;
    // 403, 500 e afins são problema de verdade: deixar subir para o boundary de
    // erro em vez de fingir que o usuário nunca esteve logado.
    throw erro;
  }
});

/** Sessão obrigatória. Sem ela, redireciona para o login guardando o destino. */
export async function requireSession(): Promise<Sessao> {
  const sessao = await getSession();
  if (sessao) return sessao;
  redirect(`/login?next=${encodeURIComponent(await caminhoAtual())}`);
}

async function caminhoAtual(): Promise<string> {
  const cabecalhos = await headers();
  return cabecalhos.get(HEADER_PATHNAME) ?? "/app";
}

/** Erro para quando um caminho de servidor precisa recusar sem redirecionar
 *  (Route Handler, por exemplo). */
export function naoAutenticado(): ApiError {
  return new ApiError("UNAUTHORIZED", "Sessão ausente ou expirada.", 401);
}
