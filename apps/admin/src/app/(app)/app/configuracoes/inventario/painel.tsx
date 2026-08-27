"use client";

import * as React from "react";
import { Blocks, DoorClosed, Pencil, Plus, Power, Users } from "lucide-react";

import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { EstadoVazio } from "@/components/layout/estados";
import { ModalDeConfirmacao } from "@/components/layout/modal-de-confirmacao";
import { Nota, Secao } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { Produto, Unidade, UnidadeDaComposicao } from "@/lib/api/comercial";
import { formatarBRL } from "@/lib/dinheiro";
import { cn } from "@/lib/utils";

import { desativarProduto, desativarUnidade } from "./acoes";
import { ModalDeComposicao, ModalDeProduto, ModalDeUnidade, type AlvoDaComposicao } from "./formularios";

export type Composicoes = Record<string, UnidadeDaComposicao[]>;

type Permissoes = { criar: boolean; editar: boolean; excluir: boolean };

/**
 * Inventário: produtos em cima, unidades embaixo, composição no meio das duas.
 *
 * A separação visual repete a do modelo, porque é ela que a reunião com os
 * proprietários precisa entender: **produto é o que se vende, unidade é o que
 * se ocupa**, e a composição é a ponte muitos-para-muitos entre os dois. Sem
 * isso ninguém explica por que vender um apartamento fecha a White House
 * Completa.
 */
export function PainelDeInventario({
  produtos,
  unidades,
  composicoes,
  permissoes,
}: {
  produtos: Produto[];
  unidades: Unidade[];
  composicoes: Composicoes;
  permissoes: Permissoes;
}) {
  const cadastroDeProduto = useControleDeModal<Produto | null>();
  const composicao = useControleDeModal<AlvoDaComposicao>();
  const cadastroDeUnidade = useControleDeModal<Unidade | null>();

  const [produtoADesativar, setProdutoADesativar] = React.useState<Produto | null>(null);
  const [unidadeADesativar, setUnidadeADesativar] = React.useState<Unidade | null>(null);

  // Índice inverso: em que produtos cada unidade entra. É o que responde, na
  // linha da unidade, "desativar esta afeta o quê?".
  const produtosPorUnidade = React.useMemo(() => {
    const mapa = new Map<string, string[]>();
    for (const produto of produtos) {
      for (const membro of composicoes[produto.id] ?? []) {
        mapa.set(membro.unit_id, [...(mapa.get(membro.unit_id) ?? []), produto.code]);
      }
    }
    return mapa;
  }, [produtos, composicoes]);


  return (
    <>
      <Secao
        titulo="Produtos — o que se vende"
        descricao="Cada produto declara a capacidade, a taxa de limpeza e quantas unidades uma venda ocupa."
        acoes={
          permissoes.criar ? (
            <Button size="sm" onClick={() => cadastroDeProduto.abrir(null)}>
              <Plus aria-hidden="true" />
              Novo produto
            </Button>
          ) : null
        }
      >
        {produtos.length === 0 ? (
          <EstadoVazio
            titulo="Nenhum produto cadastrado"
            descricao="Sem produto não há o que vender: o orçamento precisa de um para achar tarifa, capacidade e limpeza."
            acao={
              permissoes.criar ? (
                <Button size="sm" onClick={() => cadastroDeProduto.abrir(null)}>
                  <Plus aria-hidden="true" />
                  Criar o primeiro
                </Button>
              ) : null
            }
          />
        ) : (
          <ul className="grid gap-3 lg:grid-cols-2">
            {produtos.map((produto) => (
              <CartaoDeProduto
                key={produto.id}
                produto={produto}
                composicao={composicoes[produto.id] ?? []}
                totalDeUnidades={unidades.length}
                permissoes={permissoes}
                aoEditar={() => cadastroDeProduto.abrir(produto)}
                aoCompor={() => composicao.abrir({ produto, atual: composicoes[produto.id] ?? [] })}
                aoDesativar={() => setProdutoADesativar(produto)}
              />
            ))}
          </ul>
        )}
      </Secao>

      <Secao
        titulo="Unidades físicas — o que se ocupa"
        descricao="A unidade é o que a constraint do banco protege: duas estadias sobrepostas na mesma unidade são impossíveis, não improváveis."
        acoes={
          permissoes.criar ? (
            <Button size="sm" variant="outline" onClick={() => cadastroDeUnidade.abrir(null)}>
              <Plus aria-hidden="true" />
              Nova unidade
            </Button>
          ) : null
        }
      >
        {unidades.length === 0 ? (
          <EstadoVazio
            titulo="Nenhuma unidade cadastrada"
            descricao="Sem unidade nominal não existe constraint capaz de impedir overbooking — e a operação não saberia qual apartamento preparar."
            icone={DoorClosed}
            acao={
              permissoes.criar ? (
                <Button size="sm" onClick={() => cadastroDeUnidade.abrir(null)}>
                  <Plus aria-hidden="true" />
                  Criar a primeira
                </Button>
              ) : null
            }
          />
        ) : (
          <ul className="flex flex-col gap-2">
            {unidades.map((unidade) => (
              <LinhaDeUnidade
                key={unidade.id}
                unidade={unidade}
                produtos={produtosPorUnidade.get(unidade.id) ?? []}
                permissoes={permissoes}
                aoEditar={() => cadastroDeUnidade.abrir(unidade)}
                aoDesativar={() => setUnidadeADesativar(unidade)}
              />
            ))}
          </ul>
        )}
      </Secao>

      <ModalDeProduto controle={cadastroDeProduto.ref} />
      <ModalDeUnidade controle={cadastroDeUnidade.ref} />
      <ModalDeComposicao controle={composicao.ref} unidades={unidades} />

      <ModalDeConfirmacao
        aberto={produtoADesativar !== null}
        aoMudar={(aberto) => !aberto && setProdutoADesativar(null)}
        titulo={`Desativar ${produtoADesativar?.name ?? ""}?`}
        destrutivo
        rotuloConfirmar="Desativar produto"
        descricao={
          <>
            Desativar não apaga: o produto some da disponibilidade e deixa de aceitar venda nova, e as
            reservas antigas continuam legíveis com o preço que congelaram. Se ainda houver reserva viva
            neste produto, o servidor recusa.
          </>
        }
        aoConfirmar={() => desativarProduto(produtoADesativar!.id)}
      />

      <ModalDeConfirmacao
        aberto={unidadeADesativar !== null}
        aoMudar={(aberto) => !aberto && setUnidadeADesativar(null)}
        titulo={`Desativar ${unidadeADesativar?.code ?? ""}?`}
        destrutivo
        rotuloConfirmar="Desativar unidade"
        descricao={
          <>
            A unidade sai da alocação automática e do mapa de ocupação, mas continua no histórico — quem
            dormiu onde não se apaga. Ocupação futura em aberto ou vínculo com algum produto fazem o
            servidor recusar.
          </>
        }
        aoConfirmar={() => desativarUnidade(unidadeADesativar!.id)}
      />
    </>
  );
}

function CartaoDeProduto({
  produto,
  composicao,
  totalDeUnidades,
  permissoes,
  aoEditar,
  aoCompor,
  aoDesativar,
}: {
  produto: Produto;
  composicao: UnidadeDaComposicao[];
  totalDeUnidades: number;
  permissoes: Permissoes;
  aoEditar: () => void;
  aoCompor: () => void;
  aoDesativar: () => void;
}) {
  const consomeTudo = produto.consumes === "all_members";
  const fechaACasa = consomeTudo && composicao.length > 0 && composicao.length === totalDeUnidades;

  return (
    <li
      className={cn(
        "flex flex-col rounded-xl border bg-card/60 p-4",
        consomeTudo ? "border-primary/35" : "border-border/60",
        !produto.active && "opacity-70",
      )}
    >
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-xs text-muted-foreground">{produto.code}</span>
            {!produto.active ? <Badge variant="outline">inativo</Badge> : null}
          </div>
          <h3 className="font-display mt-0.5 text-base leading-tight">{produto.name}</h3>
        </div>
        <Badge variant={consomeTudo ? "brand" : "neutral"}>
          <Blocks aria-hidden="true" />
          {consomeTudo ? "consome todas" : "consome uma"}
        </Badge>
      </div>

      <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
        <div>
          <dt className="text-xs text-muted-foreground">Capacidade</dt>
          <dd className="flex items-center gap-1.5 tabular-nums">
            <Users className="size-3.5 text-muted-foreground" aria-hidden="true" />
            {produto.capacity} hóspedes
          </dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Limpeza por estadia</dt>
          <dd className="font-mono tabular-nums">{formatarBRL(produto.cleaning_fee_cents)}</dd>
        </div>
      </dl>

      <div className="mt-3">
        <p className="text-xs text-muted-foreground">
          Composição{" "}
          <span className="tabular-nums">
            ({composicao.length} unidade{composicao.length === 1 ? "" : "s"})
          </span>
        </p>
        {composicao.length === 0 ? (
          <p className="mt-1 text-xs font-medium text-destructive">
            Sem composição — nenhuma venda é possível até definir as unidades.
          </p>
        ) : (
          <div className="mt-1.5 flex flex-wrap gap-1">
            {composicao.map((membro) => (
              <span
                key={membro.unit_id}
                className={cn(
                  "rounded-md border px-1.5 py-0.5 font-mono text-[0.7rem]",
                  membro.active
                    ? "border-border/70 text-foreground"
                    : "border-dashed border-border/70 text-muted-foreground line-through",
                )}
                title={membro.active ? membro.unit_name : `${membro.unit_name} — unidade inativa, não é alocada`}
              >
                {membro.unit_code}
              </span>
            ))}
          </div>
        )}
      </div>

      {fechaACasa ? (
        <Nota variante="atencao" className="mt-3">
          Vender este produto ocupa as {composicao.length} unidades de uma vez: enquanto ele estiver
          reservado, nenhum outro produto pode ser vendido nessas datas — e basta uma unidade ocupada
          para que a venda dele seja recusada pelo banco.
        </Nota>
      ) : null}

      <div className="mt-4 flex flex-wrap gap-2">
        {permissoes.editar ? (
          <>
            <Button size="sm" variant="outline" onClick={aoEditar}>
              <Pencil aria-hidden="true" />
              Editar
            </Button>
            <Button size="sm" variant="secondary" onClick={aoCompor}>
              <Blocks aria-hidden="true" />
              Composição
            </Button>
          </>
        ) : null}
        {permissoes.excluir && produto.active ? (
          <Button size="sm" variant="ghost" onClick={aoDesativar}>
            <Power aria-hidden="true" />
            Desativar
          </Button>
        ) : null}
      </div>
    </li>
  );
}

function LinhaDeUnidade({
  unidade,
  produtos,
  permissoes,
  aoEditar,
  aoDesativar,
}: {
  unidade: Unidade;
  produtos: string[];
  permissoes: Permissoes;
  aoEditar: () => void;
  aoDesativar: () => void;
}) {
  return (
    <li
      className={cn(
        "flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl border border-border/60 bg-card/60 px-3.5 py-3",
        !unidade.active && "opacity-70",
      )}
    >
      <span className="font-mono text-sm">{unidade.code}</span>
      <span className="min-w-0 flex-1 truncate text-sm">{unidade.name}</span>

      {unidade.floor ? <span className="text-xs text-muted-foreground">{unidade.floor}</span> : null}

      <div className="flex flex-wrap items-center gap-1">
        {produtos.length === 0 ? (
          <Badge variant="outline">fora de todo produto</Badge>
        ) : (
          produtos.map((codigo) => (
            <span key={codigo} className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-[0.7rem] text-muted-foreground">
              {codigo}
            </span>
          ))
        )}
        {!unidade.active ? <Badge variant="outline">inativa</Badge> : null}
      </div>

      <div className="flex gap-1">
        {permissoes.editar ? (
          <Button size="iconSm" variant="ghost" onClick={aoEditar} aria-label={`Editar ${unidade.code}`}>
            <Pencil aria-hidden="true" />
          </Button>
        ) : null}
        {permissoes.excluir && unidade.active ? (
          <Button size="iconSm" variant="ghost" onClick={aoDesativar} aria-label={`Desativar ${unidade.code}`}>
            <Power aria-hidden="true" />
          </Button>
        ) : null}
      </div>
    </li>
  );
}
