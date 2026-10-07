"use client";

import * as React from "react";
import { Loader2 } from "lucide-react";

import { ModalShell } from "@/components/layout/modal-shell";
import { Button } from "@/components/ui/button";
import type { Resultado } from "@/lib/acoes/resultado";
import { mensagemDeBens, type Contexto } from "@/lib/bens/mensagens";

/**
 * Confirmação de gesto destrutivo do inventário.
 *
 * É o `ModalDeConfirmacao` da casca com **um** desvio: a recusa é traduzida
 * pelo dicionário do inventário (`mensagemDeBens`). O geral diz, para
 * `RESOURCE_IN_USE`, "ainda há reservas usando isto" — e aqui o que segura um
 * cômodo ou um bem é conferência, avaria e colocação, com a saída certa
 * (desativar) escrita junto. Mesma regra da casca: o erro aparece **dentro** do
 * modal e ele não fecha.
 */
export function ConfirmacaoDoInventario({
  aberto,
  aoMudar,
  titulo,
  descricao,
  rotuloConfirmar = "Confirmar",
  destrutivo = false,
  contexto,
  aoConfirmar,
}: {
  aberto: boolean;
  aoMudar: (aberto: boolean) => void;
  titulo: string;
  descricao: React.ReactNode;
  rotuloConfirmar?: string;
  destrutivo?: boolean;
  contexto?: Contexto;
  aoConfirmar: () => Promise<Resultado<unknown>>;
}) {
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
      const resultado = await aoConfirmar();
      if (!resultado.ok) {
        setErro(mensagemDeBens(resultado, contexto));
        return;
      }
      aoMudar(false);
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
          <Button variant={destrutivo ? "destructive" : "default"} onClick={confirmar} disabled={enviando}>
            {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
            {rotuloConfirmar}
          </Button>
        </>
      }
    >
      <div className="text-sm text-muted-foreground">{descricao}</div>
      {erro ? (
        <p role="alert" className="mt-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erro}
        </p>
      ) : null}
    </ModalShell>
  );
}
