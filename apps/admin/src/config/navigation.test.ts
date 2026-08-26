import { describe, expect, it } from "vitest";

import { mobileTabs, navigation } from "@/config/navigation";
import { RECURSOS_DO_CATALOGO } from "@/lib/auth/recursos";

/**
 * O que este arquivo guarda é a **integridade do catálogo de destinos**: destino
 * repetido, item sem recurso, recurso fora do vocabulário do banco, aba de
 * celular apontando para um item que não existe.
 *
 * Quem vê o quê **não se testa mais por nome de papel**. Os casos antigos
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

describe("catálogo de navegação", () => {
  it("não repete destino", () => {
    const vistos = navigation.map((item) => item.href);
    expect(new Set(vistos).size).toBe(vistos.length);
  });

  it("todo item declara um recurso do catálogo do banco", () => {
    // O tipo `RecursoCodigo` já recusa código inventado no build; este caso é o
    // que sobra depois de um `as` distraído. Item sem recurso seria item fora da
    // matriz — o buraco por onde `allowedRoles` voltaria — e recurso fora do
    // catálogo esconde a tela de TODO MUNDO, inclusive do admin, sem 403 nenhum
    // para denunciar: ninguém tem permissão num recurso que não existe.
    for (const item of navigation) {
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
        `aba ${aba} não existe no navigation.ts`,
      ).toBeDefined();
    }
  });
});
