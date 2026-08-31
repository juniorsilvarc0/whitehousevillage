"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { Download, Loader2, Pencil, ShieldOff, Trash2 } from "lucide-react";

import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { Nota } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatarData, formatarInstante } from "@/lib/datas";
import { formatarDocumento } from "@/lib/contatos/documento";
import { formatarTelefone } from "@/lib/contatos/telefone";
import {
  EXPLICACAO_DA_BASE,
  ROTULO_DA_BASE,
  ROTULO_DO_DOCUMENTO,
  ROTULO_DO_VINCULO,
  totalDeVinculos,
  vinculosDosDetalhes,
  type ContatoCompleto,
  type VinculosDoContato,
} from "@/lib/contatos/tipos";

import { apagarContato, exportarContato } from "@/app/(app)/app/contatos/acoes";
import { AvisosDeContatos, notificar, notificarSucesso } from "@/components/contatos/avisos";
import { ModalDeAnonimizacao } from "@/components/contatos/modal-de-anonimizacao";
import { ModalDeContato } from "@/components/contatos/modal-de-contato";

/** Uma linha do bloco de dados. `—` explícito: campo vazio que some da tela vira
 *  "esqueci de perguntar" indistinguível de "não tem". */
function Linha({ rotulo, children }: { rotulo: string; children: React.ReactNode }) {
  return (
    <div className="flex items-baseline gap-3 py-1.5">
      <dt className="w-32 shrink-0 text-xs uppercase tracking-wider text-muted-foreground">{rotulo}</dt>
      <dd className="min-w-0 flex-1 text-sm text-foreground">{children || "—"}</dd>
    </div>
  );
}

/**
 * A ficha da pessoa — e as três ações que só existem aqui.
 *
 * ## Apagar e anonimizar não são a mesma coisa, e a tela não deixa confundir
 *
 * `DELETE` apaga de verdade, e **só** o que nunca existiu comercialmente: serve
 * para o erro de digitação de dois minutos atrás. Assim que houver qualquer
 * vínculo, a API recusa com `409 RESOURCE_IN_USE` — apagar derrubaria a FK
 * `reservations.contact_id`, que é `NOT NULL`, e o preço de "sumir com o
 * cadastro" seria sumir com a venda.
 *
 * Por isso o botão de apagar **não some** quando há vínculo: ele existe, é
 * oferecido, e a recusa da API é mostrada com a contagem do que impediu, junto
 * com o caminho certo (anonimizar). Esconder o botão com base no `references`
 * lido há dez minutos mentiria nas duas direções — sumindo quando ainda dava, e
 * aparecendo quando já não dá.
 */
export function FichaDoContato({
  contato,
  permissoes,
}: {
  contato: ContatoCompleto;
  permissoes: { editar: boolean; excluir: boolean };
}) {
  const router = useRouter();
  const edicao = useControleDeModal<ContatoCompleto | null>();
  const anonimizacao = useControleDeModal<null>();

  const [exportando, setExportando] = React.useState(false);
  const [apagando, setApagando] = React.useState(false);
  const [recusaDeExclusao, setRecusaDeExclusao] = React.useState<VinculosDoContato | null>(null);

  const anonimizado = Boolean(contato.anonymized_at);
  const vinculos = contato.references;
  const totalVinculado = totalDeVinculos(vinculos);

  async function exportar() {
    setExportando(true);
    try {
      const resultado = await exportarContato(contato.id);
      if (!resultado.ok) {
        notificar(resultado);
        return;
      }
      // O arquivo nasce no navegador a partir do que a action trouxe: o JWT vive
      // em cookie `httpOnly` e um link direto para a API sairia sem
      // `Authorization`.
      const blob = new Blob([JSON.stringify(resultado.data, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const ancora = document.createElement("a");
      ancora.href = url;
      ancora.download = `contato-${contato.id}.json`;
      ancora.click();
      URL.revokeObjectURL(url);
      notificarSucesso("Exportação pronta", "A leitura ficou registrada na trilha de LGPD.");
    } finally {
      setExportando(false);
    }
  }

  async function apagar() {
    setApagando(true);
    setRecusaDeExclusao(null);
    try {
      const resultado = await apagarContato(contato.id);
      if (resultado.ok) {
        notificarSucesso("Contato apagado");
        router.replace("/app/contatos");
        return;
      }
      notificar(resultado);
      if (resultado.code === "RESOURCE_IN_USE") {
        setRecusaDeExclusao(vinculosDosDetalhes(resultado.details) ?? vinculos ?? null);
      }
    } finally {
      setApagando(false);
    }
  }

  const impedimentos = recusaDeExclusao
    ? (Object.keys(ROTULO_DO_VINCULO) as (keyof VinculosDoContato)[])
        .filter((chave) => (recusaDeExclusao[chave] ?? 0) > 0)
        .map((chave) => `${recusaDeExclusao[chave]} ${ROTULO_DO_VINCULO[chave]}`)
    : [];

  return (
    <>
      <AvisosDeContatos />
      <ModalDeContato controle={edicao.ref} />
      <ModalDeAnonimizacao controle={anonimizacao.ref} contato={contato} vinculos={vinculos} />

      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2">
          {anonimizado ? (
            <Badge variant="outline">
              <ShieldOff aria-hidden="true" />
              anonimizado em {formatarInstante(contato.anonymized_at as string)}
            </Badge>
          ) : null}
          {contato.marketing_opt_in ? <Badge variant="accent">aceita ofertas</Badge> : null}
          {totalVinculado > 0 ? (
            <Badge variant="neutral">{totalVinculado} registros vinculados</Badge>
          ) : null}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {permissoes.editar && !anonimizado ? (
            <Button size="sm" variant="outline" onClick={() => edicao.abrir(contato)}>
              <Pencil aria-hidden="true" />
              Editar
            </Button>
          ) : null}
          <Button size="sm" variant="outline" onClick={exportar} disabled={exportando}>
            {exportando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Download aria-hidden="true" />}
            Exportar dados
          </Button>
        </div>
      </div>

      <section aria-label="Dados do contato" className="rounded-xl border border-border/60 bg-muted/20 p-4 sm:p-5">
        <dl className="divide-y divide-border/50">
          <Linha rotulo="Telefone">
            <span className="font-mono">{formatarTelefone(contato.phone_e164)}</span>
          </Linha>
          <Linha rotulo="E-mail">{contato.email}</Linha>
          <Linha rotulo="Documento">
            {contato.doc_number ? (
              <>
                {contato.doc_type ? ROTULO_DO_DOCUMENTO[contato.doc_type] : ""}{" "}
                <span className="font-mono">{formatarDocumento(contato.doc_type, contato.doc_number)}</span>
              </>
            ) : null}
          </Linha>
          <Linha rotulo="Nascimento">{contato.birth_date ? formatarData(contato.birth_date) : null}</Linha>
          <Linha rotulo="Cidade">{[contato.city, contato.state].filter(Boolean).join("/")}</Linha>
          <Linha rotulo="Observações">{contato.notes}</Linha>
          <Linha rotulo="Cadastrado">{formatarInstante(contato.created_at)}</Linha>
        </dl>
      </section>

      <section aria-label="Base legal e consentimento" className="rounded-xl border border-border/60 bg-muted/20 p-4 sm:p-5">
        <h2 className="font-display text-base leading-tight">Base legal e consentimento</h2>
        <p className="mt-1 max-w-prose text-sm text-muted-foreground">
          Dois eixos, nunca o mesmo: a base legal sustenta <strong>guardar</strong> a ficha; o
          aceite autoriza <strong>mandar oferta</strong>. Executar a reserva de quem nunca aceitou
          propaganda é legítimo; mandar promoção para essa pessoa não é.
        </p>
        <dl className="mt-3 divide-y divide-border/50">
          <Linha rotulo="Base legal">
            {contato.lgpd_basis ? (
              <>
                {ROTULO_DA_BASE[contato.lgpd_basis]}
                <span className="ml-2 text-xs text-muted-foreground">
                  {EXPLICACAO_DA_BASE[contato.lgpd_basis]}
                </span>
              </>
            ) : null}
          </Linha>
          <Linha rotulo="Marketing">{contato.marketing_opt_in ? "Aceita receber" : "Não aceita"}</Linha>
          <Linha rotulo="Aceite em">
            {contato.consent_at ? formatarInstante(contato.consent_at) : null}
          </Linha>
        </dl>
      </section>

      {permissoes.excluir && !anonimizado ? (
        <section
          aria-label="Eliminação de dado pessoal"
          className="rounded-xl border border-destructive/25 bg-destructive/[0.04] p-4 sm:p-5"
        >
          <h2 className="font-display text-base leading-tight">Eliminação de dado pessoal</h2>
          <p className="mt-1 max-w-prose text-sm text-muted-foreground">
            <strong>Anonimizar</strong> é o direito de eliminação da LGPD: a pessoa some e a venda
            fica — reservas, valores e razão continuam apontando para o mesmo id.{" "}
            <strong>Apagar</strong> só funciona em contato que nunca foi usado; com histórico, a API
            recusa, porque a reserva exige o cadastro.
          </p>

          {impedimentos.length > 0 ? (
            <Nota variante="atencao" className="mt-3">
              Não dá para apagar: existem <strong>{impedimentos.join(", ")}</strong> apontando para
              esta ficha. O caminho é a anonimização — ela elimina o dado pessoal sem derrubar o que
              já foi vendido.
            </Nota>
          ) : null}

          <div className="mt-4 flex flex-wrap items-center gap-2">
            <Button size="sm" variant="destructive" onClick={() => anonimizacao.abrir(null)}>
              <ShieldOff aria-hidden="true" />
              Anonimizar
            </Button>
            <Button size="sm" variant="ghost" onClick={apagar} disabled={apagando}>
              {apagando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Trash2 aria-hidden="true" />}
              Apagar cadastro
            </Button>
          </div>
        </section>
      ) : null}
    </>
  );
}
