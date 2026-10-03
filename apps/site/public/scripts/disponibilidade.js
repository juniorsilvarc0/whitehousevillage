/* Central de reservas — calendário, orçamento e indicadores, servidos pela API.
 *
 * Esta página é pública, e três regras valem até a última linha:
 *  - o site pergunta, não calcula: catálogo, calendário, orçamento, sinal e
 *    prazos vêm de /api/v1/public/* — o MESMO motor do painel. Se a API não
 *    responde, a página diz isso e oferece o WhatsApp; ela NUNCA inventa preço;
 *  - o visitante não negocia preço: não há controle de desconto, nem regra
 *    comercial interna (passo D2 de docs/unificacao-site-crm.md);
 *  - um dia indisponível é só "indisponível": nunca quem ocupa, por quanto, nem
 *    por quê (invariante 1 do mesmo plano — a API já nem manda isso).
 */
(function () {
  'use strict';
  if (!window.WH) return;

  const params = new URLSearchParams(location.search);
  const $ = s => document.querySelector(s);
  const el = {
    produto: $('[data-produto]'),
    hospedes: $('[data-hospedes]'),
    cals: $('[data-calendars]'),
    quote: $('[data-quote]'),
    kpis: $('[data-kpis]'),
    rates: $('[data-rates] tbody'),
    minimos: $('[data-minimos]'),
    toast: $('[data-toast]')
  };

  const state = {
    produtos: [],
    politica: null,
    hoje: null,
    produto: null,          // código, ex.: 'grand-villa'
    hospedes: 2,
    ano: 0, mes: 0,
    checkin: null, checkout: null,
    consulta: null,         // data sob consulta clicada (sem check-in)
    dias: {},               // `${produtoId}|${data}` → dia da API
    orcamento: null,        // { chave, dados } | { chave, erro }
    carregando: false
  };

  /* Número do WhatsApp: o nginx escreve no <meta name="whv:whatsapp"> por SSI, a
     partir da única linha que o define (apps/site/nginx.conf). Servida por outro
     servidor, a página chega com o comentário SSI cru no lugar do número: aí o
     botão do orçamento simplesmente não aparece, em vez de abrir um link quebrado. */
  const metaWa = document.querySelector('meta[name="whv:whatsapp"]');
  const WHATSAPP = metaWa && /^\d{10,15}$/.test(metaWa.content) ? metaWa.content : null;

  const unit = () => state.produtos.find(p => p.code === state.produto);

  /* "Sob consulta" não é "indisponível": a data pode estar livre, só não tem
     preço de tabela (Réveillon da Grand Villa, a White House Completa). O
     caminho é a conversa, com as datas já escritas. */
  function linkConsulta(u, de, ate) {
    if (!WHATSAPP) return '';
    const linhas = ['Olá! Quero consultar valores na White House:', '', `*${u.name}*`];
    if (de) linhas.push(`Check-in: ${WH.dataBR(de)}`);
    if (ate) linhas.push(`Check-out: ${WH.dataBR(ate)}`);
    linhas.push(`Hóspedes: ${state.hospedes}`);
    return `https://wa.me/${WHATSAPP}?text=` + encodeURIComponent(linhas.join('\n'));
  }
  const diaDe = (s) => state.dias[unit().unit_type_id + '|' + s];
  const curto = c => {
    const reais = Math.floor(c / 100);
    return reais >= 1000 ? (reais / 1000).toFixed(reais % 1000 === 0 ? 0 : 1).replace('.', ',') + 'k' : String(reais);
  };

  let toastTimer;
  function toast(msg) {
    if (!el.toast) return;
    el.toast.textContent = msg;
    el.toast.classList.add('is-visible');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.toast.classList.remove('is-visible'), 4200);
  }

  function falhaGeral() {
    const wa = WHATSAPP ? ` <a class="btn btn--secondary" target="_blank" rel="noopener" href="https://wa.me/${WHATSAPP}">Falar no WhatsApp</a>` : '';
    el.cals.innerHTML = `<p class="quote__empty">A central de reservas está indisponível agora. Tente em instantes${WHATSAPP ? ' ou fale com a gente pelo WhatsApp' : ''}.</p>${wa}`;
    el.quote.innerHTML = '';
    el.kpis.innerHTML = '';
  }

  /* ─────────── Carga ─────────── */

  /* Garante no cache os dias de [de, ate) do produto atual. A API aceita até
     93 dias por pedido; o calendário pede dois meses por vez. */
  async function carregarDias(de, ate) {
    const u = unit();
    let falta = false;
    for (let c = de; c < ate; c = WH.addDays(c, 1)) {
      if (!state.dias[u.unit_type_id + '|' + c]) { falta = true; break; }
    }
    if (!falta) return;
    const dias = await WH.api.disponibilidade(u.unit_type_id, de, ate);
    for (const d of dias) state.dias[u.unit_type_id + '|' + d.date] = d;
  }

  /* Os dois meses visíveis. O primeiro nunca é anterior ao mês corrente, e a
     API aceita até 31 dias para trás: o mês corrente cabe inteiro. */
  function janelaVisivel() {
    return { de: WH.key(state.ano, state.mes, 1), ate: WH.toKey(new Date(state.ano, state.mes + 2, 1)) };
  }

  async function carregarVisivel() {
    const j = janelaVisivel();
    await carregarDias(j.de, j.ate);
  }

  /* ─────────── Controles ─────────── */
  function initControls() {
    /* Agrupado por categoria: o cliente escolhe exatamente qual duplex ou qual
       Pool Suíte quer, não "um duplex qualquer". */
    const opcao = p => `<option value="${WH.esc(p.code)}">${WH.esc(p.name)} · ${p.on_request ? 'sob consulta' : 'até ' + p.capacity + ' hóspedes'}</option>`;
    el.produto.innerHTML = WH.agrupar(state.produtos).map(g => g.produtos.length > 1
      ? `<optgroup label="${WH.esc(g.titulo)}">${g.produtos.map(opcao).join('')}</optgroup>`
      : opcao(g.produtos[0])).join('');
    el.produto.value = state.produto;
    renderHospedes();

    el.produto.addEventListener('change', async () => {
      state.produto = el.produto.value;
      state.checkin = state.checkout = null;
      state.orcamento = null; state.consulta = null;
      state.hospedes = Math.min(state.hospedes, unit().capacity);
      renderHospedes();
      await recarregar();
    });
    el.hospedes.addEventListener('change', () => {
      state.hospedes = Number(el.hospedes.value);
      orcar();
    });
    $('[data-prev]').addEventListener('click', () => shiftMonth(-1));
    $('[data-next]').addEventListener('click', () => shiftMonth(1));
  }

  function renderHospedes() {
    const max = unit().capacity;
    let out = '';
    for (let i = 1; i <= max; i++) out += `<option value="${i}">${i} ${i === 1 ? 'hóspede' : 'hóspedes'}</option>`;
    el.hospedes.innerHTML = out;
    el.hospedes.value = String(state.hospedes);
  }

  async function shiftMonth(n) {
    const d = new Date(state.ano, state.mes + n, 1);
    const hoje = WH.parse(state.hoje);
    const min = new Date(hoje.getFullYear(), hoje.getMonth(), 1);
    /* O calendário público vai até 548 dias (~18 meses); o último par de
       meses visível precisa caber nele, com folga. */
    const max = new Date(hoje.getFullYear(), hoje.getMonth() + 15, 1);
    if (d < min || d > max) return;
    state.ano = d.getFullYear();
    state.mes = d.getMonth();
    await recarregar();
  }

  async function recarregar() {
    state.carregando = true;
    render();
    try {
      await carregarVisivel();
    } catch (e) {
      state.carregando = false;
      toast(e.message || 'Não foi possível carregar o calendário.');
      render();
      return;
    }
    state.carregando = false;
    render();
  }

  /* ─────────── Calendário ─────────── */
  const ESPECIAIS = ['feriado', 'reveillon', 'carnaval', 'alta'];

  function renderCalendars() {
    let html = '';
    for (let i = 0; i < 2; i++) {
      const d = new Date(state.ano, state.mes + i, 1);
      html += mes(d.getFullYear(), d.getMonth());
    }
    el.cals.innerHTML = html;
    el.cals.querySelectorAll('.day[data-d]').forEach(b => {
      b.addEventListener('click', () => onPick(b.dataset.d));
    });
  }

  function mes(ano, m) {
    const first = new Date(ano, m, 1);
    const total = new Date(ano, m + 1, 0).getDate();
    const u = unit();

    let livres = 0;
    let cells = '';
    for (let i = 0; i < first.getDay(); i++) cells += '<span class="day day--empty"></span>';

    for (let d = 1; d <= total; d++) {
      const s = WH.key(ano, m, d);
      const dia = diaDe(s);
      const past = s < state.hoje;
      const livre = !!(dia && dia.available) && !past;
      if (livre) livres++;

      const cls = ['day', dia ? (dia.available ? 'day--free' : dia.on_request ? 'day--consult' : 'day--busy') : 'day--loading'];
      if (past) cls.push('day--past');
      if (dia && ESPECIAIS.indexOf(dia.date_type) !== -1) cls.push('day--special');
      if (s === state.hoje) cls.push('day--today');
      if (s === state.checkin || s === state.checkout) cls.push('day--sel');
      else if (state.checkin && state.checkout && s > state.checkin && s < state.checkout) cls.push('day--in-range');

      const titulo = !dia ? 'Carregando…'
        : dia.available ? `${WH.rotulo(dia.date_type)} · ${WH.brl(dia.price_cents)}${dia.min_nights > 1 ? ' · mínimo ' + dia.min_nights + ' noites' : ''}`
        : dia.on_request ? `${WH.rotulo(dia.date_type)} · sob consulta` : 'Indisponível';

      cells += `<button type="button" class="${cls.join(' ')}" data-d="${s}" title="${WH.esc(titulo)}">
          <span class="day__n">${d}</span>
          <span class="day__p">${livre && dia.price_cents != null ? curto(dia.price_cents) : dia && dia.on_request && !past ? 'cons.' : '—'}</span>
        </button>`;
    }

    return `<div class="calendar">
        <h3 class="calendar__title">${WH.MESES[m]} ${ano}</h3>
        <p class="calendar__sub">${state.carregando ? 'consultando a central…' : `${livres} ${livres === 1 ? 'noite livre' : 'noites livres'} para ${WH.esc(u.name)}`}</p>
        <div class="calendar__weekdays"><span>dom</span><span>seg</span><span>ter</span><span>qua</span><span>qui</span><span>sex</span><span>sáb</span></div>
        <div class="calendar__grid">${cells}</div>
      </div>`;
  }

  async function onPick(s) {
    if (s < state.hoje) { toast('Data já passou.'); return; }
    const dia = diaDe(s);
    if (!dia) return;

    /* O dia de check-out não é noite da estadia: pode estar ocupado por quem
       chega nesse dia (a estadia é half-open, [check-in, check-out)). */
    const escolhendoSaida = state.checkin && !state.checkout && s > state.checkin;
    if (!escolhendoSaida && !dia.available && dia.on_request) {
      toast(`${WH.dataBR(s)} é sob consulta${WHATSAPP ? ' — fale com a gente pelo WhatsApp' : ''}.`);
      state.consulta = s;
      renderQuote();
      return;
    }
    if (!escolhendoSaida && !dia.available) {
      toast(`${WH.dataBR(s)} indisponível.`);
      return;
    }
    if (!escolhendoSaida) {
      state.checkin = s; state.checkout = null; state.orcamento = null; state.consulta = null;
      render();
      return;
    }

    /* Todas as noites entre check-in e check-out precisam estar livres. As que
       estiverem fora dos meses visíveis são pedidas agora. */
    if (WH.parse(s) - WH.parse(state.checkin) > 92 * 864e5) {
      toast('Para estadias acima de 90 noites, fale com a gente pelo WhatsApp.');
      return;
    }
    try {
      await carregarDias(state.checkin, s);
    } catch (e) {
      toast(e.message);
      return;
    }
    for (let cur = state.checkin; cur < s; cur = WH.addDays(cur, 1)) {
      const noite = diaDe(cur);
      if (!noite || !noite.available) {
        toast('Há datas ocupadas no intervalo. Escolha um novo check-in.');
        state.checkin = null; state.checkout = null; state.orcamento = null;
        render();
        return;
      }
    }
    state.checkout = s;
    render();
    orcar();
  }

  /* ─────────── Orçamento (POST /public/quotes) ─────────── */
  let pedidoAtual = 0;

  async function orcar() {
    if (!state.checkin || !state.checkout) { renderQuote(); return; }
    const u = unit();
    const chave = [u.unit_type_id, state.checkin, state.checkout, state.hospedes].join('|');
    const meu = ++pedidoAtual;
    state.orcamento = { chave, carregando: true };
    renderQuote();
    try {
      const dados = await WH.api.orcar({
        unit_type_id: u.unit_type_id, check_in: state.checkin,
        check_out: state.checkout, guests_count: state.hospedes
      });
      if (meu !== pedidoAtual) return;
      state.orcamento = { chave, dados };
    } catch (erro) {
      if (meu !== pedidoAtual) return;
      state.orcamento = { chave, erro };
    }
    renderQuote();
  }

  function cabecalho(u) {
    return `
      <div class="quote__head">
        <span class="quote__title">Seu orçamento</span>
        <span class="pill">Tabela vigente</span>
      </div>
      <div class="quote__unit"><b>${WH.esc(u.name)}</b>até ${u.capacity} hóspedes · check-in 14h · check-out 11h</div>`;
  }

  function blocoPolitica() {
    const P = state.politica;
    return `
      <div class="quote__signal">
        <div class="row"><span>Sinal para confirmar</span><b>${P.deposit_pct}%</b></div>
        <div class="row"><span>Saldo</span><b>até ${P.balance_due_days} dias antes</b></div>
        <div class="row"><span>Pré-reserva</span><b>segura ${P.hold_hours}h</b></div>
      </div>`;
  }

  function renderQuote() {
    const u = unit();
    const P = state.politica;
    const head = cabecalho(u);

    if (u.on_request || (state.consulta && !state.checkin)) {
      const link = linkConsulta(u, state.consulta, null);
      el.quote.innerHTML = head + `
        <div class="quote__hint quote__hint--warn">${u.on_request
          ? `${WH.esc(u.name)} tem valores sob consulta: cada pedido é montado com a nossa equipe.`
          : `${WH.dataBR(state.consulta)} tem valores sob consulta para ${WH.esc(u.name)}.`}</div>
        ${link ? `<div class="quote__actions"><a class="btn btn--dark" target="_blank" rel="noopener" href="${link}">Consultar no WhatsApp</a></div>` : ''}
        ${blocoPolitica()}`;
      return;
    }

    if (!state.checkin || !state.checkout) {
      el.quote.innerHTML = head + `
        <div class="quote__dates">
          <div class="quote__date"><div class="l">Check-in</div><div class="v">${state.checkin ? WH.dataCurta(state.checkin) : '—'}</div></div>
          <div class="quote__arrow">→</div>
          <div class="quote__date"><div class="l">Check-out</div><div class="v">—</div></div>
        </div>
        <p class="quote__empty">${state.checkin
          ? 'Agora selecione a data de check-out no calendário.'
          : 'Selecione a data de check-in no calendário para ver o valor da estadia.'}</p>
        ${blocoPolitica()}`;
      return;
    }

    const datas = `
      <div class="quote__dates">
        <div class="quote__date"><div class="l">Check-in</div><div class="v">${WH.dataCurta(state.checkin)}</div></div>
        <div class="quote__arrow">→</div>
        <div class="quote__date"><div class="l">Check-out</div><div class="v">${WH.dataCurta(state.checkout)}</div></div>
      </div>`;

    const o = state.orcamento;
    if (!o || o.carregando) {
      el.quote.innerHTML = head + datas + `<p class="quote__empty">Calculando com a tabela vigente…</p>`;
      return;
    }
    if (o.erro) {
      /* A mensagem é a da API (mínimo de noites, lotação etc.): é ela quem sabe a regra. */
      const consulta = o.erro.code === 'RATE_NOT_FOUND';
      const tipo = o.erro.code === 'MIN_STAY_NOT_MET' || consulta ? 'warn' : 'block';
      const msg = consulta ? 'Essas datas têm valores sob consulta. Fale com a gente e montamos o seu pedido.' : o.erro.message;
      const link = linkConsulta(u, state.checkin, state.checkout);
      el.quote.innerHTML = head + datas + `
        <div class="quote__hint quote__hint--${tipo}">${WH.esc(msg)}</div>
        ${blocoPolitica()}
        ${link ? `<div class="quote__actions"><a class="btn btn--secondary" target="_blank" rel="noopener" href="${link}">${consulta ? 'Consultar no WhatsApp' : 'Falar no WhatsApp'}</a></div>` : ''}`;
      return;
    }

    const c = o.dados;
    el.quote.innerHTML = head + datas + `
      <div class="quote__lines">
        ${c.lines.map(l => `
          <div class="quote__line quote__line--tag">
            <span><span class="tarifa-tag">${WH.esc(l.label || WH.rotulo(l.date_type))}</span> ${l.nights}×</span>
            <b>${WH.brl(l.subtotal_cents)}</b>
          </div>`).join('')}
        <div class="quote__line"><span>Taxa de limpeza</span><b>${WH.brl(c.cleaning_cents)}</b></div>
      </div>

      <div class="quote__sep"></div>

      <div class="quote__total">
        <span class="l">Total · ${c.night_count} ${c.night_count === 1 ? 'noite' : 'noites'}</span>
        <span class="v">${WH.brl(c.total_cents)}</span>
      </div>

      <div class="quote__signal">
        <div class="row"><span>Sinal (${P.deposit_pct}%) para confirmar</span><b>${WH.brl(c.deposit_cents)}</b></div>
        <div class="row"><span>Saldo até ${P.balance_due_days} dias antes</span><b>${WH.brl(c.balance_cents)}</b></div>
        <div class="row"><span>Diária média</span><b>${WH.brl(c.avg_nightly_cents)}</b></div>
      </div>

      <div class="quote__actions">
        ${WHATSAPP ? `<a class="btn btn--dark" data-wa target="_blank" rel="noopener" href="#">Pedir pré-reserva no WhatsApp</a>` : ''}
      </div>
      <p class="quote__note">Valores da tabela vigente. A pré-reserva segura a data por ${P.hold_hours}h; sem o sinal, a data é liberada automaticamente.</p>`;

    const wa = el.quote.querySelector('[data-wa]');
    if (wa) {
      const linhas = [
        'Olá! Quero pré-reservar na White House:', '',
        `*${u.name}*`,
        `Check-in: ${WH.dataBR(state.checkin)}`,
        `Check-out: ${WH.dataBR(state.checkout)}`,
        `Hóspedes: ${state.hospedes}`,
        `Noites: ${c.night_count}`, '',
        `Total: ${WH.brl(c.total_cents)}`,
        `Sinal (${P.deposit_pct}%): ${WH.brl(c.deposit_cents)}`
      ];
      wa.href = `https://wa.me/${WHATSAPP}?text=` + encodeURIComponent(linhas.join('\n'));
    }
  }

  /* ─────────── Indicadores públicos do mês ─────────── */
  function renderKPIs() {
    const P = state.politica;
    const total = new Date(state.ano, state.mes + 1, 0).getDate();
    let livres = 0, menor = null;
    for (let d = 1; d <= total; d++) {
      const s = WH.key(state.ano, state.mes, d);
      if (s < state.hoje) continue;
      const dia = diaDe(s);
      if (dia && dia.available) {
        livres++;
        if (dia.price_cents != null && (menor === null || dia.price_cents < menor)) menor = dia.price_cents;
      }
    }
    const cards = [
      { v: livres, l: 'Noites livres em ' + WH.MESES[state.mes], d: unit().name },
      { v: menor === null ? '—' : WH.brl(menor), l: 'Diária a partir de', d: 'no mês selecionado' },
      { v: P.deposit_pct + '%', l: 'Sinal para confirmar', d: 'saldo até ' + P.balance_due_days + ' dias antes' },
      { v: P.hold_hours + 'h', l: 'Pré-reserva sem pagamento', d: 'a data fica segura' }
    ];
    el.kpis.innerHTML = cards.map(c => `
      <div class="kpi"><div class="kpi__v">${c.v}</div><div class="kpi__l">${c.l}</div><div class="kpi__d">${WH.esc(c.d)}</div></div>`).join('');
  }

  /* ─────────── Tabela de tarifas (da tabela vigente) ─────────── */
  function renderRates() {
    el.rates.innerHTML = state.produtos.map(p => {
      const por = {};
      for (const r of p.rates) por[r.date_type] = r.price_cents;
      const pacotes = (p.packages || []).slice().sort((x, y) => x.date_types.join() === y.date_types.join() ? x.nights - y.nights : x.date_types.join() > y.date_types.join() ? -1 : 1).map(k =>
        `${k.nights} diárias (${k.date_types.map(t => WH.rotulo(t).toLowerCase()).join(' / ')}): ${WH.brl(k.total_cents)}`);
      return `
      <tr>
        <td>${WH.esc(p.name)}${pacotes.length ? `<small class="rates__pacotes">Pacotes: ${WH.esc(pacotes.join(' · '))}</small>` : ''}</td>
        <td>${p.capacity} hóspedes</td>
        ${WH.TIPOS.map(t => `<td${t === 'reveillon' || t === 'carnaval' ? ' class="hi"' : ''}>${por[t] != null ? WH.brl(por[t]) : 'consulta'}</td>`).join('')}
      </tr>`;
    }).join('');

    /* A estadia mínima agora varia por acomodação (Pool Suítes e villas pedem
       2 diárias, duplex aceita 1 em dia comum): uma frase só com "o maior
       mínimo" mentiria para metade da tabela. A do produto escolhido sai da
       API e aparece no calendário (título de cada dia) e na recusa do orçamento. */
    if (el.minimos && state.produtos.length) {
      el.minimos.textContent = 'A estadia mínima varia por acomodação e tipo de data — passe o mouse sobre o dia no calendário para ver a de cada data.';
    }
  }

  function render() {
    renderCalendars();
    renderQuote();
    renderKPIs();
  }

  /* ─────────── Início ─────────── */
  async function iniciar() {
    try {
      const [produtos, politica] = await Promise.all([WH.api.produtos(), WH.api.politica()]);
      state.produtos = produtos;
      state.politica = politica;
      state.hoje = politica.today;
    } catch (e) {
      falhaGeral();
      return;
    }
    if (!state.produtos.length) { falhaGeral(); return; }

    const pedido = params.get('produto');
    state.produto = state.produtos.some(p => p.code === pedido) ? pedido
      : WH.agrupar(state.produtos)[0].produtos[0].code;
    state.hospedes = Math.min(2, unit().capacity);
    const hoje = WH.parse(state.hoje);
    state.ano = hoje.getFullYear(); state.mes = hoje.getMonth();

    /* Deep link opcional: ?produto=grand-villa&checkin=2026-12-28&checkout=2027-01-02 */
    const ci = params.get('checkin'), co = params.get('checkout');
    const valido = /^\d{4}-\d{2}-\d{2}$/;
    if (ci && co && valido.test(ci) && valido.test(co) && ci < co && ci >= state.hoje) {
      const d = WH.parse(ci);
      state.ano = d.getFullYear(); state.mes = d.getMonth();
    }

    initControls();
    renderRates();
    await recarregar();

    if (ci && co && valido.test(ci) && valido.test(co) && ci < co && ci >= state.hoje) {
      const dia = diaDe(ci);
      if (dia && dia.available) {
        state.checkin = ci;
        await onPick(co);
      }
    }
  }

  iniciar();
})();
