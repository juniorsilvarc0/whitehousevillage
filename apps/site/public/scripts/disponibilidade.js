/* Central de reservas — calendário, orçamento e indicadores (dados mocados).
 *
 * Esta página é pública. Duas regras valem até a última linha:
 *  - o visitante não negocia preço: nenhum controle mexe no valor, e nenhuma
 *    regra comercial interna aparece aqui (passo D2 de docs/unificacao-site-crm.md);
 *  - um dia indisponível é só "indisponível": nunca quem ocupa, por quanto, nem
 *    se é reserva, pré-reserva ou bloqueio (invariante 1 do mesmo plano). */
(function () {
  'use strict';
  if (!window.WH) return;

  const P = WH.politica;
  const params = new URLSearchParams(location.search);

  const state = {
    produto: params.get('produto') || 'cobertura',
    hospedes: 6,
    ano: WH.parse(WH.HOJE).getFullYear(),
    mes: WH.parse(WH.HOJE).getMonth(),
    checkin: null,
    checkout: null
  };

  const $ = s => document.querySelector(s);
  const el = {
    produto: $('[data-produto]'),
    hospedes: $('[data-hospedes]'),
    cals: $('[data-calendars]'),
    quote: $('[data-quote]'),
    kpis: $('[data-kpis]'),
    rates: $('[data-rates] tbody'),
    toast: $('[data-toast]')
  };

  const unit = () => WH.units.find(u => u.id === state.produto);

  /* Número do WhatsApp: o nginx escreve no <meta name="whv:whatsapp"> por SSI, a
     partir da única linha que o define (apps/site/nginx.conf). Servida por outro
     servidor, a página chega com o comentário SSI cru no lugar do número: aí o
     botão do orçamento simplesmente não aparece, em vez de abrir um link quebrado. */
  const metaWa = document.querySelector('meta[name="whv:whatsapp"]');
  const WHATSAPP = metaWa && /^\d{10,15}$/.test(metaWa.content) ? metaWa.content : null;

  /* Deep link opcional: ?produto=cobertura&checkin=2026-12-28&checkout=2027-01-02 */
  function aplicarDeepLink() {
    const ci = params.get('checkin'), co = params.get('checkout');
    if (!ci || !co || ci >= co || ci < WH.HOJE) return;
    for (let c = ci; c < co; c = WH.addDays(c, 1)) {
      if (WH.status(state.produto, c).st !== 'livre') return;
    }
    state.checkin = ci; state.checkout = co;
    const d = WH.parse(ci);
    state.ano = d.getFullYear(); state.mes = d.getMonth();
  }
  const curto = n => n >= 1000 ? (n / 1000).toFixed(n % 1000 === 0 ? 0 : 1).replace('.', ',') + 'k' : String(n);

  let toastTimer;
  function toast(msg) {
    if (!el.toast) return;
    el.toast.textContent = msg;
    el.toast.classList.add('is-visible');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.toast.classList.remove('is-visible'), 4200);
  }

  /* ─────────── Controles ─────────── */
  function initControls() {
    el.produto.innerHTML = WH.units
      .map(u => `<option value="${u.id}">${u.nome} · até ${u.capacidade} hóspedes</option>`).join('');
    el.produto.value = state.produto;
    if (!unit()) { state.produto = 'cobertura'; el.produto.value = state.produto; }
    state.hospedes = Math.min(6, unit().capacidade);

    renderHospedes();

    el.produto.addEventListener('change', () => {
      state.produto = el.produto.value;
      state.checkin = state.checkout = null;
      state.hospedes = Math.min(state.hospedes, unit().capacidade);
      renderHospedes();
      render();
    });
    el.hospedes.addEventListener('change', () => {
      state.hospedes = Number(el.hospedes.value);
      renderQuote();
    });
    $('[data-prev]').addEventListener('click', () => shiftMonth(-1));
    $('[data-next]').addEventListener('click', () => shiftMonth(1));
  }

  function renderHospedes() {
    const max = unit().capacidade;
    let out = '';
    for (let i = 1; i <= max; i++) out += `<option value="${i}">${i} ${i === 1 ? 'hóspede' : 'hóspedes'}</option>`;
    el.hospedes.innerHTML = out;
    el.hospedes.value = String(state.hospedes);
  }

  function shiftMonth(n) {
    const d = new Date(state.ano, state.mes + n, 1);
    const min = new Date(WH.parse(WH.HOJE).getFullYear(), WH.parse(WH.HOJE).getMonth(), 1);
    const max = new Date(2027, 11, 1);
    if (d < min || d > max) return;
    state.ano = d.getFullYear();
    state.mes = d.getMonth();
    render();
  }

  /* ─────────── Calendário ─────────── */
  const CLASSE = { 'livre': 'free', 'ocupado': 'busy' };

  function renderCalendars() {
    let html = '';
    for (let i = 0; i < 2; i++) {
      const d = new Date(state.ano, state.mes + i, 1);
      html += mes(d.getFullYear(), d.getMonth());
    }
    el.cals.innerHTML = html;
    el.cals.querySelectorAll('.day[data-d]').forEach(b => {
      b.addEventListener('click', () => onPick(b.dataset.d, b.dataset.st));
    });
  }

  function mes(ano, m) {
    const first = new Date(ano, m, 1);
    const dias = new Date(ano, m + 1, 0).getDate();
    const offset = first.getDay();
    const u = unit();

    let livres = 0;
    let cells = '';
    for (let i = 0; i < offset; i++) cells += '<span class="day day--empty"></span>';

    for (let d = 1; d <= dias; d++) {
      const s = WH.key(ano, m, d);
      const st = WH.status(u.id, s).st;
      const t = WH.tarifa(s);
      const past = s < WH.HOJE;
      const especial = ['feriado', 'reveillon', 'carnaval', 'alta'].indexOf(t.tipo) !== -1;
      if (st === 'livre' && !past) livres++;

      const cls = ['day', 'day--' + CLASSE[st]];
      if (past) cls.push('day--past');
      if (especial) cls.push('day--special');
      if (s === WH.HOJE) cls.push('day--today');
      if (s === state.checkin || s === state.checkout) cls.push('day--sel');
      else if (state.checkin && state.checkout && s > state.checkin && s < state.checkout) cls.push('day--in-range');

      const titulo = st === 'livre'
        ? `${t.label} · ${WH.brl(WH.preco(u.id, s))}`
        : 'Indisponível';

      cells += `<button type="button" class="${cls.join(' ')}" data-d="${s}" data-st="${st}" title="${titulo}">
          <span class="day__n">${d}</span>
          <span class="day__p">${st === 'livre' && !past ? curto(WH.preco(u.id, s)) : '—'}</span>
        </button>`;
    }

    return `<div class="calendar">
        <h3 class="calendar__title">${WH.MESES[m]} ${ano}</h3>
        <p class="calendar__sub">${livres} ${livres === 1 ? 'noite livre' : 'noites livres'} para ${u.nome}</p>
        <div class="calendar__weekdays"><span>dom</span><span>seg</span><span>ter</span><span>qua</span><span>qui</span><span>sex</span><span>sáb</span></div>
        <div class="calendar__grid">${cells}</div>
      </div>`;
  }

  function onPick(s, st) {
    if (s < WH.HOJE) { toast('Data já passou.'); return; }
    if (st !== 'livre') {
      toast(`${s.split('-').reverse().join('/')} indisponível.`);
      return;
    }
    if (!state.checkin || state.checkout || s <= state.checkin) {
      state.checkin = s; state.checkout = null;
    } else {
      /* valida se todas as noites entre check-in e check-out estão livres */
      for (let cur = state.checkin; cur < s; cur = WH.addDays(cur, 1)) {
        if (WH.status(state.produto, cur).st !== 'livre') {
          toast('Há datas ocupadas no intervalo. Selecionamos um novo check-in.');
          state.checkin = s; state.checkout = null;
          render(); return;
        }
      }
      state.checkout = s;
    }
    render();
  }

  /* ─────────── Orçamento ─────────── */
  function calcular() {
    const u = unit();
    const linhas = {};
    let subtotal = 0, noites = 0, minExigido = 1;

    for (let cur = state.checkin; cur < state.checkout; cur = WH.addDays(cur, 1)) {
      const t = WH.tarifa(cur);
      const v = u.rates[t.tipo];
      linhas[t.tipo] = linhas[t.tipo] || { tipo: t.tipo, label: rotulo(t.tipo), qtd: 0, unit: v };
      linhas[t.tipo].qtd++;
      subtotal += v; noites++;
      minExigido = Math.max(minExigido, P.minNoites[t.tipo] || 1);
    }
    const limpeza = P.taxaLimpeza[u.id] || 0;
    const total = subtotal + limpeza;
    return { linhas: Object.values(linhas), subtotal, limpeza, total, noites, minExigido,
             sinal: Math.round(total * P.sinalPercentual / 100) };
  }

  function rotulo(tipo) {
    return { normal: 'Diária normal', fds: 'Fim de semana', feriado: 'Feriado',
             alta: 'Alta temporada', reveillon: 'Réveillon', carnaval: 'Carnaval' }[tipo] || tipo;
  }

  function renderQuote() {
    const u = unit();
    const head = `
      <div class="quote__head">
        <span class="quote__title">Seu orçamento</span>
        <span class="pill">Simulação</span>
      </div>
      <div class="quote__unit"><b>${u.nome}</b>até ${u.capacidade} hóspedes · check-in 14h · check-out 11h</div>`;

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
        <div class="quote__signal">
          <div class="row"><span>Sinal para confirmar</span><b>${P.sinalPercentual}%</b></div>
          <div class="row"><span>Saldo</span><b>até ${P.prazoSaldoDias} dias antes</b></div>
          <div class="row"><span>Pré-reserva</span><b>segura ${P.preReservaHoras}h</b></div>
        </div>`;
      return;
    }

    const c = calcular();
    const capOK = state.hospedes <= u.capacidade;
    const minOK = c.noites >= c.minExigido;

    el.quote.innerHTML = head + `
      <div class="quote__dates">
        <div class="quote__date"><div class="l">Check-in</div><div class="v">${WH.dataCurta(state.checkin)}</div></div>
        <div class="quote__arrow">→</div>
        <div class="quote__date"><div class="l">Check-out</div><div class="v">${WH.dataCurta(state.checkout)}</div></div>
      </div>

      <div class="quote__lines">
        ${c.linhas.map(l => `
          <div class="quote__line quote__line--tag">
            <span><span class="tarifa-tag">${l.label}</span> ${l.qtd}×</span>
            <b>${WH.brl(l.unit * l.qtd)}</b>
          </div>`).join('')}
        <div class="quote__line"><span>Taxa de limpeza</span><b>${WH.brl(c.limpeza)}</b></div>
      </div>

      <div class="quote__sep"></div>

      <div class="quote__total">
        <span class="l">Total · ${c.noites} ${c.noites === 1 ? 'noite' : 'noites'}</span>
        <span class="v">${WH.brl(c.total)}</span>
      </div>

      <div class="quote__signal">
        <div class="row"><span>Sinal (${P.sinalPercentual}%) para confirmar</span><b>${WH.brl(c.sinal)}</b></div>
        <div class="row"><span>Saldo até ${P.prazoSaldoDias} dias antes</span><b>${WH.brl(c.total - c.sinal)}</b></div>
        <div class="row"><span>Diária média</span><b>${WH.brl(Math.round(c.total / c.noites))}</b></div>
      </div>

      ${!minOK ? `<div class="quote__hint quote__hint--warn">Estadia mínima para este período: ${c.minExigido} noites.</div>` : ''}
      ${!capOK ? `<div class="quote__hint quote__hint--block">${state.hospedes} hóspedes excede a capacidade de ${u.capacidade}.</div>` : ''}

      <div class="quote__actions">
        <button class="btn btn--dark" data-pre ${(!minOK || !capOK) ? 'disabled style="opacity:.5;cursor:not-allowed"' : ''}>Gerar pré-reserva (${P.preReservaHoras}h)</button>
        ${WHATSAPP ? `<a class="btn btn--secondary" data-wa target="_blank" rel="noopener" href="#">Enviar orçamento no WhatsApp</a>` : ''}
      </div>
      <p class="quote__note">Pré-reserva bloqueia a data por ${P.preReservaHoras}h. Sem o sinal, a data é liberada automaticamente.</p>`;

    const pre = el.quote.querySelector('[data-pre]');
    if (pre && !pre.disabled) {
      pre.addEventListener('click', () => {
        WH.ocupacoes.push({ unit: state.produto, de: state.checkin, ate: state.checkout });
        toast(`Pré-reserva registrada: ${WH.dataCurta(state.checkin)} → ${WH.dataCurta(state.checkout)} · ${WH.brl(c.total)}. A data ficou bloqueada por ${P.preReservaHoras}h.`);
        state.checkin = state.checkout = null;
        render();
      });
    }

    const wa = el.quote.querySelector('[data-wa]');
    if (wa) {
      const txt = `Olá! Orçamento White House%0A%0A*${u.nome}*%0ACheck-in: ${state.checkin.split('-').reverse().join('/')}%0ACheck-out: ${state.checkout.split('-').reverse().join('/')}%0AHóspedes: ${state.hospedes}%0ANoites: ${c.noites}%0A%0ATotal: ${WH.brl(c.total)}%0ASinal (${P.sinalPercentual}%25): ${WH.brl(c.sinal)}`;
      wa.href = `https://wa.me/${WHATSAPP}?text=` + txt;
    }
  }

  /* ─────────── Indicadores públicos do mês ─────────── */
  function renderKPIs() {
    const dias = new Date(state.ano, state.mes + 1, 0).getDate();
    let livres = 0, menor = Infinity;
    for (let d = 1; d <= dias; d++) {
      const s = WH.key(state.ano, state.mes, d);
      if (s < WH.HOJE) continue;
      if (WH.status(state.produto, s).st === 'livre') {
        livres++;
        menor = Math.min(menor, WH.preco(state.produto, s));
      }
    }
    const cards = [
      { v: livres, l: 'Noites livres em ' + WH.MESES[state.mes], d: unit().nome },
      { v: menor === Infinity ? '—' : WH.brl(menor), l: 'Diária a partir de', d: 'no mês selecionado' },
      { v: P.sinalPercentual + '%', l: 'Sinal para confirmar', d: 'saldo até ' + P.prazoSaldoDias + ' dias antes' },
      { v: P.preReservaHoras + 'h', l: 'Pré-reserva sem pagamento', d: 'a data fica segura' }
    ];
    el.kpis.innerHTML = cards.map(c => `
      <div class="kpi"><div class="kpi__v">${c.v}</div><div class="kpi__l">${c.l}</div><div class="kpi__d">${c.d}</div></div>`).join('');
  }

  /* ─────────── Tabelas e listas estáticas ─────────── */
  function renderRates() {
    el.rates.innerHTML = WH.units.map(u => `
      <tr>
        <td>${u.nome}</td>
        <td>${u.capacidade} hóspedes</td>
        <td>${WH.brl(u.rates.normal)}</td>
        <td>${WH.brl(u.rates.fds)}</td>
        <td>${WH.brl(u.rates.feriado)}</td>
        <td>${WH.brl(u.rates.alta)}</td>
        <td class="hi">${WH.brl(u.rates.reveillon)}</td>
        <td class="hi">${WH.brl(u.rates.carnaval)}</td>
      </tr>`).join('');
  }

  function render() {
    renderCalendars();
    renderQuote();
    renderKPIs();
  }

  initControls();
  aplicarDeepLink();
  renderRates();
  render();
})();
