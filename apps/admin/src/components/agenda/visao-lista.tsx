"use client";

import * as React from "react";

import { CartaoDaLista } from "@/components/agenda/cartao-do-evento";
import { dataLonga, diaEMes, semanaCurta } from "@/lib/agenda/rotulos";
import { agruparPorDia, type Janela } from "@/lib/agenda/visoes";
import type { EventoDaAgenda } from "@/lib/agenda/tipos";
import { cn } from "@/lib/utils";

/**
 * A lista — a visão de quem trabalha de pé, no celular: um grupo por dia da
 * janela, **agrupado pelo `date` da API** (o dia da casa em que o evento
 * começa), na ordem em que a API entregou.
 *
 * O dia sem nada aparece também, numa linha: "sábado livre" é informação para
 * quem escala a limpeza. O cabeçalho do dia gruda no topo ao rolar, abaixo da
 * barra da casca (`--app-chrome-top`, o token — nunca um `calc()` à mão).
 */
export function VisaoLista({
  janela,
  hoje,
  eventos,
  aoAbrir,
  podeConcluir,
  aoConcluir,
}: {
  janela: Janela;
  hoje: string;
  eventos: readonly EventoDaAgenda[];
  aoAbrir: (evento: EventoDaAgenda) => void;
  podeConcluir: boolean;
  aoConcluir: (evento: EventoDaAgenda) => Promise<void>;
}) {
  const grupos = React.useMemo(() => agruparPorDia(eventos, janela), [eventos, janela]);

  return (
    <ol className="agenda flex flex-col gap-4" aria-label={`Agenda de ${diaEMes(janela.from)} em diante`}>
      {grupos.map((grupo) => {
        const ehHoje = grupo.data === hoje;
        return (
          <li key={grupo.data}>
            <section aria-label={`${dataLonga(grupo.data)}${ehHoje ? " (hoje)" : ""}`} data-dia={grupo.data}>
              <h3
                aria-current={ehHoje ? "date" : undefined}
                className={cn(
                  "sticky top-[var(--app-chrome-top)] z-[2] -mx-1 mb-2 flex items-baseline gap-2 rounded-lg bg-card/95 px-1 py-1 text-sm backdrop-blur",
                )}
              >
                {ehHoje ? (
                  <span className="rounded-full bg-brand-gradient px-2 py-0.5 text-[0.6875rem] font-medium text-white">Hoje</span>
                ) : null}
                <span className="font-display capitalize">{semanaCurta(grupo.data)}</span>
                <span className="tabular-nums text-muted-foreground">{diaEMes(grupo.data)}</span>
                {grupo.eventos.length > 0 ? (
                  <span className="ml-auto text-xs text-muted-foreground tabular-nums">
                    {grupo.eventos.length} {grupo.eventos.length === 1 ? "evento" : "eventos"}
                  </span>
                ) : null}
              </h3>
              {grupo.eventos.length === 0 ? (
                <p className="rounded-xl border border-dashed border-border/60 px-3 py-2.5 text-xs text-muted-foreground">
                  Nada marcado.
                </p>
              ) : (
                <ul className="flex flex-col gap-2">
                  {grupo.eventos.map((e) => (
                    <li key={e.id}>
                      <CartaoDaLista
                        evento={e}
                        diaDoGrupo={grupo.data}
                        aoAbrir={aoAbrir}
                        podeConcluir={podeConcluir}
                        aoConcluir={aoConcluir}
                      />
                    </li>
                  ))}
                </ul>
              )}
            </section>
          </li>
        );
      })}
    </ol>
  );
}
