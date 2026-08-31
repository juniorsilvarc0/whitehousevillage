import Link from "next/link";
import { ChevronLeft } from "lucide-react";

import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import { DetalheDaReserva } from "@/components/reservas/detalhe";
import type { Produto, UnidadeDaComposicao } from "@/lib/api/comercial";
import { carregarLista } from "@/lib/api/carregar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { carregarReserva } from "@/lib/reservas/api";
import { podeRealocarUnidade } from "@/lib/reservas/estados";

export const metadata = { title: "Reserva" };

/**
 * A tela da reserva.
 *
 * **Uma chamada** desenha tudo (`GET /reservations/{id}/full`): reserva,
 * noites, unidades, hóspedes, linha do tempo e a simulação de cancelamento.
 * Cinco requisições encadeadas do navegador seriam cinco chances de mostrar
 * meia tela — e um estado intermediário em que as noites já vieram e os totais
 * ainda não.
 *
 * Fora do escopo do requisitante a API devolve **404, nunca 403**: responder
 * 403 confirmaria que a reserva existe, e "não é sua" já é informação.
 */
export default async function ReservaPage({ params }: { params: Promise<{ id: string }> }) {
  const { permissions } = await requireSession();
  if (!can(permissions, "reservations", "ver")) return <SemAcesso recurso="reservations" />;

  const { id } = await params;

  // Os produtos vão junto porque o diálogo de remarcação precisa deles no
  // primeiro clique — remarcar trocando de produto é o caminho do upgrade, e
  // buscar o catálogo ao abrir o modal colocaria uma espera no meio dele.
  const [completo, produtos] = await Promise.all([
    carregarReserva(id),
    carregarLista<Produto>("/unit-types", { sort: "sort_order" }),
  ]);

  if (!completo.ok) {
    return (
      <Tela>
        <CabecalhoDeTela voltar={{ href: "/app/reservas", rotulo: "Reservas" }} titulo="Reserva" />
        <EstadoDeErro
          code={completo.code}
          titulo="Não foi possível abrir a reserva"
          detalhe={
            completo.code === "NOT_FOUND"
              ? "Ou ela não existe, ou é de outro corretor — a API responde 404 nos dois casos, de propósito: 403 confirmaria que ela existe."
              : undefined
          }
        />
      </Tela>
    );
  }

  const reserva = completo.data.reservation;

  /**
   * A composição só é buscada quando a realocação faz sentido — reserva com
   * calendário bloqueado e uma unidade só. Nos outros casos seria uma
   * requisição por nada, e a rota exige `inventory:ver`, que nem todo perfil
   * com `reservations:editar` tem.
   */
  const precisaDaComposicao = podeRealocarUnidade(reserva) && can(permissions, "reservations", "editar");
  const composicao = precisaDaComposicao
    ? await carregarLista<UnidadeDaComposicao>(`/unit-types/${reserva.unit_type_id}/members`)
    : null;

  const permissoes = {
    editar: can(permissions, "reservations", "editar"),
    excluir: can(permissions, "reservations", "excluir"),
  };

  return (
    <Tela>
      {/* Sem `CabecalhoDeTela` aqui: o `<h1>` desta tela é o código da reserva,
          dentro do componente. Dois `<h1>` na mesma página é o tipo de coisa que
          ninguém vê e que o leitor de tela lê duas vezes. */}
      <Link
        href="/app/reservas"
        className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
      >
        <ChevronLeft className="size-3.5" aria-hidden="true" />
        Reservas
      </Link>

      {composicao && !composicao.ok ? (
        <Nota variante="atencao">
          A composição deste produto não carregou ({composicao.code}), então trocar a unidade não é oferecido
          aqui. Essa rota pede <code className="font-mono text-xs">inventory:ver</code> — se o seu perfil só
          tem reservas, peça o acesso à gestão em Configurações → Perfis.
        </Nota>
      ) : null}

      <DetalheDaReserva
        completo={completo.data}
        permissoes={permissoes}
        produtos={produtos.ok ? produtos.data : []}
        composicao={composicao?.ok ? composicao.data : []}
      />
    </Tela>
  );
}
