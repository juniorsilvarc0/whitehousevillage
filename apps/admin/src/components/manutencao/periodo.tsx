"use client";

import type { UseFormRegisterReturn } from "react-hook-form";

import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { ehDataISO, formatarData, hojeISO, somarDias } from "@/lib/datas";
import { cn } from "@/lib/utils";

/**
 * As duas datas do bloqueio — usadas na criação da ordem e no `PUT /block`.
 *
 * **Half-open**, como toda estadia: o segundo campo é o dia em que a unidade
 * **volta à venda** (fica livre), e o rótulo diz isso em vez de "Até", que
 * todo mundo leria como inclusivo. Quando as duas datas fazem sentido, a frase
 * de baixo escreve as noites que ficam bloqueadas — formatação, não regra.
 *
 * O `min` é só conveniência do seletor (dias passados aparecem apagados) e usa
 * o "hoje" do fuso da casa. Não decide nada: os formulários são `noValidate`,
 * e se o período vale quem diz é a API, cuja recusa chega em `erroDe`,
 * `erroAte` ou `erroDoPeriodo`.
 */
export function CamposDoPeriodo({
  prefixo,
  de,
  ate,
  registrarDe,
  registrarAte,
  erroDe,
  erroAte,
  erroDoPeriodo,
  className,
}: {
  prefixo: string;
  de: string;
  ate: string;
  registrarDe: UseFormRegisterReturn;
  registrarAte: UseFormRegisterReturn;
  erroDe?: string;
  erroAte?: string;
  /** O período inteiro: `DATE_CONFLICT` ou unidade inativa (`details.block`). */
  erroDoPeriodo?: string | null;
  className?: string;
}) {
  const hoje = hojeISO();
  const legivel = ehDataISO(de) && ehDataISO(ate) && ate > de;
  const ultima = legivel ? somarDias(ate, -1) : "";

  return (
    <fieldset className={cn("flex flex-col gap-3", className)}>
      <legend className="sr-only">Período do bloqueio</legend>
      <div className="grid gap-4 sm:grid-cols-2">
        <Campo id={`${prefixo}-de`} label="Primeiro dia bloqueado" obrigatorio erro={erroDe}>
          {(p) => <Input {...p} type="date" min={hoje} {...registrarDe} />}
        </Campo>
        <Campo id={`${prefixo}-ate`} label="Volta à venda em" obrigatorio erro={erroAte} hint="Este dia fica livre para check-in.">
          {(p) => <Input {...p} type="date" min={de && ehDataISO(de) ? de : hoje} {...registrarAte} />}
        </Campo>
      </div>
      <p className="text-xs text-muted-foreground" aria-live="polite">
        {legivel
          ? ultima === de
            ? `Bloqueia a noite de ${formatarData(de)}; ${formatarData(ate)} fica livre.`
            : `Bloqueia as noites de ${formatarData(de)} a ${formatarData(ultima)}; ${formatarData(ate)} fica livre.`
          : "Como numa reserva, o dia da saída fica livre: de 10 a 15 bloqueia as noites de 10 a 14."}
      </p>
      {erroDoPeriodo ? (
        <p role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erroDoPeriodo}
        </p>
      ) : null}
    </fieldset>
  );
}
