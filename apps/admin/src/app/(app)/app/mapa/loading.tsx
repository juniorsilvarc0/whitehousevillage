/**
 * Esqueleto com a FORMA do mapa, não um spinner centralizado (`docs/ui.md` §8).
 *
 * A diferença é de percepção: um esqueleto que já mostra a coluna de unidades e
 * a grade de dias diz "é isto que está chegando" e deixa o olho se posicionar
 * antes do dado; um spinner diz "espere" e depois joga uma tela inteira nova em
 * cima de quem estava olhando para o nada.
 */
export default function CarregandoMapa() {
  return (
    <div className="altura-da-tela-cheia flex min-h-0 flex-col gap-3 p-4 sm:p-6 lg:px-8" aria-busy="true">
      <div className="h-8 w-64 animate-pulse rounded-lg bg-muted" />
      <div className="h-4 w-full max-w-prose animate-pulse rounded bg-muted/70" />

      <div className="flex flex-wrap gap-2">
        <div className="h-8 w-32 animate-pulse rounded-full bg-muted" />
        <div className="h-8 w-36 animate-pulse rounded-xl bg-muted" />
        <div className="h-8 w-28 animate-pulse rounded-xl bg-muted" />
        <div className="h-8 w-48 animate-pulse rounded-xl bg-muted" />
      </div>

      <div className="min-h-0 flex-1 overflow-hidden rounded-xl border border-border/60 bg-card">
        <div className="flex h-full">
          <div className="w-44 shrink-0 border-r border-border/60">
            {Array.from({ length: 11 }).map((_, i) => (
              <div key={i} className="h-10 border-b border-border/40 px-3 py-2.5">
                <div className="h-4 w-24 animate-pulse rounded bg-muted" />
              </div>
            ))}
          </div>
          <div className="min-w-0 flex-1">
            {Array.from({ length: 11 }).map((_, i) => (
              <div key={i} className="flex h-10 items-center gap-1 border-b border-border/40 px-1">
                {Array.from({ length: 24 }).map((__, j) => (
                  <div
                    key={j}
                    className="h-6 flex-1 animate-pulse rounded bg-muted/60"
                    style={{ animationDelay: `${(i + j) * 12}ms` }}
                  />
                ))}
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="h-11 animate-pulse rounded-lg bg-muted/40" />
    </div>
  );
}
