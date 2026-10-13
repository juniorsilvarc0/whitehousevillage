"use client";

import * as React from "react";
import { Ban, Plus, Trash2 } from "lucide-react";

import { notificarSucesso } from "@/components/agenda/avisos";
import { ModalDeBloqueio } from "@/components/agenda/modal-de-bloqueio";
import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { EstadoVazio } from "@/components/layout/estados";
import { ModalDeConfirmacao } from "@/components/layout/modal-de-confirmacao";
import { Button } from "@/components/ui/button";
import { removerBloqueioDeHorario } from "@/lib/agenda/acoes";
import { dataLonga, horarioSemODia } from "@/lib/agenda/rotulos";
import type { BloqueioDeHorario } from "@/lib/agenda/tipos";
import { paredeDaCasa } from "@/lib/fuso";

import "./agenda.css";

/**
 * Os bloqueios de horário da operação numa janela — lista e criação.
 *
 * Criar pede `agenda:criar` em escopo `all` (o bloqueio é da operação inteira,
 * não tem dono); remover, `agenda:excluir` em `all`. Quem não tem continua
 * vendo a lista: o corretor precisa saber que a operação está fechada antes de
 * marcar a visita.
 *
 * Remover **apaga** (`204`) — é configuração do tempo, não histórico — e por
 * isso pede confirmação.
 */
export function ListaDeBloqueios({
  bloqueios,
  hoje,
  podeCriar,
  podeRemover,
}: {
  bloqueios: readonly BloqueioDeHorario[];
  hoje: string;
  podeCriar: boolean;
  podeRemover: boolean;
}) {
  const novo = useControleDeModal<{ data: string }>();
  const [removendo, setRemovendo] = React.useState<BloqueioDeHorario | null>(null);

  const botaoNovo = podeCriar ? (
    <Button onClick={() => novo.abrir({ data: hoje })}>
      <Plus aria-hidden="true" />
      Bloquear horário
    </Button>
  ) : null;

  return (
    <div className="flex flex-col gap-4">
      {botaoNovo ? <div className="flex justify-end">{botaoNovo}</div> : null}

      {bloqueios.length === 0 ? (
        <EstadoVazio
          icone={Ban}
          titulo="Nenhum horário bloqueado"
          descricao={<>Treinamento da equipe, dedetização, folga coletiva: o que deixar a operação fora do ar entra aqui, e quem marcar algo nesse horário é avisado.</>}
          acao={botaoNovo}
        />
      ) : (
        <ul className="flex flex-col gap-2" aria-label="Bloqueios de horário">
          {bloqueios.map((b) => {
            const inicio = paredeDaCasa(b.starts_at);
            return (
              <li key={b.id}>
                <article
                  aria-label={b.reason}
                  className="agenda-bloqueio-de-horario flex items-center gap-3 rounded-xl border border-border/60 px-4 py-3"
                >
                  <Ban aria-hidden="true" className="size-4 shrink-0 text-destructive" />
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium">{b.reason}</p>
                    <p className="mt-0.5 text-xs text-muted-foreground tabular-nums first-letter:uppercase">
                      {inicio ? `${dataLonga(inicio.data)}, ` : ""}
                      {horarioSemODia(b.starts_at, b.ends_at)}
                    </p>
                  </div>
                  {podeRemover ? (
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`Remover o bloqueio: ${b.reason}`}
                      onClick={() => setRemovendo(b)}
                      className="text-destructive hover:bg-destructive/10"
                    >
                      <Trash2 aria-hidden="true" />
                    </Button>
                  ) : null}
                </article>
              </li>
            );
          })}
        </ul>
      )}

      <ModalDeBloqueio controle={novo.ref} />
      <ModalDeConfirmacao
        aberto={removendo !== null}
        aoMudar={(aberto) => {
          if (!aberto) setRemovendo(null);
        }}
        destrutivo
        titulo="Remover este bloqueio?"
        rotuloConfirmar="Remover"
        descricao={
          removendo ? (
            <>
              “{removendo.reason}” deixa de valer, e quem marcar algo nesse horário não é mais avisado. Fica registrado na
              auditoria quem removeu.
            </>
          ) : null
        }
        aoConfirmar={async () => {
          if (!removendo) return { ok: true, data: null };
          const r = await removerBloqueioDeHorario(removendo.id);
          if (r.ok) notificarSucesso("Bloqueio removido", removendo.reason);
          return r;
        }}
      />
    </div>
  );
}
