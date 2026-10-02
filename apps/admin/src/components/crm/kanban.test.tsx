import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";

import { PipelineKanban } from "@/components/crm/kanban";
import { buscarQuadro, moverEtapa } from "@/lib/crm/acoes";
import type { CardDaOportunidade, ColunaDoKanban, EtapaDoFunil, QuadroKanban } from "@/lib/crm/tipos";
import type { FonteDeEventos } from "@/lib/tempo-real/sse";

/**
 * O movimento otimista do kanban — e o desfazer.
 *
 * O que se protege aqui:
 *
 * 1. **O card muda de coluna antes da resposta.** É o que faz o gesto parecer
 *    instantâneo; se a tela esperasse o servidor, o arrasto teria meio segundo
 *    de nada acontecendo e o operador arrastaria de novo.
 * 2. **A recusa repõe exatamente o estado anterior** — card *e* totais das duas
 *    colunas. Rollback que esquece o total deixa a soma do funil errada até o
 *    próximo F5, e a soma do funil é o primeiro número que a gestão olha.
 * 3. **A recusa é dita em voz alta.** Card que volta sozinho e em silêncio
 *    ensina o operador que "o sistema não deixa arrastar".
 * 4. **Coluna terminal não move card.** Ganhar cria reserva e perder exige
 *    motivo; soltar em "Ganho" abre o diálogo e o card fica onde está.
 *
 * O teste dirige pelo menu **"Mover para"**, que é o caminho de teclado exigido
 * pela tela e que passa exatamente pela mesma função do arrasto (`mover`). O
 * arrasto em si depende de `getBoundingClientRect`, que em jsdom devolve zeros:
 * testá-lo aqui mediria o jsdom, não o produto.
 */

vi.mock("sonner", () => ({
  Toaster: () => null,
  toast: { error: vi.fn(), success: vi.fn() },
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh: vi.fn(), push: vi.fn(), replace: vi.fn() }),
}));

vi.mock("@/lib/crm/acoes", () => ({
  buscarQuadro: vi.fn(),
  moverEtapa: vi.fn(),
  ganharOportunidade: vi.fn(),
  perderOportunidade: vi.fn(),
  atualizarOportunidade: vi.fn(),
  criarAtividade: vi.fn(),
  concluirAtividade: vi.fn(),
}));

const moverEtapaMock = vi.mocked(moverEtapa);
const buscarQuadroMock = vi.mocked(buscarQuadro);

/**
 * O duplo do `EventSource`, porque o jsdom não tem um — e porque um teste que
 * abrisse conexão de verdade mediria a rede.
 *
 * `abertas` é contador de CLASSE de propósito: a única forma de provar que a
 * conexão não reabre é contar aberturas ao longo de vários renders, e é essa a
 * medida que a suíte do hook não sabia fazer enquanto a conexão reabria 220
 * vezes por segundo em produção.
 */
class FonteFalsa implements FonteDeEventos {
  static abertas = 0;
  static ultima: FonteFalsa | null = null;

  readyState = 0;
  private ouvintes = new Map<string, ((evento: MessageEvent<string>) => void)[]>();

  constructor(readonly url: string) {
    FonteFalsa.abertas += 1;
    FonteFalsa.ultima = this;
  }

  addEventListener(tipo: string, ouvinte: (evento: MessageEvent<string>) => void): void {
    this.ouvintes.set(tipo, [...(this.ouvintes.get(tipo) ?? []), ouvinte]);
  }

  close(): void {
    this.readyState = 2;
  }

  emitir(tipo: string, data: string): void {
    act(() => {
      for (const ouvinte of this.ouvintes.get(tipo) ?? []) {
        ouvinte(new MessageEvent(tipo, { data }));
      }
    });
  }
}

/** Estável de módulo — a forma que NÃO consegue reproduzir o defeito de
 *  reconexão, e por isso a que serve a todo teste que não é sobre ele. */
const criarFonte = (url: string): FonteDeEventos => new FonteFalsa(url);
const fonte = () => FonteFalsa.ultima!;

/** O `data:` que o barramento entrega quando alguém mexe num card. `topic` vem
 *  no envelope (o `pg_notify` o monta) e o cliente o ignora: quem separa os
 *  assuntos é o nome do evento SSE. */
const oportunidade = (id: string, v: number) =>
  JSON.stringify({ topic: "crm", entity: "opportunity", id, v });

function etapa(id: string, name: string, position: number, tipo: EtapaDoFunil["type"] = "aberto"): EtapaDoFunil {
  return {
    id,
    pipeline_id: "f-1",
    name,
    position,
    probability: position * 25,
    color: "#8FA36B",
    type: tipo,
    sla_days: 2,
    auto_task_subject: null,
    auto_task_type: null,
    auto_task_due_days: null,
    auto_notify: true,
  };
}

function card(id: string, contato: string, centavos: number): CardDaOportunidade {
  return {
    id,
    contact_id: `c-${id}`,
    contact_name: contato,
    unit_type_name: "White House Completa",
    check_in: "2026-12-20",
    check_out: "2026-12-23",
    amount_cents: centavos,
    probability: 25,
    expected_close: null,
    owner_id: "u-1",
    owner_name: "Ana Souza",
    status: "aberta",
    entered_stage_at: "2026-08-20T12:00:00Z",
    sla_due_at: "2026-08-22T12:00:00Z",
    sla_breached: false,
    pending_task_count: 0,
    next_due_at: null,
    reservation_code: null,
  };
}

function coluna(stage: EtapaDoFunil, cards: CardDaOportunidade[]): ColunaDoKanban {
  return {
    stage,
    count: cards.length,
    amount_cents: cards.reduce((total, c) => total + c.amount_cents, 0),
    has_more: false,
    cards,
  };
}

const NOVO = etapa("e-novo", "Novo lead", 0);
const NEGOCIACAO = etapa("e-negociacao", "Negociação", 1);
const GANHO = etapa("e-ganho", "Ganho", 2, "ganho");
const PERDIDO = etapa("e-perdido", "Perdido", 3, "perdido");

const FERNANDA = card("op-1", "Fernanda Lima", 1_200_000);

function quadro(): QuadroKanban {
  return {
    pipeline: { id: "f-1", name: "Funil de Reservas", is_default: true, active: true, stage_count: 4, open_opportunity_count: 2 },
    columns: [
      coluna(NOVO, [FERNANDA, card("op-2", "Rui Barros", 300_000)]),
      coluna(NEGOCIACAO, []),
      coluna(GANHO, []),
      coluna(PERDIDO, []),
    ],
    totals: { count: 2, amount_cents: 1_500_000 },
  };
}

const MOTIVOS = [{ id: "m-1", label: "Preço acima do orçamento", active: true, usage_count: 3 }];

function montar() {
  render(
    <PipelineKanban
      quadro={quadro()}
      motivos={MOTIVOS}
      permissoes={{ editar: true }}
      filtros={{}}
      // Sempre injetada: sem isto o hook cairia no `EventSource` do navegador,
      // que o jsdom não tem, e TODO teste deste arquivo morreria no efeito.
      criarFonte={criarFonte}
    />,
  );
}

/**
 * A coluna, pelo rótulo acessível que ela declara.
 *
 * `hidden: true` é obrigatório: quando um diálogo modal abre, o Base UI esconde
 * o resto da página da árvore de acessibilidade — que é o comportamento certo, e
 * que sem esta opção faria o quadro inteiro sumir das consultas exatamente nos
 * testes que precisam provar que **o card não se moveu** enquanto o diálogo está
 * aberto.
 */
function colunaNaTela(nome: string): HTMLElement {
  return screen.getByRole("listitem", { name: `Etapa ${nome}`, hidden: true });
}

function cardsDe(nome: string): string[] {
  return within(colunaNaTela(nome))
    .queryAllByRole("article", { hidden: true })
    .map((elemento) => within(elemento).getByRole("link", { hidden: true }).textContent ?? "");
}

async function moverPeloMenu(contato: string, destino: string) {
  fireEvent.click(screen.getByLabelText(`Mover ${contato} para outra etapa`));
  const item = await screen.findByRole("menuitem", { name: new RegExp(destino) });
  // O clique dispara uma função assíncrona: o movimento otimista acontece
  // dentro do `act` do `fireEvent`, mas a resposta da action resolve num
  // microtask depois dele. `act(async …)` esvazia a fila antes de devolver, e
  // é o que mantém o rollback dentro do escopo controlado do React.
  await act(async () => {
    fireEvent.click(item);
  });
}

/** Uma promessa que o teste resolve quando quiser — é o que permite observar a
 *  tela **durante** a requisição, que é o instante em que o otimismo existe. */
function represada<T>() {
  let liberar!: (valor: T) => void;
  const promessa = new Promise<T>((resolve) => {
    liberar = resolve;
  });
  return { promessa, liberar };
}

beforeEach(() => {
  vi.mocked(toast.error).mockClear();
  vi.mocked(toast.success).mockClear();
  moverEtapaMock.mockReset();
  buscarQuadroMock.mockReset();
  FonteFalsa.abertas = 0;
  FonteFalsa.ultima = null;
});

describe("PipelineKanban — movimento otimista", () => {
  it("move o card antes de o servidor responder", async () => {
    const { promessa, liberar } = represada<Awaited<ReturnType<typeof moverEtapa>>>();
    moverEtapaMock.mockReturnValue(promessa);

    montar();
    expect(cardsDe("Novo lead")).toEqual(["Fernanda Lima", "Rui Barros"]);

    await moverPeloMenu("Fernanda Lima", "Negociação");

    // A requisição ainda não voltou e o card já está do outro lado.
    await waitFor(() => expect(cardsDe("Negociação")).toEqual(["Fernanda Lima"]));
    expect(cardsDe("Novo lead")).toEqual(["Rui Barros"]);
    expect(moverEtapaMock).toHaveBeenCalledWith("op-1", "e-negociacao", "e-novo");

    // Liberar dentro de `act`: a resolução da promessa ainda dispara estado
    // (desmarca o card como pendente), e deixá-la para depois do teste vira
    // aviso de act — ruído que esconde o aviso legítimo do dia seguinte.
    await act(async () => {
      liberar({ ok: true, data: { opportunity: {}, auto_task: null, sla: {} } as never });
    });
  });

  it("leva contagem e soma junto com o card", async () => {
    const { promessa, liberar } = represada<Awaited<ReturnType<typeof moverEtapa>>>();
    moverEtapaMock.mockReturnValue(promessa);

    montar();
    expect(within(colunaNaTela("Novo lead")).getByText(/2 cards/)).toBeTruthy();

    await moverPeloMenu("Fernanda Lima", "Negociação");

    await waitFor(() => {
      // R$ 12.000,00 saíram de uma coluna e entraram na outra. Total parado
      // durante o movimento é a soma discordando dos cards à vista justamente
      // no instante em que alguém está conferindo o funil.
      expect(within(colunaNaTela("Novo lead")).getByText(/1 card · R\$\s*3\.000,00/)).toBeTruthy();
      expect(within(colunaNaTela("Negociação")).getByText(/1 card · R\$\s*12\.000,00/)).toBeTruthy();
    });

    // Liberar dentro de `act`: a resolução da promessa ainda dispara estado
    // (desmarca o card como pendente), e deixá-la para depois do teste vira
    // aviso de act — ruído que esconde o aviso legítimo do dia seguinte.
    await act(async () => {
      liberar({ ok: true, data: { opportunity: {}, auto_task: null, sla: {} } as never });
    });
  });

  it("avisa quando a etapa nova criou a tarefa automática de follow-up", async () => {
    moverEtapaMock.mockResolvedValue({
      ok: true,
      data: {
        opportunity: {},
        auto_task: { subject: "Ligar em 2 dias" },
        sla: {},
      },
    } as never);

    montar();
    await moverPeloMenu("Fernanda Lima", "Negociação");

    await waitFor(() =>
      expect(vi.mocked(toast.success)).toHaveBeenCalledWith(
        "Card movido para Negociação",
        expect.objectContaining({ description: "Tarefa criada: Ligar em 2 dias." }),
      ),
    );
  });
});

describe("PipelineKanban — rollback", () => {
  it("repõe o card na coluna de origem quando a API recusa", async () => {
    moverEtapaMock.mockResolvedValue({
      ok: false,
      code: "OPPORTUNITY_ALREADY_CLOSED",
      message: "já fechada",
      details: {},
    });

    montar();
    await moverPeloMenu("Fernanda Lima", "Negociação");

    await waitFor(() => expect(cardsDe("Novo lead")).toEqual(["Fernanda Lima", "Rui Barros"]));
    expect(cardsDe("Negociação")).toEqual([]);
  });

  it("repõe também contagem e soma das duas colunas", async () => {
    moverEtapaMock.mockResolvedValue({
      ok: false,
      code: "INVALID_STATE_TRANSITION",
      message: "outro moveu antes",
      details: { current_stage_id: "e-negociacao" },
    });

    montar();
    await moverPeloMenu("Fernanda Lima", "Negociação");

    await waitFor(() => {
      expect(within(colunaNaTela("Novo lead")).getByText(/2 cards · R\$\s*15\.000,00/)).toBeTruthy();
      expect(within(colunaNaTela("Negociação")).getByText(/0 cards · R\$\s*0,00/)).toBeTruthy();
    });
  });

  it("diz o que houve, em vez de o card voltar em silêncio", async () => {
    moverEtapaMock.mockResolvedValue({
      ok: false,
      code: "OPPORTUNITY_ALREADY_CLOSED",
      message: "já fechada",
      details: {},
    });

    montar();
    await moverPeloMenu("Fernanda Lima", "Negociação");

    await waitFor(() =>
      expect(vi.mocked(toast.error)).toHaveBeenCalledWith(
        "Oportunidade já fechada",
        expect.objectContaining({
          description: expect.stringContaining("já foi ganha ou perdida") as unknown as string,
        }),
      ),
    );
  });
});

describe("PipelineKanban — colunas terminais", () => {
  it("não move o card para Ganho: abre o diálogo, porque ganhar cria reserva", async () => {
    montar();
    await moverPeloMenu("Fernanda Lima", "Ganho");

    expect(await screen.findByRole("dialog")).toBeTruthy();
    expect(screen.getByText("Ganhar a oportunidade")).toBeTruthy();

    // Nem requisição de etapa, nem movimento otimista: `/stage` recusaria o
    // destino terminal, e mover o card na tela seria mentir sobre o estado.
    expect(moverEtapaMock).not.toHaveBeenCalled();
    expect(cardsDe("Novo lead")).toEqual(["Fernanda Lima", "Rui Barros"]);
    expect(cardsDe("Ganho")).toEqual([]);
  });

  it("o botão de ganhar continua clicável mesmo o card não sabendo do orçamento", async () => {
    // Regressão: `CardDaOportunidade` não carrega `quote_id` (o contrato o
    // mantém fora por peso). Tratar essa ausência como "não tem orçamento"
    // desabilitaria o botão em **todo** card do quadro — e o quadro é de onde
    // vem a maioria dos fechamentos. Quem decide se há orçamento vigente é o
    // servidor, que recusa com QUOTE_REQUIRED_TO_WIN sem mudar nada.
    montar();
    await moverPeloMenu("Fernanda Lima", "Ganho");

    await screen.findByRole("dialog");
    const botao = screen.getByRole("button", { name: /Ganhar e criar a reserva/ });
    expect((botao as HTMLButtonElement).disabled).toBe(false);
  });

  it("não move o card para Perdido: pede o motivo antes", async () => {
    montar();
    await moverPeloMenu("Fernanda Lima", "Perdido");

    expect(await screen.findByRole("dialog")).toBeTruthy();
    expect(screen.getByLabelText(/^Motivo/)).toBeTruthy();

    expect(moverEtapaMock).not.toHaveBeenCalled();
    expect(cardsDe("Novo lead")).toEqual(["Fernanda Lima", "Rui Barros"]);
  });
});


/** O quadro como o servidor o devolve DEPOIS de o colega arrastar a Fernanda —
 *  é isto que o refetch do tempo real traz de volta. */
function quadroDoColega(): QuadroKanban {
  const base = quadro();
  return {
    ...base,
    columns: [
      coluna(NOVO, [card("op-2", "Rui Barros", 300_000)]),
      coluna(NEGOCIACAO, [FERNANDA]),
      coluna(GANHO, []),
      coluna(PERDIDO, []),
    ],
    totals: { count: 1, amount_cents: 300_000 },
  };
}

/**
 * O funil ao vivo — a dívida **D10**.
 *
 * O barramento existia inteiro e ninguém o consumia: `whv_crm` com dois
 * gatilhos, `topics=crm` servido e conferido contra a matriz, e um único
 * consumidor do hook no painel (o mapa). O efeito na casa: duas pessoas no
 * funil não veem o trabalho uma da outra, e quem está com a tela aberta liga
 * para o cliente que o colega acabou de ganhar.
 *
 * O teste com forma obrigatória é o terceiro. Os treze testes do hook de SSE
 * ficaram verdes durante um defeito que abria **1957 conexões em 9 segundos**
 * porque todos passavam uma fábrica estável de módulo — a única forma que não
 * podia falhar. Aqui a fábrica muda de identidade a cada render, que é o que a
 * minificação faz com o valor default de um parâmetro, e a cobrança é uma
 * conexão só.
 */
describe("PipelineKanban — o quadro se move sozinho", () => {
  beforeEach(() => {
    // `shouldAdvanceTime` porque a janela de agrupamento é temporizador e o
    // `waitFor` do testing-library é temporizador: sem isso um espera pelo
    // outro para sempre.
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("assina só o tópico crm, e pelo BFF — nunca a API direto", () => {
    // `EventSource` não manda cabeçalho e o contrato recusa token em query
    // string (ele pararia no log do proxy). O cookie httpOnly só viaja em mesma
    // origem, então o caminho é a Route Handler do painel. E o kanban não pede
    // `calendar`: evento de bloqueio ele descartaria.
    montar();
    expect(fonte().url).toBe("/api/stream?topics=crm");
  });

  it("o card que o colega moveu na outra aba chega sem F5", async () => {
    buscarQuadroMock.mockResolvedValue({ ok: true, data: quadroDoColega() });

    montar();
    expect(cardsDe("Novo lead")).toEqual(["Fernanda Lima", "Rui Barros"]);
    expect(screen.getByText(/2 negócios · R\$\s*15\.000,00/)).toBeTruthy();

    fonte().emitir("crm", oportunidade("op-1", 4_812));

    // A janela de agrupamento é o que junta a rajada de uma transação numa
    // requisição só; antes dela não sai nada.
    expect(buscarQuadroMock).not.toHaveBeenCalled();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });

    // O evento chega magro: quem traz o card é o refetch autenticado, com o
    // MESMO recorte que está na URL — que é onde o `scope='own'` do corretor é
    // aplicado.
    expect(buscarQuadroMock).toHaveBeenCalledTimes(1);
    expect(buscarQuadroMock).toHaveBeenCalledWith({});

    await waitFor(() => expect(cardsDe("Negociação")).toEqual(["Fernanda Lima"]));
    expect(cardsDe("Novo lead")).toEqual(["Rui Barros"]);
    // O total do cabeçalho anda junto: ele é o primeiro número que a gestão
    // olha, e um total parado sobre colunas que se mexeram é pior do que a tela
    // velha inteira, porque parece atual.
    expect(screen.getByText(/1 negócio aberto · R\$\s*3\.000,00/)).toBeTruthy();
  });

  it("não reabre a conexão quando só a identidade de criarFonte muda", () => {
    // Regressão medida em produção: `criarFonte` chegava ao efeito pelo valor
    // default de um parâmetro, e a identidade de um default NÃO sobrevive à
    // minificação. Em `next dev` o efeito rodava uma vez; no build ele passou a
    // reabrir a conexão a cada repintura — ~220 aberturas por segundo com o
    // painel parado. Um consumidor novo do hook é exatamente a ocasião de o
    // defeito voltar, então a cobrança é feita daqui, pelo componente.
    const tela = () => (
      <PipelineKanban
        quadro={quadro()}
        motivos={MOTIVOS}
        permissoes={{ editar: true }}
        // Objeto novo a cada render, como o RSC entrega depois de um refresh.
        filtros={{}}
        // Função nova a cada render, de propósito.
        criarFonte={(url) => new FonteFalsa(url)}
      />
    );

    const { rerender } = render(tela());
    for (let i = 0; i < 5; i += 1) rerender(tela());

    expect(FonteFalsa.abertas).toBe(1);
  });

  it("evento que não é oportunidade não custa uma requisição", async () => {
    // O canal `whv_crm` só publica `opportunity` hoje, mas o envelope é
    // compartilhado e `lead` e `activity` já estão no vocabulário do cliente.
    // Sem o filtro, uma nota registrada por outra pessoa recarregaria o quadro
    // de todo mundo.
    buscarQuadroMock.mockResolvedValue({ ok: true, data: quadro() });

    montar();
    fonte().emitir("crm", JSON.stringify({ topic: "crm", entity: "lead", id: "l-1", v: 9 }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });

    expect(buscarQuadroMock).not.toHaveBeenCalled();
  });

  it("atualização que falha não apaga o quadro — ela se anuncia", async () => {
    // Dado velho e rotulado como velho é melhor que tela vazia: o operador
    // continua vendo o último desenho que deu certo e sabe que ele é o último.
    buscarQuadroMock.mockResolvedValue({
      ok: false,
      code: "FORBIDDEN",
      message: "sem permissão",
      details: {},
    });

    montar();
    fonte().emitir("crm", oportunidade("op-1", 4_813));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });

    expect(cardsDe("Novo lead")).toEqual(["Fernanda Lima", "Rui Barros"]);
    expect(await screen.findByText(/A última atualização automática falhou/)).toBeTruthy();
  });
});
