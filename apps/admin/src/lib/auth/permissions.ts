import type { Acao, Escopo, Permissao, Usuario } from "@/lib/api/types";
import { type NavItem, navigation } from "@/config/navigation";

/**
 * Leitura da matriz de permissões no painel.
 *
 * **Esconder não é autorizar.** Nada aqui protege coisa alguma: o middleware da
 * API checa papel × recurso × ação × escopo a cada requisição, e é ele que
 * responde 403. Estas funções existem só para não oferecer ao usuário um botão
 * que vai falhar — o que é cortesia, não segurança.
 */

export function can(permissoes: Permissao[], recurso: string, acao: Acao = "ver"): boolean {
  return permissoes.some((p) => p.resource === recurso && p.action === acao);
}

/** Escopo com que o usuário enxerga o recurso. `own` é o corretor vendo só o
 *  que é dele — e quem aplica isso é o `WHERE` do repositório, não o painel. */
export function escopoDe(permissoes: Permissao[], recurso: string, acao: Acao = "ver"): Escopo | null {
  return permissoes.find((p) => p.resource === recurso && p.action === acao)?.scope ?? null;
}

export function acoesDe(permissoes: Permissao[], recurso: string): Acao[] {
  return permissoes.filter((p) => p.resource === recurso).map((p) => p.action);
}

/** Agrupa a matriz por recurso, na ordem em que os recursos aparecem — é o que
 *  a tela precisa para desenhar a grade sem reordenar a cada render. */
export function porRecurso(permissoes: Permissao[]): { recurso: string; acoes: Acao[]; escopo: Escopo }[] {
  const mapa = new Map<string, { recurso: string; acoes: Acao[]; escopo: Escopo }>();
  for (const p of permissoes) {
    const atual = mapa.get(p.resource);
    if (atual) {
      atual.acoes.push(p.action);
      // Um recurso com qualquer ação em `own` já é um recurso restrito ao dono.
      if (p.scope === "own") atual.escopo = "own";
    } else {
      mapa.set(p.resource, { recurso: p.resource, acoes: [p.action], escopo: p.scope });
    }
  }
  return [...mapa.values()];
}

/**
 * Abrir uma tela é lê-la, então o menu pergunta sempre por `ver`. Criar, editar
 * e excluir são decisões de dentro da tela — os atalhos do Painel, por exemplo,
 * declaram a própria ação (`settings:editar`) e não passam por aqui.
 */
const ACAO_DO_MENU: Acao = "ver";

/**
 * Menu visível: os itens cujo recurso a matriz do usuário concede em `ver`.
 *
 * **O `code` do perfil não entra na decisão** — regra 8 do CLAUDE.md, e o
 * motivo pelo qual `allowedRoles` deixou de existir. `user` continua na
 * assinatura porque a chamada é "o menu deste usuário" e porque o dado é útil
 * em log e telemetria; o corpo não o lê, e `menu-e-dado.test.ts` fica vermelho
 * no dia em que alguém voltar a lê-lo: dois perfis com a mesma matriz e códigos
 * diferentes têm que ver exatamente o mesmo menu.
 *
 * Todo item declara recurso (`NavItem.recurso`, obrigatório em tipo), inclusive
 * o Painel — não há item isento passando pelo filtro sem ser checado.
 */
export function navegacaoVisivel(_user: Pick<Usuario, "role">, permissoes: Permissao[]): NavItem[] {
  return navigation.filter((item) => can(permissoes, item.recurso, ACAO_DO_MENU));
}
