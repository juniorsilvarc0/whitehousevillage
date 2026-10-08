import Link from "next/link";
import { ChevronLeft, ChevronRight } from "lucide-react";

import { buttonVariants } from "@/components/ui/button";
import type { Meta } from "@/lib/api/types";
import { cn } from "@/lib/utils";

/**
 * Paginação em links — a página é o `page` da query string, como o resto do
 * recorte. Mesmo desenho da lista de reservas: `<span>` quando não há para onde
 * ir, nunca um link desativado que o teclado ainda alcança.
 */
export function Paginacao({
  meta,
  caminho,
  parametros,
  substantivo,
}: {
  meta: Meta;
  caminho: string;
  /** A query string atual, sem o `?`. */
  parametros: string;
  substantivo: [singular: string, plural: string];
}) {
  const total = (
    <>
      <span className="tabular-nums">{meta.total}</span> {meta.total === 1 ? substantivo[0] : substantivo[1]}
    </>
  );

  if (meta.total_pages <= 1) return <p className="text-xs text-muted-foreground">{total} no total.</p>;

  const para = (pagina: number) => {
    const p = new URLSearchParams(parametros);
    p.set("page", String(pagina));
    return `${caminho}?${p.toString()}`;
  };

  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <p className="text-xs text-muted-foreground">
        Página <span className="tabular-nums">{meta.page}</span> de{" "}
        <span className="tabular-nums">{meta.total_pages}</span> · {total} no total.
      </p>
      <div className="flex items-center gap-2">
        <Passo href={meta.page > 1 ? para(meta.page - 1) : null} rotulo="Página anterior" icone={ChevronLeft} />
        <Passo href={meta.page < meta.total_pages ? para(meta.page + 1) : null} rotulo="Próxima página" icone={ChevronRight} />
      </div>
    </div>
  );
}

function Passo({ href, rotulo, icone: Icone }: { href: string | null; rotulo: string; icone: typeof ChevronLeft }) {
  const classe = cn(buttonVariants({ variant: "outline", size: "iconSm" }), !href && "opacity-45");
  if (!href) {
    return (
      <span aria-hidden="true" className={classe}>
        <Icone />
      </span>
    );
  }
  return (
    <Link href={href} aria-label={rotulo} className={classe}>
      <Icone aria-hidden="true" />
    </Link>
  );
}

/** A query string de `searchParams` já lidos — para a paginação montar links. */
export function parametrosDe(cru: Record<string, string | string[] | undefined>): string {
  const p = new URLSearchParams();
  for (const [chave, valor] of Object.entries(cru)) {
    const v = Array.isArray(valor) ? valor[0] : valor;
    if (v) p.set(chave, v);
  }
  return p.toString();
}
