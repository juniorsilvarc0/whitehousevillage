import Link from "next/link";
import { Clock, Ban } from "lucide-react";

import { AgendaOperacional } from "@/components/agenda/agenda-operacional";
import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import { buttonVariants } from "@/components/ui/button";
import { recarregarAgenda } from "@/lib/agenda/acoes";
import { carregarBloqueios, carregarEventos, carregarHorarios, pessoasDaAgenda, unidadesDaAgenda } from "@/lib/agenda/api";
import { CAMINHO_DOS_BLOQUEIOS, CAMINHO_DOS_HORARIOS } from "@/lib/agenda/mensagens";
import { permissoesDaAgenda } from "@/lib/agenda/permissoes";
import { consultaDosEventos, janelaDaVisao, lerFiltros, type ParametrosCrus } from "@/lib/agenda/visoes";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { FUSO } from "@/lib/fuso";
import { cn } from "@/lib/utils";

export const metadata = { title: "Agenda" };

/**
 * A agenda operacional (spec §9, F2-21): check-in, check-out, limpeza,
 * manutenção, visita, evento e bloqueio — o que a operação faz, onde e quando.
 *
 * Três leituras, em ordem:
 *
 * 1. `GET /agenda/settings` primeiro, porque traz o **`today` da casa** — é
 *    ele, e não o relógio de quem abriu a tela, que diz que dia a agenda abre
 *    e qual coluna é "hoje". Sem ele a tela não abre.
 * 2. `GET /agenda/events` (a janela inteira, `per_page=100`, seguindo
 *    `total_pages`) e `GET /agenda/blocks` (o fundo da grade), em paralelo.
 * 3. As unidades e as pessoas do formulário, pela rota que a matriz alcançar.
 *
 * O recurso é `agenda`; o escopo `own` do corretor é do SQL — a tela mostra o
 * que vem e não filtra nada.
 */
export default async function AgendaPage({ searchParams }: { searchParams: Promise<ParametrosCrus> }) {
  const { user, permissions } = await requireSession();
  const permissoes = permissoesDaAgenda(permissions);
  if (!permissoes.ver) return <SemAcesso recurso="agenda" />;

  const horarios = await carregarHorarios();
  if (!horarios.ok) {
    return (
      <Tela>
        <CabecalhoDeTela titulo="Agenda" />
        <EstadoDeErro
          code={horarios.code}
          titulo="Não foi possível carregar os horários da operação"
          detalhe="Sem eles a agenda não sabe que dia é hoje na casa nem onde começa o expediente. Tente recarregar a página."
        />
      </Tela>
    );
  }

  const hoje = horarios.data.today;
  const { filtros, avisos } = lerFiltros(await searchParams, hoje);
  const janela = janelaDaVisao(filtros.visao, filtros.data);

  const [eventos, bloqueios, unidades, pessoas] = await Promise.all([
    carregarEventos(consultaDosEventos(filtros, janela, user.id)),
    carregarBloqueios(janela),
    // As unidades servem ao filtro da barra (a diarista que só olha o AP-02) e
    // ao formulário; as pessoas, só ao formulário.
    unidadesDaAgenda(permissions, hoje),
    permissoes.criar || permissoes.editar ? pessoasDaAgenda(permissions) : Promise.resolve([]),
  ]);

  const acoes = (
    <>
      <Link href={CAMINHO_DOS_BLOQUEIOS} className={cn(buttonVariants({ variant: "outline", size: "sm" }), "max-sm:h-10")}>
        <Ban aria-hidden="true" />
        Bloqueios de horário
      </Link>
      <Link href={CAMINHO_DOS_HORARIOS} className={cn(buttonVariants({ variant: "ghost", size: "sm" }), "max-sm:h-10")}>
        <Clock aria-hidden="true" />
        Horários
      </Link>
    </>
  );

  return (
    <Tela>
      <CabecalhoDeTela
        titulo="Agenda"
        descricao="Check-in, check-out e limpeza nascem da reserva confirmada. Manutenção e bloqueio de unidade aparecem sozinhos. Visitas, eventos e limpezas avulsas se marcam aqui."
        acoes={acoes}
      />

      {horarios.data.provisional ? (
        <Nota variante="atencao">
          <strong>Horários provisórios, a confirmar.</strong> Check-in às {horarios.data.checkin_time}, check-out às{" "}
          {horarios.data.checkout_time} e limpeza das {horarios.data.cleaning_start} às {horarios.data.cleaning_end} são os
          valores iniciais do sistema, e o dono ainda não os confirmou.{" "}
          <Link href={CAMINHO_DOS_HORARIOS} className="font-medium text-primary underline-offset-4 hover:underline">
            Ver os horários
          </Link>
        </Nota>
      ) : null}
      {horarios.data.timezone && horarios.data.timezone !== FUSO ? (
        <Nota variante="atencao">
          O fuso da propriedade ({horarios.data.timezone}) não é o fuso do painel ({FUSO}): as horas desta tela podem
          aparecer deslocadas. Avise quem cuida do sistema.
        </Nota>
      ) : null}
      {avisos.length > 0 ? <Nota variante="atencao">{avisos.join(" ")}</Nota> : null}
      {eventos.ok && eventos.data.incompleta ? (
        <Nota variante="atencao">
          Este período tem mais eventos do que a tela carrega de uma vez, e alguns podem não aparecer. Use os filtros ou
          uma visão menor.
        </Nota>
      ) : null}
      {!bloqueios.ok ? (
        <Nota variante="atencao">Os bloqueios de horário da operação não carregaram: a grade aparece sem eles.</Nota>
      ) : null}
      {(permissoes.criar || permissoes.editar) && unidades && !unidades.ok ? (
        <Nota variante="atencao">A lista de unidades não carregou: dá para marcar, mas sem escolher a unidade.</Nota>
      ) : null}

      {!eventos.ok ? (
        <EstadoDeErro
          code={eventos.code}
          titulo="Não foi possível carregar a agenda"
          detalhe={eventos.code === "NOT_FOUND" ? "Esta parte do sistema pode ainda não estar no ar." : undefined}
        />
      ) : (
        <AgendaOperacional
          filtros={filtros}
          hoje={hoje}
          horarios={horarios.data}
          eventosIniciais={eventos.data.eventos}
          bloqueiosIniciais={bloqueios.ok ? bloqueios.data.bloqueios : []}
          unidades={unidades && unidades.ok ? unidades.data : null}
          pessoas={pessoas}
          eu={{ id: user.id, name: user.name }}
          permissoes={permissoes}
          destinos={{
            reservas: can(permissions, "reservations", "ver"),
            manutencao: can(permissions, "maintenance", "ver"),
            mapa: can(permissions, "calendar", "ver"),
          }}
          buscas={{ reservas: can(permissions, "reservations", "ver"), contatos: can(permissions, "contacts", "ver") }}
          aoVivo={can(permissions, "calendar", "ver")}
          aoRecarregar={recarregarAgenda}
        />
      )}
    </Tela>
  );
}
