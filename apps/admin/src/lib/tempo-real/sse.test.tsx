import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useSSE, type FonteDeEventos } from "./sse";

/**
 * O barramento testado sem barramento.
 *
 * `EventSource` não existe no jsdom, e mesmo que existisse um teste que abre
 * conexão de verdade mediria a rede. `useSSE` recebe a fábrica da fonte por
 * parâmetro justamente para isto: aqui entra um duplo que deixa o teste
 * *empurrar* os eventos que o servidor mandaria, incluindo os quatro de
 * controle (`ready`, `resync`, `expiring`, `expired`) que são a parte que
 * ninguém consegue reproduzir por acidente.
 */
class FonteFalsa implements FonteDeEventos {
  static ultima: FonteFalsa | null = null;

  readonly url: string;
  readyState = 0;
  fechada = false;
  private ouvintes = new Map<string, ((evento: MessageEvent<string>) => void)[]>();

  constructor(url: string) {
    this.url = url;
    FonteFalsa.ultima = this;
  }

  addEventListener(tipo: string, ouvinte: (evento: MessageEvent<string>) => void): void {
    const lista = this.ouvintes.get(tipo) ?? [];
    lista.push(ouvinte);
    this.ouvintes.set(tipo, lista);
  }

  close(): void {
    this.fechada = true;
    this.readyState = 2;
  }

  emitir(tipo: string, data: string, id?: string): void {
    act(() => {
      for (const ouvinte of this.ouvintes.get(tipo) ?? []) {
        ouvinte(new MessageEvent(tipo, { data, lastEventId: id ?? "" }));
      }
    });
  }
}

const criarFonte = (url: string): FonteDeEventos => new FonteFalsa(url);
const fonte = () => FonteFalsa.ultima!;

const CALENDAR = '{"entity":"stay_block","id":"b1","unit_id":"u1","v":7}';

describe("useSSE", () => {
  it("assina os tópicos pedidos, na URL do BFF — nunca a API direto", () => {
    // `EventSource` não manda cabeçalho, e o contrato recusa token em query
    // string (ele pararia no log do proxy). O cookie httpOnly só chega em mesma
    // origem, então o caminho é a Route Handler.
    renderHook(() => useSSE({ topicos: ["calendar"], aoEvento: vi.fn(), criarFonte }));
    expect(fonte().url).toBe("/api/stream?topics=calendar");
  });

  it("o primeiro evento é `ready`, e é ele que diz quais tópicos VALERAM", () => {
    const { result } = renderHook(() =>
      useSSE({ topicos: ["calendar", "crm"], aoEvento: vi.fn(), criarFonte }),
    );

    expect(result.current.estado).toBe("conectando");
    fonte().emitir("ready", '{"topics":["calendar"],"server_time":"2026-08-27T10:00:00-03:00"}');

    expect(result.current.estado).toBe("ligado");
    // O CRM foi descartado da assinatura por falta de permissão. Saber disso é
    // a razão de o `ready` existir: o cliente precisa saber o que não conseguiu.
    expect(result.current.topicosAceitos).toEqual(["calendar"]);
  });

  it("entrega o evento uma vez e descarta a reentrega do mesmo `v`", () => {
    const aoEvento = vi.fn();
    renderHook(() => useSSE({ topicos: ["calendar"], aoEvento, criarFonte }));

    fonte().emitir("calendar", CALENDAR, "8412");
    fonte().emitir("calendar", CALENDAR, "8412");

    expect(aoEvento).toHaveBeenCalledTimes(1);
    expect(aoEvento).toHaveBeenCalledWith({ entity: "stay_block", id: "b1", unit_id: "u1", v: 7 }, "calendar");
  });

  it("`resync` refaz o fetch inteiro e apaga o que a tela já tinha por referência", () => {
    // O servidor está dizendo que o cursor pedido é mais velho que o buffer.
    // Fingir que nada se perdeu é como o mapa fica errado sem ninguém perceber.
    const aoEvento = vi.fn();
    const aoResync = vi.fn();
    renderHook(() => useSSE({ topicos: ["calendar"], aoEvento, aoResync, criarFonte }));

    fonte().emitir("calendar", CALENDAR);
    fonte().emitir("resync", "{}");
    fonte().emitir("calendar", CALENDAR);

    expect(aoResync).toHaveBeenCalledTimes(1);
    expect(aoEvento).toHaveBeenCalledTimes(2);
  });

  it("`expiring` renova a sessão FORA do stream e a conexão segue", async () => {
    // Não dá para responder 401 depois do 200 — os cabeçalhos já foram enviados
    // e o EventSource não deixa ler status depois disso. Por isso o servidor
    // avisa antes, e a renovação acontece em /auth/refresh, num lugar só.
    const renovarSessao = vi.fn().mockResolvedValue(true);
    const { result } = renderHook(() =>
      useSSE({ topicos: ["calendar"], aoEvento: vi.fn(), renovarSessao, criarFonte }),
    );

    fonte().emitir("ready", '{"topics":["calendar"]}');
    fonte().emitir("expiring", '{"expires_at":"2026-08-27T10:15:00-03:00"}');

    expect(renovarSessao).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(result.current.estado).toBe("ligado"));
  });

  it("`expired` é fim de stream limpo: quem reabre é o navegador, com o cookie novo", async () => {
    const { result } = renderHook(() =>
      useSSE({ topicos: ["calendar"], aoEvento: vi.fn(), criarFonte }),
    );

    fonte().emitir("ready", '{"topics":["calendar"]}');
    fonte().emitir("expired", "{}");

    await waitFor(() => expect(result.current.estado).toBe("reconectando"));
  });

  it("conexão fechada de vez com a sessão morta vira `desligado` — não um mapa parado fingindo estar vivo", async () => {
    const renovarSessao = vi.fn().mockResolvedValue(false);
    const { result } = renderHook(() =>
      useSSE({ topicos: ["calendar"], aoEvento: vi.fn(), renovarSessao, criarFonte }),
    );

    fonte().readyState = 2;
    fonte().emitir("error", "");

    await waitFor(() => expect(result.current.estado).toBe("desligado"));
  });

  it("queda de transporte (o navegador reabrindo sozinho) é `reconectando`, não `desligado`", () => {
    const { result } = renderHook(() =>
      useSSE({ topicos: ["calendar"], aoEvento: vi.fn(), criarFonte }),
    );

    fonte().readyState = 0;
    fonte().emitir("error", "");

    expect(result.current.estado).toBe("reconectando");
  });

  it("voltar a aba para o primeiro plano força um refetch — tique perdido é indistinguível de nada", () => {
    const aoResync = vi.fn();
    renderHook(() => useSSE({ topicos: ["calendar"], aoEvento: vi.fn(), aoResync, criarFonte }));

    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });

    expect(aoResync).toHaveBeenCalledTimes(1);
  });

  it("sem tópico não abre conexão nenhuma", () => {
    // Uma conexão que só receberia 403 é pior que nenhuma: o cliente fica
    // esperando para sempre um evento que não vem.
    FonteFalsa.ultima = null;
    const { result } = renderHook(() =>
      useSSE({ topicos: [], aoEvento: vi.fn(), criarFonte }),
    );

    expect(FonteFalsa.ultima).toBeNull();
    expect(result.current.estado).toBe("desligado");
  });

  it("`habilitado: false` também não abre — é o caminho de quem não tem permissão", () => {
    FonteFalsa.ultima = null;
    const { result } = renderHook(() =>
      useSSE({ topicos: ["calendar"], aoEvento: vi.fn(), habilitado: false, criarFonte }),
    );

    expect(FonteFalsa.ultima).toBeNull();
    expect(result.current.estado).toBe("desligado");
  });

  it("reabrir à mão leva o cursor junto — sem ele o servidor manda recarregar tudo", async () => {
    // O navegador só reenvia `Last-Event-ID` nas reconexões que ELE faz.
    const { result } = renderHook(() =>
      useSSE({ topicos: ["calendar"], aoEvento: vi.fn(), criarFonte }),
    );

    fonte().emitir("calendar", CALENDAR, "8412");
    act(() => result.current.reconectar());

    await waitFor(() => expect(fonte().url).toContain("last_event_id=8412"));
  });

  it("desmontar fecha a conexão — aba fechada não deixa stream de pé no servidor", () => {
    const { unmount } = renderHook(() =>
      useSSE({ topicos: ["calendar"], aoEvento: vi.fn(), criarFonte }),
    );
    const aberta = fonte();
    unmount();
    expect(aberta.fechada).toBe(true);
  });
});
