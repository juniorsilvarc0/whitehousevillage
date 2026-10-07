"use client";

import * as React from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";

import { AvisoGeral, RodapeDoModal } from "@/components/bens/formulario-comum";
import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { aplicarFalha } from "@/lib/acoes/formulario";
import { salvarAmbiente } from "@/lib/bens/acoes";
import { AmbienteFormulario } from "@/lib/bens/esquemas";
import { mensagemDeBens } from "@/lib/bens/mensagens";
import { ROTULO_DO_AMBIENTE } from "@/lib/bens/rotulos";
import { TIPOS_DE_AMBIENTE, type Ambiente } from "@/lib/bens/tipos";

/** Abre com o ambiente a editar, ou com a ordem sugerida para um novo. */
export type AlvoDoAmbiente = { ambiente: Ambiente | null; ordemSugerida: number };

function valores(alvo: AlvoDoAmbiente | null): AmbienteFormulario {
  const a = alvo?.ambiente;
  return {
    name: a?.name ?? "",
    kind: a?.kind ?? "quarto",
    sort_order: String(a?.sort_order ?? alvo?.ordemSugerida ?? 0),
    active: a?.active ?? true,
  };
}

/**
 * Criar ou editar um ambiente (cômodo) da unidade.
 *
 * A unidade não se escolhe aqui e não se troca: cômodo não muda de unidade
 * (levaria junto a lista de bens e o histórico). Nome repetido na mesma unidade
 * é `409 CODE_IN_USE` — e o erro cai no campo do nome, que é onde se corrige.
 */
export function ModalDeAmbiente({
  controle,
  unitId,
  unidadeRotulo,
}: {
  controle: React.RefObject<ControleDeModal<AlvoDoAmbiente> | null>;
  unitId: string;
  unidadeRotulo: string;
}) {
  const [aberto, setAberto] = React.useState(false);
  const [ambiente, setAmbiente] = React.useState<Ambiente | null>(null);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const form = useForm<AmbienteFormulario>({ resolver: zodResolver(AmbienteFormulario), defaultValues: valores(null) });

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(alvo: AlvoDoAmbiente) {
        setAmbiente(alvo.ambiente);
        form.reset(valores(alvo));
        setErroGeral(null);
        setAberto(true);
      },
    }),
    [form],
  );

  const { errors, isSubmitting } = form.formState;

  async function enviar(v: AmbienteFormulario) {
    setErroGeral(null);
    const r = await salvarAmbiente(unitId, ambiente?.id ?? null, v);
    if (!r.ok) {
      if (r.code === "CODE_IN_USE") {
        form.setError("name", { type: "server", message: mensagemDeBens(r, "ambiente") });
        form.setFocus("name");
        return;
      }
      setErroGeral(aplicarFalha(r, form));
      return;
    }
    setAberto(false);
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title={ambiente ? `Editar ${ambiente.name}` : `Novo ambiente em ${unidadeRotulo}`}
      description="Cada cômodo da unidade, na ordem em que se caminha pela casa ao conferir."
      footer={<RodapeDoModal formulario="form-ambiente" enviando={isSubmitting} aoCancelar={() => setAberto(false)} />}
    >
      <AvisoGeral mensagem={erroGeral} />
      <form id="form-ambiente" onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        <Campo id="ambiente-name" label="Nome" obrigatorio erro={errors.name?.message} hint="Único na unidade — “Suíte 1”, “Cozinha”, “Varanda gourmet”.">
          {(p) => <Input {...p} {...form.register("name")} />}
        </Campo>
        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="ambiente-kind" label="Tipo" obrigatorio erro={errors.kind?.message}>
            {(p) => (
              <Select {...p} {...form.register("kind")}>
                {TIPOS_DE_AMBIENTE.map((k) => (
                  <option key={k} value={k}>
                    {ROTULO_DO_AMBIENTE[k]}
                  </option>
                ))}
              </Select>
            )}
          </Campo>
          <Campo
            id="ambiente-sort"
            label="Ordem na caminhada"
            erro={errors.sort_order?.message}
            hint="A lista de conferência segue esta ordem: 0 é o primeiro cômodo."
          >
            {(p) => <Input {...p} {...form.register("sort_order")} inputMode="numeric" className="tabular-nums" />}
          </Campo>
        </div>
        <CheckboxCampo
          id="ambiente-active"
          label="Ativo"
          hint="Desmarcado, o cômodo sai das conferências novas e o histórico continua legível."
          {...form.register("active")}
        />
      </form>
    </ModalShell>
  );
}
