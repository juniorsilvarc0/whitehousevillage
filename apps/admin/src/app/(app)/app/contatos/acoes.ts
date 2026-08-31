"use server";

import { revalidatePath } from "next/cache";

import { detalhesDoZod } from "@/lib/acoes/campos";
import { exigir } from "@/lib/acoes/guarda";
import {
  chamarContatos,
  contatoJaExistente,
  falhaDeContato,
  type FalhaDeContato,
  type ResultadoDeContato,
} from "@/lib/contatos/api";
import {
  AnonimizacaoFormulario,
  ContatoFormulario,
  PALAVRA_DE_CONFIRMACAO,
  contatoParaEntrada,
} from "@/lib/contatos/esquemas";
import type { Contato, ResultadoDeAnonimizacao } from "@/lib/contatos/tipos";

const CAMINHO = "/app/contatos";

/**
 * Escrita do cadastro de contatos.
 *
 * Editar usa `PUT`, não `PATCH`: o formulário carrega a ficha inteira, então
 * mandar o estado completo é o que ele de fato representa. `PATCH` existe para
 * quem quer mudar um campo sem conhecer os outros — não é o caso de um modal que
 * abriu com todos preenchidos, e usá-lo esconderia o campo que a pessoa apagou
 * de propósito (o e-mail que o hóspede pediu para tirar, por exemplo).
 */

/**
 * O que a tela recebe quando o telefone (ou o documento) já é de alguém.
 *
 * **Este é o desfecho mais importante do cadastro**, e é por isso que ele tem um
 * tipo próprio em vez de virar uma mensagem de erro. Quem digita um telefone que
 * já existe não cometeu um engano: ele quer chegar naquela pessoa e não sabia
 * que ela já estava lá. A resposta certa é o caminho até a ficha — nunca um
 * "409" seco.
 */
export type Duplicata = {
  /** `phone_e164` ou `doc_number`, como o contrato manda em `details.field`. */
  campo: "phone_e164" | "doc_number" | null;
  /** O `details.contact_id` da recusa. Basta para abrir a ficha. */
  contactId: string | null;
  /** Quem é, quando deu para descobrir sem gravar `pii_access_log`. */
  contato: Contato | null;
};

/** Sucesso, a duplicata (que tem caminho de saída) ou qualquer outra recusa.
 *  Quem consome estreita com `"duplicata" in resultado`. */
export type SalvamentoDeContato =
  | { ok: true; data: Contato }
  | { ok: false; duplicata: Duplicata }
  | FalhaDeContato;

export async function salvarContato(
  id: string | null,
  valores: ContatoFormulario,
): Promise<SalvamentoDeContato> {
  const recusa = await exigir("contacts", id ? "editar" : "criar");
  if (recusa) return falhaDeContato(recusa.code, recusa.message, recusa.details);

  const analise = ContatoFormulario.safeParse(valores);
  if (!analise.success) {
    return falhaDeContato("VALIDATION_ERROR", "Confira os dados do contato.", detalhesDoZod(analise.error));
  }

  const corpo = contatoParaEntrada(analise.data);
  const resultado = await chamarContatos<Contato>(id ? `/contacts/${id}` : "/contacts", {
    method: id ? "PUT" : "POST",
    body: corpo,
  });

  if (resultado.ok) {
    revalidatePath(CAMINHO);
    if (id) revalidatePath(`${CAMINHO}/${id}`);
    return resultado;
  }

  if (resultado.code === "CONTACT_DUPLICATE") {
    const campo = resultado.details.field;
    return {
      ok: false,
      duplicata: {
        campo: campo === "phone_e164" || campo === "doc_number" ? campo : null,
        contactId: typeof resultado.details.contact_id === "string" ? resultado.details.contact_id : null,
        // Descobre o nome pela LISTA, não por `GET /contacts/{id}`: a rota de
        // detalhe grava `pii_access_log`, e registrar leitura de dado pessoal por
        // causa de uma digitação repetida enche a trilha de LGPD de ruído.
        contato: await contatoJaExistente(resultado.details, {
          phone_e164: corpo.phone_e164,
          doc_number: corpo.doc_number,
        }),
      },
    };
  }

  return resultado;
}

/**
 * Apaga — e só o que nunca existiu comercialmente.
 *
 * Assim que o contato tem **qualquer** vínculo, a API responde
 * `409 RESOURCE_IN_USE` com a contagem por tipo, e o caminho passa a ser a
 * anonimização. Apagar ali derrubaria a FK `reservations.contact_id`, que é
 * `NOT NULL`: o preço de "sumir com o cadastro" seria sumir com a venda.
 *
 * A tela **não** esconde o botão por conta própria com base em `references`:
 * quem decide é a API, na hora, e a recusa dela traz a contagem atualizada. Um
 * botão que some por causa de um número lido há dez minutos mente nas duas
 * direções.
 */
export async function apagarContato(id: string): Promise<ResultadoDeContato<null>> {
  const recusa = await exigir("contacts", "excluir");
  if (recusa) return falhaDeContato(recusa.code, recusa.message, recusa.details);

  const resultado = await chamarContatos<void>(`/contacts/${id}`, { method: "DELETE" });
  if (!resultado.ok) return resultado;

  revalidatePath(CAMINHO);
  return { ok: true, data: null };
}

/**
 * Anonimiza — o direito de eliminação da LGPD (art. 18, VI) aplicado do único
 * jeito que sobrevive a uma auditoria fiscal: **a pessoa some, a venda fica**.
 *
 * A confirmação digitada é conferida aqui, e não só no cliente, porque Server
 * Action é endpoint público: quem tem o id da action chama sem passar pelo
 * formulário. Não é autorização (quem autoriza é o middleware da API) — é o
 * mesmo cuidado de exigir o gesto deliberado antes do irreversível.
 */
export async function anonimizarContato(
  id: string,
  valores: AnonimizacaoFormulario,
): Promise<ResultadoDeContato<ResultadoDeAnonimizacao>> {
  const recusa = await exigir("contacts", "excluir");
  if (recusa) return falhaDeContato(recusa.code, recusa.message, recusa.details);

  const analise = AnonimizacaoFormulario.safeParse(valores);
  if (!analise.success) {
    return falhaDeContato("VALIDATION_ERROR", "Confira o motivo.", detalhesDoZod(analise.error));
  }
  if (analise.data.confirmacao.toUpperCase() !== PALAVRA_DE_CONFIRMACAO) {
    return falhaDeContato("VALIDATION_ERROR", "Confirmação não confere.", {
      confirmacao: `Digite ${PALAVRA_DE_CONFIRMACAO} para confirmar.`,
    });
  }

  const resultado = await chamarContatos<ResultadoDeAnonimizacao>(`/contacts/${id}/anonymize`, {
    method: "POST",
    body: { reason: analise.data.reason },
  });

  if (resultado.ok) {
    revalidatePath(CAMINHO);
    revalidatePath(`${CAMINHO}/${id}`);
  }
  return resultado;
}

/**
 * Portabilidade (LGPD art. 18, V): tudo que o sistema sabe da pessoa, em JSON.
 *
 * ## Por que passa por uma action em vez de um link para a API
 *
 * O JWT vive em cookie `httpOnly` e o navegador **nunca** fala com a API Go — um
 * `<a href="…/contacts/x/export">` sairia sem `Authorization` e receberia 401.
 * A action busca do lado do servidor, onde o token existe, e devolve o pacote
 * para a tela transformar em arquivo. É o mesmo caminho de todo o resto do
 * painel; o que muda é o destino do dado.
 *
 * **É JSON, não PDF**, como o contrato decidiu: portabilidade é "em formato de
 * uso comum e interoperável" — PDF é para ler, JSON é para carregar em outro
 * sistema.
 *
 * Esta rota grava `pii_access_log` com `reason: "export"`. Exportar é a maior
 * leitura de dado pessoal que o sistema faz numa chamada só; se alguma leitura
 * tem de deixar rastro, é esta.
 */
export async function exportarContato(id: string): Promise<ResultadoDeContato<unknown>> {
  const recusa = await exigir("contacts", "ver");
  if (recusa) return falhaDeContato(recusa.code, recusa.message, recusa.details);

  return chamarContatos<unknown>(`/contacts/${id}/export`);
}
