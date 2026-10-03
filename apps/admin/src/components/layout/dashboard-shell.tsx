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
 * A PÁGINA rola, não o painel. Até 03/10/2026 o painel branco tinha altura fixa
 * e rolava por dentro (`overflow-y-auto` + `overscroll-contain`): a roda do
 * mouse fora da caixa não rolava nada, tabelas e o mapa dentro dela prendiam o
 * gesto, e o gestor achava que a tela tinha travado. Agora quem rola é o
 * documento, como em qualquer site, e a barra de cima fica presa ao topo
 * (`sticky`). A tela de mapa continua com área de rolagem própria — ela mesma
 * se dá a altura da janela (`altura-da-tela-cheia`, em globals.css).
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
    <div
      className={cn(
        "mx-auto flex min-h-dvh w-full max-w-[110rem] flex-col px-3 pt-4 sm:px-4",
        // No celular a barra de navegação de baixo é fixa: o fim da página
        // precisa de espaço para não ficar escondido atrás dela.
        "pb-[calc(var(--mobile-nav-height)+env(safe-area-inset-bottom)+0.75rem)] md:pb-3",
      )}
    >
      <div className="sticky top-2 z-40">
        <AppHeader user={user} hrefs={hrefs} />
      </div>

      <main className="panel-float mt-3 flex-1">{children}</main>

      <MobileNav hrefs={hrefs} />
    </div>
  );
}
