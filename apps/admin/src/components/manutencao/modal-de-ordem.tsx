"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2, Lock, Search, Wrench } from "lucide-react";

import { notificarInfo, notificarSucesso } from "@/components/manutencao/avisos";
import { CamposDoPeriodo } from "@/components/manutencao/periodo";
import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { abrirOrdem, buscarBens, opcoesDaUnidade } from "@/lib/manutencao/acoes";
import { OrdemFormulario, ordemVazia } from "@/lib/manutencao/esquemas";
import { caminhoDaOrdem, falhaNosCampos, ordemJaAberta, type CampoDaOrdem } from "@/lib/manutencao/mensagens";
import { ROTULO_DA_PRIORIDADE, periodoPorExtenso } from "@/lib/manutencao/rotulos";
import { PRIORIDADES_DA_ORDEM, type BemDoCatalogo, type OpcoesDaUnidade } from "@/lib/manutencao/tipos";

/** A unidade como o seletor a mostra. */
export type UnidadeDoSeletor = { id: string; code: string; name: string };

/**
 * Com o que o modal abre.
 *
 * - `avaria`: a ação "Abrir ordem de manutenção" da tela de avarias. Unidade,
 *   cômodo e bem **travados** (são os da avaria — o contrato recusa outros) e
 *   um título sugerido, editável.
 * - `unitId`: a unidade já escolhida no filtro da lista.
 */
export type AlvoDaOrdem = {
  unitId?: string;
  avaria?: {
    id: string;
    unitId: string;
    unitCode?: string;
    roomId: string;
    roomName?: string;
    itemId: string;
    itemName?: string;
    tituloSugerido: string;
  };
};

/**
 * Abrir uma ordem de manutenção — **pensado para o celular**: quem registra é
 * a operação, de pé, dentro do apartamento. Selects nativos (abrem a roleta do
 * sistema), campos de 44px, nada que dependa de hover, e o mínimo obrigatório
 * é unidade e título.
 *
 * Dinheiro vai em centavos inteiros (`lib/dinheiro.ts`, por string, sem float).
 * O período do bloqueio só é conferido na forma; **quem decide se vale é a
 * API** (`maintenance.Replan`), e o `422` dela cai no campo da data. O `min`
 * da data é conveniência do seletor, não regra: o formulário é `noValidate`.
 */
export function ModalDeOrdem({
  controle,
  unidades,
  irParaOrdem = true,
}: {
  controle: React.RefObject<ControleDeModal<AlvoDaOrdem> | null>;
  unidades: readonly UnidadeDoSeletor[];
  /** Depois de criar, abrir a ordem (lista) ou ficar onde está (avarias). */
  irParaOrdem?: boolean;
}) {
  const router = useRouter();
  const [aberto, setAberto] = React.useState(false);
  const [avaria, setAvaria] = React.useState<AlvoDaOrdem["avaria"] | null>(null);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const [erroPeriodo, setErroPeriodo] = React.useState<string | null>(null);
  const [opcoes, setOpcoes] = React.useState<OpcoesDaUnidade | null>(null);
  const [carregando, setCarregando] = React.useState(false);
  const [erroOpcoes, setErroOpcoes] = React.useState<string | null>(null);
  const [bemExtra, setBemExtra] = React.useState<BemDoCatalogo | null>(null);
  const pedidoDeOpcoes = React.useRef(0);

  const form = useForm<OrdemFormulario>({ resolver: zodResolver(OrdemFormulario), defaultValues: ordemVazia() });
  const { errors, isSubmitting } = form.formState;

  /** Carrega cômodos e bens da unidade. Chamado no evento (abrir, trocar de
   *  unidade), não num efeito: o pedido mais novo vence o mais velho. */
  const carregarOpcoes = React.useCallback(async (unitId: string) => {
    const pedido = ++pedidoDeOpcoes.current;
    setOpcoes(null);
    setErroOpcoes(null);
    setCarregando(Boolean(unitId));
    if (!unitId) return;
    const r = await opcoesDaUnidade(unitId);
    if (pedido !== pedidoDeOpcoes.current) return;
    setCarregando(false);
    if (r.ok) setOpcoes(r.data);
    else setErroOpcoes("Os cômodos desta unidade não carregaram. Dá para abrir a ordem sem cômodo e sem bem.");
  }, []);

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(alvo: AlvoDaOrdem) {
        const a = alvo.avaria ?? null;
        form.reset(
          a
            ? ordemVazia({ unit_id: a.unitId, room_id: a.roomId, item_id: a.itemId, issue_id: a.id, title: a.tituloSugerido })
            : ordemVazia({ unit_id: alvo.unitId ?? "" }),
        );
        setAvaria(a);
        setErroGeral(null);
        setErroPeriodo(null);
        setBemExtra(null);
        setAberto(true);
        if (a) {
          // Travado na avaria: cômodo e bem já estão escritos, não há o que escolher.
          pedidoDeOpcoes.current++;
          setOpcoes(null);
          setErroOpcoes(null);
          setCarregando(false);
        } else {
          void carregarOpcoes(alvo.unitId ?? "");
        }
      },
    }),
    [form, carregarOpcoes],
  );

  const unitId = useWatch({ control: form.control, name: "unit_id" });
  const roomId = useWatch({ control: form.control, name: "room_id" });
  const bloquear = useWatch({ control: form.control, name: "bloquear" });
  const de = useWatch({ control: form.control, name: "block_from" });
  const ate = useWatch({ control: form.control, name: "block_to" });

  const unidadeRotulo = avaria?.unitCode ?? unidades.find((u) => u.id === unitId)?.code ?? "a unidade";

  async function enviar(v: OrdemFormulario) {
    setErroGeral(null);
    setErroPeriodo(null);
    const r = await abrirOrdem(v);

    if (r.ok) {
      setAberto(false);
      const ver = (
        <Link href={caminhoDaOrdem(r.data.id)} className="text-sm font-medium underline underline-offset-4">
          Ver ordem
        </Link>
      );
      notificarSucesso(
        "Ordem de manutenção aberta",
        r.data.block ? `Calendário bloqueado: ${periodoPorExtenso(r.data.block)}.` : undefined,
        irParaOrdem ? undefined : ver,
      );
      if (irParaOrdem) router.push(caminhoDaOrdem(r.data.id));
      return;
    }

    // O segundo toque na mesma avaria: leva à ordem que já existe, em vez de
    // um "erro" seco. É o caso comum (duas pessoas no plantão, tela velha).
    const existente = ordemJaAberta(r);
    if (existente) {
      setAberto(false);
      notificarInfo(
        "Esta avaria já tem uma ordem aberta",
        "Ninguém abriu outra: continue pela que já existe.",
        <Link href={existente.caminho} className="text-sm font-medium underline underline-offset-4">
          Abrir a ordem
        </Link>,
      );
      router.refresh();
      return;
    }

    const leitura = falhaNosCampos(r, "criacao");
    for (const [campo, mensagem] of Object.entries(leitura.campos) as [CampoDaOrdem, string][]) {
      form.setError(campo, { type: "server", message: mensagem });
    }
    setErroPeriodo(leitura.periodo);
    setErroGeral(leitura.geral);
  }

  const ambientes = opcoes?.ambientes ?? [];
  const ambiente = ambientes.find((a) => a.id === roomId);
  const itensDaUnidade = ambiente ? [{ id: ambiente.id, name: ambiente.name, itens: ambiente.itens }] : ambientes;
  const extraJaListado = bemExtra && itensDaUnidade.some((a) => a.itens.some((i) => i.id === bemExtra.id));

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title={avaria ? "Abrir ordem para esta avaria" : "Nova ordem de manutenção"}
      description="Só unidade e título são obrigatórios. O resto ajuda quem vai consertar."
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button type="submit" form="form-ordem" disabled={isSubmitting} className="max-md:h-11 max-md:flex-1">
            {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Wrench aria-hidden="true" />}
            Abrir ordem
          </Button>
        </>
      }
    >
      {erroGeral ? (
        <p role="alert" className="mb-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erroGeral}
        </p>
      ) : null}

      <form id="form-ordem" noValidate onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        {avaria ? (
          <div className="rounded-xl border border-border/60 bg-muted/30 px-3.5 py-3 text-sm" aria-label="Onde">
            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Lock aria-hidden="true" className="size-3.5" />
              Da avaria — unidade, cômodo e bem não mudam
            </p>
            <p className="mt-1">
              <span className="font-mono">{avaria.unitCode ?? "Unidade"}</span> · {avaria.roomName ?? "cômodo"} ·{" "}
              <strong className="font-medium">{avaria.itemName ?? "bem"}</strong>
            </p>
            {errors.issue_id?.message || errors.room_id?.message || errors.item_id?.message || errors.unit_id?.message ? (
              <p role="alert" className="mt-1 text-xs font-medium text-destructive">
                {errors.issue_id?.message ?? errors.room_id?.message ?? errors.item_id?.message ?? errors.unit_id?.message}
              </p>
            ) : null}
          </div>
        ) : (
          <>
            <Campo id="ordem-unidade" label="Unidade" obrigatorio erro={errors.unit_id?.message}>
              {(p) => (
                <Select
                  {...p}
                  {...form.register("unit_id", {
                    // Trocar de unidade invalida cômodo e bem: eram da outra.
                    onChange: (e: React.ChangeEvent<HTMLSelectElement>) => {
                      form.setValue("room_id", "");
                      form.setValue("item_id", "");
                      setBemExtra(null);
                      void carregarOpcoes(e.target.value);
                    },
                  })}
                >
                  <option value="">Escolha</option>
                  {unidades.map((u) => (
                    <option key={u.id} value={u.id}>
                      {u.code} — {u.name}
                    </option>
                  ))}
                </Select>
              )}
            </Campo>

            {erroOpcoes ? <p className="text-xs text-muted-foreground">{erroOpcoes}</p> : null}

            <div className="grid gap-4 sm:grid-cols-2">
              <Campo
                id="ordem-comodo"
                label="Cômodo (opcional)"
                erro={errors.room_id?.message}
                hint={carregando ? "Carregando os cômodos…" : undefined}
              >
                {(p) => (
                  <Select
                    {...p}
                    {...form.register("room_id", {
                      // O bem escolhido pode não estar no cômodo novo — o
                      // contrato aceita bem fora do cômodo, mas a lista muda.
                      onChange: () => form.setValue("item_id", bemExtra?.id ?? ""),
                    })}
                    disabled={!unitId || carregando || ambientes.length === 0}
                  >
                    <option value="">{unitId ? "A unidade toda" : "Escolha a unidade antes"}</option>
                    {ambientes.map((a) => (
                      <option key={a.id} value={a.id}>
                        {a.name}
                      </option>
                    ))}
                  </Select>
                )}
              </Campo>

              <Campo id="ordem-bem" label="Bem (opcional)" erro={errors.item_id?.message}>
                {(p) => (
                  <Select {...p} {...form.register("item_id")} disabled={!unitId || carregando}>
                    <option value="">Nenhum em especial</option>
                    {bemExtra && !extraJaListado ? (
                      <optgroup label="Do catálogo">
                        <option value={bemExtra.id}>{bemExtra.name}</option>
                      </optgroup>
                    ) : null}
                    {itensDaUnidade.map((a) =>
                      a.itens.length > 0 ? (
                        <optgroup key={a.id} label={a.name}>
                          {a.itens.map((i) => (
                            <option key={`${a.id}-${i.id}`} value={i.id}>
                              {i.name}
                            </option>
                          ))}
                        </optgroup>
                      ) : null,
                    )}
                  </Select>
                )}
              </Campo>
            </div>

            {unitId ? (
              <BuscaNoCatalogo
                aoEscolher={(bem) => {
                  setBemExtra(bem);
                  form.setValue("item_id", bem.id, { shouldValidate: true });
                }}
              />
            ) : null}
          </>
        )}

        <Campo id="ordem-titulo" label="O que precisa ser feito" obrigatorio erro={errors.title?.message}>
          {(p) => (
            <Input {...p} {...form.register("title")} placeholder="Ar-condicionado da suíte não gela" maxLength={200} autoComplete="off" />
          )}
        </Campo>

        <Campo id="ordem-descricao" label="Detalhes (opcional)" erro={errors.description?.message}>
          {(p) => <Textarea {...p} rows={3} {...form.register("description")} maxLength={4000} />}
        </Campo>

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="ordem-prioridade" label="Prioridade" obrigatorio erro={errors.priority?.message}>
            {(p) => (
              <Select {...p} {...form.register("priority")}>
                {PRIORIDADES_DA_ORDEM.map((pr) => (
                  <option key={pr} value={pr}>
                    {ROTULO_DA_PRIORIDADE[pr]}
                  </option>
                ))}
              </Select>
            )}
          </Campo>
          <Campo id="ordem-custo" label="Custo (R$, opcional)" erro={errors.cost?.message} hint="A nota pode chegar depois.">
            {(p) => <Input {...p} {...form.register("cost")} inputMode="decimal" placeholder="350,00" className="tabular-nums" />}
          </Campo>
        </div>

        <div className="rounded-xl border border-border/60 bg-muted/20 p-3.5">
          <CheckboxCampo
            id="ordem-bloquear"
            label="Bloquear calendário"
            hint={`Tira ${unidadeRotulo} da venda enquanto o conserto acontece. Dá para pedir depois também.`}
            {...form.register("bloquear", { onChange: () => setErroPeriodo(null) })}
          />
          {bloquear ? (
            <CamposDoPeriodo
              className="mt-4"
              prefixo="ordem"
              de={de}
              ate={ate}
              registrarDe={form.register("block_from", { onChange: () => setErroPeriodo(null) })}
              registrarAte={form.register("block_to", { onChange: () => setErroPeriodo(null) })}
              erroDe={errors.block_from?.message}
              erroAte={errors.block_to?.message}
              erroDoPeriodo={erroPeriodo}
            />
          ) : null}
        </div>
      </form>
    </ModalShell>
  );
}

/**
 * Procurar o bem no catálogo inteiro — o defeito pode ser do móvel que ninguém
 * conta, e o contrato não exige que o bem esteja colocado no cômodo. Os
 * resultados são botões largos (toque, não hover).
 */
function BuscaNoCatalogo({ aoEscolher }: { aoEscolher: (bem: BemDoCatalogo) => void }) {
  const [aberta, setAberta] = React.useState(false);
  const [termo, setTermo] = React.useState("");
  const [resultado, setResultado] = React.useState<BemDoCatalogo[] | null>(null);
  const [buscando, setBuscando] = React.useState(false);
  const [erro, setErro] = React.useState<string | null>(null);

  if (!aberta) {
    return (
      <Button variant="link" size="sm" className="self-start px-0" onClick={() => setAberta(true)}>
        <Search aria-hidden="true" />
        Procurar o bem no catálogo inteiro
      </Button>
    );
  }

  async function buscar() {
    setBuscando(true);
    setErro(null);
    const r = await buscarBens(termo);
    setBuscando(false);
    if (r.ok) setResultado(r.data);
    else setErro("A busca no catálogo não respondeu agora.");
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-end gap-2">
        <Campo id="ordem-busca-bem" label="Procurar no catálogo" className="flex-1">
          {(p) => (
            <Input
              {...p}
              value={termo}
              onChange={(e) => setTermo(e.target.value)}
              onKeyDown={(e) => {
                // Enter aqui não pode submeter a ordem inteira.
                if (e.key === "Enter") {
                  e.preventDefault();
                  void buscar();
                }
              }}
              placeholder="Geladeira, chuveiro…"
              autoComplete="off"
            />
          )}
        </Campo>
        <Button variant="outline" className="h-11" onClick={() => void buscar()} disabled={buscando || termo.trim().length < 2}>
          {buscando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Search aria-hidden="true" />}
          Buscar
        </Button>
      </div>
      {erro ? <p className="text-xs text-destructive">{erro}</p> : null}
      {resultado ? (
        resultado.length === 0 ? (
          <p className="text-xs text-muted-foreground">Nenhum bem com esse nome no catálogo.</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {resultado.map((b) => (
              <li key={b.id}>
                <button
                  type="button"
                  onClick={() => {
                    aoEscolher(b);
                    setAberta(false);
                    setResultado(null);
                    setTermo("");
                  }}
                  className="flex min-h-11 w-full items-center rounded-xl border border-border/60 bg-card px-3.5 text-left text-sm outline-none transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring/35"
                >
                  {b.name}
                </button>
              </li>
            ))}
          </ul>
        )
      ) : null}
    </div>
  );
}
