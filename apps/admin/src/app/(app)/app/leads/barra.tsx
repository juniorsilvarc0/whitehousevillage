"use client";

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Search, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select } from "@/components/ui/select";

/** Filtros do painel de leads — **na query string**, para o recorte ser um link
 *  que reabre a mesma tela. "Me manda os leads de WhatsApp ainda não atendidos"
 *  é uma frase que precisa virar URL. */
export function BarraDeLeads() {
  const router = useRouter();
  const parametros = useSearchParams();

  const status = parametros.get("status") ?? "";
  const origem = parametros.get("source") ?? "";
  const busca = parametros.get("q") ?? "";
  const temFiltro = status !== "" || origem !== "" || busca !== "";

  const [rascunho, setRascunho] = React.useState(busca);
  const [buscaAnterior, setBuscaAnterior] = React.useState(busca);
  if (busca !== buscaAnterior) {
    setBuscaAnterior(busca);
    setRascunho(busca);
  }

  function aplicar(chave: string, valor: string) {
    const proximos = new URLSearchParams(parametros.toString());
    if (valor === "") proximos.delete(chave);
    else proximos.set(chave, valor);
    const consulta = proximos.toString();
    router.replace(consulta ? `?${consulta}` : "/app/leads");
  }

  return (
    <div className="flex flex-wrap items-end gap-3">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="lead-status" className="text-xs text-muted-foreground">
          Situação
        </Label>
        <Select
          id="lead-status"
          value={status}
          onChange={(evento) => aplicar("status", evento.target.value)}
          className="h-9 w-52"
        >
          <option value="">Todas</option>
          <option value="novo,em_atendimento,qualificado">Em aberto</option>
          <option value="novo">Novo</option>
          <option value="em_atendimento">Em atendimento</option>
          <option value="qualificado">Qualificado</option>
          <option value="convertido">Convertido</option>
          <option value="descartado">Descartado</option>
        </Select>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="lead-origem" className="text-xs text-muted-foreground">
          Origem
        </Label>
        {/* Vocabulário **aberto** no contrato: um `<select>` fechado esconderia
            a origem que o time de marketing criar amanhã. Campo livre com
            sugestões via `datalist`. */}
        <Input
          id="lead-origem"
          list="origens-de-lead"
          value={origem}
          onChange={(evento) => aplicar("source", evento.target.value)}
          placeholder="whatsapp, site, indicacao…"
          className="h-9 w-48"
        />
        <datalist id="origens-de-lead">
          <option value="whatsapp" />
          <option value="site" />
          <option value="indicacao" />
          <option value="ota" />
          <option value="telefone" />
          <option value="instagram" />
        </datalist>
      </div>

      <form
        className="flex flex-col gap-1.5"
        onSubmit={(evento) => {
          evento.preventDefault();
          aplicar("q", rascunho.trim());
        }}
      >
        <Label htmlFor="lead-busca" className="text-xs text-muted-foreground">
          Buscar
        </Label>
        <div className="flex items-center gap-2">
          <Input
            id="lead-busca"
            value={rascunho}
            onChange={(evento) => setRascunho(evento.target.value)}
            placeholder="Nome, e-mail ou telefone"
            className="h-9 w-56"
          />
          <Button type="submit" size="sm" variant="outline" aria-label="Buscar">
            <Search aria-hidden="true" />
          </Button>
        </div>
      </form>

      {temFiltro ? (
        <Button size="sm" variant="ghost" onClick={() => router.replace("/app/leads")}>
          <X aria-hidden="true" />
          Limpar filtros
        </Button>
      ) : null}
    </div>
  );
}
