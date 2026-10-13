import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Tela } from "@/components/layout/tela";
import { DetalheDaOrdem } from "@/components/manutencao/detalhe-da-ordem";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { carregarOrdem } from "@/lib/manutencao/api";
import { CAMINHO_DA_MANUTENCAO } from "@/lib/manutencao/mensagens";
import { permissoesDaManutencao } from "@/lib/manutencao/permissoes";

export const metadata = { title: "Ordem de manutenção" };

/**
 * Uma ordem de manutenção: dados, avaria de origem, custo, bloqueio do
 * calendário e histórico — numa chamada (`GET /maintenance-orders/{id}`).
 *
 * Os botões saem de `allowed_actions` e `editable` da própria resposta; a
 * página só repassa a matriz do perfil.
 */
export default async function OrdemPage({ params }: { params: Promise<{ id: string }> }) {
  const { permissions } = await requireSession();
  const permissoes = permissoesDaManutencao(permissions);
  if (!permissoes.ver) return <SemAcesso recurso="maintenance" />;

  const { id } = await params;
  const ordem = await carregarOrdem(id);
  const voltar = { href: CAMINHO_DA_MANUTENCAO, rotulo: "Manutenção" };

  if (!ordem.ok) {
    return (
      <Tela>
        <CabecalhoDeTela voltar={voltar} titulo="Ordem de manutenção" />
        <EstadoDeErro
          code={ordem.code}
          titulo="Não foi possível abrir esta ordem"
          detalhe={ordem.code === "NOT_FOUND" ? "Confira o endereço, ou volte à lista de ordens." : undefined}
        />
      </Tela>
    );
  }

  const o = ordem.data;
  return (
    <Tela>
      <CabecalhoDeTela voltar={voltar} titulo={o.title} descricao={[o.unit_code, o.room_name].filter(Boolean).join(" · ")} />
      <DetalheDaOrdem ordem={o} permissoes={permissoes} podeVerInventario={can(permissions, "inventory.goods", "ver")} />
    </Tela>
  );
}
