import Link from "next/link";
import { CalendarDays, Calculator, Package, ScrollText, Table2 } from "lucide-react";

import { SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import { CardDescription, CardTitle } from "@/components/ui/card";
import type { Acao } from "@/lib/api/types";
import { can } from "@/lib/auth/permissions";
import type { RecursoCodigo } from "@/lib/auth/recursos";
import { requireSession } from "@/lib/auth/session";

export const metadata = { title: "Configurações" };

type Area = {
  titulo: string;
  descricao: string;
  href: string;
  recurso: RecursoCodigo;
  acao: Acao;
  icone: React.ComponentType<{ className?: string }>;
};

/**
 * As telas que definem **como o sistema vende**.
 *
 * Cada área declara o par recurso × ação que a própria tela exige — a mesma
 * chave que a OpenAPI declara em `x-rbac` e que a API confere no middleware.
 * "Unidades e produtos" exige `inventory`, não `settings`, embora more aqui
 * dentro: o agrupamento é editorial, a permissão é do recurso.
 */
const AREAS: Area[] = [
  {
    // Era "Inventário" até 07/10/2026. O nome passou a ser do inventário de
    // BENS (menu Operação, recurso `inventory.goods`); este cartão é o cadastro
    // comercial (recurso `inventory`, rótulo "Cadastro de unidades e produtos").
    titulo: "Unidades e produtos",
    descricao: "O que você vende (produtos), os apartamentos de verdade (unidades) e quais apartamentos formam cada produto.",
    href: "/app/configuracoes/inventario",
    recurso: "inventory",
    acao: "ver",
    icone: Package,
  },
  {
    titulo: "Tarifário",
    descricao: "O preço da diária de cada produto em cada tipo de data, e o mínimo de noites por período.",
    href: "/app/configuracoes/tarifario",
    recurso: "settings",
    acao: "ver",
    icone: Table2,
  },
  {
    titulo: "Calendário comercial",
    descricao: "Feriados e temporadas — o que define se uma noite é normal, fim de semana, feriado ou alta.",
    href: "/app/configuracoes/calendario",
    recurso: "settings",
    acao: "ver",
    icone: CalendarDays,
  },
  {
    titulo: "Política comercial",
    descricao: "Sinal, prazos, pré-reserva, limites de desconto e regras de cancelamento. Ao salvar, as novas regras passam a valer.",
    href: "/app/configuracoes/politica",
    recurso: "settings",
    acao: "ver",
    icone: ScrollText,
  },
];

export default async function ConfiguracoesPage() {
  const { permissions } = await requireSession();

  const areas = AREAS.filter((area) => can(permissions, area.recurso, area.acao));
  // O guard desta tela é a união das áreas, não `settings` sozinho: quem tem
  // `inventory` mas não `settings` (o perfil `usuario` do seed) precisa chegar
  // a unidades e produtos, e uma porta trancada com a chave errada só faria essa pessoa
  // digitar a URL da tela de dentro na mão.
  if (areas.length === 0) return <SemAcesso recurso="settings" />;

  const podeOrcar = can(permissions, "quotes", "criar");

  return (
    <Tela>
      <CabecalhoDeTela
        titulo="Configurações"
        descricao="É aqui que se decide como o sistema vende: o que existe para vender, quanto custa cada noite e o que acontece quando alguém desiste."
      />

      <Nota>
        Mudanças aqui <strong>nunca alteram o passado</strong>. Mudar preço, feriado, período ou política
        vale para os próximos orçamentos; cada reserva já feita continua com o preço e as regras que
        valiam quando foi criada.
      </Nota>

      <div className="grid gap-3 sm:grid-cols-2">
        {areas.map((area) => (
          <Link
            key={area.href}
            href={area.href}
            className="group rounded-xl border border-border/60 bg-muted/25 p-4 transition-colors outline-none hover:bg-muted/45 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-card"
          >
            <div className="flex items-start gap-3">
              <area.icone className="mt-0.5 size-5 shrink-0 text-primary" aria-hidden="true" />
              <div className="min-w-0">
                <CardTitle>{area.titulo}</CardTitle>
                <CardDescription className="mt-1">{area.descricao}</CardDescription>
              </div>
            </div>
          </Link>
        ))}

        {podeOrcar ? (
          <Link
            href="/app/orcamento"
            className="group rounded-xl border border-transparent bg-brand-gradient p-4 text-white transition-[filter] outline-none hover:brightness-110 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-card"
          >
            <div className="flex items-start gap-3">
              <Calculator className="mt-0.5 size-5 shrink-0" aria-hidden="true" />
              <div className="min-w-0">
                <CardTitle className="text-white">Simular um orçamento</CardTitle>
                <CardDescription className="mt-1 text-white/80">
                  Confira se a configuração está certa: o mesmo cálculo usado na venda, mostrado noite a
                  noite.
                </CardDescription>
              </div>
            </div>
          </Link>
        ) : null}
      </div>
    </Tela>
  );
}
