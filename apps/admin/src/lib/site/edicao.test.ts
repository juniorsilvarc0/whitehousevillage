import { describe, expect, it } from "vitest";

import {
  asteriscoSemPar,
  conferirArquivo,
  enderecoDePrevia,
  itemVazio,
  limiteDe,
  listaComFotoFaltando,
  marcadoresFaltando,
  mover,
  mudou,
  paragrafos,
  remover,
  trechosDoTitulo,
  valorParaGravar,
} from "@/lib/site/edicao";
import { mensagemDoSite } from "@/lib/site/mensagens";

const MB = 1024 * 1024;

describe("trechosDoTitulo — a prévia segue a regra do site", () => {
  it("*palavra* vira destaque e o resto fica como está", () => {
    expect(trechosDoTitulo("O litoral em *exclusividade*")).toEqual([
      { texto: "O litoral em ", destaque: false },
      { texto: "exclusividade", destaque: true },
    ]);
  });

  it("quebra de linha vira linha nova", () => {
    expect(trechosDoTitulo("Não é uma diária.\nÉ a casa *inteira*.")).toEqual([
      { texto: "Não é uma diária.", destaque: false },
      { quebra: true },
      { texto: "É a casa ", destaque: false },
      { texto: "inteira", destaque: true },
      { texto: ".", destaque: false },
    ]);
  });

  it("HTML digitado é só texto (a prévia nunca vira marcação)", () => {
    expect(trechosDoTitulo("<b>oi</b>")).toEqual([{ texto: "<b>oi</b>", destaque: false }]);
  });

  it("avisa asterisco sem par", () => {
    expect(asteriscoSemPar("um *dois")).toBe(true);
    expect(asteriscoSemPar("um *dois* três")).toBe(false);
  });
});

describe("paragrafos", () => {
  it("linha em branco separa parágrafos; linhas soltas ficam juntas", () => {
    expect(paragrafos("Um\ncontinua\n\n  \nDois\n")).toEqual(["Um\ncontinua", "Dois"]);
  });
});

describe("limiteDe", () => {
  it("usa o do catálogo e cai no do tipo quando falta", () => {
    expect(limiteDe({ kind: "texto", max: 80 })).toBe(80);
    expect(limiteDe({ kind: "titulo", max: null })).toBe(200);
    expect(limiteDe({ kind: "texto_longo", max: 0 })).toBe(2000);
    expect(limiteDe({ kind: "imagem", max: null })).toBeNull();
  });
});

describe("listas", () => {
  it("sobe e desce sem alterar a original e sem sair dos limites", () => {
    const original = ["a", "b", "c"];
    expect(mover(original, 1, -1)).toEqual(["b", "a", "c"]);
    expect(mover(original, 1, 1)).toEqual(["a", "c", "b"]);
    expect(mover(original, 0, -1)).toEqual(["a", "b", "c"]);
    expect(mover(original, 2, 1)).toEqual(["a", "b", "c"]);
    expect(original).toEqual(["a", "b", "c"]);
  });

  it("remove pelo índice", () => {
    expect(remover(["a", "b", "c"], 1)).toEqual(["a", "c"]);
  });

  it("item novo nasce com cada subcampo vazio", () => {
    expect(itemVazio([{ key: "titulo", label: "T", kind: "texto" }, { key: "foto", label: "F", kind: "imagem" }])).toEqual({
      titulo: "",
      foto: null,
    });
  });

  it("acusa item sem foto", () => {
    const subs = [{ key: "foto", label: "F", kind: "imagem" as const }];
    expect(listaComFotoFaltando([{ foto: { media_id: "x" } }], subs)).toBe(false);
    expect(listaComFotoFaltando([{ foto: null }], subs)).toBe(true);
  });
});

describe("conferirArquivo — antes de enviar", () => {
  it("aceita foto JPG/PNG/WebP até 15 MB", () => {
    expect(conferirArquivo({ type: "image/jpeg", size: 3 * MB, name: "a.jpg" }, "imagem")).toBeNull();
    expect(conferirArquivo({ type: "image/webp", size: 15 * MB, name: "a.webp" }, "imagem")).toBeNull();
  });

  it("recusa foto grande ou de outro tipo, em linguagem do gestor", () => {
    expect(conferirArquivo({ type: "image/jpeg", size: 16 * MB, name: "a.jpg" }, "imagem")).toMatch(/grande demais.*15 MB/);
    expect(conferirArquivo({ type: "image/gif", size: MB, name: "a.gif" }, "imagem")).toMatch(/JPG, PNG ou WebP/);
    expect(conferirArquivo({ type: "video/mp4", size: MB, name: "a.mp4" }, "imagem")).toMatch(/JPG, PNG ou WebP/);
  });

  it("vídeo MP4/WebM até 300 MB", () => {
    expect(conferirArquivo({ type: "video/mp4", size: 299 * MB, name: "a.mp4" }, "video")).toBeNull();
    expect(conferirArquivo({ type: "video/quicktime", size: MB, name: "a.mov" }, "video")).toMatch(/MP4 ou WebM/);
    expect(conferirArquivo({ type: "video/webm", size: 301 * MB, name: "a.webm" }, "video")).toMatch(/300 MB/);
  });

  it("sem tipo informado pelo navegador, olha a extensão", () => {
    expect(conferirArquivo({ type: "", size: MB, name: "FOTO.JPEG" }, "imagem")).toBeNull();
  });

  it("nenhuma mensagem fala a língua de TI", () => {
    const msgs = [
      conferirArquivo({ type: "image/gif", size: MB, name: "a.gif" }, "imagem"),
      conferirArquivo({ type: "image/png", size: 20 * MB, name: "a.png" }, "imagem"),
      mensagemDoSite({ code: "NOT_FOUND", details: {} }),
      mensagemDoSite({ code: "VALIDATION_ERROR", details: {} }, 413),
      mensagemDoSite({ code: "NETWORK_ERROR", details: {} }),
    ].join(" ");
    expect(msgs).not.toMatch(/\b(API|upload|endpoint|cache|JSON|MIME|HTTP|erro \d+|código)\b/i);
  });
});

describe("valorParaGravar — só o que a API grava", () => {
  it("foto: id e descrição, sem o endereço", () => {
    expect(valorParaGravar("imagem", { media_id: "m1", url: "/x", alt: " Piscina " })).toEqual({ media_id: "m1", alt: "Piscina" });
  });

  it("vídeo: só o id", () => {
    expect(valorParaGravar("video", { media_id: "v1", url: "/v" })).toEqual({ media_id: "v1" });
  });

  it("lista: subcampos do catálogo, foto sem endereço, campo estranho fora", () => {
    const subs = [
      { key: "titulo", label: "T", kind: "texto" as const },
      { key: "foto", label: "F", kind: "imagem" as const },
    ];
    expect(valorParaGravar("lista", [{ titulo: "A", foto: { media_id: "m", url: "/u", alt: "" }, intruso: "x" }], subs)).toEqual([
      { titulo: "A", foto: { media_id: "m", alt: "" } },
    ]);
  });

  it("mudou compara o que seria gravado, não o endereço de prévia", () => {
    expect(mudou("imagem", { media_id: "m", url: "/a", alt: "x" }, { media_id: "m", url: "/b", alt: "x" })).toBe(false);
    expect(mudou("imagem", { media_id: "m", alt: "x" }, { media_id: "m", alt: "y" })).toBe(true);
    expect(mudou("texto", "a", "a")).toBe(false);
  });
});

describe("enderecoDePrevia", () => {
  it("arquivo enviado passa pela rota do painel", () => {
    expect(enderecoDePrevia({ media_id: "abc" })).toBe("/api/site/midia/abc");
  });
  it("original do site é buscado no site", () => {
    expect(enderecoDePrevia({ url: "/assets/logo.png" })).toBe("https://www.whitehousevillage.com.br/assets/logo.png");
  });
  it("sem arquivo, sem prévia; endereço estranho é ignorado", () => {
    expect(enderecoDePrevia(null)).toBeNull();
    expect(enderecoDePrevia({ url: "javascript:alert(1)" })).toBeNull();
    expect(enderecoDePrevia({ url: "//outro.host/x.png" })).toBeNull();
  });
});

describe("mensagemDoSite", () => {
  it("usa a frase do campo que a API escreveu para o gestor", () => {
    expect(mensagemDoSite({ code: "VALIDATION_ERROR", details: { file: "A foto precisa ser JPG, PNG ou WebP, até 15 MB." } })).toBe(
      "A foto precisa ser JPG, PNG ou WebP, até 15 MB.",
    );
  });
  it("arquivo grande demais", () => {
    expect(mensagemDoSite({ code: "INTERNAL", details: {} }, 413)).toMatch(/^Arquivo grande demais/);
  });
});

describe("marcadoresFaltando", () => {
  it("aponta o {marcador} do original que sumiu do texto editado", () => {
    const original = "A pré-reserva segura a data por {horas}h; sinal de {sinal}%.";
    expect(marcadoresFaltando(original, "Segura por {horas}h.")).toEqual(["{sinal}"]);
    expect(marcadoresFaltando(original, "Segura por {horas}h, sinal {sinal}%.")).toEqual([]);
  });
  it("texto sem marcador e valor que não é texto não geram aviso", () => {
    expect(marcadoresFaltando("Reservar", "Reserve já")).toEqual([]);
    expect(marcadoresFaltando({ url: "/x" }, "")).toEqual([]);
  });
});
