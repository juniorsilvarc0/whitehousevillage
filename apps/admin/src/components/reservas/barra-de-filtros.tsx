"use client";

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Search, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select } from "@/components/ui/select";
import type { Produto } from "@/lib/api/comercial";
import { aplicar, ORDENACAO_PADRAO, ORDENACOES, RECORTES } from "@/lib/reservas/filtros";
import { ESTADOS, ESTADOS_EM_ORDEM } from "@/lib/reservas/estados";
import { cn } from "@/lib/utils";

/**
 * O recorte da lista — **na query string**, sempre.
 *
 * "Me manda as pré-reservas que vencem essa semana" precisa virar um link que
 * reabre a mesma tela. Filtro em estado local morre no F5 e não se cola no
 * WhatsApp, que é por onde a gestão pede as coisas.
 *
 * Os atalhos de cima não são um `<select>` bonito: são as cinco perguntas que a
 * operação faz de verdade. O `<select>` de situação continua embaixo para quem
 * quer um estado específico — os dois escrevem o mesmo parâmetro, e um atalho
 * fica marcado quando o valor bate exatamente.
 */
export function BarraDeFiltros({ produtos, caminho = "/app/reservas" }: { produtos: Produto[]; caminho?: string }) {
  const router = useRouter();
  const parametros = useSearchParams();

  const status = parametros.get("status") ?? "";
  const de = parametros.get("from") ?? "";
  const ate = parametros.get("to") ?? "";
  const produto = parametros.get("unit_type_id") ?? "";
  const busca = parametros.get("q") ?? "";
  const ordem = parametros.get("sort") ?? "";
  const temFiltro = Boolean(status || de || ate || produto || busca || ordem);

  const [rascunho, setRascunho] = React.useState(busca);
  const [buscaAnterior, setBuscaAnterior] = React.useState(busca);
  if (busca !== buscaAnterior) {
    setBuscaAnterior(busca);
    setRascunho(busca);
  }

  function ir(chave: string, valor: string) {
    router.replace(`${caminho}${aplicar(parametros, chave, valor)}`);
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        {RECORTES.map((recorte) => {
          const ativo = status === recorte.status;
          return (
            <button
              key={recorte.id}
              type="button"
              aria-pressed={ativo}
              onClick={() => ir("status", ativo ? "" : recorte.status)}
              className={cn(
                "rounded-full border px-3 py-1 text-xs transition-colors",
                ativo
                  ? "border-primary bg-accent text-accent-foreground"
                  : "border-border bg-card text-muted-foreground hover:bg-muted",
              )}
            >
              {recorte.rotulo}
            </button>
          );
        })}
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="reserva-status" className="text-xs text-muted-foreground">
            Situação
          </Label>
          <Select
            id="reserva-status"
            value={status}
            onChange={(evento) => ir("status", evento.target.value)}
            className="h-9 w-48"
          >
            <option value="">Todas</option>
            {/* Só os recortes que juntam mais de um estado entram aqui: um
                recorte de estado único teria o mesmo `value` da opção individual
                logo abaixo, e `<select>` com dois valores iguais marca o
                primeiro — a tela mostraria "Pré-reservas" onde o usuário
                escolheu "Pré-reserva", ou vice-versa, sem nenhuma diferença
                real por trás. */}
            <optgroup label="Recortes">
              {RECORTES.filter((recorte) => recorte.status.includes(",")).map((recorte) => (
                <option key={recorte.id} value={recorte.status}>
                  {recorte.rotulo}
                </option>
              ))}
            </optgroup>
            <optgroup label="Situação">
              {ESTADOS_EM_ORDEM.map((estado) => (
                <option key={estado} value={estado}>
                  {ESTADOS[estado].rotulo}
                </option>
              ))}
            </optgroup>
          </Select>
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="reserva-de" className="text-xs text-muted-foreground">
            Estadias a partir de
          </Label>
          <Input
            id="reserva-de"
            type="date"
            value={de}
            onChange={(evento) => ir("from", evento.target.value)}
            className="h-9 w-40 tabular-nums"
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="reserva-ate" className="text-xs text-muted-foreground">
            até
          </Label>
          <Input
            id="reserva-ate"
            type="date"
            value={ate}
            onChange={(evento) => ir("to", evento.target.value)}
            className="h-9 w-40 tabular-nums"
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="reserva-produto" className="text-xs text-muted-foreground">
            Produto
          </Label>
          <Select
            id="reserva-produto"
            value={produto}
            onChange={(evento) => ir("unit_type_id", evento.target.value)}
            className="h-9 w-52"
          >
            <option value="">Todos</option>
            {produtos.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name}
              </option>
            ))}
          </Select>
        </div>

        <form
          className="flex flex-col gap-1.5"
          onSubmit={(evento) => {
            evento.preventDefault();
            ir("q", rascunho.trim());
          }}
        >
          <Label htmlFor="reserva-busca" className="text-xs text-muted-foreground">
            Buscar
          </Label>
          <div className="flex items-center gap-2">
            <Input
              id="reserva-busca"
              value={rascunho}
              onChange={(evento) => setRascunho(evento.target.value)}
              placeholder="WH-2026-0001 ou nome do hóspede"
              className="h-9 w-60"
            />
            <Button type="submit" size="sm" variant="outline" aria-label="Buscar">
              <Search aria-hidden="true" />
            </Button>
          </div>
        </form>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="reserva-ordem" className="text-xs text-muted-foreground">
            Ordenar por
          </Label>
          <Select
            id="reserva-ordem"
            // `value` cai no padrão quando a URL não traz `sort`: um `<select>`
            // controlado com valor fora das opções renderiza sem nada marcado, e
            // a tela mentiria sobre a ordenação que está aplicando.
            value={ordem || ORDENACAO_PADRAO}
            onChange={(evento) => ir("sort", evento.target.value)}
            className="h-9 w-52"
          >
            {ORDENACOES.map((opcao) => (
              <option key={opcao.valor} value={opcao.valor}>
                {opcao.rotulo}
              </option>
            ))}
          </Select>
        </div>

        {temFiltro ? (
          <Button size="sm" variant="ghost" onClick={() => router.replace(caminho)}>
            <X aria-hidden="true" />
            Limpar
          </Button>
        ) : null}
      </div>
    </div>
  );
}
