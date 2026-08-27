"use client";

import * as React from "react";
import { RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { descreverTempoReal, idadeEmTexto } from "@/lib/tempo-real/rotulos";
import type { EstadoDoTempoReal } from "@/lib/tempo-real/sse";
import { useAgora } from "@/lib/tempo-real/relogio";
import { cn } from "@/lib/utils";

/**
 * O selo de tempo real. Pequeno de propósito — ele só precisa ser grande quando
 * está errado, e é o que a cor faz.
 */
export function IndicadorDeTempoReal({
  estado,
  atualizando,
  atualizadoEm,
  aoReconectar,
  className,
}: {
  estado: EstadoDoTempoReal;
  atualizando: boolean;
  atualizadoEm: number | null;
  aoReconectar: () => void;
  className?: string;
}) {
  const agora = useAgora();
  const descricao = descreverTempoReal(estado);
  const idade = idadeEmTexto(atualizadoEm, agora);

  return (
    <div
      className={cn("flex items-center gap-2 text-xs", className)}
      // `polite` e não `assertive`: a mudança de estado da conexão nunca deve
      // cortar o que o leitor de tela está lendo — ela informa, não interrompe.
      role="status"
      aria-live="polite"
      title={descricao.detalhe}
    >
      <span
        aria-hidden="true"
        className={cn(
          "size-2 shrink-0 rounded-full",
          descricao.tom === "ok" && "bg-alcada-livre",
          descricao.tom === "atencao" && "bg-alcada-atencao",
          descricao.tom === "falha" && "bg-destructive",
          (atualizando || estado === "conectando") && "mapa-pulso",
        )}
      />
      <span
        className={cn(
          "font-medium",
          descricao.tom === "falha" ? "text-destructive" : "text-muted-foreground",
        )}
      >
        {atualizando ? "Atualizando" : descricao.rotulo}
      </span>
      {idade && !atualizando ? <span className="text-muted-foreground/80 tabular-nums">{idade}</span> : null}
      {descricao.ofereceReconectar ? (
        <Button variant="ghost" size="iconSm" onClick={aoReconectar} aria-label="Reconectar o canal de eventos">
          <RefreshCw />
        </Button>
      ) : null}
    </div>
  );
}
