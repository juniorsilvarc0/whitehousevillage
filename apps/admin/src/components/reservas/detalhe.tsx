import * as React from "react";
import Link from "next/link";
import { BedDouble, Globe, Lock, Mail, MessageCircle, PartyPopper, Phone, Undo2, UserRound, Users } from "lucide-react";

import { EstadoVazio } from "@/components/layout/estados";
import { Nota } from "@/components/layout/tela";
import { AcoesDaReserva } from "@/components/reservas/acoes-da-reserva";
import { EtiquetaDeEstado } from "@/components/reservas/etiqueta-de-estado";
import { LinhaDoTempo } from "@/components/reservas/linha-do-tempo";
import { FaixaDePrazo } from "@/components/reservas/prazo-do-hold";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import type { Produto, UnidadeDaComposicao } from "@/lib/api/comercial";
import { classeDoTipo, rotuloDoTipo } from "@/lib/comercial/tipos-de-data";
import { formatarTelefone } from "@/lib/contatos/telefone";
import { linkDoWhatsApp, mensagemDaReserva } from "@/lib/contatos/whatsapp";
import { formatarData, formatarDataCurta, formatarInstante } from "@/lib/datas";
import { formatarBRL, formatarPct } from "@/lib/dinheiro";
import { rotuloDaOrigem } from "@/lib/origem";
import { ESTADOS, rotuloDoMotivo } from "@/lib/reservas/estados";
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
            {r.source ? ` · veio por ${rotuloDaOrigem(r.source)}` : ""}
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

      <Cliente completo={completo} />

      <p className="text-sm text-muted-foreground">{ESTADOS[r.status].explicacao}</p>

      {r.hold_expires_at ? <FaixaDePrazo expiraEm={r.hold_expires_at} /> : null}

      {r.rebooked_from_id ? (
        <Nota>
          Esta reserva nasceu de uma <strong>remarcação</strong>.{" "}
          <Link href={`/app/reservas/${r.rebooked_from_id}`} className="text-primary underline-offset-4 hover:underline">
            Abrir a reserva anterior
          </Link>{" "}
          — ela continua guardada, cancelada com o motivo “remarcação”, para a venda original não sumir
          do histórico.
        </Nota>
      ) : null}

      {r.cancelled_at ? (
        <Nota variante="atencao">
          Cancelada em <strong>{formatarInstante(r.cancelled_at)}</strong>
          {r.cancel_reason ? (
            <>
              {" "}
              com o motivo <strong>{rotuloDoMotivo(r.cancel_reason)}</strong>
            </>
          ) : null}
          . As datas foram liberadas; a reserva continua no histórico.
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
          · regras de cancelamento versão {previa.policy_version}
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
        É só uma simulação: nada foi feito. O motivo escolhido no cancelamento pode mudar estes números —
        em caso de <strong>não comparecimento</strong>, vale a regra de menor antecedência.
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
        Valores combinados na venda. Mudar os preços amanhã não altera nada disto.
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
        <Valor rotulo="Saldo" centavos={r.balance_cents} nota="Vence antes do check-in, conforme a política comercial." />
      </dl>
      <p className="mt-3 text-xs text-muted-foreground">
        Contas a receber, comissões e o acerto final ficarão na tela Financeiro, que ainda está sendo feita.
        Por enquanto, os pagamentos aparecem no histórico da reserva.
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
        O preço que <strong>foi cobrado</strong> em cada noite, combinado na venda — não o que a tabela de
        preços diz hoje.
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
              <caption className="sr-only">Cada noite com o tipo de data e o preço cobrado</caption>
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
          Esta reserva ainda não tem noites detalhadas — isso só acontece em rascunho, antes de o preço ser
          fechado.
        </p>
      )}
    </section>
  );
}

function Unidades({ completo }: { completo: ReservaCompleta }) {
  return (
    <section aria-label="Apartamentos" className="rounded-xl border border-border/60 bg-muted/20 p-4">
      <h2 className="font-display text-base">Apartamentos</h2>
      {completo.units.length === 0 ? (
        <p className="mt-2 text-sm text-muted-foreground">
          Nenhum apartamento reservado — esta reserva não está ocupando datas no calendário.
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
                  <Badge variant="outline" title="A gestão fixou este apartamento: o sistema não vai trocá-lo sozinho.">
                    <Lock aria-hidden="true" />
                    Travada
                  </Badge>
                ) : null}
              </li>
            ))}
          </ul>
          {completo.units.length > 1 ? (
            <p className="mt-2 text-xs text-muted-foreground">
              Casa inteira: este produto ocupa todos os apartamentos, por isso não dá para trocar de
              apartamento.
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
        É normal ter menos nomes que hóspedes: a venda é para{" "}
        {completo.reservation.guests_count}, e nem todo mundo enviou os dados ainda.
      </p>

      {completo.guests.length === 0 ? (
        <EstadoVazio
          className="mt-3"
          icone={Users}
          titulo="Nenhum nome informado"
          descricao="A lista de hóspedes é o que a portaria usa no dia da chegada para conferir quem entra."
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
                <span className="flex items-center gap-3">
                  <a
                    href={`tel:${hospede.phone_e164}`}
                    className="flex items-center gap-1.5 font-mono text-xs text-muted-foreground hover:text-foreground"
                  >
                    <Phone className="size-3.5" aria-hidden="true" />
                    {formatarTelefone(hospede.phone_e164)}
                  </a>
                  <LinkDoWhatsApp
                    telefone={hospede.phone_e164}
                    texto={mensagemDaReserva(hospede.name, completo.reservation.code)}
                    compacto
                  />
                </span>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/**
 * Quem reservou — no topo, porque é a primeira coisa que a gestão faz com uma
 * pré-reserva: falar com o cliente para receber o sinal.
 *
 * O titular sai da rooming list (que já traz telefone e e-mail cheios e grava o
 * rastro em `pii_access_log` na leitura do `/full`); sem rooming list, sobra o
 * nome do contato da reserva e o link para a ficha.
 */
function Cliente({ completo }: { completo: ReservaCompleta }) {
  const r = completo.reservation;
  const titular = completo.guests.find((h) => h.is_lead_guest) ?? completo.guests[0] ?? null;
  const nome = titular?.name ?? r.contact_name;
  const telefone = titular?.phone_e164 ?? null;
  const email = titular?.email ?? null;
  const contatoId = titular?.contact_id ?? r.contact_id;

  return (
    <section
      aria-label="Cliente"
      className="flex flex-wrap items-center justify-between gap-4 rounded-xl border border-border/60 bg-card p-4"
    >
      <div className="flex min-w-0 items-start gap-3">
        <span className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-full bg-muted">
          <UserRound className="size-4" aria-hidden="true" />
        </span>
        <div className="min-w-0">
          <p className="text-xs uppercase tracking-wide text-muted-foreground">Cliente</p>
          <p className="truncate text-base font-medium">{nome}</p>
          <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
            {telefone ? (
              <a href={`tel:${telefone}`} className="flex items-center gap-1.5 font-mono hover:text-foreground">
                <Phone className="size-3.5" aria-hidden="true" />
                {formatarTelefone(telefone)}
              </a>
            ) : (
              <span>Sem telefone na ficha</span>
            )}
            {email ? (
              <a href={`mailto:${email}`} className="flex items-center gap-1.5 hover:text-foreground">
                <Mail className="size-3.5" aria-hidden="true" />
                {email}
              </a>
            ) : null}
            {r.source === "site" ? (
              <span className="flex items-center gap-1.5">
                <Globe className="size-3.5" aria-hidden="true" />
                Reservou pelo site
              </span>
            ) : null}
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {telefone ? <LinkDoWhatsApp telefone={telefone} texto={mensagemDaReserva(nome, r.code)} /> : null}
        <Link href={`/app/contatos/${contatoId}`} className={buttonVariants({ variant: "outline", size: "md" })}>
          Ver ficha
        </Link>
      </div>
    </section>
  );
}

/**
 * Abre a conversa do WhatsApp com o número do hóspede e uma primeira mensagem
 * citando o código da reserva. Número fora de E.164 não ganha botão.
 */
function LinkDoWhatsApp({ telefone, texto, compacto }: { telefone: string; texto: string; compacto?: boolean }) {
  const href = linkDoWhatsApp(telefone, texto);
  if (!href) return null;
  if (compacto) {
    return (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="flex items-center gap-1 text-xs font-medium text-[#1f8f4e] hover:underline"
        aria-label="Abrir conversa no WhatsApp"
      >
        <MessageCircle className="size-3.5" aria-hidden="true" />
        WhatsApp
      </a>
    );
  }
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className={cn(buttonVariants({ size: "md" }), "bg-none bg-[#1f8f4e] text-white hover:brightness-110")}
    >
      <MessageCircle aria-hidden="true" />
      Chamar no WhatsApp
    </a>
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
    <section aria-label="Regras desta reserva" className="rounded-xl border border-border/60 bg-muted/20 p-4">
      <h2 className="font-display text-base">Regras que valem para esta reserva</h2>
      <dl className="mt-3 flex flex-col gap-2 text-sm">
        <div>
          <dt className="text-xs text-muted-foreground">Política comercial</dt>
          <dd className="tabular-nums">{r.policy_version !== null ? `versão ${r.policy_version}` : "—"}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Política de cancelamento</dt>
          <dd title={r.cancellation_policy_id ?? undefined}>
            {r.cancellation_policy_id ? "a que valia no dia da reserva" : "—"}
          </dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Tabela de preços</dt>
          <dd title={r.rate_table_id ?? undefined}>{r.rate_table_id ? "a que valia no dia da reserva" : "—"}</dd>
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
        Mudar as regras amanhã não altera esta reserva — num cancelamento, valem as regras do dia em que ela
        foi feita.
      </p>
    </section>
  );
}
