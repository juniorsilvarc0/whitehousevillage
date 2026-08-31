import { existsSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { CATALOGO, EM_CONSTRUCAO, mobileTabs, navigation } from "@/config/navigation";
import { RECURSOS_DO_CATALOGO } from "@/lib/auth/recursos";

/**
 * O que este arquivo guarda é a **integridade do catálogo de destinos**: destino
 * repetido, item sem recurso, recurso fora do vocabulário do banco, aba de
 * celular apontando para um item que não existe — e, desde 27/08/2026, a
 * correspondência entre o menu e as telas que realmente estão na pasta.
 *
 * Quem vê o quê **não se testa aqui por nome de papel**. Os casos antigos
 * (`navigationFor("corretor")` não contém `/app/financeiro`…) descreviam o
 * defeito, não a regra: um deles chegava a exigir `/app/agenda` ESCONDIDO do
 * corretor, enquanto o seed concede `agenda` em escopo `own` a esse mesmo
 * perfil. Eles morreram junto com `allowedRoles`, e a garantia que carregavam —
 * spec §11, "corretor nunca vê financeiro global, dados de outros corretores ou
 * configurações" — passou a ser afirmada onde ela de fato mora, contra a matriz
 * do seed, em `src/lib/auth/permissions.test.ts` e `menu-e-dado.test.ts`.
 *
 * Esconder não é autorizar: quem recusa de verdade é o middleware da API.
 */

const AQUI = path.dirname(fileURLToPath(import.meta.url));
const RAIZ_ADMIN = path.resolve(AQUI, "../..");
/** As telas autenticadas vivem no grupo de rotas `(app)`, que não entra na URL. */
const RAIZ_DAS_TELAS = path.join(RAIZ_ADMIN, "src/app/(app)");

/** `/app/mapa` → `src/app/(app)/app/mapa/page.tsx`. */
function arquivoDaTela(href: string): string {
  return path.join(RAIZ_DAS_TELAS, href, "page.tsx");
}

function telaExiste(href: string): boolean {
  return existsSync(arquivoDaTela(href));
}

/**
 * Os destinos de **primeiro nível** que existem no disco: `/app` e
 * `/app/<segmento>` com `page.tsx`.
 *
 * Só o primeiro nível conta como destino de menu. `/app/configuracoes/tarifario`
 * é sub-tela (chega-se a ela por dentro de Configurações) e
 * `/app/oportunidades/[id]` é rota de detalhe, que não tem como virar item de
 * menu — não existe "a oportunidade" no singular para linkar.
 */
function telasDePrimeiroNivel(): string[] {
  const raiz = path.join(RAIZ_DAS_TELAS, "app");
  const destinos = existsSync(path.join(raiz, "page.tsx")) ? ["/app"] : [];
  for (const entrada of readdirSync(raiz, { withFileTypes: true })) {
    if (!entrada.isDirectory()) continue;
    // `[id]`, `[...slug]` e afins: rota dinâmica não é destino de menu.
    if (entrada.name.startsWith("[") || entrada.name.startsWith("(") || entrada.name.startsWith("@")) continue;
    if (existsSync(path.join(raiz, entrada.name, "page.tsx"))) destinos.push(`/app/${entrada.name}`);
  }
  return destinos.sort();
}

describe("catálogo de navegação", () => {
  it("não repete destino", () => {
    const vistos = CATALOGO.map((item) => item.href);
    expect(new Set(vistos).size).toBe(vistos.length);
  });

  it("todo item declara um recurso do catálogo do banco", () => {
    // O tipo `RecursoCodigo` já recusa código inventado no build; este caso é o
    // que sobra depois de um `as` distraído. Item sem recurso seria item fora da
    // matriz — o buraco por onde `allowedRoles` voltaria — e recurso fora do
    // catálogo esconde a tela de TODO MUNDO, inclusive do admin, sem 403 nenhum
    // para denunciar: ninguém tem permissão num recurso que não existe.
    for (const item of CATALOGO) {
      expect(item.recurso, `${item.href} não declara recurso`).toBeTruthy();
      expect(
        RECURSOS_DO_CATALOGO,
        `${item.href} → "${item.recurso}" não está no catálogo`,
      ).toContain(item.recurso);
    }
  });

  it("as abas do celular existem no menu", () => {
    // A barra inferior remonta o item pelo href (`MobileNav`); aba sem item
    // correspondente simplesmente não desenha, e ninguém percebe.
    for (const aba of mobileTabs) {
      expect(
        navigation.find((n) => n.href === aba),
        `aba ${aba} não existe no menu entregue`,
      ).toBeDefined();
    }
  });

  it("as abas do celular cabem na barra", () => {
    // Cinco é o que cabe num aparelho estreito sem o rótulo virar reticências.
    expect(mobileTabs.length).toBeLessThanOrEqual(5);
  });
});

/**
 * ════════════════════════════════════════════════════════════════════════════
 * O menu contra o disco — a guarda da Dívida A.
 *
 * O defeito, medido em 27/08/2026 com o stack no ar: **8 dos 13 itens do menu
 * respondiam 404**. Não era só um link morto — `next/link` faz *prefetch*, e a
 * gaveta de navegação desenha o menu inteiro de uma vez, então cada abertura
 * disparava uma rajada de buscas de rota inexistente. E o inverso também valia:
 * `/app/orcamento` existia e não estava no menu.
 *
 * Estes dois casos são o que impede o defeito de voltar **nas duas direções**, e
 * são o que faz o item reaparecer sem ninguém lembrar dele: o disco é a fonte da
 * verdade e a marca `emConstrucao` é uma afirmação sobre o disco. Se a
 * afirmação envelhecer — porque a tela nasceu, ou porque alguém a marcou como
 * pronta antes da hora —, o CI fica vermelho dizendo exatamente o que apagar.
 */
describe("o menu é o que o painel entrega", () => {
  it("nenhum item do menu leva a 404", () => {
    const mortos = navigation
      .filter((item) => !telaExiste(item.href))
      .map((item) => `${item.title} → ${item.href} (falta ${path.relative(RAIZ_ADMIN, arquivoDaTela(item.href))})`);

    expect(
      mortos,
      "item de menu sem page.tsx: ou a tela vem junto, ou o item recebe `emConstrucao: true` em src/config/navigation.ts",
    ).toEqual([]);
  });

  it("tela entregue não fica escondida atrás de `emConstrucao`", () => {
    // Este é o caso que faz o item "voltar sozinho". Quem entregar
    // `app/(app)/app/agenda/page.tsx` vê esta falha nomeando a linha a corrigir
    // — e a correção é apagar uma palavra.
    const nascidas = EM_CONSTRUCAO.filter((item) => telaExiste(item.href)).map(
      (item) => `${item.title} → ${item.href}: a tela existe; apague \`emConstrucao: true\` da linha dele`,
    );

    expect(nascidas, "src/config/navigation.ts está atrás do que a pasta já entrega").toEqual([]);
  });

  it("nenhuma tela de primeiro nível fica fora do catálogo", () => {
    // A direção que ninguém lembra de checar: a tela existe, funciona, e não há
    // como chegar nela. Foi o caso de `/app/orcamento`, que estava no ar desde a
    // Fase 1 e não aparecia em menu nenhum — quem não soubesse a URL não a
    // encontrava.
    const conhecidos = new Set(CATALOGO.map((item) => item.href));
    const orfas = telasDePrimeiroNivel().filter((href) => !conhecidos.has(href));

    expect(orfas, "tela sem item de menu — ninguém chega nela sem saber a URL de cor").toEqual([]);
  });

  it("item em construção descreve quando a tela chega", () => {
    // O que se perde ao esconder é a silhueta do produto. Esta frase é o que o
    // Painel mostra no lugar — sem link, para ninguém clicar num 404.
    for (const item of EM_CONSTRUCAO) {
      expect(item.quando, `${item.href} está escondido e não diz por quê`).toBeTruthy();
    }
  });
});
