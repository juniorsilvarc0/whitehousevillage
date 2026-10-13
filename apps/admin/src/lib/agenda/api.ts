import { apiList, type Query } from "@/lib/api/client";
import { carregar } from "@/lib/api/carregar";
import { tentar } from "@/lib/acoes/executar";
import type { Resultado } from "@/lib/acoes/resultado";
import type { Permissao } from "@/lib/api/types";
import { can } from "@/lib/auth/permissions";
import { somarDias } from "@/lib/datas";
import { todasAsPaginas } from "@/lib/agenda/paginas";
import type { Janela } from "@/lib/agenda/visoes";
import type {
  BloqueioDeHorario,
  ConfiguracaoDaAgenda,
  EventoDaAgenda,
  PessoaDaAgenda,
  UnidadeDaAgenda,
} from "@/lib/agenda/tipos";

/*
 * Leituras da agenda — **servidor-only**, como todo acesso à API (o JWT vive
 * em cookie `httpOnly`). Não leva `import "server-only"` porque o pacote não
 * está no `package.json`; quem o mantém fora do bundle do navegador é a cadeia
 * real: `@/lib/api/client` importa `next/headers`.
 */

/** Os horários da operação — e o `today` da casa, que é o "hoje" da tela. */
export function carregarHorarios(): Promise<Resultado<ConfiguracaoDaAgenda>> {
  return carregar<ConfiguracaoDaAgenda>("/agenda/settings");
}

export type EventosDaJanela = { eventos: EventoDaAgenda[]; incompleta: boolean };

/** A janela inteira de eventos, seguindo `meta.total_pages`. */
export function carregarEventos(consulta: Query): Promise<Resultado<EventosDaJanela>> {
  return tentar(async () => {
    const { itens, incompleta } = await todasAsPaginas((page) =>
      apiList<EventoDaAgenda>("/agenda/events", { query: { ...consulta, page } }),
    );
    return { eventos: itens, incompleta };
  });
}

export type BloqueiosDaJanela = { bloqueios: BloqueioDeHorario[]; incompleta: boolean };

/** Os bloqueios de horário da operação que cruzam a janela — o fundo da grade. */
export function carregarBloqueios(janela: Pick<Janela, "from" | "to">): Promise<Resultado<BloqueiosDaJanela>> {
  return tentar(async () => {
    const { itens, incompleta } = await todasAsPaginas((page) =>
      apiList<BloqueioDeHorario>("/agenda/blocks", { query: { from: janela.from, to: janela.to, page, per_page: 100 } }),
    );
    return { bloqueios: itens, incompleta };
  });
}

/**
 * As unidades que o formulário oferece.
 *
 * A tag Agenda não publica lista de unidades, e cada rota que publica pede um
 * recurso diferente: `/units` (`inventory:ver`, o cadastro comercial),
 * `/inventory/units` (`inventory.goods:ver`) e `/availability/units`
 * (`calendar:ver`, o mapa). Pergunta-se pela primeira que a matriz alcança —
 * o corretor só tem a do mapa, e é por ela que ele escolhe a unidade da
 * visita. `null` quando nenhuma: o formulário marca sem unidade e diz por quê.
 */
export async function unidadesDaAgenda(permissoes: Permissao[], hoje: string): Promise<Resultado<UnidadeDaAgenda[]> | null> {
  if (can(permissoes, "inventory", "ver")) {
    return tentar(async () => {
      const { itens } = await todasAsPaginas((page) =>
        apiList<UnidadeDaAgenda>("/units", { query: { active: true, sort: "code", page, per_page: 100 } }),
      );
      return itens.map((u) => ({ id: u.id, code: u.code, name: u.name }));
    });
  }
  if (can(permissoes, "inventory.goods", "ver")) {
    return tentar(async () => {
      const { itens } = await todasAsPaginas((page) =>
        apiList<UnidadeDaAgenda>("/inventory/units", { query: { active: true, page, per_page: 100 } }),
      );
      return itens.map((u) => ({ id: u.id, code: u.code, name: u.name }));
    });
  }
  if (can(permissoes, "calendar", "ver")) {
    // Um dia basta: a resposta traz uma linha por unidade ativa, com código e
    // nome — a ocupação daquele dia é descartada aqui mesmo.
    return tentar(async () => {
      const { data } = await apiList<{ unit_id: string; unit_code: string; unit_name: string }>("/availability/units", {
        query: { from: hoje, to: somarDias(hoje, 1) },
      });
      return data.map((l) => ({ id: l.unit_id, code: l.unit_code, name: l.unit_name }));
    });
  }
  return null;
}

/**
 * Quem pode ser responsável, além do próprio usuário e de quem já aparece na
 * agenda. `GET /users` é a tela de administração de acesso (na matriz semeada,
 * só o `admin` tem `users`); sem ela, a lista é feita do que a agenda já
 * mostra — a tela não inventa outra porta para listar pessoas.
 */
export async function pessoasDaAgenda(permissoes: Permissao[]): Promise<PessoaDaAgenda[]> {
  if (!can(permissoes, "users", "ver")) return [];
  const r = await tentar(async () => {
    const { itens } = await todasAsPaginas((page) =>
      apiList<{ id: string; name: string; active: boolean }>("/users", { query: { active: true, sort: "name", page, per_page: 100 } }),
    );
    return itens;
  });
  return r.ok ? r.data.filter((u) => u.active !== false).map((u) => ({ id: u.id, name: u.name })) : [];
}
