import type {
  CategoriaDeBem,
  DesfechoDeAvaria,
  StatusDeConferencia,
  TipoDeAmbiente,
  TipoDeAvaria,
  UnidadeDeMedida,
} from "@/lib/bens/tipos";

/**
 * O vocabulário fechado do contrato, dito na língua de quem limpa e confere.
 *
 * `Record<…>` e não um `switch`: valor novo no enum do contrato vira erro de
 * compilação aqui, em vez de um rótulo vazio na tela do celular.
 */

export const ROTULO_DO_AMBIENTE: Record<TipoDeAmbiente, string> = {
  quarto: "Quarto",
  banheiro: "Banheiro",
  cozinha: "Cozinha",
  sala: "Sala",
  area_externa: "Área externa",
  lavanderia: "Lavanderia",
  varanda: "Varanda",
  outro: "Outro",
};

export const ROTULO_DA_CATEGORIA: Record<CategoriaDeBem, string> = {
  louca: "Louça",
  talher: "Talheres",
  copo: "Copos e taças",
  cama: "Cama",
  banho: "Banho",
  mobilia: "Mobília",
  eletro: "Eletro",
  utensilio: "Utensílios",
  decoracao: "Decoração",
  outro: "Outro",
};

/** Para o `<select>` do cadastro: o nome por extenso, com o exemplo do caso. */
export const ROTULO_DA_MEDIDA: Record<UnidadeDeMedida, string> = {
  un: "Peça (un) — quase tudo",
  par: "Par — fronhas, chinelos",
  jogo: "Jogo — lençol, jogo de toalhas",
  kg: "Quilo (kg)",
  l: "Litro (l)",
  m: "Metro (m)",
};

/** Ao lado da quantidade: `12 un`, `3 jogos`. */
export function quantidadeComMedida(qtd: number, medida: UnidadeDeMedida | undefined): string {
  const m = medida ?? "un";
  if (m === "par") return `${qtd} ${qtd === 1 ? "par" : "pares"}`;
  if (m === "jogo") return `${qtd} ${qtd === 1 ? "jogo" : "jogos"}`;
  return `${qtd} ${m}`;
}

export const ROTULO_DA_AVARIA: Record<TipoDeAvaria, string> = {
  quebrado: "Quebrado",
  faltando: "Faltando",
  avariado: "Avariado",
  outro: "Outro",
};

export const ROTULO_DO_DESFECHO: Record<DesfechoDeAvaria, string> = {
  reposto: "Reposto",
  consertado: "Consertado",
  cobrado: "Cobrado do hóspede",
  perda_aceita: "Perda aceita pela casa",
  descartado: "Descartado",
};

export const ROTULO_DO_STATUS: Record<StatusDeConferencia, string> = {
  aberta: "Em andamento",
  fechada: "Fechada",
  cancelada: "Cancelada",
};
