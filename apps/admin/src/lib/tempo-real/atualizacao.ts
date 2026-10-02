"use client";

import * as React from "react";

import type { CodigoDeErro } from "@/lib/api/codigos";

import { useSSE, type CriarFonte, type TempoReal } from "./sse";
import type { EventoDoStream, Topico } from "./eventos";

/**
 * A fatia do resultado que este hook usa — `ok`, `data` e `code`, e mais nada.
 *
 * Tipar só o necessário (mesma razão de `FonteDeEventos` em `sse.ts`) é o que
 * permite servir o mapa, que fala `Resultado` com o vocabulário geral de erro,
 * e o funil, que fala `ResultadoCrm` com sete códigos a mais. A alternativa
 * seria um dos dois traduzir o código para o vocabulário do outro antes de
 * chamar — e a tradução perde a frase específica, que é justamente a que o
 * operador precisa ler para resolver a recusa sozinho.
 */
export type ResultadoDaBusca<T, C extends string> =
  | { ok: true; data: T }
  | { ok: false; code: C };

/**
 * "Chegou aviso, refaz o fetch" — com as três guardas que a versão ingênua não
 * tem.
 *
 * A forma ingênua (`aoEvento: () => recarregar()`) quebra de três jeitos, e os
 * três acontecem no uso normal:
 *
 * 1. **Uma venda da White House Completa emite nove eventos** — oito
 *    `stay_block` e um `reservation`, todos na mesma transação. Nove buscas
 *    para chegar ao mesmo desenho. Daí a **janela de agrupamento**: eventos
 *    dentro de 250 ms viram uma requisição só. O número não é chute — é a
 *    granularidade abaixo da qual o olho não separa dois repintes, e deixa
 *    folga larga dentro do requisito de 2 s.
 *
 * 2. **Duas buscas no ar, a primeira voltando depois da segunda.** A tela
 *    terminaria desenhando o estado mais VELHO, sem nenhum evento novo para
 *    corrigi-la depois. A guarda é a trava `noAr`: existe **uma** requisição no
 *    ar por vez, e é ela que dá a ordem. Um selo de sequência seria a solução
 *    para o caso concorrente — mas com a trava o caso concorrente não existe, e
 *    duas defesas para o mesmo buraco são a que ninguém mantém.
 *
 * 3. **A rajada durante o voo.** Evento que chega enquanto uma busca acontece
 *    não pode ser perdido nem abrir uma busca paralela: fica marcado em
 *    `pendente` e vira exatamente mais uma volta do laço.
 */
export type AtualizacaoAoVivo<C extends string = CodigoDeErro> = {
  tempoReal: TempoReal;
  /** Uma busca está no ar. A tela sinaliza discretamente — **nunca** com
   *  skeleton: trocar o mapa por um esqueleto a cada evento é pior do que não
   *  atualizar, porque perde o lugar para onde a pessoa estava olhando. */
  atualizando: boolean;
  /** Código da última falha de atualização, ou `null`. O desenho na tela
   *  continua sendo o último que deu certo — dado velho e rotulado como velho é
   *  melhor que tela vazia. */
  falha: C | null;
  /** Instante da última atualização bem-sucedida. */
  atualizadoEm: number | null;
  atualizarAgora: () => void;
};

export const JANELA_DE_AGRUPAMENTO_MS = 250;

export function useAtualizacaoAoVivo<T, C extends string = CodigoDeErro>({
  topicos,
  interessa,
  buscar,
  aoAtualizar,
  habilitado = true,
  criarFonte,
  janelaMs = JANELA_DE_AGRUPAMENTO_MS,
}: {
  topicos: readonly Topico[];
  /** Filtro barato antes de gastar uma requisição. O mapa só se importa com
   *  `stay_block` e `reservation`; uma oportunidade mudando na mesma conexão não
   *  pode custar um fetch de disponibilidade. */
  interessa: (evento: EventoDoStream) => boolean;
  buscar: () => Promise<ResultadoDaBusca<T, C>>;
  aoAtualizar: (dados: T) => void;
  habilitado?: boolean;
  criarFonte?: CriarFonte;
  janelaMs?: number;
}): AtualizacaoAoVivo<C> {
  const [atualizando, setAtualizando] = React.useState(false);
  const [falha, setFalha] = React.useState<C | null>(null);
  const [atualizadoEm, setAtualizadoEm] = React.useState<number | null>(null);

  // Refs escritos DENTRO de um efeito, nunca no corpo do render. Acessar
  // `.current` durante o render é o que a regra `react-hooks/refs` reprova, e
  // com razão: um valor lido no render que não dispara re-render é a receita da
  // tela que não acompanha a própria prop.
  const buscarRef = React.useRef(buscar);
  const aplicarRef = React.useRef(aoAtualizar);
  const interessaRef = React.useRef(interessa);

  React.useEffect(() => {
    buscarRef.current = buscar;
    aplicarRef.current = aoAtualizar;
    interessaRef.current = interessa;
  });

  const noAr = React.useRef(false);
  const pendente = React.useRef(false);
  const agrupador = React.useRef<ReturnType<typeof setTimeout> | null>(null);
  const montado = React.useRef(true);

  React.useEffect(() => {
    montado.current = true;
    return () => {
      montado.current = false;
      if (agrupador.current) clearTimeout(agrupador.current);
    };
  }, []);

  /**
   * Um laço `do…while`, e não recursão. A rajada que chega durante o voo vira
   * mais uma volta; a trava garante uma requisição por vez. Recursão aqui
   * também seria `executar` referindo-se a si mesmo antes de estar declarado —
   * o laço resolve as duas coisas sem um ref intermediário.
   */
  const executar = React.useCallback(async () => {
    if (noAr.current) {
      pendente.current = true;
      return;
    }
    noAr.current = true;
    setAtualizando(true);

    try {
      do {
        pendente.current = false;
        const resultado = await buscarRef.current();
        if (!montado.current) return;

        if (resultado.ok) {
          aplicarRef.current(resultado.data);
          setFalha(null);
          setAtualizadoEm(Date.now());
        } else {
          setFalha(resultado.code);
        }
      } while (pendente.current);
    } finally {
      noAr.current = false;
      if (montado.current) setAtualizando(false);
    }
  }, []);

  const agendar = React.useCallback(() => {
    if (agrupador.current) clearTimeout(agrupador.current);
    agrupador.current = setTimeout(() => {
      agrupador.current = null;
      void executar();
    }, janelaMs);
  }, [executar, janelaMs]);

  const aoEvento = React.useCallback(
    (evento: EventoDoStream) => {
      if (!interessaRef.current(evento)) return;
      agendar();
    },
    [agendar],
  );

  const tempoReal = useSSE({ topicos, aoEvento, aoResync: agendar, habilitado, criarFonte });

  return { tempoReal, atualizando, falha, atualizadoEm, atualizarAgora: agendar };
}
