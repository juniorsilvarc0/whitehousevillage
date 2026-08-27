import { Suspense } from "react";
import Link from "next/link";
import { Columns3, UserPlus } from "lucide-react";

import { AvisoDeErro } from "@/components/crm/avisos";
import { PipelineKanban } from "@/components/crm/kanban";
import { SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import { buttonVariants } from "@/components/ui/button";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { chamarCrm, listarCrm } from "@/lib/crm/api";
import type { Funil, MotivoDePerda, QuadroKanban } from "@/lib/crm/tipos";
import { formatarBRL } from "@/lib/dinheiro";
import { cn } from "@/lib/utils";

import { BarraDoFunil } from "./barra";

export const metadata = { title: "Funil" };

type Filtros = {
  pipeline_id?: string;
  q?: string;
  owner_id?: string;
  unit_type_id?: string;
  from?: string;
  to?: string;
  include_closed?: string;
};

/**
 * O quadro do funil.
 *
 * Uma chamada desenha o quadro inteiro (`GET /crm/opportunities/kanban`):
 * colunas, cards e **totais por coluna**. Os totais vêm do servidor porque a
 * coluna é paginada — somar o que veio na tela daria um valor de funil que muda
 * conforme se rola a página, e é justamente o total que a gestão olha primeiro.
 *
 * **Escopo `own`**: o corretor vê no kanban apenas os próprios cards. Quem
 * aplica isso é o `WHERE` do repositório, não esta tela — aqui o escopo só vira
 * a frase que explica por que o quadro está mais vazio do que o do colega.
 */
export default async function FunilPage({ searchParams }: { searchParams: Promise<Filtros> }) {
  const { user, permissions } = await requireSession();
  if (!can(permissions, "crm.opportunities", "ver")) return <SemAcesso recurso="crm.opportunities" />;

  const filtros = await searchParams;

  // As três coleções carregam em paralelo e falham em separado: o módulo do CRM
  // está sendo escrito ao mesmo tempo que esta tela, e uma rota ainda não
  // registrada responde 404 sem que isso diga nada sobre as outras duas.
  const [funis, quadro, motivos] = await Promise.all([
    listarCrm<Funil>("/crm/pipelines", { active: true }),
    chamarCrm<QuadroKanban>("/crm/opportunities/kanban", {
      query: {
        pipeline_id: filtros.pipeline_id,
        q: filtros.q,
        owner_id: filtros.owner_id,
        unit_type_id: filtros.unit_type_id,
        from: filtros.from,
        to: filtros.to,
        include_closed: filtros.include_closed === "true" ? "true" : undefined,
      },
    }),
    listarCrm<MotivoDePerda>("/crm/lost-reasons", { active: true }),
  ]);

  const permissoes = { editar: can(permissions, "crm.opportunities", "editar") };
  const escopoProprio = permissions.some(
    (p) => p.resource === "crm.opportunities" && p.action === "ver" && p.scope === "own",
  );

  return (
    <Tela>
      <CabecalhoDeTela
        titulo="Funil"
        descricao={
          escopoProprio
            ? "O seu funil: o quadro mostra apenas os negócios de que você é dono. Quem recorta isso é o servidor, na consulta."
            : "Onde cada negócio está, quanto ele vale e há quanto tempo não anda."
        }
        acoes={
          can(permissions, "crm.leads", "ver") ? (
            <Link href="/app/leads" className={cn(buttonVariants({ variant: "outline", size: "sm" }))}>
              <UserPlus aria-hidden="true" />
              Leads
            </Link>
          ) : null
        }
      />

      {funis.ok ? (
        <Suspense fallback={<div className="h-24" />}>
          <BarraDoFunil
            funis={funis.data}
            funilAtual={filtros.pipeline_id ?? (quadro.ok ? quadro.data.pipeline.id : null)}
            usuarioId={user.id}
          />
        </Suspense>
      ) : (
        <AvisoDeErro code={funis.code} titulo="Não foi possível carregar os funis" />
      )}

      {quadro.ok ? (
        <>
          <p className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
            <span className="flex items-center gap-2">
              <Columns3 className="size-4" aria-hidden="true" />
              <strong className="font-display text-base text-foreground">{quadro.data.pipeline.name}</strong>
            </span>
            <span className="font-mono tabular-nums">
              {quadro.data.totals.count} {quadro.data.totals.count === 1 ? "negócio aberto" : "negócios"} ·{" "}
              {formatarBRL(quadro.data.totals.amount_cents)}
            </span>
          </p>

          <PipelineKanban
            quadro={quadro.data}
            motivos={motivos.ok ? motivos.data : []}
            permissoes={permissoes}
          />

          {!motivos.ok ? (
            <Nota variante="atencao">
              O catálogo de motivos de perda não carregou ({motivos.code}). Marcar um negócio como perdido
              exige um motivo do catálogo — enquanto ele não vier, a perda vai ser recusada pelo servidor.
            </Nota>
          ) : null}

          <Nota>
            Arrastar não é o único caminho: cada card tem <strong>Mover para</strong>, que funciona com
            teclado e no celular. Soltar em <strong>Ganho</strong> ou <strong>Perdido</strong> não move o
            card sozinho — ganhar cria reserva e perder exige motivo, então os dois abrem um diálogo.
          </Nota>
        </>
      ) : (
        <AvisoDeErro
          code={quadro.code}
          titulo="Não foi possível carregar o quadro"
          detalhe={
            quadro.code === "NOT_FOUND"
              ? "Ou o funil escolhido não existe, ou não há nenhum funil marcado como padrão. A gestão define o padrão em Configurações."
              : undefined
          }
        />
      )}
    </Tela>
  );
}
