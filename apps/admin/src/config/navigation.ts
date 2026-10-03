import type { ComponentType } from "react";
import {
  CalendarRange, LayoutGrid, ClipboardList, Columns3, ContactRound, MessageCircle,
  CalendarDays, Wallet, Users, Package, Share2, ChartLine, SlidersHorizontal, Calculator, BookUser, Globe,
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
 *
 * ════════════════════════════════════════════════════════════════════════════
 *
 * ## O menu mentia — 8 dos 13 destinos levavam a 404
 *
 * Medido em 27/08/2026, com o stack no ar (painel em :3100, sessão de
 * `admin@wh.local`), pedindo cada href do menu:
 *
 * ```
 * 200  /app             200  /app/mapa        200  /app/funil
 * 200  /app/leads       200  /app/configuracoes
 * 404  /app/reservas    404  /app/chat        404  /app/agenda
 * 404  /app/financeiro  404  /app/comissoes   404  /app/inventario
 * 404  /app/canais      404  /app/relatorios
 * ```
 *
 * E o inverso também: `/app/orcamento` respondia `200` e **não estava no menu**.
 *
 * Não é só um link quebrado. `next/link` faz *prefetch* do destino assim que o
 * item entra na viewport — então a gaveta de navegação, que desenha o menu
 * inteiro de uma vez, disparava oito buscas de rota inexistente por abertura.
 * O custo real é o de quem apresenta o sistema: clicar em "Financeiro" na
 * frente do proprietário e cair numa página de erro.
 *
 * ## A decisão: esconder, e o que ela custa
 *
 * As duas saídas possíveis eram **esconder o que não existe** ou **manter
 * visível e desabilitado, rotulado "em construção"**. Escolhi esconder.
 *
 * O que se ganha: o menu volta a ser uma afirmação verdadeira — "isto é o que o
 * sistema faz". E a garantia é de **um ponto só**: `navigation` é a lista que a
 * gaveta, a barra do celular, a paleta de comandos (⌘K) e os atalhos do Painel
 * consomem, todos por interseção com os hrefs que o servidor manda. Item que não
 * está aqui não é renderizado por ninguém, não vira `<Link>` e portanto não é
 * prefetchado. Não há um quarto lugar que possa esquecer de honrar a marca.
 *
 * O que se perde, e é uma perda real: quem apresenta o sistema não vê mais a
 * silhueta do produto inteiro. Um menu com oito itens acinzentados conta a
 * história "vem mais por aí"; um menu com sete itens conta "é isto". A
 * compensação está no Painel (`app/(app)/app/page.tsx`), que lista
 * `EM_CONSTRUCAO` **como texto, sem link** — a noção do todo sobrevive num lugar
 * onde ninguém clica por engano.
 *
 * A variante "visível e desabilitado" foi recusada por um motivo mecânico, não
 * estético: ela exige que **três** componentes de renderização honrem a marca
 * (gaveta, barra do celular, paleta). O primeiro que esquecer volta a publicar
 * um `<Link>` para 404, com prefetch, e o defeito reaparece pela porta de quem
 * não leu este arquivo. Esconder é impossível de esquecer.
 *
 * ## Como o item volta quando a tela nascer
 *
 * O catálogo abaixo continua listando **todos** os destinos — nenhum foi
 * apagado. O que separa "no menu" de "escondido" é uma palavra na linha do item:
 * `emConstrucao: true`.
 *
 * E ninguém precisa lembrar de apagá-la: `navigation.test.ts` **varre
 * `src/app/(app)/` atrás de `page.tsx`** e compara o resultado com estas marcas,
 * nos dois sentidos. No minuto em que `app/(app)/app/agenda/page.tsx` existir, a
 * suíte fica vermelha dizendo o arquivo, a linha e a palavra a apagar; e um item
 * marcado como pronto sem tela na pasta reprova do mesmo jeito. O disco é a
 * fonte da verdade; esta lista é uma afirmação sobre ele que o CI confere.
 *
 * **O custo honesto dessa escolha:** não é o item que "volta sozinho", é o teste
 * que impede o menu de ficar dessincronizado — a correção é de uma palavra e o
 * teste aponta qual. A versão realmente automática (derivar do disco em tempo de
 * execução) foi tentada e não sobrevive: este módulo é importado por componentes
 * `"use client"` (`mobile-nav.tsx`, `nav-utils.ts`), onde `node:fs` não existe e
 * o build quebra na hora — e mesmo do lado do servidor o `src/` não é embarcado
 * no build de produção, então a varredura acertaria em desenvolvimento e
 * mentiria em produção. Derivar em tempo de *build* precisaria de um passo de
 * codegen no `package.json`, que é de outro dono; está no relatório.
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
  /**
   * A tela ainda não existe no painel — não há `page.tsx` para este href.
   *
   * **Não é permissão e não substitui permissão.** Um item sem esta marca ainda
   * pode ficar invisível para quem não tem o recurso na matriz; um item com ela
   * fica invisível para todo mundo, inclusive o admin, porque o destino não
   * existe para ninguém. Os dois filtros são independentes e se compõem:
   * `navigation` (o que existe) ∩ matriz (o que o perfil alcança).
   *
   * Apagar esta linha é o gesto de "a tela nasceu". `navigation.test.ts` cobra.
   */
  emConstrucao?: true;
  /** Uma frase sobre o que a tela vai ser e quando — mostrada no Painel, sem
   *  link. É o que preserva a noção do todo depois de esconder o item. */
  quando?: string;
};

/**
 * O produto inteiro, entregue ou não — a silhueta que se mostra numa reunião.
 *
 * A ordem daqui é a ordem do menu (e a de `EM_CONSTRUCAO`): menu que troca de
 * ordem entre dois perfis desorienta quem alterna de conta.
 *
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
export const CATALOGO: NavItem[] = [
  { title: "Painel",        href: "/app",               icon: LayoutGrid,        group: "Operação",      recurso: "dashboard" },
  { title: "Mapa",          href: "/app/mapa",          icon: CalendarRange,     group: "Operação",      recurso: "calendar" },

  // Estava marcada `emConstrucao` quando esta rodada começou. O `page.tsx`
  // nasceu no meio dela, por outro agente do painel, e quem avisou foi
  // `navigation.test.ts` ficando vermelho e nomeando a linha — que é exatamente
  // o mecanismo que a marca existe para acionar. Um item, uma palavra apagada.
  { title: "Reservas",      href: "/app/reservas",      icon: ClipboardList,     group: "Operação",      recurso: "reservations" },

  { title: "Contatos",      href: "/app/contatos",      icon: BookUser,      group: "Comercial",     recurso: "contacts" },
  { title: "Orçamento",     href: "/app/orcamento",     icon: Calculator,        group: "Comercial",     recurso: "quotes" },
  { title: "Funil",         href: "/app/funil",         icon: Columns3,          group: "Comercial",     recurso: "crm.opportunities" },
  { title: "Leads",         href: "/app/leads",         icon: ContactRound,      group: "Comercial",     recurso: "crm.leads" },

  { title: "WhatsApp",      href: "/app/chat",          icon: MessageCircle,     group: "Comercial",     recurso: "chat",
    emConstrucao: true, quando: "Em breve — atendimento por WhatsApp dentro do sistema." },
  { title: "Agenda",        href: "/app/agenda",        icon: CalendarDays,      group: "Operação",      recurso: "agenda",
    emConstrucao: true, quando: "Em breve — agenda da operação e visitas do corretor." },
  { title: "Financeiro",    href: "/app/financeiro",    icon: Wallet,            group: "Análise",       recurso: "finance.receivables",
    emConstrucao: true, quando: "Em breve — contas a receber, a pagar e conferência de pagamentos." },
  { title: "Comissões",     href: "/app/comissoes",     icon: Users,             group: "Comercial",     recurso: "finance.commissions",
    emConstrucao: true, quando: "Em breve — comissões dos corretores." },
  // A *configuração* do inventário já existe, em Configurações → Inventário.
  // Este item é a tela **operacional** (ordens de manutenção, enxoval), que é
  // outra coisa e é de outra fase. Repontá-lo para a tela de configuração seria
  // criar dois caminhos para o mesmo lugar e acender dois itens do menu ao mesmo
  // tempo, porque `estaAtivo()` casa por prefixo.
  { title: "Inventário",    href: "/app/inventario",    icon: Package,           group: "Operação",      recurso: "inventory",
    emConstrucao: true, quando: "Em breve — manutenção e controle dos apartamentos. O cadastro já está em Configurações." },
  { title: "Canais",        href: "/app/canais",        icon: Share2,            group: "Administração", recurso: "channels",
    emConstrucao: true, quando: "Em breve — reservas do Airbnb e do Booking direto no calendário." },
  { title: "Relatórios",    href: "/app/relatorios",    icon: ChartLine,         group: "Análise",       recurso: "reports",
    emConstrucao: true, quando: "Em breve — relatórios de ocupação, diária média e vendas." },

  // Textos, fotos e vídeos do site de vendas (docs/site-cms.md).
  { title: "Site",          href: "/app/site",          icon: Globe,             group: "Administração", recurso: "site" },

  { title: "Configurações", href: "/app/configuracoes", icon: SlidersHorizontal, group: "Administração", recurso: "settings" },
];

/**
 * O menu de verdade: os destinos que existem.
 *
 * É esta a lista que `navegacaoVisivel()` filtra pela matriz, e é dela que saem
 * a gaveta, a barra do celular, a paleta de comandos e os atalhos do Painel.
 */
export const navigation: NavItem[] = CATALOGO.filter((item) => !item.emConstrucao);

/** O que o produto ainda não entregou — para o Painel dizer em voz alta, sem
 *  link. Derivado, nunca declarado: não há uma segunda lista para desalinhar. */
export const EM_CONSTRUCAO: NavItem[] = CATALOGO.filter((item) => item.emConstrucao);

/**
 * As abas da barra inferior do celular.
 *
 * A ordem é **editorial**, não um slice dos primeiros itens: a gestão atende do
 * celular, então WhatsApp e Mapa precisam estar a um toque, não escondidos atrás
 * de "Mais". O que a barra mostra são as **cinco primeiras que existem** — quando
 * a tela de WhatsApp nascer, ela entra na barra sozinha e a última cai, sem
 * ninguém editar nada aqui.
 *
 * Quem alcança cada aba continua sendo a matriz: a `MobileNav` intersecta esta
 * lista com os hrefs visíveis que o servidor mandou.
 */
const PREFERENCIA_NO_CELULAR = [
  "/app",
  "/app/mapa",
  "/app/reservas",
  "/app/chat",
  "/app/funil",
  "/app/contatos",
  "/app/leads",
] as const;

const MAXIMO_DE_ABAS = 5;

export const mobileTabs: readonly string[] = PREFERENCIA_NO_CELULAR.filter((href) =>
  navigation.some((item) => item.href === href),
).slice(0, MAXIMO_DE_ABAS);
