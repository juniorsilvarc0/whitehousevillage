import { apiFetch, apiList, isApiError, type ApiFetchInit, type Query } from "@/lib/api/client";
import type { Lista } from "@/lib/api/types";
import { ehCodigoDeContatos, type CodigoContato } from "@/lib/contatos/codigos";
import type { ContatoNaLista } from "@/lib/contatos/tipos";

/**
 * A fronteira do cadastro de contatos com a API — **servidor-only**, como todo o
 * resto: o JWT vive em cookie `httpOnly` e quem o injeta é o RSC ou a Server
 * Action.
 *
 * ## Por que não usar `tentar()` direto
 *
 * `tentar()` devolve o `code` que o `ApiError` carrega, e o `ApiError` já nasceu
 * com o código **normalizado** — `apiRequest` chama `normalizarCodigo()` antes
 * de lançar e descarta o código cru do corpo. Como o espelho de códigos do
 * painel ainda não conhece `CONTACT_DUPLICATE` (ver `lib/contatos/codigos.ts`),
 * a colisão de telefone chegaria à tela como `INTERNAL`: "erro no servidor" em
 * cima da recusa mais comum e mais fácil de resolver do cadastro.
 *
 * `ApiFetchInit.onResponse` entrega a `Response` **antes** do parse; uma cópia
 * (`clone()`) lida em paralelo devolve o `error.code` original, e o corpo
 * verdadeiro segue intacto para o `apiRequest`, que continua sendo o único lugar
 * que monta URL, injeta token e trata cache. Só há clone quando a resposta
 * **não** é ok. É a mesma técnica de `lib/crm/api.ts`, pelo mesmo motivo — e as
 * duas encolhem juntas no dia em que os códigos entrarem no espelho geral.
 */

export type FalhaDeContato = {
  ok: false;
  code: CodigoContato;
  message: string;
  details: Record<string, unknown>;
};

export type ResultadoDeContato<T> = { ok: true; data: T } | FalhaDeContato;

export function falhaDeContato(
  code: CodigoContato,
  message: string,
  details: Record<string, unknown> = {},
): FalhaDeContato {
  return { ok: false, code, message, details };
}

async function codigoCruDoCorpo(resposta: Response): Promise<string | undefined> {
  try {
    const corpo = (await resposta.json()) as { error?: { code?: string } };
    return corpo?.error?.code;
  } catch {
    // Corpo ilegível é problema de infraestrutura, e o `apiRequest` já o trata.
    // Aqui só significa "não há código cru a recuperar".
    return undefined;
  }
}

export async function chamarContatos<T>(
  path: string,
  init: ApiFetchInit = {},
): Promise<ResultadoDeContato<T>> {
  // Holder em objeto, e não `let`: a atribuição acontece dentro do callback, e o
  // TypeScript estreitaria uma variável solta para `null` no ponto da leitura.
  const capturado: { cru?: Promise<string | undefined> } = {};

  try {
    const data = await apiFetch<T>(path, {
      ...init,
      onResponse: (resposta) => {
        init.onResponse?.(resposta);
        if (!resposta.ok) capturado.cru = codigoCruDoCorpo(resposta.clone());
      },
    });
    return { ok: true, data };
  } catch (erro) {
    if (!isApiError(erro)) {
      // Erro que não veio da API é defeito do painel. Não se disfarça de erro de
      // negócio: o texto genérico avisa sem inventar uma causa comercial.
      return falhaDeContato("INTERNAL", "Erro inesperado no painel.");
    }
    const cru = capturado.cru ? await capturado.cru : undefined;
    return {
      ok: false,
      code: cru && ehCodigoDeContatos(cru) ? cru : erro.code,
      message: erro.message,
      details: erro.details,
    };
  }
}

/** Lista **com** `meta` — a agenda da casa é paginada de verdade. */
export async function paginarContatos<T>(
  path: string,
  query?: Query,
): Promise<ResultadoDeContato<Lista<T>>> {
  const capturado: { cru?: Promise<string | undefined> } = {};
  try {
    const lista = await apiList<T>(path, {
      query,
      onResponse: (resposta) => {
        if (!resposta.ok) capturado.cru = codigoCruDoCorpo(resposta.clone());
      },
    });
    return { ok: true, data: lista };
  } catch (erro) {
    if (!isApiError(erro)) return falhaDeContato("INTERNAL", "Erro inesperado no painel.");
    const cru = capturado.cru ? await capturado.cru : undefined;
    return {
      ok: false,
      code: cru && ehCodigoDeContatos(cru) ? cru : erro.code,
      message: erro.message,
      details: erro.details,
    };
  }
}

/**
 * Descobre **quem** é o contato que já existe, depois de um `409
 * CONTACT_DUPLICATE`.
 *
 * O contrato manda `details.contact_id` e `details.field` na recusa. Com o `id`
 * na mão, o caminho óbvio seria `GET /contacts/{id}` — e é justamente o que não
 * se faz aqui: **essa rota grava `pii_access_log`**. Registrar leitura de dado
 * pessoal por causa de uma digitação repetida encheria a trilha de LGPD de ruído
 * e faria a auditoria perder de vista as leituras que importam.
 *
 * A lista, ao contrário, não registra: `phone` e `doc_number` são buscas de
 * igualdade exata que devolvem zero ou um registro — desenhadas no contrato
 * exatamente para o inbound de WhatsApp perguntar "esta pessoa já existe?" antes
 * de criar qualquer coisa. É a mesma pergunta, feita pela porta certa.
 *
 * Devolve `null` sem reclamar quando não dá para descobrir (campo desconhecido,
 * lista que falhou): a tela ainda tem o `contact_id` para oferecer o link. Um
 * nome é melhor que um id; um id é muito melhor que nada.
 *
 * O que volta é a **linha da lista** (`ContatoNaLista`), mascarada e sem
 * `birth_date`/`notes`: serve para dizer o nome e se a ficha está anonimizada,
 * nunca para preencher formulário nem para ligar.
 */
export async function contatoJaExistente(
  details: Record<string, unknown>,
  enviado: { phone_e164: string | null; doc_number: string | null },
): Promise<ContatoNaLista | null> {
  // `details.field` é do contrato (`phone_e164` | `doc_number`); o VALOR vem do
  // que acabou de ser enviado, e não de `details` — o contrato promete o campo e
  // o `contact_id`, não o valor, e ler um campo que o contrato não publica é
  // combinar com a implementação de hoje.
  const campo = typeof details.field === "string" ? details.field : null;

  const consulta: Query | null =
    campo === "phone_e164" && enviado.phone_e164
      ? { phone: enviado.phone_e164 }
      : campo === "doc_number" && enviado.doc_number
        ? { doc_number: enviado.doc_number }
        : null;

  if (!consulta) return null;

  const encontrados = await paginarContatos<ContatoNaLista>("/contacts", {
    ...consulta,
    per_page: 1,
    // Ficha anonimizada não aparece por padrão — mas ela ainda ocupa o telefone
    // até o `/anonymize` liberá-lo. Escondê-la aqui devolveria "já existe" sem
    // dizer quem, que é o erro seco que esta rodada veio consertar.
    include_anonymized: true,
  });
  return encontrados.ok ? (encontrados.data.data[0] ?? null) : null;
}
