import { Suspense } from "react";
import Link from "next/link";
import { UserPlus } from "lucide-react";

import { AvisoDeErro } from "@/components/crm/avisos";
import { PipelineKanban } from "@/components/crm/kanban";
import { SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import { buttonVariants } from "@/components/ui/button";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { listarCrm } from "@/lib/crm/api";
import { carregarQuadro, FiltrosDoFunil } from "@/lib/crm/quadro";
import type { Funil, MotivoDePerda } from "@/lib/crm/tipos";
import { cn } from "@/lib/utils";

import { BarraDoFunil } from "./barra";

export const metadata = { title: "Funil" };

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
export default async function FunilPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { user, permissions } = await requireSession();
  if (!can(permissions, "crm.opportunities", "ver")) return <SemAcesso recurso="crm.opportunities" />;

  // O MESMO schema e a MESMA busca que a atualização automática usa
  // (`lib/crm/quadro.ts`). Se a primeira carga e o refetch do tempo real
  // montassem a query cada um de um jeito, um filtro novo entraria na barra e o
  // quadro voltaria sem ele a cada evento — sem erro em lugar nenhum.
  const filtros = FiltrosDoFunil.safeParse(await searchParams).data ?? {};

  // As três coleções carregam em paralelo e falham em separado: o módulo do CRM
  // está sendo escrito ao mesmo tempo que esta tela, e uma rota ainda não
  // registrada responde 404 sem que isso diga nada sobre as outras duas.
  const [funis, quadro, motivos] = await Promise.all([
    listarCrm<Funil>("/crm/pipelines", { active: true }),
    carregarQuadro(filtros),
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
            ? "O seu funil: o quadro mostra apenas os negócios que são seus."
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
          {/* Nome do funil, total e selo de tempo real desenham DENTRO do
              kanban: os três mudam quando um evento repõe o quadro, e o que o
              RSC pintasse aqui ficaria congelado na primeira carga. */}
          <PipelineKanban
            quadro={quadro.data}
            motivos={motivos.ok ? motivos.data : []}
            permissoes={permissoes}
            filtros={filtros}
          />

          {!motivos.ok ? (
            <Nota variante="atencao">
              A lista de motivos de perda não carregou. Para marcar um negócio como perdido é preciso
              escolher um motivo — tente recarregar a página antes de fazer isso.
            </Nota>
          ) : null}

          <Nota>
            Além de arrastar, cada card tem o botão <strong>Mover para</strong>, que funciona no teclado e
            no celular. Ao soltar em <strong>Ganho</strong> ou <strong>Perdido</strong>, abre uma janela
            antes: ganhar cria a reserva e perder pede o motivo.
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
