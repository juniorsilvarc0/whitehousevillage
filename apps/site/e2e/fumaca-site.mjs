#!/usr/bin/env node
// Fumaça do site público (apps/site). Node 22 puro, sem dependência: só fetch,
// fs e path. Não abre navegador — o que ela prova é o que o nginx SERVE.
//
// Uso:
//   node apps/site/e2e/fumaca-site.mjs [URL_BASE]
//   SITE_URL=http://localhost:3200 node apps/site/e2e/fumaca-site.mjs
// Padrão: http://localhost:3200 (SITE_PORT do .env.example). Sai com 1 se
// qualquer critério reprovar, e lista todos — não para no primeiro.
//
// ── O porquê de cada critério ──────────────────────────────────────────────
//
// 1. "/" e "/disponibilidade.html" respondem 200.
//    São as duas páginas do site. Se uma delas cai, não há site.
//
// 2. "/admin", "/admin/", "/scripts/admin.js" e "/styles/admin.css" respondem 404
//    — 404 direto, sem redirect no meio.
//    O back-office mocado (passo D1) ficou no ar sem autenticação nenhuma,
//    mostrando receita, conversão e funil a quem soubesse o caminho. Foi apagado,
//    e o nginx.conf tem uma trava explícita em /admin. Este critério segura as
//    duas coisas: se a pasta voltar ou a trava sumir, a fumaça reprova.
//
// 3. Um caminho inventado responde 404, com a página de erro do site.
//    O fallback antigo devolvia a home com 200 para QUALQUER caminho — foi assim
//    que /admin/ continuaria "existindo" depois de apagado, e cada link quebrado
//    virava uma cópia da home para o buscador. Conferir o corpo prova que o
//    error_page está ligado (e não só o 404 cru do nginx).
//
// 4. Todo recurso interno referenciado responde 200, sem redirect, com o
//    Content-Type certo para CSS, JS e fonte.
//    "Interno" é todo src/href/url() relativo ou absoluto para o mesmo host, nas
//    páginas, na 404 e dentro dos CSS (é lá que as fontes moram). Um 200 com
//    application/octet-stream num .css passa num curl e quebra a página no
//    navegador — por isso o tipo também é conferido.
//
// 5. Nenhuma referência a host externo fora da lista permitida.
//    Varre src/href/url() das páginas e dos CSS e as URLs literais dos JS. O
//    site já carregou fontes do Google, entregando o IP de cada visitante a um
//    terceiro antes de qualquer consentimento (LGPD). Lista permitida, explícita:
//      - wa.me e api.whatsapp.com: os botões de contato.
//    Não há link de mapa no site hoje; se entrar um, ele precisa vir para esta
//    lista de propósito, com o motivo.
//
// 6. WhatsApp: um número só, vindo de um lugar só.
//    a) O HTML servido não tem diretiva SSI crua (`<!--#`). O número é escrito
//       pelo nginx por SSI; se o `ssi on` sumir, todo botão de contato vira um
//       link quebrado e nada mais acusa.
//    b) Todo link wa.me e o <meta name="whv:whatsapp"> carregam o MESMO número,
//       só dígitos, com DDI e DDD (10 a 15 dígitos).
//    c) Esse número aparece UMA vez no código-fonte de apps/site (contado no
//       repositório ao lado deste script, não na resposta): trocar o número
//       real tem de ser editar uma linha. Antes eram oito cópias.
//    Aviso, sem reprovar: número com cara de fictício (o mesmo dígito repetido
//    no fim) — o real está pendente do dono do negócio.

import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const BASE = (process.argv[2] || process.env.SITE_URL || 'http://localhost:3200').replace(/\/+$/, '');
const ORIGEM = new URL(BASE);
const SITE_DIR = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOSTS_PERMITIDOS = new Set(['wa.me', 'api.whatsapp.com']);
const TIMEOUT_MS = 10_000;

const falhas = [];
const avisos = [];
const ok = msg => console.log(`  ok     ${msg}`);
const reprova = msg => { falhas.push(msg); console.log(`  FALHA  ${msg}`); };

async function pedir(caminho, metodo = 'GET') {
  const url = new URL(caminho, BASE + '/');
  url.hash = '';
  try {
    const r = await fetch(url, { method: metodo, redirect: 'manual', signal: AbortSignal.timeout(TIMEOUT_MS) });
    const corpo = metodo === 'GET' ? await r.text() : '';
    return { status: r.status, tipo: r.headers.get('content-type') || '', corpo, url: url.href };
  } catch (e) {
    return { status: 0, tipo: '', corpo: '', url: url.href, erro: e.cause?.code || e.message };
  }
}

// Referências de um HTML ou CSS: atributos src/href/poster/srcset e url()/@import.
function referencias(texto) {
  const refs = [];
  for (const m of texto.matchAll(/\b(?:src|href|poster|srcset)\s*=\s*(?:"([^"]*)"|'([^']*)')/gi)) {
    const v = (m[1] ?? m[2]).trim();
    if (/srcset/i.test(m[0])) v.split(',').forEach(p => refs.push(p.trim().split(/\s+/)[0]));
    else refs.push(v);
  }
  for (const m of texto.matchAll(/url\(\s*(?:"([^"]*)"|'([^']*)'|([^)\s]*))\s*\)/gi)) refs.push((m[1] ?? m[2] ?? m[3]).trim());
  for (const m of texto.matchAll(/@import\s+(?:"([^"]*)"|'([^']*)')/gi)) refs.push((m[1] ?? m[2]).trim());
  return refs.filter(Boolean);
}

// Classifica uma referência: ignorada (âncora, mailto, tel, data:), interna ou externa.
function classificar(ref, baseDoArquivo) {
  if (/^(#|mailto:|tel:|data:|javascript:)/i.test(ref)) return { tipo: 'ignorada' };
  if (ref.includes('<!--#')) return { tipo: 'ssi-crua' };
  let u;
  try { u = new URL(ref, baseDoArquivo); } catch { return { tipo: 'invalida' }; }
  if (!/^https?:$/.test(u.protocol)) return { tipo: 'ignorada' };
  if (u.host === ORIGEM.host) return { tipo: 'interna', caminho: u.pathname + u.search };
  return { tipo: 'externa', host: u.hostname };
}

const TIPO_ESPERADO = [
  [/\.css$/i, /^text\/css/],
  [/\.m?js$/i, /^(application|text)\/javascript/],
  [/\.woff2$/i, /^font\/woff2/],
];

console.log(`Fumaça do site em ${BASE}\n`);

// ── 1. Páginas ──────────────────────────────────────────────────────────────
console.log('1. páginas respondem 200');
const paginas = {};
for (const p of ['/', '/disponibilidade.html']) {
  const r = await pedir(p);
  paginas[p] = r;
  if (r.status === 200 && /text\/html/.test(r.tipo)) ok(`${p} → 200`);
  else reprova(`${p} → ${r.status || r.erro} (${r.tipo || 'sem content-type'}), esperado 200 text/html`);
}

// ── 2. Back-office apagado ─────────────────────────────────────────────────
console.log('2. back-office mocado responde 404');
for (const p of ['/admin', '/admin/', '/scripts/admin.js', '/styles/admin.css']) {
  const r = await pedir(p);
  if (r.status === 404) ok(`${p} → 404`);
  else reprova(`${p} → ${r.status || r.erro}, esperado 404`);
}

// ── 3. Caminho inventado ───────────────────────────────────────────────────
console.log('3. caminho inexistente responde 404 com a página de erro do site');
const inventado = `/fumaca-${Date.now().toString(36)}/nao-existe`;
const r404 = await pedir(inventado);
if (r404.status !== 404) reprova(`${inventado} → ${r404.status || r404.erro}, esperado 404 (fallback para a home voltou?)`);
else if (!/Erro 404/.test(r404.corpo)) reprova(`${inventado} → 404, mas sem a página de erro do site (error_page desligado?)`);
else ok(`${inventado} → 404 com a página de erro`);
if (r404.status === 404) paginas['(página 404)'] = r404;

// ── 4 e 5. Recursos internos e hosts externos ──────────────────────────────
console.log('4. recursos internos respondem 200 com o tipo certo');
const internos = new Map();   // caminho → de onde veio
const externos = new Map();   // host → de onde veio
const ssiCrua = [];
const fila = [];
for (const [nome, r] of Object.entries(paginas)) {
  if (r.status !== 200 && r.status !== 404) continue;
  for (const ref of referencias(r.corpo)) fila.push({ ref, origem: nome, base: r.url });
}
const vistosCss = new Set();
while (fila.length) {
  const { ref, origem, base } = fila.shift();
  const c = classificar(ref, base);
  if (c.tipo === 'ssi-crua') ssiCrua.push(`${origem}: ${ref}`);
  else if (c.tipo === 'externa') externos.set(c.host, externos.get(c.host) || origem);
  else if (c.tipo === 'invalida') reprova(`referência inválida em ${origem}: ${ref}`);
  else if (c.tipo === 'interna' && !internos.has(c.caminho)) {
    internos.set(c.caminho, origem);
    // CSS é baixado e varrido: as fontes só aparecem lá dentro.
    if (/\.css(\?|$)/i.test(c.caminho) && !vistosCss.has(c.caminho)) {
      vistosCss.add(c.caminho);
      const css = await pedir(c.caminho);
      if (css.status === 200) for (const r of referencias(css.corpo)) fila.push({ ref: r, origem: c.caminho, base: css.url });
    }
  }
}
for (const [caminho, origem] of [...internos].sort()) {
  // HEAD para mídia e fonte: o vídeo da home tem 12 MB e o que importa aqui é o status.
  const semQuery = caminho.split('?')[0];
  const baixar = semQuery.endsWith('/') || /\.(html|css)$/i.test(semQuery);
  const r = await pedir(caminho, baixar ? 'GET' : 'HEAD');
  const esperado = TIPO_ESPERADO.find(([ext]) => ext.test(semQuery));
  if (r.status !== 200) reprova(`${caminho} (de ${origem}) → ${r.status || r.erro}, esperado 200`);
  else if (esperado && !esperado[1].test(r.tipo)) reprova(`${caminho} (de ${origem}) → 200, mas Content-Type "${r.tipo}"`);
  else ok(`${caminho} → 200${esperado ? ` ${r.tipo.split(';')[0]}` : ''}`);
}

console.log('5. nenhum host externo fora da lista permitida');
// JS: só URLs literais http(s) — o JS do site não deve buscar nada fora do host.
for (const caminho of [...internos.keys()].filter(c => /\.m?js(\?|$)/.test(c))) {
  const js = await pedir(caminho);
  for (const m of js.corpo.matchAll(/https?:\/\/([a-z0-9.-]+)/gi)) {
    if (m[1].toLowerCase() !== ORIGEM.hostname) externos.set(m[1].toLowerCase(), externos.get(m[1].toLowerCase()) || caminho);
  }
}
for (const [host, origem] of externos) {
  if (HOSTS_PERMITIDOS.has(host)) ok(`${host} (permitido; primeiro uso em ${origem})`);
  else reprova(`host externo não permitido: ${host} (em ${origem})`);
}
if (!externos.size) ok('nenhuma referência externa');

// ── 6. WhatsApp ─────────────────────────────────────────────────────────────
console.log('6. WhatsApp: um número só, vindo de um lugar só');
if (ssiCrua.length) reprova(`diretiva SSI crua no HTML servido (ssi on desligado?): ${ssiCrua.slice(0, 2).join(' | ')}`);
else ok('nenhuma diretiva SSI crua no HTML servido');

const numeros = new Map();   // número → onde
for (const [nome, r] of Object.entries(paginas)) {
  for (const m of r.corpo.matchAll(/https:\/\/(?:wa\.me|api\.whatsapp\.com)\/([^"'?\s<]*)/g)) numeros.set(m[1], numeros.get(m[1]) || nome);
  for (const m of r.corpo.matchAll(/<meta\s+name="whv:whatsapp"\s+content="([^"]*)"/g)) numeros.set(m[1], numeros.get(m[1]) || `${nome} <meta>`);
}
const lista = [...numeros.keys()];
let numero = null;
if (lista.length === 0) reprova('nenhum link de WhatsApp nas páginas');
else if (lista.length > 1) reprova(`números diferentes nos links: ${[...numeros].map(([n, o]) => `${n || '(vazio)'} em ${o}`).join('; ')}`);
else if (!/^\d{10,15}$/.test(lista[0])) reprova(`número de WhatsApp malformado: "${lista[0]}"`);
else { numero = lista[0]; ok(`todos os links e o <meta> usam o mesmo número (${numero.length} dígitos)`); }

if (numero) {
  if (/(\d)\1{7,}$/.test(numero)) avisos.push('o número de WhatsApp parece fictício (mesmo dígito repetido no fim): o real está pendente do dono do negócio');
  const ocorrencias = [];
  const BINARIO = /\.(woff2?|mp4|webm|png|jpe?g|gif|ico|webp)$/i;
  (function varrer(dir) {
    for (const nome of readdirSync(dir)) {
      if (nome === 'node_modules' || nome.startsWith('.')) continue;
      const p = join(dir, nome);
      if (statSync(p).isDirectory()) { varrer(p); continue; }
      if (BINARIO.test(nome)) continue;
      readFileSync(p, 'utf8').split('\n').forEach((linha, i) => {
        if (linha.includes(numero)) ocorrencias.push(`${relative(SITE_DIR, p)}:${i + 1}`);
      });
    }
  })(SITE_DIR);
  if (ocorrencias.length === 1) ok(`o número aparece uma vez no código-fonte: apps/site/${ocorrencias[0]}`);
  else reprova(`o número aparece ${ocorrencias.length} vezes no código-fonte de apps/site (esperado 1): ${ocorrencias.join(', ') || '(nenhuma — de onde o nginx tirou?)'}`);
}

// ── Resultado ──────────────────────────────────────────────────────────────
for (const a of avisos) console.log(`\n  AVISO  ${a}`);
if (falhas.length) {
  console.log(`\nREPROVADO: ${falhas.length} falha(s).`);
  process.exit(1);
}
console.log(`\nAPROVADO: ${internos.size} recursos internos, ${externos.size} host(s) externo(s) permitido(s).`);
