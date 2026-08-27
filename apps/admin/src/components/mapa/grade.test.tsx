import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { Produto, UnidadeDaComposicao } from "@/lib/api/comercial";
import { janelaDe } from "@/lib/mapa/janela";
import { montarGrupos } from "@/lib/mapa/linhas";
import type { CelulaDoMapa, LinhaDoMapa, StatusDaCelula } from "@/lib/mapa/tipos";

import { GradeDoMapa, type AlvoDaFaixa, type AlvoDoDia, type Selecao } from "./grade";

/**
 * A grade no tamanho real da casa: **8 unidades físicas + a linha sintética da
 * White House Completa, sobre 90 dias**. É o caso que `docs/ui.md` §9 chama de
 * "a tela mais cara do projeto", e o número que este arquivo mede é o que o
 * relatório cita.
 */

const DIAS = 90;
const JANELA = janelaDe("2026-01-01", DIAS);
const HOJE = "2026-01-08";

const UNIDADES = ["AP-01", "AP-02", "AP-03", "SP-01", "SP-02", "SP-03", "SP-04", "COB-01"] as const;

function produto(
  id: string,
  code: string,
  name: string,
  consumes: Produto["consumes"],
  sort: number,
): Produto {
  return {
    id,
    code,
    name,
    capacity: 6,
    consumes,
    cleaning_fee_cents: 0,
    description: null,
    sort_order: sort,
    active: true,
    created_at: "",
    updated_at: "",
  };
}

const membro = (code: string): UnidadeDaComposicao => ({
  unit_id: `u-${code}`,
  unit_code: code,
  unit_name: code,
  active: true,
});

const PRODUTOS = [
  produto("p-ap", "AP2S", "Apartamento 2 Suítes", "one_member", 1),
  produto("p-sp", "SP", "Suítes da Piscina", "one_member", 2),
  produto("p-cob", "COB", "White House Cobertura", "one_member", 3),
  produto("p-casa", "COMPLETA", "White House Completa", "all_members", 9),
];

const COMPOSICOES = {
  "p-ap": [membro("AP-01"), membro("AP-02"), membro("AP-03")],
  "p-sp": [membro("SP-01"), membro("SP-02"), membro("SP-03"), membro("SP-04")],
  "p-cob": [membro("COB-01")],
  "p-casa": UNIDADES.map(membro),
};

function celula(i: number, status: StatusDaCelula, extra: Partial<CelulaDoMapa> = {}): CelulaDoMapa {
  const date = JANELA.dias[i]!;
  return {
    date,
    // Sexta e sábado com fundo próprio: o tipo de data é o que a precedência do
    // tarifário manda, e a grade tem de pintar as 90 colunas com ele.
    date_type: [5, 6].includes(new Date(`${date}T00:00:00Z`).getUTCDay()) ? "fds" : "normal",
    status,
    stay_block_id: null,
    reservation_id: null,
    reservation_code: null,
    guest_name: null,
    ...extra,
  };
}

/**
 * Ocupação plausível: cada unidade com quatro estadias na faixa, uma delas
 * longa. Dá ~32 faixas no total, que é a ordem de grandeza real de um trimestre
 * cheio.
 */
function linhas(): LinhaDoMapa[] {
  return UNIDADES.map((code, u) => {
    const dias = JANELA.dias.map((_, i) => celula(i, "livre"));
    const estadias = [
      { inicio: 2 + u, noites: 12, status: "confirmed" as const },
      { inicio: 30 + u, noites: 5, status: "hold" as const },
      { inicio: 45 + u, noites: 3, status: "maintenance" as const },
      { inicio: 60 + u, noites: 8, status: "checked_out" as const },
    ];

    for (const [n, estadia] of estadias.entries()) {
      const bloco = `b-${code}-${n}`;
      for (let d = estadia.inicio; d < estadia.inicio + estadia.noites && d < DIAS; d += 1) {
        dias[d] = celula(d, estadia.status, {
          stay_block_id: bloco,
          reservation_id: estadia.status === "maintenance" ? null : `r-${code}-${n}`,
          reservation_code: estadia.status === "maintenance" ? null : `WH-2026-${u}${n}`,
          guest_name: estadia.status === "maintenance" ? null : "Maria de Souza",
        });
      }
    }

    return { unit_id: `u-${code}`, unit_code: code, unit_name: `Unidade ${code}`, days: dias };
  });
}

const GRUPOS = montarGrupos({
  produtos: PRODUTOS,
  composicoes: COMPOSICOES,
  linhas: linhas(),
  janela: JANELA,
});

type PropsDeTeste = {
  aoApontar: (alvo: AlvoDoDia | null) => void;
  aoAbrirFaixa: (alvo: AlvoDaFaixa) => void;
  aoSelecionar: (selecao: Selecao) => void;
  grupos: typeof GRUPOS;
};

function montar(overrides: Partial<PropsDeTeste> = {}) {
  const props: PropsDeTeste = {
    aoApontar: vi.fn(),
    aoAbrirFaixa: vi.fn(),
    aoSelecionar: vi.fn(),
    grupos: GRUPOS,
    ...overrides,
  };
  return { ...render(grade(props)), ...props };
}

function grade(props: PropsDeTeste) {
  return (
    <GradeDoMapa
      janela={JANELA}
      grupos={props.grupos}
      expiracoes={new Map()}
      hoje={HOJE}
      podeBloquear
      aoApontar={props.aoApontar}
      aoAbrirFaixa={props.aoAbrirFaixa}
      aoSelecionar={props.aoSelecionar}
    />
  );
}

/** No jsdom `getBoundingClientRect` é tudo zero e a medida cai no fallback de
 *  36px por coluna — o que torna `clientX` um índice de coluna determinístico. */
const COLUNA = 36;

/**
 * O jsdom não implementa `PointerEvent`, e o `fireEvent.pointerMove` do
 * testing-library acaba criando um evento **sem `clientX`** — o que faria o
 * teste medir `Math.floor(NaN)` e passar por acidente. Um `MouseEvent` com o
 * tipo de ponteiro carrega as coordenadas e é o que o React escuta (ele casa
 * ouvinte por NOME de evento, não por classe).
 */
function ponteiro(alvo: HTMLElement, tipo: string, clientX: number, button = 0): void {
  fireEvent(alvo, new MouseEvent(tipo, { bubbles: true, clientX, button }));
}

const mover = (alvo: HTMLElement, x: number) => ponteiro(alvo, "pointermove", x);
const descer = (alvo: HTMLElement, x: number, button = 0) => ponteiro(alvo, "pointerdown", x, button);
const subir = (alvo: HTMLElement, x: number) => ponteiro(alvo, "pointerup", x);

function pista(indiceDaLinha: number): HTMLElement {
  // A pista 0 é o cabeçalho de dias; as linhas começam em 1.
  const encontrada = document.querySelectorAll<HTMLElement>(".mapa-pista")[indiceDaLinha + 1];
  encontrada!.setPointerCapture = vi.fn();
  encontrada!.releasePointerCapture = vi.fn();
  return encontrada!;
}

describe("GradeDoMapa — 9 linhas × 90 dias", () => {
  it("desenha as 8 unidades agrupadas por produto mais a linha sintética da casa", () => {
    montar();
    // Cada pista carrega o resumo textual da linha — é a leitura acessível de
    // um desenho que não tem semântica de tabela.
    expect(screen.getAllByRole("img")).toHaveLength(9);
    // A última pista é a linha derivada da casa inteira — ela fecha o mapa
    // porque é a leitura das outras oito, não uma nona unidade.
    const resumo = screen.getAllByRole("img").at(-1)?.getAttribute("aria-label") ?? "";
    expect(resumo).toContain("COMPLETA White House Completa");
  });

  it("virtualiza as COLUNAS: 90 dias não viram 90 nós por linha", () => {
    // 366 × 9 = 3.294 células de fundo repintadas a cada evento derrubam a aba.
    // O custo tem de ser proporcional ao que se vê, não ao que existe.
    montar();
    const colunasPorLinha = document.querySelectorAll(".mapa-pista")[1]!.querySelectorAll(".mapa-coluna");
    expect(colunasPorLinha.length).toBeLessThan(DIAS);
    expect(colunasPorLinha.length).toBeGreaterThan(20);
  });

  it("uma estadia de 12 noites é UMA barra, não doze retângulos", () => {
    montar();
    const primeiraLinha = document.querySelectorAll(".mapa-pista")[1]!;
    const confirmadas = primeiraLinha.querySelectorAll("[data-status='confirmed']");
    expect(confirmadas).toHaveLength(1);
    expect((confirmadas[0] as HTMLElement).style.width).toBe("calc(var(--mapa-dia) * 12 - 4px)");
  });

  it("a linha da casa inteira acusa o dia em que uma unidade está vendida", () => {
    montar();
    const pistas = document.querySelectorAll(".mapa-pista");
    const ultima = pistas[pistas.length - 1]!;
    expect(ultima.querySelectorAll("[data-status='parcial']").length).toBeGreaterThan(0);
  });

  it("pinta 90 dias × 9 linhas em menos de 300 ms", () => {
    // O requisito de `docs/ui.md` §9. A medição é do trabalho do React + DOM no
    // jsdom, que é MAIS lento que o navegador para operação de DOM — passar
    // aqui é um piso, não um teto otimista. O número medido vai no relatório.
    const inicio = performance.now();
    montar();
    const decorrido = performance.now() - inicio;

    console.log(`[mapa] 9 linhas × ${DIAS} dias renderizadas em ${decorrido.toFixed(1)} ms`);
    expect(decorrido).toBeLessThan(300);
  });
});

describe("GradeDoMapa — o ponteiro", () => {
  it("avisa uma vez por COLUNA atravessada, não por pixel percorrido", () => {
    // `pointermove` dispara a cada poucos pixels. Sem a comparação contra o
    // último dia apontado, atravessar uma célula custaria dez repinturas das
    // nove linhas para desenhar exatamente a mesma coisa.
    const { aoApontar } = montar();
    const linha = pista(0);

    mover(linha, 4);
    mover(linha, 12);
    mover(linha, 30);
    expect(aoApontar).toHaveBeenCalledTimes(1);

    mover(linha, COLUNA + 5);
    expect(aoApontar).toHaveBeenCalledTimes(2);
    expect(aoApontar).toHaveBeenLastCalledWith(expect.objectContaining({ indice: 1 }));
  });

  it("clicar numa reserva abre o detalhe em vez de começar um bloqueio", () => {
    const { aoAbrirFaixa, aoSelecionar } = montar();
    const linha = pista(0);

    // A AP-01 tem estadia confirmada a partir do índice 2.
    descer(linha, COLUNA * 3 + 5, 0);
    subir(linha, COLUNA * 3 + 5);

    expect(aoAbrirFaixa).toHaveBeenCalledTimes(1);
    expect(aoSelecionar).not.toHaveBeenCalled();
  });

  it("arrastar sobre dias livres propõe um bloqueio com `fim` EXCLUSIVO", () => {
    // Arrastar de 10 a 11 bloqueia as noites 10 e 11 e deixa 12 livre para
    // check-in — a mesma convenção half-open do `period` no banco. Escrevê-la
    // diferente aqui roubaria uma noite de quem opera.
    const { aoSelecionar } = montar();
    const linha = pista(0);

    descer(linha, COLUNA * 20 + 5, 0);
    mover(linha, COLUNA * 22 + 5);
    subir(linha, COLUNA * 22 + 5);

    expect(aoSelecionar).toHaveBeenCalledWith(expect.objectContaining({ inicio: 20, fim: 23 }));
  });

  it("arrastar de trás para a frente dá o mesmo período", () => {
    const { aoSelecionar } = montar();
    const linha = pista(0);

    descer(linha, COLUNA * 22 + 5, 0);
    mover(linha, COLUNA * 20 + 5);
    subir(linha, COLUNA * 20 + 5);

    expect(aoSelecionar).toHaveBeenCalledWith(expect.objectContaining({ inicio: 20, fim: 23 }));
  });

  it("a linha sintética da casa não vira bloqueio: não existe unidade 'casa inteira'", () => {
    const { aoSelecionar } = montar();
    const pistas = document.querySelectorAll<HTMLElement>(".mapa-pista");
    const sintetica = pistas[pistas.length - 1]!;
    sintetica.setPointerCapture = vi.fn();

    descer(sintetica, COLUNA * 85 + 5, 0);
    subir(sintetica, COLUNA * 85 + 5);

    expect(aoSelecionar).not.toHaveBeenCalled();
  });

  it("botão direito não começa arrasto — o menu de contexto abriria por cima", () => {
    const { aoSelecionar } = montar();
    const linha = pista(0);

    descer(linha, COLUNA * 20 + 5, 2);
    subir(linha, COLUNA * 20 + 5);

    expect(aoSelecionar).not.toHaveBeenCalled();
  });
});

describe("GradeDoMapa — o repinte que o tempo real dispara", () => {
  it("repõe a matriz inteira em menos de 300 ms", () => {
    // É o caminho do evento SSE: a ocupação chega nova e a grade repinta. O
    // requisito de ponta a ponta é 2 s incluindo a ida ao servidor, então o que
    // sobra para o desenho é o que se mede aqui.
    const { rerender } = montar();

    const outras = montarGrupos({
      produtos: PRODUTOS,
      composicoes: COMPOSICOES,
      linhas: linhas(),
      janela: JANELA,
    });

    const inicio = performance.now();
    rerender(
      grade({ aoApontar: vi.fn(), aoAbrirFaixa: vi.fn(), aoSelecionar: vi.fn(), grupos: outras }),
    );
    const decorrido = performance.now() - inicio;

    console.log(`[mapa] repinte por evento de tempo real em ${decorrido.toFixed(1)} ms`);
    expect(decorrido).toBeLessThan(300);
  });
});
