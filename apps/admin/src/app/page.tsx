/**
 * Casca de verificação da Fase 0: prova que os tokens, o wash de fundo e as
 * utilities de assinatura estão no ar. A navegação real entra na Fase 1.
 */
export default function Home() {
  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-5xl flex-col gap-6 p-4 sm:p-6">
      <header className="bg-brand-bar flex h-[var(--app-bar-height)] items-center justify-between rounded-2xl px-5 text-white shadow-soft ring-1 ring-white/15">
        <span className="font-display text-sm tracking-[0.28em]">WHITE HOUSE VILLAGE</span>
        <span className="text-xs text-white/70">Gestão comercial</span>
      </header>

      <section className="panel-float flex-1 p-6 sm:p-8">
        <p className="text-xs uppercase tracking-[0.24em] text-muted-foreground">Fase 0 — fundação</p>
        <h1 className="font-display mt-2 text-3xl">O sistema está de pé.</h1>
        <p className="mt-3 max-w-prose text-muted-foreground">
          Casca, tokens da marca e wash de fundo no ar. O núcleo — mapa de ocupação,
          reservas, funil e WhatsApp — entra na Fase 1, contra a API em Go.
        </p>

        <div className="mt-8 grid gap-3 sm:grid-cols-3">
          {[
            { t: "Disponibilidade garantida pelo banco", d: "Overbooking é impossível por constraint, não por boa intenção." },
            { t: "Regra comercial é dado", d: "Tarifa, alçada e cancelamento versionados — decidir depois não custa reescrita." },
            { t: "Tudo por API", d: "CRUD completo e contrato versionado em OpenAPI." },
          ].map((c) => (
            <article key={c.t} className="rounded-xl border border-border/60 bg-muted/20 p-4">
              <h2 className="font-display text-base">{c.t}</h2>
              <p className="mt-1 text-sm text-muted-foreground">{c.d}</p>
            </article>
          ))}
        </div>
      </section>
    </main>
  );
}
