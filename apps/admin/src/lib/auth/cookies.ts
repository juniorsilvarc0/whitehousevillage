/**
 * Nomes e opções dos cookies da sessão do painel.
 *
 * Vive num módulo sem dependência nenhuma porque três lados precisam dele e não
 * podem depender uns dos outros: o cliente da API (que lê o access token), as
 * Route Handlers (que escrevem) e o middleware (que só confere presença).
 */
/**
 * ATENÇÃO: estas duas envs precisam existir **no build**, não só no runtime.
 * O `proxy.ts` roda no edge e o Next substitui `process.env.X` por literal ao
 * compilar aquele bundle — definir só em runtime faria o proxy procurar o
 * cookie com o nome padrão enquanto o servidor grava com o nome custom, e a
 * sessão simplesmente nunca seria encontrada.
 */

/** O access token JWT. O nome vem do ambiente para não travar o deploy quando
 *  dois painéis dividirem o mesmo domínio. */
export const SESSION_COOKIE_NAME = process.env.SESSION_COOKIE_NAME ?? "whv-session";

/** O refresh rotativo da API Go. Fica aqui, no BFF, e nunca chega ao navegador
 *  como valor legível: só o Next o reapresenta em `/auth/refresh`. */
export const REFRESH_COOKIE_NAME = process.env.REFRESH_COOKIE_NAME ?? "whv-refresh";

/** Cabeçalho com o caminho pedido, injetado pelo middleware. O RSC não tem
 *  como saber a URL corrente sozinho, e o `requireSession()` precisa dela para
 *  montar o `?next=` do login. */
export const HEADER_PATHNAME = "x-whv-pathname";

/** 30 dias — a validade do refresh declarada no contrato. */
export const REFRESH_MAX_AGE = 60 * 60 * 24 * 30;

/** Fallback de validade do access token quando a API não manda `expires_in`. */
export const ACCESS_MAX_AGE_FALLBACK = 15 * 60;

/** Só o que usamos. Tipar à mão evita import profundo de `next/dist`, que muda
 *  de caminho entre minor e quebraria o typecheck sem aviso. */
type OpcoesDeCookie = {
  httpOnly: boolean;
  sameSite: "lax";
  secure: boolean;
  path: string;
  maxAge: number;
  expires?: Date;
};

/**
 * `httpOnly` é o ponto inteiro do BFF: nenhum script da página alcança o token,
 * então um XSS não sai daqui com credencial no bolso.
 *
 * `sameSite: "lax"` deixa o retorno de navegação (link externo, redirect do
 * login) chegar com a sessão, mas corta o envio em POST de terceiro.
 *
 * `path: "/"` porque o middleware precisa enxergar os dois cookies em
 * `/app/:path*` — cookie preso em `/api/auth` simplesmente não é enviado lá.
 */
function base(maxAge: number): OpcoesDeCookie {
  return {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge,
  };
}

export function opcoesDaSessao(expiresIn?: number): OpcoesDeCookie {
  // A vida do cookie acompanha a do token: quando um morre, o outro some junto
  // e o middleware para de deixar passar sem precisar decodificar JWT no edge.
  return base(expiresIn && expiresIn > 0 ? expiresIn : ACCESS_MAX_AGE_FALLBACK);
}

export function opcoesDoRefresh(): OpcoesDeCookie {
  return base(REFRESH_MAX_AGE);
}

/** Apagar é sobrescrever com `maxAge: 0` mantendo path e flags — cookie
 *  removido com atributos diferentes dos originais fica no navegador. */
export function opcoesDeRemocao(): OpcoesDeCookie {
  return { ...base(0), expires: new Date(0) };
}
