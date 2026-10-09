"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { Loader2 } from "lucide-react";

import { ModalShell } from "@/components/layout/modal-shell";
import { Button } from "@/components/ui/button";
import type { Resultado } from "@/lib/acoes/resultado";
import { mensagemDeManutencao, pedeRecarga, type Contexto } from "@/lib/manutencao/mensagens";

/**
 * Confirmação de gesto que mexe no calendário ou encerra a ordem.
 *
 * O erro aparece **dentro** do modal e ele não fecha — com a frase pelo código
 * (`mensagemDeManutencao`). Quando a recusa diz que o estado mudou por baixo
 * da tela (`MAINTENANCE_ORDER_CLOSED`, `INVALID_STATE_TRANSITION`), a ordem é
 * recarregada na hora: o modal explica, e o fundo já mostra como ela ficou.
 */
export function ConfirmacaoDaOrdem({
  aberto,
  aoMudar,
  titulo,
  descricao,
  rotuloConfirmar,
  destrutivo = false,
  contexto = "transicao",
  aoConfirmar,
  aoConcluir,
}: {
  aberto: boolean;
  aoMudar: (aberto: boolean) => void;
  titulo: string;
  descricao: React.ReactNode;
  rotuloConfirmar: string;
  destrutivo?: boolean;
  contexto?: Contexto;
  aoConfirmar: () => Promise<Resultado<unknown>>;
  aoConcluir?: () => void;
}) {
  const router = useRouter();
  const [enviando, setEnviando] = React.useState(false);
  const [erro, setErro] = React.useState<string | null>(null);

  const [abertoAnterior, setAbertoAnterior] = React.useState(aberto);
  if (aberto !== abertoAnterior) {
    setAbertoAnterior(aberto);
    if (aberto) setErro(null);
  }

  async function confirmar() {
    setEnviando(true);
    setErro(null);
    try {
      const r = await aoConfirmar();
      if (!r.ok) {
        setErro(mensagemDeManutencao(r, contexto));
        if (pedeRecarga(r)) router.refresh();
        return;
      }
      aoMudar(false);
      aoConcluir?.();
    } finally {
      setEnviando(false);
    }
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={aoMudar}
      title={titulo}
      footer={
        <>
          <Button variant="ghost" onClick={() => aoMudar(false)} disabled={enviando}>
            Voltar
          </Button>
          <Button
            variant={destrutivo ? "destructive" : "default"}
            onClick={confirmar}
            disabled={enviando}
            className="max-md:h-11 max-md:flex-1"
          >
            {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
            {rotuloConfirmar}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3 text-sm text-muted-foreground">{descricao}</div>
      {erro ? (
        <p role="alert" className="mt-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erro}
        </p>
      ) : null}
    </ModalShell>
  );
}
