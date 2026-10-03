import { z } from "zod";

import { ehDataISO } from "@/lib/datas";
import { documentoParaApi, documentoValido, soDigitos } from "@/lib/contatos/documento";
import { ehE164, limparTelefone } from "@/lib/contatos/telefone";
import { BASES_LEGAIS, TIPOS_DE_DOCUMENTO, type ContatoEntrada } from "@/lib/contatos/tipos";

/**
 * Espelho em zod do que o contrato aceita em `ContatoCriar` — validação do lado
 * de cá para o erro chegar no campo certo antes de gastar uma requisição.
 *
 * **Não substitui o servidor**: as mesmas regras existem no DTO do Go, e é lá
 * que elas valem. O que se ganha aqui é a mensagem em português apontando para o
 * campo que a pessoa errou, na hora em que ela ainda está com o documento na
 * mão.
 *
 * **Todo campo é `string` no formulário**, inclusive a caixa de marcação de
 * opt-in (que é `boolean`, a única exceção). É a convenção de `lib/acoes/campos`
 * e existe para o formulário falar a língua do `<input>`: a conversão para o DTO
 * acontece num lugar só, em `contatoParaEntrada()`, na fronteira.
 */

/**
 * Campo de texto que aceita vazio.
 *
 * Sem `.default("")` de propósito: um `default` faz o **tipo de entrada** do
 * schema virar opcional enquanto o de saída continua obrigatório, e aí o
 * `zodResolver` deixa de casar com o `useForm<ContatoFormulario>` — o formulário
 * passaria a precisar de três parâmetros de tipo para não reclamar. Quem garante
 * que o campo existe é `valoresDoContato()`, que monta o estado inicial inteiro.
 */
const TEXTO_OPCIONAL = z.string().trim();

export const ContatoFormulario = z
  .object({
    name: z.string().trim().min(2, "Informe ao menos duas letras.").max(200, "No máximo 200 caracteres."),
    email: TEXTO_OPCIONAL,
    /** Com `+` e DDI. Ver `lib/contatos/telefone.ts`: não se adivinha DDI. */
    phone_e164: TEXTO_OPCIONAL,
    doc_type: z.union([z.enum(TIPOS_DE_DOCUMENTO as [string, ...string[]]), z.literal("")]),
    doc_number: TEXTO_OPCIONAL,
    birth_date: TEXTO_OPCIONAL,
    city: TEXTO_OPCIONAL,
    state: TEXTO_OPCIONAL,
    notes: z.string().trim().max(2000, "No máximo 2000 caracteres."),
    lgpd_basis: z.union([z.enum(BASES_LEGAIS as unknown as [string, ...string[]]), z.literal("")]),
    marketing_opt_in: z.boolean(),
    /** `date` no formulário (o `<input type="date">` não tem hora); vira
     *  `date-time` na fronteira, no fuso da casa. */
    consent_at: TEXTO_OPCIONAL,
  })
  .superRefine((v, ctx) => {
    if (v.email !== "" && !/^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/.test(v.email)) {
      ctx.addIssue({ code: "custom", path: ["email"], message: "E-mail inválido." });
    }

    // Formato errado é recusa, nunca normalização silenciosa: `85999990000` pode
    // ser Fortaleza ou outra coisa, e adivinhar o DDI cria dois cadastros da
    // mesma pessoa — que é o que a UNIQUE do telefone existe para impedir.
    if (v.phone_e164 !== "" && !ehE164(limparTelefone(v.phone_e164))) {
      ctx.addIssue({
        code: "custom",
        path: ["phone_e164"],
        message: "Use o formato internacional com DDI: +5585999990000.",
      });
    }

    // Número sem tipo não se valida nem se compara — é o que o contrato diz em
    // `ContatoCriar.doc_number`, e é verdade também para a busca do balcão.
    if (v.doc_number !== "" && v.doc_type === "") {
      ctx.addIssue({ code: "custom", path: ["doc_type"], message: "Escolha o tipo do documento." });
    }
    if (v.doc_number !== "" && v.doc_type !== "") {
      const tipo = v.doc_type as "cpf" | "cnpj" | "passaporte";
      if (!documentoValido(tipo, v.doc_number)) {
        ctx.addIssue({
          code: "custom",
          path: ["doc_number"],
          message:
            tipo === "passaporte"
              ? "Passaporte tem de 5 a 20 letras ou números."
              : `${tipo.toUpperCase()} inválido — confira os dígitos.`,
        });
      }
    }

    if (v.birth_date !== "" && !ehDataISO(v.birth_date)) {
      ctx.addIssue({ code: "custom", path: ["birth_date"], message: "Data inválida." });
    }

    if (v.state !== "" && !/^[A-Za-z]{2}$/.test(v.state)) {
      ctx.addIssue({ code: "custom", path: ["state"], message: "Duas letras: CE, SP, RJ." });
    }

    // A assimetria que a LGPD impõe e que um formulário genérico esconderia:
    // consentimento sem data não é consentimento, é afirmação. O contrato
    // devolve 422 nesse caso; recusar aqui evita a viagem e aponta o campo.
    if (v.marketing_opt_in && v.consent_at === "") {
      ctx.addIssue({
        code: "custom",
        path: ["consent_at"],
        message: "Quem aceitou receber ofertas aceitou em algum dia. Informe a data.",
      });
    }
    if (v.consent_at !== "" && !ehDataISO(v.consent_at)) {
      ctx.addIssue({ code: "custom", path: ["consent_at"], message: "Data inválida." });
    }
  });

export type ContatoFormulario = z.infer<typeof ContatoFormulario>;

/**
 * Formulário → corpo do `POST`/`PUT`.
 *
 * `""` vira `null`: é a distinção que o contrato usa para "campo vazio", e
 * mandar string vazia gravaria um e-mail que existe e não serve para nada.
 *
 * `consent_at` vira instante **no fuso da casa** (`-03:00`, America/Fortaleza,
 * que não tem horário de verão). Sem o deslocamento, "01/08" viraria 31/07 às
 * 21h para quem lê o relatório — a mesma armadilha que `lib/datas.ts` descreve,
 * do lado da escrita.
 */
export function contatoParaEntrada(v: ContatoFormulario): ContatoEntrada {
  const tipo = v.doc_type === "" ? null : (v.doc_type as "cpf" | "cnpj" | "passaporte");
  const documento = v.doc_number === "" || tipo === null ? null : documentoParaApi(tipo, v.doc_number);

  return {
    name: v.name.trim(),
    email: v.email === "" ? null : v.email.toLowerCase(),
    phone_e164: v.phone_e164 === "" ? null : limparTelefone(v.phone_e164),
    doc_type: documento === null ? null : tipo,
    doc_number: documento,
    birth_date: v.birth_date === "" ? null : v.birth_date,
    city: v.city === "" ? null : v.city,
    state: v.state === "" ? null : v.state.toUpperCase(),
    notes: v.notes === "" ? null : v.notes,
    lgpd_basis: v.lgpd_basis === "" ? null : (v.lgpd_basis as ContatoEntrada["lgpd_basis"]),
    marketing_opt_in: v.marketing_opt_in,
    // Desligar o opt-in limpa a data na mesma escrita: revogação não pede
    // segunda chamada, e data de aceite sobrevivente faria o relatório dizer que
    // a pessoa aceitou.
    consent_at: !v.marketing_opt_in || v.consent_at === "" ? null : `${v.consent_at}T12:00:00-03:00`,
  };
}

/**
 * O motivo da anonimização.
 *
 * Obrigatório porque a ação é irreversível — não há dado guardado em lugar
 * nenhum para desfazê-la. O texto vai para `audit_log` e é a única coisa que
 * responde "por que esta ficha está vazia?" seis meses depois; é também o
 * registro do pedido do titular que a ANPD pede em fiscalização.
 */
export const AnonimizacaoFormulario = z.object({
  reason: z
    .string()
    .trim()
    .min(3, "Escreva o motivo — normalmente o pedido do titular, com a data.")
    .max(500, "No máximo 500 caracteres."),
  /** Confirmação digitada. Não vai para a API: existe para o gesto irreversível
   *  custar mais que um clique distraído no fim do dia. */
  confirmacao: z.string().trim(),
});

export type AnonimizacaoFormulario = z.infer<typeof AnonimizacaoFormulario>;

export const PALAVRA_DE_CONFIRMACAO = "ANONIMIZAR";

/** Só dígitos, para a busca por documento — o contrato compara por igualdade
 *  exata e grava CPF/CNPJ sem pontuação. */
export function documentoParaBusca(termo: string): string {
  return soDigitos(termo);
}
