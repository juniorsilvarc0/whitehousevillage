"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { Menu } from "@base-ui/react/menu";
import { ChevronDown, LogOut, ShieldCheck } from "lucide-react";

import { Avatar } from "@/components/ui/avatar";
import { cn } from "@/lib/utils";

export function UserMenu({ nome, email, papel }: { nome: string; email: string; papel: string }) {
  const router = useRouter();
  const [saindo, setSaindo] = React.useState(false);

  async function sair() {
    setSaindo(true);
    // A Route Handler derruba os cookies mesmo se a API não responder — não
    // existe caminho em que o botão "Sair" deixe a sessão de pé no navegador.
    await fetch("/api/auth/logout", { method: "POST" }).catch(() => undefined);
    router.replace("/login");
    router.refresh();
  }

  return (
    <Menu.Root>
      <Menu.Trigger
        className={cn(
          "flex h-10 items-center gap-2 rounded-full pl-1 pr-2 text-white/90 outline-none transition-colors",
          "hover:bg-white/15 focus-visible:ring-2 focus-visible:ring-white/60",
        )}
        aria-label={`Conta de ${nome}`}
      >
        <Avatar nome={nome} className="size-8 ring-white/30" />
        <span className="hidden max-w-32 truncate text-sm sm:inline">{nome.split(" ")[0]}</span>
        <ChevronDown className="size-3.5 opacity-70" aria-hidden="true" />
      </Menu.Trigger>

      <Menu.Portal>
        <Menu.Positioner side="bottom" align="end" sideOffset={10} className="z-50">
          <Menu.Popup
            className={cn(
              "panel-float w-60 overflow-hidden p-1.5 outline-none",
              "transition-[opacity,transform] duration-150",
              "data-[ending-style]:scale-[0.98] data-[ending-style]:opacity-0",
              "data-[starting-style]:scale-[0.98] data-[starting-style]:opacity-0",
            )}
          >
            <div className="px-2.5 py-2">
              <p className="truncate text-sm font-medium">{nome}</p>
              <p className="truncate text-xs text-muted-foreground">{email}</p>
              <p className="mt-2 inline-flex items-center gap-1.5 rounded-full bg-accent px-2 py-0.5 text-[0.68rem] font-medium text-accent-foreground">
                <ShieldCheck className="size-3" aria-hidden="true" />
                {papel}
              </p>
            </div>

            <div className="my-1 h-px bg-border/70" />

            <Menu.Item
              onClick={sair}
              disabled={saindo}
              className={cn(
                "flex cursor-default items-center gap-2 rounded-lg px-2.5 py-2 text-sm outline-none",
                "data-[highlighted]:bg-muted data-[disabled]:opacity-60",
              )}
            >
              <LogOut className="size-4" aria-hidden="true" />
              {saindo ? "Saindo…" : "Sair"}
            </Menu.Item>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  );
}
