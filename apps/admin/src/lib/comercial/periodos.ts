import type { PeriodoEspecial } from "@/lib/api/comercial";

/**
 * Quem se sobrepõe a quem, entre os períodos especiais.
 *
 * A sobreposição é **esperada**, não um erro a corrigir: o Réveillon mora
 * dentro da alta temporada, e é a precedência que decide qual tipo a noite
 * recebe. A tabela não tem constraint de exclusão justamente por isso.
 *
 * Mostrar isso na tela evita o reflexo errado — alguém "consertar" o cadastro
 * encolhendo a alta temporada para não encostar no Réveillon, e com isso mudar
 * o preço de todas as noites entre os dois.
 *
 * As pontas são inclusivas nos dois lados (`starts_on..ends_on`), ao contrário
 * da estadia: comparar as strings ISO já dá a ordem cronológica certa.
 */
export function sobreposicoes(periodos: PeriodoEspecial[]): Map<string, PeriodoEspecial[]> {
  const mapa = new Map<string, PeriodoEspecial[]>();

  for (const periodo of periodos) {
    const cruzam = periodos.filter(
      (outro) =>
        outro.id !== periodo.id &&
        outro.active &&
        periodo.active &&
        periodo.starts_on <= outro.ends_on &&
        outro.starts_on <= periodo.ends_on,
    );
    if (cruzam.length > 0) mapa.set(periodo.id, cruzam);
  }

  return mapa;
}
