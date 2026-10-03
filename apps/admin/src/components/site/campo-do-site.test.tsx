import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/app/(app)/app/site/acoes", () => ({
  salvarCampoDoSite: vi.fn(),
  restaurarCampoDoSite: vi.fn(),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() }, Toaster: () => null }));

import { CampoDoSiteEditor, DICA_DO_TITULO, type AcoesDoSite } from "@/components/site/campo-do-site";
import type { CampoDoSite } from "@/lib/site/tipos";

/**
 * O campo do site decide três coisas que o gestor vê: quando dá para salvar,
 * quando aparece "Restaurar original" e o que é mandado para gravar. A API
 * confere de novo; aqui se protege a tela de oferecer o gesto errado.
 */

function titulo(parcial: Partial<CampoDoSite> = {}): CampoDoSite {
  return {
    key: "inicio.titulo",
    label: "Título da capa",
    kind: "titulo",
    help: "A frase grande do topo.",
    max: 200,
    value: "O litoral do Piauí em *exclusividade*",
    default_value: "O litoral do Piauí em *exclusividade*",
    is_default: true,
    ...parcial,
  };
}

function acoes(): AcoesDoSite & { salvar: ReturnType<typeof vi.fn>; restaurar: ReturnType<typeof vi.fn> } {
  return {
    salvar: vi.fn(async (chave: string, valor: unknown) => ({
      ok: true as const,
      data: { ...titulo(), key: chave, value: valor as string, is_default: false },
    })),
    restaurar: vi.fn(async () => ({ ok: true as const, data: null })),
  };
}

describe("CampoDoSiteEditor", () => {
  it("Salvar só acende quando o texto muda; original não oferece restaurar", () => {
    render(<CampoDoSiteEditor campo={titulo()} podeEditar acoes={acoes()} />);
    expect(screen.getByText("Texto original")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Restaurar original/ })).toBeNull();
    const salvar = screen.getByRole("button", { name: "Salvar" }) as HTMLButtonElement;
    expect(salvar.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Título da capa"), { target: { value: "Novo *título*" } });
    expect(salvar.disabled).toBe(false);
  });

  it("mostra a dica do asterisco, a prévia com destaque e a contagem de letras", () => {
    render(<CampoDoSiteEditor campo={titulo()} podeEditar acoes={acoes()} />);
    expect(screen.getByText(DICA_DO_TITULO)).toBeTruthy();
    expect(document.querySelector("[data-previa] em")?.textContent).toBe("exclusividade");
    expect(screen.getByText("37 de 200 letras")).toBeTruthy();
  });

  it("passou do limite: Salvar fica apagado", () => {
    render(<CampoDoSiteEditor campo={titulo({ max: 10 })} podeEditar acoes={acoes()} />);
    fireEvent.change(screen.getByLabelText("Título da capa"), { target: { value: "x".repeat(11) } });
    expect((screen.getByRole("button", { name: "Salvar" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("salvar manda o texto e o campo vira Editado", async () => {
    const a = acoes();
    render(<CampoDoSiteEditor campo={titulo()} podeEditar acoes={a} />);
    fireEvent.change(screen.getByLabelText("Título da capa"), { target: { value: "Novo *título*" } });
    fireEvent.click(screen.getByRole("button", { name: "Salvar" }));
    await waitFor(() => expect(screen.getByText("Editado")).toBeTruthy());
    expect(a.salvar).toHaveBeenCalledWith("inicio.titulo", "Novo *título*");
    expect(screen.getByRole("button", { name: /Restaurar original/ })).toBeTruthy();
  });

  it("recusa da API aparece em linguagem do gestor, pela frase do campo", async () => {
    const a = acoes();
    a.salvar.mockResolvedValueOnce({ ok: false, code: "VALIDATION_ERROR", message: "x", details: { value: "Texto longo demais." } });
    render(<CampoDoSiteEditor campo={titulo()} podeEditar acoes={a} />);
    fireEvent.change(screen.getByLabelText("Título da capa"), { target: { value: "Outro" } });
    fireEvent.click(screen.getByRole("button", { name: "Salvar" }));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("Texto longo demais."));
  });

  it("sem permissão de editar: nada de Salvar nem Restaurar", () => {
    render(<CampoDoSiteEditor campo={titulo({ is_default: false })} podeEditar={false} acoes={acoes()} />);
    expect(screen.queryByRole("button", { name: "Salvar" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Restaurar original/ })).toBeNull();
  });

  it("lista: adicionar, subir e salvar a lista inteira de uma vez", async () => {
    const a = acoes();
    const lista: CampoDoSite = {
      key: "faixa.itens",
      label: "Temas da faixa",
      kind: "lista",
      value: [{ texto: "Temporada" }, { texto: "Aniversários" }],
      default_value: [{ texto: "Temporada" }, { texto: "Aniversários" }],
      is_default: true,
      item_fields: [{ key: "texto", label: "Tema", kind: "texto" }],
    };
    render(<CampoDoSiteEditor campo={lista} podeEditar acoes={a} />);
    fireEvent.click(screen.getByRole("button", { name: "Subir o item 2" }));
    fireEvent.click(screen.getByRole("button", { name: /Adicionar/ }));
    const campos = screen.getAllByLabelText("Tema") as HTMLInputElement[];
    expect(campos.map((c) => c.value)).toEqual(["Aniversários", "Temporada", ""]);
    fireEvent.change(campos[2], { target: { value: "Réveillon" } });
    fireEvent.click(screen.getByRole("button", { name: "Salvar" }));
    await waitFor(() => expect(a.salvar).toHaveBeenCalled());
    expect(a.salvar).toHaveBeenCalledWith("faixa.itens", [{ texto: "Aniversários" }, { texto: "Temporada" }, { texto: "Réveillon" }]);
  });
});
