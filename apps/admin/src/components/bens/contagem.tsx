"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowRight, CheckCircle2, ClipboardCheck, Loader2, XCircle } from "lucide-react";

import { notificarFalha, notificarSucesso } from "@/components/bens/avisos";
import { ConfirmacaoDoInventario } from "@/components/bens/confirmacao";
import { AvisoGeral } from "@/components/bens/formulario-comum";
import { LinhaDaContagem } from "@/components/bens/linha-da-contagem";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Textarea } from "@/components/ui/textarea";
import { cancelarConferencia, contarLinha, fecharConferencia } from "@/lib/bens/acoes";
import {
  notaDoFechamento,
  pendenciasPorAmbiente,
  pendentesDoAmbiente,
  proximoComPendencia,
  type PendenciaDoAmbiente,
} from "@/lib/bens/contagem";
import { mensagemDeBens } from "@/lib/bens/mensagens";
import type {
  AmbienteDaConferencia,
  ApuracaoDaConferencia,
  ConferenciaCompleta,
  ProgressoDaConferencia,
  ResultadoDoFechamento,
} from "@/lib/bens/tipos";
import { formatarBRL } from "@/lib/dinheiro";
import { cn } from "@/lib/utils";

/** Tempo que o contador espera o dedo parar antes de gravar. Três toques
 *  seguidos no `+` viram **uma** chamada com o número final, e não três que
 *  podem chegar fora de ordem. */
const ESPERA_MS = 350;

type Contagens = Record<string, number | null>;

function contagensDe(conferencia: ConferenciaCompleta): Contagens {
  const mapa: Contagens = {};
  for (const ambiente of conferencia.rooms) for (const l of ambiente.lines) mapa[l.id] = l.counted_qty;
  return mapa;
}

/**
 * A conferência no celular — quem conta é quem limpa, dentro do apartamento.
 *
 * ## Um ambiente por vez
 *
 * As abas de cima são os cômodos na ordem de caminhada, cada um com quantos
 * itens faltam contar. Termina a cozinha, o botão do fim leva ao próximo
 * cômodo que ainda tem pendência.
 *
 * ## Otimista, com volta
 *
 * O número muda na tela no toque; a gravação sai depois que o dedo para
 * (`ESPERA_MS`) e a resposta traz a linha e o progresso. Se a API recusar, a
 * linha volta ao último valor gravado e o toast diz por quê.
 * `COUNT_CLOSED` (alguém fechou ou cancelou enquanto se contava) recarrega a
 * tela, que passa a mostrar a conferência encerrada.
 *
 * Antes de fechar, tudo que estiver esperando para gravar é gravado — senão o
 * último toque, feito meio segundo antes de "Fechar", ficaria de fora.
 */
export function TelaDeContagem({
  conferencia,
  permissoes,
}: {
  conferencia: ConferenciaCompleta;
  permissoes: { editar: boolean; excluir: boolean };
}) {
  const router = useRouter();
  const aberta = conferencia.status === "aberta";
  const podeContar = aberta && permissoes.editar;

  // Estado local das contagens, reposto quando o servidor manda outra versão
  // da conferência (revalidação) — no render, como no resto do painel.
  const [base, setBase] = React.useState(conferencia);
  const [contagens, setContagens] = React.useState<Contagens>(() => contagensDe(conferencia));
  const [progresso, setProgresso] = React.useState<ProgressoDaConferencia>(conferencia.progress);
  if (conferencia !== base) {
    setBase(conferencia);
    setContagens(contagensDe(conferencia));
    setProgresso(conferencia.progress);
  }

  const ambientes: AmbienteDaConferencia[] = React.useMemo(
    () =>
      conferencia.rooms.map((a) => ({
        ...a,
        lines: a.lines.map((l) => (l.id in contagens ? { ...l, counted_qty: contagens[l.id] } : l)),
      })),
    [conferencia.rooms, contagens],
  );

  const [ativo, setAtivo] = React.useState<string>(
    () => conferencia.rooms.find((a) => a.lines.some((l) => l.counted_qty === null))?.room_id ?? conferencia.rooms[0]?.room_id ?? "",
  );
  const ambiente = ambientes.find((a) => a.room_id === ativo) ?? ambientes[0];

  const [soPendentes, setSoPendentes] = React.useState(false);
  const [tocadas, setTocadas] = React.useState<Set<string>>(() => new Set());
  const [gravando, setGravando] = React.useState(0);

  // ── Gravação ──────────────────────────────────────────────────────────────
  const confirmadas = React.useRef<Contagens>({});
  React.useEffect(() => {
    // O último valor que o servidor confirmou, por linha — é para ele que a
    // linha volta quando uma gravação é recusada.
    confirmadas.current = contagensDe(conferencia);
  }, [conferencia]);
  const esperas = React.useRef(new Map<string, { timer: ReturnType<typeof setTimeout>; valor: number | null }>());
  const versoes = React.useRef(new Map<string, number>());
  const emVoo = React.useRef(new Set<Promise<void>>());
  const ordemDoProgresso = React.useRef({ emitida: 0, aplicada: 0 });

  const gravar = React.useCallback(
    (lineId: string, valor: number | null) => {
      const versao = (versoes.current.get(lineId) ?? 0) + 1;
      versoes.current.set(lineId, versao);
      const ordem = ++ordemDoProgresso.current.emitida;
      setGravando((n) => n + 1);

      const tarefa = (async () => {
        const r = await contarLinha(conferencia.id, lineId, valor);
        setGravando((n) => n - 1);
        if (versoes.current.get(lineId) !== versao) return; // um toque mais novo já saiu
        const outroToqueEsperando = esperas.current.has(lineId);
        if (r.ok) {
          confirmadas.current[lineId] = r.data.line.counted_qty;
          if (!outroToqueEsperando) setContagens((c) => ({ ...c, [lineId]: r.data.line.counted_qty }));
          if (ordem > ordemDoProgresso.current.aplicada) {
            ordemDoProgresso.current.aplicada = ordem;
            setProgresso(r.data.progress);
          }
          return;
        }
        if (!outroToqueEsperando) setContagens((c) => ({ ...c, [lineId]: confirmadas.current[lineId] ?? null }));
        notificarFalha("A contagem não foi salva", r, "conferencia");
        if (r.code === "COUNT_CLOSED") router.refresh();
      })();

      emVoo.current.add(tarefa);
      void tarefa.finally(() => emVoo.current.delete(tarefa));
    },
    [conferencia.id, router],
  );

  const mudar = React.useCallback(
    (lineId: string, valor: number | null) => {
      setContagens((c) => ({ ...c, [lineId]: valor }));
      setTocadas((t) => (t.has(lineId) ? t : new Set(t).add(lineId)));
      const anterior = esperas.current.get(lineId);
      if (anterior) clearTimeout(anterior.timer);
      const timer = setTimeout(() => {
        esperas.current.delete(lineId);
        gravar(lineId, valor);
      }, ESPERA_MS);
      esperas.current.set(lineId, { timer, valor });
    },
    [gravar],
  );

  /** Grava já o que está esperando e aguarda tudo que está em voo. */
  const descarregar = React.useCallback(async () => {
    for (const [lineId, { timer, valor }] of esperas.current) {
      clearTimeout(timer);
      esperas.current.delete(lineId);
      gravar(lineId, valor);
    }
    await Promise.allSettled([...emVoo.current]);
  }, [gravar]);

  // Sair da tela (voltar, trocar de aba do painel) não pode perder o último toque.
  React.useEffect(() => {
    const pendentes = esperas.current;
    return () => {
      for (const [lineId, { timer, valor }] of pendentes) {
        clearTimeout(timer);
        void contarLinha(conferencia.id, lineId, valor);
      }
      pendentes.clear();
    };
  }, [conferencia.id]);

  // ── Fechar / cancelar ─────────────────────────────────────────────────────
  const [fechando, setFechando] = React.useState(false);
  const [cancelando, setCancelando] = React.useState(false);
  // O resultado vem do `GET` (`result`, com o custo congelado no fechamento) —
  // é ele que sobrevive a recarregar a página. A resposta do `POST /close` só
  // cobre o instante entre fechar e a tela recarregar.
  const [recemFechada, setRecemFechada] = React.useState<ResultadoDoFechamento | null>(null);
  const apuracao: ApuracaoDaConferencia | null =
    conferencia.status === "fechada" ? (conferencia.result ?? recemFechada) : recemFechada;

  const linhasVisiveis = ambiente
    ? soPendentes
      ? ambiente.lines.filter((l) => l.counted_qty === null || tocadas.has(l.id))
      : ambiente.lines
    : [];
  const proximo = ambiente ? proximoComPendencia(ambientes, ambiente.room_id) : null;
  const nomeDoProximo = proximo ? ambientes.find((a) => a.room_id === proximo)?.room_name : null;

  function irPara(roomId: string) {
    setAtivo(roomId);
    setTocadas(new Set());
    if (typeof window !== "undefined") window.scrollTo({ top: 0, behavior: "smooth" });
  }

  return (
    <div className="flex flex-col gap-4">
      {apuracao ? (
        <ApuracaoView apuracao={apuracao} countId={conferencia.id} aoIrPara={irPara} />
      ) : null}

      {!aberta && !apuracao ? (
        <Nota variante={conferencia.status === "cancelada" ? "atencao" : "info"}>
          {conferencia.status === "cancelada"
            ? "Esta conferência foi cancelada. O que foi contado até ali ficou registrado, mas ela não gerou divergência nem avaria."
            : "Esta conferência está fechada, mas o resultado da apuração não veio. As linhas abaixo mostram o que se esperava e o que foi contado."}
        </Nota>
      ) : null}

      {/* Os cômodos, presos logo abaixo da barra do painel. */}
      <nav
        aria-label="Ambientes da conferência"
        className="glass sticky top-[var(--app-chrome-top)] z-30 -mx-1 overflow-x-auto rounded-2xl border border-border/50 px-1 py-1.5"
      >
        <ul className="flex min-w-max gap-1.5">
          {ambientes.map((a) => {
            const faltam = pendentesDoAmbiente(a);
            const atual = a.room_id === ambiente?.room_id;
            return (
              <li key={a.room_id}>
                <button
                  type="button"
                  onClick={() => irPara(a.room_id)}
                  aria-current={atual ? "true" : undefined}
                  className={cn(
                    "inline-flex h-10 items-center gap-2 rounded-full px-3.5 text-sm outline-none transition-colors",
                    "focus-visible:ring-2 focus-visible:ring-ring",
                    atual ? "bg-brand-gradient text-white" : "bg-card text-foreground hover:bg-muted",
                  )}
                >
                  {a.room_name}
                  {faltam > 0 ? (
                    <span
                      className={cn(
                        "rounded-full px-1.5 text-[0.7rem] tabular-nums",
                        atual ? "bg-white/20" : "border border-dashed border-foreground/30 text-muted-foreground",
                      )}
                      aria-label={`${faltam} sem contar`}
                    >
                      {faltam}
                    </span>
                  ) : (
                    <CheckCircle2 className={cn("size-4", atual ? "text-white" : "text-alcada-livre")} aria-label="tudo contado" />
                  )}
                </button>
              </li>
            );
          })}
        </ul>
      </nav>

      {ambiente ? (
        <section aria-label={ambiente.room_name} className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h2 className="font-display text-lg">{ambiente.room_name}</h2>
            {podeContar ? (
              <CheckboxCampo
                id="contagem-so-pendentes"
                label="Só o que falta contar"
                checked={soPendentes}
                onChange={(e) => {
                  setSoPendentes(e.target.checked);
                  setTocadas(new Set());
                }}
              />
            ) : null}
          </div>

          {linhasVisiveis.length === 0 ? (
            <p className="rounded-xl border border-border/60 bg-muted/20 px-4 py-6 text-center text-sm text-muted-foreground">
              Tudo contado neste ambiente.
            </p>
          ) : (
            <ul className="flex flex-col gap-2.5">
              {linhasVisiveis.map((l) => (
                <li key={l.id}>
                  <LinhaDaContagem linha={l} podeContar={podeContar} aoMudar={(v) => mudar(l.id, v)} />
                </li>
              ))}
            </ul>
          )}

          {podeContar && proximo && nomeDoProximo ? (
            <Button variant="outline" size="lg" className="self-stretch sm:self-start" onClick={() => irPara(proximo)}>
              Próximo: {nomeDoProximo}
              <ArrowRight aria-hidden="true" />
            </Button>
          ) : null}
        </section>
      ) : null}

      {aberta ? (
        <Rodape
          progresso={progresso}
          gravando={gravando > 0}
          podeFechar={permissoes.editar}
          podeCancelar={permissoes.excluir}
          aoFechar={() => setFechando(true)}
          aoCancelar={() => setCancelando(true)}
        />
      ) : null}

      <ModalDeFechamento
        aberto={fechando}
        aoMudar={setFechando}
        countId={conferencia.id}
        notaAtual={conferencia.note ?? null}
        pendentes={progresso.pending}
        antesDeFechar={descarregar}
        aoIrPara={(roomId) => {
          setFechando(false);
          irPara(roomId);
        }}
        aoFechar={(r) => {
          setRecemFechada(r);
          notificarSucesso(
            "Conferência fechada",
            r.issues_created > 0 ? `${r.issues_created} avaria(s) aberta(s) para as faltas.` : "Nenhuma avaria aberta.",
          );
          router.refresh();
        }}
      />

      <ConfirmacaoDoInventario
        aberto={cancelando}
        aoMudar={setCancelando}
        contexto="conferencia"
        destrutivo
        titulo="Cancelar esta conferência?"
        rotuloConfirmar="Cancelar conferência"
        descricao={
          <>
            Cancelar não apaga: o que já foi contado fica registrado, mas a conferência não apura divergência nem abre
            avaria. É o caminho para quem não vai terminar — e libera a unidade para uma contagem nova.
          </>
        }
        aoConfirmar={async () => {
          await descarregar();
          const r = await cancelarConferencia(conferencia.id);
          if (r.ok) {
            notificarSucesso("Conferência cancelada");
            router.push(`/app/inventario?unidade=${conferencia.unit_id}`);
          }
          return r;
        }}
      />
    </div>
  );
}

function Rodape({
  progresso,
  gravando,
  podeFechar,
  podeCancelar,
  aoFechar,
  aoCancelar,
}: {
  progresso: ProgressoDaConferencia;
  gravando: boolean;
  podeFechar: boolean;
  podeCancelar: boolean;
  aoFechar: () => void;
  aoCancelar: () => void;
}) {
  const fracao = progresso.lines > 0 ? progresso.counted / progresso.lines : 0;
  return (
    <div
      className={cn(
        "glass sticky z-30 flex flex-col gap-2 rounded-2xl border border-border/50 px-3 py-2.5",
        // Acima da barra de baixo do celular; no desktop, rente ao fim da janela.
        "bottom-[calc(var(--mobile-nav-height)+env(safe-area-inset-bottom)+0.5rem)] md:bottom-3",
      )}
    >
      <div
        role="progressbar"
        aria-label="Itens contados"
        aria-valuemin={0}
        aria-valuemax={progresso.lines}
        aria-valuenow={progresso.counted}
        className="h-1.5 w-full overflow-hidden rounded-full bg-muted"
      >
        <div className="h-full bg-brand-gradient transition-[width]" style={{ width: `${Math.round(fracao * 100)}%` }} />
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground" aria-live="polite">
          <strong className="tabular-nums text-foreground">{progresso.counted}</strong> de{" "}
          <span className="tabular-nums">{progresso.lines}</span> contados ·{" "}
          <span className={cn("tabular-nums", progresso.pending > 0 && "font-medium text-foreground")}>{progresso.pending}</span>{" "}
          sem contar · <span className="tabular-nums">{progresso.diverging}</span>{" "}
          {progresso.diverging === 1 ? "divergência" : "divergências"}
          {gravando ? (
            <span className="ml-2 inline-flex items-center gap-1">
              <Loader2 className="size-3 animate-spin" aria-hidden="true" />
              salvando
            </span>
          ) : null}
        </p>
        <div className="flex gap-2">
          {podeCancelar ? (
            <Button size="sm" variant="ghost" onClick={aoCancelar}>
              <XCircle aria-hidden="true" />
              Cancelar
            </Button>
          ) : null}
          {podeFechar ? (
            <Button size="sm" onClick={aoFechar}>
              <ClipboardCheck aria-hidden="true" />
              Fechar conferência
            </Button>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function ModalDeFechamento({
  aberto,
  aoMudar,
  countId,
  notaAtual,
  pendentes,
  antesDeFechar,
  aoIrPara,
  aoFechar,
}: {
  aberto: boolean;
  aoMudar: (aberto: boolean) => void;
  countId: string;
  /** A observação que a conferência já tem — o campo abre com ela. */
  notaAtual: string | null;
  pendentes: number;
  antesDeFechar: () => Promise<void>;
  aoIrPara: (roomId: string) => void;
  aoFechar: (r: ResultadoDoFechamento) => void;
}) {
  const [abrirAvarias, setAbrirAvarias] = React.useState(true);
  const [nota, setNota] = React.useState(notaAtual ?? "");
  const [enviando, setEnviando] = React.useState(false);
  const [erro, setErro] = React.useState<string | null>(null);
  const [faltam, setFaltam] = React.useState<PendenciaDoAmbiente[]>([]);

  const [abertoAntes, setAbertoAntes] = React.useState(aberto);
  if (aberto !== abertoAntes) {
    setAbertoAntes(aberto);
    if (aberto) {
      setAbrirAvarias(true);
      setNota(notaAtual ?? "");
      setErro(null);
      setFaltam([]);
    }
  }

  async function fechar() {
    setEnviando(true);
    setErro(null);
    setFaltam([]);
    try {
      await antesDeFechar();
      const r = await fecharConferencia(countId, abrirAvarias, notaDoFechamento(nota, notaAtual));
      if (r.ok) {
        aoMudar(false);
        aoFechar(r.data);
        return;
      }
      setErro(mensagemDeBens(r, "conferencia"));
      if (r.code === "COUNT_HAS_PENDING_LINES") setFaltam(pendenciasPorAmbiente(r.details.pending_by_room));
    } finally {
      setEnviando(false);
    }
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={aoMudar}
      title="Fechar a conferência"
      description="Compara o que se esperava com o que foi contado, item a item."
      footer={
        <>
          <Button variant="ghost" onClick={() => aoMudar(false)} disabled={enviando}>
            Voltar a contar
          </Button>
          <Button onClick={fechar} disabled={enviando}>
            {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <ClipboardCheck aria-hidden="true" />}
            Fechar e apurar
          </Button>
        </>
      }
    >
      <AvisoGeral mensagem={erro} />
      {faltam.length > 0 ? (
        <ul className="-mt-2 mb-4 flex flex-col gap-1 text-sm" aria-label="Onde ainda falta contar">
          {faltam.map((f) => (
            <li key={f.roomId} className="flex items-center justify-between gap-2">
              <span>
                {f.ambiente}: <span className="tabular-nums">{f.pendentes}</span> sem contar
              </span>
              <Button size="sm" variant="link" onClick={() => aoIrPara(f.roomId)}>
                Ir contar
              </Button>
            </li>
          ))}
        </ul>
      ) : null}

      <div className="flex flex-col gap-4 text-sm">
        {pendentes > 0 ? (
          <Nota variante="atencao">
            Ainda há <strong className="tabular-nums">{pendentes}</strong> {pendentes === 1 ? "item" : "itens"} sem
            contagem. Para fechar é preciso contar todos — “não contei” e “não achei” não podem virar a mesma coisa. Se
            não vai terminar, cancele a conferência.
          </Nota>
        ) : null}
        <CheckboxCampo
          id="fechamento-avarias"
          label="Abrir uma avaria para cada falta"
          hint="Cada item contado abaixo do esperado vira uma pendência “faltando”, ligada a esta conferência. Sobra não abre nada — é correção de cadastro. Desmarque só para contagem de aferição."
          checked={abrirAvarias}
          onChange={(e) => setAbrirAvarias(e.target.checked)}
        />
        <Campo
          id="fechamento-nota"
          label="Observação da conferência"
          hint={notaAtual ? "Já vem com a observação da abertura. Mexa só se quiser trocá-la; apagar tudo limpa." : "Opcional."}
        >
          {(p) => <Textarea {...p} rows={2} value={nota} onChange={(e) => setNota(e.target.value)} maxLength={2000} />}
        </Campo>
      </div>
    </ModalShell>
  );
}

/**
 * A apuração: divergência item a item e o prejuízo de cada falta, em centavos
 * do **custo congelado no fechamento** — o número não muda se o catálogo for
 * recotado depois. A tela só formata; quem multiplica é o servidor.
 */
function ApuracaoView({
  apuracao,
  countId,
  aoIrPara,
}: {
  apuracao: ApuracaoDaConferencia;
  countId: string;
  aoIrPara: (roomId: string) => void;
}) {
  const { divergences, issues_created } = apuracao;
  return (
    <section aria-label="Resultado da conferência" className="flex flex-col gap-3 rounded-xl border border-border/60 bg-muted/20 p-4">
      <h2 className="font-display text-lg">
        {divergences.length === 0
          ? "Tudo confere: a casa está como o cadastro diz."
          : `${divergences.length} ${divergences.length === 1 ? "divergência" : "divergências"}`}
      </h2>
      {divergences.length > 0 ? (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[36rem] text-sm">
            <thead className="text-left text-xs text-muted-foreground">
              <tr>
                <th className="py-1 pr-3 font-medium">Bem</th>
                <th className="py-1 pr-3 font-medium">Ambiente</th>
                <th className="py-1 pr-3 text-right font-medium">Esperado</th>
                <th className="py-1 pr-3 text-right font-medium">Contado</th>
                <th className="py-1 pr-3 text-right font-medium">Diferença</th>
                <th className="py-1 text-right font-medium">Prejuízo</th>
              </tr>
            </thead>
            <tbody>
              {divergences.map((d) => (
                <tr key={`${d.room_id}-${d.item_id}`} className="border-t border-border/50">
                  <td className="py-1.5 pr-3">{d.item_name}</td>
                  <td className="py-1.5 pr-3">
                    <button
                      type="button"
                      onClick={() => aoIrPara(d.room_id)}
                      className="text-muted-foreground underline-offset-4 outline-none hover:text-foreground hover:underline focus-visible:ring-2 focus-visible:ring-ring"
                    >
                      {d.room_name}
                    </button>
                  </td>
                  <td className="py-1.5 pr-3 text-right tabular-nums">{d.expected_qty}</td>
                  <td className="py-1.5 pr-3 text-right tabular-nums">{d.counted_qty}</td>
                  <td className={cn("py-1.5 pr-3 text-right tabular-nums", d.diff < 0 && "text-destructive")}>
                    {d.diff > 0 ? `+${d.diff}` : d.diff}
                  </td>
                  <td className="py-1.5 text-right font-mono tabular-nums">
                    {typeof d.loss_cents === "number" ? formatarBRL(d.loss_cents) : d.diff < 0 ? "sem custo cotado" : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      <p className="text-xs text-muted-foreground">
        O prejuízo usa o custo de reposição de quando a conferência foi fechada; recotar o catálogo depois não muda
        este número.
      </p>
      <p className="text-sm text-muted-foreground">
        {issues_created > 0 ? (
          <>
            {issues_created} {issues_created === 1 ? "avaria foi aberta" : "avarias foram abertas"} para as faltas.{" "}
            <Link href={`/app/inventario/avarias?conferencia=${countId}&situacao=todas`} className="text-primary underline-offset-4 hover:underline">
              Ver as avarias desta conferência
            </Link>
          </>
        ) : (
          "Nenhuma avaria foi aberta. Sobras não abrem avaria: elas indicam que o padrão da casa precisa de ajuste."
        )}
      </p>
    </section>
  );
}
