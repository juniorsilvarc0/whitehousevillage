"use client";

import * as React from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2 } from "lucide-react";

import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import type { Produto, Unidade, UnidadeDaComposicao } from "@/lib/api/comercial";
import { aplicarFalha } from "@/lib/acoes/formulario";
import { mensagemDoErro } from "@/lib/acoes/resultado";
import { reaisDeCentavos } from "@/lib/dinheiro";
import { cn } from "@/lib/utils";

import { salvarComposicao, salvarProduto, salvarUnidade } from "./acoes";
import { ProdutoFormulario, UnidadeFormulario } from "./esquemas";

/** Rodapé igual nos três modais: cancelar à esquerda, salvar com spinner. */
function Rodape({
  formulario,
  enviando,
  aoCancelar,
  rotulo = "Salvar",
  desabilitado = false,
}: {
  /** Id do `<form>` deste modal. Único por modal de propósito: os três ficam
   *  montados na mesma árvore, e um id repetido faria o botão de um submeter o
   *  formulário do outro no primeiro dia em que dois abrissem juntos. */
  formulario: string;
  enviando: boolean;
  aoCancelar: () => void;
  rotulo?: string;
  desabilitado?: boolean;
}) {
  return (
    <>
      <Button variant="ghost" onClick={aoCancelar} disabled={enviando}>
        Cancelar
      </Button>
      <Button type="submit" form={formulario} disabled={enviando || desabilitado}>
        {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
        {rotulo}
      </Button>
    </>
  );
}

function AvisoGeral({ mensagem }: { mensagem: string | null }) {
  if (!mensagem) return null;
  return (
    <p role="alert" className="mb-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
      {mensagem}
    </p>
  );
}

// ─────────────────────────────── Produto ───────────────────────────────────

function valoresDoProduto(produto: Produto | null): ProdutoFormulario {
  return {
    code: produto?.code ?? "",
    name: produto?.name ?? "",
    capacity: String(produto?.capacity ?? 2),
    consumes: produto?.consumes ?? "one_member",
    cleaning_fee_cents: reaisDeCentavos(produto?.cleaning_fee_cents ?? 0),
    description: produto?.description ?? "",
    sort_order: String(produto?.sort_order ?? 0),
    active: produto?.active ?? true,
  };
}

export function ModalDeProduto({ controle }: { controle: React.RefObject<ControleDeModal<Produto | null> | null> }) {
  const [aberto, setAberto] = React.useState(false);
  const [produto, setProduto] = React.useState<Produto | null>(null);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const form = useForm<ProdutoFormulario>({
    resolver: zodResolver(ProdutoFormulario),
    defaultValues: valoresDoProduto(null),
  });

  // O modal fica montado para não perder a animação; o estado é reposto **no
  // evento que abre**. Sem isto, reabrir em outro produto mostraria os valores
  // do anterior por um quadro — e, se o usuário salvasse rápido, para valer.
  React.useImperativeHandle(
    controle,
    () => ({
      abrir(escolhido: Produto | null) {
        setProduto(escolhido);
        form.reset(valoresDoProduto(escolhido));
        setErroGeral(null);
        setAberto(true);
      },
    }),
    [form],
  );

  const aoMudar = setAberto;
  const { errors, isSubmitting } = form.formState;
  // `useWatch`, não `form.watch()`: veja o comentário em `ModalDePeriodo`.
  const consumo = useWatch({ control: form.control, name: "consumes" });

  async function enviar(valores: ProdutoFormulario) {
    setErroGeral(null);
    const resultado = await salvarProduto(produto?.id ?? null, valores);
    if (!resultado.ok) {
      setErroGeral(aplicarFalha(resultado, form, { CODE_IN_USE: "code" }));
      return;
    }
    aoMudar(false);
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={aoMudar}
      title={produto ? `Editar ${produto.name}` : "Novo produto"}
      description="Produto é o que você vende ao cliente. O apartamento que se ocupa e se limpa é a unidade."
      footer={<Rodape formulario="form-produto" enviando={isSubmitting} aoCancelar={() => aoMudar(false)} />}
    >
      <AvisoGeral mensagem={erroGeral} />
      <form id="form-produto" onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        <div className="grid gap-4 sm:grid-cols-2">
          <Campo
            id="produto-code"
            label="Código"
            obrigatorio
            erro={errors.code?.message}
            hint="Um código curto que não muda — AP2S, SP, COB, COMPLETA."
          >
            {(p) => <Input {...p} {...form.register("code")} autoCapitalize="characters" className="font-mono" />}
          </Campo>

          <Campo id="produto-name" label="Nome comercial" obrigatorio erro={errors.name?.message}>
            {(p) => <Input {...p} {...form.register("name")} />}
          </Campo>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo
            id="produto-capacity"
            label="Capacidade (hóspedes)"
            obrigatorio
            erro={errors.capacity?.message}
            hint="Quantas pessoas o produto aceita. Não é a soma dos apartamentos: a Completa aceita 24, mesmo que os apartamentos somem 40."
          >
            {(p) => <Input {...p} {...form.register("capacity")} inputMode="numeric" className="tabular-nums" />}
          </Campo>

          <Campo
            id="produto-cleaning"
            label="Taxa de limpeza (R$)"
            obrigatorio
            erro={errors.cleaning_fee_cents?.message}
            hint="Cobrada uma vez por estadia. O desconto nunca incide sobre ela."
          >
            {(p) => (
              <Input {...p} {...form.register("cleaning_fee_cents")} inputMode="decimal" className="text-right tabular-nums" />
            )}
          </Campo>
        </div>

        <Campo
          id="produto-consumes"
          label="O que uma venda ocupa"
          obrigatorio
          erro={errors.consumes?.message}
          hint={
            consumo === "all_members"
              ? "Uma venda ocupa TODOS os apartamentos do produto. É o caso da casa inteira: vendê-lo fecha o calendário para todos os outros produtos."
              : "Uma venda ocupa UM dos apartamentos do produto. O sistema escolhe o que deixa o calendário menos picado."
          }
        >
          {(p) => (
            <Select {...p} {...form.register("consumes")}>
              <option value="one_member">Um dos apartamentos do produto</option>
              <option value="all_members">Todos os apartamentos do produto</option>
            </Select>
          )}
        </Campo>

        <Campo
          id="produto-sort"
          label="Ordem no catálogo"
          erro={errors.sort_order?.message}
          hint="Define a ordem em que os produtos aparecem nas listas."
        >
          {(p) => <Input {...p} {...form.register("sort_order")} inputMode="numeric" className="tabular-nums" />}
        </Campo>

        <Campo id="produto-description" label="Descrição" erro={errors.description?.message}>
          {(p) => <Textarea {...p} {...form.register("description")} />}
        </Campo>

        <CheckboxCampo
          id="produto-active"
          label="Ativo"
          hint="Desmarcado, o produto deixa de ser vendido; as reservas antigas continuam visíveis."
          {...form.register("active")}
        />
      </form>
    </ModalShell>
  );
}

// ─────────────────────────────── Unidade ───────────────────────────────────

function valoresDaUnidade(unidade: Unidade | null): UnidadeFormulario {
  return {
    code: unidade?.code ?? "",
    name: unidade?.name ?? "",
    floor: unidade?.floor ?? "",
    notes: unidade?.notes ?? "",
    sort_order: String(unidade?.sort_order ?? 0),
    active: unidade?.active ?? true,
  };
}

export function ModalDeUnidade({ controle }: { controle: React.RefObject<ControleDeModal<Unidade | null> | null> }) {
  const [aberto, setAberto] = React.useState(false);
  const [unidade, setUnidade] = React.useState<Unidade | null>(null);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const form = useForm<UnidadeFormulario>({
    resolver: zodResolver(UnidadeFormulario),
    defaultValues: valoresDaUnidade(null),
  });

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(escolhida: Unidade | null) {
        setUnidade(escolhida);
        form.reset(valoresDaUnidade(escolhida));
        setErroGeral(null);
        setAberto(true);
      },
    }),
    [form],
  );

  const aoMudar = setAberto;
  const { errors, isSubmitting } = form.formState;

  async function enviar(valores: UnidadeFormulario) {
    setErroGeral(null);
    const resultado = await salvarUnidade(unidade?.id ?? null, valores);
    if (!resultado.ok) {
      setErroGeral(aplicarFalha(resultado, form, { CODE_IN_USE: "code" }));
      return;
    }
    aoMudar(false);
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={aoMudar}
      title={unidade ? `Editar ${unidade.code}` : "Nova unidade"}
      description="Um apartamento de verdade. O sistema nunca deixa a mesma unidade ser vendida duas vezes na mesma noite."
      footer={<Rodape formulario="form-unidade" enviando={isSubmitting} aoCancelar={() => aoMudar(false)} />}
    >
      <AvisoGeral mensagem={erroGeral} />
      <form id="form-unidade" onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        <div className="grid gap-4 sm:grid-cols-2">
          <Campo
            id="unidade-code"
            label="Código"
            obrigatorio
            erro={errors.code?.message}
            hint="Como a equipe chama o apartamento — AP-01, SP-04, COB-01. Também define a ordem nas listas."
          >
            {(p) => <Input {...p} {...form.register("code")} autoCapitalize="characters" className="font-mono" />}
          </Campo>

          <Campo id="unidade-name" label="Nome" obrigatorio erro={errors.name?.message}>
            {(p) => <Input {...p} {...form.register("name")} />}
          </Campo>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="unidade-floor" label="Pavimento" erro={errors.floor?.message}>
            {(p) => <Input {...p} {...form.register("floor")} placeholder="Térreo" />}
          </Campo>

          <Campo id="unidade-sort" label="Ordem" erro={errors.sort_order?.message}>
            {(p) => <Input {...p} {...form.register("sort_order")} inputMode="numeric" className="tabular-nums" />}
          </Campo>
        </div>

        <Campo id="unidade-notes" label="Observações da operação" erro={errors.notes?.message}>
          {(p) => <Textarea {...p} {...form.register("notes")} />}
        </Campo>

        <CheckboxCampo
          id="unidade-active"
          label="Ativa"
          hint="Desmarcada, a unidade deixa de receber reservas e sai do mapa; o histórico continua."
          {...form.register("active")}
        />
      </form>
    </ModalShell>
  );
}

// ────────────────────────────── Composição ─────────────────────────────────

/**
 * Quais unidades o produto consome.
 *
 * A tela manda **o estado inteiro da grade**: o `PUT` substitui, não acumula, e
 * o que não vier deixa de fazer parte. É a mesma semântica de
 * `PUT /roles/{id}/permissions`, e é a razão de não haver botão de "remover
 * unidade" — desmarcar já é remover.
 */
export type AlvoDaComposicao = { produto: Produto; atual: UnidadeDaComposicao[] };

export function ModalDeComposicao({
  controle,
  unidades,
}: {
  controle: React.RefObject<ControleDeModal<AlvoDaComposicao> | null>;
  unidades: Unidade[];
}) {
  const [aberto, setAberto] = React.useState(false);
  const [produto, setProduto] = React.useState<Produto | null>(null);
  const [selecionadas, setSelecionadas] = React.useState<Set<string>>(new Set());
  const [erro, setErro] = React.useState<string | null>(null);
  const [enviando, setEnviando] = React.useState(false);

  // A composição atual chega **junto com o clique**, e não como prop que o
  // modal fechado fica observando: assim a marcação nasce do produto escolhido
  // agora, sem efeito nenhum reconciliando duas fontes.
  React.useImperativeHandle(
    controle,
    () => ({
      abrir({ produto: escolhido, atual }: AlvoDaComposicao) {
        setProduto(escolhido);
        setSelecionadas(new Set(atual.map((u) => u.unit_id)));
        setErro(null);
        setAberto(true);
      },
    }),
    [],
  );

  const aoMudar = setAberto;

  const ordenadas = React.useMemo(
    () => [...unidades].sort((a, b) => a.code.localeCompare(b.code, "pt-BR")),
    [unidades],
  );

  function alternar(id: string) {
    setSelecionadas((atual) => {
      const proximo = new Set(atual);
      if (proximo.has(id)) proximo.delete(id);
      else proximo.add(id);
      return proximo;
    });
  }

  async function salvar() {
    if (!produto) return;
    if (selecionadas.size === 0) {
      setErro("Escolha ao menos uma unidade — sem apartamento o produto não pode ser vendido.");
      return;
    }
    setEnviando(true);
    setErro(null);
    try {
      const escolhidas = ordenadas
        .filter((u) => selecionadas.has(u.id))
        .map((u) => ({ id: u.id, code: u.code }));
      const resultado = await salvarComposicao(produto.id, escolhidas);
      if (!resultado.ok) {
        setErro(mensagemDoErro(resultado.code));
        return;
      }
      aoMudar(false);
    } finally {
      setEnviando(false);
    }
  }

  const total = selecionadas.size;
  const consomeTodas = produto?.consumes === "all_members";

  return (
    <ModalShell
      open={aberto}
      onOpenChange={aoMudar}
      title={produto ? `Apartamentos de ${produto.name}` : "Apartamentos do produto"}
      description="Quais apartamentos formam este produto."
      footer={
        <>
          <Button variant="ghost" onClick={() => aoMudar(false)} disabled={enviando}>
            Cancelar
          </Button>
          <Button onClick={salvar} disabled={enviando}>
            {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
            Salvar apartamentos
          </Button>
        </>
      }
    >
      {erro ? <AvisoGeral mensagem={erro} /> : null}

      <Nota variante={consomeTodas ? "atencao" : "info"} className="mb-4">
        {consomeTodas ? (
          <>
            Uma venda deste produto ocupa <strong>todas</strong> as unidades marcadas de uma vez. Se
            alguma delas já estiver ocupada, a venda não é possível naquelas datas.
          </>
        ) : (
          <>
            Uma venda ocupa <strong>uma</strong> das unidades marcadas, escolhida pelo sistema. Com{" "}
            {total} marcada{total === 1 ? "" : "s"}, dá para vender {total} estadia{total === 1 ? "" : "s"}{" "}
            simultânea{total === 1 ? "" : "s"} deste produto.
          </>
        )}
      </Nota>

      <ul className="flex flex-col gap-1.5">
        {ordenadas.map((unidade) => {
          const marcada = selecionadas.has(unidade.id);
          return (
            <li key={unidade.id}>
              <label
                className={cn(
                  "flex cursor-pointer items-center gap-3 rounded-lg border px-3 py-2.5 transition-colors",
                  marcada ? "border-primary/40 bg-accent/40" : "border-border/60 bg-card/60 hover:bg-muted/40",
                )}
              >
                <input
                  type="checkbox"
                  checked={marcada}
                  onChange={() => alternar(unidade.id)}
                  className="size-4 accent-[var(--primary)]"
                />
                <span className="font-mono text-sm">{unidade.code}</span>
                <span className="min-w-0 flex-1 truncate text-sm text-muted-foreground">{unidade.name}</span>
                {!unidade.active ? <Badge variant="outline">inativa</Badge> : null}
              </label>
            </li>
          );
        })}
      </ul>

      <p className="mt-3 text-xs text-muted-foreground">
        <span className="tabular-nums">{total}</span> de {ordenadas.length} unidades marcadas, em ordem de
        código.
      </p>
    </ModalShell>
  );
}
