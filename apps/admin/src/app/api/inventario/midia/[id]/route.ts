import { apiBaseUrl } from "@/lib/api/client";
import { lerAccessToken } from "@/lib/auth/session";

/**
 * `GET /api/inventario/midia/{id}[?size=thumb]` — a foto de um bem, para o
 * `<img>` do painel.
 *
 * **A foto do inventário nunca é pública.** Ela mostra o interior da casa — o
 * que há de valor e onde fica —, e por isso `GET /inventory/media/{id}` exige
 * token e `inventory.goods:ver`. O navegador não tem o token (cookie
 * `httpOnly`), então esta rota repassa o pedido com o `Authorization`, em
 * fluxo, e devolve o arquivo com os cabeçalhos de conteúdo como vieram.
 *
 * O `Cache-Control` da API é `private, max-age=31536000, immutable` e é
 * repassado como está: a linha de mídia é imutável, então o cache longo é
 * seguro, e `private` impede que um proxy compartilhado guarde o interior da
 * casa. Sem cabeçalho da API, o padrão aqui também é `private`.
 */
export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

const REPASSADOS = ["content-type", "content-length", "etag", "last-modified", "cache-control"];

export async function GET(request: Request, { params }: { params: Promise<{ id: string }> }): Promise<Response> {
  const token = await lerAccessToken();
  if (!token) return new Response(null, { status: 401 });

  const { id } = await params;
  if (!ID.test(id)) return new Response(null, { status: 404 });

  const pedido = new URL(request.url).searchParams.get("size");
  const tamanho = pedido === "thumb" || pedido === "original" ? `?size=${pedido}` : "";

  const cabecalhos: Record<string, string> = { Authorization: `Bearer ${token}` };
  for (const nome of ["if-none-match", "if-modified-since"]) {
    const v = request.headers.get(nome);
    if (v) cabecalhos[nome] = v;
  }

  let resposta: Response;
  try {
    resposta = await fetch(`${apiBaseUrl()}/inventory/media/${id}${tamanho}`, {
      headers: cabecalhos,
      cache: "no-store",
      signal: request.signal,
    });
  } catch {
    return new Response(null, { status: 502 });
  }

  const saida = new Headers();
  for (const nome of REPASSADOS) {
    const v = resposta.headers.get(nome);
    if (v) saida.set(nome, v);
  }
  if (!saida.has("cache-control")) saida.set("cache-control", "private, no-store");
  return new Response(resposta.body, { status: resposta.status, headers: saida });
}
