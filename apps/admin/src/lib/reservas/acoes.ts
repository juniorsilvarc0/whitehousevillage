"use server";

import { revalidatePath } from "next/cache";

import { apiFetch, apiFetchComMeta } from "@/lib/api/client";
import { tentar } from "@/lib/acoes/executar";
import { exigir } from "@/lib/acoes/guarda";
import { falha, type Resultado } from "@/lib/acoes/resultado";
import type {
  ConfirmacaoDeReserva,
  MetaDaRemarcacao,
  PedidoDeExtensaoDeHold,
  PedidoDeRealocacao,
  PedidoDeRemarcacao,
  RegistroDeEstadia,
  Reserva,
  ResultadoDeCancelamento,
  ResultadoDeExtensaoDeHold,
} from "@/lib/reservas/tipos";

/**
 * As ações do ciclo de vida da reserva.
 *
 * Todas devolvem `Resultado<T>` em vez de lançar: Server Action que lança perde
 * a mensagem em produção e entrega um digest opaco ao cliente — e junto com ela
 * some o `code`, que é a única coisa a que a tela tem o direito de reagir.
 * `DATE_CONFLICT` na remarcação, `HOLD_EXPIRED` na confirmação e
 * `RESERVATION_NOT_CANCELLABLE` no cancelamento não são defeitos: são respostas
 * de negócio que a tela precisa saber ler.
 *
 * `exigir()` recusa cedo o que a API recusaria de qualquer jeito. **Não
 * autoriza nada**: Server Action é endpoint público e quem decide é o
 * middleware da API, a cada requisição.
 */

const LISTA = "/app/reservas";

/** O calendário muda junto: confirmar promove blocos, cancelar os solta, e o
 *  mapa é a tela que a operação deixa aberta o dia inteiro. */
function revalidar(id: string): void {
  revalidatePath(LISTA);
  revalidatePath(`${LISTA}/${id}`);
  revalidatePath("/app/mapa");
}

/**
 * `Idempotency-Key` é obrigatório onde se cria reserva ou se move dinheiro.
 *
 * A chave nasce **no navegador**, quando o diálogo abre, e sobrevive a todas as
 * tentativas daquele diálogo: gerada por tentativa, ela seria um identificador
 * novo a cada clique — o mesmo que não existir —, e o duplo clique em
 * "Confirmar" registraria o sinal duas vezes.
 *
 * O contrato pede 8..255 caracteres; chave curta é recusada aqui em vez de
 * gastar uma ida ao servidor para receber `422`.
 */
function chaveInvalida(chave: string): Resultado<never> | null {
  if (chave.length < 8 || chave.length > 255) {
    return falha("VALIDATION_ERROR", "Não foi possível enviar. Recarregue a página e tente de novo.");
  }
  return null;
}

// ── Confirmar (registra o sinal) ───────────────────────────────────────────

export async function confirmarReserva(
  id: string,
  entrada: ConfirmacaoDeReserva,
  chave: string,
): Promise<Resultado<Reserva>> {
  const recusa = await exigir("reservations", "editar");
  if (recusa) return recusa;
  const ruim = chaveInvalida(chave);
  if (ruim) return ruim;

  const resultado = await tentar(() =>
    apiFetch<Reserva>(`/reservations/${id}/confirm`, {
      method: "POST",
      body: entrada,
      headers: { "Idempotency-Key": chave },
    }),
  );

  if (resultado.ok) revalidar(id);
  return resultado;
}

// ── Cancelar ───────────────────────────────────────────────────────────────

/**
 * A simulação — `?dry_run=1`, que **calcula e não executa nada**.
 *
 * É separada do cancelamento de propósito, e não um parâmetro booleano da mesma
 * função: o gesto que simula é o de abrir o diálogo, e o que executa é o de
 * confirmar. Um `cancelar(id, { dryRun })` faria os dois passarem pelo mesmo
 * `if`, e o dia em que esse `if` invertesse ninguém descobriria com uma
 * simulação — descobriria com uma reserva cancelada.
 *
 * O `reason` entra na simulação porque muda o resultado inteiro: `no_show`
 * aplica a faixa de menor antecedência (retenção integral) e termina a reserva
 * em `no_show`, não em `cancelled`. Medido em 27/08/2026 na `WH-2026-0001`:
 * sem motivo, `refund 352500 / retained 0`; com `no_show`, `refund 0 /
 * retained 352500`. Simular sem o motivo mostraria ao operador um número que
 * não é o que vai acontecer.
 */
export async function simularCancelamento(
  id: string,
  reason: string,
): Promise<Resultado<ResultadoDeCancelamento>> {
  const recusa = await exigir("reservations", "editar");
  if (recusa) return recusa;

  return tentar(() =>
    apiFetch<ResultadoDeCancelamento>(`/reservations/${id}/cancel`, {
      method: "POST",
      query: { dry_run: 1 },
      body: reason ? { reason } : {},
    }),
  );
}

export async function cancelarReserva(
  id: string,
  reason: string,
): Promise<Resultado<ResultadoDeCancelamento>> {
  const recusa = await exigir("reservations", "editar");
  if (recusa) return recusa;

  const resultado = await tentar(() =>
    apiFetch<ResultadoDeCancelamento>(`/reservations/${id}/cancel`, {
      method: "POST",
      body: reason ? { reason } : {},
    }),
  );

  if (resultado.ok) revalidar(id);
  return resultado;
}

// ── Remarcar ───────────────────────────────────────────────────────────────

export type Remarcacao = { reserva: Reserva; meta: MetaDaRemarcacao | null };

/**
 * Remarcar **não edita** a reserva: a original vai para `cancelled` com motivo
 * `remarcacao` e nasce uma nova apontando para ela, na mesma transação. Por isso
 * `DATE_CONFLICT` aqui é seguro — ou as duas mudanças valem, ou nenhuma vale, e
 * a reserva antiga continua exatamente como estava.
 *
 * `meta.difference_cents` é o que falta cobrar (positivo) ou devolver
 * (negativo); `meta.credit_cents` é o sinal que não coube na estadia nova. Os
 * dois vêm no `meta` do envelope, e é por isso que esta é a única ação que usa
 * `apiFetchComMeta`.
 */
export async function remarcarReserva(
  id: string,
  entrada: PedidoDeRemarcacao,
  chave: string,
): Promise<Resultado<Remarcacao>> {
  const recusa = await exigir("reservations", "editar");
  if (recusa) return recusa;
  const ruim = chaveInvalida(chave);
  if (ruim) return ruim;

  const resultado = await tentar(async () => {
    const { data, meta } = await apiFetchComMeta<Reserva, MetaDaRemarcacao>(`/reservations/${id}/reschedule`, {
      method: "POST",
      body: entrada,
      headers: { "Idempotency-Key": chave },
    });
    return { reserva: data, meta: meta ?? null };
  });

  if (resultado.ok) {
    revalidar(id);
    revalidatePath(`${LISTA}/${resultado.data.reserva.id}`);
  }
  return resultado;
}

// ── Check-in e check-out ───────────────────────────────────────────────────

/**
 * O `at` é validado contra o período da reserva, no fuso da casa: no check-in,
 * `[check_in, check_out)`; no check-out, `[check_in, check_out]` e nunca antes
 * do `at` do `checked_in`. Fora disso é `422`, com as datas em `details`.
 *
 * A tela avisa antes de enviar, mas quem recusa é o servidor: o registro é a
 * prova de quando a estadia aconteceu, e prova conferida só no cliente não é
 * prova.
 */
export async function registrarCheckIn(id: string, entrada: RegistroDeEstadia): Promise<Resultado<Reserva>> {
  return registrarEstadia(id, "check-in", entrada);
}

export async function registrarCheckOut(id: string, entrada: RegistroDeEstadia): Promise<Resultado<Reserva>> {
  return registrarEstadia(id, "check-out", entrada);
}

async function registrarEstadia(
  id: string,
  rota: "check-in" | "check-out",
  entrada: RegistroDeEstadia,
): Promise<Resultado<Reserva>> {
  const recusa = await exigir("reservations", "editar");
  if (recusa) return recusa;

  const resultado = await tentar(() =>
    apiFetch<Reserva>(`/reservations/${id}/${rota}`, { method: "POST", body: entrada }),
  );

  if (resultado.ok) revalidar(id);
  return resultado;
}

// ── Realocar unidade ───────────────────────────────────────────────────────

/**
 * Trocar `AP-02` por `AP-01` sem mexer em datas nem em preço é operação
 * legítima — é o que salva um conflito de canal sem cancelar ninguém. Se a
 * unidade de destino estiver ocupada, a constraint recusa e **nada muda**:
 * `409 UNIT_NOT_AVAILABLE`, e a reserva continua na unidade antiga.
 */
export async function realocarUnidade(
  id: string,
  entrada: PedidoDeRealocacao,
): Promise<Resultado<Reserva>> {
  const recusa = await exigir("reservations", "editar");
  if (recusa) return recusa;

  const resultado = await tentar(() =>
    apiFetch<Reserva>(`/reservations/${id}/reassign-unit`, { method: "POST", body: entrada }),
  );

  if (resultado.ok) revalidar(id);
  return resultado;
}

// ── Estender a pré-reserva ─────────────────────────────────────────────────

/**
 * Ação explícita e auditada, nunca automática: cada extensão entra na linha do
 * tempo com autor e prazo novo. Pré-reserva que se renova sozinha é data morta
 * no calendário — e o limite de extensões (`409 HOLD_LIMIT_REACHED`) existe
 * para forçar a decisão humana: confirmar com sinal, ou soltar a data.
 */
export async function estenderHold(
  id: string,
  entrada: PedidoDeExtensaoDeHold,
): Promise<Resultado<ResultadoDeExtensaoDeHold>> {
  const recusa = await exigir("reservations", "editar");
  if (recusa) return recusa;

  const resultado = await tentar(() =>
    apiFetch<ResultadoDeExtensaoDeHold>(`/reservations/${id}/extend-hold`, { method: "POST", body: entrada }),
  );

  if (resultado.ok) revalidar(id);
  return resultado;
}

// ── Descartar o rascunho ───────────────────────────────────────────────────

/**
 * Só apaga o que nunca existiu comercialmente: reserva em `quote`, sem bloco de
 * calendário, sem sinal e fora do razão. A partir de `hold` a API responde
 * `409 INVALID_STATE_TRANSITION` apontando `/cancel` — apagar reserva quebraria
 * o razão e sumiria com a prova de quem segurou a data.
 */
export async function descartarReserva(id: string): Promise<Resultado<null>> {
  const recusa = await exigir("reservations", "excluir");
  if (recusa) return recusa;

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/reservations/${id}`, { method: "DELETE" });
    return null;
  });

  if (resultado.ok) revalidar(id);
  return resultado;
}
