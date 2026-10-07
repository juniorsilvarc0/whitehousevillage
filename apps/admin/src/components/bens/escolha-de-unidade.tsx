import Link from "next/link";
import { ClipboardCheck, DoorOpen } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { caminhoDaConferencia } from "@/lib/bens/mensagens";
import type { UnidadeDoInventario } from "@/lib/bens/tipos";
import { formatarInstante } from "@/lib/datas";
import { cn } from "@/lib/utils";

/**
 * A escolha da unidade, com o que importa saber antes de entrar nela: quantos
 * ambientes e bens já tem, se há conferência em andamento e quando foi a
 * última.
 *
 * Vem de `GET /inventory/units`, e por isso **aparece a unidade sem nenhum
 * ambiente** — que é justamente a que precisa de alguém criando o primeiro
 * cômodo. Ela ganha destaque e o convite, em vez de sumir da lista.
 *
 * A conferência aberta é um link à parte (e não o cartão inteiro): quem chega
 * para continuar a contagem vai direto para ela, sem passar pela unidade.
 */
export function EscolhaDeUnidade({ unidades }: { unidades: readonly UnidadeDoInventario[] }) {
  return (
    <section aria-label="Escolha a unidade" className="flex flex-col gap-3">
      <p className="text-sm text-muted-foreground">Escolha o apartamento:</p>
      <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
        {unidades.map((u) => {
          const semPlanta = u.rooms === 0;
          return (
            <li
              key={u.id}
              aria-label={`${u.code} — ${u.name}`}
              className={cn(
                "flex flex-col gap-2 rounded-xl border bg-muted/20 px-4 py-3",
                semPlanta ? "border-dashed border-foreground/25" : "border-border/60",
              )}
            >
              <Link
                href={`/app/inventario?unidade=${u.id}`}
                className="rounded-lg outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-card"
              >
                <span className="font-mono text-sm">{u.code}</span>
                <span className="block truncate text-xs text-muted-foreground">{u.name}</span>
              </Link>

              {semPlanta ? (
                <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
                  <DoorOpen className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
                  <span>
                    Ainda sem ambientes.{" "}
                    <Link href={`/app/inventario?unidade=${u.id}`} className="font-medium text-primary underline-offset-4 hover:underline">
                      Crie o primeiro cômodo ou copie de outra unidade
                    </Link>
                    .
                  </span>
                </p>
              ) : (
                <p className="text-xs text-muted-foreground">
                  <span className="tabular-nums">{u.rooms}</span> {u.rooms === 1 ? "ambiente" : "ambientes"} ·{" "}
                  <span className="tabular-nums">{u.items}</span> {u.items === 1 ? "bem" : "bens"}
                  {u.last_closed_at ? <> · conferida em {formatarInstante(u.last_closed_at)}</> : <> · nunca conferida</>}
                </p>
              )}

              {u.open_count_id ? (
                <Link
                  href={caminhoDaConferencia(u.open_count_id)}
                  className="inline-flex w-fit items-center gap-1.5 rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <Badge variant="brand">
                    <ClipboardCheck aria-hidden="true" />
                    Conferência em andamento — continuar
                  </Badge>
                </Link>
              ) : null}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
