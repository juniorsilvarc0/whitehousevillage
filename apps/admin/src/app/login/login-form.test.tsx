import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { LoginForm } from "@/app/login/login-form";

/**
 * O formulário de login é um componente de decisão: ele escolhe o que contar
 * para quem errou. Duas coisas se protegem aqui.
 *
 * 1. **A mensagem é sempre a mesma.** O contrato usa um único
 *    `INVALID_CREDENTIALS` para e-mail inexistente, senha errada e e-mail
 *    bloqueado por tentativas. Se a tela distinguisse, devolveria pela
 *    interface a enumeração de usuários que a API fecha de propósito.
 * 2. **A validação segura o envio.** Campo vazio não vira requisição — é o que
 *    evita gastar uma das cinco tentativas por um Enter apressado.
 */

const replace = vi.fn();
const refresh = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace, refresh }),
}));

function respostaDeErro(status: number, code: string): Response {
  return {
    ok: false,
    status,
    json: async () => ({ error: { code, message: "mensagem que a tela deve ignorar" } }),
  } as unknown as Response;
}

function respostaDeSucesso(): Response {
  return {
    ok: true,
    status: 200,
    json: async () => ({ data: { user: { id: "u1" } } }),
  } as unknown as Response;
}

function preencher(email: string, senha: string) {
  fireEvent.change(screen.getByLabelText("E-mail"), { target: { value: email } });
  fireEvent.change(screen.getByLabelText("Senha"), { target: { value: senha } });
}

function enviar() {
  fireEvent.click(screen.getByRole("button", { name: /entrar/i }));
}

describe("LoginForm", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
    replace.mockClear();
    refresh.mockClear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // Os dois testes abaixo guardam um defeito já corrigido, e o registro fica:
  // `@hookform/resolvers@4.1.3` foi feito para o zod 3 e detectava o erro de
  // validação por `Array.isArray(error.errors)` — campo que o zod 4 renomeou
  // para `issues`. Com isso o `ZodError` escapava do resolver como promessa
  // rejeitada: clicar em "Entrar" com o campo vazio não mostrava mensagem
  // nenhuma e prendia o botão em "Entrando…" para sempre. Corrigido subindo o
  // resolver para ^5 (o primeiro que fala zod 4). Voltar a dependência para a
  // major 4 sem baixar o zod junto ressuscita a tela morta — e estes dois
  // testes voltam a vermelho antes de qualquer usuário perceber.
  it("recusa o envio com os campos vazios e avisa o usuário", async () => {
    render(<LoginForm destino="/app" />);
    enviar();

    await waitFor(() => {
      expect(
        document.body.textContent,
        "clicar em Entrar com os campos vazios não produziu mensagem nenhuma na tela",
      ).toContain("Informe o e-mail.");
    });
    expect(document.body.textContent).toContain("Informe a senha.");
    expect(fetch, "campo vazio não pode virar requisição — gasta uma das 5 tentativas").not.toHaveBeenCalled();
  });

  it("recusa e-mail malformado antes de sair do navegador", async () => {
    render(<LoginForm destino="/app" />);
    preencher("gestao@", "uma-senha-qualquer");
    enviar();

    await waitFor(() => {
      expect(
        document.body.textContent,
        "e-mail malformado não produziu mensagem: a validação do formulário não está funcionando",
      ).toContain("E-mail inválido.");
    });
    expect(fetch).not.toHaveBeenCalled();
  });

  it("mostra a mesma mensagem genérica em INVALID_CREDENTIALS, sem dizer qual campo errou", async () => {
    vi.mocked(fetch).mockResolvedValue(respostaDeErro(401, "INVALID_CREDENTIALS"));

    render(<LoginForm destino="/app" />);
    preencher("gestao@wh.local", "senha-errada");
    enviar();

    const aviso = await screen.findByRole("alert");
    expect(aviso.textContent).toBe("E-mail ou senha incorretos.");

    // Nada na tela pode sugerir que a conta existe (ou não existe).
    const texto = document.body.textContent ?? "";
    for (const proibido of ["não existe", "não encontrado", "não cadastrado", "senha incorreta para"]) {
      expect(texto.toLowerCase()).not.toContain(proibido);
    }
    // E a mensagem que a API mandou não aparece: a tela reage ao code.
    expect(texto).not.toContain("mensagem que a tela deve ignorar");
  });

  it("limpa a senha e devolve o foco a ela depois da recusa", async () => {
    vi.mocked(fetch).mockResolvedValue(respostaDeErro(401, "INVALID_CREDENTIALS"));

    render(<LoginForm destino="/app" />);
    preencher("gestao@wh.local", "senha-errada");
    enviar();

    await screen.findByRole("alert");

    const senha = screen.getByLabelText("Senha") as HTMLInputElement;
    expect(senha.value).toBe("");
    expect(document.activeElement).toBe(senha);
  });

  it("explica o bloqueio por tentativas sem confundir com senha errada", async () => {
    vi.mocked(fetch).mockResolvedValue(respostaDeErro(429, "RATE_LIMITED"));

    render(<LoginForm destino="/app" />);
    preencher("gestao@wh.local", "seja-la-qual-for");
    enviar();

    const aviso = await screen.findByRole("alert");
    expect(aviso.textContent).toBe("Tentativas demais. Espere alguns minutos e tente de novo.");
  });

  it("cai numa mensagem genérica quando o código do erro é desconhecido", async () => {
    // Código novo no contrato não pode deixar a tela muda: o usuário precisa
    // saber que falhou, ainda que a tela não saiba por quê.
    vi.mocked(fetch).mockResolvedValue(respostaDeErro(500, "CODIGO_QUE_A_TELA_NAO_CONHECE"));

    render(<LoginForm destino="/app" />);
    preencher("gestao@wh.local", "uma-senha-qualquer");
    enviar();

    const aviso = await screen.findByRole("alert");
    expect(aviso.textContent).toBe("Não foi possível entrar agora. Tente novamente.");
  });

  it("avisa quando a API não responde, em vez de travar no botão", async () => {
    vi.mocked(fetch).mockRejectedValue(new Error("connect ECONNREFUSED"));

    render(<LoginForm destino="/app" />);
    preencher("gestao@wh.local", "uma-senha-qualquer");
    enviar();

    const aviso = await screen.findByRole("alert");
    expect(aviso.textContent).toBe(
      "Sem conexão com o sistema agora. Verifique a internet e tente de novo em instantes.",
    );
  });

  it("no sucesso, leva ao destino pedido sem deixar o login no histórico", async () => {
    vi.mocked(fetch).mockResolvedValue(respostaDeSucesso());

    render(<LoginForm destino="/app/reservas" />);
    preencher("gestao@wh.local", "senha-correta");
    enviar();

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/app/reservas"));
    // O refresh existe para os componentes de servidor relerem o cookie recém
    // gravado; sem ele a casca renderiza deslogada.
    expect(refresh).toHaveBeenCalled();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("manda as credenciais para o BFF, nunca direto para a API Go", async () => {
    vi.mocked(fetch).mockResolvedValue(respostaDeSucesso());

    render(<LoginForm destino="/app" />);
    preencher("Gestao@WH.local", "senha-correta");
    enviar();

    await waitFor(() => expect(fetch).toHaveBeenCalled());

    const [url, init] = vi.mocked(fetch).mock.calls[0];
    expect(url).toBe("/api/auth/login");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(String(init?.body))).toEqual({
      email: "Gestao@WH.local",
      password: "senha-correta",
    });
  });
});
