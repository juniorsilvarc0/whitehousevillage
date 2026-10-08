"use client";

import * as React from "react";
import { Check, Minus, Plus, Undo2 } from "lucide-react";

import { FotoDoBem } from "@/components/bens/foto";
import { Button } from "@/components/ui/button";
import {
  aplicarPasso,
  estadoDaLinha,
  exibicaoDaContagem,
  lerDigitado,
  rotuloDoEstado,
  type EstadoDaLinha,
} from "@/lib/bens/contagem";
import { quantidadeComMedida } from "@/lib/bens/rotulos";
import type { LinhaDeConferencia } from "@/lib/bens/tipos";
import { formatarBRL } from "@/lib/dinheiro";
import { cn } from "@/lib/utils";

const MOLDURA: Record<EstadoDaLinha, string> = {
  // Pendente é TRACEJADA: é a única moldura assim na tela, e é o que faz o olho
  // achar o que falta contar sem ler etiqueta nenhuma.
  pendente: "border-dashed border-foreground/25 bg-card/50",
  confere: "border-alcada-livre/45 bg-card/80",
  falta: "border-destructive/45 bg-destructive/5",
  sobra: "border-alcada-atencao/50 bg-alcada-atencao/5",
};

const ETIQUETA: Record<EstadoDaLinha, string> = {
  pendente: "border border-dashed border-foreground/30 text-muted-foreground",
  confere: "bg-alcada-livre/12 text-alcada-livre",
  falta: "bg-destructive/12 text-destructive",
  sobra: "bg-alcada-atencao/15 text-foreground",
};

/**
 * Uma linha da conferência — o que a pessoa vê com a pilha de pratos na frente.
 *
 * ## `null` e `0` não se parecem
 *
 * - **Não contado** (`null`): moldura tracejada, traço (`—`) no contador,
 *   etiqueta "Não contado".
 * - **Contei e não achei** (`0`): moldura cheia em vermelho, o número `0` em
 *   destaque, etiqueta "Nenhum encontrado (falta N)".
 *
 * E nenhum gesto converte um no outro por acidente: o `−` de uma linha pendente
 * parte do esperado (12 → 11), apagar o campo não desfaz nada, e voltar a
 * pendente é um botão próprio — "Desfazer".
 *
 * Toque grande: os botões têm 48 px, que é o alvo que um dedo acerta com o
 * celular numa mão e a taça na outra.
 */
export function LinhaDaContagem({
  linha,
  podeContar,
  aoMudar,
}: {
  linha: Pick<
    LinhaDeConferencia,
    "id" | "item_name" | "item_description" | "item_unit_measure" | "cover" | "expected_qty" | "counted_qty"
  > &
    Partial<Pick<LinhaDeConferencia, "replacement_cost_cents">>;
  podeContar: boolean;
  aoMudar: (valor: number | null) => void;
}) {
  const estado = estadoDaLinha(linha);
  const nome = linha.item_name ?? "Bem";
  const [rascunho, setRascunho] = React.useState<string | null>(null);

  const exibido = rascunho ?? (linha.counted_qty === null ? "" : String(linha.counted_qty));

  return (
    <article
      aria-label={nome}
      data-estado={estado}
      className={cn("flex flex-col gap-3 rounded-xl border-2 p-3 transition-colors", MOLDURA[estado])}
    >
      <div className="flex gap-3">
        <FotoDoBem midia={linha.cover} alt={nome} className="size-16 sm:size-20" />
        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-medium leading-snug">{nome}</h3>
          {linha.item_description ? <p className="line-clamp-2 text-xs text-muted-foreground">{linha.item_description}</p> : null}
          <p className="mt-1 text-xs text-muted-foreground">
            Esperado: <strong className="tabular-nums text-foreground">{quantidadeComMedida(linha.expected_qty, linha.item_unit_measure)}</strong>
          </p>
          <span className={cn("mt-1.5 inline-flex rounded-full px-2 py-0.5 text-[0.72rem] font-medium", ETIQUETA[estado])}>
            {rotuloDoEstado(linha)}
          </span>
        </div>
      </div>

      {podeContar ? (
        <div className="flex flex-wrap items-center gap-2">
          {linha.counted_qty !== linha.expected_qty ? (
            <Button
              variant="outline"
              className="h-12 flex-1 basis-40 border-alcada-livre/50 text-sm"
              onClick={() => aoMudar(linha.expected_qty)}
              aria-label={`${nome}: conferido igual ao esperado, ${linha.expected_qty}`}
            >
              <Check aria-hidden="true" />
              Confere ({linha.expected_qty})
            </Button>
          ) : null}

          <div className="flex items-center gap-1.5">
            <Button
              variant="secondary"
              className="size-12 p-0 [&_svg]:size-5"
              aria-label={`Um a menos de ${nome}`}
              disabled={linha.counted_qty === 0}
              onClick={() => aoMudar(aplicarPasso(linha.counted_qty, linha.expected_qty, -1))}
            >
              <Minus aria-hidden="true" />
            </Button>
            <input
              type="text"
              inputMode="numeric"
              pattern="[0-9]*"
              enterKeyHint="done"
              aria-label={`Quantidade contada de ${nome}`}
              placeholder={exibicaoDaContagem(null)}
              value={exibido}
              onFocus={(e) => {
                setRascunho(exibido);
                e.currentTarget.select();
              }}
              onChange={(e) => {
                const texto = e.target.value.replace(/\D/g, "");
                setRascunho(texto);
                const lido = lerDigitado(texto);
                // Vazio não mexe: apagar o campo para redigitar não pode virar
                // "desfazer a contagem" no meio do caminho.
                if (typeof lido === "number") aoMudar(lido);
              }}
              onBlur={() => setRascunho(null)}
              onKeyDown={(e) => {
                if (e.key === "Enter") e.currentTarget.blur();
              }}
              className={cn(
                "h-12 w-16 rounded-xl border bg-card text-center text-lg font-semibold tabular-nums outline-none",
                "focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/35",
                linha.counted_qty === null ? "border-dashed border-foreground/30 placeholder:text-muted-foreground" : "border-input",
                estado === "falta" && "text-destructive",
              )}
            />
            <Button
              variant="secondary"
              className="size-12 p-0 [&_svg]:size-5"
              aria-label={`Um a mais de ${nome}`}
              onClick={() => aoMudar(aplicarPasso(linha.counted_qty, linha.expected_qty, 1))}
            >
              <Plus aria-hidden="true" />
            </Button>
          </div>

          {linha.counted_qty !== null ? (
            <Button variant="ghost" size="sm" onClick={() => aoMudar(null)} aria-label={`Desfazer a contagem de ${nome}`}>
              <Undo2 aria-hidden="true" />
              Desfazer
            </Button>
          ) : null}
        </div>
      ) : (
        <p className="text-sm">
          Contado:{" "}
          <strong className={cn("tabular-nums", estado === "falta" && "text-destructive")}>
            {exibicaoDaContagem(linha.counted_qty)}
          </strong>
          {typeof linha.replacement_cost_cents === "number" ? (
            // Congelado no fechamento: é o custo da época, não o do catálogo de hoje.
            <span className="ml-3 text-xs text-muted-foreground">
              custo no fechamento: <span className="font-mono tabular-nums">{formatarBRL(linha.replacement_cost_cents)}</span>
            </span>
          ) : null}
        </p>
      )}
    </article>
  );
}
