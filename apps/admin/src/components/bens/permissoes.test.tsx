import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { CatalogoDeBens } from "@/components/bens/catalogo";
import { TelaDeContagem } from "@/components/bens/contagem";
import { InventarioDaUnidadeView } from "@/components/bens/unidade";
import type { Acao, Permissao } from "@/lib/api/types";
import { permissoesDoInventario } from "@/lib/bens/permissoes";
import type { Bem, ConferenciaCompleta, InventarioDaUnidade } from "@/lib/bens/tipos";

/**
 * `can()` esconde o que o perfil não pode fazer — **sempre** lido em
 * `inventory.goods`, nunca em `inventory`.
 *
 * Esconder é cortesia (a API recusa com 403 de qualquer jeito), mas o botão que
 * aparece e falha ensina a pessoa que "o sistema está com defeito". E o caso
 * que este módulo nasceu evitando é o da troca de recurso: quem tem o cadastro
 * comercial inteiro (`inventory`) não ganha, por tabela, o poder de apagar
 * cômodo ou fechar conferência.
 */

vi.mock("sonner", () => ({ Toaster: () => null, toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() } }));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/app/inventario",
  useSearchParams: () => new URLSearchParams(),
}));
vi.mock("@/lib/bens/acoes", () => ({
  salvarBem: vi.fn(),
  alternarAtivoDoBem: vi.fn(),
  apagarBem: vi.fn(),
  salvarGaleria: vi.fn(),
  salvarAmbiente: vi.fn(),
  alternarAtivoDoAmbiente: vi.fn(),
  apagarAmbiente: vi.fn(),
  colocarBem: vi.fn(),
  salvarColocacao: vi.fn(),
  tirarBem: vi.fn(),
  copiarInventario: vi.fn(),
  abrirConferencia: vi.fn(),
  contarLinha: vi.fn(),
  fecharConferencia: vi.fn(),
  cancelarConferencia: vi.fn(),
  registrarAvaria: vi.fn(),
  resolverAvaria: vi.fn(),
  apagarAvaria: vi.fn(),
}));

function matriz(recurso: string, acoes: Acao[]): Permissao[] {
  return acoes.map((action) => ({ resource: recurso, action, scope: "all" }));
}

const TUDO: Acao[] = ["ver", "criar", "editar", "excluir"];
const SO_VER = permissoesDoInventario(matriz("inventory.goods", ["ver"]));
const COMPLETO = permissoesDoInventario(matriz("inventory.goods", TUDO));

const UNIDADE = "11111111-1111-4111-8111-111111111111";
const AMBIENTE = "22222222-2222-4222-8222-222222222222";
const ITEM = "33333333-3333-4333-8333-333333333333";

const INVENTARIO: InventarioDaUnidade = {
  unit: { id: UNIDADE, code: "AP-01", name: "Duplex 01" },
  rooms: [
    {
      id: AMBIENTE,
      unit_id: UNIDADE,
      name: "Cozinha",
      kind: "cozinha",
      sort_order: 0,
      active: true,
      items: [{ id: `${AMBIENTE}_${ITEM}`, room_id: AMBIENTE, item_id: ITEM, item_name: "Prato raso", expected_qty: 12 }],
    },
  ],
  totals: { rooms: 1, items: 1, expected_qty: 12, open_issues: 0 },
  open_count: null,
  last_closed_count: null,
};

const BENS: Bem[] = [{ id: ITEM, name: "Prato raso", category: "louca", unit_measure: "un", active: true }];

function conferencia(status: ConferenciaCompleta["status"] = "aberta"): ConferenciaCompleta {
  return {
    id: "c1",
    unit_id: UNIDADE,
    unit_code: "AP-01",
    status,
    opened_at: "2026-10-07T12:00:00Z",
    closed_at: status === "aberta" ? null : "2026-10-07T13:00:00Z",
    progress: { lines: 1, counted: 0, pending: 1, diverging: 0 },
    rooms: [
      {
        room_id: AMBIENTE,
        room_name: "Cozinha",
        lines: [{ id: "l1", count_id: "c1", room_id: AMBIENTE, item_id: ITEM, item_name: "Prato raso", expected_qty: 12, counted_qty: null }],
      },
    ],
  };
}

describe("a matriz é lida em inventory.goods", () => {
  it("o cadastro comercial inteiro (`inventory`) não acende nada aqui", () => {
    expect(permissoesDoInventario(matriz("inventory", TUDO))).toEqual({
      ver: false,
      criar: false,
      editar: false,
      excluir: false,
    });
  });

  it("cada ação vem da sua linha da matriz", () => {
    expect(permissoesDoInventario(matriz("inventory.goods", ["ver", "editar"]))).toMatchObject({
      ver: true,
      criar: false,
      editar: true,
      excluir: false,
    });
  });
});

describe("catálogo", () => {
  it("sem criar, não há 'Novo bem'", () => {
    render(<CatalogoDeBens bens={BENS} permissoes={SO_VER} temFiltro={false} soSemFoto={false} />);
    expect(screen.queryByRole("button", { name: /novo bem/i })).toBeNull();
  });

  it("com criar, há", () => {
    render(<CatalogoDeBens bens={BENS} permissoes={COMPLETO} temFiltro={false} soSemFoto={false} />);
    expect(screen.getByRole("button", { name: /novo bem/i })).toBeTruthy();
  });
});

describe("inventário da unidade", () => {
  function montar(permissoes: typeof SO_VER) {
    render(<InventarioDaUnidadeView inventario={INVENTARIO} catalogo={BENS} unidades={[]} permissoes={permissoes} filtrado={false} />);
  }

  it("só ver: lê, imprime e exporta — e nada mais", () => {
    montar(SO_VER);
    for (const nome of [/abrir conferência/i, /novo ambiente/i, /copiar de outra unidade/i, /registrar avaria/i, /colocar bem/i]) {
      expect(screen.queryByRole("button", { name: nome }), String(nome)).toBeNull();
    }
    const cozinha = screen.getByRole("region", { name: "Cozinha" });
    expect(within(cozinha).queryAllByRole("button")).toHaveLength(0);
    expect(screen.getByRole("link", { name: /imprimir lista/i })).toBeTruthy();
    expect(screen.getByRole("link", { name: /exportar planilha/i })).toBeTruthy();
  });

  it("completo: cada ação aparece", () => {
    montar(COMPLETO);
    expect(screen.getByRole("button", { name: /abrir conferência/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /novo ambiente/i })).toBeTruthy();
    const cozinha = screen.getByRole("region", { name: "Cozinha" });
    expect(within(cozinha).getByRole("button", { name: /colocar bem/i })).toBeTruthy();
    expect(within(cozinha).getByRole("button", { name: "Apagar Cozinha" })).toBeTruthy();
    expect(within(cozinha).getByRole("button", { name: /mudar a quantidade de prato raso/i })).toBeTruthy();
  });

  it("editar sem excluir: muda quantidade, não apaga", () => {
    montar(permissoesDoInventario(matriz("inventory.goods", ["ver", "editar"])));
    const cozinha = screen.getByRole("region", { name: "Cozinha" });
    expect(within(cozinha).getByRole("button", { name: /mudar a quantidade/i })).toBeTruthy();
    expect(within(cozinha).queryByRole("button", { name: /apagar/i })).toBeNull();
    expect(within(cozinha).queryByRole("button", { name: /tirar .* do ambiente/i })).toBeNull();
  });
});

describe("conferência", () => {
  it("só ver: nenhum contador, nem fechar, nem cancelar", () => {
    render(<TelaDeContagem conferencia={conferencia()} permissoes={SO_VER} />);
    expect(screen.queryByRole("button", { name: /um a mais/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /fechar conferência/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /^cancelar$/i })).toBeNull();
  });

  it("editar sem excluir: conta e fecha, mas não cancela", () => {
    render(<TelaDeContagem conferencia={conferencia()} permissoes={permissoesDoInventario(matriz("inventory.goods", ["ver", "editar"]))} />);
    expect(screen.getByRole("button", { name: /um a mais de prato raso/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /fechar conferência/i })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^cancelar$/i })).toBeNull();
  });

  it("conferência fechada não se conta, nem com todas as permissões", () => {
    render(<TelaDeContagem conferencia={conferencia("fechada")} permissoes={COMPLETO} />);
    expect(screen.queryByRole("button", { name: /um a mais/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /fechar conferência/i })).toBeNull();
  });
});
