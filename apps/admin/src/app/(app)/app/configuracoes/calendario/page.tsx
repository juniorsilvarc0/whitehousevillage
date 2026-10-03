import { Suspense } from "react";

import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Secao, Tela } from "@/components/layout/tela";
import type { Feriado, PeriodoEspecial } from "@/lib/api/comercial";
import { carregarLista } from "@/lib/api/carregar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";

import { FiltroDoCalendario } from "./filtro";
import { ListaDeFeriados, ListaDePeriodos } from "./painel";

export const metadata = { title: "Calendário comercial" };

/**
 * Feriados e períodos especiais — o que decide o **tipo** de cada noite.
 *
 * As duas coleções ficam na mesma tela porque respondem juntas à mesma
 * pergunta ("por que esta data é cara?") e porque a sobreposição entre elas é
 * parte da regra, não um efeito colateral a esconder.
 */
export default async function CalendarioPage({
  searchParams,
}: {
  searchParams: Promise<{ de?: string; ate?: string; tipo?: string }>;
}) {
  const { permissions } = await requireSession();
  if (!can(permissions, "settings", "ver")) return <SemAcesso recurso="settings" />;

  const { de, ate, tipo } = await searchParams;

  const [feriados, periodos] = await Promise.all([
    carregarLista<Feriado>("/holidays", { from: de, to: ate }),
    carregarLista<PeriodoEspecial>("/special-periods", { from: de, to: ate, kind: tipo }),
  ]);

  const permissoes = {
    criar: can(permissions, "settings", "criar"),
    editar: can(permissions, "settings", "editar"),
    excluir: can(permissions, "settings", "excluir"),
  };

  return (
    <Tela>
      <CabecalhoDeTela
        voltar={{ href: "/app/configuracoes", rotulo: "Configurações" }}
        titulo="Calendário comercial"
        descricao="Aqui você marca feriados e temporadas. É isso que define o tipo de cada noite e, portanto, o preço da diária."
      />

      <Nota variante="atencao">
        <strong>Pode haver períodos um dentro do outro.</strong> O Réveillon fica dentro da alta temporada,
        e tudo bem: quando dois períodos caem na mesma noite, vale o mais importante (réveillon e carnaval
        &gt; feriado &gt; alta temporada &gt; fim de semana &gt; dia normal). Não encurte a alta temporada
        para &ldquo;não encostar&rdquo; no réveillon — isso só mudaria o preço das noites entre os dois.
      </Nota>

      {/* `useSearchParams` obriga a fronteira de Suspense; sem ela a rota
          inteira sairia do pré-render estático com um erro de build. */}
      <Suspense fallback={<div className="h-16" />}>
        <FiltroDoCalendario />
      </Suspense>

      <Secao
        titulo="Feriados"
        descricao="Cada feriado ocupa uma data. Nessa noite vale o preço de feriado, a não ser que seja réveillon ou carnaval."
      >
        {feriados.ok ? (
          <ListaDeFeriados feriados={feriados.data} permissoes={permissoes} />
        ) : (
          <EstadoDeErro code={feriados.code} titulo="Não foi possível carregar os feriados" />
        )}
      </Secao>

      <Secao
        titulo="Períodos especiais"
        descricao="Temporadas com data de início e de fim. As duas datas entram: 28/12 a 02/01 conta os seis dias, inclusive 02/01."
      >
        {periodos.ok ? (
          <ListaDePeriodos periodos={periodos.data} permissoes={permissoes} />
        ) : (
          <EstadoDeErro code={periodos.code} titulo="Não foi possível carregar os períodos especiais" />
        )}
      </Secao>
    </Tela>
  );
}
