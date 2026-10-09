"use client";

import * as React from "react";
import Link from "next/link";
import { CheckCircle2, Loader2, RotateCcw, ShieldCheck, Trash2, TriangleAlert, Wrench } from "lucide-react";

import { notificarFalha, notificarSucesso } from "@/components/bens/avisos";
import { ConfirmacaoDoInventario } from "@/components/bens/confirmacao";
import { FotoDoBem } from "@/components/bens/foto";
import { ModalDeAvaria, type AlvoDaAvaria } from "@/components/bens/modal-de-avaria";
import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { EstadoVazio } from "@/components/layout/estados";
import { ModalShell } from "@/components/layout/modal-shell";
import { ModalDeOrdem, type AlvoDaOrdem } from "@/components/manutencao/modal-de-ordem";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Select } from "@/components/ui/select";
import { apagarAvaria, resolverAvaria } from "@/lib/bens/acoes";
import { caminhoDaConferencia, mensagemDeBens } from "@/lib/bens/mensagens";
import { ROTULO_DA_AVARIA, ROTULO_DO_DESFECHO } from "@/lib/bens/rotulos";
import { DESFECHOS_DE_AVARIA, type AmbienteDoInventario, type Avaria, type DesfechoDeAvaria } from "@/lib/bens/tipos";
import { formatarInstante } from "@/lib/datas";
import { formatarBRL } from "@/lib/dinheiro";
import { caminhoDaOrdem } from "@/lib/manutencao/mensagens";
import { cn } from "@/lib/utils";

export type PermissoesDeAvarias = { criar: boolean; editar: boolean; excluir: boolean };

/**
 * A ponte com as ordens de manutenção (spec §12) — **outro recurso**,
 * `maintenance`, lido pela página, não `inventory.goods`:
 *
 * - `podeAbrir` (`maintenance:criar`) acende "Abrir ordem de manutenção";
 * - `podeVerOrdens` (`maintenance:ver`) faz do aviso "ordem em aberto" um
 *   link. Sem ela o aviso continua (o id vem na avaria para quem só vê o
 *   inventário), mas sem levar a uma tela que responderia "sem acesso".
 *
 * Qual avaria tem ordem aberta sai de `Avaria.open_maintenance_order_id`, na
 * própria resposta da lista — sem uma chamada por linha.
 */
export type ManutencaoDasAvarias = { podeAbrir: boolean; podeVerOrdens: boolean };

const SEM_MANUTENCAO: ManutencaoDasAvarias = { podeAbrir: false, podeVerOrdens: false };

/** O título que a ordem nasce sugerindo — editável no modal. */
export function tituloSugerido(a: Pick<Avaria, "kind" | "item_name" | "room_name">): string {
  const bem = a.item_name ?? "bem";
  return a.room_name ? `${ROTULO_DA_AVARIA[a.kind]}: ${bem} (${a.room_name})` : `${ROTULO_DA_AVARIA[a.kind]}: ${bem}`;
}

/** Os quatro ids que a ordem herda da avaria — unidade, cômodo, bem e ela. */
function alvoDaAvaria(a: Avaria): AlvoDaOrdem {
  return {
    avaria: {
      id: a.id,
      unitId: a.unit_id,
      unitCode: a.unit_code,
      roomId: a.room_id,
      roomName: a.room_name,
      itemId: a.item_id,
      itemName: a.item_name,
      tituloSugerido: tituloSugerido(a),
    },
  };
}

/**
 * A lista de pendências: o que quebrou, o que sumiu, o que estragou — e o
 * desfecho de cada um.
 *
 * **Pendência aberta é avaria sem desfecho** (`resolution` nulo). Não existe
 * "status" à parte: dar o desfecho é o que fecha, e tirá-lo (`Reabrir`) é o que
 * se faz quando o prato "reposto" não chegou. O valor (`total_cost_cents`) é
 * calculado no servidor; a tela só formata.
 */
export function ListaDeAvarias({
  avarias,
  permissoes,
  unidade,
  temFiltro,
  manutencao = SEM_MANUTENCAO,
}: {
  avarias: readonly Avaria[];
  permissoes: PermissoesDeAvarias;
  /** A unidade do filtro, com os cômodos e bens — sem ela não há como registrar. */
  unidade: { code: string; ambientes: AmbienteDoInventario[] } | null;
  temFiltro: boolean;
  manutencao?: ManutencaoDasAvarias;
}) {
  const registrar = useControleDeModal<AlvoDaAvaria>();
  const ordem = useControleDeModal<AlvoDaOrdem>();
  const [aResolver, setAResolver] = React.useState<Avaria | null>(null);
  const [aApagar, setAApagar] = React.useState<Avaria | null>(null);

  async function reabrir(avaria: Avaria) {
    const r = await resolverAvaria(avaria.id, null);
    if (!r.ok) notificarFalha("Não foi possível reabrir", r, "avaria");
    else notificarSucesso("Pendência reaberta");
  }

  return (
    <>
      {permissoes.criar ? (
        <div className="flex flex-wrap items-center gap-3">
          <Button size="sm" onClick={() => registrar.abrir({})} disabled={!unidade || unidade.ambientes.length === 0}>
            <TriangleAlert aria-hidden="true" />
            Registrar avaria
          </Button>
          {!unidade ? <span className="text-xs text-muted-foreground">Escolha a unidade no filtro para registrar.</span> : null}
        </div>
      ) : null}

      {avarias.length === 0 ? (
        <EstadoVazio
          icone={ShieldCheck}
          titulo={temFiltro ? "Nada com esse recorte" : "Nenhuma pendência em aberto"}
          descricao={
            temFiltro ? (
              <>Troque o filtro de situação para ver as já resolvidas.</>
            ) : (
              <>
                Avarias nascem de duas formas: registradas aqui (o prato que quebrou no jantar) ou abertas sozinhas ao
                fechar uma conferência com falta.
              </>
            )
          }
        />
      ) : (
        <ul className="flex flex-col gap-2">
          {avarias.map((a) => (
            <li key={a.id}>
              <CartaoDaAvaria
                avaria={a}
                permissoes={permissoes}
                aoResolver={() => setAResolver(a)}
                aoReabrir={() => void reabrir(a)}
                aoApagar={() => setAApagar(a)}
                podeVerOrdem={manutencao.podeVerOrdens}
                aoAbrirOrdem={manutencao.podeAbrir ? (alvo) => ordem.abrir(alvo) : null}
              />
            </li>
          ))}
        </ul>
      )}

      {unidade ? (
        <ModalDeAvaria controle={registrar.ref} ambientes={unidade.ambientes} unidadeRotulo={unidade.code} />
      ) : null}

      {manutencao.podeAbrir ? <ModalDeOrdem controle={ordem.ref} unidades={[]} irParaOrdem={false} /> : null}

      <ModalDeDesfecho avaria={aResolver} aoFechar={() => setAResolver(null)} />

      <ConfirmacaoDoInventario
        aberto={aApagar !== null}
        aoMudar={(a) => !a && setAApagar(null)}
        contexto="avaria"
        destrutivo
        titulo="Apagar este registro?"
        rotuloConfirmar="Apagar registro"
        descricao={
          <>
            Apagar é para o registro feito por engano. <strong>Resolver não é apagar</strong>: avaria atendida se fecha com
            o desfecho, e continua somando no histórico do bem.
          </>
        }
        aoConfirmar={() => apagarAvaria(aApagar!.id)}
      />
    </>
  );
}

function CartaoDaAvaria({
  avaria: a,
  permissoes,
  aoResolver,
  aoReabrir,
  aoApagar,
  podeVerOrdem,
  aoAbrirOrdem,
}: {
  avaria: Avaria;
  permissoes: PermissoesDeAvarias;
  aoResolver: () => void;
  aoReabrir: () => void;
  aoApagar: () => void;
  /** `maintenance:ver` — o aviso de ordem aberta vira link. */
  podeVerOrdem: boolean;
  /** Presente só com `maintenance:criar`. */
  aoAbrirOrdem: ((alvo: AlvoDaOrdem) => void) | null;
}) {
  const aberta = !a.resolution;
  const ordemAberta = a.open_maintenance_order_id ?? null;
  // A ação só existe para pendência aberta (`resolution === null`): avaria
  // resolvida não pede conserto. Já com ordem aberta, o lugar dela é o aviso.
  // A corrida (duas pessoas, tela velha) continua coberta pelo 409
  // `MAINTENANCE_ORDER_ALREADY_OPEN`, que o modal transforma em link.
  const alvoDaOrdem = aberta && aoAbrirOrdem && !ordemAberta ? alvoDaAvaria(a) : null;
  return (
    <article
      aria-label={`${a.item_name ?? "Bem"} — ${ROTULO_DA_AVARIA[a.kind]}`}
      className={cn(
        "flex flex-col gap-3 rounded-xl border bg-card/70 p-3 sm:flex-row sm:items-center",
        aberta ? "border-destructive/30" : "border-border/60 opacity-85",
      )}
    >
      <div className="flex min-w-0 flex-1 gap-3">
        <FotoDoBem midia={a.cover} alt={a.item_name ?? ""} className="size-14" />
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <Link href={`/app/inventario/bens/${a.item_id}`} className="truncate text-sm font-medium hover:underline">
              {a.item_name ?? "Bem"}
            </Link>
            <Badge variant={aberta ? "destructive" : "neutral"}>
              {ROTULO_DA_AVARIA[a.kind]} · <span className="tabular-nums">{a.qty}</span>
            </Badge>
            {aberta ? null : (
              <Badge variant="accent">
                <CheckCircle2 aria-hidden="true" />
                {ROTULO_DO_DESFECHO[a.resolution!]}
              </Badge>
            )}
          </div>
          <p className="mt-0.5 text-xs text-muted-foreground">
            <span className="font-mono">{a.unit_code ?? "—"}</span> · {a.room_name ?? "ambiente"} · relatada em{" "}
            {formatarInstante(a.reported_at)}
            {a.reported_by_name ? ` por ${a.reported_by_name}` : ""}
            {a.reservation_code ? (
              <>
                {" "}
                · reserva <span className="font-mono">{a.reservation_code}</span>
              </>
            ) : null}
            {a.count_id ? (
              <>
                {" "}
                ·{" "}
                <Link href={caminhoDaConferencia(a.count_id)} className="underline-offset-4 hover:underline">
                  achada em conferência
                </Link>
              </>
            ) : null}
          </p>
          {a.note ? <p className="mt-1 text-xs">{a.note}</p> : null}
          {aberta && ordemAberta ? (
            <p className="mt-1 text-xs">
              {podeVerOrdem ? (
                <Link href={caminhoDaOrdem(ordemAberta)} className="inline-flex items-center gap-1 text-primary underline-offset-4 hover:underline">
                  <Wrench aria-hidden="true" className="size-3.5" />
                  Ordem de manutenção em aberto
                </Link>
              ) : (
                <span className="inline-flex items-center gap-1 text-muted-foreground">
                  <Wrench aria-hidden="true" className="size-3.5" />
                  Ordem de manutenção em aberto
                </span>
              )}
            </p>
          ) : null}
          {!aberta && a.resolved_at ? (
            <p className="mt-1 text-xs text-muted-foreground">
              Resolvida em {formatarInstante(a.resolved_at)}
              {a.resolved_by_name ? ` por ${a.resolved_by_name}` : ""}.
            </p>
          ) : null}
        </div>
      </div>

      <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 sm:flex-col sm:items-end">
        <span className="font-mono text-sm tabular-nums" title="Quantidade × custo de reposição, calculado pelo sistema">
          {typeof a.total_cost_cents === "number" ? formatarBRL(a.total_cost_cents) : "sem custo cotado"}
        </span>
        <div className="flex flex-wrap justify-end gap-1">
          {alvoDaOrdem ? (
            <Button size="sm" variant="outline" onClick={() => aoAbrirOrdem!(alvoDaOrdem)} className="max-sm:h-10">
              <Wrench aria-hidden="true" />
              Abrir ordem de manutenção
            </Button>
          ) : null}
          {permissoes.editar ? (
            aberta ? (
              <Button size="sm" variant="outline" onClick={aoResolver}>
                <CheckCircle2 aria-hidden="true" />
                Dar desfecho
              </Button>
            ) : (
              <Button size="sm" variant="ghost" onClick={aoReabrir}>
                <RotateCcw aria-hidden="true" />
                Reabrir
              </Button>
            )
          ) : null}
          {permissoes.excluir ? (
            <Button size="iconSm" variant="ghost" onClick={aoApagar} aria-label="Apagar este registro">
              <Trash2 aria-hidden="true" />
            </Button>
          ) : null}
        </div>
      </div>
    </article>
  );
}

function ModalDeDesfecho({ avaria, aoFechar }: { avaria: Avaria | null; aoFechar: () => void }) {
  const [desfecho, setDesfecho] = React.useState<DesfechoDeAvaria>("reposto");
  const [enviando, setEnviando] = React.useState(false);
  const [erro, setErro] = React.useState<string | null>(null);

  const [alvoAntes, setAlvoAntes] = React.useState(avaria);
  if (avaria !== alvoAntes) {
    setAlvoAntes(avaria);
    if (avaria) {
      setDesfecho(avaria.reservation_id ? "cobrado" : "reposto");
      setErro(null);
    }
  }

  async function salvar() {
    if (!avaria) return;
    setEnviando(true);
    setErro(null);
    try {
      const r = await resolverAvaria(avaria.id, desfecho);
      if (!r.ok) {
        setErro(mensagemDeBens(r, "avaria"));
        return;
      }
      notificarSucesso("Pendência resolvida", ROTULO_DO_DESFECHO[desfecho]);
      aoFechar();
    } finally {
      setEnviando(false);
    }
  }

  return (
    <ModalShell
      open={avaria !== null}
      onOpenChange={(aberto) => !aberto && aoFechar()}
      title={`Desfecho: ${avaria?.item_name ?? "avaria"}`}
      description="Dar o desfecho fecha a pendência. Quem resolveu e quando o sistema registra sozinho."
      footer={
        <>
          <Button variant="ghost" onClick={aoFechar} disabled={enviando}>
            Voltar
          </Button>
          <Button onClick={salvar} disabled={enviando}>
            {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <CheckCircle2 aria-hidden="true" />}
            Resolver
          </Button>
        </>
      }
    >
      {erro ? (
        <p role="alert" className="mb-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erro}
        </p>
      ) : null}
      <Campo id="desfecho" label="O que foi feito" obrigatorio hint={avaria?.reservation_code ? `Ligada à reserva ${avaria.reservation_code}.` : "Sem reserva ligada — não há hóspede a cobrar."}>
        {(p) => (
          <Select {...p} value={desfecho} onChange={(e) => setDesfecho(e.target.value as DesfechoDeAvaria)}>
            {DESFECHOS_DE_AVARIA.map((d) => (
              <option key={d} value={d}>
                {ROTULO_DO_DESFECHO[d]}
              </option>
            ))}
          </Select>
        )}
      </Campo>
    </ModalShell>
  );
}
