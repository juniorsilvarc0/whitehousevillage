"use client";

import * as React from "react";

import { TIPOS_DE_DATA } from "@/lib/comercial/tipos-de-data";
import { EXPLICACAO_DO_STATUS, ROTULO_DO_STATUS } from "@/lib/mapa/rotulos";
import type { StatusDaCelulaSintetica } from "@/lib/mapa/tipos";
import { cn } from "@/lib/utils";

/**
 * A legenda. Existe porque o mapa tem **duas** codificações simultâneas — o
 * fundo diz o tipo de data (e portanto a tarifa), a barra diz a ocupação — e
 * ninguém adivinha as duas.
 *
 * Ela é enxuta de propósito: fica sempre visível, no rodapé do painel, em vez
 * de escondida atrás de um "?" que ninguém abre duas vezes.
 */
const ESTADOS: readonly StatusDaCelulaSintetica[] = [
  "confirmed",
  "hold",
  "checked_out",
  "maintenance",
  "owner_hold",
  "ota",
  "parcial",
];

export function LegendaDoMapa({ className }: { className?: string }) {
  return (
    <div className={cn("flex flex-wrap items-center gap-x-4 gap-y-2 text-[0.6875rem]", className)}>
      <span className="font-medium uppercase tracking-wide text-muted-foreground">Ocupação</span>
      {ESTADOS.map((estado) => (
        <span key={estado} className="flex items-center gap-1.5 text-muted-foreground" title={EXPLICACAO_DO_STATUS[estado]}>
          <span
            aria-hidden="true"
            className={cn("mapa-faixa", `mapa-faixa--${estado}`)}
            style={{ position: "static", width: "1.5rem", height: "0.75rem", padding: 0, flexShrink: 0 }}
          />
          {ROTULO_DO_STATUS[estado]}
        </span>
      ))}

      <span className="h-3 w-px bg-border" aria-hidden="true" />
      <span className="font-medium uppercase tracking-wide text-muted-foreground">Tipo de data</span>
      {TIPOS_DE_DATA.map((tipo) => (
        <span key={tipo.code} className="flex items-center gap-1.5 text-muted-foreground" title={tipo.regra}>
          <span
            aria-hidden="true"
            className="mapa-coluna"
            data-tipo={tipo.code}
            style={{
              position: "static",
              width: "1rem",
              height: "0.75rem",
              borderRadius: "0.25rem",
              border: "1px solid var(--mapa-grade)",
              flexShrink: 0,
            }}
          />
          {tipo.label}
        </span>
      ))}
    </div>
  );
}
