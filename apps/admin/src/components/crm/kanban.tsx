"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { Columns3 } from "lucide-react";
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
import { IndicadorDeTempoReal } from "@/components/tempo-real/indicador";
import { buscarQuadro, moverEtapa } from "@/lib/crm/acoes";
import { mensagemCrm, type CodigoCrm } from "@/lib/crm/codigos";
import { localizarCard, planejarMovimento } from "@/lib/crm/kanban";
import type { FiltrosDoFunil } from "@/lib/crm/quadro";
import type {
  CardDaOportunidade,
  ColunaDoKanban,
  EtapaDoFunil,
  MotivoDePerda,
  QuadroKanban,
} from "@/lib/crm/tipos";
import { formatarBRL } from "@/lib/dinheiro";
import { useAtualizacaoAoVivo } from "@/lib/tempo-real/atualizacao";
import type { EventoDoStream, Topico } from "@/lib/tempo-real/eventos";
import type { CriarFonte } from "@/lib/tempo-real/sse";
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
 *
 * ## O quadro se move sozinho, e o selo diz quando parou de se mover
 *
 * O kanban assina o tópico `crm` do barramento. O evento chega magro
 * (`{entity:"opportunity", id, v}`) e a tela **refaz o fetch autenticado** do
 * mesmo recorte que está na URL — que é onde o `scope='own'` do corretor é
 * aplicado. Sem isso, duas pessoas no funil não veem o trabalho uma da outra:
 * quem arrasta o card vê, quem está com a tela aberta ao lado continua vendo o
 * card na coluna antiga até apertar F5, e liga para o cliente que o colega
 * acabou de ganhar.
 *
 * O selo de tempo real não é enfeite: um quadro que parou de receber eventos é
 * **visualmente idêntico** a um quadro onde ninguém mexeu. Sem ele, a versão
 * "ao vivo" seria pior do que a versão sem tempo real, porque ensinaria a
 * confiar num desenho velho.
 */

/** O kanban assina **só** `crm`. Pedir `calendar` junto abriria a mesma conexão
 *  para receber evento de bloqueio que esta tela descarta — e o handshake do
 *  `/stream` confere permissão por tópico, então quem não tem `calendar`
 *  receberia um `ready` com um tópico a menos, sem nenhuma razão. */
const TOPICOS: readonly Topico[] = ["crm"];

/** Hoje o canal `whv_crm` só publica `opportunity` (migration `20260827120000`),
 *  mas o envelope do barramento é compartilhado e `lead` e `activity` já estão
 *  no vocabulário do cliente. O filtro é o que impede o dia em que um deles
 *  entrar no canal de virar um refetch do quadro inteiro por nota registrada. */
function interessaAoFunil(evento: EventoDoStream): boolean {
  return evento.entity === "opportunity";
}

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
  filtros,
  criarFonte,
}: {
  quadro: QuadroKanban;
  motivos: MotivoDePerda[];
  permissoes: PermissoesDoFunil;
  /** O recorte que está na URL. Vai de volta ao servidor a cada atualização
   *  automática: o evento diz que o funil mudou, e o que a tela precisa é o
   *  MESMO recorte de novo — nunca o funil inteiro. */
  filtros: FiltrosDoFunil;
  /**
   * Costura de teste, e ela é exigida pela forma do defeito que este componente
   * não pode reintroduzir.
   *
   * `useSSE` resolve a fábrica da conexão por valor default de parâmetro, e a
   * identidade de um default **não sobrevive à minificação**: no build de
   * produção ela passou a valer uma função nova a cada render e a conexão
   * reabria a cada repintura (~220 aberturas por segundo, medidas). A regressão
   * só é observável re-renderizando com uma fábrica de identidade NOVA — o que
   * é impossível sem poder injetá-la daqui, porque o default é uma constante de
   * módulo cuja identidade nunca muda. Em produção fica `undefined` e vale o
   * default.
   */
  criarFonte?: CriarFonte;
}) {
  const router = useRouter();
  const ganho = useControleDeModal<AlvoDeGanho>();
  const perda = useControleDeModal<AlvoDePerda>();

  // O quadro inteiro num estado só — colunas **e** totais. Eram dois lugares
  // até o tempo real: as colunas aqui e o total no cabeçalho desenhado pelo
  // RSC. Com a atualização automática isso deixaria de fechar, porque o evento
  // repõe as colunas e o total ficaria no valor da primeira carga — e o total
  // do funil é o primeiro número que a gestão olha.
  const [vivo, setVivo] = React.useState<QuadroKanban>(quadro);
  const colunas = vivo.columns;

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
  const [quadroAnterior, setQuadroAnterior] = React.useState(quadro);
  if (quadro !== quadroAnterior) {
    setQuadroAnterior(quadro);
    setVivo(quadro);
  }

  /**
   * A assinatura do barramento — o mesmo hook que o mapa de ocupação usa.
   *
   * `aoAtualizar` é o `setVivo` direto, e é de propósito: o que o servidor
   * devolve é a verdade, inclusive por cima de um movimento otimista que ainda
   * não foi confirmado. O palpite otimista dura o voo de uma requisição; a
   * resposta do `/stage` chega logo atrás e o `router.refresh()` dela repõe o
   * quadro gravado.
   */
  const aoVivo = useAtualizacaoAoVivo<QuadroKanban, CodigoCrm>({
    topicos: TOPICOS,
    interessa: interessaAoFunil,
    buscar: React.useCallback(() => buscarQuadro(filtros), [filtros]),
    aoAtualizar: setVivo,
    criarFonte,
  });

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

    setVivo((atual) => ({ ...atual, columns: plano.colunas }));
    marcar(cardId, true);
    try {
      const resultado = await moverEtapa(cardId, destinoId, plano.origem);
      if (!resultado.ok) {
        setVivo((atual) => ({ ...atual, columns: anterior }));
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

      {/* Nome do funil, total do funil e o selo de tempo real na mesma linha —
          e os três saem do MESMO estado. O total desenhado pelo servidor, fora
          deste componente, ficaria parado enquanto as colunas se mexem. */}
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
        <p className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
          <span className="flex items-center gap-2">
            <Columns3 className="size-4" aria-hidden="true" />
            <strong className="font-display text-base text-foreground">{vivo.pipeline.name}</strong>
          </span>
          <span className="font-mono tabular-nums">
            {vivo.totals.count} {vivo.totals.count === 1 ? "negócio aberto" : "negócios"} ·{" "}
            {formatarBRL(vivo.totals.amount_cents)}
          </span>
        </p>

        <IndicadorDeTempoReal
          estado={aoVivo.tempoReal.estado}
          atualizando={aoVivo.atualizando}
          atualizadoEm={aoVivo.atualizadoEm}
          aoReconectar={aoVivo.tempoReal.reconectar}
          assunto="funil"
        />
      </div>

      {aoVivo.falha ? (
        <p
          role="status"
          className="rounded-lg border border-alcada-atencao/40 bg-alcada-atencao/10 px-3 py-2 text-xs text-foreground"
        >
          A última atualização automática falhou ({mensagemCrm(aoVivo.falha)}) — o quadro mostra o
          desenho anterior. Recarregue a página para ver o funil de agora.
        </p>
      ) : null}

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
