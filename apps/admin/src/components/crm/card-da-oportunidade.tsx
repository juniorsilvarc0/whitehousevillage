"use client";

import * as React from "react";
import Link from "next/link";
import { useDraggable } from "@dnd-kit/core";
import { Menu } from "@base-ui/react/menu";
import { CalendarRange, CheckCircle2, GripVertical, ListChecks, MoveRight, Trophy, XCircle } from "lucide-react";

import { FaixaDeSlaDoCard } from "@/components/crm/faixa-de-sla";
import { Avatar } from "@/components/ui/avatar";
import type { CardDaOportunidade, EtapaDoFunil } from "@/lib/crm/tipos";
import { formatarDataCurta, noitesEntre } from "@/lib/datas";
import { formatarBRL } from "@/lib/dinheiro";
import { cn } from "@/lib/utils";

/**
 * O card do kanban.
 *
 * **Neutro, com aro fino** (docs/ui.md §9): quem carrega a cor é a coluna. Card
 * colorido num quadro de oito etapas é oito cores competindo pela mesma atenção
 * — e aí a única cor que precisa gritar, a da faixa de SLA estourado, deixa de
 * ser vista.
 *
 * Sem título: o card se identifica por contato, produto e datas, que é o que a
 * casa vende. O contrato não tem campo `title` de propósito.
 */
export function CartaoDaOportunidade({
  card,
  etapaAtual,
  etapas,
  aoMover,
  podeEditar,
  ocupado,
}: {
  card: CardDaOportunidade;
  etapaAtual: EtapaDoFunil;
  /** Todas as etapas do funil — inclusive as terminais, que o menu marca com o
   *  ícone da ação que elas de fato disparam. */
  etapas: EtapaDoFunil[];
  aoMover: (destinoId: string) => void;
  podeEditar: boolean;
  ocupado: boolean;
}) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, isDragging } = useDraggable({
    id: card.id,
    disabled: !podeEditar || ocupado,
    data: { origem: etapaAtual.id },
  });

  const noites = card.check_in && card.check_out ? noitesEntre(card.check_in, card.check_out) : 0;

  return (
    <article
      ref={setNodeRef}
      {...listeners}
      data-arrastando={isDragging ? "true" : undefined}
      style={transform ? { transform: `translate3d(${transform.x}px, ${transform.y}px, 0)` } : undefined}
      className={cn(
        // Sub-superfície dentro do `panel-float`: borda e fundo, nunca sombra.
        "group relative rounded-xl border border-border/70 bg-card px-3 py-2.5",
        "transition-[border-color,opacity] hover:border-border",
        isDragging && "opacity-40",
        ocupado && "opacity-60",
      )}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <Link
            href={`/app/oportunidades/${card.id}`}
            // O clique tem de chegar ao link sem virar arrasto: o pointerdown
            // para aqui, antes do sensor do dnd-kit.
            onPointerDown={(evento) => evento.stopPropagation()}
            className="block truncate text-sm font-medium leading-tight text-foreground outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring/40"
          >
            {card.contact_name}
          </Link>
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            {card.unit_type_name ?? "Produto a definir"}
          </p>
        </div>
        <p className="shrink-0 font-mono text-sm tabular-nums text-foreground">
          {formatarBRL(card.amount_cents)}
        </p>
      </div>

      {card.check_in && card.check_out ? (
        <p className="mt-1.5 flex items-center gap-1.5 text-xs text-muted-foreground">
          <CalendarRange className="size-3 shrink-0" aria-hidden="true" />
          <span className="truncate">
            {formatarDataCurta(card.check_in)} → {formatarDataCurta(card.check_out)}
            <span className="ml-1 tabular-nums">
              ({noites} {noites === 1 ? "noite" : "noites"})
            </span>
          </span>
        </p>
      ) : null}

      <FaixaDeSlaDoCard card={card} className="mt-2" />

      <div className="mt-2 flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          {card.owner_name ? (
            <Avatar nome={card.owner_name} className="size-5 text-[0.6rem]" />
          ) : (
            <span className="rounded-full bg-muted px-2 py-0.5 text-[0.65rem] text-muted-foreground">
              Sem dono
            </span>
          )}
          <span className="font-mono text-[0.68rem] tabular-nums text-muted-foreground">
            {card.probability}%
          </span>
          {card.pending_task_count > 0 ? (
            <span
              className="flex items-center gap-1 text-[0.68rem] text-muted-foreground"
              title={`${card.pending_task_count} tarefa(s) pendente(s)`}
            >
              <ListChecks className="size-3" aria-hidden="true" />
              <span className="tabular-nums">{card.pending_task_count}</span>
            </span>
          ) : null}
          {card.reservation_code ? (
            <span className="truncate font-mono text-[0.68rem] text-muted-foreground">
              {card.reservation_code}
            </span>
          ) : null}
        </div>

        {podeEditar ? (
          <div className="flex shrink-0 items-center">
            <MenuMoverPara
              contato={card.contact_name}
              etapaAtual={etapaAtual}
              etapas={etapas}
              aoMover={aoMover}
              ocupado={ocupado}
            />
            <button
              ref={setActivatorNodeRef}
              {...attributes}
              {...listeners}
              type="button"
              aria-label={`Arrastar o card de ${card.contact_name}`}
              className="cursor-grab rounded-md p-1 text-muted-foreground/60 outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/40"
            >
              <GripVertical className="size-4" aria-hidden="true" />
            </button>
          </div>
        ) : null}
      </div>
    </article>
  );
}

/**
 * "Mover para" — o caminho que **não** é o arrasto.
 *
 * Existe por três motivos, e nenhum deles é preferência: arrastar não funciona
 * com teclado nem com leitor de tela; não funciona bem num celular onde a
 * coluna de destino está fora da tela; e não diz para onde dá para ir — o menu
 * lista as etapas e marca as duas que abrem diálogo em vez de mover.
 */
function MenuMoverPara({
  contato,
  etapaAtual,
  etapas,
  aoMover,
  ocupado,
}: {
  contato: string;
  etapaAtual: EtapaDoFunil;
  etapas: EtapaDoFunil[];
  aoMover: (destinoId: string) => void;
  ocupado: boolean;
}) {
  return (
    <Menu.Root>
      <Menu.Trigger
        disabled={ocupado}
        onPointerDown={(evento) => evento.stopPropagation()}
        aria-label={`Mover ${contato} para outra etapa`}
        className={cn(
          "rounded-md p-1 text-muted-foreground/60 outline-none transition-colors",
          "hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/40",
          "disabled:opacity-50",
        )}
      >
        <MoveRight className="size-4" aria-hidden="true" />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner side="bottom" align="end" sideOffset={6} className="z-50">
          <Menu.Popup className="panel-float w-60 overflow-hidden p-1.5 outline-none">
            <p className="px-2.5 py-1.5 text-[0.68rem] uppercase tracking-wide text-muted-foreground">
              Mover para
            </p>
            {etapas.map((etapa) => {
              const atual = etapa.id === etapaAtual.id;
              const Icone = etapa.type === "ganho" ? Trophy : etapa.type === "perdido" ? XCircle : CheckCircle2;
              return (
                <Menu.Item
                  key={etapa.id}
                  disabled={atual}
                  onClick={() => aoMover(etapa.id)}
                  className={cn(
                    "flex cursor-default items-center gap-2 rounded-lg px-2.5 py-2 text-sm outline-none",
                    "data-[highlighted]:bg-muted data-[disabled]:opacity-45",
                  )}
                >
                  <span
                    aria-hidden="true"
                    className="size-2 shrink-0 rounded-full"
                    style={{ backgroundColor: etapa.color }}
                  />
                  <span className="min-w-0 flex-1 truncate">{etapa.name}</span>
                  {etapa.type !== "aberto" ? (
                    <Icone className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
                  ) : null}
                </Menu.Item>
              );
            })}
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  );
}
