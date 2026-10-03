"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { CalendarRange, Loader2, TriangleAlert, Trophy } from "lucide-react";

import { descricaoDaFalha, notificarSucesso } from "@/components/crm/avisos";
import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Button, buttonVariants } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Textarea } from "@/components/ui/textarea";
import { ganharOportunidade } from "@/lib/crm/acoes";
import type { FalhaCrm } from "@/lib/crm/api";
import { GanhoFormulario } from "@/lib/crm/esquemas";
import { novaChaveDeIdempotencia } from "@/lib/crm/idempotencia";
import { formatarData } from "@/lib/datas";
import { formatarBRL } from "@/lib/dinheiro";
import { cn } from "@/lib/utils";

/**
 * "Ganhar" — o botão que fecha o negócio e cria a reserva.
 *
 * Três coisas que a tela precisa dizer, e que este diálogo existe para dizer:
 *
 * 1. **A reserva nasce `hold`, não confirmada.** Ganhar é o acordo comercial;
 *    confirmar é dinheiro recebido (spec §5). Quem clica aqui tem de saber que
 *    a data fica segura por um prazo, não vendida para sempre.
 * 2. **Nada é redigitado.** A reserva sai do orçamento vigente, com o preço
 *    congelado. Sem orçamento, o servidor recusa com `QUOTE_REQUIRED_TO_WIN` —
 *    e o diálogo avisa disso *antes* do clique, em vez de colher a recusa.
 * 3. **A data pode ter sido vendida enquanto se negociava.** É a recusa mais
 *    comum da alta temporada, e é a única que se resolve olhando o mapa. Por
 *    isso o `409 DATE_CONFLICT` fica **dentro** do modal, com a unidade, o
 *    intervalo e o caminho para o mapa — e o modal não fecha.
 */

export type AlvoDeGanho = {
  id: string;
  contato: string;
  produto: string | null;
  check_in: string | null;
  check_out: string | null;
  amount_cents: number;
  /**
   * O orçamento vigente da oportunidade — e a distinção que faz o botão
   * funcionar em duas telas diferentes:
   *
   * - `string` — há orçamento;
   * - `null` — **sabidamente** não há (a tela da oportunidade leu o `/full`);
   * - `undefined` — **não se sabe** (o kanban: `CardDaOportunidade` não carrega
   *   `quote_id`, por peso).
   *
   * Só `null` desabilita o botão. Tratar "não sei" como "não tem" bloquearia
   * todo ganho vindo do quadro, que é de onde vem a maioria deles.
   */
  quote_id: string | null | undefined;
};

export function DialogoDeGanho({
  controle,
  aoGanhar,
}: {
  controle: React.RefObject<ControleDeModal<AlvoDeGanho> | null>;
  aoGanhar?: () => void;
}) {
  const router = useRouter();
  const [aberto, setAberto] = React.useState(false);
  const [alvo, setAlvo] = React.useState<AlvoDeGanho | null>(null);
  const [falha, setFalha] = React.useState<FalhaCrm | null>(null);
  /** Estável por abertura: é o que impede o duplo clique de virar duas reservas. */
  const [chave, setChave] = React.useState(() => novaChaveDeIdempotencia());

  const form = useForm<GanhoFormulario>({
    resolver: zodResolver(GanhoFormulario),
    defaultValues: { note: "" },
  });

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(escolhido: AlvoDeGanho) {
        setAlvo(escolhido);
        setFalha(null);
        setChave(novaChaveDeIdempotencia());
        form.reset({ note: "" });
        setAberto(true);
      },
    }),
    [form],
  );

  const semOrcamento = alvo !== null && alvo.quote_id === null;
  const orcamentoDesconhecido = alvo !== null && alvo.quote_id === undefined;

  async function enviar(valores: GanhoFormulario) {
    if (!alvo) return;
    setFalha(null);
    const resultado = await ganharOportunidade(alvo.id, chave, valores);
    if (!resultado.ok) {
      // A recusa fica onde o clique aconteceu. Fechar o modal e avisar longe
      // do contexto transformaria uma recusa explicada em "não aconteceu nada".
      setFalha(resultado);
      return;
    }
    const reserva = resultado.data.reservation;
    notificarSucesso(
      resultado.data.reservation_created ? `Reserva ${reserva.code} criada` : `Oportunidade ganha`,
      resultado.data.reservation_created
        ? "Foi criada como pré-reserva: as datas estão guardadas. O sinal é registrado na tela da reserva."
        : `A pré-reserva ${reserva.code}, que já existia, foi ligada a este negócio — nenhuma reserva nova foi criada.`,
    );
    setAberto(false);
    aoGanhar?.();
    router.refresh();
  }

  const { isSubmitting } = form.formState;

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title="Ganhar a oportunidade"
      description={alvo ? `${alvo.contato} · ${alvo.produto ?? "produto a definir"}` : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button type="submit" form="form-ganhar" disabled={isSubmitting || semOrcamento}>
            {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Trophy aria-hidden="true" />}
            Ganhar e criar a reserva
          </Button>
        </>
      }
    >
      {alvo ? (
        <form id="form-ganhar" onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
          <div className="rounded-xl border border-border/60 bg-muted/20 p-3.5">
            <p className="flex items-center gap-2 text-sm">
              <CalendarRange className="size-4 text-muted-foreground" aria-hidden="true" />
              {alvo.check_in && alvo.check_out
                ? `${formatarData(alvo.check_in)} → ${formatarData(alvo.check_out)}`
                : "Datas ainda não definidas"}
            </p>
            <p className="mt-1 font-mono text-lg tabular-nums">{formatarBRL(alvo.amount_cents)}</p>
          </div>

          {semOrcamento ? (
            <Nota variante="atencao">
              Esta oportunidade não tem <strong>orçamento válido</strong>, e sem ele não há preço para a
              reserva. Faça o orçamento primeiro.
            </Nota>
          ) : (
            <Nota>
              A reserva é criada como <strong>pré-reserva</strong>, com o preço do orçamento atual. Ganhar
              é fechar o acordo; confirmar é receber o sinal, que continua sendo um passo separado na tela
              da reserva.
              {orcamentoDesconhecido ? (
                <>
                  {" "}
                  Se não houver orçamento válido, o sistema avisa aqui mesmo, sem mudar nada.
                </>
              ) : null}
            </Nota>
          )}

          <Campo id="ganho-note" label="Observação" hint="Fica registrado no histórico do negócio.">
            {(props) => <Textarea {...props} {...form.register("note")} placeholder="Fechado por telefone com a Fernanda." />}
          </Campo>

          {falha ? <RecusaDoGanho falha={falha} alvo={alvo} /> : null}
        </form>
      ) : null}
    </ModalShell>
  );
}

/**
 * A recusa, explicada.
 *
 * `DATE_CONFLICT` ganha tratamento próprio porque é o único caso em que a ação
 * seguinte não está nesta tela: a data foi vendida, e quem decide o que fazer
 * precisa ver o mapa daquele período. O link já vai com as datas.
 */
function RecusaDoGanho({ falha, alvo }: { falha: FalhaCrm; alvo: AlvoDeGanho }) {
  const conflitoDeData = falha.code === "DATE_CONFLICT";
  return (
    <div
      role="alert"
      data-codigo={falha.code}
      title={`Código para o suporte: ${falha.code}`}
      className={cn(
        "rounded-xl border px-3.5 py-3",
        conflitoDeData ? "border-destructive/35 bg-destructive/10" : "border-destructive/25 bg-destructive/8",
      )}
    >
      <p className="flex items-start gap-2 text-sm text-destructive">
        <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
        <span className="min-w-0">{descricaoDaFalha(falha)}</span>
      </p>
      <p className="mt-2 text-xs text-muted-foreground">
        Nada mudou: a oportunidade continua aberta, na etapa em que estava.
      </p>
      {conflitoDeData && alvo.check_in ? (
        <Link
          href={`/app/mapa?from=${alvo.check_in}${alvo.check_out ? `&to=${alvo.check_out}` : ""}`}
          className={cn(buttonVariants({ variant: "outline", size: "sm" }), "mt-3")}
        >
          Abrir o mapa nessas datas
        </Link>
      ) : null}
    </div>
  );
}
