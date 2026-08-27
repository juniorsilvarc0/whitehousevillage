import Link from "next/link";
import { ChevronLeft } from "lucide-react";

import { AvisoDeErro } from "@/components/crm/avisos";
import { TelaDaOportunidade } from "@/components/crm/oportunidade";
import { SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Tela } from "@/components/layout/tela";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { chamarCrm, listarCrm } from "@/lib/crm/api";
import type { MotivoDePerda, OportunidadeCompleta } from "@/lib/crm/tipos";

export const metadata = { title: "Oportunidade" };

/**
 * A tela da oportunidade.
 *
 * **Uma chamada** desenha tudo (`/full`): dados, contato, etapas, SLA,
 * atividades, notas, documentos, alertas, histórico, linha do tempo, orçamento
 * e reserva. Seis requisições paralelas dariam seis instantes diferentes da
 * mesma negociação — e a linha do tempo, que é literalmente a ordem dos fatos,
 * seria a primeira a discordar de si mesma.
 *
 * Fora do escopo do requisitante a API devolve **404, nunca 403**: responder
 * 403 confirmaria que a oportunidade existe, e "não é sua" já é informação.
 */
export default async function OportunidadePage({ params }: { params: Promise<{ id: string }> }) {
  const { permissions } = await requireSession();
  if (!can(permissions, "crm.opportunities", "ver")) return <SemAcesso recurso="crm.opportunities" />;

  const { id } = await params;

  // O catálogo de motivos carrega junto porque o botão "Perder" precisa dele no
  // primeiro clique — buscá-lo só ao abrir o diálogo colocaria uma espera no
  // meio de uma decisão que já é desagradável de tomar.
  const [completo, motivos] = await Promise.all([
    chamarCrm<OportunidadeCompleta>(`/crm/opportunities/${id}/full`),
    listarCrm<MotivoDePerda>("/crm/lost-reasons", { active: true }),
  ]);

  if (!completo.ok) {
    return (
      <Tela>
        <CabecalhoDeTela voltar={{ href: "/app/funil", rotulo: "Funil" }} titulo="Oportunidade" />
        <AvisoDeErro
          code={completo.code}
          titulo="Não foi possível abrir a oportunidade"
          detalhe={
            completo.code === "NOT_FOUND"
              ? "Ou ela não existe mais, ou é de outro corretor — a API responde 404 nos dois casos, de propósito: 403 confirmaria que ela existe."
              : undefined
          }
        />
      </Tela>
    );
  }

  const permissoes = {
    editar: can(permissions, "crm.opportunities", "editar"),
    criarAtividade: can(permissions, "crm.activities", "criar"),
    editarAtividade: can(permissions, "crm.activities", "editar"),
  };

  return (
    <Tela>
      {/* Sem `CabecalhoDeTela` aqui: o `<h1>` desta tela é o nome do contato,
          dentro do componente. Dois `<h1>` na mesma página é o tipo de coisa
          que ninguém vê e que o leitor de tela lê duas vezes. */}
      <Link
        href="/app/funil"
        className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
      >
        <ChevronLeft className="size-3.5" aria-hidden="true" />
        Funil
      </Link>
      <TelaDaOportunidade
        completo={completo.data}
        motivos={motivos.ok ? motivos.data : []}
        permissoes={permissoes}
      />
    </Tela>
  );
}
