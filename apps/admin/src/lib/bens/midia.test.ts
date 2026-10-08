import { describe, expect, it } from "vitest";

import { conferirArquivo, enderecoDaFoto } from "@/lib/bens/midia";
import { consultaDoCatalogo, lerFiltrosDoCatalogo, linkDeExportacao } from "@/lib/bens/filtros";

const ID = "0b7c2f4e-0000-4000-8000-000000000000";

describe("a foto do inventário nunca é endereço público", () => {
  it("a grade carrega a miniatura pelo intermediário autenticado", () => {
    const midia = { id: ID, thumb_url: `/api/v1/inventory/media/${ID}?size=thumb` };
    expect(enderecoDaFoto(midia)).toBe(`/api/inventario/midia/${ID}?size=thumb`);
  });

  it("quando a miniatura falhou, thumb_url é o original — e é o original que se pede", () => {
    const midia = { id: ID, thumb_url: `/api/v1/inventory/media/${ID}` };
    expect(enderecoDaFoto(midia)).toBe(`/api/inventario/midia/${ID}`);
  });

  it("a ampliação pede o original", () => {
    expect(enderecoDaFoto({ id: ID, thumb_url: `/x?size=thumb` }, "original")).toBe(`/api/inventario/midia/${ID}`);
  });

  it("o endereço da API nunca vai direto para o <img> — o navegador não tem o token", () => {
    expect(enderecoDaFoto({ id: ID, thumb_url: `/api/v1/inventory/media/${ID}?size=thumb` })).not.toContain("/api/v1/");
  });
});

describe("conferência local do arquivo (a API confere de novo, pelos bytes)", () => {
  it("aceita JPEG, PNG e WebP até 15 MB", () => {
    expect(conferirArquivo({ type: "image/jpeg", size: 3_000_000 })).toBeNull();
    expect(conferirArquivo({ type: "image/webp", size: 15 * 1024 * 1024 })).toBeNull();
  });

  it("recusa vídeo, HEIC e foto acima de 15 MB", () => {
    expect(conferirArquivo({ type: "video/mp4", size: 1000 })).toMatch(/JPG, PNG ou WebP/);
    expect(conferirArquivo({ type: "image/heic", size: 1000 })).toMatch(/JPG, PNG ou WebP/);
    expect(conferirArquivo({ type: "image/jpeg", size: 15 * 1024 * 1024 + 1 })).toMatch(/15 MB/);
  });

  it("tipo vazio (câmera de alguns celulares) passa — quem decide é a API", () => {
    expect(conferirArquivo({ type: "", size: 2000 })).toBeNull();
  });
});

describe("o recorte do catálogo na URL", () => {
  it("“só sem foto” é a fila de trabalho: has_photo=false", () => {
    const { filtros } = lerFiltrosDoCatalogo({ foto: "sem" });
    expect(consultaDoCatalogo(filtros).has_photo).toBe(false);
  });

  it("o padrão é o catálogo em uso (active=true)", () => {
    const { filtros } = lerFiltrosDoCatalogo({});
    expect(consultaDoCatalogo(filtros).active).toBe(true);
    expect(consultaDoCatalogo(lerFiltrosDoCatalogo({ situacao: "todos" }).filtros).active).toBeUndefined();
  });

  it("categoria inventada é descartada com aviso, em vez de virar 422", () => {
    const { filtros, avisos } = lerFiltrosDoCatalogo({ categoria: "panelas" });
    expect(filtros.categoria).toBe("");
    expect(avisos).toHaveLength(1);
  });

  it("ambiente sem unidade cai — a barra não teria como mostrá-lo", () => {
    const { filtros } = lerFiltrosDoCatalogo({ ambiente: "9f1c1e2a-0d3b-4f6a-9a1e-2b7c5d8e0f11" });
    expect(filtros.ambiente).toBe("");
  });

  it("a planilha leva só os filtros que /inventory/export conhece", () => {
    expect(linkDeExportacao({ unidade: "u", categoria: "louca" })).toBe("/api/inventario/exportar?unit_id=u&category=louca");
    expect(linkDeExportacao({})).toBe("/api/inventario/exportar");
  });
});
