"use client";

import * as React from "react";
import { usePathname, useRouter } from "next/navigation";

import { EstadoVazio } from "@/components/layout/estados";
import { IndicadorDeTempoReal } from "@/components/tempo-real/indicador";
import type { DisponibilidadeDoProduto, Produto, UnidadeDaComposicao } from "@/lib/api/comercial";
import { mensagemDoErro, type Resultado } from "@/lib/acoes/resultado";
import { somarDias, type DataISO } from "@/lib/datas";
import { janelaDosFiltros, paraQueryString, type FiltrosDoMapa } from "@/lib/mapa/filtros";
import { filtrarOcupadas, filtrarPorProduto, montarGrupos } from "@/lib/mapa/linhas";
import { chaveDePreco, type DadosDoMapa, type IndiceDePrecos, type PrecoDaNoite } from "@/lib/mapa/tipos";
import { useAtualizacaoAoVivo } from "@/lib/tempo-real/atualizacao";
import type { EventoDoStream } from "@/lib/tempo-real/eventos";
import { cn } from "@/lib/utils";

import { BarraDoMapa } from "./barra";
import { ModalDeBloqueio, type PedidoDeBloqueio } from "./bloqueio";
import { DetalheDaOcupacao } from "./detalhe";
import { GradeDoMapa, type AlvoDaFaixa, type AlvoDoDia, type Selecao } from "./grade";
import { InspetorDoDia } from "./inspetor";
import { LegendaDoMapa } from "./legenda";

import "./mapa.css";

/**
 * O mapa de ocupação — a tela que a gestão olha o dia inteiro.
 *
 * ## Onde cada estado mora, e por quê
 *
 * | Estado | Onde | Motivo |
 * |---|---|---|
 * | Filtros (faixa, produto) | **query string** | é o que torna a tela um link compartilhável, e devolve o botão Voltar |
 * | Ocupação | estado local, semeado pelo servidor | é o que o evento de tempo real repõe sem refazer a árvore de RSC |
 * | Produtos, composição, tarifa | props do servidor | não mudam por venda; refazê-las a cada evento seriam três requisições a mais por nada |
 * | Rolagem, hover, arrasto | estado da grade | some com a tela e não pertence a mais ninguém |
 *
 * Mudar filtro é navegação (`router.replace`), e não `setState`: a faixa nova
 * precisa de dados novos do servidor de qualquer forma, e passar pela URL é o
 * que mantém "o que estou vendo" e "o que o link mostra" sendo a mesma coisa.
 *
 * ## O evento chega magro, e é o que o mantém correto
 *
 * O `data:` do SSE traz `{entity, id, v}` e nada mais. A tela refaz o fetch
 * autenticado da faixa visível — que é onde o RBAC do usuário é aplicado, com o
 * mesmo `WHERE` e o mesmo escopo do `GET` de sempre. Um evento que carregasse o
 * dado seria um segundo caminho de leitura, com uma segunda chance de errar a
 * permissão. E como não há verbo (`created`/`deleted`), criação, alteração e
 * remoção têm o mesmo tratamento: um código de cliente só.
 */
export type PermissoesDoMapa = { criar: boolean; excluir: boolean };

export function MapaDeOcupacao({
  filtros,
  produtos,
  composicoes,
  dadosIniciais,
  precos,
  hoje,
  permissoes,
  aoBuscarOcupacao,
  aoCriarBloqueio,
  aoLiberarBloqueio,
}: {
  filtros: FiltrosDoMapa;
  produtos: readonly Produto[];
  composicoes: Readonly<Record<string, readonly UnidadeDaComposicao[]>>;
  dadosIniciais: DadosDoMapa;
  precos: readonly DisponibilidadeDoProduto[];
  hoje: DataISO;
  permissoes: PermissoesDoMapa;
  aoBuscarOcupacao: (from: string, dias: number) => Promise<Resultado<DadosDoMapa>>;
  aoCriarBloqueio: (valores: {
    unit_id: string;
    from: string;
    to: string;
    source: "maintenance" | "owner_hold";
    note?: string;
  }) => Promise<Resultado<null>>;
  aoLiberarBloqueio: (stayBlockId: string) => Promise<Resultado<null>>;
}) {
  const router = useRouter();
  const caminho = usePathname();
  const [navegando, iniciarNavegacao] = React.useTransition();

  const [dados, setDados] = React.useState(dadosIniciais);
  // Ajuste de estado durante o render — a forma que o React documenta para
  // "prop nova manda". Um efeito faria a tela desenhar uma vez com a ocupação
  // da faixa anterior antes de se corrigir, e num mapa isso é um piscar de
  // dados errados a cada troca de filtro.
  const [semente, setSemente] = React.useState(dadosIniciais);
  if (semente !== dadosIniciais) {
    setSemente(dadosIniciais);
    setDados(dadosIniciais);
  }

  const janela = React.useMemo(() => janelaDosFiltros(filtros), [filtros]);

  const aoVivo = useAtualizacaoAoVivo<DadosDoMapa>({
    topicos: ["calendar"],
    interessa: React.useCallback(
      (evento: EventoDoStream) => evento.entity === "stay_block" || evento.entity === "reservation",
      [],
    ),
    buscar: React.useCallback(
      () => aoBuscarOcupacao(filtros.from, filtros.dias),
      [aoBuscarOcupacao, filtros.from, filtros.dias],
    ),
    aoAtualizar: setDados,
  });

  const grupos = React.useMemo(() => {
    const todos = montarGrupos({ produtos, composicoes, linhas: dados.linhas, janela });
    const porProduto = filtrarPorProduto(todos, filtros.unitTypeId);
    return filtros.somenteOcupadas ? filtrarOcupadas(porProduto) : porProduto;
  }, [produtos, composicoes, dados.linhas, janela, filtros.unitTypeId, filtros.somenteOcupadas]);

  const indiceDePrecos: IndiceDePrecos = React.useMemo(() => {
    const mapa = new Map<string, PrecoDaNoite>();
    for (const produto of precos) {
      for (const dia of produto.days) {
        mapa.set(chaveDePreco(produto.unit_type_id, dia.date), {
          price_cents: dia.price_cents,
          date_type: dia.date_type,
          min_nights: dia.min_nights,
          available: dia.available,
        });
      }
    }
    return mapa;
  }, [precos]);

  const expiracoes = React.useMemo(
    () => new Map(Object.entries(dados.expiracoes)),
    [dados.expiracoes],
  );

  const [apontado, setApontado] = React.useState<AlvoDoDia | null>(null);
  const [faixaAberta, setFaixaAberta] = React.useState<AlvoDaFaixa | null>(null);
  const [pedido, setPedido] = React.useState<PedidoDeBloqueio | null>(null);

  function navegar(novos: FiltrosDoMapa) {
    // `replace`, não `push`: rolar a temporada mês a mês encheria o histórico
    // de trinta entradas e o botão Voltar deixaria de significar "a tela
    // anterior". O link continua compartilhável do mesmo jeito.
    iniciarNavegacao(() => router.replace(`${caminho}${paraQueryString(novos, hoje)}`, { scroll: false }));
  }

  function abrirBloqueio(selecao: Selecao) {
    if (!selecao.linha.unitId) return;
    const de = janela.dias[selecao.inicio];
    if (!de) return;
    // `fim` é índice exclusivo; a data exclusiva é o dia seguinte ao último
    // arrastado, que pode cair fora da janela desenhada.
    const ate = janela.dias[selecao.fim] ?? somarDias(janela.from, selecao.fim);
    setPedido({
      unitId: selecao.linha.unitId,
      unitCode: selecao.linha.codigo,
      unitName: selecao.linha.nome,
      from: de,
      to: ate,
    });
  }

  function abrirBloqueioPeloTeclado() {
    const primeira = grupos.flatMap((grupo) => grupo.linhas).find((linha) => linha.unitId);
    if (!primeira?.unitId) return;
    setPedido({
      unitId: primeira.unitId,
      unitCode: primeira.codigo,
      unitName: primeira.nome,
      from: hoje,
      to: somarDias(hoje, 1),
    });
  }

  const vazio = grupos.length === 0;

  return (
    <div className="altura-da-tela-cheia flex min-h-0 flex-col gap-3 p-4 sm:p-6 lg:px-8">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h1 className="font-display text-2xl leading-tight sm:text-3xl">Mapa de ocupação</h1>
          <p className="mt-1 max-w-prose text-sm text-muted-foreground">
            Os apartamentos e a casa inteira, noite a noite. A cor de fundo mostra o tipo de data (que define
            o preço); a barra mostra quem está na casa. Arraste sobre dias livres para bloquear.
          </p>
        </div>
      </header>

      <BarraDoMapa
        filtros={filtros}
        produtos={produtos}
        podeBloquear={permissoes.criar}
        aoMudar={navegar}
        aoBloquear={abrirBloqueioPeloTeclado}
        indicador={
          <IndicadorDeTempoReal
            estado={aoVivo.tempoReal.estado}
            atualizando={aoVivo.atualizando || navegando}
            atualizadoEm={aoVivo.atualizadoEm ?? Date.parse(dados.buscadoEm)}
            aoReconectar={aoVivo.tempoReal.reconectar}
          />
        }
      />

      {aoVivo.falha ? (
        <p
          role="status"
          className="rounded-lg border border-alcada-atencao/40 bg-alcada-atencao/10 px-3 py-2 text-xs text-foreground"
        >
          A última atualização automática falhou ({mensagemDoErro(aoVivo.falha)}) — o mapa pode estar
          desatualizado. Use o botão de recarregar da barra.
        </p>
      ) : null}

      {vazio ? (
        <EstadoVazio
          titulo="Nenhuma linha para mostrar"
          descricao={
            filtros.somenteOcupadas
              ? "Nenhuma unidade tem ocupação nesta faixa. Desmarque “só linhas com ocupação” para ver o calendário inteiro."
              : "Não há apartamentos ativos, ou o produto escolhido não tem apartamentos definidos. Confira o Inventário."
          }
        />
      ) : (
        <GradeDoMapa
          className={cn(
            "min-h-0 flex-1 overflow-hidden rounded-xl border border-border/60 bg-card",
            navegando && "opacity-70 transition-opacity",
          )}
          janela={janela}
          grupos={grupos}
          expiracoes={expiracoes}
          hoje={hoje}
          podeBloquear={permissoes.criar}
          aoApontar={setApontado}
          aoAbrirFaixa={setFaixaAberta}
          aoSelecionar={abrirBloqueio}
        />
      )}

      <InspetorDoDia alvo={apontado} janela={janela} precos={indiceDePrecos} expiracoes={expiracoes} />

      <LegendaDoMapa />

      <ModalDeBloqueio
        pedido={pedido}
        aoFechar={() => setPedido(null)}
        aoGravar={async (valores) => {
          const resultado = await aoCriarBloqueio(valores);
          // A gravação é a única mudança que a tela sabe ter causado. Não
          // esperar pelo evento do barramento aqui é o que faz o bloqueio
          // aparecer no instante do clique — o evento chega logo depois e o
          // `v` já visto o descarta, sem repintar duas vezes.
          if (resultado.ok) aoVivo.atualizarAgora();
          return resultado;
        }}
      />

      <DetalheDaOcupacao
        alvo={faixaAberta}
        janela={janela}
        expiracoes={expiracoes}
        podeLiberar={permissoes.excluir}
        aoFechar={() => setFaixaAberta(null)}
        aoLiberar={async (id) => {
          const resultado = await aoLiberarBloqueio(id);
          if (resultado.ok) aoVivo.atualizarAgora();
          return resultado;
        }}
      />
    </div>
  );
}
