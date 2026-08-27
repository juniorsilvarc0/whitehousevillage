"use client";

import * as React from "react";

import type { TipoDeData } from "@/lib/api/comercial";
import type { Faixa } from "@/lib/mapa/faixas";
import { expiracaoDeHold } from "@/lib/mapa/expiracao";
import { ROTULO_DO_STATUS, seloDaFaixa, textoDaFaixa } from "@/lib/mapa/rotulos";
import { useAgora } from "@/lib/tempo-real/relogio";
import { cn } from "@/lib/utils";

/**
 * A célula do mapa, separada em **duas** camadas — e a separação é o desenho
 * inteiro da tela.
 *
 * - `ColunaDeFundo` é o dia: cor pelo tipo de data (a precedência do tarifário),
 *   uma por coluna visível. É virtualizada.
 * - `FaixaDaGrade` é a ocupação: uma barra por bloco de calendário, atravessando
 *   quantas colunas a estadia durar. **Não** é uma por dia.
 *
 * Fundir as duas — uma célula por dia, colorida pelo status — foi a primeira
 * versão e é o caminho para 810 nós com sombra, borda e texto repetidos, sem
 * conseguir arredondar só as pontas da estadia nem escrever o nome do hóspede
 * uma vez só. Separadas, o fundo é barato e a ocupação é rara.
 */

export function ColunaDeFundo({
  indice,
  tipo,
  hoje,
  apontada,
}: {
  indice: number;
  tipo: TipoDeData;
  hoje: boolean;
  apontada: boolean;
}) {
  return (
    <div
      className="mapa-coluna"
      data-tipo={tipo}
      data-hoje={hoje ? "1" : "0"}
      data-apontada={apontada ? "1" : "0"}
      style={{ left: `calc(var(--mapa-dia) * ${indice})` }}
      aria-hidden="true"
    />
  );
}

export function FaixaDaGrade({
  faixa,
  /** `hold_expires_at` da reserva desta faixa, quando ela é uma pré-reserva. */
  expiraEm,
  compacta,
}: {
  faixa: Faixa;
  expiraEm?: string | null;
  /** Faixa de um ou dois dias: só o selo cabe, o texto seria três letras e
   *  reticências. */
  compacta?: boolean;
}) {
  const agora = useAgora();
  const expiracao = faixa.status === "hold" ? expiracaoDeHold(expiraEm, agora) : null;
  const selo = seloDaFaixa(faixa.status);
  const dias = faixa.fim - faixa.inicio;
  const apertada = compacta ?? dias <= 2;

  return (
    <div
      className={cn("mapa-faixa", `mapa-faixa--${faixa.status}`)}
      data-toca-inicio={faixa.tocaInicio ? "1" : "0"}
      data-toca-fim={faixa.tocaFim ? "1" : "0"}
      data-urgencia={expiracao?.urgencia ?? ""}
      data-status={faixa.status}
      style={{
        left: `calc(var(--mapa-dia) * ${faixa.inicio} + 2px)`,
        width: `calc(var(--mapa-dia) * ${dias} - 4px)`,
      }}
      // A grade é um desenho; quem lê por leitor de tela recebe o resumo textual
      // da linha, montado uma vez em `resumoDaLinha`. Repetir cada faixa aqui
      // faria a navegação por leitor atravessar 60 rótulos para descobrir o que
      // uma frase diz.
      aria-hidden="true"
    >
      {selo ? <span className="mapa-selo">{selo}</span> : null}
      {apertada ? null : (
        <span className="min-w-0 flex-1 truncate font-medium">
          {textoDaFaixa(faixa.status, faixa.reservationCode, faixa.guestName)}
        </span>
      )}
      {expiracao ? (
        <span
          className={cn(
            "shrink-0 rounded-full px-1.5 py-px text-[0.5625rem] font-semibold tabular-nums",
            "bg-[color-mix(in_oklab,currentColor_14%,transparent)]",
            expiracao.urgencia === "critica" && "mapa-pulso",
          )}
        >
          {expiracao.curto}
        </span>
      ) : null}
    </div>
  );
}

/**
 * O resumo textual de uma linha, para leitor de tela.
 *
 * A grade não tem semântica de tabela — ela é posicionada por `calc()` sobre
 * índices, e uma `<table>` com 366 colunas seria pior para todo mundo. O que
 * substitui a semântica é esta frase: "COB-01: 3 ocupações na janela; 12 a 15
 * de janeiro, confirmada, WH-2026-0001".
 */
export function resumoDaLinha(codigo: string, faixas: readonly Faixa[], dias: readonly string[]): string {
  if (faixas.length === 0) return `${codigo}: livre em toda a faixa mostrada.`;
  const trechos = faixas.map((faixa) => {
    const de = dias[faixa.inicio] ?? "";
    const ate = dias[Math.max(faixa.inicio, faixa.fim - 1)] ?? "";
    const quem = textoDaFaixa(faixa.status, faixa.reservationCode, faixa.guestName);
    return `${de} a ${ate}: ${ROTULO_DO_STATUS[faixa.status]}${quem === ROTULO_DO_STATUS[faixa.status] ? "" : `, ${quem}`}`;
  });
  return `${codigo}: ${trechos.join("; ")}.`;
}
