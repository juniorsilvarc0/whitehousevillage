import { AvisosDoInventario } from "@/components/bens/avisos";

/**
 * Layout do inventário de bens.
 *
 * Existe por dois motivos práticos: montar o `<Toaster/>` das telas do módulo
 * (a casca não monta nenhum) e dar às actions um ponto único para revalidar —
 * `revalidatePath("/(app)/app/inventario", "layout")` atualiza as quatro telas
 * e as fichas de uma vez.
 */
export default function InventarioLayout({ children }: { children: React.ReactNode }) {
  return (
    <>
      {children}
      <AvisosDoInventario />
    </>
  );
}
