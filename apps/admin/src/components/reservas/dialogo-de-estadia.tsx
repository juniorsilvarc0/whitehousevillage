"use client";

import * as React from "react";
import { DoorClosed, DoorOpen, Loader2 } from "lucide-react";

import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Recusa } from "@/components/reservas/recusa";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import type { Falha, Resultado } from "@/lib/acoes/resultado";
import { instanteDaOperacao } from "@/lib/crm/datas";
import { formatarData, hojeISO } from "@/lib/datas";
import type { RegistroDeEstadia, Reserva } from "@/lib/reservas/tipos";

/**
 * Check-in e check-out — o mesmo diálogo, duas consequências opostas.
 *
 * **Check-in não solta a data**: as linhas de `stay_blocks` continuam
 * `confirmed`, o hóspede está dentro e o calendário segue bloqueado. O que muda
 * é o estado comercial.
 *
 * **Check-out solta**: os blocos vão para `completed`, que sai da constraint (a
 * data libera na hora) e **continua visível no mapa** — estadia cumprida não é
 * estadia cancelada, e marcar assim faria quatro noites de receita real sumirem
 * do calendário que alimenta ocupação, ADR e RevPAR.
 *
 * ## A janela válida do `at`, e por que as duas pontas diferem
 *
 * O servidor valida a data do registro no fuso da casa: no check-in,
 * `[check_in, check_out)`; no check-out, `[check_in, check_out]`. O teto do
 * check-in é **estrito** porque a noite do check-out não pertence a esta
 * reserva — quem chega nela está fazendo check-in da reserva seguinte. O do
 * check-out é **inclusivo** porque é a manhã em que o hóspede sai: half-open
 * conta noites, não pessoas na porta.
 *
 * A tela avisa antes de enviar, mas quem recusa é o servidor: o registro é a
 * prova de quando a estadia aconteceu, e prova conferida só no cliente não é
 * prova. Medido em 26/08/2026, sem a faixa: um check-in aceito com
 * `at: 2019-01-01` gravou o evento sete anos antes de a reserva existir.
 */

export type TipoDeRegistro = "entrada" | "saida";

export type AlvoDeEstadia = Pick<Reserva, "id" | "code" | "contact_name" | "check_in" | "check_out">;

const TEXTO: Record<TipoDeRegistro, { titulo: string; botao: string; icone: typeof DoorOpen }> = {
  entrada: { titulo: "Registrar o check-in", botao: "Registrar check-in", icone: DoorOpen },
  saida: { titulo: "Registrar o check-out", botao: "Registrar check-out", icone: DoorClosed },
};

export function DialogoDeEstadia({
  tipo,
  controle,
  aoRegistrar,
  aoConcluir,
}: {
  tipo: TipoDeRegistro;
  controle: React.RefObject<ControleDeModal<AlvoDeEstadia> | null>;
  aoRegistrar: (id: string, entrada: RegistroDeEstadia) => Promise<Resultado<Reserva>>;
  aoConcluir?: () => void;
}) {
  const [aberto, setAberto] = React.useState(false);
  const [alvo, setAlvo] = React.useState<AlvoDeEstadia | null>(null);
  const [data, setData] = React.useState("");
  const [hora, setHora] = React.useState("");
  const [nota, setNota] = React.useState("");
  const [enviando, setEnviando] = React.useState(false);
  const [recusa, setRecusa] = React.useState<Falha | null>(null);

  React.useImperativeHandle(controle, () => ({
    abrir(escolhido: AlvoDeEstadia) {
      setAlvo(escolhido);
      // O padrão é hoje **na operação**, não no relógio de quem abriu a tela:
      // a gestão pode atender de outro fuso, e o dia da estadia é o da casa.
      setData(hojeISO());
      setHora("");
      setNota("");
      setRecusa(null);
      setAberto(true);
    },
  }), []);

  const foraDaJanela = alvo !== null && data !== "" && !dentroDaJanela(tipo, data, alvo);

  async function enviar() {
    if (!alvo) return;
    setEnviando(true);
    setRecusa(null);
    try {
      const resultado = await aoRegistrar(alvo.id, {
        ...(data ? { at: instanteDaOperacao(data, hora) } : {}),
        ...(nota.trim() ? { note: nota.trim() } : {}),
      });
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

  const { titulo, botao, icone: Icone } = TEXTO[tipo];

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title={titulo}
      description={alvo ? `${alvo.code} · ${alvo.contact_name}` : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={enviando}>
            Cancelar
          </Button>
          <Button onClick={() => void enviar()} disabled={enviando}>
            {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Icone aria-hidden="true" />}
            {botao}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {alvo ? (
          <p className="text-sm text-muted-foreground">
            Estadia vendida de {formatarData(alvo.check_in)} a {formatarData(alvo.check_out)} — a noite da
            saída não é cobrada.
          </p>
        ) : null}

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="estadia-data" label="Data do registro">
            {(props) => (
              <Input
                {...props}
                type="date"
                value={data}
                onChange={(evento) => setData(evento.target.value)}
                className="tabular-nums"
              />
            )}
          </Campo>
          <Campo id="estadia-hora" label="Hora" hint="Em branco assume 09:00, no fuso da casa.">
            {(props) => (
              <Input
                {...props}
                type="time"
                value={hora}
                onChange={(evento) => setHora(evento.target.value)}
                className="tabular-nums"
              />
            )}
          </Campo>
        </div>

        <Campo id="estadia-nota" label="Observação">
          {(props) => (
            <Textarea
              {...props}
              value={nota}
              onChange={(evento) => setNota(evento.target.value)}
              placeholder={tipo === "entrada" ? "Chegou de madrugada; chaves na portaria." : "Sem avarias."}
            />
          )}
        </Campo>

        {foraDaJanela && alvo ? (
          <Nota variante="atencao">
            {tipo === "entrada" ? (
              <>
                O servidor só aceita check-in entre {formatarData(alvo.check_in)} e a véspera de{" "}
                {formatarData(alvo.check_out)}. Entrar antes é ocupar noite que ninguém vendeu; a noite da
                saída já é da reserva seguinte. Com esta data, a resposta será recusa.
              </>
            ) : (
              <>
                O servidor só aceita check-out entre {formatarData(alvo.check_in)} e{" "}
                {formatarData(alvo.check_out)}, e nunca antes do check-in registrado. Com esta data, a
                resposta será recusa.
              </>
            )}
          </Nota>
        ) : null}

        <Nota>
          {tipo === "entrada" ? (
            <>
              O check-in <strong>não libera</strong> o calendário: o hóspede está dentro e a data segue
              bloqueada até a saída.
            </>
          ) : (
            <>
              O check-out <strong>libera as unidades na hora</strong> e a estadia continua visível no mapa —
              ela aconteceu, e é dela que saem ocupação e receita.
            </>
          )}
        </Nota>

        {recusa ? <Recusa falha={recusa} /> : null}
      </div>
    </ModalShell>
  );
}

/**
 * A mesma faixa que o servidor aplica, para a tela poder avisar antes.
 *
 * Comparação de strings `YYYY-MM-DD`, e não de `Date`: a data já vem no fuso da
 * casa e transformá-la em instante é como uma diária de 20/12 vira 19/12 para
 * metade da equipe.
 */
export function dentroDaJanela(
  tipo: TipoDeRegistro,
  data: string,
  reserva: Pick<Reserva, "check_in" | "check_out">,
): boolean {
  if (data < reserva.check_in) return false;
  return tipo === "entrada" ? data < reserva.check_out : data <= reserva.check_out;
}
