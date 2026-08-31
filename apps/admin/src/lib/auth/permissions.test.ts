import { existsSync, readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import type { Acao, Escopo, Permissao } from "@/lib/api/types";
import { CATALOGO, navigation } from "@/config/navigation";
import { can, escopoDe, navegacaoVisivel, porRecurso } from "@/lib/auth/permissions";
import { RECURSOS_DO_CATALOGO } from "@/lib/auth/recursos";

/**
 * Esta suíte guarda um defeito específico, do tipo que volta calado.
 *
 * O painel escondia Mapa, Financeiro e Comissões de **todo mundo**, inclusive do
 * admin, porque citava `availability`, `finance` e `commissions` — três códigos
 * que não existem no catálogo do banco. Recurso inexistente não protege
 * ninguém: como ninguém tem (nem pode ter) permissão nele, `can()` responde
 * `false` para o admin também. E não há 403 para denunciar, porque requisição
 * nenhuma chega a sair. A tela simplesmente some.
 *
 * Duas defesas, em camadas diferentes:
 * 1. `RecursoCodigo` (em `recursos.ts`) faz o typecheck recusar código fora do
 *    catálogo — o erro aparece antes do build;
 * 2. os testes abaixo comparam o espelho com o `acesso.go` de verdade e provam,
 *    no comportamento, quem enxerga o quê.
 */

const AQUI = path.dirname(fileURLToPath(import.meta.url));
const RAIZ_ADMIN = path.resolve(AQUI, "../../..");
const SEED_GO = path.resolve(RAIZ_ADMIN, "../api/cmd/seed/acesso.go");
const CONSTANTES_GO = path.resolve(RAIZ_ADMIN, "../api/internal/auth/recursos.go");

/**
 * Lê o catálogo direto de `catalogoSeed`, no Go. Ler o arquivo é preferível a
 * confiar só no espelho: espelho conferido contra si mesmo não confere nada.
 *
 * Devolve `null` quando o `apps/api` não está por perto (checkout parcial do
 * monorepo) — aí o teste se declara pulado em vez de reprovar por ausência.
 */
function catalogoDoSeed(): string[] | null {
  if (!existsSync(SEED_GO) || !existsSync(CONSTANTES_GO)) return null;

  // Duas linhas do catálogo citam constantes (`auth.RecursoUsuarios`) em vez de
  // literal; sem resolvê-las o código viria com o nome do símbolo Go.
  const constantes = new Map<string, string>();
  const go = readFileSync(CONSTANTES_GO, "utf8");
  for (const [, nome, valor] of go.matchAll(/(Recurso\w+)\s*=\s*"([^"]+)"/g)) {
    constantes.set(`auth.${nome}`, valor);
  }

  const fonte = readFileSync(SEED_GO, "utf8");
  const bloco = fonte.match(/var catalogoSeed = \[\]recurso\{([\s\S]*?)\n\}/);
  if (!bloco) throw new Error(`não achei catalogoSeed em ${SEED_GO} — o seed mudou de forma?`);

  const codigos: string[] = [];
  for (const [, campo] of bloco[1].matchAll(/^\s*\{\s*(auth\.\w+|"[^"]+")/gm)) {
    codigos.push(campo.startsWith('"') ? campo.slice(1, -1) : (constantes.get(campo) ?? campo));
  }
  if (codigos.length === 0) throw new Error("catalogoSeed veio vazio — o parser precisa de conserto");
  return codigos;
}

const doSeed = catalogoDoSeed();

/** Arquivos de produção do painel — testes de fora, senão o scanner se acha. */
function fontesDoPainel(dir = path.join(RAIZ_ADMIN, "src")): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entrada) => {
    const caminho = path.join(dir, entrada.name);
    if (entrada.isDirectory()) return fontesDoPainel(caminho);
    if (!/\.tsx?$/.test(entrada.name) || /\.test\.tsx?$/.test(entrada.name)) return [];
    return [caminho];
  });
}

describe("vocabulário de recursos", () => {
  it.skipIf(doSeed === null)("o espelho do painel é o catálogo que o seed grava", () => {
    // Ordem não importa (o seed ordena por `sort_order`, o painel por leitura);
    // o conjunto importa, nos dois sentidos: código a mais no espelho é tela que
    // some, código a menos é recurso que o painel nunca vai saber citar.
    expect([...RECURSOS_DO_CATALOGO].sort()).toEqual([...(doSeed as string[])].sort());
  });

  it("nenhum arquivo do painel cita recurso fora do catálogo", () => {
    // Varredura no fonte porque o mapa do menu não é o único lugar que nomeia
    // recurso: os atalhos do Painel também declaram `recurso: "..."`, e foi lá
    // que os três códigos fantasmas se repetiram.
    const forasteiros: string[] = [];
    for (const arquivo of fontesDoPainel()) {
      const conteudo = readFileSync(arquivo, "utf8");
      for (const [, codigo] of conteudo.matchAll(/\brecurso:\s*"([^"]+)"/g)) {
        if (!(RECURSOS_DO_CATALOGO as readonly string[]).includes(codigo)) {
          forasteiros.push(`${path.relative(RAIZ_ADMIN, arquivo)}: "${codigo}"`);
        }
      }
    }
    expect(forasteiros).toEqual([]);
  });

  it("nenhum item do menu escapa da checagem — nem o Painel", () => {
    // Item sem recurso passaria pelo filtro sem ser checado, e aí a tela de
    // perfis passaria a mentir sobre ele: a gestão desmarca, salva, e nada muda.
    // O Painel era a exceção documentada ("é a tela de chegada de quem logou") e
    // deixou de ser: `dashboard` está no catálogo, com o rótulo "Painel", e é
    // concedido a admin, usuario e corretor no seed. `/app` continua alcançável
    // pela rota — o que sumiu foi o item isento, não a casa.
    //
    // A prova é comportamental de propósito: tirar UM recurso da matriz tem que
    // apagar UM item do menu, para todo item.
    for (const item of navigation) {
      const semEsse = RECURSOS_DO_CATALOGO.filter((r) => r !== item.recurso);
      const visiveis = navegacaoVisivel({ role: "admin" }, permissoesDe(semEsse)).map((n) => n.href);

      expect(visiveis, `${item.href} aparece sem "${item.recurso}" na matriz`).not.toContain(item.href);
      expect(visiveis, `tirar "${item.recurso}" apagou mais de um item`).toHaveLength(navigation.length - 1);
    }
  });
});

function permissoesDe(recursos: readonly string[], escopo: Escopo = "all", acao: Acao = "ver"): Permissao[] {
  return recursos.map((resource) => ({ resource, action: acao, scope: escopo }));
}

/** O admin recebe o catálogo inteiro em `all` — é o que `montarMatriz()` gera. */
const PERMISSOES_DO_ADMIN = permissoesDe(RECURSOS_DO_CATALOGO);

/**
 * Recorte de `matrizSeed["corretor"]` (apps/api/cmd/seed/acesso.go) só com as
 * linhas de ação `ver`, que é a ação que o menu consulta. Financeiro global,
 * inventário, canais e configurações ficam de fora — spec §11.
 */
const PERMISSOES_DO_CORRETOR: Permissao[] = [
  ...permissoesDe(["dashboard", "contacts", "crm.pipelines"], "all"),
  ...permissoesDe(
    ["reservations", "quotes", "calendar", "agenda", "crm.leads", "crm.opportunities", "crm.activities", "chat", "brokers", "finance.commissions"],
    "own",
  ),
];

describe("navegacaoVisivel", () => {
  const hrefs = (user: { role: string }, permissoes: Permissao[]) =>
    navegacaoVisivel(user, permissoes).map((item) => item.href);

  it("o admin com o catálogo inteiro não perde NENHUM destino", () => {
    // O caso que reprovou a Fase 0: `navigation.ts` citava `availability`,
    // `finance` e `commissions` — três códigos que não existem em `resources`.
    // Recurso inexistente não protege ninguém: como ninguém tem (nem pode ter)
    // permissão nele, `can()` responde `false` para o admin também, e a tela
    // some sem 403 nenhum para denunciar.
    //
    // A asserção deixou de nomear Mapa, Financeiro e Comissões e passou a ser
    // universal — é a mesma garantia, mais forte, e não envelhece quando o
    // catálogo cresce. Nomear três telas era o que fazia este caso reprovar em
    // 27/08/2026 por um motivo diferente do que ele mede: Financeiro e Comissões
    // saíram do menu porque as TELAS não existem (Fase 2), não porque o recurso
    // sumiu. Distinguir os dois motivos é justamente o trabalho desta suíte.
    const doAdmin = hrefs({ role: "admin" }, PERMISSOES_DO_ADMIN);

    expect(doAdmin).toEqual(navigation.map((item) => item.href));
    expect(doAdmin).toContain("/app/mapa");
  });

  it("recurso concedido não basta: a tela precisa existir — e são checagens diferentes", () => {
    // A composição instalada em 27/08/2026 (Dívida A): o menu é
    // `telas entregues ∩ matriz`. As duas metades falham por motivos diferentes
    // e a diferença importa, porque o conserto de cada uma é outro:
    //
    // - recurso ausente da matriz → a gestão marca em Configurações → Perfis;
    // - tela ausente da pasta → alguém tem de escrevê-la, e permissão nenhuma
    //   resolve.
    //
    // Sem este caso, "consertar" o menu voltando a listar `/app/chat` passaria
    // despercebido — e o item voltaria a prefetchar um 404.
    const comChat = hrefs({ role: "admin" }, permissoesDe(["dashboard", "chat"]));
    expect(comChat, "chat está na matriz, mas a tela não existe").not.toContain("/app/chat");
    expect(comChat).toEqual(["/app"]);

    // E o contrapeso: a ausência é da TELA, não do recurso. `chat` continua no
    // catálogo do banco, o item continua no `CATALOGO` do painel, e ele volta
    // sozinho ao menu quando `app/(app)/app/chat/page.tsx` existir — é o que
    // `navigation.test.ts` cobra contra o disco.
    expect(CATALOGO.map((item) => item.href)).toContain("/app/chat");
    expect(RECURSOS_DO_CATALOGO as readonly string[]).toContain("chat");
  });

  it("o corretor não vê Financeiro nem Configurações", () => {
    const doCorretor = hrefs({ role: "corretor" }, PERMISSOES_DO_CORRETOR);

    expect(doCorretor).not.toContain("/app/financeiro");
    expect(doCorretor).not.toContain("/app/configuracoes");
  });

  it("o corretor vê o que é dele — todas as telas que existem e a matriz concede", () => {
    // Esta lista é a interseção de duas coisas, e as duas mudam com o tempo: a
    // matriz do seed (docs/ui.md §7) e as telas entregues. `/app/chat`,
    // `/app/agenda` e `/app/comissoes` estão na matriz do corretor e **não**
    // estão aqui — as telas são de Fase 1g e Fase 2. Voltam ao menu dele sozinhas
    // no dia em que existirem, sem tocar nesta suíte nem na matriz.
    expect(hrefs({ role: "corretor" }, PERMISSOES_DO_CORRETOR)).toEqual([
      "/app",
      "/app/mapa",
      "/app/reservas",
      // Contatos e Orçamento nasceram na rodada de 27/08/2026: o seed já dava
      // `contacts` (escopo `all` — a agenda é da casa) e `quotes` (`own`) ao
      // corretor; o que faltava era a tela.
      "/app/contatos",
      "/app/orcamento",
      "/app/funil",
      // Leads entrou junto com o CRM: o seed dá `crm.leads` ao corretor em
      // escopo `own` — são os leads dele, e o menu é função só da matriz.
      "/app/leads",
    ]);
  });

  it("perfil sem permissão nenhuma não vê menu nenhum", () => {
    // O caso de controle do conserto: se o menu deixasse de filtrar, TODO item
    // apareceria aqui. Sobrava o Painel enquanto ele era isento; agora ele
    // depende de `dashboard` como qualquer outro. A casa continua acessível
    // (o logo da barra aponta para `/app` e a tela avisa que o perfil não tem
    // recurso liberado) — o que não existe mais é item fora da matriz.
    expect(hrefs({ role: "admin" }, [])).toEqual([]);
  });

  it("perfil desconhecido é regido só pela matriz", () => {
    // `role` é o `code` do perfil e não é enum fechado: "gerente-de-eventos"
    // nasce em /roles sem deploy. Não há mais lista de papéis para ele não estar
    // — o menu dele é exatamente o que a gestão marcou, nem mais nem menos.
    // Sem `dashboard` na matriz não há nem Painel: RBAC é dado (regra 8).
    // `finance.receivables` e `reports` estão na matriz deste perfil e não
    // aparecem: as telas são de Fase 2 e Fase 3. Não é decisão de permissão —
    // é ausência de tela, e o caso acima ("recurso concedido não basta")
    // separa os dois motivos.
    const permissoes = permissoesDe(["reservations", "finance.receivables", "reports"]);

    expect(hrefs({ role: "gerente-de-eventos" }, permissoes)).toEqual(["/app/reservas"]);
  });

  it("perfil desconhecido não ganha tela por omissão", () => {
    // O contrapeso do teste acima: sem a linha na matriz, a tela não aparece —
    // nem para um code que o painel nunca viu.
    const doNovo = hrefs({ role: "gerente-de-eventos" }, permissoesDe(["reservations"]));

    expect(doNovo).not.toContain("/app/configuracoes");
    expect(doNovo).not.toContain("/app/contatos");
  });
});

describe("leitura da matriz", () => {
  const matriz: Permissao[] = [
    { resource: "finance.commissions", action: "ver", scope: "own" },
    { resource: "finance.commissions", action: "criar", scope: "own" },
    { resource: "reservations", action: "ver", scope: "all" },
  ];

  it("can() confere recurso e ação, não só o recurso", () => {
    expect(can(matriz, "finance.commissions", "criar")).toBe(true);
    expect(can(matriz, "finance.commissions", "excluir")).toBe(false);
    expect(can(matriz, "reservations", "criar")).toBe(false);
  });

  it("escopoDe() devolve null para o que o perfil não alcança", () => {
    expect(escopoDe(matriz, "finance.commissions")).toBe("own");
    expect(escopoDe(matriz, "finance.receivables")).toBeNull();
  });

  it("porRecurso() agrupa e marca o recurso inteiro como own", () => {
    // Uma única ação em `own` já restringe o recurso: a tela não pode rotular
    // como "todos" uma lista que o SQL vai filtrar pelo dono.
    expect(porRecurso(matriz)).toEqual([
      { recurso: "finance.commissions", acoes: ["ver", "criar"], escopo: "own" },
      { recurso: "reservations", acoes: ["ver"], escopo: "all" },
    ]);
  });
});

/**
 * A propriedade que o conserto instalou, dita de uma vez: **o menu é função
 * apenas da matriz**. Não do `code` do perfil, não de uma lista compilada, não
 * da ordem em que as permissões chegaram.
 *
 * Os casos acima são exemplos escolhidos a dedo — o corretor do seed, um perfil
 * novo, a matriz vazia. Estes aqui varrem **todas as matrizes possíveis** sobre
 * os recursos que o menu consulta (2^n combinações, uma por subconjunto),
 * contra códigos de perfil arbitrários. É barato porque o menu é curto, e é o
 * que fecha a porta: nenhum `if role ==` sobrevive a uma varredura exaustiva.
 */
describe("propriedade: o menu é função só da matriz", () => {
  const RECURSOS_DO_MENU = navigation.map((item) => item.recurso);

  /** Códigos arbitrários: os três do seed, dois que nasceriam em `/roles` sem
   *  deploy e um vazio, que é o que um `/auth/me` incompleto entregaria. */
  const CODIGOS = ["admin", "usuario", "corretor", "plantonista", "gerente-de-eventos", ""];

  /** Todas as combinações de recursos concedidos, uma por bit da máscara. */
  function* matrizesPossiveis(): Generator<{ concedidos: string[]; permissoes: Permissao[] }> {
    for (let mascara = 0; mascara < 1 << RECURSOS_DO_MENU.length; mascara++) {
      const concedidos = RECURSOS_DO_MENU.filter((_, i) => (mascara >> i) & 1);
      yield { concedidos, permissoes: permissoesDe(concedidos) };
    }
  }

  const menuDe = (role: string, permissoes: Permissao[]) =>
    navegacaoVisivel({ role }, permissoes).map((item) => item.href).join(" ");

  it("dois perfis com a mesma matriz e códigos diferentes veem exatamente o mesmo menu", () => {
    // Em linguagem de negócio: a gestão copia a matriz do Corretor para um
    // perfil "Plantonista", célula por célula. Os dois times têm que enxergar a
    // mesma tela — a diferença que existia antes não estava na tela de perfis,
    // estava no código, e ninguém conseguia explicá-la olhando o sistema.
    const divergencias: string[] = [];

    for (const { concedidos, permissoes } of matrizesPossiveis()) {
      const referencia = menuDe(CODIGOS[0], permissoes);
      for (const code of CODIGOS.slice(1)) {
        const menu = menuDe(code, permissoes);
        if (menu !== referencia) {
          divergencias.push(`[${concedidos.join(",")}] → ${CODIGOS[0]}: "${referencia}" ≠ ${code}: "${menu}"`);
        }
      }
    }

    expect(divergencias.slice(0, 3), `${divergencias.length} matrizes dependeram do code do perfil`).toEqual([]);
  });

  it("o menu é exatamente os itens cujo recurso a matriz concede, na ordem da fonte", () => {
    // O contrapeso: "igual para todo mundo" também seria satisfeito por
    // "mostrar tudo para todo mundo". Aqui a igualdade é com a matriz, item a
    // item — nada aparece sem concessão, nada some com concessão, e a ordem é a
    // do `navigation.ts` (menu que troca de ordem desorienta quem alterna de
    // conta).
    const divergencias: string[] = [];

    for (const { concedidos, permissoes } of matrizesPossiveis()) {
      const esperado = navigation
        .filter((item) => concedidos.includes(item.recurso))
        .map((item) => item.href)
        .join(" ");
      const menu = menuDe("plantonista", permissoes);
      if (menu !== esperado) divergencias.push(`[${concedidos.join(",")}] → "${menu}" (esperado "${esperado}")`);
    }

    expect(divergencias.slice(0, 3), `${divergencias.length} matrizes divergiram`).toEqual([]);
  });

  it("escopo não muda o menu; ação muda", () => {
    // `own` foi exatamente o caso do corretor com a agenda: escopo restringe o
    // QUE se vê dentro da tela (é o `WHERE` do repositório), nunca se a tela
    // existe. Já a ação importa: quem só pode criar não recebe o item de menu,
    // porque abrir a tela é lê-la.
    const emAll = permissoesDe(RECURSOS_DO_MENU, "all");
    const emOwn = permissoesDe(RECURSOS_DO_MENU, "own");

    expect(menuDe("corretor", emOwn)).toBe(menuDe("corretor", emAll));
    expect(menuDe("corretor", permissoesDe(["agenda"], "own", "criar"))).toBe("");
  });
});
