"use client";

import * as React from "react";
import Link from "next/link";
import { CalendarSync, Loader2 } from "lucide-react";

import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Recusa } from "@/components/reservas/recusa";
import { Button, buttonVariants } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import type { Produto } from "@/lib/api/comercial";
import type { Falha, Resultado } from "@/lib/acoes/resultado";
import { novaChaveDeIdempotencia } from "@/lib/crm/idempotencia";
import { formatarData, noitesEntre } from "@/lib/datas";
import { formatarBRL } from "@/lib/dinheiro";
import type { MetaDaRemarcacao, PedidoDeRemarcacao, Reserva } from "@/lib/reservas/tipos";
import { cn } from "@/lib/utils";

/**
 * Remarcar — **não é editar a reserva**.
 *
 * Numa transação, a original vai para `cancelled` com motivo `remarcacao` e
 * nasce uma reserva nova apontando para ela (`rebooked_from_id`). O código
 * antigo continua existindo, o hóspede continua rastreável e o BI não perde a
 * venda original. É por isso que a tela fala em "reserva nova" e entrega o link
 * dela: quem remarcou precisa saber que o número mudou antes de repetir o
 * código velho ao telefone.
 *
 * A nova é **recalculada do zero** com a tabela e a política vigentes hoje —
 * não com as congeladas na original. Remarcar é vender de novo, e o preço de
 * dezembro não acompanha uma estadia que foi empurrada para o réveillon.
 *
 * ## `DATE_CONFLICT` aqui é informação, não falha
 *
 * As duas mudanças acontecem na mesma transação: se a data nova estiver
 * ocupada, **nada muda** e a reserva antiga continua de pé, exatamente como
 * estava. É o desfecho normal de dezembro, e a tela o mostra como aviso — não
 * como erro vermelho, que ensinaria o operador a temer o botão.
 */

export type AlvoDeRemarcacao = Pick<
  Reserva,
  "id" | "code" | "contact_name" | "check_in" | "check_out" | "unit_type_id" | "guests_count" | "discount_pct" | "total_cents" | "status"
>;

type Sucesso = { reserva: Reserva; meta: MetaDaRemarcacao | null };

export function DialogoDeRemarcacao({
  controle,
  produtos,
  aoRemarcar,
  aoConcluir,
}: {
  controle: React.RefObject<ControleDeModal<AlvoDeRemarcacao> | null>;
  /** Permite remarcar trocando de produto. Vazio mantém o produto da original. */
  produtos: Produto[];
  aoRemarcar: (id: string, entrada: PedidoDeRemarcacao, chave: string) => Promise<Resultado<Sucesso>>;
  aoConcluir?: () => void;
}) {
  const [aberto, setAberto] = React.useState(false);
  const [alvo, setAlvo] = React.useState<AlvoDeRemarcacao | null>(null);
  const [entrada, setEntrada] = React.useState("");
  const [saida, setSaida] = React.useState("");
  const [produto, setProduto] = React.useState("");
  const [hospedes, setHospedes] = React.useState("");
  const [motivo, setMotivo] = React.useState("");
  const [enviando, setEnviando] = React.useState(false);
  const [recusa, setRecusa] = React.useState<Falha | null>(null);
  const [sucesso, setSucesso] = React.useState<Sucesso | null>(null);

  const chave = React.useRef("");

  React.useImperativeHandle(controle, () => ({
    abrir(escolhido: AlvoDeRemarcacao) {
      setAlvo(escolhido);
      setEntrada(escolhido.check_in);
      setSaida(escolhido.check_out);
      setProduto(escolhido.unit_type_id);
      setHospedes(String(escolhido.guests_count));
      setMotivo("");
      setRecusa(null);
      setSucesso(null);
      chave.current = novaChaveDeIdempotencia();
      setAberto(true);
    },
  }), []);

  const noites = entrada && saida ? noitesEntre(entrada, saida) : 0;
  const hospedesNumero = Number.parseInt(hospedes, 10);
  const datasInvalidas = !entrada || !saida || noites < 1;
  const hospedesInvalidos = !Number.isFinite(hospedesNumero) || hospedesNumero < 1;
  const mesmasDatas = alvo !== null && entrada === alvo.check_in && saida === alvo.check_out && produto === alvo.unit_type_id;

  async function enviar() {
    if (!alvo || datasInvalidas || hospedesInvalidos) return;
    setEnviando(true);
    setRecusa(null);
    try {
      const resultado = await aoRemarcar(
        alvo.id,
        {
          check_in: entrada,
          check_out: saida,
          ...(produto && produto !== alvo.unit_type_id ? { unit_type_id: produto } : {}),
          ...(hospedesNumero !== alvo.guests_count ? { guests_count: hospedesNumero } : {}),
          ...(motivo.trim() ? { reason: motivo.trim() } : {}),
        },
        chave.current,
      );
      if (!resultado.ok) {
        setRecusa(resultado);
        return;
      }
      setSucesso(resultado.data);
      aoConcluir?.();
    } finally {
      setEnviando(false);
    }
  }

  return (
    <ModalShell
      open={aberto}
      onOpenChange={setAberto}
      title="Remarcar a estadia"
      description={alvo ? `${alvo.code} · ${alvo.contact_name}` : undefined}
      footer={
        sucesso ? (
          <>
            <Button variant="ghost" onClick={() => setAberto(false)}>
              Fechar
            </Button>
            <Link
              href={`/app/reservas/${sucesso.reserva.id}`}
              className={cn(buttonVariants({ size: "md" }))}
              onClick={() => setAberto(false)}
            >
              Abrir {sucesso.reserva.code}
            </Link>
          </>
        ) : (
          <>
            <Button variant="ghost" onClick={() => setAberto(false)} disabled={enviando}>
              Cancelar
            </Button>
            <Button
              onClick={() => void enviar()}
              disabled={enviando || datasInvalidas || hospedesInvalidos || mesmasDatas}
            >
              {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <CalendarSync aria-hidden="true" />}
              Remarcar
            </Button>
          </>
        )
      }
    >
      <div className="flex flex-col gap-4">
        {sucesso ? (
          <ResultadoDaRemarcacao sucesso={sucesso} />
        ) : (
          <>
            {alvo ? (
              <p className="text-sm text-muted-foreground">
                Hoje: {formatarData(alvo.check_in)} → {formatarData(alvo.check_out)} ·{" "}
                <span className="font-mono tabular-nums">{formatarBRL(alvo.total_cents)}</span>
              </p>
            ) : null}

            <div className="grid gap-4 sm:grid-cols-2">
              <Campo id="remarcar-entrada" label="Nova entrada" obrigatorio>
                {(props) => (
                  <Input
                    {...props}
                    type="date"
                    value={entrada}
                    onChange={(evento) => setEntrada(evento.target.value)}
                    className="tabular-nums"
                  />
                )}
              </Campo>
              <Campo
                id="remarcar-saida"
                label="Nova saída"
                obrigatorio
                erro={datasInvalidas && entrada && saida ? "A saída tem que ser depois da entrada." : undefined}
                hint="O dia da saída não conta como diária."
              >
                {(props) => (
                  <Input
                    {...props}
                    type="date"
                    value={saida}
                    onChange={(evento) => setSaida(evento.target.value)}
                    className="tabular-nums"
                  />
                )}
              </Campo>
            </div>

            {noites > 0 ? (
              <p className="text-xs text-muted-foreground">
                <span className="tabular-nums text-foreground">{noites}</span> noite{noites === 1 ? "" : "s"} na
                estadia nova.
              </p>
            ) : null}

            <div className="grid gap-4 sm:grid-cols-2">
              <Campo
                id="remarcar-produto"
                label="Produto"
                hint="Dá para trocar de produto ao remarcar — é o jeito de fazer um upgrade sem cancelar a venda."
              >
                {(props) => (
                  <Select {...props} value={produto} onChange={(evento) => setProduto(evento.target.value)}>
                    {produtos.length === 0 && alvo ? <option value={alvo.unit_type_id}>Manter o atual</option> : null}
                    {produtos.map((item) => (
                      <option key={item.id} value={item.id}>
                        {item.name}
                        {item.capacity ? ` — até ${item.capacity} hóspedes` : ""}
                      </option>
                    ))}
                  </Select>
                )}
              </Campo>
              <Campo
                id="remarcar-hospedes"
                label="Hóspedes"
                erro={hospedesInvalidos ? "Informe ao menos um hóspede." : undefined}
              >
                {(props) => (
                  <Input
                    {...props}
                    value={hospedes}
                    onChange={(evento) => setHospedes(evento.target.value)}
                    inputMode="numeric"
                    className="text-right tabular-nums"
                  />
                )}
              </Campo>
            </div>

            <Campo id="remarcar-motivo" label="Motivo" hint="Fica registrado no histórico das duas reservas.">
              {(props) => (
                <Input
                  {...props}
                  value={motivo}
                  onChange={(evento) => setMotivo(evento.target.value)}
                  placeholder="Hóspede antecipou a viagem"
                />
              )}
            </Campo>

            {mesmasDatas ? (
              <Nota>
                Nada mudou ainda. Remarcar para as mesmas datas e o mesmo produto criaria uma reserva nova
                idêntica e cancelaria a atual — troque a data antes.
              </Nota>
            ) : null}

            <Nota variante="atencao">
              A reserva <strong>{alvo?.code}</strong> será <strong>cancelada com motivo “remarcação”</strong> e
              uma reserva nova é criada no lugar, com código novo. O preço é calculado de novo com os preços e
              as regras <strong>de hoje</strong>, e o sinal já pago passa para a nova.
            </Nota>
          </>
        )}

        {recusa ? (
          <Recusa
            falha={recusa}
            garantia={`Nada foi alterado: ${alvo?.code ?? "a reserva"} continua valendo, com as mesmas datas.`}
          />
        ) : null}
      </div>
    </ModalShell>
  );
}

/**
 * O que a remarcação deixou pendente, em dinheiro.
 *
 * `difference_cents` é o que falta cobrar (positivo) ou devolver (negativo);
 * `credit_cents` é o sinal já pago que não coube na estadia nova. A Fase 1
 * apenas informa: gerar o recebível ou o pagável é do módulo financeiro, e
 * dizer isso na tela evita que alguém espere um boleto que não vem.
 */
function ResultadoDaRemarcacao({ sucesso }: { sucesso: Sucesso }) {
  const meta = sucesso.meta;
  return (
    <div className="flex flex-col gap-3">
      <div className="rounded-xl border border-alcada-livre/35 bg-alcada-livre/10 px-4 py-3">
        <p className="text-sm">
          Reserva nova: <strong className="font-mono">{sucesso.reserva.code}</strong> ·{" "}
          {formatarData(sucesso.reserva.check_in)} → {formatarData(sucesso.reserva.check_out)}
        </p>
        <p className="mt-1 text-sm">
          Total <span className="font-mono tabular-nums">{formatarBRL(sucesso.reserva.total_cents)}</span>
        </p>
      </div>

      {meta ? (
        <dl className="rounded-xl border border-border/60 bg-muted/20 px-4 py-3 text-sm">
          <div className="flex items-baseline justify-between gap-3 py-1">
            <dt className="text-muted-foreground">Reserva anterior ({meta.previous_code})</dt>
            <dd className="font-mono tabular-nums">{formatarBRL(meta.previous_total_cents)}</dd>
          </div>
          <div className="flex items-baseline justify-between gap-3 border-t border-border/60 py-1 pt-2">
            <dt>{meta.difference_cents >= 0 ? "Falta cobrar" : "Há a devolver"}</dt>
            <dd className="font-mono tabular-nums">{formatarBRL(Math.abs(meta.difference_cents))}</dd>
          </div>
          {meta.credit_cents > 0 ? (
            <div className="mt-2 rounded-lg bg-alcada-atencao/12 px-3 py-2 text-xs leading-relaxed">
              <strong className="font-mono tabular-nums">{formatarBRL(meta.credit_cents)}</strong> de sinal já
              pago <strong>não coube</strong> na estadia nova e virou crédito do hóspede, registrado no
              histórico. O crédito acompanha as próximas remarcações e não se perde.
            </div>
          ) : null}
          <p className="mt-2 text-xs text-muted-foreground">
            Por enquanto o sistema só informa esse valor: a cobrança ou a devolução ficará na tela Financeiro,
            que ainda está sendo feita.
          </p>
        </dl>
      ) : null}
    </div>
  );
}
