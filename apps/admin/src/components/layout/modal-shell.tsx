"use client";

import * as React from "react";
import { Dialog } from "@base-ui/react/dialog";
import { X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/**
 * Casca canônica de modal: header fixo, corpo rolável, footer.
 *
 * No desktop é um diálogo centrado; abaixo de `md` vira **drawer de baixo para
 * cima**, sem componente diferente e sem `useMediaQuery` — só media query no
 * CSS. Trocar de componente por largura de tela custa uma remontagem inteira e
 * perde o estado do formulário quando alguém gira o celular.
 *
 * O footer desconta `env(safe-area-inset-bottom)`: no iPhone, o botão de
 * confirmar cairia embaixo da barra de gestos.
 */
export type ModalShellProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  footer?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
};

export function ModalShell({
  open,
  onOpenChange,
  title,
  description,
  footer,
  children,
  className,
}: ModalShellProps) {
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
            "panel-float fixed z-50 flex flex-col overflow-hidden outline-none",
            // Desktop: centrado, altura limitada — o corpo rola por dentro.
            "left-1/2 top-1/2 max-h-[min(46rem,88dvh)] w-[min(34rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2",
            "transition-[opacity,transform] duration-200",
            "data-[ending-style]:scale-[0.97] data-[ending-style]:opacity-0",
            "data-[starting-style]:scale-[0.97] data-[starting-style]:opacity-0",
            // Mobile: folha de baixo, encostada nas laterais.
            "max-md:inset-x-0 max-md:top-auto max-md:bottom-0 max-md:w-full max-md:max-w-none",
            "max-md:max-h-[88dvh] max-md:translate-x-0 max-md:translate-y-0",
            "max-md:rounded-b-none max-md:rounded-t-3xl",
            "max-md:data-[ending-style]:translate-y-full max-md:data-[ending-style]:scale-100",
            "max-md:data-[starting-style]:translate-y-full max-md:data-[starting-style]:scale-100",
            className,
          )}
        >
          <header className="flex shrink-0 items-start gap-3 border-b border-border/60 px-5 py-4">
            <div className="min-w-0 flex-1">
              <Dialog.Title className="font-display truncate text-lg leading-tight">{title}</Dialog.Title>
              {description ? (
                <Dialog.Description className="mt-0.5 text-sm text-muted-foreground">
                  {description}
                </Dialog.Description>
              ) : null}
            </div>
            {/* Fechar precisa estar DENTRO do popup: com foco preso, é a única
                saída que o leitor de tela de toque alcança. */}
            <Dialog.Close
              render={
                <Button variant="ghost" size="iconSm" aria-label="Fechar">
                  <X aria-hidden="true" />
                </Button>
              }
            />
          </header>

          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 py-4">{children}</div>

          {footer ? (
            <footer className="flex shrink-0 items-center justify-end gap-2 border-t border-border/60 px-5 py-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))] md:pb-3">
              {footer}
            </footer>
          ) : null}
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
