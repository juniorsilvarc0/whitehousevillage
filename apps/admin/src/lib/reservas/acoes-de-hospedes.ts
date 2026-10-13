"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { salvarContato } from "@/app/(app)/app/contatos/acoes";
import { apiFetch, apiList } from "@/lib/api/client";
import { tentar } from "@/lib/acoes/executar";
import { exigir } from "@/lib/acoes/guarda";
import { falha, type Falha, type Resultado } from "@/lib/acoes/resultado";
import { documentoParaApi, documentoValido } from "@/lib/contatos/documento";
import type { ContatoFormulario } from "@/lib/contatos/esquemas";
import { TIPOS_DE_DOCUMENTO, type ContatoNaLista, type TipoDeDocumento } from "@/lib/contatos/tipos";
import { pedeRecarga } from "@/lib/reservas/hospedes";
import type { HospedeAcrescentar, HospedeDaReserva, NumeroDeHospedes, Reserva } from "@/lib/reservas/tipos";

/**
 * A escrita da rooming list (F2-22) — `POST` e `DELETE` em
 * `/reservations/{id}/guests`, o `PATCH` que abre lugar nela e a busca por
 * documento que vem antes de tudo.
 *
 * ## Uma leitura do `/full` por gesto, e só quando ela muda algo
 *
 * O `/full` grava **uma linha de `pii_access_log` por hóspede** a cada leitura
 * (até 24 na Completa). Então cada gesto aqui recarrega a tela **uma** vez:
 * a action chama `revalidatePath` e o Next desenha a rota de novo na própria
 * resposta (o "single roundtrip" das Server Actions). A tela **não** chama
 * `router.refresh()` depois — seria a segunda leitura do mesmo gesto, e o
 * dobro de linhas na trilha da LGPD para mostrar a mesma lista.
 *
 * Na recusa, recarrega só quando ela diz que a tela está velha
 * (`pedeRecarga`: alguém acrescentou, retirou ou mudou o estado da reserva).
 * A busca por documento **não** revalida nada: ela lê a coleção mascarada, que
 * não grava rastro, e redesenhar a reserva por causa dela seria gravar 24
 * linhas por uma pergunta.
 *
 * ## Dado pessoal
 *
 * O documento digitado atravessa no corpo da action (POST), nunca na URL do
 * navegador; aqui ele vira a query de `GET /contacts`, servidor a servidor. Nada
 * é registrado em console. As respostas devolvem à tela o **mínimo**: o nome e o
 * `contact_id` de quem entrou — o documento cheio volta pelo `/full`, que é a
 * leitura que registra quem viu.
 *
 * `exigir()` recusa cedo o que a API recusaria; **não autoriza nada** — quem
 * barra é o middleware da API, a cada requisição.
 */

const RESERVAS = "/app/reservas";

/** Server Action é endpoint público: o id entra no caminho da API e não pode
 *  virar outro caminho. Forma de GUID, sem exigir versão — quem diz se existe
 *  é o servidor. */
const Id = z.guid();

/** Quem entrou na lista, do jeito que a tela precisa para dizer "entrou". */
export type PessoaNaLista = { contact_id: string; name: string };

/** Quem a busca por documento achou — a **linha da lista**, mascarada. Serve
 *  para escolher pelo id e mais nada: não preenche formulário nem liga. */
export type PessoaEncontrada = Pick<ContatoNaLista, "id" | "name" | "doc_type" | "doc_number">;

function revalidarReserva(reservaId: string): void {
  revalidatePath(`${RESERVAS}/${reservaId}`);
}

function idsInvalidos(): Falha {
  return falha("VALIDATION_ERROR", "Identificador inválido.");
}

// ── Procurar pelo documento ────────────────────────────────────────────────

/**
 * O primeiro passo do balcão: a pessoa já tem ficha?
 *
 * `GET /contacts?doc_number=` compara o número **cheio** no servidor e devolve a
 * linha **mascarada** (`ContatoNaLista`), sem gravar `pii_access_log`. Ficha
 * anonimizada fica de fora (o padrão da rota): ela não volta a uma estadia, e
 * o documento dela já foi liberado.
 *
 * O dígito verificador é conferido antes de gastar a requisição — mesma regra
 * do cadastro, para o CPF digitado errado aparecer no campo, com o documento
 * ainda na mão, e não como "ninguém encontrado" seguido de um cadastro que o
 * servidor recusaria.
 */
export async function procurarPorDocumento(
  tipo: TipoDeDocumento,
  numero: string,
): Promise<Resultado<PessoaEncontrada[]>> {
  const recusa = await exigir("contacts", "ver");
  if (recusa) return recusa;

  if (!TIPOS_DE_DOCUMENTO.includes(tipo) || typeof numero !== "string" || numero.length > 40) {
    return falha("VALIDATION_ERROR", "Documento inválido.", { doc_number: "Escolha o tipo e digite o número." });
  }
  if (!documentoValido(tipo, numero)) {
    return falha("VALIDATION_ERROR", "Documento inválido.", {
      doc_number:
        tipo === "passaporte"
          ? "Passaporte tem de 5 a 20 letras ou números."
          : `${tipo.toUpperCase()} inválido — confira os dígitos.`,
    });
  }

  return tentar(async () => {
    const lista = await apiList<ContatoNaLista>("/contacts", {
      query: { doc_number: documentoParaApi(tipo, numero), per_page: 10 },
    });
    return lista.data.map(({ id, name, doc_type, doc_number }) => ({ id, name, doc_type, doc_number }));
  });
}

// ── Acrescentar quem já tem ficha ──────────────────────────────────────────

/**
 * `POST /reservations/{id}/guests` com o `contact_id` — o acompanhante entra.
 *
 * Sem `Idempotency-Key`, como o contrato decide: não cria reserva nem move
 * dinheiro, e o segundo clique responde `409 CODE_IN_USE`.
 */
export async function acrescentarHospede(reservaId: string, contactId: string): Promise<Resultado<PessoaNaLista>> {
  const recusa = await exigir("reservations", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(reservaId).success || !Id.safeParse(contactId).success) return idsInvalidos();

  return postarHospede(reservaId, contactId);
}

/** O `POST` em si, com a recarga certa — compartilhado com o cadastro e com o
 *  aumento, que terminam no mesmo passo. */
async function postarHospede(reservaId: string, contactId: string): Promise<Resultado<PessoaNaLista>> {
  const corpo: HospedeAcrescentar = { contact_id: contactId };
  const resultado = await tentar(() =>
    apiFetch<HospedeDaReserva>(`/reservations/${reservaId}/guests`, { method: "POST", body: corpo }),
  );

  if (resultado.ok) {
    revalidarReserva(reservaId);
    // Só o nome e o id voltam ao navegador: o documento cheio que a resposta
    // traz chega pela recarga do `/full`, que é a leitura que registra.
    return { ok: true, data: { contact_id: resultado.data?.contact_id ?? contactId, name: resultado.data?.name ?? "" } };
  }
  if (pedeRecarga(resultado)) revalidarReserva(reservaId);
  return resultado;
}

// ── Cadastrar a pessoa e acrescentar ───────────────────────────────────────

/** O documento ou o telefone já são de alguém: `409 CONTACT_DUPLICATE`. */
export type DuplicataNaLista = {
  /** `details.contact_id` — é com ele que o fluxo segue. */
  contactId: string | null;
  /** `details.field`: qual chave colidiu. */
  campo: "phone_e164" | "doc_number" | null;
  /** Quem é, pela linha **mascarada** da lista (não grava rastro). Basta para o
   *  operador conferir que é a mesma pessoa antes de seguir. */
  pessoa: (PessoaEncontrada & { anonimizada: boolean }) | null;
};

/**
 * Os quatro desfechos do "Cadastrar pessoa". Quem consome estreita com
 * `"duplicata" in r` e `"cadastrada" in r`.
 */
export type CadastroNaLista =
  /** Cadastrou e entrou na lista. */
  | { ok: true; data: PessoaNaLista }
  /** O documento ou o telefone já são de alguém — nada foi cadastrado. */
  | { ok: false; duplicata: DuplicataNaLista }
  /** A ficha nasceu, mas a pessoa não entrou na lista (lista cheia, reserva
   *  encerrada). A ficha fica no cadastro, sem vínculo: é uma pessoa real cujo
   *  dado foi colhido para a estadia — a tela diz o que falta e tenta de novo
   *  com este `contact_id`, sem cadastrar de novo. */
  | { ok: false; cadastrada: PessoaNaLista; falha: Falha }
  /** Nada foi cadastrado (campo inválido, permissão, rede). */
  | Falha;

/**
 * Não achou pelo documento: `POST /contacts` e, com a ficha nascida, o `POST`
 * de hóspede — **numa** ida ao servidor, que é uma recarga da tela só.
 *
 * O cadastro é o de sempre (`salvarContato`): a mesma validação, a mesma
 * normalização, a mesma base legal e a mesma tradução de colisão — "um caminho
 * só cria pessoa" é a razão de o contrato não aceitar nome e CPF no `POST` de
 * hóspede. As duas permissões são pedidas antes de qualquer escrita: sem
 * `reservations:editar`, a ficha nasceria e o segundo passo recusaria.
 *
 * Na colisão (`409 CONTACT_DUPLICATE`) **não** se acrescenta sozinho: o fluxo
 * segue com `details.contact_id`, mas depois de a tela mostrar quem é. Telefone
 * repetido é comum em família — a filha cadastrada com o celular da mãe — e
 * seguir sem perguntar poria na lista a pessoa que já tem aquele número, não a
 * que está no balcão.
 */
export async function cadastrarEAcrescentar(
  reservaId: string,
  valores: ContatoFormulario,
): Promise<CadastroNaLista> {
  const recusa = await exigir("reservations", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(reservaId).success) return idsInvalidos();

  const cadastro = await salvarContato(null, valores);

  if ("duplicata" in cadastro) {
    const { contactId, campo, contato } = cadastro.duplicata;
    return {
      ok: false,
      duplicata: {
        contactId,
        campo,
        pessoa: contato
          ? {
              id: contato.id,
              name: contato.name,
              doc_type: contato.doc_type,
              doc_number: contato.doc_number,
              anonimizada: Boolean(contato.anonymized_at),
            }
          : null,
      },
    };
  }
  if (!cadastro.ok) return cadastro;

  const pessoa: PessoaNaLista = { contact_id: cadastro.data.id, name: cadastro.data.name };
  const lista = await postarHospede(reservaId, pessoa.contact_id);
  if (lista.ok) return lista;
  return { ok: false, cadastrada: pessoa, falha: lista };
}

// ── Retirar ────────────────────────────────────────────────────────────────

/**
 * `DELETE /reservations/{id}/guests/{contactId}` — só acompanhante.
 *
 * **`404` é "já saiu"**, e volta como sucesso (`jaSaiu: true`): é o segundo
 * clique de um "retirar" repetido, ou o colega que retirou primeiro. A tela
 * recarrega nos dois casos — a lista desenhada ainda mostrava a pessoa.
 */
export async function retirarHospede(
  reservaId: string,
  contactId: string,
): Promise<Resultado<{ jaSaiu: boolean }>> {
  const recusa = await exigir("reservations", "editar");
  if (recusa) return recusa;
  if (!Id.safeParse(reservaId).success || !Id.safeParse(contactId).success) return idsInvalidos();

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/reservations/${reservaId}/guests/${contactId}`, { method: "DELETE" });
    return { jaSaiu: false };
  });

  if (resultado.ok) {
    revalidarReserva(reservaId);
    return resultado;
  }
  if (resultado.code === "NOT_FOUND") {
    revalidarReserva(reservaId);
    return { ok: true, data: { jaSaiu: true } };
  }
  if (pedeRecarga(resultado)) revalidarReserva(reservaId);
  return resultado;
}

// ── Abrir lugar: aumentar o número de hóspedes ─────────────────────────────

/** O número subiu; e, se havia alguém esperando, se entrou. */
export type AumentoFeito = { guests_count: number; acrescentada: PessoaNaLista | null };

/** Em que passo o aumento parou: `numero` (nada mudou) ou `lista` (o número
 *  subiu e a pessoa não entrou). */
export type FalhaDoAumento = Falha & { etapa: "numero" | "lista" };

/**
 * Chegou mais gente: `PATCH /reservations/{id}` com **só** `guests_count` —
 * o caminho que o contrato dá ("aumenta-se `guests_count` e só então se
 * acrescenta o nome"). Quem confere a capacidade do produto é o servidor
 * (`422 CAPACITY_EXCEEDED`); o `PATCH` não mexe em data, produto nem preço.
 *
 * Com `contactId`, acrescenta a pessoa que esperava lugar na **mesma** ida ao
 * servidor — uma recarga da tela, não duas.
 */
export async function aumentarHospedes(
  reservaId: string,
  numero: number,
  contactId: string | null = null,
): Promise<{ ok: true; data: AumentoFeito } | FalhaDoAumento> {
  const recusa = await exigir("reservations", "editar");
  if (recusa) return { ...recusa, etapa: "numero" };
  if (!Id.safeParse(reservaId).success || (contactId !== null && !Id.safeParse(contactId).success)) {
    return { ...idsInvalidos(), etapa: "numero" };
  }
  if (!Number.isInteger(numero) || numero < 1 || numero > 999) {
    return { ...falha("VALIDATION_ERROR", "Número de hóspedes inválido."), etapa: "numero" };
  }

  const corpo: NumeroDeHospedes = { guests_count: numero };
  const patch = await tentar(() => apiFetch<Reserva>(`/reservations/${reservaId}`, { method: "PATCH", body: corpo }));
  if (!patch.ok) {
    if (pedeRecarga(patch)) revalidarReserva(reservaId);
    return { ...patch, etapa: "numero" };
  }

  // A lista de reservas mostra o número de hóspedes; o detalhe é recarregado
  // de qualquer jeito (pelo `postarHospede` ou aqui embaixo).
  revalidatePath(RESERVAS);
  const guests_count = patch.data?.guests_count ?? numero;

  if (contactId === null) {
    revalidarReserva(reservaId);
    return { ok: true, data: { guests_count, acrescentada: null } };
  }

  const lista = await postarHospede(reservaId, contactId);
  if (!lista.ok) {
    revalidarReserva(reservaId);
    return { ...lista, etapa: "lista" };
  }
  return { ok: true, data: { guests_count, acrescentada: lista.data } };
}
