import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { MapaDeOcupacao } from "@/components/mapa/mapa-de-ocupacao";
import { hojeISO } from "@/lib/datas";
import { filtrosDosParametros, janelaDosFiltros, type ParametrosCrus } from "@/lib/mapa/filtros";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";

import { buscarOcupacao, criarBloqueio, liberarBloqueio } from "./acoes";
import { carregarConfiguracao, carregarOcupacao, carregarPrecos } from "./dados";

export const metadata = { title: "Mapa de ocupação" };

/**
 * A tela mais cara do projeto (`docs/ui.md` §9), montada em três buscas
 * paralelas e uma decisão de permissão.
 *
 * O recurso é `calendar` — o mesmo que a OpenAPI declara em `x-rbac` para
 * `/availability/units`, `/blocks` e o tópico `calendar` do `/stream`. O menu já
 * esconde o item para quem não o tem; esta guarda existe para quem chega pela
 * URL, e o `SemAcesso` diz **o que pedir a quem** em vez de mostrar uma página
 * quebrada.
 *
 * `criar` e `excluir` viajam separados porque a matriz os concede separados — e
 * o contrato registra que concedê-los assim, quebrados, foi um defeito medido:
 * o corretor bloqueou dois meses da Cobertura e recebeu `403` ao tentar
 * desfazer. A tela mostra o cadeado só para quem tem a chave.
 */
export default async function MapaPage({
  searchParams,
}: {
  searchParams: Promise<ParametrosCrus>;
}) {
  const { permissions } = await requireSession();
  if (!can(permissions, "calendar", "ver")) return <SemAcesso recurso="calendar" />;

  const hoje = hojeISO();
  const filtros = filtrosDosParametros(await searchParams, hoje);
  const janela = janelaDosFiltros(filtros);

  // As três saem juntas e falham em separado. Ocupação sem preço ainda é um
  // mapa (perde o valor no inspetor); preço sem ocupação não é nada — por isso
  // só a ocupação e a configuração derrubam a tela.
  const [configuracao, ocupacao, precos] = await Promise.all([
    carregarConfiguracao(),
    carregarOcupacao(janela),
    carregarPrecos(janela),
  ]);

  if (!configuracao.ok) {
    return (
      <div className="p-4 sm:p-6 lg:p-8">
        <EstadoDeErro
          code={configuracao.code}
          titulo="Não foi possível carregar o inventário"
          detalhe="Sem os produtos e a composição não há como agrupar as unidades nem derivar a linha da casa inteira."
        />
      </div>
    );
  }

  if (!ocupacao.ok) {
    return (
      <div className="p-4 sm:p-6 lg:p-8">
        <EstadoDeErro
          code={ocupacao.code}
          titulo="Não foi possível carregar a ocupação"
          detalhe="A matriz unidade × dia vem de GET /availability/units."
        />
      </div>
    );
  }

  return (
    <MapaDeOcupacao
      filtros={filtros}
      produtos={configuracao.data.produtos}
      composicoes={configuracao.data.composicoes}
      dadosIniciais={ocupacao.data}
      precos={precos.ok ? precos.data : []}
      hoje={hoje}
      permissoes={{
        criar: can(permissions, "calendar", "criar"),
        excluir: can(permissions, "calendar", "excluir"),
      }}
      aoBuscarOcupacao={buscarOcupacao}
      aoCriarBloqueio={criarBloqueio}
      aoLiberarBloqueio={liberarBloqueio}
    />
  );
}
