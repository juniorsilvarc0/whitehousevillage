"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { AlertTriangle, ImageOff, MapPin, Plus } from "lucide-react";

import { FotoDoBem } from "@/components/bens/foto";
import { ModalDeBem } from "@/components/bens/modal-de-bem";
import { notificarSucesso } from "@/components/bens/avisos";
import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { EstadoVazio } from "@/components/layout/estados";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatarBRL } from "@/lib/dinheiro";
import { ROTULO_DA_CATEGORIA, quantidadeComMedida } from "@/lib/bens/rotulos";
import type { Bem } from "@/lib/bens/tipos";
import { cn } from "@/lib/utils";

export type PermissoesDoCatalogo = { criar: boolean; editar: boolean; excluir: boolean };

/**
 * A grade do catálogo: um cartão por bem, com a **miniatura**.
 *
 * O cartão inteiro leva à ficha, que é onde se envia foto, reordena a galeria
 * e se vê "onde está este prato". Criar fica num botão à parte, e só aparece
 * para quem pode (`inventory.goods:criar`) — esconder é cortesia, a API recusa
 * de qualquer jeito.
 */
export function CatalogoDeBens({
  bens,
  permissoes,
  temFiltro,
  soSemFoto,
}: {
  bens: readonly Bem[];
  permissoes: PermissoesDoCatalogo;
  temFiltro: boolean;
  soSemFoto: boolean;
}) {
  const router = useRouter();
  const modal = useControleDeModal<Bem | null>();

  function depoisDeSalvar(bem: Bem, criado: boolean) {
    if (!criado) return;
    notificarSucesso("Bem cadastrado", "Agora envie a foto — é ela que faz quem confere reconhecer a peça.");
    router.push(`/app/inventario/bens/${bem.id}`);
  }

  return (
    <>
      <ModalDeBem controle={modal.ref} aoSalvar={depoisDeSalvar} />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">
          {bens.length === 0 ? "Nenhum bem com esses filtros." : `${bens.length} ${bens.length === 1 ? "bem" : "bens"} nesta página.`}
        </p>
        {permissoes.criar ? (
          <Button size="sm" onClick={() => modal.abrir(null)}>
            <Plus aria-hidden="true" />
            Novo bem
          </Button>
        ) : null}
      </div>

      {bens.length === 0 ? (
        <EstadoVazio
          icone={soSemFoto ? ImageOff : undefined}
          titulo={soSemFoto ? "Todos os bens têm foto" : temFiltro ? "Nada com esse recorte" : "O catálogo está vazio"}
          descricao={
            soSemFoto ? (
              <>A fila de fotos está zerada. Quem confere reconhece cada peça pela imagem.</>
            ) : temFiltro ? (
              <>Tente outra categoria, ou limpe os filtros para ver o catálogo inteiro.</>
            ) : (
              <>
                O catálogo é a lista de <strong>tudo o que a casa tem</strong> — um item por objeto, esteja ele em
                um cômodo ou em seis apartamentos. A quantidade de cada lugar se define depois, na tela da unidade.
              </>
            )
          }
          acao={
            permissoes.criar && !soSemFoto ? (
              <Button onClick={() => modal.abrir(null)}>
                <Plus aria-hidden="true" />
                Cadastrar o primeiro
              </Button>
            ) : null
          }
        />
      ) : (
        <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {bens.map((bem) => (
            <li key={bem.id}>
              <CartaoDoBem bem={bem} />
            </li>
          ))}
        </ul>
      )}
    </>
  );
}

function CartaoDoBem({ bem }: { bem: Bem }) {
  const abertas = bem.open_issues_count ?? 0;
  return (
    <Link
      href={`/app/inventario/bens/${bem.id}`}
      className={cn(
        "flex h-full gap-3 rounded-xl border border-border/60 bg-card/70 p-3 outline-none transition-colors hover:bg-muted/40",
        "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-card",
        !bem.active && "opacity-70",
      )}
    >
      <FotoDoBem midia={bem.cover} alt={bem.name} className="size-20" />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex items-start justify-between gap-2">
          <h3 className="font-display min-w-0 truncate text-sm leading-tight">{bem.name}</h3>
          {!bem.active ? <Badge variant="outline">fora de uso</Badge> : null}
        </div>
        {bem.description ? <p className="line-clamp-2 text-xs text-muted-foreground">{bem.description}</p> : null}
        <div className="mt-auto flex flex-wrap items-center gap-1.5 pt-1">
          <Badge variant="neutral">{ROTULO_DA_CATEGORIA[bem.category]}</Badge>
          {bem.cover ? null : (
            <Badge variant="outline">
              <ImageOff aria-hidden="true" />
              sem foto
            </Badge>
          )}
          {abertas > 0 ? (
            <Badge variant="destructive">
              <AlertTriangle aria-hidden="true" />
              {abertas} {abertas === 1 ? "avaria" : "avarias"}
            </Badge>
          ) : null}
        </div>
        <dl className="flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
          <div className="flex items-center gap-1">
            <dt className="sr-only">Na casa</dt>
            <MapPin className="size-3" aria-hidden="true" />
            <dd className="tabular-nums">
              {quantidadeComMedida(bem.expected_qty_total ?? 0, bem.unit_measure)} em{" "}
              {bem.placements_count ?? 0} {bem.placements_count === 1 ? "ambiente" : "ambientes"}
            </dd>
          </div>
          <div>
            <dt className="sr-only">Custo de reposição</dt>
            <dd className="font-mono tabular-nums">
              {typeof bem.replacement_cost_cents === "number" ? formatarBRL(bem.replacement_cost_cents) : "não cotado"}
            </dd>
          </div>
        </dl>
      </div>
    </Link>
  );
}
