"use client";

import * as React from "react";
import { Loader2, Wrench } from "lucide-react";

import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { formatarData, noitesEntre } from "@/lib/datas";
import { mensagemDoErro, type Resultado } from "@/lib/acoes/resultado";

import type { OrigemDoBloqueio } from "@/app/(app)/app/mapa/esquemas";

/**
 * O que o arrasto no mapa vira: um bloqueio operacional, com confirmação.
 *
 * **O arrasto não grava sozinho.** Tirar data do estoque é a operação mais fácil
 * de fazer por engano numa grade densa — o dedo escorrega uma linha e a
 * Cobertura sai do mercado por duas semanas. O gesto propõe; o modal confirma,
 * e mostra por escrito o que vai acontecer: quantas noites, quais datas, qual
 * unidade.
 *
 * As datas chegam preenchidas pelo arrasto e continuam **editáveis**: é o
 * caminho de teclado para a mesma ação, e é como se corrige um dia a mais sem
 * refazer o gesto.
 */
export type PedidoDeBloqueio = {
  unitId: string;
  unitCode: string;
  unitName: string;
  from: string;
  /** Exclusivo. */
  to: string;
};

export function ModalDeBloqueio({
  pedido,
  aoFechar,
  aoGravar,
}: {
  pedido: PedidoDeBloqueio | null;
  aoFechar: () => void;
  aoGravar: (valores: {
    unit_id: string;
    from: string;
    to: string;
    source: OrigemDoBloqueio;
    note?: string;
  }) => Promise<Resultado<null>>;
}) {
  const [de, setDe] = React.useState("");
  const [ate, setAte] = React.useState("");
  const [origem, setOrigem] = React.useState<OrigemDoBloqueio>("maintenance");
  const [nota, setNota] = React.useState("");
  const [salvando, setSalvando] = React.useState(false);
  const [erros, setErros] = React.useState<Record<string, string>>({});
  const [recusa, setRecusa] = React.useState<string | null>(null);

  // Repor no evento que abre, e não num efeito que observa a prop: um efeito
  // renderizaria uma vez com os valores da abertura anterior antes de corrigir.
  // Aqui a chave é o próprio pedido — trocar de pedido remonta o corpo.
  const chave = pedido ? `${pedido.unitId}:${pedido.from}:${pedido.to}` : "";
  const [chaveAnterior, setChaveAnterior] = React.useState(chave);
  if (chave !== chaveAnterior) {
    setChaveAnterior(chave);
    setDe(pedido?.from ?? "");
    setAte(pedido?.to ?? "");
    setOrigem("maintenance");
    setNota("");
    setErros({});
    setRecusa(null);
  }

  const noites = de && ate ? noitesEntre(de, ate) : 0;

  async function gravar() {
    if (!pedido) return;
    setSalvando(true);
    setErros({});
    setRecusa(null);

    const resultado = await aoGravar({
      unit_id: pedido.unitId,
      from: de,
      to: ate,
      source: origem,
      note: nota,
    });

    setSalvando(false);
    if (resultado.ok) {
      aoFechar();
      return;
    }

    const detalhes = resultado.details as Record<string, unknown>;
    const porCampo: Record<string, string> = {};
    for (const campo of ["from", "to", "source", "unit_ids", "note"]) {
      const valor = detalhes[campo];
      if (typeof valor === "string") porCampo[campo === "unit_ids" ? "unit_id" : campo] = valor;
    }
    setErros(porCampo);
    // A tela reage ao CÓDIGO, nunca ao texto que a API escreveu — a API escreve
    // para quem depura, e a tela escreve para quem vende.
    setRecusa(mensagemDoErro(resultado.code));
  }

  return (
    <ModalShell
      open={pedido !== null}
      onOpenChange={(aberto) => {
        if (!aberto) aoFechar();
      }}
      title="Bloquear a unidade"
      description={pedido ? `${pedido.unitCode} · ${pedido.unitName}` : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={aoFechar} disabled={salvando}>
            Cancelar
          </Button>
          <Button onClick={gravar} disabled={salvando || noites < 1}>
            {salvando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Wrench aria-hidden="true" />}
            Bloquear {noites > 0 ? `${noites} noite${noites > 1 ? "s" : ""}` : ""}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Nota>
          O bloqueio vive na <strong>mesma tabela</strong> das reservas, e é por isso que uma manutenção
          impede uma venda pela mesma constraint que impede duas vendas. A saída é{" "}
          <strong>exclusiva</strong>:{" "}
          {de && ate ? (
            <>
              de {formatarData(de)} a {formatarData(ate)} ocupa {noites} noite{noites > 1 ? "s" : ""} e deixa{" "}
              {formatarData(ate)} livre para check-in.
            </>
          ) : (
            "bloquear 10 → 12 ocupa 10 e 11 e deixa 12 livre para check-in."
          )}
        </Nota>

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="bloqueio-de" label="Entrada" erro={erros.from} obrigatorio>
            {(props) => (
              <Input {...props} type="date" value={de} onChange={(e) => setDe(e.target.value)} />
            )}
          </Campo>
          <Campo id="bloqueio-ate" label="Saída (exclusiva)" erro={erros.to} obrigatorio>
            {(props) => (
              <Input {...props} type="date" value={ate} onChange={(e) => setAte(e.target.value)} />
            )}
          </Campo>
        </div>

        <Campo
          id="bloqueio-origem"
          label="Motivo"
          erro={erros.source}
          hint="Reserva de canal e reserva própria não se criam por aqui: uma vem do importador, a outra da venda."
          obrigatorio
        >
          {(props) => (
            <Select
              {...props}
              value={origem}
              onChange={(e) => setOrigem(e.target.value as OrigemDoBloqueio)}
            >
              <option value="maintenance">Manutenção</option>
              <option value="owner_hold">Uso do proprietário</option>
            </Select>
          )}
        </Campo>

        <Campo id="bloqueio-nota" label="Observação" erro={erros.note}>
          {(props) => (
            <Textarea
              {...props}
              rows={3}
              value={nota}
              onChange={(e) => setNota(e.target.value)}
              placeholder="Troca do ar-condicionado da suíte"
            />
          )}
        </Campo>

        {recusa ? (
          <p role="alert" className="rounded-lg border border-destructive/30 bg-destructive/8 px-3 py-2 text-sm text-destructive">
            {recusa}
          </p>
        ) : null}
      </div>
    </ModalShell>
  );
}
