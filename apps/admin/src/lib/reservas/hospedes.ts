import type { CodigoDeErro } from "@/lib/api/codigos";
import { mensagemDoErro, type Falha } from "@/lib/acoes/resultado";
import { ehEstadoDeReserva, ESTADOS } from "@/lib/reservas/estados";
import type { HospedeDaReserva, ListaCheia } from "@/lib/reservas/tipos";

/**
 * A rooming list (F2-22) do lado do painel — o que a seção "Hóspedes" decide
 * sem perguntar ao servidor, e o que ela diz quando o servidor recusa.
 *
 * Vive num módulo puro (sem React, sem `next/headers`) pelo mesmo motivo de
 * `lib/reservas/estados.ts`: a decisão de mostrar um botão é testável em
 * milissegundos aqui e cara de testar dentro de um componente.
 *
 * **Nada aqui autoriza nem decide regra de domínio.** Se a lista se edita no
 * estado em que a reserva está é `guests_editable`, que vem do `/full`
 * (`guests.Editable` no Go) — o painel não carrega uma segunda cópia dos
 * estados. Quem recusa de verdade é a API, por código: `INVALID_STATE_TRANSITION`,
 * `GUEST_LIST_FULL`, `CODE_IN_USE`, `RESOURCE_IN_USE`, `CONTACT_ANONYMIZED`.
 * Os portões abaixo só deixam de oferecer o botão que já se sabe que vai falhar.
 */

/** Quem está vendo a lista — e se pode mexer nela. */
export type PoderesSobreALista = {
  /** `guests_editable` do `/full`. */
  editavel: boolean;
  /** `reservations:editar` — esconde; quem barra é o servidor. */
  podeEditar: boolean;
};

/**
 * "Acrescentar hóspede" aparece só com a lista editável agora, o perfil com
 * `reservations:editar` **e** lugar sobrando: menos nomes que o número vendido.
 * Com a lista completa o caminho é outro — aumentar antes o número de hóspedes
 * —, e oferecer o botão seria oferecer o `422 GUEST_LIST_FULL`.
 */
export function podeAcrescentar(poderes: PoderesSobreALista, listados: number, vendidos: number): boolean {
  return poderes.editavel && poderes.podeEditar && listados < vendidos;
}

/**
 * **O titular tem uma fonte só: `reservations.contact_id`** (spec §6). É a
 * mesma pergunta que o domínio faz (`id == r.Lead` em `guests.CheckRemove` e
 * `CheckAdd`), e é a única que a tela faz.
 *
 * Nem `is_lead_guest` nem a posição na lista decidem. O banco garante que a
 * linha marcada, quando existe, é a do contato da reserva — mas "ao menos um
 * titular" é regra da aplicação, e uma correção manual por SQL deixa a lista
 * sem a linha do titular, ou com ele desmarcado. Escolher pelo marcador (ou
 * pelo primeiro da lista) promoveu um acompanhante a cliente: foi o defeito que
 * abriu o F2-22, e o do cartão "Cliente" que o QA achou depois dele.
 */
export function ehTitular(hospede: Pick<HospedeDaReserva, "contact_id">, contatoDaReserva: string): boolean {
  return hospede.contact_id === contatoDaReserva;
}

/**
 * O titular, se ele está na lista — **nunca** outro no lugar dele. `null` quer
 * dizer "a lista não tem a linha do titular"; quem desenha cai no contato da
 * reserva (`contact_name`/`contact_id`), não no primeiro acompanhante.
 */
export function titularNaLista(hospedes: readonly HospedeDaReserva[], contatoDaReserva: string): HospedeDaReserva | null {
  return hospedes.find((h) => ehTitular(h, contatoDaReserva)) ?? null;
}

/**
 * "Retirar" só nos acompanhantes. O titular é a projeção de
 * `reservations.contact_id`: retirá-lo é `409 RESOURCE_IN_USE`, e troca-se o
 * titular editando a reserva, que traz a lista junto. `titular` vem de
 * `ehTitular` — o mesmo critério do servidor, para a tela não oferecer o botão
 * que ele recusa.
 */
export function podeRetirar(titular: boolean, poderes: PoderesSobreALista): boolean {
  return poderes.editavel && poderes.podeEditar && !titular;
}

/**
 * A lista está completa e a tela pode oferecer abrir lugar (aumentar o número
 * de hóspedes). `capacidade` `null` é "não sei" — o catálogo de produtos não
 * carregou para este perfil —, e aí o botão aparece e quem responde é o
 * servidor (`422 CAPACITY_EXCEEDED`). Com a capacidade na mão, não se oferece
 * o que ela já diz que não cabe.
 */
export function podeAbrirLugar(
  poderes: PoderesSobreALista,
  listados: number,
  vendidos: number,
  capacidade: number | null,
): boolean {
  if (!poderes.editavel || !poderes.podeEditar) return false;
  if (listados < vendidos) return false;
  return capacidade === null || vendidos < capacidade;
}

/**
 * O número a pedir para caber **mais uma** pessoa: os nomes que a lista já tem,
 * mais um. Não é regra — é contar quem vai estar na lista. Usar `listados` e
 * não `vendidos` cobre a lista anterior ao F2-22, que pode estar acima do
 * número: pedir `vendidos + 1` ali não abriria lugar nenhum.
 */
export function numeroParaMaisUm(listados: number): number {
  return listados + 1;
}

/** `details` do `GUEST_LIST_FULL`, lido sem confiar nele: falta um número, não
 *  há o que oferecer — a frase cai no texto geral. */
export function lerListaCheia(details: Record<string, unknown> | undefined): ListaCheia | null {
  const d = details ?? {};
  const inteiro = (chave: string) => {
    const v = d[chave];
    return typeof v === "number" && Number.isInteger(v) && v >= 0 ? v : null;
  };
  const guests_count = inteiro("guests_count");
  const listed = inteiro("listed");
  const capacity = inteiro("capacity");
  if (guests_count === null || listed === null || capacity === null) return null;
  return { guests_count, listed, capacity };
}

/** A condição que o contrato dá para oferecer o aumento: o vendido abaixo da
 *  capacidade do produto. */
export function cabeAumentar(lista: ListaCheia): boolean {
  return lista.guests_count < lista.capacity;
}

/**
 * Recusas que querem dizer "a tela está velha": alguém acrescentou, retirou ou
 * mudou o estado da reserva depois que ela foi desenhada. A tela recarrega o
 * `/full` — uma leitura, uma linha de `pii_access_log` por hóspede, e por isso
 * só nestes casos, nunca por reflexo em toda recusa.
 */
export function pedeRecarga(falha: Pick<Falha, "code">): boolean {
  return (
    falha.code === "CODE_IN_USE" ||
    falha.code === "GUEST_LIST_FULL" ||
    falha.code === "INVALID_STATE_TRANSITION" ||
    falha.code === "RESOURCE_IN_USE"
  );
}

/** Onde a recusa aconteceu — o mesmo código quer frases diferentes. */
export type GestoNaLista = "acrescentar" | "retirar" | "aumentar";

function plural(n: number, um: string, varios: string): string {
  return `${n} ${n === 1 ? um : varios}`;
}

/**
 * O que dizer quando a lista recusa — **pelo código**, nunca pelo texto da API.
 * `details` entra só onde o contrato o promete. `quem` é o nome de quem se
 * tentou acrescentar ou retirar, quando a tela o tem; sem ele, "esta pessoa".
 */
export function mensagemDaLista(
  falha: Pick<Falha, "code" | "details">,
  gesto: GestoNaLista,
  quem?: string | null,
): string {
  const d = falha.details ?? {};
  const pessoa = quem && quem.trim() !== "" ? quem : "Esta pessoa";
  const alvo = quem && quem.trim() !== "" ? quem : "a pessoa";

  switch (falha.code as CodigoDeErro) {
    case "GUEST_LIST_FULL": {
      const lista = lerListaCheia(d);
      if (!lista) return mensagemDoErro("GUEST_LIST_FULL");
      const situacao = `A lista já tem ${plural(lista.listed, "nome", "nomes")} para ${plural(
        lista.guests_count,
        "hóspede",
        "hóspedes",
      )} da reserva.`;
      return cabeAumentar(lista)
        ? `${situacao} O produto comporta até ${lista.capacity}: aumente o número de hóspedes da reserva para acrescentar ${alvo}.`
        : `${situacao} É o máximo que o produto comporta (${lista.capacity}): para acrescentar ${alvo}, retire antes alguém da lista.`;
    }
    case "CODE_IN_USE":
      return d.is_lead_guest === true
        ? `${pessoa} já está na lista: é o titular da reserva.`
        : `${pessoa} já está na lista. A tela foi atualizada.`;
    case "CONTACT_ANONYMIZED":
      return "Os dados desta pessoa foram apagados a pedido dela (LGPD), e quem pediu a eliminação não volta a ser associado a uma estadia. Se ela vai se hospedar, cadastre-a de novo — o documento e o telefone dela ficaram livres para isso.";
    case "INVALID_STATE_TRANSITION": {
      const status = typeof d.status === "string" ? d.status : null;
      const rotulo = status && ehEstadoDeReserva(status) ? ESTADOS[status].rotulo : null;
      return rotulo
        ? `A reserva está como "${rotulo}": a lista de hóspedes não muda mais. A tela foi atualizada.`
        : "A situação da reserva mudou e a lista de hóspedes não aceita mais mudanças. A tela foi atualizada.";
    }
    case "RESOURCE_IN_USE":
      // Só chega por corrida: o titular não tem o botão. A tela recarrega e
      // mostra quem é o titular agora.
      return `${pessoa} é o titular da reserva e não sai da lista — trocar o titular é editar a reserva, não retirar.`;
    case "VALIDATION_ERROR":
      if (gesto === "acrescentar" && typeof d.contact_id === "string") {
        return "Este cadastro não foi encontrado nesta casa. Procure a pessoa de novo pelo documento.";
      }
      if (gesto === "aumentar" && typeof d.guests_count === "string") {
        return "O número de hóspedes não pode ficar abaixo dos nomes que a lista já tem.";
      }
      return mensagemDoErro("VALIDATION_ERROR");
    case "CAPACITY_EXCEEDED": {
      const capacidade = typeof d.capacity === "number" ? d.capacity : null;
      return capacidade !== null
        ? `O produto comporta no máximo ${capacidade} hóspedes: a reserva não pode passar disso.`
        : mensagemDoErro("CAPACITY_EXCEEDED");
    }
    case "NOT_FOUND":
      return "A reserva não foi encontrada: ela pode ter sido removida, ou não estar mais liberada para o seu perfil.";
    default:
      return mensagemDoErro(falha.code);
  }
}
