"use client";

import * as React from "react";
import Link from "next/link";
import { Mail, Pencil, Plus, ShieldOff } from "lucide-react";

import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { EstadoVazio } from "@/components/layout/estados";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ROTULO_DO_DOCUMENTO, type ContatoNaLista } from "@/lib/contatos/tipos";
import { cn } from "@/lib/utils";

import { AvisosDeContatos } from "@/components/contatos/avisos";
import { ModalDeContato, type AberturaDoContato } from "@/components/contatos/modal-de-contato";

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
 *
 * ## A linha é mascarada, e editar passa pela ficha (dívida D11)
 *
 * Cada linha é um `ContatoNaLista`: documento, telefone e e-mail mascarados, e
 * sem `birth_date` nem `notes`. A tela mostra a máscara **como a API a
 * devolveu** — ela já vem pontuada (`***.***.777-35`), e passá-la pelos
 * formatadores de CPF e de telefone só "funcionava" porque eles devolvem como
 * veio o que não reconhecem.
 *
 * "Editar" **não** entrega a linha ao formulário: entrega o `id`, e o modal
 * busca a ficha (`GET /contacts/{id}`, que grava `pii_access_log`) antes de
 * montar qualquer campo. Com a linha, o `PUT` mandaria a máscara de volta e
 * gravaria vazio o que ela não trouxe — a anotação de um contato some assim, sem
 * erro nenhum.
 */
export function ListaDeContatos({
  contatos,
  permissoes,
  temFiltro,
}: {
  contatos: readonly ContatoNaLista[];
  permissoes: { criar: boolean; editar: boolean };
  temFiltro: boolean;
}) {
  const modal = useControleDeModal<AberturaDoContato>();

  return (
    <>
      <AvisosDeContatos />
      <ModalDeContato controle={modal.ref} />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">
          {contatos.length === 0
            ? "Nenhum contato com esses filtros."
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
                O cadastro de contatos é a base de tudo que envolve pessoas: toda reserva precisa de
                um contato, e o lead guarda o interesse, não a pessoa.
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

                    {/* Texto, nunca `tel:`: a máscara não é E.164. Ligar é pela ficha. */}
                    <div className="min-w-0 flex-1 font-mono text-xs text-muted-foreground">
                      {contato.phone_e164 ?? "—"}
                    </div>

                    <div className="min-w-0 flex-1 text-xs text-muted-foreground">
                      {contato.doc_number ? (
                        <>
                          <span className="uppercase">
                            {contato.doc_type ? ROTULO_DO_DOCUMENTO[contato.doc_type] : ""}
                          </span>{" "}
                          <span className="font-mono">{contato.doc_number}</span>
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
                        <Badge variant="outline" title="Dados pessoais apagados a pedido da pessoa. A ficha fica para manter as reservas antigas.">
                          <ShieldOff aria-hidden="true" />
                          anonimizado
                        </Badge>
                      ) : null}
                      {contato.marketing_opt_in ? (
                        <Badge variant="accent" title="Aceitou receber ofertas — com a data do aceite registrada.">
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
                      onClick={() => modal.abrir({ buscar: { id: contato.id, nome: contato.name } })}
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
