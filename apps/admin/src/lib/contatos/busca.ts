import { soDigitos } from "@/lib/contatos/documento";
import { ehE164, limparTelefone } from "@/lib/contatos/telefone";

/**
 * **Uma** caixa de busca, três consultas diferentes.
 *
 * O contrato publica três filtros que não são intercambiáveis, e a tela tem de
 * escolher entre eles antes de gastar a requisição:
 *
 * | Filtro | O que é | O que devolve |
 * |---|---|---|
 * | `phone` | igualdade exata em E.164 | zero ou **um** — é o caminho do WhatsApp |
 * | `doc_number` | igualdade exata, só dígitos | zero ou um — é o caminho do balcão |
 * | `q` | trigram sobre nome e e-mail | a busca humana, aproximada |
 *
 * Obrigar quem atende a escolher o filtro num `<select>` seria transferir para
 * ela uma decisão que o formato do que ela digitou já responde: `+5585…` é
 * telefone, onze dígitos são CPF, "Ana" é nome. Três caixas separadas custariam
 * o mesmo em atenção e ainda ocupariam a barra inteira.
 *
 * ## O que este módulo protege
 *
 * Mandar um telefone incompleto em `phone` é `422 VALIDATION_ERROR` — porque
 * "não achei" e "você perguntou errado" são respostas diferentes, e o inbound
 * que as confunde cria um contato duplicado a cada mensagem. Do lado da tela,
 * porém, quem está digitando `+5585` ainda não terminou de digitar: transformar
 * isso num erro vermelho seria punir o meio da frase. Por isso o `+` só vira
 * consulta por telefone quando o número **já está completo**; enquanto não
 * estiver, o termo desce como `q` e a lista se comporta como busca humana.
 *
 * O mesmo vale para dígitos soltos: `1234` não é CPF nem CNPJ e desce como `q`
 * — pode ser parte de um nome de empresa, e a busca por documento devolveria
 * vazio sem explicar por quê.
 */

export type ConsultaDeContatos = {
  q?: string;
  phone?: string;
  doc_number?: string;
};

/** Como a barra explica, em uma linha, o que vai procurar. */
export type ModoDeBusca = "telefone" | "documento" | "nome" | "vazio";

export function modoDaBusca(termo: string): ModoDeBusca {
  const limpo = termo.trim();
  if (limpo === "") return "vazio";

  if (limpo.startsWith("+")) return ehE164(limparTelefone(limpo)) ? "telefone" : "nome";

  // Só pontuação de documento é tolerada (`123.456.789-09`, `11.222.333/0001-81`).
  // Com isso "Casa 11122233344" continua sendo nome: sobra letra depois da
  // limpeza, e a busca exata devolveria vazio sem dizer por quê.
  const semPontuacao = limpo.replace(/[.\-/\s]/g, "");
  if (/^\d+$/.test(semPontuacao) && (semPontuacao.length === 11 || semPontuacao.length === 14)) {
    return "documento";
  }
  return "nome";
}

export function consultaDeBusca(termo: string): ConsultaDeContatos {
  const limpo = termo.trim();
  switch (modoDaBusca(limpo)) {
    case "vazio":
      return {};
    case "telefone":
      return { phone: limparTelefone(limpo) };
    case "documento":
      return { doc_number: soDigitos(limpo) };
    case "nome":
      // `q` tem `minLength: 2` no contrato. Uma letra só desce como busca vazia
      // em vez de virar 422: quem digitou "A" está começando, não errando.
      return limpo.length >= 2 ? { q: limpo } : {};
  }
}

export const EXPLICACAO_DO_MODO: Record<ModoDeBusca, string> = {
  telefone: "Busca exata por telefone — devolve a pessoa, ou ninguém.",
  // Onze dígitos são CPF **e** celular brasileiro sem DDI. A leitura vai para
  // documento porque é a única das duas que pode achar alguém — a busca por
  // telefone exige E.164 —, e a frase avisa quem quis dizer a outra coisa. É o
  // que faz o palpite se corrigir sozinho, em vez de devolver "não achei" mudo.
  documento: "Busca exata por documento. Se era um telefone, acrescente o DDI: +5585…",
  nome: "Busca por nome ou e-mail.",
  vazio: "Nome, telefone com DDI (+55…) ou documento.",
};
