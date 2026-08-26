import type { ComponentType } from "react";
import {
  CalendarRange, LayoutGrid, ClipboardList, Columns3, MessageCircle,
  CalendarDays, Wallet, Users, Package, Share2, ChartLine, SlidersHorizontal,
} from "lucide-react";

import type { RecursoCodigo } from "@/lib/auth/recursos";

/**
 * Catálogo de destinos do painel.
 *
 * **Não existe mais `allowedRoles`.** A lista de papéis escrita aqui era a
 * `if role == "corretor"` que a regra 8 do CLAUDE.md proíbe, só que em
 * TypeScript — e mentia para a gestão: o perfil `corretor` recebe `agenda` em
 * escopo `own` no seed, a tela de perfis mostrava a permissão marcada e salva, e
 * o item não aparecia no menu porque a constante `GESTAO` não citava
 * `"corretor"`. Marcar de novo não mudava nada, porque a diferença estava no
 * código, num lugar que a gestão não tem como ver.
 *
 * Agora cada item declara **um recurso do catálogo do banco** e a visibilidade é
 * função só disso: `navegacaoVisivel()` mostra o item quando a matriz de
 * `/auth/me` concede `ver` no recurso. Perfil novo criado em `/roles` não é caso
 * especial nenhum — nasce com o menu que a matriz dele descreve, sem deploy.
 *
 * **Esconder não é autorizar**: o guard real é o middleware da API, que responde
 * 403 de qualquer jeito. Isto aqui só evita oferecer um botão que vai falhar.
 */

export type NavGroup = "Operação" | "Comercial" | "Análise" | "Administração";

export type NavItem = {
  title: string;
  href: string;
  icon: ComponentType<{ className?: string }>;
  group: NavGroup;
  /**
   * Recurso do catálogo (`resources`) que o item exige em ação `ver`.
   *
   * Obrigatório e tipado como `RecursoCodigo` por dois motivos: item sem recurso
   * seria item fora da matriz — exatamente o buraco por onde `allowedRoles`
   * voltaria —, e código fora do catálogo esconderia a tela de todo mundo,
   * inclusive do admin, sem 403 nenhum para denunciar (ninguém pode ter
   * permissão num recurso que não existe).
   */
  recurso: RecursoCodigo;
};

/**
 * O Painel também declara recurso: `dashboard`, que existe no catálogo com o
 * rótulo "Painel" e é concedido a `admin`, `usuario` e `corretor` no seed.
 *
 * A alternativa era isentá-lo por ser a tela de chegada de quem loga. Foi
 * recusada: a tela de perfis oferece o checkbox "Painel" e desmarcá-lo não
 * mudaria coisa alguma — a mesma mentira que este arquivo acabou de perder, em
 * miniatura. Sem isenção, a regra fica sem exceção: menu é função da matriz.
 *
 * Nenhum perfil fica sem casa por causa disso: `/app` continua acessível pela
 * rota (o logo da barra leva até lá e é onde o login desemboca), e a própria
 * tela avisa quando o perfil não tem recurso liberado.
 *
 * Três destinos merecem nota, porque o nome da tela não é o nome do recurso: o
 * Mapa é a visão de `calendar`; o Financeiro abre nos recebíveis
 * (`finance.receivables` — pagáveis é aba de dentro, com permissão própria); e
 * Comissões é `finance.commissions`, o único financeiro que o corretor alcança,
 * em escopo `own`.
 */
export const navigation: NavItem[] = [
  { title: "Painel",         href: "/app",              icon: LayoutGrid,        group: "Operação",      recurso: "dashboard" },
  { title: "Mapa",           href: "/app/mapa",         icon: CalendarRange,     group: "Operação",      recurso: "calendar" },
  { title: "Reservas",       href: "/app/reservas",     icon: ClipboardList,     group: "Operação",      recurso: "reservations" },
  { title: "Funil",          href: "/app/funil",        icon: Columns3,          group: "Comercial",     recurso: "crm.opportunities" },
  { title: "WhatsApp",       href: "/app/chat",         icon: MessageCircle,     group: "Comercial",     recurso: "chat" },
  { title: "Agenda",         href: "/app/agenda",       icon: CalendarDays,      group: "Operação",      recurso: "agenda" },
  { title: "Financeiro",     href: "/app/financeiro",   icon: Wallet,            group: "Análise",       recurso: "finance.receivables" },
  { title: "Comissões",      href: "/app/comissoes",    icon: Users,             group: "Comercial",     recurso: "finance.commissions" },
  { title: "Inventário",     href: "/app/inventario",   icon: Package,           group: "Operação",      recurso: "inventory" },
  { title: "Canais",         href: "/app/canais",       icon: Share2,            group: "Administração", recurso: "channels" },
  { title: "Relatórios",     href: "/app/relatorios",   icon: ChartLine,         group: "Análise",       recurso: "reports" },
  { title: "Configurações",  href: "/app/configuracoes",icon: SlidersHorizontal, group: "Administração", recurso: "settings" },
];

/** As abas da barra inferior do celular — escolha editorial, não um slice dos
 *  primeiros itens. A gestão atende do celular: WhatsApp e Mapa precisam estar
 *  a um toque, não escondidos atrás de "Mais". Quem alcança cada aba continua
 *  sendo a matriz: a `MobileNav` intersecta esta lista com os hrefs visíveis. */
export const mobileTabs = ["/app", "/app/mapa", "/app/reservas", "/app/funil", "/app/chat"] as const;
