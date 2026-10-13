/**
 * Esqueleto com a FORMA da agenda — a barra de visões, o período e a grade de
 * horas — e não um spinner centralizado (`docs/ui.md` §8).
 */
export default function CarregandoAgenda() {
  return (
    <div className="flex flex-col gap-4 p-4 sm:p-6 lg:p-8" aria-busy="true">
      <div className="h-8 w-40 animate-pulse rounded-lg bg-muted" />
      <div className="h-4 w-full max-w-prose animate-pulse rounded bg-muted/70" />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="h-10 w-full animate-pulse rounded-full bg-muted sm:w-80" />
        <div className="h-10 w-full animate-pulse rounded-full bg-muted/70 sm:w-48" />
      </div>
      <div className="h-6 w-56 animate-pulse rounded bg-muted/70" />
      <div className="overflow-hidden rounded-xl border border-border/60 bg-card">
        <div className="flex h-12 border-b border-border/60">
          <div className="w-12 shrink-0" />
          {Array.from({ length: 7 }).map((_, i) => (
            <div key={i} className={i === 0 ? "flex flex-1 items-center justify-center border-l border-border/40" : "hidden flex-1 items-center justify-center border-l border-border/40 sm:flex"}>
              <div className="h-6 w-8 animate-pulse rounded bg-muted" />
            </div>
          ))}
        </div>
        {Array.from({ length: 10 }).map((_, i) => (
          <div key={i} className="flex h-12 border-b border-border/30">
            <div className="w-12 shrink-0 px-2 pt-1">
              <div className="h-3 w-8 animate-pulse rounded bg-muted/70" />
            </div>
            <div className="flex-1 border-l border-border/40 p-1">
              {i % 3 === 1 ? (
                <div className="h-8 w-3/4 animate-pulse rounded-md bg-muted/60" style={{ animationDelay: `${i * 40}ms` }} />
              ) : null}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
