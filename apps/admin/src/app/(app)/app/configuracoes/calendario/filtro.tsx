"use client";

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select } from "@/components/ui/select";
import { TIPOS_DE_PERIODO } from "@/lib/comercial/tipos-de-data";

/**
 * Filtro do calendário — **na query string**, não em estado.
 *
 * "Me manda o que está cadastrado para o réveillon" tem que ser um link, e um
 * link que reabre exatamente a mesma tela. Estado interno perderia isso no
 * primeiro F5 e no primeiro copiar-e-colar.
 */
export function FiltroDoCalendario() {
  const router = useRouter();
  const parametros = useSearchParams();

  const de = parametros.get("de") ?? "";
  const ate = parametros.get("ate") ?? "";
  const tipo = parametros.get("tipo") ?? "";
  const temFiltro = de !== "" || ate !== "" || tipo !== "";

  function aplicar(chave: string, valor: string) {
    const proximos = new URLSearchParams(parametros.toString());
    if (valor === "") proximos.delete(chave);
    else proximos.set(chave, valor);
    const consulta = proximos.toString();
    router.replace(consulta ? `?${consulta}` : "/app/configuracoes/calendario");
  }

  return (
    <div className="flex flex-wrap items-end gap-3">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="filtro-de" className="text-xs text-muted-foreground">
          De
        </Label>
        <Input
          id="filtro-de"
          type="date"
          value={de}
          onChange={(evento) => aplicar("de", evento.target.value)}
          className="h-9 w-40 tabular-nums"
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="filtro-ate" className="text-xs text-muted-foreground">
          Até
        </Label>
        <Input
          id="filtro-ate"
          type="date"
          value={ate}
          onChange={(evento) => aplicar("ate", evento.target.value)}
          className="h-9 w-40 tabular-nums"
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="filtro-tipo" className="text-xs text-muted-foreground">
          Tipo de período
        </Label>
        <Select
          id="filtro-tipo"
          value={tipo}
          onChange={(evento) => aplicar("tipo", evento.target.value)}
          className="h-9 w-48"
        >
          <option value="">Todos</option>
          {TIPOS_DE_PERIODO.map((t) => (
            <option key={t.code} value={t.code}>
              {t.label}
            </option>
          ))}
        </Select>
      </div>

      {temFiltro ? (
        <Button size="sm" variant="ghost" onClick={() => router.replace("/app/configuracoes/calendario")}>
          <X aria-hidden="true" />
          Limpar filtros
        </Button>
      ) : null}
    </div>
  );
}
