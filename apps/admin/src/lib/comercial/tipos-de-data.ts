import type { TipoDeData, TipoDePeriodo } from "@/lib/api/comercial";

/**
 * Os seis tipos de data e a **precedência** que decide qual vale numa noite.
 *
 * A precedência mora no banco (`date_type_rules`) — é dado, e mudá-la é uma
 * linha, não um deploy. O que está aqui é a tradução para a tela: o rótulo, a
 * regra em uma frase e a ordem. Fica em ordem **decrescente de precedência**
 * porque é assim que a tela precisa mostrar: é a leitura de cima para baixo que
 * explica por que 31/12 custa réveillon e não fim de semana, mesmo caindo numa
 * sexta.
 *
 * Os números repetem os da spec §3 de propósito, para a tela poder mostrá-los;
 * se o banco divergir, quem vale é o banco, e a divergência aparece na hora de
 * conferir uma tarifa.
 */
export type DescricaoDoTipo = {
  code: TipoDeData;
  label: string;
  precedencia: number;
  regra: string;
};

export const TIPOS_DE_DATA: readonly DescricaoDoTipo[] = [
  { code: "reveillon", label: "Réveillon",     precedencia: 100, regra: "período especial cadastrado" },
  { code: "carnaval",  label: "Carnaval",      precedencia: 100, regra: "período especial cadastrado" },
  { code: "feriado",   label: "Feriado",       precedencia: 80,  regra: "data na lista de feriados" },
  { code: "alta",      label: "Alta temporada", precedencia: 60, regra: "período especial de alta" },
  { code: "fds",       label: "Fim de semana", precedencia: 40,  regra: "sexta ou sábado" },
  { code: "normal",    label: "Normal",        precedencia: 0,   regra: "todas as demais noites" },
] as const;

const POR_CODIGO = new Map(TIPOS_DE_DATA.map((t) => [t.code, t]));

export function descricaoDoTipo(code: TipoDeData): DescricaoDoTipo {
  return POR_CODIGO.get(code) ?? { code, label: code, precedencia: 0, regra: "" };
}

export function rotuloDoTipo(code: TipoDeData): string {
  return descricaoDoTipo(code).label;
}

/**
 * Classe da etiqueta do tipo de data.
 *
 * A escala é de **precedência**, não de gosto: quanto mais forte o tipo, mais
 * cheia a etiqueta. Tudo em token da marca — cor solta no meio do componente é
 * anti-padrão da casca.
 */
export function classeDoTipo(code: TipoDeData): string {
  switch (descricaoDoTipo(code).precedencia) {
    case 100:
      return "bg-primary text-primary-foreground";
    case 80:
      return "bg-accent text-accent-foreground";
    case 60:
      return "bg-secondary text-secondary-foreground";
    case 40:
      return "bg-muted text-muted-foreground";
    default:
      return "border border-border text-muted-foreground";
  }
}

/** Que tipo de data um período especial passa a valer nas noites que cobre. */
export const TIPOS_DE_PERIODO: readonly { code: TipoDePeriodo; label: string; nota: string }[] = [
  { code: "reveillon", label: "Réveillon",      nota: "precedência 100 — vence tudo" },
  { code: "carnaval",  label: "Carnaval",       nota: "precedência 100 — vence tudo" },
  { code: "alta",      label: "Alta temporada", nota: "precedência 60 — perde para feriado" },
  { code: "evento",    label: "Evento",         nota: "faixa marcada no calendário" },
] as const;

export function rotuloDoPeriodo(code: TipoDePeriodo): string {
  return TIPOS_DE_PERIODO.find((p) => p.code === code)?.label ?? code;
}
