"use client";

import * as React from "react";
import Link from "next/link";
import { Mail, Pencil, Plus, ShieldOff } from "lucide-react";

import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { EstadoVazio } from "@/components/layout/estados";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatarDocumento } from "@/lib/contatos/documento";
import { formatarTelefone } from "@/lib/contatos/telefone";
import { ROTULO_DO_DOCUMENTO, type Contato } from "@/lib/contatos/tipos";
import { cn } from "@/lib/utils";

import { AvisosDeContatos } from "@/components/contatos/avisos";
import { ModalDeContato } from "@/components/contatos/modal-de-contato";

/**
 * A agenda da casa.
 *
 * ## Por que a linha inteira é um link, e o botão de editar é um botão
 *
 * O gesto mais comum aqui não é editar: é **descobrir quem é essa pessoa** —
 * quantas vezes ela veio, quanto gastou, se está hospedada agora. Esse gesto
 * mora na ficha, então a linha leva até ela. Editar continua sendo um clique,
 * mas um clique explícito: abrir o formulário sem querer é como se perde
 * cadastro.
 *
 * ## Contato anonimizado aparece, e aparece marcado
 *
 * Ele não some da lista quando alguém pede `include_anonymized`, porque a ficha
 * ainda sustenta reservas antigas e a operação precisa saber que ela existe. O
 * que não pode é ele se parecer com um contato vendável — daí a etiqueta e o
 * nome esmaecido.
 */
export function ListaDeContatos({
  contatos,
  permissoes,
  temFiltro,
}: {
  contatos: readonly Contato[];
  permissoes: { criar: boolean; editar: boolean };
  temFiltro: boolean;
}) {
  const modal = useControleDeModal<Contato | null>();

  return (
    <>
      <AvisosDeContatos />
      <ModalDeContato controle={modal.ref} />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">
          {contatos.length === 0
            ? "Nenhum contato no recorte."
            : `${contatos.length} ${contatos.length === 1 ? "contato" : "contatos"} nesta página.`}
        </p>
        {permissoes.criar ? (
          <Button size="sm" onClick={() => modal.abrir(null)}>
            <Plus aria-hidden="true" />
            Novo contato
          </Button>
        ) : null}
      </div>

      {contatos.length === 0 ? (
        <EstadoVazio
          titulo={temFiltro ? "Ninguém com esse dado" : "A agenda está vazia"}
          descricao={
            temFiltro ? (
              <>
                A busca por telefone e por documento é <strong>exata</strong>: ela responde “esta
                pessoa existe?”, não “quem se parece com isso?”. Para procurar por aproximação,
                digite parte do nome.
              </>
            ) : (
              <>
                O cadastro de contatos é a base de tudo que tem gente: a reserva exige um
                <code className="mx-1 font-mono text-xs">contact_id</code>, e o lead guarda o
                interesse, não a pessoa.
              </>
            )
          }
          acao={
            permissoes.criar ? (
              <Button onClick={() => modal.abrir(null)}>
                <Plus aria-hidden="true" />
                Cadastrar o primeiro
              </Button>
            ) : null
          }
        />
      ) : (
        // A tabela é larga por natureza (nome, contato, documento, cidade,
        // marcadores). Rola dentro do próprio container: tabela que estoura a
        // página horizontalmente é anti-padrão da casca.
        <div className="overflow-x-auto">
          <ul className="flex min-w-[44rem] flex-col gap-1.5">
            {contatos.map((contato) => {
              const anonimizado = Boolean(contato.anonymized_at);
              return (
                <li
                  key={contato.id}
                  className="flex items-center gap-3 rounded-xl border border-border/60 bg-card/70 px-3 py-2.5 transition-colors hover:bg-muted/40"
                >
                  <Link
                    href={`/app/contatos/${contato.id}`}
                    className={cn(
                      "flex min-w-0 flex-1 items-center gap-3 rounded-lg outline-none",
                      "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-card",
                    )}
                  >
                    <div className="min-w-0 flex-[2]">
                      <p
                        className={cn(
                          "truncate text-sm font-medium",
                          anonimizado ? "text-muted-foreground italic" : "text-foreground",
                        )}
                      >
                        {contato.name}
                      </p>
                      {contato.email ? (
                        <p className="flex items-center gap-1 truncate text-xs text-muted-foreground">
                          <Mail className="size-3 shrink-0" aria-hidden="true" />
                          {contato.email}
                        </p>
                      ) : null}
                    </div>

                    <div className="min-w-0 flex-1 font-mono text-xs text-muted-foreground">
                      {formatarTelefone(contato.phone_e164) || "—"}
                    </div>

                    <div className="min-w-0 flex-1 text-xs text-muted-foreground">
                      {contato.doc_number ? (
                        <>
                          <span className="uppercase">
                            {contato.doc_type ? ROTULO_DO_DOCUMENTO[contato.doc_type] : ""}
                          </span>{" "}
                          <span className="font-mono">
                            {formatarDocumento(contato.doc_type, contato.doc_number)}
                          </span>
                        </>
                      ) : (
                        "—"
                      )}
                    </div>

                    <div className="min-w-0 flex-1 truncate text-xs text-muted-foreground">
                      {[contato.city, contato.state].filter(Boolean).join("/") || "—"}
                    </div>

                    <div className="flex shrink-0 items-center gap-1.5">
                      {anonimizado ? (
                        <Badge variant="outline" title="Ficha esvaziada a pedido do titular. Existe para sustentar as reservas antigas.">
                          <ShieldOff aria-hidden="true" />
                          anonimizado
                        </Badge>
                      ) : null}
                      {contato.marketing_opt_in ? (
                        <Badge variant="accent" title="Aceitou receber ofertas — com data de consentimento registrada.">
                          opt-in
                        </Badge>
                      ) : null}
                    </div>
                  </Link>

                  {permissoes.editar && !anonimizado ? (
                    <Button
                      variant="ghost"
                      size="iconSm"
                      aria-label={`Editar ${contato.name}`}
                      onClick={() => modal.abrir(contato)}
                    >
                      <Pencil aria-hidden="true" />
                    </Button>
                  ) : null}
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </>
  );
}
