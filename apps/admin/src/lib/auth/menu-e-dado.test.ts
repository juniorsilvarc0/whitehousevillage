import { describe, expect, it } from "vitest";

import type { Permissao } from "@/lib/api/types";
import { CATALOGO } from "@/config/navigation";
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

    // A âncora era `/app/agenda`, que é onde o defeito de `allowedRoles`
    // aparecia. Ela mudou em 27/08/2026 por um motivo que **não** é o defeito:
    // a tela de agenda não existe (Fase 2), e o menu passou a esconder o que
    // leva a 404 — oito dos treze destinos levavam, e o Next ainda os
    // prefetchava. `/app/reservas` é a mesma prova com uma tela que existe: o
    // corretor tem `reservations` em escopo `own`, e escopo restringe o QUE se
    // vê dentro da tela, nunca se a tela aparece.
    expect(
      visiveis,
      `o corretor tem ${concedidos.join(", ")} na matriz, mas o menu dele é ${visiveis.join(", ")}`,
    ).toContain("/app/reservas");
  });

  it("a agenda continua ausente por falta de TELA, não por causa do papel", () => {
    // O contrapeso do caso acima, e o que impede a troca de âncora de virar um
    // afrouxamento: `/app/agenda` some do menu do corretor, sim — mas some do
    // menu de todo mundo, inclusive do admin, porque não há `page.tsx`. Se
    // algum dia ela sumir só para o corretor, este caso continua verde e o de
    // cima fica vermelho, que é a divisão certa de trabalho entre os dois.
    const agenda = CATALOGO.find((item) => item.href === "/app/agenda");

    expect(agenda, "o destino saiu do catálogo — deveria continuar lá, marcado").toBeDefined();
    expect(agenda?.emConstrucao, "a agenda voltou a existir: reescreva a âncora deste teste").toBe(true);
    expect(hrefs("admin", MATRIZ_DO_CORRETOR)).not.toContain("/app/agenda");
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

    // A lista encolheu para UM destino, e o encolhimento é a asserção ficando
    // mais honesta, não mais fraca. `/app/financeiro`, `/app/relatorios` e
    // `/app/canais` saíram porque essas telas **não existem** (Fases 2 a 5): a
    // ausência delas do menu não prova nada sobre permissão — elas somem para o
    // admin também. Um caso que "passa" por falta de tela é um verde vazio, e
    // ele ficaria verde mesmo no dia em que o filtro por matriz quebrasse.
    //
    // Configurações é a única tela que EXISTE e que a spec §11 mantém fora do
    // alcance do corretor — é sobre ela, portanto, que a garantia se afirma.
    expect(visiveis).not.toContain("/app/configuracoes");

    // O inventário de bens nasceu em 07/10/2026 (`/app/inventario`, recurso
    // `inventory.goods`) e é a segunda tela que existe e que o corretor não
    // alcança: o seed concede `inventory.goods` a admin e usuario, e não a ele
    // (docs/db.md §11). Enquanto o item ainda estiver marcado `emConstrucao`
    // em `navigation.ts`, este caso é o verde vazio descrito acima; quando a
    // marca cair, é a matriz que o mantém verde.
    expect(visiveis).not.toContain("/app/inventario");
  });
});
