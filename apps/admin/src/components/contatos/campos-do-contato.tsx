"use client";

import { useFormState, useWatch, type SubmitHandler, type UseFormReturn } from "react-hook-form";

import { Nota } from "@/components/layout/tela";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import type { ContatoFormulario } from "@/lib/contatos/esquemas";
import {
  BASES_LEGAIS,
  EXPLICACAO_DA_BASE,
  ROTULO_DA_BASE,
  ROTULO_DO_DOCUMENTO,
  TIPOS_DE_DOCUMENTO,
  type Contato,
} from "@/lib/contatos/tipos";

/**
 * O formulário da ficha — **um só**, para quem cadastra pela agenda de contatos
 * e para a recepção que cadastra o acompanhante no check-in (rooming list).
 *
 * Saiu de dentro do `ModalDeContato` quando a segunda tela precisou dele: "uma
 * pessoa, um cadastro" vale também para o formulário. Dois formulários de
 * contato seriam duas validações, duas bases legais e dois lugares para
 * esquecer o aviso do opt-in — e a primeira divergência é a ficha que nasce
 * diferente dependendo da porta.
 *
 * Quem usa é dono do `useForm` (com `zodResolver(ContatoFormulario)`) e do
 * envio; aqui só se desenha e se liga cada campo ao erro dele.
 */

export function valoresDoContato(contato: Contato | null): ContatoFormulario {
  return {
    name: contato?.name ?? "",
    email: contato?.email ?? "",
    phone_e164: contato?.phone_e164 ?? "",
    doc_type: contato?.doc_type ?? "",
    doc_number: contato?.doc_number ?? "",
    birth_date: contato?.birth_date ?? "",
    city: contato?.city ?? "",
    state: contato?.state ?? "",
    notes: contato?.notes ?? "",
    lgpd_basis: contato?.lgpd_basis ?? "",
    marketing_opt_in: contato?.marketing_opt_in ?? false,
    // `consent_at` é `timestamptz` e o campo é `<input type="date">`: entra só a
    // parte da data. A hora não é informação que alguém digite — ela vem do
    // instante em que o aceite foi registrado, e reapresentá-la para edição
    // convidaria a inventar um horário.
    consent_at: contato?.consent_at ? contato.consent_at.slice(0, 10) : "",
  };
}

export function CamposDoContato({
  form,
  idDoForm,
  aoEnviar,
  prefixo = "contato",
}: {
  form: UseFormReturn<ContatoFormulario>;
  idDoForm: string;
  aoEnviar: SubmitHandler<ContatoFormulario>;
  /** Prefixo dos `id` dos campos — o rótulo aponta para eles, e dois
   *  formulários na mesma página não podem repetir id. */
  prefixo?: string;
}) {
  // `useFormState` assina os erros AQUI: ler `form.formState` num filho não
  // re-renderiza o filho quando o erro chega, e o campo ficaria sem a mensagem.
  const { errors } = useFormState({ control: form.control });
  // `useWatch` e não `form.watch()`: a assinatura pontual não força o
  // formulário inteiro a re-renderizar a cada tecla de qualquer campo.
  const optIn = useWatch({ control: form.control, name: "marketing_opt_in" });
  const tipoDeDocumento = useWatch({ control: form.control, name: "doc_type" });
  const base = useWatch({ control: form.control, name: "lgpd_basis" });

  const id = (campo: string) => `${prefixo}-${campo}`;

  return (
    <form id={idDoForm} onSubmit={form.handleSubmit(aoEnviar)} className="flex flex-col gap-4">
      <Campo id={id("name")} label="Nome" obrigatorio erro={errors.name?.message}>
        {(p) => <Input {...p} {...form.register("name")} autoComplete="name" />}
      </Campo>

      <div className="grid gap-4 sm:grid-cols-2">
        <Campo
          id={id("phone")}
          label="Telefone"
          erro={errors.phone_e164?.message}
          hint="Com o código do país (DDI): +5585999990000. Sem ele, a mesma pessoa pode acabar com dois cadastros."
        >
          {(p) => (
            <Input
              {...p}
              {...form.register("phone_e164")}
              inputMode="tel"
              placeholder="+5585999990000"
              className="font-mono"
            />
          )}
        </Campo>

        <Campo id={id("email")} label="E-mail" erro={errors.email?.message}>
          {(p) => <Input {...p} {...form.register("email")} type="email" autoComplete="email" />}
        </Campo>
      </div>

      <div className="grid gap-4 sm:grid-cols-[10rem_1fr]">
        <Campo id={id("doc-type")} label="Documento" erro={errors.doc_type?.message}>
          {(p) => (
            <Select {...p} {...form.register("doc_type")}>
              <option value="">—</option>
              {TIPOS_DE_DOCUMENTO.map((tipo) => (
                <option key={tipo} value={tipo}>
                  {ROTULO_DO_DOCUMENTO[tipo]}
                </option>
              ))}
            </Select>
          )}
        </Campo>

        <Campo
          id={id("doc-number")}
          label="Número"
          erro={errors.doc_number?.message}
          hint={
            tipoDeDocumento === "passaporte"
              ? "Passaporte é aceito como digitado, porque cada país tem o seu formato."
              : "CPF e CNPJ são conferidos automaticamente e guardados só com os números."
          }
        >
          {(p) => (
            <Input
              {...p}
              {...form.register("doc_number")}
              inputMode={tipoDeDocumento === "passaporte" ? "text" : "numeric"}
              // Documento não entra no histórico de preenchimento do navegador:
              // o balcão é um computador (ou um celular) de muita gente.
              autoComplete="off"
              className="font-mono"
            />
          )}
        </Campo>
      </div>

      <div className="grid gap-4 sm:grid-cols-3">
        <Campo id={id("birth")} label="Nascimento" erro={errors.birth_date?.message}>
          {(p) => <Input {...p} {...form.register("birth_date")} type="date" />}
        </Campo>
        <Campo id={id("city")} label="Cidade" erro={errors.city?.message}>
          {(p) => <Input {...p} {...form.register("city")} />}
        </Campo>
        <Campo id={id("state")} label="UF" erro={errors.state?.message}>
          {(p) => <Input {...p} {...form.register("state")} maxLength={2} className="uppercase" />}
        </Campo>
      </div>

      <Campo id={id("notes")} label="Observações" erro={errors.notes?.message}>
        {(p) => <Textarea {...p} {...form.register("notes")} rows={3} />}
      </Campo>

      {/* ── LGPD ─────────────────────────────────────────────────────────
          Dois eixos que a tela mantém separados porque a lei os separa:
          `lgpd_basis` sustenta GUARDAR a ficha; `marketing_opt_in` autoriza
          MANDAR oferta. Executar a reserva de quem nunca aceitou propaganda é
          legítimo; mandar promoção para essa mesma pessoa não é. */}
      <fieldset className="rounded-xl border border-border/60 bg-muted/20 p-4">
        <legend className="px-1.5 text-xs font-medium uppercase tracking-wider text-muted-foreground">
          Base legal e consentimento
        </legend>

        <div className="flex flex-col gap-4">
          <Campo
            id={id("lgpd")}
            label="Por que podemos guardar esta ficha"
            erro={errors.lgpd_basis?.message}
            hint={base && base !== "" ? EXPLICACAO_DA_BASE[base as keyof typeof EXPLICACAO_DA_BASE] : undefined}
          >
            {(p) => (
              <Select {...p} {...form.register("lgpd_basis")}>
                <option value="">—</option>
                {BASES_LEGAIS.map((valor) => (
                  <option key={valor} value={valor}>
                    {ROTULO_DA_BASE[valor]}
                  </option>
                ))}
              </Select>
            )}
          </Campo>

          <CheckboxCampo
            id={id("optin")}
            label="Aceita receber ofertas e novidades"
            hint="É diferente da base legal: guardar o cadastro de quem se hospedou é permitido; mandar promoção exige este aceite."
            {...form.register("marketing_opt_in")}
          />

          {optIn ? (
            <Campo
              id={id("consent")}
              label="Data do aceite"
              obrigatorio
              erro={errors.consent_at?.message}
              hint="Informe quando a pessoa aceitou. Sem a data, o aceite não é registrado."
            >
              {(p) => <Input {...p} {...form.register("consent_at")} type="date" />}
            </Campo>
          ) : (
            <Nota>
              Desmarcar o aceite <strong>apaga a data</strong> junto, para nenhum relatório dizer que a
              pessoa ainda aceita receber ofertas.
            </Nota>
          )}
        </div>
      </fieldset>
    </form>
  );
}
