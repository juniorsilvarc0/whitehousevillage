"use client";

import * as React from "react";
import { CalendarCheck, Loader2, Undo2, XCircle } from "lucide-react";

import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Recusa } from "@/components/reservas/recusa";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import type { Resultado } from "@/lib/acoes/resultado";
import { formatarData } from "@/lib/datas";
import { formatarBRL } from "@/lib/dinheiro";
import { ESTADOS } from "@/lib/reservas/estados";
import type { ResultadoDeCancelamento } from "@/lib/reservas/tipos";
import { cn } from "@/lib/utils";

/**
 * Cancelar — **com o número na frente antes do clique**.
 *
 * A política de cancelamento é a que a reserva **congelou**, não a vigente hoje,
 * e a conta muda com a antecedência e com o sinal efetivamente recebido. Quem
 * cancela precisa dizer ao hóspede, no telefone, quanto volta — e descobrir isso
 * depois de executar não é descobrir: é ter cancelado no escuro.
 *
 * Por isso o diálogo abre chamando `?dry_run=1`, que **calcula e não executa
 * nada**, e o botão de confirmar só liga quando a simulação chega.
 *
 * ## O motivo entra na simulação, e é obrigatório
 *
 * `no_show` não é um rótulo: ele aplica a faixa de menor antecedência (retenção
 * integral) e termina a reserva no estado `no_show`, não em `cancelled`. Medido
 * em 27/08/2026 na `WH-2026-0001` (85 dias de antecedência): sem motivo, a
 * simulação devolveu `refund 352500 / retained 0`; com `no_show`, `refund 0 /
 * retained 352500` — R$ 3.525,00 de diferença decididos por um campo que a tela
 * poderia ter deixado em branco.
 *
 * O motivo é exigido antes de confirmar exatamente por isso: assim o número que
 * o operador leu é o número que vai acontecer, e o relatório de por que a casa
 * perde estadia não nasce com metade das linhas vazias.
 *
 * `remarcacao` **não** é oferecido: é o que a própria API grava ao remarcar, e
 * escolhê-lo à mão contaria uma remarcação que não houve.
 */

export type AlvoDeCancelamento = {
  id: string;
  code: string;
  contato: string;
  check_in: string;
  check_out: string;
  /** Prévia que veio junto do `/full`. Preenche a tela no primeiro quadro, mas
   *  não substitui a simulação: ela foi calculada sem motivo nenhum. */
  previa: ResultadoDeCancelamento | null;
};

export const MOTIVOS: { valor: string; rotulo: string }[] = [
  { valor: "desistencia", rotulo: "Desistência do hóspede" },
  { valor: "no_show", rotulo: "Não compareceu (no-show)" },
  { valor: "alteracao_de_planos", rotulo: "Alteração de planos" },
  { valor: "problema_de_pagamento", rotulo: "Problema de pagamento" },
  { valor: "erro_de_lancamento", rotulo: "Erro de lançamento" },
  { valor: "outro", rotulo: "Outro (descrever)" },
];

/** Atraso curto antes de simular: o campo "outro" muda a cada tecla e uma
 *  requisição por tecla entupiria a rota para mostrar números que ninguém lê. */
const ATRASO_MS = 350;

export function DialogoDeCancelamento({
  controle,
  aoSimular,
  aoCancelar,
  aoConcluir,
}: {
  controle: React.RefObject<ControleDeModal<AlvoDeCancelamento> | null>;
  aoSimular: (id: string, motivo: string) => Promise<Resultado<ResultadoDeCancelamento>>;
  aoCancelar: (id: string, motivo: string) => Promise<Resultado<ResultadoDeCancelamento>>;
  aoConcluir?: (resultado: ResultadoDeCancelamento) => void;
}) {
  const [aberto, setAberto] = React.useState(false);
  const [alvo, setAlvo] = React.useState<AlvoDeCancelamento | null>(null);
  const [motivo, setMotivo] = React.useState("");
  const [descricao, setDescricao] = React.useState("");
  const [simulacao, setSimulacao] = React.useState<Resultado<ResultadoDeCancelamento> | null>(null);
  const [simulando, setSimulando] = React.useState(false);
  const [executando, setExecutando] = React.useState(false);
  const [falhaDaExecucao, setFalhaDaExecucao] = React.useState<Resultado<unknown> | null>(null);

  // Repor no evento que abre, nunca num efeito: o modal fica montado para não
  // perder a animação, e um efeito reporia depois de já ter renderizado com os
  // dados da reserva anterior.
  React.useImperativeHandle(controle, () => ({
    abrir(escolhido: AlvoDeCancelamento) {
      setAlvo(escolhido);
      setMotivo("");
      setDescricao("");
      setSimulacao(escolhido.previa ? { ok: true, data: escolhido.previa } : null);
      setFalhaDaExecucao(null);
      setAberto(true);
    },
  }), []);

  const motivoEfetivo = motivo === "outro" ? descricao.trim() : motivo;

  /**
   * A simulação, com o mesmo desenho do `QuoteBuilder`: o efeito só **agenda** e
   * cancela a anterior; nada de `setState` síncrono dentro dele. `cancelado`
   * impede que a resposta de um motivo já trocado pinte a tela por cima do
   * pedido novo — que é como a pessoa leria o número do `no_show` depois de ter
   * voltado para "desistência".
   */
  React.useEffect(() => {
    if (!aberto || !alvo) return;

    let cancelado = false;
    const temporizador = setTimeout(async () => {
      setSimulando(true);
      try {
        const resultado = await aoSimular(alvo.id, motivoEfetivo);
        if (!cancelado) setSimulacao(resultado);
      } finally {
        if (!cancelado) setSimulando(false);
      }
    }, ATRASO_MS);

    return () => {
      cancelado = true;
      clearTimeout(temporizador);
    };
  }, [aberto, alvo, motivoEfetivo, aoSimular]);

  async function executar() {
    if (!alvo) return;
    setExecutando(true);
    setFalhaDaExecucao(null);
    try {
      const resultado = await aoCancelar(alvo.id, motivoEfetivo);
      if (!resultado.ok) {
        setFalhaDaExecucao(resultado);
        return;
      }
      setAberto(false);
      aoConcluir?.(resultado.data);
    } finally {
      setExecutando(false);
    }
  }

  const previsao = simulacao?.ok ? simulacao.data : null;
  const motivoDescrito = motivo !== "" && (motivo !== "outro" || descricao.trim() !== "");
  const podeConfirmar = Boolean(previsao) && !simulando && !executando && motivoDescrito;

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title="Cancelar a reserva"
      description={alvo ? `${alvo.code} · ${alvo.contato}` : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={executando}>
            Voltar sem cancelar
          </Button>
          <Button variant="destructive" onClick={() => void executar()} disabled={!podeConfirmar}>
            {executando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <XCircle aria-hidden="true" />}
            {previsao?.status === "no_show" ? "Registrar não comparecimento" : "Cancelar a reserva"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {alvo ? (
          <p className="text-sm text-muted-foreground">
            Estadia de {formatarData(alvo.check_in)} a {formatarData(alvo.check_out)}.
          </p>
        ) : null}

        <Campo
          id="cancel-motivo"
          label="Motivo"
          obrigatorio
          hint="O motivo muda a conta: “não compareceu”, por exemplo, faz a casa ficar com todo o sinal."
        >
          {(props) => (
            <Select {...props} value={motivo} onChange={(evento) => setMotivo(evento.target.value)}>
              <option value="">Escolha o motivo…</option>
              {MOTIVOS.map((item) => (
                <option key={item.valor} value={item.valor}>
                  {item.rotulo}
                </option>
              ))}
            </Select>
          )}
        </Campo>

        {motivo === "outro" ? (
          <Campo id="cancel-descricao" label="Qual" obrigatorio>
            {(props) => (
              <Input
                {...props}
                value={descricao}
                onChange={(evento) => setDescricao(evento.target.value)}
                placeholder="Obra no prédio vizinho"
              />
            )}
          </Campo>
        ) : null}

        <PrevisaoDoCancelamento previsao={previsao} simulando={simulando} />

        {simulacao && !simulacao.ok ? (
          <Recusa
            falha={simulacao}
            garantia="Nada mudou na reserva — isto era só a simulação."
          />
        ) : null}

        {falhaDaExecucao && !falhaDaExecucao.ok ? (
          <Recusa falha={falhaDaExecucao} garantia="A reserva continua exatamente como estava." />
        ) : null}

        {previsao ? (
          <Nota variante="atencao">
            Ao confirmar, as datas desta reserva <strong>voltam a ficar livres para venda na hora</strong> e o
            cancelamento entra no histórico com o motivo.
          </Nota>
        ) : null}
      </div>
    </ModalShell>
  );
}

/**
 * O quadro que a tela existe para mostrar: quanto volta, quanto fica.
 *
 * Os três números vêm inteiros do servidor — `refund_cents`, `retained_cents` e
 * `credit_cents` —, e a soma deles é o que o hóspede pagou. O painel não
 * subtrai nada aqui: a base do cálculo é o sinal efetivamente recebido, e uma
 * conta local sobre um valor herdado de remarcação erra em silêncio.
 */
function PrevisaoDoCancelamento({
  previsao,
  simulando,
}: {
  previsao: ResultadoDeCancelamento | null;
  simulando: boolean;
}) {
  if (!previsao) {
    return (
      <div
        aria-live="polite"
        className="flex min-h-[7rem] items-center justify-center rounded-xl border border-dashed border-border/70 bg-muted/20 px-4 text-center"
      >
        {simulando ? (
          <Loader2 className="size-5 animate-spin text-muted-foreground" aria-label="Simulando o cancelamento" />
        ) : (
          <p className="text-sm text-muted-foreground">
            Escolha o motivo para ver quanto volta para o hóspede e quanto fica com a casa.
          </p>
        )}
      </div>
    );
  }

  return (
    <div
      aria-live="polite"
      // Esmaece enquanto a simulação nova não chega — nunca vira esqueleto:
      // trocar o número por um bloco cinza faz o operador perder o valor que
      // estava lendo em voz alta ao telefone.
      className={cn(
        "rounded-xl border border-border/60 bg-muted/20 px-4 py-3 transition-opacity",
        simulando && "opacity-60",
      )}
    >
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 className="font-display text-base">{previsao.label}</h3>
        <span className="text-xs text-muted-foreground">
          {previsao.days_before >= 0
            ? `${previsao.days_before} dias de antecedência`
            : `${Math.abs(previsao.days_before)} dias depois do check-in`}{" "}
          · política v{previsao.policy_version}
        </span>
      </div>

      <dl className="mt-3 grid gap-3 sm:grid-cols-2">
        <div className="rounded-lg border border-alcada-livre/35 bg-alcada-livre/10 px-3 py-2.5">
          <dt className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Undo2 className="size-3.5" aria-hidden="true" />
            Volta para o hóspede
          </dt>
          <dd className="mt-0.5 text-right font-mono text-xl tabular-nums">{formatarBRL(previsao.refund_cents)}</dd>
        </div>
        <div className="rounded-lg border border-border/60 bg-card px-3 py-2.5">
          <dt className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <CalendarCheck className="size-3.5" aria-hidden="true" />
            Fica com a casa
          </dt>
          <dd className="mt-0.5 text-right font-mono text-xl tabular-nums">{formatarBRL(previsao.retained_cents)}</dd>
        </div>
      </dl>

      <p className="mt-2 text-xs text-muted-foreground">
        {previsao.deposit_paid_cents === 0 ? (
          // O caso da pré-reserva: `refund 0 / retained 0` com a faixa dizendo
          // "devolução integral" se lê como se a casa estivesse ficando com o
          // dinheiro. Não há dinheiro — e é isso que precisa estar escrito.
          <>Nada foi recebido ainda, então não há o que devolver nem o que reter. A regra acima é a que valeria se houvesse sinal.</>
        ) : (
          <>
            Base do cálculo: sinal recebido de{" "}
            <span className="font-mono tabular-nums text-foreground">{formatarBRL(previsao.deposit_paid_cents)}</span>.
          </>
        )}
      </p>

      {previsao.credit_cents > 0 ? (
        <p className="mt-2 rounded-lg bg-alcada-atencao/12 px-3 py-2 text-xs leading-relaxed">
          Além disso, há{" "}
          <strong className="font-mono tabular-nums">{formatarBRL(previsao.credit_cents)}</strong> de{" "}
          <strong>crédito em aberto</strong> do hóspede — dinheiro que ele já pagou e que este cancelamento
          não resolve. Devolução, valor retido e crédito somados dão tudo o que ele pagou: a conta com ele
          ainda não está fechada.
        </p>
      ) : null}

      <p className="mt-2 text-xs text-muted-foreground">
        A reserva termina em <strong className="text-foreground">{ESTADOS[previsao.status].rotulo}</strong>.{" "}
        {ESTADOS[previsao.status].explicacao}
      </p>
    </div>
  );
}
