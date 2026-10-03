/**
 * Conteúdo do site de vendas, como `GET /site/content` entrega
 * (docs/site-cms.md §5). O catálogo de campos mora na API; o painel desenha o
 * formulário a partir dele e não conhece nenhuma chave de cor.
 */

export type TipoDeCampo = "texto" | "texto_longo" | "titulo" | "imagem" | "video" | "lista";

/** Subcampo de um item de lista. */
export type TipoDeSubcampo = "texto" | "texto_longo" | "imagem";

/** Mídia como a API devolve: já resolvida, com o endereço para mostrar. */
export type Midia = { media_id?: string | null; url?: string | null; alt?: string | null };

export type ItemDeLista = Record<string, string | Midia | null | undefined>;

export type ValorDeCampo = string | Midia | ItemDeLista[] | null;

export type Subcampo = { key: string; label: string; kind: TipoDeSubcampo };

export type CampoDoSite = {
  key: string;
  label: string;
  kind: TipoDeCampo;
  help?: string | null;
  max?: number | null;
  value: ValorDeCampo;
  default_value: ValorDeCampo;
  is_default: boolean;
  item_fields?: Subcampo[];
};

export type SecaoDoSite = { key: string; label: string; fields: CampoDoSite[] };

export type ConteudoDoSite = { sections: SecaoDoSite[] };

/** O que `POST /site/media` devolve. */
export type MidiaEnviada = { id: string; kind: "imagem" | "video"; mime: string; bytes: number; url: string };
