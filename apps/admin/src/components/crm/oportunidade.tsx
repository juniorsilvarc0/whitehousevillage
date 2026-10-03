"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import {
  CalendarRange, CheckCircle2, FileText, Loader2, Mail, MapPin, Phone, Plus, Trophy, XCircle,
} from "lucide-react";

import { Abas } from "@/components/crm/abas";
import { AvisosDoCrm, notificar, notificarSucesso } from "@/components/crm/avisos";
import { CampoInline } from "@/components/crm/campo-inline";
import { DialogoDeGanho, type AlvoDeGanho } from "@/components/crm/dialogo-de-ganho";
import { DialogoDePerda, type AlvoDePerda } from "@/components/crm/dialogo-de-perda";
import { FaixaDeSla } from "@/components/crm/faixa-de-sla";
import { TrilhaDeEtapas } from "@/components/crm/trilha-de-etapas";
import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { EstadoVazio } from "@/components/layout/estados";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { paraCentavos } from "@/lib/acoes/campos";
import { atualizarOportunidade, concluirAtividade, criarAtividade } from "@/lib/crm/acoes";
import { CampoInline as ESQUEMA, NotaFormulario, TarefaFormulario } from "@/lib/crm/esquemas";
import { instanteDaOperacao } from "@/lib/crm/datas";
import type {
  Atividade, MotivoDePerda, OportunidadeCompleta,
} from "@/lib/crm/tipos";
import { formatarData, formatarInstante, noitesEntre } from "@/lib/datas";
import { formatarBRL, reaisDeCentavos } from "@/lib/dinheiro";
import { formatarTelefone } from "@/lib/contatos/telefone";
import { ehEstadoDeReserva, ESTADOS } from "@/lib/reservas/estados";
import { RailDaOportunidade } from "@/components/crm/rail-da-oportunidade";
import { cn } from "@/lib/utils";

/**
 * A tela da oportunidade — a que a gestão vive dentro.
 *
 * Tudo desenhado aqui vem de **uma** chamada (`GET /crm/opportunities/{id}/full`):
 * dados, etapas, SLA, atividades, notas, documentos, histórico, linha do tempo,
 * orçamento e reserva. Seis requisições paralelas dariam seis instantes
 * diferentes da mesma negociação, e a linha do tempo — que é literalmente a
 * ordem dos fatos — seria a primeira a mentir.
 *
 * **Estado só muda por ação nomeada.** Os campos inline salvam valor, datas e
 * probabilidade; etapa, status, motivo e reserva não estão entre eles, porque
 * cada um tem uma ação com efeito colateral obrigatório.
 */

export type PermissoesDaOportunidade = {
  editar: boolean;
  criarAtividade: boolean;
  editarAtividade: boolean;
};

export function TelaDaOportunidade({
  completo,
  motivos,
  permissoes,
}: {
  completo: OportunidadeCompleta;
  motivos: MotivoDePerda[];
  permissoes: PermissoesDaOportunidade;
}) {
  const router = useRouter();
  const ganho = useControleDeModal<AlvoDeGanho>();
  const perda = useControleDeModal<AlvoDePerda>();
  const tarefa = useControleDeModal<null>();

  const { opportunity: o, contact } = completo;
  const aberta = o.status === "aberta";
  const podeEditar = permissoes.editar && aberta;

  const proxima = React.useMemo(
    () =>
      completo.activities.find((a) => a.status === "pendente" && a.overdue) ??
      completo.activities.find((a) => a.status === "pendente") ??
      null,
    [completo.activities],
  );

  async function salvarCampo(campos: Parameters<typeof atualizarOportunidade>[1]) {
    const resultado = await atualizarOportunidade(o.id, campos);
    if (resultado.ok) router.refresh();
    return resultado;
  }

  return (
    <>
      <AvisosDoCrm />

      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="font-display text-2xl leading-tight sm:text-3xl">{contact.name}</h1>
            <EtiquetaDeEstado oportunidade={o} />
          </div>
          <p className="mt-1 text-sm text-muted-foreground">
            {o.unit_type_name ?? "Produto a definir"}
            {o.check_in && o.check_out ? (
              <>
                {" · "}
                {formatarData(o.check_in)} → {formatarData(o.check_out)}{" "}
                <span className="tabular-nums">({noitesEntre(o.check_in, o.check_out)} noites)</span>
              </>
            ) : null}
          </p>
          <p className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
            {contact.phone_e164 ? (
              <a href={`tel:${contact.phone_e164}`} className="flex items-center gap-1.5 hover:text-foreground">
                <Phone className="size-3.5" aria-hidden="true" />
                {formatarTelefone(contact.phone_e164)}
              </a>
            ) : null}
            {contact.email ? (
              <a href={`mailto:${contact.email}`} className="flex items-center gap-1.5 hover:text-foreground">
                <Mail className="size-3.5" aria-hidden="true" />
                {contact.email}
              </a>
            ) : null}
            {contact.city ? (
              <span className="flex items-center gap-1.5">
                <MapPin className="size-3.5" aria-hidden="true" />
                {contact.city}
                {contact.state ? `/${contact.state}` : ""}
              </span>
            ) : null}
          </p>
        </div>

        <div className="flex shrink-0 flex-col items-end gap-2">
          <p className="font-mono text-2xl tabular-nums">{formatarBRL(o.amount_cents)}</p>
          {podeEditar ? (
            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                onClick={() =>
                  ganho.abrir({
                    id: o.id,
                    contato: contact.name,
                    produto: o.unit_type_name,
                    check_in: o.check_in,
                    check_out: o.check_out,
                    amount_cents: o.amount_cents,
                    quote_id: o.quote_id,
                  })
                }
              >
                <Trophy aria-hidden="true" />
                Ganhar
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => perda.abrir({ id: o.id, contato: contact.name, reservation_code: o.reservation_code })}
              >
                <XCircle aria-hidden="true" />
                Perder
              </Button>
            </div>
          ) : null}
        </div>
      </header>

      {!aberta ? (
        <Nota variante="atencao">
          Esta oportunidade está <strong>{o.status === "ganha" ? "ganha" : "perdida"}</strong> e não volta a
          ser alterada. Se o cliente voltar, crie uma oportunidade nova para o mesmo contato — assim a
          mesma venda não é contada duas vezes.
        </Nota>
      ) : null}

      <TrilhaDeEtapas etapas={completo.stages} oportunidade={o} />
      <FaixaDeSla faixa={completo.sla} />

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <Abas
          abas={[
            {
              id: "geral",
              rotulo: "Visão geral",
              conteudo: (
                <VisaoGeral completo={completo} podeEditar={podeEditar} aoSalvar={salvarCampo} />
              ),
            },
            {
              id: "atividades",
              rotulo: "Atividades",
              contagem: completo.activities.filter((a) => a.status === "pendente").length,
              conteudo: (
                <Atividades
                  atividades={completo.activities}
                  oportunidadeId={o.id}
                  podeCriar={permissoes.criarAtividade && aberta}
                  podeConcluir={permissoes.editarAtividade}
                  aoNovaTarefa={() => tarefa.abrir(null)}
                />
              ),
            },
            {
              id: "notas",
              rotulo: "Notas",
              contagem: completo.notes.length,
              conteudo: (
                <Notas
                  notas={completo.notes}
                  oportunidadeId={o.id}
                  podeCriar={permissoes.criarAtividade}
                />
              ),
            },
            { id: "documentos", rotulo: "Documentos", contagem: completo.documents.length, conteudo: <Documentos /> },
            { id: "historico", rotulo: "Histórico", conteudo: <Historico completo={completo} /> },
          ]}
        />

        <RailDaOportunidade
          alertas={completo.alerts}
          proxima={proxima}
          oportunidadeId={o.id}
          podeConcluir={permissoes.editarAtividade && aberta}
        />
      </div>

      <DialogoDeGanho controle={ganho.ref} />
      <DialogoDePerda controle={perda.ref} motivos={motivos} />
      <ModalDeTarefa controle={tarefa.ref} oportunidadeId={o.id} />
    </>
  );
}

function EtiquetaDeEstado({ oportunidade }: { oportunidade: OportunidadeCompleta["opportunity"] }) {
  if (oportunidade.status === "ganha") {
    return (
      <Badge className="bg-alcada-livre/15 text-foreground">
        <Trophy aria-hidden="true" />
        Ganha
        {oportunidade.reservation_code ? ` · ${oportunidade.reservation_code}` : ""}
      </Badge>
    );
  }
  if (oportunidade.status === "perdida") {
    return (
      <Badge variant="destructive">
        <XCircle aria-hidden="true" />
        Perdida{oportunidade.lost_reason_label ? ` · ${oportunidade.lost_reason_label}` : ""}
      </Badge>
    );
  }
  return <Badge variant="outline">{oportunidade.stage_name}</Badge>;
}

// ─────────────────────────────── Visão geral ───────────────────────────────

function VisaoGeral({
  completo,
  podeEditar,
  aoSalvar,
}: {
  completo: OportunidadeCompleta;
  podeEditar: boolean;
  aoSalvar: (campos: Parameters<typeof atualizarOportunidade>[1]) => ReturnType<typeof atualizarOportunidade>;
}) {
  const { opportunity: o } = completo;

  return (
    <div className="flex flex-col gap-5">
      <section className="rounded-xl border border-border/60 bg-muted/20 p-4">
        <h3 className="font-display text-sm">O negócio</h3>
        <p className="mt-1 text-xs text-muted-foreground">
          Salva ao sair do campo, sem botão. <kbd className="font-mono">Esc</kbd> volta ao valor salvo.
        </p>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <CampoInline
            id="op-valor"
            label="Valor esperado"
            valorInicial={reaisDeCentavos(o.amount_cents)}
            esquema={ESQUEMA.amount}
            desabilitado={!podeEditar}
            formatar={(v) => formatarBRL(paraCentavos(v))}
            sufixo="R$"
            aoSalvar={(v) => aoSalvar({ amount_cents: paraCentavos(v) })}
            hint="É este número que soma no total da coluna do funil."
          />
          <CampoInline
            id="op-probabilidade"
            label="Probabilidade"
            valorInicial={String(o.probability)}
            esquema={ESQUEMA.probability}
            tipo="number"
            sufixo="%"
            desabilitado={!podeEditar}
            aoSalvar={(v) => aoSalvar({ probability: Number(v) })}
            hint="Muda sozinha quando o negócio muda de etapa; pode ser ajustada à mão depois."
          />
          <CampoInline
            id="op-check-in"
            label="Entrada pretendida"
            valorInicial={o.check_in ?? ""}
            esquema={ESQUEMA.data}
            tipo="date"
            desabilitado={!podeEditar}
            aoSalvar={(v) => aoSalvar({ check_in: v || null })}
            hint="Data desejada — ainda não reserva nada no calendário."
          />
          <CampoInline
            id="op-check-out"
            label="Saída pretendida"
            valorInicial={o.check_out ?? ""}
            esquema={ESQUEMA.data}
            tipo="date"
            desabilitado={!podeEditar}
            aoSalvar={(v) => aoSalvar({ check_out: v || null })}
            hint="O dia da saída não conta como diária e já fica livre para outro hóspede."
          />
          <CampoInline
            id="op-fechamento"
            label="Fechamento esperado"
            valorInicial={o.expected_close ?? ""}
            esquema={ESQUEMA.data}
            tipo="date"
            desabilitado={!podeEditar}
            aoSalvar={(v) => aoSalvar({ expected_close: v || null })}
          />
        </div>
      </section>

      <section className="rounded-xl border border-border/60 bg-muted/20 p-4">
        <h3 className="font-display text-sm">Orçamento e reserva</h3>
        {completo.quote ? (
          <div className="mt-3 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border/60 bg-card px-3 py-2.5">
            <div className="min-w-0">
              <p className="text-sm">Orçamento atual</p>
              <p className="text-xs text-muted-foreground">
                {completo.quote.nights.length} noites · limpeza {formatarBRL(completo.quote.cleaning_cents)}
              </p>
            </div>
            <p className="font-mono text-lg tabular-nums">{formatarBRL(completo.quote.total_cents)}</p>
          </div>
        ) : (
          <p className="mt-2 text-xs text-muted-foreground">
            Sem orçamento válido. <strong>Para marcar como ganha é preciso ter um</strong>: é dele que vem o
            preço da reserva.
          </p>
        )}

        {completo.reservation ? (
          <div className="mt-3 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border/60 bg-card px-3 py-2.5">
            <div className="min-w-0">
              <Link href={`/app/reservas/${completo.reservation.id}`} className="font-mono text-sm hover:underline">
                {completo.reservation.code}
              </Link>
              <p className="text-xs text-muted-foreground">
                {ehEstadoDeReserva(completo.reservation.status)
                  ? ESTADOS[completo.reservation.status].rotulo
                  : completo.reservation.status}
                {completo.reservation.hold_expires_at
                  ? ` · expira em ${formatarInstante(completo.reservation.hold_expires_at)}`
                  : ""}
              </p>
            </div>
            <p className="font-mono text-sm tabular-nums">{formatarBRL(completo.reservation.total_cents)}</p>
          </div>
        ) : null}
      </section>

      {completo.opportunity.event ? (
        <section className="rounded-xl border border-border/60 bg-muted/20 p-4">
          <h3 className="font-display text-sm">Evento</h3>
          <dl className="mt-3 grid gap-2 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-xs text-muted-foreground">Tipo</dt>
              <dd>{completo.opportunity.event.event_type ?? "—"}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">Convidados previstos</dt>
              <dd className="tabular-nums">{completo.opportunity.event.guests_expected ?? "—"}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">Buffet</dt>
              <dd>{completo.opportunity.event.needs_catering ? "Necessário" : "Não"}</dd>
            </div>
          </dl>
          {completo.opportunity.event.notes ? (
            <p className="mt-2 text-sm text-muted-foreground">{completo.opportunity.event.notes}</p>
          ) : null}
        </section>
      ) : null}
    </div>
  );
}

// ─────────────────────────────── Atividades ────────────────────────────────

function Atividades({
  atividades,
  oportunidadeId,
  podeCriar,
  podeConcluir,
  aoNovaTarefa,
}: {
  atividades: Atividade[];
  oportunidadeId: string;
  podeCriar: boolean;
  podeConcluir: boolean;
  aoNovaTarefa: () => void;
}) {
  return (
    <div className="flex flex-col gap-3">
      {podeCriar ? (
        <div className="flex justify-end">
          <Button size="sm" variant="outline" onClick={aoNovaTarefa}>
            <Plus aria-hidden="true" />
            Nova tarefa
          </Button>
        </div>
      ) : null}

      {atividades.length === 0 ? (
        <EstadoVazio
          titulo="Nenhuma atividade ainda"
          descricao="Tarefas, ligações, reuniões, e-mails e WhatsApp aparecem aqui, na ordem do prazo. Algumas etapas criam uma tarefa sozinhas quando o negócio entra nelas."
        />
      ) : (
        <ul className="flex flex-col gap-2">
          {atividades.map((atividade) => (
            <LinhaDeAtividade
              key={atividade.id}
              atividade={atividade}
              oportunidadeId={oportunidadeId}
              podeConcluir={podeConcluir}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

function LinhaDeAtividade({
  atividade,
  oportunidadeId,
  podeConcluir,
}: {
  atividade: Atividade;
  oportunidadeId: string;
  podeConcluir: boolean;
}) {
  const router = useRouter();
  const [ocupado, setOcupado] = React.useState(false);

  async function concluir() {
    setOcupado(true);
    try {
      const resultado = await concluirAtividade(atividade.id, oportunidadeId);
      if (!resultado.ok) {
        notificar(resultado);
        return;
      }
      notificarSucesso("Tarefa concluída");
      router.refresh();
    } finally {
      setOcupado(false);
    }
  }

  return (
    <li
      className={cn(
        "flex flex-wrap items-center justify-between gap-3 rounded-xl border px-3 py-2.5",
        atividade.overdue ? "border-destructive/30 bg-destructive/8" : "border-border/60 bg-muted/20",
        atividade.status !== "pendente" && "opacity-70",
      )}
    >
      <div className="min-w-0">
        <p className="truncate text-sm font-medium">{atividade.subject}</p>
        <p className="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
          <span className="capitalize">{atividade.type}</span>
          {atividade.due_at ? <span>· {formatarInstante(atividade.due_at)}</span> : null}
          {atividade.owner_name ? <span>· {atividade.owner_name}</span> : null}
          {atividade.auto ? <span>· automática</span> : null}
          {atividade.status !== "pendente" ? <span>· {atividade.status}</span> : null}
        </p>
      </div>
      {podeConcluir && atividade.status === "pendente" ? (
        <Button size="sm" variant="ghost" onClick={() => void concluir()} disabled={ocupado}>
          {ocupado ? <Loader2 className="animate-spin" aria-hidden="true" /> : <CheckCircle2 aria-hidden="true" />}
          Concluir
        </Button>
      ) : null}
    </li>
  );
}

/**
 * Nova tarefa em `ModalShell` com zod, como manda a casa.
 *
 * Data e hora em **campos separados**: `datetime-local` é anti-padrão declarado
 * (docs/ui.md §10), e juntar os dois no fuso do navegador faria a tarefa das 9h
 * nascer às 5h para quem abrisse a tela de outro país.
 */
function ModalDeTarefa({
  controle,
  oportunidadeId,
}: {
  controle: React.RefObject<import("@/components/layout/controle-de-modal").ControleDeModal<null> | null>;
  oportunidadeId: string;
}) {
  const router = useRouter();
  const [aberto, setAberto] = React.useState(false);
  const [enviando, setEnviando] = React.useState(false);
  const [valores, setValores] = React.useState<TarefaFormulario>({
    type: "tarefa",
    subject: "",
    due_date: "",
    due_time: "",
    priority: "normal",
  });
  const [erros, setErros] = React.useState<Record<string, string>>({});

  React.useImperativeHandle(controle, () => ({
    abrir() {
      setValores({ type: "tarefa", subject: "", due_date: "", due_time: "", priority: "normal" });
      setErros({});
      setAberto(true);
    },
  }), []);

  async function enviar(evento: React.FormEvent) {
    evento.preventDefault();
    const analise = TarefaFormulario.safeParse(valores);
    if (!analise.success) {
      const detalhes: Record<string, string> = {};
      for (const problema of analise.error.issues) {
        const chave = problema.path.join(".");
        if (chave && !(chave in detalhes)) detalhes[chave] = problema.message;
      }
      setErros(detalhes);
      return;
    }

    setEnviando(true);
    try {
      const resultado = await criarAtividade(oportunidadeId, {
        type: analise.data.type,
        subject: analise.data.subject,
        priority: analise.data.priority,
        due_at: analise.data.due_date ? instanteDaOperacao(analise.data.due_date, analise.data.due_time) : null,
      });
      if (!resultado.ok) {
        notificar(resultado);
        return;
      }
      notificarSucesso("Tarefa criada");
      setAberto(false);
      router.refresh();
    } finally {
      setEnviando(false);
    }
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title="Nova tarefa"
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={enviando}>
            Cancelar
          </Button>
          <Button type="submit" form="form-tarefa" disabled={enviando}>
            {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
            Criar
          </Button>
        </>
      }
    >
      <form id="form-tarefa" onSubmit={enviar} className="flex flex-col gap-4">
        <Campo id="tarefa-assunto" label="Assunto" obrigatorio erro={erros.subject}>
          {(props) => (
            <Input
              {...props}
              value={valores.subject}
              onChange={(e) => setValores((v) => ({ ...v, subject: e.target.value }))}
              placeholder="Ligar para confirmar a proposta"
            />
          )}
        </Campo>

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="tarefa-tipo" label="Tipo">
            {(props) => (
              <Select
                {...props}
                value={valores.type}
                onChange={(e) => setValores((v) => ({ ...v, type: e.target.value as TarefaFormulario["type"] }))}
              >
                <option value="tarefa">Tarefa</option>
                <option value="ligacao">Ligação</option>
                <option value="reuniao">Reunião</option>
                <option value="email">E-mail</option>
                <option value="whatsapp">WhatsApp</option>
              </Select>
            )}
          </Campo>
          <Campo id="tarefa-prioridade" label="Prioridade">
            {(props) => (
              <Select
                {...props}
                value={valores.priority}
                onChange={(e) => setValores((v) => ({ ...v, priority: e.target.value as TarefaFormulario["priority"] }))}
              >
                <option value="baixa">Baixa</option>
                <option value="normal">Normal</option>
                <option value="alta">Alta</option>
              </Select>
            )}
          </Campo>
          <Campo id="tarefa-data" label="Data do prazo" erro={erros.due_date}>
            {(props) => (
              <Input
                {...props}
                type="date"
                value={valores.due_date}
                onChange={(e) => setValores((v) => ({ ...v, due_date: e.target.value }))}
                className="tabular-nums"
              />
            )}
          </Campo>
          <Campo id="tarefa-hora" label="Hora" erro={erros.due_time} hint="Em branco, usa 09:00 (horário de Fortaleza).">
            {(props) => (
              <Input
                {...props}
                type="time"
                value={valores.due_time}
                onChange={(e) => setValores((v) => ({ ...v, due_time: e.target.value }))}
                className="tabular-nums"
              />
            )}
          </Campo>
        </div>
      </form>
    </ModalShell>
  );
}

// ────────────────────────────────── Notas ──────────────────────────────────

/**
 * Nota é **atividade de tipo `nota`** — não há tabela nem CRUD próprio. Assim
 * ela entra na linha do tempo pelo mesmo caminho de todo o resto, em vez de
 * exigir uma segunda leitura costurada na hora de montar o histórico.
 */
function Notas({
  notas,
  oportunidadeId,
  podeCriar,
}: {
  notas: OportunidadeCompleta["notes"];
  oportunidadeId: string;
  podeCriar: boolean;
}) {
  const router = useRouter();
  const [texto, setTexto] = React.useState("");
  const [erro, setErro] = React.useState<string | null>(null);
  const [enviando, setEnviando] = React.useState(false);

  async function enviar(evento: React.FormEvent) {
    evento.preventDefault();
    const analise = NotaFormulario.safeParse({ body: texto });
    if (!analise.success) {
      setErro(analise.error.issues[0]?.message ?? "Escreva a nota.");
      return;
    }
    setErro(null);
    setEnviando(true);
    try {
      const resultado = await criarAtividade(oportunidadeId, {
        type: "nota",
        // O contrato guarda o corpo em `description`; `subject` é o rótulo que
        // aparece na linha do tempo unificada.
        subject: "Nota",
        description: analise.data.body,
      });
      if (!resultado.ok) {
        notificar(resultado);
        return;
      }
      setTexto("");
      router.refresh();
    } finally {
      setEnviando(false);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      {podeCriar ? (
        <form onSubmit={enviar} className="flex flex-col gap-2">
          <Textarea
            aria-label="Nova nota"
            value={texto}
            invalid={Boolean(erro)}
            onChange={(evento) => setTexto(evento.target.value)}
            placeholder="O que foi combinado com o hóspede."
          />
          {erro ? (
            <p role="alert" className="text-xs font-medium text-destructive">
              {erro}
            </p>
          ) : null}
          <div className="flex justify-end">
            <Button type="submit" size="sm" disabled={enviando}>
              {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
              Registrar nota
            </Button>
          </div>
        </form>
      ) : null}

      {notas.length === 0 ? (
        <p className="text-sm text-muted-foreground">Nenhuma nota registrada.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {notas.map((nota) => (
            <li key={nota.id} className="rounded-xl border border-border/60 bg-muted/20 px-3 py-2.5">
              <p className="whitespace-pre-wrap text-sm">{nota.body}</p>
              <p className="mt-1 text-xs text-muted-foreground">
                {nota.author_name ?? "Sistema"} · {formatarInstante(nota.created_at)}
              </p>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// ──────────────────────────────── Documentos ───────────────────────────────

function Documentos() {
  return (
    <EstadoVazio
      icone={FileText}
      titulo="Anexos ainda não estão disponíveis"
      descricao="Em breve você poderá guardar aqui documentos e arquivos do negócio."
    />
  );
}

// ──────────────────────────────── Histórico ────────────────────────────────

function Historico({ completo }: { completo: OportunidadeCompleta }) {
  return (
    <div className="flex flex-col gap-6">
      <section>
        <h3 className="font-display text-sm">Linha do tempo</h3>
        <p className="mt-1 text-xs text-muted-foreground">
          Mudanças de etapa, atividades, notas, orçamentos e reservas, em ordem de data — para ver
          &ldquo;o que já foi falado com essa pessoa&rdquo; sem trocar de tela.
        </p>
        {completo.timeline.length === 0 ? (
          <p className="mt-3 text-sm text-muted-foreground">Nada registrado ainda.</p>
        ) : (
          <ol className="mt-3 flex flex-col gap-2">
            {completo.timeline.map((evento) => (
              <li key={evento.id} className="flex items-start gap-3 rounded-lg border border-border/60 bg-muted/20 px-3 py-2">
                <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-primary" aria-hidden="true" />
                <div className="min-w-0">
                  {/* O texto vem montado do servidor: remontar a frase aqui
                      criaria uma segunda redação dos mesmos fatos. */}
                  <p className="text-sm">{evento.title}</p>
                  <p className="text-xs text-muted-foreground">
                    {formatarInstante(evento.at)}
                    {evento.actor_name ? ` · ${evento.actor_name}` : ""}
                  </p>
                </div>
              </li>
            ))}
          </ol>
        )}
      </section>

      <section>
        <h3 className="font-display text-sm">Passagem por etapas</h3>
        <p className="mt-1 text-xs text-muted-foreground">
          Por onde o negócio passou. É daqui que saem os números de conversão do funil, por isso nada aqui
          pode ser editado nem apagado.
        </p>
        {completo.stage_history.length === 0 ? (
          <p className="mt-3 text-sm text-muted-foreground">Sem movimentações registradas.</p>
        ) : (
          <ol className="mt-3 flex flex-col gap-2">
            {completo.stage_history.map((evento) => (
              <li key={evento.id} className="rounded-lg border border-border/60 bg-muted/20 px-3 py-2 text-sm">
                <p className="flex flex-wrap items-center gap-1.5">
                  <CalendarRange className="size-3.5 text-muted-foreground" aria-hidden="true" />
                  {evento.from_stage_name ? `${evento.from_stage_name} → ` : "Criada em "}
                  <strong className="font-medium">{evento.to_stage_name}</strong>
                  {evento.days_in_stage !== null ? (
                    <span className="font-mono text-xs tabular-nums text-muted-foreground">
                      ({evento.days_in_stage.toFixed(1)} dias na anterior)
                    </span>
                  ) : null}
                </p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  {formatarInstante(evento.at)}
                  {evento.user_name ? ` · ${evento.user_name}` : " · automático"}
                  {evento.reason ? ` · ${evento.reason}` : ""}
                </p>
              </li>
            ))}
          </ol>
        )}
      </section>
    </div>
  );
}
