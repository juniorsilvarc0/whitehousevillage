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
  'menu.reservar': 'Reserve já',
  'marca.icone': { url: '/api/v1/public/media/44444444-4444-4444-4444-444444444444', alt: '' },
  'acomodacoes.botao-card': 'Quero ver',
  'acomodacoes.opcoes': '{n} apartamentos',
  'local.foto-legenda': 'Legenda nova',
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

// Central de reservas inventada, para desenhar orçamento e formulário.
const HOJE = '2026-11-02';
const PRODUTO_DISP = { unit_type_id: 'u1', code: 'grand-villa', name: 'Grand Villa', capacity: 8, on_request: false,
  from_price_cents: 300000, rates: [{ date_type: 'normal', price_cents: 300000 }], packages: [] };
const POLITICA = { today: HOJE, hold_hours: 48, deposit_pct: 30, balance_due_days: 7 };
function diasDe(url) {
  const u = new URL(url);
  const out = [];
  for (let d = new Date(u.searchParams.get('from') + 'T12:00:00Z'); d.toISOString().slice(0, 10) < u.searchParams.get('to'); d.setUTCDate(d.getUTCDate() + 1)) {
    out.push({ date: d.toISOString().slice(0, 10), available: true, on_request: false, price_cents: 300000, date_type: 'normal', min_nights: 1 });
  }
  return out;
}
const ORCAMENTO = { lines: [{ date_type: 'normal', label: 'Diária normal', nights: 2, subtotal_cents: 600000 }], cleaning_cents: 20000,
  night_count: 2, total_cents: 620000, deposit_cents: 186000, balance_cents: 434000, avg_nightly_cents: 300000 };
async function centralInventada(page, valores) {
  await page.route('**/api/v1/public/site', r => (valores ? json(r, { values: valores }) : r.fulfill({ status: 502, body: '' })));
  await page.route('**/api/v1/public/products', r => json(r, [PRODUTO_DISP]));
  await page.route('**/api/v1/public/policy', r => json(r, POLITICA));
  await page.route('**/api/v1/public/availability**', r => json(r, diasDe(r.request().url())));
  await page.route('**/api/v1/public/quotes', r => json(r, ORCAMENTO));
}
const DEEP = '/disponibilidade.html?produto=grand-villa&checkin=2026-11-10&checkout=2026-11-12';

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
    confere((await page.locator('.site-header__nav a').allTextContents()).join('|') === 'Início|Acomodações|Eventos|Estrutura|Disponibilidade', 'menu original');
    confere((await page.textContent('[data-cms="menu.reservar"]')) === 'Reservar', 'botão Reservar original');
    confere((await page.getAttribute('link[rel=icon]', 'href')) === '/assets/logo.png', 'ícone da aba original');
    confere((await page.textContent('.unit-card__desc')).startsWith('Não conseguimos carregar'), 'aviso de acomodações sem a central');
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
  confere((await page.textContent('[data-cms="menu.reservar"]')) === 'Reserve já', 'botão do topo editado');
  confere((await page.getAttribute('link[rel=icon]', 'href')).includes('4444'), 'ícone da aba editado');
  confere((await card.locator('.unit-card__cta').textContent()) === 'Quero ver', 'botão do card editado (texto desenhado pelo JavaScript)');
  confere((await card.locator('.unit-card__specs li').last().textContent()) === '2 apartamentos', 'marcador {n} trocado no card');
  confere((await page.textContent('.location__media .scene__label')) === 'Legenda nova', 'legenda do fundo desenhado editada');
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

  // ── 4. Textos desenhados pelo JavaScript (orçamento e pré-reserva) ──
  console.log('4. orçamento e formulário: original e editado');
  {
    const p = await browser.newPage();
    await centralInventada(p, null);
    await p.goto(BASE + DEEP, { waitUntil: 'load' });
    await p.waitForSelector('[data-hold] button[type=submit]');
    confere((await p.textContent('[data-hold] button[type=submit]')).trim() === 'Fazer pré-reserva', 'sem edição: botão "Fazer pré-reserva"');
    confere((await p.textContent('.quote__title')) === 'Seu orçamento', 'sem edição: título do orçamento');
    confere((await p.textContent('.hold__consent span')).startsWith('Autorizo a White House'), 'sem edição: autorização original');
    confere((await p.textContent('.quote__note')).includes('48h'), 'sem edição: prazo da política na nota');
    confere((await p.textContent('.cta-banner__lead')).includes('48 horas') && (await p.textContent('.cta-banner__lead')).includes('30%'), 'sem edição: frase da política com números');
    confere((await p.textContent('[data-cms="disp.legenda-livre"]')) === 'Livre', 'sem edição: legenda');
    if (CAPTURAS) await p.screenshot({ path: CAPTURAS + '/disp-original.png', fullPage: true });
    await p.close();
  }
  {
    const p = await browser.newPage();
    await centralInventada(p, {
      'pre-reserva.botao': 'Segurar a <b>data</b>',
      'pre-reserva.nota': 'Fica segura {horas}h, sinal {sinal}%. {inexistente}',
      'orcamento.titulo': 'Quanto fica',
      'disp.chamada.texto': 'Segura por {horas} horas e pronto.',
      'disp.legenda-livre': 'Disponível',
      'menu.disponibilidade': 'Calendário',
      'calendario.noites-livres': '{n} livres — {acomodacao}'
    });
    await p.goto(BASE + DEEP, { waitUntil: 'load' });
    await p.waitForSelector('[data-hold] button[type=submit]');
    confere((await p.textContent('[data-hold] button[type=submit]')).trim() === 'Segurar a <b>data</b>', 'botão do formulário editado, HTML como texto');
    confere(await p.locator('[data-hold] button[type=submit] b').count() === 0, 'nenhuma marcação injetada no botão');
    confere((await p.textContent('.quote__title')) === 'Quanto fica', 'título do orçamento editado');
    confere((await p.textContent('form .quote__note')) === 'Fica segura 48h, sinal 30%. {inexistente}', 'marcadores trocados pelos números da política');
    confere((await p.textContent('.cta-banner__lead')) === 'Segura por 48 horas e pronto.', 'frase da política editada, com o número');
    confere((await p.textContent('[data-cms="disp.legenda-livre"]')) === 'Disponível', 'legenda editada');
    confere((await p.locator('[data-cms="menu.disponibilidade"]').allTextContents()).every(x => x === 'Calendário'), 'menu editado em todos os lugares');
    confere(/^\d+ livres — Grand Villa$/.test(await p.textContent('.calendar__sub')), 'resumo do mês editado');
    if (CAPTURAS) await p.screenshot({ path: CAPTURAS + '/disp-editado.png', fullPage: true });
    await p.close();
  }
} finally {
  await browser.close();
}

console.log(falhas.length ? `\nREPROVADO: ${falhas.length} falha(s).` : '\nAPROVADO.');
process.exit(falhas.length ? 1 : 0);
