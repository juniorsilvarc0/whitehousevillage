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

// A lista espelha o menu do painel (`src/config/navigation.ts`) mais as telas
// que existem sem estar nele. Tela nova entregue e ausente daqui é tela que
// nunca foi aberta em navegador nenhum — foi assim que `/app/reservas` chegou
// ao fim de uma rodada sem que ninguém notasse que a imagem servida em :3100
// nem a continha.
const TELAS = [
  "/app",
  "/app/mapa",
  "/app/reservas",
  "/app/contatos",
  "/app/funil",
  "/app/leads",
  "/app/orcamento",
  "/app/configuracoes",
  "/app/configuracoes/tarifario",
  "/app/configuracoes/inventario",
  // Inventário de bens por ambiente (Fase 5, `inventory.goods`).
  "/app/inventario",
  "/app/inventario/bens",
  "/app/inventario/conferencias",
  "/app/inventario/avarias",
  // A folha de impressão vive fora da casca (grupo de rotas `(impressao)`).
  // Sem `?unidade=` ela responde 200 com a instrução de onde escolher — o que
  // basta para provar que a rota existe e que a sessão chega até ela.
  "/app/inventario/imprimir",
];

// `channel: "chrome"` usa o Chrome instalado na máquina — é o que mais se
// parece com o navegador do operador, e é o que existe aqui. Num runner de CI
// ele pode não existir, e o Chromium que o Playwright baixa serve: a fumaça
// procura 5xx, 404 e aviso de erro, nada disso depende da marca do navegador.
// Sem este fallback a fumaça não teria como entrar no CI.
const navegador = await chromium
  .launch({ channel: "chrome" })
  .catch(() => chromium.launch());
const pagina = await (await navegador.newContext({ viewport: { width: 1440, height: 950 } })).newPage();

const falhas = [];
pagina.on("response", (r) => {
  const rota = r.url().replace(BASE, "");
  if (r.status() >= 500) {
    falhas.push(`${r.status()} ${r.request().method()} ${rota}`);
    return;
  }
  // Prefetch de RSC com 404 é destino de MENU que não existe. Já foi ruído
  // tolerado aqui ("não é falha de aplicação") enquanto o menu anunciava 13
  // destinos para 5 telas; a rodada de 27/08 pagou essa dívida e a contagem
  // medida caiu de 9 por visita ao /app para 0. Manter a isenção devolveria o
  // buraco em silêncio no dia em que alguém recolocasse um link morto — e o
  // custo dele é real: o menu inteiro fica na barra do desktop, então cada
  // visita dispara a rajada de 404 antes de qualquer clique.
  if (r.status() === 404 && r.url().includes("_rsc=")) {
    falhas.push(`link de menu para tela inexistente (prefetch 404): ${rota.split("?")[0]}`);
  }
});
pagina.on("pageerror", (e) => falhas.push(`erro de página: ${String(e).slice(0, 120)}`));

// Cada `EventSource` aberto é uma requisição a `/api/stream`. A contagem é o
// único jeito de enxergar a reconexão em laço: ela não produz erro, não produz
// 5xx e não muda nada na tela — só multiplica conexões contra a API. Ver o
// bloco "o funil ao vivo" no fim deste arquivo.
const aberturasDoStream = [];
pagina.on("request", (r) => {
  if (r.url().includes("/api/stream")) aberturasDoStream.push(Date.now());
});

// O que o BFF respondeu ao login fica guardado para o caso de o login falhar.
// Sem isto a única pista é "o login não saiu de /login", que não distingue
// credencial recusada, API fora do ar e navegação que não completou — e o
// login é o passo que, quando quebra, faz TODAS as telas seguintes reprovarem
// pela mesma razão, escondendo o que quer que esteja quebrado nelas.
let respostaDoLogin = "nenhuma resposta de /api/auth/login foi observada";
pagina.on("response", async (r) => {
  if (!r.url().includes("/api/auth/login")) return;
  const corpo = await r.text().catch(() => "");
  respostaDoLogin = `${r.status()} ${corpo.replace(/\s+/g, " ").slice(0, 200)}`;
});

await pagina.goto(`${BASE}/login`, { waitUntil: "domcontentloaded" });

// Preencher e CONFERIR que ficou preenchido, antes de clicar.
//
// `domcontentloaded` chega antes da hidratação do React, e um input controlado
// hidratado depois volta ao valor inicial — vazio. Observado aqui numa execução
// em que o contêiner do painel tinha acabado de reiniciar: os dois `fill`
// rodaram, o clique saiu, nenhuma requisição para /api/auth/login foi observada,
// e a tela mostrava "Informe o e-mail. Informe a senha.". Sem esta conferência o
// diagnóstico vira "o login não saiu de /login", que é o sintoma de meia dúzia
// de causas diferentes.
const email = process.env.SMOKE_EMAIL ?? "admin@wh.local";
const senha = process.env.SMOKE_SENHA ?? "whv@2026";
let preenchido = false;
for (let tentativa = 1; tentativa <= 3 && !preenchido; tentativa++) {
  await pagina.fill('input[type="email"]', email);
  await pagina.fill('input[type="password"]', senha);
  preenchido = await pagina
    .waitForFunction(
      ([e, s]) =>
        document.querySelector('input[type="email"]')?.value === e &&
        document.querySelector('input[type="password"]')?.value === s,
      [email, senha],
      { timeout: 3000 },
    )
    .then(() => true)
    .catch(() => false);
}
if (!preenchido) {
  falhas.push(
    "o formulário de login perdeu o que foi digitado em 3 tentativas — a hidratação " +
      "do painel está limpando os campos, e clicar em Entrar agora enviaria um formulário vazio",
  );
}
await pagina.click('button[type="submit"]');
// Espera pela URL DENTRO da página, e não por `waitForURL`.
//
// Medido nesta rodada: o login do painel é navegação de MESMO documento (o
// router do Next empurra a rota depois da Server Action), e `waitForURL`
// depende do `waitUntil` — `load` pode não chegar por causa da conexão SSE que
// a tela de destino abre, e `commit`, que seria a correção óbvia, NUNCA
// dispara numa navegação soft. As duas variantes estouraram 20 s aqui com o
// login funcionando: o navegador estava em /app o tempo todo.
//
// `waitForFunction` não tem esse eixo: pergunta ao próprio documento onde ele
// está, e responde igual para navegação soft e hard.
await pagina
  .waitForFunction(() => !location.pathname.startsWith("/login"), null, { timeout: 20000 })
  .catch(async () => {
    const visivel = await pagina.locator("body").innerText().catch(() => "");
    falhas.push(
      `o login não saiu de /login — /api/auth/login respondeu [${respostaDoLogin}]; ` +
        `tela: "${visivel.replace(/\s+/g, " ").slice(0, 160)}"`,
    );
  });

// Todo link para dentro do painel que apareceu em alguma tela, e de onde veio.
//
// A conferência de prefetch acima só pega o link que o Next resolveu buscar —
// e ele só prefetcha o que entra no viewport. Um destino morto num menu
// recolhido, numa aba não aberta ou numa linha de tabela abaixo da dobra
// atravessa. Esta coleta pergunta ao DOM quais links EXISTEM e, no fim, bate em
// todos eles. Foi assim que `/app/reservas/{id}` ficou uma rodada inteira como
// link morto no meio do fluxo de venda: o card do CRM já apontava para ele
// (`components/crm/oportunidade.tsx`) e a tela não existia.
const destinos = new Map();
const registrarDestinos = async (origem) => {
  const hrefs = await pagina
    .$$eval("a[href]", (as) => as.map((a) => a.getAttribute("href") ?? ""))
    .catch(() => []);
  for (const href of hrefs) {
    // Só o que é navegação interna do painel: nada de âncora, mailto, tel,
    // externo ou o `#` de botão disfarçado de link.
    if (!href.startsWith("/app") || href.startsWith("//")) continue;
    const limpo = href.split("#")[0];
    if (!destinos.has(limpo)) destinos.set(limpo, origem);
  }
};

for (const rota of TELAS) {
  const resposta = await pagina
    .goto(BASE + rota, { waitUntil: "domcontentloaded" })
    .catch((e) => {
      falhas.push(`${rota}: ${e.message.slice(0, 80)}`);
      return null;
    });

  // O status do DOCUMENTO, e não só 5xx. Sem isto uma tela que não existe
  // responde 404, desenha o "This page could not be found" do Next — que não é
  // 5xx e não traz aviso de erro do painel — e a fumaça a dá por boa. Era o
  // caso de `/app/reservas` até esta rodada.
  if (resposta && resposta.status() >= 400) {
    falhas.push(`${rota}: a tela respondeu ${resposta.status()}`);
  }

  await pagina.waitForTimeout(800);

  // Sessão perdida no meio da varredura leva toda tela protegida de volta ao
  // login, que responde 200 e sem aviso: sem esta conferência o resto da lista
  // passaria verde percorrendo dez vezes a mesma tela de login.
  if (new URL(pagina.url()).pathname.startsWith("/login")) {
    falhas.push(`${rota}: voltou para /login — a sessão não sobreviveu`);
  }

  // A tela pode responder 200 e ainda assim estar quebrada: o painel mostra o
  // erro num aviso, e é ele que o usuário vê.
  const aviso = await pagina.locator("text=/Não foi possível|Algo deu errado/i").first()
    .textContent({ timeout: 500 }).catch(() => null);
  if (aviso) falhas.push(`${rota}: a tela abriu com aviso de erro — "${aviso.trim().slice(0, 80)}"`);

  // Nenhum link de ligação ou de WhatsApp com máscara (dívida D11).
  //
  // As coleções que embutem contato devolvem o telefone mascarado
  // (`+*********0000`) — a lista de contatos e os leads, até no detalhe. Até a
  // D11 o card do lead montava `tel:` com a máscara, e o botão discava
  // asteriscos; nenhum teste via, porque todos usavam telefone cheio. O teste
  // de componente cobre o card; esta conferência cobre o que ele não alcança:
  // qualquer tela, com o dado que o banco servido tem.
  const ligacoesMascaradas = await pagina
    .$$eval('a[href^="tel:"], a[href*="wa.me/"]', (as) =>
      as
        .map((a) => a.getAttribute("href") ?? "")
        // No `wa.me` só o número conta: o `?text=` é a mensagem, não o destino.
        .filter((href) => /\*|%2A/i.test(href.startsWith("tel:") ? href : href.split("?")[0])),
    )
    .catch(() => []);
  for (const href of ligacoesMascaradas) {
    falhas.push(`${rota}: link de ligação com telefone mascarado — ${href.slice(0, 60)} (dívida D11)`);
  }

  await registrarDestinos(rota);
  console.log(`  ${rota}`);
}

// ── Nenhum link do painel pode levar a 404 ──────────────────────────────────
//
// Vale para o menu e para tudo mais que a tela oferecer: o operador não sabe
// distinguir "botão do menu" de "link da tabela" — ele clica, e o Next desenha
// "This page could not be found", que não é 5xx e não é aviso do painel.
//
// `pagina.request` reaproveita os cookies da sessão, então o que se mede é o
// que o usuário logado receberia. As telas já visitadas saem da lista: elas
// acabaram de ser conferidas com o navegador, e repetir custa tempo sem
// acrescentar informação.
const jaVisitadas = new Set(TELAS);
const candidatos = [...destinos.entries()].filter(([alvo]) => !jaVisitadas.has(alvo));

// Um link por FORMA de rota primeiro, e só então o resto — com teto.
//
// `/app/contatos/<uuid>` é uma rota só; conferir os vinte contatos da página
// mede vinte vezes a mesma coisa e faz a fumaça crescer com o volume do banco,
// que é o jeito mais fácil de ela virar lenta e ser desligada. Ordenando por
// forma, o teto nunca corta uma rota inteira — corta repetição.
const forma = (alvo) =>
  alvo
    .split("?")[0]
    .replace(/\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/gi, "/{id}")
    .replace(/\/\d+/g, "/{n}");

const TETO_DE_LINKS = 60;
const vistas = new Set();
const primeiros = [];
const demais = [];
for (const par of candidatos) {
  const f = forma(par[0]);
  if (vistas.has(f)) demais.push(par);
  else {
    vistas.add(f);
    primeiros.push(par);
  }
}
const paraConferir = [...primeiros, ...demais].slice(0, TETO_DE_LINKS);
if (candidatos.length > TETO_DE_LINKS) {
  console.log(
    `  (${candidatos.length} links encontrados, conferindo ${TETO_DE_LINKS}; ` +
      `as ${vistas.size} formas de rota distintas estão todas cobertas)`,
  );
}

console.log(`\n  ${paraConferir.length} link(s) interno(s) a conferir (${vistas.size} forma(s) de rota)`);
for (const [alvo, origem] of paraConferir) {
  const r = await pagina.request.get(BASE + alvo, { failOnStatusCode: false }).catch(() => null);
  if (!r) {
    falhas.push(`link morto ${alvo} (oferecido em ${origem}): a requisição nem completou`);
    continue;
  }
  if (r.status() >= 400) {
    falhas.push(`link morto: ${origem} oferece ${alvo}, que responde ${r.status()}`);
    continue;
  }
  console.log(`  ${alvo}  (de ${origem})`);
}

// ── O funil ao vivo: pelo menos uma conexão, e no máximo duas ──────────────
//
// Duas coisas que nenhum teste de componente alcança, e que já aconteceram:
//
// 1. **Zero conexões.** Foi o estado da dívida D10 por uma fase inteira: o
//    barramento servido, os gatilhos publicando, o tópico `crm` com RBAC
//    próprio — e ninguém assinando. Medido em 31/08 contra o painel servido:
//    duas abas no funil, uma move o card, a outra continua mostrando a coluna
//    antiga, e `/api/stream` recebe **0** requisições. Um teto sozinho passaria
//    verde nesse estado, então o piso é metade da guarda.
//
// 2. **Conexões demais.** `criarFonte` entrou uma vez na lista de dependências
//    do efeito do SSE, e a identidade do valor default de um parâmetro não
//    sobrevive à minificação: em `next dev` uma conexão, no build servido
//    **1957 em 9 segundos**. A suíte inteira ficou verde durante o defeito
//    porque todo teste passava uma fábrica estável de módulo. Só a aplicação
//    servida mostra isso, e só contando.
//
// 30 s parados: o defeito de reconexão aparece em dois, mas uma escada de
// backoff mal fechada leva dezenas — e 30 s é o intervalo em que o teto de 2
// separa "reconectou uma vez" de "está reconectando sozinho".
const JANELA_DO_FUNIL_MS = 30_000;
const marcoDoStream = aberturasDoStream.length;

console.log(`\n  medindo o tempo real do funil por ${JANELA_DO_FUNIL_MS / 1000}s`);
await pagina.goto(`${BASE}/app/funil`, { waitUntil: "domcontentloaded" });
const quadroDePe = await pagina
  .waitForSelector('li[aria-label^="Etapa "]', { timeout: 20000 })
  .then(() => true)
  .catch(() => false);

if (!quadroDePe) {
  falhas.push("/app/funil: o quadro não desenhou nenhuma coluna — não há o que medir de tempo real");
} else {
  await pagina.waitForTimeout(JANELA_DO_FUNIL_MS);
  const abertas = aberturasDoStream.length - marcoDoStream;
  if (abertas < 1) {
    falhas.push(
      "/app/funil: o quadro não abriu conexão nenhuma com /api/stream — " +
        "o funil voltou a não ter tempo real (dívida D10)",
    );
  } else if (abertas > 2) {
    falhas.push(
      `/app/funil: ${abertas} aberturas de /api/stream em ${JANELA_DO_FUNIL_MS / 1000}s com a tela parada — ` +
        "a conexão está reabrindo sozinha (o defeito de dependência do efeito do SSE)",
    );
  } else {
    console.log(`  /app/funil ao vivo com ${abertas} abertura(s) de /api/stream em ${JANELA_DO_FUNIL_MS / 1000}s`);
  }
}

await navegador.close();
if (falhas.length) {
  console.error("\nFALHOU:");
  falhas.forEach((f) => console.error("  ! " + f));
  process.exit(1);
}
console.log(
  "\n  fumaça ok — login, telas, TODO link interno sem 5xx/404/aviso de erro, e o funil ao vivo com uma conexão",
);
