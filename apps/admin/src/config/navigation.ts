import type { ComponentType } from "react";
import {
  CalendarRange, LayoutGrid, ClipboardList, Columns3, MessageCircle,
  CalendarDays, Wallet, Users, Package, Share2, ChartLine, SlidersHorizontal,
} from "lucide-react";

/** Os três perfis de hoje. Novos perfis entram por configuração, não por código. */
export type Role = "admin" | "usuario" | "corretor";

export type NavGroup = "Operação" | "Comercial" | "Análise" | "Administração";

export type NavItem = {
  title: string;
  href: string;
  icon: ComponentType<{ className?: string }>;
  group: NavGroup;
  /** Lista explícita: um perfil novo nunca ganha acesso por omissão.
   *  O guard de verdade é no servidor — isto aqui apenas esconde. */
  allowedRoles: readonly Role[];
};

const TODOS: readonly Role[] = ["admin", "usuario", "corretor"];
const GESTAO: readonly Role[] = ["admin", "usuario"];
const SO_ADMIN: readonly Role[] = ["admin"];

export const navigation: NavItem[] = [
  { title: "Painel",         href: "/app",              icon: LayoutGrid,        group: "Operação",       allowedRoles: TODOS },
  { title: "Mapa",           href: "/app/mapa",         icon: CalendarRange,     group: "Operação",       allowedRoles: TODOS },
  { title: "Reservas",       href: "/app/reservas",     icon: ClipboardList,     group: "Operação",       allowedRoles: TODOS },
  { title: "Funil",          href: "/app/funil",        icon: Columns3,          group: "Comercial",      allowedRoles: TODOS },
  { title: "WhatsApp",       href: "/app/chat",         icon: MessageCircle,     group: "Comercial",      allowedRoles: TODOS },
  { title: "Agenda",         href: "/app/agenda",       icon: CalendarDays,      group: "Operação",       allowedRoles: GESTAO },
  { title: "Financeiro",     href: "/app/financeiro",   icon: Wallet,            group: "Análise",        allowedRoles: GESTAO },
  { title: "Comissões",      href: "/app/comissoes",    icon: Users,             group: "Comercial",      allowedRoles: TODOS },
  { title: "Inventário",     href: "/app/inventario",   icon: Package,           group: "Operação",       allowedRoles: GESTAO },
  { title: "Canais",         href: "/app/canais",       icon: Share2,            group: "Administração",  allowedRoles: GESTAO },
  { title: "Relatórios",     href: "/app/relatorios",   icon: ChartLine,         group: "Análise",        allowedRoles: GESTAO },
  { title: "Configurações",  href: "/app/configuracoes",icon: SlidersHorizontal, group: "Administração",  allowedRoles: SO_ADMIN },
];

/** As abas da barra inferior do celular — escolha explícita, não um slice dos
 *  primeiros itens. A gestão atende do celular: WhatsApp e Mapa precisam estar
 *  a um toque, não escondidos atrás de "Mais". */
export const mobileTabs = ["/app", "/app/mapa", "/app/reservas", "/app/funil", "/app/chat"] as const;

export function navigationFor(role: Role): NavItem[] {
  return navigation.filter((item) => item.allowedRoles.includes(role));
}
