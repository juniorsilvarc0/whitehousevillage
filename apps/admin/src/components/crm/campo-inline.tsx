"use client";

import * as React from "react";
import type { ZodType } from "zod";
import { Check, Loader2, RotateCcw } from "lucide-react";

import { notificar } from "@/components/crm/avisos";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { ResultadoCrm } from "@/lib/crm/api";
import { cn } from "@/lib/utils";

/**
 * Edição inline que salva no **`blur`**, sem botão Salvar.
 *
 * A tela da oportunidade é onde a gestão vive: o valor muda, a data muda, a
 * probabilidade muda, e cada mudança é uma frase de telefone. Um botão Salvar
 * por campo faria o operador ou clicar quinze vezes ou — o que acontece de
 * verdade — sair da tela com a alteração perdida.
 *
 * Três decisões que este componente carrega:
 *
 * 1. **Campo intocado não salva.** Comparar com o último valor confirmado evita
 *    um `PATCH` por passagem de foco, que enche o `audit_log` de linhas sem
 *    mudança nenhuma.
 * 2. **Recusa não apaga o que a pessoa digitou.** O texto fica, com o erro ao
 *    lado e um "desfazer" explícito. Reverter sozinho é a forma mais rápida de
 *    alguém perder um número que acabou de combinar por telefone.
 * 3. **`Esc` volta ao valor salvo, `Enter` confirma.** São os dois atalhos que
 *    todo mundo já tenta; sem eles o `Enter` dentro de um `<form>` submeteria
 *    outra coisa.
 */
export function CampoInline({
  id,
  label,
  valorInicial,
  esquema,
  aoSalvar,
  formatar,
  tipo = "text",
  sufixo,
  className,
  desabilitado = false,
  hint,
}: {
  id: string;
  label: string;
  /** O valor como se digita — string, sempre (mesma regra dos formulários). */
  valorInicial: string;
  esquema: ZodType<string>;
  aoSalvar: (valor: string) => Promise<ResultadoCrm<unknown>>;
  /** Como o valor confirmado aparece quando o campo não está em foco. */
  formatar?: (valor: string) => string;
  tipo?: "text" | "date" | "number";
  sufixo?: string;
  className?: string;
  desabilitado?: boolean;
  hint?: React.ReactNode;
}) {
  const [salvo, setSalvo] = React.useState(valorInicial);
  const [valor, setValor] = React.useState(valorInicial);
  const [estado, setEstado] = React.useState<"parado" | "salvando" | "confirmado" | "erro">("parado");
  const [erro, setErro] = React.useState<string | null>(null);

  // O servidor pode ter mudado o campo (outra pessoa editou, ou o `/stage`
  // reescreveu a probabilidade). Adota o valor novo no render, e só quando o
  // campo não está no meio de uma edição — senão a tela apagaria o que está
  // sendo digitado.
  const [inicialAnterior, setInicialAnterior] = React.useState(valorInicial);
  if (valorInicial !== inicialAnterior) {
    setInicialAnterior(valorInicial);
    if (estado !== "salvando" && valor === salvo) {
      setSalvo(valorInicial);
      setValor(valorInicial);
    }
  }

  async function confirmar() {
    if (valor === salvo) {
      setErro(null);
      setEstado("parado");
      return;
    }

    const analise = esquema.safeParse(valor);
    if (!analise.success) {
      setErro(analise.error.issues[0]?.message ?? "Valor inválido.");
      setEstado("erro");
      return;
    }

    setEstado("salvando");
    setErro(null);
    const resultado = await aoSalvar(valor);
    if (!resultado.ok) {
      // O toast conta o que houve; o campo guarda o texto e oferece o desfazer.
      notificar(resultado);
      setErro("Não foi salvo.");
      setEstado("erro");
      return;
    }
    setSalvo(valor);
    setEstado("confirmado");
  }

  function desfazer() {
    setValor(salvo);
    setErro(null);
    setEstado("parado");
  }

  const idErro = erro ? `${id}-erro` : undefined;
  const idDica = hint ? `${id}-dica` : undefined;

  return (
    <div className={cn("flex flex-col gap-1.5", className)}>
      <Label htmlFor={id} className="text-xs text-muted-foreground">
        {label}
      </Label>
      <div className="flex items-center gap-2">
        <div className="relative min-w-0 flex-1">
          <Input
            id={id}
            type={tipo}
            value={valor}
            disabled={desabilitado || estado === "salvando"}
            invalid={Boolean(erro)}
            aria-describedby={[idDica, idErro].filter(Boolean).join(" ") || undefined}
            onChange={(evento) => {
              setValor(evento.target.value);
              if (estado === "confirmado") setEstado("parado");
            }}
            onBlur={() => void confirmar()}
            onKeyDown={(evento) => {
              if (evento.key === "Enter") {
                evento.preventDefault();
                evento.currentTarget.blur();
              }
              if (evento.key === "Escape") {
                evento.preventDefault();
                desfazer();
              }
            }}
            className={cn("h-9 pr-8", tipo !== "text" && "tabular-nums")}
          />
          <span className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2">
            {estado === "salvando" ? (
              <Loader2 className="size-3.5 animate-spin text-muted-foreground" aria-hidden="true" />
            ) : null}
            {estado === "confirmado" ? (
              <Check className="size-3.5 text-alcada-livre" aria-label="Salvo" role="img" />
            ) : null}
          </span>
        </div>
        {sufixo ? <span className="shrink-0 text-xs text-muted-foreground">{sufixo}</span> : null}
        {erro ? (
          <button
            type="button"
            onClick={desfazer}
            className="flex shrink-0 items-center gap-1 rounded-md px-1.5 py-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <RotateCcw className="size-3" aria-hidden="true" />
            Desfazer
          </button>
        ) : null}
      </div>
      {hint ? (
        <p id={idDica} className="text-[0.68rem] leading-snug text-muted-foreground">
          {hint}
        </p>
      ) : null}
      {erro ? (
        <p id={idErro} role="alert" className="text-xs font-medium text-destructive">
          {erro} {formatar ? `Valor salvo: ${formatar(salvo)}.` : null}
        </p>
      ) : null}
    </div>
  );
}
