#!/usr/bin/env node
// Conteúdo editado no painel (menu "Site") chegando ao site — docs/site-cms.md §7.
//
// Abre o site num Chromium de verdade e intercepta /api/v1/public/site com um
// conteúdo inventado. Prova que:
//   1. sem edição (pedido falhando), o texto original do HTML continua;
//   2. título com *destaque* vira <em>, texto longo vira parágrafos, foto troca
//      o fundo do bloco e some o rótulo de cena, lista é redesenhada pelo
//      <template>, as categorias de acomodação usam o texto editado;
//   3. `<script>` num valor aparece como TEXTO — nunca vira HTML nem roda;
//   4. foto apontando para outro host é ignorada (nenhum terceiro recebe o
//      visitante).
//
// Uso:
//   node apps/site/e2e/conteudo-site.mjs [URL_BASE] [PASTA_DAS_CAPTURAS]
// Variáveis: PLAYWRIGHT_MODULE (caminho do index.mjs do playwright, se não
// estiver instalado no Node), CHROMIUM_PATH (executável do Chromium).

const BASE = process.argv[2] || process.env.SITE_URL || 'http://localhost:3200';
const CAPTURAS = process.argv[3] || process.env.CAPTURAS || '';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');

const falhas = [];
const ok = m => console.log('  ok     ' + m);
const reprova = m => { falhas.push(m); console.log('  FALHA  ' + m); };
const confere = (cond, m) => (cond ? ok(m) : reprova(m));

const FOTO_PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z/C/HgAGgwJ/lK3Q6wAAAABJRU5ErkJggg==',
  'base64');

const EDITADO = {
  'seo.inicio.titulo': 'Título editado da aba',
  'seo.inicio.descricao': 'Descrição editada',
  'inicio.titulo': 'Um título com *destaque*\nem duas linhas',
  'inicio.texto': 'Primeiro parágrafo.\n\nSegundo <script>window.__invadiu = 1</script> parágrafo.',
  'inicio.local': '<b>não negrito</b>',
  'inicio.numeros': [{ valor: '30', rotulo: 'Hóspedes' }, { valor: '1', rotulo: '<img src=x onerror="window.__invadiu=2">' }],
  'casa.texto': 'Parágrafo A\n\nParágrafo B\n\nParágrafo C',
  'casa.foto': { url: '/api/v1/public/media/11111111-1111-1111-1111-111111111111', alt: 'Varanda ao entardecer' },
  'local.foto': { url: 'https://terceiro.example.com/rastreio.png', alt: 'host de fora' },
  'eventos.cards': [
    { titulo: 'Card editado', texto: 'Texto do card', itens: 'Um\nDois\n\nTrês', foto: { url: '/api/v1/public/media/22222222-2222-2222-2222-222222222222', alt: 'Festa' } },
    { titulo: 'Segundo card', texto: 'Outro', itens: '' }
  ],
  'faixa.itens': [{ texto: 'Só um tema' }],
  'rodape.email': 'contato@exemplo.com.br',
  'rodape.endereco': 'Rua Um\nBairro Dois\nCidade',
  'chamada.texto': 'Chamada editada pelo gestor.',
  'categoria.duplex.titulo': 'Duplex editado',
  'categoria.duplex.selo': 'Selo novo',
  'categoria.duplex.itens': [{ texto: 'item editado' }],
  'categoria.duplex.foto': { url: '/api/v1/public/media/33333333-3333-3333-3333-333333333333', alt: 'Duplex' }
};

const PRODUTOS = [
  { id: 'a', code: 'duplex-aurora', name: 'Duplex Aurora', from_price_cents: 90000 },
  { id: 'b', code: 'duplex-brisa', name: 'Duplex Brisa', from_price_cents: 95000 },
  { id: 'c', code: 'grand-villa', name: 'Grand Villa', from_price_cents: 300000 }
];

const json = (route, data) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data }) });

const browser = await chromium.launch(process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {});
try {
  // ── 1. Sem a API: o original fica ──
  console.log('1. sem conteúdo editado, o HTML original continua');
  {
    const page = await browser.newPage();
    await page.route('**/api/v1/public/**', r => r.fulfill({ status: 502, body: '' }));
    await page.goto(BASE + '/', { waitUntil: 'load' });
    await page.evaluate(() => window.WH_CONTEUDO);
    confere((await page.textContent('.hero__title')).includes('exclusividade'), 'título original da capa');
    confere(await page.locator('.hero__meta > div').count() === 3, 'três números originais na capa');
    confere(await page.locator('.amenities__grid > .amenity').count() === 8, 'oito itens de estrutura');
    confere(await page.locator('.events__grid > article').count() === 3, 'três cards de evento');
    if (CAPTURAS) await page.screenshot({ path: CAPTURAS + '/site-original.png', fullPage: true });
    await page.close();
  }

  // ── 2. Com conteúdo editado ──
  console.log('2. conteúdo editado é aplicado');
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  const pedidosDeFora = [];
  page.on('request', req => { if (!req.url().startsWith(BASE)) pedidosDeFora.push(req.url()); });
  await page.route('**/api/v1/public/site', r => json(r, { values: EDITADO }));
  await page.route('**/api/v1/public/products', r => json(r, PRODUTOS));
  await page.route('**/api/v1/public/policy', r => json(r, { hold_hours: 48, deposit_pct: 30, balance_due_days: 7 }));
  await page.route('**/api/v1/public/media/**', r => r.fulfill({ status: 200, contentType: 'image/png', body: FOTO_PNG }));
  await page.route(u => !u.href.startsWith(BASE), r => r.abort());
  await page.goto(BASE + '/', { waitUntil: 'load' });
  await page.evaluate(() => window.WH_CONTEUDO);
  await page.waitForSelector('.unit-card__title');

  confere(await page.title() === 'Título editado da aba', '<title> editado');
  confere(await page.getAttribute('meta[name=description]', 'content') === 'Descrição editada', 'descrição para o Google editada');
  confere(await page.innerHTML('.hero__title') === 'Um título com <em>destaque</em><br>em duas linhas', '*destaque* vira <em> e a quebra vira <br>');
  confere(await page.textContent('.hero__eyebrow') === '<b>não negrito</b>', 'HTML digitado em texto aparece como texto');
  const lead = await page.innerHTML('.hero__lead');
  confere(lead.includes('&lt;script&gt;') && !lead.includes('<script'), '<script> num valor aparece escapado');
  confere(await page.locator('.hero__meta > div').count() === 2, 'lista de números redesenhada (2 itens)');
  confere(await page.locator('.hero__meta img').count() === 0, 'HTML num item de lista não vira elemento');
  confere(await page.evaluate(() => window.__invadiu) === undefined, 'nenhum script injetado rodou');
  confere(await page.locator('.intro__body > p').count() === 3, 'texto longo vira três parágrafos');
  const fundoCasa = await page.evaluate(() => getComputedStyle(document.querySelector('.intro__media')).backgroundImage);
  confere(fundoCasa.includes('/api/v1/public/media/1111'), 'foto da casa vira fundo do bloco');
  confere(await page.isHidden('.intro__media .scene__label'), 'rótulo de cena some quando há foto');
  confere(!(await page.evaluate(() => getComputedStyle(document.querySelector('.location__media')).backgroundImage)).includes('terceiro'),
    'foto de outro host é ignorada');
  confere(await page.isVisible('.location__media .scene__label'), 'bloco sem foto válida mantém o rótulo');
  confere(await page.locator('.events__grid > article').count() === 2, 'cards de evento redesenhados');
  confere(await page.locator('.events__grid > article').first().evaluate(e => e.classList.contains('event-card--tall')), 'primeiro card continua alto');
  confere(await page.locator('.events__grid > article:first-of-type li').allTextContents().then(t => t.join('|')) === 'Um|Dois|Três', 'itens do card, um por linha');
  confere(await page.locator('.events__grid > article:nth-of-type(2) li').count() === 0, 'card sem itens fica sem lista');
  confere(await page.locator('.marquee__inner > span').count() === 1, 'faixa de temas com um item');
  confere(await page.getAttribute('[data-cms-email]', 'href') === 'mailto:contato@exemplo.com.br', 'e-mail do rodapé editado');
  confere(await page.locator('[data-cms-linhas="rodape.endereco"] li').count() === 3, 'endereço em três linhas');
  confere((await page.textContent('.cta-banner__lead')).trim() === 'Chamada editada pelo gestor.', 'chamada editada vence a frase-padrão');
  const card = page.locator('.unit-card').first();
  confere((await card.locator('.unit-card__title').textContent()) === 'Duplex editado', 'título da categoria editado no card');
  confere((await card.locator('.unit-card__tag').textContent()) === 'Selo novo', 'selo da categoria editado');
  confere((await card.locator('.unit-card__specs li').first().textContent()) === 'item editado', 'itens da categoria editados');
  confere((await card.locator('.unit-card__media').evaluate(e => getComputedStyle(e).backgroundImage)).includes('3333'), 'foto da categoria no card');
  confere((await page.locator('.unit-card').nth(1).locator('.unit-card__title').textContent()) === 'Grand Villa', 'categoria não editada fica como está');
  confere(pedidosDeFora.length === 0, 'nenhum pedido a host de fora' + (pedidosDeFora.length ? ': ' + pedidosDeFora.join(', ') : ''));

  if (CAPTURAS) {
    await page.evaluate(() => document.querySelectorAll('[data-reveal]').forEach(e => e.classList.add('is-visible')));
    await page.screenshot({ path: CAPTURAS + '/site-editado-topo.png' });
    await page.screenshot({ path: CAPTURAS + '/site-editado.png', fullPage: true });
  }
  await page.close();

  // ── 3. As outras páginas também leem o conteúdo ──
  console.log('3. disponibilidade e página não encontrada');
  for (const [caminho, chave, sel] of [['/disponibilidade.html', 'disp.titulo', '.page-hero__title'], ['/nao-existe-xyz', 'erro.titulo', '.cta-banner__title']]) {
    const p = await browser.newPage();
    await p.route('**/api/v1/public/site', r => json(r, { values: { [chave]: 'Editado *aqui*', 'rodape.copyright': '© Teste' } }));
    await p.route('**/api/v1/public/**', r => r.fallback());
    await p.goto(BASE + caminho, { waitUntil: 'load' });
    await p.evaluate(() => window.WH_CONTEUDO);
    confere(await p.innerHTML(sel) === 'Editado <em>aqui</em>', caminho + ': ' + chave);
    confere(await p.textContent('[data-cms="rodape.copyright"]') === '© Teste', caminho + ': rodapé');
    await p.close();
  }
} finally {
  await browser.close();
}

console.log(falhas.length ? `\nREPROVADO: ${falhas.length} falha(s).` : '\nAPROVADO.');
process.exit(falhas.length ? 1 : 0);
