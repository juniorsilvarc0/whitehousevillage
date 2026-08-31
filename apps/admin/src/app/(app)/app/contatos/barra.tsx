"use client";

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Search, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select } from "@/components/ui/select";
import { EXPLICACAO_DO_MODO, modoDaBusca } from "@/lib/contatos/busca";

/**
 * Filtros do cadastro — **na query string**, para o recorte ser um link que
 * reabre a mesma tela. "Me manda quem aceitou receber oferta em Fortaleza" é
 * uma frase que precisa virar URL.
 *
 * A caixa de busca é uma só e o **formato do que foi digitado escolhe o
 * filtro** (`lib/contatos/busca.ts`): `+55…` completo vira busca exata por
 * telefone, onze ou catorze dígitos viram busca por documento, o resto vira
 * busca por nome. A linha embaixo do campo diz qual dos três está valendo —
 * sem ela, "não achei" por busca exata e "não achei" por busca aproximada
 * seriam a mesma tela, e são conclusões diferentes.
 */
export function BarraDeContatos() {
  const router = useRouter();
  const parametros = useSearchParams();

  const busca = parametros.get("busca") ?? "";
  const optIn = parametros.get("marketing_opt_in") ?? "";
  const incluirAnonimizados = parametros.get("include_anonymized") === "true";
  const temFiltro = busca !== "" || optIn !== "" || incluirAnonimizados;

  const [rascunho, setRascunho] = React.useState(busca);
  const [buscaAnterior, setBuscaAnterior] = React.useState(busca);
  if (busca !== buscaAnterior) {
    setBuscaAnterior(busca);
    setRascunho(busca);
  }

  const modo = modoDaBusca(rascunho);

  function aplicar(chave: string, valor: string) {
    const proximos = new URLSearchParams(parametros.toString());
    if (valor === "") proximos.delete(chave);
    else proximos.set(chave, valor);
    // Trocar o filtro volta para a primeira página: manter `page=4` num recorte
    // novo mostra uma lista vazia que parece "não achei nada".
    proximos.delete("page");
    const consulta = proximos.toString();
    router.replace(consulta ? `?${consulta}` : "/app/contatos");
  }

  return (
    <div className="flex flex-wrap items-end gap-3">
      <form
        className="flex flex-col gap-1.5"
        onSubmit={(evento) => {
          evento.preventDefault();
          aplicar("busca", rascunho.trim());
        }}
      >
        <Label htmlFor="contato-busca" className="text-xs text-muted-foreground">
          Buscar
        </Label>
        <div className="flex items-center gap-2">
          <Input
            id="contato-busca"
            value={rascunho}
            onChange={(evento) => setRascunho(evento.target.value)}
            placeholder="Nome, +5585999990000 ou CPF"
            className="h-9 w-72"
            aria-describedby="contato-busca-modo"
          />
          <Button type="submit" size="sm" variant="outline" aria-label="Buscar">
            <Search aria-hidden="true" />
          </Button>
        </div>
        <p id="contato-busca-modo" className="text-xs text-muted-foreground">
          {EXPLICACAO_DO_MODO[modo]}
        </p>
      </form>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="contato-optin" className="text-xs text-muted-foreground">
          Marketing
        </Label>
        <Select
          id="contato-optin"
          value={optIn}
          onChange={(evento) => aplicar("marketing_opt_in", evento.target.value)}
          className="h-9 w-52"
        >
          <option value="">Todos</option>
          <option value="true">Aceitaram receber</option>
          <option value="false">Não aceitaram</option>
        </Select>
      </div>

      <CheckboxCampo
        id="contato-anonimizados"
        label="Mostrar anonimizados"
        hint="Ficha esvaziada a pedido do titular. Fica fora por padrão para não ser oferecida num negócio novo."
        checked={incluirAnonimizados}
        onChange={(evento) => aplicar("include_anonymized", evento.target.checked ? "true" : "")}
        className="pb-1"
      />

      {temFiltro ? (
        <Button size="sm" variant="ghost" onClick={() => router.replace("/app/contatos")}>
          <X aria-hidden="true" />
          Limpar filtros
        </Button>
      ) : null}
    </div>
  );
}
