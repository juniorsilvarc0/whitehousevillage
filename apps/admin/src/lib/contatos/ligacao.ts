import { ehE164 } from "@/lib/contatos/telefone";

/**
 * O `tel:` do painel — **um lugar só**, e só para E.164.
 *
 * Até a D11 cada tela montava o próprio `` `tel:${telefone}` `` com o que viesse
 * da API, e o lead passou a vir mascarado (`+*********0000`): o botão de ligar
 * discava uma sequência de asteriscos. A máscara não é E.164, então a recusa
 * aqui é a mesma que `linkDoWhatsApp` já fazia — número que não se sabe discar
 * não ganha link, porque um link que liga para o nada é pior do que nenhum.
 *
 * Isto não decide **de onde** vem o número. Quem precisa ligar a partir de uma
 * coleção mascarada (lista de contatos, leads) busca a ficha primeiro
 * (`lerFichaDoContato`, que grava `pii_access_log`) — ver
 * `components/contatos/botao-de-ligar.tsx`.
 */
export function linkDeLigacao(e164: string | null | undefined): string | null {
  if (!e164 || !ehE164(e164)) return null;
  return `tel:${e164}`;
}

/**
 * Pede ao sistema para discar. Devolve `false`, sem navegar, quando o número
 * não é E.164.
 *
 * Isolado num módulo para o teste trocar a navegação por um espião: o jsdom não
 * navega, e o que importa medir é **qual número** foi entregue ao discador.
 */
export function discar(e164: string): boolean {
  const href = linkDeLigacao(e164);
  if (!href) return false;
  window.location.assign(href);
  return true;
}
