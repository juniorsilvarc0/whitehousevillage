"use client";

import * as React from "react";
import { AlertTriangle, BellRing, CalendarClock, CheckCircle2, Clock, Loader2, Wallet } from "lucide-react";

import { notificar, notificarSucesso } from "@/components/crm/avisos";
import { Button } from "@/components/ui/button";
import { concluirAtividade } from "@/lib/crm/acoes";
import type { AlertaDaOportunidade, Atividade, CodigoDeAlerta } from "@/lib/crm/tipos";
import { formatarInstante } from "@/lib/datas";
import { cn } from "@/lib/utils";

/**
 * O rail: **alertas** e a **próxima ação**.
 *
 * Os alertas são derivados na leitura, no servidor — não há tabela de alertas.
 * Alerta gravado precisa de um job para nascer e de outro para morrer, e erra
 * em silêncio nos dois: fica de pé depois de resolvido, ou nunca aparece porque
 * o job caiu. Aqui a tela só pinta o que o `/full` acabou de calcular.
 *
 * A próxima ação é **uma**, não a lista. A pergunta que o rail responde é "o
 * que eu faço agora com essa pessoa"; cinco tarefas empilhadas devolvem a
 * pergunta em vez de respondê-la, e a lista inteira já está na aba de
 * atividades.
 */

const ICONE_DO_ALERTA: Record<CodigoDeAlerta, React.ComponentType<{ className?: string }>> = {
  sla_estourado: AlertTriangle,
  tarefa_vencida: Clock,
  parado_ha_n_dias: CalendarClock,
  hold_expirando: BellRing,
  saldo_a_vencer: Wallet,
};

const CLASSE_DA_SEVERIDADE: Record<AlertaDaOportunidade["severity"], string> = {
  critico: "border-destructive/35 bg-destructive/10 text-destructive",
  atencao: "border-alcada-atencao/40 bg-alcada-atencao/12 text-foreground",
  info: "border-border/60 bg-muted/30 text-muted-foreground",
};

export function RailDaOportunidade({
  alertas,
  proxima,
  oportunidadeId,
  podeConcluir,
}: {
  alertas: AlertaDaOportunidade[];
  /** A tarefa pendente mais urgente — normalmente a automática da etapa. */
  proxima: Atividade | null;
  oportunidadeId: string;
  podeConcluir: boolean;
}) {
  return (
    <aside className="flex flex-col gap-4" aria-label="Alertas e próxima ação">
      <section className="rounded-xl border border-border/60 bg-muted/20 p-4">
        <h2 className="font-display text-sm">Próxima ação</h2>
        {proxima ? (
          <ProximaAcao atividade={proxima} oportunidadeId={oportunidadeId} podeConcluir={podeConcluir} />
        ) : (
          <p className="mt-2 text-xs text-muted-foreground">
            Nenhuma tarefa pendente. Quando o negócio entra numa etapa com tarefa configurada, ela é
            criada sozinha — com o prazo da etapa e no nome do dono do negócio.
          </p>
        )}
      </section>

      <section className="rounded-xl border border-border/60 bg-muted/20 p-4">
        <h2 className="font-display text-sm">Alertas</h2>
        {alertas.length === 0 ? (
          <p className="mt-2 text-xs text-muted-foreground">Nada em atraso neste negócio.</p>
        ) : (
          <ul className="mt-2 flex flex-col gap-2">
            {alertas.map((alerta, indice) => {
              const Icone = ICONE_DO_ALERTA[alerta.code] ?? AlertTriangle;
              return (
                <li
                  key={`${alerta.code}-${alerta.entity_id ?? indice}`}
                  className={cn("flex items-start gap-2 rounded-lg border px-2.5 py-2 text-xs", CLASSE_DA_SEVERIDADE[alerta.severity])}
                >
                  <Icone className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
                  <span className="min-w-0">
                    {/* A frase vem montada do servidor: "há 2 dias" contado no
                        navegador daria um número por fuso de quem abriu. */}
                    {alerta.message}
                    {alerta.due_at ? (
                      <span className="block text-[0.68rem] opacity-80">{formatarInstante(alerta.due_at)}</span>
                    ) : null}
                  </span>
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </aside>
  );
}

function ProximaAcao({
  atividade,
  oportunidadeId,
  podeConcluir,
}: {
  atividade: Atividade;
  oportunidadeId: string;
  podeConcluir: boolean;
}) {
  const [concluindo, setConcluindo] = React.useState(false);

  async function concluir() {
    setConcluindo(true);
    try {
      const resultado = await concluirAtividade(atividade.id, oportunidadeId);
      if (!resultado.ok) {
        notificar(resultado);
        return;
      }
      notificarSucesso("Tarefa concluída");
    } finally {
      setConcluindo(false);
    }
  }

  return (
    <div className="mt-2">
      <p className="text-sm font-medium leading-tight">{atividade.subject}</p>
      <p
        className={cn(
          "mt-1 flex items-center gap-1.5 text-xs",
          atividade.overdue ? "text-destructive" : "text-muted-foreground",
        )}
      >
        <Clock className="size-3.5" aria-hidden="true" />
        {atividade.due_at ? formatarInstante(atividade.due_at) : "Sem prazo"}
        {atividade.overdue ? " · vencida" : ""}
      </p>
      {atividade.auto ? (
        <p className="mt-1 text-[0.68rem] text-muted-foreground">
          Criada automaticamente ao entrar na etapa.
        </p>
      ) : null}
      {podeConcluir ? (
        <Button size="sm" variant="outline" className="mt-3" onClick={() => void concluir()} disabled={concluindo}>
          {concluindo ? <Loader2 className="animate-spin" aria-hidden="true" /> : <CheckCircle2 aria-hidden="true" />}
          Concluir
        </Button>
      ) : null}
    </div>
  );
}
