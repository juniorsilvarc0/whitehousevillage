import { type NavGroup, type NavItem, navigation } from "@/config/navigation";

/**
 * A travessia RSC → cliente não aceita o `icon` do `NavItem` (é uma função).
 * Por isso o servidor manda só os **hrefs** visíveis e o cliente remonta os
 * itens a partir do `navigation.ts`, que ele já importa de qualquer forma.
 * A ordem vem da fonte, não do array recebido: menu que muda de ordem entre
 * dois papéis desorienta quem alterna de conta.
 */
export function itensPorHref(hrefs: readonly string[]): NavItem[] {
  const permitidos = new Set(hrefs);
  return navigation.filter((item) => permitidos.has(item.href));
}

export type SecaoDeNavegacao = { grupo: NavGroup; itens: NavItem[] };

export function agruparPorSecao(itens: NavItem[]): SecaoDeNavegacao[] {
  const ordem: NavGroup[] = ["Operação", "Comercial", "Análise", "Administração"];
  return ordem
    .map((grupo) => ({ grupo, itens: itens.filter((item) => item.group === grupo) }))
    .filter((secao) => secao.itens.length > 0);
}

/**
 * `/app` só está ativo em `/app` exato — como prefixo ele acenderia junto com
 * todas as outras telas, já que todas descem dele.
 */
export function estaAtivo(pathname: string, href: string): boolean {
  if (href === "/app") return pathname === "/app";
  return pathname === href || pathname.startsWith(`${href}/`);
}
