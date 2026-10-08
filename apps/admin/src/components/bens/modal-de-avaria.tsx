"use client";

import * as React from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";

import { notificarSucesso } from "@/components/bens/avisos";
import { AvisoGeral, RodapeDoModal } from "@/components/bens/formulario-comum";
import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { aplicarFalha } from "@/lib/acoes/formulario";
import { registrarAvaria } from "@/lib/bens/acoes";
import { AvariaFormulario } from "@/lib/bens/esquemas";
import { ROTULO_DA_AVARIA } from "@/lib/bens/rotulos";
import { TIPOS_DE_AVARIA, type AmbienteDoInventario } from "@/lib/bens/tipos";

/** Abre já apontando para o cômodo e o bem, quando quem chama sabe. */
export type AlvoDaAvaria = { ambienteId?: string; itemId?: string };

/**
 * Registrar um bem quebrado, faltando ou avariado.
 *
 * A avaria aponta para o **bem do catálogo** num **ambiente** — e o bem tem de
 * estar colocado ali (o contrato recusa o contrário). Por isso o bem se escolhe
 * dentre os do cômodo escolhido, não do catálogo inteiro.
 *
 * O código da reserva é opcional e aparece para todo mundo que registra: é o
 * vínculo que permite cobrar o hóspede, e quem o resolve é a API — sem exigir
 * de quem conta a permissão de consultar reservas. Código que não existe volta
 * `422` com `details.reservation_code`, e a frase cai no próprio campo. A
 * avaria nunca mostra quem se hospedou: "o que quebrou nesta estadia" se
 * responde pelo código.
 */
export function ModalDeAvaria({
  controle,
  ambientes,
  unidadeRotulo,
}: {
  controle: React.RefObject<ControleDeModal<AlvoDaAvaria> | null>;
  ambientes: readonly AmbienteDoInventario[];
  unidadeRotulo: string;
}) {
  const [aberto, setAberto] = React.useState(false);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const form = useForm<AvariaFormulario>({
    resolver: zodResolver(AvariaFormulario),
    defaultValues: { room_id: "", item_id: "", kind: "quebrado", qty: "1", note: "", reservation_code: "" },
  });

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(alvo: AlvoDaAvaria) {
        form.reset({
          room_id: alvo.ambienteId ?? "",
          item_id: alvo.itemId ?? "",
          kind: "quebrado",
          qty: "1",
          note: "",
          reservation_code: "",
        });
        setErroGeral(null);
        setAberto(true);
      },
    }),
    [form],
  );

  const roomId = useWatch({ control: form.control, name: "room_id" });
  const itens = ambientes.find((a) => a.id === roomId)?.items ?? [];
  const { errors, isSubmitting } = form.formState;

  async function enviar(v: AvariaFormulario) {
    setErroGeral(null);
    const r = await registrarAvaria(v);
    if (!r.ok) {
      setErroGeral(aplicarFalha(r, form));
      return;
    }
    setAberto(false);
    notificarSucesso("Avaria registrada", "Ela fica na lista de pendências até alguém dar o desfecho.");
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title={`Registrar avaria em ${unidadeRotulo}`}
      description="Quebrado, faltando ou avariado — fica pendente até alguém dar o desfecho."
      footer={<RodapeDoModal formulario="form-avaria" enviando={isSubmitting} aoCancelar={() => setAberto(false)} rotulo="Registrar" />}
    >
      <AvisoGeral mensagem={erroGeral} />
      <form id="form-avaria" onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="avaria-room" label="Ambiente" obrigatorio erro={errors.room_id?.message}>
            {(p) => (
              <Select
                {...p}
                {...form.register("room_id", {
                  // Trocar de cômodo invalida o bem escolhido: ele era do outro.
                  onChange: () => form.setValue("item_id", ""),
                })}
              >
                <option value="">Escolha</option>
                {ambientes.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </Select>
            )}
          </Campo>
          <Campo id="avaria-item" label="Bem" obrigatorio erro={errors.item_id?.message}>
            {(p) => (
              <Select {...p} {...form.register("item_id")} disabled={!roomId}>
                <option value="">{roomId ? "Escolha" : "Escolha o ambiente antes"}</option>
                {itens.map((c) => (
                  <option key={c.item_id} value={c.item_id}>
                    {c.item_name ?? c.item_id}
                  </option>
                ))}
              </Select>
            )}
          </Campo>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="avaria-kind" label="O que aconteceu" obrigatorio erro={errors.kind?.message}>
            {(p) => (
              <Select {...p} {...form.register("kind")}>
                {TIPOS_DE_AVARIA.map((k) => (
                  <option key={k} value={k}>
                    {ROTULO_DA_AVARIA[k]}
                  </option>
                ))}
              </Select>
            )}
          </Campo>
          <Campo id="avaria-qty" label="Quantas peças" obrigatorio erro={errors.qty?.message}>
            {(p) => <Input {...p} {...form.register("qty")} inputMode="numeric" className="w-28 tabular-nums" />}
          </Campo>
        </div>

        <Campo
          id="avaria-reserva"
          label="Código da reserva (opcional)"
          erro={errors.reservation_code?.message}
          hint="Se aconteceu durante uma estadia — é o que permite cobrar. Desgaste e achado de rotina ficam sem."
        >
          {(p) => (
            <Input {...p} {...form.register("reservation_code")} placeholder="WH-2026-0142" autoCapitalize="characters" className="w-48 font-mono" />
          )}
        </Campo>

        <Campo id="avaria-note" label="Observação" erro={errors.note?.message}>
          {(p) => <Textarea {...p} rows={2} {...form.register("note")} />}
        </Campo>
      </form>
    </ModalShell>
  );
}
