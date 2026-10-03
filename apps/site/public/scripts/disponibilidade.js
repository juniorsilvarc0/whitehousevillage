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

  /* Textos editáveis no painel (menu "Site"): T devolve HTML escapado, C o
     texto puro (mensagens do WhatsApp, avisos). O texto daqui é o de fábrica. */
  const T = WH.t, C = WH.cru;

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
    cliente: { name: '', phone: '', email: '', consent: false },  // formulário da pré-reserva
    envio: null,            // { chave, pedido, enviando, erro } da pré-reserva em curso
    preReserva: null,       // resposta 201 de POST /public/holds
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
    const linhas = [C('whatsapp.mensagem-consulta', 'Olá! Quero consultar valores na White House:'), '', `*${u.name}*`];
    if (de) linhas.push(`${C('orcamento.check-in', 'Check-in')}: ${WH.dataBR(de)}`);
    if (ate) linhas.push(`${C('orcamento.check-out', 'Check-out')}: ${WH.dataBR(ate)}`);
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
    const wa = WHATSAPP ? ` <a class="btn btn--secondary" target="_blank" rel="noopener" href="https://wa.me/${WHATSAPP}">${T('calendario.botao-whatsapp', 'Falar no WhatsApp')}</a>` : '';
    el.cals.innerHTML = `<p class="quote__empty">${WHATSAPP
      ? T('calendario.falha', 'A central de reservas está indisponível agora. Tente em instantes ou fale com a gente pelo WhatsApp.')
      : 'A central de reservas está indisponível agora. Tente em instantes.'}</p>${wa}`;
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
      state.orcamento = null; state.consulta = null; state.preReserva = null; state.envio = null;
      state.hospedes = Math.min(state.hospedes, unit().capacity);
      renderHospedes();
      await recarregar();
    });
    el.hospedes.addEventListener('change', () => {
      state.hospedes = Number(el.hospedes.value);
      orcar();
    });
    el.quote.addEventListener('click', ev => {
      if (!ev.target.closest('[data-hold-nova]')) return;
      state.preReserva = null; state.envio = null;
      state.checkin = state.checkout = null; state.orcamento = null;
      render();
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
      toast(e.message || C('calendario.falha-mes', 'Não foi possível carregar o calendário.'));
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
        : dia.available ? `${WH.rotulo(dia.date_type)} · ${WH.brl(dia.price_cents)}${dia.min_nights > 1 ? ' · ' + C('calendario.dia-minimo', 'mínimo {noites} noites', { noites: dia.min_nights }) : ''}`
        : dia.on_request ? `${WH.rotulo(dia.date_type)} · ${C('calendario.dia-consulta', 'sob consulta')}` : C('calendario.dia-indisponivel', 'Indisponível');

      cells += `<button type="button" class="${cls.join(' ')}" data-d="${s}" title="${WH.esc(titulo)}">
          <span class="day__n">${d}</span>
          <span class="day__p">${livre && dia.price_cents != null ? curto(dia.price_cents) : dia && dia.on_request && !past ? 'cons.' : '—'}</span>
        </button>`;
    }

    return `<div class="calendar">
        <h3 class="calendar__title">${WH.MESES[m]} ${ano}</h3>
        <p class="calendar__sub">${state.carregando ? T('calendario.carregando', 'consultando a central…')
          : livres === 1 ? T('calendario.noite-livre', '{n} noite livre para {acomodacao}', { n: livres, acomodacao: u.name })
          : T('calendario.noites-livres', '{n} noites livres para {acomodacao}', { n: livres, acomodacao: u.name })}</p>
        <div class="calendar__weekdays"><span>dom</span><span>seg</span><span>ter</span><span>qua</span><span>qui</span><span>sex</span><span>sáb</span></div>
        <div class="calendar__grid">${cells}</div>
      </div>`;
  }

  async function onPick(s) {
    if (s < state.hoje) { toast(C('calendario.aviso-passou', 'Data já passou.')); return; }
    const dia = diaDe(s);
    if (!dia) return;

    /* O dia de check-out não é noite da estadia: pode estar ocupado por quem
       chega nesse dia (a estadia é half-open, [check-in, check-out)). */
    const escolhendoSaida = state.checkin && !state.checkout && s > state.checkin;
    if (!escolhendoSaida && !dia.available && dia.on_request) {
      toast(WHATSAPP ? C('calendario.aviso-consulta', '{data} é sob consulta — fale com a gente pelo WhatsApp.', { data: WH.dataBR(s) })
        : `${WH.dataBR(s)} é sob consulta.`);
      state.consulta = s;
      renderQuote();
      return;
    }
    if (!escolhendoSaida && !dia.available) {
      toast(C('calendario.aviso-indisponivel', '{data} indisponível.', { data: WH.dataBR(s) }));
      return;
    }
    if (!escolhendoSaida) {
      state.checkin = s; state.checkout = null; state.orcamento = null; state.consulta = null;
      state.preReserva = null; state.envio = null;
      render();
      return;
    }

    /* Todas as noites entre check-in e check-out precisam estar livres. As que
       estiverem fora dos meses visíveis são pedidas agora. */
    if (WH.parse(s) - WH.parse(state.checkin) > 92 * 864e5) {
      toast(C('calendario.aviso-longa', 'Para estadias acima de 90 noites, fale com a gente pelo WhatsApp.'));
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
        toast(C('calendario.aviso-intervalo', 'Há datas ocupadas no intervalo. Escolha um novo check-in.'));
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
        <span class="quote__title">${T('orcamento.titulo', 'Seu orçamento')}</span>
        <span class="pill">${T('orcamento.selo', 'Tabela vigente')}</span>
      </div>
      <div class="quote__unit"><b>${WH.esc(u.name)}</b>${T('orcamento.detalhes', 'até {hospedes} hóspedes · check-in 14h · check-out 11h', { hospedes: u.capacity })}</div>`;
  }

  function blocoPolitica() {
    const P = state.politica;
    return `
      <div class="quote__signal">
        <div class="row"><span>${T('orcamento.sinal', 'Sinal para confirmar')}</span><b>${P.deposit_pct}%</b></div>
        <div class="row"><span>${T('orcamento.saldo', 'Saldo')}</span><b>${T('orcamento.saldo-prazo', 'até {dias} dias antes', { dias: P.balance_due_days })}</b></div>
        <div class="row"><span>${T('orcamento.pre-reserva', 'Pré-reserva')}</span><b>${T('orcamento.pre-reserva-prazo', 'segura {horas}h', { horas: P.hold_hours })}</b></div>
      </div>`;
  }

  function renderQuote() {
    const u = unit();
    const P = state.politica;
    const head = cabecalho(u);

    if (state.preReserva) {
      el.quote.innerHTML = head + confirmacaoDePreReserva(state.preReserva, P);
      return;
    }

    if (u.on_request || (state.consulta && !state.checkin)) {
      const link = linkConsulta(u, state.consulta, null);
      el.quote.innerHTML = head + `
        <div class="quote__hint quote__hint--warn">${u.on_request
          ? T('orcamento.consulta-produto', '{acomodacao} tem valores sob consulta: cada pedido é montado com a nossa equipe.', { acomodacao: u.name })
          : T('orcamento.consulta-data', '{data} tem valores sob consulta para {acomodacao}.', { data: WH.dataBR(state.consulta), acomodacao: u.name })}</div>
        ${link ? `<div class="quote__actions"><a class="btn btn--dark" target="_blank" rel="noopener" href="${link}">${T('orcamento.botao-consultar', 'Consultar no WhatsApp')}</a></div>` : ''}
        ${blocoPolitica()}`;
      return;
    }

    if (!state.checkin || !state.checkout) {
      el.quote.innerHTML = head + `
        <div class="quote__dates">
          <div class="quote__date"><div class="l">${T('orcamento.check-in', 'Check-in')}</div><div class="v">${state.checkin ? WH.dataCurta(state.checkin) : '—'}</div></div>
          <div class="quote__arrow">→</div>
          <div class="quote__date"><div class="l">${T('orcamento.check-out', 'Check-out')}</div><div class="v">—</div></div>
        </div>
        <p class="quote__empty">${state.checkin
          ? T('orcamento.escolha-saida', 'Agora selecione a data de check-out no calendário.')
          : T('orcamento.escolha-entrada', 'Selecione a data de check-in no calendário para ver o valor da estadia.')}</p>
        ${blocoPolitica()}`;
      return;
    }

    const datas = `
      <div class="quote__dates">
        <div class="quote__date"><div class="l">${T('orcamento.check-in', 'Check-in')}</div><div class="v">${WH.dataCurta(state.checkin)}</div></div>
        <div class="quote__arrow">→</div>
        <div class="quote__date"><div class="l">${T('orcamento.check-out', 'Check-out')}</div><div class="v">${WH.dataCurta(state.checkout)}</div></div>
      </div>`;

    const o = state.orcamento;
    if (!o || o.carregando) {
      el.quote.innerHTML = head + datas + `<p class="quote__empty">${T('orcamento.calculando', 'Calculando com a tabela vigente…')}</p>`;
      return;
    }
    if (o.erro) {
      /* A mensagem é a da API (mínimo de noites, lotação etc.): é ela quem sabe a regra. */
      const consulta = o.erro.code === 'RATE_NOT_FOUND';
      const tipo = o.erro.code === 'MIN_STAY_NOT_MET' || consulta ? 'warn' : 'block';
      const msg = consulta ? C('orcamento.consulta-datas', 'Essas datas têm valores sob consulta. Fale com a gente e montamos o seu pedido.') : o.erro.message;
      const link = linkConsulta(u, state.checkin, state.checkout);
      el.quote.innerHTML = head + datas + `
        <div class="quote__hint quote__hint--${tipo}">${WH.esc(msg)}</div>
        ${blocoPolitica()}
        ${link ? `<div class="quote__actions"><a class="btn btn--secondary" target="_blank" rel="noopener" href="${link}">${consulta ? T('orcamento.botao-consultar', 'Consultar no WhatsApp') : T('calendario.botao-whatsapp', 'Falar no WhatsApp')}</a></div>` : ''}`;
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
        <div class="quote__line"><span>${T('orcamento.limpeza', 'Taxa de limpeza')}</span><b>${WH.brl(c.cleaning_cents)}</b></div>
      </div>

      <div class="quote__sep"></div>

      <div class="quote__total">
        <span class="l">${T('orcamento.total', 'Total')} · ${c.night_count} ${c.night_count === 1 ? 'noite' : 'noites'}</span>
        <span class="v">${WH.brl(c.total_cents)}</span>
      </div>

      <div class="quote__signal">
        <div class="row"><span>${T('orcamento.sinal-valor', 'Sinal ({sinal}%) para confirmar', { sinal: P.deposit_pct })}</span><b>${WH.brl(c.deposit_cents)}</b></div>
        <div class="row"><span>${T('orcamento.saldo-valor', 'Saldo até {dias} dias antes', { dias: P.balance_due_days })}</span><b>${WH.brl(c.balance_cents)}</b></div>
        <div class="row"><span>${T('orcamento.diaria-media', 'Diária média')}</span><b>${WH.brl(c.avg_nightly_cents)}</b></div>
      </div>

      ${formularioDePreReserva(P)}
      ${WHATSAPP ? `<a class="btn btn--secondary" data-wa target="_blank" rel="noopener" href="#">${T('orcamento.botao-whatsapp', 'Prefiro falar no WhatsApp')}</a>` : ''}
      <p class="quote__note">${T('orcamento.nota', 'Valores da tabela vigente. A pré-reserva segura a data por {horas}h; sem o sinal, a data é liberada automaticamente.', { horas: P.hold_hours })}</p>`;

    ligarFormulario();

    const wa = el.quote.querySelector('[data-wa]');
    if (wa) {
      const linhas = [
        C('whatsapp.mensagem-orcamento', 'Olá! Quero pré-reservar na White House:'), '',
        `*${u.name}*`,
        `${C('orcamento.check-in', 'Check-in')}: ${WH.dataBR(state.checkin)}`,
        `${C('orcamento.check-out', 'Check-out')}: ${WH.dataBR(state.checkout)}`,
        `Hóspedes: ${state.hospedes}`,
        `Noites: ${c.night_count}`, '',
        `Total: ${WH.brl(c.total_cents)}`,
        `Sinal (${P.deposit_pct}%): ${WH.brl(c.deposit_cents)}`
      ];
      wa.href = `https://wa.me/${WHATSAPP}?text=` + encodeURIComponent(linhas.join('\n'));
    }
  }

  /* ─────────── Pré-reserva (POST /public/holds) ───────────
     O site não promete o que o banco não fez: "data segura" só aparece com o
     201 na mão. Conflito (alguém levou a data entre o calendário e o envio)
     vira aviso e recarga do calendário — nunca uma segunda tentativa
     automática. */
  function formularioDePreReserva(P) {
    const c = state.cliente, e = state.envio || {};
    const det = (e.erro && e.erro.details) || {};
    const erroDe = campo => det[campo] ? `<span class="hold__err">${WH.esc(det[campo])}</span>` : '';
    const geral = e.erro && e.erro.code !== 'VALIDATION_ERROR'
      ? `<div class="quote__hint quote__hint--${e.erro.code === 'DATE_CONFLICT' ? 'block' : 'warn'}">${WH.esc(mensagemDoEnvio(e.erro))}</div>` : '';
    return `
      <form class="hold" data-hold novalidate>
        <p class="hold__title">${T('pre-reserva.titulo', 'Garanta a data agora')}</p>
        <label class="hold__field"><span>${T('pre-reserva.nome', 'Nome completo')}</span>
          <input name="name" autocomplete="name" required minlength="2" maxlength="120" value="${WH.esc(c.name)}">${erroDe('name')}</label>
        <label class="hold__field"><span>${T('pre-reserva.whatsapp', 'WhatsApp com DDD')}</span>
          <input name="phone" type="tel" autocomplete="tel" inputmode="tel" required placeholder="${T('pre-reserva.whatsapp-exemplo', '(86) 99999-9999')}" value="${WH.esc(c.phone)}">${erroDe('phone')}</label>
        <label class="hold__field"><span>${T('pre-reserva.email', 'E-mail')} <small>${T('pre-reserva.opcional', '(opcional)')}</small></span>
          <input name="email" type="email" autocomplete="email" value="${WH.esc(c.email)}">${erroDe('email')}</label>
        <label class="hold__consent"><input name="consent" type="checkbox"${c.consent ? ' checked' : ''}>
          <span>${T('pre-reserva.consentimento', 'Autorizo a White House a usar meus dados para esta reserva e para falar comigo sobre ela.')}</span></label>
        ${erroDe('consent')}
        ${geral}
        <button class="btn btn--dark" type="submit"${e.enviando ? ' disabled' : ''}>${e.enviando ? T('pre-reserva.enviando', 'Reservando…') : T('pre-reserva.botao', 'Fazer pré-reserva')}</button>
        <p class="quote__note">${T('pre-reserva.nota', 'Sem pagamento agora. A data fica segura por {horas}h; para confirmar, paga-se o sinal de {sinal}%.', { horas: P.hold_hours, sinal: P.deposit_pct })}</p>
      </form>`;
  }

  function mensagemDoEnvio(erro) {
    switch (erro.code) {
      case 'DATE_CONFLICT': return C('pre-reserva.erro-conflito', 'Essas datas acabaram de ser reservadas por outra pessoa. Escolha outras no calendário.');
      case 'HOLD_LIMIT_REACHED': return erro.message;
      case 'RATE_LIMITED': return C('pre-reserva.erro-tentativas', 'Muitas tentativas seguidas. Tente de novo mais tarde ou fale com a gente pelo WhatsApp.');
      case 'NETWORK': return C('pre-reserva.erro-conexao', 'Sem conexão. Tente de novo — se a primeira tentativa chegou, a mesma pré-reserva é devolvida, sem duplicar.');
      default: return erro.message || C('pre-reserva.erro-geral', 'Não foi possível concluir agora.');
    }
  }

  function ligarFormulario() {
    const form = el.quote.querySelector('[data-hold]');
    if (!form) return;
    form.addEventListener('input', () => {
      state.cliente = {
        name: form.name.value, phone: form.phone.value,
        email: form.email.value, consent: form.consent.checked
      };
    });
    form.addEventListener('submit', ev => { ev.preventDefault(); preReservar(); });
  }

  async function preReservar() {
    const u = unit(), c = state.cliente;
    const pedido = {
      unit_type_id: u.unit_type_id, check_in: state.checkin, check_out: state.checkout,
      guests_count: state.hospedes, name: c.name.trim(), phone: c.phone.trim(), consent: !!c.consent
    };
    if (c.email.trim()) pedido.email = c.email.trim();
    if (!pedido.consent) {
      state.envio = { erro: { code: 'VALIDATION_ERROR', details: { consent: C('pre-reserva.consentimento-falta', 'é preciso aceitar para reservar.') } } };
      renderQuote();
      return;
    }
    /* Mesma tentativa (mesmo pedido) → mesma chave: um reenvio depois de uma
       queda de rede devolve a pré-reserva que já existe. Pedido diferente →
       chave nova. */
    const assinatura = JSON.stringify(pedido);
    const chave = state.envio && state.envio.assinatura === assinatura && state.envio.chave
      ? state.envio.chave : WH.novaChave();
    state.envio = { chave, assinatura, enviando: true };
    renderQuote();
    try {
      state.preReserva = await WH.api.preReservar(pedido, chave);
      state.envio = null;
      /* As noites agora são dele: o calendário relido mostra a data tomada,
         como qualquer outro visitante vai vê-la. */
      state.dias = {};
      state.checkin = state.checkout = null; state.orcamento = null;
      await recarregar();
    } catch (erro) {
      state.envio = { chave, assinatura, erro };
      if (erro.code === 'DATE_CONFLICT') {
        /* O calendário que o visitante via estava velho: some com o cache e
           redesenha, para a data tomada aparecer ocupada. */
        state.dias = {};
        state.envio = { erro };
        await recarregar();
        return;
      }
      renderQuote();
    }
  }

  function confirmacaoDePreReserva(r, P) {
    const expira = new Date(r.hold_expires_at);
    const quando = expira.toLocaleString('pt-BR', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit', timeZone: 'America/Fortaleza' });
    let wa = '';
    if (WHATSAPP) {
      const texto = [
        C('whatsapp.mensagem-pre-reserva', 'Olá! Fiz a pré-reserva *{codigo}* no site da White House.', { codigo: r.code }), '',
        `*${r.product_name}*`,
        `Check-in: ${WH.dataBR(r.check_in)} · Check-out: ${WH.dataBR(r.check_out)}`,
        `Hóspedes: ${r.guests_count}`,
        `Total: ${WH.brl(r.total_cents)} · Sinal: ${WH.brl(r.deposit_cents)}`, '',
        C('whatsapp.pergunta-sinal', 'Como faço o pagamento do sinal?')
      ].join('\n');
      wa = `<a class="btn btn--dark" target="_blank" rel="noopener" href="https://wa.me/${WHATSAPP}?text=${encodeURIComponent(texto)}">${T('pre-reserva.ok-botao-whatsapp', 'Enviar o código no WhatsApp')}</a>`;
    }
    return `
      <div class="hold-ok" data-hold-ok>
        <p class="hold-ok__eyebrow">${T('pre-reserva.ok-rotulo', 'Pré-reserva feita')}</p>
        <p class="hold-ok__code">${WH.esc(r.code)}</p>
        <p class="hold-ok__lead">${T('pre-reserva.ok-texto', '{acomodacao} está segura para você até {prazo}.', { acomodacao: r.product_name, prazo: { html: '<b>' + WH.esc(quando) + '</b>' } })}</p>
        <div class="quote__signal">
          <div class="row"><span>${WH.dataCurta(r.check_in)} → ${WH.dataCurta(r.check_out)} · ${r.night_count} ${r.night_count === 1 ? 'noite' : 'noites'}</span><b>${WH.brl(r.total_cents)}</b></div>
          <div class="row"><span>${T('orcamento.sinal-valor', 'Sinal ({sinal}%) para confirmar', { sinal: P.deposit_pct })}</span><b>${WH.brl(r.deposit_cents)}</b></div>
          <div class="row"><span>${T('orcamento.saldo-valor', 'Saldo até {dias} dias antes', { dias: P.balance_due_days })}</span><b>${WH.brl(r.balance_cents)}</b></div>
        </div>
        <p class="hold-ok__next">${T('pre-reserva.ok-proximo', 'Próximo passo: pagar o sinal até o prazo. Envie o código pelo WhatsApp e a nossa equipe passa os dados do pagamento. Sem o sinal, a data é liberada automaticamente.')}</p>
        ${wa}
        <button class="btn btn--secondary" type="button" data-hold-nova>${T('pre-reserva.ok-botao-nova', 'Fazer outra consulta')}</button>
      </div>`;
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
      { v: livres, l: T('calendario.kpi-livres', 'Noites livres em {mes}', { mes: WH.MESES[state.mes] }), d: WH.esc(unit().name) },
      { v: menor === null ? '—' : WH.brl(menor), l: T('calendario.kpi-diaria', 'Diária a partir de'), d: T('calendario.kpi-diaria-nota', 'no mês selecionado') },
      { v: P.deposit_pct + '%', l: T('calendario.kpi-sinal', 'Sinal para confirmar'), d: T('calendario.kpi-sinal-nota', 'saldo até {dias} dias antes', { dias: P.balance_due_days }) },
      { v: P.hold_hours + 'h', l: T('calendario.kpi-pre-reserva', 'Pré-reserva sem pagamento'), d: T('calendario.kpi-pre-reserva-nota', 'a data fica segura') }
    ];
    /* `l` e `d` já saem escapados de T/esc. */
    el.kpis.innerHTML = cards.map(c => `
      <div class="kpi"><div class="kpi__v">${c.v}</div><div class="kpi__l">${c.l}</div><div class="kpi__d">${c.d}</div></div>`).join('');
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
        <td>${WH.esc(p.name)}${pacotes.length ? `<small class="rates__pacotes">${T('disp.tabela-pacotes', 'Pacotes:')} ${WH.esc(pacotes.join(' · '))}</small>` : ''}</td>
        <td>${p.capacity} hóspedes</td>
        ${WH.TIPOS.map(t => `<td${t === 'reveillon' || t === 'carnaval' ? ' class="hi"' : ''}>${por[t] != null ? WH.brl(por[t]) : T('disp.tabela-consulta', 'consulta')}</td>`).join('')}
      </tr>`;
    }).join('');

    /* A estadia mínima agora varia por acomodação (Pool Suítes e villas pedem
       2 diárias, duplex aceita 1 em dia comum): uma frase só com "o maior
       mínimo" mentiria para metade da tabela. A do produto escolhido sai da
       API e aparece no calendário (título de cada dia) e na recusa do orçamento. */
    if (el.minimos && state.produtos.length) {
      el.minimos.textContent = C('disp.tarifas.minimos', 'A estadia mínima varia por acomodação e tipo de data — passe o mouse sobre o dia no calendário para ver a de cada data.');
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
      /* Os textos editados chegam junto (ou desistem em 3 s): o orçamento e o
         formulário já nascem com eles. */
      const [produtos, politica] = await Promise.all([WH.api.produtos(), WH.api.politica(), window.WH_CONTEUDO]);
      state.produtos = produtos;
      state.politica = politica;
      state.hoje = politica.today;
    } catch (e) {
      await window.WH_CONTEUDO;
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
