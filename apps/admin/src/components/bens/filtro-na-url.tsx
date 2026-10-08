"use client";

import * as React from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Search } from "lucide-react";

import { Button } from "@/components/ui/button";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select } from "@/components/ui/select";
import { comParametro } from "@/lib/bens/filtros";
import { cn } from "@/lib/utils";

/**
 * Peças de filtro que escrevem **na query string** — o recorte é um link.
 * Trocar qualquer filtro volta à primeira página (`comParametro`).
 */
function useAplicar() {
  const router = useRouter();
  const caminho = usePathname();
  const parametros = useSearchParams();
  const aplicar = React.useCallback(
    (chave: string, valor: string, limpar: string[] = []) => {
      let consulta = comParametro(parametros.toString(), chave, valor);
      for (const outra of limpar) consulta = comParametro(consulta.slice(1), outra, "");
      router.replace(`${caminho}${consulta === "?" ? "" : consulta}`);
    },
    [router, caminho, parametros],
  );
  return { aplicar, parametros };
}

export function SelecaoNaUrl({
  id,
  chave,
  rotulo,
  opcoes,
  vazio,
  limpar,
  className,
}: {
  id: string;
  chave: string;
  rotulo: string;
  opcoes: { valor: string; rotulo: string }[];
  /** Rótulo da opção sem filtro. Ausente: não há opção vazia. */
  vazio?: string;
  /** Parâmetros que dependem deste e caem quando ele muda (ambiente da unidade). */
  limpar?: string[];
  className?: string;
}) {
  const { aplicar, parametros } = useAplicar();
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id} className="text-xs text-muted-foreground">
        {rotulo}
      </Label>
      <Select
        id={id}
        value={parametros.get(chave) ?? ""}
        onChange={(e) => aplicar(chave, e.target.value, limpar)}
        className={cn("h-9 w-52", className)}
      >
        {vazio !== undefined ? <option value="">{vazio}</option> : null}
        {opcoes.map((o) => (
          <option key={o.valor} value={o.valor}>
            {o.rotulo}
          </option>
        ))}
      </Select>
    </div>
  );
}

export function BuscaNaUrl({ id, rotulo, placeholder }: { id: string; rotulo: string; placeholder: string }) {
  const { aplicar, parametros } = useAplicar();
  const q = parametros.get("q") ?? "";
  const [rascunho, setRascunho] = React.useState(q);
  const [anterior, setAnterior] = React.useState(q);
  if (q !== anterior) {
    setAnterior(q);
    setRascunho(q);
  }
  return (
    <form
      className="flex flex-col gap-1.5"
      onSubmit={(e) => {
        e.preventDefault();
        aplicar("q", rascunho.trim());
      }}
    >
      <Label htmlFor={id} className="text-xs text-muted-foreground">
        {rotulo}
      </Label>
      <div className="flex items-center gap-2">
        <Input id={id} value={rascunho} onChange={(e) => setRascunho(e.target.value)} placeholder={placeholder} className="h-9 w-52" />
        <Button type="submit" size="sm" variant="outline" aria-label="Buscar">
          <Search aria-hidden="true" />
        </Button>
      </div>
    </form>
  );
}

export function MarcacaoNaUrl({ id, chave, rotulo, hint }: { id: string; chave: string; rotulo: string; hint?: string }) {
  const { aplicar, parametros } = useAplicar();
  return (
    <CheckboxCampo
      id={id}
      label={rotulo}
      hint={hint}
      checked={parametros.get(chave) === "1"}
      onChange={(e) => aplicar(chave, e.target.checked ? "1" : "")}
      className="pb-1"
    />
  );
}
