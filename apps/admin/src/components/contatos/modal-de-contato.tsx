"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { ArrowRight, Loader2, RotateCcw, UserRoundCheck } from "lucide-react";

import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Button, buttonVariants } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import type { FalhaDeContato } from "@/lib/contatos/api";
import { aplicarFalhaDeContato } from "@/lib/contatos/formulario";
import { ContatoFormulario } from "@/lib/contatos/esquemas";
import { formatarTelefone, limparTelefone } from "@/lib/contatos/telefone";
import {
  BASES_LEGAIS,
  EXPLICACAO_DA_BASE,
  ROTULO_DA_BASE,
  ROTULO_DO_DOCUMENTO,
  TIPOS_DE_DOCUMENTO,
  type Contato,
} from "@/lib/contatos/tipos";
import { cn } from "@/lib/utils";

import { lerFichaDoContato, salvarContato, type Duplicata } from "@/app/(app)/app/contatos/acoes";
import { AvisoDeErro, notificarSucesso } from "@/components/contatos/avisos";

const ID_DO_FORM = "form-contato";

/**
 * Com o que o modal abre — e **a linha da lista não é uma das opções**.
 *
 * - `null`: cadastro novo, formulário em branco.
 * - `{ ficha }`: a ficha cheia, já lida por `GET /contacts/{id}`. É o caso da
 *   tela da ficha, que a tem na mão e cuja leitura já deixou rastro.
 * - `{ buscar }`: só o `id` e o nome. É o caso da lista de contatos, que é
 *   mascarada: o modal abre na hora, busca a ficha e **só monta o formulário
 *   quando ela chega**.
 *
 * ## Por que a linha não serve (dívida D11)
 *
 * A coleção devolve documento, telefone e e-mail mascarados e não traz
 * `birth_date` nem `notes`. O salvamento é `PUT` com o formulário inteiro: com a
 * linha, ele responderia `422` em campos que o operador não tocou (e-mail com
 * `*`, telefone que não é E.164) — e, num contato só com nome e anotação, nada
 * recusaria e **a anotação seria gravada vazia**, em silêncio.
 *
 * `{ ficha: Contato }` exige `birth_date` e `notes`, que `ContatoNaLista` não
 * tem: passar a linha aqui é erro de compilação (`lib/contatos/tipos.test.ts`).
 */
export type AberturaDoContato = null | { ficha: Contato } | { buscar: { id: string; nome: string } };

/** Onde o modal está. O formulário só existe em `novo` e `editando`. */
type Estado =
  | { fase: "novo" }
  | { fase: "carregando"; id: string; nome: string }
  | { fase: "falhou"; id: string; nome: string; falha: FalhaDeContato }
  | { fase: "editando"; ficha: Contato };

/** Recusa que tentar de novo não resolve: a ficha sumiu, foi anonimizada ou o
 *  perfil não alcança. */
const FALHA_DEFINITIVA: readonly FalhaDeContato["code"][] = ["NOT_FOUND", "FORBIDDEN", "CONTACT_ANONYMIZED"];

function falhaLocal(code: FalhaDeContato["code"]): FalhaDeContato {
  return { ok: false, code, message: "", details: {} };
}

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
  telefoneEnviado,
  aoCorrigir,
}: {
  duplicata: Duplicata;
  /** O número que o operador acabou de digitar. É ele que a tela repete, e não
   *  o `phone_e164` de `duplicata.contato`: aquele é a linha da lista,
   *  mascarada (`+*********0000`). A colisão é por igualdade exata, então o
   *  número digitado **é** o da ficha existente. */
  telefoneEnviado: string | null;
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
            {porTelefone && telefoneEnviado ? (
              <>
                O telefone{" "}
                <span className="font-mono text-foreground">{formatarTelefone(telefoneEnviado)}</span>{" "}
                é dessa ficha. Uma pessoa, um cadastro — abra o que já existe em vez de criar outro.
              </>
            ) : (
              <>
                Uma pessoa, um cadastro: é isso que deixa o WhatsApp reconhecer quem já escreveu.
              </>
            )}
          </p>

          {anonimizado ? (
            <p className="mt-2 text-sm text-muted-foreground">
              A ficha existente está <strong>anonimizada</strong> (dados apagados a pedido da pessoa)
              e não pode receber dados de volta. Use outro telefone neste cadastro.
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

/**
 * O que aparece enquanto a ficha não chega: o **formato** do formulário, não um
 * spinner solto (docs/ui.md §8). Nenhum campo existe aqui — nem desabilitado,
 * nem vazio —, porque campo vazio à espera de dado é o que convida a digitar por
 * cima e salvar antes da hora.
 */
function EsqueletoDaFicha({ nome }: { nome: string }) {
  return (
    <div role="status" aria-live="polite" className="flex flex-col gap-4">
      <p className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" aria-hidden="true" />
        Abrindo a ficha de {nome}…
      </p>
      <div aria-hidden="true" className="flex flex-col gap-4">
        <div className="h-10 w-full animate-pulse rounded-xl bg-muted" />
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="h-10 animate-pulse rounded-xl bg-muted" />
          <div className="h-10 animate-pulse rounded-xl bg-muted" />
        </div>
        <div className="grid gap-4 sm:grid-cols-[10rem_1fr]">
          <div className="h-10 animate-pulse rounded-xl bg-muted" />
          <div className="h-10 animate-pulse rounded-xl bg-muted" />
        </div>
        <div className="h-20 w-full animate-pulse rounded-xl bg-muted/70" />
      </div>
      <Nota>
        Abrir a ficha para editar fica registrado: o sistema guarda quem olhou os dados pessoais e
        quando, como pede a LGPD.
      </Nota>
    </div>
  );
}

export function ModalDeContato({
  controle,
}: {
  controle: React.RefObject<ControleDeModal<AberturaDoContato> | null>;
}) {
  const router = useRouter();
  const [aberto, setAberto] = React.useState(false);
  const [estado, setEstado] = React.useState<Estado>({ fase: "novo" });
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const [duplicata, setDuplicata] = React.useState<{ duplicata: Duplicata; telefone: string | null } | null>(
    null,
  );
  // Número da abertura em curso. Resposta de ficha que chega depois de o modal
  // fechar, ou de ele reabrir em outra pessoa, é de outra abertura — e montaria
  // o formulário de um contato com o `id` de outro.
  const abertura = React.useRef(0);

  const form = useForm<ContatoFormulario>({
    resolver: zodResolver(ContatoFormulario),
    defaultValues: valoresDoContato(null),
  });

  const buscarFicha = React.useCallback(
    async (id: string, nome: string) => {
      const minha = ++abertura.current;
      setEstado({ fase: "carregando", id, nome });

      let resultado: Awaited<ReturnType<typeof lerFichaDoContato>>;
      try {
        resultado = await lerFichaDoContato(id);
      } catch {
        // A action não respondeu (rede, servidor do painel reiniciando). Não é
        // recusa da API, e "tentar de novo" é exatamente o gesto certo.
        resultado = falhaLocal("NETWORK_ERROR");
      }
      if (minha !== abertura.current) return;

      if (!resultado.ok) {
        setEstado({ fase: "falhou", id, nome, falha: resultado });
        return;
      }
      // A lista esconde "Editar" de ficha anonimizada, mas a lista pode ser de
      // minutos atrás. Formulário de ficha anonimizada só serviria para receber
      // a recusa da API depois de o operador digitar tudo.
      if (resultado.data.anonymized_at) {
        setEstado({ fase: "falhou", id, nome, falha: falhaLocal("CONTACT_ANONYMIZED") });
        return;
      }
      form.reset(valoresDoContato(resultado.data));
      setEstado({ fase: "editando", ficha: resultado.data });
    },
    [form],
  );

  // O modal fica montado para não perder a animação de entrada; o estado é
  // reposto **no evento que abre**. Sem isto, reabrir em outro contato mostraria
  // os dados do anterior por um quadro — e quem salvasse rápido salvaria a
  // pessoa errada, porque o `id` do envio vem do mesmo estado atrasado.
  React.useImperativeHandle(
    controle,
    () => ({
      abrir(alvo: AberturaDoContato) {
        abertura.current += 1;
        setErroGeral(null);
        setDuplicata(null);
        if (alvo === null) {
          form.reset(valoresDoContato(null));
          setEstado({ fase: "novo" });
        } else if ("ficha" in alvo) {
          form.reset(valoresDoContato(alvo.ficha));
          setEstado({ fase: "editando", ficha: alvo.ficha });
        } else {
          void buscarFicha(alvo.buscar.id, alvo.buscar.nome);
        }
        setAberto(true);
      },
    }),
    [form, buscarFicha],
  );

  function mudarAberto(valor: boolean) {
    // Fechar no meio da busca invalida a resposta que ainda vai chegar.
    if (!valor) abertura.current += 1;
    setAberto(valor);
  }

  const { errors, isSubmitting } = form.formState;
  // `useWatch` e não `form.watch()`: a assinatura pontual não força o modal
  // inteiro a re-renderizar a cada tecla de qualquer campo.
  const optIn = useWatch({ control: form.control, name: "marketing_opt_in" });
  const tipoDeDocumento = useWatch({ control: form.control, name: "doc_type" });
  const base = useWatch({ control: form.control, name: "lgpd_basis" });

  const comFormulario = estado.fase === "novo" || estado.fase === "editando";

  async function enviar(valores: ContatoFormulario) {
    // O formulário só existe com a ficha na mão (ou em branco, para criar). A
    // guarda é para nenhum caminho salvar sem ela — e, pior, cair no `POST` por
    // falta de `id` e criar uma segunda pessoa.
    if (estado.fase !== "novo" && estado.fase !== "editando") return;
    const id = estado.fase === "editando" ? estado.ficha.id : null;

    setErroGeral(null);
    setDuplicata(null);

    const resultado = await salvarContato(id, valores);
    if (resultado.ok) {
      notificarSucesso(id ? "Contato atualizado" : "Contato cadastrado", resultado.data.name);
      setAberto(false);
      // `revalidatePath` já invalidou o cache do servidor; o `refresh` é o que
      // faz esta árvore buscar de novo sem recarregar a página inteira.
      router.refresh();
      return;
    }

    if ("duplicata" in resultado) {
      setDuplicata({
        duplicata: resultado.duplicata,
        telefone: valores.phone_e164 === "" ? null : limparTelefone(valores.phone_e164),
      });
      // Marca o campo também: o aviso explica, o campo mostra onde.
      const campo = resultado.duplicata.campo === "doc_number" ? "doc_number" : "phone_e164";
      form.setError(campo, { type: "server", message: "Já pertence a outro contato." });
      return;
    }

    setErroGeral(aplicarFalhaDeContato(resultado, form, { CONTACT_ANONYMIZED: "phone_e164" }));
  }

  function corrigirCampoDuplicado() {
    const campo = duplicata?.duplicata.campo === "doc_number" ? "doc_number" : "phone_e164";
    setDuplicata(null);
    form.clearErrors(campo);
    form.setFocus(campo);
  }

  const titulo =
    estado.fase === "novo"
      ? "Novo contato"
      : `Editar ${estado.fase === "editando" ? estado.ficha.name : estado.nome}`;

  return (
    <ModalShell
      open={aberto}
      onOpenChange={mudarAberto}
      title={titulo}
      description="Uma pessoa, um cadastro: lead, hóspede e proprietário usam esta mesma ficha."
      footer={
        <>
          <Button variant="ghost" onClick={() => mudarAberto(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button type="submit" form={ID_DO_FORM} disabled={isSubmitting || !comFormulario}>
            {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
            Salvar
          </Button>
        </>
      }
    >
      {estado.fase === "carregando" ? <EsqueletoDaFicha nome={estado.nome} /> : null}

      {estado.fase === "falhou" ? (
        <div className="flex flex-col gap-3">
          <AvisoDeErro
            code={estado.falha.code}
            titulo={`Não foi possível abrir a ficha de ${estado.nome}`}
            detalhe="Sem a ficha completa o formulário não abre: a lista mostra os dados pela metade, e salvar a partir dela apagaria o que ela não mostra."
          />
          {FALHA_DEFINITIVA.includes(estado.falha.code) ? null : (
            <div>
              <Button
                size="sm"
                variant="outline"
                onClick={() => void buscarFicha(estado.id, estado.nome)}
              >
                <RotateCcw aria-hidden="true" />
                Tentar de novo
              </Button>
            </div>
          )}
        </div>
      ) : null}

      {comFormulario && duplicata ? (
        <CaminhoDaDuplicata
          duplicata={duplicata.duplicata}
          telefoneEnviado={duplicata.telefone}
          aoCorrigir={corrigirCampoDuplicado}
        />
      ) : null}

      {comFormulario && erroGeral ? (
        <p role="alert" className="mb-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erroGeral}
        </p>
      ) : null}

      {comFormulario ? (
        <form id={ID_DO_FORM} onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
          <Campo id="contato-name" label="Nome" obrigatorio erro={errors.name?.message}>
            {(p) => <Input {...p} {...form.register("name")} autoComplete="name" />}
          </Campo>

          <div className="grid gap-4 sm:grid-cols-2">
            <Campo
              id="contato-phone"
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
                  ? "Passaporte é aceito como digitado, porque cada país tem o seu formato."
                  : "CPF e CNPJ são conferidos automaticamente e guardados só com os números."
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
                hint="É diferente da base legal: guardar o cadastro de quem se hospedou é permitido; mandar promoção exige este aceite."
                {...form.register("marketing_opt_in")}
              />

              {optIn ? (
                <Campo
                  id="contato-consent"
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
      ) : null}
    </ModalShell>
  );
}
