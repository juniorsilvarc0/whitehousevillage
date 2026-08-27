"use client";

import * as React from "react";

import type { Alcada } from "@/lib/api/comercial";
import { SEMAFORO, alcadaDe, faixasDaAlcada, type LimitesDeAlcada } from "@/lib/comercial/alcada";
import { formatarPct } from "@/lib/dinheiro";
import { cn } from "@/lib/utils";

/**
 * O semáforo de alçada de desconto.
 *
 * Três faixas, sempre as três à vista — e não só a atual. Quem está negociando
 * precisa saber **onde termina** o próprio poder de decisão antes de dizer um
 * número ao hóspede; um rótulo isolado ("exige aprovação") responde o que
 * aconteceu, e não quanto ainda dá para dar.
 *
 * Os limites vêm da política vigente, que é dado versionado — 5% e 10% são o
 * que a política diz hoje, não constantes deste arquivo.
 *
 * `alcada` sobrescreve o cálculo local: depois de um orçamento, quem manda é o
 * `discount_authority` que o motor devolveu. O cálculo local existe para o
 * instante em que o dedo ainda está no controle e não houve requisição nenhuma.
 */
export function SemaforoDeAlcada({
  pct,
  limites,
  alcada,
  className,
}: {
  pct: number;
  limites: LimitesDeAlcada;
  alcada?: Alcada;
  className?: string;
}) {
  const atual = alcada ?? alcadaDe(pct, limites);
  const descricao = SEMAFORO[atual];
  const faixas = faixasDaAlcada(limites);

  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <div className="grid gap-1.5 sm:grid-cols-3">
        {faixas.map((faixa) => {
          const ativa = faixa.alcada === atual;
          const info = SEMAFORO[faixa.alcada];
          return (
            <div
              key={faixa.alcada}
              data-alcada={faixa.alcada}
              data-ativa={ativa ? "true" : "false"}
              className={cn(
                "flex items-center gap-2 rounded-lg border px-2.5 py-1.5 transition-colors",
                ativa ? info.classe : "border-border/60 bg-card/60 text-muted-foreground",
              )}
            >
              <span
                aria-hidden="true"
                className={cn("size-2 shrink-0 rounded-full", ativa ? info.ponto : "bg-border")}
              />
              <span className="min-w-0">
                <span className="block truncate text-xs font-medium leading-tight">{info.rotulo}</span>
                <span className="block text-[0.65rem] tabular-nums leading-tight opacity-80">
                  {faixa.ate === null
                    ? `acima de ${formatarPct(faixa.de)}`
                    : faixa.de === 0
                      ? `até ${formatarPct(faixa.ate)}`
                      : `${formatarPct(faixa.de)} a ${formatarPct(faixa.ate)}`}
                </span>
              </span>
            </div>
          );
        })}
      </div>

      {/* `role="status"` porque o texto muda enquanto o controle é arrastado:
          quem usa leitor de tela precisa ouvir a mudança de faixa sem sair do
          campo. */}
      <p role="status" className="text-xs leading-relaxed text-muted-foreground">
        <strong className="text-foreground">
          {formatarPct(pct)} de desconto — {descricao.rotulo}.
        </strong>{" "}
        {descricao.explicacao}
      </p>
    </div>
  );
}
