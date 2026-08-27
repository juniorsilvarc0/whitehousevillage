"use client";

import * as React from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { CalendarDays, Layers, Loader2, Pencil, Plus, Trash2 } from "lucide-react";

import { useControleDeModal, type ControleDeModal } from "@/components/layout/controle-de-modal";
import { EstadoVazio } from "@/components/layout/estados";
import { ModalDeConfirmacao } from "@/components/layout/modal-de-confirmacao";
import { ModalShell } from "@/components/layout/modal-shell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import type { Feriado, PeriodoEspecial } from "@/lib/api/comercial";
import { aplicarFalha } from "@/lib/acoes/formulario";
import { TIPOS_DE_PERIODO, classeDoTipo, rotuloDoPeriodo } from "@/lib/comercial/tipos-de-data";
import { sobreposicoes } from "@/lib/comercial/periodos";
import { diasInclusivos, formatarData, hojeISO } from "@/lib/datas";
import { cn } from "@/lib/utils";

import { removerFeriado, removerPeriodo, salvarFeriado, salvarPeriodo } from "./acoes";
import { FeriadoFormulario, PeriodoFormulario } from "./esquemas";

type Permissoes = { criar: boolean; editar: boolean; excluir: boolean };

// ─────────────────────────────── Feriados ──────────────────────────────────

export function ListaDeFeriados({
  feriados,
  permissoes,
}: {
  feriados: Feriado[];
  permissoes: Permissoes;
}) {
  const cadastro = useControleDeModal<Feriado | null>();
  const [aRemover, setARemover] = React.useState<Feriado | null>(null);
  const hoje = hojeISO();

  return (
    <>
      {permissoes.criar ? (
        <div className="mb-3 flex justify-end">
          <Button size="sm" variant="outline" onClick={() => cadastro.abrir(null)}>
            <Plus aria-hidden="true" />
            Novo feriado
          </Button>
        </div>
      ) : null}

      {feriados.length === 0 ? (
        <EstadoVazio
          titulo="Nenhum feriado no período"
          descricao="Sem feriado cadastrado, a data cai em fim de semana ou normal — e é cobrada como tal."
          icone={CalendarDays}
        />
      ) : (
        <ul className="flex flex-col gap-1.5">
          {feriados.map((feriado) => (
            <li
              key={feriado.id}
              className={cn(
                "flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg bg-card/70 px-3 py-2",
                !feriado.active && "opacity-65",
              )}
            >
              <span className="w-24 shrink-0 font-mono text-sm tabular-nums">{formatarData(feriado.date)}</span>
              <span className="min-w-0 flex-1 truncate text-sm">{feriado.name}</span>
              {feriado.date < hoje ? <Badge variant="outline">passado</Badge> : null}
              {!feriado.active ? <Badge variant="outline">inativo</Badge> : null}
              <div className="flex gap-1">
                {permissoes.editar ? (
                  <Button size="iconSm" variant="ghost" onClick={() => cadastro.abrir(feriado)} aria-label={`Editar ${feriado.name}`}>
                    <Pencil aria-hidden="true" />
                  </Button>
                ) : null}
                {permissoes.excluir ? (
                  <Button size="iconSm" variant="ghost" onClick={() => setARemover(feriado)} aria-label={`Remover ${feriado.name}`}>
                    <Trash2 aria-hidden="true" />
                  </Button>
                ) : null}
              </div>
            </li>
          ))}
        </ul>
      )}

      <ModalDeFeriado controle={cadastro.ref} />

      <ModalDeConfirmacao
        aberto={aRemover !== null}
        aoMudar={(aberto) => !aberto && setARemover(null)}
        titulo={`Remover ${aRemover?.name ?? ""}?`}
        destrutivo
        rotuloConfirmar="Remover feriado"
        descricao={
          <>
            A data volta a ser classificada como fim de semana ou normal <strong>nos próximos
            cálculos</strong>. Reservas já emitidas mantêm o tipo e o preço que gravaram noite a noite.
          </>
        }
        aoConfirmar={() => removerFeriado(aRemover!.id)}
      />
    </>
  );
}

function valoresDoFeriado(feriado: Feriado | null): FeriadoFormulario {
  return {
    date: feriado?.date ?? hojeISO(),
    name: feriado?.name ?? "",
    active: feriado?.active ?? true,
  };
}

function ModalDeFeriado({ controle }: { controle: React.RefObject<ControleDeModal<Feriado | null> | null> }) {
  const [aberto, setAberto] = React.useState(false);
  const [feriado, setFeriado] = React.useState<Feriado | null>(null);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const form = useForm<FeriadoFormulario>({
    resolver: zodResolver(FeriadoFormulario),
    defaultValues: valoresDoFeriado(null),
  });

  // Reposição no evento que abre. O modal segue montado — quem sai de cena é o
  // conteúdo do popup, e é o Base UI que cuida disso com a transição inteira.
  React.useImperativeHandle(
    controle,
    () => ({
      abrir(escolhido: Feriado | null) {
        setFeriado(escolhido);
        form.reset(valoresDoFeriado(escolhido));
        setErroGeral(null);
        setAberto(true);
      },
    }),
    [form],
  );

  const aoMudar = setAberto;
  const { errors, isSubmitting } = form.formState;

  async function enviar(valores: FeriadoFormulario) {
    setErroGeral(null);
    const resultado = await salvarFeriado(feriado?.id ?? null, valores);
    if (!resultado.ok) {
      setErroGeral(aplicarFalha(resultado, form, { CODE_IN_USE: "date" }));
      return;
    }
    aoMudar(false);
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={aoMudar}
      title={feriado ? `Editar ${feriado.name}` : "Novo feriado"}
      description="Data em feriados classifica a noite como feriado — precedência 80."
      footer={
        <>
          <Button variant="ghost" onClick={() => aoMudar(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button type="submit" form="form-feriado" disabled={isSubmitting}>
            {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
            Salvar
          </Button>
        </>
      }
    >
      {erroGeral ? (
        <p role="alert" className="mb-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erroGeral}
        </p>
      ) : null}

      <form id="form-feriado" onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        <Campo
          id="feriado-date"
          label="Data"
          obrigatorio
          erro={errors.date?.message}
          hint="Uma data, um feriado: a chave natural é a própria data dentro da propriedade."
        >
          {(p) => <Input {...p} type="date" {...form.register("date")} className="tabular-nums" />}
        </Campo>

        <Campo id="feriado-name" label="Nome" obrigatorio erro={errors.name?.message}>
          {(p) => <Input {...p} {...form.register("name")} placeholder="Independência" />}
        </Campo>

        <CheckboxCampo
          id="feriado-active"
          label="Ativo"
          hint="Feriado inativo deixa de classificar a noite nos cálculos seguintes, sem sumir do histórico."
          {...form.register("active")}
        />
      </form>
    </ModalShell>
  );
}

// ────────────────────────── Períodos especiais ─────────────────────────────

export function ListaDePeriodos({
  periodos,
  permissoes,
}: {
  periodos: PeriodoEspecial[];
  permissoes: Permissoes;
}) {
  const cadastro = useControleDeModal<PeriodoEspecial | null>();
  const [aRemover, setARemover] = React.useState<PeriodoEspecial | null>(null);

  const cruzamentos = React.useMemo(() => sobreposicoes(periodos), [periodos]);

  return (
    <>
      {permissoes.criar ? (
        <div className="mb-3 flex justify-end">
          <Button size="sm" variant="outline" onClick={() => cadastro.abrir(null)}>
            <Plus aria-hidden="true" />
            Novo período
          </Button>
        </div>
      ) : null}

      {periodos.length === 0 ? (
        <EstadoVazio
          titulo="Nenhum período especial"
          descricao="Réveillon, carnaval e alta temporada são períodos: sem eles, todas as noites são normais ou fim de semana."
          icone={Layers}
        />
      ) : (
        <ul className="grid gap-2 xl:grid-cols-2">
          {periodos.map((periodo) => {
            const cruzam = cruzamentos.get(periodo.id) ?? [];
            return (
              <li
                key={periodo.id}
                className={cn(
                  "rounded-xl border border-border/60 bg-card/60 p-3.5",
                  !periodo.active && "opacity-65",
                )}
              >
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div className="min-w-0">
                    <h3 className="font-display truncate text-sm leading-tight">{periodo.name}</h3>
                    <p className="mt-0.5 text-xs tabular-nums text-muted-foreground">
                      {formatarData(periodo.starts_on)} — {formatarData(periodo.ends_on)}{" "}
                      <span className="text-foreground">
                        ({diasInclusivos(periodo.starts_on, periodo.ends_on)} dias)
                      </span>
                    </p>
                  </div>
                  <div className="flex shrink-0 items-center gap-1.5">
                    <span
                      className={cn(
                        "rounded-full px-2 py-0.5 text-[0.7rem]",
                        periodo.kind === "evento" ? "bg-secondary text-secondary-foreground" : classeDoTipo(periodo.kind),
                      )}
                    >
                      {rotuloDoPeriodo(periodo.kind)}
                    </span>
                    {!periodo.active ? <Badge variant="outline">inativo</Badge> : null}
                  </div>
                </div>

                {cruzam.length > 0 ? (
                  <p className="mt-2 text-xs text-muted-foreground">
                    Sobrepõe{" "}
                    {cruzam.map((outro, indice) => (
                      <span key={outro.id}>
                        {indice > 0 ? ", " : ""}
                        <strong className="text-foreground">{outro.name}</strong>
                      </span>
                    ))}
                    . Nas noites em comum vence o de maior precedência — é assim que deve ser.
                  </p>
                ) : null}

                <div className="mt-3 flex gap-1">
                  {permissoes.editar ? (
                    <Button size="sm" variant="ghost" onClick={() => cadastro.abrir(periodo)}>
                      <Pencil aria-hidden="true" />
                      Editar
                    </Button>
                  ) : null}
                  {permissoes.excluir ? (
                    <Button size="sm" variant="ghost" onClick={() => setARemover(periodo)}>
                      <Trash2 aria-hidden="true" />
                      Remover
                    </Button>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ul>
      )}

      <ModalDePeriodo controle={cadastro.ref} />

      <ModalDeConfirmacao
        aberto={aRemover !== null}
        aoMudar={(aberto) => !aberto && setARemover(null)}
        titulo={`Remover ${aRemover?.name ?? ""}?`}
        destrutivo
        rotuloConfirmar="Remover período"
        descricao={
          <>
            As noites cobertas voltam a ser classificadas pelo que sobrar (outro período, feriado, fim de
            semana ou normal) <strong>nos próximos cálculos</strong> — e podem mudar de preço. Reservas
            emitidas não mudam.
          </>
        }
        aoConfirmar={() => removerPeriodo(aRemover!.id)}
      />
    </>
  );
}

function valoresDoPeriodo(periodo: PeriodoEspecial | null): PeriodoFormulario {
  return {
    name: periodo?.name ?? "",
    kind: periodo?.kind ?? "alta",
    starts_on: periodo?.starts_on ?? hojeISO(),
    ends_on: periodo?.ends_on ?? hojeISO(),
    active: periodo?.active ?? true,
  };
}

function ModalDePeriodo({ controle }: { controle: React.RefObject<ControleDeModal<PeriodoEspecial | null> | null> }) {
  const [aberto, setAberto] = React.useState(false);
  const [periodo, setPeriodo] = React.useState<PeriodoEspecial | null>(null);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const form = useForm<PeriodoFormulario>({
    resolver: zodResolver(PeriodoFormulario),
    defaultValues: valoresDoPeriodo(null),
  });

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(escolhido: PeriodoEspecial | null) {
        setPeriodo(escolhido);
        form.reset(valoresDoPeriodo(escolhido));
        setErroGeral(null);
        setAberto(true);
      },
    }),
    [form],
  );

  const aoMudar = setAberto;
  const { errors, isSubmitting } = form.formState;
  // `useWatch` em vez de `form.watch()`: o `watch` devolve uma função que o
  // compilador do React não consegue memoizar, e por causa dela o componente
  // inteiro sai da compilação (`react-hooks/incompatible-library`). O `useWatch`
  // é a assinatura que a própria react-hook-form oferece para isso, e assina o
  // campo em vez de reassinar o formulário a cada render.
  const inicio = useWatch({ control: form.control, name: "starts_on" });
  const fim = useWatch({ control: form.control, name: "ends_on" });
  const dias = inicio && fim && fim >= inicio ? diasInclusivos(inicio, fim) : null;

  async function enviar(valores: PeriodoFormulario) {
    setErroGeral(null);
    const resultado = await salvarPeriodo(periodo?.id ?? null, valores);
    if (!resultado.ok) {
      setErroGeral(aplicarFalha(resultado, form, { CODE_IN_USE: "name" }));
      return;
    }
    aoMudar(false);
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={aoMudar}
      title={periodo ? `Editar ${periodo.name}` : "Novo período especial"}
      description="Faixa do calendário comercial, inclusiva nas duas pontas."
      footer={
        <>
          <Button variant="ghost" onClick={() => aoMudar(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button type="submit" form="form-periodo" disabled={isSubmitting}>
            {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
            Salvar
          </Button>
        </>
      }
    >
      {erroGeral ? (
        <p role="alert" className="mb-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erroGeral}
        </p>
      ) : null}

      <form id="form-periodo" onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        <Campo
          id="periodo-name"
          label="Nome"
          obrigatorio
          erro={errors.name?.message}
          hint="Carregue o ano no nome — o Réveillon 2027/2028 é linha nova, não edição desta."
        >
          {(p) => <Input {...p} {...form.register("name")} placeholder="Réveillon 2026/2027" />}
        </Campo>

        <Campo
          id="periodo-kind"
          label="Tipo"
          obrigatorio
          erro={errors.kind?.message}
          hint="É o tipo que as noites deste período recebem — e é ele que decide a precedência."
        >
          {(p) => (
            <Select {...p} {...form.register("kind")}>
              {TIPOS_DE_PERIODO.map((t) => (
                <option key={t.code} value={t.code}>
                  {t.label} — {t.nota}
                </option>
              ))}
            </Select>
          )}
        </Campo>

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="periodo-de" label="Começa em" obrigatorio erro={errors.starts_on?.message}>
            {(p) => <Input {...p} type="date" {...form.register("starts_on")} className="tabular-nums" />}
          </Campo>

          <Campo
            id="periodo-ate"
            label="Termina em"
            obrigatorio
            erro={errors.ends_on?.message}
            hint="Inclusivo: esta data também é classificada."
          >
            {(p) => <Input {...p} type="date" {...form.register("ends_on")} className="tabular-nums" />}
          </Campo>
        </div>

        {dias !== null ? (
          <p className="rounded-lg bg-card/70 px-3 py-2 text-xs text-muted-foreground">
            <span className="tabular-nums text-foreground">{dias}</span> dia{dias === 1 ? "" : "s"}{" "}
            classificados — as duas pontas entram. Ao contrário da estadia, que é meia-aberta e não
            cobra a noite do check-out.
          </p>
        ) : null}

        <CheckboxCampo
          id="periodo-active"
          label="Ativo"
          hint="Período inativo deixa de classificar as noites nos cálculos seguintes."
          {...form.register("active")}
        />
      </form>
    </ModalShell>
  );
}
