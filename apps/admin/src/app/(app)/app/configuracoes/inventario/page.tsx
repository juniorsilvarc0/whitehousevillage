import { Package } from "lucide-react";

import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import type { Produto, Unidade, UnidadeDaComposicao } from "@/lib/api/comercial";
import { carregar, carregarLista } from "@/lib/api/carregar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";

import { PainelDeInventario, type Composicoes } from "./painel";

export const metadata = { title: "Inventário" };

/**
 * Produtos, unidades e a composição que liga os dois.
 *
 * O recurso do RBAC é `inventory`, o mesmo que a OpenAPI declara em `x-rbac`
 * para todas as rotas de `/unit-types` e `/units` — não `settings`, embora a
 * tela viva dentro de Configurações. O menu esconde; quem recusa é a API.
 */
export default async function InventarioPage() {
  const { permissions } = await requireSession();
  if (!can(permissions, "inventory", "ver")) return <SemAcesso recurso="inventory" />;

  // As três coleções carregam em paralelo e falham em separado: nesta fase os
  // módulos da API estão sendo escritos ao mesmo tempo, e uma rota ainda não
  // registrada responde 404 sem que isso signifique nada sobre as outras.
  const [produtos, unidades] = await Promise.all([
    carregarLista<Produto>("/unit-types", { sort: "sort_order" }),
    carregarLista<Unidade>("/units", { sort: "code" }),
  ]);

  const composicoes: Composicoes = {};
  if (produtos.ok) {
    const carregadas = await Promise.all(
      produtos.data.map(async (produto) => ({
        id: produto.id,
        resultado: await carregar<UnidadeDaComposicao[]>(`/unit-types/${produto.id}/members`),
      })),
    );
    for (const { id, resultado } of carregadas) {
      // Composição que não carregou vira lista vazia com o aviso do cartão
      // ("sem composição"), e não um erro de tela inteira: o resto do
      // inventário continua utilizável.
      composicoes[id] = resultado.ok ? resultado.data : [];
    }
  }

  const permissoes = {
    criar: can(permissions, "inventory", "criar"),
    editar: can(permissions, "inventory", "editar"),
    excluir: can(permissions, "inventory", "excluir"),
  };

  return (
    <Tela>
      <CabecalhoDeTela
        voltar={{ href: "/app/configuracoes", rotulo: "Configurações" }}
        titulo="Inventário"
        descricao={
          <>
            <strong>Produto</strong> é o que você vende ao cliente; <strong>unidade</strong> é o
            apartamento de verdade, que se ocupa e se limpa. Cada produto é formado por um ou mais
            apartamentos — e um apartamento pode estar em mais de um produto: a AP-01 faz parte do
            Apartamento 2 Suítes <em>e</em> da White House Completa.
          </>
        }
      />

      <Nota>
        Um produto que ocupa <strong>todas</strong> as unidades reserva todos os apartamentos de uma vez.
        Por isso vender um apartamento fecha a White House Completa naquelas datas, e vice-versa — o
        sistema nunca deixa o mesmo apartamento ser vendido duas vezes.
      </Nota>

      {!produtos.ok ? (
        <EstadoDeErro
          code={produtos.code}
          titulo="Não foi possível carregar os produtos"
          detalhe="Sem os produtos não dá para mostrar quais apartamentos formam cada um."
        />
      ) : null}

      {!unidades.ok ? (
        <EstadoDeErro code={unidades.code} titulo="Não foi possível carregar as unidades físicas" />
      ) : null}

      {produtos.ok && unidades.ok ? (
        <PainelDeInventario
          produtos={produtos.data}
          unidades={unidades.data}
          composicoes={composicoes}
          permissoes={permissoes}
        />
      ) : (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Package className="size-4" aria-hidden="true" />
          O cadastro aparece assim que produtos e unidades carregarem.
        </p>
      )}
    </Tela>
  );
}
