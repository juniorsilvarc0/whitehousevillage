"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { CalendarRange, CheckCircle2, ExternalLink, Loader2, Lock, Pencil, XCircle } from "lucide-react";

import { notificarSucesso } from "@/components/agenda/avisos";
import { SeloDeConflito, SeloDeStatus, SeloDoTipo } from "@/components/agenda/selos";
import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Button } from "@/components/ui/button";
import { concluirEvento, desmarcarEvento } from "@/lib/agenda/acoes";
import { mensagemDaAgenda, pedeRecarga } from "@/lib/agenda/mensagens";
import { botoesDoEvento, type PermissoesDaAgenda } from "@/lib/agenda/permissoes";
import { ROTULO_DA_ACAO, ROTULO_DA_CONCLUSAO, ROTULO_DA_ORIGEM, dataLonga, janelaPorExtenso } from "@/lib/agenda/rotulos";
import type { EventoDaAgenda } from "@/lib/agenda/tipos";
import { formatarInstante } from "@/lib/datas";

/** Onde o detalhe pode levar — cada destino pede a própria permissão. */
export type DestinosDoDetalhe = {
  /** `/app/reservas/{id}` — `reservations:ver`. */
  reservas: boolean;
  /** `/app/manutencao/{id}` — `maintenance:ver`. */
  manutencao: boolean;
  /** `/app/mapa` — `calendar:ver`; é onde o bloqueio de unidade se mexe. */
  mapa: boolean;
};

/**
 * O detalhe do evento — aberto pelo toque em qualquer visão.
 *
 * **Os botões saem da resposta**: `allowed_actions` (Concluir, Desmarcar) e
 * `editable` (Editar), cruzados com a matriz em `botoesDoEvento()`. A tela não
 * sabe que o check-in automático conclui pela reserva, nem que a limpeza de
 * amanhã ainda não conclui — quem sabe é a API, e a tela só obedece.
 *
 * O derivado (manutenção, bloqueio de unidade) é **só leitura** aqui: o
 * detalhe leva à ordem de manutenção ou ao mapa, que é onde ele se mexe.
 *
 * Desmarcar pede confirmação **dentro** do mesmo modal — um segundo diálogo
 * por cima do primeiro, no celular, é uma folha sobre outra folha.
 */
export function DetalheDoEvento({
  controle,
  permissoes,
  destinos,
  aoEditar,
}: {
  controle: React.RefObject<ControleDeModal<EventoDaAgenda> | null>;
  permissoes: Pick<PermissoesDaAgenda, "editar" | "excluir">;
  destinos: DestinosDoDetalhe;
  aoEditar: (evento: EventoDaAgenda) => void;
}) {
  const router = useRouter();
  const [evento, setEvento] = React.useState<EventoDaAgenda | null>(null);
  const [aberto, setAberto] = React.useState(false);
  const [confirmando, setConfirmando] = React.useState(false);
  const [enviando, setEnviando] = React.useState<"concluir" | "desmarcar" | null>(null);
  const [erro, setErro] = React.useState<string | null>(null);

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(alvo: EventoDaAgenda) {
        setEvento(alvo);
        setConfirmando(false);
        setErro(null);
        setEnviando(null);
        setAberto(true);
      },
    }),
    [],
  );

  const botoes = evento ? botoesDoEvento(evento, permissoes) : { concluir: false, desmarcar: false, editar: false };
  const derivado = evento?.origin === "maintenance" || evento?.origin === "stay_block";

  async function concluir() {
    if (!evento) return;
    setEnviando("concluir");
    setErro(null);
    try {
      const r = await concluirEvento(evento.id);
      if (r.ok) {
        setAberto(false);
        notificarSucesso(ROTULO_DA_CONCLUSAO[r.data.type] ?? "Concluído", r.data.title);
        return;
      }
      setErro(mensagemDaAgenda(r, "conclusao"));
      if (pedeRecarga(r)) router.refresh();
    } finally {
      setEnviando(null);
    }
  }

  async function desmarcar() {
    if (!evento) return;
    setEnviando("desmarcar");
    setErro(null);
    try {
      const r = await desmarcarEvento(evento.id);
      if (r.ok) {
        setAberto(false);
        notificarSucesso("Desmarcado", `${r.data.title} continua na agenda como desmarcado.`);
        return;
      }
      setErro(mensagemDaAgenda(r, "desmarcacao"));
      if (pedeRecarga(r)) router.refresh();
    } finally {
      setEnviando(null);
    }
  }

  const rodape = !evento ? null : confirmando ? (
    <>
      <Button variant="ghost" onClick={() => setConfirmando(false)} disabled={enviando !== null}>
        Voltar
      </Button>
      <Button variant="destructive" onClick={() => void desmarcar()} disabled={enviando !== null} className="max-md:h-11 max-md:flex-1">
        {enviando === "desmarcar" ? <Loader2 className="animate-spin" aria-hidden="true" /> : <XCircle aria-hidden="true" />}
        {ROTULO_DA_ACAO.cancel}
      </Button>
    </>
  ) : (
    <div className="flex w-full flex-wrap items-center justify-end gap-2" role="group" aria-label="Ações do evento">
      {botoes.desmarcar ? (
        <Button
          variant="ghost"
          onClick={() => setConfirmando(true)}
          disabled={enviando !== null}
          className="text-destructive hover:bg-destructive/10 max-md:h-11"
        >
          <XCircle aria-hidden="true" />
          {ROTULO_DA_ACAO.cancel}
        </Button>
      ) : null}
      {botoes.editar ? (
        <Button
          variant="outline"
          onClick={() => {
            setAberto(false);
            aoEditar(evento);
          }}
          disabled={enviando !== null}
          className="max-md:h-11"
        >
          <Pencil aria-hidden="true" />
          {evento.editable === "so_notas" ? "Editar anotação" : "Editar"}
        </Button>
      ) : null}
      {botoes.concluir ? (
        <Button onClick={() => void concluir()} disabled={enviando !== null} className="max-md:h-11 max-md:flex-1">
          {enviando === "concluir" ? <Loader2 className="animate-spin" aria-hidden="true" /> : <CheckCircle2 aria-hidden="true" />}
          {ROTULO_DA_ACAO.complete}
        </Button>
      ) : null}
    </div>
  );

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title={evento?.title ?? "Evento"}
      description={evento ? `${dataLonga(evento.date)} · ${janelaPorExtenso(evento)}` : undefined}
      footer={rodape}
    >
      {evento ? (
        confirmando ? (
          <div className="flex flex-col gap-3 text-sm text-muted-foreground">
            <p className="text-foreground">Desmarcar “{evento.title}”?</p>
            <p>
              Desmarcar não apaga: o evento continua na agenda como desmarcado, e não volta. Se voltar a acontecer, marque
              de novo.
            </p>
            {erro ? (
              <p role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {erro}
              </p>
            ) : null}
          </div>
        ) : (
          <div className="flex flex-col gap-4 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <SeloDoTipo tipo={evento.type} />
              <SeloDeStatus status={evento.status} />
              {evento.has_unit_conflict ? <SeloDeConflito /> : null}
            </div>

            {evento.has_unit_conflict ? (
              <p className="rounded-lg bg-destructive/8 px-3 py-2 text-xs text-foreground">
                Outra coisa ocupa a mesma unidade neste horário — a venda, a manutenção ou um ajuste chegou depois. A agenda
                não trava a venda: ela mostra o conflito para alguém mudar um dos dois horários.
              </p>
            ) : null}

            <dl className="grid gap-3 sm:grid-cols-2">
              <Dado rotulo="Quando">
                <span className="tabular-nums">{janelaPorExtenso(evento)}</span>
              </Dado>
              <Dado rotulo="Onde">
                {evento.units.length > 0 ? (
                  <span className="font-mono">{evento.units.map((u) => u.unit_code).join(", ")}</span>
                ) : (
                  <span className="text-muted-foreground">Não ocupa unidade</span>
                )}
              </Dado>
              {evento.reservation_id ? (
                <Dado rotulo="Reserva">
                  {destinos.reservas ? (
                    <Link
                      href={`/app/reservas/${evento.reservation_id}`}
                      className="font-mono text-primary underline-offset-4 hover:underline"
                    >
                      {evento.reservation_code ?? "Abrir a reserva"}
                    </Link>
                  ) : (
                    <span className="font-mono">{evento.reservation_code ?? "—"}</span>
                  )}
                </Dado>
              ) : null}
              {evento.contact_name ? <Dado rotulo={evento.origin === "reservation" ? "Hóspede" : "Com quem"}>{evento.contact_name}</Dado> : null}
              {!derivado ? (
                <Dado rotulo="Responsável">
                  {evento.assignee_name ?? <span className="text-muted-foreground">Ninguém ainda</span>}
                </Dado>
              ) : null}
              <Dado rotulo="Origem">{ROTULO_DA_ORIGEM[evento.origin]}</Dado>
            </dl>

            {evento.notes ? <p className="whitespace-pre-line rounded-lg bg-muted/40 px-3 py-2">{evento.notes}</p> : null}

            {evento.status === "concluido" && evento.done_at ? (
              <p className="text-xs text-muted-foreground">Concluído em {formatarInstante(evento.done_at)}.</p>
            ) : null}
            {evento.status === "cancelado" && evento.cancelled_at ? (
              <p className="text-xs text-muted-foreground">Desmarcado em {formatarInstante(evento.cancelled_at)}.</p>
            ) : null}

            {evento.origin === "reservation" ? (
              <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
                <Lock aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
                Nasceu da reserva e a segue: a unidade e o hóspede vêm dela. Daqui mudam o horário, o responsável e a
                anotação.
              </p>
            ) : null}

            {derivado ? (
              <div className="flex flex-col gap-2 rounded-lg border border-border/60 bg-muted/30 px-3 py-2.5">
                <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
                  <Lock aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
                  {evento.origin === "maintenance"
                    ? "Vem da ordem de manutenção, e é só leitura aqui: o período muda na ordem."
                    : "Vem de um bloqueio de unidade, e é só leitura aqui: ele muda no mapa de ocupação."}
                </p>
                {evento.origin === "maintenance" && evento.maintenance_order_id && destinos.manutencao ? (
                  <Link
                    href={`/app/manutencao/${evento.maintenance_order_id}`}
                    className="inline-flex items-center gap-1.5 self-start text-sm font-medium text-primary underline-offset-4 hover:underline"
                  >
                    <ExternalLink aria-hidden="true" className="size-3.5" />
                    Abrir a ordem de manutenção
                  </Link>
                ) : null}
                {evento.origin === "stay_block" && destinos.mapa ? (
                  <Link
                    href={`/app/mapa?from=${evento.date}`}
                    className="inline-flex items-center gap-1.5 self-start text-sm font-medium text-primary underline-offset-4 hover:underline"
                  >
                    <CalendarRange aria-hidden="true" className="size-3.5" />
                    Abrir o bloqueio no mapa
                  </Link>
                ) : null}
              </div>
            ) : null}

            {erro ? (
              <p role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {erro}
              </p>
            ) : null}
          </div>
        )
      ) : null}
    </ModalShell>
  );
}

function Dado({ rotulo, children }: { rotulo: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{rotulo}</dt>
      <dd className="mt-0.5">{children}</dd>
    </div>
  );
}
