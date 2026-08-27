"use client";

import * as React from "react";

/**
 * Um relógio para a tela inteira, e não um `setInterval` por contador.
 *
 * O mapa pode ter meia dúzia de pré-reservas visíveis, cada uma com o próprio
 * contador de expiração. As duas formas erradas de fazer isso:
 *
 * 1. **Um `agora` no estado da raiz do mapa.** Cada tique repinta as 9 linhas e
 *    as ~40 colunas visíveis — 300 ms de trabalho a cada 30 s para mudar dois
 *    dígitos dentro de uma barra.
 * 2. **Um `setInterval` dentro de cada contador.** Corrige o alcance do
 *    render, mas cria N temporizadores que acordam em instantes diferentes, e
 *    dois contadores lado a lado passam a virar o minuto em momentos distintos.
 *
 * O que sobra é uma fonte externa: **um** temporizador no módulo, e
 * `useSyncExternalStore` para que só quem lê o relógio re-renderize. Os
 * contadores viram o minuto juntos porque leem o mesmo tique.
 *
 * O temporizador só existe enquanto alguém está inscrito — tela sem
 * pré-reserva visível não acorda a aba.
 */

/**
 * 30 s. O contador tem resolução de minuto, então tique de 1 s seria 30 vezes
 * mais trabalho para o mesmo texto; tique de 60 s deixaria o número errado por
 * até 59 s, e "expira em 1 min" parado enquanto já expirou é o tipo de mentira
 * que faz alguém não ligar para o hóspede.
 */
export const INTERVALO_DO_RELOGIO = 30_000;

let agora = Date.now();
let timer: ReturnType<typeof setInterval> | null = null;
const inscritos = new Set<() => void>();

function assinar(notificar: () => void): () => void {
  inscritos.add(notificar);
  if (timer === null) {
    timer = setInterval(() => {
      agora = Date.now();
      for (const fn of inscritos) fn();
    }, INTERVALO_DO_RELOGIO);
  }
  return () => {
    inscritos.delete(notificar);
    if (inscritos.size === 0 && timer !== null) {
      clearInterval(timer);
      timer = null;
    }
  };
}

function ler(): number {
  return agora;
}

/**
 * No servidor o valor é fixo: um `Date.now()` durante a renderização do HTML e
 * outro durante a hidratação produzem textos diferentes, e o React reclama de
 * incompatibilidade em cada contador da tela. Zero é reconhecível — o contador
 * trata "sem relógio" como "ainda não sei", e o primeiro tique no cliente
 * escreve o número certo.
 */
function lerNoServidor(): number {
  return 0;
}

export function useAgora(): number {
  return React.useSyncExternalStore(assinar, ler, lerNoServidor);
}

/** Só para teste: força o tique sem esperar 30 s. */
export function forcarTique(instante: number = Date.now()): void {
  agora = instante;
  for (const fn of inscritos) fn();
}
