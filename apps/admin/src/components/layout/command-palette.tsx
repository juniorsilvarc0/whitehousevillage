"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { Popover } from "@base-ui/react/popover";
import { CornerDownLeft, Search } from "lucide-react";

import { Button } from "@/components/ui/button";
import { itensPorHref } from "@/components/layout/nav-utils";
import { cn } from "@/lib/utils";

/**
 * Busca da barra — ⌘K / Ctrl+K.
 *
 * Por enquanto navega entre destinos. Reserva, contato e oportunidade entram
 * quando os endpoints existirem: a casca do teclado (atalho, foco, Enter,
 * setas) é o que custa caro e já fica pronta aqui, não a lista de resultados.
 */
export function CommandPalette({ hrefs }: { hrefs: readonly string[] }) {
  const router = useRouter();
  const [open, setOpen] = React.useState(false);
  const [busca, setBusca] = React.useState("");
  const [selecionado, setSelecionado] = React.useState(0);
  const campo = React.useRef<HTMLInputElement>(null);

  const itens = React.useMemo(() => itensPorHref(hrefs), [hrefs]);
  const resultados = React.useMemo(() => {
    const termo = busca.trim().toLowerCase();
    if (!termo) return itens;
    return itens.filter((item) => item.title.toLowerCase().includes(termo));
  }, [itens, busca]);

  React.useEffect(() => {
    function atalho(evento: KeyboardEvent) {
      if ((evento.metaKey || evento.ctrlKey) && evento.key.toLowerCase() === "k") {
        // O navegador reserva ⌘K para a barra de endereço em alguns casos;
        // sem o preventDefault o atalho abriria as duas coisas.
        evento.preventDefault();
        setOpen((atual) => !atual);
      }
    }
    window.addEventListener("keydown", atalho);
    return () => window.removeEventListener("keydown", atalho);
  }, []);

  function abrir(aberto: boolean) {
    setOpen(aberto);
    if (!aberto) {
      setBusca("");
      setSelecionado(0);
    }
  }

  function navegar(href: string) {
    abrir(false);
    router.push(href);
  }

  function teclado(evento: React.KeyboardEvent<HTMLInputElement>) {
    if (resultados.length === 0) return;
    if (evento.key === "ArrowDown") {
      evento.preventDefault();
      setSelecionado((i) => (i + 1) % resultados.length);
    } else if (evento.key === "ArrowUp") {
      evento.preventDefault();
      setSelecionado((i) => (i - 1 + resultados.length) % resultados.length);
    } else if (evento.key === "Enter") {
      evento.preventDefault();
      const alvo = resultados[Math.min(selecionado, resultados.length - 1)];
      if (alvo) navegar(alvo.href);
    }
  }

  return (
    <Popover.Root open={open} onOpenChange={abrir}>
      <Popover.Trigger
        render={
          <Button variant="onBrand" size="sm" aria-label="Buscar (Ctrl+K)" className="gap-2">
            <Search aria-hidden="true" />
            <span className="hidden lg:inline">Buscar</span>
            <kbd className="hidden rounded-md bg-white/15 px-1.5 py-0.5 font-mono text-[0.65rem] text-white/80 lg:inline">
              ⌘K
            </kbd>
          </Button>
        }
      />
      <Popover.Portal>
        <Popover.Positioner side="bottom" align="end" sideOffset={10} className="z-50">
          <Popover.Popup
            initialFocus={campo}
            className={cn(
              "panel-float w-[min(24rem,calc(100vw-1.5rem))] overflow-hidden p-0 outline-none",
              "transition-[opacity,transform] duration-150",
              "data-[ending-style]:scale-[0.98] data-[ending-style]:opacity-0",
              "data-[starting-style]:scale-[0.98] data-[starting-style]:opacity-0",
            )}
          >
            <div className="flex items-center gap-2 border-b border-border/60 px-3.5">
              <Search className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
              <input
                ref={campo}
                value={busca}
                onChange={(e) => {
                  setBusca(e.target.value);
                  setSelecionado(0);
                }}
                onKeyDown={teclado}
                placeholder="Ir para…"
                aria-label="Buscar destino"
                className="h-11 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground/70"
              />
            </div>

            <ul className="max-h-72 overflow-y-auto p-1.5">
              {resultados.length === 0 ? (
                <li className="px-3 py-6 text-center text-sm text-muted-foreground">
                  Nada encontrado para “{busca}”.
                </li>
              ) : (
                resultados.map((item, indice) => (
                  <li key={item.href}>
                    <button
                      type="button"
                      onClick={() => navegar(item.href)}
                      onMouseEnter={() => setSelecionado(indice)}
                      className={cn(
                        "flex w-full items-center gap-3 rounded-lg px-2.5 py-2 text-left text-sm transition-colors",
                        indice === selecionado ? "bg-muted text-foreground" : "text-muted-foreground",
                      )}
                    >
                      <item.icon className="size-4 shrink-0" />
                      <span className="flex-1 truncate text-foreground">{item.title}</span>
                      <span className="text-[0.68rem] uppercase tracking-wider text-muted-foreground">
                        {item.group}
                      </span>
                      {indice === selecionado ? (
                        <CornerDownLeft className="size-3.5 text-muted-foreground" aria-hidden="true" />
                      ) : null}
                    </button>
                  </li>
                ))
              )}
            </ul>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
}
