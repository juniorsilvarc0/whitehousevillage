import Link from "next/link";
import { Calculator } from "lucide-react";

import { ReguaDePrecedencia } from "@/components/comercial/grade-de-tarifas";
import { EstadoDeErro, EstadoVazio, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Secao, Tela } from "@/components/layout/tela";
import { buttonVariants } from "@/components/ui/button";
import type { MinimoDeNoites, Produto, TabelaDeTarifas, Tarifa, TipoDeData } from "@/lib/api/comercial";
import { carregarLista } from "@/lib/api/carregar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { hojeISO } from "@/lib/datas";
import { cn } from "@/lib/utils";

import { GradeDaTabela } from "./grade";
import { EditorDeMinimos } from "./minimos";
import { SeletorDeTabelas } from "./tabelas";

export const metadata = { title: "Tarifário" };

/**
 * A grade produto × tipo de data, e a precedência que a explica.
 *
 * A régua de precedência fica na mesma tela de propósito: sem ela, "por que 31
 * de dezembro custa isso?" é uma pergunta que só a documentação responde. Com
 * ela, a resposta está ao lado do número.
 */
export default async function TarifarioPage({
  searchParams,
}: {
  searchParams: Promise<{ tabela?: string }>;
}) {
  const { permissions } = await requireSession();
  if (!can(permissions, "settings", "ver")) return <SemAcesso recurso="settings" />;

  const { tabela: tabelaPedida } = await searchParams;

  const [tabelas, produtos] = await Promise.all([
    carregarLista<TabelaDeTarifas>("/rate-tables"),
    carregarLista<Produto>("/unit-types", { sort: "sort_order", active: true }),
  ]);

  const escolhida = tabelas.ok ? escolherTabela(tabelas.data, tabelaPedida) : null;

  const [tarifas, minimos] = escolhida
    ? await Promise.all([
        carregarLista<Tarifa>("/rates", { rate_table_id: escolhida.id }),
        carregarLista<MinimoDeNoites>("/min-nights", { rate_table_id: escolhida.id }),
      ])
    : [null, null];

  const permissoes = {
    criar: can(permissions, "settings", "criar"),
    editar: can(permissions, "settings", "editar"),
    excluir: can(permissions, "settings", "excluir"),
  };

  const minimosPorTipo: Partial<Record<TipoDeData, number>> = {};
  for (const regra of minimos?.ok ? minimos.data : []) minimosPorTipo[regra.date_type] = regra.nights;

  return (
    <Tela>
      <CabecalhoDeTela
        voltar={{ href: "/app/configuracoes", rotulo: "Configurações" }}
        titulo="Tarifário"
        descricao="O preço da diária de cada produto em cada tipo de data (normal, fim de semana, feriado…)."
        acoes={
          <Link href="/app/orcamento" className={cn(buttonVariants({ variant: "outline", size: "sm" }))}>
            <Calculator aria-hidden="true" />
            Simular orçamento
          </Link>
        }
      />

      <Nota>
        Mudar um preço vale para os <strong>próximos</strong> orçamentos. Orçamentos e reservas já
        feitos continuam com o preço combinado, noite a noite.
      </Nota>

      {!tabelas.ok ? <EstadoDeErro code={tabelas.code} titulo="Não foi possível carregar as tabelas de tarifas" /> : null}
      {!produtos.ok ? <EstadoDeErro code={produtos.code} titulo="Não foi possível carregar os produtos" /> : null}

      {tabelas.ok ? (
        <SeletorDeTabelas tabelas={tabelas.data} selecionada={escolhida} permissoes={permissoes} />
      ) : null}

      {tabelas.ok && tabelas.data.length === 0 ? (
        <EstadoVazio
          titulo="Nenhuma tabela de tarifas"
          descricao="Sem uma tabela de preços valendo, não dá para fazer orçamentos. Crie a primeira."
        />
      ) : null}

      {escolhida && produtos.ok ? (
        <Secao
          titulo={`Preços — ${escolhida.name}`}
          descricao="Altere os valores direto na tabela e salve tudo de uma vez. Se algo der errado, nada é salvo pela metade."
        >
          {tarifas === null || !tarifas.ok ? (
            <EstadoDeErro
              code={tarifas?.code ?? "INTERNAL"}
              titulo="Não foi possível carregar as tarifas desta tabela"
              detalhe="Por segurança, só dá para editar depois de ver os preços atuais. Tente recarregar a página."
            />
          ) : produtos.data.length === 0 ? (
            <EstadoVazio
              titulo="Nenhum produto ativo"
              descricao="Cada linha da tabela é um produto. Cadastre os produtos no inventário primeiro."
              acao={
                <Link href="/app/configuracoes/inventario" className={cn(buttonVariants({ size: "sm", variant: "outline" }))}>
                  Abrir o inventário
                </Link>
              }
            />
          ) : (
            // `key` na tabela escolhida: trocar de tabela **recomeça** a edição, e é
            // a remontagem que repõe o rascunho — não um efeito correndo atrás
            // das props depois de a grade já ter renderizado a tabela anterior.
            <GradeDaTabela
              key={escolhida.id}
              tabelaId={escolhida.id}
              produtos={produtos.data.map((p) => ({ id: p.id, code: p.code, name: p.name, active: p.active }))}
              tarifas={tarifas.data}
              minimos={minimos?.ok ? minimos.data : []}
              podeEditar={permissoes.editar}
            />
          )}
        </Secao>
      ) : null}

      <div className="grid gap-4 lg:grid-cols-2">
        <Secao
          titulo="Qual preço vale em cada noite"
          descricao="Cada noite tem um tipo só. Quando dois períodos caem na mesma noite, vale o que está mais acima nesta lista."
        >
          <ReguaDePrecedencia minimosPorTipo={minimosPorTipo} />
          <Nota className="mt-3">
            Réveillon dentro da alta temporada é normal, não é erro de cadastro: 31/12 numa sexta-feira
            é cobrado como <strong>réveillon</strong>, não como fim de semana.
          </Nota>
        </Secao>

        {escolhida ? (
          <Secao
            titulo="Estadia mínima por tipo de data"
            descricao="Vale o maior mínimo entre as noites da estadia — três noites normais com um réveillon no meio exigem o mínimo do réveillon."
          >
            {minimos === null || !minimos.ok ? (
              <EstadoDeErro code={minimos?.code ?? "INTERNAL"} titulo="Não foi possível carregar os mínimos" />
            ) : (
              <EditorDeMinimos
                key={escolhida.id}
                tabelaId={escolhida.id}
                minimos={minimos.data}
                podeEditar={permissoes.editar}
                podeExcluir={permissoes.excluir}
              />
            )}
          </Secao>
        ) : null}
      </div>
    </Tela>
  );
}

/**
 * Qual tabela a tela abre.
 *
 * A da query string ganha (é o link que alguém compartilhou). Sem ela, a
 * **vigente hoje** — é a que responde "quanto custa se eu vender agora?", que é
 * a pergunta que traz a gestão a esta tela. Só então a primeira da lista.
 */
function escolherTabela(tabelas: TabelaDeTarifas[], pedida?: string): TabelaDeTarifas | null {
  if (tabelas.length === 0) return null;
  const daQuery = pedida ? tabelas.find((t) => t.id === pedida) : undefined;
  if (daQuery) return daQuery;

  const hoje = hojeISO();
  const vigentes = tabelas
    .filter((t) => t.active && t.valid_from <= hoje && (t.valid_to === null || t.valid_to >= hoje))
    .sort((a, b) => b.valid_from.localeCompare(a.valid_from));

  return vigentes[0] ?? tabelas[0] ?? null;
}
