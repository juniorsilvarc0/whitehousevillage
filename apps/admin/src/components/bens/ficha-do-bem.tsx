"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Pencil, Power, Trash2 } from "lucide-react";

import { notificarSucesso } from "@/components/bens/avisos";
import { ConfirmacaoDoInventario } from "@/components/bens/confirmacao";
import { ModalDeBem } from "@/components/bens/modal-de-bem";
import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { alternarAtivoDoBem, apagarBem } from "@/lib/bens/acoes";
import { ROTULO_DA_CATEGORIA, ROTULO_DA_MEDIDA, ROTULO_DO_AMBIENTE, quantidadeComMedida } from "@/lib/bens/rotulos";
import type { Bem, BemCompleto, Colocacao } from "@/lib/bens/tipos";
import { formatarBRL } from "@/lib/dinheiro";

type Permissoes = { editar: boolean; excluir: boolean };

/**
 * Os dados do bem e os três gestos sobre ele: editar, tirar de linha, apagar.
 *
 * **Apagar não é desativar.** Apagar some com o bem — só vale para o cadastro
 * em duplicidade, que nunca foi conferido nem deu avaria. Com histórico, a API
 * recusa (`409 RESOURCE_IN_USE`) e a confirmação mostra o motivo no lugar onde
 * se clicou, com a saída certa: desativar.
 */
export function FichaDoBem({ bem, permissoes }: { bem: BemCompleto; permissoes: Permissoes }) {
  const router = useRouter();
  const edicao = useControleDeModal<Bem | null>();
  const [confirmarAtivo, setConfirmarAtivo] = React.useState(false);
  const [confirmarApagar, setConfirmarApagar] = React.useState(false);

  return (
    <>
      <div className="flex flex-col gap-4 rounded-xl border border-border/60 bg-muted/20 p-4 sm:p-5">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant="neutral">{ROTULO_DA_CATEGORIA[bem.category]}</Badge>
          {bem.active ? <Badge variant="accent">em uso</Badge> : <Badge variant="outline">fora de uso</Badge>}
          {bem.source_ref ? (
            <span className="font-mono text-[0.7rem] text-muted-foreground" title="Origem da importação">
              {bem.source_ref}
            </span>
          ) : null}
        </div>

        {bem.description ? <p className="text-sm text-muted-foreground">{bem.description}</p> : null}

        <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm sm:grid-cols-4">
          <Dado rotulo="Conta-se em">{ROTULO_DA_MEDIDA[bem.unit_measure]}</Dado>
          <Dado rotulo="Custo de reposição">
            <span className="font-mono tabular-nums">
              {typeof bem.replacement_cost_cents === "number" ? formatarBRL(bem.replacement_cost_cents) : "não cotado"}
            </span>
          </Dado>
          <Dado rotulo="Na casa inteira">
            <span className="tabular-nums">{quantidadeComMedida(bem.expected_qty_total ?? 0, bem.unit_measure)}</span>
          </Dado>
          <Dado rotulo="Avarias em aberto">
            <span className="tabular-nums">{bem.open_issues_count ?? 0}</span>
          </Dado>
        </dl>

        {permissoes.editar || permissoes.excluir ? (
          <div className="flex flex-wrap gap-2">
            {permissoes.editar ? (
              <>
                <Button size="sm" variant="outline" onClick={() => edicao.abrir(bem)}>
                  <Pencil aria-hidden="true" />
                  Editar
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setConfirmarAtivo(true)}>
                  <Power aria-hidden="true" />
                  {bem.active ? "Tirar de uso" : "Voltar a usar"}
                </Button>
              </>
            ) : null}
            {permissoes.excluir ? (
              <Button size="sm" variant="ghost" onClick={() => setConfirmarApagar(true)}>
                <Trash2 aria-hidden="true" />
                Apagar
              </Button>
            ) : null}
          </div>
        ) : null}
      </div>

      <ModalDeBem controle={edicao.ref} />

      <ConfirmacaoDoInventario
        aberto={confirmarAtivo}
        contexto="bem"
        aoMudar={setConfirmarAtivo}
        titulo={bem.active ? `Tirar ${bem.name} de uso?` : `Voltar a usar ${bem.name}?`}
        rotuloConfirmar={bem.active ? "Tirar de uso" : "Voltar a usar"}
        descricao={
          bem.active ? (
            <>O bem deixa de entrar nas conferências novas. As contagens e avarias antigas continuam legíveis.</>
          ) : (
            <>O bem volta ao catálogo em uso e entra nas próximas conferências dos ambientes onde está colocado.</>
          )
        }
        aoConfirmar={() => alternarAtivoDoBem(bem.id, !bem.active)}
      />

      <ConfirmacaoDoInventario
        aberto={confirmarApagar}
        aoMudar={setConfirmarApagar}
        titulo={`Apagar ${bem.name}?`}
        destrutivo
        contexto="bem"
        rotuloConfirmar="Apagar de vez"
        descricao={
          <>
            Apagar é para o cadastro feito em duplicidade. Bem que já foi conferido, deu avaria ou está colocado em
            algum ambiente não pode ser apagado — nesse caso, tire-o de uso.
          </>
        }
        aoConfirmar={async () => {
          const r = await apagarBem(bem.id);
          if (r.ok) {
            notificarSucesso("Bem apagado");
            router.push("/app/inventario/bens");
          }
          return r;
        }}
      />
    </>
  );
}

function Dado({ rotulo, children }: { rotulo: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{rotulo}</dt>
      <dd className="mt-0.5">{children}</dd>
    </div>
  );
}

/** "Onde está este prato" — a segunda pergunta de quem abre a ficha. */
export function OndeEsta({ colocacoes, medida }: { colocacoes: Colocacao[]; medida: BemCompleto["unit_measure"] }) {
  if (colocacoes.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        Este bem ainda não está em nenhum ambiente. Coloque-o pela tela da unidade, no cômodo onde ele fica.
      </p>
    );
  }
  const porUnidade = new Map<string, { code: string; unitId?: string; linhas: Colocacao[] }>();
  for (const c of colocacoes) {
    const chave = c.unit_id ?? c.unit_code ?? "?";
    const grupo = porUnidade.get(chave) ?? { code: c.unit_code ?? "—", unitId: c.unit_id, linhas: [] };
    grupo.linhas.push(c);
    porUnidade.set(chave, grupo);
  }
  return (
    <ul className="flex flex-col gap-3">
      {[...porUnidade.values()].map((grupo) => (
        <li key={grupo.code} className="rounded-xl border border-border/60 bg-card/70 p-3">
          {grupo.unitId ? (
            <Link href={`/app/inventario?unidade=${grupo.unitId}`} className="font-mono text-sm text-primary underline-offset-4 hover:underline">
              {grupo.code}
            </Link>
          ) : (
            <span className="font-mono text-sm">{grupo.code}</span>
          )}
          <ul className="mt-2 flex flex-col gap-1">
            {grupo.linhas.map((c) => (
              <li key={c.id} className="flex items-center justify-between gap-3 text-sm">
                <span className="min-w-0 truncate">
                  {c.room_name ?? "Ambiente"}
                  {c.room_kind ? <span className="text-muted-foreground"> · {ROTULO_DO_AMBIENTE[c.room_kind]}</span> : null}
                </span>
                <span className="shrink-0 tabular-nums">{quantidadeComMedida(c.expected_qty, medida)}</span>
              </li>
            ))}
          </ul>
        </li>
      ))}
    </ul>
  );
}
