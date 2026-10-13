import {
  Ban,
  CircleCheck,
  DoorOpen,
  LogIn,
  LogOut,
  PartyPopper,
  SprayCan,
  TriangleAlert,
  Wrench,
  XCircle,
  type LucideIcon,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { ROTULO_DO_STATUS, ROTULO_DO_TIPO } from "@/lib/agenda/rotulos";
import type { StatusDoEvento, TipoDeEvento } from "@/lib/agenda/tipos";
import { cn } from "@/lib/utils";

/**
 * Os selos da agenda. Só tokens da casca: o conflito usa o vermelho de
 * `destructive`, o concluído fica neutro, o desmarcado vira contorno — cor
 * demais é cor nenhuma.
 *
 * Os ícones dizem o que a operação FAZ: entra (check-in), sai (check-out),
 * limpa, conserta, mostra a casa (visita), celebra (evento), fecha (bloqueio).
 */
export const ICONE_DO_TIPO: Record<TipoDeEvento, LucideIcon> = {
  checkin: LogIn,
  checkout: LogOut,
  limpeza: SprayCan,
  manutencao: Wrench,
  visita: DoorOpen,
  evento: PartyPopper,
  bloqueio: Ban,
};

export function IconeDoTipo({ tipo, className }: { tipo: TipoDeEvento; className?: string }) {
  const Icone = ICONE_DO_TIPO[tipo];
  return <Icone aria-hidden="true" className={cn("size-3.5 shrink-0", className)} />;
}

/** O status do evento — agendado, concluído ou desmarcado. */
export function SeloDeStatus({ status, className }: { status: StatusDoEvento; className?: string }) {
  if (status === "concluido") {
    return (
      <Badge variant="neutral" className={className}>
        <CircleCheck aria-hidden="true" />
        {ROTULO_DO_STATUS.concluido}
      </Badge>
    );
  }
  if (status === "cancelado") {
    return (
      <Badge variant="outline" className={className}>
        <XCircle aria-hidden="true" />
        {ROTULO_DO_STATUS.cancelado}
      </Badge>
    );
  }
  return (
    <Badge variant="accent" className={className}>
      {ROTULO_DO_STATUS.agendado}
    </Badge>
  );
}

/**
 * `has_unit_conflict` — o evento cruza outra ocupação da mesma unidade. A
 * agenda nunca trava a venda: quando a venda, a manutenção ou o ajuste de um
 * automático chega depois, o conflito aparece aqui em vez de sumir.
 */
export function SeloDeConflito({ className }: { className?: string }) {
  return (
    <Badge
      variant="destructive"
      className={className}
      title="Outra coisa ocupa a mesma unidade neste horário. Mude o horário de um dos dois."
    >
      <TriangleAlert aria-hidden="true" />
      Conflito de unidade
    </Badge>
  );
}

/** O tipo por extenso, com o ícone — para o detalhe e a legenda. */
export function SeloDoTipo({ tipo, className }: { tipo: TipoDeEvento; className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-xs font-medium", className)}>
      <span className="agenda-ponto inline-block size-2 rounded-full" data-tipo={tipo} aria-hidden="true" />
      <IconeDoTipo tipo={tipo} />
      {ROTULO_DO_TIPO[tipo]}
    </span>
  );
}
