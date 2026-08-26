import { NextResponse } from "next/server";
import { z } from "zod";

import { ApiError, apiFetch } from "@/lib/api/client";
import type { ParDeTokens } from "@/lib/api/types";
import { gravarSessao } from "@/lib/auth/session";
import { extrairRefresh, respostaDeErro } from "@/lib/auth/upstream";

/**
 * Ponte de login. O navegador manda e-mail e senha para cá, **nunca** para a
 * API Go: o par de tokens é trocado por cookies `httpOnly` antes de qualquer
 * coisa voltar para a página. O access token não aparece na resposta — se
 * aparecesse, teria acabado o motivo de existir este arquivo.
 */

const Corpo = z.object({
  email: z.string().email(),
  password: z.string().min(1),
});

export async function POST(request: Request): Promise<NextResponse> {
  let bruto: unknown;
  try {
    bruto = await request.json();
  } catch {
    return respostaDeErro(new ApiError("VALIDATION_ERROR", "Corpo inválido.", 422));
  }

  const analise = Corpo.safeParse(bruto);
  if (!analise.success) {
    return respostaDeErro(new ApiError("VALIDATION_ERROR", "Informe e-mail e senha.", 422));
  }

  try {
    let refresh: string | undefined;

    const par = await apiFetch<ParDeTokens>("/auth/login", {
      method: "POST",
      anonymous: true,
      body: analise.data,
      onResponse: (resposta) => {
        refresh = extrairRefresh(resposta);
      },
    });

    // Só o usuário volta. Quem precisa do token é o servidor, e ele lê do cookie.
    const resposta = NextResponse.json({ data: { user: par.user } }, { status: 200 });
    gravarSessao(resposta.cookies, par, refresh);
    return resposta;
  } catch (erro) {
    // `INVALID_CREDENTIALS` chega igual para e-mail inexistente, senha errada e
    // e-mail bloqueado — repassar o código cru mantém essa indistinção, que é o
    // que impede a tela de virar oráculo de "quem tem conta aqui".
    return respostaDeErro(erro);
  }
}
