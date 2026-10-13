import Link from "next/link";
import { ChevronRight } from "lucide-react";

import { LinhaDoBloqueio, SeloDePrioridade, SeloDeStatus } from "@/components/manutencao/selos";
import { formatarInstante } from "@/lib/datas";
import { caminhoDaOrdem } from "@/lib/manutencao/mensagens";
import type { OrdemDeManutencao } from "@/lib/manutencao/tipos";
import { cn } from "@/lib/utils";

/**
 * A lista de trabalho — **na ordem em que a API devolveu**. A escada
 * (abertas primeiro, `urgente → baixa`, a mais antiga no topo; depois as
 * encerradas, da mais recente) é `sort=urgencia`, decidida no SQL com
 * `maintenance.ByUrgency()`. Reordenar aqui faria a página 2 repetir ou perder
 * linha, e criaria uma segunda escada que diverge da primeira.
 *
 * Cada linha é um cartão-link inteiro: no celular o alvo de toque é a ordem
 * toda, não um ícone de 16px no canto.
 */
export function ListaDeOrdens({ ordens }: { ordens: readonly OrdemDeManutencao[] }) {
  return (
    <ul className="flex flex-col gap-2" aria-label="Ordens de manutenção">
      {ordens.map((o) => (
        <li key={o.id}>
          <CartaoDaOrdem ordem={o} />
        </li>
      ))}
    </ul>
  );
}

export function CartaoDaOrdem({ ordem: o }: { ordem: OrdemDeManutencao }) {
  const encerrada = o.status === "concluida" || o.status === "cancelada";
  const onde = [o.room_name, o.item_name].filter(Boolean).join(" · ");
  return (
    <Link
      href={caminhoDaOrdem(o.id)}
      className={cn(
        "flex items-center gap-3 rounded-xl border bg-card/70 px-4 py-3 outline-none transition-colors hover:bg-muted/40",
        "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-card",
        o.priority === "urgente" && !encerrada ? "border-destructive/40" : "border-border/60",
        encerrada && "opacity-85",
      )}
    >
      <article className="min-w-0 flex-1" aria-label={o.title}>
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-sm">{o.unit_code}</span>
          <SeloDePrioridade prioridade={o.priority} />
          <SeloDeStatus status={o.status} />
        </div>
        <p className="mt-1 text-sm font-medium leading-snug">{o.title}</p>
        {onde ? <p className="mt-0.5 truncate text-xs text-muted-foreground">{onde}</p> : null}
        {o.block ? <LinhaDoBloqueio bloqueio={o.block} className="mt-1" /> : null}
        <p className="mt-1 text-xs text-muted-foreground">
          {encerrada && o.closed_at
            ? `${o.status === "cancelada" ? "Cancelada" : "Concluída"} em ${formatarInstante(o.closed_at)}`
            : `Aberta em ${formatarInstante(o.opened_at)}${o.opened_by_name ? ` por ${o.opened_by_name}` : ""}`}
        </p>
      </article>
      <ChevronRight aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
    </Link>
  );
}
