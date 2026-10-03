"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { ArrowLeftRight, CalendarSync, CheckCircle2, Clock, DoorClosed, DoorOpen, Trash2, XCircle } from "lucide-react";

import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalDeConfirmacao } from "@/components/layout/modal-de-confirmacao";
import { DialogoDeCancelamento, type AlvoDeCancelamento } from "@/components/reservas/dialogo-de-cancelamento";
import { DialogoDeConfirmacao, type AlvoDeConfirmacao } from "@/components/reservas/dialogo-de-confirmacao";
import { DialogoDeEstadia, type AlvoDeEstadia } from "@/components/reservas/dialogo-de-estadia";
import { DialogoDeHold, type AlvoDeExtensao } from "@/components/reservas/dialogo-de-hold";
import { DialogoDeRealocacao, type AlvoDeRealocacao } from "@/components/reservas/dialogo-de-realocacao";
import { DialogoDeRemarcacao, type AlvoDeRemarcacao } from "@/components/reservas/dialogo-de-remarcacao";
import { Button } from "@/components/ui/button";
import type { Produto, UnidadeDaComposicao } from "@/lib/api/comercial";
import {
  cancelarReserva,
  confirmarReserva,
  descartarReserva,
  estenderHold,
  realocarUnidade,
  registrarCheckIn,
  registrarCheckOut,
  remarcarReserva,
  simularCancelamento,
} from "@/lib/reservas/acoes";
import {
  podeCancelar,
  podeConfirmar,
  podeDescartar,
  podeEstenderHold,
  podeFazerCheckIn,
  podeFazerCheckOut,
  podeRealocarUnidade,
  podeRemarcar,
} from "@/lib/reservas/estados";
import type { Reserva, ResultadoDeCancelamento } from "@/lib/reservas/tipos";

/**
 * As ações do ciclo de vida, num lugar só.
 *
 * A lista e o detalhe fazem as mesmas perguntas ao mesmo estado, e responder
 * diferente nos dois seria pior do que não oferecer a ação em um deles: o
 * operador aprende a regra pela tela em que está, e depois erra na outra. Os
 * predicados vivem em `lib/reservas/estados.ts`, puros e testáveis; aqui só se
 * desenha o botão.
 *
 * **Nenhuma ação executa sem um diálogo antes**, e nenhum diálogo se limita a
 * "tem certeza?": cada um diz o que vai acontecer com a data, com o dinheiro e
 * com o estado da reserva. Cancelar vai mais longe e mostra o número — é a
 * única ação que muda o saldo do hóspede sem que ninguém tenha digitado um
 * valor.
 *
 * `compacto` é a linha da lista: só a ação seguinte natural, para vinte linhas
 * não virarem cento e vinte botões. O resto mora no detalhe.
 */
export function AcoesDaReserva({
  reserva,
  permissoes,
  previaDeCancelamento = null,
  produtos = [],
  composicao = [],
  compacto = false,
}: {
  reserva: Reserva;
  permissoes: { editar: boolean; excluir: boolean };
  /** Vem do `/full`. Preenche o diálogo no primeiro quadro; a simulação com
   *  motivo é refeita de qualquer forma. */
  previaDeCancelamento?: ResultadoDeCancelamento | null;
  /** Só o detalhe carrega — sem eles, remarcar e realocar não são oferecidos. */
  produtos?: Produto[];
  composicao?: UnidadeDaComposicao[];
  compacto?: boolean;
}) {
  const router = useRouter();
  const confirmacao = useControleDeModal<AlvoDeConfirmacao>();
  const cancelamento = useControleDeModal<AlvoDeCancelamento>();
  const extensao = useControleDeModal<AlvoDeExtensao>();
  const entrada = useControleDeModal<AlvoDeEstadia>();
  const saida = useControleDeModal<AlvoDeEstadia>();
  const remarcacao = useControleDeModal<AlvoDeRemarcacao>();
  const realocacao = useControleDeModal<AlvoDeRealocacao>();
  const [descarte, setDescarte] = React.useState(false);

  const atualizar = React.useCallback(() => router.refresh(), [router]);

  const tamanho = compacto ? "sm" : "md";
  const podeEditar = permissoes.editar;

  // A ação seguinte natural do estado — a que a linha da lista mostra sozinha.
  const principal = podeEditar
    ? podeConfirmar(reserva)
      ? {
          rotulo: "Confirmar",
          icone: CheckCircle2,
          abrir: () => confirmacao.abrir(reserva),
        }
      : podeFazerCheckIn(reserva)
        ? { rotulo: "Check-in", icone: DoorOpen, abrir: () => entrada.abrir(reserva) }
        : podeFazerCheckOut(reserva)
          ? { rotulo: "Check-out", icone: DoorClosed, abrir: () => saida.abrir(reserva) }
          : null
    : null;

  const secundarias: { rotulo: string; icone: typeof Clock; abrir: () => void; destrutivo?: boolean }[] = [];
  if (podeEditar && !compacto) {
    if (podeEstenderHold(reserva)) {
      secundarias.push({ rotulo: "Estender prazo", icone: Clock, abrir: () => extensao.abrir(reserva) });
    }
    if (podeRemarcar(reserva) && produtos.length > 0) {
      secundarias.push({ rotulo: "Remarcar", icone: CalendarSync, abrir: () => remarcacao.abrir(reserva) });
    }
    if (podeRealocarUnidade(reserva) && composicao.length > 0) {
      secundarias.push({
        rotulo: "Trocar apartamento",
        icone: ArrowLeftRight,
        abrir: () => realocacao.abrir({ ...reserva, unidades: reserva.units }),
      });
    }
  }
  if (podeEditar && podeCancelar(reserva) && !podeDescartar(reserva)) {
    secundarias.push({
      rotulo: "Cancelar",
      icone: XCircle,
      destrutivo: true,
      abrir: () =>
        cancelamento.abrir({
          id: reserva.id,
          code: reserva.code,
          contato: reserva.contact_name,
          check_in: reserva.check_in,
          check_out: reserva.check_out,
          previa: previaDeCancelamento,
        }),
    });
  }
  if (permissoes.excluir && podeDescartar(reserva)) {
    secundarias.push({
      rotulo: "Descartar",
      icone: Trash2,
      destrutivo: true,
      abrir: () => setDescarte(true),
    });
  }

  // Na linha da lista sobra só a ação principal; sem ela, o cancelamento é a
  // única coisa que ainda faz sentido oferecer ali.
  const visiveis = compacto ? secundarias.filter((a) => a.destrutivo).slice(0, 1) : secundarias;

  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        {principal ? (
          <Button size={tamanho} onClick={principal.abrir}>
            <principal.icone aria-hidden="true" />
            {principal.rotulo}
          </Button>
        ) : null}
        {visiveis.map((acao) => (
          <Button
            key={acao.rotulo}
            size={tamanho}
            // Ação destrutiva em `outline` com texto vermelho, nunca em botão
            // cheio: o botão sólido vermelho ao lado do primário faz a mão errar
            // — e cancelar reserva não é o gesto que a tela deve facilitar.
            variant="outline"
            onClick={acao.abrir}
            className={acao.destrutivo ? "text-destructive hover:bg-destructive/10" : undefined}
          >
            <acao.icone aria-hidden="true" />
            {acao.rotulo}
          </Button>
        ))}
      </div>

      {/* Cada diálogo é montado só onde a ação dele cabe. Numa lista de vinte
          linhas, montar os sete em todas seriam cento e quarenta diálogos vivos
          para no máximo um abrir. O critério é o **estado da reserva**, não o
          "está aberto": assim a montagem é estável enquanto a linha existir, e a
          animação de entrada do Base UI continua acontecendo. */}
      {podeEditar && podeConfirmar(reserva) ? (
        <DialogoDeConfirmacao controle={confirmacao.ref} aoConfirmar={confirmarReserva} aoConcluir={atualizar} />
      ) : null}
      {podeEditar && podeCancelar(reserva) ? (
        <DialogoDeCancelamento
          controle={cancelamento.ref}
          aoSimular={simularCancelamento}
          aoCancelar={cancelarReserva}
          aoConcluir={atualizar}
        />
      ) : null}
      {podeEditar && !compacto && podeEstenderHold(reserva) ? (
        <DialogoDeHold controle={extensao.ref} aoEstender={estenderHold} aoConcluir={atualizar} />
      ) : null}
      {podeEditar && podeFazerCheckIn(reserva) ? (
        <DialogoDeEstadia tipo="entrada" controle={entrada.ref} aoRegistrar={registrarCheckIn} aoConcluir={atualizar} />
      ) : null}
      {podeEditar && podeFazerCheckOut(reserva) ? (
        <DialogoDeEstadia tipo="saida" controle={saida.ref} aoRegistrar={registrarCheckOut} aoConcluir={atualizar} />
      ) : null}
      {podeEditar && !compacto && podeRemarcar(reserva) && produtos.length > 0 ? (
        <DialogoDeRemarcacao
          controle={remarcacao.ref}
          produtos={produtos}
          aoRemarcar={remarcarReserva}
          aoConcluir={atualizar}
        />
      ) : null}
      {podeEditar && !compacto && podeRealocarUnidade(reserva) && composicao.length > 0 ? (
        <DialogoDeRealocacao
          controle={realocacao.ref}
          composicao={composicao}
          aoRealocar={realocarUnidade}
          aoConcluir={atualizar}
        />
      ) : null}

      {permissoes.excluir && podeDescartar(reserva) ? (
      <ModalDeConfirmacao
        aberto={descarte}
        aoMudar={setDescarte}
        titulo="Descartar o rascunho"
        destrutivo
        rotuloConfirmar="Descartar"
        descricao={
          <>
            <strong>{reserva.code}</strong> está em rascunho: não guarda nenhuma data, não tem sinal e não
            entrou nas contas. Descartar apaga o rascunho de vez. Depois que vira pré-reserva isso não é mais
            possível — aí o caminho é cancelar, que segue a política e mantém o histórico.
          </>
        }
        aoConfirmar={async () => {
          const resultado = await descartarReserva(reserva.id);
          if (resultado.ok) atualizar();
          return resultado;
        }}
      />
      ) : null}
    </>
  );
}
