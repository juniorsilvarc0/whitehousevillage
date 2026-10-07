import { Suspense } from "react";

import { AbasDoInventario } from "@/components/bens/abas";
import { ListaDeAvarias } from "@/components/bens/avarias";
import { SelecaoNaUrl } from "@/components/bens/filtro-na-url";
import { Paginacao, parametrosDe } from "@/components/bens/paginacao";
import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import { apiList } from "@/lib/api/client";
import { carregar } from "@/lib/api/carregar";
import { tentar } from "@/lib/acoes/executar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { unidadesDoInventario } from "@/lib/bens/api";
import { consultaDeAvarias, lerFiltrosDeAvarias, type ParametrosCrus } from "@/lib/bens/filtros";
import { permissoesDoInventario } from "@/lib/bens/permissoes";
import { ROTULO_DA_AVARIA } from "@/lib/bens/rotulos";
import { TIPOS_DE_AVARIA, type Avaria, type InventarioDaUnidade } from "@/lib/bens/tipos";

export const metadata = { title: "Avarias" };

/**
 * Pendências de avaria — a lista de trabalho de quem repõe, conserta ou cobra.
 *
 * Abre em `open=true` (pendência aberta é `resolution IS NULL`). Escolhida a
 * unidade, a tela também carrega os cômodos e bens dela, que é de onde o
 * formulário de registro escolhe — a avaria precisa apontar para um bem que
 * está colocado no ambiente informado.
 */
export default async function AvariasPage({ searchParams }: { searchParams: Promise<ParametrosCrus> }) {
  const { permissions } = await requireSession();
  if (!can(permissions, "inventory.goods", "ver")) return <SemAcesso recurso="inventory.goods" />;

  const cru = await searchParams;
  const { filtros, avisos } = lerFiltrosDeAvarias(cru);
  const permissoes = permissoesDoInventario(permissions);

  const [lista, unidades, inventario] = await Promise.all([
    tentar(() => apiList<Avaria>("/inventory/issues", { query: consultaDeAvarias(filtros) })),
    unidadesDoInventario(),
    filtros.unidade && permissoes.criar
      ? carregar<InventarioDaUnidade>(`/units/${filtros.unidade}/inventory`)
      : Promise.resolve(null),
  ]);

  const temFiltro = filtros.situacao !== "abertas" || Boolean(filtros.unidade || filtros.tipo || filtros.conferencia);

  return (
    <Tela>
      <CabecalhoDeTela
        titulo="Avarias"
        descricao={
          <>
            Quebrado, faltando, avariado. Cada registro fica pendente até alguém dar o desfecho — repor, consertar, cobrar
            do hóspede, aceitar a perda ou descartar.
          </>
        }
      />

      <AbasDoInventario ativa="avarias" />

      <Suspense fallback={<div className="h-16" />}>
        <div className="flex flex-wrap items-end gap-3">
          <SelecaoNaUrl
            id="avarias-situacao"
            chave="situacao"
            rotulo="Situação"
            opcoes={[
              { valor: "", rotulo: "Em aberto" },
              { valor: "resolvidas", rotulo: "Resolvidas" },
              { valor: "todas", rotulo: "Todas" },
            ]}
            className="w-40"
          />
          <SelecaoNaUrl
            id="avarias-unidade"
            chave="unidade"
            rotulo="Unidade"
            vazio="Todas"
            opcoes={unidades.ok ? unidades.data.map((u) => ({ valor: u.id, rotulo: `${u.code} — ${u.name}` })) : []}
            className="w-60"
          />
          <SelecaoNaUrl
            id="avarias-tipo"
            chave="tipo"
            rotulo="Tipo"
            vazio="Todos"
            opcoes={TIPOS_DE_AVARIA.map((t) => ({ valor: t, rotulo: ROTULO_DA_AVARIA[t] }))}
            className="w-40"
          />
        </div>
      </Suspense>

      {filtros.conferencia ? <Nota>Mostrando só as avarias achadas numa conferência específica.</Nota> : null}
      {avisos.length > 0 ? <Nota variante="atencao">{avisos.join(" ")}</Nota> : null}
      {inventario && !inventario.ok ? (
        <Nota variante="atencao">Os cômodos desta unidade não carregaram, então não dá para registrar avaria agora.</Nota>
      ) : null}

      {lista.ok ? (
        <>
          <ListaDeAvarias
            avarias={lista.data.data}
            permissoes={permissoes}
            temFiltro={temFiltro}
            unidade={
              inventario && inventario.ok ? { code: inventario.data.unit.code, ambientes: inventario.data.rooms } : null
            }
          />
          <Paginacao meta={lista.data.meta} caminho="/app/inventario/avarias" parametros={parametrosDe(cru)} substantivo={["avaria", "avarias"]} />
        </>
      ) : (
        <EstadoDeErro code={lista.code} titulo="Não foi possível carregar as avarias" />
      )}
    </Tela>
  );
}
