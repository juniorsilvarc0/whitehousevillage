"use client";

import * as React from "react";
import Link from "next/link";
import { ExternalLink, Loader2, Unlock } from "lucide-react";

import { ModalShell } from "@/components/layout/modal-shell";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { formatarData } from "@/lib/datas";
import { mensagemDoErro, type Resultado } from "@/lib/acoes/resultado";
import { expiracaoDeHold } from "@/lib/mapa/expiracao";
import type { Janela } from "@/lib/mapa/janela";
import { EXPLICACAO_DO_STATUS, ROTULO_DO_STATUS } from "@/lib/mapa/rotulos";
import type { ExpiracaoDeHold } from "@/lib/mapa/tipos";
import { useAgora } from "@/lib/tempo-real/relogio";
import { cn } from "@/lib/utils";

import type { AlvoDaFaixa } from "./grade";

/**
 * O detalhe de uma ocupação, aberto pelo clique na barra.
 *
 * Duas honestidades que este modal tem de manter:
 *
 * 1. **As datas mostradas são as da JANELA, não necessariamente as da estadia.**
 *    A resposta cobre só o intervalo pedido; uma reserva que começou antes do
 *    primeiro dia da tela aparece cortada. O modal diz "a partir de" em vez de
 *    afirmar um check-in que não veio na resposta — inventá-lo seria a tela
 *    falando de dado que não tem.
 * 2. **Bloqueio de reserva não se libera por aqui.** O contrato responde
 *    `409 INVALID_STATE_TRANSITION`, porque quem solta a data de uma reserva é
 *    `/cancel`, `/check-out` ou o job de expiração. Apagar o bloco por fora
 *    deixaria a reserva `confirmed` sem calendário e ninguém veria. O botão não
 *    aparece — recusar depois do clique seria oferecer o que não existe.
 */
export function DetalheDaOcupacao({
  alvo,
  janela,
  expiracoes,
  podeLiberar,
  aoFechar,
  aoLiberar,
}: {
  alvo: AlvoDaFaixa | null;
  janela: Janela;
  expiracoes: ExpiracaoDeHold;
  podeLiberar: boolean;
  aoFechar: () => void;
  aoLiberar: (stayBlockId: string) => Promise<Resultado<null>>;
}) {
  const agora = useAgora();
  const [liberando, setLiberando] = React.useState(false);
  const [recusa, setRecusa] = React.useState<string | null>(null);

  // O último alvo sobrevive ao fechamento por um motivo de animação: o modal
  // continua montado enquanto sai, e desmontá-lo junto com o alvo faria o
  // conteúdo evaporar antes de a folha descer. Ao reabrir, o `open` muda antes
  // do conteúdo e o `data-starting-style` do Base UI chega inteiro.
  //
  // Guardado em `useState` e ajustado no render — a forma que o React documenta
  // para "prop nova manda". Um `useRef` faria o mesmo e é o que a regra
  // `react-hooks/refs` reprova: valor lido no render que não dispara re-render.
  const [visivel, setVisivel] = React.useState<AlvoDaFaixa | null>(alvo);
  if (alvo && alvo !== visivel) {
    setVisivel(alvo);
    setRecusa(null);
    setLiberando(false);
  }

  if (!visivel) return null;

  const { faixa, linha } = visivel;
  const inicio = janela.dias[faixa.inicio];
  const fim = janela.dias[Math.max(faixa.inicio, faixa.fim - 1)];
  const expiracao =
    faixa.status === "hold" && faixa.reservationId
      ? expiracaoDeHold(expiracoes.get(faixa.reservationId), agora)
      : null;

  const ehBloqueioOperacional =
    (faixa.status === "maintenance" || faixa.status === "owner_hold") &&
    faixa.stayBlockId !== null &&
    faixa.reservationId === null;

  async function liberar() {
    if (!faixa.stayBlockId) return;
    setLiberando(true);
    setRecusa(null);
    const resultado = await aoLiberar(faixa.stayBlockId);
    setLiberando(false);
    if (resultado.ok) aoFechar();
    else setRecusa(mensagemDoErro(resultado.code));
  }

  return (
    <ModalShell
      open={alvo !== null}
      onOpenChange={(aberto) => {
        if (!aberto) aoFechar();
      }}
      title={ROTULO_DO_STATUS[faixa.status]}
      description={`${linha.codigo} · ${linha.nome}`}
      footer={
        <>
          {ehBloqueioOperacional && podeLiberar ? (
            <Button variant="outline" onClick={liberar} disabled={liberando}>
              {liberando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Unlock aria-hidden="true" />}
              Liberar bloqueio
            </Button>
          ) : null}
          {faixa.reservationId ? (
            <Link href={`/app/reservas/${faixa.reservationId}`} className={cn(buttonVariants())}>
              <ExternalLink aria-hidden="true" />
              Abrir a reserva
            </Link>
          ) : (
            <Button variant="ghost" onClick={aoFechar}>
              Fechar
            </Button>
          )}
        </>
      }
    >
      <dl className="flex flex-col gap-3 text-sm">
        <Linha rotulo="Período mostrado">
          <span className="tabular-nums">
            {faixa.tocaInicio ? "a partir de " : ""}
            {inicio ? formatarData(inicio) : "—"}
            {" a "}
            {fim ? formatarData(fim) : "—"}
            {faixa.tocaFim ? " (segue além da faixa)" : ""}
          </span>
        </Linha>

        <Linha rotulo="Noites na faixa">
          <span className="tabular-nums">{faixa.fim - faixa.inicio}</span>
        </Linha>

        {faixa.reservationCode ? (
          <Linha rotulo="Reserva">
            <span className="font-mono">{faixa.reservationCode}</span>
          </Linha>
        ) : null}

        {faixa.guestName ? <Linha rotulo="Hóspede">{faixa.guestName}</Linha> : null}

        {expiracao ? (
          <Linha rotulo="Pré-reserva">
            <Badge variant={expiracao.urgencia === "tranquila" ? "neutral" : "destructive"}>
              {expiracao.texto}
            </Badge>
          </Linha>
        ) : null}
      </dl>

      <p className="mt-4 rounded-lg bg-muted/40 px-3 py-2 text-xs leading-relaxed text-muted-foreground">
        {EXPLICACAO_DO_STATUS[faixa.status]}
        {ehBloqueioOperacional && !podeLiberar ? (
          <>
            {" "}
            Liberar exige <code className="font-mono">calendar:excluir</code> na matriz do seu perfil — a
            permissão é o par simétrico de criar o bloqueio.
          </>
        ) : null}
        {faixa.reservationId && faixa.stayBlockId ? (
          <> Esta data é de uma reserva: quem a solta é o cancelamento, o check-out ou a expiração.</>
        ) : null}
      </p>

      {recusa ? (
        <p role="alert" className="mt-3 rounded-lg border border-destructive/30 bg-destructive/8 px-3 py-2 text-sm text-destructive">
          {recusa}
        </p>
      ) : null}
    </ModalShell>
  );
}

function Linha({ rotulo, children }: { rotulo: string; children: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 border-b border-border/40 pb-2 last:border-0">
      <dt className="shrink-0 text-xs uppercase tracking-wide text-muted-foreground">{rotulo}</dt>
      <dd className="min-w-0 text-right text-foreground">{children}</dd>
    </div>
  );
}
