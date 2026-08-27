"use client";

import * as React from "react";
import { Check, Loader2, RotateCcw, Save } from "lucide-react";

import { Nota } from "@/components/layout/tela";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { GradeGravada, MetaDaGrade, Tarifa, TarifaDaGrade, TipoDeData } from "@/lib/api/comercial";
import { mensagemDoErro, type Resultado } from "@/lib/acoes/resultado";
import { TIPOS_DE_DATA, classeDoTipo } from "@/lib/comercial/tipos-de-data";
import { centavosDeTexto, reaisDeCentavos } from "@/lib/dinheiro";
import { cn } from "@/lib/utils";

/**
 * A grade produto × tipo de data — o `RateEditor` do design system.
 *
 * Três decisões que a tela materializa:
 *
 * 1. **Salva em lote, numa transação.** Salvar célula a célula seriam 24
 *    requisições capazes de falhar pela metade e deixar a tabela comercial num
 *    estado que nunca existiu no papel. `POST /rates/bulk` é tudo-ou-nada.
 * 2. **Substitui os produtos que estão na tela, e só eles.** O salvamento
 *    declara o escopo (`unit_type_ids` = as linhas desta grade) e, dentro dele,
 *    o par (produto, tipo de data) que não for enviado é *removido*. Produto
 *    fora do escopo — o inativo, que a página nem lista — fica intacto.
 *
 *    Era o contrário até 26/08/2026, e o defeito foi medido: salvar a grade de
 *    um produto apagava as células dos outros três, e a venda deles passava a
 *    morrer em `RATE_NOT_FOUND` sem que ninguém tivesse pedido para apagar
 *    nada. A tela conta as remoções antes de deixar salvar, e depois mostra o
 *    que o servidor de fato fez — é assim que um `removed` inesperado aparece
 *    para quem salvou, e não semanas depois, para quem tenta vender.
 * 3. **Não reescreve o passado.** Reserva e orçamento emitidos guardam o preço
 *    que usaram; mudar aqui vale do próximo cálculo em diante.
 *
 * `aoSalvar` entra por parâmetro em vez de a grade importar a Server Action:
 * assim o componente é testável sem servidor, e a página continua sendo quem
 * decide em que tabela a gravação acontece.
 */

export type ProdutoDaGrade = { id: string; code: string; name: string; active: boolean };

type Chave = `${string}:${TipoDeData}`;

function chave(produtoId: string, tipo: TipoDeData): Chave {
  return `${produtoId}:${tipo}`;
}

function valoresIniciais(produtos: ProdutoDaGrade[], tarifas: Tarifa[]): Record<string, string> {
  const porChave = new Map<string, number>();
  for (const tarifa of tarifas) porChave.set(chave(tarifa.unit_type_id, tarifa.date_type), tarifa.amount_cents);

  const valores: Record<string, string> = {};
  for (const produto of produtos) {
    for (const tipo of TIPOS_DE_DATA) {
      const centavos = porChave.get(chave(produto.id, tipo.code));
      valores[chave(produto.id, tipo.code)] = centavos === undefined ? "" : reaisDeCentavos(centavos);
    }
  }
  return valores;
}

export function GradeDeTarifas({
  tabelaId,
  produtos,
  tarifas,
  minimosPorTipo,
  podeEditar,
  aoSalvar,
}: {
  tabelaId: string;
  produtos: ProdutoDaGrade[];
  tarifas: Tarifa[];
  /** Estadia mínima por tipo de data, só para o cabeçalho explicar a coluna. */
  minimosPorTipo: Partial<Record<TipoDeData, number>>;
  podeEditar: boolean;
  /** `escopo` são os produtos que esta gravação reescreve — as linhas da grade.
   *  Vai separado das células de propósito: é ele que permite **zerar** um
   *  produto (citado no escopo, sem nenhuma célula) sem que apagar aconteça por
   *  omissão para quem não foi citado. */
  aoSalvar: (tabelaId: string, escopo: string[], celulas: TarifaDaGrade[]) => Promise<Resultado<GradeGravada>>;
}) {
  const iniciais = React.useMemo(() => valoresIniciais(produtos, tarifas), [produtos, tarifas]);
  const [base, setBase] = React.useState(iniciais);
  const [valores, setValores] = React.useState(iniciais);
  const [estado, setEstado] = React.useState<"parado" | "salvando" | "salvo">("parado");
  const [erro, setErro] = React.useState<string | null>(null);
  const [gravado, setGravado] = React.useState<MetaDaGrade | null>(null);

  // Não há efeito repondo a grade quando as props mudam: escolher outra tabela
  // remonta o componente (`key={escolhida.id}` na página), que é a forma que o
  // React oferece para "recomeçar do zero", e a revalidação da mesma tabela não
  // pode passar por cima do que a pessoa está digitando. Depois de salvar, quem
  // vira a nova base é a resposta do servidor, logo ali embaixo.

  function editar(produtoId: string, tipo: TipoDeData, texto: string) {
    setValores((atual) => ({ ...atual, [chave(produtoId, tipo)]: texto }));
    setEstado("parado");
    setGravado(null);
  }

  const alteradas = React.useMemo(
    () => Object.keys(valores).filter((k) => (valores[k] ?? "").trim() !== (base[k] ?? "").trim()),
    [valores, base],
  );

  const invalidas = React.useMemo(
    () => Object.keys(valores).filter((k) => {
      const texto = (valores[k] ?? "").trim();
      if (texto === "") return false;
      const centavos = centavosDeTexto(texto);
      return centavos === null || centavos < 1;
    }),
    [valores],
  );

  const removidas = React.useMemo(
    () => Object.keys(valores).filter((k) => (valores[k] ?? "").trim() === "" && (base[k] ?? "").trim() !== ""),
    [valores, base],
  );

  const preenchidas = React.useMemo(
    () => Object.keys(valores).filter((k) => (valores[k] ?? "").trim() !== ""),
    [valores],
  );

  const sujo = alteradas.length > 0;

  async function salvar() {
    setErro(null);

    if (invalidas.length > 0) {
      setErro(
        `${invalidas.length} célula${invalidas.length === 1 ? "" : "s"} com valor inválido. A diária precisa ser um valor em reais maior que zero.`,
      );
      return;
    }
    if (preenchidas.length === 0) {
      setErro(
        "A grade inteira está vazia. Salvar assim apagaria a tarifa de todos os produtos desta tela e nenhum orçamento sairia.",
      );
      return;
    }

    const celulas: TarifaDaGrade[] = [];
    for (const produto of produtos) {
      for (const tipo of TIPOS_DE_DATA) {
        const texto = (valores[chave(produto.id, tipo.code)] ?? "").trim();
        if (texto === "") continue;
        celulas.push({
          unit_type_id: produto.id,
          date_type: tipo.code,
          amount_cents: centavosDeTexto(texto) ?? 0,
        });
      }
    }

    setEstado("salvando");
    // O escopo é a grade inteira que está à vista, não só o que mudou: é o que
    // faz uma célula esvaziada virar remoção. Produto que a página não lista
    // (inativo) fica de fora do escopo — e por isso não é tocado.
    const escopo = produtos.map((produto) => produto.id);
    const resultado = await aoSalvar(tabelaId, escopo, celulas);
    if (!resultado.ok) {
      setEstado("parado");
      setErro(mensagemDoErro(resultado.code));
      return;
    }
    // A resposta é a grade como ficou gravada: vira a nova base, e o que estava
    // "alterado" deixa de estar sem esperar o servidor renderizar de novo.
    const gravada = valoresIniciais(produtos, resultado.data.tarifas);
    setBase(gravada);
    setValores(gravada);
    setGravado(resultado.data.meta);
    setEstado("salvo");
  }

  function restaurar() {
    setValores(base);
    setEstado("parado");
    setErro(null);
    setGravado(null);
  }

  return (
    <div className="flex flex-col gap-3">
      {/* Conteúdo largo rola DENTRO do próprio container: tabela que estoura a
          página horizontalmente é anti-padrão da casca. */}
      <div className="overflow-x-auto rounded-xl border border-border/60 bg-card/60">
        <table className="w-full min-w-[46rem] border-collapse text-sm">
          <caption className="sr-only">
            Tarifa da diária por produto e tipo de data, em reais. As colunas estão em ordem
            decrescente de precedência.
          </caption>
          <thead>
            <tr className="border-b border-border/60">
              <th scope="col" className="sticky left-0 z-10 bg-card px-3 py-2.5 text-left font-medium">
                Produto
              </th>
              {TIPOS_DE_DATA.map((tipo) => (
                <th key={tipo.code} scope="col" className="px-2 py-2.5 text-right font-medium">
                  <span className={cn("inline-block rounded-full px-2 py-0.5 text-[0.7rem]", classeDoTipo(tipo.code))}>
                    {tipo.label}
                  </span>
                  <span className="mt-1 block text-[0.65rem] font-normal tabular-nums text-muted-foreground">
                    precedência {tipo.precedencia}
                    {minimosPorTipo[tipo.code] ? ` · mín. ${minimosPorTipo[tipo.code]}n` : ""}
                  </span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {produtos.map((produto) => (
              <tr key={produto.id} className="border-b border-border/40 last:border-b-0">
                <th scope="row" className="sticky left-0 z-10 bg-card px-3 py-2 text-left font-normal">
                  <span className="block truncate text-sm">{produto.name}</span>
                  <span className="block font-mono text-[0.65rem] text-muted-foreground">{produto.code}</span>
                </th>
                {TIPOS_DE_DATA.map((tipo) => {
                  const k = chave(produto.id, tipo.code);
                  const texto = valores[k] ?? "";
                  const invalida = invalidas.includes(k);
                  const removida = removidas.includes(k);
                  const mudou = alteradas.includes(k);
                  return (
                    <td key={tipo.code} className="px-1.5 py-1.5">
                      <Input
                        aria-label={`${produto.name} — ${tipo.label}`}
                        value={texto}
                        onChange={(evento) => editar(produto.id, tipo.code, evento.target.value)}
                        disabled={!podeEditar}
                        inputMode="decimal"
                        placeholder="—"
                        invalid={invalida}
                        className={cn(
                          "h-9 min-w-[6.5rem] text-right font-mono text-sm tabular-nums",
                          mudou && !invalida && !removida && "border-primary/50 bg-accent/25",
                          removida && "border-dashed border-destructive/50",
                        )}
                      />
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {removidas.length > 0 ? (
        <Nota variante="atencao">
          {removidas.length} célula{removidas.length === 1 ? "" : "s"} ficou vazia e será{" "}
          <strong>removida</strong> ao salvar — o salvamento substitui a grade dos produtos desta tela.
          Sem a célula, uma noite daquele tipo passa a recusar o orçamento com{" "}
          <code>RATE_NOT_FOUND</code>, que é melhor do que vender por um valor inventado.
        </Nota>
      ) : null}

      {erro ? (
        <p role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erro}
        </p>
      ) : null}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground" aria-live="polite">
          {estado === "salvo" && !sujo ? (
            <span className="inline-flex flex-wrap items-center gap-x-1.5 text-alcada-livre">
              <Check className="size-3.5" aria-hidden="true" />
              <span>Grade salva.</span>
              {gravado ? (
                <span className="tabular-nums">
                  {gravado.created} criada{gravado.created === 1 ? "" : "s"}, {gravado.updated} atualizada
                  {gravado.updated === 1 ? "" : "s"}, {gravado.unchanged} inalterada
                  {gravado.unchanged === 1 ? "" : "s"},{" "}
                  <strong className={cn(gravado.removed > 0 && "text-destructive")}>
                    {gravado.removed} removida{gravado.removed === 1 ? "" : "s"}
                  </strong>{" "}
                  em {gravado.unit_type_ids.length} produto{gravado.unit_type_ids.length === 1 ? "" : "s"}.
                </span>
              ) : null}
              <span>
                Vale do próximo cálculo em diante — orçamentos e reservas já emitidos não mudam.
              </span>
            </span>
          ) : sujo ? (
            <>
              <span className="tabular-nums">{alteradas.length}</span> célula
              {alteradas.length === 1 ? "" : "s"} alterada{alteradas.length === 1 ? "" : "s"}, ainda não
              salva{alteradas.length === 1 ? "" : "s"}.
            </>
          ) : (
            <>
              <span className="tabular-nums">{preenchidas.length}</span> tarifas nesta tabela. Total da grade
              cheia: {produtos.length * TIPOS_DE_DATA.length} células.
            </>
          )}
        </p>

        {podeEditar ? (
          <div className="flex gap-2">
            <Button size="sm" variant="ghost" onClick={restaurar} disabled={!sujo || estado === "salvando"}>
              <RotateCcw aria-hidden="true" />
              Descartar
            </Button>
            <Button size="sm" onClick={salvar} disabled={!sujo || estado === "salvando"}>
              {estado === "salvando" ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Save aria-hidden="true" />}
              Salvar a grade
            </Button>
          </div>
        ) : null}
      </div>
    </div>
  );
}

/** A régua de precedência — é o que explica por que 31/12 numa sexta custa
 *  réveillon e não fim de semana. */
export function ReguaDePrecedencia({ minimosPorTipo }: { minimosPorTipo: Partial<Record<TipoDeData, number>> }) {
  return (
    <ol className="flex flex-col gap-1.5">
      {TIPOS_DE_DATA.map((tipo, indice) => (
        <li key={tipo.code} className="flex items-center gap-3 rounded-lg bg-card/70 px-3 py-2">
          <span className="w-5 shrink-0 text-center font-mono text-xs text-muted-foreground">{indice + 1}</span>
          <span className={cn("shrink-0 rounded-full px-2 py-0.5 text-[0.7rem]", classeDoTipo(tipo.code))}>
            {tipo.label}
          </span>
          <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{tipo.regra}</span>
          <span className="shrink-0 font-mono text-xs tabular-nums text-muted-foreground">
            {tipo.precedencia}
          </span>
          <span className="w-16 shrink-0 text-right text-xs tabular-nums text-muted-foreground">
            {minimosPorTipo[tipo.code] ? `mín. ${minimosPorTipo[tipo.code]}n` : "mín. 1n"}
          </span>
        </li>
      ))}
    </ol>
  );
}
