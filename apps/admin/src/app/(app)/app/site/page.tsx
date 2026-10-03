import { ExternalLink } from "lucide-react";

import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Tela } from "@/components/layout/tela";
import { EditorDoSite } from "@/components/site/editor-do-site";
import { buttonVariants } from "@/components/ui/button";
import { carregar } from "@/lib/api/carregar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { ENDERECO_DO_SITE } from "@/lib/site/edicao";
import type { ConteudoDoSite } from "@/lib/site/tipos";
import { cn } from "@/lib/utils";

export const metadata = { title: "Site" };

/**
 * Site — textos, fotos e vídeos do site de vendas (docs/site-cms.md).
 *
 * O formulário é desenhado a partir do catálogo que a API devolve: campo novo no
 * catálogo aparece aqui sem mudar esta tela.
 */
export default async function SitePage() {
  const { permissions } = await requireSession();
  if (!can(permissions, "site", "ver")) return <SemAcesso recurso="site" />;

  const conteudo = await carregar<ConteudoDoSite>("/site/content");

  return (
    <Tela>
      <CabecalhoDeTela
        titulo="Site"
        descricao="Tudo o que o visitante vê no site. Ao salvar, a mudança aparece no site em até 1 minuto."
        acoes={
          <a
            href={ENDERECO_DO_SITE}
            target="_blank"
            rel="noopener noreferrer"
            className={cn(buttonVariants({ variant: "outline" }))}
          >
            <ExternalLink aria-hidden="true" />
            Ver o site
          </a>
        }
      />

      {conteudo.ok ? (
        <EditorDoSite secoes={conteudo.data?.sections ?? []} podeEditar={can(permissions, "site", "editar")} />
      ) : (
        <EstadoDeErro code={conteudo.code} titulo="Não foi possível abrir o conteúdo do site" />
      )}
    </Tela>
  );
}
