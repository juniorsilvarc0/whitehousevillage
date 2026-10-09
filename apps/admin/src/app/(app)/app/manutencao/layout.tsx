import { AvisosDaManutencao } from "@/components/manutencao/avisos";

/**
 * Layout das ordens de manutenção.
 *
 * Monta o `<Toaster/>` do módulo (a casca não monta nenhum) e dá às actions um
 * ponto único para revalidar — `revalidatePath("/(app)/app/manutencao",
 * "layout")` atualiza a lista e o detalhe de uma vez.
 */
export default function ManutencaoLayout({ children }: { children: React.ReactNode }) {
  return (
    <>
      {children}
      <AvisosDaManutencao />
    </>
  );
}
