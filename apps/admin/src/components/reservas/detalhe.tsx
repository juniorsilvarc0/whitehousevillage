import * as React from "react";
import Link from "next/link";
import { BedDouble, Lock, PartyPopper, Phone, Undo2, Users } from "lucide-react";

import { EstadoVazio } from "@/components/layout/estados";
import { Nota } from "@/components/layout/tela";
import { AcoesDaReserva } from "@/components/reservas/acoes-da-reserva";
import { EtiquetaDeEstado } from "@/components/reservas/etiqueta-de-estado";
import { LinhaDoTempo } from "@/components/reservas/linha-do-tempo";
import { FaixaDePrazo } from "@/components/reservas/prazo-do-hold";
import { Badge } from "@/components/ui/badge";
import type { Produto, UnidadeDaComposicao } from "@/lib/api/comercial";
import { classeDoTipo, rotuloDoTipo } from "@/lib/comercial/tipos-de-data";
import { formatarData, formatarDataCurta, formatarInstante } from "@/lib/datas";
import { formatarBRL, formatarPct } from "@/lib/dinheiro";
import { ESTADOS } from "@/lib/reservas/estados";
import type { ReservaCompleta } from "@/lib/reservas/tipos";
import { cn } from "@/lib/utils";

/**
 * A tela da reserva — tudo de **uma** chamada (`GET /reservations/{id}/full`).
 *
 * Os totais e as noites vêm do snapshot da venda, não de um recálculo: mostrar
 * o preço de hoje para uma estadia vendida em março é exatamente o que
 * `reservation_nights` existe para impedir. O painel não soma, não desconta e
 * não arredonda nada nesta tela — cada número exibido é um campo que o servidor
 * congelou.
 */
export function DetalheDaReserva({
  completo,
  permissoes,
  produtos,
  composicao,
}: {
  completo: ReservaCompleta;
  permissoes: { editar: boolean; excluir: boolean };
  produtos: Produto[];
  composicao: UnidadeDaComposicao[];
}) {
  const r = completo.reservation;

  return (
    <>
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="font-display font-mono text-2xl leading-tight sm:text-3xl">{r.code}</h1>
            <EtiquetaDeEstado estado={r.status} />
            {r.is_event ? (
              <Badge variant="outline">
                <PartyPopper aria-hidden="true" />
                Evento{r.event_type ? ` · ${r.event_type}` : ""}
              </Badge>
            ) : null}
          </div>
          <p className="mt-1 text-sm">
            {r.unit_type_name} · {r.contact_name}
          </p>
          <p className="mt-1 text-sm text-muted-foreground">
            {formatarData(r.check_in)} → {formatarData(r.check_out)} ·{" "}
            <span className="tabular-nums">{r.night_count}</span> noite{r.night_count === 1 ? "" : "s"} ·{" "}
            <span className="tabular-nums">{r.guests_count}</span> hóspede{r.guests_count === 1 ? "" : "s"}
            {r.source ? ` · origem ${r.source}` : ""}
          </p>
        </div>

        <div className="flex shrink-0 flex-col items-end gap-3">
          <p className="font-mono text-2xl tabular-nums">{formatarBRL(r.total_cents)}</p>
          <AcoesDaReserva
            reserva={r}
            permissoes={permissoes}
            previaDeCancelamento={completo.cancellation_preview}
            produtos={produtos}
            composicao={composicao}
          />
        </div>
      </header>

      <p className="text-sm text-muted-foreground">{ESTADOS[r.status].explicacao}</p>

      {r.hold_expires_at ? <FaixaDePrazo expiraEm={r.hold_expires_at} /> : null}

      {r.rebooked_from_id ? (
        <Nota>
          Esta reserva nasceu de uma <strong>remarcação</strong>.{" "}
          <Link href={`/app/reservas/${r.rebooked_from_id}`} className="text-primary underline-offset-4 hover:underline">
            Abrir a reserva anterior
          </Link>{" "}
          — ela continua existindo, cancelada com motivo “remarcação”, para o histórico e o relatório não
          perderem a venda original.
        </Nota>
      ) : null}

      {r.cancelled_at ? (
        <Nota variante="atencao">
          Cancelada em <strong>{formatarInstante(r.cancelled_at)}</strong>
          {r.cancel_reason ? (
            <>
              {" "}
              com o motivo <strong>{r.cancel_reason}</strong>
            </>
          ) : null}
          . A data foi liberada; o registro fica.
        </Nota>
      ) : null}

      {completo.cancellation_preview ? <PreviaDeCancelamento completo={completo} /> : null}

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <div className="flex flex-col gap-6">
          <Noites completo={completo} />
          <Hospedes completo={completo} />
          <LinhaDoTempo eventos={completo.timeline} />
        </div>

        <div className="flex flex-col gap-6">
          <Financeiro completo={completo} />
          <Unidades completo={completo} />
          <Snapshots completo={completo} />
        </div>
      </div>

      {r.notes ? (
        <section aria-label="Observações" className="rounded-xl border border-border/60 bg-muted/20 p-4">
          <h2 className="font-display text-base">Observações</h2>
          <p className="mt-2 whitespace-pre-wrap text-sm text-muted-foreground">{r.notes}</p>
        </section>
      ) : null}
    </>
  );
}

/**
 * O que aconteceria se a reserva fosse cancelada **agora**.
 *
 * Vem pronto do `/full` — é o mesmo cálculo do `?dry_run=1`, sem motivo. Está no
 * topo da tela de propósito: é a resposta à pergunta que o hóspede faz por
 * telefone antes de qualquer outra, e obrigá-la a passar por um diálogo faria a
 * gestão abrir o modal de cancelamento só para consultar um número — o que é
 * uma forma muito ruim de aprender a consultar.
 */
function PreviaDeCancelamento({ completo }: { completo: ReservaCompleta }) {
  const previa = completo.cancellation_preview!;
  return (
    <section
      aria-label="Simulação de cancelamento"
      className="rounded-xl border border-border/60 bg-muted/20 px-4 py-3"
    >
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="font-display text-base">Se cancelasse agora: {previa.label}</h2>
        <span className="text-xs text-muted-foreground">
          {previa.days_before >= 0
            ? `${previa.days_before} dias de antecedência`
            : `${Math.abs(previa.days_before)} dias depois do check-in`}{" "}
          · política de cancelamento v{previa.policy_version}
        </span>
      </div>
      <dl className="mt-2 flex flex-wrap gap-x-8 gap-y-2 text-sm">
        <div className="flex items-baseline gap-2">
          <dt className="flex items-center gap-1.5 text-muted-foreground">
            <Undo2 className="size-3.5" aria-hidden="true" />
            Volta para o hóspede
          </dt>
          <dd className="font-mono tabular-nums">{formatarBRL(previa.refund_cents)}</dd>
        </div>
        <div className="flex items-baseline gap-2">
          <dt className="text-muted-foreground">Fica com a casa</dt>
          <dd className="font-mono tabular-nums">{formatarBRL(previa.retained_cents)}</dd>
        </div>
        <div className="flex items-baseline gap-2">
          <dt className="text-muted-foreground">Sinal recebido</dt>
          <dd className="font-mono tabular-nums">{formatarBRL(previa.deposit_paid_cents)}</dd>
        </div>
        {previa.credit_cents > 0 ? (
          <div className="flex items-baseline gap-2">
            <dt className="text-muted-foreground">Crédito em aberto</dt>
            <dd className="font-mono tabular-nums">{formatarBRL(previa.credit_cents)}</dd>
          </div>
        ) : null}
      </dl>
      <p className="mt-2 text-xs text-muted-foreground">
        {previa.deposit_paid_cents === 0
          ? "Nada foi recebido ainda: não há o que devolver nem o que reter. "
          : ""}
        Simulação: nada foi executado. O motivo escolhido no cancelamento pode mudar estes números —{" "}
        <strong>não comparecimento</strong> aplica a faixa de menor antecedência.
      </p>
    </section>
  );
}

function Financeiro({ completo }: { completo: ReservaCompleta }) {
  const r = completo.reservation;
  return (
    <section aria-label="Financeiro" className="rounded-xl border border-border/60 bg-muted/20 p-4">
      <h2 className="font-display text-base">Financeiro</h2>
      <p className="mt-1 text-xs text-muted-foreground">
        Congelado na venda. Mudar o tarifário amanhã não reescreve nada disto.
      </p>
      <dl className="mt-3 text-sm">
        <Valor rotulo="Subtotal das diárias" centavos={r.subtotal_cents} />
        {r.discount_cents > 0 ? (
          <Valor
            rotulo={`Desconto (${formatarPct(r.discount_pct)})`}
            centavos={r.discount_cents}
            negativo
            nota="Incide só sobre as diárias."
          />
        ) : null}
        <Valor rotulo="Taxa de limpeza" centavos={r.cleaning_cents} nota="Uma vez por estadia." />
        {r.event_deposit_cents > 0 ? (
          <Valor rotulo="Caução de evento" centavos={r.event_deposit_cents} nota="Reembolsável." />
        ) : null}
        <Valor rotulo="Total" centavos={r.total_cents} destaque />
        <Valor rotulo="Sinal para confirmar" centavos={r.deposit_cents} />
        <Valor rotulo="Saldo" centavos={r.balance_cents} nota="Vence antes do check-in, pela política." />
      </dl>
      <p className="mt-3 text-xs text-muted-foreground">
        Recebíveis, comissões e o acerto final são do módulo financeiro — a Fase 1 registra o pagamento na
        linha do tempo.
      </p>
    </section>
  );
}

function Valor({
  rotulo,
  centavos,
  nota,
  destaque = false,
  negativo = false,
}: {
  rotulo: React.ReactNode;
  centavos: number;
  nota?: string;
  destaque?: boolean;
  negativo?: boolean;
}) {
  return (
    <div className={cn("flex items-baseline justify-between gap-4 py-1", destaque && "border-t border-border/60 pt-2")}>
      <div className="min-w-0">
        <dt className={cn(destaque && "font-display")}>{rotulo}</dt>
        {nota ? <dd className="text-[0.7rem] leading-snug text-muted-foreground">{nota}</dd> : null}
      </div>
      <dd className={cn("shrink-0 font-mono tabular-nums", destaque ? "text-lg" : "text-sm")}>
        {negativo && centavos > 0 ? "−" : ""}
        {formatarBRL(centavos)}
      </dd>
    </div>
  );
}

function Noites({ completo }: { completo: ReservaCompleta }) {
  return (
    <section aria-label="Noites e tarifas" className="rounded-xl border border-border/60 bg-muted/20 p-4">
      <h2 className="font-display text-base">Noites e tarifas aplicadas</h2>
      <p className="mt-1 text-xs text-muted-foreground">
        O preço que <strong>foi</strong> aplicado a cada noite, não o que a tabela diz hoje. É deste snapshot
        que saem auditoria, financeiro e BI.
      </p>

      {completo.lines.length > 0 ? (
        <div className="mt-3 overflow-x-auto rounded-lg border border-border/60 bg-card/60">
          <table className="w-full min-w-[26rem] border-collapse text-sm">
            <caption className="sr-only">Noites agrupadas por tipo de tarifa</caption>
            <thead>
              <tr className="border-b border-border/60 text-xs text-muted-foreground">
                <th scope="col" className="px-3 py-2 text-left font-medium">Tipo de noite</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Noites</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Diária</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Subtotal</th>
              </tr>
            </thead>
            <tbody>
              {completo.lines.map((linha) => (
                <tr key={linha.date_type} className="border-b border-border/40 last:border-b-0">
                  <td className="px-3 py-2">
                    <span className={cn("rounded-full px-2 py-0.5 text-[0.7rem]", classeDoTipo(linha.date_type))}>
                      {linha.label || rotuloDoTipo(linha.date_type)}
                    </span>
                  </td>
                  <td className="px-3 py-2 text-right tabular-nums">{linha.nights}</td>
                  <td className="px-3 py-2 text-right font-mono tabular-nums">{formatarBRL(linha.unit_price_cents)}</td>
                  <td className="px-3 py-2 text-right font-mono tabular-nums">{formatarBRL(linha.subtotal_cents)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {completo.nights.length > 0 ? (
        <details className="mt-3 rounded-lg border border-border/60 bg-card/60">
          <summary className="cursor-pointer select-none px-3 py-2 text-sm text-muted-foreground">
            Noite a noite ({completo.nights.length})
          </summary>
          <div className="max-h-72 overflow-y-auto border-t border-border/60">
            <table className="w-full border-collapse text-sm">
              <caption className="sr-only">Cada noite com o tipo de data e o preço congelado</caption>
              <tbody>
                {completo.nights.map((noite) => (
                  <tr key={noite.night} className="border-b border-border/30 last:border-b-0">
                    <td className="px-3 py-1.5 tabular-nums">{formatarDataCurta(noite.night)}</td>
                    <td className="px-3 py-1.5">
                      <span className={cn("rounded-full px-2 py-0.5 text-[0.65rem]", classeDoTipo(noite.date_type))}>
                        {rotuloDoTipo(noite.date_type)}
                      </span>
                    </td>
                    <td className="px-3 py-1.5 text-right font-mono tabular-nums">
                      {formatarBRL(noite.price_cents)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      ) : (
        <p className="mt-3 text-sm text-muted-foreground">
          Esta reserva não tem noites detalhadas — o que só acontece em rascunho, antes de o motor congelar o
          preço.
        </p>
      )}
    </section>
  );
}

function Unidades({ completo }: { completo: ReservaCompleta }) {
  return (
    <section aria-label="Unidades alocadas" className="rounded-xl border border-border/60 bg-muted/20 p-4">
      <h2 className="font-display text-base">Unidades alocadas</h2>
      {completo.units.length === 0 ? (
        <p className="mt-2 text-sm text-muted-foreground">
          Nenhuma unidade alocada — a reserva não está segurando calendário.
        </p>
      ) : (
        <>
          <ul className="mt-3 flex flex-col gap-2">
            {completo.units.map((unidade) => (
              <li
                key={unidade.unit_id}
                className="flex items-center justify-between gap-3 rounded-lg border border-border/60 bg-card px-3 py-2"
              >
                <div className="min-w-0">
                  <p className="flex items-center gap-1.5 font-mono text-sm">
                    <BedDouble className="size-3.5 text-muted-foreground" aria-hidden="true" />
                    {unidade.unit_code}
                  </p>
                  <p className="mt-0.5 text-xs text-muted-foreground">{unidade.unit_name}</p>
                </div>
                {unidade.locked ? (
                  <Badge variant="outline" title="Fora da realocação automática — a gestão travou a escolha.">
                    <Lock aria-hidden="true" />
                    Travada
                  </Badge>
                ) : null}
              </li>
            ))}
          </ul>
          {completo.units.length > 1 ? (
            <p className="mt-2 text-xs text-muted-foreground">
              A casa inteira: este produto ocupa todas as unidades da composição, e por isso não realoca — não
              há para onde mover.
            </p>
          ) : null}
        </>
      )}
    </section>
  );
}

function Hospedes({ completo }: { completo: ReservaCompleta }) {
  return (
    <section aria-label="Hóspedes" className="rounded-xl border border-border/60 bg-muted/20 p-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="font-display text-base">Hóspedes</h2>
        <span className="text-xs text-muted-foreground">
          <span className="tabular-nums">{completo.guests.length}</span> nome
          {completo.guests.length === 1 ? "" : "s"} de{" "}
          <span className="tabular-nums">{completo.reservation.guests_count}</span> vendido
          {completo.reservation.guests_count === 1 ? "" : "s"}
        </span>
      </div>
      <p className="mt-1 text-xs text-muted-foreground">
        A lista de nomes e a quantidade vendida podem divergir de propósito: a venda é de{" "}
        {completo.reservation.guests_count}, e nem todo mundo se identificou ainda.
      </p>

      {completo.guests.length === 0 ? (
        <EstadoVazio
          className="mt-3"
          icone={Users}
          titulo="Nenhum nome informado"
          descricao="A rooming list é o que a portaria usa no dia da chegada. Sem ela, quem chega não é conferido contra nada."
        />
      ) : (
        <ul className="mt-3 flex flex-col gap-2">
          {completo.guests.map((hospede) => (
            <li
              key={hospede.contact_id}
              className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border/60 bg-card px-3 py-2 text-sm"
            >
              <span className="min-w-0">
                {hospede.name}
                {hospede.is_lead_guest ? (
                  <Badge variant="outline" className="ml-2">
                    Titular
                  </Badge>
                ) : null}
              </span>
              {hospede.phone_e164 ? (
                <a
                  href={`tel:${hospede.phone_e164}`}
                  className="flex items-center gap-1.5 font-mono text-xs text-muted-foreground hover:text-foreground"
                >
                  <Phone className="size-3.5" aria-hidden="true" />
                  {hospede.phone_e164}
                </a>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/**
 * O que a reserva **congelou** — e por que isso aparece na tela.
 *
 * Tabela de tarifas, versão da política comercial e política de cancelamento
 * são a resposta a "por que essa reserva devolve 50% e aquela devolve tudo".
 * Sem os três à vista, a diferença parece defeito do sistema em vez de decisão
 * registrada no dia da venda.
 */
function Snapshots({ completo }: { completo: ReservaCompleta }) {
  const r = completo.reservation;
  return (
    <section aria-label="Snapshots" className="rounded-xl border border-border/60 bg-muted/20 p-4">
      <h2 className="font-display text-base">O que esta reserva congelou</h2>
      <dl className="mt-3 flex flex-col gap-2 text-sm">
        <div>
          <dt className="text-xs text-muted-foreground">Política comercial</dt>
          <dd className="tabular-nums">{r.policy_version !== null ? `versão ${r.policy_version}` : "—"}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Política de cancelamento</dt>
          <dd className="truncate font-mono text-xs">{r.cancellation_policy_id ?? "—"}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Tabela de tarifas</dt>
          <dd className="truncate font-mono text-xs">{r.rate_table_id ?? "—"}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Criada em</dt>
          <dd className="tabular-nums">{formatarInstante(r.created_at)}</dd>
        </div>
        {r.confirmed_at ? (
          <div>
            <dt className="text-xs text-muted-foreground">Confirmada em</dt>
            <dd className="tabular-nums">{formatarInstante(r.confirmed_at)}</dd>
          </div>
        ) : null}
      </dl>
      <p className="mt-3 text-xs text-muted-foreground">
        Publicar uma política nova amanhã não altera nada aqui — é esta versão que o cancelamento aplica.
      </p>
    </section>
  );
}
