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
import { Textarea } from "@/components/ui/textarea";
import { aplicarFalha } from "@/lib/acoes/formulario";
import { salvarBem } from "@/lib/bens/acoes";
import { BemFormulario, valoresDoBem } from "@/lib/bens/esquemas";
import { ROTULO_DA_CATEGORIA, ROTULO_DA_MEDIDA } from "@/lib/bens/rotulos";
import { CATEGORIAS_DE_BEM, UNIDADES_DE_MEDIDA, type Bem } from "@/lib/bens/tipos";

/**
 * Cadastrar ou editar um bem do **catálogo**.
 *
 * O bem é um só, esteja em dois ambientes ou em seis apartamentos: corrigir o
 * nome aqui corrige em todos de uma vez. A quantidade de cada lugar não mora
 * aqui — mora na colocação, na tela da unidade.
 *
 * O custo de reposição é digitado em **reais** e viaja em **centavos**; vazio
 * quer dizer "ainda não cotado", e zero é recusado (seria um segundo jeito,
 * errado, de dizer "não sei" — e uma avaria que sairia de graça).
 */
export function ModalDeBem({
  controle,
  aoSalvar,
}: {
  controle: React.RefObject<ControleDeModal<Bem | null> | null>;
  /** Depois de gravar. Na criação, a tela leva à ficha para enviar a foto. */
  aoSalvar?: (bem: Bem, criado: boolean) => void;
}) {
  const [aberto, setAberto] = React.useState(false);
  const [bem, setBem] = React.useState<Bem | null>(null);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const form = useForm<BemFormulario>({ resolver: zodResolver(BemFormulario), defaultValues: valoresDoBem(null) });

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(escolhido: Bem | null) {
        setBem(escolhido);
        form.reset(valoresDoBem(escolhido));
        setErroGeral(null);
        setAberto(true);
      },
    }),
    [form],
  );

  const { errors, isSubmitting } = form.formState;

  async function enviar(valores: BemFormulario) {
    setErroGeral(null);
    const resultado = await salvarBem(bem?.id ?? null, valores);
    if (!resultado.ok) {
      setErroGeral(aplicarFalha(resultado, form));
      return;
    }
    setAberto(false);
    aoSalvar?.(resultado.data, bem === null);
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title={bem ? `Editar ${bem.name}` : "Novo bem"}
      description="Um item do catálogo da casa. A quantidade de cada cômodo se define na tela da unidade."
      footer={<RodapeDoModal formulario="form-bem" enviando={isSubmitting} aoCancelar={() => setAberto(false)} />}
    >
      <AvisoGeral mensagem={erroGeral} />
      <form id="form-bem" onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        <Campo id="bem-name" label="Nome" obrigatorio erro={errors.name?.message} hint="Como a equipe chama — “Prato raso branco”, “Taça de vinho”.">
          {(p) => <Input {...p} {...form.register("name")} />}
        </Campo>

        <Campo
          id="bem-description"
          label="Descrição"
          erro={errors.description?.message}
          hint="O detalhe que distingue dois bens parecidos: “borda dourada, 26 cm”."
        >
          {(p) => <Textarea {...p} rows={2} {...form.register("description")} />}
        </Campo>

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="bem-category" label="Categoria" obrigatorio erro={errors.category?.message}>
            {(p) => (
              <Select {...p} {...form.register("category")}>
                {CATEGORIAS_DE_BEM.map((c) => (
                  <option key={c} value={c}>
                    {ROTULO_DA_CATEGORIA[c]}
                  </option>
                ))}
              </Select>
            )}
          </Campo>

          <Campo
            id="bem-unit-measure"
            label="Conta-se em"
            obrigatorio
            erro={errors.unit_measure?.message}
            hint="Contar jogo como peça faz a conferência fechar com o dobro."
          >
            {(p) => (
              <Select {...p} {...form.register("unit_measure")}>
                {UNIDADES_DE_MEDIDA.map((m) => (
                  <option key={m} value={m}>
                    {ROTULO_DA_MEDIDA[m]}
                  </option>
                ))}
              </Select>
            )}
          </Campo>
        </div>

        <Campo
          id="bem-replacement-cost"
          label="Custo de reposição (R$)"
          erro={errors.replacement_cost?.message}
          hint="Quanto custa repor a peça hoje — é a base da cobrança de uma avaria. Deixe vazio se ainda não foi cotado."
        >
          {(p) => (
            <Input
              {...p}
              {...form.register("replacement_cost")}
              inputMode="decimal"
              placeholder="não cotado"
              className="text-right tabular-nums"
            />
          )}
        </Campo>

        <CheckboxCampo
          id="bem-active"
          label="Em uso"
          hint="Desmarcado, o bem sai de linha: não entra em conferência nova, e o histórico continua legível."
          {...form.register("active")}
        />
      </form>
    </ModalShell>
  );
}
