/* Comportamentos globais: header, menu mobile, reveals e cards da home. */
(function () {
  'use strict';

  /* ─── Header ao rolar ─── */
  const header = document.getElementById('site-header');
  const onScroll = () => {
    if (!header) return;
    header.classList.toggle('site-header--scrolled', window.scrollY > 24);
  };
  onScroll();
  window.addEventListener('scroll', onScroll, { passive: true });

  /* ─── Menu mobile ─── */
  const toggle = document.querySelector('.menu-toggle');
  if (toggle) {
    toggle.addEventListener('click', () => {
      const open = document.body.classList.toggle('menu-open');
      toggle.setAttribute('aria-expanded', String(open));
    });
    document.querySelectorAll('.mobile-nav a').forEach(a => {
      a.addEventListener('click', () => {
        document.body.classList.remove('menu-open');
        toggle.setAttribute('aria-expanded', 'false');
      });
    });
  }

  /* ─── Reveals ─── */
  const targets = document.querySelectorAll('[data-reveal]');
  if ('IntersectionObserver' in window) {
    const io = new IntersectionObserver((entries) => {
      entries.forEach((e, i) => {
        if (!e.isIntersecting) return;
        setTimeout(() => e.target.classList.add('is-visible'), i * 70);
        io.unobserve(e.target);
      });
    }, { threshold: 0.12, rootMargin: '0px 0px -8% 0px' });
    targets.forEach(t => io.observe(t));
  } else {
    targets.forEach(t => t.classList.add('is-visible'));
  }

  /* Rede de segurança: qualquer elemento que esteja na viewport vira visível.
     Evita seção invisível ao abrir a página já com âncora (#acomodacoes). */
  let ticking = false;
  function sweep() {
    ticking = false;
    document.querySelectorAll('[data-reveal]:not(.is-visible)').forEach(t => {
      const r = t.getBoundingClientRect();
      if (r.top < window.innerHeight * 0.98 && r.bottom > 0) t.classList.add('is-visible');
    });
  }
  function schedule() { if (!ticking) { ticking = true; requestAnimationFrame(sweep); } }
  window.addEventListener('scroll', schedule, { passive: true });
  window.addEventListener('resize', schedule, { passive: true });
  window.addEventListener('hashchange', () => setTimeout(sweep, 320));
  window.addEventListener('load', () => setTimeout(sweep, 240));
  setTimeout(sweep, 600);

  /* ─── Textos com a política vigente ───
     Prazo da pré-reserva, sinal e saldo vêm de /api/v1/public/policy. O HTML
     traz uma frase sem número, que fica no lugar se a API não responder: frase
     vaga é melhor que número que o banco já não confirma. */
  const textosDaPolitica = document.querySelectorAll('[data-politica-texto]');
  if (textosDaPolitica.length && window.WH) {
    WH.api.politica().then(P => {
      textosDaPolitica.forEach(el => {
        el.textContent = el.dataset.politicaTexto === 'curto'
          ? `Consulte o calendário em tempo real, monte o orçamento da sua estadia e garanta a data com uma pré-reserva de ${P.hold_hours} horas.`
          : `A pré-reserva bloqueia o calendário por ${P.hold_hours} horas. A confirmação acontece com o sinal de ${P.deposit_pct}%; o saldo vence ${P.balance_due_days} dias antes do check-in.`;
      });
    }).catch(() => { /* fica a frase sem número */ });
  }

  /* ─── Cards de acomodação (home) ───
     Nome, lotação e "a partir de" vêm do catálogo público da API; o texto de
     vitrine (selo, diferenciais, descrição) é do site. Sem a API, os cards não
     são desenhados com preço inventado: a seção mostra o convite para a
     central de reservas. */
  const grid = document.querySelector('[data-units]');
  if (grid && window.WH) {
    const animar = () => {
      if ('IntersectionObserver' in window) {
        const io2 = new IntersectionObserver((entries) => {
          entries.forEach((e, i) => {
            if (!e.isIntersecting) return;
            setTimeout(() => e.target.classList.add('is-visible'), i * 90);
            io2.unobserve(e.target);
          });
        }, { threshold: 0.1 });
        grid.querySelectorAll('[data-reveal]').forEach(t => io2.observe(t));
      } else {
        grid.querySelectorAll('[data-reveal]').forEach(t => t.classList.add('is-visible'));
      }
      setTimeout(sweep, 300);
    };

    WH.api.produtos().then(produtos => {
      /* Um card por categoria (Duplex, Pool Suítes, as villas, a Completa):
         treze cards iguais na home cansariam. A escolha da unidade exata —
         Duplex Aurora, Pool Suíte Coral — acontece na central de reservas. */
      grid.innerHTML = WH.agrupar(produtos).map(g => {
        const t = WH.CATEGORIAS[g.chave] || { tag: 'Hospedagem', scene: 'scene--house', specs: [], desc: '' };
        const precos = g.produtos.map(p => p.from_price_cents).filter(v => v != null);
        const menor = precos.length ? Math.min.apply(null, precos) : null;
        const titulo = g.produtos.length === 1 ? g.produtos[0].name : g.titulo;
        const quantos = g.produtos.length > 1 ? `${g.produtos.length} opções` : '';
        return `
      <article class="unit-card" data-reveal>
        <div class="unit-card__media scene ${t.scene}">
          <span class="unit-card__tag">${WH.esc(t.tag)}</span>
          <span class="scene__label">${WH.esc(titulo)}</span>
        </div>
        <div class="unit-card__body">
          <h3 class="unit-card__title">${WH.esc(titulo)}</h3>
          <ul class="unit-card__specs">${t.specs.concat(quantos ? [quantos] : []).map(s => `<li>${WH.esc(s)}</li>`).join('')}</ul>
          ${t.desc ? `<p class="unit-card__desc">${WH.esc(t.desc)}</p>` : ''}
          <div class="unit-card__price">
            ${menor != null
              ? `<span class="v">${WH.brl(menor)}</span><span class="l">/ diária · a partir de</span>`
              : `<span class="v">Sob consulta</span><span class="l">fale com a gente</span>`}
          </div>
          <a class="btn btn--secondary unit-card__cta" href="/disponibilidade.html?produto=${encodeURIComponent(g.produtos[0].code)}">Ver disponibilidade</a>
        </div>
      </article>`;
      }).join('');
      animar();
    }).catch(() => {
      grid.innerHTML = `
      <article class="unit-card is-visible">
        <div class="unit-card__body">
          <h3 class="unit-card__title">Acomodações</h3>
          <p class="unit-card__desc">Não conseguimos carregar as acomodações agora. Consulte datas e valores na central de reservas.</p>
          <a class="btn btn--secondary unit-card__cta" href="/disponibilidade.html">Ver disponibilidade</a>
        </div>
      </article>`;
    });
  }
})();
