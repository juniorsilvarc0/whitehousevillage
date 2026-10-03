"use client";

import { AlarmClock } from "lucide-react";

import { formatarInstante } from "@/lib/datas";
import { expiracaoDeHold } from "@/lib/mapa/expiracao";
import { useAgora } from "@/lib/tempo-real/relogio";
import { cn } from "@/lib/utils";

/**
 * O prazo da pré-reserva, contando.
 *
 * ## Por que isto é cliente, e não um `Date.now()` na tela de servidor
 *
 * Uma contagem regressiva calculada no servidor congela no instante em que o
 * HTML foi montado: a aba fica aberta a manhã inteira dizendo "expira em 6 h"
 * enquanto o prazo passou às 11h. Pior, `Date.now()` durante o render é chamada
 * impura — a regra `react-hooks/purity` recusa, e recusa com razão.
 *
 * O relógio é o **compartilhado do painel** (`lib/tempo-real/relogio.ts`), o
 * mesmo do mapa: um temporizador para a tela inteira, e todas as linhas viram o
 * minuto juntas. Ele devolve **0 no servidor**, o que faz servidor e hidratação
 * renderizarem exatamente a mesma coisa; aqui esse zero significa "ainda não sei
 * a hora", e o que se desenha é o **instante absoluto**, que é verdade sempre.
 * Calcular contra o zero daria "expira em 20500 d", que é o formato de um bug.
 *
 * O cálculo vem de `expiracaoDeHold`, o mesmo do mapa, de propósito: duas
 * frases diferentes para o mesmo prazo é como a operação deixa de confiar nas
 * duas.
 */

/** A linha curta da tabela: relógio + "expira em 3 h 12 min". */
export function ContadorDeHold({ expiraEm, className }: { expiraEm: string | null; className?: string }) {
  const agora = useAgora();
  if (!expiraEm) return null;
  const expiracao = agora === 0 ? null : expiracaoDeHold(expiraEm, agora);

  return (
    <p
      className={cn(
        "mt-1 flex items-center gap-1 text-[0.7rem] tabular-nums",
        expiracao && (expiracao.urgencia === "expirada" || expiracao.urgencia === "critica")
          ? "text-destructive"
          : "text-muted-foreground",
        className,
      )}
      title={`Prazo: ${formatarInstante(expiraEm)}`}
    >
      <AlarmClock className="size-3" aria-hidden="true" />
      {expiracao ? expiracao.texto : formatarInstante(expiraEm)}
    </p>
  );
}

/**
 * A faixa do detalhe — a mesma informação com a consequência escrita por
 * extenso, porque é ali que alguém decide ligar para o hóspede ou soltar a data.
 */
export function FaixaDePrazo({ expiraEm }: { expiraEm: string }) {
  const agora = useAgora();
  const expiracao = agora === 0 ? null : expiracaoDeHold(expiraEm, agora);
  const vencido = expiracao?.urgencia === "expirada";

  return (
    <div
      className={cn(
        "flex flex-wrap items-center gap-2 rounded-xl border px-4 py-3 text-sm",
        vencido ? "border-destructive/30 bg-destructive/8" : "border-alcada-atencao/40 bg-alcada-atencao/10",
      )}
    >
      <AlarmClock className="size-4 shrink-0" aria-hidden="true" />
      {vencido ? (
        <span>
          O prazo venceu em <strong className="tabular-nums">{formatarInstante(expiraEm)}</strong>. As datas
          já podem ter sido liberadas automaticamente, então não dá mais para confirmar esta pré-reserva — crie
          outra.
        </span>
      ) : (
        <span>
          As datas estão guardadas até <strong className="tabular-nums">{formatarInstante(expiraEm)}</strong>
          {expiracao ? (
            <>
              {" "}
              (<span className="tabular-nums">{expiracao.texto}</span>)
            </>
          ) : null}
          . Se o sinal não for pago até lá, elas voltam a ficar livres automaticamente.
        </span>
      )}
    </div>
  );
}
