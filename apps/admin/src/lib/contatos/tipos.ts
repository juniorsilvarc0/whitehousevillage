/**
 * Tipos do cadastro de contatos — espelho de `apps/api/openapi/openapi.yaml`
 * (tag `Contatos`), escrito à mão pelo mesmo motivo que `lib/api/types.ts`: o
 * contrato é a fonte da verdade e um gerador entraria no caminho sem
 * acrescentar nada enquanto a superfície couber numa leitura.
 *
 * Fica em `lib/contatos/` e não em `lib/api/comercial.ts` por posse de pasta
 * (`docs/agents.md` §2) — e porque quem mexe em tarifa não deveria abrir o
 * arquivo que descreve dado pessoal.
 *
 * ## A ideia que o cadastro carrega
 *
 * **Uma pessoa, um registro** (spec §6). Lead, hóspede, corretor e proprietário
 * apontam todos para cá; não existe um cadastro de hóspede paralelo a este. É o
 * que permite ao WhatsApp reconhecer quem já existe pelo telefone em vez de
 * abrir um segundo cadastro a cada mensagem — e é por isso que `phone_e164` é
 * único em toda a base.
 */

/**
 * Base legal que sustenta **guardar a ficha** (LGPD art. 7).
 *
 * Não é a base do marketing — essa é `marketing_opt_in` com `consent_at`, e são
 * eixos separados de propósito: `contrato` legitima guardar o cadastro de quem
 * se hospedou e **não** legitima mandar oferta para ele.
 */
export type BaseLegalLGPD = "consentimento" | "contrato" | "obrigacao_legal" | "legitimo_interesse";

export const BASES_LEGAIS: readonly BaseLegalLGPD[] = [
  "consentimento",
  "contrato",
  "obrigacao_legal",
  "legitimo_interesse",
];

export const ROTULO_DA_BASE: Record<BaseLegalLGPD, string> = {
  consentimento: "Consentimento",
  contrato: "Execução de contrato",
  obrigacao_legal: "Obrigação legal",
  legitimo_interesse: "Legítimo interesse",
};

/** A frase que explica a base legal na hora de escolher — sem ela o campo vira
 *  um `<select>` que se preenche no chute, e base legal errada é multa. */
export const EXPLICACAO_DA_BASE: Record<BaseLegalLGPD, string> = {
  consentimento: "A pessoa preencheu um formulário e autorizou o cadastro.",
  contrato: "Hospedou-se ou vai se hospedar: guardar a ficha é executar o contrato.",
  obrigacao_legal: "A lei manda guardar — é o que sobrevive à anonimização.",
  legitimo_interesse: "Prospecção registrada, sem consentimento formal.",
};

/** `cpf` e `cnpj` são gravados **só com dígitos** e validados por dígito
 *  verificador; `passaporte` é alfanumérico e não tem validação de formato —
 *  cada país tem o seu, e recusar o que não se sabe validar barraria hóspede
 *  estrangeiro no balcão. */
export type TipoDeDocumento = "cpf" | "cnpj" | "passaporte";

export const TIPOS_DE_DOCUMENTO: readonly TipoDeDocumento[] = ["cpf", "cnpj", "passaporte"];

export const ROTULO_DO_DOCUMENTO: Record<TipoDeDocumento, string> = {
  cpf: "CPF",
  cnpj: "CNPJ",
  passaporte: "Passaporte",
};

/**
 * **A ficha**, com os valores cheios. Só sai em resposta de UM registro
 * (`GET`/`PATCH /contacts/{id}`, que gravam `pii_access_log`; `POST`/`PUT`; e
 * `/anonymize`). A coleção é `ContatoNaLista` — mascarada e sem
 * `birth_date`/`notes`.
 */
export type Contato = {
  id: string;
  name: string;
  email: string | null;
  /**
   * E.164, com `+` e DDI. **Único em toda a base**
   * (`UNIQUE (phone_e164) WHERE phone_e164 IS NOT NULL`) — é a chave de
   * deduplicação do WhatsApp. `NULL` não colide com `NULL`: o índice é parcial
   * justamente para caber mais de um contato sem telefone.
   */
  phone_e164: string | null;
  doc_type: TipoDeDocumento | null;
  doc_number: string | null;
  birth_date: string | null;
  city: string | null;
  state: string | null;
  notes: string | null;
  lgpd_basis: BaseLegalLGPD | null;
  marketing_opt_in: boolean;
  consent_at: string | null;
  /** Preenchido pelo `/anonymize`. Enquanto for `null` a ficha é normal;
   *  preenchido, ela é uma casca que existe só para sustentar os registros
   *  financeiros que apontam para este `id`. */
  anonymized_at: string | null;
  created_at: string;
  updated_at: string;
};

/**
 * A linha de `GET /contacts` — **não é a ficha**, e o tipo existe para o `tsc`
 * não deixar ninguém confundir as duas (dívida D11 do roadmap).
 *
 * Desde o F2-23 a coleção devolve o schema `ContatoNaLista` da OpenAPI:
 * documento, telefone e e-mail **mascarados**, e **sem** as chaves `birth_date`
 * e `notes` (nem como `null`). Os campos mantêm os nomes da ficha para a tela
 * trocar de fonte sem trocar de vocabulário — mas o conteúdo é outro.
 *
 * ## O que esta linha não pode fazer
 *
 * - **Preencher formulário.** O `PUT` manda a ficha inteira: a máscara voltaria
 *   como entrada (`422` em campo que ninguém tocou) e `notes`/`birth_date`, que
 *   não vieram, iriam vazios — num contato só com nome e anotação, nada recusa e
 *   **a anotação some**. Editar a partir da lista busca a ficha
 *   (`GET /contacts/{id}`, que grava `pii_access_log`) e só então monta o
 *   formulário: ver `AberturaDoContato` em `components/contatos/modal-de-contato`.
 * - **Ligar nem chamar no WhatsApp.** `+*********0000` não é E.164.
 *
 * As duas chaves ausentes são o que torna a linha **inatribuível** a `Contato`:
 * passar a linha onde se espera a ficha é erro de compilação, e
 * `tipos.test.ts` prova que continua sendo. Não as acrescente aqui como
 * opcionais "para facilitar" — é exatamente o atalho que reabre a perda.
 */
export type ContatoNaLista = {
  id: string;
  /** Cheio — o nome não é mascarado em coleção nenhuma. */
  name: string;
  /** **Mascarado**: `fernanda.lima@gmail.com` → `f***@gmail.com`. */
  email: string | null;
  /** **Mascarado**: `+5585999990000` → `+*********0000`. Não é E.164: não
   *  serve para `tel:` nem para WhatsApp. `null` continua `null` — a máscara
   *  não inventa dado, então a presença do valor diz se a ficha tem telefone. */
  phone_e164: string | null;
  doc_type: TipoDeDocumento | null;
  /** **Mascarado** e já pontuado pela API: CPF sai `***.***.777-35`, CNPJ
   *  sai com os dígitos 9 a 14 e o resto em asteriscos, passaporte
   *  `******567`. A tela mostra como veio. */
  doc_number: string | null;
  city: string | null;
  state: string | null;
  lgpd_basis: BaseLegalLGPD | null;
  marketing_opt_in: boolean;
  consent_at: string | null;
  anonymized_at: string | null;
  created_at: string;
  updated_at: string;
};

/** Quantos registros apontam para este contato, por tipo. É o que o `DELETE`
 *  devolve em `details.references` quando recusa, e o que a ficha mostra antes
 *  de oferecer o botão de anonimizar. */
export type VinculosDoContato = {
  reservations: number;
  leads: number;
  opportunities: number;
  activities: number;
  notes: number;
  conversations: number;
};

export type ContatoCompleto = Contato & {
  /** Só no detalhe: contá-los é uma consulta por contato, e numa página de 25
   *  seriam 25 contagens para desenhar um número que a lista não mostra. */
  references?: VinculosDoContato;
};

/** Corpo do `POST` e do `PUT`. `name` é o único obrigatório. */
export type ContatoEntrada = {
  name: string;
  email: string | null;
  phone_e164: string | null;
  doc_type: TipoDeDocumento | null;
  doc_number: string | null;
  birth_date: string | null;
  city: string | null;
  state: string | null;
  notes: string | null;
  lgpd_basis: BaseLegalLGPD | null;
  marketing_opt_in: boolean;
  consent_at: string | null;
};

export type ResultadoDeAnonimizacao = {
  contact: Contato;
  /** O que **continua existindo** apontando para este `id`. Está na resposta de
   *  propósito: quem anonimiza precisa ver, na hora, que a reserva e o razão não
   *  foram tocados — é a diferença entre "eliminei o dado pessoal" e "apaguei a
   *  venda". */
  preserved: VinculosDoContato;
};

export const ROTULO_DO_VINCULO: Record<keyof VinculosDoContato, string> = {
  reservations: "reservas",
  leads: "leads",
  opportunities: "oportunidades",
  activities: "atividades",
  notes: "notas",
  conversations: "conversas",
};

/** Soma dos vínculos — zero é o único estado em que o `DELETE` é aceito. */
export function totalDeVinculos(vinculos: VinculosDoContato | undefined): number {
  if (!vinculos) return 0;
  return Object.values(vinculos).reduce((soma, n) => soma + (Number.isFinite(n) ? n : 0), 0);
}

/** Lê `details.references` de um `409 RESOURCE_IN_USE` sem confiar no formato:
 *  a recusa continua útil mesmo se o corpo vier pela metade. */
export function vinculosDosDetalhes(details: Record<string, unknown>): VinculosDoContato | null {
  const cru = details.references;
  if (!cru || typeof cru !== "object") return null;
  const objeto = cru as Record<string, unknown>;
  const numero = (chave: string) => (typeof objeto[chave] === "number" ? (objeto[chave] as number) : 0);
  return {
    reservations: numero("reservations"),
    leads: numero("leads"),
    opportunities: numero("opportunities"),
    activities: numero("activities"),
    notes: numero("notes"),
    conversations: numero("conversations"),
  };
}
