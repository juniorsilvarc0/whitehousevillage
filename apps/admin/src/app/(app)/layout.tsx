import type { Metadata } from "next";

import { DashboardShell } from "@/components/layout/dashboard-shell";
import { navegacaoVisivel } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";

export const metadata: Metadata = {
  title: {
    default: "Painel · White House Village",
    template: "%s · White House Village",
  },
};

/**
 * Toda tela autenticada passa por aqui.
 *
 * `force-dynamic` porque a casca depende de cookie e de `/auth/me`: prerender
 * serviria a casca de uma pessoa para outra — e, num build sem API no ar, nem
 * chegaria a existir HTML para servir.
 */
export const dynamic = "force-dynamic";

export default async function AppLayout({ children }: { children: React.ReactNode }) {
  const { user, permissions } = await requireSession();

  // Só hrefs atravessam para o cliente: `NavItem.icon` é uma função e não passa
  // pela serialização RSC. O componente do ícone é resolvido do outro lado.
  const hrefs = navegacaoVisivel(user, permissions).map((item) => item.href);

  return (
    <DashboardShell
      user={{ nome: user.name, email: user.email, papel: user.role_name }}
      hrefs={hrefs}
    >
      {children}
    </DashboardShell>
  );
}
