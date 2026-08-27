"use client";

import * as React from "react";
import { Loader2 } from "lucide-react";

import { ModalShell } from "@/components/layout/modal-shell";
import { Button } from "@/components/ui/button";
import { mensagemDoErro, type Resultado } from "@/lib/acoes/resultado";

/**
 * Confirmação de ação irreversível-o-bastante.
 *
 * O erro aparece **dentro do modal**, e o modal não fecha: desativar um produto
 * ainda vendido devolve `409 RESOURCE_IN_USE`, e essa é exatamente a informação
 * que o usuário precisa ler no lugar onde clicou. Fechar e mostrar um aviso
 * longe do contexto transformaria uma recusa explicada em "não aconteceu nada".
 */
export function ModalDeConfirmacao({
  aberto,
  aoMudar,
  titulo,
  descricao,
  rotuloConfirmar = "Confirmar",
  destrutivo = false,
  aoConfirmar,
}: {
  aberto: boolean;
  aoMudar: (aberto: boolean) => void;
  titulo: string;
  descricao: React.ReactNode;
  rotuloConfirmar?: string;
  destrutivo?: boolean;
  aoConfirmar: () => Promise<Resultado<unknown>>;
}) {
  const [enviando, setEnviando] = React.useState(false);
  const [erro, setErro] = React.useState<string | null>(null);

  /**
   * O erro da tentativa anterior não pode sobreviver à reabertura: quem fecha e
   * abre de novo tem que ver a pergunta, não a recusa de antes.
   *
   * Ajuste **durante o render**, não num efeito. É o padrão que o React
   * documenta para "repor estado quando uma prop muda" e não custa o render em
   * cascata do efeito: o React reexecuta este componente antes de pintar
   * qualquer coisa, e nenhum filho chega a renderizar com o valor velho.
   *
   * Repor por `key` no chamador seria o caminho mais óbvio, mas remontaria o
   * `Dialog` — e a remontagem come a animação: o Base UI só marca
   * `data-starting-style` quando `open` passa de `false` para `true` num
   * componente **já montado** (`useTransitionStatus` inicia `mounted` com o
   * valor de `open`). Modal que aparece sem transição é regressão visível, e são
   * cinco chamadores.
   */
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
        setErro(mensagemDoErro(resultado.code));
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
            Cancelar
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
