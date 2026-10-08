"use client";

import * as React from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";

import { FotoDoBem } from "@/components/bens/foto";
import { AvisoGeral, RodapeDoModal } from "@/components/bens/formulario-comum";
import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { aplicarFalha } from "@/lib/acoes/formulario";
import { colocarBem, salvarColocacao } from "@/lib/bens/acoes";
import { ColocacaoFormulario } from "@/lib/bens/esquemas";
import { mensagemDeBens } from "@/lib/bens/mensagens";
import { ROTULO_DA_CATEGORIA } from "@/lib/bens/rotulos";
import type { AmbienteDoInventario, Bem, Colocacao } from "@/lib/bens/tipos";
import { cn } from "@/lib/utils";

export type AlvoDaColocacao =
  | { modo: "colocar"; ambiente: AmbienteDoInventario }
  | { modo: "editar"; ambiente: AmbienteDoInventario; colocacao: Colocacao };

/**
 * Pôr um bem num ambiente, ou mudar quanto se espera dele ali.
 *
 * A quantidade daqui é o **padrão da casa**, não a contada. Mudá-la não
 * reescreve conferência nenhuma — a conferência congela o esperado na abertura
 * — e por isso a nota diz que vale "da próxima em diante". Zero é aceito: é o
 * cômodo que, de propósito, não tem a peça.
 */
export function ModalDeColocacao({
  controle,
  catalogo,
}: {
  controle: React.RefObject<ControleDeModal<AlvoDaColocacao> | null>;
  /** O catálogo em uso — a lista de onde se escolhe o bem a colocar. */
  catalogo: readonly Bem[];
}) {
  const [aberto, setAberto] = React.useState(false);
  const [alvo, setAlvo] = React.useState<AlvoDaColocacao | null>(null);
  const [itemId, setItemId] = React.useState("");
  const [busca, setBusca] = React.useState("");
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const [erroDoItem, setErroDoItem] = React.useState<string | null>(null);
  const form = useForm<ColocacaoFormulario>({
    resolver: zodResolver(ColocacaoFormulario),
    defaultValues: { expected_qty: "1", note: "" },
  });

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(escolhido: AlvoDaColocacao) {
        setAlvo(escolhido);
        setItemId("");
        setBusca("");
        setErroGeral(null);
        setErroDoItem(null);
        form.reset(
          escolhido.modo === "editar"
            ? { expected_qty: String(escolhido.colocacao.expected_qty), note: escolhido.colocacao.note ?? "" }
            : { expected_qty: "1", note: "" },
        );
        setAberto(true);
      },
    }),
    [form],
  );

  const jaColocados = React.useMemo(() => new Set(alvo?.ambiente.items.map((c) => c.item_id) ?? []), [alvo]);
  const opcoes = React.useMemo(() => {
    const termo = busca.trim().toLocaleLowerCase("pt-BR");
    return catalogo.filter(
      (b) => !jaColocados.has(b.id) && (termo === "" || b.name.toLocaleLowerCase("pt-BR").includes(termo)),
    );
  }, [catalogo, jaColocados, busca]);

  const { errors, isSubmitting } = form.formState;

  async function enviar(v: ColocacaoFormulario) {
    if (!alvo) return;
    setErroGeral(null);
    setErroDoItem(null);
    if (alvo.modo === "colocar" && !itemId) {
      setErroDoItem("Escolha o bem que fica neste ambiente.");
      return;
    }
    const r =
      alvo.modo === "editar"
        ? await salvarColocacao(alvo.colocacao.id, v)
        : await colocarBem(alvo.ambiente.id, itemId, v);
    if (!r.ok) {
      if (r.code === "CODE_IN_USE") {
        setErroDoItem(mensagemDeBens(r, "colocacao"));
        return;
      }
      if (typeof r.details.item_id === "string") setErroDoItem(r.details.item_id);
      setErroGeral(aplicarFalha(r, form));
      return;
    }
    setAberto(false);
  }

  const editando = alvo?.modo === "editar";

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title={
        alvo?.modo === "editar"
          ? `${alvo.colocacao.item_name ?? "Bem"} — ${alvo.ambiente.name}`
          : `Colocar um bem em ${alvo?.ambiente.name ?? "ambiente"}`
      }
      description="Quanto se espera encontrar deste bem neste cômodo."
      footer={
        <RodapeDoModal
          formulario="form-colocacao"
          enviando={isSubmitting}
          aoCancelar={() => setAberto(false)}
          rotulo={editando ? "Salvar quantidade" : "Colocar no ambiente"}
        />
      }
    >
      <AvisoGeral mensagem={erroGeral} />
      <form id="form-colocacao" onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        {alvo?.modo === "colocar" ? (
          <fieldset className="flex flex-col gap-2">
            <legend className="text-sm font-medium">
              Bem <span className="text-destructive">*</span>
            </legend>
            <Input
              aria-label="Buscar no catálogo"
              value={busca}
              onChange={(e) => setBusca(e.target.value)}
              placeholder="Buscar no catálogo"
              className="h-9"
            />
            {opcoes.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                {catalogo.length === 0
                  ? "O catálogo está vazio. Cadastre o bem primeiro, na aba Catálogo."
                  : "Nada no catálogo com esse nome que já não esteja neste ambiente."}
              </p>
            ) : (
              <ul className="flex max-h-72 flex-col gap-1 overflow-y-auto overscroll-contain pr-1" role="radiogroup" aria-label="Bens do catálogo">
                {opcoes.map((b) => (
                  <li key={b.id}>
                    <label
                      className={cn(
                        "flex cursor-pointer items-center gap-3 rounded-lg border px-2 py-1.5 transition-colors",
                        itemId === b.id ? "border-primary/50 bg-accent/40" : "border-border/60 bg-card/60 hover:bg-muted/40",
                      )}
                    >
                      <input
                        type="radio"
                        name="item_id"
                        value={b.id}
                        checked={itemId === b.id}
                        onChange={() => setItemId(b.id)}
                        className="size-4 accent-[var(--primary)]"
                      />
                      <FotoDoBem midia={b.cover} alt="" className="size-10" />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm">{b.name}</span>
                        <span className="block truncate text-xs text-muted-foreground">{ROTULO_DA_CATEGORIA[b.category]}</span>
                      </span>
                    </label>
                  </li>
                ))}
              </ul>
            )}
            {erroDoItem ? (
              <p role="alert" className="text-xs font-medium text-destructive">
                {erroDoItem}
              </p>
            ) : null}
          </fieldset>
        ) : erroDoItem ? (
          <AvisoGeral mensagem={erroDoItem} />
        ) : null}

        <Campo
          id="colocacao-qty"
          label="Quantidade esperada"
          obrigatorio
          erro={errors.expected_qty?.message}
          hint="O padrão da casa. Zero quer dizer “este cômodo não tem, de propósito”."
        >
          {(p) => <Input {...p} {...form.register("expected_qty")} inputMode="numeric" className="w-32 tabular-nums" />}
        </Campo>

        <Campo id="colocacao-note" label="Observação" erro={errors.note?.message} hint="Onde fica — “armário de cima, à esquerda”.">
          {(p) => <Textarea {...p} rows={2} {...form.register("note")} />}
        </Campo>

        {editando ? (
          <Nota>
            Mudar a quantidade vale <strong>da próxima conferência em diante</strong>. Conferências já abertas ou
            fechadas continuam esperando o que esperavam quando começaram.
          </Nota>
        ) : null}
      </form>
    </ModalShell>
  );
}
