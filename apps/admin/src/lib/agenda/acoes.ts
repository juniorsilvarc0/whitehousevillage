"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { apiFetch, apiFetchComMeta, apiList } from "@/lib/api/client";
import { detalhesDoZod } from "@/lib/acoes/campos";
import { tentar } from "@/lib/acoes/executar";
import { exigir } from "@/lib/acoes/guarda";
import { falha, type Resultado } from "@/lib/acoes/resultado";
import { getSession } from "@/lib/auth/session";
import { carregarBloqueios, carregarEventos } from "@/lib/agenda/api";
import {
  BloqueioFormulario,
  HorariosFormulario,
  bloqueioParaEntrada,
  esquemaDoEvento,
  eventoParaAtualizar,
  eventoParaCriar,
  eventoParaSubstituir,
  horariosParaEntrada,
  type EventoFormulario,
} from "@/lib/agenda/esquemas";
import { verboDaEdicao } from "@/lib/agenda/permissoes";
import { ehDataISO } from "@/lib/datas";
import { consultaDosEventos, janelaDaVisao, lerFiltros, type FiltrosDaAgenda } from "@/lib/agenda/visoes";
import type {
  AvisoDeAgenda,
  BloqueioDeHorario,
  ConfiguracaoDaAgenda,
  ContatoDaAgenda,
  EdicaoDoEvento,
  EscritaNaAgenda,
  EventoDaAgenda,
  GravacaoDosHorarios,
  ReservaDaAgenda,
} from "@/lib/agenda/tipos";

/**
 * Escrita da agenda.
 *
 * Cada action declara o par recurso × ação que o `x-rbac` da rota pede — a
 * guarda aqui é cortesia, a API recusa de novo e é ela que vale. As transições
 * são **ações nomeadas** (`/complete`, `DELETE`), nunca um `PATCH {status}`: a
 * máquina de estados é da API, e o painel só pede o passo.
 *
 * Sem `Idempotency-Key`, como o contrato decide: nenhuma rota daqui cria
 * reserva nem dinheiro, e os duplicados que importam o banco já recusa (o
 * índice único dos automáticos, a `EXCLUDE` dos manuais na mesma unidade).
 *
 * Revalida a agenda **e** os horários: gravar os horários reprograma os
 * eventos automáticos, e a grade lê os dois.
 */

const AGENDA = "/(app)/app/agenda";
const HORARIOS = "/(app)/app/configuracoes/agenda";

function revalidar(): void {
  revalidatePath(AGENDA, "layout");
}

const Id = z.string().uuid();

function idInvalido(): ReturnType<typeof falha> {
  return falha("VALIDATION_ERROR", "Identificador inválido.");
}

type MetaDaEscrita = { warnings?: AvisoDeAgenda[] };

function escrita(r: { data: EventoDaAgenda; meta?: MetaDaEscrita }): EscritaNaAgenda {
  return { evento: r.data, avisos: Array.isArray(r.meta?.warnings) ? r.meta.warnings : [] };
}

// ── Eventos ─────────────────────────────────────────────────────────────────

/**
 * Marca um evento à mão — visita, evento ou limpeza avulsa. O `201` pode vir
 * com `meta.warnings` (fora do horário, dentro de um bloqueio de horário, o
 * responsável sem folga): o evento **foi criado**, e a tela avisa sem
 * desfazer nada. Unidade ocupada é `409 UNIT_NOT_AVAILABLE` com
 * `details.conflicts`, e nada é criado.
 */
export async function marcarEvento(valores: EventoFormulario): Promise<Resultado<EscritaNaAgenda>> {
  const recusa = await exigir("agenda", "criar");
  if (recusa) return recusa;

  const analise = esquemaDoEvento("criar").safeParse(valores);
  if (!analise.success) return falha("VALIDATION_ERROR", "Confira o evento.", detalhesDoZod(analise.error));

  const resultado = await tentar(async () =>
    escrita(
      await apiFetchComMeta<EventoDaAgenda, MetaDaEscrita>("/agenda/events", {
        method: "POST",
        body: eventoParaCriar(analise.data),
      }),
    ),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

/**
 * Grava a edição pelo `editable` que a API mandou: `tudo` substitui o manual
 * inteiro (`PUT`); `segue_a_reserva` e `so_notas` mudam só o que aceitam
 * (`PATCH`). Quem confere é a API: se o `editable` mudou por baixo da tela, a
 * resposta é `409 AGENDA_EVENT_CLOSED` com o `editable` de agora.
 */
export async function salvarEvento(
  id: string,
  editable: EdicaoDoEvento,
  valores: EventoFormulario,
): Promise<Resultado<EscritaNaAgenda>> {
  const recusa = await exigir("agenda", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const verbo = verboDaEdicao(editable);
  if (!verbo || editable === "nada") {
    return falha("AGENDA_EVENT_CLOSED", "O evento não aceita mudanças.", { editable });
  }

  const analise = esquemaDoEvento(editable).safeParse(valores);
  if (!analise.success) return falha("VALIDATION_ERROR", "Confira o evento.", detalhesDoZod(analise.error));

  const corpo =
    verbo === "PUT"
      ? eventoParaSubstituir(analise.data)
      : eventoParaAtualizar(analise.data, editable as "segue_a_reserva" | "so_notas");

  const resultado = await tentar(async () =>
    escrita(
      await apiFetchComMeta<EventoDaAgenda, MetaDaEscrita>(`/agenda/events/${id}`, { method: verbo, body: corpo }),
    ),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

/** `agendado → concluido` — a limpeza terminou, a visita aconteceu. Sem corpo.
 *  O check-in e o check-out automáticos concluem pela reserva (`409
 *  segue_a_reserva`); a limpeza de amanhã ainda não conclui (`nao_comecou`). */
export async function concluirEvento(id: string): Promise<Resultado<EventoDaAgenda>> {
  const recusa = await exigir("agenda", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const resultado = await tentar(() => apiFetch<EventoDaAgenda>(`/agenda/events/${id}/complete`, { method: "POST" }));
  if (resultado.ok) revalidar();
  return resultado;
}

/** Desmarca — **cancela, não apaga**: a visita desmarcada continua na agenda
 *  como desmarcada. `DELETE` responde `200` com o evento como ficou. */
export async function desmarcarEvento(id: string): Promise<Resultado<EventoDaAgenda>> {
  const recusa = await exigir("agenda", "excluir");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const resultado = await tentar(() => apiFetch<EventoDaAgenda>(`/agenda/events/${id}`, { method: "DELETE" }));
  if (resultado.ok) revalidar();
  return resultado;
}

// ── Bloqueios de horário da operação ────────────────────────────────────────

/** "Equipe em treinamento das 14:00 às 16:00". Não ocupa unidade e não recusa
 *  nada — o evento marcado em cima recebe o aviso `bloqueio_de_horario`. Em
 *  escopo `own` a API recusa (`own_scope_not_applicable`). */
export async function bloquearHorario(valores: BloqueioFormulario): Promise<Resultado<BloqueioDeHorario>> {
  const recusa = await exigir("agenda", "criar");
  if (recusa) return recusa;

  const analise = BloqueioFormulario.safeParse(valores);
  if (!analise.success) return falha("VALIDATION_ERROR", "Confira o bloqueio.", detalhesDoZod(analise.error));

  const resultado = await tentar(() =>
    apiFetch<BloqueioDeHorario>("/agenda/blocks", { method: "POST", body: bloqueioParaEntrada(analise.data) }),
  );
  if (resultado.ok) revalidar();
  return resultado;
}

/** Apaga de verdade (`204`): é configuração do tempo da operação, não
 *  histórico — o antes fica em `audit_log`. */
export async function removerBloqueioDeHorario(id: string): Promise<Resultado<null>> {
  const recusa = await exigir("agenda", "excluir");
  if (recusa) return recusa;
  if (!Id.safeParse(id).success) return idInvalido();

  const resultado = await tentar(async () => {
    await apiFetch<unknown>(`/agenda/blocks/${id}`, { method: "DELETE" });
    return null;
  });
  if (resultado.ok) revalidar();
  return resultado;
}

// ── Os horários da operação ─────────────────────────────────────────────────

/**
 * `PUT /agenda/settings` — pede `settings:editar`, e não `agenda:editar`: o
 * horário de entrada é o que a casa promete ao hóspede. Reprograma, na mesma
 * transação, os eventos automáticos agendados de hoje em diante que ainda
 * estavam no horário antigo; `meta.retimed_events` diz quantos.
 */
export async function gravarHorarios(valores: HorariosFormulario): Promise<Resultado<GravacaoDosHorarios>> {
  const recusa = await exigir("settings", "editar");
  if (recusa) return recusa;

  const analise = HorariosFormulario.safeParse(valores);
  if (!analise.success) return falha("VALIDATION_ERROR", "Confira os horários.", detalhesDoZod(analise.error));

  const resultado = await tentar(async () => {
    const r = await apiFetchComMeta<ConfiguracaoDaAgenda, { retimed_events?: number }>("/agenda/settings", {
      method: "PUT",
      body: horariosParaEntrada(analise.data),
    });
    const n = r.meta?.retimed_events;
    return { configuracao: r.data, reprogramados: typeof n === "number" && n >= 0 ? n : 0 };
  });
  if (resultado.ok) {
    revalidatePath(HORARIOS);
    revalidar();
  }
  return resultado;
}

// ── Apoio do formulário ─────────────────────────────────────────────────────

/** Busca de reserva por código ou nome do titular — só as que aceitam evento
 *  ligado (`hold`, `confirmed`, `checked_in`). */
export async function buscarReservas(termo: string): Promise<Resultado<ReservaDaAgenda[]>> {
  const recusa = await exigir("reservations", "ver");
  if (recusa) return recusa;
  const q = termo.trim().slice(0, 120);
  if (q.length < 2) return { ok: true, data: [] };

  const r = await tentar(() =>
    apiList<{ id: string; code: string; check_in: string; check_out: string; contact_name?: string | null }>("/reservations", {
      query: { q, status: "hold,confirmed,checked_in", sort: "check_in", per_page: 10 },
    }),
  );
  if (!r.ok) return r;
  return {
    ok: true,
    data: r.data.data.map((x) => ({
      id: x.id,
      code: x.code,
      contact_name: x.contact_name ?? null,
      check_in: x.check_in,
      check_out: x.check_out,
    })),
  };
}

/** Busca de contato pelo nome — a coleção mascara documento e telefone, e o
 *  nome não tem máscara: é só ele que o formulário usa. */
export async function buscarContatos(termo: string): Promise<Resultado<ContatoDaAgenda[]>> {
  const recusa = await exigir("contacts", "ver");
  if (recusa) return recusa;
  const q = termo.trim().slice(0, 120);
  if (q.length < 2) return { ok: true, data: [] };

  const r = await tentar(() => apiList<{ id: string; name: string }>("/contacts", { query: { q, per_page: 10 } }));
  if (!r.ok) return r;
  return { ok: true, data: r.data.data.map((c) => ({ id: c.id, name: c.name })) };
}

// ── Tempo real ──────────────────────────────────────────────────────────────

/**
 * A janela visível de novo — o que o aviso do barramento manda refazer. O
 * evento do SSE é magro (`{entity, id, v}`); o dado vem daqui, pela mesma
 * rota autenticada e no mesmo escopo do `GET` de sempre.
 */
export async function recarregarAgenda(
  filtros: FiltrosDaAgenda,
): Promise<Resultado<{ eventos: EventoDaAgenda[]; bloqueios: BloqueioDeHorario[] }>> {
  const recusa = await exigir("agenda", "ver");
  if (recusa) return recusa;
  // Server Action é endpoint público: o recorte que chega do navegador passa
  // pelo mesmo leitor da URL, e o que não for válido não vira consulta.
  if (!ehDataISO(filtros?.data ?? "")) return falha("VALIDATION_ERROR", "Recorte inválido.");
  const { filtros: f } = lerFiltros(
    {
      visao: filtros.visao,
      data: filtros.data,
      tipo: filtros.tipo,
      unidade: filtros.unidade,
      responsavel: filtros.responsavel,
      cancelados: filtros.cancelados ? "1" : "",
    },
    filtros.data,
  );
  const sessao = await getSession();
  const janela = janelaDaVisao(f.visao, f.data);
  const [eventos, bloqueios] = await Promise.all([
    carregarEventos(consultaDosEventos(f, janela, sessao?.user.id ?? "")),
    carregarBloqueios(janela),
  ]);
  if (!eventos.ok) return eventos;
  return { ok: true, data: { eventos: eventos.data.eventos, bloqueios: bloqueios.ok ? bloqueios.data.bloqueios : [] } };
}
