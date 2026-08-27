"use client";

import * as React from "react";
import { Check, Loader2, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { MinimoDeNoites, TipoDeData } from "@/lib/api/comercial";
import { mensagemDoErro } from "@/lib/acoes/resultado";
import { TIPOS_DE_DATA, classeDoTipo } from "@/lib/comercial/tipos-de-data";
import { cn } from "@/lib/utils";

import { removerMinimoDeNoites, salvarMinimoDeNoites } from "./acoes";

/**
 * Estadia mínima por tipo de data.
 *
 * Salva **uma linha por vez**, e é deliberado: não existe endpoint em lote para
 * `min-nights`, e fingir um botão "salvar tudo" que na verdade dispara seis
 * requisições sequenciais criaria o estado meio-salvo que o `bulk` das tarifas
 * existe justamente para evitar. Uma linha, uma requisição, um resultado.
 *
 * Vale o **maior** mínimo entre as noites da estadia — quem aplica é o motor,
 * não esta tela. Aqui só se cadastra o número.
 */
export function EditorDeMinimos({
  tabelaId,
  minimos,
  podeEditar,
  podeExcluir,
}: {
  tabelaId: string;
  minimos: MinimoDeNoites[];
  podeEditar: boolean;
  podeExcluir: boolean;
}) {
  const porTipo = React.useMemo(() => {
    const mapa = new Map<TipoDeData, MinimoDeNoites>();
    for (const regra of minimos) mapa.set(regra.date_type, regra);
    return mapa;
  }, [minimos]);

  const iniciais = React.useMemo(() => {
    const valores: Partial<Record<TipoDeData, string>> = {};
    for (const tipo of TIPOS_DE_DATA) valores[tipo.code] = String(porTipo.get(tipo.code)?.nights ?? "");
    return valores;
  }, [porTipo]);

  // O rascunho começa no que o servidor mandou e só muda por ato do usuário —
  // digitar, salvar ou remover. Não há efeito repondo `valores` a cada chegada
  // de props: quando a tabela escolhida muda, quem repõe tudo é a remontagem
  // (`key={escolhida.id}` na página), e a revalidação da *mesma* tabela não tem
  // por que apagar o que a pessoa está digitando na linha ao lado.
  const [valores, setValores] = React.useState(iniciais);
  const [ocupado, setOcupado] = React.useState<TipoDeData | null>(null);
  const [salvo, setSalvo] = React.useState<TipoDeData | null>(null);
  const [erro, setErro] = React.useState<string | null>(null);

  async function salvar(tipo: TipoDeData) {
    const texto = (valores[tipo] ?? "").trim();
    setErro(null);
    setSalvo(null);
    setOcupado(tipo);
    try {
      const existente = porTipo.get(tipo);
      const resultado = await salvarMinimoDeNoites(tabelaId, existente?.id ?? null, {
        date_type: tipo,
        nights: texto,
      });
      if (!resultado.ok) {
        setErro(mensagemDoErro(resultado.code));
        return;
      }
      setSalvo(tipo);
    } finally {
      setOcupado(null);
    }
  }

  async function remover(tipo: TipoDeData) {
    const existente = porTipo.get(tipo);
    if (!existente) return;
    setErro(null);
    setOcupado(tipo);
    try {
      const resultado = await removerMinimoDeNoites(existente.id);
      if (!resultado.ok) {
        setErro(mensagemDoErro(resultado.code));
        return;
      }
      // Sem regra, o tipo volta a valer 1 noite — e o campo tem que esvaziar
      // junto. Limpar aqui, no evento que removeu, é o que substitui o efeito
      // que repunha a grade inteira a cada revalidação: sem isto o número
      // apagado continuaria no campo, agora como "alteração não salva".
      setValores((atual) => ({ ...atual, [tipo]: "" }));
      setSalvo(null);
    } finally {
      setOcupado(null);
    }
  }

  return (
    <div className="flex flex-col gap-2">
      {TIPOS_DE_DATA.map((tipo) => {
        const existente = porTipo.get(tipo.code);
        const texto = valores[tipo.code] ?? "";
        const mudou = texto.trim() !== String(existente?.nights ?? "");
        const valido = /^\d+$/.test(texto.trim()) && Number(texto.trim()) >= 1;

        return (
          <div key={tipo.code} className="flex flex-wrap items-center gap-2 rounded-lg bg-card/70 px-3 py-2">
            <span className={cn("w-32 shrink-0 rounded-full px-2 py-0.5 text-center text-[0.7rem]", classeDoTipo(tipo.code))}>
              {tipo.label}
            </span>

            <label htmlFor={`minimo-${tipo.code}`} className="sr-only">
              Mínimo de noites em {tipo.label}
            </label>
            <Input
              id={`minimo-${tipo.code}`}
              value={texto}
              onChange={(evento) => {
                setValores((atual) => ({ ...atual, [tipo.code]: evento.target.value }));
                setSalvo(null);
              }}
              disabled={!podeEditar || ocupado === tipo.code}
              inputMode="numeric"
              placeholder="1"
              invalid={texto.trim() !== "" && !valido}
              className="h-9 w-20 text-right font-mono tabular-nums"
            />
            <span className="text-xs text-muted-foreground">
              {existente ? "noite(s)" : "sem regra — vale 1 noite"}
            </span>

            <div className="ml-auto flex items-center gap-1">
              {salvo === tipo.code && !mudou ? (
                <Check className="size-4 text-alcada-livre" aria-label="Salvo" />
              ) : null}
              {podeEditar && mudou ? (
                <Button size="sm" variant="secondary" onClick={() => salvar(tipo.code)} disabled={!valido || ocupado === tipo.code}>
                  {ocupado === tipo.code ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
                  Salvar
                </Button>
              ) : null}
              {podeExcluir && existente ? (
                <Button
                  size="iconSm"
                  variant="ghost"
                  onClick={() => remover(tipo.code)}
                  disabled={ocupado === tipo.code}
                  aria-label={`Remover a regra de ${tipo.label}`}
                >
                  <Trash2 aria-hidden="true" />
                </Button>
              ) : null}
            </div>
          </div>
        );
      })}

      {erro ? (
        <p role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erro}
        </p>
      ) : null}
    </div>
  );
}
