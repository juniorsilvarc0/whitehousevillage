"use client";

import * as React from "react";
import Link from "next/link";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2, Pencil, Plus, Power } from "lucide-react";

import { useControleDeModal, type ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalDeConfirmacao } from "@/components/layout/modal-de-confirmacao";
import { ModalShell } from "@/components/layout/modal-shell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import type { TabelaDeTarifas } from "@/lib/api/comercial";
import { aplicarFalha } from "@/lib/acoes/formulario";
import { formatarData, hojeISO } from "@/lib/datas";
import { cn } from "@/lib/utils";

import { desativarTabela, salvarTabela } from "./acoes";
import { TabelaFormulario } from "./esquemas";

/**
 * Escolha da tabela vigente e o cadastro dela.
 *
 * A tabela escolhida vive na **query string** (`?tabela=<id>`), não em estado
 * de componente: a grade do réveillon é a coisa que a gestão manda por WhatsApp
 * para o proprietário conferir, e um link que abre sempre na tabela padrão não
 * serve para isso.
 */
export function SeletorDeTabelas({
  tabelas,
  selecionada,
  permissoes,
}: {
  tabelas: TabelaDeTarifas[];
  selecionada: TabelaDeTarifas | null;
  permissoes: { criar: boolean; editar: boolean; excluir: boolean };
}) {
  const cadastro = useControleDeModal<TabelaDeTarifas | null>();
  const [aDesativar, setADesativar] = React.useState<TabelaDeTarifas | null>(null);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        {tabelas.map((tabela) => {
          const ativa = tabela.id === selecionada?.id;
          return (
            <Link
              key={tabela.id}
              href={`/app/configuracoes/tarifario?tabela=${tabela.id}`}
              aria-current={ativa ? "true" : undefined}
              className={cn(
                "inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-sm transition-colors outline-none",
                "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-card",
                ativa
                  ? "border-transparent bg-brand-gradient text-white"
                  : "border-border/70 bg-card/70 text-foreground hover:bg-muted",
              )}
            >
              {tabela.name}
              {!tabela.active ? (
                <span className={cn("rounded-full px-1.5 text-[0.65rem]", ativa ? "bg-white/20" : "bg-muted")}>
                  inativa
                </span>
              ) : null}
            </Link>
          );
        })}

        {permissoes.criar ? (
          <Button size="sm" variant="outline" onClick={() => cadastro.abrir(null)}>
            <Plus aria-hidden="true" />
            Nova tabela
          </Button>
        ) : null}
      </div>

      {selecionada ? (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg bg-card/70 px-3 py-2 text-xs text-muted-foreground">
          <span>
            Vigência: <span className="tabular-nums text-foreground">{formatarData(selecionada.valid_from)}</span>{" "}
            {selecionada.valid_to ? (
              <>
                até <span className="tabular-nums text-foreground">{formatarData(selecionada.valid_to)}</span>
              </>
            ) : (
              <span className="text-foreground">sem data de fim</span>
            )}
          </span>
          {vigenteHoje(selecionada) ? <Badge variant="accent">valendo hoje</Badge> : null}

          <span className="ml-auto flex gap-1">
            {permissoes.editar ? (
              <Button size="sm" variant="ghost" onClick={() => cadastro.abrir(selecionada)}>
                <Pencil aria-hidden="true" />
                Editar vigência
              </Button>
            ) : null}
            {permissoes.excluir && selecionada.active ? (
              <Button size="sm" variant="ghost" onClick={() => setADesativar(selecionada)}>
                <Power aria-hidden="true" />
                Desativar
              </Button>
            ) : null}
          </span>
        </div>
      ) : null}

      <ModalDeTabela controle={cadastro.ref} />

      <ModalDeConfirmacao
        aberto={aDesativar !== null}
        aoMudar={(aberto) => !aberto && setADesativar(null)}
        titulo={`Desativar ${aDesativar?.name ?? ""}?`}
        destrutivo
        rotuloConfirmar="Desativar tabela"
        descricao={
          <>
            Desativar não apaga os preços: as reservas antigas continuam com o preço combinado. Se esta
            for a única tabela valendo hoje, não será possível desativar — sem ela, nenhum orçamento
            poderia ser feito.
          </>
        }
        aoConfirmar={() => desativarTabela(aDesativar!.id)}
      />
    </div>
  );
}

function vigenteHoje(tabela: TabelaDeTarifas): boolean {
  const hoje = hojeISO();
  return tabela.active && tabela.valid_from <= hoje && (tabela.valid_to === null || tabela.valid_to >= hoje);
}

function valoresDaTabela(tabela: TabelaDeTarifas | null): TabelaFormulario {
  return {
    name: tabela?.name ?? "",
    valid_from: tabela?.valid_from ?? hojeISO(),
    valid_to: tabela?.valid_to ?? "",
    active: tabela?.active ?? true,
  };
}

function ModalDeTabela({ controle }: { controle: React.RefObject<ControleDeModal<TabelaDeTarifas | null> | null> }) {
  const [aberto, setAberto] = React.useState(false);
  const [tabela, setTabela] = React.useState<TabelaDeTarifas | null>(null);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const form = useForm<TabelaFormulario>({
    resolver: zodResolver(TabelaFormulario),
    defaultValues: valoresDaTabela(null),
  });

  // Repor no evento que abre, não num efeito que reage a ter aberto: o
  // formulário já nasce com a vigência da tabela escolhida agora.
  React.useImperativeHandle(
    controle,
    () => ({
      abrir(escolhida: TabelaDeTarifas | null) {
        setTabela(escolhida);
        form.reset(valoresDaTabela(escolhida));
        setErroGeral(null);
        setAberto(true);
      },
    }),
    [form],
  );

  const aoMudar = setAberto;
  const { errors, isSubmitting } = form.formState;

  async function enviar(valores: TabelaFormulario) {
    setErroGeral(null);
    const resultado = await salvarTabela(tabela?.id ?? null, valores);
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
      title={tabela ? `Editar ${tabela.name}` : "Nova tabela de tarifas"}
      description="Cada tabela vale por um período. Se duas valerem na mesma data, vale a que começou mais recentemente."
      footer={
        <>
          <Button variant="ghost" onClick={() => aoMudar(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button type="submit" form="form-tabela-de-tarifas" disabled={isSubmitting}>
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

      <form id="form-tabela-de-tarifas" onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        <Campo
          id="tabela-name"
          label="Nome"
          obrigatorio
          erro={errors.name?.message}
          hint="Um nome que não se repita — Tabela Comercial V1, Tabela 2027."
        >
          {(p) => <Input {...p} {...form.register("name")} />}
        </Campo>

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="tabela-de" label="Vale a partir de" obrigatorio erro={errors.valid_from?.message}>
            {(p) => <Input {...p} type="date" {...form.register("valid_from")} className="tabular-nums" />}
          </Campo>

          <Campo
            id="tabela-ate"
            label="Vale até"
            erro={errors.valid_to?.message}
            hint="Em branco = sem fim."
          >
            {(p) => <Input {...p} type="date" {...form.register("valid_to")} className="tabular-nums" />}
          </Campo>
        </div>

        <CheckboxCampo
          id="tabela-active"
          label="Ativa"
          hint="Desmarcada, a tabela não é usada em orçamentos novos; as reservas antigas não mudam."
          {...form.register("active")}
        />
      </form>
    </ModalShell>
  );
}
