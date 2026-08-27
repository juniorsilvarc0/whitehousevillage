// Fumaça de aplicação: sobe o navegador, faz login e percorre as telas.
//
// Existe porque a suíte inteira estava verde e o stack não subia: a imagem do
// painel morria no boot por dependência não copiada, o BFF respondia 502 porque
// a URL da API tinha sido embutida em tempo de build, e `GET /crm/pipelines`
// devolvia 500 por um parâmetro sem tipo. Nenhum teste via nada disso — todos
// rodam contra o código, não contra a aplicação servida.
//
// Uso: node e2e/fumaca.mjs [http://localhost:3100]
import { chromium } from "playwright";

// `networkidle` NÃO serve aqui: o painel mantém uma conexão SSE aberta em toda
// tela (o mapa ao vivo), então a rede nunca fica ociosa e todo goto estoura o
// tempo. Esperar por `domcontentloaded` e dar uma folga é o que corresponde ao
// que o usuário vê.

const BASE = process.argv[2] ?? "http://localhost:3100";
const TELAS = ["/app", "/app/mapa", "/app/funil", "/app/leads", "/app/orcamento",
               "/app/configuracoes/tarifario", "/app/configuracoes/inventario"];

const navegador = await chromium.launch({ channel: "chrome" });
const pagina = await (await navegador.newContext({ viewport: { width: 1440, height: 950 } })).newPage();

const falhas = [];
pagina.on("response", (r) => {
  // 404 de prefetch RSC é destino de menu ainda não construído: ruído conhecido,
  // não falha de aplicação. 5xx é sempre falha.
  if (r.status() >= 500) falhas.push(`${r.status()} ${r.request().method()} ${r.url().replace(BASE, "")}`);
});
pagina.on("pageerror", (e) => falhas.push(`erro de página: ${String(e).slice(0, 120)}`));

await pagina.goto(`${BASE}/login`, { waitUntil: "domcontentloaded" });
await pagina.fill('input[type="email"]', process.env.SMOKE_EMAIL ?? "admin@wh.local");
await pagina.fill('input[type="password"]', process.env.SMOKE_SENHA ?? "whv@2026");
await pagina.click('button[type="submit"]');
await pagina.waitForURL((u) => !u.pathname.startsWith("/login"), { timeout: 20000 })
  .catch(() => falhas.push("o login não saiu de /login"));

for (const rota of TELAS) {
  await pagina.goto(BASE + rota, { waitUntil: "domcontentloaded" }).catch((e) => falhas.push(`${rota}: ${e.message.slice(0, 80)}`));
  await pagina.waitForTimeout(800);
  // A tela pode responder 200 e ainda assim estar quebrada: o painel mostra o
  // erro num aviso, e é ele que o usuário vê.
  const aviso = await pagina.locator("text=/Não foi possível|Erro no servidor/i").first()
    .textContent({ timeout: 500 }).catch(() => null);
  if (aviso) falhas.push(`${rota}: a tela abriu com aviso de erro — "${aviso.trim().slice(0, 80)}"`);
  console.log(`  ${rota}`);
}

await navegador.close();
if (falhas.length) {
  console.error("\nFALHOU:");
  falhas.forEach((f) => console.error("  ! " + f));
  process.exit(1);
}
console.log("\n  fumaça ok — login e telas sem 5xx nem aviso de erro");
