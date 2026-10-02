/* ═══════════════════════════════════════════════════════════
   Back-office White House — painel da gestão comercial.
   Dados 100% mocados (window.WH). Nada persiste no servidor.
   ═══════════════════════════════════════════════════════════ */
(function () {
  'use strict';
  if (!window.WH) return;

  const P = WH.politica;
  const $  = s => document.querySelector(s);
  const $$ = s => Array.prototype.slice.call(document.querySelectorAll(s));

  const state = {
    produto: 'cobertura',
    hospedes: 6,
    ano: WH.parse(WH.HOJE).getFullYear(),
    mes: WH.parse(WH.HOJE).getMonth(),
    checkin: null,
    checkout: null,
    desconto: 0,
    filtro: 'todas'
  };

  const unit = () => WH.units.find(u => u.id === state.produto);
  const nomeUnit = id => (WH.units.find(u => u.id === id) || {}).nome || id;
  const curto = n => n >= 1000 ? (n / 1000).toFixed(n % 1000 === 0 ? 0 : 1).replace('.', ',') + 'k' : String(n);
  const mesKey = () => WH.key(state.ano, state.mes, 1).slice(0, 7);

  let toastTimer;
  function toast(msg) {
    const t = $('[data-toast]');
    t.textContent = msg;
    t.classList.add('is-visible');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => t.classList.remove('is-visible'), 4200);
  }

  /* ════════ Router ════════ */
  const VIEWS = {
    painel:     ['Painel', 'Visão geral da operação'],
    calendario: ['Calendário', 'Disponibilidade, orçamento e pré-reserva'],
    reservas:   ['Reservas', 'Agenda única de toda a casa'],
    leads:      ['Leads & funil', 'Do primeiro contato à reserva confirmada'],
    tarifas:    ['Tabela de tarifas', 'Preço por produto e tipo de data'],
    politica:   ['Política comercial', 'Sinal, prazos, alçadas e mínimos']
  };

  function rotear() {
    const id = (location.hash.replace('#/', '') || 'painel');
    const alvo = VIEWS[id] ? id : 'painel';
    $$('.view').forEach(v => v.classList.toggle('is-active', v.id === 'v-' + alvo));
    $$('.side__link').forEach(a => a.classList.toggle('is-active', a.getAttribute('href') === '#/' + alvo));
    $('[data-view-title]').innerHTML = VIEWS[alvo][0] + '<small>' + VIEWS[alvo][1] + '</small>';
    window.scrollTo(0, 0);
  }

  /* ════════ Painel ════════ */
  function renderPainel() {
    const grupos = ['apto-2s', 'suite-piscina', 'cobertura'];
    const dias = new Date(state.ano, state.mes + 1, 0).getDate();
    let ocupadas = 0;
    for (let d = 1; d <= dias; d++) {
      const s = WH.key(state.ano, state.mes, d);
      grupos.forEach(g => { if (WH.status(g, s).st !== 'livre') ocupadas++; });
    }
    const ocup = Math.round(ocupadas / (dias * grupos.length) * 100);

    const doMes = WH.reservas.filter(r => r.de.slice(0, 7) === mesKey());
    const receita = doMes.filter(r => r.status === 'confirmada').reduce((a, r) => a + r.valor, 0);
    const pre = doMes.filter(r => r.status === 'pre-reserva');
    const confirmadas = doMes.filter(r => r.status === 'confirmada');
    const ticket = confirmadas.length ? Math.round(receita / confirmadas.length) : 0;
    const conv = Math.round(WH.leads.filter(l => l.etapa === 'confirmada').length / WH.leads.length * 100);

    $('[data-metrics]').innerHTML = [
      { l: 'Ocupação · ' + WH.MESES[state.mes], v: ocup + '%', d: ocupadas + ' de ' + (dias * grupos.length) + ' noites disponíveis' },
      { l: 'Receita confirmada', v: WH.brl(receita), d: '<b>' + confirmadas.length + ' reservas</b> no mês' },
      { l: 'Ticket médio', v: ticket ? WH.brl(ticket) : '—', d: 'por reserva confirmada' },
      { l: 'Conversão do funil', v: conv + '%', d: WH.leads.length + ' leads nos últimos 30 dias' }
    ].map(m => `<div class="metric">
        <span class="metric__l">${m.l}</span>
        <span class="metric__v">${m.v}</span>
        <span class="metric__d">${m.d}</span>
      </div>`).join('');

    /* Alertas acionáveis */
    const alertas = [];
    pre.forEach(r => alertas.push({
      i: '⏳', t: 'Pré-reserva sem sinal',
      d: `${r.cliente} · ${nomeUnit(r.unit)} · ${WH.dataCurta(r.de)} → ${WH.dataCurta(r.ate)}. Expira em ${P.preReservaHoras}h — cobrar ${WH.brl(Math.round(r.valor * P.sinalPercentual / 100))} de sinal.`
    }));
    WH.reservas.filter(r => r.status === 'confirmada' && WH.diffDays(WH.HOJE, r.de) > 0 && WH.diffDays(WH.HOJE, r.de) <= P.prazoSaldoDias)
      .forEach(r => alertas.push({
        i: '💰', t: 'Saldo a receber',
        d: `${r.cliente} entra em ${WH.diffDays(WH.HOJE, r.de)} dia(s). Saldo de ${WH.brl(Math.round(r.valor * (100 - P.sinalPercentual) / 100))} vence hoje.`
      }));
    ['2026-12-31', '2027-02-07'].forEach(s => {
      if (WH.status('completa', s).st === 'livre') {
        const t = WH.tarifa(s);
        alertas.push({ i: '★', t: t.label + ' ainda sem exclusividade vendida',
          d: `White House Completa livre em ${WH.dataCurta(s)} — ${WH.brl(WH.preco('completa', s))} a diária. Data de maior procura do ano.` });
      }
    });

    $('[data-alertas]').innerHTML = `
      <div class="card__head"><div class="card__title">Precisa da sua atenção<small>${alertas.length} pendência(s) hoje</small></div></div>
      ${alertas.length ? alertas.map(a => `
        <div class="notice" style="margin-bottom:var(--space-2)">
          <span aria-hidden="true">${a.i}</span>
          <span><b>${a.t}</b>${a.d}</span>
        </div>`).join('') : '<p class="empty">Nada pendente.</p>'}`;

    /* Ocupação por produto */
    $('[data-ocupacao-produto]').innerHTML = `
      <div class="card__head"><div class="card__title">Ocupação por produto<small>${WH.MESES[state.mes]} de ${state.ano}</small></div></div>
      <div class="bars">${WH.units.map(u => {
        let oc = 0;
        for (let d = 1; d <= dias; d++) if (WH.status(u.id, WH.key(state.ano, state.mes, d)).st !== 'livre') oc++;
        const pct = Math.round(oc / dias * 100);
        return `<div class="bar__row">
            <span>${u.nome}</span>
            <span class="bar__track"><span class="bar__fill${pct >= 60 ? ' bar__fill--gold' : ''}" style="width:${pct}%"></span></span>
            <span class="bar__v">${pct}% · ${oc}n</span>
          </div>`;
      }).join('')}</div>`;

    /* Receita por mês */
    const meses = [];
    for (let i = 0; i < 7; i++) {
      const d = new Date(2026, 7 + i, 1);
      const k = WH.key(d.getFullYear(), d.getMonth(), 1).slice(0, 7);
      const v = WH.reservas.filter(r => r.de.slice(0, 7) === k && r.status === 'confirmada').reduce((a, r) => a + r.valor, 0);
      meses.push({ k: k, l: WH.MESES[d.getMonth()].slice(0, 3), v: v, atual: k === mesKey() });
    }
    const max = Math.max.apply(null, meses.map(m => m.v)) || 1;
    $('[data-receita]').innerHTML = `
      <div class="card__head"><div class="card__title">Receita confirmada por mês<small>Base gerencial que hoje não existe — começa a nascer aqui</small></div></div>
      <div class="chart">${meses.map(m => `
        <div class="chart__col" title="${WH.brl(m.v)}">
          <span class="chart__v">${m.v ? curto(m.v) : '—'}</span>
          <span class="chart__bar${m.atual ? ' chart__bar--hi' : ''}" style="height:${Math.round(m.v / max * 100)}%"></span>
          <span class="chart__l">${m.l}</span>
        </div>`).join('')}</div>`;

    /* Movimento dos próximos 7 dias */
    const mov = [];
    WH.reservas.forEach(r => {
      const dIn = WH.diffDays(WH.HOJE, r.de), dOut = WH.diffDays(WH.HOJE, r.ate);
      if (dIn >= 0 && dIn <= 7) mov.push({ d: r.de, tipo: 'Check-in', r: r, quando: dIn });
      if (dOut >= 0 && dOut <= 7) mov.push({ d: r.ate, tipo: 'Check-out', r: r, quando: dOut });
    });
    mov.sort((a, b) => a.d.localeCompare(b.d));
    $('[data-movimento]').innerHTML = `
      <div class="card__head"><div class="card__title">Movimento dos próximos 7 dias<small>O que a operação local precisa preparar</small></div></div>
      <div class="tbl-wrap">
        <table class="tbl">
          <thead><tr><th>Data</th><th>Movimento</th><th>Produto</th><th>Cliente</th><th class="num">Valor</th><th>Status</th></tr></thead>
          <tbody>${mov.length ? mov.map(m => `
            <tr>
              <td class="tbl__main">${WH.dataCurta(m.d)}<span class="tbl__sub">${m.quando === 0 ? 'hoje' : 'em ' + m.quando + ' dia(s)'}</span></td>
              <td><span class="chip ${m.tipo === 'Check-in' ? 'chip--free' : 'chip--ghost'}">${m.tipo}</span></td>
              <td>${nomeUnit(m.r.unit)}</td>
              <td>${m.r.cliente}<span class="tbl__sub">${m.r.origem}</span></td>
              <td class="num">${m.r.valor ? WH.brl(m.r.valor) : '—'}</td>
              <td>${chipStatus(m.r.status)}</td>
            </tr>`).join('') : '<tr><td colspan="6" class="empty">Sem movimento nos próximos 7 dias.</td></tr>'}
          </tbody>
        </table>
      </div>`;
  }

  function chipStatus(st) {
    const m = { 'confirmada': ['chip--free', 'Confirmada'], 'pre-reserva': ['chip--hold', 'Pré-reserva'], 'bloqueio': ['chip--block', 'Bloqueio'] };
    return `<span class="chip ${m[st][0]}">${m[st][1]}</span>`;
  }

  /* ════════ Calendário ════════ */
  const CLASSE = { 'livre': 'free', 'pre-reserva': 'hold', 'confirmada': 'busy', 'bloqueio': 'block' };

  function initControles() {
    const sel = $('[data-produto]');
    sel.innerHTML = WH.units.map(u => `<option value="${u.id}">${u.nome} · até ${u.capacidade}</option>`).join('');
    sel.value = state.produto;
    renderHospedes();
    sel.addEventListener('change', () => {
      state.produto = sel.value;
      state.checkin = state.checkout = null;
      state.hospedes = Math.min(state.hospedes, unit().capacidade);
      renderHospedes(); renderCalendario();
    });
    $('[data-hospedes]').addEventListener('change', e => { state.hospedes = Number(e.target.value); renderQuote(); });
    $('[data-prev]').addEventListener('click', () => shiftMes(-1));
    $('[data-next]').addEventListener('click', () => shiftMes(1));
  }

  function renderHospedes() {
    const max = unit().capacidade;
    let out = '';
    for (let i = 1; i <= max; i++) out += `<option value="${i}">${i} hóspede${i > 1 ? 's' : ''}</option>`;
    const h = $('[data-hospedes]');
    h.innerHTML = out;
    h.value = String(Math.min(state.hospedes, max));
    state.hospedes = Number(h.value);
  }

  function shiftMes(n) {
    const d = new Date(state.ano, state.mes + n, 1);
    const min = new Date(WH.parse(WH.HOJE).getFullYear(), WH.parse(WH.HOJE).getMonth(), 1);
    if (d < min || d > new Date(2027, 11, 1)) return;
    state.ano = d.getFullYear(); state.mes = d.getMonth();
    renderCalendario(); renderPainel();
  }

  function renderCalendario() {
    renderMapa();
    let html = '';
    for (let i = 0; i < 2; i++) {
      const d = new Date(state.ano, state.mes + i, 1);
      html += mesHTML(d.getFullYear(), d.getMonth());
    }
    $('[data-calendars]').innerHTML = html;
    $$('.day[data-d]').forEach(b => b.addEventListener('click', () => escolher(b.dataset.d, b.dataset.st)));
    renderQuote();
  }

  function mesHTML(ano, m) {
    const dias = new Date(ano, m + 1, 0).getDate();
    const offset = new Date(ano, m, 1).getDay();
    const u = unit();
    let livres = 0, cells = '';
    for (let i = 0; i < offset; i++) cells += '<span class="day day--empty"></span>';
    for (let d = 1; d <= dias; d++) {
      const s = WH.key(ano, m, d);
      const st = WH.status(u.id, s).st;
      const t = WH.tarifa(s);
      const past = s < WH.HOJE;
      if (st === 'livre' && !past) livres++;
      const cls = ['day', 'day--' + CLASSE[st]];
      if (past) cls.push('day--past');
      if (['feriado', 'reveillon', 'carnaval', 'alta'].indexOf(t.tipo) !== -1) cls.push('day--special');
      if (s === WH.HOJE) cls.push('day--today');
      if (s === state.checkin || s === state.checkout) cls.push('day--sel');
      else if (state.checkin && state.checkout && s > state.checkin && s < state.checkout) cls.push('day--in-range');
      const r = WH.status(u.id, s).r;
      const titulo = st === 'livre' ? `${t.label} · ${WH.brl(WH.preco(u.id, s))}` : `${r ? r.cliente : ''} · ${st}`;
      cells += `<button type="button" class="${cls.join(' ')}" data-d="${s}" data-st="${st}" title="${titulo}">
          <span class="day__n">${d}</span>
          <span class="day__p">${st === 'livre' && !past ? curto(WH.preco(u.id, s)) : '—'}</span>
        </button>`;
    }
    return `<div class="calendar">
        <h3 class="calendar__title">${WH.MESES[m]} ${ano}</h3>
        <p class="calendar__sub">${livres} noite(s) livre(s) · ${unit().nome}</p>
        <div class="calendar__weekdays"><span>dom</span><span>seg</span><span>ter</span><span>qua</span><span>qui</span><span>sex</span><span>sáb</span></div>
        <div class="calendar__grid">${cells}</div>
      </div>`;
  }

  function escolher(s, st) {
    if (s < WH.HOJE) { toast('Data já passou.'); return; }
    if (st !== 'livre') {
      const r = WH.status(state.produto, s).r;
      toast(`${s.split('-').reverse().join('/')} ocupado — ${r ? r.cliente : 'reservado'}.`);
      return;
    }
    if (!state.checkin || state.checkout || s <= state.checkin) {
      state.checkin = s; state.checkout = null;
    } else {
      for (let c = state.checkin; c < s; c = WH.addDays(c, 1)) {
        if (WH.status(state.produto, c).st !== 'livre') {
          toast('Há datas ocupadas no intervalo — check-in redefinido.');
          state.checkin = s; state.checkout = null;
          renderCalendario(); return;
        }
      }
      state.checkout = s;
    }
    renderCalendario();
  }

  function calcular() {
    const u = unit();
    const linhas = {};
    let subtotal = 0, noites = 0, minExigido = 1;
    for (let c = state.checkin; c < state.checkout; c = WH.addDays(c, 1)) {
      const t = WH.tarifa(c), v = u.rates[t.tipo];
      linhas[t.tipo] = linhas[t.tipo] || { label: rotulo(t.tipo), qtd: 0, unit: v };
      linhas[t.tipo].qtd++; subtotal += v; noites++;
      minExigido = Math.max(minExigido, P.minNoites[t.tipo] || 1);
    }
    const limpeza = P.taxaLimpeza[u.id] || 0;
    const descontoV = Math.round(subtotal * state.desconto / 100);
    const total = subtotal - descontoV + limpeza;
    return { linhas: Object.keys(linhas).map(k => linhas[k]), subtotal, limpeza, descontoV, total, noites, minExigido,
             sinal: Math.round(total * P.sinalPercentual / 100) };
  }

  const rotulo = t => ({ normal: 'Diária normal', fds: 'Fim de semana', feriado: 'Feriado',
                         alta: 'Alta temporada', reveillon: 'Réveillon', carnaval: 'Carnaval' }[t] || t);

  function renderQuote() {
    const u = unit();
    const q = $('[data-quote]');
    const head = `
      <div class="quote__head">
        <span class="quote__title">Nova reserva</span>
        <span class="pill">Orçamento</span>
      </div>
      <div class="quote__unit"><b>${u.nome}</b>até ${u.capacidade} hóspedes · check-in 14h · check-out 11h</div>`;

    if (!state.checkin || !state.checkout) {
      q.innerHTML = head + `
        <div class="quote__dates">
          <div class="quote__date"><div class="l">Check-in</div><div class="v">${state.checkin ? WH.dataCurta(state.checkin) : '—'}</div></div>
          <div class="quote__arrow">→</div>
          <div class="quote__date"><div class="l">Check-out</div><div class="v">—</div></div>
        </div>
        <p class="quote__empty">${state.checkin ? 'Selecione o check-out no calendário.' : 'Selecione o check-in no calendário.'}</p>
        <div class="quote__signal">
          <div class="row"><span>Sinal para confirmar</span><b>${P.sinalPercentual}%</b></div>
          <div class="row"><span>Saldo</span><b>até ${P.prazoSaldoDias} dias antes</b></div>
          <div class="row"><span>Pré-reserva segura</span><b>${P.preReservaHoras}h</b></div>
        </div>`;
      return;
    }

    const c = calcular();
    const capOK = state.hospedes <= u.capacidade;
    const minOK = c.noites >= c.minExigido;
    const hint = state.desconto <= P.descontoGestao
      ? ['ok', `Dentro da alçada da gestão (até ${P.descontoGestao}%) — pode fechar agora.`]
      : state.desconto <= P.descontoProprietario
        ? ['warn', `Acima de ${P.descontoGestao}% — precisa de aprovação do proprietário.`]
        : ['block', `Acima de ${P.descontoProprietario}% — fora da política comercial.`];

    q.innerHTML = head + `
      <div class="quote__dates">
        <div class="quote__date"><div class="l">Check-in</div><div class="v">${WH.dataCurta(state.checkin)}</div></div>
        <div class="quote__arrow">→</div>
        <div class="quote__date"><div class="l">Check-out</div><div class="v">${WH.dataCurta(state.checkout)}</div></div>
      </div>
      <div class="quote__lines">
        ${c.linhas.map(l => `<div class="quote__line quote__line--tag">
            <span><span class="tarifa-tag">${l.label}</span> ${l.qtd}×</span><b>${WH.brl(l.unit * l.qtd)}</b>
          </div>`).join('')}
        <div class="quote__line"><span>Taxa de limpeza</span><b>${WH.brl(c.limpeza)}</b></div>
        ${c.descontoV ? `<div class="quote__line"><span>Desconto (${state.desconto}%)</span><b style="color:var(--c-free)">− ${WH.brl(c.descontoV)}</b></div>` : ''}
      </div>
      <div class="quote__sep"></div>
      <div class="discount">
        <div class="discount__head"><span>Desconto negociado</span><b>${state.desconto}%</b></div>
        <input type="range" min="0" max="15" step="1" value="${state.desconto}" data-desc />
        <div class="discount__hint discount__hint--${hint[0]}">${hint[1]}</div>
      </div>
      <div class="quote__sep"></div>
      <div class="quote__total">
        <span class="l">Total · ${c.noites} noite(s)</span>
        <span class="v">${WH.brl(c.total)}</span>
      </div>
      <div class="quote__signal">
        <div class="row"><span>Sinal (${P.sinalPercentual}%)</span><b>${WH.brl(c.sinal)}</b></div>
        <div class="row"><span>Saldo até ${P.prazoSaldoDias} dias antes</span><b>${WH.brl(c.total - c.sinal)}</b></div>
        <div class="row"><span>Diária média</span><b>${WH.brl(Math.round(c.total / c.noites))}</b></div>
      </div>
      ${!minOK ? `<div class="discount__hint discount__hint--warn">Mínimo do período: ${c.minExigido} noites.</div>` : ''}
      ${!capOK ? `<div class="discount__hint discount__hint--block">${state.hospedes} hóspedes excede a capacidade (${u.capacidade}).</div>` : ''}
      <div class="quote__actions">
        <button class="btn btn--dark" data-pre ${(!minOK || !capOK || hint[0] === 'block') ? 'disabled style="opacity:.5;cursor:not-allowed"' : ''}>Gerar pré-reserva (${P.preReservaHoras}h)</button>
        <button class="btn btn--secondary" data-conf ${(!minOK || !capOK || hint[0] === 'block') ? 'disabled style="opacity:.5;cursor:not-allowed"' : ''}>Registrar sinal e confirmar</button>
        <a class="btn btn--ghost" style="justify-content:center" data-wa target="_blank" rel="noopener" href="#">Enviar orçamento no WhatsApp</a>
      </div>
      <p class="quote__note">A pré-reserva bloqueia a data por ${P.preReservaHoras}h. Sem sinal, o sistema libera automaticamente.</p>`;

    const range = q.querySelector('[data-desc]');
    range.addEventListener('input', () => { state.desconto = Number(range.value); renderQuote(); });

    function gravar(status, msg) {
      WH.reservas.push({ unit: state.produto, de: state.checkin, ate: state.checkout, status: status,
                         cliente: status === 'pre-reserva' ? 'Pré-reserva (balcão)' : 'Reserva (balcão)',
                         valor: c.total, origem: 'Painel' });
      toast(msg);
      state.checkin = state.checkout = null; state.desconto = 0;
      renderCalendario(); renderPainel(); renderReservas(); renderBadges();
    }
    const pre = q.querySelector('[data-pre]');
    if (pre && !pre.disabled) pre.addEventListener('click', () =>
      gravar('pre-reserva', `Pré-reserva criada · ${WH.brl(c.total)} · expira em ${P.preReservaHoras}h.`));
    const conf = q.querySelector('[data-conf]');
    if (conf && !conf.disabled) conf.addEventListener('click', () =>
      gravar('confirmada', `Reserva confirmada · sinal de ${WH.brl(c.sinal)} registrado.`));

    q.querySelector('[data-wa]').href = 'https://wa.me/5586999999999?text=' +
      `Orçamento White House Village%0A%0A*${u.nome}*%0A${state.checkin.split('-').reverse().join('/')} → ${state.checkout.split('-').reverse().join('/')}%0A${c.noites} noites · ${state.hospedes} hóspedes%0A%0ATotal: ${WH.brl(c.total)}%0ASinal (${P.sinalPercentual}%25): ${WH.brl(c.sinal)}`;
  }

  /* Mapa produto × dias do mês */
  function renderMapa() {
    const dias = new Date(state.ano, state.mes + 1, 0).getDate();
    let head = '';
    for (let d = 1; d <= dias; d++) head += `<span class="occ__d">${d}</span>`;
    const linhas = WH.units.map(u => {
      let cells = '';
      for (let d = 1; d <= dias; d++) {
        const s = WH.key(state.ano, state.mes, d);
        const st = WH.status(u.id, s).st;
        const cls = ['occ__d'];
        if (st !== 'livre') cls.push('occ__d--' + CLASSE[st]);
        if (s < WH.HOJE) cls.push('occ__d--past');
        const r = WH.status(u.id, s).r;
        cells += `<span class="${cls.join(' ')}" title="${d}/${state.mes + 1} — ${r ? r.cliente : 'livre · ' + WH.brl(WH.preco(u.id, s))}"></span>`;
      }
      return `<div class="occ__row">
          <span class="occ__name">${u.nome}<small>até ${u.capacidade} hóspedes</small></span>
          <span class="occ__days">${cells}</span>
        </div>`;
    }).join('');

    $('[data-mapa]').innerHTML = `
      <div class="card__head">
        <div class="card__title">Mapa de ocupação<small>${WH.MESES[state.mes]} de ${state.ano} — reservar a casa completa trava as unidades e vice-versa</small></div>
        <div class="card__actions">
          <span class="chip chip--free">Livre</span>
          <span class="chip chip--hold">Pré-reserva</span>
          <span class="chip chip--busy">Reservado</span>
          <span class="chip chip--block">Bloqueio</span>
        </div>
      </div>
      <div class="occ">
        <div class="occ__grid">
          <div class="occ__head"><span></span><span class="occ__days">${head}</span></div>
          ${linhas}
        </div>
      </div>`;
  }

  /* ════════ Reservas ════════ */
  function renderReservas() {
    const futuras = WH.reservas.filter(r => r.ate >= WH.HOJE).sort((a, b) => a.de.localeCompare(b.de));
    const lista = state.filtro === 'todas' ? futuras : futuras.filter(r => r.status === state.filtro);

    $('[data-metrics-reservas]').innerHTML = [
      { l: 'Reservas futuras', v: futuras.length, d: 'a partir de hoje' },
      { l: 'Confirmadas', v: futuras.filter(r => r.status === 'confirmada').length, d: 'com sinal pago' },
      { l: 'Pré-reservas', v: futuras.filter(r => r.status === 'pre-reserva').length, d: `expiram em ${P.preReservaHoras}h` },
      { l: 'Receita contratada', v: WH.brl(futuras.filter(r => r.status === 'confirmada').reduce((a, r) => a + r.valor, 0)), d: 'reservas futuras confirmadas' }
    ].map(m => `<div class="metric"><span class="metric__l">${m.l}</span><span class="metric__v">${m.v}</span><span class="metric__d">${m.d}</span></div>`).join('');

    $('[data-filtros]').innerHTML = [['todas', 'Todas'], ['confirmada', 'Confirmadas'], ['pre-reserva', 'Pré-reservas'], ['bloqueio', 'Bloqueios']]
      .map(f => `<button class="filter${state.filtro === f[0] ? ' is-active' : ''}" data-f="${f[0]}">${f[1]}</button>`).join('');
    $$('[data-f]').forEach(b => b.addEventListener('click', () => { state.filtro = b.dataset.f; renderReservas(); }));

    $('[data-tabela-reservas]').innerHTML = `
      <table class="tbl">
        <thead><tr><th>Período</th><th>Produto</th><th>Cliente / origem</th><th class="num">Valor</th><th class="num">Sinal</th><th>Status</th><th></th></tr></thead>
        <tbody>${lista.length ? lista.map((r, i) => `
          <tr>
            <td class="tbl__main">${WH.dataCurta(r.de)} → ${WH.dataCurta(r.ate)}<span class="tbl__sub">${WH.diffDays(r.de, r.ate)} noite(s)</span></td>
            <td>${nomeUnit(r.unit)}</td>
            <td class="tbl__main">${r.cliente}<span class="tbl__sub">${r.origem}</span></td>
            <td class="num">${r.valor ? WH.brl(r.valor) : '—'}</td>
            <td class="num">${r.valor ? WH.brl(Math.round(r.valor * P.sinalPercentual / 100)) : '—'}</td>
            <td>${chipStatus(r.status)}</td>
            <td class="num">${r.status === 'pre-reserva'
                ? `<button class="btn-sm" data-confirmar="${WH.reservas.indexOf(r)}">Confirmar sinal</button>`
                : r.status === 'confirmada'
                  ? `<button class="btn-sm btn-sm--ghost" data-cancelar="${WH.reservas.indexOf(r)}">Cancelar</button>` : ''}</td>
          </tr>`).join('') : '<tr><td colspan="7" class="empty">Nenhuma reserva neste filtro.</td></tr>'}
        </tbody>
      </table>`;

    $$('[data-confirmar]').forEach(b => b.addEventListener('click', () => {
      const r = WH.reservas[Number(b.dataset.confirmar)];
      r.status = 'confirmada';
      toast(`Sinal de ${WH.brl(Math.round(r.valor * P.sinalPercentual / 100))} registrado — ${r.cliente} confirmado.`);
      renderReservas(); renderCalendario(); renderPainel(); renderBadges();
    }));
    $$('[data-cancelar]').forEach(b => b.addEventListener('click', () => {
      const i = Number(b.dataset.cancelar), r = WH.reservas[i];
      const dias = WH.diffDays(WH.HOJE, r.de);
      const regra = dias >= 30 ? 'devolução integral do sinal'
                  : dias >= 7 ? 'retenção de 50% do sinal' : 'retenção integral do sinal';
      WH.reservas.splice(i, 1);
      toast(`Reserva cancelada (${dias} dias de antecedência) — política: ${regra}. Data liberada no calendário.`);
      renderReservas(); renderCalendario(); renderPainel(); renderBadges();
    }));
  }

  /* ════════ Leads ════════ */
  function renderLeads() {
    const ativos = WH.leads.filter(l => ['confirmada', 'perdida'].indexOf(l.etapa) === -1).length;
    const ganhos = WH.leads.filter(l => l.etapa === 'confirmada');
    const perdidos = WH.leads.filter(l => l.etapa === 'perdida');
    const receita = ganhos.reduce((a, l) => a + l.valor, 0);

    $('[data-metrics-leads]').innerHTML = [
      { l: 'Leads em aberto', v: ativos, d: 'aguardando ação da gestão' },
      { l: 'Convertidos', v: ganhos.length, d: WH.brl(receita) + ' em reservas' },
      { l: 'Perdidos', v: perdidos.length, d: 'com motivo registrado' },
      { l: 'Taxa de conversão', v: Math.round(ganhos.length / WH.leads.length * 100) + '%', d: 'últimos 30 dias' }
    ].map(m => `<div class="metric"><span class="metric__l">${m.l}</span><span class="metric__v">${m.v}</span><span class="metric__d">${m.d}</span></div>`).join('');

    $('[data-kanban]').innerHTML = WH.ETAPAS.map(e => {
      const itens = WH.leads.filter(l => l.etapa === e.id);
      return `<div class="col">
          <div class="col__head"><span class="col__t">${e.label}</span><span class="col__n">${itens.length}</span></div>
          ${itens.map(l => `
            <article class="lead lead--${l.etapa}">
              <span class="lead__n">${l.nome}</span>
              <span class="lead__m"><span>${nomeUnit(l.produto)}</span></span>
              <span class="lead__m"><span>${l.datas}</span><span>${l.origem}</span></span>
              <span class="lead__v">${WH.brl(l.valor)}</span>
              <span class="lead__m"><span>${l.tel}</span><span>${l.dias}d no funil</span></span>
            </article>`).join('') || '<p class="empty" style="padding:var(--space-4);font-size:12px">—</p>'}
        </div>`;
    }).join('');

    $('[data-perdas]').innerHTML = `
      <div class="card__head"><div class="card__title">Motivos de perda<small>O dado que hoje não existe — e que orienta preço e estoque</small></div></div>
      <div class="tbl-wrap">
        <table class="tbl">
          <thead><tr><th>Lead</th><th>Produto</th><th>Datas</th><th class="num">Valor perdido</th><th>Motivo</th></tr></thead>
          <tbody>${perdidos.map(l => `
            <tr>
              <td class="tbl__main">${l.nome}<span class="tbl__sub">${l.origem}</span></td>
              <td>${nomeUnit(l.produto)}</td>
              <td>${l.datas}</td>
              <td class="num">${WH.brl(l.valor)}</td>
              <td><span class="chip chip--busy">${l.motivo}</span></td>
            </tr>`).join('')}
          </tbody>
        </table>
      </div>`;
  }

  /* ════════ Tarifas ════════ */
  const TIPOS = [['normal', 'Normal'], ['fds', 'Fim de semana'], ['feriado', 'Feriado'], ['alta', 'Alta temporada'], ['reveillon', 'Réveillon'], ['carnaval', 'Carnaval']];

  function renderTarifas() {
    $('[data-tabela-tarifas]').innerHTML = `
      <table class="tbl">
        <thead><tr><th>Produto</th><th>Capacidade</th>${TIPOS.map(t => `<th class="num">${t[1]}</th>`).join('')}</tr></thead>
        <tbody>${WH.units.map(u => `
          <tr>
            <td class="tbl__main">${u.nome}<span class="tbl__sub">${u.unidades} unidade(s)</span></td>
            <td>${u.capacidade} hóspedes</td>
            ${TIPOS.map(t => `<td class="num"><input class="cell" type="number" step="50" min="0" value="${u.rates[t[0]]}" data-rate="${u.id}:${t[0]}" /></td>`).join('')}
          </tr>`).join('')}
        </tbody>
      </table>`;

    $('[data-datas-especiais]').innerHTML = `
      <div class="card__head"><div class="card__title">Calendário comercial<small>Períodos que disparam tarifa e estadia mínima</small></div></div>
      <div class="tbl-wrap">
        <table class="tbl">
          <thead><tr><th>Período</th><th>De</th><th>Até</th><th>Tarifa aplicada</th><th class="num">Mínimo de noites</th></tr></thead>
          <tbody>
            ${WH.periodos.map(p => `
              <tr>
                <td class="tbl__main">${p.label}</td>
                <td>${WH.dataCurta(p.de)}</td>
                <td>${WH.dataCurta(p.ate)}</td>
                <td><span class="chip chip--hold">${rotulo(p.tipo)}</span></td>
                <td class="num">${P.minNoites[p.tipo]} noites</td>
              </tr>`).join('')}
            ${Object.keys(WH.feriados).map(k => `
              <tr>
                <td class="tbl__main">${WH.feriados[k]}</td>
                <td>${WH.dataCurta(k)}</td>
                <td>${WH.dataCurta(k)}</td>
                <td><span class="chip chip--ghost">Feriado</span></td>
                <td class="num">${P.minNoites.feriado} noites</td>
              </tr>`).join('')}
          </tbody>
        </table>
      </div>`;

    $('[data-salvar-tarifas]').onclick = function () {
      let n = 0;
      $$('[data-rate]').forEach(inp => {
        const parts = inp.dataset.rate.split(':');
        const u = WH.units.find(x => x.id === parts[0]);
        const v = Number(inp.value);
        if (u && v > 0 && u.rates[parts[1]] !== v) { u.rates[parts[1]] = v; n++; }
      });
      toast(n ? `${n} tarifa(s) atualizada(s) — calendário e orçamentos já usam a tabela nova.` : 'Nenhuma alteração na tabela.');
      renderCalendario(); renderPainel();
    };
  }

  /* ════════ Política ════════ */
  function renderPolitica() {
    const campos = [
      { k: 'sinalPercentual',      l: 'Sinal para confirmar',     u: '%',      h: 'Percentual pago no ato da reserva. Sem sinal, a data não fica bloqueada.' },
      { k: 'prazoSaldoDias',       l: 'Prazo do saldo',           u: 'dias antes', h: 'Quantos dias antes do check-in o restante precisa estar pago.' },
      { k: 'preReservaHoras',      l: 'Validade da pré-reserva',  u: 'horas',  h: 'Tempo que a data fica segura sem pagamento. Depois disso, o sistema libera.' },
      { k: 'descontoGestao',       l: 'Alçada da gestão',         u: '% máx.', h: 'Até aqui o atendimento fecha sozinho, sem consultar os proprietários.' },
      { k: 'descontoProprietario', l: 'Alçada do proprietário',   u: '% máx.', h: 'Entre a alçada da gestão e este teto, exige aprovação. Acima, não é autorizado.' },
      { k: 'caucaoEvento',         l: 'Caução para eventos',      u: 'R$',     h: 'Valor retido como garantia contra danos em festas e produções.' }
    ];
    $('[data-politica]').innerHTML = campos.map(c => `
      <div class="policy__item">
        <span class="policy__l">${c.l}</span>
        <div class="policy__row">
          <input type="number" min="0" value="${P[c.k]}" data-pol="${c.k}" />
          <span class="policy__u">${c.u}</span>
        </div>
        <p class="policy__h">${c.h}</p>
      </div>`).join('');

    $('[data-salvar-politica]').onclick = function () {
      $$('[data-pol]').forEach(inp => { const v = Number(inp.value); if (v >= 0) P[inp.dataset.pol] = v; });
      toast('Política comercial atualizada — orçamentos e alçadas já seguem as novas regras.');
      renderCalendario(); renderPainel(); renderReservas();
    };

    const abertos = [
      ['Composição da White House Completa', 'Quais unidades exatamente entram e qual a capacidade total contratada.'],
      ['Cancelamento e remarcação', 'Faixas de prazo, percentual retido, quantas remarcações e diferença tarifária.'],
      ['No-show', 'O que acontece quando o hóspede simplesmente não aparece.'],
      ['Visitantes de hóspedes', 'Se pode receber convidados sem contratar evento, quantos e até que horas.'],
      ['Eventos', 'Capacidade máxima, horário limite, restrição de som e responsável por danos.'],
      ['Responsáveis nomeados', 'Nome e telefone de quem responde por pagamento, operação e emergência.']
    ];
    $('[data-pendencias]').innerHTML = `
      <div class="card__head"><div class="card__title">Decisões em aberto<small>Enquanto não forem definidas, cada caso vira consulta aos proprietários</small></div></div>
      <div class="tbl-wrap">
        <table class="tbl">
          <thead><tr><th>Regra</th><th>O que falta decidir</th><th>Status</th></tr></thead>
          <tbody>${abertos.map(a => `
            <tr><td class="tbl__main">${a[0]}</td><td>${a[1]}</td><td><span class="chip chip--busy">Em aberto</span></td></tr>`).join('')}
          </tbody>
        </table>
      </div>`;
  }

  function renderBadges() {
    $('[data-badge-reservas]').textContent = WH.reservas.filter(r => r.ate >= WH.HOJE).length;
    $('[data-badge-leads]').textContent = WH.leads.filter(l => ['confirmada', 'perdida'].indexOf(l.etapa) === -1).length;
  }

  /* ════════ Busca ════════ */
  $('[data-busca]').addEventListener('keydown', function (e) {
    if (e.key !== 'Enter') return;
    const t = this.value.trim().toLowerCase();
    if (!t) return;
    const r = WH.reservas.filter(x => x.ate >= WH.HOJE)
      .filter(x => x.cliente.toLowerCase().indexOf(t) !== -1 || nomeUnit(x.unit).toLowerCase().indexOf(t) !== -1)[0];
    if (r) {
      state.filtro = 'todas';
      location.hash = '#/reservas';
      renderReservas();
      toast(`${r.cliente} · ${nomeUnit(r.unit)} · ${WH.dataCurta(r.de)} → ${WH.dataCurta(r.ate)} · ${chipTexto(r.status)}`);
    } else {
      toast('Nada encontrado para "' + this.value.trim() + '".');
    }
  });
  const chipTexto = st => ({ 'confirmada': 'confirmada', 'pre-reserva': 'pré-reserva', 'bloqueio': 'bloqueio' }[st]);

  /* ════════ Boot ════════ */
  window.addEventListener('hashchange', rotear);
  initControles();
  rotear();
  renderPainel();
  renderCalendario();
  renderReservas();
  renderLeads();
  renderTarifas();
  renderPolitica();
  renderBadges();
})();
