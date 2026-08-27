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
 * O inventário exige `inventory`, não `settings`, embora more aqui dentro: o
 * agrupamento é editorial, a permissão é do recurso.
 */
const AREAS: Area[] = [
  {
    titulo: "Inventário",
    descricao: "Produtos, unidades físicas e a composição que liga os dois — inclusive a que fecha a casa inteira.",
    href: "/app/configuracoes/inventario",
    recurso: "inventory",
    acao: "ver",
    icone: Package,
  },
  {
    titulo: "Tarifário",
    descricao: "A grade produto × tipo de data, a precedência que a explica e o mínimo de noites por período.",
    href: "/app/configuracoes/tarifario",
    recurso: "settings",
    acao: "ver",
    icone: Table2,
  },
  {
    titulo: "Calendário comercial",
    descricao: "Feriados e períodos especiais — o que decide o tipo de cada noite antes de a tarifa ser buscada.",
    href: "/app/configuracoes/calendario",
    recurso: "settings",
    acao: "ver",
    icone: CalendarDays,
  },
  {
    titulo: "Política comercial",
    descricao: "Sinal, prazos, pré-reserva, alçadas de desconto e faixas de cancelamento. Versionada: salvar publica.",
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
  // ao inventário, e uma porta trancada com a chave errada só faria essa pessoa
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
        Tudo nestas telas é <strong>dado versionado</strong>, não constante em código — e nada reescreve o
        passado. Mudar tarifa, feriado, período ou política vale do próximo cálculo em diante; cada reserva
        guarda o preço, a tabela e a versão da política que usou quando foi criada.
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
                  A prova de que a configuração está certa: o mesmo motor que vende, com o cálculo aberto
                  noite a noite.
                </CardDescription>
              </div>
            </div>
          </Link>
        ) : null}
      </div>
    </Tela>
  );
}
