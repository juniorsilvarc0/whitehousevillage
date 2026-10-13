"use client";

import * as React from "react";
import Link from "next/link";

import { BlocoDaGrade, ChipDoEvento } from "@/components/agenda/cartao-do-evento";
import {
  bloqueiosNoDia,
  diaInteiroNoDia,
  dispor,
  faixaDeHoras,
  fechadoNoDia,
  abertoNoDia,
  trechoNoDia,
  type Trecho,
} from "@/lib/agenda/grade";
import { dataLonga, diaEMes, numeroDoDia, semanaCurta } from "@/lib/agenda/rotulos";
import type { BloqueioDeHorario, EventoDaAgenda, IntervaloDeFuncionamento } from "@/lib/agenda/tipos";
import { cn } from "@/lib/utils";

/**
 * A grade de horários — as visões de **dia** (uma coluna) e de **semana**
 * (sete). Grade própria, sem biblioteca de calendário (`docs/ui.md` §9).
 *
 * - **Fundo**: o que está fora do horário de funcionamento do dia (dia sem
 *   intervalo é dia fechado) e os bloqueios de horário da operação, que vêm
 *   de `GET /agenda/settings` e `GET /agenda/blocks` — restrições, não eventos.
 * - **Faixa de dia inteiro**: manutenção, bloqueio de unidade e evento de dia
 *   inteiro, que não têm hora para ocupar.
 * - **Eventos**: posicionados pela hora de parede da casa e dispostos em
 *   colunas quando se sobrepõem.
 * - **Hoje** é o `hoje` que chega por prop (o `today` do servidor), nunca o
 *   relógio do navegador.
 *
 * No celular, a semana rola de lado **dentro** do próprio contêiner (cada dia
 * com largura mínima); o dia ocupa a largura toda.
 */

/** A altura mínima de um cartão, em minutos: abaixo disso o título não cabe.
 *  É também o que o layout de colisão usa como tamanho desenhado. */
const MINIMO_VISUAL = 30;

export function GradeDeHorarios({
  dias,
  hoje,
  eventos,
  bloqueios,
  horarios,
  passoEmMinutos,
  aoAbrir,
  linkDoDia,
}: {
  dias: readonly string[];
  hoje: string;
  eventos: readonly EventoDaAgenda[];
  bloqueios: readonly BloqueioDeHorario[];
  horarios: readonly IntervaloDeFuncionamento[];
  /** `slot_minutes` — o passo das linhas da grade. */
  passoEmMinutos: number;
  aoAbrir: (evento: EventoDaAgenda) => void;
  /** Na semana, o cabeçalho do dia leva à visão daquele dia. */
  linkDoDia?: (dia: string) => string;
}) {
  const porDia = React.useMemo(
    () =>
      dias.map((dia) => {
        const trechos = eventos.map((e) => trechoNoDia(e, dia)).filter((t): t is Trecho => t !== null);
        return {
          dia,
          trechos,
          diaInteiro: diaInteiroNoDia(eventos, dia),
          bloqueios: bloqueiosNoDia(bloqueios, dia),
        };
      }),
    [dias, eventos, bloqueios],
  );

  const faixa = React.useMemo(
    () =>
      faixaDeHoras(
        dias,
        horarios,
        porDia.flatMap((d) => d.trechos),
        porDia.flatMap((d) => d.bloqueios.filter((b) => !b.diaInteiro)),
      ),
    [dias, horarios, porDia],
  );

  const horas = [];
  for (let m = faixa.inicio; m < faixa.fim; m += 60) horas.push(m);
  const alturaEmHoras = (faixa.fim - faixa.inicio) / 60;
  const temFaixaDeDiaInteiro = porDia.some((d) => d.diaInteiro.length > 0 || d.bloqueios.some((b) => b.diaInteiro));
  const semana = dias.length > 1;

  return (
    <div
      className="agenda agenda-rolagem rounded-xl border border-border/60 bg-card/60"
      style={
        {
          "--agenda-dias": dias.length,
          "--agenda-largura-min": semana ? "6.5rem" : "0px",
          // A linha do passo, nunca mais densa que 15 min: com o passo de 5, a
          // 48px por hora, seria uma linha a cada 4px — fundo, não grade.
          "--agenda-passo-fracao": Math.min(1, Math.max(passoEmMinutos, 15) / 60),
        } as React.CSSProperties
      }
    >
      <div style={semana ? { minWidth: "calc(var(--agenda-rotulo) + 7 * 6.5rem)" } : undefined}>
        {/* Cabeçalho dos dias */}
        <div className="agenda-colunas border-b border-border/60">
          <div aria-hidden="true" />
          {dias.map((dia) => {
            const ehHoje = dia === hoje;
            const fechado = abertoNoDia(horarios, dia).length === 0;
            const conteudo = (
              <>
                <span className="text-[0.6875rem] uppercase tracking-wide text-muted-foreground">{semanaCurta(dia)}</span>
                <span
                  className={cn(
                    "font-display grid size-7 place-items-center rounded-full text-sm tabular-nums",
                    ehHoje && "bg-brand-gradient text-white",
                  )}
                >
                  {numeroDoDia(dia)}
                </span>
                {fechado ? <span className="text-[0.625rem] text-muted-foreground">fechado</span> : null}
              </>
            );
            return (
              <div
                key={dia}
                className="flex flex-col items-center gap-0.5 border-l border-border/40 py-1.5"
                aria-current={ehHoje ? "date" : undefined}
                data-dia={dia}
              >
                {linkDoDia ? (
                  <Link
                    href={linkDoDia(dia)}
                    className="flex flex-col items-center gap-0.5 rounded-lg px-2 outline-none hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring"
                    aria-label={`Abrir ${dataLonga(dia)}`}
                  >
                    {conteudo}
                  </Link>
                ) : (
                  conteudo
                )}
              </div>
            );
          })}
        </div>

        {/* Faixa de dia inteiro */}
        {temFaixaDeDiaInteiro ? (
          <div className="agenda-colunas border-b border-border/60">
            <div className="px-1 py-1.5 text-right text-[0.625rem] leading-tight text-muted-foreground">dia todo</div>
            {porDia.map(({ dia, diaInteiro, bloqueios: bs }) => (
              <div key={dia} className="flex min-w-0 flex-col gap-1 border-l border-border/40 p-1" data-faixa-dia-inteiro={dia}>
                {bs
                  .filter((b) => b.diaInteiro)
                  .map((b) => (
                    <p
                      key={b.bloqueio.id}
                      className="agenda-bloqueio-de-horario truncate rounded-md px-1.5 py-0.5 text-[0.6875rem]"
                      title={`Horário bloqueado: ${b.bloqueio.reason}`}
                    >
                      {b.bloqueio.reason}
                    </p>
                  ))}
                {diaInteiro.map((e) => (
                  <ChipDoEvento key={e.id} evento={e} aoAbrir={aoAbrir} />
                ))}
              </div>
            ))}
          </div>
        ) : null}

        {/* A grade de horas */}
        <div className="agenda-colunas">
          <div className="relative" style={{ height: `calc(${alturaEmHoras} * var(--agenda-hora))` }} aria-hidden="true">
            {horas.map((m) => (
              <span
                key={m}
                className="absolute right-1.5 -translate-y-1/2 text-[0.625rem] tabular-nums text-muted-foreground first:translate-y-0"
                style={{ top: `calc(${(m - faixa.inicio) / 60} * var(--agenda-hora))` }}
              >
                {String(m / 60).padStart(2, "0")}:00
              </span>
            ))}
          </div>
          {porDia.map(({ dia, trechos, bloqueios: bs }) => {
            const dispostos = dispor(trechos, MINIMO_VISUAL);
            return (
              <section
                key={dia}
                aria-label={`${dataLonga(dia)}${dia === hoje ? " (hoje)" : ""}`}
                className="agenda-pista"
                data-hoje={dia === hoje ? "1" : undefined}
                data-dia={dia}
                style={{ height: `calc(${alturaEmHoras} * var(--agenda-hora))` }}
              >
                {fechadoNoDia(horarios, dia, faixa).map((f) => (
                  <div
                    key={`fechado-${f.inicio}`}
                    className="agenda-fora-do-horario"
                    data-fora-do-horario=""
                    aria-hidden="true"
                    style={{
                      top: `calc(${(f.inicio - faixa.inicio) / 60} * var(--agenda-hora))`,
                      height: `calc(${(f.fim - f.inicio) / 60} * var(--agenda-hora))`,
                    }}
                  />
                ))}
                {bs
                  .filter((b) => !b.diaInteiro)
                  .map((b) => (
                    <div
                      key={b.bloqueio.id}
                      className="agenda-bloqueio-de-horario"
                      data-bloqueio-de-horario={b.bloqueio.id}
                      title={`Horário bloqueado: ${b.bloqueio.reason}`}
                      style={{
                        top: `calc(${(Math.max(b.inicio, faixa.inicio) - faixa.inicio) / 60} * var(--agenda-hora))`,
                        height: `calc(${(Math.min(b.fim, faixa.fim) - Math.max(b.inicio, faixa.inicio)) / 60} * var(--agenda-hora))`,
                      }}
                    >
                      <span className="block truncate px-1 text-[0.625rem] text-muted-foreground">{b.bloqueio.reason}</span>
                    </div>
                  ))}
                {dispostos.length === 0 ? (
                  <span className="sr-only">Nada marcado em {diaEMes(dia)}.</span>
                ) : (
                  dispostos.map((t) => (
                    <BlocoDaGrade
                      key={`${t.evento.id}-${dia}`}
                      trecho={t}
                      inicioDaFaixa={faixa.inicio}
                      minimoVisual={MINIMO_VISUAL}
                      aoAbrir={aoAbrir}
                    />
                  ))
                )}
              </section>
            );
          })}
        </div>
      </div>
    </div>
  );
}
