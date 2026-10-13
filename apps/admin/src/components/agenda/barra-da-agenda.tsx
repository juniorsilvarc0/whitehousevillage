"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { ChevronLeft, ChevronRight, SlidersHorizontal } from "lucide-react";

import { Button, buttonVariants } from "@/components/ui/button";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Select } from "@/components/ui/select";
import { ROTULO_DO_TIPO } from "@/lib/agenda/rotulos";
import {
  ROTULO_DA_VISAO,
  VISOES,
  dataVizinha,
  paraQueryString,
  tituloDaJanela,
  type FiltrosDaAgenda,
  type Janela,
  type Visao,
} from "@/lib/agenda/visoes";
import { TIPOS_DE_EVENTO, type TipoDeEvento, type UnidadeDaAgenda } from "@/lib/agenda/tipos";
import { cn } from "@/lib/utils";

/**
 * A barra da agenda: visão, período, filtros. **Tudo na query string** — o
 * recorte é um link, e o botão Voltar do navegador continua significando
 * "a tela anterior".
 *
 * Trocar de visão, andar para trás e para a frente são `<Link>` de verdade (o
 * Next pré-carrega o vizinho); os filtros, que mudam por `<select>`, usam
 * `router.replace` — rolar a temporada filtro a filtro não pode encher o
 * histórico.
 *
 * "Hoje" leva ao `hoje` que chegou do servidor (`settings.today`): é a URL sem
 * `data`, que a página lê como o dia da casa.
 */
export function BarraDaAgenda({
  filtros,
  janela,
  hoje,
  unidades,
  indicador,
}: {
  filtros: FiltrosDaAgenda;
  janela: Janela;
  hoje: string;
  unidades: readonly UnidadeDaAgenda[] | null;
  indicador?: React.ReactNode;
}) {
  const router = useRouter();
  const caminho = usePathname();
  const [, iniciar] = React.useTransition();

  const href = (novos: Partial<FiltrosDaAgenda>) => `${caminho}${paraQueryString({ ...filtros, ...novos }, hoje)}`;
  const aplicar = (novos: Partial<FiltrosDaAgenda>) => iniciar(() => router.replace(href(novos), { scroll: false }));

  const [filtrosAbertos, setFiltrosAbertos] = React.useState(false);
  const ativos = [filtros.tipo, filtros.unidade, filtros.responsavel, filtros.cancelados].filter(Boolean).length;
  const mostraHoje = hoje >= janela.from && hoje < janela.to;
  const titulo = tituloDaJanela(filtros.visao, filtros.data, janela);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <nav aria-label="Visão da agenda" className="flex w-full rounded-full border border-border/60 bg-muted/30 p-0.5 sm:w-auto">
          {VISOES.map((v: Visao) => {
            const ativa = v === filtros.visao;
            return (
              <Link
                key={v}
                href={href({ visao: v })}
                aria-current={ativa ? "page" : undefined}
                scroll={false}
                className={cn(
                  "flex h-9 flex-1 items-center justify-center rounded-full px-4 text-sm font-medium outline-none transition-colors sm:flex-none",
                  "focus-visible:ring-2 focus-visible:ring-ring",
                  ativa ? "bg-brand-gradient text-white" : "text-muted-foreground hover:bg-muted hover:text-foreground",
                )}
              >
                {ROTULO_DA_VISAO[v]}
              </Link>
            );
          })}
        </nav>

        <div className="flex w-full items-center gap-2 sm:w-auto">
          <Link
            href={href({ data: dataVizinha(filtros.visao, filtros.data, -1) })}
            scroll={false}
            aria-label="Período anterior"
            className={buttonVariants({ variant: "outline", size: "icon" })}
          >
            <ChevronLeft aria-hidden="true" />
          </Link>
          <Link
            href={href({ data: hoje })}
            scroll={false}
            aria-label="Ir para hoje"
            className={cn(buttonVariants({ variant: mostraHoje ? "secondary" : "outline" }), "flex-1 sm:flex-none")}
          >
            Hoje
          </Link>
          <Link
            href={href({ data: dataVizinha(filtros.visao, filtros.data, 1) })}
            scroll={false}
            aria-label="Próximo período"
            className={buttonVariants({ variant: "outline", size: "icon" })}
          >
            <ChevronRight aria-hidden="true" />
          </Link>
        </div>
      </div>

      <div className="flex flex-wrap items-end justify-between gap-3">
        <h2 className="font-display text-lg leading-tight first-letter:uppercase sm:text-xl" aria-live="polite">
          {titulo}
        </h2>
        {indicador}
      </div>

      {/* No celular os filtros ficam atrás de um botão: quatro campos empilhados
          empurrariam a grade para a segunda tela, e quem abre a agenda de pé
          quer ver o dia, não o recorte. */}
      <Button
        variant="outline"
        size="sm"
        className="self-start sm:hidden"
        aria-expanded={filtrosAbertos}
        aria-controls="agenda-filtros"
        onClick={() => setFiltrosAbertos((a) => !a)}
      >
        <SlidersHorizontal aria-hidden="true" />
        {ativos > 0 ? `Filtros (${ativos})` : "Filtros"}
      </Button>

      <div id="agenda-filtros" className={cn("flex-wrap items-end gap-3 sm:flex", filtrosAbertos ? "flex" : "hidden")}>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="agenda-tipo" className="text-xs text-muted-foreground">
            Tipo
          </Label>
          <Select
            id="agenda-tipo"
            value={filtros.tipo}
            onChange={(e) => aplicar({ tipo: e.target.value as TipoDeEvento | "" })}
            className="h-9 w-40"
          >
            <option value="">Todos</option>
            {TIPOS_DE_EVENTO.map((t) => (
              <option key={t} value={t}>
                {ROTULO_DO_TIPO[t]}
              </option>
            ))}
          </Select>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="agenda-responsavel" className="text-xs text-muted-foreground">
            Responsável
          </Label>
          <Select
            id="agenda-responsavel"
            value={filtros.responsavel}
            onChange={(e) => aplicar({ responsavel: e.target.value as FiltrosDaAgenda["responsavel"] })}
            className="h-9 w-44"
          >
            <option value="">Todos</option>
            <option value="eu">Comigo</option>
            <option value="ninguem">Sem responsável</option>
          </Select>
        </div>
        {unidades && unidades.length > 0 ? (
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="agenda-unidade" className="text-xs text-muted-foreground">
              Unidade
            </Label>
            <Select
              id="agenda-unidade"
              value={filtros.unidade}
              onChange={(e) => aplicar({ unidade: e.target.value })}
              className="h-9 w-44"
            >
              <option value="">Todas</option>
              {unidades.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.code}
                </option>
              ))}
            </Select>
          </div>
        ) : null}
        <CheckboxCampo
          id="agenda-cancelados"
          label="Mostrar desmarcados"
          checked={filtros.cancelados}
          onChange={(e) => aplicar({ cancelados: e.target.checked })}
          className="pb-2"
        />
      </div>
    </div>
  );
}
