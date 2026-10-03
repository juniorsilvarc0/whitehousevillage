"use client";

import * as React from "react";
import { useFieldArray, useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Check, Loader2, Plus, Save, Trash2 } from "lucide-react";

import { Nota } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import type { PoliticaDeCancelamento } from "@/lib/api/comercial";
import { aplicarFalha } from "@/lib/acoes/formulario";
import { formatarData, hojeISO } from "@/lib/datas";

import { publicarPoliticaDeCancelamento } from "./acoes";
import { PoliticaDeCancelamentoFormulario, buracosNasFaixas } from "./esquemas";

/** As faixas do seed, para quando ainda não há versão publicada nenhuma. */
const FAIXAS_PADRAO = [
  { days_before_min: "30", days_before_max: "", refund_pct: "100", label: "Devolução integral do sinal" },
  { days_before_min: "7", days_before_max: "29", refund_pct: "50", label: "Retenção de 50% do sinal" },
  { days_before_min: "", days_before_max: "6", refund_pct: "0", label: "Retenção integral do sinal" },
];

function valoresDaPolitica(politica: PoliticaDeCancelamento | null): PoliticaDeCancelamentoFormulario {
  return {
    name: politica?.name ?? "Padrão White House",
    valid_from: hojeISO(),
    tiers:
      politica && politica.tiers.length > 0
        ? [...politica.tiers]
            .sort((a, b) => a.sort_order - b.sort_order)
            .map((faixa) => ({
              days_before_min: faixa.days_before_min === null ? "" : String(faixa.days_before_min),
              days_before_max: faixa.days_before_max === null ? "" : String(faixa.days_before_max),
              refund_pct: String(faixa.refund_pct),
              label: faixa.label,
            }))
        : FAIXAS_PADRAO,
  };
}

/**
 * As faixas de cancelamento, sempre inteiras.
 *
 * Não existe "editar uma faixa": o `PUT` substitui a lista completa e cria uma
 * versão nova. Uma faixa sozinha não é uma política — o que o motor procura é a
 * **primeira faixa aplicável** de cima para baixo, e a ordem da tela é a ordem
 * de avaliação (`sort_order`).
 */
export function FormularioDeCancelamento({
  politica,
  podeEditar,
}: {
  politica: PoliticaDeCancelamento | null;
  podeEditar: boolean;
}) {
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const [versaoPublicada, setVersaoPublicada] = React.useState<number | null>(null);

  const form = useForm<PoliticaDeCancelamentoFormulario>({
    resolver: zodResolver(PoliticaDeCancelamentoFormulario),
    defaultValues: valoresDaPolitica(politica),
  });

  const faixas = useFieldArray({ control: form.control, name: "tiers" });
  const { errors, isSubmitting } = form.formState;

  // `useWatch` assina o campo; `form.watch()` devolveria uma função que o
  // compilador do React não memoiza — e por causa dela o componente inteiro
  // sairia da compilação (`react-hooks/incompatible-library`).
  const observadas = useWatch({ control: form.control, name: "tiers" });
  const buracos = React.useMemo(() => buracosNasFaixas(observadas ?? []), [observadas]);

  async function enviar(valores: PoliticaDeCancelamentoFormulario) {
    setErroGeral(null);
    setVersaoPublicada(null);
    const resultado = await publicarPoliticaDeCancelamento(valores);
    if (!resultado.ok) {
      setErroGeral(aplicarFalha(resultado, form, { POLICY_IMMUTABLE: "valid_from" }));
      return;
    }
    setVersaoPublicada(resultado.data.version);
    form.reset(valoresDaPolitica(resultado.data));
  }

  return (
    <form onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-5">
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        {politica ? (
          <>
            <Badge variant="accent">versão {politica.version}</Badge>
            <span>
              valendo desde <span className="tabular-nums text-foreground">{formatarData(politica.valid_from)}</span>
            </span>
          </>
        ) : (
          <Badge variant="outline">nenhuma versão publicada ainda</Badge>
        )}
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <Campo id="cancel-name" label="Nome da política" obrigatorio erro={errors.name?.message}>
          {(p) => <Input {...p} {...form.register("name")} disabled={!podeEditar} />}
        </Campo>

        <Campo
          id="cancel-validfrom"
          label="Vale a partir de"
          obrigatorio
          erro={errors.valid_from?.message}
        >
          {(p) => <Input {...p} type="date" {...form.register("valid_from")} className="tabular-nums" disabled={!podeEditar} />}
        </Campo>
      </div>

      <div className="flex flex-col gap-2">
        <p className="text-sm font-medium">
          Faixas por antecedência{" "}
          <span className="font-normal text-muted-foreground">
            — avaliadas de cima para baixo, da mais generosa à mais restritiva
          </span>
        </p>

        {faixas.fields.map((campo, indice) => {
          const erroFaixa = errors.tiers?.[indice];
          return (
            <div key={campo.id} className="rounded-xl border border-border/60 bg-card/60 p-3">
              <div className="flex items-start gap-3">
                <span className="mt-2 w-5 shrink-0 text-center font-mono text-xs text-muted-foreground">
                  {indice + 1}
                </span>

                <div className="grid min-w-0 flex-1 gap-3 sm:grid-cols-4">
                  <Campo
                    id={`faixa-${indice}-min`}
                    label="De (dias antes)"
                    erro={erroFaixa?.days_before_min?.message}
                    hint="Em branco = sem piso"
                  >
                    {(p) => (
                      <Input
                        {...p}
                        {...form.register(`tiers.${indice}.days_before_min`)}
                        inputMode="numeric"
                        placeholder="—"
                        className="h-9 text-right tabular-nums"
                        disabled={!podeEditar}
                      />
                    )}
                  </Campo>

                  <Campo
                    id={`faixa-${indice}-max`}
                    label="Até (dias antes)"
                    erro={erroFaixa?.days_before_max?.message}
                    hint="Em branco = sem teto"
                  >
                    {(p) => (
                      <Input
                        {...p}
                        {...form.register(`tiers.${indice}.days_before_max`)}
                        inputMode="numeric"
                        placeholder="—"
                        className="h-9 text-right tabular-nums"
                        disabled={!podeEditar}
                      />
                    )}
                  </Campo>

                  <Campo
                    id={`faixa-${indice}-pct`}
                    label="Devolve (% do sinal)"
                    erro={erroFaixa?.refund_pct?.message}
                  >
                    {(p) => (
                      <Input
                        {...p}
                        {...form.register(`tiers.${indice}.refund_pct`)}
                        inputMode="decimal"
                        className="h-9 text-right tabular-nums"
                        disabled={!podeEditar}
                      />
                    )}
                  </Campo>

                  <Campo
                    id={`faixa-${indice}-label`}
                    label="Texto mostrado"
                    erro={erroFaixa?.label?.message}
                  >
                    {(p) => (
                      <Input {...p} {...form.register(`tiers.${indice}.label`)} className="h-9" disabled={!podeEditar} />
                    )}
                  </Campo>
                </div>

                {podeEditar ? (
                  <Button
                    size="iconSm"
                    variant="ghost"
                    className="mt-6"
                    onClick={() => faixas.remove(indice)}
                    disabled={faixas.fields.length <= 1}
                    aria-label={`Remover a faixa ${indice + 1}`}
                  >
                    <Trash2 aria-hidden="true" />
                  </Button>
                ) : null}
              </div>
            </div>
          );
        })}

        {typeof errors.tiers?.message === "string" ? (
          <p role="alert" className="text-xs font-medium text-destructive">
            {errors.tiers.message}
          </p>
        ) : null}

        {podeEditar ? (
          <div>
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                faixas.append({ days_before_min: "", days_before_max: "", refund_pct: "0", label: "" })
              }
            >
              <Plus aria-hidden="true" />
              Acrescentar faixa
            </Button>
          </div>
        ) : null}
      </div>

      {buracos.length > 0 ? (
        <Nota variante="atencao">
          Nenhuma faixa cobre {buracos.join(", ")}. Um cancelamento com essa antecedência
          <strong> não devolve nada</strong> — o hóspede perderia o sinal por uma falha no cadastro, não
          por uma decisão sua.
        </Nota>
      ) : null}

      {erroGeral ? (
        <p role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erroGeral}
        </p>
      ) : null}

      {versaoPublicada !== null ? (
        <p role="status" className="flex items-center gap-2 rounded-lg bg-alcada-livre/12 px-3 py-2 text-sm text-alcada-livre">
          <Check className="size-4" aria-hidden="true" />
          Versão {versaoPublicada} publicada. As reservas antigas continuam com as regras da época em
          que foram feitas.
        </p>
      ) : null}

      <Nota variante="atencao">
        Salvar <strong>publica uma versão nova com todas as faixas</strong>. Ao cancelar uma reserva,
        valem as regras do dia em que <em>ela</em> foi feita, não estas — mudar as regras hoje não muda o
        que já foi prometido a quem reservou ontem.
      </Nota>

      {podeEditar ? (
        <div className="flex justify-end">
          <Button type="submit" disabled={isSubmitting}>
            {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Save aria-hidden="true" />}
            Publicar nova versão
          </Button>
        </div>
      ) : null}
    </form>
  );
}
