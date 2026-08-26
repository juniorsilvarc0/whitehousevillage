"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Menu as MenuIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { CommandPalette } from "@/components/layout/command-palette";
import { NavigationDrawer } from "@/components/layout/navigation-drawer";
import { UserMenu } from "@/components/layout/user-menu";
import { estaAtivo, itensPorHref } from "@/components/layout/nav-utils";
import { cn } from "@/lib/utils";

export type UsuarioDaBarra = {
  nome: string;
  email: string;
  papel: string;
};

/**
 * Barra superior flutuante — o elemento de assinatura da casca.
 *
 * Não há sidebar: a navegação inteira mora aqui no desktop e na gaveta no
 * celular. `hrefs` já vem filtrado pelo servidor (papel × matriz de permissões);
 * este componente só desenha, e esconder um item **não** é o que impede o
 * acesso — a API responde 403 de qualquer jeito.
 */
export function AppHeader({ user, hrefs }: { user: UsuarioDaBarra; hrefs: readonly string[] }) {
  const pathname = usePathname();
  const [gaveta, setGaveta] = React.useState(false);
  const itens = React.useMemo(() => itensPorHref(hrefs), [hrefs]);

  return (
    <>
      <header
        className={cn(
          "bg-brand-bar flex h-[var(--app-bar-height)] items-center gap-2 rounded-2xl px-2 text-white",
          "shadow-soft ring-1 ring-white/15",
        )}
      >
        <Link
          href="/app"
          className="flex shrink-0 items-center gap-2.5 rounded-full px-1.5 py-1 outline-none focus-visible:ring-2 focus-visible:ring-white/60"
        >
          <span
            aria-hidden="true"
            className="flex size-8 items-center justify-center rounded-xl bg-white/15 text-[0.7rem] font-semibold tracking-[0.06em] ring-1 ring-white/20"
          >
            WH
          </span>
          <span className="font-display hidden text-[0.68rem] uppercase leading-none tracking-[0.24em] sm:block md:hidden xl:block">
            White House
          </span>
          <span className="sr-only">Ir para o painel</span>
        </Link>

        {/* A navegação rola dentro da própria barra em vez de esconder itens
            atrás de "Mais": o menu inteiro continua alcançável, e nada estoura
            a largura da página. */}
        <nav
          aria-label="Navegação principal"
          className="hidden min-w-0 flex-1 md:block"
        >
          <ul className="flex items-center gap-0.5 overflow-x-auto [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
            {itens.map((item) => {
              const ativo = estaAtivo(pathname, item.href);
              return (
                <li key={item.href} className="shrink-0">
                  <Link
                    href={item.href}
                    aria-current={ativo ? "page" : undefined}
                    className={cn(
                      "flex h-9 items-center gap-1.5 rounded-full px-2.5 text-[0.8rem] transition-colors outline-none",
                      "focus-visible:ring-2 focus-visible:ring-white/60",
                      // Sobre o gradiente escuro da barra, "ativo" é o branco
                      // translúcido — o `bg-brand-gradient` sumiria no fundo.
                      ativo ? "bg-white/20 text-white ring-1 ring-white/25" : "text-white/75 hover:bg-white/12 hover:text-white",
                    )}
                  >
                    <item.icon className="size-4 shrink-0" />
                    <span className="hidden lg:inline">{item.title}</span>
                    <span className="lg:hidden sr-only">{item.title}</span>
                  </Link>
                </li>
              );
            })}
          </ul>
        </nav>

        <div className="ml-auto flex shrink-0 items-center gap-1">
          <CommandPalette hrefs={hrefs} />
          <UserMenu nome={user.nome} email={user.email} papel={user.papel} />
          <Button
            variant="onBrand"
            size="icon"
            className="md:hidden"
            aria-label="Abrir navegação"
            aria-expanded={gaveta}
            onClick={() => setGaveta(true)}
          >
            <MenuIcon aria-hidden="true" />
          </Button>
        </div>
      </header>

      <NavigationDrawer open={gaveta} onOpenChange={setGaveta} hrefs={hrefs} />
    </>
  );
}
