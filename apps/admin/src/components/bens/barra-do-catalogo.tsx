"use client";

import * as React from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Search, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select } from "@/components/ui/select";
import { ORDENS_DO_CATALOGO, comParametro } from "@/lib/bens/filtros";
import { ROTULO_DA_CATEGORIA } from "@/lib/bens/rotulos";
import { CATEGORIAS_DE_BEM, type Ambiente, type UnidadeDoInventario } from "@/lib/bens/tipos";

/**
 * Filtros do catálogo — na query string. A ordem é a toolbar canônica do
 * painel: busca → filtros → limpar.
 *
 * "Só sem foto" fica à vista, e não escondido num menu: é a fila de trabalho do
 * cadastro (`has_photo=false`), o recorte que alguém abre de propósito todo dia
 * até a fila zerar.
 */
export function BarraDoCatalogo({
  unidades,
  ambientes,
}: {
  unidades: UnidadeDoInventario[];
  /** Ambientes da unidade escolhida — vazio quando não há unidade no filtro. */
  ambientes: Ambiente[];
}) {
  const router = useRouter();
  const caminho = usePathname();
  const parametros = useSearchParams();

  const q = parametros.get("q") ?? "";
  const [rascunho, setRascunho] = React.useState(q);
  const [qAnterior, setQAnterior] = React.useState(q);
  if (q !== qAnterior) {
    setQAnterior(q);
    setRascunho(q);
  }

  function aplicar(chave: string, valor: string) {
    let consulta = comParametro(parametros.toString(), chave, valor);
    // Trocar de unidade invalida o ambiente escolhido: ele é da unidade anterior.
    if (chave === "unidade") consulta = comParametro(consulta.slice(1), "ambiente", "");
    router.replace(`${caminho}${consulta === "?" ? "" : consulta}`);
  }

  const temFiltro = ["q", "categoria", "situacao", "foto", "unidade", "ambiente", "ordem"].some((c) => parametros.get(c));
  const unidade = parametros.get("unidade") ?? "";

  return (
    <div className="flex flex-wrap items-end gap-3">
      <form
        className="flex flex-col gap-1.5"
        onSubmit={(e) => {
          e.preventDefault();
          aplicar("q", rascunho.trim());
        }}
      >
        <Label htmlFor="bens-busca" className="text-xs text-muted-foreground">
          Buscar
        </Label>
        <div className="flex items-center gap-2">
          <Input
            id="bens-busca"
            value={rascunho}
            onChange={(e) => setRascunho(e.target.value)}
            placeholder="Nome ou descrição"
            className="h-9 w-56"
          />
          <Button type="submit" size="sm" variant="outline" aria-label="Buscar">
            <Search aria-hidden="true" />
          </Button>
        </div>
      </form>

      <Filtro id="bens-categoria" rotulo="Categoria" valor={parametros.get("categoria") ?? ""} aoMudar={(v) => aplicar("categoria", v)}>
        <option value="">Todas</option>
        {CATEGORIAS_DE_BEM.map((c) => (
          <option key={c} value={c}>
            {ROTULO_DA_CATEGORIA[c]}
          </option>
        ))}
      </Filtro>

      <Filtro id="bens-situacao" rotulo="Situação" valor={parametros.get("situacao") ?? ""} aoMudar={(v) => aplicar("situacao", v)}>
        <option value="">Em uso</option>
        <option value="inativos">Fora de uso</option>
        <option value="todos">Todos</option>
      </Filtro>

      <Filtro id="bens-foto" rotulo="Foto" valor={parametros.get("foto") ?? ""} aoMudar={(v) => aplicar("foto", v)}>
        <option value="">Com e sem foto</option>
        <option value="sem">Só sem foto</option>
        <option value="com">Só com foto</option>
      </Filtro>

      <Filtro id="bens-unidade" rotulo="Unidade" valor={unidade} aoMudar={(v) => aplicar("unidade", v)}>
        <option value="">Todas</option>
        {unidades.map((u) => (
          <option key={u.id} value={u.id}>
            {u.code} — {u.name}
          </option>
        ))}
      </Filtro>

      {unidade ? (
        <Filtro id="bens-ambiente" rotulo="Ambiente" valor={parametros.get("ambiente") ?? ""} aoMudar={(v) => aplicar("ambiente", v)}>
          <option value="">Todos da unidade</option>
          {ambientes.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name}
            </option>
          ))}
        </Filtro>
      ) : null}

      <Filtro id="bens-ordem" rotulo="Ordem" valor={parametros.get("ordem") ?? ""} aoMudar={(v) => aplicar("ordem", v)}>
        {ORDENS_DO_CATALOGO.map((o) => (
          <option key={o.valor} value={o.valor === "name" ? "" : o.valor}>
            {o.rotulo}
          </option>
        ))}
      </Filtro>

      {temFiltro ? (
        <Button size="sm" variant="ghost" onClick={() => router.replace(caminho)}>
          <X aria-hidden="true" />
          Limpar filtros
        </Button>
      ) : null}
    </div>
  );
}

function Filtro({
  id,
  rotulo,
  valor,
  aoMudar,
  children,
}: {
  id: string;
  rotulo: string;
  valor: string;
  aoMudar: (valor: string) => void;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id} className="text-xs text-muted-foreground">
        {rotulo}
      </Label>
      <Select id={id} value={valor} onChange={(e) => aoMudar(e.target.value)} className="h-9 w-44">
        {children}
      </Select>
    </div>
  );
}
