"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { ChevronLeft, ChevronRight, PartyPopper } from "lucide-react";

import { BotaoWhatsApp, TelefoneClicavel } from "@/components/contatos/botao-whatsapp";
import { EstadoVazio } from "@/components/layout/estados";
import { AcoesDaReserva } from "@/components/reservas/acoes-da-reserva";
import { EtiquetaDeEstado } from "@/components/reservas/etiqueta-de-estado";
import { ContadorDeHold } from "@/components/reservas/prazo-do-hold";
import { Button, buttonVariants } from "@/components/ui/button";
import type { Meta } from "@/lib/api/types";
import { formatarData } from "@/lib/datas";
import { formatarBRL } from "@/lib/dinheiro";
import { aplicar, temRecorte, type FiltrosDeReservas } from "@/lib/reservas/filtros";
import type { Reserva } from "@/lib/reservas/tipos";
import { cn } from "@/lib/utils";

/**
 * A lista de reservas.
 *
 * ## Uma tabela, e não cartões
 *
 * A pergunta que esta tela responde é comparativa — "qual pré-reserva vence
 * primeiro", "quanto dessas dez ainda não foi pago" —, e comparar números
 * exige que eles fiquem na mesma coluna, alinhados à direita, em
 * `tabular-nums`. Cartão empilhado é bonito e obriga o olho a caçar o valor em
 * posição diferente a cada linha.
 *
 * No celular a mesma tabela rola dentro do próprio contêiner (`overflow-x`), em
 * vez de virar outro componente: dois desenhos do mesmo dado são dois lugares
 * para a regra de negócio divergir.
 *
 * ## O que cada linha mostra, e por quê
 *
 * Código (é por ele que se fala da reserva ao telefone), produto, hóspede,
 * datas com a contagem de noites, valor total, **sinal** e situação. O sinal
 * está ali porque é ele que separa "a data está segura" de "a data vence
 * quinta-feira" — e a coluna de saldo mostra o que ainda falta entrar.
 */
export function ListaDeReservas({
  reservas,
  meta,
  filtros,
  permissoes,
  caminho = "/app/reservas",
}: {
  reservas: Reserva[];
  meta: Meta;
  filtros: FiltrosDeReservas;
  permissoes: { editar: boolean; excluir: boolean };
  caminho?: string;
}) {
  if (reservas.length === 0) {
    return <ListaVazia filtrada={temRecorte(filtros)} caminho={caminho} />;
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="overflow-x-auto rounded-xl border border-border/60 bg-card/60">
        <table className="w-full min-w-[62rem] border-collapse text-sm">
          <caption className="sr-only">
            Reservas dos filtros atuais, com código, produto, hóspede, estadia, valores e situação
          </caption>
          <thead>
            <tr className="border-b border-border/60 text-xs text-muted-foreground">
              <th scope="col" className="px-3 py-2 text-left font-medium">Código</th>
              <th scope="col" className="px-3 py-2 text-left font-medium">Produto e hóspede</th>
              <th scope="col" className="px-3 py-2 text-left font-medium">Estadia</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">Total</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">Sinal</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">Saldo</th>
              <th scope="col" className="px-3 py-2 text-left font-medium">Situação</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">
                <span className="sr-only">Ações</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {reservas.map((reserva) => (
              <Linha key={reserva.id} reserva={reserva} permissoes={permissoes} />
            ))}
          </tbody>
        </table>
      </div>

      <Paginacao meta={meta} caminho={caminho} />
    </div>
  );
}

function Linha({ reserva, permissoes }: { reserva: Reserva; permissoes: { editar: boolean; excluir: boolean } }) {
  return (
    <tr className="border-b border-border/40 align-top last:border-b-0 hover:bg-muted/25">
      <td className="px-3 py-3">
        <Link href={`/app/reservas/${reserva.id}`} className="font-mono text-sm hover:underline">
          {reserva.code}
        </Link>
        {reserva.units.length > 0 ? (
          <p className="mt-0.5 font-mono text-[0.7rem] text-muted-foreground">
            {reserva.units.map((u) => u.unit_code).join(", ")}
          </p>
        ) : null}
      </td>

      <td className="px-3 py-3">
        <p className="flex items-center gap-1.5">
          {reserva.unit_type_name}
          {reserva.is_event ? (
            <PartyPopper className="size-3.5 text-muted-foreground" aria-label="Evento" />
          ) : null}
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">
          {reserva.contact_name} · {reserva.guests_count} hóspede{reserva.guests_count === 1 ? "" : "s"}
        </p>
        {reserva.contact_phone_e164 ? (
          <div className="mt-1.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <TelefoneClicavel telefone={reserva.contact_phone_e164} />
            <BotaoWhatsApp telefone={reserva.contact_phone_e164} nome={reserva.contact_name} codigo={reserva.code} />
          </div>
        ) : null}
      </td>

      <td className="px-3 py-3 whitespace-nowrap">
        <p className="tabular-nums">
          {formatarData(reserva.check_in)} → {formatarData(reserva.check_out)}
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">
          <span className="tabular-nums">{reserva.night_count}</span> noite
          {reserva.night_count === 1 ? "" : "s"}
        </p>
      </td>

      <td className="px-3 py-3 text-right font-mono tabular-nums">{formatarBRL(reserva.total_cents)}</td>
      <td className="px-3 py-3 text-right font-mono tabular-nums text-muted-foreground">
        {formatarBRL(reserva.deposit_cents)}
      </td>
      <td className="px-3 py-3 text-right font-mono tabular-nums">{formatarBRL(reserva.balance_cents)}</td>

      <td className="px-3 py-3">
        <EtiquetaDeEstado estado={reserva.status} />
        <ContadorDeHold expiraEm={reserva.hold_expires_at} />
      </td>

      <td className="px-3 py-3">
        <div className="flex justify-end">
          <AcoesDaReserva reserva={reserva} permissoes={permissoes} compacto />
        </div>
      </td>
    </tr>
  );
}

/**
 * A lista vazia diz **o que fazer**, e a frase muda conforme a causa.
 *
 * "Nenhuma reserva encontrada" para quem filtrou é uma acusação sem instrução;
 * a saída dali é limpar o filtro, não recarregar. Para quem não filtrou, o vazio
 * é o estado inicial da casa, e o caminho é o mapa — é de lá que sai a venda.
 */
function ListaVazia({ filtrada, caminho }: { filtrada: boolean; caminho: string }) {
  const router = useRouter();

  if (filtrada) {
    return (
      <EstadoVazio
        titulo="Nenhuma reserva com esses filtros"
        descricao="Nenhuma reserva com esses filtros. Confira as datas: o filtro procura estadias que passam por esse período, não reservas feitas nele."
        acao={
          <Button variant="outline" onClick={() => router.replace(caminho)}>
            Limpar os filtros
          </Button>
        }
      />
    );
  }

  return (
    <EstadoVazio
      titulo="Ainda não há reservas"
      descricao={
        <>
          A venda começa no mapa: escolha uma data livre, monte o orçamento e crie a pré-reserva, que guarda
          as datas pelo prazo da política comercial. Se o sinal não for pago nesse prazo, as datas voltam a
          ficar livres sozinhas.
        </>
      }
      acao={
        <Link href="/app/mapa" className="text-sm text-primary underline-offset-4 hover:underline">
          Abrir o mapa de ocupação
        </Link>
      }
    />
  );
}

/**
 * Paginação em links, não em estado.
 *
 * A página é o `page` da query string, como todo o resto do recorte: assim a
 * segunda página é um endereço, o botão de voltar do navegador funciona, e um
 * `router.refresh()` depois de confirmar uma reserva devolve exatamente a mesma
 * página em que a pessoa estava.
 */
function Paginacao({ meta, caminho }: { meta: Meta; caminho: string }) {
  const parametros = useSearchParams();

  if (meta.total_pages <= 1) {
    return (
      <p className="text-xs text-muted-foreground">
        <span className="tabular-nums">{meta.total}</span> reserva{meta.total === 1 ? "" : "s"} no total.
      </p>
    );
  }

  const anterior = meta.page > 1 ? `${caminho}${aplicar(parametros, "page", String(meta.page - 1))}` : null;
  const proxima =
    meta.page < meta.total_pages ? `${caminho}${aplicar(parametros, "page", String(meta.page + 1))}` : null;

  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <p className="text-xs text-muted-foreground">
        Página <span className="tabular-nums">{meta.page}</span> de{" "}
        <span className="tabular-nums">{meta.total_pages}</span> ·{" "}
        <span className="tabular-nums">{meta.total}</span> reserva{meta.total === 1 ? "" : "s"} no total.
      </p>
      <div className="flex items-center gap-2">
        <Passo href={anterior} rotulo="Página anterior" icone={ChevronLeft} />
        <Passo href={proxima} rotulo="Próxima página" icone={ChevronRight} />
      </div>
    </div>
  );
}

/**
 * Um passo da paginação. É `<Link>` quando há para onde ir e `<span>` quando
 * não há — nunca um `<a>` desativado: link sem destino continua focável pelo
 * teclado e anuncia "link" ao leitor de tela, prometendo uma página que não
 * existe.
 */
function Passo({
  href,
  rotulo,
  icone: Icone,
}: {
  href: string | null;
  rotulo: string;
  icone: typeof ChevronLeft;
}) {
  const classe = cn(buttonVariants({ variant: "outline", size: "iconSm" }), !href && "opacity-45");
  if (!href) {
    return (
      <span aria-hidden="true" className={classe}>
        <Icone />
      </span>
    );
  }
  return (
    <Link href={href} aria-label={rotulo} className={classe}>
      <Icone aria-hidden="true" />
    </Link>
  );
}
