/**
 * Esqueleto com a FORMA da lista, não um spinner (`docs/ui.md` §8).
 *
 * A diferença é de percepção: um esqueleto que já mostra as colunas diz "é isto
 * que está chegando" e deixa o olho se posicionar antes do dado; um spinner diz
 * "espere" e depois joga uma tabela inteira em cima de quem estava olhando para
 * o nada.
 */
export default function CarregandoReservas() {
  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8" aria-busy="true">
      <div className="h-9 w-56 animate-pulse rounded-lg bg-muted" />
      <div className="h-4 w-full max-w-prose animate-pulse rounded bg-muted/70" />

      <div className="flex flex-wrap gap-2">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="h-7 w-28 animate-pulse rounded-full bg-muted" />
        ))}
      </div>

      <div className="overflow-hidden rounded-xl border border-border/60 bg-card/60">
        <div className="h-9 border-b border-border/60 bg-muted/40" />
        {Array.from({ length: 8 }).map((_, i) => (
          <div key={i} className="flex items-center gap-4 border-b border-border/40 px-3 py-3 last:border-b-0">
            <div className="h-4 w-28 animate-pulse rounded bg-muted" style={{ animationDelay: `${i * 40}ms` }} />
            <div className="h-4 flex-1 animate-pulse rounded bg-muted/70" style={{ animationDelay: `${i * 40}ms` }} />
            <div className="h-4 w-40 animate-pulse rounded bg-muted/70" style={{ animationDelay: `${i * 40}ms` }} />
            <div className="h-4 w-24 animate-pulse rounded bg-muted/60" style={{ animationDelay: `${i * 40}ms` }} />
            <div className="h-6 w-24 animate-pulse rounded-full bg-muted" style={{ animationDelay: `${i * 40}ms` }} />
          </div>
        ))}
      </div>
    </div>
  );
}
