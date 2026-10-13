"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { ArrowRight, Loader2, RotateCcw, UserRoundCheck } from "lucide-react";

import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Button, buttonVariants } from "@/components/ui/button";
import type { FalhaDeContato } from "@/lib/contatos/api";
import { aplicarFalhaDeContato } from "@/lib/contatos/formulario";
import { ContatoFormulario } from "@/lib/contatos/esquemas";
import { formatarTelefone, limparTelefone } from "@/lib/contatos/telefone";
import type { Contato } from "@/lib/contatos/tipos";
import { cn } from "@/lib/utils";

import { lerFichaDoContato, salvarContato, type Duplicata } from "@/app/(app)/app/contatos/acoes";
import { AvisoDeErro, notificarSucesso } from "@/components/contatos/avisos";
import { CamposDoContato, valoresDoContato } from "@/components/contatos/campos-do-contato";

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

  const { isSubmitting } = form.formState;

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

      {comFormulario ? <CamposDoContato form={form} idDoForm={ID_DO_FORM} aoEnviar={enviar} /> : null}
    </ModalShell>
  );
}
