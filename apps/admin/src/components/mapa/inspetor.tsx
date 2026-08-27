"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { formatarBRL } from "@/lib/dinheiro";
import { formatarDataCurta } from "@/lib/datas";
import { classeDoTipo, rotuloDoTipo } from "@/lib/comercial/tipos-de-data";
import type { Janela } from "@/lib/mapa/janela";
import { expiracaoDeHold } from "@/lib/mapa/expiracao";
import { ROTULO_DO_STATUS } from "@/lib/mapa/rotulos";
import { chaveDePreco, type CelulaSintetica, type ExpiracaoDeHold, type IndiceDePrecos } from "@/lib/mapa/tipos";
import { useAgora } from "@/lib/tempo-real/relogio";
import { cn } from "@/lib/utils";

import type { AlvoDoDia } from "./grade";

/**
 * O inspetor: uma faixa fixa que descreve o dia sob o cursor.
 *
 * **Não é um tooltip flutuante, e a escolha é de leitura.** Um balão sobre uma
 * grade de 36px por coluna cobre os vizinhos — e o vizinho é exatamente o que
 * se está comparando ("quanto custa a noite seguinte?"). A faixa fica sempre no
 * mesmo lugar, então o olho aprende onde olhar e a mão não precisa parar de se
 * mover para ler.
 *
 * ## A tarifa vem do PRODUTO, nunca da unidade
 *
 * `GET /availability/units` não traz preço, e o contrato explica por quê: a
 * mesma AP-01 custa uma coisa vendida como Apartamento 2 Suítes e outra dentro
 * da White House Completa. O preço aqui sai de `GET /availability`, indexado por
 * (produto da linha, dia) — a linha sabe qual é o produto dela porque o
 * agrupamento veio da composição.
 */
export function InspetorDoDia({
  alvo,
  janela,
  precos,
  expiracoes,
  className,
}: {
  alvo: AlvoDoDia | null;
  janela: Janela;
  precos: IndiceDePrecos;
  expiracoes: ExpiracaoDeHold;
  className?: string;
}) {
  const agora = useAgora();

  if (!alvo) {
    return (
      <div
        className={cn(
          "flex h-11 items-center gap-2 rounded-lg border border-border/60 bg-muted/20 px-3 text-xs text-muted-foreground",
          className,
        )}
      >
        Passe o cursor por uma noite para ver a tarifa, o tipo de data e quem está na casa.
      </div>
    );
  }

  const { linha, indice } = alvo;
  const data = janela.dias[indice];
  const celula = linha.celulas[indice];
  if (!data || !celula) return null;

  const preco = linha.unitTypeId ? precos.get(chaveDePreco(linha.unitTypeId, data)) : undefined;
  const expiracao =
    celula.status === "hold" && celula.reservation_id
      ? expiracaoDeHold(expiracoes.get(celula.reservation_id), agora)
      : null;
  const bloqueadaPor = (celula as CelulaSintetica).bloqueadaPor ?? [];

  return (
    <div
      className={cn(
        "flex h-11 flex-wrap items-center gap-x-3 gap-y-1 overflow-hidden rounded-lg border border-border/60 bg-muted/20 px-3 text-xs",
        className,
      )}
      // A faixa muda a cada movimento do ponteiro. Anunciar cada mudança
      // transformaria o leitor de tela num locutor de esporte; o resumo da
      // linha, que já existe, é o caminho acessível para o mesmo dado.
      aria-hidden="true"
    >
      <span className="font-mono font-medium text-foreground">{linha.codigo}</span>
      <span className="text-muted-foreground">{formatarDataCurta(data)}</span>
      <Badge className={classeDoTipo(celula.date_type)}>{rotuloDoTipo(celula.date_type)}</Badge>

      {preco?.price_cents != null ? (
        <span className="tabular-nums font-medium text-foreground">{formatarBRL(preco.price_cents)}</span>
      ) : (
        <span className="text-muted-foreground">
          {linha.unitTypeId ? "sem tarifa cadastrada" : "sem produto"}
        </span>
      )}

      {preco && preco.min_nights > 1 ? (
        <span className="text-muted-foreground">mín. {preco.min_nights} noites</span>
      ) : null}

      <span className="h-4 w-px bg-border" />

      <span className={cn("font-medium", celula.status === "livre" ? "text-muted-foreground" : "text-foreground")}>
        {ROTULO_DO_STATUS[celula.status]}
      </span>

      {celula.reservation_code ? (
        <span className="font-mono text-muted-foreground">{celula.reservation_code}</span>
      ) : null}
      {celula.guest_name ? <span className="truncate text-muted-foreground">{celula.guest_name}</span> : null}
      {expiracao ? (
        <span
          className={cn(
            "tabular-nums font-medium",
            expiracao.urgencia === "critica" || expiracao.urgencia === "expirada"
              ? "text-destructive"
              : "text-alcada-atencao",
          )}
        >
          {expiracao.texto}
        </span>
      ) : null}
      {bloqueadaPor.length > 0 ? (
        <span className="truncate text-muted-foreground">ocupada em {bloqueadaPor.join(", ")}</span>
      ) : null}
    </div>
  );
}
