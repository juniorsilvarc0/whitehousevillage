import type { Metadata } from "next";
import { redirect } from "next/navigation";

import { LoginForm } from "@/app/login/login-form";
import { getSession } from "@/lib/auth/session";
import { destinoSeguro } from "@/lib/auth/upstream";

export const metadata: Metadata = {
  title: "Entrar · White House Village",
};

// A página lê cookie para saber se já há sessão: é dinâmica por natureza, e
// declarar isso evita qualquer tentativa de prerender contra uma API que pode
// nem estar no ar durante o build.
export const dynamic = "force-dynamic";

export default async function LoginPage({
  searchParams,
}: {
  searchParams: Promise<{ next?: string }>;
}) {
  const { next } = await searchParams;
  const destino = destinoSeguro(next);

  // Quem já está logado não vê formulário de login — vai direto para onde queria.
  if (await getSession()) redirect(destino);

  return (
    <main className="flex min-h-dvh items-center justify-center p-4 sm:p-6">
      <div className="w-full max-w-sm">
        <div className="panel-float shadow-soft-lg p-6 sm:p-8">
          <div className="flex flex-col items-center gap-3 text-center">
            <span
              aria-hidden="true"
              className="bg-brand-gradient flex size-14 items-center justify-center rounded-2xl text-lg font-semibold tracking-[0.08em] text-white ring-1 ring-white/20"
            >
              WH
            </span>
            <div>
              <p className="font-display text-[0.7rem] uppercase tracking-[0.3em] text-muted-foreground">
                White House Village
              </p>
              <h1 className="font-display mt-1 text-2xl leading-tight">Entrar na gestão</h1>
            </div>
          </div>

          <div className="mt-7">
            <LoginForm destino={destino} />
          </div>
        </div>

        <p className="mt-5 text-center text-xs text-muted-foreground">
          Acesso restrito à equipe. Cada ação fica registrada em auditoria.
        </p>
      </div>
    </main>
  );
}
