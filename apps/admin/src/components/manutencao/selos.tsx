import { CalendarOff, CalendarX2, CircleDot, Flame } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { ROTULO_DA_PRIORIDADE, ROTULO_DO_STATUS, faseCurta } from "@/lib/manutencao/rotulos";
import type { BloqueioDaOrdem, PrioridadeDaOrdem, StatusDaOrdem } from "@/lib/manutencao/tipos";
import { cn } from "@/lib/utils";

/**
 * Os selos da ordem. Só tokens da casca: o urgente usa o vermelho de
 * `destructive`, o alto o âmbar de `alcada-atencao` — a mesma cor de "atenção"
 * do resto do painel —, e o resto fica neutro. Prioridade baixa não ganha cor:
 * cor demais é cor nenhuma.
 */
export function SeloDePrioridade({ prioridade }: { prioridade: PrioridadeDaOrdem }) {
  const rotulo = ROTULO_DA_PRIORIDADE[prioridade];
  if (prioridade === "urgente") {
    return (
      <Badge variant="destructive" aria-label={`Prioridade ${rotulo}`}>
        <Flame aria-hidden="true" />
        {rotulo}
      </Badge>
    );
  }
  if (prioridade === "alta") {
    return (
      <Badge className="bg-alcada-atencao/15 text-foreground" aria-label={`Prioridade ${rotulo}`}>
        <CircleDot aria-hidden="true" className="text-alcada-atencao" />
        {rotulo}
      </Badge>
    );
  }
  return (
    <Badge variant={prioridade === "normal" ? "neutral" : "outline"} aria-label={`Prioridade ${rotulo}`}>
      {rotulo}
    </Badge>
  );
}

export function SeloDeStatus({ status }: { status: StatusDaOrdem }) {
  const variante = status === "em_andamento" ? "brand" : status === "aberta" ? "accent" : status === "concluida" ? "neutral" : "outline";
  return <Badge variant={variante}>{ROTULO_DO_STATUS[status]}</Badge>;
}

/** A fase do bloqueio numa linha — a frase sai de `block.phase`, nunca de uma
 *  conta com a data de hoje. */
export function LinhaDoBloqueio({ bloqueio, className }: { bloqueio: BloqueioDaOrdem; className?: string }) {
  const Icone = bloqueio.phase === "liberado" || bloqueio.phase === "encerrado" ? CalendarOff : CalendarX2;
  const ativo = bloqueio.phase === "agendado" || bloqueio.phase === "em_curso";
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-xs", ativo ? "text-foreground" : "text-muted-foreground", className)}>
      <Icone aria-hidden="true" className={cn("size-3.5 shrink-0", ativo ? "text-destructive" : "text-muted-foreground")} />
      {faseCurta(bloqueio)}
    </span>
  );
}
