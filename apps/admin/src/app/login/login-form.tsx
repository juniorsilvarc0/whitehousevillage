"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Loader2, LogIn } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

// `@hookform/resolvers` precisa ser >= 5: a major 4 reconhecia o erro do zod por
// `error.errors`, campo que o zod 4 renomeou para `issues`. Com a versão errada
// o erro de validação não vira mensagem de campo — ele escapa como promessa
// rejeitada e trava o formulário em "Entrando…", sem aviso nenhum na tela.
const Credenciais = z.object({
  email: z.string().min(1, "Informe o e-mail.").email("E-mail inválido."),
  password: z.string().min(1, "Informe a senha."),
});

type Credenciais = z.infer<typeof Credenciais>;

type RespostaDeErro = { error?: { code?: string; message?: string; details?: Record<string, unknown> } };

/**
 * Mensagem por código, nunca por texto vindo da API.
 *
 * `INVALID_CREDENTIALS` cobre três casos no contrato — e-mail inexistente, senha
 * errada e e-mail bloqueado por tentativas — e a mensagem aqui é uma só de
 * propósito. Escrever "este e-mail não existe" entregaria de graça a lista de
 * quem tem conta.
 */
function mensagemDoErro(code: string | undefined): string {
  switch (code) {
    case "INVALID_CREDENTIALS":
      return "E-mail ou senha incorretos.";
    case "RATE_LIMITED":
      return "Tentativas demais. Espere alguns minutos e tente de novo.";
    case "NETWORK_ERROR":
      return "Não foi possível falar com o servidor. Tente novamente em instantes.";
    case "VALIDATION_ERROR":
      return "Confira os dados informados.";
    default:
      return "Não foi possível entrar agora. Tente novamente.";
  }
}

export function LoginForm({ destino }: { destino: string }) {
  const router = useRouter();
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);

  const form = useForm<Credenciais>({
    resolver: zodResolver(Credenciais),
    defaultValues: { email: "", password: "" },
    mode: "onSubmit",
  });

  const { errors, isSubmitting } = form.formState;

  async function enviar(valores: Credenciais) {
    setErroGeral(null);
    try {
      const resposta = await fetch("/api/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(valores),
      });

      if (!resposta.ok) {
        const corpo = (await resposta.json().catch(() => ({}))) as RespostaDeErro;
        setErroGeral(mensagemDoErro(corpo.error?.code));
        // Foco volta para a senha: é o campo que quase sempre está errado, e
        // devolver o cursor evita uma navegação de teclado inteira.
        form.setFocus("password");
        form.resetField("password");
        return;
      }

      // `replace` para o login não voltar no histórico, e `refresh` para os RSC
      // relerem o cookie recém-gravado — sem ele a casca renderiza deslogada.
      router.replace(destino);
      router.refresh();
    } catch {
      setErroGeral(mensagemDoErro("NETWORK_ERROR"));
    }
  }

  return (
    <form noValidate onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
      {erroGeral ? (
        <p
          role="alert"
          aria-live="assertive"
          className="rounded-xl border border-destructive/30 bg-destructive/8 px-3.5 py-2.5 text-sm text-destructive"
        >
          {erroGeral}
        </p>
      ) : null}

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="email">E-mail</Label>
        <Input
          id="email"
          type="email"
          inputMode="email"
          autoComplete="username"
          autoFocus
          placeholder="voce@whitehousevillage.com.br"
          invalid={Boolean(errors.email)}
          aria-describedby={errors.email ? "email-erro" : undefined}
          {...form.register("email")}
        />
        {errors.email ? (
          <p id="email-erro" className="text-xs text-destructive">
            {errors.email.message}
          </p>
        ) : null}
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="password">Senha</Label>
        <Input
          id="password"
          type="password"
          autoComplete="current-password"
          placeholder="••••••••"
          invalid={Boolean(errors.password)}
          aria-describedby={errors.password ? "password-erro" : undefined}
          {...form.register("password")}
        />
        {errors.password ? (
          <p id="password-erro" className="text-xs text-destructive">
            {errors.password.message}
          </p>
        ) : null}
      </div>

      <Button type="submit" size="lg" disabled={isSubmitting} className={cn("mt-2 w-full")}>
        {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : <LogIn aria-hidden="true" />}
        {isSubmitting ? "Entrando…" : "Entrar"}
      </Button>

      <p className="text-center text-xs text-muted-foreground">
        Esqueceu a senha? Fale com a gestão para receber um link de recuperação.
      </p>
    </form>
  );
}
