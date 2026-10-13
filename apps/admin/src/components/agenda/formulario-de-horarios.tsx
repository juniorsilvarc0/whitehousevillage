"use client";

import * as React from "react";
import { useFieldArray, useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Check, Loader2, Plus, Save, X } from "lucide-react";

import { Nota, Secao } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { gravarHorarios } from "@/lib/agenda/acoes";
import { HorariosFormulario, valoresDosHorarios } from "@/lib/agenda/esquemas";
import { falhaNosHorarios, fraseDosReprogramados, type FalhaNosHorarios } from "@/lib/agenda/mensagens";
import { DIAS_DA_SEMANA } from "@/lib/agenda/rotulos";
import type { ConfiguracaoDaAgenda } from "@/lib/agenda/tipos";
import { formatarData, formatarInstante } from "@/lib/datas";

/**
 * Configurações → Agenda: os horários da operação (`GET|PUT /agenda/settings`).
 *
 * - **Faixa "provisórios, a confirmar"** enquanto `provisional` for `true` — os
 *   valores são os que o sistema semeou, e o dono ainda não os confirmou
 *   (decisão 12 do roadmap). Some depois da primeira gravação.
 * - **Salvar só aparece com `settings:editar`** — e não `agenda:editar`: o
 *   horário de entrada é o que a casa promete ao hóspede, e quem marca a
 *   limpeza do AP-02 não muda o check-in da casa.
 * - Depois de gravar, diz **quantos eventos automáticos foram reprogramados**
 *   (`meta.retimed_events`): confirmar os horários move os check-ins futuros
 *   que ainda estavam no horário antigo, e deixa os ajustados à mão.
 *
 * A coerência (`check-out ≤ início da limpeza < fim da limpeza ≤ check-in`,
 * o passo que divide o dia, intervalos sem sobreposição) é de
 * `agenda.Settings.Validate`. A API devolve **todas** as violações de uma vez,
 * e cada uma cai no seu campo — inclusive o intervalo de funcionamento certo,
 * pelo índice.
 */
export function FormularioDeHorarios({
  configuracao: inicial,
  podeEditar,
}: {
  configuracao: ConfiguracaoDaAgenda;
  podeEditar: boolean;
}) {
  const [configuracao, setConfiguracao] = React.useState(inicial);
  const [reprogramados, setReprogramados] = React.useState<number | null>(null);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const [errosDosIntervalos, setErrosDosIntervalos] = React.useState<FalhaNosHorarios["intervalos"]>({});
  const [erroDoFuncionamento, setErroDoFuncionamento] = React.useState<string | null>(null);

  const form = useForm<HorariosFormulario>({
    resolver: zodResolver(HorariosFormulario),
    defaultValues: valoresDosHorarios(inicial),
  });
  const { errors, isSubmitting } = form.formState;
  const intervalos = useFieldArray({ control: form.control, name: "business_hours" });

  function limparErrosDoFuncionamento() {
    setErrosDosIntervalos({});
    setErroDoFuncionamento(null);
  }

  async function enviar(v: HorariosFormulario) {
    setErroGeral(null);
    setReprogramados(null);
    limparErrosDoFuncionamento();
    const r = await gravarHorarios(v);
    if (r.ok) {
      setConfiguracao(r.data.configuracao);
      setReprogramados(r.data.reprogramados);
      form.reset(valoresDosHorarios(r.data.configuracao));
      return;
    }
    const leitura = falhaNosHorarios(r);
    for (const [campo, mensagem] of Object.entries(leitura.campos)) {
      form.setError(campo as keyof HorariosFormulario, { type: "server", message: mensagem });
    }
    for (const [i, erro] of Object.entries(leitura.intervalos)) {
      if (erro.start) form.setError(`business_hours.${Number(i)}.start`, { type: "server", message: erro.start });
      if (erro.end) form.setError(`business_hours.${Number(i)}.end`, { type: "server", message: erro.end });
    }
    setErrosDosIntervalos(leitura.intervalos);
    setErroDoFuncionamento(leitura.funcionamento);
    setErroGeral(leitura.geral);
  }

  const desabilitado = !podeEditar;

  return (
    <form onSubmit={form.handleSubmit(enviar)} noValidate className="flex flex-col gap-5">
      {configuracao.provisional ? (
        <Nota variante="atencao">
          <strong>Horários provisórios, a confirmar.</strong> Estes são os valores iniciais do sistema e ainda não foram
          confirmados pelo dono da casa. Check-in, check-out e limpeza já aparecem na agenda com eles; gravar confirma — e
          move para o horário novo os eventos automáticos que ainda estavam no antigo.
        </Nota>
      ) : null}

      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        {configuracao.provisional ? <Badge variant="outline">provisório</Badge> : <Badge variant="accent">confirmado</Badge>}
        {!configuracao.provisional && configuracao.updated_at ? (
          <span>
            gravado em <span className="tabular-nums text-foreground">{formatarInstante(configuracao.updated_at)}</span>
            {configuracao.updated_by_name ? ` por ${configuracao.updated_by_name}` : ""}
          </span>
        ) : null}
        <span>
          · hoje na casa: <span className="tabular-nums">{formatarData(configuracao.today)}</span>
        </span>
      </div>

      <Secao
        titulo="O dia de troca"
        descricao="Um hóspede sai, a unidade é limpa e o seguinte entra — no mesmo dia. Por isso o check-out vem antes da limpeza, e a limpeza termina antes do check-in."
      >
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <Campo id="horarios-checkout" label="Check-out até" obrigatorio erro={errors.checkout_time?.message}>
            {(p) => <Input {...p} type="time" step={300} {...form.register("checkout_time")} disabled={desabilitado} className="tabular-nums" />}
          </Campo>
          <Campo id="horarios-limpeza-inicio" label="Limpeza a partir de" obrigatorio erro={errors.cleaning_start?.message}>
            {(p) => <Input {...p} type="time" step={300} {...form.register("cleaning_start")} disabled={desabilitado} className="tabular-nums" />}
          </Campo>
          <Campo id="horarios-limpeza-fim" label="Limpeza até" obrigatorio erro={errors.cleaning_end?.message}>
            {(p) => <Input {...p} type="time" step={300} {...form.register("cleaning_end")} disabled={desabilitado} className="tabular-nums" />}
          </Campo>
          <Campo id="horarios-checkin" label="Check-in a partir de" obrigatorio erro={errors.checkin_time?.message}>
            {(p) => <Input {...p} type="time" step={300} {...form.register("checkin_time")} disabled={desabilitado} className="tabular-nums" />}
          </Campo>
        </div>
      </Secao>

      <Secao titulo="A grade e a folga" descricao="O passo é a altura de cada linha nas visões de dia e semana, e a duração do check-in e do check-out.">
        <div className="grid gap-4 sm:grid-cols-2">
          <Campo
            id="horarios-passo"
            label="Passo da grade (minutos)"
            obrigatorio
            erro={errors.slot_minutes?.message}
            hint="Um número que divida o dia em passos inteiros: 5, 10, 15, 20, 30, 60…"
          >
            {(p) => <Input {...p} inputMode="numeric" {...form.register("slot_minutes")} disabled={desabilitado} className="text-right tabular-nums sm:w-32" />}
          </Campo>
          <Campo
            id="horarios-folga"
            label="Folga entre compromissos (minutos)"
            obrigatorio
            erro={errors.buffer_minutes?.message}
            hint="O tempo mínimo entre dois compromissos da mesma pessoa. Menos que isso, a agenda avisa — não recusa."
          >
            {(p) => <Input {...p} inputMode="numeric" {...form.register("buffer_minutes")} disabled={desabilitado} className="text-right tabular-nums sm:w-32" />}
          </Campo>
        </div>
      </Secao>

      <Secao
        titulo="Funcionamento"
        descricao="Quando a operação atende, dia a dia. Fora disso a agenda avisa quem marcar algo — não recusa. Dia sem intervalo é dia fechado; para ir até a meia-noite, use 00:00 no fim."
      >
        {erroDoFuncionamento ? (
          <p role="alert" className="mb-3 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {erroDoFuncionamento}
          </p>
        ) : null}
        <ul className="flex flex-col divide-y divide-border/50" aria-label="Funcionamento por dia">
          {DIAS_DA_SEMANA.map((dia) => {
            const doDia = intervalos.fields.map((f, indice) => ({ f, indice })).filter(({ f }) => f.weekday === dia.iso);
            return (
              <li key={dia.iso} className="flex flex-col gap-2 py-3 sm:flex-row sm:items-start">
                <span className="w-24 shrink-0 pt-2 text-sm font-medium">{dia.longo}</span>
                <div className="flex min-w-0 flex-1 flex-col gap-2">
                  {doDia.length === 0 ? <p className="pt-2 text-sm text-muted-foreground">Fechado</p> : null}
                  {doDia.map(({ f, indice }) => {
                    const erroLinha = errosDosIntervalos[indice]?.linha ?? errosDosIntervalos[indice]?.weekday;
                    const erroInicio = errors.business_hours?.[indice]?.start?.message;
                    const erroFim = errors.business_hours?.[indice]?.end?.message;
                    return (
                      <div key={f.id} className="flex flex-col gap-1" data-intervalo={indice}>
                        <div className="flex items-center gap-2">
                          <Input
                            type="time"
                            step={300}
                            aria-label={`${dia.longo}: abre às`}
                            invalid={Boolean(erroInicio)}
                            {...form.register(`business_hours.${indice}.start`, { onChange: limparErrosDoFuncionamento })}
                            disabled={desabilitado}
                            className="w-32 tabular-nums"
                          />
                          <span className="text-sm text-muted-foreground">às</span>
                          <Input
                            type="time"
                            step={300}
                            aria-label={`${dia.longo}: fecha às`}
                            invalid={Boolean(erroFim)}
                            {...form.register(`business_hours.${indice}.end`, { onChange: limparErrosDoFuncionamento })}
                            disabled={desabilitado}
                            className="w-32 tabular-nums"
                          />
                          {!desabilitado ? (
                            <Button
                              variant="ghost"
                              size="icon"
                              aria-label={`Tirar o intervalo de ${dia.longo.toLowerCase()}`}
                              onClick={() => {
                                limparErrosDoFuncionamento();
                                intervalos.remove(indice);
                              }}
                            >
                              <X aria-hidden="true" />
                            </Button>
                          ) : null}
                        </div>
                        {[erroInicio, erroFim, erroLinha].filter(Boolean).map((m) => (
                          <p key={m} role="alert" className="text-xs font-medium text-destructive">
                            {m}
                          </p>
                        ))}
                      </div>
                    );
                  })}
                  {!desabilitado ? (
                    <Button
                      variant="link"
                      size="sm"
                      className="self-start px-0"
                      onClick={() => {
                        limparErrosDoFuncionamento();
                        intervalos.append({ weekday: dia.iso, start: "", end: "" });
                      }}
                    >
                      <Plus aria-hidden="true" />
                      {doDia.length === 0 ? `Abrir na ${dia.longo.toLowerCase()}` : "Mais um intervalo"}
                    </Button>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ul>
      </Secao>

      {erroGeral ? (
        <p role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erroGeral}
        </p>
      ) : null}

      {reprogramados !== null ? (
        <p role="status" className="flex items-center gap-2 rounded-lg bg-alcada-livre/12 px-3 py-2 text-sm text-alcada-livre">
          <Check className="size-4" aria-hidden="true" />
          {fraseDosReprogramados(reprogramados)}
        </p>
      ) : null}

      {podeEditar ? (
        <div className="flex justify-end">
          <Button type="submit" disabled={isSubmitting} className="max-sm:h-11 max-sm:w-full">
            {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Save aria-hidden="true" />}
            {configuracao.provisional ? "Confirmar e gravar" : "Gravar horários"}
          </Button>
        </div>
      ) : (
        <Nota>
          Só quem edita Configurações grava estes horários: o horário de entrada é o que a casa promete ao hóspede, e mudá-lo
          reprograma a agenda.
        </Nota>
      )}
    </form>
  );
}
