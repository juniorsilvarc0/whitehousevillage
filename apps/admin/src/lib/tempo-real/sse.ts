"use client";

import * as React from "react";

import { Deduplicador, lerEvento, type EventoDoStream, type Topico } from "./eventos";

/**
 * `useSSE` — a assinatura do barramento de eventos, e o que o usuário vê quando
 * ela não está de pé.
 *
 * ## O caminho do token
 *
 * `EventSource` **não** manda cabeçalho customizado: não existe
 * `Authorization: Bearer` aqui. E o contrato recusa token em query string, com
 * razão — ele pararia no log de acesso do proxy. Sobra o cookie `httpOnly`, e é
 * por isso que a URL é `/api/stream` (mesma origem, Route Handler do painel) e
 * não a API Go direto: o BFF é quem lê o cookie e injeta o `Bearer` no salto
 * seguinte.
 *
 * ## Os quatro modos de falha, e o que aparece na tela em cada um
 *
 * | Situação | Quem reconecta | O que o usuário vê |
 * |---|---|---|
 * | Rede caiu | o navegador, sozinho (`retry: 5000`) | `reconectando`, com o horário do último evento |
 * | Aba em segundo plano | ninguém — a conexão sobrevive | ao voltar, um refetch imediato (`aoResync`), porque tique perdido é indistinguível de nada ter acontecido |
 * | Token expirando | este hook, em `expiring`, chamando `/api/auth/refresh` | `renovando`, sem interrupção |
 * | Sessão morta de vez | ninguém | `desligado`, com o botão de reconectar — e não um mapa parado fingindo estar vivo |
 *
 * O caso do token merece o parágrafo que o contrato dedica a ele: **não dá para
 * responder `401` depois do `200`**. Os cabeçalhos já foram enviados e o
 * `EventSource` não deixa ler status depois disso. Por isso o servidor avisa
 * (`expiring`, 60 s antes), o cliente renova o cookie **fora** do stream, e o
 * servidor encerra limpo no `exp` (`expired`) — o navegador reconecta sozinho e
 * a conexão nova já sai com o token novo. Renovar sessão por dentro do stream
 * seria um segundo lugar onde a sessão se renova, e o segundo lugar é sempre o
 * que ninguém testa.
 *
 * ## Por que o estado é observável
 *
 * Um mapa que parou de receber eventos é **idêntico** a um mapa onde nada
 * aconteceu. Sem o indicador, a gestão confia num desenho velho — que é
 * exatamente o risco que o tempo real existia para eliminar. Daí `estado` e
 * `ultimoEventoEm` serem parte do retorno, e não detalhe interno.
 */
export type EstadoDoTempoReal =
  | "conectando"
  | "ligado"
  | "reconectando"
  | "renovando"
  | "sem_rede"
  | "desligado";

/** A fatia do `EventSource` que este hook usa — o resto é ruído, e tipar só o
 *  necessário é o que permite injetar um duplo no teste sem jsdom. */
export type FonteDeEventos = {
  addEventListener(tipo: string, ouvinte: (evento: MessageEvent<string>) => void): void;
  close(): void;
  readonly readyState: number;
};

export type CriarFonte = (url: string) => FonteDeEventos;

const FECHADA = 2;

/** Espera antes de reabrir à mão, dobrando até meio minuto. O navegador já
 *  tenta sozinho a cada 5 s quando a queda é de transporte; esta escada é para
 *  a queda que o encerra de vez (resposta não-2xx), onde insistir de imediato
 *  vira uma tempestade de requisições contra um servidor já em apuros. */
const ESPERAS_MS = [1_000, 2_000, 5_000, 10_000, 30_000];

export type OpcoesDoSSE = {
  topicos: readonly Topico[];
  /** Chamado uma vez por evento **novo** (já deduplicado por `v`). */
  aoEvento: (evento: EventoDoStream, topico: Topico) => void;
  /** "Não sei o que você perdeu": refaça o fetch inteiro. Vem do `event:
   *  resync` do servidor e do retorno da aba ao primeiro plano. */
  aoResync?: () => void;
  /** Renova o cookie de sessão. `false` significa que a sessão morreu. */
  renovarSessao?: () => Promise<boolean>;
  criarFonte?: CriarFonte;
  /** Desligar é legítimo: a tela sem permissão no tópico não abre conexão
   *  nenhuma, em vez de abrir uma que só receberia `403`. */
  habilitado?: boolean;
};

export type TempoReal = {
  estado: EstadoDoTempoReal;
  /** Instante do último evento de dado. `null` enquanto nenhum chegou — e é
   *  informação: conexão aberta sem evento é o estado normal de uma casa parada. */
  ultimoEventoEm: number | null;
  /** Tópicos que o servidor de fato aceitou, do `event: ready`. Pode ser menor
   *  que o pedido: tópico fora da matriz é descartado da assinatura. */
  topicosAceitos: readonly Topico[];
  reconectar: () => void;
};

function fonteDoNavegador(url: string): FonteDeEventos {
  const fonte = new EventSource(url, { withCredentials: true });
  return {
    addEventListener: (tipo, ouvinte) => fonte.addEventListener(tipo, ouvinte as EventListener),
    close: () => fonte.close(),
    get readyState() {
      return fonte.readyState;
    },
  };
}

async function renovarPeloBFF(): Promise<boolean> {
  try {
    const resposta = await fetch("/api/auth/refresh", { method: "POST", cache: "no-store" });
    return resposta.ok;
  } catch {
    return false;
  }
}

export function useSSE({
  topicos,
  aoEvento,
  aoResync,
  renovarSessao = renovarPeloBFF,
  criarFonte = fonteDoNavegador,
  habilitado = true,
}: OpcoesDoSSE): TempoReal {
  const [estadoDaConexao, setEstado] = React.useState<EstadoDoTempoReal>("conectando");
  const [ultimoEventoEm, setUltimoEventoEm] = React.useState<number | null>(null);
  const [topicosAceitos, setTopicosAceitos] = React.useState<readonly Topico[]>([]);
  const [tentativa, setTentativa] = React.useState(0);

  // As três callbacks entram como **Effect Events**, e não como dependências do
  // efeito. O chamador as recria a cada render (são closures sobre o estado da
  // tela); amarrá-las ao efeito reabriria a conexão a cada repintura — que é
  // como um "tempo real" vira um laço de reconexão capaz de derrubar o servidor
  // sozinho. `useEffectEvent` é a forma que o React oferece para "sempre a
  // versão mais nova, sem entrar na lista de dependências".
  const emitir = React.useEffectEvent((evento: EventoDoStream, topico: Topico) => {
    aoEvento(evento, topico);
  });
  const ressincronizar = React.useEffectEvent(() => {
    aoResync?.();
  });
  const renovar = React.useEffectEvent(() => renovarSessao());

  const ultimoIdRef = React.useRef<string | null>(null);
  const dedupRef = React.useRef(new Deduplicador());

  const chaveDosTopicos = [...topicos].sort().join(",");
  const ligavel = habilitado && chaveDosTopicos !== "";

  // Desligado é DERIVADO, não guardado: escrever "desligado" no estado dentro do
  // efeito seria um render em cascata a cada montagem, e o estado passaria a ter
  // duas fontes (a prop e ele mesmo) que precisariam concordar.
  const estado: EstadoDoTempoReal = ligavel ? estadoDaConexao : "desligado";

  const reconectar = React.useCallback(() => {
    setTentativa((n) => n + 1);
  }, []);

  React.useEffect(() => {
    if (!ligavel) return;

    let vivo = true;
    let reabertura: ReturnType<typeof setTimeout> | null = null;

    const busca = new URLSearchParams({ topics: chaveDosTopicos });
    // O navegador só reenvia `Last-Event-ID` nas reconexões que ELE faz. Numa
    // reabertura nossa (backoff, botão, volta da rede) o cursor iria embora e o
    // servidor mandaria `resync` — recarga inteira em vez de reposição. O
    // contrato aceita o cursor como query param exatamente para isto.
    if (ultimoIdRef.current) busca.set("last_event_id", ultimoIdRef.current);

    // Abrir a conexão NÃO escreve estado: `conectando` já é o valor inicial, e
    // numa reabertura o estado corrente (`reconectando`, posto pelo tratador de
    // erro) é mais verdadeiro do que voltar a "conectando" — quem confirma que
    // a conexão vale é o `ready`, que é o primeiro evento que o servidor manda,
    // com os tópicos efetivamente aceitos.
    const fonte = criarFonte(`/api/stream?${busca.toString()}`);

    function marcarEvento(topico: Topico) {
      return (evento: MessageEvent<string>) => {
        if (!vivo) return;
        if (evento.lastEventId) ultimoIdRef.current = evento.lastEventId;
        const lido = lerEvento(evento.data);
        if (!lido) return;
        setUltimoEventoEm(Date.now());
        if (!dedupRef.current.novidade(lido)) return;
        emitir(lido, topico);
      };
    }

    fonte.addEventListener("ready", (evento) => {
      if (!vivo) return;
      setEstado("ligado");
      try {
        const corpo = JSON.parse(evento.data) as { topics?: unknown };
        if (Array.isArray(corpo.topics)) {
          setTopicosAceitos(corpo.topics.filter((t): t is Topico => t === "calendar" || t === "crm"));
        }
      } catch {
        // `ready` ilegível não é motivo para derrubar a conexão: os tópicos
        // aceitos viram desconhecidos, e o resto do stream continua valendo.
        setTopicosAceitos([]);
      }
    });

    fonte.addEventListener("calendar", marcarEvento("calendar"));
    fonte.addEventListener("crm", marcarEvento("crm"));

    fonte.addEventListener("resync", () => {
      if (!vivo) return;
      // O servidor está dizendo que o cursor pedido é mais velho que o buffer
      // dele. Fingir que nada se perdeu é como o mapa fica errado sem ninguém
      // perceber — então o que a tela já desenhou deixa de ser referência.
      dedupRef.current.esquecer();
      ressincronizar();
    });

    fonte.addEventListener("expiring", () => {
      if (!vivo) return;
      setEstado("renovando");
      void renovar().then((ok) => {
        if (vivo) setEstado(ok ? "ligado" : "desligado");
      });
    });

    fonte.addEventListener("expired", () => {
      if (!vivo) return;
      // Fim de stream limpo. O navegador reabre sozinho, já com o cookie novo
      // que o `expiring` renovou — não há nada a fazer além de dizer isso.
      setEstado("reconectando");
    });

    fonte.addEventListener("error", () => {
      if (!vivo) return;
      if (navigator.onLine === false) {
        setEstado("sem_rede");
        return;
      }
      if (fonte.readyState !== FECHADA) {
        // O navegador está reabrindo por conta própria (queda de transporte).
        setEstado("reconectando");
        return;
      }

      // Fechada de vez: resposta não-2xx. A causa mais provável e a única que a
      // tela consegue tratar sozinha é sessão vencida — tenta renovar uma vez
      // e reabre com a escada de espera.
      setEstado("reconectando");
      void renovar().then((ok) => {
        if (!vivo) return;
        if (!ok) {
          setEstado("desligado");
          return;
        }
        const espera = ESPERAS_MS[Math.min(tentativa, ESPERAS_MS.length - 1)]!;
        reabertura = setTimeout(reconectar, espera);
      });
    });

    function aoVoltarParaFrente() {
      if (document.visibilityState !== "visible") return;
      // A aba em segundo plano não perde a conexão, mas o navegador estrangula
      // temporizador e pode adiar entrega. Ao voltar, um refetch imediato é
      // mais barato do que a dúvida: um mapa que "não recebeu evento" é
      // idêntico a um mapa onde nada aconteceu.
      ressincronizar();
      if (fonte.readyState === FECHADA) reconectar();
    }

    function aoVoltarARede() {
      setEstado("reconectando");
      reconectar();
    }

    document.addEventListener("visibilitychange", aoVoltarParaFrente);
    window.addEventListener("online", aoVoltarARede);
    window.addEventListener("offline", () => setEstado("sem_rede"));

    return () => {
      vivo = false;
      if (reabertura) clearTimeout(reabertura);
      document.removeEventListener("visibilitychange", aoVoltarParaFrente);
      window.removeEventListener("online", aoVoltarARede);
      fonte.close();
    };
    // `tentativa` entra de propósito: incrementá-lo é o gesto de reabrir.
  }, [chaveDosTopicos, ligavel, criarFonte, reconectar, tentativa]);

  return { estado, ultimoEventoEm, topicosAceitos, reconectar };
}
