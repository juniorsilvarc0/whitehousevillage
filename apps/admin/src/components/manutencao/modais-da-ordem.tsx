"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { CalendarX2, CheckCircle2, Loader2, Lock, Receipt, Save } from "lucide-react";

import { notificarSucesso } from "@/components/manutencao/avisos";
import { CamposDoPeriodo } from "@/components/manutencao/periodo";
import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import type { Falha } from "@/lib/acoes/resultado";
import { concluirOrdem, definirBloqueio, lancarCusto, opcoesDaUnidade, salvarOrdem } from "@/lib/manutencao/acoes";
import {
  CustoFormulario,
  EdicaoFormulario,
  PeriodoFormulario,
  custoParaCampo,
  valoresDaEdicao,
} from "@/lib/manutencao/esquemas";
import { falhaNosCampos, pedeRecarga, type CampoDaOrdem, type Contexto } from "@/lib/manutencao/mensagens";
import { ROTULO_DA_PRIORIDADE, avisoDoEncerramento, periodoPorExtenso } from "@/lib/manutencao/rotulos";
import { PRIORIDADES_DA_ORDEM, type OpcoesDaUnidade, type OrdemDeManutencao } from "@/lib/manutencao/tipos";
import { cn } from "@/lib/utils";

/**
 * Os formulários do detalhe da ordem. Todos abrem pelo evento (controle
 * imperativo, `components/layout/controle-de-modal.ts`), nascem limpos, e
 * levam a recusa da API para o campo certo pelo código (`falhaNosCampos`).
 */

type Controle = React.RefObject<ControleDeModal<OrdemDeManutencao> | null>;

function AvisoGeral({ mensagem }: { mensagem: string | null }) {
  if (!mensagem) return null;
  return (
    <p role="alert" className="mb-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
      {mensagem}
    </p>
  );
}

/** Leva a falha ao formulário; devolve a mensagem geral e a do período. */
function aplicar<T extends Record<string, unknown>>(
  falha: Falha,
  contexto: Contexto,
  setError: (campo: CampoDaOrdem & keyof T, erro: { type: string; message: string }) => void,
  campos: readonly (keyof T)[],
): { geral: string | null; periodo: string | null } {
  const leitura = falhaNosCampos(falha, contexto);
  const sobrou: string[] = [];
  for (const [campo, mensagem] of Object.entries(leitura.campos) as [CampoDaOrdem, string][]) {
    if ((campos as readonly string[]).includes(campo)) setError(campo as CampoDaOrdem & keyof T, { type: "server", message: mensagem });
    else sobrou.push(mensagem);
  }
  return { geral: leitura.geral ?? (sobrou.length > 0 ? sobrou.join(" ") : null), periodo: leitura.periodo };
}

// ── Editar (PUT) ────────────────────────────────────────────────────────────

/**
 * Título, descrição, prioridade, cômodo, bem e custo. Com avaria ligada,
 * cômodo e bem ficam **travados** — são os da avaria, e o `PUT` nem os manda.
 */
export function ModalDeEdicao({ controle }: { controle: Controle }) {
  const router = useRouter();
  const [ordem, setOrdem] = React.useState<OrdemDeManutencao | null>(null);
  const [aberto, setAberto] = React.useState(false);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const [opcoes, setOpcoes] = React.useState<OpcoesDaUnidade | null>(null);
  const [carregando, setCarregando] = React.useState(false);
  const form = useForm<EdicaoFormulario>({
    resolver: zodResolver(EdicaoFormulario),
    defaultValues: { room_id: "", item_id: "", title: "", description: "", priority: "normal", cost: "" },
  });
  const { errors, isSubmitting } = form.formState;

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(alvo: OrdemDeManutencao) {
        form.reset(valoresDaEdicao(alvo));
        setOrdem(alvo);
        setErroGeral(null);
        setOpcoes(null);
        setAberto(true);
        if (!alvo.issue_id) {
          setCarregando(true);
          void opcoesDaUnidade(alvo.unit_id).then((r) => {
            setCarregando(false);
            if (r.ok) setOpcoes(r.data);
          });
        }
      },
    }),
    [form],
  );

  const roomId = useWatch({ control: form.control, name: "room_id" });
  const travado = Boolean(ordem?.issue_id);
  const ambientes = opcoes?.ambientes ?? [];
  const ambiente = ambientes.find((a) => a.id === roomId);
  const grupos = ambiente ? [ambiente] : ambientes;
  // O que já está gravado continua escolhível mesmo fora da lista (cômodo
  // desativado, bem procurado no catálogo): sumir com ele seria limpá-lo calado.
  const comodoAtualFora = ordem?.room_id && !ambientes.some((a) => a.id === ordem.room_id);
  const bemAtualFora = ordem?.item_id && !grupos.some((a) => a.itens.some((i) => i.id === ordem.item_id));

  async function enviar(v: EdicaoFormulario) {
    if (!ordem) return;
    setErroGeral(null);
    const r = await salvarOrdem(ordem.id, v, travado);
    if (r.ok) {
      setAberto(false);
      notificarSucesso("Ordem atualizada");
      return;
    }
    const { geral } = aplicar<EdicaoFormulario>(r, "edicao", form.setError, Object.keys(v) as (keyof EdicaoFormulario)[]);
    setErroGeral(geral);
    if (pedeRecarga(r)) router.refresh();
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title="Editar a ordem"
      description={ordem ? `${ordem.unit_code} — ${ordem.title}` : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button type="submit" form="form-edicao-ordem" disabled={isSubmitting} className="max-md:h-11 max-md:flex-1">
            {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Save aria-hidden="true" />}
            Salvar
          </Button>
        </>
      }
    >
      <AvisoGeral mensagem={erroGeral} />
      <form id="form-edicao-ordem" noValidate onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        <Campo id="edicao-titulo" label="O que precisa ser feito" obrigatorio erro={errors.title?.message}>
          {(p) => <Input {...p} {...form.register("title")} maxLength={200} autoComplete="off" />}
        </Campo>
        <Campo id="edicao-descricao" label="Detalhes" erro={errors.description?.message}>
          {(p) => <Textarea {...p} rows={3} {...form.register("description")} maxLength={4000} />}
        </Campo>

        {travado ? (
          <div className="rounded-xl border border-border/60 bg-muted/30 px-3.5 py-3 text-sm" aria-label="Onde">
            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Lock aria-hidden="true" className="size-3.5" />
              Cômodo e bem são os da avaria de origem e não mudam
            </p>
            <p className="mt-1">
              {ordem?.room_name ?? "cômodo"} · <strong className="font-medium">{ordem?.item_name ?? "bem"}</strong>
            </p>
            {errors.room_id?.message || errors.item_id?.message ? (
              <p role="alert" className="mt-1 text-xs font-medium text-destructive">
                {errors.room_id?.message ?? errors.item_id?.message}
              </p>
            ) : null}
          </div>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2">
            <Campo id="edicao-comodo" label="Cômodo" erro={errors.room_id?.message} hint={carregando ? "Carregando os cômodos…" : undefined}>
              {(p) => (
                <Select {...p} {...form.register("room_id")} disabled={carregando}>
                  <option value="">A unidade toda</option>
                  {comodoAtualFora ? <option value={ordem!.room_id!}>{ordem?.room_name ?? "Cômodo atual"}</option> : null}
                  {ambientes.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.name}
                    </option>
                  ))}
                </Select>
              )}
            </Campo>
            <Campo id="edicao-bem" label="Bem" erro={errors.item_id?.message}>
              {(p) => (
                <Select {...p} {...form.register("item_id")} disabled={carregando}>
                  <option value="">Nenhum em especial</option>
                  {bemAtualFora ? <option value={ordem!.item_id!}>{ordem?.item_name ?? "Bem atual"}</option> : null}
                  {grupos.map((a) =>
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
        )}

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="edicao-prioridade" label="Prioridade" obrigatorio erro={errors.priority?.message}>
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
          <Campo id="edicao-custo" label="Custo (R$)" erro={errors.cost?.message} hint="Vazio = ainda não lançado.">
            {(p) => <Input {...p} {...form.register("cost")} inputMode="decimal" placeholder="350,00" className="tabular-nums" />}
          </Campo>
        </div>
      </form>
    </ModalShell>
  );
}

// ── Custo: lançar depois, ou concluir ───────────────────────────────────────

/**
 * Dois gestos com o mesmo campo:
 *
 * - **concluir** (`POST /complete`): custo opcional — vazio conclui sem mexer
 *   no que já estava lançado. Antes do botão, a frase do que acontece com o
 *   calendário e com a avaria.
 * - **lançar** (`PATCH {cost_cents}`): a nota que chega depois. Vazio limpa.
 */
export function ModalDeCusto({ controle, modo }: { controle: Controle; modo: "concluir" | "lancar" }) {
  const router = useRouter();
  const [ordem, setOrdem] = React.useState<OrdemDeManutencao | null>(null);
  const [aberto, setAberto] = React.useState(false);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const form = useForm<CustoFormulario>({ resolver: zodResolver(CustoFormulario), defaultValues: { cost: "" } });
  const { errors, isSubmitting } = form.formState;

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(alvo: OrdemDeManutencao) {
        form.reset({ cost: custoParaCampo(alvo.cost_cents) });
        setOrdem(alvo);
        setErroGeral(null);
        setAberto(true);
      },
    }),
    [form],
  );

  const concluir = modo === "concluir";
  const formulario = concluir ? "form-conclusao-ordem" : "form-custo-ordem";

  async function enviar(v: CustoFormulario) {
    if (!ordem) return;
    setErroGeral(null);
    const r = concluir ? await concluirOrdem(ordem.id, v) : await lancarCusto(ordem.id, v);
    if (r.ok) {
      setAberto(false);
      if (concluir) {
        notificarSucesso(
          "Ordem concluída",
          r.data.block ? `Calendário: ${periodoPorExtenso(r.data.block)}.` : undefined,
        );
      } else {
        notificarSucesso(typeof r.data.cost_cents === "number" ? "Custo lançado" : "Custo apagado");
      }
      return;
    }
    const { geral } = aplicar<CustoFormulario>(r, concluir ? "transicao" : "custo", form.setError, ["cost"]);
    setErroGeral(geral);
    if (pedeRecarga(r)) router.refresh();
  }

  const avariaPendente = ordem?.issue && !ordem.issue.resolution;

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title={concluir ? "Concluir a ordem?" : "Custo do serviço"}
      description={ordem ? `${ordem.unit_code} — ${ordem.title}` : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={isSubmitting}>
            Voltar
          </Button>
          <Button type="submit" form={formulario} disabled={isSubmitting} className="max-md:h-11 max-md:flex-1">
            {isSubmitting ? (
              <Loader2 className="animate-spin" aria-hidden="true" />
            ) : concluir ? (
              <CheckCircle2 aria-hidden="true" />
            ) : (
              <Receipt aria-hidden="true" />
            )}
            {concluir ? "Concluir" : "Salvar custo"}
          </Button>
        </>
      }
    >
      <AvisoGeral mensagem={erroGeral} />
      <form id={formulario} noValidate onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        {concluir ? (
          <div className="flex flex-col gap-2 text-sm text-muted-foreground">
            <p>{avisoDoEncerramento(ordem?.block)}</p>
            {avariaPendente ? <p>A avaria de origem, ainda pendente, fica marcada como consertada.</p> : null}
            <p>Concluída, a ordem não reabre — retrabalho é uma ordem nova. O custo ainda pode ser lançado depois.</p>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">
            A nota do serviço costuma chegar dias depois. Este valor entra na margem da estadia.
          </p>
        )}
        <Campo
          id={`${formulario}-custo`}
          label={concluir ? "Custo (R$, opcional)" : "Custo (R$)"}
          erro={errors.cost?.message}
          hint={concluir ? "Vazio conclui sem mexer no custo." : "Vazio apaga o custo lançado."}
        >
          {(p) => (
            <Input {...p} {...form.register("cost")} inputMode="decimal" placeholder="350,00" className={cn("tabular-nums sm:w-48")} />
          )}
        </Campo>
      </form>
    </ModalShell>
  );
}

// ── Bloqueio (PUT /{id}/block) ──────────────────────────────────────────────

/**
 * Bloquear a unidade da ordem, ou estender/encurtar o bloqueio. O que
 * acontece (criar linha nova, alterar a mesma, só o fim) é decisão de
 * `maintenance.Replan`; a tela manda o período pedido e mostra a recusa no
 * campo. A frase sobre o bloqueio em curso só **cita** a regra.
 */
export function ModalDoBloqueio({ controle }: { controle: Controle }) {
  const router = useRouter();
  const [ordem, setOrdem] = React.useState<OrdemDeManutencao | null>(null);
  const [aberto, setAberto] = React.useState(false);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const [erroPeriodo, setErroPeriodo] = React.useState<string | null>(null);
  const form = useForm<PeriodoFormulario>({
    resolver: zodResolver(PeriodoFormulario),
    defaultValues: { block_from: "", block_to: "" },
  });
  const { errors, isSubmitting } = form.formState;

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(alvo: OrdemDeManutencao) {
        const vivo = alvo.block && (alvo.block.phase === "agendado" || alvo.block.phase === "em_curso") ? alvo.block : null;
        form.reset({ block_from: vivo?.from ?? "", block_to: vivo?.to ?? "" });
        setOrdem(alvo);
        setErroGeral(null);
        setErroPeriodo(null);
        setAberto(true);
      },
    }),
    [form],
  );

  const de = useWatch({ control: form.control, name: "block_from" });
  const ate = useWatch({ control: form.control, name: "block_to" });
  const emCurso = ordem?.block?.phase === "em_curso";
  const mudando = ordem?.block?.phase === "agendado" || emCurso;

  async function enviar(v: PeriodoFormulario) {
    if (!ordem) return;
    setErroGeral(null);
    setErroPeriodo(null);
    const r = await definirBloqueio(ordem.id, v);
    if (r.ok) {
      setAberto(false);
      notificarSucesso("Calendário atualizado", r.data.block ? periodoPorExtenso(r.data.block) : undefined);
      return;
    }
    const { geral, periodo } = aplicar<PeriodoFormulario>(r, "bloqueio", form.setError, ["block_from", "block_to"]);
    setErroGeral(geral);
    setErroPeriodo(periodo);
    if (pedeRecarga(r)) router.refresh();
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title={mudando ? "Mudar o período do bloqueio" : "Bloquear o calendário"}
      description={ordem ? `${ordem.unit_code} sai da venda nas noites escolhidas.` : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button type="submit" form="form-bloqueio-ordem" disabled={isSubmitting} className="max-md:h-11 max-md:flex-1">
            {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : <CalendarX2 aria-hidden="true" />}
            {mudando ? "Salvar período" : "Bloquear"}
          </Button>
        </>
      }
    >
      <AvisoGeral mensagem={erroGeral} />
      <form id="form-bloqueio-ordem" noValidate onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        <Nota>
          O bloqueio ocupa a unidade <strong>como uma reserva</strong>: enquanto existir, ninguém vende essas noites. Se
          cruzar uma reserva, o sistema recusa e nada muda.
          {emCurso
            ? " Se ele começou antes de hoje, o primeiro dia fica como está e só muda o dia em que a unidade volta à venda — noite que já passou não muda."
            : ""}
        </Nota>
        <CamposDoPeriodo
          prefixo="bloqueio-ordem"
          de={de}
          ate={ate}
          registrarDe={form.register("block_from", { onChange: () => setErroPeriodo(null) })}
          registrarAte={form.register("block_to", { onChange: () => setErroPeriodo(null) })}
          erroDe={errors.block_from?.message}
          erroAte={errors.block_to?.message}
          erroDoPeriodo={erroPeriodo}
        />
      </form>
    </ModalShell>
  );
}
