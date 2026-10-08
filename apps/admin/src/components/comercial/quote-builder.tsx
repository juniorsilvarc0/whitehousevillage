"use client";

import * as React from "react";
import { CalendarRange, Loader2, TriangleAlert } from "lucide-react";

import { SemaforoDeAlcada } from "@/components/comercial/semaforo-de-alcada";
import { Nota } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { Campo } from "@/components/ui/campo";
import { CheckboxCampo } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import type { Orcamento } from "@/lib/api/comercial";
import { detalhesDoZod } from "@/lib/acoes/campos";
import { mensagemDoErro, type Falha, type Resultado } from "@/lib/acoes/resultado";
import { SEMAFORO, alcadaDe, type LimitesDeAlcada } from "@/lib/comercial/alcada";
import { classeDoTipo, rotuloDoTipo } from "@/lib/comercial/tipos-de-data";
import { formatarDataCurta, noitesEntre } from "@/lib/datas";
import { formatarBRL, formatarPct } from "@/lib/dinheiro";
import { cn } from "@/lib/utils";
import { OrcamentoFormulario, type OrcamentoFormulario as Valores } from "@/lib/comercial/orcamento";

export type ProdutoParaOrcar = {
  id: string;
  code: string;
  name: string;
  /** Ausente quando a lista veio da disponibilidade e não do inventário. */
  capacity: number | null;
  cleaning_fee_cents: number | null;
};

/**
 * O `QuoteBuilder`: produto + datas + hóspedes → o cálculo aberto.
 *
 * Três compromissos:
 *
 * - **O painel não calcula nada.** Todo número exibido vem de `POST /quotes`,
 *   que roda o motor puro do servidor. A única conta local é a faixa da alçada,
 *   e ela existe só para o instante anterior à requisição — quando o orçamento
 *   volta, quem vale é o `discount_authority` que veio nele.
 * - **Orçar não segura data.** Nada é gravado. Entre este número e a venda, a
 *   data pode ser tomada por outra pessoa, e quem impede a dupla venda é a
 *   constraint do banco.
 * - **Acima do teto não se pergunta.** Desconto na faixa vermelha não vira
 *   requisição: o servidor responderia `422 DISCOUNT_ABOVE_LIMIT`, e mandar
 *   assim mesmo só ensinaria o vendedor a tratar a recusa como ruído.
 */
export function QuoteBuilder({
  produtos,
  limites,
  limitesConfirmados,
  inicial,
  aoCalcular,
}: {
  produtos: ProdutoParaOrcar[];
  limites: LimitesDeAlcada;
  limitesConfirmados: boolean;
  inicial: Valores;
  aoCalcular: (valores: Valores) => Promise<Resultado<Orcamento>>;
}) {
  const [valores, setValores] = React.useState<Valores>(inicial);
  const [resposta, setResposta] = React.useState<Resultado<Orcamento> | null>(null);
  const [calculando, setCalculando] = React.useState(false);

  /**
   * Validação e recorte do resultado são **derivados**, não sincronizados.
   *
   * Eram estado escrito de dentro de um efeito, e isso custava duas coisas: um
   * segundo render a cada tecla, e a janela de um quadro em que a tela mostrava
   * o erro (ou o número) de um valor que o usuário já tinha corrigido. Calcular
   * no render fecha a janela — não existe instante em que `erros` discorde de
   * `valores`.
   */
  const analise = React.useMemo(() => OrcamentoFormulario.safeParse(valores), [valores]);
  const erros = React.useMemo(
    () => (analise.success ? {} : detalhesDoZod(analise.error)),
    [analise],
  );

  const pctDigitado = Number((valores.discount_pct || "0").replace(",", "."));
  const pct = Number.isFinite(pctDigitado) ? pctDigitado : 0;
  const alcadaLocal = alcadaDe(pct, limites);
  const bloqueado = !SEMAFORO[alcadaLocal].podeFechar;
  const calculavel = analise.success && !bloqueado;

  // Formulário que não pode gerar cálculo não mostra cálculo: a resposta velha
  // continua guardada (é ela que reaparece dimmed enquanto o recálculo não
  // volta), mas não é exibida enquanto não houver pedido válido por trás dela.
  const orcamento = calculavel && resposta?.ok ? resposta.data : null;
  const falhaDoCalculo = calculavel && resposta && !resposta.ok ? resposta : null;
  // O mesmo recorte conserta o spinner eterno: requisição cancelada não zera
  // `calculando` (é sempre sucedida por outra), e se o formulário ficasse
  // inválido nesse intervalo nenhuma requisição nova vinha desligá-lo.
  const carregando = calculando && calculavel;

  // Depois de calcular, quem manda é o servidor: ele leu a política vigente, e
  // esta tela pode estar com limites assumidos.
  const alcada = orcamento?.discount_authority ?? alcadaLocal;

  const produto = produtos.find((p) => p.id === valores.unit_type_id) ?? null;
  const noites = noitesEntre(valores.check_in, valores.check_out);

  function definir<K extends keyof Valores>(campo: K, valor: Valores[K]) {
    setValores((atual) => ({ ...atual, [campo]: valor }));
  }

  // Cálculo com atraso curto: o campo de hóspedes e o de desconto mudam a cada
  // tecla, e uma requisição por tecla entupiria a API para mostrar números que
  // ninguém chega a ler. O efeito não escreve estado nenhum de forma síncrona —
  // só agenda a requisição e cancela a anterior, que é a única coisa que um
  // efeito deve fazer.
  React.useEffect(() => {
    if (!calculavel) return;

    let cancelado = false;
    const temporizador = setTimeout(async () => {
      setCalculando(true);
      try {
        const resultado = await aoCalcular(valores);
        if (!cancelado) setResposta(resultado);
      } finally {
        if (!cancelado) setCalculando(false);
      }
    }, 350);

    return () => {
      cancelado = true;
      clearTimeout(temporizador);
    };
  }, [valores, calculavel, aoCalcular]);

  /**
   * A URL acompanha o formulário — o orçamento fechado é o que se manda ao
   * proprietário para aprovar, e a tela inteira precisa caber num link.
   *
   * Escrito com `history.replaceState`, **não** com `router.replace`: aqui a
   * URL é marcador, não navegação. `router.replace` refaria a renderização do
   * lado do servidor a cada meio segundo de slider arrastado, trazendo a árvore
   * inteira de volta pela rede para não mudar nada na tela.
   */
  React.useEffect(() => {
    const temporizador = setTimeout(() => {
      const parametros = new URLSearchParams();
      if (valores.unit_type_id) parametros.set("produto", valores.unit_type_id);
      if (valores.check_in) parametros.set("entrada", valores.check_in);
      if (valores.check_out) parametros.set("saida", valores.check_out);
      if (valores.guests_count) parametros.set("hospedes", valores.guests_count);
      if (valores.discount_pct && valores.discount_pct !== "0") parametros.set("desconto", valores.discount_pct);
      if (valores.is_event) parametros.set("evento", "1");
      const consulta = parametros.toString();
      if (window.location.search.replace(/^\?/, "") === consulta) return;
      window.history.replaceState(null, "", consulta ? `/app/orcamento?${consulta}` : "/app/orcamento");
    }, 400);
    return () => clearTimeout(temporizador);
  }, [valores]);

  return (
    <div className="grid gap-5 lg:grid-cols-[22rem_1fr]">
      <section aria-label="Dados do orçamento" className="flex flex-col gap-4 rounded-xl border border-border/60 bg-muted/20 p-4">
        <Campo id="orc-produto" label="Produto" obrigatorio erro={erros.unit_type_id}>
          {(p) => (
            <Select {...p} value={valores.unit_type_id} onChange={(e) => definir("unit_type_id", e.target.value)}>
              <option value="">Escolha…</option>
              {produtos.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                  {item.capacity ? ` — até ${item.capacity} hóspedes` : ""}
                </option>
              ))}
            </Select>
          )}
        </Campo>

        <div className="grid grid-cols-2 gap-3">
          <Campo id="orc-entrada" label="Entrada" obrigatorio erro={erros.check_in}>
            {(p) => (
              <Input {...p} type="date" value={valores.check_in} onChange={(e) => definir("check_in", e.target.value)} className="tabular-nums" />
            )}
          </Campo>

          <Campo id="orc-saida" label="Saída" obrigatorio erro={erros.check_out}>
            {(p) => (
              <Input {...p} type="date" value={valores.check_out} onChange={(e) => definir("check_out", e.target.value)} className="tabular-nums" />
            )}
          </Campo>
        </div>

        {noites > 0 ? (
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <CalendarRange className="size-3.5" aria-hidden="true" />
            <span className="tabular-nums text-foreground">{noites}</span> noite{noites === 1 ? "" : "s"} — o dia
            do check-out não conta como diária e já fica livre para o próximo hóspede.
          </p>
        ) : null}

        <Campo
          id="orc-hospedes"
          label="Hóspedes"
          obrigatorio
          erro={erros.guests_count}
          hint={produto?.capacity ? `Capacidade de ${produto.name}: ${produto.capacity}.` : undefined}
        >
          {(p) => (
            <Input {...p} value={valores.guests_count} onChange={(e) => definir("guests_count", e.target.value)} inputMode="numeric" className="text-right tabular-nums" />
          )}
        </Campo>

        <div className="flex flex-col gap-2">
          <div className="flex items-end gap-3">
            <Campo id="orc-desconto" label="Desconto (%)" erro={erros.discount_pct} className="w-28">
              {(p) => (
                <Input
                  {...p}
                  value={valores.discount_pct}
                  onChange={(e) => definir("discount_pct", e.target.value)}
                  inputMode="decimal"
                  className="text-right tabular-nums"
                />
              )}
            </Campo>
            <label htmlFor="orc-desconto-slider" className="sr-only">
              Desconto em porcentagem
            </label>
            <input
              id="orc-desconto-slider"
              type="range"
              min={0}
              max={Math.max(Math.ceil(limites.aprovacao) + 5, 15)}
              step={0.5}
              value={Number.isFinite(pct) ? pct : 0}
              onChange={(e) => definir("discount_pct", e.target.value)}
              className="mb-3 h-1.5 flex-1 cursor-pointer accent-[var(--primary)]"
            />
          </div>

          <SemaforoDeAlcada pct={pct} limites={limites} alcada={alcada} />

          {!limitesConfirmados ? (
            <p className="text-[0.7rem] leading-snug text-muted-foreground">
              Limites de desconto estimados ({formatarPct(limites.auto)} / {formatarPct(limites.aprovacao)}): seu
              perfil não tem acesso à política comercial para conferi-los. O cálculo ao lado confirma o valor
              certo.
            </p>
          ) : null}
        </div>

        <CheckboxCampo
          id="orc-evento"
          label="É evento"
          hint="Soma ao total a caução de evento. Ela é devolvida depois e o desconto não se aplica a ela."
          checked={valores.is_event}
          onChange={(e) => definir("is_event", e.target.checked)}
        />
      </section>

      <section aria-label="Resultado do orçamento" className="min-w-0">
        {bloqueado ? (
          <div className={cn("rounded-xl border px-4 py-4", SEMAFORO.negado.classe)}>
            <p className="flex items-start gap-2 text-sm">
              <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
              <span>
                <strong>{formatarPct(pct)} está acima do teto da política ({formatarPct(limites.aprovacao)}).</strong>{" "}
                Esse desconto não é permitido, nem com aprovação do proprietário. Para mudar o limite, é
                preciso publicar uma nova versão da política comercial.
              </span>
            </p>
          </div>
        ) : falhaDoCalculo ? (
          <FalhaDoOrcamento falha={falhaDoCalculo} />
        ) : orcamento ? (
          <ResultadoDoOrcamento orcamento={orcamento} calculando={carregando} />
        ) : (
          <div className="flex h-full min-h-[16rem] flex-col items-center justify-center rounded-xl border border-dashed border-border/70 bg-muted/20 px-6 text-center">
            {carregando ? (
              <Loader2 className="size-5 animate-spin text-muted-foreground" aria-hidden="true" />
            ) : (
              <>
                <h3 className="font-display text-base">O cálculo aparece aqui</h3>
                <p className="mt-1 max-w-prose text-sm text-muted-foreground">
                  Escolha o produto e as datas. Cada noite tem um tipo (normal, fim de semana, feriado…), e
                  cada tipo tem o seu preço de diária.
                </p>
              </>
            )}
          </div>
        )}
      </section>
    </div>
  );
}

/**
 * A recusa do motor, explicada com o que veio em `details`.
 *
 * Reagir ao `code` é a regra; usar o `details` é o que transforma "não deu" em
 * "faltam duas noites". Os números vêm do servidor, nunca de uma conta local.
 */
function FalhaDoOrcamento({ falha }: { falha: Falha }) {
  const d = falha.details;
  const numero = (chave: string): number | null => {
    const valor = d[chave];
    return typeof valor === "number" ? valor : null;
  };

  let detalhe: React.ReactNode = null;
  switch (falha.code) {
    case "MIN_STAY_NOT_MET": {
      const exigido = numero("required");
      const pedido = numero("requested");
      detalhe =
        exigido !== null ? (
          <>
            O período exige no mínimo <strong>{exigido} noites</strong>
            {pedido !== null ? <> e a estadia tem {pedido}</> : null}. Vale o maior mínimo entre as noites da
            estadia — uma única noite de réveillon no meio já exige o mínimo do réveillon.
          </>
        ) : null;
      break;
    }
    case "CAPACITY_EXCEEDED": {
      const capacidade = numero("capacity");
      const pedido = numero("requested");
      detalhe =
        capacidade !== null ? (
          <>
            A capacidade do produto é <strong>{capacidade}</strong>
            {pedido !== null ? <> e foram pedidos {pedido} hóspedes</> : null}.
          </>
        ) : null;
      break;
    }
    case "RATE_NOT_FOUND": {
      const data = typeof d.date === "string" ? d.date : null;
      const tipo = typeof d.date_type === "string" ? d.date_type : null;
      detalhe = (
        <>
          A tabela de preços atual não tem preço
          {tipo ? (
            <>
              {" "}
              para <strong>{rotuloDoTipo(tipo as Parameters<typeof rotuloDoTipo>[0])}</strong>
            </>
          ) : null}
          {data ? <> (noite de {formatarDataCurta(data)})</> : null}. Preencha o preço em Configurações → Tarifário.
        </>
      );
      break;
    }
    case "COMPOSITION_INCOMPLETE": {
      const esperadas = numero("expected_units");
      const ativas = numero("active_units");
      const faltando = Array.isArray(d.missing_unit_codes)
        ? d.missing_unit_codes.filter((c): c is string => typeof c === "string")
        : [];
      detalhe = (
        <>
          Este produto é formado por{" "}
          {esperadas !== null ? <strong>{esperadas} apartamentos</strong> : "apartamentos"}
          {ativas !== null ? <> e só {ativas} está{ativas === 1 ? "" : "ão"} ativo{ativas === 1 ? "" : "s"}</> : null}
          {faltando.length > 0 ? (
            <>
              {" "}
              — falta{faltando.length === 1 ? "" : "m"} <strong>{faltando.join(", ")}</strong>
            </>
          ) : null}
          . O problema não é a data: <strong>nenhuma data funciona</strong> enquanto faltar apartamento.
          Reative o apartamento em Configurações → Unidades e produtos.
        </>
      );
      break;
    }
    case "DISCOUNT_ABOVE_LIMIT": {
      const teto = numero("max_pct");
      detalhe = teto !== null ? <>O desconto máximo permitido pela política é {formatarPct(teto)}.</> : null;
      break;
    }
    default:
      detalhe = null;
  }

  return (
    <div
      role="alert"
      data-codigo={falha.code}
      title={`Código para o suporte: ${falha.code}`}
      className="rounded-xl border border-destructive/30 bg-destructive/8 px-4 py-4"
    >
      <h3 className="font-display text-base">Este orçamento não pode ser feito</h3>
      <p className="mt-1 text-sm text-muted-foreground">{mensagemDoErro(falha.code)}</p>
      {detalhe ? <p className="mt-2 text-sm text-muted-foreground">{detalhe}</p> : null}
    </div>
  );
}

function Linha({
  rotulo,
  valor,
  nota,
  destaque = false,
  negativo = false,
}: {
  rotulo: React.ReactNode;
  valor: number;
  nota?: React.ReactNode;
  destaque?: boolean;
  negativo?: boolean;
}) {
  return (
    <div className={cn("flex items-baseline justify-between gap-4 py-1.5", destaque && "border-t border-border/60 pt-3")}>
      <div className="min-w-0">
        <span className={cn("text-sm", destaque && "font-display text-base")}>{rotulo}</span>
        {nota ? <span className="block text-[0.7rem] leading-snug text-muted-foreground">{nota}</span> : null}
      </div>
      <span
        className={cn(
          "shrink-0 font-mono tabular-nums",
          destaque ? "text-lg" : "text-sm",
          negativo && "text-alcada-livre",
        )}
      >
        {negativo && valor > 0 ? "−" : ""}
        {formatarBRL(valor)}
      </span>
    </div>
  );
}

function ResultadoDoOrcamento({ orcamento, calculando }: { orcamento: Orcamento; calculando: boolean }) {
  return (
    <div className={cn("flex flex-col gap-4 transition-opacity", calculando && "opacity-60")}>
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="accent">
          {orcamento.night_count} noite{orcamento.night_count === 1 ? "" : "s"}
        </Badge>
        <Badge variant="outline">diária média {formatarBRL(orcamento.avg_nightly_cents)}</Badge>
        <Badge variant="outline">mínimo do período: {orcamento.min_nights} {orcamento.min_nights === 1 ? "noite" : "noites"}</Badge>
        <Badge variant="outline">política comercial versão {orcamento.policy_version}</Badge>
        {calculando ? <Loader2 className="size-4 animate-spin text-muted-foreground" aria-label="Recalculando" /> : null}
      </div>

      <div className="overflow-x-auto rounded-xl border border-border/60 bg-card/60">
        <table className="w-full min-w-[28rem] border-collapse text-sm">
          <caption className="sr-only">Noites agrupadas por tipo de tarifa</caption>
          <thead>
            <tr className="border-b border-border/60 text-xs text-muted-foreground">
              <th scope="col" className="px-3 py-2 text-left font-medium">Tipo de noite</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">Noites</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">Diária</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">Subtotal</th>
            </tr>
          </thead>
          <tbody>
            {orcamento.lines.map((linha) => (
              <tr key={linha.date_type} className="border-b border-border/40 last:border-b-0">
                <td className="px-3 py-2">
                  <span className={cn("rounded-full px-2 py-0.5 text-[0.7rem]", classeDoTipo(linha.date_type))}>
                    {linha.label || rotuloDoTipo(linha.date_type)}
                  </span>
                </td>
                <td className="px-3 py-2 text-right tabular-nums">{linha.nights}</td>
                <td className="px-3 py-2 text-right font-mono tabular-nums">{formatarBRL(linha.unit_price_cents)}</td>
                <td className="px-3 py-2 text-right font-mono tabular-nums">{formatarBRL(linha.subtotal_cents)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <details className="rounded-xl border border-border/60 bg-card/60">
        <summary className="cursor-pointer select-none px-3 py-2 text-sm text-muted-foreground">
          Noite a noite ({orcamento.nights.length}) — são estes os preços que ficam na reserva
        </summary>
        <div className="max-h-72 overflow-y-auto border-t border-border/60">
          <table className="w-full border-collapse text-sm">
            <caption className="sr-only">Cada noite com o tipo de data e o preço aplicado</caption>
            <tbody>
              {orcamento.nights.map((noite) => (
                <tr key={noite.date} className="border-b border-border/30 last:border-b-0">
                  <td className="px-3 py-1.5 tabular-nums">{formatarDataCurta(noite.date)}</td>
                  <td className="px-3 py-1.5">
                    <span className={cn("rounded-full px-2 py-0.5 text-[0.65rem]", classeDoTipo(noite.date_type))}>
                      {noite.label || rotuloDoTipo(noite.date_type)}
                    </span>
                  </td>
                  <td className="px-3 py-1.5 text-right font-mono tabular-nums">{formatarBRL(noite.price_cents)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>

      <div className="rounded-xl border border-border/60 bg-card/60 px-4 py-3">
        <Linha rotulo="Subtotal das diárias" valor={orcamento.subtotal_cents} />
        {orcamento.discount_cents > 0 ? (
          <Linha
            rotulo={`Desconto (${formatarPct(orcamento.discount_pct)})`}
            valor={orcamento.discount_cents}
            negativo
            nota="Vale só para as diárias — nunca para a limpeza nem para a caução."
          />
        ) : null}
        <Linha
          rotulo="Taxa de limpeza"
          valor={orcamento.cleaning_cents}
          nota="Cobrada uma vez por estadia, sem desconto."
        />
        {orcamento.event_deposit_cents > 0 ? (
          <Linha
            rotulo="Caução de evento"
            valor={orcamento.event_deposit_cents}
            nota="Devolvida depois do evento — entra no total, mas não é receita."
          />
        ) : null}
        <Linha rotulo="Total" valor={orcamento.total_cents} destaque />
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        <div className="rounded-xl border border-primary/30 bg-accent/25 px-4 py-3">
          <p className="text-xs text-muted-foreground">Sinal para confirmar</p>
          <p className="font-mono text-xl tabular-nums">{formatarBRL(orcamento.deposit_cents)}</p>
          <p className="mt-1 text-[0.7rem] leading-snug text-muted-foreground">
            Se não for pago no prazo, a pré-reserva vence e as datas voltam a ficar livres.
          </p>
        </div>
        <div className="rounded-xl border border-border/60 bg-card/60 px-4 py-3">
          <p className="text-xs text-muted-foreground">Saldo</p>
          <p className="font-mono text-xl tabular-nums">{formatarBRL(orcamento.balance_cents)}</p>
          <p className="mt-1 text-[0.7rem] leading-snug text-muted-foreground">
            Vence alguns dias antes do check-in, conforme a política comercial.
          </p>
        </div>
      </div>

      <Nota>
        Este orçamento <strong>não guarda as datas</strong>. Só a pré-reserva reserva o calendário — até lá,
        as mesmas noites podem ser vendidas para outra pessoa.
      </Nota>
    </div>
  );
}
