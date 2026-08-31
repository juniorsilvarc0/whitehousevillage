/**
 * CPF, CNPJ e passaporte.
 *
 * ## Por que validar dígito verificador no painel
 *
 * Não é para "proteger" nada — o contrato diz que a API recusa CPF inválido com
 * `422`, e é ela quem manda. É para o erro aparecer **no campo**, no momento em
 * que a pessoa está com o documento na mão, em vez de depois de um envio.
 * Documento digitado errado no balcão vira uma ficha que nunca casa com a
 * segunda visita do mesmo hóspede.
 *
 * ## Passaporte não se valida, e é decisão
 *
 * Cada país tem o seu formato. Recusar o que não se sabe validar barraria
 * hóspede estrangeiro no balcão — o custo de aceitar é uma ficha com documento
 * estranho; o de recusar é uma venda perdida.
 */

import type { TipoDeDocumento } from "@/lib/contatos/tipos";

export function soDigitos(valor: string): string {
  return valor.replace(/\D/g, "");
}

/**
 * Dígito verificador do CPF (módulo 11).
 *
 * A rejeição de `111.111.111-11` e afins é obrigatória: todos os onze
 * repetidos passam na conta do módulo 11 e nenhum é CPF de ninguém — é o
 * preenchimento de quem quer atravessar o campo.
 */
export function ehCPF(valor: string): boolean {
  const d = soDigitos(valor);
  if (d.length !== 11 || /^(\d)\1{10}$/.test(d)) return false;

  for (const [tamanho, peso] of [[9, 10], [10, 11]] as const) {
    let soma = 0;
    for (let i = 0; i < tamanho; i++) soma += Number(d[i]) * (peso - i);
    const resto = (soma * 10) % 11 % 10;
    if (resto !== Number(d[tamanho])) return false;
  }
  return true;
}

/** Dígito verificador do CNPJ (módulo 11 com pesos cíclicos 2..9). */
export function ehCNPJ(valor: string): boolean {
  const d = soDigitos(valor);
  if (d.length !== 14 || /^(\d)\1{13}$/.test(d)) return false;

  for (const tamanho of [12, 13]) {
    let soma = 0;
    let peso = tamanho - 7;
    for (let i = 0; i < tamanho; i++) {
      soma += Number(d[i]) * peso;
      peso = peso === 2 ? 9 : peso - 1;
    }
    const resto = soma % 11;
    const esperado = resto < 2 ? 0 : 11 - resto;
    if (esperado !== Number(d[tamanho])) return false;
  }
  return true;
}

/** Passaporte: alfanumérico, 5 a 20 caracteres. É a única checagem possível sem
 *  escolher um país pelo hóspede. */
export function ehPassaporte(valor: string): boolean {
  return /^[A-Za-z0-9]{5,20}$/.test(valor.trim());
}

export function documentoValido(tipo: TipoDeDocumento, valor: string): boolean {
  if (tipo === "cpf") return ehCPF(valor);
  if (tipo === "cnpj") return ehCNPJ(valor);
  return ehPassaporte(valor);
}

/**
 * O que vai para a API: CPF e CNPJ **só com dígitos**, como o contrato manda
 * gravar; passaporte em maiúsculas, sem espaço.
 *
 * Guardar `123.456.789-09` e `12345678909` na mesma coluna é ter a mesma pessoa
 * duas vezes — a busca por `doc_number` é igualdade exata.
 */
export function documentoParaApi(tipo: TipoDeDocumento, valor: string): string {
  if (tipo === "passaporte") return valor.trim().toUpperCase();
  return soDigitos(valor);
}

/** `12345678909` → `123.456.789-09`. Documento é lido em voz alta ao telefone;
 *  a pontuação é o que permite acompanhar sem perder o lugar. */
export function formatarDocumento(tipo: TipoDeDocumento | null, valor: string | null): string {
  if (!valor) return "";
  if (tipo === "cpf") {
    const d = soDigitos(valor);
    if (d.length !== 11) return valor;
    return `${d.slice(0, 3)}.${d.slice(3, 6)}.${d.slice(6, 9)}-${d.slice(9)}`;
  }
  if (tipo === "cnpj") {
    const d = soDigitos(valor);
    if (d.length !== 14) return valor;
    return `${d.slice(0, 2)}.${d.slice(2, 5)}.${d.slice(5, 8)}/${d.slice(8, 12)}-${d.slice(12)}`;
  }
  return valor;
}
