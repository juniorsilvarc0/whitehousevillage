import { AvisosDaAgenda } from "@/components/agenda/avisos";

/**
 * Layout da agenda operacional.
 *
 * Monta o `<Toaster/>` do módulo (a casca não monta nenhum) e dá às actions um
 * ponto único para revalidar — `revalidatePath("/(app)/app/agenda", "layout")`
 * atualiza a agenda e os bloqueios de horário de uma vez.
 */
export default function AgendaLayout({ children }: { children: React.ReactNode }) {
  return (
    <>
      {children}
      <AvisosDaAgenda />
    </>
  );
}
