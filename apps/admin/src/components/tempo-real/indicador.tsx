"use client";

import * as React from "react";
import { RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { descreverTempoReal, idadeEmTexto, type AssuntoDoTempoReal } from "@/lib/tempo-real/rotulos";
import type { EstadoDoTempoReal } from "@/lib/tempo-real/sse";
import { useAgora } from "@/lib/tempo-real/relogio";
import { cn } from "@/lib/utils";

/**
 * O selo de tempo real. Pequeno de propósito — ele só precisa ser grande quando
 * está errado, e é o que a cor faz.
 *
 * Serve o mapa de ocupação e o quadro do funil — e é por isso que ele saiu de
 * `components/mapa/`: um kanban importando do mapa é a dependência que, na
 * próxima tela ao vivo, vira uma segunda cópia do selo. `assunto` existe
 * porque as frases nomeiam a tela: um selo que diz "o mapa se atualiza
 * sozinho" em cima do funil é o tipo de texto que ensina a não ler o
 * indicador.
 */
export function IndicadorDeTempoReal({
  estado,
  atualizando,
  atualizadoEm,
  aoReconectar,
  assunto,
  className,
}: {
  estado: EstadoDoTempoReal;
  atualizando: boolean;
  atualizadoEm: number | null;
  aoReconectar: () => void;
  assunto?: AssuntoDoTempoReal;
  className?: string;
}) {
  const agora = useAgora();
  const descricao = descreverTempoReal(estado, assunto);
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
          // `motion-safe:animate-pulse` do Tailwind, e não a `.mapa-pulso` de
          // `mapa.css`: aquela regra só existe onde a folha do mapa é
          // importada, e este selo passou a desenhar também no funil — onde
          // ficaria parado, sem erro nenhum a apontar o motivo.
          (atualizando || estado === "conectando") && "motion-safe:animate-pulse",
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
