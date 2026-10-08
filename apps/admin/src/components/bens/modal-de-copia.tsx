"use client";

import * as React from "react";
import Link from "next/link";
import { Copy, Loader2 } from "lucide-react";

import { notificarSucesso } from "@/components/bens/avisos";
import { AvisoGeral } from "@/components/bens/formulario-comum";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Select } from "@/components/ui/select";
import { copiarInventario } from "@/lib/bens/acoes";
import { conferenciaJaAberta, mensagemDeBens } from "@/lib/bens/mensagens";
import { ROTULO_DO_AMBIENTE } from "@/lib/bens/rotulos";
import type { ResultadoDaCopia, RotuloDeUnidade, UnidadeDoInventario } from "@/lib/bens/tipos";

/**
 * Copiar ambientes e bens de outra unidade — seis duplex iguais não se
 * cadastram seis vezes.
 *
 * O gesto tem **dois passos de propósito**: primeiro a simulação
 * (`?dry_run=1`), que não grava nada e mostra o plano inteiro; depois a
 * confirmação. A cópia só acrescenta: o que já existe no destino fica como
 * está, inclusive a quantidade — e a lista "fica como está" é a parte que mais
 * importa ler antes de confirmar.
 */
export function ModalDeCopia({
  aberto,
  aoMudar,
  destino,
  unidades,
}: {
  aberto: boolean;
  aoMudar: (aberto: boolean) => void;
  destino: RotuloDeUnidade;
  unidades: readonly UnidadeDoInventario[];
}) {
  const [origem, setOrigem] = React.useState("");
  const [soAmbientes, setSoAmbientes] = React.useState(false);
  const [plano, setPlano] = React.useState<ResultadoDaCopia | null>(null);
  const [enviando, setEnviando] = React.useState(false);
  const [erro, setErro] = React.useState<string | null>(null);
  const [conferencia, setConferencia] = React.useState<string | null>(null);

  const [abertoAntes, setAbertoAntes] = React.useState(aberto);
  if (aberto !== abertoAntes) {
    setAbertoAntes(aberto);
    if (aberto) {
      setOrigem("");
      setSoAmbientes(false);
      setPlano(null);
      setErro(null);
      setConferencia(null);
    }
  }

  const candidatas = unidades.filter((u) => u.id !== destino.id);

  async function executar(simular: boolean) {
    if (!origem) {
      setErro("Escolha de qual unidade copiar.");
      return;
    }
    setEnviando(true);
    setErro(null);
    setConferencia(null);
    try {
      const r = await copiarInventario(destino.id, origem, soAmbientes, simular);
      if (!r.ok) {
        const aberta = conferenciaJaAberta(r);
        if (aberta) setConferencia(aberta.caminho);
        setErro(
          aberta
            ? `${destino.code} tem uma conferência em andamento. Feche ou cancele a conferência antes de copiar — senão os bens novos ficariam fora da contagem.`
            : mensagemDeBens(r, "copia"),
        );
        return;
      }
      if (simular) {
        setPlano(r.data);
        return;
      }
      aoMudar(false);
      notificarSucesso(
        "Cópia feita",
        `${r.data.rooms_created.length} ambiente(s) e ${r.data.placements_created.length} bem(ns) acrescentados a ${destino.code}.`,
      );
    } finally {
      setEnviando(false);
    }
  }

  const nada = plano && plano.rooms_created.length === 0 && plano.placements_created.length === 0;

  return (
    <ModalShell
      open={aberto}
      onOpenChange={aoMudar}
      title={`Copiar para ${destino.code}`}
      description="Traz os ambientes e os bens de outra unidade. Nada do que já existe aqui é apagado ou mudado."
      footer={
        <>
          <Button variant="ghost" onClick={() => aoMudar(false)} disabled={enviando}>
            Voltar
          </Button>
          {plano && !nada ? (
            <Button onClick={() => executar(false)} disabled={enviando}>
              {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Copy aria-hidden="true" />}
              Confirmar cópia
            </Button>
          ) : (
            <Button onClick={() => executar(true)} disabled={enviando || !origem}>
              {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
              Ver o que vai mudar
            </Button>
          )}
        </>
      }
    >
      <AvisoGeral mensagem={erro} />
      {conferencia ? (
        <p className="-mt-2 mb-4 text-sm">
          <Link href={conferencia} className="text-primary underline-offset-4 hover:underline">
            Abrir a conferência em andamento
          </Link>
        </p>
      ) : null}

      <div className="flex flex-col gap-4">
        <Campo id="copia-origem" label="Copiar de" obrigatorio>
          {(p) => (
            <Select
              {...p}
              value={origem}
              onChange={(e) => {
                setOrigem(e.target.value);
                setPlano(null);
              }}
            >
              <option value="">Escolha a unidade de origem</option>
              {candidatas.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.code} — {u.name} ({u.rooms} {u.rooms === 1 ? "ambiente" : "ambientes"}, {u.items} {u.items === 1 ? "bem" : "bens"})
                </option>
              ))}
            </Select>
          )}
        </Campo>

        <CheckboxCampo
          id="copia-so-ambientes"
          label="Só os ambientes, sem os bens"
          hint="Para quem vai cadastrar os bens à mão e só quer a planta da casa."
          checked={soAmbientes}
          onChange={(e) => {
            setSoAmbientes(e.target.checked);
            setPlano(null);
          }}
        />

        {plano ? <Plano plano={plano} /> : null}
      </div>
    </ModalShell>
  );
}

function Plano({ plano }: { plano: ResultadoDaCopia }) {
  const divergentes = plano.kept.filter((k) => k.source_qty !== k.current_qty);
  if (plano.rooms_created.length === 0 && plano.placements_created.length === 0) {
    return (
      <Nota>
        Nada a copiar: tudo o que {plano.source?.code ?? "a origem"} tem já existe nesta unidade.
        {divergentes.length > 0 ? " Algumas quantidades diferem — veja abaixo e ajuste item a item, se for o caso." : ""}
      </Nota>
    );
  }
  return (
    <div className="flex flex-col gap-3 text-sm">
      <Nota>Simulação — nada foi gravado ainda. Confira e confirme.</Nota>
      {plano.rooms_created.length > 0 ? (
        <Bloco titulo={`Ambientes novos (${plano.rooms_created.length})`}>
          {plano.rooms_created.map((r, i) => (
            <li key={`${r.name}-${i}`}>
              {r.name}
              {r.kind ? <span className="text-muted-foreground"> · {ROTULO_DO_AMBIENTE[r.kind]}</span> : null}
            </li>
          ))}
        </Bloco>
      ) : null}
      {plano.placements_created.length > 0 ? (
        <Bloco titulo={`Bens que entram (${plano.placements_created.length})`}>
          {plano.placements_created.map((p, i) => (
            <li key={`${p.room_name}-${p.item_id}-${i}`} className="flex justify-between gap-3">
              <span className="min-w-0 truncate">
                {p.item_name} <span className="text-muted-foreground">· {p.room_name}</span>
              </span>
              <span className="shrink-0 tabular-nums">{p.expected_qty}</span>
            </li>
          ))}
        </Bloco>
      ) : null}
      {divergentes.length > 0 ? (
        <Bloco titulo={`Já existiam e ficam como estão (${divergentes.length})`}>
          {divergentes.map((k, i) => (
            <li key={`${k.room_name}-${k.item_name}-${i}`} className="flex justify-between gap-3">
              <span className="min-w-0 truncate">
                {k.item_name} <span className="text-muted-foreground">· {k.room_name}</span>
              </span>
              <span className="shrink-0 tabular-nums">
                fica {k.current_qty} <span className="text-muted-foreground">(origem: {k.source_qty})</span>
              </span>
            </li>
          ))}
        </Bloco>
      ) : null}
    </div>
  );
}

function Bloco({ titulo, children }: { titulo: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-border/60 bg-muted/20 p-3">
      <p className="text-xs font-medium text-foreground">{titulo}</p>
      <ul className="mt-2 flex max-h-48 flex-col gap-1 overflow-y-auto overscroll-contain text-sm">{children}</ul>
    </div>
  );
}
