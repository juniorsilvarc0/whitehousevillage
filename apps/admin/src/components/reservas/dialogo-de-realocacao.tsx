"use client";

import * as React from "react";
import { ArrowLeftRight, Loader2 } from "lucide-react";

import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Recusa } from "@/components/reservas/recusa";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import type { UnidadeDaComposicao } from "@/lib/api/comercial";
import type { Falha, Resultado } from "@/lib/acoes/resultado";
import { formatarData } from "@/lib/datas";
import type { PedidoDeRealocacao, Reserva, UnidadeAlocada } from "@/lib/reservas/tipos";

/**
 * Trocar a unidade física — sem mexer em datas nem em preço.
 *
 * As unidades são nominais: trocar `AP-02` por `AP-01` na mesma estadia é
 * operação legítima, e é o que salva um conflito de canal sem cancelar ninguém.
 *
 * Numa transação, o bloco da unidade antiga fecha e o da nova entra no mesmo
 * período. Se a nova estiver ocupada, a constraint recusa e **nada muda** — a
 * reserva continua na unidade antiga. Esse caso é `UNIT_NOT_AVAILABLE`, e não
 * `DATE_CONFLICT`, porque a estadia não está em disputa: só a unidade está.
 */

export type AlvoDeRealocacao = Pick<Reserva, "id" | "code" | "contact_name" | "check_in" | "check_out"> & {
  unidades: UnidadeAlocada[];
};

export function DialogoDeRealocacao({
  controle,
  composicao,
  aoRealocar,
  aoConcluir,
}: {
  controle: React.RefObject<ControleDeModal<AlvoDeRealocacao> | null>;
  /** As unidades que compõem o produto da reserva (`unit_type_members`). O
   *  destino precisa estar aqui: fora da composição é `422`. */
  composicao: UnidadeDaComposicao[];
  aoRealocar: (id: string, entrada: PedidoDeRealocacao) => Promise<Resultado<Reserva>>;
  aoConcluir?: () => void;
}) {
  const [aberto, setAberto] = React.useState(false);
  const [alvo, setAlvo] = React.useState<AlvoDeRealocacao | null>(null);
  const [destino, setDestino] = React.useState("");
  const [travar, setTravar] = React.useState(true);
  const [motivo, setMotivo] = React.useState("");
  const [enviando, setEnviando] = React.useState(false);
  const [recusa, setRecusa] = React.useState<Falha | null>(null);

  React.useImperativeHandle(controle, () => ({
    abrir(escolhido: AlvoDeRealocacao) {
      setAlvo(escolhido);
      setDestino("");
      setTravar(true);
      setMotivo("");
      setRecusa(null);
      setAberto(true);
    },
  }), []);

  const atual = alvo?.unidades[0] ?? null;
  const candidatas = composicao.filter((u) => u.unit_id !== atual?.unit_id);
  const inativaEscolhida = candidatas.find((u) => u.unit_id === destino && !u.active) ?? null;

  async function enviar() {
    if (!alvo || !destino) return;
    setEnviando(true);
    setRecusa(null);
    try {
      const resultado = await aoRealocar(alvo.id, {
        ...(atual ? { from_unit_id: atual.unit_id } : {}),
        to_unit_id: destino,
        locked: travar,
        ...(motivo.trim() ? { reason: motivo.trim() } : {}),
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

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title="Trocar o apartamento"
      description={alvo ? `${alvo.code} · ${alvo.contact_name}` : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={enviando}>
            Cancelar
          </Button>
          <Button onClick={() => void enviar()} disabled={enviando || destino === ""}>
            {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <ArrowLeftRight aria-hidden="true" />}
            Trocar apartamento
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {alvo && atual ? (
          <p className="text-sm text-muted-foreground">
            Hoje em <strong className="font-mono text-foreground">{atual.unit_code}</strong> ({atual.unit_name}),
            de {formatarData(alvo.check_in)} a {formatarData(alvo.check_out)}. Datas e preço não mudam.
          </p>
        ) : null}

        <Campo
          id="realocar-destino"
          label="Novo apartamento"
          obrigatorio
          hint="Só aparecem os apartamentos que fazem parte deste produto."
        >
          {(props) => (
            <Select {...props} value={destino} onChange={(evento) => setDestino(evento.target.value)}>
              <option value="">Escolha o apartamento…</option>
              {candidatas.map((unidade) => (
                <option key={unidade.unit_id} value={unidade.unit_id}>
                  {unidade.unit_code} — {unidade.unit_name}
                  {unidade.active ? "" : " (desativado)"}
                </option>
              ))}
            </Select>
          )}
        </Campo>

        {candidatas.length === 0 ? (
          <Nota variante="atencao">
            Este produto não tem outro apartamento para onde trocar. Se o apartamento atual não pode ser
            usado, o caminho é remarcar ou cancelar.
          </Nota>
        ) : null}

        {inativaEscolhida ? (
          <Nota variante="atencao">
            {inativaEscolhida.unit_code} está <strong>desativado</strong> no inventário. Mover a estadia para
            ele deixa a reserva num apartamento que a casa marcou como fora de uso.
          </Nota>
        ) : null}

        <CheckboxCampo
          id="realocar-travar"
          label="Fixar este apartamento"
          hint="Use quando o hóspede pediu aquele apartamento. Sem fixar, o sistema pode trocá-lo sozinho mais tarde para organizar o calendário."
          checked={travar}
          onChange={(evento) => setTravar(evento.target.checked)}
        />

        <Campo id="realocar-motivo" label="Motivo" hint="Fica registrado no histórico da reserva, com o seu nome.">
          {(props) => (
            <Input
              {...props}
              value={motivo}
              onChange={(evento) => setMotivo(evento.target.value)}
              placeholder="Ar-condicionado em manutenção no AP-02"
            />
          )}
        </Campo>

        <Nota>
          Se o apartamento escolhido estiver ocupado no período, a troca não acontece e{" "}
          <strong>nada muda</strong>: a reserva continua onde está.
        </Nota>

        {recusa ? (
          <Recusa falha={recusa} garantia="A reserva continua no mesmo apartamento." />
        ) : null}
      </div>
    </ModalShell>
  );
}
