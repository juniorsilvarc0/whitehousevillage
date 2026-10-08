// E2E — a conferência de bens pelo CELULAR (Fase 5, `inventory.goods`).
//
// A jornada de quem limpa e conta, dentro do apartamento, num viewport de
// celular: abrir a conferência, contar cômodo a cômodo, tentar fechar com
// pendência, ir ao cômodo que a recusa indica, terminar, fechar, ver a apuração
// e RECARREGAR a página — o resultado tem de sobreviver ao recarregar, porque
// vem do `result` da conferência fechada e não da memória da tela.
//
// Roda contra a aplicação SERVIDA (painel + API), como `apps/admin/e2e/fumaca.mjs`:
//
//   E2E_PAINEL=http://localhost:3100 E2E_API=http://localhost:8080 \
//     node tests/e2e/bens-contagem-celular.mjs
//
// Contas: as de desenvolvimento do seed (`admin@wh.local` prepara os dados pela
// API; `gestao@wh.local`, perfil `usuario`, conta pelo celular). Senha em
// E2E_SENHA (padrão: a do seed de desenvolvimento).
//
// Sem `sleep`: toda espera é por condição (o texto do rodapé, a URL, o toast,
// a seção do resultado). O contador grava ~350 ms depois do último toque; o
// teste espera o RODAPÉ refletir a resposta do servidor, não um relógio.
//
// O que fica no banco: uma unidade `E2E-xxxxxx` com três cômodos, quatro bens,
// uma conferência fechada e duas avarias. A unidade é desativada no fim.

import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const require = createRequire(new URL("../../apps/admin/package.json", import.meta.url));
const { chromium } = require("playwright");

const PAINEL = (process.env.E2E_PAINEL ?? "http://localhost:3100").replace(/\/+$/, "");
const API = (process.env.E2E_API ?? "http://localhost:8080").replace(/\/+$/, "") + "/api/v1";
const SENHA = process.env.E2E_SENHA ?? "whv@2026";
const ADMIN = process.env.E2E_ADMIN_EMAIL ?? "admin@wh.local";
const OPERADOR = process.env.E2E_OPERADOR_EMAIL ?? "gestao@wh.local";
const SAIDA = process.env.E2E_SAIDA ?? mkdtempSync(join(tmpdir(), "e2e-bens-"));
const ESPERA = 20_000;

// ─────────────────────────── API (preparação e conferência) ─────────────────

async function api(metodo, caminho, token, corpo) {
  const r = await fetch(API + caminho, {
    method: metodo,
    headers: { "Content-Type": "application/json", ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: corpo === undefined ? undefined : JSON.stringify(corpo),
  });
  const texto = await r.text();
  let json = null;
  try {
    json = texto ? JSON.parse(texto) : null;
  } catch {
    /* corpo não-JSON (CSV) */
  }
  return { status: r.status, json, texto };
}

async function exigir(promessa, status, oQue) {
  const r = await promessa;
  assert.equal(r.status, status, `${oQue}: status ${r.status} — ${r.texto}`);
  return r.json?.data;
}

async function entrarNaApi(email) {
  const d = await exigir(api("POST", "/auth/login", "", { email, password: SENHA }), 200, `login de ${email} na API`);
  return d.access_token;
}

const sufixo = Math.random().toString(36).slice(2, 8).toUpperCase();
const NOMES = {
  prato: `Prato raso ${sufixo}`,
  taca: `Taça de vinho ${sufixo}`,
  toalha: `Toalha de banho ${sufixo}`,
  travesseiro: `Travesseiro ${sufixo}`,
};

async function prepararCasa(token) {
  const unidade = await exigir(
    api("POST", "/units", token, { code: `E2E-${sufixo}`, name: `E2E Celular ${sufixo}`, sort_order: 999 }),
    201,
    "criando a unidade",
  );
  const comodo = async (name, kind, sort_order) =>
    (await exigir(api("POST", "/rooms", token, { unit_id: unidade.id, name, kind, sort_order }), 201, `cômodo ${name}`)).id;
  const bem = async (name, category, custo) =>
    (
      await exigir(
        api("POST", "/inventory/items", token, { name, category, ...(custo ? { replacement_cost_cents: custo } : {}) }),
        201,
        `bem ${name}`,
      )
    ).id;
  const colocar = (room_id, item_id, expected_qty) =>
    exigir(api("POST", "/inventory/placements", token, { room_id, item_id, expected_qty }), 201, "colocação");

  const cozinha = await comodo("Cozinha", "cozinha", 1);
  const banheiro = await comodo("Banheiro", "banheiro", 2);
  const quarto = await comodo("Quarto", "quarto", 3);
  const ids = {
    prato: await bem(NOMES.prato, "louca", 1890),
    taca: await bem(NOMES.taca, "copo", 2500),
    toalha: await bem(NOMES.toalha, "banho", null),
    travesseiro: await bem(NOMES.travesseiro, "cama", 6000),
  };
  await colocar(cozinha, ids.prato, 12);
  await colocar(cozinha, ids.taca, 6);
  await colocar(banheiro, ids.toalha, 4);
  await colocar(quarto, ids.travesseiro, 2);
  return { unidade, ids };
}

// ─────────────────────────── Navegador ──────────────────────────────────────

const falhas = [];
let pagina;

async function passo(nome, fn) {
  const inicio = Date.now();
  try {
    await fn();
    console.log(`  ok   ${nome} (${Date.now() - inicio} ms)`);
  } catch (e) {
    const arquivo = join(SAIDA, `falha-${falhas.length + 1}.png`);
    await pagina?.screenshot({ path: arquivo, fullPage: true }).catch(() => {});
    falhas.push(`${nome}: ${e.message}`);
    console.log(`  FALHA ${nome}\n        ${e.message.split("\n").join("\n        ")}\n        tela: ${arquivo}`);
    throw e;
  }
}

/** Espera o caminho da página pelo próprio documento: o login e o
 *  `router.push` do painel são navegação SOFT, e `waitForURL` depende de um
 *  `load` que a conexão SSE das telas pode nunca deixar chegar. */
async function esperarCaminho(condicao) {
  await pagina.waitForFunction(`(${condicao.toString()})(location.pathname)`, null, { timeout: ESPERA });
}

/** Espera o React anexar os handlers ao elemento (hidratação). Em modo dev a
 *  hidratação passa de segundos; antes dela, o clique em "Entrar" vira um
 *  submit nativo que recarrega a tela com os campos vazios. */
async function esperarHidratacao(seletor) {
  await pagina.waitForFunction(
    (sel) => {
      const el = document.querySelector(sel);
      return !!el && Object.keys(el).some((k) => k.startsWith("__reactProps"));
    },
    seletor,
    { timeout: 60_000 },
  );
}

/** Clica até `aparece` ficar visível — o botão renderizado no servidor só
 *  ganha o handler na hidratação. Espera por condição, sem relógio. */
async function clicarAte(alvo, aparece, tentativas = 5) {
  for (let i = 1; i <= tentativas; i++) {
    await alvo.click();
    const ok = await aparece
      .waitFor({ state: "visible", timeout: 3000 })
      .then(() => true)
      .catch(() => false);
    if (ok) return;
  }
  throw new Error(`clicar não fez aparecer o esperado depois de ${tentativas} tentativas`);
}

/** O rodapé da contagem diz "<n> de <total> contados · <p> sem contar". */
function rodape() {
  return pagina.locator('p[aria-live="polite"]').filter({ hasText: "contados" });
}

async function esperarRodape(contados, total) {
  await rodape()
    .filter({ hasText: new RegExp(`^\\s*${contados}\\s*de\\s*${total}\\s*contados`) })
    .filter({ hasNotText: "salvando" })
    .waitFor({ timeout: ESPERA });
}

function linha(nome) {
  return pagina.getByRole("article", { name: nome, exact: true });
}

async function digitar(nome, qtd) {
  const campo = pagina.getByLabel(`Quantidade contada de ${nome}`, { exact: true });
  await campo.click();
  await campo.fill(String(qtd));
  await campo.press("Enter");
}

async function abaAtiva() {
  return (await pagina.locator('nav[aria-label="Ambientes da conferência"] button[aria-current="true"]').innerText()).trim();
}

async function principal() {
  console.log(`==> E2E bens pelo celular — painel ${PAINEL}, API ${API}`);
  const tokenAdmin = await entrarNaApi(ADMIN);
  const tokenOperador = await entrarNaApi(OPERADOR);
  const { unidade } = await prepararCasa(tokenAdmin);
  console.log(`    unidade ${unidade.code} (${unidade.id}), capturas em ${SAIDA}`);

  const navegador = await chromium.launch({ channel: "chrome" }).catch(() => chromium.launch());
  const contexto = await navegador.newContext({
    viewport: { width: 390, height: 844 },
    deviceScaleFactor: 3,
    isMobile: true,
    hasTouch: true,
    locale: "pt-BR",
    timezoneId: "America/Fortaleza",
    userAgent:
      "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
  });
  pagina = await contexto.newPage();
  const errosDoServidor = [];
  pagina.on("response", (r) => {
    if (r.status() >= 500) errosDoServidor.push(`${r.status()} ${r.request().method()} ${r.url()}`);
  });

  let conferenciaId;
  try {
    await passo("login do operador (perfil usuario) no celular", async () => {
      await pagina.goto(`${PAINEL}/login`, { waitUntil: "domcontentloaded" });
      // `domcontentloaded` chega antes da hidratação, e o input hidratado depois
      // volta a vazio (mesma lição de apps/admin/e2e/fumaca.mjs): preenche e
      // CONFERE o valor, por condição.
      await esperarHidratacao('input[type="email"]');
      await esperarHidratacao('button[type="submit"]');
      let preenchido = false;
      for (let tentativa = 1; tentativa <= 3 && !preenchido; tentativa++) {
        await pagina.getByLabel("E-mail").fill(OPERADOR);
        await pagina.getByLabel("Senha").fill(SENHA);
        preenchido = await pagina
          .waitForFunction(
            ([e, s]) =>
              document.querySelector('input[type="email"]')?.value === e &&
              document.querySelector('input[type="password"]')?.value === s,
            [OPERADOR, SENHA],
            { timeout: 3000 },
          )
          .then(() => true)
          .catch(() => false);
      }
      assert.ok(preenchido, "o formulário de login perdeu o que foi digitado em 3 tentativas");
      await pagina.getByRole("button", { name: "Entrar" }).click();
      await esperarCaminho((c) => c.startsWith("/app"));
    });

    await passo("abrir a conferência pela tela da unidade", async () => {
      await pagina.goto(`${PAINEL}/app/inventario?unidade=${unidade.id}`, { waitUntil: "domcontentloaded" });
      // Antes da hidratação o botão não tem onClick: clica até o modal aparecer.
      await clicarAte(pagina.getByRole("button", { name: "Abrir conferência" }), pagina.getByRole("dialog"));
      await pagina.getByRole("button", { name: "Abrir e começar a contar" }).click();
      await esperarCaminho((c) => /^\/app\/inventario\/conferencias\/[0-9a-f-]{36}$/.test(c));
      conferenciaId = new URL(pagina.url()).pathname.split("/").pop();
      await esperarRodape(0, 4);
    });

    await passo("cômodo 1 — Cozinha: digita 9 pratos (faltam 3) e confere as 6 taças", async () => {
      assert.match(await abaAtiva(), /^Cozinha\s*2$/, "a conferência abre no primeiro cômodo com pendência, na ordem de caminhada, com 2 sem contar");
      await digitar(NOMES.prato, 9);
      await linha(NOMES.taca).getByRole("button", { name: `${NOMES.taca}: conferido igual ao esperado, 6` }).click();
      await esperarRodape(2, 4);
      await pagina.locator('nav[aria-label="Ambientes da conferência"]').getByRole("button", { name: /^Cozinha/ }).getByLabel("tudo contado").waitFor({ timeout: ESPERA });
    });

    await passo("cômodo 2 — Banheiro pelo botão Próximo: toalha com um a menos (3 de 4)", async () => {
      await pagina.getByRole("button", { name: /^Próximo: Banheiro/ }).click();
      await pagina.getByRole("heading", { name: "Banheiro", level: 2 }).waitFor({ timeout: ESPERA });
      // `−` numa linha pendente parte do esperado: 4 − 1 = 3.
      await linha(NOMES.toalha).getByRole("button", { name: `Um a menos de ${NOMES.toalha}` }).click();
      await esperarRodape(3, 4);
      assert.equal(await pagina.getByLabel(`Quantidade contada de ${NOMES.toalha}`, { exact: true }).inputValue(), "3");
    });

    await passo("tentar fechar com o Quarto pendente: recusa e aponta o cômodo", async () => {
      await pagina.getByRole("button", { name: "Fechar conferência" }).click();
      const dialogo = pagina.getByRole("dialog");
      await dialogo.getByText("Ainda há").waitFor({ timeout: ESPERA });
      await dialogo.getByRole("button", { name: "Fechar e apurar" }).click();
      const onde = dialogo.getByRole("list", { name: "Onde ainda falta contar" });
      await onde.waitFor({ timeout: ESPERA });
      const itens = await onde.getByRole("listitem").allInnerTexts();
      assert.equal(itens.length, 1, `só o Quarto tem pendência; a tela listou: ${itens.join(" | ")}`);
      assert.match(itens[0], /Quarto:\s*1 sem contar/);
      const r = await api("GET", `/inventory/counts/${conferenciaId}`, tokenOperador);
      assert.equal(r.json.data.status, "aberta", "a recusa não pode ter fechado a conferência");
    });

    await passo("ir ao cômodo indicado pela recusa e contar o travesseiro", async () => {
      await pagina
        .getByRole("list", { name: "Onde ainda falta contar" })
        .getByRole("button", { name: "Ir contar" })
        .click();
      await pagina.getByRole("dialog").waitFor({ state: "detached", timeout: ESPERA });
      await pagina.getByRole("heading", { name: "Quarto", level: 2 }).waitFor({ timeout: ESPERA });
      assert.match(await abaAtiva(), /^Quarto\s*1$/);
      await linha(NOMES.travesseiro).getByRole("button", { name: `${NOMES.travesseiro}: conferido igual ao esperado, 2` }).click();
      await esperarRodape(4, 4);
    });

    await passo("fechar e ver a apuração", async () => {
      await pagina.getByRole("button", { name: "Fechar conferência" }).click();
      await pagina.getByRole("dialog").getByRole("button", { name: "Fechar e apurar" }).click();
      const resultado = pagina.getByRole("region", { name: "Resultado da conferência" });
      await resultado.getByRole("heading", { name: "2 divergências" }).waitFor({ timeout: ESPERA });
      await conferirResultado(resultado);
    });

    await passo("recarregar a página: o resultado continua lá, e não há mais o que contar", async () => {
      await pagina.reload({ waitUntil: "domcontentloaded" });
      const resultado = pagina.getByRole("region", { name: "Resultado da conferência" });
      await resultado.getByRole("heading", { name: "2 divergências" }).waitFor({ timeout: ESPERA });
      await conferirResultado(resultado);
      assert.equal(await pagina.getByRole("button", { name: "Fechar conferência" }).count(), 0, "conferência fechada não oferece fechar de novo");
      assert.equal(await pagina.getByLabel(/^Quantidade contada de /).count(), 0, "conferência fechada não oferece contador");
      await pagina.getByText("Fechada", { exact: true }).first().waitFor({ timeout: ESPERA });
    });

    await passo("a API confirma o que a tela mostrou", async () => {
      const r = await api("GET", `/inventory/counts/${conferenciaId}`, tokenOperador);
      assert.equal(r.status, 200);
      const c = r.json.data;
      assert.equal(c.status, "fechada");
      assert.equal(c.result.issues_created, 2);
      const porBem = Object.fromEntries(c.result.divergences.map((d) => [d.item_name, d]));
      assert.equal(porBem[NOMES.prato].diff, -3);
      assert.equal(porBem[NOMES.prato].loss_cents, 5670);
      assert.equal(porBem[NOMES.toalha].diff, -1);
      assert.equal(porBem[NOMES.toalha].loss_cents, null);
    });
  } finally {
    if (errosDoServidor.length > 0) falhas.push(`respostas 5xx durante a jornada: ${errosDoServidor.join("; ")}`);
    await navegador.close();
    await api("PATCH", `/units/${unidade.id}`, tokenAdmin, { active: false }).catch(() => {});
  }
}

async function conferirResultado(resultado) {
  const linhas = await resultado.locator("tbody tr").allInnerTexts();
  assert.equal(linhas.length, 2, `duas divergências; a tabela tem: ${linhas.join(" | ")}`);
  const prato = linhas.find((l) => l.includes(NOMES.prato));
  const toalha = linhas.find((l) => l.includes(NOMES.toalha));
  assert.ok(prato && /Cozinha/.test(prato) && /\b12\b/.test(prato) && /\b9\b/.test(prato) && /-3/.test(prato), `linha do prato: ${prato}`);
  assert.ok(/R\$\s*56,70/.test(prato), `prejuízo do prato tem de ser R$ 56,70 (3 × R$ 18,90): ${prato}`);
  assert.ok(toalha && /Banheiro/.test(toalha) && /-1/.test(toalha) && /sem custo cotado/.test(toalha), `linha da toalha: ${toalha}`);
  await resultado.getByText(/2 avarias foram abertas para as faltas/).waitFor({ timeout: ESPERA });
}

try {
  await principal();
} catch (e) {
  if (!falhas.length) falhas.push(e.message);
}
if (falhas.length) {
  console.log(`==> E2E REPROVADO (${falhas.length}):\n  - ${falhas.join("\n  - ")}`);
  process.exit(1);
}
console.log("==> E2E aprovado: a conferência pelo celular, do primeiro toque ao resultado recarregado");
