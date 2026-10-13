import { Suspense } from "react";
import { Wrench } from "lucide-react";

import { Paginacao, parametrosDe } from "@/components/bens/paginacao";
import { EstadoDeErro, EstadoVazio, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import { BotaoNovaOrdem } from "@/components/manutencao/botao-nova-ordem";
import { FiltrosDaLista } from "@/components/manutencao/filtros";
import { ListaDeOrdens } from "@/components/manutencao/lista-de-ordens";
import { requireSession } from "@/lib/auth/session";
import { unidadesDoInventario } from "@/lib/bens/api";
import { listarOrdens } from "@/lib/manutencao/api";
import { CAMINHO_DA_MANUTENCAO } from "@/lib/manutencao/mensagens";
import { lerFiltros, temFiltro, type ParametrosCrus } from "@/lib/manutencao/filtros";
import { permissoesDaManutencao } from "@/lib/manutencao/permissoes";

export const metadata = { title: "Manutenção" };

/**
 * Ordens de manutenção — a lista de trabalho da operação.
 *
 * Abre sem filtro, e a API já entrega na ordem certa (`sort=urgencia`): o que
 * está aberto primeiro, do mais urgente ao menos, e o mais antigo no topo;
 * depois o que foi encerrado, do mais recente. Quem abre no celular, dentro do
 * apartamento, vê no topo o que está há mais tempo esperando.
 *
 * As unidades vêm de `GET /inventory/units` (`inventory.goods:ver`, como o
 * contrato decide para o formulário); sem ela, a lista funciona e o filtro de
 * unidade fica vazio.
 */
export default async function ManutencaoPage({ searchParams }: { searchParams: Promise<ParametrosCrus> }) {
  const { permissions } = await requireSession();
  const permissoes = permissoesDaManutencao(permissions);
  if (!permissoes.ver) return <SemAcesso recurso="maintenance" />;

  const cru = await searchParams;
  const { filtros, avisos } = lerFiltros(cru);

  const [lista, unidades] = await Promise.all([listarOrdens(filtros), unidadesDoInventario()]);
  const seletor = unidades.ok ? unidades.data.map((u) => ({ id: u.id, code: u.code, name: u.name })) : [];
  const filtrado = temFiltro(filtros);

  const novaOrdem = permissoes.criar ? <BotaoNovaOrdem unidades={seletor} unidadeInicial={filtros.unit_id || undefined} /> : null;

  return (
    <Tela>
      <CabecalhoDeTela
        titulo="Manutenção"
        descricao={
          <>
            O que precisa de conserto em cada unidade. Uma ordem pode <strong>bloquear o calendário</strong> enquanto o
            serviço acontece, e libera as datas sozinha ao ser concluída ou cancelada.
          </>
        }
        acoes={novaOrdem}
      />

      <Suspense fallback={<div className="h-16" />}>
        <FiltrosDaLista unidades={seletor} />
      </Suspense>

      {avisos.length > 0 ? <Nota variante="atencao">{avisos.join(" ")}</Nota> : null}
      {permissoes.criar && !unidades.ok ? (
        <Nota variante="atencao">
          A lista de unidades não carregou, então o formulário não tem onde escolher a unidade. Para abrir ordens, o
          perfil precisa ver o inventário de bens.
        </Nota>
      ) : null}

      {!lista.ok ? (
        <EstadoDeErro
          code={lista.code}
          titulo="Não foi possível carregar as ordens"
          detalhe={lista.code === "NOT_FOUND" ? "Esta parte do sistema pode ainda não estar no ar." : undefined}
        />
      ) : lista.data.data.length === 0 ? (
        <EstadoVazio
          icone={Wrench}
          titulo={filtrado ? "Nenhuma ordem com esse recorte" : "Nenhuma ordem de manutenção"}
          descricao={
            filtrado ? (
              <>Troque a situação para “Todas” ou limpe a busca.</>
            ) : (
              <>
                O ar que parou, o chuveiro que pinga, a pintura antes do Réveillon. Uma ordem também nasce de uma avaria,
                pela tela de avarias do inventário.
              </>
            )
          }
          acao={filtrado ? undefined : novaOrdem}
        />
      ) : (
        <>
          <ListaDeOrdens ordens={lista.data.data} />
          <Paginacao
            meta={lista.data.meta}
            caminho={CAMINHO_DA_MANUTENCAO}
            parametros={parametrosDe(cru)}
            substantivo={["ordem", "ordens"]}
          />
        </>
      )}
    </Tela>
  );
}
