"use client";

import * as React from "react";
import { CalendarClock, ChevronLeft, ChevronRight, Wrench } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import type { Produto } from "@/lib/api/comercial";
import { hojeISO, somarDias } from "@/lib/datas";
import type { FiltrosDoMapa } from "@/lib/mapa/filtros";
import { cn } from "@/lib/utils";

/**
 * A barra do mapa: onde estou olhando, o que estou vendo, e o que está vivo.
 *
 * A ordem dos controles é a canônica de `docs/ui.md` §8 — navegação → faixa →
 * filtros → ações. O que muda aqui é que **todo controle escreve na query
 * string**: a barra não guarda estado, ela edita uma URL. É o que torna "olha o
 * Réveillon, a COB-01 está livre" um link, e não um roteiro de cliques.
 */

/** Tamanhos de janela que a operação de fato usa. 180 e 366 existem para o
 *  planejamento de temporada; o padrão continua sendo o trimestre. */
const TAMANHOS = [30, 60, 90, 180, 366] as const;

export function BarraDoMapa({
  filtros,
  produtos,
  podeBloquear,
  aoMudar,
  aoBloquear,
  indicador,
  className,
}: {
  filtros: FiltrosDoMapa;
  produtos: readonly Produto[];
  podeBloquear: boolean;
  aoMudar: (filtros: FiltrosDoMapa) => void;
  aoBloquear: () => void;
  indicador: React.ReactNode;
  className?: string;
}) {
  const hoje = hojeISO();
  const passo = Math.max(7, Math.round(filtros.dias / 3));

  return (
    <div className={cn("flex flex-wrap items-center gap-2", className)}>
      <div className="flex items-center gap-1">
        <Button
          variant="outline"
          size="iconSm"
          aria-label={`Voltar ${passo} dias`}
          onClick={() => aoMudar({ ...filtros, from: somarDias(filtros.from, -passo) })}
        >
          <ChevronLeft />
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => aoMudar({ ...filtros, from: somarDias(hoje, -7) })}
        >
          <CalendarClock />
          Hoje
        </Button>
        <Button
          variant="outline"
          size="iconSm"
          aria-label={`Avançar ${passo} dias`}
          onClick={() => aoMudar({ ...filtros, from: somarDias(filtros.from, passo) })}
        >
          <ChevronRight />
        </Button>
      </div>

      <Input
        type="date"
        aria-label="Primeiro dia da faixa"
        className="h-8 w-[9.5rem] px-2 text-xs"
        value={filtros.from}
        onChange={(evento) => {
          const valor = evento.target.value;
          // `<input type="date">` entrega "" enquanto a pessoa digita; aplicar
          // isso jogaria a janela para o começo dos tempos a cada tecla.
          if (valor) aoMudar({ ...filtros, from: valor });
        }}
      />

      <Select
        aria-label="Tamanho da faixa"
        className="h-8 w-[7.5rem] px-2 py-0 text-xs"
        value={String(filtros.dias)}
        onChange={(evento) => aoMudar({ ...filtros, dias: Number(evento.target.value) })}
      >
        {TAMANHOS.map((dias) => (
          <option key={dias} value={dias}>
            {dias} dias
          </option>
        ))}
      </Select>

      <Select
        aria-label="Produto"
        className="h-8 w-[13rem] px-2 py-0 text-xs"
        value={filtros.unitTypeId ?? "todos"}
        onChange={(evento) =>
          aoMudar({
            ...filtros,
            unitTypeId: evento.target.value === "todos" ? null : evento.target.value,
          })
        }
      >
        <option value="todos">Todos os produtos</option>
        {produtos
          .filter((produto) => produto.active)
          .map((produto) => (
            <option key={produto.id} value={produto.id}>
              {produto.name}
            </option>
          ))}
      </Select>

      <label className="flex cursor-pointer items-center gap-2 text-xs text-muted-foreground">
        <Checkbox
          checked={filtros.somenteOcupadas}
          onChange={(evento) => aoMudar({ ...filtros, somenteOcupadas: evento.target.checked })}
        />
        Só linhas com ocupação
      </label>

      <div className="ml-auto flex items-center gap-2">
        {indicador}
        {podeBloquear ? (
          <Button variant="outline" size="sm" onClick={aoBloquear}>
            <Wrench />
            Bloquear
          </Button>
        ) : null}
      </div>
    </div>
  );
}
