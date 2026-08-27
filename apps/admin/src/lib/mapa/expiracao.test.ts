import { describe, expect, it } from "vitest";

import { expiracaoDeHold } from "./expiracao";

const AGORA = Date.parse("2026-01-10T12:00:00-03:00");
const daquiA = (ms: number) => new Date(AGORA + ms).toISOString();

const MINUTO = 60_000;
const HORA = 60 * MINUTO;

describe("expiracaoDeHold", () => {
  it("sem `expires_at` não há contador — e isso não é erro", () => {
    expect(expiracaoDeHold(null, AGORA)).toBeNull();
    expect(expiracaoDeHold(undefined, AGORA)).toBeNull();
    expect(expiracaoDeHold("não é data", AGORA)).toBeNull();
  });

  it("mais de um dia mostra dias e horas", () => {
    expect(expiracaoDeHold(daquiA(30 * HORA), AGORA)).toMatchObject({
      urgencia: "tranquila",
      texto: "expira em 1 d 6 h",
      curto: "1d6",
    });
  });

  it("entre 6 h e 24 h é tranquila", () => {
    expect(expiracaoDeHold(daquiA(8 * HORA + 12 * MINUTO), AGORA)).toMatchObject({
      urgencia: "tranquila",
      curto: "8h12",
    });
  });

  it("abaixo de 6 h vira atenção — ainda dá para ligar hoje", () => {
    expect(expiracaoDeHold(daquiA(5 * HORA), AGORA)?.urgencia).toBe("atencao");
  });

  it("abaixo de 2 h vira crítica — vence no meu turno", () => {
    expect(expiracaoDeHold(daquiA(90 * MINUTO), AGORA)).toMatchObject({
      urgencia: "critica",
      texto: "expira em 1 h 30 min",
    });
  });

  it("menos de uma hora mostra só minutos", () => {
    expect(expiracaoDeHold(daquiA(12 * MINUTO), AGORA)).toMatchObject({
      texto: "expira em 12 min",
      curto: "12min",
    });
  });

  it("arredonda o minuto para CIMA: 30 s é '1 min', nunca '0 min'", () => {
    // "expira em 0 min" fica errado no instante seguinte e parece defeito.
    expect(expiracaoDeHold(daquiA(30_000), AGORA)?.texto).toBe("expira em 1 min");
  });

  it("vencida é 'expirada', e não some", () => {
    // Entre o vencimento e a passagem do job (a cada minuto) a data ainda está
    // ocupada. Mostrar "expirada" separa "ocupada" de "prestes a liberar".
    expect(expiracaoDeHold(daquiA(-5 * MINUTO), AGORA)).toMatchObject({
      urgencia: "expirada",
      texto: "pré-reserva expirada",
    });
  });
});
