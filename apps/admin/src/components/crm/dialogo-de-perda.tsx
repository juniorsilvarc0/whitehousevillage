"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2, TriangleAlert, XCircle } from "lucide-react";

import { descricaoDaFalha, notificarSucesso } from "@/components/crm/avisos";
import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { perderOportunidade } from "@/lib/crm/acoes";
import { aplicarFalhaCrm } from "@/lib/crm/formulario";
import { PerdaFormulario } from "@/lib/crm/esquemas";
import type { MotivoDePerda } from "@/lib/crm/tipos";

/**
 * "Perder" — com **motivo obrigatório**.
 *
 * Não é burocracia: "motivos de perda" é um dos indicadores do relatório, e é o
 * único lugar em que a casa descobre que perdeu doze negócios por estadia
 * mínima e não por preço. Um campo de texto livre opcional viraria campo vazio
 * em 90% dos cards, e relatório nenhum. A observação existe para o detalhe, ao
 * lado do motivo — nunca no lugar dele.
 *
 * O catálogo vem do servidor já filtrado por `active`: motivo desativado
 * continua existindo nos cards antigos (senão o relatório do ano passado
 * mudaria sozinho), mas não é oferecido para uma perda nova.
 */

export type AlvoDePerda = {
  id: string;
  contato: string;
  /** Código da pré-reserva, quando há uma. Perder **não** a cancela. */
  reservation_code: string | null;
};

export function DialogoDePerda({
  controle,
  motivos,
  aoPerder,
}: {
  controle: React.RefObject<ControleDeModal<AlvoDePerda> | null>;
  motivos: MotivoDePerda[];
  aoPerder?: () => void;
}) {
  const router = useRouter();
  const [aberto, setAberto] = React.useState(false);
  const [alvo, setAlvo] = React.useState<AlvoDePerda | null>(null);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);

  const form = useForm<PerdaFormulario>({
    resolver: zodResolver(PerdaFormulario),
    defaultValues: { lost_reason_id: "", note: "" },
  });

  React.useImperativeHandle(
    controle,
    () => ({
      abrir(escolhido: AlvoDePerda) {
        setAlvo(escolhido);
        setErroGeral(null);
        form.reset({ lost_reason_id: "", note: "" });
        setAberto(true);
      },
    }),
    [form],
  );

  async function enviar(valores: PerdaFormulario) {
    if (!alvo) return;
    setErroGeral(null);
    const resultado = await perderOportunidade(alvo.id, valores);
    if (!resultado.ok) {
      setErroGeral(aplicarFalhaCrm(resultado, form, { LOSS_REASON_REQUIRED: "lost_reason_id" }));
      return;
    }
    const reserva = resultado.data.reservation;
    notificarSucesso(
      "Oportunidade marcada como perdida",
      reserva
        ? `A pré-reserva ${reserva.code} continua valendo: para liberar as datas, cancele-a na tela da reserva.`
        : undefined,
    );
    setAberto(false);
    aoPerder?.();
    router.refresh();
  }

  const { errors, isSubmitting } = form.formState;
  const ativos = motivos.filter((motivo) => motivo.active);

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title="Marcar como perdida"
      description={alvo ? alvo.contato : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button
            type="submit"
            form="form-perder"
            variant="destructive"
            disabled={isSubmitting || ativos.length === 0}
          >
            {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : <XCircle aria-hidden="true" />}
            Marcar como perdida
          </Button>
        </>
      }
    >
      <form id="form-perder" onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
        {erroGeral ? (
          <p role="alert" className="flex items-start gap-2 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
            <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
            <span className="min-w-0">{erroGeral}</span>
          </p>
        ) : null}

        {ativos.length === 0 ? (
          <Nota variante="atencao">
            Não há motivo de perda cadastrado, e sem motivo não é possível marcar como perdido. Peça à
            gestão para cadastrar os motivos em Configurações.
          </Nota>
        ) : null}

        <Campo
          id="perda-motivo"
          label="Motivo"
          obrigatorio
          erro={errors.lost_reason_id?.message}
          hint="É com este campo que a casa entende por que perde negócios."
        >
          {(props) => (
            <Select {...props} {...form.register("lost_reason_id")} disabled={ativos.length === 0}>
              <option value="">Escolha o motivo…</option>
              {ativos.map((motivo) => (
                <option key={motivo.id} value={motivo.id}>
                  {motivo.label}
                </option>
              ))}
            </Select>
          )}
        </Campo>

        <Campo
          id="perda-note"
          label="Observação"
          erro={errors.note?.message}
          hint="Detalhes que completam o motivo escolhido."
        >
          {(props) => <Textarea {...props} {...form.register("note")} placeholder="Fechou com a pousada vizinha por R$ 400 a menos." />}
        </Campo>

        {alvo?.reservation_code ? (
          <Nota>
            A pré-reserva <strong>{alvo.reservation_code}</strong> <strong>não</strong> é cancelada aqui.
            Perder o negócio e liberar as datas são duas decisões separadas — liberar as datas segue a
            política de cancelamento, pode envolver reembolso e é feito na tela da reserva.
          </Nota>
        ) : null}
      </form>
    </ModalShell>
  );
}
