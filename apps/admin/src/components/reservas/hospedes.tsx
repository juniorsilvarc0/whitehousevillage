"use client";

import * as React from "react";
import Link from "next/link";
import { UserMinus, UserPlus, Users } from "lucide-react";

import { TelefoneClicavel } from "@/components/contatos/botao-whatsapp";
import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { EstadoVazio } from "@/components/layout/estados";
import { ModalDeConfirmacao } from "@/components/layout/modal-de-confirmacao";
import { DialogoDeHospede } from "@/components/reservas/dialogo-de-hospede";
import { LinkDoWhatsApp } from "@/components/reservas/link-do-whatsapp";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { falha } from "@/lib/acoes/resultado";
import { formatarDocumento } from "@/lib/contatos/documento";
import { ROTULO_DO_DOCUMENTO } from "@/lib/contatos/tipos";
import { mensagemDaReserva } from "@/lib/contatos/whatsapp";
import { aumentarHospedes, retirarHospede } from "@/lib/reservas/acoes-de-hospedes";
import { ESTADOS } from "@/lib/reservas/estados";
import {
  mensagemDaLista,
  numeroParaMaisUm,
  podeAbrirLugar,
  podeAcrescentar,
  podeRetirar,
} from "@/lib/reservas/hospedes";
import type { HospedeDaReserva, Reserva } from "@/lib/reservas/tipos";

/**
 * A seção "Hóspedes" da reserva — a rooming list (F2-22, spec §6).
 *
 * Quem dorme na casa: o titular (o `contact_id` da reserva, sempre primeiro) e
 * os acompanhantes. É a tela de trabalho da recepção no check-in — muitas vezes
 * pelo celular —, então a lista mostra o que se confere na porta: nome,
 * documento **cheio** e telefone. Os dados vêm do `/full`, que grava uma linha
 * de `pii_access_log` por hóspede a cada leitura; por isso cada gesto aqui
 * recarrega a tela **uma** vez, e só quando muda alguma coisa
 * (`lib/reservas/acoes-de-hospedes.ts`).
 *
 * O que decide os botões:
 *
 * - **se a lista se edita agora** é `guests_editable`, do servidor — a tela não
 *   tem uma segunda cópia dos estados;
 * - **quem pode** é `reservations:editar` (esconde; quem barra é a API);
 * - **"Acrescentar"** só com lugar sobrando (menos nomes que o vendido);
 *   **"Retirar"** só nos acompanhantes — o titular se troca editando a reserva.
 *
 * Lista completa e mais gente chegando: o caminho do contrato é aumentar antes
 * o número de hóspedes da reserva (`PATCH guests_count`, conferido contra a
 * capacidade do produto), e é o que a seção oferece.
 */

export type ReservaDaLista = Pick<Reserva, "id" | "code" | "status" | "guests_count">;

export type PermissoesDaLista = {
  /** `reservations:editar`. */
  editar: boolean;
  /** `contacts:ver` — a busca pelo documento. */
  procurarPessoas: boolean;
  /** `contacts:criar` — "Cadastrar pessoa". */
  cadastrarPessoas: boolean;
};

export function SecaoDeHospedes({
  reserva,
  hospedes,
  editavel,
  permissoes,
  capacidade,
}: {
  reserva: ReservaDaLista;
  hospedes: HospedeDaReserva[];
  /** `guests_editable` do `/full`. */
  editavel: boolean;
  permissoes: PermissoesDaLista;
  /** Capacidade declarada do produto, quando o catálogo carregou; `null` é "não
   *  sei", e aí quem diz se cabe é o servidor. */
  capacidade: number | null;
}) {
  const poderes = { editavel, podeEditar: permissoes.editar };
  const listados = hospedes.length;
  const vendidos = reserva.guests_count;

  const acrescimo = useControleDeModal<null>();
  const [aviso, setAviso] = React.useState<string | null>(null);
  const [retirada, setRetirada] = React.useState<{ aberto: boolean; alvo: HospedeDaReserva | null }>({
    aberto: false,
    alvo: null,
  });
  const [aumento, setAumento] = React.useState(false);

  const mostrarAcrescentar = podeAcrescentar(poderes, listados, vendidos);
  const mostrarAbrirLugar = podeAbrirLugar(poderes, listados, vendidos, capacidade);
  const novoNumero = numeroParaMaisUm(listados);

  function abrirAcrescimo() {
    setAviso(null);
    acrescimo.abrir(null);
  }

  function pedirRetirada(hospede: HospedeDaReserva) {
    setAviso(null);
    setRetirada({ aberto: true, alvo: hospede });
  }

  return (
    <section aria-label="Hóspedes" className="rounded-xl border border-border/60 bg-muted/20 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="font-display text-base">Hóspedes</h2>
          <p className="mt-0.5 text-xs text-muted-foreground" data-contador>
            <span className="tabular-nums">{listados}</span> de <span className="tabular-nums">{vendidos}</span> com
            nome na lista
          </p>
        </div>
        {mostrarAcrescentar ? (
          <Button size="md" onClick={abrirAcrescimo} className="max-sm:w-full">
            <UserPlus aria-hidden="true" />
            Acrescentar hóspede
          </Button>
        ) : null}
      </div>

      <Explicacao
        listados={listados}
        vendidos={vendidos}
        capacidade={capacidade}
        editavel={editavel}
        podeEditar={permissoes.editar}
        estado={reserva.status}
      />

      {aviso ? (
        <p role="status" className="mt-3 rounded-lg bg-accent/60 px-3 py-2 text-sm text-accent-foreground">
          {aviso}
        </p>
      ) : (
        // O `role=status` precisa existir antes da mensagem para o leitor de
        // tela anunciá-la quando ela chega.
        <p role="status" className="sr-only" />
      )}

      {mostrarAbrirLugar ? (
        <div className="mt-3">
          <Button size="sm" variant="outline" className="max-sm:h-10 max-sm:w-full max-sm:text-sm" onClick={() => setAumento(true)}>
            <Users aria-hidden="true" />
            Aumentar para {novoNumero} hóspedes
          </Button>
        </div>
      ) : null}

      {listados === 0 ? (
        <EstadoVazio
          className="mt-3"
          icone={Users}
          titulo="Nenhum nome informado"
          descricao="A lista de hóspedes é o que a portaria usa no dia da chegada para conferir quem entra."
        />
      ) : (
        <ul aria-label="Lista de hóspedes" className="mt-3 flex flex-col gap-2">
          {hospedes.map((hospede) => (
            <LinhaDoHospede
              key={hospede.contact_id}
              hospede={hospede}
              codigoDaReserva={reserva.code}
              retiravel={podeRetirar(hospede, poderes)}
              aoRetirar={() => pedirRetirada(hospede)}
            />
          ))}
        </ul>
      )}

      {/* Montados pela permissão, que não muda enquanto a tela vive — e não pelo
          estado da lista: a recarga que vem depois de uma recusa pode esconder o
          botão de acrescentar, e o diálogo aberto não pode sumir com a recusa
          que o operador está lendo. */}
      {permissoes.editar ? (
        <>
          <DialogoDeHospede
            controle={acrescimo.ref}
            reserva={reserva}
            hospedes={hospedes}
            podeProcurar={permissoes.procurarPessoas}
            podeCadastrar={permissoes.cadastrarPessoas}
            aoConcluir={setAviso}
          />

          <ModalDeConfirmacao
            aberto={retirada.aberto}
            aoMudar={(aberto) => setRetirada((atual) => ({ ...atual, aberto }))}
            titulo="Retirar da lista"
            destrutivo
            rotuloConfirmar="Retirar"
            descricao={
              <>
                <strong>{retirada.alvo?.name}</strong> sai da lista de hóspedes de{" "}
                <strong>{reserva.code}</strong>. A ficha da pessoa continua no cadastro, e a retirada fica no
                histórico da reserva. Se foi engano, é só acrescentar de novo.
              </>
            }
            aoConfirmar={async () => {
              const alvo = retirada.alvo;
              if (!alvo) return falha("VALIDATION_ERROR", "");
              let resultado: Awaited<ReturnType<typeof retirarHospede>>;
              try {
                resultado = await retirarHospede(reserva.id, alvo.contact_id);
              } catch {
                resultado = falha("NETWORK_ERROR", "");
              }
              if (resultado.ok) {
                setAviso(
                  resultado.data.jaSaiu ? `${alvo.name} já tinha saído da lista.` : `${alvo.name} saiu da lista.`,
                );
              }
              return resultado;
            }}
            mensagemDaFalha={(recusa) => mensagemDaLista(recusa, "retirar", retirada.alvo?.name)}
          />

          <ModalDeConfirmacao
            aberto={aumento}
            aoMudar={setAumento}
            titulo="Aumentar o número de hóspedes"
            rotuloConfirmar={`Aumentar para ${novoNumero}`}
            descricao={
              <>
                <strong>{reserva.code}</strong> passa de {vendidos} para <strong>{novoNumero}</strong> hóspedes, e
                a lista ganha lugar para mais um nome. O valor da reserva não muda: muda quantas pessoas ela
                acomoda
                {capacidade !== null ? ` — e o produto comporta até ${capacidade}` : ""}.
              </>
            }
            aoConfirmar={async () => {
              let resultado: Awaited<ReturnType<typeof aumentarHospedes>>;
              try {
                resultado = await aumentarHospedes(reserva.id, novoNumero);
              } catch {
                resultado = { ...falha("NETWORK_ERROR", ""), etapa: "numero" };
              }
              if (resultado.ok) {
                setAviso(`A reserva agora é para ${resultado.data.guests_count} hóspedes. Já dá para acrescentar o nome.`);
              }
              return resultado;
            }}
            mensagemDaFalha={(recusa) => mensagemDaLista(recusa, "aumentar")}
          />
        </>
      ) : null}
    </section>
  );
}

/** A frase sob o contador — o que a lista quer dizer agora, e o que fazer. */
function Explicacao({
  listados,
  vendidos,
  capacidade,
  editavel,
  podeEditar,
  estado,
}: {
  listados: number;
  vendidos: number;
  capacidade: number | null;
  editavel: boolean;
  podeEditar: boolean;
  estado: Reserva["status"];
}) {
  if (!editavel) {
    return (
      <p className="mt-1 text-xs text-muted-foreground">
        Com a reserva como “{ESTADOS[estado]?.rotulo ?? estado}”, a lista de hóspedes não muda mais: ela é o registro
        de quem foi vendido e de quem se hospedou.
      </p>
    );
  }

  if (listados < vendidos) {
    const faltam = vendidos - listados;
    return (
      <p className="mt-1 text-xs text-muted-foreground">
        {faltam === 1 ? "Falta 1 nome" : `Faltam ${faltam} nomes`}. É normal: a venda é para {vendidos}, e nem todo
        mundo enviou os dados ainda — a recepção completa a lista no check-in.
      </p>
    );
  }

  if (!podeEditar) {
    return <p className="mt-1 text-xs text-muted-foreground">A lista está completa.</p>;
  }

  const noMaximo = capacidade !== null && vendidos >= capacidade;
  return (
    <p className="mt-1 text-xs text-muted-foreground">
      A lista está completa.{" "}
      {noMaximo
        ? `A reserva já está no máximo que o produto comporta (${capacidade}): para acrescentar alguém, retire antes um nome.`
        : "Chegou mais gente? Aumente antes o número de hóspedes da reserva — depois, acrescente o nome."}
    </p>
  );
}

function LinhaDoHospede({
  hospede,
  codigoDaReserva,
  retiravel,
  aoRetirar,
}: {
  hospede: HospedeDaReserva;
  codigoDaReserva: string;
  retiravel: boolean;
  aoRetirar: () => void;
}) {
  const documento = hospede.doc_number
    ? `${hospede.doc_type ? `${ROTULO_DO_DOCUMENTO[hospede.doc_type]} ` : ""}${formatarDocumento(
        hospede.doc_type ?? null,
        hospede.doc_number,
      )}`
    : null;

  return (
    <li
      data-contato={hospede.contact_id}
      className="flex flex-col gap-2 rounded-lg border border-border/60 bg-card px-3 py-2.5 text-sm sm:flex-row sm:items-center sm:justify-between"
    >
      <div className="min-w-0">
        <p className="flex flex-wrap items-center gap-2">
          <Link href={`/app/contatos/${hospede.contact_id}`} className="font-medium hover:underline">
            {hospede.name}
          </Link>
          {hospede.is_lead_guest ? <Badge variant="outline">Titular</Badge> : null}
        </p>
        <p className="mt-0.5 font-mono text-xs text-muted-foreground">
          {documento ?? <span className="font-sans">Sem documento na ficha</span>}
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        {hospede.phone_e164 ? (
          <span className="flex items-center gap-3">
            <TelefoneClicavel telefone={hospede.phone_e164} className="text-xs text-muted-foreground" />
            <LinkDoWhatsApp
              telefone={hospede.phone_e164}
              texto={mensagemDaReserva(hospede.name, codigoDaReserva)}
              compacto
            />
          </span>
        ) : null}
        {retiravel ? (
          <Button
            size="sm"
            variant="outline"
            onClick={aoRetirar}
            aria-label={`Retirar ${hospede.name} da lista`}
            className="text-destructive hover:bg-destructive/10 max-sm:h-10 max-sm:px-4 max-sm:text-sm"
          >
            <UserMinus aria-hidden="true" />
            Retirar
          </Button>
        ) : null}
      </div>
    </li>
  );
}
