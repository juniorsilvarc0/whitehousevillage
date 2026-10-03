import type { CampoDoSite, ItemDeLista, Midia, Subcampo, TipoDeCampo, ValorDeCampo } from "@/lib/site/tipos";

/**
 * Regras de tela da edição do site — puras, sem React, testadas em
 * `edicao.test.ts`. Nenhuma delas decide o que vale: a API confere tudo de
 * novo. Elas existem para o gestor saber do problema antes de esperar o envio.
 */

/** Endereço público do site, para o botão "Ver o site". */
export const ENDERECO_DO_SITE = "https://www.whitehousevillage.com.br";

// ── Destaque em títulos ─────────────────────────────────────────────────────

export type Trecho = { texto: string; destaque: boolean } | { quebra: true };

/**
 * Como o site vai desenhar o título: `*palavra*` vira destaque e a quebra de
 * linha vira linha nova. Mesma regra do `scripts/conteudo.js` do site — devolve
 * trechos, não HTML, para a prévia nunca precisar de `dangerouslySetInnerHTML`.
 */
export function trechosDoTitulo(texto: string): Trecho[] {
  const trechos: Trecho[] = [];
  const linhas = texto.replace(/\r\n?/g, "\n").split("\n");
  linhas.forEach((linha, i) => {
    if (i > 0) trechos.push({ quebra: true });
    const re = /\*([^*\n]+)\*/g;
    let ultimo = 0;
    for (let m = re.exec(linha); m; m = re.exec(linha)) {
      if (m.index > ultimo) trechos.push({ texto: linha.slice(ultimo, m.index), destaque: false });
      trechos.push({ texto: m[1], destaque: true });
      ultimo = m.index + m[0].length;
    }
    if (ultimo < linha.length) trechos.push({ texto: linha.slice(ultimo), destaque: false });
  });
  return trechos;
}

/** Asterisco sem par: o site mostraria o `*` cru. Vale avisar. */
export function asteriscoSemPar(texto: string): boolean {
  return texto.replace(/\*[^*\n]+\*/g, "").includes("*");
}

/** Parágrafos como o site vai separar: linha em branco começa outro. */
export function paragrafos(texto: string): string[] {
  return texto
    .replace(/\r\n?/g, "\n")
    .split(/\n\s*\n/)
    .map((p) => p.trim())
    .filter(Boolean);
}

/** Limite de caracteres de cada tipo, quando o catálogo não disser outro. */
export function limiteDe(campo: Pick<CampoDoSite, "kind" | "max">): number | null {
  if (campo.max && campo.max > 0) return campo.max;
  if (campo.kind === "texto" || campo.kind === "titulo") return 200;
  if (campo.kind === "texto_longo") return 2000;
  return null;
}

// ── Listas ──────────────────────────────────────────────────────────────────

/** Move o item `de` uma posição para cima (-1) ou para baixo (+1). Fora dos
 *  limites, devolve a mesma lista. Nunca altera a original. */
export function mover<T>(lista: readonly T[], de: number, passo: -1 | 1): T[] {
  const para = de + passo;
  if (de < 0 || de >= lista.length || para < 0 || para >= lista.length) return [...lista];
  const copia = [...lista];
  const [item] = copia.splice(de, 1);
  copia.splice(para, 0, item);
  return copia;
}

export function remover<T>(lista: readonly T[], indice: number): T[] {
  return lista.filter((_, i) => i !== indice);
}

/** Item novo, com cada subcampo vazio. */
export function itemVazio(subcampos: readonly Subcampo[]): ItemDeLista {
  const item: ItemDeLista = {};
  for (const s of subcampos) item[s.key] = s.kind === "imagem" ? null : "";
  return item;
}

// ── Arquivos ────────────────────────────────────────────────────────────────

const MB = 1024 * 1024;

export const LIMITES = {
  imagem: { tipos: ["image/jpeg", "image/png", "image/webp"], bytes: 15 * MB, aceitar: "image/jpeg,image/png,image/webp" },
  video: { tipos: ["video/mp4", "video/webm"], bytes: 300 * MB, aceitar: "video/mp4,video/webm" },
} as const;

export const AVISO_DE_LIMITE = {
  imagem: "JPG, PNG ou WebP, até 15 MB.",
  video: "MP4 ou WebM, até 300 MB.",
} as const;

/**
 * Confere tipo e tamanho antes de enviar. Devolve a mensagem para o gestor, ou
 * `null` se pode seguir. É só cortesia: a API olha os bytes do arquivo de
 * verdade, e um arquivo renomeado passa aqui e é recusado lá.
 */
export function conferirArquivo(arquivo: { type: string; size: number; name: string }, tipo: "imagem" | "video"): string | null {
  const limite = LIMITES[tipo];
  const mime = arquivo.type || mimePelaExtensao(arquivo.name);
  if (!(limite.tipos as readonly string[]).includes(mime)) {
    return tipo === "imagem"
      ? "Esta foto não pode ser usada. Envie uma foto JPG, PNG ou WebP."
      : "Este vídeo não pode ser usado. Envie um vídeo MP4 ou WebM.";
  }
  if (arquivo.size > limite.bytes) {
    return tipo === "imagem"
      ? `Foto grande demais (${tamanho(arquivo.size)}). O máximo é 15 MB.`
      : `Vídeo grande demais (${tamanho(arquivo.size)}). O máximo é 300 MB.`;
  }
  if (arquivo.size === 0) return "O arquivo está vazio.";
  return null;
}

function mimePelaExtensao(nome: string): string {
  const ext = nome.toLowerCase().split(".").pop() ?? "";
  return (
    { jpg: "image/jpeg", jpeg: "image/jpeg", png: "image/png", webp: "image/webp", mp4: "video/mp4", webm: "video/webm" } as Record<string, string>
  )[ext] ?? "";
}

/** Tamanho legível: "3,4 MB", "820 KB". */
export function tamanho(bytes: number): string {
  if (bytes >= MB) return `${(bytes / MB).toLocaleString("pt-BR", { maximumFractionDigits: 1 })} MB`;
  return `${Math.max(1, Math.round(bytes / 1024)).toLocaleString("pt-BR")} KB`;
}

// ── Valor para gravar ───────────────────────────────────────────────────────

function ehMidia(v: unknown): v is Midia {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Mídia como a API grava: só o id (e o texto alternativo, na foto). O
 *  endereço é calculado por ela na leitura. */
function midiaParaGravar(v: Midia | null | undefined, comAlt: boolean): Midia | null {
  if (!v?.media_id) return null;
  return comAlt ? { media_id: v.media_id, alt: (v.alt ?? "").trim() } : { media_id: v.media_id };
}

/** Corpo do `PUT /site/content/{key}`. */
export function valorParaGravar(kind: TipoDeCampo, valor: ValorDeCampo, subcampos: readonly Subcampo[] = []): unknown {
  if (kind === "imagem") return midiaParaGravar(ehMidia(valor) ? valor : null, true);
  if (kind === "video") return midiaParaGravar(ehMidia(valor) ? valor : null, false);
  if (kind === "lista") {
    const itens = Array.isArray(valor) ? valor : [];
    return itens.map((item) => {
      const saida: Record<string, unknown> = {};
      for (const s of subcampos) {
        const v = item[s.key];
        saida[s.key] = s.kind === "imagem" ? midiaParaGravar(ehMidia(v) ? v : null, true) : typeof v === "string" ? v : "";
      }
      return saida;
    });
  }
  return typeof valor === "string" ? valor : "";
}

/** O gestor mudou algo? Compara o que seria gravado, não a forma da tela. */
export function mudou(kind: TipoDeCampo, atual: ValorDeCampo, rascunho: ValorDeCampo, subcampos: readonly Subcampo[] = []): boolean {
  return JSON.stringify(valorParaGravar(kind, atual, subcampos)) !== JSON.stringify(valorParaGravar(kind, rascunho, subcampos));
}

/** Foto de algum item da lista ainda sem arquivo — o site mostraria vazio. */
export function listaComFotoFaltando(itens: readonly ItemDeLista[], subcampos: readonly Subcampo[]): boolean {
  return itens.some((item) =>
    subcampos.some((s) => s.kind === "imagem" && !(ehMidia(item[s.key]) && (item[s.key] as Midia).media_id)),
  );
}

/**
 * Endereço para mostrar a mídia **no painel**.
 *
 * Arquivo enviado (tem `media_id`) passa pela rota do próprio painel, que busca
 * na API — o painel e o site são endereços diferentes, e o caminho público
 * relativo que a API devolve não existiria aqui. Mídia original do site (sem
 * `media_id`, ex. `/assets/logo.png`) é buscada no próprio site.
 */
export function enderecoDePrevia(midia: Midia | null | undefined): string | null {
  if (!midia) return null;
  if (midia.media_id) return `/api/site/midia/${encodeURIComponent(midia.media_id)}`;
  const url = midia.url ?? "";
  if (/^https:\/\//.test(url)) return url;
  if (url.startsWith("/") && !url.startsWith("//")) return `${ENDERECO_DO_SITE}${url}`;
  return null;
}
