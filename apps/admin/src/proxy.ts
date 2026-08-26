import { NextResponse, type NextRequest } from "next/server";

import { HEADER_PATHNAME, REFRESH_COOKIE_NAME, SESSION_COOKIE_NAME } from "@/lib/auth/cookies";

/**
 * Guarda de **navegação** do painel.
 *
 * Este é o arquivo que o Next 15 chamava de `middleware.ts`. O Next 16 renomeou
 * a convenção para `proxy.ts` e passou a avisar no build a cada compilação com
 * o nome antigo — manter `middleware.ts` só trocaria um arquivo de nome por um
 * warning permanente no CI, e o nome some no Next 17 de qualquer forma.
 *
 * Isto não autoriza nada. O guard de verdade é o middleware da API Go, que
 * confere papel × recurso × ação × escopo a cada requisição e responde 403 —
 * este aqui só evita que alguém sem sessão veja a casca piscar antes de ser
 * mandado embora. Um cookie forjado passa por aqui e morre na primeira chamada
 * de dados, que é exatamente onde tem que morrer.
 *
 * Roda no edge: nada de decodificar JWT, consultar banco ou falar com a API.
 * Presença de cookie é tudo que se decide neste ponto.
 */
export function proxy(request: NextRequest): NextResponse {
  const { pathname, search } = request.nextUrl;
  const destino = `${pathname}${search}`;

  const temSessao = request.cookies.has(SESSION_COOKIE_NAME);
  if (!temSessao) {
    // Access expirado com refresh vivo é o caso comum (o cookie de sessão morre
    // junto com o token, em 15 min). Passa pela rota de refresh, que rotaciona e
    // devolve o usuário à página pedida — sem essa escala, todo intervalo de
    // café terminaria em tela de login.
    const rota = request.cookies.has(REFRESH_COOKIE_NAME) ? "/api/auth/refresh" : "/login";
    const url = new URL(rota, request.nextUrl.origin);
    url.searchParams.set("next", destino);
    return NextResponse.redirect(url);
  }

  // O RSC não enxerga a URL corrente; o `requireSession()` lê daqui para montar
  // o `?next=` quando a sessão cai no meio da renderização.
  const cabecalhos = new Headers(request.headers);
  cabecalhos.set(HEADER_PATHNAME, destino);
  return NextResponse.next({ request: { headers: cabecalhos } });
}

export const config = {
  matcher: ["/app/:path*"],
};
