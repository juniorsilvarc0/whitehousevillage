import type { CodigoDeErro } from "@/lib/api/codigos";
import { mensagemDoErro, type Falha } from "@/lib/acoes/resultado";
import { lerConflito } from "@/lib/reservas/conflito";
import type { EdicaoDaOrdem } from "@/lib/manutencao/tipos";

/**
 * O que dizer quando a ordem de manutenção é recusada — **pelo código**, nunca
 * pelo texto da API. Os `details` entram só onde o contrato os promete:
 * `maintenance_order_id` de `MAINTENANCE_ORDER_ALREADY_OPEN`, `editable` de
 * `MAINTENANCE_ORDER_CLOSED`, `unit_code`/`period` de `DATE_CONFLICT`, e o
 * erro por campo de `VALIDATION_ERROR`.
 */

export const CAMINHO_DA_MANUTENCAO = "/app/manutencao";

export function caminhoDaOrdem(id: string): string {
  return `${CAMINHO_DA_MANUTENCAO}/${id}`;
}

/**
 * `409 MAINTENANCE_ORDER_ALREADY_OPEN` não é beco: `details.maintenance_order_id`
 * é a ordem que a avaria já tem, e o segundo toque em "Abrir ordem de
 * manutenção" tem de levar a ela. `null` para qualquer outra falha — ou para o
 * 409 sem o id, caso em que a tela mostra a frase e não inventa destino.
 */
export function ordemJaAberta(falha: Pick<Falha, "code" | "details">): { id: string; caminho: string } | null {
  if (falha.code !== "MAINTENANCE_ORDER_ALREADY_OPEN") return null;
  const id = falha.details?.maintenance_order_id;
  if (typeof id !== "string" || id === "") return null;
  return { id, caminho: caminhoDaOrdem(id) };
}

/** Onde a recusa aconteceu — o mesmo código quer frases diferentes. */
export type Contexto = "criacao" | "edicao" | "custo" | "bloqueio" | "transicao";

/**
 * A frase do `DATE_CONFLICT`, com a unidade e o período quando vierem.
 *
 * `details.period` é o período **pedido** (não o ocupado, como em `reservas`),
 * em half-open — e é dito com os nomes dos dois campos do formulário, para o
 * `to` não ser lido como uma noite bloqueada.
 */
export function fraseDoConflito(details: Record<string, unknown>, contexto: Contexto): string {
  const { unidade, de, ate } = lerConflito(details);
  const onde = unidade ? ` de ${unidade}` : " desta unidade";
  const quando = de && ate ? ` (primeiro dia ${de}, volta à venda em ${ate})` : "";
  const desfecho =
    contexto === "criacao"
      ? "Nada foi criado: escolha outras datas, ou abra a ordem sem bloquear o calendário."
      : "O bloqueio continua como estava.";
  return `O período pedido${quando} cruza uma reserva, pré-reserva ou outro bloqueio${onde}. ${desfecho}`;
}

/** `details.editable` do `MAINTENANCE_ORDER_CLOSED`, quando vier no vocabulário. */
function edicaoDe(details: Record<string, unknown>): EdicaoDaOrdem | null {
  const v = details?.editable;
  return v === "tudo" || v === "so_custo" || v === "nada" ? v : null;
}

export function mensagemDeManutencao(falha: Pick<Falha, "code" | "details">, contexto?: Contexto): string {
  const d = falha.details ?? {};
  switch (falha.code as CodigoDeErro) {
    case "DATE_CONFLICT":
      return fraseDoConflito(d, contexto ?? "bloqueio");
    case "MAINTENANCE_ORDER_CLOSED": {
      const editavel = edicaoDe(d);
      if (editavel === "so_custo") {
        return "Esta ordem já foi concluída e agora só aceita o custo. A tela foi atualizada.";
      }
      if (editavel === "nada") {
        return "Esta ordem foi cancelada e não aceita mais nada. A tela foi atualizada — retrabalho é uma ordem nova.";
      }
      return "Esta ordem já foi encerrada. A tela foi atualizada para mostrar como ela ficou.";
    }
    case "MAINTENANCE_ORDER_ALREADY_OPEN":
      return "Esta avaria já tem uma ordem de manutenção em aberto. Continue por ela em vez de abrir outra.";
    case "INVALID_STATE_TRANSITION":
      // O segundo toque em "Iniciar": a ordem já está em andamento.
      return "Alguém já mexeu nesta ordem — ela não aceita mais esse passo. A tela foi atualizada com a situação de agora.";
    case "FORBIDDEN":
      return "Seu perfil não tem permissão para mexer em ordens de manutenção. Se precisar, peça à gestão.";
    case "NOT_FOUND":
      return contexto === "criacao"
        ? "A unidade, o cômodo ou o bem escolhido não foi encontrado. Feche e abra o formulário de novo."
        : "Esta ordem não foi encontrada. Ela pode ter sido removida, ou esta parte do sistema ainda não está no ar.";
    default:
      return mensagemDoErro(falha.code as CodigoDeErro);
  }
}

/** A falha pede recarregar a ordem: o estado mudou por baixo da tela. */
export function pedeRecarga(falha: Pick<Falha, "code">): boolean {
  return falha.code === "MAINTENANCE_ORDER_CLOSED" || falha.code === "INVALID_STATE_TRANSITION";
}

// ── Erro por campo ──────────────────────────────────────────────────────────

/** Os campos que os formulários da ordem desenham. */
export type CampoDaOrdem =
  | "unit_id"
  | "room_id"
  | "item_id"
  | "issue_id"
  | "title"
  | "description"
  | "priority"
  | "cost"
  | "block_from"
  | "block_to";

export type FalhaNosCampos = {
  /** Mensagem por campo do formulário. */
  campos: Partial<Record<CampoDaOrdem, string>>;
  /** Erro do período inteiro — `details.block` (unidade inativa) ou o
   *  `DATE_CONFLICT`. Mostrado embaixo das duas datas. */
  periodo: string | null;
  /** O que não coube em campo nenhum. `null` quando tudo coube. */
  geral: string | null;
};

/**
 * De `details` do contrato para o campo do formulário.
 *
 * O contrato fala a língua do DTO, e o formulário a do `<input>`: `cost_cents`
 * é o campo "Custo (R$)", e o período do bloqueio chega com nomes diferentes
 * conforme a rota — `block.from`/`block.to`/`block` na **criação**,
 * `from`/`to`/`block` no `PUT /{id}/block`. As duas formas caem nos mesmos dois
 * campos de data.
 *
 * `DATE_CONFLICT` não tem campo em `details`, mas é sempre sobre o período: vai
 * para baixo das datas, com a unidade e as datas em conflito, e não para o topo
 * do modal — é ali que a pessoa está olhando quando vai trocar a data.
 */
export function falhaNosCampos(falha: Pick<Falha, "code" | "details">, contexto: Contexto): FalhaNosCampos {
  const d = falha.details ?? {};
  const campos: FalhaNosCampos["campos"] = {};
  let periodo: string | null = null;

  if (falha.code === "DATE_CONFLICT") {
    return { campos, periodo: fraseDoConflito(d, contexto), geral: null };
  }

  if (falha.code === "VALIDATION_ERROR") {
    const texto = (chave: string) => (typeof d[chave] === "string" && d[chave] !== "" ? (d[chave] as string) : null);
    const DIRETOS: [string, CampoDaOrdem][] = [
      ["unit_id", "unit_id"],
      ["room_id", "room_id"],
      ["item_id", "item_id"],
      ["issue_id", "issue_id"],
      ["title", "title"],
      ["description", "description"],
      ["priority", "priority"],
      ["cost_cents", "cost"],
      ["cost", "cost"],
      // A revalidação da action devolve os nomes do formulário.
      ["block_from", "block_from"],
      ["block_to", "block_to"],
    ];
    const DO_PERIODO: [string, CampoDaOrdem][] =
      contexto === "criacao"
        ? [
            ["block.from", "block_from"],
            ["block.to", "block_to"],
          ]
        : [
            ["from", "block_from"],
            ["to", "block_to"],
          ];
    for (const [chave, campo] of [...DIRETOS, ...DO_PERIODO]) {
      const frase = texto(chave);
      if (frase && !campos[campo]) campos[campo] = frase;
    }
    periodo = texto("block");
  }

  const coube = Object.keys(campos).length > 0 || periodo !== null;
  return { campos, periodo, geral: coube ? null : mensagemDeManutencao(falha, contexto) };
}
