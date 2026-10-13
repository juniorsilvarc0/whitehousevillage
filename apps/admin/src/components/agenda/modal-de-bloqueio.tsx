"use client";

import * as React from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Ban, Loader2 } from "lucide-react";

import { notificarSucesso } from "@/components/agenda/avisos";
import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { bloquearHorario } from "@/lib/agenda/acoes";
import { BloqueioFormulario, bloqueioVazio } from "@/lib/agenda/esquemas";
import { falhaNoFormulario, type CampoDoFormulario } from "@/lib/agenda/mensagens";

/**
 * Bloquear um horário da operação — "equipe em treinamento das 14:00 às
 * 16:00", "operação fechada no dia da dedetização".
 *
 * Não ocupa unidade e **não recusa nada**: o evento marcado em cima recebe o
 * aviso `bloqueio_de_horario` e é criado do mesmo jeito. Para tirar uma
 * unidade da venda, o caminho é o mapa (`POST /blocks`). A tela diz isso antes
 * do botão, porque os dois "bloqueios" se confundem no primeiro dia.
 */
export function ModalDeBloqueio({ controle }: { controle: React.RefObject<ControleDeModal<{ data: string }> | null> }) {
  const [aberto, setAberto] = React.useState(false);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const form = useForm<BloqueioFormulario>({ resolver: zodResolver(BloqueioFormulario), defaultValues: bloqueioVazio() });
  const { errors, isSubmitting } = form.formState;

  React.useImperativeHandle(
    controle,
    () => ({
      abrir({ data }: { data: string }) {
        form.reset(bloqueioVazio({ data_inicio: data }));
        setErroGeral(null);
        setAberto(true);
      },
    }),
    [form],
  );

  const diaInteiro = useWatch({ control: form.control, name: "all_day" });

  async function enviar(v: BloqueioFormulario) {
    setErroGeral(null);
    const r = await bloquearHorario(v);
    if (r.ok) {
      setAberto(false);
      notificarSucesso("Horário bloqueado", r.data.reason);
      return;
    }
    const leitura = falhaNoFormulario(r, "bloqueio", v.all_day);
    const campos: (keyof BloqueioFormulario)[] = ["reason", "all_day", "data_inicio", "hora_inicio", "data_fim", "hora_fim"];
    const sobrou: string[] = [];
    for (const [campo, mensagem] of Object.entries(leitura.campos) as [CampoDoFormulario, string][]) {
      if ((campos as string[]).includes(campo)) form.setError(campo as keyof BloqueioFormulario, { type: "server", message: mensagem });
      else sobrou.push(mensagem);
    }
    setErroGeral(leitura.geral ?? (sobrou.length > 0 ? sobrou.join(" ") : null));
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title="Bloquear um horário da operação"
      description="Quem marcar algo nesse horário recebe um aviso — nada é recusado."
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button type="submit" form="form-bloqueio-de-horario" disabled={isSubmitting} className="max-md:h-11 max-md:flex-1">
            {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Ban aria-hidden="true" />}
            Bloquear horário
          </Button>
        </>
      }
    >
      {erroGeral ? (
        <p role="alert" className="mb-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erroGeral}
        </p>
      ) : null}
      <form id="form-bloqueio-de-horario" noValidate onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        <Nota>
          Este bloqueio é do <strong>tempo da operação</strong>, não de uma unidade: a casa continua à venda. Para tirar um
          apartamento da venda, bloqueie no mapa de ocupação.
        </Nota>
        <Campo id="bloqueio-motivo" label="Motivo" obrigatorio erro={errors.reason?.message}>
          {(p) => <Input {...p} {...form.register("reason")} maxLength={200} placeholder="Equipe em treinamento" autoComplete="off" />}
        </Campo>
        <fieldset className="flex flex-col gap-3 rounded-xl border border-border/60 bg-muted/20 p-3.5" aria-label="Quando">
          <legend className="sr-only">Quando</legend>
          <CheckboxCampo id="bloqueio-dia-inteiro" label="Dia inteiro" {...form.register("all_day")} />
          <div className="grid grid-cols-2 gap-3">
            <Campo id="bloqueio-data-inicio" label={diaInteiro ? "Primeiro dia" : "Dia"} obrigatorio erro={errors.data_inicio?.message}>
              {(p) => <Input {...p} type="date" {...form.register("data_inicio")} />}
            </Campo>
            {diaInteiro ? (
              <Campo id="bloqueio-data-fim" label="Último dia" erro={errors.data_fim?.message} hint="Vazio: só o primeiro dia.">
                {(p) => <Input {...p} type="date" {...form.register("data_fim")} />}
              </Campo>
            ) : (
              <Campo id="bloqueio-data-fim" label="Termina no dia" erro={errors.data_fim?.message} hint="Vazio: no mesmo dia.">
                {(p) => <Input {...p} type="date" {...form.register("data_fim")} />}
              </Campo>
            )}
          </div>
          {!diaInteiro ? (
            <div className="grid grid-cols-2 gap-3">
              <Campo id="bloqueio-hora-inicio" label="Das" obrigatorio erro={errors.hora_inicio?.message}>
                {(p) => <Input {...p} type="time" step={300} {...form.register("hora_inicio")} />}
              </Campo>
              <Campo id="bloqueio-hora-fim" label="Às" obrigatorio erro={errors.hora_fim?.message}>
                {(p) => <Input {...p} type="time" step={300} {...form.register("hora_fim")} />}
              </Campo>
            </div>
          ) : null}
        </fieldset>
      </form>
    </ModalShell>
  );
}
