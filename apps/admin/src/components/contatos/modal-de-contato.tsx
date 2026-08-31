"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { ArrowRight, Loader2, UserRoundCheck } from "lucide-react";

import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Button, buttonVariants } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { aplicarFalhaDeContato } from "@/lib/contatos/formulario";
import { ContatoFormulario } from "@/lib/contatos/esquemas";
import { formatarTelefone } from "@/lib/contatos/telefone";
import {
  BASES_LEGAIS,
  EXPLICACAO_DA_BASE,
  ROTULO_DA_BASE,
  ROTULO_DO_DOCUMENTO,
  TIPOS_DE_DOCUMENTO,
  type Contato,
} from "@/lib/contatos/tipos";
import { cn } from "@/lib/utils";

import { salvarContato, type Duplicata } from "@/app/(app)/app/contatos/acoes";
import { notificarSucesso } from "@/components/contatos/avisos";

const ID_DO_FORM = "form-contato";

function valoresDoContato(contato: Contato | null): ContatoFormulario {
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

/**
 * O momento mais importante do cadastro: **o telefone já é de alguém**.
 *
 * Quem digita um número que já existe não errou — ele quer chegar naquela
 * pessoa e não sabia que ela estava lá. A tela antiga do sistema (e a maioria
 * das telas de cadastro que existem) responde "409" e para. Aqui a recusa vira
 * uma porta: o nome de quem já está cadastrado e um botão que abre a ficha.
 *
 * Fica **dentro do modal**, e não num toast, porque toast some. O operador
 * precisa poder ler, decidir e clicar — e continuar vendo o que digitou, caso
 * decida que é outra pessoa mesmo e vá corrigir o número.
 */
function CaminhoDaDuplicata({
  duplicata,
  aoCorrigir,
}: {
  duplicata: Duplicata;
  aoCorrigir: () => void;
}) {
  const porTelefone = duplicata.campo === "phone_e164";
  const chave = porTelefone ? "telefone" : duplicata.campo === "doc_number" ? "documento" : "contato";
  const nome = duplicata.contato?.name;
  const href = duplicata.contactId ? `/app/contatos/${duplicata.contactId}` : null;
  const anonimizado = Boolean(duplicata.contato?.anonymized_at);

  return (
    <div
      role="alert"
      className="mb-4 rounded-xl border border-primary/30 bg-accent/50 px-4 py-3.5"
    >
      <div className="flex items-start gap-3">
        <UserRoundCheck className="mt-0.5 size-5 shrink-0 text-primary" aria-hidden="true" />
        <div className="min-w-0 flex-1">
          <h3 className="font-display text-base leading-tight">
            {nome ? `${nome} já está cadastrada` : `Este ${chave} já está cadastrado`}
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            {porTelefone && duplicata.contato?.phone_e164 ? (
              <>
                O telefone{" "}
                <span className="font-mono text-foreground">
                  {formatarTelefone(duplicata.contato.phone_e164)}
                </span>{" "}
                é dessa ficha. Uma pessoa, um registro — abra a que já existe em vez de criar a
                segunda.
              </>
            ) : (
              <>
                Uma pessoa, um registro: o cadastro guarda um contato por ser humano, e é isso que
                deixa o WhatsApp reconhecer quem já escreveu.
              </>
            )}
          </p>

          {anonimizado ? (
            <p className="mt-2 text-sm text-muted-foreground">
              A ficha existente está <strong>anonimizada</strong> — ela sustenta reservas antigas e
              não recebe dado pessoal de volta. Este cadastro precisa de outro telefone, ou a
              anonimização precisa liberar o número.
            </p>
          ) : null}

          <div className="mt-3 flex flex-wrap items-center gap-2">
            {href ? (
              <Link href={href} className={cn(buttonVariants({ size: "sm" }))}>
                {nome ? `Abrir a ficha de ${nome.split(" ")[0]}` : "Abrir o contato existente"}
                <ArrowRight aria-hidden="true" />
              </Link>
            ) : null}
            <Button size="sm" variant="ghost" onClick={aoCorrigir}>
              Não é essa pessoa — corrigir o {chave}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

export function ModalDeContato({
  controle,
}: {
  controle: React.RefObject<ControleDeModal<Contato | null> | null>;
}) {
  const router = useRouter();
  const [aberto, setAberto] = React.useState(false);
  const [contato, setContato] = React.useState<Contato | null>(null);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const [duplicata, setDuplicata] = React.useState<Duplicata | null>(null);

  const form = useForm<ContatoFormulario>({
    resolver: zodResolver(ContatoFormulario),
    defaultValues: valoresDoContato(null),
  });

  // O modal fica montado para não perder a animação de entrada; o estado é
  // reposto **no evento que abre**. Sem isto, reabrir em outro contato mostraria
  // os dados do anterior por um quadro — e quem salvasse rápido salvaria a
  // pessoa errada, porque o `id` do envio vem do mesmo estado atrasado.
  React.useImperativeHandle(
    controle,
    () => ({
      abrir(escolhido: Contato | null) {
        setContato(escolhido);
        form.reset(valoresDoContato(escolhido));
        setErroGeral(null);
        setDuplicata(null);
        setAberto(true);
      },
    }),
    [form],
  );

  const { errors, isSubmitting } = form.formState;
  // `useWatch` e não `form.watch()`: a assinatura pontual não força o modal
  // inteiro a re-renderizar a cada tecla de qualquer campo.
  const optIn = useWatch({ control: form.control, name: "marketing_opt_in" });
  const tipoDeDocumento = useWatch({ control: form.control, name: "doc_type" });
  const base = useWatch({ control: form.control, name: "lgpd_basis" });

  async function enviar(valores: ContatoFormulario) {
    setErroGeral(null);
    setDuplicata(null);

    const resultado = await salvarContato(contato?.id ?? null, valores);
    if (resultado.ok) {
      notificarSucesso(
        contato ? "Contato atualizado" : "Contato cadastrado",
        resultado.data.name,
      );
      setAberto(false);
      // `revalidatePath` já invalidou o cache do servidor; o `refresh` é o que
      // faz esta árvore buscar de novo sem recarregar a página inteira.
      router.refresh();
      return;
    }

    if ("duplicata" in resultado) {
      setDuplicata(resultado.duplicata);
      // Marca o campo também: o aviso explica, o campo mostra onde.
      const campo = resultado.duplicata.campo === "doc_number" ? "doc_number" : "phone_e164";
      form.setError(campo, { type: "server", message: "Já pertence a outro contato." });
      return;
    }

    setErroGeral(aplicarFalhaDeContato(resultado, form, { CONTACT_ANONYMIZED: "phone_e164" }));
  }

  function corrigirCampoDuplicado() {
    const campo = duplicata?.campo === "doc_number" ? "doc_number" : "phone_e164";
    setDuplicata(null);
    form.clearErrors(campo);
    form.setFocus(campo);
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title={contato ? `Editar ${contato.name}` : "Novo contato"}
      description="Uma pessoa, um registro: lead, hóspede e proprietário apontam todos para esta ficha."
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button type="submit" form={ID_DO_FORM} disabled={isSubmitting}>
            {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
            Salvar
          </Button>
        </>
      }
    >
      {duplicata ? <CaminhoDaDuplicata duplicata={duplicata} aoCorrigir={corrigirCampoDuplicado} /> : null}

      {erroGeral ? (
        <p role="alert" className="mb-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erroGeral}
        </p>
      ) : null}

      <form id={ID_DO_FORM} onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        <Campo id="contato-name" label="Nome" obrigatorio erro={errors.name?.message}>
          {(p) => <Input {...p} {...form.register("name")} autoComplete="name" />}
        </Campo>

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo
            id="contato-phone"
            label="Telefone"
            erro={errors.phone_e164?.message}
            hint="Com DDI e sem adivinhação: +5585999990000. O painel não completa o país por você — chutar o DDI cria dois cadastros da mesma pessoa."
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

          <Campo id="contato-email" label="E-mail" erro={errors.email?.message}>
            {(p) => <Input {...p} {...form.register("email")} type="email" autoComplete="email" />}
          </Campo>
        </div>

        <div className="grid gap-4 sm:grid-cols-[10rem_1fr]">
          <Campo id="contato-doc-type" label="Documento" erro={errors.doc_type?.message}>
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
            id="contato-doc-number"
            label="Número"
            erro={errors.doc_number?.message}
            hint={
              tipoDeDocumento === "passaporte"
                ? "Passaporte não tem dígito verificador — cada país tem o seu formato, e recusar o que não se sabe validar barraria hóspede estrangeiro."
                : "CPF e CNPJ são conferidos pelo dígito verificador e gravados só com dígitos."
            }
          >
            {(p) => <Input {...p} {...form.register("doc_number")} inputMode="numeric" className="font-mono" />}
          </Campo>
        </div>

        <div className="grid gap-4 sm:grid-cols-3">
          <Campo id="contato-birth" label="Nascimento" erro={errors.birth_date?.message}>
            {(p) => <Input {...p} {...form.register("birth_date")} type="date" />}
          </Campo>
          <Campo id="contato-city" label="Cidade" erro={errors.city?.message}>
            {(p) => <Input {...p} {...form.register("city")} />}
          </Campo>
          <Campo id="contato-state" label="UF" erro={errors.state?.message}>
            {(p) => <Input {...p} {...form.register("state")} maxLength={2} className="uppercase" />}
          </Campo>
        </div>

        <Campo id="contato-notes" label="Observações" erro={errors.notes?.message}>
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
              id="contato-lgpd"
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
              id="contato-optin"
              label="Aceita receber ofertas e novidades"
              hint="Eixo separado da base legal. Guardar o cadastro de quem se hospedou é legítimo; mandar promoção para ele exige este aceite."
              {...form.register("marketing_opt_in")}
            />

            {optIn ? (
              <Campo
                id="contato-consent"
                label="Data do aceite"
                obrigatorio
                erro={errors.consent_at?.message}
                hint="Consentimento sem data não é consentimento, é afirmação — a API recusa o opt-in sem ela."
              >
                {(p) => <Input {...p} {...form.register("consent_at")} type="date" />}
              </Campo>
            ) : (
              <Nota>
                Desligar o aceite <strong>limpa a data</strong> na mesma gravação: revogação não pede
                segunda chamada, e uma data de aceite sobrevivente faria o relatório dizer que a
                pessoa aceitou.
              </Nota>
            )}
          </div>
        </fieldset>
      </form>
    </ModalShell>
  );
}
