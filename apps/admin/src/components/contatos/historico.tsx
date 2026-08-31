import Link from "next/link";
import { CalendarRange } from "lucide-react";

import { EstadoVazio } from "@/components/layout/estados";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { formatarData } from "@/lib/datas";
import { formatarBRL } from "@/lib/dinheiro";
import type { EstadoDaReserva, Reserva } from "@/lib/reservas/tipos";

/**
 * O histórico da pessoa — a metade da pergunta "quem é essa pessoa" que o
 * cadastro sozinho não responde.
 *
 * Nome e telefone dizem como falar com ela; isto aqui diz **o que ela já é para
 * a casa**: quantas vezes veio, quanto deixou, se está hospedada agora. Era o
 * dado que só existia por SQL.
 *
 * Componente de servidor: não há interação nenhuma aqui, e a lista já chega
 * pronta da API. Marcá-lo como cliente arrastaria `formatarBRL` e o mapa de
 * rótulos para o bundle sem trocar nada em tela.
 *
 * O `Reserva` vem de `lib/reservas/tipos.ts` de propósito — é o espelho do
 * contrato que o módulo de reservas mantém, e ter um segundo espelho aqui seria
 * ter duas verdades sobre o mesmo `GET /reservations`.
 */

const ROTULO_DO_ESTADO: Record<EstadoDaReserva, string> = {
  quote: "orçamento",
  hold: "pré-reserva",
  confirmed: "confirmada",
  checked_in: "hospedado",
  checked_out: "concluída",
  closed: "encerrada",
  cancelled: "cancelada",
  expired: "expirada",
  no_show: "não compareceu",
};

/** A cor separa o que **vale** do que não vale: pré-reserva e confirmada seguram
 *  data e dinheiro; cancelada e expirada não seguram nada. */
const COR_DO_ESTADO: Record<EstadoDaReserva, BadgeProps["variant"]> = {
  quote: "outline",
  hold: "accent",
  confirmed: "brand",
  checked_in: "brand",
  checked_out: "neutral",
  closed: "neutral",
  cancelled: "destructive",
  expired: "destructive",
  no_show: "destructive",
};

/** Estados que representam receita realizada ou contratada. Cancelada e expirada
 *  ficam de fora: somá-las diria que a pessoa gastou um dinheiro que ela não
 *  gastou. */
const CONTAM_NO_TOTAL: readonly EstadoDaReserva[] = [
  "confirmed",
  "checked_in",
  "checked_out",
  "closed",
];

export function HistoricoDoContato({ reservas }: { reservas: readonly Reserva[] }) {
  if (reservas.length === 0) {
    return (
      <EstadoVazio
        icone={CalendarRange}
        titulo="Ainda não se hospedou"
        descricao="Nenhuma reserva aponta para este contato — nem pré-reserva, nem cancelada."
      />
    );
  }

  const valendo = reservas.filter((r) => CONTAM_NO_TOTAL.includes(r.status));
  const totalCentavos = valendo.reduce((soma, r) => soma + r.total_cents, 0);
  const noites = valendo.reduce((soma, r) => soma + r.night_count, 0);

  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-muted-foreground">
        <strong className="text-foreground">{valendo.length}</strong>{" "}
        {valendo.length === 1 ? "estadia contratada" : "estadias contratadas"} ·{" "}
        <span className="tabular-nums">{noites}</span> {noites === 1 ? "noite" : "noites"} ·{" "}
        <span className="tabular-nums text-foreground">{formatarBRL(totalCentavos)}</span>
        {reservas.length > valendo.length ? (
          <> · {reservas.length - valendo.length} sem efeito (cancelada, expirada ou não compareceu)</>
        ) : null}
      </p>

      <div className="overflow-x-auto">
        <ul className="flex min-w-[36rem] flex-col gap-1.5">
          {reservas.map((reserva) => (
            <li
              key={reserva.id}
              className="flex items-center gap-3 rounded-xl border border-border/60 bg-card/70 px-3 py-2.5"
            >
              {/* A tela de reservas nasce nesta mesma rodada; o link já aponta
                  para o destino final. Enquanto ele não existir, o código da
                  reserva continua sendo lido e falado ao telefone. */}
              <Link
                href={`/app/reservas/${reserva.id}`}
                className="shrink-0 font-mono text-xs text-primary underline-offset-4 hover:underline"
              >
                {reserva.code}
              </Link>
              <span className="min-w-0 flex-1 truncate text-sm text-foreground">
                {reserva.unit_type_name}
              </span>
              <span className="shrink-0 text-xs text-muted-foreground">
                {formatarData(reserva.check_in)} → {formatarData(reserva.check_out)}
                <span className="ml-1.5 tabular-nums">
                  ({reserva.night_count} {reserva.night_count === 1 ? "noite" : "noites"})
                </span>
              </span>
              <span className="shrink-0 tabular-nums text-sm text-foreground">
                {formatarBRL(reserva.total_cents)}
              </span>
              <Badge variant={COR_DO_ESTADO[reserva.status]} className="shrink-0">
                {ROTULO_DO_ESTADO[reserva.status]}
              </Badge>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
