"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Dialog } from "@base-ui/react/dialog";
import { X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { agruparPorSecao, estaAtivo, itensPorHref } from "@/components/layout/nav-utils";
import { cn } from "@/lib/utils";

/**
 * Gaveta de navegação — o menu inteiro, agrupado por seção.
 *
 * Ocupa `88dvh` porque o que não coube na barra vive aqui: doze destinos num
 * popover apertado viram uma lista de rolagem cega. A faixa de 12dvh que sobra
 * mostra o conteúdo atrás e deixa claro que dá para fechar tocando fora.
 */
export function NavigationDrawer({
  open,
  onOpenChange,
  hrefs,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  hrefs: readonly string[];
}) {
  const pathname = usePathname();
  const secoes = React.useMemo(() => agruparPorSecao(itensPorHref(hrefs)), [hrefs]);

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop
          className={cn(
            "fixed inset-0 z-50 bg-[oklch(0.22_0.016_130_/_48%)] backdrop-blur-[2px]",
            "transition-opacity duration-200 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0",
          )}
        />
        <Dialog.Popup
          className={cn(
            "panel-float fixed inset-x-0 bottom-0 z-50 flex h-[88dvh] flex-col overflow-hidden outline-none",
            "rounded-b-none rounded-t-3xl",
            "transition-transform duration-250 ease-out",
            "data-[ending-style]:translate-y-full data-[starting-style]:translate-y-full",
          )}
        >
          <header className="flex shrink-0 items-center gap-3 border-b border-border/60 px-5 py-4">
            <span
              aria-hidden="true"
              className="bg-brand-gradient flex size-9 items-center justify-center rounded-xl text-xs font-semibold text-white"
            >
              WH
            </span>
            <div className="min-w-0 flex-1">
              <Dialog.Title className="font-display text-base leading-tight">Navegação</Dialog.Title>
              <Dialog.Description className="text-xs text-muted-foreground">
                White House Village · gestão
              </Dialog.Description>
            </div>
            <Dialog.Close
              render={
                <Button variant="ghost" size="iconSm" aria-label="Fechar navegação">
                  <X aria-hidden="true" />
                </Button>
              }
            />
          </header>

          <nav
            aria-label="Navegação principal"
            className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-3 py-4 pb-[calc(1rem+env(safe-area-inset-bottom))]"
          >
            {secoes.map((secao) => (
              <section key={secao.grupo} className="mb-5 last:mb-0">
                <h2 className="px-3 pb-2 text-[0.68rem] font-medium uppercase tracking-[0.18em] text-muted-foreground">
                  {secao.grupo}
                </h2>
                <ul className="flex flex-col gap-1">
                  {secao.itens.map((item) => {
                    const ativo = estaAtivo(pathname, item.href);
                    return (
                      <li key={item.href}>
                        <Link
                          href={item.href}
                          onClick={() => onOpenChange(false)}
                          aria-current={ativo ? "page" : undefined}
                          className={cn(
                            "flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm transition-colors",
                            ativo
                              ? "bg-brand-gradient text-white"
                              : "text-foreground hover:bg-muted",
                          )}
                        >
                          <item.icon className="size-4.5 shrink-0" />
                          <span className="truncate">{item.title}</span>
                        </Link>
                      </li>
                    );
                  })}
                </ul>
              </section>
            ))}
          </nav>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
