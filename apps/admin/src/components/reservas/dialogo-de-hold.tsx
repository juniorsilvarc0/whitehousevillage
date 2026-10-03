"use client";

import * as React from "react";
import { Clock, Loader2 } from "lucide-react";

import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Recusa } from "@/components/reservas/recusa";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import type { Falha, Resultado } from "@/lib/acoes/resultado";
import { formatarInstante } from "@/lib/datas";
import type { PedidoDeExtensaoDeHold, Reserva, ResultadoDeExtensaoDeHold } from "@/lib/reservas/tipos";

/**
 * Estender o prazo da pré-reserva — **ação explícita e auditada**.
 *
 * Cada extensão entra em `reservation_events` com autor e prazo novo. O prazo é
 * contado **a partir de agora**, não do vencimento antigo: hold vencido não se
 * conserta para trás, e somar horas ao passado devolveria uma data que o job já
 * pode ter liberado para outra pessoa.
 *
 * O diálogo não fecha no sucesso: mostra quantas extensões restam. É a única
 * informação que decide a próxima conversa — pedir mais um dia ou cobrar o
 * sinal —, e ela chega só na resposta.
 */

export type AlvoDeExtensao = Pick<Reserva, "id" | "code" | "contact_name" | "hold_expires_at">;

export function DialogoDeHold({
  controle,
  aoEstender,
  aoConcluir,
}: {
  controle: React.RefObject<ControleDeModal<AlvoDeExtensao> | null>;
  aoEstender: (id: string, entrada: PedidoDeExtensaoDeHold) => Promise<Resultado<ResultadoDeExtensaoDeHold>>;
  aoConcluir?: () => void;
}) {
  const [aberto, setAberto] = React.useState(false);
  const [alvo, setAlvo] = React.useState<AlvoDeExtensao | null>(null);
  const [horas, setHoras] = React.useState("");
  const [motivo, setMotivo] = React.useState("");
  const [enviando, setEnviando] = React.useState(false);
  const [recusa, setRecusa] = React.useState<Falha | null>(null);
  const [sucesso, setSucesso] = React.useState<ResultadoDeExtensaoDeHold | null>(null);

  React.useImperativeHandle(controle, () => ({
    abrir(escolhido: AlvoDeExtensao) {
      setAlvo(escolhido);
      setHoras("");
      setMotivo("");
      setRecusa(null);
      setSucesso(null);
      setAberto(true);
    },
  }), []);

  const horasNumero = horas.trim() === "" ? null : Number.parseInt(horas.trim(), 10);
  const horasInvalidas = horas.trim() !== "" && (!Number.isFinite(horasNumero) || (horasNumero ?? 0) < 1);

  async function enviar() {
    if (!alvo || horasInvalidas) return;
    setEnviando(true);
    setRecusa(null);
    try {
      const resultado = await aoEstender(alvo.id, {
        ...(horasNumero !== null ? { hours: horasNumero } : {}),
        ...(motivo.trim() ? { reason: motivo.trim() } : {}),
      });
      if (!resultado.ok) {
        setRecusa(resultado);
        return;
      }
      setSucesso(resultado.data);
      aoConcluir?.();
    } finally {
      setEnviando(false);
    }
  }

  const restantes = sucesso ? Math.max(sucesso.max_extensions - sucesso.extensions_count, 0) : 0;

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title="Estender a pré-reserva"
      description={alvo ? `${alvo.code} · ${alvo.contact_name}` : undefined}
      footer={
        sucesso ? (
          <Button onClick={() => setAberto(false)}>Fechar</Button>
        ) : (
          <>
            <Button variant="ghost" onClick={() => setAberto(false)} disabled={enviando}>
              Cancelar
            </Button>
            <Button onClick={() => void enviar()} disabled={enviando || horasInvalidas}>
              {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Clock aria-hidden="true" />}
              Estender prazo
            </Button>
          </>
        )
      }
    >
      <div className="flex flex-col gap-4">
        {alvo?.hold_expires_at && !sucesso ? (
          <div className="flex items-baseline justify-between gap-3 rounded-xl border border-border/60 bg-muted/20 px-3 py-2.5 text-sm">
            <span className="text-muted-foreground">Prazo atual</span>
            <span className="font-mono tabular-nums">{formatarInstante(alvo.hold_expires_at)}</span>
          </div>
        ) : null}

        {sucesso ? (
          <div className="rounded-xl border border-alcada-livre/35 bg-alcada-livre/10 px-4 py-3">
            <p className="text-sm">
              Prazo novo:{" "}
              <strong className="font-mono tabular-nums">{formatarInstante(sucesso.hold_expires_at)}</strong>
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {restantes === 0 ? (
                <>
                  Esta foi a <strong>última</strong> extensão permitida ({sucesso.extensions_count} de{" "}
                  {sucesso.max_extensions}). Agora é preciso confirmar com o sinal ou liberar as datas.
                </>
              ) : (
                <>
                  {sucesso.extensions_count} de {sucesso.max_extensions} extensões usadas — resta
                  {restantes === 1 ? "" : "m"} {restantes}.
                </>
              )}
            </p>
          </div>
        ) : (
          <>
            <Campo
              id="hold-horas"
              label="Horas a partir de agora"
              erro={horasInvalidas ? "Informe um número inteiro de horas, a partir de 1." : undefined}
              hint="Em branco, usa o prazo padrão da política desta reserva. A contagem começa agora, não no vencimento anterior."
            >
              {(props) => (
                <Input
                  {...props}
                  value={horas}
                  onChange={(evento) => setHoras(evento.target.value)}
                  inputMode="numeric"
                  placeholder="48"
                  className="text-right tabular-nums"
                />
              )}
            </Campo>

            <Campo id="hold-motivo" label="Motivo" hint="Fica registrado no histórico da reserva, com o seu nome.">
              {(props) => (
                <Input
                  {...props}
                  value={motivo}
                  onChange={(evento) => setMotivo(evento.target.value)}
                  placeholder="Hóspede pediu mais um dia para fechar"
                />
              )}
            </Campo>

            <Nota variante="atencao">
              Estender mantém as datas guardadas sem nenhum pagamento. Para não travar o calendário à toa, cada
              extensão fica registrada e há um limite de extensões.
            </Nota>
          </>
        )}

        {recusa ? <Recusa falha={recusa} /> : null}
      </div>
    </ModalShell>
  );
}
