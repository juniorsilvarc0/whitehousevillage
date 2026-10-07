import { describe, expect, it } from "vitest";

import {
  aplicarPasso,
  estadoDaLinha,
  exibicaoDaContagem,
  lerDigitado,
  pendenciasPorAmbiente,
  notaDoFechamento,
  pendentesDoAmbiente,
  proximoComPendencia,
  rotuloDoEstado,
} from "@/lib/bens/contagem";

/**
 * `counted_qty: null` é "não contei"; `0` é "contei e não achei". Fechar a
 * conferência confundindo os dois transforma "não olhei" numa perda que alguém
 * vai cobrar de um hóspede (docs/db.md §11). Estes casos fixam que a tela nunca
 * os confunde — nem no que mostra, nem no que um toque produz.
 */
describe("null não é zero", () => {
  it("pendente e zero são estados diferentes", () => {
    expect(estadoDaLinha({ expected_qty: 12, counted_qty: null })).toBe("pendente");
    expect(estadoDaLinha({ expected_qty: 12, counted_qty: 0 })).toBe("falta");
  });

  it("zero esperado e zero contado confere — não é falta", () => {
    expect(estadoDaLinha({ expected_qty: 0, counted_qty: 0 })).toBe("confere");
  });

  it("a etiqueta diz as duas coisas com palavras diferentes", () => {
    expect(rotuloDoEstado({ expected_qty: 12, counted_qty: null })).toBe("Não contado");
    expect(rotuloDoEstado({ expected_qty: 12, counted_qty: 0 })).toBe("Nenhum encontrado (falta 12)");
    expect(rotuloDoEstado({ expected_qty: 12, counted_qty: 9 })).toBe("Falta 3");
    expect(rotuloDoEstado({ expected_qty: 12, counted_qty: 14 })).toBe("Sobra 2");
    expect(rotuloDoEstado({ expected_qty: 12, counted_qty: 12 })).toBe("Confere");
  });

  it("o contador mostra traço para pendente e o número para zero", () => {
    expect(exibicaoDaContagem(null)).toBe("—");
    expect(exibicaoDaContagem(0)).toBe("0");
  });
});

describe("o toque no + e no −", () => {
  it("na linha pendente, parte do esperado — 12 esperados e um toque no − dá 11", () => {
    expect(aplicarPasso(null, 12, -1)).toBe(11);
    expect(aplicarPasso(null, 12, 1)).toBe(13);
  });

  it("o − de uma linha pendente nunca vira zero sozinho", () => {
    // Era o defeito óbvio: partir de 0 faria o primeiro toque gravar
    // "não achei nenhum" numa linha que ninguém contou.
    expect(aplicarPasso(null, 5, -1)).not.toBe(0);
  });

  it("na linha contada, parte do contado e não desce abaixo de zero", () => {
    expect(aplicarPasso(3, 12, -1)).toBe(2);
    expect(aplicarPasso(0, 12, -1)).toBe(0);
  });
});

describe("o número digitado", () => {
  it("campo vazio não é zero nem desfaz — é 'não mexer'", () => {
    expect(lerDigitado("")).toBeNull();
    expect(lerDigitado("   ")).toBeNull();
  });

  it("aceita inteiro não-negativo, recusa o resto", () => {
    expect(lerDigitado("0")).toBe(0);
    expect(lerDigitado("12")).toBe(12);
    expect(lerDigitado("-1")).toBe("invalido");
    expect(lerDigitado("1,5")).toBe("invalido");
    expect(lerDigitado("doze")).toBe("invalido");
  });
});

describe("caminhar pela casa", () => {
  const ambientes = [
    { room_id: "sala", room_name: "Sala", lines: [{ counted_qty: 2 }, { counted_qty: 0 }] },
    { room_id: "cozinha", room_name: "Cozinha", lines: [{ counted_qty: null }, { counted_qty: 1 }] },
    { room_id: "varanda", room_name: "Varanda", lines: [{ counted_qty: null }] },
  ] as never[];

  it("zero contado não conta como pendente", () => {
    expect(pendentesDoAmbiente(ambientes[0])).toBe(0);
    expect(pendentesDoAmbiente(ambientes[1])).toBe(1);
  });

  it("o próximo com pendência segue a ordem e dá a volta", () => {
    expect(proximoComPendencia(ambientes, "sala")).toBe("cozinha");
    expect(proximoComPendencia(ambientes, "cozinha")).toBe("varanda");
    expect(proximoComPendencia(ambientes, "varanda")).toBe("cozinha");
  });

  it("sem pendência em lugar nenhum, não há próximo", () => {
    const tudoContado = [{ room_id: "a", lines: [{ counted_qty: 0 }] }] as never[];
    expect(proximoComPendencia(tudoContado, "a")).toBeNull();
  });
});

describe("details.pending_by_room do 409 COUNT_HAS_PENDING_LINES", () => {
  it("lê a lista do contrato — [{room_id, room_name, pending}] — e guarda o id para levar ao cômodo", () => {
    expect(
      pendenciasPorAmbiente([
        { room_id: "r1", room_name: "Cozinha", pending: 3 },
        { room_id: "r2", room_name: "Varanda", pending: 1 },
      ]),
    ).toEqual([
      { roomId: "r1", ambiente: "Cozinha", pendentes: 3 },
      { roomId: "r2", ambiente: "Varanda", pendentes: 1 },
    ]);
  });

  it("forma fora do contrato vira lista vazia, não exceção", () => {
    expect(pendenciasPorAmbiente(undefined)).toEqual([]);
    expect(pendenciasPorAmbiente({ r1: 2 })).toEqual([]);
    expect(pendenciasPorAmbiente([{ room_name: "Sala", pending: 1 }])).toEqual([]);
    expect(pendenciasPorAmbiente([{ room_id: "r1", pending: "3" }])).toEqual([]);
  });
});

/**
 * `PedidoDeFechamento.note`: ausente mantém, texto substitui, `null` limpa. O
 * campo do fechamento abre com a observação que a conferência já tem, e é o
 * que a pessoa fez com ele que decide qual dos três vai.
 */
describe("a observação do fechamento", () => {
  it("não mexeu: não vai no corpo (mantém a que existe)", () => {
    expect(notaDoFechamento("check-out WH-2026-0142", "check-out WH-2026-0142")).toBeUndefined();
    expect(notaDoFechamento("  check-out  ", "check-out")).toBeUndefined();
    expect(notaDoFechamento("", null)).toBeUndefined();
  });

  it("trocou o texto: substitui", () => {
    expect(notaDoFechamento("faltou conferir o cofre", "check-out")).toBe("faltou conferir o cofre");
    expect(notaDoFechamento("primeira nota", null)).toBe("primeira nota");
  });

  it("apagou tudo: null, que limpa — e não string vazia", () => {
    expect(notaDoFechamento("   ", "check-out")).toBeNull();
  });
});
