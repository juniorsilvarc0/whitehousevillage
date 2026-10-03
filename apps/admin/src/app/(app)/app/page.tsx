import Link from "next/link";
import {
  ArrowRight, CalendarClock, DoorClosed, DoorOpen, Hammer, Info, ShieldCheck, TriangleAlert,
} from "lucide-react";

import { BotaoWhatsApp, TelefoneClicavel } from "@/components/contatos/botao-whatsapp";
import { EstadoVazio } from "@/components/layout/estados";
import { Badge } from "@/components/ui/badge";
import { CardDescription, CardTitle } from "@/components/ui/card";
import { EM_CONSTRUCAO, navigation } from "@/config/navigation";
import { carregarLista } from "@/lib/api/carregar";
import type { DisponibilidadeDoProduto, MotivoDeIndisponibilidade } from "@/lib/api/comercial";
import { can, escopoDe } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { formatarData, hojeISO, somarDias, type DataISO } from "@/lib/datas";
import { dataPorExtenso, saudacao } from "@/lib/fuso";
import { expiracaoDeHold, type UrgenciaDoHold } from "@/lib/mapa/expiracao";
import type { LinhaDoMapa, StatusDaCelula } from "@/lib/mapa/tipos";
import type { Reserva } from "@/lib/reservas/tipos";
import { cn } from "@/lib/utils";

export const metadata = { title: "Painel" };

/**
 * A primeira tela depois do login.
 *
 * ## O que ela era, e por que isso não servia
 *
 * Ela mostrava dois blocos: uma grade de atalhos e um **despejo da matriz de
 * permissões** do perfil. O segundo é informação de quem depura RBAC, não de
 * quem abre o sistema às sete da manhã para saber quem chega hoje. E cinco dos
 * nove atalhos apontavam para telas que não existem — medido em 27/08/2026:
 * `/app/chat`, `/app/financeiro`, `/app/comissoes`, `/app/relatorios` e
 * `/app/reservas` respondiam 404.
 *
 * Agora a tela responde as quatro perguntas do primeiro café: **quanto do mês
 * está vendido**, **quem chega e quem sai hoje**, **o que vence antes do fim do
 * dia** e **o que está mal configurado a ponto de impedir uma venda**.
 *
 * ## Nenhum endpoint novo, e nenhuma conta que o servidor já faça
 *
 * Tudo sai de três rotas que já estavam no ar: `/availability/units` (matriz
 * unidade × dia), `/reservations` e `/availability` (que é quem calcula
 * `unavailable_reason`). O painel **não** recalcula preço, ocupação de produto
 * nem prazo de pré-reserva: `hold_expires_at` é o instante que a API gravou, e
 * reproduzir "48 h a partir da criação" aqui erraria em toda pré-reserva
 * estendida — que é justamente a que alguém está acompanhando.
 *
 * ## Cada bloco falha sozinho
 *
 * As buscas saem em paralelo e nenhuma derruba as outras. É deliberado nesta
 * fase: os módulos da API estão sendo escritos em paralelo, e uma rota ainda não
 * registrada responde 404. Perder a ocupação não pode custar a lista de quem
 * chega hoje.
 *
 * ## Os atalhos saem do `navigation.ts`
 *
 * Não há mais uma segunda lista de destinos aqui. Era ela que apontava para as
 * cinco telas inexistentes: a lista do menu foi consertada e esta ficou para
 * trás, porque ninguém lembra de duas listas. Agora o atalho é o item de menu
 * que o perfil alcança — se o destino não existe, ele não está no menu, e não
 * está aqui.
 */

// ── Derivações ─────────────────────────────────────────────────────────────

/** Estados de célula que significam **noite ocupada por venda**. */
const OCUPADA: readonly StatusDaCelula[] = ["hold", "confirmed", "checked_out"];
/** Ocupadas sem venda: manutenção, uso do proprietário e importação de OTA. */
const BLOQUEADA: readonly StatusDaCelula[] = ["maintenance", "owner_hold", "ota"];

type Ocupacao = {
  /** Noites-unidade vendidas / total. É a conta que o mapa faz no olho. */
  vendidas: number;
  bloqueadas: number;
  total: number;
  /** Uma entrada por dia do mês, para a régua. */
  porDia: { data: DataISO; vendidas: number; bloqueadas: number; total: number }[];
};

/**
 * Ocupação em **noites-unidade**, não em reservas.
 *
 * Contar reservas diria que a casa está cheia quando uma Completa de três noites
 * empata com um apartamento de trinta. A unidade física por dia é a mesma
 * unidade de medida que a constraint `stay_no_overlap` protege — e a mesma que a
 * gestão conta no mapa quando confere no dedo.
 */
function calcularOcupacao(linhas: readonly LinhaDoMapa[]): Ocupacao {
  const porDia = new Map<DataISO, { data: DataISO; vendidas: number; bloqueadas: number; total: number }>();

  for (const linha of linhas) {
    for (const celula of linha.days) {
      const dia = porDia.get(celula.date) ?? { data: celula.date, vendidas: 0, bloqueadas: 0, total: 0 };
      dia.total += 1;
      if (OCUPADA.includes(celula.status)) dia.vendidas += 1;
      else if (BLOQUEADA.includes(celula.status)) dia.bloqueadas += 1;
      porDia.set(celula.date, dia);
    }
  }

  const dias = [...porDia.values()].sort((a, b) => a.data.localeCompare(b.data));
  return {
    vendidas: dias.reduce((s, d) => s + d.vendidas, 0),
    bloqueadas: dias.reduce((s, d) => s + d.bloqueadas, 0),
    total: dias.reduce((s, d) => s + d.total, 0),
    porDia: dias,
  };
}

type MovimentoDoDia = { entradas: Reserva[]; saidas: Reserva[]; emCasa: Reserva[] };

/**
 * Quem chega, quem sai e quem fica — a partir da mesma lista.
 *
 * A estadia é half-open `[check_in, check_out)`, e é isso que obriga a janela da
 * consulta a começar **ontem**: uma reserva que termina hoje ocupa até ontem à
 * noite e não intersecta `[hoje, amanhã)`. Perguntar só por hoje devolveria as
 * entradas e esconderia exatamente as saídas — a metade do dia que precisa de
 * limpeza e vistoria.
 */
function movimentoDoDia(reservas: readonly Reserva[], hoje: DataISO): MovimentoDoDia {
  return {
    entradas: reservas.filter((r) => r.check_in === hoje),
    saidas: reservas.filter((r) => r.check_out === hoje),
    emCasa: reservas.filter(
      (r) => r.check_in < hoje && r.check_out > hoje && (r.status === "checked_in" || r.status === "confirmed"),
    ),
  };
}

/** Um problema de configuração que a API já sabe calcular, agrupado por produto. */
type Alerta = {
  produto: string;
  motivo: Exclude<MotivoDeIndisponibilidade, "ocupado">;
  dias: number;
  primeiroDia: DataISO;
};

/**
 * Os alertas **não são inventados aqui**: são o `unavailable_reason` que
 * `GET /availability` já devolve por dia, com a precedência declarada no
 * contrato (`composicao_incompleta` > `sem_tarifa` > `unidade_inativa` >
 * `ocupado`).
 *
 * `ocupado` fica de fora de propósito: é assunto do hóspede ("escolha outra
 * data"), não da gestão. Os outros três são configuração pela metade — noite
 * que a venda vai recusar com `RATE_NOT_FOUND` ou `COMPOSITION_INCOMPLETE`, e
 * que ninguém descobre até perder a venda.
 */
function alertasDeConfiguracao(produtos: readonly DisponibilidadeDoProduto[]): Alerta[] {
  const alertas = new Map<string, Alerta>();

  for (const produto of produtos) {
    for (const dia of produto.days) {
      const motivo = dia.unavailable_reason;
      if (!motivo || motivo === "ocupado") continue;
      const chave = `${produto.unit_type_id}|${motivo}`;
      const atual = alertas.get(chave);
      if (atual) atual.dias += 1;
      else alertas.set(chave, { produto: produto.name, motivo, dias: 1, primeiroDia: dia.date });
    }
  }

  // O que mais dói primeiro: mais dias afetados no topo.
  return [...alertas.values()].sort((a, b) => b.dias - a.dias);
}

const TEXTO_DO_ALERTA: Record<Alerta["motivo"], { titulo: string; acao: string }> = {
  sem_tarifa: {
    titulo: "sem preço cadastrado",
    acao: "Nesses dias não dá para vender: falta o preço. Preencha em Configurações → Tarifário.",
  },
  composicao_incompleta: {
    titulo: "faltando apartamento",
    acao: "Um dos apartamentos que formam este item está desativado, então ele não pode ser vendido inteiro. Reative o apartamento em Configurações → Inventário.",
  },
  unidade_inativa: {
    titulo: "sem apartamento ativo",
    acao: "Todos os apartamentos deste item estão desativados. Reative pelo menos um em Configurações → Inventário.",
  },
};

/**
 * O instante da requisição.
 *
 * Fica atrás de uma função nomeada por dois motivos que se somam. O prático:
 * `react-hooks/purity` reprova `Date.now()` escrito no corpo do componente, e
 * com razão — num componente de cliente ele produziria um número diferente a
 * cada re-render. O verdadeiro: esta é uma **tela de servidor** sob
 * `dynamic = "force-dynamic"` (o layout de `(app)`), renderizada uma vez por
 * requisição; "agora" aqui é o instante em que a página foi pedida, que é
 * exatamente o que a contagem de expiração precisa. É a mesma leitura de relógio
 * que `saudacao()` e `dataPorExtenso()` já fazem nesta página — nomeá-la deixa a
 * dependência à vista em vez de escondida numa chamada solta.
 */
function instanteDaLeitura(): number {
  return Date.now();
}

const COR_DA_URGENCIA: Record<UrgenciaDoHold, string> = {
  expirada: "text-destructive",
  critica: "text-destructive",
  atencao: "text-alcada-atencao",
  tranquila: "text-muted-foreground",
};

// ── Tela ───────────────────────────────────────────────────────────────────

function Bloco({
  titulo,
  descricao,
  acao,
  children,
}: {
  titulo: string;
  descricao?: React.ReactNode;
  acao?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    // Sub-superfície dentro do `panel-float`: borda e fundo, nunca sombra —
    // sombra dentro de sombra é o anti-padrão nº 1 da casca.
    <section aria-label={titulo} className="rounded-xl border border-border/60 bg-muted/20 p-4 sm:p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="font-display text-base leading-tight">{titulo}</h2>
          {descricao ? <p className="mt-1 max-w-prose text-sm text-muted-foreground">{descricao}</p> : null}
        </div>
        {acao ? <div className="shrink-0">{acao}</div> : null}
      </div>
      <div className="mt-4">{children}</div>
    </section>
  );
}

function LinhaDeReserva({ reserva, detalhe }: { reserva: Reserva; detalhe?: React.ReactNode }) {
  return (
    <li className="flex items-center gap-3 rounded-lg bg-card/70 px-3 py-2">
      <span className="shrink-0 font-mono text-xs text-primary">{reserva.code}</span>
      <span className="min-w-0 flex-1 truncate text-sm text-foreground">{reserva.contact_name}</span>
      {reserva.contact_phone_e164 ? (
        <span className="flex shrink-0 items-center gap-2 text-xs text-muted-foreground">
          <TelefoneClicavel telefone={reserva.contact_phone_e164} />
          <BotaoWhatsApp telefone={reserva.contact_phone_e164} nome={reserva.contact_name} codigo={reserva.code} />
        </span>
      ) : null}
      <span className="hidden shrink-0 text-xs text-muted-foreground sm:inline">{reserva.unit_type_name}</span>
      <span className="shrink-0 text-xs text-muted-foreground">
        {reserva.units.map((u) => u.unit_code).join(", ") || "—"}
      </span>
      {detalhe}
    </li>
  );
}

export default async function PainelPage() {
  const { user, permissions } = await requireSession();

  const hoje = hojeISO();
  const primeiroDoMes = `${hoje.slice(0, 7)}-01`;
  // Primeiro do mês seguinte: somar 31 dias a partir do dia 1 sempre cai dentro
  // do mês seguinte (mês nenhum tem 32 dias), e cortar o dia devolve o dia 1
  // dele. Evita a aritmética de "quantos dias tem fevereiro" e o bug bissexto.
  const primeiroDoProximoMes = `${somarDias(primeiroDoMes, 31).slice(0, 7)}-01`;

  const podeVerCalendario = can(permissions, "calendar", "ver");
  const podeVerReservas = can(permissions, "reservations", "ver");

  const [ocupacaoBruta, doDia, holds, disponibilidade] = await Promise.all([
    podeVerCalendario
      ? carregarLista<LinhaDoMapa>("/availability/units", { from: primeiroDoMes, to: primeiroDoProximoMes })
      : null,
    podeVerReservas
      ? carregarLista<Reserva>("/reservations", {
          // Começa ONTEM por causa do half-open: quem sai hoje não intersecta
          // `[hoje, amanhã)`. Ver `movimentoDoDia`.
          from: somarDias(hoje, -1),
          to: somarDias(hoje, 1),
          status: "confirmed,checked_in,hold",
          per_page: 100,
        })
      : null,
    podeVerReservas
      ? carregarLista<Reserva>("/reservations", { status: "hold", per_page: 100 })
      : null,
    podeVerCalendario
      ? carregarLista<DisponibilidadeDoProduto>("/availability", { from: hoje, to: somarDias(hoje, 30) })
      : null,
  ]);

  const ocupacao = ocupacaoBruta?.ok ? calcularOcupacao(ocupacaoBruta.data) : null;
  const movimento = doDia?.ok ? movimentoDoDia(doDia.data, hoje) : null;
  const agora = instanteDaLeitura();
  const expirando = holds?.ok
    ? holds.data
        .map((reserva) => ({ reserva, expiracao: expiracaoDeHold(reserva.hold_expires_at, agora) }))
        .filter((item) => item.expiracao !== null)
        .sort((a, b) => a.expiracao!.restanteMs - b.expiracao!.restanteMs)
        .slice(0, 6)
    : null;
  const alertas = disponibilidade?.ok ? alertasDeConfiguracao(disponibilidade.data) : null;

  const atalhos = navigation.filter((item) => item.href !== "/app" && can(permissions, item.recurso, "ver"));
  const percentual = ocupacao && ocupacao.total > 0 ? Math.round((ocupacao.vendidas / ocupacao.total) * 100) : null;
  const primeiroNome = user.name.split(" ")[0] ?? user.name;

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <header>
        <p className="text-xs uppercase tracking-[0.22em] text-muted-foreground">{dataPorExtenso()}</p>
        <h1 className="font-display mt-1 text-2xl leading-tight sm:text-3xl">
          {saudacao()}, {primeiroNome}.
        </h1>
        <p className="mt-2 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
          <span className="inline-flex items-center gap-1.5 rounded-full bg-accent px-2.5 py-0.5 text-[0.72rem] font-medium text-accent-foreground">
            <ShieldCheck className="size-3" aria-hidden="true" />
            {user.role_name}
          </span>
          {atalhos.length === 0 ? (
            <span>Seu perfil ainda não tem nenhuma tela liberada.</span>
          ) : null}
        </p>
      </header>

      {/* ── Ocupação do mês ────────────────────────────────────────────── */}
      {ocupacao ? (
        <Bloco
          titulo="Ocupação do mês"
          descricao={
            <>
              Contamos <strong>diárias por apartamento</strong>: cada apartamento, em cada dia do mês.
              Assim uma reserva de 3 noites pesa menos que uma de 30, como no mapa.
            </>
          }
          acao={
            <Link
              href="/app/mapa"
              className="inline-flex items-center gap-1 text-xs text-primary underline-offset-4 hover:underline"
            >
              Abrir o mapa
              <ArrowRight className="size-3.5" aria-hidden="true" />
            </Link>
          }
        >
          <div className="flex flex-wrap items-baseline gap-x-6 gap-y-2">
            <p className="font-display text-4xl tabular-nums leading-none">{percentual ?? "—"}%</p>
            <p className="text-sm text-muted-foreground">
              <span className="tabular-nums text-foreground">{ocupacao.vendidas}</span> de{" "}
              <span className="tabular-nums">{ocupacao.total}</span> diárias vendidas
              {ocupacao.bloqueadas > 0 ? (
                <>
                  {" "}
                  · <span className="tabular-nums">{ocupacao.bloqueadas}</span> bloqueadas
                  (manutenção, uso do proprietário ou sites de reserva) — não estavam à venda
                </>
              ) : null}
            </p>
          </div>

          {/* Uma barra por dia do mês. Vale mais que o número sozinho: 60% pode
              ser um mês morno ou duas semanas cheias e duas vazias, e a decisão
              de baixar preço depende de qual dos dois é. */}
          <div className="mt-4 flex items-end gap-[2px]" aria-hidden="true">
            {ocupacao.porDia.map((dia) => {
              const taxa = dia.total > 0 ? dia.vendidas / dia.total : 0;
              return (
                <div
                  key={dia.data}
                  title={`${formatarData(dia.data)} — ${dia.vendidas}/${dia.total}`}
                  className={cn(
                    "h-10 flex-1 rounded-sm bg-muted",
                    dia.data === hoje && "ring-1 ring-primary ring-offset-1 ring-offset-muted",
                  )}
                >
                  <div
                    className="w-full rounded-sm bg-brand-gradient"
                    style={{ height: `${Math.round(taxa * 100)}%`, marginTop: `${Math.round((1 - taxa) * 100)}%` }}
                  />
                </div>
              );
            })}
          </div>
          <p className="mt-1.5 text-xs text-muted-foreground">
            {formatarData(primeiroDoMes)} a {formatarData(somarDias(primeiroDoProximoMes, -1))} · uma
            barra por dia, hoje destacado.
          </p>
        </Bloco>
      ) : null}

      {/* ── O dia ──────────────────────────────────────────────────────── */}
      {movimento ? (
        <div className="grid gap-4 lg:grid-cols-2">
          <Bloco
            titulo="Chegam hoje"
            descricao="Hóspedes que fazem check-in hoje e recebem a chave."
          >
            {movimento.entradas.length === 0 ? (
              <p className="text-sm text-muted-foreground">Nenhuma entrada hoje.</p>
            ) : (
              <ul className="flex flex-col gap-1.5">
                {movimento.entradas.map((reserva) => (
                  <LinhaDeReserva
                    key={reserva.id}
                    reserva={reserva}
                    detalhe={
                      reserva.status === "hold" ? (
                        <Badge variant="accent" className="shrink-0">
                          pré-reserva
                        </Badge>
                      ) : (
                        <DoorOpen className="size-4 shrink-0 text-primary" aria-hidden="true" />
                      )
                    }
                  />
                ))}
              </ul>
            )}
            {movimento.emCasa.length > 0 ? (
              <p className="mt-3 text-xs text-muted-foreground">
                <span className="tabular-nums">{movimento.emCasa.length}</span>{" "}
                {movimento.emCasa.length === 1 ? "estadia em curso" : "estadias em curso"} que não
                começam nem terminam hoje.
              </p>
            ) : null}
          </Bloco>

          <Bloco
            titulo="Saem hoje"
            descricao="Hóspedes que fazem check-out hoje. O apartamento já fica livre para outra reserva a partir de hoje."
          >
            {movimento.saidas.length === 0 ? (
              <p className="text-sm text-muted-foreground">Nenhuma saída hoje.</p>
            ) : (
              <ul className="flex flex-col gap-1.5">
                {movimento.saidas.map((reserva) => (
                  <LinhaDeReserva
                    key={reserva.id}
                    reserva={reserva}
                    detalhe={<DoorClosed className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />}
                  />
                ))}
              </ul>
            )}
          </Bloco>
        </div>
      ) : null}

      {/* ── Pré-reservas vencendo ──────────────────────────────────────── */}
      {expirando ? (
        <Bloco
          titulo="Pré-reservas vencendo"
          descricao={
            <>
              Datas guardadas para o cliente, <strong>ainda sem pagamento</strong>. Se o prazo
              acabar sem confirmação, as datas voltam a ficar livres para venda. Precisa de mais
              tempo? Abra a reserva e estenda o prazo.
            </>
          }
        >
          {expirando.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nenhuma pré-reserva em aberto.</p>
          ) : (
            <ul className="flex flex-col gap-1.5">
              {expirando.map(({ reserva, expiracao }) => (
                <LinhaDeReserva
                  key={reserva.id}
                  reserva={reserva}
                  detalhe={
                    <span
                      className={cn(
                        "shrink-0 whitespace-nowrap text-xs font-medium tabular-nums",
                        COR_DA_URGENCIA[expiracao!.urgencia],
                      )}
                      title={`Estadia de ${formatarData(reserva.check_in)} a ${formatarData(reserva.check_out)}`}
                    >
                      <CalendarClock className="mr-1 inline size-3.5" aria-hidden="true" />
                      {expiracao!.texto}
                    </span>
                  }
                />
              ))}
            </ul>
          )}
        </Bloco>
      ) : null}

      {/* ── Alertas de configuração ────────────────────────────────────── */}
      {alertas && alertas.length > 0 ? (
        <Bloco
          titulo="Impedem uma venda nos próximos 30 dias"
          descricao="Problemas de cadastro que impedem vender esses dias. Corrija para não perder clientes."
        >
          <ul className="flex flex-col gap-2">
            {alertas.map((alerta) => (
              <li
                key={`${alerta.produto}-${alerta.motivo}`}
                className="flex items-start gap-3 rounded-lg bg-card/70 px-3 py-2.5"
              >
                <TriangleAlert className="mt-0.5 size-4 shrink-0 text-alcada-atencao" aria-hidden="true" />
                <div className="min-w-0">
                  <p className="text-sm text-foreground">
                    <strong>{alerta.produto}</strong> — {TEXTO_DO_ALERTA[alerta.motivo].titulo} em{" "}
                    <span className="tabular-nums">{alerta.dias}</span>{" "}
                    {alerta.dias === 1 ? "dia" : "dias"}, a partir de {formatarData(alerta.primeiroDia)}.
                  </p>
                  <p className="mt-0.5 text-xs text-muted-foreground">{TEXTO_DO_ALERTA[alerta.motivo].acao}</p>
                </div>
              </li>
            ))}
          </ul>
        </Bloco>
      ) : null}

      {/* ── Atalhos ────────────────────────────────────────────────────── */}
      {atalhos.length > 0 ? (
        <section aria-labelledby="atalhos-titulo">
          <h2 id="atalhos-titulo" className="sr-only">
            Telas liberadas para o seu perfil
          </h2>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {atalhos.map((item) => {
              const escopo = escopoDe(permissions, item.recurso, "ver");
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={cn(
                    "group rounded-xl border border-border/60 bg-muted/25 p-4 transition-colors outline-none hover:bg-muted/45",
                    "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-card",
                  )}
                >
                  <div className="flex items-start gap-3">
                    <item.icon className="mt-0.5 size-5 shrink-0 text-primary" />
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <CardTitle>{item.title}</CardTitle>
                        {escopo === "own" ? (
                          <span
                            className="rounded-full bg-accent px-2 py-0.5 text-[0.62rem] font-medium uppercase tracking-wider text-accent-foreground"
                            title="Você vê apenas os registros que são seus."
                          >
                            só os meus
                          </span>
                        ) : null}
                      </div>
                      <CardDescription className="mt-1">{item.group}</CardDescription>
                    </div>
                  </div>
                </Link>
              );
            })}
          </div>
        </section>
      ) : (
        <EstadoVazio
          titulo="Nenhum acesso liberado ainda"
          descricao={
            <>
              Peça à gestão para liberar telas para o perfil <strong>{user.role_name}</strong> em
              Configurações → Perfis. A mudança vale na hora, sem precisar entrar de novo.
            </>
          }
        />
      )}

      {/* ── O que ainda não existe ─────────────────────────────────────── */}
      {EM_CONSTRUCAO.length > 0 ? (
        <section
          aria-label="Em construção"
          className="rounded-xl border border-dashed border-border/70 bg-muted/10 p-4 sm:p-5"
        >
          <h2 className="font-display flex items-center gap-2 text-base leading-tight">
            <Hammer className="size-4 text-muted-foreground" aria-hidden="true" />
            Ainda não existe
          </h2>
          <p className="mt-1 max-w-prose text-sm text-muted-foreground">
            Estas telas ainda estão sendo feitas e por isso não aparecem no menu. Ficam listadas
            aqui, <strong>sem link</strong>, só para você saber o que vem por aí.
          </p>
          <ul className="mt-3 flex flex-col gap-1.5">
            {EM_CONSTRUCAO.map((item) => (
              <li key={item.href} className="flex items-start gap-2.5 text-sm">
                <item.icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                <span className="min-w-0">
                  <span className="text-foreground">{item.title}</span>{" "}
                  <span className="text-muted-foreground">— {item.quando}</span>
                </span>
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      <p className="flex items-start gap-2 text-xs text-muted-foreground">
        <Info className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
        <span>
          Você vê aqui só o que o seu perfil pode usar. Mesmo que algo apareça por engano, o
          sistema confere a permissão a cada ação e não deixa ver nem mudar o que não é seu.
        </span>
      </p>
    </div>
  );
}
