import { ImageOff } from "lucide-react";

import { enderecoDaFoto, type Tamanho } from "@/lib/bens/midia";
import type { MidiaDeBem } from "@/lib/bens/tipos";
import { cn } from "@/lib/utils";

/**
 * A foto de um bem — ou o aviso de que ele ainda não tem.
 *
 * Carrega a **miniatura** por padrão (`thumb_url`), pelo intermediário
 * autenticado do painel. `loading="lazy"`: a grade do catálogo e a lista de
 * conferência têm dezenas de fotos, e o celular dentro do apartamento só
 * precisa das que estão na tela.
 *
 * Sem foto, a moldura continua lá com o ícone: bem sem foto é a fila de
 * trabalho do cadastro, e o buraco tem de ser visível, não um espaço em branco.
 */
export function FotoDoBem({
  midia,
  alt,
  tamanho = "thumb",
  className,
}: {
  midia: Pick<MidiaDeBem, "id" | "thumb_url"> | null | undefined;
  alt: string;
  tamanho?: Tamanho;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "relative flex shrink-0 items-center justify-center overflow-hidden rounded-lg border border-border/60 bg-muted/50",
        className,
      )}
    >
      {midia ? (
        // Foto autenticada, de tamanho variável, servida pelo intermediário do
        // painel: o otimizador de imagem do Next não teria o token para buscá-la.
        // eslint-disable-next-line @next/next/no-img-element
        <img
          src={enderecoDaFoto(midia, tamanho)}
          alt={alt}
          loading="lazy"
          decoding="async"
          className="size-full object-cover"
        />
      ) : (
        <ImageOff className="size-5 text-muted-foreground/70" aria-label="Sem foto" />
      )}
    </div>
  );
}
