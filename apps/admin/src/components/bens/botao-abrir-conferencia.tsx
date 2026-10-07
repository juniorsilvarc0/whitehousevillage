"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ClipboardCheck, Loader2 } from "lucide-react";

import { notificarInfo } from "@/components/bens/avisos";
import { AvisoGeral } from "@/components/bens/formulario-comum";
import { ModalShell } from "@/components/layout/modal-shell";
import { Button, buttonVariants } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Textarea } from "@/components/ui/textarea";
import { abrirConferencia } from "@/lib/bens/acoes";
import { caminhoDaConferencia, conferenciaJaAberta, mensagemDeBens } from "@/lib/bens/mensagens";
import { formatarInstante } from "@/lib/datas";
import { cn } from "@/lib/utils";

/**
 * Abrir a conferência da unidade — ou continuar a que já está aberta.
 *
 * ## O segundo toque não vira beco
 *
 * O banco garante **uma** conferência aberta por unidade, e a segunda abertura
 * é `409 COUNT_ALREADY_OPEN` com `details.count_id`. É o caso comum, não o
 * raro: duas pessoas no mesmo plantão, ou a mesma pessoa com a tela velha no
 * celular. A resposta certa é levar à conferência que já existe — com um aviso
 * de que ela já estava aberta, para ninguém achar que começou do zero.
 */
export function BotaoAbrirConferencia({
  unitId,
  unidadeRotulo,
  abertaId,
  tamanho = "md",
}: {
  unitId: string;
  unidadeRotulo: string;
  /** A conferência aberta que a tela já conhece. Se houver, o botão continua. */
  abertaId?: string | null;
  tamanho?: "sm" | "md";
}) {
  const router = useRouter();
  const [aberto, setAberto] = React.useState(false);
  const [nota, setNota] = React.useState("");
  const [enviando, setEnviando] = React.useState(false);
  const [erro, setErro] = React.useState<string | null>(null);

  if (abertaId) {
    return (
      <Link href={caminhoDaConferencia(abertaId)} className={cn(buttonVariants({ size: tamanho }))}>
        <ClipboardCheck aria-hidden="true" />
        Continuar conferência
      </Link>
    );
  }

  async function abrir() {
    setEnviando(true);
    setErro(null);
    try {
      const r = await abrirConferencia(unitId, nota);
      if (r.ok) {
        setAberto(false);
        router.push(caminhoDaConferencia(r.data.id));
        return;
      }
      const existente = conferenciaJaAberta(r);
      if (existente) {
        setAberto(false);
        notificarInfo(
          "Esta unidade já tinha uma conferência em andamento",
          existente.abertaEm
            ? `Aberta em ${formatarInstante(existente.abertaEm)}. Abrindo ela para você continuar.`
            : "Abrindo ela para você continuar.",
        );
        router.push(existente.caminho);
        return;
      }
      setErro(mensagemDeBens(r, "conferencia"));
    } finally {
      setEnviando(false);
    }
  }

  return (
    <>
      <Button
        size={tamanho}
        onClick={() => {
          setNota("");
          setErro(null);
          setAberto(true);
        }}
      >
        <ClipboardCheck aria-hidden="true" />
        Abrir conferência
      </Button>

      <ModalShell
        open={aberto}
        onOpenChange={setAberto}
        title={`Conferir ${unidadeRotulo}`}
        description="Quem conta caminha cômodo a cômodo pelo celular."
        footer={
          <>
            <Button variant="ghost" onClick={() => setAberto(false)} disabled={enviando}>
              Voltar
            </Button>
            <Button onClick={abrir} disabled={enviando}>
              {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <ClipboardCheck aria-hidden="true" />}
              Abrir e começar a contar
            </Button>
          </>
        }
      >
        <AvisoGeral mensagem={erro} />
        <div className="flex flex-col gap-4 text-sm text-muted-foreground">
          <p>
            A conferência guarda <strong>agora</strong> quanto se espera de cada bem em cada ambiente. Se o padrão da
            casa mudar depois, esta contagem continua comparando com o que se esperava hoje.
          </p>
          <Campo id="conferencia-nota" label="Observação (opcional)" hint="Por exemplo: “check-out da reserva WH-2026-0142”.">
            {(p) => <Textarea {...p} rows={2} value={nota} onChange={(e) => setNota(e.target.value)} maxLength={2000} />}
          </Campo>
        </div>
      </ModalShell>
    </>
  );
}
