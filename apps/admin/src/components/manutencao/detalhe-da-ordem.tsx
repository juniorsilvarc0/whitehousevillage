"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { CalendarCheck2, CalendarX2, TriangleAlert } from "lucide-react";

import { AcoesDaOrdem } from "@/components/manutencao/acoes-da-ordem";
import { notificarFalha, notificarSucesso } from "@/components/manutencao/avisos";
import { ConfirmacaoDaOrdem } from "@/components/manutencao/confirmacao";
import { ModalDeCusto, ModalDeEdicao, ModalDoBloqueio } from "@/components/manutencao/modais-da-ordem";
import { LinhaDoBloqueio, SeloDePrioridade, SeloDeStatus } from "@/components/manutencao/selos";
import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { Secao } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ROTULO_DA_AVARIA, ROTULO_DO_DESFECHO } from "@/lib/bens/rotulos";
import { formatarInstante } from "@/lib/datas";
import { formatarBRL } from "@/lib/dinheiro";
import { cancelarOrdem, iniciarOrdem, soltarBloqueio } from "@/lib/manutencao/acoes";
import { pedeRecarga } from "@/lib/manutencao/mensagens";
import { botoesDaOrdem, type PermissoesDaManutencao } from "@/lib/manutencao/permissoes";
import { COMO_O_BLOQUEIO_E_LIBERADO, ROTULO_DA_FASE, avisoDoEncerramento, periodoPorExtenso } from "@/lib/manutencao/rotulos";
import type { OrdemDeManutencao } from "@/lib/manutencao/tipos";

/**
 * O detalhe da ordem — a tela que a operação abre no celular para iniciar,
 * concluir e lançar o custo.
 *
 * **Os botões saem da resposta**: `allowed_actions` (Iniciar, Concluir,
 * Cancelar ordem) e `editable` (`tudo` edita e mexe no bloqueio; `so_custo`
 * só lança o custo; `nada` trava tudo), cruzados com a matriz em
 * `botoesDaOrdem()`. A tela não sabe que "em andamento não inicia" — quem sabe
 * é a API, e a tela só obedece.
 */
export function DetalheDaOrdem({
  ordem,
  permissoes,
  podeVerInventario,
}: {
  ordem: OrdemDeManutencao;
  permissoes: Pick<PermissoesDaManutencao, "editar" | "excluir">;
  /** As ligações para a avaria e o bem levam ao inventário (`inventory.goods:ver`). */
  podeVerInventario: boolean;
}) {
  const router = useRouter();
  const botoes = botoesDaOrdem(ordem, permissoes);

  const edicao = useControleDeModal<OrdemDeManutencao>();
  const custo = useControleDeModal<OrdemDeManutencao>();
  const conclusao = useControleDeModal<OrdemDeManutencao>();
  const bloqueio = useControleDeModal<OrdemDeManutencao>();
  const [cancelando, setCancelando] = React.useState(false);
  const [soltando, setSoltando] = React.useState(false);
  const [iniciando, setIniciando] = React.useState(false);

  async function iniciar() {
    setIniciando(true);
    try {
      const r = await iniciarOrdem(ordem.id);
      if (r.ok) {
        notificarSucesso("Serviço iniciado");
        return;
      }
      // Segundo toque (`INVALID_STATE_TRANSITION`) ou ordem já encerrada: a
      // frase explica e a ordem é recarregada para mostrar como está.
      notificarFalha("Não foi possível iniciar", r, "transicao");
      if (pedeRecarga(r)) router.refresh();
    } finally {
      setIniciando(false);
    }
  }

  const bloqueioVivo = ordem.block && (ordem.block.phase === "agendado" || ordem.block.phase === "em_curso");

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <SeloDeStatus status={ordem.status} />
          <SeloDePrioridade prioridade={ordem.priority} />
          {ordem.block ? <LinhaDoBloqueio bloqueio={ordem.block} /> : null}
        </div>
        <AcoesDaOrdem
          ordem={ordem}
          botoes={botoes}
          iniciando={iniciando}
          aoIniciar={() => void iniciar()}
          aoConcluir={() => conclusao.abrir(ordem)}
          aoCancelar={() => setCancelando(true)}
          aoEditar={() => edicao.abrir(ordem)}
          aoLancarCusto={() => custo.abrir(ordem)}
        />
        {ordem.editable === "nada" ? (
          <p className="text-xs text-muted-foreground">Ordem cancelada: não aceita mais alterações. Retrabalho é uma ordem nova.</p>
        ) : ordem.editable === "so_custo" ? (
          <p className="text-xs text-muted-foreground">Ordem concluída: só o custo ainda pode ser lançado ou corrigido.</p>
        ) : null}
      </div>

      <Secao titulo="O que é">
        <dl className="grid gap-3 text-sm sm:grid-cols-2">
          <Dado rotulo="Unidade">
            <span className="font-mono">{ordem.unit_code}</span>
            <span className="text-muted-foreground"> · {ordem.unit_name}</span>
          </Dado>
          <Dado rotulo="Cômodo">{ordem.room_name ?? <span className="text-muted-foreground">A unidade toda</span>}</Dado>
          <Dado rotulo="Bem">
            {ordem.item_id ? (
              podeVerInventario ? (
                <Link href={`/app/inventario/bens/${ordem.item_id}`} className="text-primary underline-offset-4 hover:underline">
                  {ordem.item_name ?? "Ver o bem"}
                </Link>
              ) : (
                (ordem.item_name ?? "—")
              )
            ) : (
              <span className="text-muted-foreground">Nenhum em especial</span>
            )}
          </Dado>
          <Dado rotulo="Custo">
            {typeof ordem.cost_cents === "number" ? (
              <span className="font-mono tabular-nums">{formatarBRL(ordem.cost_cents)}</span>
            ) : (
              <span className="text-muted-foreground">Ainda não lançado</span>
            )}
          </Dado>
        </dl>
        {ordem.description ? <p className="mt-4 whitespace-pre-line text-sm">{ordem.description}</p> : null}
      </Secao>

      {ordem.issue ? (
        <Secao titulo="Avaria de origem" descricao="Concluir esta ordem marca a avaria como consertada, se ninguém a resolveu antes. Cancelar não mexe nela.">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <Badge variant={ordem.issue.resolution ? "neutral" : "destructive"}>
              <TriangleAlert aria-hidden="true" />
              {ROTULO_DA_AVARIA[ordem.issue.kind]} · <span className="tabular-nums">{ordem.issue.qty}</span>
            </Badge>
            {ordem.issue.resolution ? (
              <Badge variant="accent">{ROTULO_DO_DESFECHO[ordem.issue.resolution]}</Badge>
            ) : (
              <span className="text-xs text-muted-foreground">Pendente</span>
            )}
            {ordem.issue.reported_at ? (
              <span className="text-xs text-muted-foreground">relatada em {formatarInstante(ordem.issue.reported_at)}</span>
            ) : null}
          </div>
          {ordem.issue.note ? <p className="mt-2 text-sm">{ordem.issue.note}</p> : null}
          {podeVerInventario ? (
            <Link
              href={`/app/inventario/avarias?unidade=${ordem.unit_id}&situacao=todas`}
              className="mt-3 inline-block text-sm text-primary underline-offset-4 hover:underline"
            >
              Ver nas avarias de {ordem.unit_code}
            </Link>
          ) : null}
        </Secao>
      ) : null}

      <Secao
        titulo="Calendário"
        descricao="O bloqueio tira a unidade da venda como uma reserva. Encerrar a ordem o libera sozinho."
        acoes={
          botoes.gerenciarBloqueio ? (
            <>
              <Button size="sm" variant="outline" onClick={() => bloqueio.abrir(ordem)} className="max-sm:h-11">
                <CalendarX2 aria-hidden="true" />
                {bloqueioVivo ? "Mudar período" : "Bloquear calendário"}
              </Button>
              {bloqueioVivo ? (
                <Button size="sm" variant="ghost" onClick={() => setSoltando(true)} className="max-sm:h-11">
                  <CalendarCheck2 aria-hidden="true" />
                  Soltar bloqueio
                </Button>
              ) : null}
            </>
          ) : null
        }
      >
        {ordem.block ? (
          <div className="text-sm">
            <p className="font-medium">{ROTULO_DA_FASE[ordem.block.phase]}</p>
            <p className="mt-0.5 text-muted-foreground">{periodoPorExtenso(ordem.block)}</p>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">Esta ordem não bloqueou o calendário — a unidade continua à venda.</p>
        )}
      </Secao>

      <Secao titulo="Histórico">
        <ul className="flex flex-col gap-1.5 text-sm">
          <li>
            Aberta em {formatarInstante(ordem.opened_at)}
            {ordem.opened_by_name ? ` por ${ordem.opened_by_name}` : ""}
          </li>
          {ordem.started_at ? <li>Iniciada em {formatarInstante(ordem.started_at)}</li> : null}
          {ordem.closed_at ? (
            <li>
              {ordem.status === "cancelada" ? "Cancelada" : "Concluída"} em {formatarInstante(ordem.closed_at)}
              {ordem.closed_by_name ? ` por ${ordem.closed_by_name}` : ""}
            </li>
          ) : null}
        </ul>
      </Secao>

      <ModalDeEdicao controle={edicao.ref} />
      <ModalDeCusto controle={custo.ref} modo="lancar" />
      <ModalDeCusto controle={conclusao.ref} modo="concluir" />
      <ModalDoBloqueio controle={bloqueio.ref} />

      <ConfirmacaoDaOrdem
        aberto={cancelando}
        aoMudar={setCancelando}
        destrutivo
        titulo="Cancelar esta ordem?"
        rotuloConfirmar="Cancelar ordem"
        descricao={
          <>
            <p>{avisoDoEncerramento(ordem.block)}</p>
            {ordem.issue && !ordem.issue.resolution ? (
              <p>A avaria de origem continua pendente — cancelar não conserta nada.</p>
            ) : null}
            <p>Cancelada, a ordem não reabre e não aceita mais alterações.</p>
          </>
        }
        aoConfirmar={() => cancelarOrdem(ordem.id)}
        aoConcluir={() => notificarSucesso("Ordem cancelada")}
      />

      <ConfirmacaoDaOrdem
        aberto={soltando}
        aoMudar={setSoltando}
        titulo="Soltar o bloqueio?"
        rotuloConfirmar="Soltar bloqueio"
        contexto="bloqueio"
        descricao={
          <>
            <p>
              A unidade volta à venda sem encerrar a ordem. {COMO_O_BLOQUEIO_E_LIBERADO}
            </p>
            <p>Dá para bloquear de novo depois.</p>
          </>
        }
        aoConfirmar={() => soltarBloqueio(ordem.id)}
        aoConcluir={() => notificarSucesso("Bloqueio solto", "As datas voltaram à venda.")}
      />
    </div>
  );
}

function Dado({ rotulo, children }: { rotulo: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{rotulo}</dt>
      <dd className="mt-0.5">{children}</dd>
    </div>
  );
}
