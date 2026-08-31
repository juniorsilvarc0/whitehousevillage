import { Badge } from "@/components/ui/badge";
import { ESTADOS } from "@/lib/reservas/estados";
import type { EstadoDaReserva } from "@/lib/reservas/tipos";
import { cn } from "@/lib/utils";

/**
 * O estado da reserva como etiqueta.
 *
 * A cor separa quatro leituras, não nove: o que ainda pode virar venda
 * (`aberto`), a venda de pé (`vivo`), o que terminou bem (`encerrado`) e o que
 * terminou mal (`perdido`). Nove cores numa lista de vinte linhas não são um
 * código — são um enfeite, e o olho passa a ler o texto de qualquer jeito.
 *
 * O `title` carrega a explicação do estado. Não é decoração: "expirada" e
 * "cancelada" parecem sinônimos para quem entrou ontem, e a diferença (o job
 * soltou a data sozinho × alguém decidiu) muda o que a pessoa faz em seguida.
 */
export function EtiquetaDeEstado({
  estado,
  className,
}: {
  estado: EstadoDaReserva;
  className?: string;
}) {
  const descricao = ESTADOS[estado];
  return (
    <Badge
      variant={descricao.tom === "perdido" ? "destructive" : "neutral"}
      title={descricao.explicacao}
      className={cn(
        descricao.tom === "vivo" && "bg-alcada-livre/15 text-foreground",
        descricao.tom === "aberto" && "bg-alcada-atencao/15 text-foreground",
        descricao.tom === "encerrado" && "bg-secondary text-muted-foreground",
        className,
      )}
    >
      {descricao.rotulo}
    </Badge>
  );
}
