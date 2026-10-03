"use client";

import * as React from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Check, Loader2, Save } from "lucide-react";

import { SemaforoDeAlcada } from "@/components/comercial/semaforo-de-alcada";
import { Nota } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import type { PoliticaComercial } from "@/lib/api/comercial";
import { aplicarFalha } from "@/lib/acoes/formulario";
import { paraNumero } from "@/lib/acoes/campos";
import { formatarData, hojeISO } from "@/lib/datas";
import { reaisDeCentavos } from "@/lib/dinheiro";

import { publicarPoliticaComercial } from "./acoes";
import { PoliticaComercialFormulario } from "./esquemas";

function valoresDaPolitica(politica: PoliticaComercial | null): PoliticaComercialFormulario {
  return {
    deposit_pct: String(politica?.deposit_pct ?? 50),
    balance_due_days: String(politica?.balance_due_days ?? 7),
    hold_hours: String(politica?.hold_hours ?? 48),
    discount_auto_pct: String(politica?.discount_auto_pct ?? 5),
    discount_approval_pct: String(politica?.discount_approval_pct ?? 10),
    event_deposit_cents: reaisDeCentavos(politica?.event_deposit_cents ?? 0),
    // A versão nova vale de hoje: antedatar é `409 POLICY_IMMUTABLE`, e não
    // faria sentido — a vigente já valeu no período que passou.
    valid_from: hojeISO(),
  };
}

/**
 * Sinal, prazos, pré-reserva e alçadas.
 *
 * O semáforo em cima dos dois campos de alçada não é enfeite: os números 5 e 10
 * só significam alguma coisa quando se vê o efeito deles na negociação, e é a
 * única forma de a reunião com os proprietários decidir se a faixa do meio é
 * larga demais.
 */
export function FormularioComercial({
  politica,
  podeEditar,
}: {
  politica: PoliticaComercial | null;
  podeEditar: boolean;
}) {
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const [versaoPublicada, setVersaoPublicada] = React.useState<number | null>(null);

  const form = useForm<PoliticaComercialFormulario>({
    resolver: zodResolver(PoliticaComercialFormulario),
    defaultValues: valoresDaPolitica(politica),
  });

  const { errors, isSubmitting } = form.formState;

  // `useWatch` em vez de `form.watch()` — veja o comentário no formulário de
  // cancelamento.
  const auto = paraNumero(useWatch({ control: form.control, name: "discount_auto_pct" }) || "0");
  const aprovacao = paraNumero(useWatch({ control: form.control, name: "discount_approval_pct" }) || "0");
  const limites = {
    auto: Number.isFinite(auto) ? auto : 0,
    aprovacao: Number.isFinite(aprovacao) ? aprovacao : 0,
  };

  async function enviar(valores: PoliticaComercialFormulario) {
    setErroGeral(null);
    setVersaoPublicada(null);
    const resultado = await publicarPoliticaComercial(valores);
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

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        <Campo
          id="politica-deposit"
          label="Sinal para confirmar (%)"
          obrigatorio
          erro={errors.deposit_pct?.message}
          hint="Percentual do total cobrado para a pré-reserva virar reserva confirmada."
        >
          {(p) => <Input {...p} {...form.register("deposit_pct")} inputMode="decimal" className="text-right tabular-nums" disabled={!podeEditar} />}
        </Campo>

        <Campo
          id="politica-balance"
          label="Saldo vence (dias antes)"
          obrigatorio
          erro={errors.balance_due_days?.message}
          hint="Quantos dias antes do check-in o restante precisa estar quitado."
        >
          {(p) => <Input {...p} {...form.register("balance_due_days")} inputMode="numeric" className="text-right tabular-nums" disabled={!podeEditar} />}
        </Campo>

        <Campo
          id="politica-hold"
          label="Prazo da pré-reserva (horas)"
          obrigatorio
          erro={errors.hold_hours?.message}
          hint="Se o sinal não for pago nesse prazo, a pré-reserva vence e as datas voltam a ficar livres automaticamente."
        >
          {(p) => <Input {...p} {...form.register("hold_hours")} inputMode="numeric" className="text-right tabular-nums" disabled={!podeEditar} />}
        </Campo>

        <Campo
          id="politica-caucao"
          label="Caução de evento (R$)"
          obrigatorio
          erro={errors.event_deposit_cents?.message}
          hint="Valor devolvido depois do evento. O desconto não se aplica a ela."
        >
          {(p) => <Input {...p} {...form.register("event_deposit_cents")} inputMode="decimal" className="text-right tabular-nums" disabled={!podeEditar} />}
        </Campo>

        <Campo
          id="politica-auto"
          label="Desconto que a gestão dá sozinha, até (%)"
          obrigatorio
          erro={errors.discount_auto_pct?.message}
          hint="Até este percentual a gestão fecha sem pedir aprovação."
        >
          {(p) => <Input {...p} {...form.register("discount_auto_pct")} inputMode="decimal" className="text-right tabular-nums" disabled={!podeEditar} />}
        </Campo>

        <Campo
          id="politica-aprovacao"
          label="Com aprovação do proprietário até (%)"
          obrigatorio
          erro={errors.discount_approval_pct?.message}
          hint="Acima disso o sistema não deixa fazer o orçamento."
        >
          {(p) => <Input {...p} {...form.register("discount_approval_pct")} inputMode="decimal" className="text-right tabular-nums" disabled={!podeEditar} />}
        </Campo>
      </div>

      <div className="rounded-xl border border-border/60 bg-card/60 p-3.5">
        <p className="mb-2 text-xs font-medium">Como estes dois limites aparecem na negociação</p>
        <SemaforoDeAlcada pct={limites.auto} limites={limites} />
      </div>

      <Campo
        id="politica-validfrom"
        label="Esta versão vale a partir de"
        obrigatorio
        erro={errors.valid_from?.message}
        hint="Não pode ser antes do início da versão atual — uma regra nova não vale para trás."
      >
        {(p) => <Input {...p} type="date" {...form.register("valid_from")} className="tabular-nums" disabled={!podeEditar} />}
      </Campo>

      {erroGeral ? (
        <p role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erroGeral}
        </p>
      ) : null}

      {versaoPublicada !== null ? (
        <p role="status" className="flex items-center gap-2 rounded-lg bg-alcada-livre/12 px-3 py-2 text-sm text-alcada-livre">
          <Check className="size-4" aria-hidden="true" />
          Versão {versaoPublicada} publicada. As reservas anteriores continuam com as regras da época.
        </p>
      ) : null}

      <Nota variante="atencao">
        Salvar <strong>publica uma versão nova</strong> — não edita a atual. Cada reserva segue até o fim
        as regras que valiam quando foi feita: sinal, prazo do saldo, prazo da pré-reserva e limite de
        desconto. Nada do que já foi vendido muda por causa deste botão.
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
