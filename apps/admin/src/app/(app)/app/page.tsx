import Link from "next/link";
import {
  CalendarRange, ChartLine, ClipboardList, Columns3, Info, MessageCircle,
  PlusCircle, ShieldCheck, SlidersHorizontal, Users, Wallet,
} from "lucide-react";

import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import type { Acao } from "@/lib/api/types";
import { can, escopoDe, porRecurso } from "@/lib/auth/permissions";
import type { RecursoCodigo } from "@/lib/auth/recursos";
import { requireSession } from "@/lib/auth/session";
import { dataPorExtenso, saudacao } from "@/lib/fuso";
import { cn } from "@/lib/utils";

export const metadata = { title: "Painel" };

type Atalho = {
  titulo: string;
  descricao: string;
  href: string;
  /** Código do catálogo do RBAC, não texto livre: atalho apontando para recurso
   *  inexistente não fica "protegido", fica invisível para todo mundo. */
  recurso: RecursoCodigo;
  acao: Acao;
  icone: React.ComponentType<{ className?: string }>;
  destaque?: boolean;
};

/**
 * Cada atalho declara o par recurso × ação que ele exige. É a mesma chave que a
 * API confere no middleware — quando a matriz do perfil muda, esta tela muda
 * junto, **sem deploy**, porque a fonte é `/auth/me` e não uma constante.
 */
const ATALHOS: Atalho[] = [
  { titulo: "Nova reserva", descricao: "Orçar, segurar a data e confirmar com o sinal.", href: "/app/reservas?novo=1", recurso: "reservations", acao: "criar", icone: PlusCircle, destaque: true },
  { titulo: "Mapa de ocupação", descricao: "Unidade por dia, com tarifa e pré-reservas expirando.", href: "/app/mapa", recurso: "calendar", acao: "ver", icone: CalendarRange },
  { titulo: "Reservas", descricao: "Pré-reservas, confirmadas, check-ins do período.", href: "/app/reservas", recurso: "reservations", acao: "ver", icone: ClipboardList },
  { titulo: "Funil", descricao: "Oportunidades por etapa, com SLA de resposta.", href: "/app/funil", recurso: "crm.opportunities", acao: "ver", icone: Columns3 },
  { titulo: "WhatsApp", descricao: "Conversas em aberto e assumidas pela equipe.", href: "/app/chat", recurso: "chat", acao: "ver", icone: MessageCircle },
  { titulo: "Financeiro", descricao: "Recebíveis, pagáveis e conciliação.", href: "/app/financeiro", recurso: "finance.receivables", acao: "ver", icone: Wallet },
  { titulo: "Comissões", descricao: "O que foi gerado, liberado e pago.", href: "/app/comissoes", recurso: "finance.commissions", acao: "ver", icone: Users },
  { titulo: "Relatórios", descricao: "Ocupação, ADR, RevPAR e conversão.", href: "/app/relatorios", recurso: "reports", acao: "ver", icone: ChartLine },
  { titulo: "Perfis e acessos", descricao: "A matriz papel × recurso × ação × escopo.", href: "/app/configuracoes", recurso: "settings", acao: "editar", icone: SlidersHorizontal },
];

const ROTULO_DA_ACAO: Record<Acao, string> = {
  ver: "ver",
  criar: "criar",
  editar: "editar",
  excluir: "excluir",
};

export default async function PainelPage() {
  const { user, permissions } = await requireSession();

  const atalhos = ATALHOS.filter((a) => can(permissions, a.recurso, a.acao));
  const matriz = porRecurso(permissions);
  const primeiroNome = user.name.split(" ")[0] ?? user.name;

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <header>
        <p className="text-xs uppercase tracking-[0.22em] text-muted-foreground">{dataPorExtenso()}</p>
        <h1 className="font-display mt-1 text-2xl leading-tight sm:text-3xl">
          {saudacao()}, {primeiroNome}.
        </h1>
        <p className="mt-2 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
          <span className="inline-flex items-center gap-1.5 rounded-full bg-accent px-2.5 py-0.5 text-[0.72rem] font-medium text-accent-foreground">
            <ShieldCheck className="size-3" aria-hidden="true" />
            {user.role_name}
          </span>
          <span>
            {atalhos.length === 0
              ? "Seu perfil ainda não tem nenhum recurso liberado."
              : `${atalhos.length} ${atalhos.length === 1 ? "atalho disponível" : "atalhos disponíveis"} para o seu perfil.`}
          </span>
        </p>
      </header>

      {atalhos.length === 0 ? (
        <Card className="p-6">
          <CardTitle>Nenhum acesso liberado ainda</CardTitle>
          <CardDescription className="mt-1">
            Peça à gestão para ajustar a matriz do perfil <strong>{user.role_name}</strong> em
            Configurações → Perfis. A mudança vale na requisição seguinte, sem novo login.
          </CardDescription>
        </Card>
      ) : (
        <section aria-labelledby="atalhos-titulo">
          <h2 id="atalhos-titulo" className="sr-only">
            Atalhos do seu perfil
          </h2>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {atalhos.map((atalho) => {
              const escopo = escopoDe(permissions, atalho.recurso, atalho.acao);
              return (
                <Link
                  key={atalho.href}
                  href={atalho.href}
                  className={cn(
                    "group rounded-xl border p-4 transition-colors outline-none",
                    "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-card",
                    // Sub-superfície dentro do `panel-float`: sem sombra. Sombra
                    // dentro de sombra achata a hierarquia do painel inteiro.
                    atalho.destaque
                      ? "border-transparent bg-brand-gradient text-white hover:brightness-110"
                      : "border-border/60 bg-muted/25 hover:bg-muted/45",
                  )}
                >
                  <div className="flex items-start gap-3">
                    <atalho.icone
                      className={cn("mt-0.5 size-5 shrink-0", atalho.destaque ? "text-white" : "text-primary")}
                    />
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <CardTitle className={cn(atalho.destaque && "text-white")}>{atalho.titulo}</CardTitle>
                        {escopo === "own" ? (
                          <span
                            className={cn(
                              "rounded-full px-2 py-0.5 text-[0.62rem] font-medium uppercase tracking-wider",
                              atalho.destaque ? "bg-white/20 text-white" : "bg-accent text-accent-foreground",
                            )}
                            title="O escopo é aplicado no SQL da API: a lista já chega filtrada pelo dono."
                          >
                            só os meus
                          </span>
                        ) : null}
                      </div>
                      <CardDescription className={cn("mt-1", atalho.destaque && "text-white/80")}>
                        {atalho.descricao}
                      </CardDescription>
                    </div>
                  </div>
                </Link>
              );
            })}
          </div>
        </section>
      )}

      <section aria-labelledby="matriz-titulo" className="rounded-xl border border-border/60 bg-muted/20 p-4 sm:p-5">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h2 id="matriz-titulo" className="font-display text-base">
            O que o perfil {user.role_name} alcança
          </h2>
          <span className="font-mono text-xs tabular-nums text-muted-foreground">
            {permissions.length} {permissions.length === 1 ? "permissão" : "permissões"}
          </span>
        </div>

        {matriz.length === 0 ? (
          <p className="mt-3 text-sm text-muted-foreground">A matriz deste perfil está vazia.</p>
        ) : (
          // A matriz é larga por natureza. Rola dentro do próprio container:
          // tabela que estoura a página horizontalmente é anti-padrão da casca.
          <div className="mt-3 overflow-x-auto">
            <ul className="flex min-w-max flex-col gap-1.5">
              {matriz.map((linha) => (
                <li key={linha.recurso} className="flex items-center gap-3 rounded-lg bg-card/70 px-3 py-2">
                  <code className="w-56 shrink-0 truncate font-mono text-xs text-foreground">{linha.recurso}</code>
                  <div className="flex flex-1 flex-wrap gap-1">
                    {linha.acoes.map((acao) => (
                      <span
                        key={acao}
                        className="rounded-full border border-border/70 px-2 py-0.5 text-[0.66rem] text-muted-foreground"
                      >
                        {ROTULO_DA_ACAO[acao]}
                      </span>
                    ))}
                  </div>
                  <span
                    className={cn(
                      "shrink-0 rounded-full px-2 py-0.5 text-[0.62rem] font-medium uppercase tracking-wider",
                      linha.escopo === "own" ? "bg-accent text-accent-foreground" : "bg-secondary text-secondary-foreground",
                    )}
                  >
                    {linha.escopo === "own" ? "próprios" : "todos"}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}

        <p className="mt-4 flex items-start gap-2 text-xs text-muted-foreground">
          <Info className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
          <span>
            Esta tela <strong>esconde</strong> o que o seu perfil não alcança — e esconder não é
            autorizar. Quem recusa é o middleware da API, a cada requisição, com 403. Se um botão
            vazasse para cá por engano, ele falharia; nenhum dado atravessa por engano.
          </span>
        </p>
      </section>
    </div>
  );
}
