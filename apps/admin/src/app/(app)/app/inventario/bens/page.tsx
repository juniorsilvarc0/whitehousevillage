import { Suspense } from "react";
import { FileSpreadsheet } from "lucide-react";

import { AbasDoInventario } from "@/components/bens/abas";
import { BarraDoCatalogo } from "@/components/bens/barra-do-catalogo";
import { CatalogoDeBens } from "@/components/bens/catalogo";
import { Paginacao, parametrosDe } from "@/components/bens/paginacao";
import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import { buttonVariants } from "@/components/ui/button";
import { apiList } from "@/lib/api/client";
import { carregarLista } from "@/lib/api/carregar";
import { tentar } from "@/lib/acoes/executar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { unidadesDoInventario } from "@/lib/bens/api";
import {
  consultaDoCatalogo,
  lerFiltrosDoCatalogo,
  linkDeExportacao,
  type ParametrosCrus,
} from "@/lib/bens/filtros";
import { permissoesDoInventario } from "@/lib/bens/permissoes";
import type { Ambiente, Bem } from "@/lib/bens/tipos";
import { cn } from "@/lib/utils";

export const metadata = { title: "Catálogo de bens" };

/**
 * O catálogo de bens da casa — **um item por objeto**.
 *
 * "Prato raso branco" é um bem só, esteja em dois ambientes ou em seis
 * apartamentos; a quantidade de cada lugar é a colocação, na tela da unidade.
 * Recurso RBAC `inventory.goods` (não `inventory`, que é o cadastro comercial).
 */
export default async function CatalogoPage({ searchParams }: { searchParams: Promise<ParametrosCrus> }) {
  const { permissions } = await requireSession();
  if (!can(permissions, "inventory.goods", "ver")) return <SemAcesso recurso="inventory.goods" />;

  const cru = await searchParams;
  const { filtros, avisos } = lerFiltrosDoCatalogo(cru);

  const [lista, unidades, ambientes] = await Promise.all([
    tentar(() => apiList<Bem>("/inventory/items", { query: consultaDoCatalogo(filtros) })),
    unidadesDoInventario(),
    filtros.unidade
      ? carregarLista<Ambiente>("/rooms", { unit_id: filtros.unidade, sort: "sort_order" })
      : Promise.resolve({ ok: true as const, data: [] as Ambiente[] }),
  ]);

  const permissoes = permissoesDoInventario(permissions);

  const temFiltro = Boolean(filtros.q || filtros.categoria || filtros.foto || filtros.unidade || filtros.situacao !== "ativos");

  return (
    <Tela>
      <CabecalhoDeTela
        titulo="Inventário de bens"
        descricao={
          <>
            Tudo o que a casa tem — louça, roupa de cama, eletro, mobília —, cada objeto cadastrado <strong>uma vez</strong>,
            com foto. Corrigir o nome ou o custo aqui corrige em todos os apartamentos.
          </>
        }
        acoes={
          <a
            href={linkDeExportacao({ unidade: filtros.unidade, ambiente: filtros.ambiente, categoria: filtros.categoria })}
            className={cn(buttonVariants({ variant: "outline", size: "sm" }))}
            download
          >
            <FileSpreadsheet aria-hidden="true" />
            Exportar planilha
          </a>
        }
      />

      <AbasDoInventario ativa="bens" />

      <Suspense fallback={<div className="h-16" />}>
        <BarraDoCatalogo unidades={unidades.ok ? unidades.data : []} ambientes={ambientes.ok ? ambientes.data : []} />
      </Suspense>

      {avisos.length > 0 ? (
        <Nota variante="atencao">
          {avisos.map((a) => (
            <span key={a} className="block">
              {a}
            </span>
          ))}
        </Nota>
      ) : null}

      {!unidades.ok ? (
        <Nota variante="atencao">A lista de unidades não carregou, então o filtro por unidade está vazio.</Nota>
      ) : null}

      {lista.ok ? (
        <>
          <CatalogoDeBens
            bens={lista.data.data}
            permissoes={permissoes}
            temFiltro={temFiltro}
            soSemFoto={filtros.foto === "sem"}
          />
          <Paginacao
            meta={lista.data.meta}
            caminho="/app/inventario/bens"
            parametros={parametrosDe(cru)}
            substantivo={["bem", "bens"]}
          />
        </>
      ) : (
        <EstadoDeErro
          code={lista.code}
          titulo="Não foi possível carregar o catálogo"
          detalhe={
            lista.code === "NOT_FOUND" ? "Esta parte do sistema ainda não está disponível. Tente de novo mais tarde." : undefined
          }
        />
      )}

      <Nota>
        A planilha sai em CSV pronto para o Excel em português, uma linha por bem em cada ambiente. Busca e “sem
        foto” não entram nela: o recorte da planilha é por unidade, ambiente e categoria.
      </Nota>
    </Tela>
  );
}
