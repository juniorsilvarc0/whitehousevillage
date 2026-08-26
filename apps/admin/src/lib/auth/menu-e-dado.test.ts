import { describe, expect, it } from "vitest";

import type { Permissao } from "@/lib/api/types";
import { navegacaoVisivel } from "@/lib/auth/permissions";

/**
 * O menu é função da MATRIZ, não do nome do perfil (regra 8 do CLAUDE.md:
 * "RBAC é dado, não código. Nada de `if role == 'corretor'`").
 *
 * Este arquivo nasceu VERMELHO, denunciando defeito de produção: cada item de
 * `navigation.ts` carregava `allowedRoles: readonly Role[]` com
 * `"admin" | "usuario" | "corretor"` escrito no código, e `navegacaoVisivel`
 * interseccionava essa lista com a matriz — então a lista compilada TIRAVA uma
 * tela que a matriz tinha concedido (a Agenda do corretor, escopo `own`).
 *
 * Consertado em 20/08/2026: `allowedRoles` deixou de existir, cada item declara
 * um recurso do catálogo do banco e a visibilidade é só `can(recurso, "ver")`
 * sobre a matriz de `/auth/me`. As duas expectativas que codificavam o defeito
 * foram corrigidas no mesmo commit (`navigation.test.ts`, que exigia
 * `/app/agenda` escondido do corretor, e a lista literal de hrefs em
 * `permissions.test.ts`). **As asserções daqui não mudaram uma vírgula** — é o
 * mesmo teste, agora verde, e é a guarda de regressão da propriedade.
 */

/**
 * Recorte da matriz que o seed instala no Corretor
 * (`apps/api/cmd/seed/acesso.go`), só com as linhas de ação `ver` — que é a
 * ação que o menu consulta. `agenda` em escopo `own` está lá: o corretor
 * enxerga os próprios compromissos.
 */
const MATRIZ_DO_CORRETOR: Permissao[] = [
  { resource: "calendar", action: "ver", scope: "own" },
  { resource: "reservations", action: "ver", scope: "own" },
  { resource: "crm.opportunities", action: "ver", scope: "own" },
  { resource: "chat", action: "ver", scope: "own" },
  { resource: "agenda", action: "ver", scope: "own" },
  { resource: "finance.commissions", action: "ver", scope: "own" },
];

const hrefs = (role: string, permissoes: Permissao[]) =>
  navegacaoVisivel({ role }, permissoes).map((i) => i.href);

describe("o menu é derivado da matriz, não do nome do perfil", () => {
  /**
   * A prova mais curta do defeito: DUAS pessoas com a MESMA matriz, diferentes
   * só no `code` do perfil, recebem menus diferentes.
   *
   * Em linguagem de negócio: a gestão cria em `/roles` o perfil "Plantonista",
   * copia a matriz do Corretor célula por célula e manda o time usar. Os
   * plantonistas enxergam a Agenda; os corretores, com exatamente a mesma
   * permissão, não. Ninguém consegue explicar a diferença olhando a tela de
   * perfis — porque ela não está lá, está no código.
   */
  it("dois perfis com a mesma matriz enxergam o mesmo menu", () => {
    expect(hrefs("corretor", MATRIZ_DO_CORRETOR)).toEqual(
      hrefs("plantonista", MATRIZ_DO_CORRETOR),
    );
  });

  /**
   * O mesmo defeito dito pelo lado do dano: a lista compilada em
   * `navigation.ts` remove uma tela que a matriz concedeu.
   *
   * Em linguagem de negócio: o corretor recebeu `agenda` em escopo "só os meus"
   * — é a agenda de visitas dele, a ferramenta do trabalho dele. A tela de
   * perfis mostra a permissão concedida, marcada, salva. E o item não aparece no
   * menu dele. A gestão marca de novo, salva de novo, e continua não aparecendo.
   */
  it("não esconde tela que a matriz concedeu", () => {
    const concedidos = MATRIZ_DO_CORRETOR.map((p) => p.resource);
    const visiveis = hrefs("corretor", MATRIZ_DO_CORRETOR);

    expect(
      visiveis,
      `o corretor tem ${concedidos.join(", ")} na matriz, mas o menu dele é ${visiveis.join(", ")}`,
    ).toContain("/app/agenda");
  });

  /**
   * Controle: a filtragem por matriz continua TIRANDO o que não foi concedido.
   * Sem este caso, o conserto poderia ser "mostrar tudo para todo mundo", que
   * satisfaria os dois testes acima e abriria o financeiro global ao corretor.
   *
   * Este caso passa hoje e precisa continuar passando depois do conserto.
   */
  it("continua escondendo o que a matriz não concede", () => {
    const visiveis = hrefs("corretor", MATRIZ_DO_CORRETOR);

    for (const proibido of [
      "/app/financeiro",
      "/app/configuracoes",
      "/app/relatorios",
      "/app/inventario",
      "/app/canais",
    ]) {
      expect(visiveis).not.toContain(proibido);
    }
  });
});
