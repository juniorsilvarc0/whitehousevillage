"use client";

import * as React from "react";
import { usePathname, useRouter } from "next/navigation";
import { CalendarPlus } from "lucide-react";

import { notificarFalha, notificarSucesso } from "@/components/agenda/avisos";
import { BarraDaAgenda } from "@/components/agenda/barra-da-agenda";
import { DetalheDoEvento, type DestinosDoDetalhe } from "@/components/agenda/detalhe-do-evento";
import { GradeDeHorarios } from "@/components/agenda/grade-de-horarios";
import { ModalDeEvento, type AlvoDoEvento } from "@/components/agenda/modal-de-evento";
import { VisaoLista } from "@/components/agenda/visao-lista";
import { VisaoMes } from "@/components/agenda/visao-mes";
import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { EstadoVazio } from "@/components/layout/estados";
import { IndicadorDeTempoReal } from "@/components/tempo-real/indicador";
import { Button } from "@/components/ui/button";
import type { Resultado } from "@/lib/acoes/resultado";
import { concluirEvento } from "@/lib/agenda/acoes";
import { pedeRecarga } from "@/lib/agenda/mensagens";
import type { PermissoesDaAgenda } from "@/lib/agenda/permissoes";
import { ROTULO_DA_CONCLUSAO } from "@/lib/agenda/rotulos";
import { janelaDaVisao, paraQueryString, temFiltro, type FiltrosDaAgenda } from "@/lib/agenda/visoes";
import type {
  BloqueioDeHorario,
  ConfiguracaoDaAgenda,
  EventoDaAgenda,
  PessoaDaAgenda,
  UnidadeDaAgenda,
} from "@/lib/agenda/tipos";
import { useAtualizacaoAoVivo } from "@/lib/tempo-real/atualizacao";
import type { EventoDoStream } from "@/lib/tempo-real/eventos";

import "./agenda.css";

type DadosDaJanela = { eventos: EventoDaAgenda[]; bloqueios: BloqueioDeHorario[] };

/**
 * A agenda operacional — dia, semana, mês e lista sobre **uma** leitura da
 * janela (`GET /agenda/events`, todas as páginas), com os bloqueios de
 * horário (`GET /agenda/blocks`) e o funcionamento (`GET /agenda/settings`)
 * desenhados como fundo.
 *
 * ## Onde cada estado mora
 *
 * | Estado | Onde | Motivo |
 * |---|---|---|
 * | Visão, dia, filtros | **query string** | o recorte é um link |
 * | Eventos e bloqueios | estado local, semeado pelo servidor | é o que o tempo real repõe sem refazer a árvore de RSC |
 * | Horários, unidades, pessoas | props do servidor | não mudam por evento |
 * | Detalhe e formulários | controle imperativo | nascem limpos a cada abertura |
 *
 * ## "Hoje"
 *
 * `hoje` é o `today` de `GET /agenda/settings` — o dia da casa, dito pelo
 * servidor. Nada aqui chama o relógio do navegador para decidir o dia.
 *
 * ## Tempo real
 *
 * O barramento não tem tópico da agenda: ela ouve `calendar` (que exige
 * `calendar:ver`), que avisa de reserva e de bloqueio de unidade — exatamente
 * o que cria, cancela e move os eventos automáticos e os derivados. O aviso é
 * magro; a janela é relida pela rota de sempre, no escopo de quem olha.
 */
export function AgendaOperacional({
  filtros,
  hoje,
  horarios,
  eventosIniciais,
  bloqueiosIniciais,
  unidades,
  pessoas,
  eu,
  permissoes,
  destinos,
  buscas,
  aoVivo: aoVivoHabilitado,
  aoRecarregar,
}: {
  filtros: FiltrosDaAgenda;
  hoje: string;
  horarios: ConfiguracaoDaAgenda;
  eventosIniciais: EventoDaAgenda[];
  bloqueiosIniciais: BloqueioDeHorario[];
  unidades: readonly UnidadeDaAgenda[] | null;
  pessoas: readonly PessoaDaAgenda[];
  eu: PessoaDaAgenda;
  permissoes: PermissoesDaAgenda;
  destinos: DestinosDoDetalhe;
  buscas: { reservas: boolean; contatos: boolean };
  /** Assina o tópico `calendar` do barramento — só com `calendar:ver`. */
  aoVivo: boolean;
  aoRecarregar: (filtros: FiltrosDaAgenda) => Promise<Resultado<DadosDaJanela>>;
}) {
  const router = useRouter();
  const caminho = usePathname();
  const janela = React.useMemo(() => janelaDaVisao(filtros.visao, filtros.data), [filtros.visao, filtros.data]);

  // Ajuste de estado durante o render — "prop nova manda", como no mapa: um
  // efeito desenharia uma vez a janela anterior antes de se corrigir.
  const semente = React.useMemo<DadosDaJanela>(
    () => ({ eventos: eventosIniciais, bloqueios: bloqueiosIniciais }),
    [eventosIniciais, bloqueiosIniciais],
  );
  const [dados, setDados] = React.useState(semente);
  const [sementeAnterior, setSementeAnterior] = React.useState(semente);
  if (sementeAnterior !== semente) {
    setSementeAnterior(semente);
    setDados(semente);
  }

  const vivo = useAtualizacaoAoVivo<DadosDaJanela>({
    topicos: ["calendar"],
    habilitado: aoVivoHabilitado,
    interessa: React.useCallback((e: EventoDoStream) => e.entity === "stay_block" || e.entity === "reservation", []),
    buscar: React.useCallback(() => aoRecarregar(filtros), [aoRecarregar, filtros]),
    aoAtualizar: setDados,
  });

  const detalhe = useControleDeModal<EventoDaAgenda>();
  const formulario = useControleDeModal<AlvoDoEvento>();

  // Quem pode ser responsável: os da lista de usuários (só o admin lê
  // `/users`) e os que a agenda já mostra — sem inventar outra porta.
  const responsaveis = React.useMemo(() => {
    const mapa = new Map<string, string>();
    for (const p of pessoas) mapa.set(p.id, p.name);
    for (const e of dados.eventos) if (e.assignee_id && e.assignee_name && !mapa.has(e.assignee_id)) mapa.set(e.assignee_id, e.assignee_name);
    return [...mapa.entries()].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name, "pt-BR"));
  }, [pessoas, dados.eventos]);

  const linkDoDia = React.useCallback(
    (dia: string) => `${caminho}${paraQueryString({ ...filtros, visao: "dia", data: dia }, hoje)}`,
    [caminho, filtros, hoje],
  );

  const concluirDaLista = React.useCallback(
    async (evento: EventoDaAgenda) => {
      const r = await concluirEvento(evento.id);
      if (r.ok) {
        notificarSucesso(ROTULO_DA_CONCLUSAO[r.data.type] ?? "Concluído", r.data.title);
        return;
      }
      notificarFalha("Não foi possível concluir", r, "conclusao");
      if (pedeRecarga(r)) router.refresh();
    },
    [router],
  );

  const novoEvento = permissoes.criar ? (
    <Button onClick={() => formulario.abrir({ modo: "criar", data: filtros.visao === "dia" || filtros.visao === "lista" ? filtros.data : hoje })}>
      <CalendarPlus aria-hidden="true" />
      Marcar
    </Button>
  ) : null;

  const vazio = dados.eventos.length === 0;

  return (
    <div className="flex flex-col gap-4">
      <BarraDaAgenda
        filtros={filtros}
        janela={janela}
        hoje={hoje}
        unidades={unidades}
        indicador={
          <div className="flex items-center gap-3">
            {aoVivoHabilitado ? (
              <IndicadorDeTempoReal
                estado={vivo.tempoReal.estado}
                atualizando={vivo.atualizando}
                atualizadoEm={vivo.atualizadoEm}
                aoReconectar={vivo.tempoReal.reconectar}
                assunto="agenda"
              />
            ) : null}
            {novoEvento}
          </div>
        }
      />

      {filtros.visao === "dia" || filtros.visao === "semana" ? (
        <GradeDeHorarios
          dias={janela.dias}
          hoje={hoje}
          eventos={dados.eventos}
          bloqueios={dados.bloqueios}
          horarios={horarios.business_hours}
          passoEmMinutos={horarios.slot_minutes}
          aoAbrir={detalhe.abrir}
          linkDoDia={filtros.visao === "semana" ? linkDoDia : undefined}
        />
      ) : filtros.visao === "mes" ? (
        <VisaoMes janela={janela} mes={filtros.data} hoje={hoje} eventos={dados.eventos} aoAbrir={detalhe.abrir} linkDoDia={linkDoDia} />
      ) : (
        <VisaoLista
          janela={janela}
          hoje={hoje}
          eventos={dados.eventos}
          aoAbrir={detalhe.abrir}
          podeConcluir={permissoes.editar}
          aoConcluir={concluirDaLista}
        />
      )}

      {vazio && filtros.visao !== "lista" ? (
        <EstadoVazio
          icone={CalendarPlus}
          titulo={temFiltro(filtros) ? "Nada com esse recorte" : "Nada marcado neste período"}
          descricao={
            temFiltro(filtros) ? (
              <>Tire um dos filtros, ou ande para outro período.</>
            ) : (
              <>
                Check-in, check-out e limpeza aparecem sozinhos quando uma reserva é confirmada. Visitas, eventos e limpezas
                avulsas se marcam aqui.
              </>
            )
          }
          acao={temFiltro(filtros) ? undefined : novoEvento}
        />
      ) : null}

      <DetalheDoEvento
        controle={detalhe.ref}
        permissoes={permissoes}
        destinos={destinos}
        aoEditar={(evento) => formulario.abrir({ modo: "editar", evento })}
      />
      <ModalDeEvento
        controle={formulario.ref}
        unidades={unidades}
        pessoas={responsaveis}
        eu={eu}
        escopoAoCriar={permissoes.escopoAoCriar}
        escopoAoEditar={permissoes.escopoAoEditar}
        passoEmMinutos={horarios.slot_minutes}
        buscas={buscas}
      />
    </div>
  );
}
