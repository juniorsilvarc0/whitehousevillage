import { Suspense } from "react";

import { AvisoDeErro } from "@/components/crm/avisos";
import { SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import type { Produto } from "@/lib/api/comercial";
import { carregarLista } from "@/lib/api/carregar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { listarCrm, paginarCrm } from "@/lib/crm/api";
import type { EtapaDoFunil, Funil, Lead } from "@/lib/crm/tipos";

import { BarraDeLeads } from "./barra";
import { PainelDeLeads } from "./painel";

export const metadata = { title: "Leads" };

type Filtros = { status?: string; source?: string; q?: string; page?: string };

/**
 * Leads — o interesse antes de virar negócio.
 *
 * **Escopo `own`**: o corretor lista só os leads de que é dono, e lead **sem
 * dono** não aparece para ele. Não é um esquecimento do filtro: lead que entra
 * por WhatsApp antes de alguém assumir é fila da gestão, e mostrá-lo a todos os
 * corretores criaria a corrida por atender primeiro que o CRM existe para
 * evitar.
 */
export default async function LeadsPage({ searchParams }: { searchParams: Promise<Filtros> }) {
  const { permissions } = await requireSession();
  if (!can(permissions, "crm.leads", "ver")) return <SemAcesso recurso="crm.leads" />;

  const filtros = await searchParams;
  const pagina = Number.parseInt(filtros.page ?? "1", 10);

  // O funil, as etapas e os produtos vão junto porque o modal de conversão
  // precisa dos três no primeiro clique. Carregar sob demanda colocaria três
  // esperas dentro do gesto que deveria ser o mais rápido da tela.
  const [leads, funis, etapas, produtos] = await Promise.all([
    paginarCrm<Lead>("/crm/leads", {
      status: filtros.status,
      source: filtros.source,
      q: filtros.q,
      page: Number.isFinite(pagina) && pagina > 0 ? pagina : 1,
      per_page: 25,
      sort: "-created_at",
    }),
    listarCrm<Funil>("/crm/pipelines", { active: true }),
    listarCrm<EtapaDoFunil>("/crm/stages"),
    carregarLista<Produto>("/unit-types", { sort: "sort_order" }),
  ]);

  const permissoes = {
    editar: can(permissions, "crm.leads", "editar"),
    excluir: can(permissions, "crm.leads", "excluir"),
  };

  return (
    <Tela>
      <CabecalhoDeTela
        voltar={{ href: "/app/funil", rotulo: "Funil" }}
        titulo="Leads"
        descricao={
          <>
            O lead guarda o <strong>interesse</strong>, não a pessoa: nome, telefone e e-mail vivem no
            cadastro de contatos, um registro por pessoa. É essa separação que deixa o WhatsApp
            reconhecer quem já existe em vez de abrir um segundo cadastro a cada mensagem.
          </>
        }
      />

      <Suspense fallback={<div className="h-16" />}>
        <BarraDeLeads />
      </Suspense>

      {leads.ok ? (
        <>
          <PainelDeLeads
            leads={leads.data.data}
            funis={funis.ok ? funis.data : []}
            etapas={etapas.ok ? etapas.data : []}
            produtos={produtos.ok ? produtos.data : []}
            permissoes={permissoes}
          />

          {leads.data.meta.total_pages > 1 ? (
            <p className="text-xs text-muted-foreground">
              Página {leads.data.meta.page} de {leads.data.meta.total_pages} ·{" "}
              <span className="tabular-nums">{leads.data.meta.total}</span> leads no recorte.
            </p>
          ) : null}

          {!funis.ok || !etapas.ok ? (
            <Nota variante="atencao">
              O catálogo de funis não carregou por inteiro ({!funis.ok ? funis.code : !etapas.ok ? etapas.code : ""}). A
              conversão ainda funciona — sem funil escolhido, o servidor usa o funil padrão e a primeira
              etapa dele.
            </Nota>
          ) : null}
        </>
      ) : (
        <AvisoDeErro code={leads.code} titulo="Não foi possível carregar os leads" />
      )}
    </Tela>
  );
}
