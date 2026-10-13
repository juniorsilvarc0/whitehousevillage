import type { Escopo, Permissao } from "@/lib/api/types";
import { can, escopoDe } from "@/lib/auth/permissions";
import type { EdicaoDoEvento, EventoDaAgenda } from "@/lib/agenda/tipos";

/**
 * O que o perfil pode fazer na agenda — lido da matriz, sempre pelo par
 * recurso × ação do `x-rbac` de cada rota. Esconder não é autorizar: cada
 * action confere de novo e a API recusa com 403.
 *
 * | Gesto | Rota | `x-rbac` |
 * |---|---|---|
 * | ver a agenda, os bloqueios de horário e os horários | `GET /agenda/*` | `agenda:ver` |
 * | marcar evento | `POST /agenda/events` | `agenda:criar` |
 * | editar, concluir | `PUT`/`PATCH`, `/complete` | `agenda:editar` |
 * | desmarcar | `DELETE /agenda/events/{id}` | `agenda:excluir` |
 * | bloquear horário da operação | `POST /agenda/blocks` | `agenda:criar` em `all` |
 * | remover bloqueio de horário | `DELETE /agenda/blocks/{id}` | `agenda:excluir` em `all` |
 * | gravar os horários | `PUT /agenda/settings` | `settings:editar` |
 *
 * O **escopo** só entra onde muda o formulário: em `own`, o responsável é o
 * próprio ator (o servidor recusa outro com `403 assignee_not_actor`), e o
 * bloqueio de horário — que não tem dono — não se aplica (`403
 * own_scope_not_applicable`). Quem filtra o que o `own` enxerga é o SQL.
 */
export type PermissoesDaAgenda = {
  ver: boolean;
  criar: boolean;
  editar: boolean;
  excluir: boolean;
  /** Escopo de `agenda:criar` — `own` fixa o responsável no ator. */
  escopoAoCriar: Escopo | null;
  /** Escopo de `agenda:editar`. */
  escopoAoEditar: Escopo | null;
  /** `POST /agenda/blocks`: `agenda:criar` em `all`. */
  bloquearHorario: boolean;
  /** `DELETE /agenda/blocks/{id}`: `agenda:excluir` em `all`. */
  removerBloqueio: boolean;
  /** `PUT /agenda/settings`: `settings:editar`, e não `agenda:editar`. */
  gravarHorarios: boolean;
};

export function permissoesDaAgenda(matriz: Permissao[]): PermissoesDaAgenda {
  const escopoAoCriar = escopoDe(matriz, "agenda", "criar");
  const escopoAoEditar = escopoDe(matriz, "agenda", "editar");
  return {
    ver: can(matriz, "agenda", "ver"),
    criar: can(matriz, "agenda", "criar"),
    editar: can(matriz, "agenda", "editar"),
    excluir: can(matriz, "agenda", "excluir"),
    escopoAoCriar,
    escopoAoEditar,
    bloquearHorario: escopoAoCriar === "all",
    removerBloqueio: escopoDe(matriz, "agenda", "excluir") === "all",
    gravarHorarios: can(matriz, "settings", "editar"),
  };
}

/** Os gestos que o detalhe do evento oferece. */
export type BotoesDoEvento = {
  concluir: boolean;
  desmarcar: boolean;
  /** Abre o formulário — os campos abertos saem de `editable` (`camposDoEvento`). */
  editar: boolean;
};

/**
 * Os botões **a partir do que a API devolveu**: `allowed_actions` para as
 * transições e `editable` para a edição, cruzados com a matriz.
 *
 * O `status`, o `type` e a `origin` não entram na decisão, de propósito. A
 * máquina de estados mora em `internal/domain/agenda` (`AllowedActions`,
 * `EditableIn`): o check-in automático não conclui por aqui, a limpeza de
 * amanhã não conclui hoje, o desmarcado não reabre — e nada disso está escrito
 * neste arquivo. Uma segunda cópia divergiria da primeira no dia em que só uma
 * mudasse.
 */
export function botoesDoEvento(
  evento: Pick<EventoDaAgenda, "allowed_actions" | "editable">,
  permissoes: Pick<PermissoesDaAgenda, "editar" | "excluir">,
): BotoesDoEvento {
  const pode = (acao: EventoDaAgenda["allowed_actions"][number]) => evento.allowed_actions.includes(acao);
  return {
    concluir: permissoes.editar && pode("complete"),
    desmarcar: permissoes.excluir && pode("cancel"),
    editar: permissoes.editar && evento.editable !== "nada",
  };
}

/** Os campos do formulário de evento. */
export type CampoDoEvento =
  | "type"
  | "title"
  | "all_day"
  | "quando"
  | "unit_id"
  | "reservation_id"
  | "contact_id"
  | "assignee_id"
  | "notes";

/**
 * Quais campos o formulário deixa mexer, **pelo `editable`** que a API mandou:
 *
 * - `tudo` (manual agendado): todos;
 * - `segue_a_reserva` (automático agendado): o horário, o responsável e a
 *   anotação — tipo, título, unidade, reserva e pessoa são da reserva;
 * - `so_notas` (concluído): só a anotação;
 * - `nada` (desmarcado, derivado): nenhum.
 *
 * "Check-in e check-out mudam de hora, não de dia" e "a limpeza nunca antes do
 * fim do check-out" **não** estão aqui: são regras da API, e a recusa dela
 * (`422` em `starts_at`) cai no campo de horário.
 */
export function camposDoEvento(editable: EdicaoDoEvento): ReadonlySet<CampoDoEvento> {
  switch (editable) {
    case "tudo":
      return new Set<CampoDoEvento>([
        "type",
        "title",
        "all_day",
        "quando",
        "unit_id",
        "reservation_id",
        "contact_id",
        "assignee_id",
        "notes",
      ]);
    case "segue_a_reserva":
      return new Set<CampoDoEvento>(["quando", "assignee_id", "notes"]);
    case "so_notas":
      return new Set<CampoDoEvento>(["notes"]);
    case "nada":
      return new Set<CampoDoEvento>();
  }
}

/** O verbo da gravação pelo `editable`: `PUT` substitui o manual inteiro;
 *  `PATCH` muda só o que o automático e o concluído aceitam. */
export function verboDaEdicao(editable: EdicaoDoEvento): "PUT" | "PATCH" | null {
  if (editable === "tudo") return "PUT";
  if (editable === "segue_a_reserva" || editable === "so_notas") return "PATCH";
  return null;
}
