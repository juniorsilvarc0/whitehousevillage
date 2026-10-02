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

  /* ─── Cards de acomodação (home) ─── */
  const grid = document.querySelector('[data-units]');
  if (grid && window.WH) {
    grid.innerHTML = WH.units.map(u => `
      <article class="unit-card" data-reveal>
        <div class="unit-card__media scene ${u.scene}">
          <span class="unit-card__tag">${u.tag}</span>
          <span class="scene__label">${u.nome}</span>
        </div>
        <div class="unit-card__body">
          <h3 class="unit-card__title">${u.nome}</h3>
          <ul class="unit-card__specs">${u.specs.map(s => `<li>${s}</li>`).join('')}</ul>
          <p class="unit-card__desc">${u.desc}</p>
          <div class="unit-card__price">
            <span class="v">${WH.brl(u.rates.normal)}</span>
            <span class="l">/ diária · a partir de</span>
          </div>
          <a class="btn btn--secondary unit-card__cta" href="/disponibilidade.html?produto=${u.id}">Ver disponibilidade</a>
        </div>
      </article>
    `).join('');

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
  }
})();
