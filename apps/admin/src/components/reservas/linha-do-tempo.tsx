import * as React from "react";

import { formatarInstante } from "@/lib/datas";
import { formatarBRL } from "@/lib/dinheiro";
import type { EventoDaReserva } from "@/lib/reservas/tipos";

/**
 * `reservation_events` — **append-only**, e é essa a única coisa que a tela
 * precisa deixar claro: nada aqui é editado nem apagado. É a prova de quem fez
 * o quê e quando, e é dela que a gestão tira a resposta quando o hóspede
 * reclama de um valor três meses depois.
 *
 * O vocabulário de `type` é **aberto** — gravado pelo módulo, não um enum. Por
 * isso o mapa de rótulos tem um padrão em vez de um `switch` exaustivo: um tipo
 * novo no servidor tem que aparecer na linha do tempo com o próprio nome, nunca
 * sumir dela porque a tela não o conhecia.
 */

const ROTULOS: Record<string, string> = {
  created: "Pré-reserva criada",
  confirmed: "Sinal registrado e reserva confirmada",
  hold_extended: "Prazo da pré-reserva estendido",
  unit_reassigned: "Unidade trocada",
  rescheduled: "Estadia remarcada",
  credit_issued: "Crédito gerado a favor do hóspede",
  checked_in: "Check-in registrado",
  checked_out: "Check-out registrado",
  cancelled: "Reserva cancelada",
  expired: "Pré-reserva expirada",
  no_show: "Hóspede não compareceu",
};

export function LinhaDoTempo({ eventos }: { eventos: EventoDaReserva[] }) {
  return (
    <section aria-label="Linha do tempo" className="rounded-xl border border-border/60 bg-muted/20 p-4">
      <h2 className="font-display text-base">Linha do tempo</h2>
      <p className="mt-1 text-xs text-muted-foreground">
        Registro append-only: criação, confirmação, extensão, realocação, remarcação, check-in, check-out e
        cancelamento entram como linhas. Ninguém edita, ninguém apaga.
      </p>

      {eventos.length === 0 ? (
        <p className="mt-3 text-sm text-muted-foreground">
          Nada registrado ainda — o que só acontece antes de a reserva existir de verdade.
        </p>
      ) : (
        <ol className="mt-3 flex flex-col gap-2">
          {eventos.map((evento) => (
            <li
              key={evento.id}
              className="flex items-start gap-3 rounded-lg border border-border/60 bg-card px-3 py-2"
            >
              <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-primary" aria-hidden="true" />
              <div className="min-w-0">
                <p className="text-sm">{ROTULOS[evento.type] ?? evento.type}</p>
                <p className="text-xs text-muted-foreground">
                  {formatarInstante(evento.at)}
                  {/* `actor_id` nulo é o job de expiração, não uma pessoa — e
                      dizer "automático" é melhor que deixar o autor em branco,
                      que se lê como dado faltando. */}
                  {evento.actor_id ? "" : " · automático"}
                </p>
                <DetalhesDoEvento payload={evento.payload} />
              </div>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

/** Os campos de `payload` que valem uma linha na tela. Dinheiro em BRL; o resto
 *  fica fora — despejar o JSON inteiro transforma a prova em ruído. */
const EM_DINHEIRO = new Set([
  "deposit_paid_cents",
  "credit_cents",
  "applied_cents",
  "paid_cents",
  "inherited_from_cents",
  "total_cents",
  "deposit_cents",
  "refund_cents",
  "retained_cents",
]);

const TEXTOS: Record<string, string> = {
  deposit_paid_cents: "sinal recebido",
  credit_cents: "crédito",
  applied_cents: "aplicado",
  paid_cents: "pago",
  inherited_from_cents: "herdado (bruto)",
  total_cents: "total",
  deposit_cents: "sinal",
  refund_cents: "devolvido",
  retained_cents: "retido",
  reason: "motivo",
  hours: "horas",
  policy_version: "política",
  status: "estado",
};

function DetalhesDoEvento({ payload }: { payload: Record<string, unknown> | null }) {
  if (!payload) return null;

  const partes: string[] = [];
  for (const [chave, valor] of Object.entries(payload)) {
    const rotulo = TEXTOS[chave];
    if (!rotulo) continue;
    if (EM_DINHEIRO.has(chave) && typeof valor === "number") {
      partes.push(`${rotulo} ${formatarBRL(valor)}`);
    } else if (typeof valor === "string" || typeof valor === "number") {
      partes.push(`${rotulo} ${valor}`);
    }
  }

  if (partes.length === 0) return null;
  return <p className="mt-0.5 text-xs tabular-nums text-muted-foreground">{partes.join(" · ")}</p>;
}
