"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { mobileTabs, navigation } from "@/config/navigation";
import { estaAtivo } from "@/components/layout/nav-utils";
import { cn } from "@/lib/utils";

/**
 * Barra inferior do celular: cinco destinos escolhidos a dedo em
 * `navigation.ts` — WhatsApp está entre eles de propósito, porque a gestão
 * atende do celular.
 *
 * `env(safe-area-inset-bottom)` é obrigatório: sem ele, a barra de gestos do
 * iPhone come a linha de rótulos.
 */
export function MobileNav({ hrefs }: { hrefs: readonly string[] }) {
  const pathname = usePathname();
  const permitidos = React.useMemo(() => new Set(hrefs), [hrefs]);

  // A aba é a interseção entre a escolha editorial e o que o usuário alcança —
  // um destino que ele não pode abrir viraria um toque para um 403.
  const abas = React.useMemo(
    () =>
      mobileTabs
        .filter((href) => permitidos.has(href))
        .flatMap((href) => navigation.filter((item) => item.href === href)),
    [permitidos],
  );

  if (abas.length === 0) return null;

  return (
    <nav
      aria-label="Navegação rápida"
      className={cn(
        "glass fixed inset-x-0 bottom-0 z-40 rounded-t-3xl border-t border-border/50 md:hidden",
        "pb-[env(safe-area-inset-bottom)]",
      )}
    >
      <ul className="flex h-[var(--mobile-nav-height)] items-stretch justify-around px-1">
        {abas.map((item) => {
          const ativo = estaAtivo(pathname, item.href);
          return (
            <li key={item.href} className="flex-1">
              <Link
                href={item.href}
                aria-current={ativo ? "page" : undefined}
                className={cn(
                  "flex h-full flex-col items-center justify-center gap-0.5 rounded-2xl text-[0.62rem] transition-colors",
                  ativo ? "text-primary" : "text-muted-foreground",
                )}
              >
                <item.icon className={cn("size-5", ativo && "drop-shadow-[0_1px_6px_oklch(0.44_0.055_110_/_45%)]")} />
                <span className="truncate">{item.title}</span>
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
