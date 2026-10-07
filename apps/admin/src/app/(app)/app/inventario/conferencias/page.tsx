import Link from "next/link";
import { Suspense } from "react";
import { ClipboardList } from "lucide-react";

import { AbasDoInventario } from "@/components/bens/abas";
import { BotaoAbrirConferencia } from "@/components/bens/botao-abrir-conferencia";
import { SelecaoNaUrl } from "@/components/bens/filtro-na-url";
import { Paginacao, parametrosDe } from "@/components/bens/paginacao";
import { EstadoDeErro, EstadoVazio, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { apiList } from "@/lib/api/client";
import { tentar } from "@/lib/acoes/executar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { unidadesDoInventario } from "@/lib/bens/api";
import { POR_PAGINA, lerFiltrosDeConferencias, type ParametrosCrus } from "@/lib/bens/filtros";
import { caminhoDaConferencia } from "@/lib/bens/mensagens";
import { permissoesDoInventario } from "@/lib/bens/permissoes";
import { ROTULO_DO_STATUS } from "@/lib/bens/rotulos";
import { STATUS_DE_CONFERENCIA, type Conferencia } from "@/lib/bens/tipos";
import { formatarInstante } from "@/lib/datas";
import { cn } from "@/lib/utils";

export const metadata = { title: "Conferências" };

/**
 * O histórico de conferências, da mais recente para a mais antiga — e a porta
 * de entrada para continuar a que está aberta.
 */
export default async function ConferenciasPage({ searchParams }: { searchParams: Promise<ParametrosCrus> }) {
  const { permissions } = await requireSession();
  if (!can(permissions, "inventory.goods", "ver")) return <SemAcesso recurso="inventory.goods" />;

  const cru = await searchParams;
  const { filtros, avisos } = lerFiltrosDeConferencias(cru);

  const [lista, unidades] = await Promise.all([
    tentar(() =>
      apiList<Conferencia>("/inventory/counts", {
        query: {
          unit_id: filtros.unidade || undefined,
          status: filtros.status || undefined,
          sort: "-opened_at",
          page: filtros.page,
          per_page: POR_PAGINA,
        },
      }),
    ),
    unidadesDoInventario(),
  ]);

  const podeAbrir = permissoesDoInventario(permissions).criar;
  // `open_count_id` vem da própria lista de unidades: a conferência aberta é
  // achada mesmo quando não está na página atual do histórico.
  const unidadeEscolhida = unidades.ok ? unidades.data.find((u) => u.id === filtros.unidade) : undefined;

  return (
    <Tela>
      <CabecalhoDeTela
        titulo="Conferências"
        descricao={
          <>
            Contar o que há em cada cômodo e comparar com o que se espera. Cada unidade tem no máximo{" "}
            <strong>uma</strong> conferência em andamento.
          </>
        }
        acoes={
          podeAbrir && unidadeEscolhida ? (
            <BotaoAbrirConferencia unitId={unidadeEscolhida.id} unidadeRotulo={unidadeEscolhida.code} abertaId={unidadeEscolhida.open_count_id} tamanho="sm" />
          ) : null
        }
      />

      <AbasDoInventario ativa="conferencias" />

      <Suspense fallback={<div className="h-16" />}>
        <div className="flex flex-wrap items-end gap-3">
          <SelecaoNaUrl
            id="conf-unidade"
            chave="unidade"
            rotulo="Unidade"
            vazio="Todas"
            opcoes={unidades.ok ? unidades.data.map((u) => ({ valor: u.id, rotulo: `${u.code} — ${u.name}` })) : []}
            className="w-60"
          />
          <SelecaoNaUrl
            id="conf-status"
            chave="status"
            rotulo="Situação"
            vazio="Todas"
            opcoes={STATUS_DE_CONFERENCIA.map((s) => ({ valor: s, rotulo: ROTULO_DO_STATUS[s] }))}
            className="w-44"
          />
        </div>
      </Suspense>

      {podeAbrir && !filtros.unidade ? (
        <p className="text-xs text-muted-foreground">Para abrir uma conferência, escolha a unidade no filtro acima.</p>
      ) : null}

      {avisos.length > 0 ? <Nota variante="atencao">{avisos.join(" ")}</Nota> : null}

      {!lista.ok ? (
        <EstadoDeErro code={lista.code} titulo="Não foi possível carregar as conferências" />
      ) : lista.data.data.length === 0 ? (
        <EstadoVazio
          icone={ClipboardList}
          titulo="Nenhuma conferência com esse recorte"
          descricao={
            <>
              A conferência se abre pela unidade: quem conta caminha cômodo a cômodo pelo celular, com a foto de cada bem
              e a quantidade esperada.
            </>
          }
        />
      ) : (
        <>
          <ul className="flex flex-col gap-2">
            {lista.data.data.map((c) => (
              <li key={c.id}>
                <CartaoDaConferencia conferencia={c} />
              </li>
            ))}
          </ul>
          <Paginacao meta={lista.data.meta} caminho="/app/inventario/conferencias" parametros={parametrosDe(cru)} substantivo={["conferência", "conferências"]} />
        </>
      )}
    </Tela>
  );
}

function CartaoDaConferencia({ conferencia: c }: { conferencia: Conferencia }) {
  const fracao = c.progress.lines > 0 ? c.progress.counted / c.progress.lines : 0;
  return (
    <Link
      href={caminhoDaConferencia(c.id)}
      className={cn(
        "flex flex-col gap-2 rounded-xl border bg-card/70 px-4 py-3 outline-none transition-colors hover:bg-muted/40 sm:flex-row sm:items-center sm:gap-4",
        "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-card",
        c.status === "aberta" ? "border-primary/40" : "border-border/60",
      )}
    >
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-sm">{c.unit_code ?? "—"}</span>
          <span className="truncate text-sm text-muted-foreground">{c.unit_name}</span>
          <Badge variant={c.status === "aberta" ? "brand" : c.status === "cancelada" ? "outline" : "neutral"}>
            {ROTULO_DO_STATUS[c.status]}
          </Badge>
        </div>
        <p className="mt-0.5 text-xs text-muted-foreground">
          Aberta em {formatarInstante(c.opened_at)}
          {c.opened_by_name ? ` por ${c.opened_by_name}` : ""}
          {c.closed_at ? ` · encerrada em ${formatarInstante(c.closed_at)}` : ""}
        </p>
      </div>
      <div className="flex shrink-0 flex-col gap-1 sm:w-56">
        <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted" aria-hidden="true">
          <div className="h-full bg-brand-gradient" style={{ width: `${Math.round(fracao * 100)}%` }} />
        </div>
        <p className="text-xs text-muted-foreground">
          <span className="tabular-nums">{c.progress.counted}</span> de <span className="tabular-nums">{c.progress.lines}</span>{" "}
          contados · <span className="tabular-nums">{c.progress.diverging}</span>{" "}
          {c.progress.diverging === 1 ? "divergência" : "divergências"}
        </p>
      </div>
    </Link>
  );
}
