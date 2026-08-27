import * as React from "react";
import { Trophy, XCircle } from "lucide-react";

import type { EtapaDoFunil, Oportunidade } from "@/lib/crm/tipos";
import { cn } from "@/lib/utils";

/**
 * A trilha de etapas — onde o negócio está, e o que vem antes e depois.
 *
 * Mostra o funil **inteiro**, inclusive as etapas que este card ainda não
 * alcançou: a pergunta que a trilha responde não é "em que etapa está" (isso o
 * kanban já disse), é "quanto falta". Só a atual seria um rótulo, não uma
 * trilha.
 *
 * As duas terminais aparecem separadas do trilho, com o ícone da ação que as
 * alcança — porque elas não são "a próxima etapa": são dois desfechos, e cada
 * um tem o seu botão.
 */
export function TrilhaDeEtapas({
  etapas,
  oportunidade,
  className,
}: {
  etapas: EtapaDoFunil[];
  oportunidade: Oportunidade;
  className?: string;
}) {
  const abertas = etapas.filter((etapa) => etapa.type === "aberto");
  const terminais = etapas.filter((etapa) => etapa.type !== "aberto");
  const atual = etapas.find((etapa) => etapa.id === oportunidade.stage_id);
  const posicaoAtual = atual?.position ?? -1;

  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <ol className="flex flex-wrap items-stretch gap-1.5">
        {abertas.map((etapa) => {
          const ehAtual = etapa.id === oportunidade.stage_id;
          const passou = etapa.position < posicaoAtual;
          return (
            <li
              key={etapa.id}
              aria-current={ehAtual ? "step" : undefined}
              data-estado={ehAtual ? "atual" : passou ? "cumprida" : "futura"}
              className={cn(
                "flex min-w-0 flex-1 basis-28 flex-col gap-0.5 rounded-lg border px-2.5 py-1.5",
                ehAtual
                  ? "border-transparent text-white"
                  : passou
                    ? "border-border/60 bg-muted/40 text-foreground"
                    : "border-dashed border-border/60 text-muted-foreground",
              )}
              // A cor da etapa é dado do funil (a gestão edita sem deploy), e
              // por isso entra por `style` em vez de token — a mesma exceção do
              // kanban, e a única em toda a tela.
              style={ehAtual ? { backgroundColor: etapa.color } : undefined}
            >
              <span className="truncate text-xs font-medium leading-tight">{etapa.name}</span>
              <span className={cn("font-mono text-[0.65rem] tabular-nums", ehAtual ? "text-white/80" : "text-muted-foreground")}>
                {etapa.probability}%
                {etapa.sla_days !== null ? ` · SLA ${etapa.sla_days}d` : ""}
              </span>
            </li>
          );
        })}
      </ol>

      {terminais.length > 0 ? (
        <ul className="flex flex-wrap gap-1.5">
          {terminais.map((etapa) => {
            const ehAtual = etapa.id === oportunidade.stage_id;
            const Icone = etapa.type === "ganho" ? Trophy : XCircle;
            return (
              <li
                key={etapa.id}
                aria-current={ehAtual ? "step" : undefined}
                className={cn(
                  "flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs",
                  ehAtual && etapa.type === "ganho" && "border-alcada-livre/40 bg-alcada-livre/15 text-foreground",
                  ehAtual && etapa.type === "perdido" && "border-destructive/35 bg-destructive/12 text-destructive",
                  !ehAtual && "border-dashed border-border/60 text-muted-foreground",
                )}
              >
                <Icone className="size-3.5" aria-hidden="true" />
                {etapa.name}
              </li>
            );
          })}
        </ul>
      ) : null}
    </div>
  );
}
