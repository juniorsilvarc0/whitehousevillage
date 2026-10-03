import type { EstadoDoTempoReal } from "./sse";

/**
 * O que a tela diz sobre a conexão — e por que ela diz alguma coisa.
 *
 * Um mapa que parou de receber eventos é **visualmente idêntico** a um mapa
 * onde nada aconteceu. Sem um indicador, a gestão confia num desenho velho, que
 * é o risco exato que o tempo real existia para eliminar: pior do que não ter
 * atualização automática é ter uma que falhou em silêncio.
 *
 * Os textos falam do **dado**, não do transporte. "Reconectando" é sobre a
 * conexão e não ajuda quem vende; "pode haver mudança não mostrada" é sobre o
 * que está na tela, que é o que importa para decidir se vale um F5.
 */
/**
 * De que tela o texto fala. São duas hoje — o mapa de ocupação e o quadro do
 * funil —, e a diferença aparece só onde a frase nomeia o assunto: dizer "o
 * mapa se atualiza sozinho" numa tela que não é o mapa é o tipo de texto que
 * ensina o operador a não ler o indicador.
 */
export type AssuntoDoTempoReal = "mapa" | "funil";

export type DescricaoDoTempoReal = {
  rotulo: string;
  detalhe: string;
  /** `ok` pinta de marca, `atencao` de âmbar, `falha` de destrutivo. */
  tom: "ok" | "atencao" | "falha";
  /** Oferece o botão de reconectar — só onde o clique muda alguma coisa. */
  ofereceReconectar: boolean;
};

export function descreverTempoReal(
  estado: EstadoDoTempoReal,
  assunto: AssuntoDoTempoReal = "mapa",
): DescricaoDoTempoReal {
  switch (estado) {
    case "ligado":
      return {
        rotulo: "Ao vivo",
        detalhe:
          assunto === "funil"
            ? "O quadro se atualiza sozinho quando alguém move, ganha ou perde um negócio."
            : "O mapa se atualiza sozinho quando alguém vende, bloqueia ou cancela.",
        tom: "ok",
        ofereceReconectar: false,
      };
    case "conectando":
      return {
        rotulo: "Conectando",
        detalhe: "Ligando a atualização automática.",
        tom: "atencao",
        ofereceReconectar: false,
      };
    case "reconectando":
      return {
        rotulo: "Reconectando",
        detalhe: "A atualização automática caiu e está voltando. Pode haver mudança ainda não mostrada.",
        tom: "atencao",
        ofereceReconectar: true,
      };
    case "renovando":
      return {
        rotulo: "Renovando sessão",
        detalhe: "A sessão estava para vencer e foi renovada sem interromper a tela.",
        tom: "atencao",
        ofereceReconectar: false,
      };
    case "sem_rede":
      return {
        rotulo: "Sem internet",
        detalhe:
          assunto === "funil"
            ? "Sem internet. O quadro volta a se atualizar sozinho quando a conexão voltar."
            : "Sem internet. O mapa volta a se atualizar sozinho quando a conexão voltar.",
        tom: "falha",
        ofereceReconectar: false,
      };
    case "desligado":
      return {
        rotulo: "Sem atualização automática",
        detalhe: "A tela pode estar desatualizada. Clique para reconectar ou recarregue a página.",
        tom: "falha",
        ofereceReconectar: true,
      };
  }
}

/** "há 12 s", "há 4 min", "há 2 h" — a idade do que está desenhado. */
export function idadeEmTexto(instante: number | null, agora: number): string | null {
  if (instante === null || agora <= 0) return null;
  const segundos = Math.max(0, Math.round((agora - instante) / 1000));
  if (segundos < 45) return `há ${segundos} s`;
  const minutos = Math.round(segundos / 60);
  if (minutos < 60) return `há ${minutos} min`;
  return `há ${Math.round(minutos / 60)} h`;
}
