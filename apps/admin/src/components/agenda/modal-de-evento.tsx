"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useForm, useWatch, type Resolver } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { CalendarPlus, Loader2, Lock, Save } from "lucide-react";

import { notificarAvisosDeHorario, notificarSucesso } from "@/components/agenda/avisos";
import { BuscaComEscolha } from "@/components/agenda/busca-com-escolha";
import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import type { Escopo } from "@/lib/api/types";
import { buscarContatos, buscarReservas, marcarEvento, salvarEvento } from "@/lib/agenda/acoes";
import { esquemaDoEvento, eventoVazio, valoresDoEvento, type EventoFormulario, type ModoDoEvento } from "@/lib/agenda/esquemas";
import { falhaNoFormulario, pedeRecarga, type CampoDoFormulario } from "@/lib/agenda/mensagens";
import { camposDoEvento, type CampoDoEvento } from "@/lib/agenda/permissoes";
import { EXEMPLO_DE_TITULO, ROTULO_DA_MARCACAO, ROTULO_DO_TIPO, diaEMes } from "@/lib/agenda/rotulos";
import {
  TIPOS_DE_EVENTO,
  TIPOS_MANUAIS,
  type ContatoDaAgenda,
  type EventoDaAgenda,
  type PessoaDaAgenda,
  type ReservaDaAgenda,
  type UnidadeDaAgenda,
} from "@/lib/agenda/tipos";

/** Com o que o formulário abre. */
export type AlvoDoEvento =
  | { modo: "criar"; data: string; hora?: string }
  | { modo: "editar"; evento: EventoDaAgenda };

const TODOS: ReadonlySet<CampoDoEvento> = new Set<CampoDoEvento>([
  "type",
  "title",
  "all_day",
  "quando",
  "unit_id",
  "reservation_id",
  "contact_id",
  "assignee_id",
  "notes",
]);

/**
 * Marcar e editar um evento da agenda — **pensado para o celular**: selects
 * nativos (a roleta do sistema), campos de 44px, data e hora separados
 * (`datetime-local` é proibido, `docs/ui.md` §10) e nada que dependa de hover.
 *
 * ## O que este formulário deixa mexer sai do `editable`
 *
 * Na criação, tudo. Na edição, os campos abertos são os de `camposDoEvento`
 * para o `editable` que a API mandou — `tudo`, `segue_a_reserva` (horário,
 * responsável e anotação) ou `so_notas`. O resto aparece **travado**, com o
 * valor à vista: o check-in de uma reserva mostra a unidade e o hóspede, mas
 * eles são da reserva.
 *
 * ## As três respostas que não são "salvou"
 *
 * - `201`/`200` com `meta.warnings` — **salvou**, e o aviso de horário
 *   aparece depois (fora do funcionamento, num horário bloqueado, o
 *   responsável sem folga). Não impede nada.
 * - `409 UNIT_NOT_AVAILABLE` — nada foi gravado; cada ocupação de
 *   `details.conflicts` aparece **junto dos campos de horário**, que é onde a
 *   pessoa está olhando quando vai trocar o horário.
 * - `403` com `assignee_not_actor` — o perfil só marca para si: o campo
 *   responsável passa a vir fixo no próprio usuário, com a frase embaixo.
 */
export function ModalDeEvento({
  controle,
  unidades,
  pessoas,
  eu,
  escopoAoCriar,
  escopoAoEditar,
  passoEmMinutos,
  buscas,
}: {
  controle: React.RefObject<ControleDeModal<AlvoDoEvento> | null>;
  /** `null`: o perfil não alcança nenhuma rota que liste unidades. */
  unidades: readonly UnidadeDaAgenda[] | null;
  /** Quem mais pode ser responsável (além do próprio usuário). */
  pessoas: readonly PessoaDaAgenda[];
  eu: PessoaDaAgenda;
  escopoAoCriar: Escopo | null;
  escopoAoEditar: Escopo | null;
  /** `slot_minutes` — a duração quando o término fica vazio. */
  passoEmMinutos: number;
  buscas: { reservas: boolean; contatos: boolean };
}) {
  const router = useRouter();
  const [aberto, setAberto] = React.useState(false);
  const [modo, setModo] = React.useState<ModoDoEvento>("criar");
  const [evento, setEvento] = React.useState<EventoDaAgenda | null>(null);
  const [inicial, setInicial] = React.useState<EventoFormulario>(eventoVazio());
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const [erroQuando, setErroQuando] = React.useState<{ mensagem: string; conflitos: string[] } | null>(null);
  const [forcarProprio, setForcarProprio] = React.useState(false);
  const [outroDia, setOutroDia] = React.useState(false);
  const [reserva, setReserva] = React.useState<string | null>(null);
  const [contato, setContato] = React.useState<string | null>(null);

  // O schema depende do modo (o título de um check-in não é conferido: não vai
  // no corpo). O resolver lê o modo na hora de validar, não na hora de montar.
  const modoRef = React.useRef<ModoDoEvento>("criar");
  const resolver = React.useCallback<Resolver<EventoFormulario>>(
    (valores, contexto, opcoes) => zodResolver(esquemaDoEvento(modoRef.current))(valores, contexto, opcoes),
    [],
  );
  const form = useForm<EventoFormulario>({ resolver, defaultValues: eventoVazio() });
  const { errors, isSubmitting } = form.formState;

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(alvo: AlvoDoEvento) {
        if (alvo.modo === "criar") {
          const valores = eventoVazio({
            data_inicio: alvo.data,
            hora_inicio: alvo.hora ?? "",
            assignee_id: escopoAoCriar === "own" ? eu.id : "",
          });
          modoRef.current = "criar";
          setModo("criar");
          setEvento(null);
          setInicial(valores);
          form.reset(valores);
          setReserva(null);
          setContato(null);
          setOutroDia(false);
        } else {
          const e = alvo.evento;
          const m = e.editable === "nada" ? "so_notas" : e.editable;
          const valores = valoresDoEvento(e);
          modoRef.current = m;
          setModo(m);
          setEvento(e);
          setInicial(valores);
          form.reset(valores);
          setReserva(e.reservation_id ? (e.reservation_code ?? "Reserva ligada") : null);
          setContato(e.contact_id ? (e.contact_name ?? "Pessoa ligada") : null);
          setOutroDia(valores.data_fim !== "" && !valores.all_day);
        }
        setErroGeral(null);
        setErroQuando(null);
        setForcarProprio(false);
        setAberto(true);
      },
    }),
    [form, escopoAoCriar, eu.id],
  );

  const abertos = modo === "criar" ? TODOS : camposDoEvento(modo);
  const pode = (campo: CampoDoEvento) => abertos.has(campo);
  const escopo = modo === "criar" ? escopoAoCriar : escopoAoEditar;
  const responsavelFixo = escopo === "own" || forcarProprio;

  const tipo = useWatch({ control: form.control, name: "type" });
  const diaInteiro = useWatch({ control: form.control, name: "all_day" });
  const reservationId = useWatch({ control: form.control, name: "reservation_id" });
  const unitId = useWatch({ control: form.control, name: "unit_id" });

  async function enviar(v: EventoFormulario) {
    setErroGeral(null);
    setErroQuando(null);
    const r = modo === "criar" || !evento ? await marcarEvento(v) : await salvarEvento(evento.id, modo, v);

    if (r.ok) {
      setAberto(false);
      const titulo = modo === "criar" ? ROTULO_DA_MARCACAO[r.data.evento.type] : "Evento atualizado";
      if (r.data.avisos.length > 0) {
        notificarAvisosDeHorario(`${titulo} — com aviso de horário`, r.data.avisos);
      } else {
        notificarSucesso(titulo, r.data.evento.title);
      }
      return;
    }

    const leitura = falhaNoFormulario(r, modo === "criar" ? "criacao" : "edicao", v.all_day);
    for (const [campo, mensagem] of Object.entries(leitura.campos) as [CampoDoFormulario, string][]) {
      if (campo === "reason") continue;
      form.setError(campo as keyof EventoFormulario, { type: "server", message: mensagem });
    }
    setErroQuando(leitura.quando);
    setErroGeral(leitura.geral);
    if (leitura.fixarResponsavel) {
      setForcarProprio(true);
      form.setValue("assignee_id", eu.id);
    }
    if (modo !== "criar" && pedeRecarga(r)) router.refresh();
  }

  // ── Opções ────────────────────────────────────────────────────────────────

  const opcoesDeUnidade = React.useMemo(() => {
    const lista = (unidades ?? []).map((u) => ({ id: u.id, rotulo: `${u.code} — ${u.name}` }));
    // O que já está gravado continua escolhível mesmo fora da lista (unidade
    // desativada, perfil que não lista unidades): sumir com ele seria limpá-lo
    // calado no próximo `PUT`.
    if (inicial.unit_id && !lista.some((u) => u.id === inicial.unit_id)) {
      const doEvento = evento?.units.find((u) => u.unit_id === inicial.unit_id);
      lista.unshift({ id: inicial.unit_id, rotulo: doEvento?.unit_code ?? "Unidade atual" });
    }
    return lista;
  }, [unidades, inicial.unit_id, evento]);

  const opcoesDeResponsavel = React.useMemo(() => {
    const vistos = new Map<string, string>([[eu.id, `Você — ${eu.name}`]]);
    for (const p of pessoas) if (!vistos.has(p.id)) vistos.set(p.id, p.name);
    if (evento?.assignee_id && !vistos.has(evento.assignee_id)) vistos.set(evento.assignee_id, evento.assignee_name ?? "Responsável atual");
    return [...vistos.entries()].map(([id, rotulo]) => ({ id, rotulo }));
  }, [eu, pessoas, evento]);

  const travado = modo === "segue_a_reserva" || modo === "so_notas";
  const titulo =
    modo === "criar" ? "Marcar na agenda" : modo === "so_notas" ? "Anotação do evento" : "Editar evento";

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title={titulo}
      description={
        modo === "criar"
          ? "Visita, evento ou limpeza avulsa. Check-in, check-out e a limpeza da saída nascem da reserva."
          : evento
            ? `${evento.title} · ${diaEMes(evento.date)}`
            : undefined
      }
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button type="submit" form="form-evento-da-agenda" disabled={isSubmitting} className="max-md:h-11 max-md:flex-1">
            {isSubmitting ? (
              <Loader2 className="animate-spin" aria-hidden="true" />
            ) : modo === "criar" ? (
              <CalendarPlus aria-hidden="true" />
            ) : (
              <Save aria-hidden="true" />
            )}
            {modo === "criar" ? "Marcar" : "Salvar"}
          </Button>
        </>
      }
    >
      {erroGeral ? (
        <p role="alert" className="mb-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erroGeral}
        </p>
      ) : null}

      {travado ? (
        <p className="mb-4 flex items-start gap-1.5 rounded-lg bg-muted/40 px-3 py-2 text-xs text-muted-foreground" data-editavel={modo}>
          <Lock aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
          {modo === "segue_a_reserva"
            ? "Este evento segue a reserva: tipo, unidade e hóspede vêm dela. Daqui mudam o horário, o responsável e a anotação."
            : "Este evento já foi concluído: só a anotação ainda muda."}
        </p>
      ) : null}

      <form id="form-evento-da-agenda" noValidate onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        <div className="grid gap-4 sm:grid-cols-[10rem_1fr]">
          <Campo id="evento-tipo" label="O que é" obrigatorio={pode("type")} erro={errors.type?.message}>
            {(p) =>
              pode("type") ? (
                <Select {...p} {...form.register("type")}>
                  {TIPOS_MANUAIS.map((t) => (
                    <option key={t} value={t}>
                      {ROTULO_DO_TIPO[t]}
                    </option>
                  ))}
                </Select>
              ) : (
                <Select {...p} value={inicial.type} disabled onChange={() => undefined}>
                  {TIPOS_DE_EVENTO.map((t) => (
                    <option key={t} value={t}>
                      {ROTULO_DO_TIPO[t]}
                    </option>
                  ))}
                </Select>
              )
            }
          </Campo>
          <Campo id="evento-titulo" label="Título" obrigatorio={pode("title")} erro={errors.title?.message}>
            {(p) =>
              pode("title") ? (
                <Input
                  {...p}
                  {...form.register("title")}
                  maxLength={200}
                  autoComplete="off"
                  placeholder={EXEMPLO_DE_TITULO[(TIPOS_MANUAIS as readonly string[]).includes(tipo) ? (tipo as "visita") : "visita"]}
                />
              ) : (
                <Input {...p} value={inicial.title} disabled readOnly />
              )
            }
          </Campo>
        </div>

        <fieldset className="flex flex-col gap-3 rounded-xl border border-border/60 bg-muted/20 p-3.5" aria-label="Quando">
          <legend className="sr-only">Quando</legend>
          {pode("all_day") ? (
            <CheckboxCampo id="evento-dia-inteiro" label="Dia inteiro" {...form.register("all_day", { onChange: () => setErroQuando(null) })} />
          ) : inicial.all_day ? (
            <p className="text-xs text-muted-foreground">Dia inteiro</p>
          ) : null}

          <div className="grid gap-3 grid-cols-2">
            <Campo id="evento-data-inicio" label={diaInteiro ? "Primeiro dia" : "Dia"} obrigatorio={pode("quando")} erro={errors.data_inicio?.message}>
              {(p) =>
                pode("quando") ? (
                  <Input {...p} type="date" {...form.register("data_inicio", { onChange: () => setErroQuando(null) })} />
                ) : (
                  <Input {...p} type="date" value={inicial.data_inicio} disabled readOnly />
                )
              }
            </Campo>
            {diaInteiro ? (
              <Campo id="evento-data-fim" label="Último dia" erro={errors.data_fim?.message} hint="Vazio: só o primeiro dia.">
                {(p) =>
                  pode("quando") ? (
                    <Input {...p} type="date" {...form.register("data_fim", { onChange: () => setErroQuando(null) })} />
                  ) : (
                    <Input {...p} type="date" value={inicial.data_fim} disabled readOnly />
                  )
                }
              </Campo>
            ) : (
              <Campo id="evento-hora-inicio" label="Começa às" obrigatorio={pode("quando")} erro={errors.hora_inicio?.message}>
                {(p) =>
                  pode("quando") ? (
                    <Input {...p} type="time" step={300} {...form.register("hora_inicio", { onChange: () => setErroQuando(null) })} />
                  ) : (
                    <Input {...p} type="time" value={inicial.hora_inicio} disabled readOnly />
                  )
                }
              </Campo>
            )}
          </div>

          {!diaInteiro ? (
            <div className="grid gap-3 grid-cols-2">
              <Campo
                id="evento-hora-fim"
                label="Termina às"
                obrigatorio={pode("quando") && modo !== "criar"}
                erro={errors.hora_fim?.message}
                hint={modo === "criar" ? `Vazio: ${passoEmMinutos} min, um passo da grade.` : undefined}
              >
                {(p) =>
                  pode("quando") ? (
                    <Input {...p} type="time" step={300} {...form.register("hora_fim", { onChange: () => setErroQuando(null) })} />
                  ) : (
                    <Input {...p} type="time" value={inicial.hora_fim} disabled readOnly />
                  )
                }
              </Campo>
              {outroDia ? (
                <Campo id="evento-data-fim" label="Termina no dia" erro={errors.data_fim?.message} hint="Vazio: no mesmo dia.">
                  {(p) =>
                    pode("quando") ? (
                      <Input {...p} type="date" {...form.register("data_fim", { onChange: () => setErroQuando(null) })} />
                    ) : (
                      <Input {...p} type="date" value={inicial.data_fim} disabled readOnly />
                    )
                  }
                </Campo>
              ) : pode("quando") ? (
                <Button variant="link" size="sm" className="self-end justify-self-start px-0 pb-3" onClick={() => setOutroDia(true)}>
                  Termina em outro dia
                </Button>
              ) : null}
            </div>
          ) : null}

          {erroQuando ? (
            <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
              <p>{erroQuando.mensagem}</p>
              {erroQuando.conflitos.length > 0 ? (
                <ul className="mt-1.5 flex list-disc flex-col gap-0.5 pl-5 text-xs" aria-label="O que ocupa a unidade">
                  {erroQuando.conflitos.map((c) => (
                    <li key={c}>{c}</li>
                  ))}
                </ul>
              ) : null}
            </div>
          ) : null}
        </fieldset>

        <Campo
          id="evento-unidade"
          label="Unidade"
          erro={errors.unit_id?.message}
          hint={
            !pode("unit_id")
              ? evento && evento.units.length > 0
                ? `Da reserva: ${evento.units.map((u) => u.unit_code).join(", ")}.`
                : undefined
              : unidades === null
                ? "Seu perfil não lista as unidades: o evento fica sem unidade, ou ligado a uma reserva."
                : reservationId && !unitId
                  ? "Sem unidade, o evento ocupa as unidades da reserva ligada."
                  : "Opcional. Se a unidade estiver ocupada nesse horário, nada é marcado e a tela mostra o que a ocupa."
          }
        >
          {(p) =>
            pode("unit_id") && (unidades !== null || opcoesDeUnidade.length > 0) ? (
              <Select {...p} {...form.register("unit_id", { onChange: () => setErroQuando(null) })}>
                <option value="">{reservationId ? "As da reserva" : "Nenhuma — não ocupa unidade"}</option>
                {opcoesDeUnidade.map((u) => (
                  <option key={u.id} value={u.id}>
                    {u.rotulo}
                  </option>
                ))}
              </Select>
            ) : (
              <Input
                {...p}
                value={evento && evento.units.length > 0 ? evento.units.map((u) => u.unit_code).join(", ") : "Nenhuma"}
                disabled
                readOnly
              />
            )
          }
        </Campo>

        {buscas.reservas || reserva !== null ? (
          <BuscaComEscolha<ReservaDaAgenda>
            id="evento-reserva"
            rotulo="Reserva ligada"
            placeholder="WH-2026-0007 ou o nome do hóspede"
            escolhido={reserva}
            desabilitado={!pode("reservation_id") || !buscas.reservas}
            descrever={(r) => `${r.code}${r.contact_name ? ` · ${r.contact_name}` : ""} · ${diaEMes(r.check_in)} a ${diaEMes(r.check_out)}`}
            buscar={buscarReservas}
            aoEscolher={(r) => {
              form.setValue("reservation_id", r?.id ?? "", { shouldDirty: true });
              setReserva(r ? `${r.code}${r.contact_name ? ` · ${r.contact_name}` : ""}` : null);
              setErroQuando(null);
            }}
            erro={errors.reservation_id?.message}
            dica={
              pode("reservation_id")
                ? "Opcional. A faxina do meio da estadia e o evento do grupo que alugou a casa não colidem com a hospedagem da própria reserva."
                : undefined
            }
          />
        ) : null}

        {buscas.contatos || contato !== null ? (
          <BuscaComEscolha<ContatoDaAgenda>
            id="evento-contato"
            rotulo={inicial.type === "visita" || tipo === "visita" ? "Quem visita" : "Com quem"}
            placeholder="Nome da pessoa"
            escolhido={contato}
            desabilitado={!pode("contact_id") || !buscas.contatos}
            descrever={(c) => c.name}
            buscar={buscarContatos}
            aoEscolher={(c) => {
              form.setValue("contact_id", c?.id ?? "", { shouldDirty: true });
              setContato(c?.name ?? null);
            }}
            erro={errors.contact_id?.message}
          />
        ) : null}

        <Campo
          id="evento-responsavel"
          label="Responsável"
          erro={errors.assignee_id?.message}
          hint={responsavelFixo && pode("assignee_id") ? "No seu perfil, o responsável é sempre você." : undefined}
        >
          {(p) =>
            pode("assignee_id") && !responsavelFixo ? (
              <Select {...p} {...form.register("assignee_id")}>
                <option value="">Ninguém ainda</option>
                {opcoesDeResponsavel.map((o) => (
                  <option key={o.id} value={o.id}>
                    {o.rotulo}
                  </option>
                ))}
              </Select>
            ) : (
              <Select {...p} value={responsavelFixo && pode("assignee_id") ? eu.id : inicial.assignee_id} disabled onChange={() => undefined}>
                <option value="">Ninguém ainda</option>
                {opcoesDeResponsavel.map((o) => (
                  <option key={o.id} value={o.id}>
                    {o.rotulo}
                  </option>
                ))}
              </Select>
            )
          }
        </Campo>

        <Campo id="evento-notas" label="Anotação" erro={errors.notes?.message}>
          {(p) =>
            pode("notes") ? (
              <Textarea {...p} rows={3} maxLength={2000} {...form.register("notes")} placeholder="Chave na portaria, levar toalhas extras…" />
            ) : (
              <Textarea {...p} rows={3} value={inicial.notes} disabled readOnly />
            )
          }
        </Campo>
      </form>
    </ModalShell>
  );
}
