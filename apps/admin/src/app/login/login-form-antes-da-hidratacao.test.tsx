import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import { LoginForm } from "@/app/login/login-form";

/**
 * O formulário de login ANTES da hidratação.
 *
 * O HTML do login chega ao navegador renderizado no servidor; o `onSubmit` do
 * React só existe depois que o JavaScript carrega e hidrata a página. No
 * celular dentro do apartamento — o uso que a Fase 5 trouxe para o painel —
 * isso leva segundos. Nesse intervalo, tocar em "Entrar" faz o navegador
 * enviar o formulário SOZINHO, e o padrão de um `<form>` sem `method` é GET:
 * o e-mail e a SENHA vão para a barra de endereço, para o histórico do
 * navegador e para o log de acesso do servidor.
 *
 * Medido no E2E de bens (tests/e2e/bens-contagem-celular.mjs), no log do
 * `next dev`: `GET /login?email=gestao%40wh.local&password=whv%402026 200`.
 *
 * O teste olha o HTML que o servidor entrega e exige que um envio nativo não
 * consiga mandar a senha por GET: ou o formulário é POST, ou o campo de senha
 * não tem `name`, ou o botão nasce desabilitado até a hidratação.
 */

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn(), refresh: vi.fn(), push: vi.fn() }),
}));

describe("LoginForm antes da hidratação", () => {
  it("não deixa um toque apressado em Entrar mandar a senha na URL", () => {
    const html = renderToStaticMarkup(<LoginForm destino="/app" />);
    const doc = new DOMParser().parseFromString(html, "text/html");
    const form = doc.querySelector("form");
    expect(form, "o login renderiza um <form>").not.toBeNull();

    const metodo = (form!.getAttribute("method") ?? "get").toLowerCase();
    const senha = form!.querySelector('input[type="password"]');
    const botao = form!.querySelector('button[type="submit"]');

    const senhaViajariaNaUrl = metodo === "get" && Boolean(senha?.getAttribute("name")) && !botao?.hasAttribute("disabled");

    expect(
      senhaViajariaNaUrl,
      `envio nativo antes da hidratação: method=${metodo}, senha name=${senha?.getAttribute("name")}, ` +
        `botão desabilitado=${botao?.hasAttribute("disabled")} — a senha iria para a query string`,
    ).toBe(false);
  });
});
