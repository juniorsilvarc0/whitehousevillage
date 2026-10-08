import Link from "next/link";

import { cn } from "@/lib/utils";

/**
 * As quatro telas do inventário de bens, como abas de navegação.
 *
 * São **links**, não abas de estado (`components/crm/abas.tsx`): cada uma é uma
 * rota com o próprio recorte na query string, e o botão de voltar do navegador
 * tem de funcionar entre elas.
 */
const ABAS = [
  { id: "unidades", href: "/app/inventario", rotulo: "Por unidade" },
  { id: "bens", href: "/app/inventario/bens", rotulo: "Catálogo" },
  { id: "conferencias", href: "/app/inventario/conferencias", rotulo: "Conferências" },
  { id: "avarias", href: "/app/inventario/avarias", rotulo: "Avarias" },
] as const;

export type AbaDoInventario = (typeof ABAS)[number]["id"];

export function AbasDoInventario({ ativa }: { ativa: AbaDoInventario }) {
  return (
    <nav aria-label="Seções do inventário" className="-mx-1 overflow-x-auto">
      <ul className="flex min-w-max gap-1 px-1">
        {ABAS.map((aba) => {
          const atual = aba.id === ativa;
          return (
            <li key={aba.id}>
              <Link
                href={aba.href}
                aria-current={atual ? "page" : undefined}
                className={cn(
                  "inline-flex h-9 items-center rounded-full px-4 text-sm outline-none transition-colors",
                  "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-card",
                  atual ? "bg-brand-gradient text-white" : "text-muted-foreground hover:bg-muted hover:text-foreground",
                )}
              >
                {aba.rotulo}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
