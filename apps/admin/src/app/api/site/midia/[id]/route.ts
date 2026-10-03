import { apiBaseUrl } from "@/lib/api/client";
import { lerAccessToken } from "@/lib/auth/session";

/**
 * `GET /api/site/midia/{id}` — prévia, no painel, de uma foto ou vídeo enviado.
 *
 * O arquivo é público na API (`GET /public/media/{id}`), mas o endereço que ela
 * devolve é relativo ao site, não ao painel. Esta rota repassa o pedido em
 * fluxo, com o `Range` (o `<video>` pede o arquivo em pedaços) e devolve status
 * e cabeçalhos de conteúdo como vieram. Só para quem está logado no painel.
 */
export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

const REPASSADOS = ["content-type", "content-length", "content-range", "accept-ranges", "etag", "last-modified", "cache-control"];

export async function GET(request: Request, { params }: { params: Promise<{ id: string }> }): Promise<Response> {
  if (!(await lerAccessToken())) return new Response(null, { status: 401 });

  const { id } = await params;
  if (!ID.test(id)) return new Response(null, { status: 404 });

  const pedido: Record<string, string> = {};
  for (const nome of ["range", "if-none-match", "if-modified-since", "if-range"]) {
    const v = request.headers.get(nome);
    if (v) pedido[nome] = v;
  }

  let resposta: Response;
  try {
    resposta = await fetch(`${apiBaseUrl()}/public/media/${id}`, { headers: pedido, cache: "no-store", signal: request.signal });
  } catch {
    return new Response(null, { status: 502 });
  }

  const saida = new Headers();
  for (const nome of REPASSADOS) {
    const v = resposta.headers.get(nome);
    if (v) saida.set(nome, v);
  }
  return new Response(resposta.body, { status: resposta.status, headers: saida });
}
