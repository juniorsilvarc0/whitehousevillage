import { ehDataISO, hojeISO, somarDias, type DataISO } from "@/lib/datas";

import { DIAS_PADRAO, janelaDe, limitarDias, type Janela } from "./janela";

/**
 * Os filtros do mapa vivem na **query string**, não em estado de componente.
 *
 * O motivo é operacional, não arquitetural: a conversa da gestão é "olha o
 * Réveillon, a COB-01 está livre" — e essa frase precisa de um link. Filtro em
 * `useState` produz uma URL que abre outra tela para quem recebe, e a resposta
 * vira "rola para dezembro e filtra por cobertura". Com a faixa na URL, o
 * `router.replace` do navegador também devolve o botão Voltar funcionando.
 */
export type FiltrosDoMapa = {
  from: DataISO;
  dias: number;
  /** `null` é "todos os produtos". */
  unitTypeId: string | null;
  /** Esconde as linhas cujo dia inteiro da janela está livre. */
  somenteOcupadas: boolean;
};

export type ParametrosCrus = Record<string, string | string[] | undefined>;

function primeiro(valor: string | string[] | undefined): string | undefined {
  return Array.isArray(valor) ? valor[0] : valor;
}

/**
 * Lê a query string com tolerância: parâmetro ausente, repetido ou impossível
 * cai no padrão em vez de derrubar a tela. Um mapa que responde 500 porque
 * alguém colou `?dias=abc` num Slack é um mapa que ninguém compartilha.
 */
export function filtrosDosParametros(
  params: ParametrosCrus,
  hoje: DataISO = hojeISO(),
): FiltrosDoMapa {
  const from = primeiro(params.from);
  const dias = Number.parseInt(primeiro(params.dias) ?? "", 10);
  const produto = primeiro(params.produto);

  return {
    from: from && ehDataISO(from) ? from : somarDias(hoje, -7),
    dias: Number.isNaN(dias) ? DIAS_PADRAO : limitarDias(dias),
    unitTypeId: produto && produto !== "todos" ? produto : null,
    somenteOcupadas: primeiro(params.ocupadas) === "1",
  };
}

export function janelaDosFiltros(filtros: FiltrosDoMapa): Janela {
  return janelaDe(filtros.from, filtros.dias);
}

/**
 * Serializa **omitindo o que é padrão**. Uma URL que carrega `?dias=90` quando
 * 90 já é o padrão envelhece mal: mudar o padrão amanhã não alcançaria os links
 * já colados em conversa, e eles continuariam abrindo a janela antiga sem que
 * ninguém entendesse por quê.
 */
export function paraQueryString(filtros: FiltrosDoMapa, hoje: DataISO = hojeISO()): string {
  const busca = new URLSearchParams();
  if (filtros.from !== somarDias(hoje, -7)) busca.set("from", filtros.from);
  if (filtros.dias !== DIAS_PADRAO) busca.set("dias", String(filtros.dias));
  if (filtros.unitTypeId) busca.set("produto", filtros.unitTypeId);
  if (filtros.somenteOcupadas) busca.set("ocupadas", "1");
  const texto = busca.toString();
  return texto ? `?${texto}` : "";
}
