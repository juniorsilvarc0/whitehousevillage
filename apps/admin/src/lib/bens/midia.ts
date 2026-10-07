import type { MidiaDeBem } from "@/lib/bens/tipos";

/**
 * Fotos do inventário no navegador.
 *
 * ## Nunca pública
 *
 * `GET /inventory/media/{id}` exige token e `inventory.goods:ver`: a foto mostra
 * o interior da casa (onde fica a TV, o que há no bar, como é a fechadura). O
 * navegador não tem o token — ele vive em cookie `httpOnly` — então o `<img>`
 * aponta para o intermediário do painel, `/api/inventario/midia/{id}`, que
 * repassa com o `Authorization` e devolve `Cache-Control: private`.
 *
 * ## A grade carrega a miniatura
 *
 * O contrato entrega `thumb_url` (`?size=thumb`) para a grade e `url` para o
 * original. A miniatura é o que vai em lista: 200 itens com foto de até 15 MB,
 * abertos no celular dentro do apartamento, não é tela utilizável. Quando a
 * geração da miniatura falhou, a API devolve `thumb_url` igual a `url` — e o
 * intermediário pede exatamente o que ela disse, sem adivinhar.
 */

const INTERMEDIARIO = "/api/inventario/midia";

export type Tamanho = "thumb" | "original";

/** O `size` que a própria URL da API pede (`?size=thumb`), ou `null`. */
function tamanhoPedidoPor(url: string | undefined): Tamanho | null {
  if (!url) return null;
  const interrogacao = url.indexOf("?");
  if (interrogacao < 0) return null;
  const size = new URLSearchParams(url.slice(interrogacao + 1)).get("size");
  return size === "thumb" || size === "original" ? size : null;
}

/**
 * O endereço do intermediário para esta mídia. `"thumb"` segue o `thumb_url`
 * do contrato (que pode ser o próprio original); `"original"` pede o arquivo
 * cheio, para a ampliação.
 */
export function enderecoDaFoto(midia: Pick<MidiaDeBem, "id" | "thumb_url">, tamanho: Tamanho = "thumb"): string {
  const id = encodeURIComponent(midia.id);
  if (tamanho === "original") return `${INTERMEDIARIO}/${id}`;
  return tamanhoPedidoPor(midia.thumb_url) === "thumb" ? `${INTERMEDIARIO}/${id}?size=thumb` : `${INTERMEDIARIO}/${id}`;
}

// ── Envio ───────────────────────────────────────────────────────────────────

export const TIPOS_ACEITOS = ["image/jpeg", "image/png", "image/webp"] as const;
export const LIMITE_DE_BYTES = 15 * 1024 * 1024;
export const ACEITAR = "image/jpeg,image/png,image/webp";
export const AVISO_DE_LIMITE = "JPG, PNG ou WebP, até 15 MB.";

/**
 * Recusa cedo o que a API recusaria — **só para o gestor não esperar o envio
 * para saber**. Quem decide é a API, que confere o tipo pelos bytes, não pela
 * extensão nem pelo `type` que o navegador declara.
 */
export function conferirArquivo(arquivo: Pick<File, "type" | "size">): string | null {
  // Alguns celulares mandam `type` vazio para foto da câmera; nesse caso quem
  // olha os bytes é a API, e não faz sentido recusar aqui.
  if (arquivo.type && !(TIPOS_ACEITOS as readonly string[]).includes(arquivo.type)) {
    return "A foto precisa ser JPG, PNG ou WebP.";
  }
  if (arquivo.size > LIMITE_DE_BYTES) return "A foto passa de 15 MB. Reduza a resolução e tente de novo.";
  if (arquivo.size === 0) return "O arquivo está vazio.";
  return null;
}
