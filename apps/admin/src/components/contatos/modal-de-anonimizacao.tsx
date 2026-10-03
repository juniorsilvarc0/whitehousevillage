"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2, ShieldOff } from "lucide-react";

import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { aplicarFalhaDeContato } from "@/lib/contatos/formulario";
import { AnonimizacaoFormulario, PALAVRA_DE_CONFIRMACAO } from "@/lib/contatos/esquemas";
import {
  ROTULO_DO_VINCULO,
  totalDeVinculos,
  type Contato,
  type VinculosDoContato,
} from "@/lib/contatos/tipos";

import { anonimizarContato } from "@/app/(app)/app/contatos/acoes";
import { notificar, notificarSucesso } from "@/components/contatos/avisos";

const ID_DO_FORM = "form-anonimizacao";

/**
 * A anonimização — direito de eliminação (LGPD art. 18, VI) do único jeito que
 * sobrevive a uma auditoria fiscal: **a pessoa some, a venda fica**.
 *
 * O que esta tela precisa deixar claro, porque é a pergunta que todo mundo faz
 * antes de clicar: *o que acontece com as reservas?* Nada. Elas continuam
 * existindo e continuam apontando para o mesmo `contact_id` — a FK nunca é
 * rompida, e o fechamento do mês bate exatamente como batia antes. Se não
 * batesse, a anonimização estaria destruindo escrituração, que é obrigação legal
 * concorrente e vence o pedido de eliminação sobre esses registros específicos.
 *
 * A confirmação digitada existe porque não há desfazer: o dado não fica
 * guardado em lugar nenhum para voltar. É o mesmo peso de gesto que a operação
 * dá a apagar um bloco de calendário — só que este não tem volta.
 */
export function ModalDeAnonimizacao({
  controle,
  contato,
  vinculos,
}: {
  controle: React.RefObject<ControleDeModal<null> | null>;
  contato: Contato;
  vinculos?: VinculosDoContato;
}) {
  const router = useRouter();
  const [aberto, setAberto] = React.useState(false);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);

  const form = useForm<AnonimizacaoFormulario>({
    resolver: zodResolver(AnonimizacaoFormulario),
    defaultValues: { reason: "", confirmacao: "" },
  });

  React.useImperativeHandle(
    controle,
    () => ({
      abrir() {
        form.reset({ reason: "", confirmacao: "" });
        setErroGeral(null);
        setAberto(true);
      },
    }),
    [form],
  );

  const { errors, isSubmitting } = form.formState;
  const confirmacao = useWatch({ control: form.control, name: "confirmacao" });
  const confirmado = (confirmacao ?? "").trim().toUpperCase() === PALAVRA_DE_CONFIRMACAO;

  const preservados = vinculos
    ? (Object.keys(ROTULO_DO_VINCULO) as (keyof VinculosDoContato)[])
        .filter((chave) => (vinculos[chave] ?? 0) > 0)
        .map((chave) => `${vinculos[chave]} ${ROTULO_DO_VINCULO[chave]}`)
    : [];

  async function enviar(valores: AnonimizacaoFormulario) {
    setErroGeral(null);
    const resultado = await anonimizarContato(contato.id, valores);
    if (!resultado.ok) {
      notificar(resultado);
      setErroGeral(aplicarFalhaDeContato(resultado, form));
      return;
    }
    const sobreviveram = totalDeVinculos(resultado.data.preserved);
    notificarSucesso(
      "Ficha anonimizada",
      sobreviveram > 0
        ? `${sobreviveram} ${sobreviveram === 1 ? "registro continua" : "registros continuam"} no histórico — nada financeiro foi apagado.`
        : "Nenhum registro estava vinculado.",
    );
    setAberto(false);
    router.refresh();
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title={`Anonimizar ${contato.name}`}
      description="Não tem volta: os dados pessoais são apagados e não ficam guardados em lugar nenhum."
      footer={
        <>
          <Button variant="ghost" onClick={() => setAberto(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button
            type="submit"
            form={ID_DO_FORM}
            variant="destructive"
            disabled={isSubmitting || !confirmado}
          >
            {isSubmitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : <ShieldOff aria-hidden="true" />}
            Anonimizar
          </Button>
        </>
      }
    >
      {erroGeral ? (
        <p role="alert" className="mb-4 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erroGeral}
        </p>
      ) : null}

      <div className="flex flex-col gap-4">
        <div className="rounded-xl border border-border/60 bg-muted/25 p-4 text-sm">
          <p className="font-medium text-foreground">O que é eliminado</p>
          <p className="mt-1 text-muted-foreground">
            Nome, e-mail, telefone, documento, nascimento, cidade e observações. O nome vira
            “Contato anonimizado”, o aceite de marketing cai e a data de consentimento é limpa.
          </p>

          <p className="mt-3 font-medium text-foreground">O que continua existindo</p>
          <p className="mt-1 text-muted-foreground">
            Reservas, noites, valores e pagamentos — tudo continua ligado a esta
            <strong> mesma ficha</strong>.{" "}
            {preservados.length > 0 ? (
              <>
                Hoje são <strong>{preservados.join(", ")}</strong>.
              </>
            ) : null}{" "}
            Apagar os dados pessoais não apaga o histórico de vendas.
          </p>
        </div>

        <Nota variante="atencao">
          Se houver reserva <strong>em andamento</strong> (pré-reserva, confirmada ou hospedado), não
          dá para anonimizar: a equipe precisa do nome para entregar a chave. Será possível quando a
          estadia terminar.
        </Nota>

        <form id={ID_DO_FORM} onSubmit={form.handleSubmit(enviar)} className="flex flex-col gap-4">
          <Campo
            id="anon-reason"
            label="Motivo"
            obrigatorio
            erro={errors.reason?.message}
            hint="Fica registrado no histórico. Responde “por que esta ficha está vazia?” no futuro e comprova o pedido da pessoa, caso a fiscalização da LGPD pergunte."
          >
            {(p) => (
              <Textarea
                {...p}
                {...form.register("reason")}
                rows={3}
                placeholder="Pedido de eliminação recebido por e-mail em 27/08/2026."
              />
            )}
          </Campo>

          <Campo
            id="anon-confirmacao"
            label={`Digite ${PALAVRA_DE_CONFIRMACAO} para confirmar`}
            obrigatorio
            erro={errors.confirmacao?.message}
          >
            {(p) => (
              <Input {...p} {...form.register("confirmacao")} autoComplete="off" className="font-mono uppercase" />
            )}
          </Campo>
        </form>
      </div>
    </ModalShell>
  );
}
