import type { DisponibilidadeDoProduto, Produto, UnidadeDaComposicao } from "@/lib/api/comercial";
import { carregar, carregarLista } from "@/lib/api/carregar";
import type { Resultado } from "@/lib/acoes/resultado";
import { tentar } from "@/lib/acoes/executar";
import { apiList } from "@/lib/api/client";
import type { Janela } from "@/lib/mapa/janela";
import type { DadosDoMapa, LinhaDoMapa } from "@/lib/mapa/tipos";

/*
 * Módulo de SERVIDOR. Não leva `import "server-only"` porque o pacote não está
 * no `package.json` e esta entrega não mexe em dependência; o que o mantém
 * fora do bundle do navegador é a cadeia real: `@/lib/api/client` importa
 * `next/headers`, que não existe no cliente e quebra o build na hora se alguém
 * importar isto de um componente `"use client"`.
 */

/**
 * As buscas do mapa, separadas pelo que **muda junto**.
 *
 * A separação não é organização: é o que o tempo real cobra. Um evento de
 * calendário significa que uma linha de `stay_blocks` mudou — e só isso. Refazer
 * o catálogo de produtos e a composição a cada venda seriam três requisições
 * extras por evento para receber exatamente as mesmas oito unidades. O que o
 * evento repõe é `carregarOcupacao`, e mais nada.
 *
 * | Busca | Repetida por evento? | Por quê |
 * |---|---|---|
 * | `carregarConfiguracao` | não | produto e composição mudam por tela de inventário, não por venda |
 * | `carregarPrecos` | não | tarifa muda pelo tarifário; `stay_block` não mexe em preço |
 * | `carregarOcupacao` | **sim** | é o que o evento está avisando que mudou |
 */

export type Composicoes = Record<string, UnidadeDaComposicao[]>;

export type Configuracao = {
  produtos: Produto[];
  composicoes: Composicoes;
};

export async function carregarConfiguracao(): Promise<Resultado<Configuracao>> {
  const produtos = await carregarLista<Produto>("/unit-types", { sort: "sort_order" });
  if (!produtos.ok) return produtos;

  const membros = await Promise.all(
    produtos.data.map(async (produto) => ({
      id: produto.id,
      resultado: await carregar<UnidadeDaComposicao[]>(`/unit-types/${produto.id}/members`),
    })),
  );

  const composicoes: Composicoes = {};
  for (const { id, resultado } of membros) {
    // Composição que não carregou vira lista vazia: o produto some do
    // agrupamento e as unidades dele caem em "Fora de composição", que é um
    // aviso visível. Derrubar a tela inteira por causa de um produto seria
    // trocar um mapa incompleto por nenhum mapa.
    composicoes[id] = resultado.ok ? resultado.data : [];
  }

  return { ok: true, data: { produtos: produtos.data, composicoes } };
}

export function carregarPrecos(janela: Janela): Promise<Resultado<DisponibilidadeDoProduto[]>> {
  return carregarLista<DisponibilidadeDoProduto>("/availability", { from: janela.from, to: janela.to });
}

/**
 * A matriz unidade × dia mais o vencimento das pré-reservas.
 *
 * As duas coisas vêm de rotas diferentes porque o contrato as publica em rotas
 * diferentes: `LinhaDoMapa` **não tem `expires_at`**, e a célula `hold` do
 * design system pede contador. A alternativa seria a tela recalcular "48 h a
 * partir da criação" pela política — o que erraria em toda pré-reserva
 * estendida, que é justamente a que alguém está acompanhando. Está anotado no
 * relatório como divergência de contrato.
 *
 * As duas buscas saem em paralelo e a segunda **não derruba a primeira**: sem
 * as expirações o mapa perde o contador, não a ocupação.
 */
export async function carregarOcupacao(janela: Janela): Promise<Resultado<DadosDoMapa>> {
  const [linhas, holds] = await Promise.all([
    carregarLista<LinhaDoMapa>("/availability/units", { from: janela.from, to: janela.to }),
    tentar(async () =>
      (
        await apiList<{ id: string; hold_expires_at: string | null }>("/reservations", {
          query: { status: "hold", from: janela.from, to: janela.to, per_page: 100 },
        })
      ).data,
    ),
  ]);

  if (!linhas.ok) return linhas;

  const expiracoes: Record<string, string> = {};
  if (holds.ok) {
    for (const reserva of holds.data) {
      if (reserva.hold_expires_at) expiracoes[reserva.id] = reserva.hold_expires_at;
    }
  }

  return {
    ok: true,
    data: { linhas: linhas.data, expiracoes, buscadoEm: new Date().toISOString() },
  };
}
