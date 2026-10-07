import type { Metadata } from "next";

import { requireSession } from "@/lib/auth/session";

export const metadata: Metadata = {
  title: { default: "Impressão · White House Village", template: "%s · White House Village" },
};

/**
 * Folhas para imprimir — **sem a casca**.
 *
 * A barra flutuante, a barra de vidro do celular e o wash de fundo são a cara
 * do painel na tela e ruído no papel. Um grupo de rotas próprio dá à folha um
 * layout limpo sem que a casca precise saber que impressão existe. A sessão é
 * conferida aqui também: a folha mostra o interior da casa, e o `proxy.ts`
 * (matcher `/app/:path*`) só confere a presença do cookie.
 */
export const dynamic = "force-dynamic";

export default async function ImpressaoLayout({ children }: { children: React.ReactNode }) {
  await requireSession();
  return <div className="folha-de-impressao min-h-dvh bg-card text-foreground">{children}</div>;
}
