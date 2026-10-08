import Link from "next/link";
import { Suspense } from "react";
import { DoorClosed } from "lucide-react";

import { AbasDoInventario } from "@/components/bens/abas";
import { EscolhaDeUnidade } from "@/components/bens/escolha-de-unidade";
import { BuscaNaUrl, MarcacaoNaUrl, SelecaoNaUrl } from "@/components/bens/filtro-na-url";
import { InventarioDaUnidadeView } from "@/components/bens/unidade";
import { EstadoDeErro, EstadoVazio, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import { carregar } from "@/lib/api/carregar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { catalogoAtivo, unidadesDoInventario } from "@/lib/bens/api";
import { descricaoDoRecorte, lerFiltrosDaUnidade, type ParametrosCrus } from "@/lib/bens/filtros";
import { permissoesDoInventario } from "@/lib/bens/permissoes";
import { ROTULO_DA_CATEGORIA } from "@/lib/bens/rotulos";
import { CATEGORIAS_DE_BEM, type Bem, type InventarioDaUnidade } from "@/lib/bens/tipos";

export const metadata = { title: "Inventário" };

/**
 * O inventário por unidade — **a tela de onde a operação parte**.
 *
 * Escolhida a unidade, uma chamada só (`GET /units/{id}/inventory`) traz os
 * ambientes na ordem de caminhada, os bens de cada um com foto e quantidade, os
 * totais, a conferência aberta e a última fechada. Cinco chamadas encadeadas do
 * navegador seriam cinco chances de mostrar meia tela.
 *
 * Recurso `inventory.goods`. Da unidade, esta tela só recebe código e nome —
 * tarifa, capacidade e composição são do cadastro comercial (`inventory`) e não
 * vazam por aqui.
 */
export default async function InventarioPage({ searchParams }: { searchParams: Promise<ParametrosCrus> }) {
  const { permissions } = await requireSession();
  if (!can(permissions, "inventory.goods", "ver")) return <SemAcesso recurso="inventory.goods" />;

  const { filtros, avisos } = lerFiltrosDaUnidade(await searchParams);
  const permissoes = permissoesDoInventario(permissions);

  const [unidades, inventario, catalogo] = await Promise.all([
    unidadesDoInventario(),
    filtros.unidade
      ? carregar<InventarioDaUnidade>(`/units/${filtros.unidade}/inventory`, {
          include_inactive: filtros.inativos ? true : undefined,
          category: filtros.categoria || undefined,
          q: filtros.q || undefined,
        })
      : Promise.resolve(null),
    // O catálogo só é preciso para colocar bem num ambiente — quem não pode
    // criar não paga a leitura.
    filtros.unidade && permissoes.criar ? catalogoAtivo() : Promise.resolve({ ok: true as const, data: [] as Bem[] }),
  ]);

  const opcoesDeUnidade = unidades.ok ? unidades.data.map((u) => ({ valor: u.id, rotulo: `${u.code} — ${u.name}` })) : [];

  return (
    <Tela>
      <CabecalhoDeTela
        titulo="Inventário de bens"
        descricao={
          <>
            O que cada apartamento tem, <strong>cômodo a cômodo</strong>: o enxoval não está “no AP-01”, está na cozinha do
            AP-01. É esta lista que a conferência percorre, na mesma ordem.
          </>
        }
      />

      <AbasDoInventario ativa="unidades" />

      <Suspense fallback={<div className="h-16" />}>
        <div className="flex flex-wrap items-end gap-3">
          <SelecaoNaUrl id="inv-unidade" chave="unidade" rotulo="Unidade" vazio="Escolha a unidade" opcoes={opcoesDeUnidade} className="w-64" />
          {filtros.unidade ? (
            <>
              <BuscaNaUrl id="inv-busca" rotulo="Buscar bem" placeholder="Nome do bem" />
              <SelecaoNaUrl
                id="inv-categoria"
                chave="categoria"
                rotulo="Categoria"
                vazio="Todas"
                opcoes={CATEGORIAS_DE_BEM.map((c) => ({ valor: c, rotulo: ROTULO_DA_CATEGORIA[c] }))}
                className="w-44"
              />
              <MarcacaoNaUrl
                id="inv-inativos"
                chave="inativos"
                rotulo="Mostrar desativados"
                hint="Ambientes e bens fora de uso — a visão de quem audita o histórico."
              />
            </>
          ) : null}
        </div>
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
        <EstadoDeErro code={unidades.code} titulo="Não foi possível carregar as unidades" />
      ) : !filtros.unidade ? (
        unidades.data.length === 0 ? (
          <EstadoVazio
            icone={DoorClosed}
            titulo="Nenhuma unidade cadastrada"
            descricao={
              <>
                Os apartamentos se cadastram em{" "}
                <Link href="/app/configuracoes/inventario" className="text-primary underline-offset-4 hover:underline">
                  Configurações → Unidades e produtos
                </Link>
                . Depois, volte aqui para descrever os cômodos de cada um.
              </>
            }
          />
        ) : (
          <EscolhaDeUnidade unidades={unidades.data} />
        )
      ) : inventario && !inventario.ok ? (
        <EstadoDeErro
          code={inventario.code}
          titulo="Não foi possível carregar o inventário desta unidade"
          detalhe={inventario.code === "NOT_FOUND" ? "A unidade pode ter sido removida, ou esta parte do sistema ainda não está disponível." : undefined}
        />
      ) : inventario && inventario.ok ? (
        <>
          {!catalogo.ok ? (
            <Nota variante="atencao">O catálogo não carregou, então colocar bem num ambiente vai mostrar a lista vazia.</Nota>
          ) : null}
          <InventarioDaUnidadeView
            inventario={inventario.data}
            catalogo={catalogo.ok ? catalogo.data : []}
            unidades={unidades.data}
            permissoes={permissoes}
            filtrado={Boolean(filtros.q || filtros.categoria)}
            recorte={descricaoDoRecorte(filtros)}
          />
        </>
      ) : null}
    </Tela>
  );
}
