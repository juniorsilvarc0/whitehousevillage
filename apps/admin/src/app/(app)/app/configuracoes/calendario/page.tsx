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
        descricao="Cada noite recebe um único tipo de data. Feriados e períodos especiais são o que a decide — e é do tipo que sai a tarifa."
      />

      <Nota variante="atencao">
        <strong>Períodos se sobrepõem de propósito.</strong> O Réveillon mora dentro da alta temporada, e
        a tabela não tem constraint de exclusão justamente por isso: quem desempata é a precedência
        (réveillon e carnaval 100 &gt; feriado 80 &gt; alta 60 &gt; fim de semana 40 &gt; normal 0).
        Encolher a alta temporada para &ldquo;não encostar&rdquo; no réveillon não corrige nada — só muda o preço de
        todas as noites entre os dois.
      </Nota>

      {/* `useSearchParams` obriga a fronteira de Suspense; sem ela a rota
          inteira sairia do pré-render estático com um erro de build. */}
      <Suspense fallback={<div className="h-16" />}>
        <FiltroDoCalendario />
      </Suspense>

      <Secao
        titulo="Feriados"
        descricao="Uma data, um feriado. Classifica a noite como feriado — precedência 80, perde só para réveillon e carnaval."
      >
        {feriados.ok ? (
          <ListaDeFeriados feriados={feriados.data} permissoes={permissoes} />
        ) : (
          <EstadoDeErro code={feriados.code} titulo="Não foi possível carregar os feriados" />
        )}
      </Secao>

      <Secao
        titulo="Períodos especiais"
        descricao="Faixas inclusivas nas duas pontas — 28/12 a 02/01 classifica as seis datas, inclusive 02/01."
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
