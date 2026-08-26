import { NextResponse } from "next/server";

import { ApiError, isApiError } from "@/lib/api/client";

/** Nome do cookie de refresh **da API Go** (`Path=/api/v1/auth`). Ele nunca
 *  chega ao navegador: morre aqui no BFF, que o reemite com o nome e o path do
 *  painel. */
const COOKIE_UPSTREAM = "wh_refresh";

/**
 * Pesca o refresh rotacionado do `Set-Cookie` da API.
 *
 * A rotação é obrigatória no contrato: cada `/auth/refresh` bem-sucedido mata o
 * token apresentado. Se este parse falhar, o painel fica com um token morto na
 * mão e a próxima renovação dispara `TOKEN_REUSED` — que revoga a família
 * inteira e derruba o usuário. Por isso o chamador trata "não achei" como
 * "apaga o cookie", não como "mantém o antigo".
 */
export function extrairRefresh(resposta: Response): string | undefined {
  const cabecalhos = resposta.headers as Headers & { getSetCookie?: () => string[] };
  const linhas = cabecalhos.getSetCookie?.() ?? (resposta.headers.get("set-cookie") ? [resposta.headers.get("set-cookie")!] : []);

  for (const linha of linhas) {
    const primeiro = linha.split(";", 1)[0] ?? "";
    const igual = primeiro.indexOf("=");
    if (igual < 0) continue;
    if (primeiro.slice(0, igual).trim() !== COOKIE_UPSTREAM) continue;
    const valor = primeiro.slice(igual + 1).trim();
    if (valor) return valor;
  }
  return undefined;
}

/** Traduz o erro para o envelope do contrato, preservando o `code` — a tela
 *  reage ao código, nunca à mensagem. */
export function respostaDeErro(erro: unknown): NextResponse {
  if (isApiError(erro)) {
    return NextResponse.json(
      { error: { code: erro.code, message: erro.message, details: erro.details } },
      { status: erro.status === 0 ? 502 : erro.status },
    );
  }
  const desconhecido = new ApiError("INTERNAL", "Erro inesperado no painel.", 500);
  return NextResponse.json(
    { error: { code: desconhecido.code, message: desconhecido.message, details: {} } },
    { status: 500 },
  );
}

/**
 * Só caminho interno é destino válido de redirecionamento. `//evil.com` é uma
 * URL protocol-relative: sem esta checagem, `?next=//evil.com` transformaria o
 * login num open redirect com a marca do cliente na barra de endereço.
 */
export function destinoSeguro(bruto: string | null | undefined, padrao = "/app"): string {
  if (!bruto) return padrao;
  if (!bruto.startsWith("/") || bruto.startsWith("//")) return padrao;
  return bruto;
}
