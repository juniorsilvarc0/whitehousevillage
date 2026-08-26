import * as React from "react";

import { AppHeader, type UsuarioDaBarra } from "@/components/layout/app-header";
import { MobileNav } from "@/components/layout/mobile-nav";
import { cn } from "@/lib/utils";

/**
 * Casca do painel: barra flutuante em cima, painel branco embaixo, barra de
 * vidro no rodapé do celular.
 *
 * A altura do `main` sai **dos tokens de geometria** — `--app-chrome-top`
 * (barra + respiro) e `--mobile-nav-height`. Os `1.5rem` que sobram no cálculo
 * são a margem visível acima e abaixo do painel, e são as únicas medidas
 * escritas aqui: mexer na altura da barra continua sendo uma linha no
 * `globals.css`, não uma caçada a `calc()` espalhado pelos componentes.
 *
 * O painel rola **por dentro** (`overflow-y-auto`). É o que mantém a barra
 * sempre no lugar e o que faz a tela de mapa e o kanban terem uma área de
 * rolagem própria em vez de arrastarem a página inteira.
 */
export function DashboardShell({
  user,
  hrefs,
  children,
}: {
  user: UsuarioDaBarra;
  hrefs: readonly string[];
  children: React.ReactNode;
}) {
  return (
    <div className="mx-auto flex min-h-dvh w-full max-w-[110rem] flex-col px-3 pb-3 pt-4 sm:px-4">
      <AppHeader user={user} hrefs={hrefs} />

      <main
        className={cn(
          "panel-float mt-3 min-h-0 flex-1 overflow-y-auto overscroll-contain",
          "h-[calc(100dvh-var(--app-chrome-top)-1.5rem-var(--mobile-nav-height)-env(safe-area-inset-bottom))]",
          "md:h-[calc(100dvh-var(--app-chrome-top)-1.5rem)]",
        )}
      >
        {children}
      </main>

      <MobileNav hrefs={hrefs} />
    </div>
  );
}
