"use client";

import * as React from "react";
import { Loader2, Search, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { Resultado } from "@/lib/acoes/resultado";
import { cn } from "@/lib/utils";

/**
 * Escolher UM registro por busca — a reserva ou a pessoa do evento.
 *
 * Não é um combobox: é uma caixa, um botão "Buscar" e resultados em botões
 * largos (toque, não hover), como a busca de bem da ordem de manutenção. No
 * celular, dentro da folha do `ModalShell`, um popover de sugestões disputaria
 * foco e rolagem com o teclado virtual.
 *
 * O valor escolhido vive no formulário de quem usa (`valor`/`aoEscolher`); o
 * rótulo do escolhido também, para a edição abrir mostrando o que já estava.
 */
export function BuscaComEscolha<T extends { id: string }>({
  id,
  rotulo,
  dica,
  placeholder,
  escolhido,
  descrever,
  buscar,
  aoEscolher,
  desabilitado = false,
  erro,
}: {
  id: string;
  rotulo: string;
  dica?: React.ReactNode;
  placeholder: string;
  /** O rótulo do que está escolhido agora — `null` é "nenhum". */
  escolhido: string | null;
  descrever: (item: T) => string;
  buscar: (termo: string) => Promise<Resultado<T[]>>;
  aoEscolher: (item: T | null) => void;
  desabilitado?: boolean;
  erro?: string;
}) {
  const [termo, setTermo] = React.useState("");
  const [resultado, setResultado] = React.useState<T[] | null>(null);
  const [buscando, setBuscando] = React.useState(false);
  const [falhou, setFalhou] = React.useState(false);
  const [trocando, setTrocando] = React.useState(false);

  async function procurar() {
    if (termo.trim().length < 2) return;
    setBuscando(true);
    setFalhou(false);
    const r = await buscar(termo);
    setBuscando(false);
    if (r.ok) setResultado(r.data);
    else {
      setResultado(null);
      setFalhou(true);
    }
  }

  const idDica = dica ? `${id}-dica` : undefined;
  const idErro = erro ? `${id}-erro` : undefined;
  const mostrarBusca = !desabilitado && (escolhido === null || trocando);

  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={mostrarBusca ? id : undefined}>{rotulo}</Label>
      {escolhido !== null ? (
        <div
          className={cn(
            "flex min-h-11 items-center gap-2 rounded-xl border border-input bg-card px-3.5 py-2 text-sm",
            desabilitado && "opacity-70",
          )}
          aria-describedby={[idDica, idErro].filter(Boolean).join(" ") || undefined}
        >
          <span className="min-w-0 flex-1 truncate">{escolhido}</span>
          {!desabilitado ? (
            <>
              <Button variant="link" size="sm" className="px-0" onClick={() => setTrocando((t) => !t)}>
                {trocando ? "Manter" : "Trocar"}
              </Button>
              <Button
                variant="ghost"
                size="iconSm"
                aria-label={`Tirar: ${escolhido}`}
                onClick={() => {
                  aoEscolher(null);
                  setTrocando(false);
                }}
              >
                <X aria-hidden="true" />
              </Button>
            </>
          ) : null}
        </div>
      ) : null}

      {mostrarBusca ? (
        <div className="flex items-end gap-2">
          <Input
            id={id}
            value={termo}
            onChange={(e) => setTermo(e.target.value)}
            onKeyDown={(e) => {
              // Enter aqui não pode submeter o evento inteiro.
              if (e.key === "Enter") {
                e.preventDefault();
                void procurar();
              }
            }}
            placeholder={placeholder}
            autoComplete="off"
            aria-describedby={[idDica, idErro].filter(Boolean).join(" ") || undefined}
            invalid={Boolean(erro)}
          />
          <Button
            variant="outline"
            className="h-11"
            onClick={() => void procurar()}
            disabled={buscando || termo.trim().length < 2}
            aria-label={`Buscar ${rotulo.toLowerCase()}`}
          >
            {buscando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Search aria-hidden="true" />}
          </Button>
        </div>
      ) : null}

      {mostrarBusca && falhou ? <p className="text-xs text-destructive">A busca não respondeu agora.</p> : null}
      {mostrarBusca && resultado ? (
        resultado.length === 0 ? (
          <p className="text-xs text-muted-foreground">Nada com esse nome.</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {resultado.map((item) => (
              <li key={item.id}>
                <button
                  type="button"
                  onClick={() => {
                    aoEscolher(item);
                    setResultado(null);
                    setTermo("");
                    setTrocando(false);
                  }}
                  className="flex min-h-11 w-full items-center rounded-xl border border-border/60 bg-card px-3.5 text-left text-sm outline-none transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring/35"
                >
                  {descrever(item)}
                </button>
              </li>
            ))}
          </ul>
        )
      ) : null}

      {dica ? (
        <p id={idDica} className="text-xs leading-snug text-muted-foreground">
          {dica}
        </p>
      ) : null}
      {erro ? (
        <p id={idErro} role="alert" className="text-xs font-medium text-destructive">
          {erro}
        </p>
      ) : null}
    </div>
  );
}
