import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";

import { PipelineKanban } from "@/components/crm/kanban";
import { moverEtapa } from "@/lib/crm/acoes";
import type { CardDaOportunidade, ColunaDoKanban, EtapaDoFunil, QuadroKanban } from "@/lib/crm/tipos";

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
  moverEtapa: vi.fn(),
  ganharOportunidade: vi.fn(),
  perderOportunidade: vi.fn(),
  atualizarOportunidade: vi.fn(),
  criarAtividade: vi.fn(),
  concluirAtividade: vi.fn(),
}));

const moverEtapaMock = vi.mocked(moverEtapa);

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

function montar() {
  render(
    <PipelineKanban
      quadro={quadro()}
      motivos={[{ id: "m-1", label: "Preço acima do orçamento", active: true, usage_count: 3 }]}
      permissoes={{ editar: true }}
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
