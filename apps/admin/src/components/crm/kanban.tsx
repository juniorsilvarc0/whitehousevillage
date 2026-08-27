"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";

import { AvisosDoCrm, notificar, notificarSucesso } from "@/components/crm/avisos";
import { CartaoDaOportunidade } from "@/components/crm/card-da-oportunidade";
import { DialogoDeGanho, type AlvoDeGanho } from "@/components/crm/dialogo-de-ganho";
import { DialogoDePerda, type AlvoDePerda } from "@/components/crm/dialogo-de-perda";
import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { EstadoVazio } from "@/components/layout/estados";
import { moverEtapa } from "@/lib/crm/acoes";
import { localizarCard, planejarMovimento } from "@/lib/crm/kanban";
import type {
  CardDaOportunidade,
  ColunaDoKanban,
  EtapaDoFunil,
  MotivoDePerda,
  QuadroKanban,
} from "@/lib/crm/tipos";
import { formatarBRL } from "@/lib/dinheiro";
import { cn } from "@/lib/utils";

/**
 * O kanban do funil.
 *
 * ## O movimento é otimista, e o desfazer é o valor de antes
 *
 * O card muda de coluna no instante do gesto e a requisição sai depois. Recusa
 * repõe o array que a função guardou antes de mexer — não existe "desaplicar",
 * porque a transformação inversa seria uma segunda implementação da mesma
 * regra, capaz de divergir. A tela avisa por toast: card que volta sozinho e em
 * silêncio ensina o operador que "o sistema não aceita arrastar".
 *
 * ## Coluna terminal não se alcança arrastando
 *
 * Soltar em "Ganho" abre o diálogo de ganho; em "Perdido", o de perda. O card
 * **não sai do lugar** nesses dois casos: ganhar cria reserva (e exige
 * `Idempotency-Key`) e perder exige motivo. `POST /stage` recusa os dois com
 * `409` justamente para que o gesto que mais se repete por engano não crie
 * reserva sozinho.
 *
 * ## A cor é da coluna
 *
 * `stage.color` tinge a coluna; o card fica neutro com aro fino (docs/ui.md §9).
 * É a única cor fora dos tokens do design system em toda a tela, e é assim
 * porque ela **é dado** — vem da configuração do funil, que a gestão edita sem
 * deploy. Por vir de dado, passa por validação antes de entrar no `style`.
 */

/** Uma cor de etapa só entra no `style` se for hex de 3 ou 6 dígitos. Valor
 *  vindo do banco é dado de usuário: inválido derruba silenciosamente a regra
 *  CSS e a coluna some de cor, o que parece defeito da tela. */
function corSegura(cor: string): string {
  return /^#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?$/.test(cor.trim()) ? cor.trim() : "var(--primary)";
}

export type PermissoesDoFunil = { editar: boolean };

export function PipelineKanban({
  quadro,
  motivos,
  permissoes,
}: {
  quadro: QuadroKanban;
  motivos: MotivoDePerda[];
  permissoes: PermissoesDoFunil;
}) {
  const router = useRouter();
  const ganho = useControleDeModal<AlvoDeGanho>();
  const perda = useControleDeModal<AlvoDePerda>();

  const [colunas, setColunas] = React.useState<ColunaDoKanban[]>(quadro.columns);
  const [pendentes, setPendentes] = React.useState<readonly string[]>([]);
  const [arrastando, setArrastando] = React.useState<string | null>(null);

  /**
   * Adota o quadro novo quando o servidor responde — **durante o render**, não
   * num efeito.
   *
   * É o padrão que o React documenta para "repor estado quando uma prop muda":
   * o componente reexecuta antes de pintar, e nenhum filho chega a renderizar
   * com as colunas velhas. Num efeito haveria um quadro com o kanban de antes
   * do refresh, logo depois de uma ação que acabou de mudar o funil.
   */
  const [quadroAnterior, setQuadroAnterior] = React.useState(quadro.columns);
  if (quadro.columns !== quadroAnterior) {
    setQuadroAnterior(quadro.columns);
    setColunas(quadro.columns);
  }

  const etapas = React.useMemo(() => colunas.map((coluna) => coluna.stage), [colunas]);

  const sensores = useSensors(
    // Só vira arrasto depois de 6px: sem isso, todo clique no card (o link para
    // a oportunidade, o menu) começaria um arrasto de zero pixel e engoliria o
    // clique.
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor),
  );

  function marcar(cardId: string, ocupado: boolean) {
    setPendentes((atual) =>
      ocupado ? [...atual, cardId] : atual.filter((id) => id !== cardId),
    );
  }

  async function mover(cardId: string, destinoId: string) {
    const plano = planejarMovimento(colunas, cardId, destinoId);
    if (plano.tipo === "nada") return;

    if (plano.tipo === "ganhar") {
      ganho.abrir({
        id: plano.card.id,
        contato: plano.card.contact_name,
        produto: plano.card.unit_type_name,
        check_in: plano.card.check_in,
        check_out: plano.card.check_out,
        amount_cents: plano.card.amount_cents,
        // `undefined` é "não se sabe", e não "não tem": o card do kanban não
        // carrega `quote_id` (o contrato o mantém fora por peso). Mandar `null`
        // aqui desabilitaria o botão de ganhar em todo card do quadro — quem
        // decide se há orçamento vigente é o servidor, no `/win`.
        quote_id: undefined,
      });
      return;
    }

    if (plano.tipo === "perder") {
      perda.abrir({
        id: plano.card.id,
        contato: plano.card.contact_name,
        reservation_code: plano.card.reservation_code,
      });
      return;
    }

    // O valor de antes, guardado antes de qualquer escrita. É este array — e
    // não uma transformação inversa — que repõe o quadro se a API recusar.
    const anterior = colunas;

    setColunas(plano.colunas);
    marcar(cardId, true);
    try {
      const resultado = await moverEtapa(cardId, destinoId, plano.origem);
      if (!resultado.ok) {
        setColunas(anterior);
        notificar(resultado);
        return;
      }
      if (resultado.data.auto_task) {
        notificarSucesso(
          `Card movido para ${plano.destino.name}`,
          `Tarefa criada: ${resultado.data.auto_task.subject}.`,
        );
      }
      // O servidor é quem ordena a coluna e recalcula os totais; o refresh
      // reconcilia o palpite otimista com o que ficou gravado.
      router.refresh();
    } finally {
      marcar(cardId, false);
    }
  }

  function aoIniciarArrasto(evento: DragStartEvent) {
    setArrastando(String(evento.active.id));
  }

  function aoTerminarArrasto(evento: DragEndEvent) {
    setArrastando(null);
    const destino = evento.over?.id;
    if (!destino) return;
    void mover(String(evento.active.id), String(destino));
  }

  const cardArrastado = arrastando ? localizarCard(colunas, arrastando)?.card ?? null : null;
  const etapaDoArrastado = arrastando ? localizarCard(colunas, arrastando)?.origem : undefined;

  if (colunas.length === 0) {
    return (
      <EstadoVazio
        titulo="Este funil não tem etapas"
        descricao="Um funil sem etapa não desenha quadro nenhum. Cadastre as etapas em Configurações para o kanban existir."
      />
    );
  }

  return (
    <>
      <AvisosDoCrm />
      <DndContext sensors={sensores} onDragStart={aoIniciarArrasto} onDragEnd={aoTerminarArrasto}>
        {/* O quadro rola **dentro** do próprio container: tabela que estoura a
            página horizontalmente é anti-padrão declarado (docs/ui.md §10). */}
        <div className="-mx-1 overflow-x-auto overscroll-x-contain px-1 pb-2">
          <ol className="flex min-h-[24rem] items-start gap-3">
            {colunas.map((coluna) => (
              <Coluna
                key={coluna.stage.id}
                coluna={coluna}
                etapas={etapas}
                aoMover={(cardId, destinoId) => void mover(cardId, destinoId)}
                pendentes={pendentes}
                podeEditar={permissoes.editar}
              />
            ))}
          </ol>
        </div>

        <DragOverlay>
          {cardArrastado && etapaDoArrastado ? (
            <div className="w-72 rotate-1 opacity-95">
              <div className="rounded-xl border border-border bg-card px-3 py-2.5 shadow-soft">
                <p className="truncate text-sm font-medium">{cardArrastado.contact_name}</p>
                <p className="truncate text-xs text-muted-foreground">
                  {cardArrastado.unit_type_name ?? "Produto a definir"}
                </p>
                <p className="mt-1 font-mono text-sm tabular-nums">
                  {formatarBRL(cardArrastado.amount_cents)}
                </p>
              </div>
            </div>
          ) : null}
        </DragOverlay>
      </DndContext>

      <DialogoDeGanho controle={ganho.ref} aoGanhar={() => router.refresh()} />
      <DialogoDePerda controle={perda.ref} motivos={motivos} aoPerder={() => router.refresh()} />
    </>
  );
}

function Coluna({
  coluna,
  etapas,
  aoMover,
  pendentes,
  podeEditar,
}: {
  coluna: ColunaDoKanban;
  etapas: EtapaDoFunil[];
  aoMover: (cardId: string, destinoId: string) => void;
  pendentes: readonly string[];
  podeEditar: boolean;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: coluna.stage.id });
  const cor = corSegura(coluna.stage.color);

  return (
    <li
      ref={setNodeRef}
      aria-label={`Etapa ${coluna.stage.name}`}
      data-sobre={isOver ? "true" : undefined}
      className={cn(
        "flex w-72 shrink-0 flex-col rounded-xl border transition-[border-color,background-color]",
        isOver && "ring-2 ring-ring/40",
      )}
      style={{
        // A cor da etapa é dado do funil, não token — daí o `style`. O
        // `color-mix` mantém o tingimento fraco o bastante para o texto do
        // cartão continuar legível em qualquer cor que a gestão escolher.
        borderColor: `color-mix(in oklab, ${cor} 38%, transparent)`,
        backgroundColor: `color-mix(in oklab, ${cor} 8%, transparent)`,
      }}
    >
      <header className="flex items-start justify-between gap-2 border-b px-3 py-2.5" style={{ borderColor: `color-mix(in oklab, ${cor} 28%, transparent)` }}>
        <div className="min-w-0">
          <h3 className="flex items-center gap-2 text-sm font-medium leading-tight">
            <span aria-hidden="true" className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: cor }} />
            <span className="truncate">{coluna.stage.name}</span>
          </h3>
          <p className="mt-1 font-mono text-xs tabular-nums text-muted-foreground">
            {/* Contagem e soma vêm do **servidor**: a coluna é paginada, e somar
                o que está na tela daria um total de funil que muda ao rolar. */}
            {coluna.count} {coluna.count === 1 ? "card" : "cards"} · {formatarBRL(coluna.amount_cents)}
          </p>
        </div>
        <span className="shrink-0 rounded-full bg-card/70 px-2 py-0.5 font-mono text-[0.65rem] tabular-nums text-muted-foreground">
          {coluna.stage.probability}%
        </span>
      </header>

      <div className="flex min-h-24 flex-col gap-2 p-2">
        {coluna.cards.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border/60 px-3 py-6 text-center text-xs text-muted-foreground">
            {coluna.stage.type === "aberto" ? "Nenhum negócio nesta etapa." : "Nada fechado nos últimos 30 dias."}
          </p>
        ) : null}

        {coluna.cards.map((card: CardDaOportunidade) => (
          <CartaoDaOportunidade
            key={card.id}
            card={card}
            etapaAtual={coluna.stage}
            etapas={etapas}
            aoMover={(destinoId) => aoMover(card.id, destinoId)}
            podeEditar={podeEditar}
            ocupado={pendentes.includes(card.id)}
          />
        ))}

        {coluna.has_more ? (
          <p className="px-1 py-1 text-center text-[0.68rem] text-muted-foreground">
            Há mais cards nesta etapa do que os {coluna.cards.length} carregados. Estreite o filtro para
            alcançá-los.
          </p>
        ) : null}
      </div>
    </li>
  );
}
