"use client";

import * as React from "react";
import Link from "next/link";
import {
  AlertTriangle,
  Copy,
  DoorOpen,
  FileSpreadsheet,
  Pencil,
  Plus,
  Power,
  Printer,
  Trash2,
  TriangleAlert,
} from "lucide-react";

import { BotaoAbrirConferencia } from "@/components/bens/botao-abrir-conferencia";
import { ConfirmacaoDoInventario } from "@/components/bens/confirmacao";
import { FotoDoBem } from "@/components/bens/foto";
import { ModalDeAmbiente, type AlvoDoAmbiente } from "@/components/bens/modal-de-ambiente";
import { ModalDeAvaria, type AlvoDaAvaria } from "@/components/bens/modal-de-avaria";
import { ModalDeColocacao, type AlvoDaColocacao } from "@/components/bens/modal-de-colocacao";
import { ModalDeCopia } from "@/components/bens/modal-de-copia";
import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { EstadoVazio } from "@/components/layout/estados";
import { Nota } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { alternarAtivoDoAmbiente, apagarAmbiente, tirarBem } from "@/lib/bens/acoes";
import { linkDeExportacao } from "@/lib/bens/filtros";
import { caminhoDaConferencia } from "@/lib/bens/mensagens";
import { ROTULO_DA_CATEGORIA, ROTULO_DO_AMBIENTE, quantidadeComMedida } from "@/lib/bens/rotulos";
import type {
  Ambiente,
  AmbienteDoInventario,
  Bem,
  Colocacao,
  InventarioDaUnidade,
  UnidadeDoInventario,
} from "@/lib/bens/tipos";
import { formatarInstante } from "@/lib/datas";
import { formatarBRL } from "@/lib/dinheiro";
import { cn } from "@/lib/utils";

export type PermissoesDaUnidade = { criar: boolean; editar: boolean; excluir: boolean };

/**
 * O inventário de uma unidade: os ambientes **na ordem de caminhada pela
 * casa**, e dentro de cada um os bens com a quantidade esperada.
 *
 * É a mesma ordem da folha de conferência e da contagem no celular — quem
 * cadastra aqui está, na prática, desenhando o roteiro de quem vai contar.
 *
 * Toda ação aparece só para quem tem a permissão correspondente em
 * `inventory.goods` (criar, editar, excluir). Esconder é cortesia: quem recusa
 * é a API.
 */
export function InventarioDaUnidadeView({
  inventario,
  catalogo,
  unidades,
  permissoes,
  filtrado,
  recorte = null,
}: {
  inventario: InventarioDaUnidade;
  catalogo: readonly Bem[];
  unidades: readonly UnidadeDoInventario[];
  permissoes: PermissoesDaUnidade;
  /** Busca ou categoria aplicadas: ambiente sem resultado sai da resposta. */
  filtrado: boolean;
  /** O filtro ativo, dito em palavras (“categoria Louça”). Os `totals` da API
   *  somam o que a resposta mostra, então com filtro eles são do recorte. */
  recorte?: string | null;
}) {
  const { unit, rooms, totals } = inventario;
  const rotulo = `${unit.code} — ${unit.name}`;

  const modalAmbiente = useControleDeModal<AlvoDoAmbiente>();
  const modalColocacao = useControleDeModal<AlvoDaColocacao>();
  const modalAvaria = useControleDeModal<AlvoDaAvaria>();
  const [copiando, setCopiando] = React.useState(false);
  const [aAlternar, setAAlternar] = React.useState<Ambiente | null>(null);
  const [aApagar, setAApagar] = React.useState<Ambiente | null>(null);
  const [aTirar, setATirar] = React.useState<{ colocacao: Colocacao; ambiente: string } | null>(null);

  const proximaOrdem = rooms.reduce((max, r) => Math.max(max, r.sort_order + 1), 0);
  const aberta = inventario.open_count ?? null;
  const ultima = inventario.last_closed_count ?? null;

  return (
    <>
      <Resumo inventario={inventario} recorte={recorte} />

      {aberta ? (
        <Nota variante="atencao">
          Conferência em andamento desde {formatarInstante(aberta.opened_at)}
          {aberta.opened_by_name ? `, aberta por ${aberta.opened_by_name}` : ""} —{" "}
          <span className="tabular-nums">
            {aberta.progress.counted} de {aberta.progress.lines}
          </span>{" "}
          itens contados.{" "}
          <Link href={caminhoDaConferencia(aberta.id)} className="font-medium text-primary underline-offset-4 hover:underline">
            Continuar a contagem
          </Link>
          . Enquanto ela estiver aberta, mudar a lista daqui não muda o que está sendo contado.
        </Nota>
      ) : ultima ? (
        <p className="text-xs text-muted-foreground">
          Última conferência fechada em {formatarInstante(ultima.closed_at ?? ultima.opened_at)}
          {ultima.closed_by_name ? ` por ${ultima.closed_by_name}` : ""}.{" "}
          <Link href={caminhoDaConferencia(ultima.id)} className="text-primary underline-offset-4 hover:underline">
            Ver o resultado
          </Link>
        </p>
      ) : (
        <p className="text-xs text-muted-foreground">Esta unidade ainda não foi conferida nenhuma vez.</p>
      )}

      <div className="flex flex-wrap items-center gap-2">
        {permissoes.criar ? (
          <BotaoAbrirConferencia unitId={unit.id} unidadeRotulo={unit.code} abertaId={aberta?.id} tamanho="sm" />
        ) : aberta ? (
          <Link href={caminhoDaConferencia(aberta.id)} className={cn(buttonVariants({ size: "sm" }))}>
            Ver a conferência
          </Link>
        ) : null}
        {permissoes.criar ? (
          <>
            <Button size="sm" variant="outline" onClick={() => modalAmbiente.abrir({ ambiente: null, ordemSugerida: proximaOrdem })}>
              <Plus aria-hidden="true" />
              Novo ambiente
            </Button>
            <Button size="sm" variant="outline" onClick={() => setCopiando(true)}>
              <Copy aria-hidden="true" />
              Copiar de outra unidade
            </Button>
            <Button size="sm" variant="outline" onClick={() => modalAvaria.abrir({})} disabled={rooms.length === 0}>
              <TriangleAlert aria-hidden="true" />
              Registrar avaria
            </Button>
          </>
        ) : null}
        <Link
          href={`/app/inventario/imprimir?unidade=${unit.id}`}
          target="_blank"
          className={cn(buttonVariants({ size: "sm", variant: "ghost" }))}
        >
          <Printer aria-hidden="true" />
          Imprimir lista
        </Link>
        <a href={linkDeExportacao({ unidade: unit.id })} download className={cn(buttonVariants({ size: "sm", variant: "ghost" }))}>
          <FileSpreadsheet aria-hidden="true" />
          Exportar planilha
        </a>
      </div>

      {rooms.length === 0 ? (
        <EstadoVazio
          icone={DoorOpen}
          titulo={filtrado ? "Nenhum bem com esse filtro nesta unidade" : "Esta unidade ainda não tem ambientes"}
          descricao={
            filtrado ? (
              <>Limpe a busca ou a categoria para ver todos os ambientes.</>
            ) : (
              <>
                Cadastre os cômodos <strong>na ordem em que se caminha pela casa</strong> — é a ordem da conferência. Se
                outra unidade é igual a esta, copie dela: ambientes e bens vêm juntos.
              </>
            )
          }
          acao={
            permissoes.criar && !filtrado ? (
              <div className="flex flex-wrap justify-center gap-2">
                <Button onClick={() => modalAmbiente.abrir({ ambiente: null, ordemSugerida: 0 })}>
                  <Plus aria-hidden="true" />
                  Criar o primeiro ambiente
                </Button>
                <Button variant="outline" onClick={() => setCopiando(true)}>
                  <Copy aria-hidden="true" />
                  Copiar de outra unidade
                </Button>
              </div>
            ) : null
          }
        />
      ) : (
        <ol className="flex flex-col gap-4">
          {rooms.map((ambiente, i) => (
            <li key={ambiente.id}>
              <CartaoDoAmbiente
                ambiente={ambiente}
                posicao={i + 1}
                permissoes={permissoes}
                aoEditar={() => modalAmbiente.abrir({ ambiente, ordemSugerida: proximaOrdem })}
                aoAlternar={() => setAAlternar(ambiente)}
                aoApagar={() => setAApagar(ambiente)}
                aoColocar={() => modalColocacao.abrir({ modo: "colocar", ambiente })}
                aoEditarColocacao={(colocacao) => modalColocacao.abrir({ modo: "editar", ambiente, colocacao })}
                aoTirar={(colocacao) => setATirar({ colocacao, ambiente: ambiente.name })}
                aoRegistrarAvaria={(colocacao) => modalAvaria.abrir({ ambienteId: ambiente.id, itemId: colocacao.item_id })}
              />
            </li>
          ))}
        </ol>
      )}

      {totals.uncosted_items ? (
        <Nota>
          {totals.uncosted_items} {totals.uncosted_items === 1 ? "bem não tem" : "bens não têm"} custo de reposição cotado e{" "}
          {totals.uncosted_items === 1 ? "ficou" : "ficaram"} fora do valor {recorte ? "do recorte" : "do enxoval"}. O valor
          mostrado é só da parte cotada.
        </Nota>
      ) : null}

      <ModalDeAmbiente controle={modalAmbiente.ref} unitId={unit.id} unidadeRotulo={unit.code} />
      <ModalDeColocacao controle={modalColocacao.ref} catalogo={catalogo} />
      <ModalDeAvaria controle={modalAvaria.ref} ambientes={rooms} unidadeRotulo={unit.code} />
      <ModalDeCopia aberto={copiando} aoMudar={setCopiando} destino={unit} unidades={unidades} />

      <ConfirmacaoDoInventario
        aberto={aAlternar !== null}
        aoMudar={(a) => !a && setAAlternar(null)}
        contexto="ambiente"
        titulo={aAlternar?.active ? `Desativar ${aAlternar?.name ?? ""}?` : `Reativar ${aAlternar?.name ?? ""}?`}
        rotuloConfirmar={aAlternar?.active ? "Desativar" : "Reativar"}
        descricao={
          aAlternar?.active ? (
            <>O cômodo sai das próximas conferências. O histórico de contagens e avarias dele continua legível.</>
          ) : (
            <>O cômodo volta a entrar nas próximas conferências, com os bens que estão colocados nele.</>
          )
        }
        aoConfirmar={() => alternarAtivoDoAmbiente(aAlternar!.id, !aAlternar!.active)}
      />

      <ConfirmacaoDoInventario
        aberto={aApagar !== null}
        aoMudar={(a) => !a && setAApagar(null)}
        contexto="ambiente"
        destrutivo
        titulo={`Apagar ${aApagar?.name ?? ""}?`}
        rotuloConfirmar="Apagar de vez"
        descricao={
          <>
            Apaga o cômodo <strong>e a lista de bens dele</strong>. É para o ambiente criado por engano. Se ele já passou
            por conferência ou teve avaria, não dá para apagar — desative em vez disso.
          </>
        }
        aoConfirmar={() => apagarAmbiente(aApagar!.id)}
      />

      <ConfirmacaoDoInventario
        aberto={aTirar !== null}
        aoMudar={(a) => !a && setATirar(null)}
        contexto="colocacao"
        destrutivo
        titulo={`Tirar ${aTirar?.colocacao.item_name ?? "o bem"} de ${aTirar?.ambiente ?? "do ambiente"}?`}
        rotuloConfirmar="Tirar do ambiente"
        descricao={
          <>
            O bem deixa de ser esperado neste cômodo. Ele continua no catálogo, nos outros ambientes, e nas conferências
            antigas. Se a ideia é dizer “este cômodo não tem mais, de propósito”, prefira editar a quantidade para 0.
          </>
        }
        aoConfirmar={() => tirarBem(aTirar!.colocacao.id)}
      />
    </>
  );
}

/**
 * Os números da unidade. A API soma **o que a resposta mostra**: com filtro,
 * "120 peças" é a louça, não o apartamento — e a legenda diz isso, senão o
 * número do recorte seria lido como o da casa inteira.
 */
function Resumo({ inventario, recorte }: { inventario: InventarioDaUnidade; recorte: string | null }) {
  const { totals } = inventario;
  return (
    <div className="flex flex-col gap-2">
      {recorte ? (
        <p className="text-xs text-muted-foreground" data-testid="legenda-do-recorte">
          Números do recorte — <strong className="font-medium text-foreground">{recorte}</strong> —, não do apartamento
          inteiro.
        </p>
      ) : null}
      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-5">
        <Numero rotulo="Ambientes" valor={totals.rooms} />
        <Numero rotulo="Bens distintos" valor={totals.items} />
        <Numero rotulo="Peças esperadas" valor={totals.expected_qty} />
        <Numero rotulo="Avarias em aberto" valor={totals.open_issues} destaque={totals.open_issues > 0} />
        <div className="col-span-2 rounded-xl border border-border/60 bg-muted/20 px-3 py-2 sm:col-span-1">
          <dt className="text-xs text-muted-foreground">{recorte ? "Valor cotado do recorte" : "Valor do enxoval cotado"}</dt>
          <dd className="font-display mt-0.5 text-lg tabular-nums">
            {typeof totals.replacement_cost_cents === "number" ? formatarBRL(totals.replacement_cost_cents) : "—"}
          </dd>
        </div>
      </dl>
    </div>
  );
}

function Numero({ rotulo, valor, destaque }: { rotulo: string; valor: number; destaque?: boolean }) {
  return (
    <div className="rounded-xl border border-border/60 bg-muted/20 px-3 py-2">
      <dt className="text-xs text-muted-foreground">{rotulo}</dt>
      <dd className={cn("font-display mt-0.5 text-lg tabular-nums", destaque && "text-destructive")}>{valor}</dd>
    </div>
  );
}

function CartaoDoAmbiente({
  ambiente,
  posicao,
  permissoes,
  aoEditar,
  aoAlternar,
  aoApagar,
  aoColocar,
  aoEditarColocacao,
  aoTirar,
  aoRegistrarAvaria,
}: {
  ambiente: AmbienteDoInventario;
  posicao: number;
  permissoes: PermissoesDaUnidade;
  aoEditar: () => void;
  aoAlternar: () => void;
  aoApagar: () => void;
  aoColocar: () => void;
  aoEditarColocacao: (c: Colocacao) => void;
  aoTirar: (c: Colocacao) => void;
  aoRegistrarAvaria: (c: Colocacao) => void;
}) {
  const abertas = ambiente.open_issues_count ?? 0;
  return (
    <section
      aria-label={ambiente.name}
      className={cn("rounded-xl border border-border/60 bg-muted/20 p-3 sm:p-4", !ambiente.active && "border-dashed opacity-75")}
    >
      <header className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-xs tabular-nums text-muted-foreground" title="Posição na caminhada pela casa">
              {posicao}.
            </span>
            <h2 className="font-display text-base leading-tight">{ambiente.name}</h2>
            <Badge variant="neutral">{ROTULO_DO_AMBIENTE[ambiente.kind]}</Badge>
            {!ambiente.active ? <Badge variant="outline">desativado</Badge> : null}
            {abertas > 0 ? (
              <Badge variant="destructive">
                <AlertTriangle aria-hidden="true" />
                {abertas} {abertas === 1 ? "avaria" : "avarias"}
              </Badge>
            ) : null}
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            <span className="tabular-nums">{ambiente.items_count ?? ambiente.items.length}</span>{" "}
            {(ambiente.items_count ?? ambiente.items.length) === 1 ? "bem" : "bens"}
            {typeof ambiente.expected_qty_total === "number" ? (
              <>
                {" "}
                · <span className="tabular-nums">{ambiente.expected_qty_total}</span> peças esperadas
              </>
            ) : null}
          </p>
        </div>
        <div className="flex flex-wrap gap-1">
          {permissoes.criar && ambiente.active ? (
            <Button size="sm" variant="secondary" onClick={aoColocar}>
              <Plus aria-hidden="true" />
              Colocar bem
            </Button>
          ) : null}
          {permissoes.editar ? (
            <>
              <Button size="iconSm" variant="ghost" onClick={aoEditar} aria-label={`Editar ${ambiente.name}`}>
                <Pencil aria-hidden="true" />
              </Button>
              <Button
                size="iconSm"
                variant="ghost"
                onClick={aoAlternar}
                aria-label={ambiente.active ? `Desativar ${ambiente.name}` : `Reativar ${ambiente.name}`}
              >
                <Power aria-hidden="true" />
              </Button>
            </>
          ) : null}
          {permissoes.excluir ? (
            <Button size="iconSm" variant="ghost" onClick={aoApagar} aria-label={`Apagar ${ambiente.name}`}>
              <Trash2 aria-hidden="true" />
            </Button>
          ) : null}
        </div>
      </header>

      {ambiente.items.length === 0 ? (
        <p className="mt-3 text-sm text-muted-foreground">Nenhum bem colocado neste ambiente ainda.</p>
      ) : (
        <ul className="mt-3 flex flex-col gap-1.5">
          {ambiente.items.map((c) => (
            <li key={c.id} className="flex items-center gap-3 rounded-lg border border-border/50 bg-card/70 px-2 py-1.5">
              <FotoDoBem midia={c.cover} alt={c.item_name ?? ""} className="size-11" />
              <div className="min-w-0 flex-1">
                <Link href={`/app/inventario/bens/${c.item_id}`} className="block truncate text-sm hover:underline">
                  {c.item_name ?? "Bem"}
                </Link>
                <p className="truncate text-xs text-muted-foreground">
                  {c.item_category ? ROTULO_DA_CATEGORIA[c.item_category] : null}
                  {c.note ? ` · ${c.note}` : null}
                </p>
              </div>
              <span className="shrink-0 text-sm font-medium tabular-nums">{quantidadeComMedida(c.expected_qty, c.item_unit_measure)}</span>
              <div className="flex shrink-0 gap-0.5">
                {permissoes.editar ? (
                  <Button size="iconSm" variant="ghost" onClick={() => aoEditarColocacao(c)} aria-label={`Mudar a quantidade de ${c.item_name ?? "bem"}`}>
                    <Pencil aria-hidden="true" />
                  </Button>
                ) : null}
                {permissoes.criar ? (
                  <Button size="iconSm" variant="ghost" onClick={() => aoRegistrarAvaria(c)} aria-label={`Registrar avaria de ${c.item_name ?? "bem"}`}>
                    <TriangleAlert aria-hidden="true" />
                  </Button>
                ) : null}
                {permissoes.excluir ? (
                  <Button size="iconSm" variant="ghost" onClick={() => aoTirar(c)} aria-label={`Tirar ${c.item_name ?? "bem"} do ambiente`}>
                    <Trash2 aria-hidden="true" />
                  </Button>
                ) : null}
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
