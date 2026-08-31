import { Suspense } from "react";

import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import { BarraDeFiltros } from "@/components/reservas/barra-de-filtros";
import { ListaDeReservas } from "@/components/reservas/lista";
import type { Produto } from "@/lib/api/comercial";
import { carregarLista } from "@/lib/api/carregar";
import { can, escopoDe } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { listarReservas } from "@/lib/reservas/api";
import { lerFiltros, paraConsulta, type ParametrosCrus } from "@/lib/reservas/filtros";

export const metadata = { title: "Reservas" };

/**
 * A lista de reservas — o produto da casa, finalmente com tela.
 *
 * O menu aponta para cá desde a Fase 0 e a rota não existia: clicar em
 * "Reservas" respondia 404 do Next, com o back-end inteiro pronto do outro lado
 * (lista, detalhe, confirmar, cancelar, remarcar, check-in/out, realocar,
 * estender). Medido em 27/08/2026: `GET /app/reservas` → **HTTP 404**, enquanto
 * `GET /api/v1/reservations` devolvia `WH-2026-0001` normalmente — e a tela da
 * oportunidade já linkava para `/app/reservas/{id}`
 * (`components/crm/oportunidade.tsx:353`), um link morto no meio do fluxo de
 * venda.
 *
 * O recorte inteiro vive na query string, e é `lerFiltros` que o valida: o que
 * a API recusaria é descartado aqui, com o aviso na tela — filtro ignorado em
 * silêncio é como o operador conclui que "a busca não funciona".
 */
export default async function ReservasPage({ searchParams }: { searchParams: Promise<ParametrosCrus> }) {
  const { permissions } = await requireSession();
  if (!can(permissions, "reservations", "ver")) return <SemAcesso recurso="reservations" />;

  const { filtros, avisos } = lerFiltros(await searchParams);

  // Os produtos vão junto porque o filtro precisa deles no primeiro quadro.
  // Buscá-los depois deixaria o `<select>` vazio no instante em que a tela
  // aparece, que é exatamente quando alguém tenta recortar.
  const [lista, produtos] = await Promise.all([
    listarReservas(paraConsulta(filtros)),
    carregarLista<Produto>("/unit-types", { sort: "sort_order" }),
  ]);

  const permissoes = {
    editar: can(permissions, "reservations", "editar"),
    excluir: can(permissions, "reservations", "excluir"),
  };
  const escopo = escopoDe(permissions, "reservations", "ver");

  return (
    <Tela>
      <CabecalhoDeTela
        titulo="Reservas"
        descricao={
          <>
            Da pré-reserva ao check-out. O código é imutável e é por ele que a reserva é falada ao telefone;
            os valores são os que a venda <strong>congelou</strong> — mudar o tarifário amanhã não reescreve
            nada aqui.
          </>
        }
      />

      {escopo === "own" ? (
        <Nota>
          Seu perfil enxerga <strong>as suas</strong> reservas. Quem aplica esse recorte é o servidor, na
          consulta — não é um filtro desta tela, e por isso a contagem das páginas também já vem recortada.
        </Nota>
      ) : null}

      <Suspense fallback={<div className="h-24" />}>
        <BarraDeFiltros produtos={produtos.ok ? produtos.data : []} />
      </Suspense>

      {avisos.length > 0 ? (
        <Nota variante="atencao">
          {avisos.map((aviso) => (
            <span key={aviso} className="block">
              {aviso}
            </span>
          ))}
        </Nota>
      ) : null}

      {!produtos.ok ? (
        <Nota variante="atencao">
          O catálogo de produtos não carregou ({produtos.code}), então o filtro por produto está vazio. A
          lista abaixo continua correta.
        </Nota>
      ) : null}

      {lista.ok ? (
        <Suspense fallback={<div className="h-64" />}>
          <ListaDeReservas
            reservas={lista.data.data}
            meta={lista.data.meta}
            filtros={filtros}
            permissoes={permissoes}
          />
        </Suspense>
      ) : (
        <EstadoDeErro
          code={lista.code}
          titulo="Não foi possível carregar as reservas"
          detalhe={
            lista.code === "NETWORK_ERROR"
              ? "A API não respondeu. O recorte continua na URL — recarregar a página repete exatamente esta consulta."
              : undefined
          }
        />
      )}
    </Tela>
  );
}
