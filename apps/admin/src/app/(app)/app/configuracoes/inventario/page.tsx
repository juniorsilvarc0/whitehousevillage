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
            <strong>Produto</strong> é o que se vende; <strong>unidade</strong> é o que se ocupa e se
            limpa. A ponte entre os dois é a composição, muitos-para-muitos de propósito: a mesma AP-01
            pertence ao Apartamento 2 Suítes <em>e</em> à White House Completa.
          </>
        }
      />

      <Nota>
        A exclusividade da casa não é uma regra escrita em código: ela cai da composição. Um produto que
        consome <strong>todas</strong> as unidades insere um bloqueio por unidade ao ser vendido, e a
        constraint do banco recusa qualquer sobreposição. Por isso vender um apartamento fecha a White
        House Completa naquelas datas, e vice-versa.
      </Nota>

      {!produtos.ok ? (
        <EstadoDeErro
          code={produtos.code}
          titulo="Não foi possível carregar os produtos"
          detalhe="Sem eles não há composição para mostrar."
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
          O cadastro aparece assim que as duas coleções carregarem.
        </p>
      )}
    </Tela>
  );
}
