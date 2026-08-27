"use client";

import * as React from "react";

import type { DataISO } from "@/lib/datas";
import { montarFaixas, faixaNoDia, faixasVisiveis, type Faixa } from "@/lib/mapa/faixas";
import type { Janela } from "@/lib/mapa/janela";
import type { ExpiracaoDeHold, GrupoDoMapa, LinhaDaGrade } from "@/lib/mapa/tipos";
import { indicesDe, intervaloVisivel } from "@/lib/mapa/virtualizacao";
import { cn } from "@/lib/utils";

import { CabecalhoDeDias } from "./cabecalho-de-dias";
import { ColunaDeFundo, FaixaDaGrade, resumoDaLinha } from "./celula";

import "./mapa.css";

/**
 * A grade. É aqui que o custo do mapa é decidido.
 *
 * ## Quatro decisões de desempenho, e o que cada uma evita
 *
 * 1. **Virtualização de coluna.** 366 dias × 9 linhas são 3.294 células de
 *    fundo. Só as colunas dentro da janela de rolagem (+ overscan) viram nó, o
 *    que trava o custo em ~40 colunas independentemente do filtro escolhido.
 *    As linhas **não** são virtualizadas de propósito: são nove, e virtualizar
 *    nove custaria mais código do que economiza.
 *
 * 2. **Ocupação em faixas, não em células.** Uma estadia de doze noites é UMA
 *    barra atravessando doze colunas — não doze retângulos. O nome do hóspede é
 *    escrito uma vez, e só as pontas reais da estadia são arredondadas.
 *
 * 3. **Um ouvinte de ponteiro por LINHA, não por célula.** O dia sob o cursor
 *    sai de uma divisão (`x / largura_da_coluna`), não de um alvo de evento.
 *    São 9 ouvintes em vez de 3.294 — e é o que faz o arrasto que cria bloqueio
 *    funcionar sem que cada dia precise saber quem é.
 *
 * 4. **Rolar não recalcula ocupação.** As faixas são memoizadas contra
 *    `grupos`; a rolagem só muda quais índices são pintados. Um `scroll` de
 *    ponta a ponta não roda `montarFaixas` uma única vez.
 */

/** Fallback quando ainda não há layout (primeiro render, e o jsdom do teste,
 *  que não implementa medição). 2,25rem a 16px de raiz. */
const DIA_PX_PADRAO = 36;

export type AlvoDaFaixa = { linha: LinhaDaGrade; faixa: Faixa; indice: number };
export type AlvoDoDia = { linha: LinhaDaGrade; indice: number };
export type Selecao = { linha: LinhaDaGrade; inicio: number; fim: number };

export function GradeDoMapa({
  janela,
  grupos,
  expiracoes,
  hoje,
  podeBloquear,
  aoApontar,
  aoAbrirFaixa,
  aoSelecionar,
  className,
}: {
  janela: Janela;
  grupos: readonly GrupoDoMapa[];
  expiracoes: ExpiracaoDeHold;
  hoje: DataISO;
  podeBloquear: boolean;
  /** `null` quando o ponteiro sai da grade — é o que apaga o inspetor. */
  aoApontar: (alvo: AlvoDoDia | null) => void;
  aoAbrirFaixa: (alvo: AlvoDaFaixa) => void;
  aoSelecionar: (selecao: Selecao) => void;
  className?: string;
}) {
  const rolagemRef = React.useRef<HTMLDivElement | null>(null);
  const [scrollLeft, setScrollLeft] = React.useState(0);
  const [medidas, setMedidas] = React.useState({ largura: 0, dia: DIA_PX_PADRAO });
  const [apontado, setApontado] = React.useState<{ chave: string; indice: number } | null>(null);
  const [arrasto, setArrasto] = React.useState<{ chave: string; ancora: number; atual: number } | null>(null);

  // Espelhos dos dois estados acima, para os tratadores de ponteiro decidirem
  // SEM ler estado no meio de um updater — updater tem de ser puro, e com
  // `reactStrictMode` o React o executa duas vezes para cobrar isso.
  const apontadoRef = React.useRef<{ chave: string; indice: number } | null>(null);
  const arrastoRef = React.useRef<{ chave: string; ancora: number; atual: number } | null>(null);

  const total = janela.dias.length;

  React.useEffect(() => {
    const el = rolagemRef.current;
    // `ResizeObserver` não existe no jsdom. Sem a guarda, todo teste que monta a
    // grade morre aqui — e a grade cai no fallback de medida, que é justamente
    // o caminho que o teste precisa exercitar.
    if (!el || typeof ResizeObserver === "undefined") return;

    function medir() {
      const alvo = rolagemRef.current;
      if (!alvo) return;
      const pista = alvo.querySelector<HTMLElement>("[data-pista]");
      const dia = pista && total > 0 ? pista.offsetWidth / total : 0;
      setMedidas({ largura: alvo.clientWidth, dia: dia > 0 ? dia : DIA_PX_PADRAO });
    }

    medir();
    const observador = new ResizeObserver(medir);
    observador.observe(el);
    return () => observador.disconnect();
  }, [total]);

  // A rolagem é o evento mais frequente da tela. Um `setState` por evento de
  // scroll enfileira mais renders do que o navegador entrega quadros; o
  // `requestAnimationFrame` casa um render com cada quadro, que é o teto útil.
  const quadro = React.useRef<number | null>(null);
  const aoRolar = React.useCallback((evento: React.UIEvent<HTMLDivElement>) => {
    const alvo = evento.currentTarget;
    if (quadro.current !== null) return;
    quadro.current = requestAnimationFrame(() => {
      quadro.current = null;
      setScrollLeft(alvo.scrollLeft);
    });
  }, []);

  React.useEffect(
    () => () => {
      if (quadro.current !== null) cancelAnimationFrame(quadro.current);
    },
    [],
  );

  const { primeiro, ultimo } = intervaloVisivel({
    scrollLeft,
    largura: medidas.largura,
    larguraDoDia: medidas.dia,
    total,
  });
  const indices = React.useMemo(() => indicesDe({ primeiro, ultimo }), [primeiro, ultimo]);

  /**
   * As faixas de todas as linhas, num cálculo só.
   *
   * Fica na raiz, e não dentro de cada linha, porque o gesto de ponteiro
   * precisa saber **antes de decidir** se o clique caiu numa reserva (abre o
   * detalhe) ou num dia livre (começa o arrasto de bloqueio) — e essa decisão é
   * tomada no `onPointerDown` da linha, que não tem como perguntar ao render
   * dela mesma. Um cache aqui é uma fonte; um cache lá seria duas.
   */
  const faixasPorLinha = React.useMemo(() => {
    const mapa = new Map<string, Faixa[]>();
    for (const grupo of grupos) {
      for (const linha of grupo.linhas) mapa.set(linha.chave, montarFaixas(linha.celulas));
    }
    return mapa;
  }, [grupos]);

  const tiposDosDias = React.useMemo(() => {
    // O tipo de data é do DIA, não da unidade: o Réveillon vale para as oito
    // linhas. Ler da primeira linha evita repetir a mesma resolução nove vezes
    // por render — e mantém o cabeçalho pintado igual às pistas.
    const primeira = grupos[0]?.linhas[0]?.celulas;
    return janela.dias.map((_, i) => primeira?.[i]?.date_type ?? "normal");
  }, [grupos, janela.dias]);

  const linhasPorChave = React.useMemo(() => {
    const mapa = new Map<string, LinhaDaGrade>();
    for (const grupo of grupos) for (const linha of grupo.linhas) mapa.set(linha.chave, linha);
    return mapa;
  }, [grupos]);

  /**
   * Os quatro tratadores de ponteiro são **estáveis** e a linha sai de
   * `dataset.linha`, em vez de uma closure por linha.
   *
   * Não é preciosismo: closures novas a cada render fazem o `React.memo` da
   * linha nunca casar — as props mudam de identidade em toda repintura —, e aí
   * mover o mouse repinta as nove linhas inteiras. Com handlers estáveis, o
   * `apontado` muda em duas linhas (a que saiu e a que entrou) e as outras sete
   * nem re-renderizam.
   */
  const aoApontarRef = React.useRef(aoApontar);
  const aoAbrirFaixaRef = React.useRef(aoAbrirFaixa);
  const aoSelecionarRef = React.useRef(aoSelecionar);
  const podeBloquearRef = React.useRef(podeBloquear);
  const faixasRef = React.useRef(faixasPorLinha);
  const diaRef = React.useRef(medidas.dia);

  React.useEffect(() => {
    aoApontarRef.current = aoApontar;
    aoAbrirFaixaRef.current = aoAbrirFaixa;
    aoSelecionarRef.current = aoSelecionar;
    podeBloquearRef.current = podeBloquear;
    faixasRef.current = faixasPorLinha;
    diaRef.current = medidas.dia;
  });

  const linhaDoEvento = React.useCallback(
    (evento: React.PointerEvent<HTMLDivElement>): { linha: LinhaDaGrade; indice: number } | null => {
      const chave = evento.currentTarget.dataset.linha;
      const linha = chave ? linhasPorChave.get(chave) : undefined;
      if (!linha) return null;
      const retangulo = evento.currentTarget.getBoundingClientRect();
      const largura = diaRef.current || DIA_PX_PADRAO;
      const bruto = Math.floor((evento.clientX - retangulo.left) / largura);
      return { linha, indice: Math.min(total - 1, Math.max(0, bruto)) };
    },
    [linhasPorChave, total],
  );

  const aoMoverPonteiro = React.useCallback(
    (evento: React.PointerEvent<HTMLDivElement>) => {
      const alvo = linhaDoEvento(evento);
      if (!alvo) return;
      const { linha, indice } = alvo;

      // Uma coluna tem 36px e o `pointermove` dispara a cada poucos pixels. Sem
      // esta comparação, atravessar uma célula custaria dez repinturas para
      // desenhar exatamente a mesma coisa.
      //
      // A comparação é contra um REF espelhado, e não dentro de um updater de
      // `setState`: com `reactStrictMode` ligado o React chama o updater duas
      // vezes para provar que ele é puro, e um `aoApontar` lá dentro viraria
      // dois avisos por movimento.
      const anterior = apontadoRef.current;
      if (!anterior || anterior.chave !== linha.chave || anterior.indice !== indice) {
        apontadoRef.current = { chave: linha.chave, indice };
        setApontado(apontadoRef.current);
        aoApontarRef.current({ linha, indice });
      }

      const arrastando = arrastoRef.current;
      if (arrastando && arrastando.chave === linha.chave && arrastando.atual !== indice) {
        arrastoRef.current = { ...arrastando, atual: indice };
        setArrasto(arrastoRef.current);
      }
    },
    [linhaDoEvento],
  );

  const aoSairPonteiro = React.useCallback(() => {
    apontadoRef.current = null;
    setApontado(null);
    aoApontarRef.current(null);
  }, []);

  const aoDescerPonteiro = React.useCallback(
    (evento: React.PointerEvent<HTMLDivElement>) => {
      // Botão direito e do meio não desenham seleção: o menu de contexto
      // abriria por cima de um arrasto já começado.
      if (evento.button !== 0) return;
      const alvo = linhaDoEvento(evento);
      if (!alvo) return;
      const { linha, indice } = alvo;

      const faixa = faixaNoDia(faixasRef.current.get(linha.chave) ?? VAZIO, indice);
      if (faixa) {
        aoAbrirFaixaRef.current({ linha, faixa, indice });
        return;
      }
      if (!podeBloquearRef.current || linha.tipo !== "unidade") return;
      evento.currentTarget.setPointerCapture(evento.pointerId);
      arrastoRef.current = { chave: linha.chave, ancora: indice, atual: indice };
      setArrasto(arrastoRef.current);
    },
    [linhaDoEvento],
  );

  const aoSubirPonteiro = React.useCallback(
    (evento: React.PointerEvent<HTMLDivElement>) => {
      const arrastando = arrastoRef.current;
      if (!arrastando || arrastando.chave !== evento.currentTarget.dataset.linha) return;

      arrastoRef.current = null;
      setArrasto(null);

      const linha = linhasPorChave.get(arrastando.chave);
      if (!linha) return;
      // `fim` exclusivo, como toda faixa de estadia: arrastar de 10 a 11
      // bloqueia as noites 10 e 11 e deixa o dia 12 livre para check-in. É a
      // mesma convenção do `period` do banco, e escrevê-la diferente aqui
      // roubaria uma noite de quem opera.
      aoSelecionarRef.current({
        linha,
        inicio: Math.min(arrastando.ancora, arrastando.atual),
        fim: Math.max(arrastando.ancora, arrastando.atual) + 1,
      });
    },
    [linhasPorChave],
  );

  return (
    <div className={cn("mapa min-h-0", className)}>
      <div ref={rolagemRef} className="mapa-rolagem h-full" onScroll={aoRolar}>
        <div className="mapa-conteudo">
          <div className="mapa-rotulo mapa-cabecalho mapa-canto flex items-end px-3 pb-2">
            <span className="text-[0.6875rem] font-medium uppercase tracking-wide text-muted-foreground">
              Unidade
            </span>
          </div>
          <div className="mapa-cabecalho" style={{ width: `calc(var(--mapa-dia) * ${total})` }}>
            <CabecalhoDeDias
              dias={janela.dias}
              indices={indices}
              tipos={tiposDosDias}
              hoje={hoje}
              apontado={apontado?.indice ?? null}
            />
          </div>

          {grupos.map((grupo) => (
            <React.Fragment key={grupo.unitTypeId}>
              <div className="mapa-rotulo flex items-center border-b border-border/40 bg-muted/25 px-3 py-1.5">
                <span className="font-display truncate text-xs text-muted-foreground">{grupo.nome}</span>
              </div>
              <div
                className="border-b border-border/40 bg-muted/25"
                style={{ width: `calc(var(--mapa-dia) * ${total})`, height: "1.75rem" }}
                aria-hidden="true"
              />

              {grupo.linhas.map((linha) => (
                <LinhaDaGradeDoMapa
                  key={linha.chave}
                  linha={linha}
                  faixas={faixasPorLinha.get(linha.chave) ?? VAZIO}
                  dias={janela.dias}
                  indices={indices}
                  primeiro={primeiro}
                  ultimo={ultimo}
                  total={total}
                  hoje={hoje}
                  expiracoes={expiracoes}
                  apontado={apontado?.chave === linha.chave ? apontado.indice : null}
                  arrasto={arrasto?.chave === linha.chave ? arrasto : null}
                  podeBloquear={podeBloquear && linha.tipo === "unidade"}
                  aoMoverPonteiro={aoMoverPonteiro}
                  aoSairPonteiro={aoSairPonteiro}
                  aoDescerPonteiro={aoDescerPonteiro}
                  aoSubirPonteiro={aoSubirPonteiro}
                />
              ))}
            </React.Fragment>
          ))}
        </div>
      </div>
    </div>
  );
}

const VAZIO: readonly Faixa[] = [];

type PropsDaLinha = {
  linha: LinhaDaGrade;
  faixas: readonly Faixa[];
  dias: readonly DataISO[];
  indices: readonly number[];
  primeiro: number;
  ultimo: number;
  total: number;
  hoje: DataISO;
  expiracoes: ExpiracaoDeHold;
  apontado: number | null;
  arrasto: { ancora: number; atual: number } | null;
  podeBloquear: boolean;
  aoMoverPonteiro: (evento: React.PointerEvent<HTMLDivElement>) => void;
  aoSairPonteiro: () => void;
  aoDescerPonteiro: (evento: React.PointerEvent<HTMLDivElement>) => void;
  aoSubirPonteiro: (evento: React.PointerEvent<HTMLDivElement>) => void;
};

export const LinhaDaGradeDoMapa = React.memo(function LinhaDaGradeDoMapa({
  linha,
  faixas,
  dias,
  indices,
  primeiro,
  ultimo,
  total,
  hoje,
  expiracoes,
  apontado,
  arrasto,
  podeBloquear,
  aoMoverPonteiro,
  aoSairPonteiro,
  aoDescerPonteiro,
  aoSubirPonteiro,
}: PropsDaLinha) {
  const visiveis = React.useMemo(
    () => faixasVisiveis(faixas, primeiro, ultimo),
    [faixas, primeiro, ultimo],
  );

  const selecao = arrasto
    ? { inicio: Math.min(arrasto.ancora, arrasto.atual), fim: Math.max(arrasto.ancora, arrasto.atual) + 1 }
    : null;

  return (
    <>
      <div className="mapa-rotulo flex items-center gap-2 border-b border-border/40 px-3">
        <span className="font-mono text-[0.6875rem] font-medium text-foreground">{linha.codigo}</span>
        <span className="min-w-0 flex-1 truncate text-[0.6875rem] text-muted-foreground">{linha.nome}</span>
      </div>

      <div
        data-pista
        data-linha={linha.chave}
        className="mapa-pista border-b border-border/40"
        style={{ width: `calc(var(--mapa-dia) * ${total})`, touchAction: podeBloquear ? "pan-y" : undefined }}
        onPointerMove={aoMoverPonteiro}
        onPointerLeave={aoSairPonteiro}
        onPointerDown={aoDescerPonteiro}
        onPointerUp={aoSubirPonteiro}
        onPointerCancel={aoSubirPonteiro}
        role="img"
        aria-label={resumoDaLinha(`${linha.codigo} ${linha.nome}`, faixas, dias)}
      >
        {indices.map((i) => (
          <ColunaDeFundo
            key={dias[i] ?? i}
            indice={i}
            tipo={linha.celulas[i]?.date_type ?? "normal"}
            hoje={dias[i] === hoje}
            apontada={apontado === i}
          />
        ))}

        {visiveis.map((faixa) => (
          <FaixaDaGrade
            key={faixa.chave}
            faixa={faixa}
            expiraEm={faixa.reservationId ? expiracoes.get(faixa.reservationId) : null}
          />
        ))}

        {selecao ? (
          <div
            className="mapa-selecao"
            style={{
              left: `calc(var(--mapa-dia) * ${selecao.inicio})`,
              width: `calc(var(--mapa-dia) * ${selecao.fim - selecao.inicio})`,
            }}
          />
        ) : null}
      </div>
    </>
  );
});
