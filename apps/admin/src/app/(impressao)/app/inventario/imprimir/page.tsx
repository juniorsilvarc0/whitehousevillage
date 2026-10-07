import Link from "next/link";

import { BotaoImprimir } from "@/components/bens/botao-imprimir";
import { FotoDoBem } from "@/components/bens/foto";
import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { buttonVariants } from "@/components/ui/button";
import { carregar } from "@/lib/api/carregar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { descricaoDoRecorte, lerFiltrosDaUnidade, type ParametrosCrus } from "@/lib/bens/filtros";
import { ROTULO_DA_CATEGORIA, ROTULO_DO_AMBIENTE, quantidadeComMedida } from "@/lib/bens/rotulos";
import type { InventarioDaUnidade } from "@/lib/bens/tipos";
import { dataPorExtenso } from "@/lib/fuso";
import { cn } from "@/lib/utils";

export const metadata = { title: "Lista de inventário" };

/**
 * A folha de inventário da unidade, para levar na mão — **foto e quantidade**,
 * cômodo a cômodo, na ordem de caminhada.
 *
 * O contrato decide que PDF não é gerado no servidor (`GET /inventory/export`
 * é planilha): a folha bem feita é esta tela com o "salvar como PDF" do
 * navegador. A coluna "Contado" fica em branco de propósito, para a caneta.
 */
export default async function ImprimirPage({ searchParams }: { searchParams: Promise<ParametrosCrus> }) {
  const { permissions } = await requireSession();
  if (!can(permissions, "inventory.goods", "ver")) return <SemAcesso recurso="inventory.goods" />;

  const { filtros } = lerFiltrosDaUnidade(await searchParams);
  if (!filtros.unidade) {
    return (
      <main className="mx-auto max-w-3xl p-8">
        <p className="text-sm text-muted-foreground">
          Escolha a unidade em{" "}
          <Link href="/app/inventario" className="text-primary underline-offset-4 hover:underline">
            Inventário
          </Link>{" "}
          e use “Imprimir lista”.
        </p>
      </main>
    );
  }

  const inventario = await carregar<InventarioDaUnidade>(`/units/${filtros.unidade}/inventory`, {
    include_inactive: filtros.inativos ? true : undefined,
    category: filtros.categoria || undefined,
  });

  if (!inventario.ok) {
    return (
      <main className="mx-auto max-w-3xl p-8">
        <EstadoDeErro code={inventario.code} titulo="Não foi possível montar a folha" />
      </main>
    );
  }

  const { unit, rooms, totals } = inventario.data;
  // Os totais da API são do que a resposta mostra: com categoria ou
  // desativados, a folha diz que é um recorte, não o apartamento inteiro.
  const recorte = descricaoDoRecorte({ q: "", categoria: filtros.categoria, inativos: filtros.inativos });

  return (
    <main className="mx-auto flex max-w-4xl flex-col gap-6 p-6 print:max-w-none print:p-0">
      <div className="flex flex-wrap items-center justify-between gap-3 print:hidden">
        <Link href={`/app/inventario?unidade=${unit.id}`} className={cn(buttonVariants({ variant: "ghost", size: "sm" }))}>
          Voltar ao inventário
        </Link>
        <BotaoImprimir />
      </div>

      <header className="flex flex-wrap items-end justify-between gap-2 border-b border-foreground/20 pb-3">
        <div>
          <p className="text-xs uppercase tracking-wider text-muted-foreground">White House Village · Inventário</p>
          <h1 className="font-display text-2xl leading-tight">
            {unit.code} — {unit.name}
          </h1>
        </div>
        <p className="text-right text-xs text-muted-foreground">
          {totals.rooms} ambientes · {totals.items} bens · {totals.expected_qty} peças
          {recorte ? (
            <>
              <br />
              Recorte: {recorte}
            </>
          ) : null}
          <br />
          Impresso em {dataPorExtenso()}
        </p>
      </header>

      {rooms.length === 0 ? (
        <p className="text-sm text-muted-foreground">Esta unidade ainda não tem ambientes com bens.</p>
      ) : (
        rooms.map((ambiente, i) => (
          <section key={ambiente.id} className="break-inside-avoid-page">
            <h2 className="font-display mb-2 flex items-baseline gap-2 text-lg [break-after:avoid]">
              <span className="font-mono text-sm text-muted-foreground">{i + 1}.</span>
              {ambiente.name}
              <span className="text-xs font-normal text-muted-foreground">{ROTULO_DO_AMBIENTE[ambiente.kind]}</span>
            </h2>
            {ambiente.items.length === 0 ? (
              <p className="text-sm text-muted-foreground">Nenhum bem colocado.</p>
            ) : (
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b border-foreground/30 text-left text-xs text-muted-foreground">
                    <th className="w-16 py-1 pr-2 font-medium">Foto</th>
                    <th className="py-1 pr-2 font-medium">Bem</th>
                    <th className="w-28 py-1 pr-2 text-right font-medium">Esperado</th>
                    <th className="w-24 py-1 text-center font-medium">Contado</th>
                  </tr>
                </thead>
                <tbody>
                  {ambiente.items.map((c) => (
                    <tr key={c.id} className="break-inside-avoid border-b border-foreground/10 align-middle">
                      <td className="py-1.5 pr-2">
                        <FotoDoBem midia={c.cover} alt={c.item_name ?? ""} className="size-12 rounded-md" />
                      </td>
                      <td className="py-1.5 pr-2">
                        <span className="block">{c.item_name}</span>
                        <span className="block text-xs text-muted-foreground">
                          {c.item_category ? ROTULO_DA_CATEGORIA[c.item_category] : null}
                          {c.note ? ` · ${c.note}` : null}
                        </span>
                      </td>
                      <td className="py-1.5 pr-2 text-right font-medium tabular-nums">
                        {quantidadeComMedida(c.expected_qty, c.item_unit_measure)}
                      </td>
                      <td className="py-1.5">
                        <span className="mx-auto block h-7 w-16 rounded border border-foreground/40" aria-hidden="true" />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>
        ))
      )}

      <footer className="mt-2 border-t border-foreground/20 pt-3 text-xs text-muted-foreground">
        Conferido por: ______________________________ Data: ____/____/______
      </footer>
    </main>
  );
}
