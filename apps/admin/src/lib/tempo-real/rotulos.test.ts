import { describe, expect, it } from "vitest";

import { descreverTempoReal, idadeEmTexto } from "./rotulos";

describe("descreverTempoReal", () => {
  it("fala do DADO na tela, não do transporte", () => {
    // "Reconectando" é sobre a conexão e não ajuda quem vende; "pode haver
    // mudança não mostrada" é sobre o que está desenhado, que é o que decide se
    // vale um F5.
    expect(descreverTempoReal("reconectando").detalhe).toContain("mudança ainda não mostrada");
    expect(descreverTempoReal("desligado").detalhe).toContain("pode estar desatualizada");
  });

  it("nomeia a tela de que fala — o funil não é o mapa", () => {
    // O selo desenha em duas telas desde que o kanban passou a assinar o
    // barramento. "O mapa se atualiza sozinho" em cima do quadro do funil é o
    // tipo de texto que ensina o operador a não ler o indicador.
    expect(descreverTempoReal("ligado", "mapa").detalhe).toContain("O mapa");
    expect(descreverTempoReal("ligado", "funil").detalhe).toContain("O quadro");
    expect(descreverTempoReal("ligado", "funil").detalhe).not.toContain("mapa");
    expect(descreverTempoReal("sem_rede", "funil").detalhe).not.toContain("mapa");
  });

  it("só oferece reconectar onde o clique muda alguma coisa", () => {
    expect(descreverTempoReal("ligado").ofereceReconectar).toBe(false);
    expect(descreverTempoReal("conectando").ofereceReconectar).toBe(false);
    // Offline: insistir não adianta, o navegador avisa quando a rede voltar.
    expect(descreverTempoReal("sem_rede").ofereceReconectar).toBe(false);
    expect(descreverTempoReal("reconectando").ofereceReconectar).toBe(true);
    expect(descreverTempoReal("desligado").ofereceReconectar).toBe(true);
  });

  it("pinta de falha só o que é falha", () => {
    expect(descreverTempoReal("ligado").tom).toBe("ok");
    expect(descreverTempoReal("renovando").tom).toBe("atencao");
    expect(descreverTempoReal("desligado").tom).toBe("falha");
  });
});

describe("idadeEmTexto", () => {
  const agora = Date.parse("2026-08-27T12:00:00-03:00");

  it("mostra a idade do desenho em segundos, minutos e horas", () => {
    expect(idadeEmTexto(agora - 12_000, agora)).toBe("há 12 s");
    expect(idadeEmTexto(agora - 4 * 60_000, agora)).toBe("há 4 min");
    expect(idadeEmTexto(agora - 2 * 3_600_000, agora)).toBe("há 2 h");
  });

  it("sem relógio ainda (servidor, antes do primeiro tique) não inventa idade", () => {
    expect(idadeEmTexto(agora, 0)).toBeNull();
    expect(idadeEmTexto(null, agora)).toBeNull();
  });
});
