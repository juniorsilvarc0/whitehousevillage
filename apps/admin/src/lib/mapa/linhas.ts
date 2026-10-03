import type { Produto, UnidadeDaComposicao } from "@/lib/api/comercial";
import type { DataISO } from "@/lib/datas";

import { indiceDoDia, type Janela } from "./janela";
import type {
  CelulaDoMapa,
  CelulaSintetica,
  GrupoDoMapa,
  LinhaDaGrade,
  LinhaDoMapa,
  StatusDaCelula,
} from "./tipos";

/**
 * Das respostas da API para as linhas que a grade desenha.
 *
 * Duas transformações moram aqui, e as duas são decisões de produto:
 *
 * 1. **Agrupar as 8 unidades por produto** — a gestão não pensa em "AP-01,
 *    AP-02, AP-03", pensa em "os apartamentos". O agrupamento sai da
 *    composição (`unit_type_members`), que é dado, e não de um prefixo do
 *    código: `AP-` funcionar hoje é coincidência de nomenclatura, e o dia em
 *    que a casa ganhar um chalé o prefixo cala.
 *
 * 2. **Derivar a linha da White House Completa** em vez de buscá-la. Ela não é
 *    a nona unidade: é a leitura das outras oito. Buscá-la de um segundo
 *    endpoint criaria a possibilidade de a linha sintética discordar das oito
 *    linhas desenhadas logo acima dela, na mesma tela, no mesmo instante — e
 *    "a casa está livre" com um apartamento vendido à vista é o pior erro que
 *    este mapa pode cometer.
 */

const LIVRES: readonly StatusDaCelula[] = ["livre", "checked_out"];

function celulaVazia(date: DataISO): CelulaDoMapa {
  return {
    date,
    status: "livre",
    // `normal` é o fundo mais neutro da escala de precedência: uma célula que a
    // resposta não cobriu não pode se pintar de Réveillon.
    date_type: "normal",
    stay_block_id: null,
    reservation_id: null,
    reservation_code: null,
    guest_name: null,
  };
}

/**
 * Alinha as células da resposta à janela desenhada, por **data**, e não por
 * posição no array.
 *
 * A API devolve exatamente o intervalo pedido; confiar nisso é o que quebra
 * durante o tempo real. O refetch disparado por um evento SSE pode voltar com
 * a janela que o usuário tinha meio segundo atrás (ele rolou o mapa enquanto a
 * requisição estava no ar) e, alinhado por índice, o dia 12 apareceria pintado
 * com a ocupação do dia 9 — desalinhamento silencioso, o defeito mais caro
 * possível numa grade.
 */
export function alinharCelulas(dias: readonly CelulaDoMapa[], janela: Janela): CelulaDoMapa[] {
  const alinhadas = janela.dias.map(celulaVazia);
  for (const dia of dias) {
    const i = indiceDoDia(janela, dia.date);
    if (i >= 0) alinhadas[i] = dia;
  }
  return alinhadas;
}

type Composicoes = Readonly<Record<string, readonly UnidadeDaComposicao[]>>;

/**
 * Monta os grupos na ordem do catálogo comercial (`sort_order`), com as
 * unidades na ordem de `code` — a mesma ordem em que o servidor insere as
 * linhas de `stay_blocks`, para a tela falar a língua do resto do sistema.
 *
 * Uma unidade pertence a mais de um produto de propósito (AP-01 é Apartamento
 * 2 Suítes **e** White House Completa). Como linha da grade ela aparece uma vez
 * só, no primeiro produto `one_member` que a contém; a Completa não a repete,
 * porque a Completa é a linha derivada.
 */
export function montarGrupos({
  produtos,
  composicoes,
  linhas,
  janela,
}: {
  produtos: readonly Produto[];
  composicoes: Composicoes;
  linhas: readonly LinhaDoMapa[];
  janela: Janela;
}): GrupoDoMapa[] {
  const porUnidade = new Map<string, LinhaDoMapa>();
  for (const linha of linhas) porUnidade.set(linha.unit_id, linha);

  const celulasPorUnidade = new Map<string, CelulaDoMapa[]>();
  for (const linha of linhas) celulasPorUnidade.set(linha.unit_id, alinharCelulas(linha.days, janela));

  const ordenados = [...produtos]
    .filter((p) => p.active)
    .sort((a, b) => a.sort_order - b.sort_order || a.code.localeCompare(b.code, "pt-BR"));

  const jaUsadas = new Set<string>();
  const grupos: GrupoDoMapa[] = [];

  for (const produto of ordenados.filter((p) => p.consumes === "one_member")) {
    const membros = [...(composicoes[produto.id] ?? [])].sort((a, b) =>
      a.unit_code.localeCompare(b.unit_code, "pt-BR"),
    );

    const linhasDoGrupo: LinhaDaGrade[] = [];
    for (const membro of membros) {
      if (jaUsadas.has(membro.unit_id)) continue;
      const original = porUnidade.get(membro.unit_id);
      if (!original) continue;
      jaUsadas.add(membro.unit_id);
      linhasDoGrupo.push({
        chave: membro.unit_id,
        tipo: "unidade",
        codigo: original.unit_code,
        nome: original.unit_name,
        unitId: membro.unit_id,
        unitTypeId: produto.id,
        celulas: celulasPorUnidade.get(membro.unit_id) ?? janela.dias.map(celulaVazia),
      });
    }

    if (linhasDoGrupo.length > 0) {
      grupos.push({ unitTypeId: produto.id, nome: produto.name, consumes: produto.consumes, linhas: linhasDoGrupo });
    }
  }

  // Unidade que não está na composição de nenhum produto vendável continua
  // ocupável (manutenção, uso do proprietário) e por isso continua no mapa. O
  // grupo tem nome próprio porque é um aviso de inventário incompleto, não uma
  // categoria comercial.
  const orfas = linhas
    .filter((linha) => !jaUsadas.has(linha.unit_id))
    .sort((a, b) => a.unit_code.localeCompare(b.unit_code, "pt-BR"));

  if (orfas.length > 0) {
    grupos.push({
      unitTypeId: "sem-produto",
      nome: "Fora de qualquer produto",
      consumes: "one_member",
      linhas: orfas.map((linha) => ({
        chave: linha.unit_id,
        tipo: "unidade" as const,
        codigo: linha.unit_code,
        nome: linha.unit_name,
        unitId: linha.unit_id,
        unitTypeId: null,
        celulas: celulasPorUnidade.get(linha.unit_id) ?? janela.dias.map(celulaVazia),
      })),
    });
  }

  for (const produto of ordenados.filter((p) => p.consumes === "all_members")) {
    const membros = composicoes[produto.id] ?? [];
    const celulasDosMembros = membros
      .map((m) => ({ codigo: m.unit_code, celulas: celulasPorUnidade.get(m.unit_id) }))
      .filter((m): m is { codigo: string; celulas: CelulaDoMapa[] } => Boolean(m.celulas));

    if (celulasDosMembros.length === 0) continue;

    grupos.push({
      unitTypeId: produto.id,
      nome: produto.name,
      consumes: produto.consumes,
      linhas: [
        {
          chave: `sintetica:${produto.id}`,
          tipo: "sintetica",
          codigo: produto.code,
          nome: produto.name,
          unitId: null,
          unitTypeId: produto.id,
          celulas: derivarLinhaSintetica(celulasDosMembros, janela),
        },
      ],
    });
  }

  return grupos;
}

/**
 * A linha da casa inteira, dia a dia, a partir das unidades que a compõem.
 *
 * Três desfechos, nesta ordem:
 *
 * 1. **As oito contam a mesma história** (mesmo status e mesma reserva) — a
 *    casa foi vendida, bloqueada ou cumprida como um todo, e a linha mostra
 *    isso, com o código da reserva.
 * 2. **Todas vendáveis** (`livre` ou `checked_out`) — a casa está livre.
 *    `checked_out` conta como livre aqui de propósito: a estadia terminou e a
 *    data voltou ao estoque, que é a razão de `completed` estar fora da
 *    `EXCLUDE` do banco. Contá-la como ocupada fecharia para a venda uma noite
 *    que a venda aceita.
 * 3. **Parte ocupada** — `parcial`, com o nome das unidades que impedem. É a
 *    resposta que a gestão precisa antes de prometer a casa a um evento: não
 *    "indisponível", mas "a SP-02 está vendida".
 */
export function derivarLinhaSintetica(
  membros: readonly { codigo: string; celulas: readonly CelulaDoMapa[] }[],
  janela: Janela,
): CelulaSintetica[] {
  return janela.dias.map((data, i) => {
    const celulas = membros.map((m) => m.celulas[i] ?? celulaVazia(data));
    const primeira = celulas[0] ?? celulaVazia(data);

    const mesmaHistoria =
      celulas.every((c) => c.status === primeira.status) &&
      celulas.every((c) => c.reservation_id === primeira.reservation_id);

    if (mesmaHistoria) {
      return {
        ...primeira,
        date: data,
        // `stay_block_id` é nulo mesmo quando as unidades têm um: são oito
        // blocos, e uma faixa da linha sintética não representa nenhum deles.
        // Quem solta a data é o bloco da unidade, na linha da unidade.
        stay_block_id: null,
        bloqueadaPor: [],
      };
    }

    const impedem = celulas
      .map((c, indice) => ({ status: c.status, codigo: membros[indice]?.codigo ?? "" }))
      .filter((c) => !LIVRES.includes(c.status))
      .map((c) => c.codigo);

    if (impedem.length === 0) {
      return { ...celulaVazia(data), date_type: primeira.date_type, bloqueadaPor: [] };
    }

    return {
      date: data,
      status: "parcial",
      date_type: primeira.date_type,
      stay_block_id: null,
      reservation_id: null,
      reservation_code: null,
      guest_name: null,
      bloqueadaPor: impedem,
    };
  });
}

/** Esconde as linhas sem nenhuma ocupação na janela — o filtro "só ocupadas". */
export function filtrarOcupadas(grupos: readonly GrupoDoMapa[]): GrupoDoMapa[] {
  return grupos
    .map((grupo) => ({
      ...grupo,
      linhas: grupo.linhas.filter((linha) => linha.celulas.some((c) => c.status !== "livre")),
    }))
    .filter((grupo) => grupo.linhas.length > 0);
}

export function filtrarPorProduto(
  grupos: readonly GrupoDoMapa[],
  unitTypeId: string | null,
): GrupoDoMapa[] {
  if (!unitTypeId) return [...grupos];
  return grupos.filter((grupo) => grupo.unitTypeId === unitTypeId);
}
