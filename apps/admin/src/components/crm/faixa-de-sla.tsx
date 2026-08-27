import * as React from "react";
import { Clock, TimerReset, TriangleAlert } from "lucide-react";

import { CLASSE_DO_TOM, faixaDoCard, lerSLA, type TomDeSLA } from "@/lib/crm/sla";
import type { CardDaOportunidade, FaixaDeSLA } from "@/lib/crm/tipos";
import { formatarInstante } from "@/lib/datas";
import { cn } from "@/lib/utils";

/**
 * A faixa de SLA da tela da oportunidade: quando entrou, quanto falta, e o
 * estado.
 *
 * Os três dados são **derivados no servidor** (`days_left`, `breached`). Este
 * componente escolhe a palavra e a cor — e nada mais. É a diferença entre uma
 * tela que informa o prazo e uma que inventa um prazo por fuso de navegador.
 */

const ICONE: Record<TomDeSLA, React.ComponentType<{ className?: string }>> = {
  sem_sla: TimerReset,
  estourado: TriangleAlert,
  vence_hoje: Clock,
  no_prazo: Clock,
};

export function FaixaDeSla({ faixa, className }: { faixa: FaixaDeSLA; className?: string }) {
  const leitura = lerSLA(faixa);
  const Icone = ICONE[leitura.tom];

  return (
    <div
      data-tom={leitura.tom}
      // `role="status"`: a faixa muda sozinha quando o card muda de etapa, e a
      // mudança precisa ser anunciada sem roubar o foco de quem está digitando.
      role="status"
      className={cn(
        "flex flex-wrap items-center gap-x-3 gap-y-1 rounded-xl border px-3.5 py-2.5",
        CLASSE_DO_TOM[leitura.tom],
        className,
      )}
    >
      <span className="flex items-center gap-2 text-sm font-medium">
        <Icone className="size-4 shrink-0" aria-hidden="true" />
        {leitura.rotulo}
      </span>
      <span className="min-w-0 text-xs text-muted-foreground">
        {leitura.detalhe} Entrou em {formatarInstante(faixa.entered_stage_at)}
        {faixa.due_at ? ` · vence em ${formatarInstante(faixa.due_at)}` : ""}.
      </span>
    </div>
  );
}

/**
 * A faixa do card do kanban — binária, e ausente quando não há o que dizer.
 *
 * Card no prazo **não** ganha faixa verde. Uma marca em todo card não distingue
 * nada: o que precisa saltar num quadro de 80 cards é o punhado que estourou.
 */
export function FaixaDeSlaDoCard({
  card,
  className,
}: {
  card: Pick<CardDaOportunidade, "sla_breached" | "sla_due_at">;
  className?: string;
}) {
  const leitura = faixaDoCard(card);
  if (!leitura) return null;

  return (
    <p
      data-tom={leitura.tom}
      className={cn(
        "flex items-center gap-1.5 rounded-md px-2 py-1 text-[0.7rem] font-medium",
        "bg-destructive/12 text-destructive",
        className,
      )}
    >
      <TriangleAlert className="size-3 shrink-0" aria-hidden="true" />
      {leitura.rotulo}
    </p>
  );
}
