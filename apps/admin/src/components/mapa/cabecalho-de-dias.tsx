"use client";

import * as React from "react";

import type { TipoDeData } from "@/lib/api/comercial";
import type { DataISO } from "@/lib/datas";
import { cn } from "@/lib/utils";

/**
 * O cabeçalho de dias: mês, número e inicial do dia da semana.
 *
 * O mês aparece só quando **muda** — 90 colunas repetindo "dez" é ruído que
 * some da leitura. A troca de mês também ganha um filete à esquerda, que é o
 * que permite achar "janeiro" rolando rápido sem ler nada.
 */

const DIA_DA_SEMANA = ["D", "S", "T", "Q", "Q", "S", "S"] as const;

function partes(data: DataISO): { dia: string; semana: string; mes: string; primeiroDoMes: boolean } {
  const d = new Date(`${data}T00:00:00Z`);
  const mes = new Intl.DateTimeFormat("pt-BR", { timeZone: "UTC", month: "short" })
    .format(d)
    .replace(".", "");
  return {
    dia: String(d.getUTCDate()).padStart(2, "0"),
    semana: DIA_DA_SEMANA[d.getUTCDay()] ?? "",
    mes,
    primeiroDoMes: d.getUTCDate() === 1,
  };
}

export function CabecalhoDeDias({
  dias,
  indices,
  tipos,
  hoje,
  apontado,
}: {
  dias: readonly DataISO[];
  indices: readonly number[];
  tipos: readonly TipoDeData[];
  hoje: DataISO;
  apontado: number | null;
}) {
  return (
    <div className="mapa-pista" style={{ height: "3.25rem" }}>
      {indices.map((i) => {
        const data = dias[i];
        if (!data) return null;
        const { dia, semana, mes, primeiroDoMes } = partes(data);
        const ehHoje = data === hoje;
        return (
          <div
            key={data}
            className={cn(
              "mapa-coluna flex flex-col items-center justify-center gap-px",
              primeiroDoMes && "border-l border-l-border",
            )}
            data-tipo={tipos[i] ?? "normal"}
            data-hoje={ehHoje ? "1" : "0"}
            data-apontada={apontado === i ? "1" : "0"}
            style={{ left: `calc(var(--mapa-dia) * ${i})`, height: "3.25rem" }}
          >
            <span className="text-[0.5625rem] uppercase leading-none text-muted-foreground">
              {i === indices[0] || primeiroDoMes ? mes : " "}
            </span>
            <span
              className={cn(
                "font-display text-xs leading-none tabular-nums",
                ehHoje ? "font-semibold text-primary" : "text-foreground",
              )}
            >
              {dia}
            </span>
            <span className="text-[0.5625rem] leading-none text-muted-foreground">{semana}</span>
          </div>
        );
      })}
    </div>
  );
}
