"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { BuscaNaUrl, SelecaoNaUrl } from "@/components/bens/filtro-na-url";
import { Label } from "@/components/ui/label";
import { Select } from "@/components/ui/select";
import { SITUACOES, comSituacao, situacaoDaUrl } from "@/lib/manutencao/filtros";
import { ROTULO_DA_PRIORIDADE } from "@/lib/manutencao/rotulos";
import { PRIORIDADES_DA_ORDEM } from "@/lib/manutencao/tipos";

/**
 * A barra de filtros da lista — tudo na query string, com os nomes da API
 * (`open`, `status`, `unit_id`, `priority`, `q`). Trocar qualquer filtro volta
 * à primeira página. Sem seletor de ordem: a ordem é a da API.
 */
export function FiltrosDaLista({ unidades }: { unidades: readonly { id: string; code: string; name: string }[] }) {
  return (
    <div className="flex flex-wrap items-end gap-3">
      <BuscaNaUrl id="manutencao-q" rotulo="Buscar" placeholder="Ar-condicionado, chuveiro…" />
      <SituacaoNaUrl />
      <SelecaoNaUrl
        id="manutencao-unidade"
        chave="unit_id"
        rotulo="Unidade"
        vazio="Todas"
        opcoes={unidades.map((u) => ({ valor: u.id, rotulo: `${u.code} — ${u.name}` }))}
        className="w-56"
      />
      <SelecaoNaUrl
        id="manutencao-prioridade"
        chave="priority"
        rotulo="Prioridade"
        vazio="Todas"
        opcoes={PRIORIDADES_DA_ORDEM.map((p) => ({ valor: p, rotulo: ROTULO_DA_PRIORIDADE[p] }))}
        className="w-36"
      />
    </div>
  );
}

/** Um select só para "o que ainda está para fazer?", escrevendo em `open` ou
 *  em `status` — os dois são parâmetros da API, e a URL fica compartilhável. */
function SituacaoNaUrl() {
  const router = useRouter();
  const caminho = usePathname();
  const parametros = useSearchParams();
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor="manutencao-situacao" className="text-xs text-muted-foreground">
        Situação
      </Label>
      <Select
        id="manutencao-situacao"
        value={situacaoDaUrl(parametros)}
        onChange={(e) => router.replace(`${caminho}${comSituacao(parametros.toString(), e.target.value)}`)}
        className="h-9 w-52"
      >
        {SITUACOES.map((s) => (
          <option key={s.valor} value={s.valor}>
            {s.rotulo}
          </option>
        ))}
      </Select>
    </div>
  );
}
