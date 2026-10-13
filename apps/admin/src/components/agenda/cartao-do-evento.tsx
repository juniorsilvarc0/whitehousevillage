"use client";

import * as React from "react";
import { CheckCircle2, Loader2, UserRound } from "lucide-react";

import { IconeDoTipo, SeloDeConflito, SeloDeStatus } from "@/components/agenda/selos";
import { Button } from "@/components/ui/button";
import { ROTULO_DA_ACAO, ROTULO_DO_STATUS, diaEMes, janelaPorExtenso } from "@/lib/agenda/rotulos";
import type { TrechoDisposto } from "@/lib/agenda/grade";
import type { EventoDaAgenda } from "@/lib/agenda/tipos";
import { cn } from "@/lib/utils";

/**
 * As três formas do evento na tela — o bloco da grade de horários, o chip da
 * faixa de dia inteiro e do mês, e o cartão da lista. As três abrem o mesmo
 * detalhe; nenhuma decide o que o evento aceita (isso é do detalhe, pelo
 * `allowed_actions`), exceto o "Concluir" do cartão da lista, que também sai
 * de `allowed_actions`.
 */

function unidades(evento: EventoDaAgenda): string {
  return evento.units.map((u) => u.unit_code).join(", ");
}

/** O que o leitor de tela anuncia — o cartão da grade é pequeno demais para
 *  carregar tudo à vista. */
export function rotuloDoEvento(evento: EventoDaAgenda): string {
  return [
    evento.title,
    janelaPorExtenso(evento),
    unidades(evento) || null,
    ROTULO_DO_STATUS[evento.status],
    evento.has_unit_conflict ? "conflito de unidade" : null,
  ]
    .filter(Boolean)
    .join(", ");
}

function atributos(evento: EventoDaAgenda) {
  return {
    "data-tipo": evento.type,
    "data-status": evento.status,
    "data-conflito": evento.has_unit_conflict ? "1" : undefined,
  };
}

/** O bloco posicionado na grade de horários (visões de dia e semana). */
export function BlocoDaGrade({
  trecho,
  inicioDaFaixa,
  minimoVisual,
  aoAbrir,
}: {
  trecho: TrechoDisposto;
  /** Minuto do dia em que a grade desenhada começa. */
  inicioDaFaixa: number;
  minimoVisual: number;
  aoAbrir: (evento: EventoDaAgenda) => void;
}) {
  const { evento } = trecho;
  const topo = (trecho.inicio - inicioDaFaixa) / 60;
  const altura = Math.max(trecho.fim - trecho.inicio, minimoVisual) / 60;
  const curto = trecho.fim - trecho.inicio <= 45;
  return (
    <button
      type="button"
      onClick={() => aoAbrir(evento)}
      aria-label={rotuloDoEvento(evento)}
      {...atributos(evento)}
      className={cn(
        "agenda-evento absolute z-[1] overflow-hidden rounded-md px-1.5 py-0.5 text-left text-[0.6875rem] leading-tight outline-none",
        "transition-[filter] hover:brightness-[0.97] focus-visible:ring-2 focus-visible:ring-ring",
      )}
      style={{
        top: `calc(${topo} * var(--agenda-hora))`,
        height: `calc(${altura} * var(--agenda-hora) - 2px)`,
        left: `calc(${trecho.coluna} * 100% / ${trecho.colunas} + 2px)`,
        width: `calc(100% / ${trecho.colunas} - 4px)`,
      }}
    >
      <span className={cn("flex min-w-0 gap-1", curto ? "items-center" : "flex-col")}>
        <span className="flex min-w-0 items-center gap-1 font-medium">
          <IconeDoTipo tipo={evento.type} className="size-3" />
          <span className="agenda-evento-titulo truncate">{evento.title}</span>
        </span>
        <span className="truncate tabular-nums text-muted-foreground">
          {trecho.continua ? "↳ " : ""}
          {janelaPorExtenso(evento)}
          {unidades(evento) ? ` · ${unidades(evento)}` : ""}
        </span>
      </span>
    </button>
  );
}

/** O chip compacto: faixa de dia inteiro e célula do mês. */
export function ChipDoEvento({
  evento,
  aoAbrir,
  mostrarHora = false,
}: {
  evento: EventoDaAgenda;
  aoAbrir: (evento: EventoDaAgenda) => void;
  mostrarHora?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={() => aoAbrir(evento)}
      aria-label={rotuloDoEvento(evento)}
      {...atributos(evento)}
      className="agenda-evento flex w-full min-w-0 items-center gap-1 rounded-md px-1.5 py-0.5 text-left text-[0.6875rem] leading-tight outline-none transition-[filter] hover:brightness-[0.97] focus-visible:ring-2 focus-visible:ring-ring"
    >
      <IconeDoTipo tipo={evento.type} className="size-3" />
      {mostrarHora && !evento.all_day ? (
        <span className="shrink-0 tabular-nums text-muted-foreground">{janelaPorExtenso(evento).slice(0, 5)}</span>
      ) : null}
      <span className="agenda-evento-titulo truncate font-medium">{evento.title}</span>
    </button>
  );
}

/**
 * O cartão da lista — a visão que a operação usa de pé, no celular. O cartão
 * inteiro abre o detalhe; o "Concluir" fica ao lado (nunca dentro: botão dentro
 * de botão não existe), em 44px, e **só aparece com `complete` em
 * `allowed_actions`** — a limpeza de amanhã não ganha o botão hoje.
 */
export function CartaoDaLista({
  evento,
  diaDoGrupo,
  aoAbrir,
  podeConcluir,
  aoConcluir,
}: {
  evento: EventoDaAgenda;
  /** O dia sob o qual o cartão aparece — se o evento começou antes, a frase diz desde quando. */
  diaDoGrupo: string;
  aoAbrir: (evento: EventoDaAgenda) => void;
  podeConcluir: boolean;
  aoConcluir?: (evento: EventoDaAgenda) => Promise<void>;
}) {
  const [concluindo, setConcluindo] = React.useState(false);
  const concluir = podeConcluir && evento.allowed_actions.includes("complete") && aoConcluir;
  const janela = janelaPorExtenso(evento);
  const desde = evento.date < diaDoGrupo ? `desde ${diaEMes(evento.date)}` : null;
  const quem = [evento.contact_name, evento.reservation_code].filter(Boolean).join(" · ");

  return (
    <article
      aria-label={evento.title}
      {...atributos(evento)}
      className="agenda-evento flex items-stretch gap-2 rounded-xl border border-border/60 pr-2"
    >
      <button
        type="button"
        onClick={() => aoAbrir(evento)}
        aria-label={`Abrir: ${rotuloDoEvento(evento)}`}
        className="flex min-h-14 min-w-0 flex-1 items-start gap-3 rounded-l-xl py-2.5 pl-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className="w-16 shrink-0 pt-0.5 text-xs font-medium tabular-nums">
          {evento.all_day ? "Dia todo" : janela.split(/[–→ ]/)[0]}
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-center gap-1.5">
            <IconeDoTipo tipo={evento.type} />
            <span className="agenda-evento-titulo truncate text-sm font-medium">{evento.title}</span>
          </span>
          <span className="mt-0.5 block truncate text-xs text-muted-foreground tabular-nums">
            {[janela, desde, unidades(evento)].filter(Boolean).join(" · ")}
          </span>
          {quem ? <span className="mt-0.5 block truncate text-xs text-muted-foreground">{quem}</span> : null}
          <span className="mt-1.5 flex flex-wrap items-center gap-1.5">
            {evento.status !== "agendado" ? <SeloDeStatus status={evento.status} /> : null}
            {evento.has_unit_conflict ? <SeloDeConflito /> : null}
            {evento.assignee_name ? (
              <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                <UserRound aria-hidden="true" className="size-3" />
                {evento.assignee_name}
              </span>
            ) : evento.origin === "reservation" || evento.origin === "manual" ? (
              <span className="text-xs text-muted-foreground">Sem responsável</span>
            ) : null}
          </span>
        </span>
      </button>
      {concluir ? (
        <Button
          size="sm"
          variant="outline"
          className="h-11 shrink-0 self-center"
          disabled={concluindo}
          onClick={async () => {
            setConcluindo(true);
            try {
              await aoConcluir(evento);
            } finally {
              setConcluindo(false);
            }
          }}
        >
          {concluindo ? <Loader2 className="animate-spin" aria-hidden="true" /> : <CheckCircle2 aria-hidden="true" />}
          {ROTULO_DA_ACAO.complete}
        </Button>
      ) : null}
    </article>
  );
}
