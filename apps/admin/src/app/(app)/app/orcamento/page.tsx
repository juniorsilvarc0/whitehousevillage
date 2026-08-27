import { EstadoDeErro, EstadoVazio, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import type { ProdutoParaOrcar } from "@/components/comercial/quote-builder";
import type { DisponibilidadeDoProduto, PoliticaComercial, Produto } from "@/lib/api/comercial";
import { carregar, carregarLista } from "@/lib/api/carregar";
import type { Resultado } from "@/lib/acoes/resultado";
import type { LimitesDeAlcada } from "@/lib/comercial/alcada";
import type { OrcamentoFormulario } from "@/lib/comercial/orcamento";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { hojeISO, somarDias } from "@/lib/datas";

import { ConstrutorDeOrcamento } from "./builder";

export const metadata = { title: "Orçamento" };

/** O padrão da spec §3, usado só quando o perfil não alcança a política. */
const ALCADA_PADRAO: LimitesDeAlcada = { auto: 5, aprovacao: 10 };

export default async function OrcamentoPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { permissions } = await requireSession();
  if (!can(permissions, "quotes", "criar")) return <SemAcesso recurso="quotes" acao="criar" />;

  const parametros = await searchParams;
  const texto = (chave: string): string => {
    const valor = parametros[chave];
    return typeof valor === "string" ? valor : "";
  };

  const [produtos, politica] = await Promise.all([carregarProdutos(), carregar<PoliticaComercial>("/policies/commercial")]);

  const limites: LimitesDeAlcada = politica.ok
    ? { auto: politica.data.discount_auto_pct, aprovacao: politica.data.discount_approval_pct }
    : ALCADA_PADRAO;

  const hoje = hojeISO();
  const inicial: OrcamentoFormulario = {
    unit_type_id: texto("produto"),
    check_in: texto("entrada") || hoje,
    check_out: texto("saida") || somarDias(hoje, 2),
    guests_count: texto("hospedes") || "2",
    discount_pct: texto("desconto") || "0",
    is_event: texto("evento") === "1",
  };

  return (
    <Tela>
      <CabecalhoDeTela
        titulo="Orçamento"
        descricao="Produto, datas e hóspedes; o servidor devolve o cálculo aberto — noite a noite, agrupado por tipo de tarifa, com sinal e saldo."
      />

      {!produtos.ok ? (
        <EstadoDeErro
          code={produtos.code}
          titulo="Não foi possível carregar os produtos"
          detalhe="Sem a lista de produtos não há o que orçar: o motor precisa de um para achar tarifa, capacidade e limpeza."
        />
      ) : produtos.data.length === 0 ? (
        <EstadoVazio
          titulo="Nenhum produto disponível"
          descricao="Cadastre ao menos um produto ativo no inventário para poder orçar."
        />
      ) : (
        <ConstrutorDeOrcamento
          produtos={produtos.data}
          limites={limites}
          limitesConfirmados={politica.ok}
          inicial={inicial}
        />
      )}

      <Nota>
        O painel não recalcula nada: cada número vem de <code>POST /quotes</code>, que roda o motor puro do
        servidor sobre a tabela de tarifas e a política vigentes. A única conta feita aqui é a faixa da
        alçada enquanto o controle é arrastado — e assim que o cálculo volta, quem vale é a alçada que ele
        trouxe.
      </Nota>
    </Tela>
  );
}

/**
 * A lista de produtos, com um desvio deliberado.
 *
 * `GET /unit-types` exige `inventory:ver`, e o **corretor não tem esse
 * recurso** — mas tem `quotes` e `calendar`. Sem desvio, o perfil que mais
 * orça seria o único incapaz de escolher o que orçar.
 *
 * `GET /availability` (recurso `calendar`) devolve os mesmos produtos ativos
 * com id, código e nome, que é tudo de que o seletor precisa. Vem sem
 * `capacity` e sem taxa de limpeza — a tela omite as duas dicas nesse caso, em
 * vez de inventar números. Fica registrado como buraco do contrato: o certo
 * seria uma leitura de catálogo que `quotes` alcance.
 */
async function carregarProdutos(): Promise<Resultado<ProdutoParaOrcar[]>> {
  const doInventario = await carregarLista<Produto>("/unit-types", { sort: "sort_order", active: true });
  if (doInventario.ok) {
    return {
      ok: true,
      data: doInventario.data.map((p) => ({
        id: p.id,
        code: p.code,
        name: p.name,
        capacity: p.capacity,
        cleaning_fee_cents: p.cleaning_fee_cents,
      })),
    };
  }
  if (doInventario.code !== "FORBIDDEN") return doInventario;

  const hoje = hojeISO();
  const daDisponibilidade = await carregar<DisponibilidadeDoProduto[]>("/availability", {
    from: hoje,
    to: somarDias(hoje, 1),
  });
  if (!daDisponibilidade.ok) return daDisponibilidade;

  return {
    ok: true,
    data: daDisponibilidade.data.map((p) => ({
      id: p.unit_type_id,
      code: p.unit_type_code,
      name: p.name,
      capacity: null,
      cleaning_fee_cents: null,
    })),
  };
}
