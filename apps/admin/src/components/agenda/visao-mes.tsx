"use client";

import * as React from "react";
import Link from "next/link";

import { ChipDoEvento } from "@/components/agenda/cartao-do-evento";
import { DIAS_DA_SEMANA, dataLonga, numeroDoDia } from "@/lib/agenda/rotulos";
import { eventosDoDia, semanasDaJanela, type Janela } from "@/lib/agenda/visoes";
import type { EventoDaAgenda } from "@/lib/agenda/tipos";
import { cn } from "@/lib/utils";

/**
 * O mês — a grade inteira, de segunda a domingo, até 6 semanas.
 *
 * No computador cada dia mostra até três eventos e "+N" para o resto; no
 * celular, onde a célula tem 48px de largura, o dia vira pontos coloridos pelo
 * tipo e o toque leva à visão do dia — é lá que se lê e se age.
 *
 * O evento aparece em todo dia que toca (a manutenção de três dias aparece nos
 * três), e o dia é o `date` da API somado ao fim lido no relógio da casa —
 * nenhum agrupamento pelo fuso do navegador.
 */

const VISIVEIS = 3;

export function VisaoMes({
  janela,
  mes,
  hoje,
  eventos,
  aoAbrir,
  linkDoDia,
}: {
  janela: Janela;
  /** Qualquer dia do mês mostrado — os dias de fora aparecem apagados. */
  mes: string;
  hoje: string;
  eventos: readonly EventoDaAgenda[];
  aoAbrir: (evento: EventoDaAgenda) => void;
  linkDoDia: (dia: string) => string;
}) {
  const semanas = React.useMemo(() => semanasDaJanela(janela), [janela]);
  const doMes = mes.slice(0, 7);

  return (
    <div className="agenda overflow-hidden rounded-xl border border-border/60 bg-card/60" aria-label="Mês">
      <div className="grid grid-cols-7 border-b border-border/60" aria-hidden="true">
        {DIAS_DA_SEMANA.map((d) => (
          <div key={d.iso} className="py-1.5 text-center text-[0.6875rem] uppercase tracking-wide text-muted-foreground">
            {d.curto}
          </div>
        ))}
      </div>
      {semanas.map((semana) => (
        <div key={semana[0]} className="grid grid-cols-7 border-b border-border/40 last:border-b-0">
          {semana.map((dia) => {
            const doDia = eventosDoDia(eventos, dia);
            const ehHoje = dia === hoje;
            const fora = dia.slice(0, 7) !== doMes;
            const sobra = doDia.length - VISIVEIS;
            return (
              <div
                key={dia}
                aria-current={ehHoje ? "date" : undefined}
                data-dia={dia}
                className={cn(
                  "relative flex min-h-16 min-w-0 flex-col gap-1 border-l border-border/40 p-1 first:border-l-0 md:min-h-28",
                  fora && "bg-muted/40",
                  ehHoje && "bg-primary/7",
                )}
              >
                <Link
                  href={linkDoDia(dia)}
                  aria-label={`${dataLonga(dia)}${ehHoje ? " (hoje)" : ""}: ${doDia.length === 0 ? "nada marcado" : `${doDia.length} ${doDia.length === 1 ? "evento" : "eventos"}`}`}
                  className={cn(
                    "font-display grid size-6 place-items-center self-start rounded-full text-xs tabular-nums outline-none focus-visible:ring-2 focus-visible:ring-ring",
                    // No celular, o toque em qualquer ponto do dia abre o dia.
                    "max-md:after:absolute max-md:after:inset-0 max-md:after:content-['']",
                    ehHoje ? "bg-brand-gradient text-white" : fora ? "text-muted-foreground" : "hover:bg-muted",
                  )}
                >
                  {numeroDoDia(dia)}
                </Link>

                {/* Celular: pontos pelo tipo. */}
                {doDia.length > 0 ? (
                  <span className="flex flex-wrap gap-0.5 md:hidden" aria-hidden="true">
                    {doDia.slice(0, 6).map((e) => (
                      <span key={e.id} className="agenda-ponto size-1.5 rounded-full" data-tipo={e.type} />
                    ))}
                  </span>
                ) : null}

                {/* Computador: até três chips e o resto contado. */}
                <ul className="hidden min-w-0 flex-col gap-0.5 md:flex" aria-label={`Eventos de ${dataLonga(dia)}`}>
                  {doDia.slice(0, VISIVEIS).map((e) => (
                    <li key={e.id} className="min-w-0">
                      <ChipDoEvento evento={e} aoAbrir={aoAbrir} mostrarHora />
                    </li>
                  ))}
                  {sobra > 0 ? (
                    <li>
                      <Link href={linkDoDia(dia)} className="px-1.5 text-[0.6875rem] font-medium text-primary underline-offset-4 hover:underline">
                        +{sobra} {sobra === 1 ? "outro" : "outros"}
                      </Link>
                    </li>
                  ) : null}
                </ul>
              </div>
            );
          })}
        </div>
      ))}
    </div>
  );
}
