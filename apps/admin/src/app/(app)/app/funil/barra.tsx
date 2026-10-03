"use client";

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Search, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select } from "@/components/ui/select";
import type { Funil } from "@/lib/crm/tipos";

/**
 * A barra do funil — busca, seletor de funil, recorte de datas, escopo.
 *
 * **Tudo na query string**, nada em estado interno. "Me manda o funil de
 * eventos filtrado por dezembro" tem de ser um link que reabre exatamente a
 * mesma tela; estado interno perderia isso no primeiro F5 e no primeiro
 * copiar-e-colar — e o quadro do funil é, por definição, o que se manda para
 * outra pessoa olhar.
 */
export function BarraDoFunil({
  funis,
  funilAtual,
  usuarioId,
}: {
  funis: Funil[];
  funilAtual: string | null;
  /** Para o atalho "só os meus": vira `owner_id` na URL. Quem tem escopo `own`
   *  já é restrito no SQL — o atalho serve a quem enxerga o funil inteiro. */
  usuarioId: string;
}) {
  const router = useRouter();
  const parametros = useSearchParams();

  const busca = parametros.get("q") ?? "";
  const de = parametros.get("from") ?? "";
  const ate = parametros.get("to") ?? "";
  const dono = parametros.get("owner_id") ?? "";
  const fechados = parametros.get("include_closed") === "true";
  const temFiltro = busca !== "" || de !== "" || ate !== "" || dono !== "" || fechados;

  const [rascunho, setRascunho] = React.useState(busca);
  // A busca vem da URL; se a URL mudar por outro caminho (voltar do navegador,
  // link colado), o campo tem de acompanhar. Ajuste no render, não em efeito.
  const [buscaAnterior, setBuscaAnterior] = React.useState(busca);
  if (busca !== buscaAnterior) {
    setBuscaAnterior(busca);
    setRascunho(busca);
  }

  function aplicar(mudancas: Record<string, string>) {
    const proximos = new URLSearchParams(parametros.toString());
    for (const [chave, valor] of Object.entries(mudancas)) {
      if (valor === "") proximos.delete(chave);
      else proximos.set(chave, valor);
    }
    const consulta = proximos.toString();
    router.replace(consulta ? `?${consulta}` : "/app/funil");
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="funil-pipeline" className="text-xs text-muted-foreground">
            Funil
          </Label>
          <Select
            id="funil-pipeline"
            value={funilAtual ?? ""}
            onChange={(evento) => aplicar({ pipeline_id: evento.target.value })}
            className="h-9 w-56"
          >
            {funis.length === 0 ? <option value="">Nenhum funil cadastrado</option> : null}
            {funis.map((funil) => (
              <option key={funil.id} value={funil.id}>
                {funil.name}
                {funil.is_default ? " (padrão)" : ""}
              </option>
            ))}
          </Select>
        </div>

        <form
          className="flex flex-col gap-1.5"
          onSubmit={(evento) => {
            evento.preventDefault();
            aplicar({ q: rascunho.trim() });
          }}
        >
          <Label htmlFor="funil-busca" className="text-xs text-muted-foreground">
            Buscar
          </Label>
          <div className="flex items-center gap-2">
            <Input
              id="funil-busca"
              value={rascunho}
              onChange={(evento) => setRascunho(evento.target.value)}
              placeholder="Nome do contato"
              className="h-9 w-56"
            />
            <Button type="submit" size="sm" variant="outline" aria-label="Buscar">
              <Search aria-hidden="true" />
            </Button>
          </div>
        </form>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="funil-de" className="text-xs text-muted-foreground">
            Entrada a partir de
          </Label>
          <Input
            id="funil-de"
            type="date"
            value={de}
            onChange={(evento) => aplicar({ from: evento.target.value })}
            className="h-9 w-40 tabular-nums"
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="funil-ate" className="text-xs text-muted-foreground">
            Até
          </Label>
          <Input
            id="funil-ate"
            type="date"
            value={ate}
            onChange={(evento) => aplicar({ to: evento.target.value })}
            className="h-9 w-40 tabular-nums"
          />
        </div>

        {temFiltro ? (
          <Button size="sm" variant="ghost" onClick={() => router.replace("/app/funil")}>
            <X aria-hidden="true" />
            Limpar filtros
          </Button>
        ) : null}
      </div>

      <div className="flex flex-wrap items-center gap-5">
        <CheckboxCampo
          id="funil-meus"
          label="Só os meus"
          checked={dono === usuarioId}
          onChange={(evento) => aplicar({ owner_id: evento.target.checked ? usuarioId : "" })}
        />
        <CheckboxCampo
          id="funil-fechados"
          label="Trazer tudo que já fechou"
          hint="Normalmente as colunas Ganho e Perdido mostram só os últimos 30 dias, para o quadro não ficar pesado."
          checked={fechados}
          onChange={(evento) => aplicar({ include_closed: evento.target.checked ? "true" : "" })}
        />
      </div>
    </div>
  );
}
