"use client";

import * as React from "react";
import { CheckCircle2, Loader2 } from "lucide-react";

import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Recusa } from "@/components/reservas/recusa";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import type { Falha, Resultado } from "@/lib/acoes/resultado";
import { centavosDeTexto, formatarBRL, reaisDeCentavos } from "@/lib/dinheiro";
import { novaChaveDeIdempotencia } from "@/lib/crm/idempotencia";
import type { ConfirmacaoDeReserva, MeioDePagamento, Reserva } from "@/lib/reservas/tipos";

/**
 * Confirmar a reserva — o que a tela precisa dizer antes.
 *
 * Confirmar não é "marcar como ok": numa transação, o sinal entra na linha do
 * tempo, as linhas de `stay_blocks` sobem de `hold` para `confirmed` e a data
 * **deixa de ter prazo de validade**. Quem clica está trocando uma reserva que
 * expira sozinha por uma que só sai com política de cancelamento.
 *
 * ## O teto do valor recebido é o **total**, e ele é dito na tela
 *
 * `1 ≤ deposit_paid_cents ≤ total_cents`: pagar a estadia inteira adiantado é
 * caso comum, e recusar isso obrigaria a gestão a mentir o valor. O piso é 1
 * porque confirmar com zero é o que o `hold` já faz, com prazo.
 *
 * Não é validação decorativa, e a tela repete o motivo: **este valor é a base do
 * reembolso** se a reserva for cancelada depois. Um dígito a mais aqui vira
 * dinheiro inventado lá — medido em 26/08/2026, um sinal de R$ 99.999.999,99
 * gerou promessa de R$ 50.000.000,00 de devolução sobre uma diária de fim de
 * semana, com a retenção de 50% calculada certinho sobre a base errada.
 */

export type AlvoDeConfirmacao = Pick<Reserva, "id" | "code" | "contact_name" | "deposit_cents" | "total_cents">;

const MEIOS: { valor: MeioDePagamento; rotulo: string }[] = [
  { valor: "pix", rotulo: "Pix" },
  { valor: "cartao", rotulo: "Cartão" },
  { valor: "transferencia", rotulo: "Transferência" },
  { valor: "dinheiro", rotulo: "Dinheiro" },
];

export function DialogoDeConfirmacao({
  controle,
  aoConfirmar,
  aoConcluir,
}: {
  controle: React.RefObject<ControleDeModal<AlvoDeConfirmacao> | null>;
  aoConfirmar: (id: string, entrada: ConfirmacaoDeReserva, chave: string) => Promise<Resultado<Reserva>>;
  aoConcluir?: () => void;
}) {
  const [aberto, setAberto] = React.useState(false);
  const [alvo, setAlvo] = React.useState<AlvoDeConfirmacao | null>(null);
  const [valor, setValor] = React.useState("");
  const [meio, setMeio] = React.useState<MeioDePagamento>("pix");
  const [nota, setNota] = React.useState("");
  const [enviando, setEnviando] = React.useState(false);
  const [recusa, setRecusa] = React.useState<Falha | null>(null);

  /**
   * A chave de idempotência nasce quando o diálogo **abre** e sobrevive a todas
   * as tentativas dele. Gerada por tentativa, ela seria um identificador novo a
   * cada clique — o mesmo que não existir —, e o duplo clique registraria o
   * sinal duas vezes.
   */
  const chave = React.useRef("");

  React.useImperativeHandle(controle, () => ({
    abrir(escolhido: AlvoDeConfirmacao) {
      setAlvo(escolhido);
      setValor(reaisDeCentavos(escolhido.deposit_cents));
      setMeio("pix");
      setNota("");
      setRecusa(null);
      chave.current = novaChaveDeIdempotencia();
      setAberto(true);
    },
  }), []);

  const centavos = centavosDeTexto(valor);
  const acimaDoTotal = alvo !== null && centavos !== null && centavos > alvo.total_cents;
  const invalido = centavos === null || centavos < 1 || acimaDoTotal;
  const erro =
    centavos === null || centavos < 1
      ? "Informe o valor recebido. Para guardar as datas sem pagamento, use a pré-reserva."
      : acimaDoTotal
        ? `O recebido não pode passar do total da reserva (${formatarBRL(alvo!.total_cents)}).`
        : undefined;

  async function enviar() {
    if (!alvo || invalido || centavos === null) return;
    setEnviando(true);
    setRecusa(null);
    try {
      const resultado = await aoConfirmar(
        alvo.id,
        {
          deposit_paid_cents: centavos,
          method: meio,
          ...(nota.trim() ? { note: nota.trim() } : {}),
        },
        chave.current,
      );
      if (!resultado.ok) {
        setRecusa(resultado);
        return;
      }
      setAberto(false);
      aoConcluir?.();
    } finally {
      setEnviando(false);
    }
  }

  const quitaTudo = alvo !== null && centavos !== null && centavos >= alvo.total_cents;

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title="Registrar o sinal e confirmar"
      description={alvo ? `${alvo.code} · ${alvo.contact_name}` : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={enviando}>
            Cancelar
          </Button>
          <Button onClick={() => void enviar()} disabled={enviando || invalido}>
            {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <CheckCircle2 aria-hidden="true" />}
            Confirmar reserva
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {alvo ? (
          <div className="flex items-baseline justify-between gap-3 rounded-xl border border-border/60 bg-muted/20 px-3 py-2.5 text-sm">
            <span className="text-muted-foreground">Sinal calculado pela política</span>
            <span className="font-mono tabular-nums">{formatarBRL(alvo.deposit_cents)}</span>
          </div>
        ) : null}

        <Campo
          id="confirmar-valor"
          label="Valor recebido (R$)"
          obrigatorio
          erro={erro}
          hint={
            alvo
              ? `De R$ 0,01 até o total da reserva (${formatarBRL(alvo.total_cents)}). Se a reserva for cancelada depois, a devolução é calculada sobre este valor.`
              : undefined
          }
        >
          {(props) => (
            <Input
              {...props}
              value={valor}
              onChange={(evento) => setValor(evento.target.value)}
              inputMode="decimal"
              className="text-right font-mono tabular-nums"
            />
          )}
        </Campo>

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="confirmar-meio" label="Meio">
            {(props) => (
              <Select {...props} value={meio} onChange={(evento) => setMeio(evento.target.value as MeioDePagamento)}>
                {MEIOS.map((item) => (
                  <option key={item.valor} value={item.valor}>
                    {item.rotulo}
                  </option>
                ))}
              </Select>
            )}
          </Campo>
        </div>

        <Campo id="confirmar-nota" label="Observação">
          {(props) => (
            <Textarea
              {...props}
              value={nota}
              onChange={(evento) => setNota(evento.target.value)}
              placeholder="Comprovante enviado por WhatsApp."
            />
          )}
        </Campo>

        {quitaTudo && !invalido ? (
          <Nota>
            O valor cobre o total da estadia. Tudo bem: o saldo fica zerado e, num cancelamento, a devolução
            é calculada sobre tudo o que foi pago.
          </Nota>
        ) : null}

        <Nota variante="atencao">
          Ao confirmar, a reserva deixa de ser <strong>pré-reserva</strong> e passa a <strong>confirmada</strong>:
          as datas ficam garantidas, sem prazo para vencer. A partir daí, liberar as datas só pelo cancelamento,
          com as regras que valiam no dia da reserva.
        </Nota>

        {recusa ? <Recusa falha={recusa} /> : null}
      </div>
    </ModalShell>
  );
}
