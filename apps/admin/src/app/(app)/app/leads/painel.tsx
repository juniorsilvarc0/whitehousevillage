"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  ArrowRightCircle, CalendarRange, Loader2, Phone, Sparkles, TriangleAlert, UserX,
} from "lucide-react";

import { AvisosDoCrm, descricaoDaFalha, notificarSucesso } from "@/components/crm/avisos";
import { useControleDeModal, type ControleDeModal } from "@/components/layout/controle-de-modal";
import { EstadoVazio } from "@/components/layout/estados";
import { ModalDeConfirmacao } from "@/components/layout/modal-de-confirmacao";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import type { Produto } from "@/lib/api/comercial";
import type { FalhaCrm } from "@/lib/crm/api";
import { codigoGeral } from "@/lib/crm/codigos";
import { formatarTelefone } from "@/lib/contatos/telefone";
import { ConversaoFormulario } from "@/lib/crm/esquemas";
import type { EstadoDoLead, EtapaDoFunil, Funil, Lead } from "@/lib/crm/tipos";
import { formatarData, formatarDataCurta, noitesEntre } from "@/lib/datas";
import { reaisDeCentavos } from "@/lib/dinheiro";
import { rotuloDaOrigem } from "@/lib/origem";
import { cn } from "@/lib/utils";

import { converterLead, descartarLead } from "./acoes";

/**
 * A lista de leads — o interesse antes de virar negócio.
 *
 * O lead guarda **o interesse, não a pessoa**: nome, telefone e e-mail vivem em
 * `contacts`, um registro por pessoa. É essa separação que deixa o WhatsApp
 * reconhecer quem já existe em vez de criar um segundo cadastro a cada
 * mensagem — e é por isso que a lista mostra o contato, mas não o edita.
 */

const ROTULO_DO_ESTADO: Record<EstadoDoLead, string> = {
  novo: "Novo",
  em_atendimento: "Em atendimento",
  qualificado: "Qualificado",
  convertido: "Convertido",
  descartado: "Descartado",
};

export type PermissoesDeLeads = { editar: boolean; excluir: boolean };

export function PainelDeLeads({
  leads,
  funis,
  etapas,
  produtos,
  permissoes,
}: {
  leads: Lead[];
  funis: Funil[];
  etapas: EtapaDoFunil[];
  produtos: Produto[];
  permissoes: PermissoesDeLeads;
}) {
  const conversao = useControleDeModal<Lead>();
  const [descartando, setDescartando] = React.useState<Lead | null>(null);

  if (leads.length === 0) {
    return (
      <>
        <AvisosDoCrm />
        <EstadoVazio
          icone={Sparkles}
          titulo="Nenhum lead com esses filtros"
          descricao="Se o seu perfil vê só os seus leads, os que ainda não têm dono não aparecem aqui: eles ficam com a gestão até serem distribuídos."
        />
      </>
    );
  }

  return (
    <>
      <AvisosDoCrm />
      <ul className="flex flex-col gap-2">
        {leads.map((lead) => (
          <CartaoDeLead
            key={lead.id}
            lead={lead}
            podeEditar={permissoes.editar}
            aoConverter={() => conversao.abrir(lead)}
            aoDescartar={() => setDescartando(lead)}
          />
        ))}
      </ul>

      <ModalDeConversao controle={conversao.ref} funis={funis} etapas={etapas} produtos={produtos} />

      <ModalDeConfirmacao
        aberto={descartando !== null}
        aoMudar={(aberto) => {
          if (!aberto) setDescartando(null);
        }}
        titulo="Descartar este lead?"
        destrutivo
        rotuloConfirmar="Descartar"
        descricao={
          <>
            O lead de <strong>{descartando?.contact_name}</strong> passa a <strong>descartado</strong> e sai
            da fila — mas <strong>não é apagado</strong>. Ele continua contando nos números de quanto
            interesse chega e quanto não vira negócio.
          </>
        }
        aoConfirmar={async () => {
          if (!descartando) return { ok: true as const, data: null };
          const resultado = await descartarLead(descartando.id);
          if (resultado.ok) setDescartando(null);
          // O `ModalDeConfirmacao` da casca só fala o vocabulário geral de
          // códigos; `codigoGeral` traduz preservando a classe da recusa.
          return resultado.ok
            ? { ok: true as const, data: null }
            : { ok: false as const, code: codigoGeral(resultado.code), message: resultado.message, details: resultado.details };
        }}
      />
    </>
  );
}

function CartaoDeLead({
  lead,
  podeEditar,
  aoConverter,
  aoDescartar,
}: {
  lead: Lead;
  podeEditar: boolean;
  aoConverter: () => void;
  aoDescartar: () => void;
}) {
  const convertido = lead.status === "convertido";
  const noites =
    lead.desired_check_in && lead.desired_check_out
      ? noitesEntre(lead.desired_check_in, lead.desired_check_out)
      : 0;

  return (
    <li className="flex flex-wrap items-start justify-between gap-3 rounded-xl border border-border/60 bg-muted/20 px-3.5 py-3">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <p className="truncate text-sm font-medium">{lead.contact_name}</p>
          <Badge variant={convertido ? "accent" : lead.status === "descartado" ? "outline" : "neutral"}>
            {ROTULO_DO_ESTADO[lead.status]}
          </Badge>
          <Badge variant="outline">{rotuloDaOrigem(lead.source)}</Badge>
          {lead.score > 0 ? (
            <span className="font-mono text-[0.68rem] tabular-nums text-muted-foreground">
              pontuação {lead.score}
            </span>
          ) : null}
        </div>

        <p className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          {lead.contact_phone_e164 ? (
            <a href={`tel:${lead.contact_phone_e164}`} className="flex items-center gap-1.5 hover:text-foreground">
              <Phone className="size-3.5" aria-hidden="true" />
              {formatarTelefone(lead.contact_phone_e164)}
            </a>
          ) : null}
          <span>{lead.interest_unit_type_name ?? "Produto não informado"}</span>
          {lead.desired_check_in && lead.desired_check_out ? (
            <span className="flex items-center gap-1.5">
              <CalendarRange className="size-3.5" aria-hidden="true" />
              {formatarDataCurta(lead.desired_check_in)} → {formatarDataCurta(lead.desired_check_out)}
              <span className="tabular-nums">({noites} {noites === 1 ? "noite" : "noites"})</span>
            </span>
          ) : (
            <span>Datas não informadas</span>
          )}
          <span>{lead.owner_name ?? "Sem dono — aguardando a gestão"}</span>
        </p>
      </div>

      <div className="flex shrink-0 flex-wrap items-center gap-2">
        {convertido && lead.opportunity_id ? (
          <Link
            href={`/app/oportunidades/${lead.opportunity_id}`}
            className={cn(buttonVariants({ variant: "outline", size: "sm" }))}
          >
            Abrir a oportunidade
          </Link>
        ) : null}

        {podeEditar && !convertido && lead.status !== "descartado" ? (
          <>
            <Button size="sm" onClick={aoConverter}>
              <ArrowRightCircle aria-hidden="true" />
              Converter
            </Button>
            <Button size="sm" variant="ghost" onClick={aoDescartar}>
              <UserX aria-hidden="true" />
              Descartar
            </Button>
          </>
        ) : null}
      </div>
    </li>
  );
}

/**
 * Converter — **numa transação**: a oportunidade nasce, o lead vira
 * `convertido`, e a entrada na primeira etapa já dispara a tarefa automática.
 *
 * Todo campo é opcional e todo campo em branco é herdado do lead. É o que
 * permite o caminho de um clique ("Converter" e pronto) sem tirar da gestão a
 * chance de corrigir o produto ou as datas na hora — que é o momento em que ela
 * de fato sabe o que o hóspede quer.
 */
function ModalDeConversao({
  controle,
  funis,
  etapas,
  produtos,
}: {
  controle: React.RefObject<ControleDeModal<Lead> | null>;
  funis: Funil[];
  etapas: EtapaDoFunil[];
  produtos: Produto[];
}) {
  const router = useRouter();
  const [aberto, setAberto] = React.useState(false);
  const [lead, setLead] = React.useState<Lead | null>(null);
  const [enviando, setEnviando] = React.useState(false);
  const [falha, setFalha] = React.useState<FalhaCrm | null>(null);
  const [erros, setErros] = React.useState<Record<string, string>>({});
  const [valores, setValores] = React.useState<ConversaoFormulario>(vazio());

  React.useImperativeHandle(controle, () => ({
    abrir(escolhido: Lead) {
      setLead(escolhido);
      setFalha(null);
      setErros({});
      // Os campos nascem **vazios**, não pré-preenchidos com o lead: em branco
      // significa "herda", e é o próprio servidor que herda, numa transação só.
      // Copiar os valores para cá e reenviá-los pareceria igual e não seria —
      // o que o lead mudasse entre abrir e salvar seria sobrescrito pelo que
      // esta tela leu primeiro.
      setValores(vazio());
      setAberto(true);
    },
  }), []);

  const etapasDoFunil = valores.pipeline_id
    ? etapas.filter((etapa) => etapa.pipeline_id === valores.pipeline_id)
    : [];

  async function enviar(evento: React.FormEvent) {
    evento.preventDefault();
    if (!lead) return;

    const analise = ConversaoFormulario.safeParse(valores);
    if (!analise.success) {
      const detalhes: Record<string, string> = {};
      for (const problema of analise.error.issues) {
        const chave = problema.path.join(".");
        if (chave && !(chave in detalhes)) detalhes[chave] = problema.message;
      }
      setErros(detalhes);
      return;
    }

    setErros({});
    setFalha(null);
    setEnviando(true);
    try {
      const resultado = await converterLead(lead.id, analise.data);
      if (!resultado.ok) {
        setFalha(resultado);
        return;
      }
      notificarSucesso(
        "Oportunidade criada",
        resultado.data.auto_task
          ? `A tarefa "${resultado.data.auto_task.subject}" já foi criada com o prazo da etapa.`
          : undefined,
      );
      setAberto(false);
      // Vai direto ao card: o passo seguinte da conversão é sempre trabalhar a
      // oportunidade, nunca voltar à lista de leads.
      router.push(`/app/oportunidades/${resultado.data.opportunity.id}`);
    } finally {
      setEnviando(false);
    }
  }

  const idDaOportunidadeExistente =
    falha?.code === "LEAD_ALREADY_CONVERTED" && typeof falha.details.opportunity_id === "string"
      ? falha.details.opportunity_id
      : null;

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title="Converter em oportunidade"
      description={lead ? lead.contact_name : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={enviando}>
            Cancelar
          </Button>
          <Button type="submit" form="form-conversao" disabled={enviando}>
            {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <ArrowRightCircle aria-hidden="true" />}
            Converter
          </Button>
        </>
      }
    >
      {lead ? (
        <form id="form-conversao" onSubmit={enviar} className="flex flex-col gap-4">
          <Nota>
            Tudo aqui é <strong>opcional</strong>. O que ficar em branco vem do lead: produto, datas e
            dono. Sem funil escolhido, vai para o funil padrão; sem etapa, para a primeira — e a tarefa
            de retorno ao cliente é criada automaticamente.
          </Nota>

          <ResumoDoLead lead={lead} />

          <div className="grid gap-4 sm:grid-cols-2">
            <Campo id="conv-funil" label="Funil">
              {(props) => (
                <Select
                  {...props}
                  value={valores.pipeline_id}
                  onChange={(e) => setValores((v) => ({ ...v, pipeline_id: e.target.value, stage_id: "" }))}
                >
                  <option value="">Funil padrão</option>
                  {funis.map((funil) => (
                    <option key={funil.id} value={funil.id}>
                      {funil.name}
                    </option>
                  ))}
                </Select>
              )}
            </Campo>

            <Campo id="conv-etapa" label="Etapa de entrada">
              {(props) => (
                <Select
                  {...props}
                  value={valores.stage_id}
                  disabled={valores.pipeline_id === ""}
                  onChange={(e) => setValores((v) => ({ ...v, stage_id: e.target.value }))}
                >
                  <option value="">{valores.pipeline_id ? "Primeira etapa" : "Escolha o funil primeiro"}</option>
                  {etapasDoFunil.map((etapa) => (
                    <option key={etapa.id} value={etapa.id}>
                      {etapa.name}
                    </option>
                  ))}
                </Select>
              )}
            </Campo>

            <Campo id="conv-produto" label="Produto pretendido" className="sm:col-span-2">
              {(props) => (
                <Select
                  {...props}
                  value={valores.unit_type_id}
                  onChange={(e) => setValores((v) => ({ ...v, unit_type_id: e.target.value }))}
                >
                  <option value="">
                    {lead.interest_unit_type_name ? `Herdar: ${lead.interest_unit_type_name}` : "Sem produto definido"}
                  </option>
                  {produtos.map((produto) => (
                    <option key={produto.id} value={produto.id}>
                      {produto.name}
                    </option>
                  ))}
                </Select>
              )}
            </Campo>

            <Campo id="conv-entrada" label="Entrada" erro={erros.check_in}>
              {(props) => (
                <Input
                  {...props}
                  type="date"
                  className="tabular-nums"
                  value={valores.check_in}
                  onChange={(e) => setValores((v) => ({ ...v, check_in: e.target.value }))}
                />
              )}
            </Campo>

            <Campo id="conv-saida" label="Saída" erro={erros.check_out} hint="O dia da saída não conta como diária.">
              {(props) => (
                <Input
                  {...props}
                  type="date"
                  className="tabular-nums"
                  value={valores.check_out}
                  onChange={(e) => setValores((v) => ({ ...v, check_out: e.target.value }))}
                />
              )}
            </Campo>

            <Campo id="conv-valor" label="Valor estimado (R$)" erro={erros.amount}>
              {(props) => (
                <Input
                  {...props}
                  inputMode="decimal"
                  className="tabular-nums"
                  value={valores.amount}
                  onChange={(e) => setValores((v) => ({ ...v, amount: e.target.value }))}
                  placeholder={reaisDeCentavos(0)}
                />
              )}
            </Campo>

            <Campo id="conv-fechamento" label="Fechamento esperado" erro={erros.expected_close}>
              {(props) => (
                <Input
                  {...props}
                  type="date"
                  className="tabular-nums"
                  value={valores.expected_close}
                  onChange={(e) => setValores((v) => ({ ...v, expected_close: e.target.value }))}
                />
              )}
            </Campo>
          </div>

          {falha ? (
            <div
              role="alert"
              data-codigo={falha.code}
              title={`Código para o suporte: ${falha.code}`}
              className="rounded-xl border border-destructive/30 bg-destructive/8 px-3.5 py-3"
            >
              <p className="flex items-start gap-2 text-sm text-destructive">
                <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
                <span className="min-w-0">{descricaoDaFalha(falha)}</span>
              </p>
              {idDaOportunidadeExistente ? (
                <Link
                  href={`/app/oportunidades/${idDaOportunidadeExistente}`}
                  className={cn(buttonVariants({ variant: "outline", size: "sm" }), "mt-3")}
                >
                  Abrir a oportunidade que já existe
                </Link>
              ) : null}
            </div>
          ) : null}
        </form>
      ) : null}
    </ModalShell>
  );
}

function ResumoDoLead({ lead }: { lead: Lead }) {
  return (
    <dl className="grid gap-2 rounded-xl border border-border/60 bg-muted/20 p-3 text-sm sm:grid-cols-3">
      <div>
        <dt className="text-xs text-muted-foreground">Origem</dt>
        <dd>{rotuloDaOrigem(lead.source)}</dd>
      </div>
      <div>
        <dt className="text-xs text-muted-foreground">Interesse</dt>
        <dd className="truncate">{lead.interest_unit_type_name ?? "—"}</dd>
      </div>
      <div>
        <dt className="text-xs text-muted-foreground">Datas pretendidas</dt>
        <dd className="tabular-nums">
          {lead.desired_check_in && lead.desired_check_out
            ? `${formatarData(lead.desired_check_in)} → ${formatarData(lead.desired_check_out)}`
            : "—"}
        </dd>
      </div>
    </dl>
  );
}

function vazio(): ConversaoFormulario {
  return {
    pipeline_id: "",
    stage_id: "",
    unit_type_id: "",
    check_in: "",
    check_out: "",
    amount: "",
    expected_close: "",
  };
}
