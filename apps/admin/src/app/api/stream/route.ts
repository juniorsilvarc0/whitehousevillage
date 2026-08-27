import { repassarStream } from "@/lib/tempo-real/proxy";

/**
 * `GET /api/stream` — o único caminho do painel para o barramento de eventos.
 *
 * Casca de três linhas de propósito: a lógica inteira mora em
 * `@/lib/tempo-real/proxy`, que é pasta de agente. Este arquivo fica sob
 * `src/app/api/`, que não é de ninguém em particular — e mais de um módulo do
 * painel (mapa e CRM) precisa da mesma rota, com os mesmos tópicos. Deixá-lo
 * mínimo é o que faz duas entregas concorrentes tocarem no mesmo caminho sem
 * disputar conteúdo.
 *
 * `force-dynamic` porque a resposta depende de cookie e nunca pode ser
 * pré-renderizada; `nodejs` porque o repasse mantém um `ReadableStream` aberto
 * por minutos, e o runtime de edge do Next tem teto de duração próprio.
 */
export const dynamic = "force-dynamic";
export const runtime = "nodejs";
export const fetchCache = "force-no-store";

export function GET(request: Request): Promise<Response> {
  return repassarStream(request);
}
