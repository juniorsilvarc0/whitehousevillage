import { FichaDoBem, OndeEsta } from "@/components/bens/ficha-do-bem";
import { GaleriaDoBem } from "@/components/bens/galeria";
import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Secao, Tela } from "@/components/layout/tela";
import { carregar } from "@/lib/api/carregar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { permissoesDoInventario } from "@/lib/bens/permissoes";
import type { BemCompleto } from "@/lib/bens/tipos";

export const metadata = { title: "Bem" };

/**
 * A ficha do bem: fotos, dados e **onde ele está**.
 *
 * Uma chamada só (`GET /inventory/items/{id}`): o contrato já traz `photos` e
 * `placements` junto, porque "onde está este prato" é a segunda pergunta de
 * quem abre a ficha e uma chamada a mais por item inviabilizaria a tela.
 */
export default async function BemPage({ params }: { params: Promise<{ id: string }> }) {
  const { permissions } = await requireSession();
  if (!can(permissions, "inventory.goods", "ver")) return <SemAcesso recurso="inventory.goods" />;

  const { id } = await params;
  const bem = await carregar<BemCompleto>(`/inventory/items/${encodeURIComponent(id)}`);

  const voltar = { href: "/app/inventario/bens", rotulo: "Catálogo de bens" };

  if (!bem.ok) {
    return (
      <Tela>
        <CabecalhoDeTela voltar={voltar} titulo="Bem" />
        <EstadoDeErro
          code={bem.code}
          titulo="Não foi possível abrir a ficha deste bem"
          detalhe={bem.code === "NOT_FOUND" ? "Ele pode ter sido apagado, ou esta parte do sistema ainda não está disponível." : undefined}
        />
      </Tela>
    );
  }

  const { criar, editar, excluir } = permissoesDoInventario(permissions);

  return (
    <Tela>
      <CabecalhoDeTela voltar={voltar} titulo={bem.data.name} />

      <FichaDoBem bem={bem.data} permissoes={{ editar, excluir }} />

      <Secao
        titulo="Fotos"
        descricao="A primeira é a capa — é ela que aparece na lista de conferência. Toque numa foto para vê-la inteira."
      >
        <GaleriaDoBem
          itemId={bem.data.id}
          nome={bem.data.name}
          fotos={bem.data.photos ?? []}
          podeEnviar={criar && editar}
          podeOrganizar={editar}
        />
      </Secao>

      <Secao titulo="Onde está" descricao="Em que ambientes de que apartamentos este bem fica, e quanto se espera encontrar em cada um.">
        <OndeEsta colocacoes={bem.data.placements ?? []} medida={bem.data.unit_measure} />
      </Secao>

      <Nota>
        Corrigir o nome, a foto ou o custo aqui vale para todos os apartamentos de uma vez. Conferências já feitas
        continuam com o que esperavam na época.
      </Nota>
    </Tela>
  );
}
