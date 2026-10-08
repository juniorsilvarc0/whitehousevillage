import Link from "next/link";

import { TelaDeContagem } from "@/components/bens/contagem";
import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Tela } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { carregar } from "@/lib/api/carregar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { permissoesDoInventario } from "@/lib/bens/permissoes";
import { ROTULO_DO_STATUS } from "@/lib/bens/rotulos";
import type { ConferenciaCompleta } from "@/lib/bens/tipos";
import { formatarInstante } from "@/lib/datas";

export const metadata = { title: "Conferência" };

/**
 * A conferência de uma unidade — no celular, dentro do apartamento.
 *
 * Uma chamada (`GET /inventory/counts/{id}`) traz o cabeçalho, o progresso e as
 * linhas agrupadas por ambiente na ordem de caminhada. Cada linha carrega o
 * esperado **congelado** na abertura: mudar o padrão da casa depois não muda o
 * que esta contagem espera.
 */
export default async function ConferenciaPage({ params }: { params: Promise<{ id: string }> }) {
  const { permissions } = await requireSession();
  if (!can(permissions, "inventory.goods", "ver")) return <SemAcesso recurso="inventory.goods" />;

  const { id } = await params;
  const conferencia = await carregar<ConferenciaCompleta>(`/inventory/counts/${encodeURIComponent(id)}`);
  const voltar = { href: "/app/inventario/conferencias", rotulo: "Conferências" };

  if (!conferencia.ok) {
    return (
      <Tela>
        <CabecalhoDeTela voltar={voltar} titulo="Conferência" />
        <EstadoDeErro
          code={conferencia.code}
          titulo="Não foi possível abrir esta conferência"
          detalhe={conferencia.code === "NOT_FOUND" ? "Confira o endereço, ou volte à lista de conferências." : undefined}
        />
      </Tela>
    );
  }

  const c = conferencia.data;
  const unidade = [c.unit_code, c.unit_name].filter(Boolean).join(" — ") || "Unidade";

  return (
    <Tela className="pb-2">
      <CabecalhoDeTela
        voltar={voltar}
        titulo={`Conferência ${c.unit_code ?? ""}`.trim()}
        descricao={
          <>
            <Link href={`/app/inventario?unidade=${c.unit_id}`} className="text-primary underline-offset-4 hover:underline">
              {unidade}
            </Link>{" "}
            · aberta em {formatarInstante(c.opened_at)}
            {c.opened_by_name ? ` por ${c.opened_by_name}` : ""}
            {c.closed_at ? ` · ${c.status === "cancelada" ? "cancelada" : "fechada"} em ${formatarInstante(c.closed_at)}` : ""}
            {c.closed_by_name ? ` por ${c.closed_by_name}` : ""}
            {c.note ? (
              <>
                <br />
                {c.note}
              </>
            ) : null}
          </>
        }
        acoes={<Badge variant={c.status === "aberta" ? "brand" : "outline"}>{ROTULO_DO_STATUS[c.status]}</Badge>}
      />

      <TelaDeContagem conferencia={c} permissoes={permissoesDoInventario(permissions)} />
    </Tela>
  );
}
