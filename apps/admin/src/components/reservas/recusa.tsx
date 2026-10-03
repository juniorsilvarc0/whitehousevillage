import * as React from "react";
import { CalendarX2, Info, TriangleAlert } from "lucide-react";

import { mensagemDoErro, type Falha } from "@/lib/acoes/resultado";
import { lerComposicaoIncompleta, lerConflito } from "@/lib/reservas/conflito";
import { ehEstadoDeReserva, ESTADOS } from "@/lib/reservas/estados";
import { cn } from "@/lib/utils";

/**
 * A recusa da API, dentro do gesto que a provocou.
 *
 * Duas decisões moram aqui:
 *
 * 1. **A tela reage ao `code`, nunca ao texto.** A API escreve para quem depura;
 *    esta função escreve para quem vende. Código novo no contrato cai no padrão
 *    em vez de deixar a tela muda.
 *
 * 2. **Conflito de data não é vermelho.** `DATE_CONFLICT` e `UNIT_NOT_AVAILABLE`
 *    são a resposta normal de um calendário disputado, e as duas vêm com a
 *    garantia de que **nada mudou**: a remarcação acontece numa transação só, e
 *    a reserva antiga continua de pé. Pintá-las como falha ensina o operador a
 *    ter medo do botão que ele mais usa em dezembro.
 */

const INFORMATIVOS = new Set(["DATE_CONFLICT", "UNIT_NOT_AVAILABLE"]);

export function Recusa({
  falha,
  /** O que a tela sabe que continua verdade depois da recusa. */
  garantia,
  className,
}: {
  falha: Falha;
  garantia?: React.ReactNode;
  className?: string;
}) {
  const informativo = INFORMATIVOS.has(falha.code);
  const conflito = informativo ? lerConflito(falha.details).frase : null;
  const composicao = falha.code === "COMPOSITION_INCOMPLETE" ? lerComposicaoIncompleta(falha.details) : null;
  const naoCancelavel = falha.code === "RESERVATION_NOT_CANCELLABLE" ? estadoAtual(falha.details) : null;
  const acima = falha.code === "VALIDATION_ERROR" ? tetoDoSinal(falha.details) : null;
  const limite = falha.code === "HOLD_LIMIT_REACHED" ? extensoes(falha.details) : null;

  const Icone = informativo ? CalendarX2 : falha.code === "NETWORK_ERROR" ? Info : TriangleAlert;

  return (
    <div
      role="alert"
      data-codigo={falha.code}
      title={`Código para o suporte: ${falha.code}`}
      className={cn(
        "flex items-start gap-2.5 rounded-lg border px-3 py-2.5 text-sm",
        informativo
          ? "border-alcada-atencao/40 bg-alcada-atencao/10 text-foreground"
          : "border-destructive/30 bg-destructive/8 text-foreground",
        className,
      )}
    >
      <Icone
        aria-hidden="true"
        className={cn("mt-0.5 size-4 shrink-0", informativo ? "text-alcada-atencao" : "text-destructive")}
      />
      <div className="min-w-0">
        <p>{mensagemDoErro(falha.code)}</p>
        {conflito ? <p className="mt-1 text-muted-foreground">{conflito}</p> : null}
        {composicao ? <p className="mt-1 text-muted-foreground">{composicao}</p> : null}
        {naoCancelavel ? <p className="mt-1 text-muted-foreground">{naoCancelavel}</p> : null}
        {acima ? <p className="mt-1 text-muted-foreground">{acima}</p> : null}
        {limite ? <p className="mt-1 text-muted-foreground">{limite}</p> : null}
        {informativo && garantia ? <p className="mt-1 text-muted-foreground">{garantia}</p> : null}
      </div>
    </div>
  );
}

/** `details.status` e `details.allowed`, como o contrato manda em `/cancel`. */
function estadoAtual(details: Record<string, unknown>): string | null {
  const status = typeof details.status === "string" ? details.status : null;
  if (!status) return null;
  const rotulo = ehEstadoDeReserva(status) ? ESTADOS[status].rotulo : status;
  return `A reserva está como "${rotulo}" e não pode voltar atrás.`;
}

/** `details.total_cents` e `details.max_cents` do `POST /confirm`. */
function tetoDoSinal(details: Record<string, unknown>): string | null {
  const max = typeof details.max_cents === "number" ? details.max_cents : null;
  if (max === null) return null;
  return `O valor recebido não pode passar do total da reserva (${(max / 100).toLocaleString("pt-BR", {
    style: "currency",
    currency: "BRL",
  })}). É essa a base do reembolso se a reserva for cancelada depois.`;
}

/** `details.extensions_count` e `details.max_extensions` do `/extend-hold`. */
function extensoes(details: Record<string, unknown>): string | null {
  const usadas = typeof details.extensions_count === "number" ? details.extensions_count : null;
  const teto = typeof details.max_extensions === "number" ? details.max_extensions : null;
  if (usadas === null || teto === null) return null;
  return `Já foram ${usadas} de ${teto} extensões. Agora é preciso decidir: confirmar com o sinal ou liberar as datas.`;
}
