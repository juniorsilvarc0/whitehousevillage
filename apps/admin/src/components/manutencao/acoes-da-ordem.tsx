"use client";

import { CheckCircle2, Loader2, Pencil, Play, Receipt, XCircle } from "lucide-react";

import { Button } from "@/components/ui/button";
import { ROTULO_DA_ACAO } from "@/lib/manutencao/rotulos";
import type { BotoesDaOrdem } from "@/lib/manutencao/permissoes";
import type { OrdemDeManutencao } from "@/lib/manutencao/tipos";

/**
 * A barra de gestos da ordem — **desenhada a partir de `botoesDaOrdem()`**, que
 * lê `allowed_actions` e `editable` da resposta. Nenhum botão aqui olha o
 * `status`: a máquina de estados é da API.
 *
 * No celular os botões ocupam a largura e têm 44px — quem toca está de pé,
 * com a outra mão segurando a ferramenta.
 */
export function AcoesDaOrdem({
  ordem,
  botoes,
  iniciando,
  aoIniciar,
  aoConcluir,
  aoCancelar,
  aoEditar,
  aoLancarCusto,
}: {
  ordem: Pick<OrdemDeManutencao, "cost_cents">;
  botoes: BotoesDaOrdem;
  iniciando: boolean;
  aoIniciar: () => void;
  aoConcluir: () => void;
  aoCancelar: () => void;
  aoEditar: () => void;
  aoLancarCusto: () => void;
}) {
  const algum = botoes.iniciar || botoes.concluir || botoes.cancelar || botoes.editarCadastro || botoes.lancarCusto;
  if (!algum) return null;

  const largura = "max-sm:h-11 max-sm:w-full";
  return (
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Ações da ordem">
      {botoes.iniciar ? (
        <Button onClick={aoIniciar} disabled={iniciando} className={largura}>
          {iniciando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Play aria-hidden="true" />}
          {ROTULO_DA_ACAO.start}
        </Button>
      ) : null}
      {botoes.concluir ? (
        <Button onClick={aoConcluir} variant={botoes.iniciar ? "outline" : "default"} className={largura}>
          <CheckCircle2 aria-hidden="true" />
          {ROTULO_DA_ACAO.complete}
        </Button>
      ) : null}
      {botoes.editarCadastro ? (
        <Button onClick={aoEditar} variant="outline" className={largura}>
          <Pencil aria-hidden="true" />
          Editar
        </Button>
      ) : null}
      {botoes.lancarCusto ? (
        <Button onClick={aoLancarCusto} variant="outline" className={largura}>
          <Receipt aria-hidden="true" />
          {typeof ordem.cost_cents === "number" ? "Corrigir custo" : "Lançar custo"}
        </Button>
      ) : null}
      {botoes.cancelar ? (
        <Button onClick={aoCancelar} variant="ghost" className={`${largura} text-destructive hover:bg-destructive/10`}>
          <XCircle aria-hidden="true" />
          {ROTULO_DA_ACAO.cancel}
        </Button>
      ) : null}
    </div>
  );
}
