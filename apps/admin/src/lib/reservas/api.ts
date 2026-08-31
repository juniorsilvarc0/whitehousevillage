import { apiList, type Query } from "@/lib/api/client";
import type { Lista } from "@/lib/api/types";
import { carregar } from "@/lib/api/carregar";
import { tentar } from "@/lib/acoes/executar";
import type { Resultado } from "@/lib/acoes/resultado";
import type { Reserva, ReservaCompleta } from "@/lib/reservas/tipos";

/**
 * A leitura de reservas — **servidor-only**, como todo o resto do painel: o JWT
 * vive em cookie `httpOnly` e quem o injeta é o RSC.
 *
 * Nada aqui lança. Uma rota que ainda não subiu, um perfil sem `reservations:ver`
 * e a API fora do ar são três coisas diferentes, e as três precisam chegar à
 * tela como código — não como página de erro.
 */

/** Lista **com** `meta`: reserva é a coleção que paginação de verdade. */
export function listarReservas(query: Query): Promise<Resultado<Lista<Reserva>>> {
  return tentar(() => apiList<Reserva>("/reservations", { query }));
}

/**
 * A tela do detalhe inteira, numa chamada.
 *
 * Cinco consultas encadeadas do navegador seriam cinco chances de mostrar meia
 * tela — e um estado intermediário em que as noites já vieram e os totais ainda
 * não. `cancellation_preview` vem junto e é o que a faixa do topo mostra sem
 * gastar um `dry_run`; a simulação do diálogo continua sendo refeita no clique,
 * porque o motivo (`no_show`) muda o resultado inteiro.
 */
export function carregarReserva(id: string): Promise<Resultado<ReservaCompleta>> {
  return carregar<ReservaCompleta>(`/reservations/${id}/full`);
}
