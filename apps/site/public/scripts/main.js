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
  /* O gestor pode editar a frase no painel; {horas}, {sinal} e {dias} viram os
     números da política. Sem a política, fica a frase do HTML — a não ser que a
     editada não dependa de número nenhum. */
  const FRASES_DA_POLITICA = {
    curto: 'Consulte o calendário em tempo real, monte o orçamento da sua estadia e garanta a data com uma pré-reserva de {horas} horas.',
    completo: 'A pré-reserva bloqueia o calendário por {horas} horas. A confirmação acontece com o sinal de {sinal}%; o saldo vence {dias} dias antes do check-in.'
  };
  const textosDaPolitica = document.querySelectorAll('[data-politica-texto]');
  if (textosDaPolitica.length && window.WH) {
    const editados = window.WH_CONTEUDO || Promise.resolve({});
    const escrever = (P) => editados.then(valores => {
      textosDaPolitica.forEach(el => {
        const chave = el.dataset.cmsParagrafos;
        const editado = chave && typeof valores[chave] === 'string' && valores[chave].trim() ? valores[chave] : null;
        const modelo = editado || FRASES_DA_POLITICA[el.dataset.politicaTexto];
        if (!modelo) return;
        if (!P) {
          if (editado && !/\{[a-z-]+\}/.test(editado)) el.textContent = editado;
          return;
        }
        el.textContent = WH.cru('', modelo, { horas: P.hold_hours, sinal: P.deposit_pct, dias: P.balance_due_days });
      });
    });
    WH.api.politica().then(escrever, () => escrever(null));
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

    const editados = window.WH_CONTEUDO || Promise.resolve({});
    Promise.all([WH.api.produtos(), editados]).then(([produtos, valores]) => {
      /* Um card por categoria (Duplex, Pool Suítes, as villas, a Completa):
         treze cards iguais na home cansariam. A escolha da unidade exata —
         Duplex Aurora, Pool Suíte Coral — acontece na central de reservas. */
      grid.innerHTML = WH.agrupar(produtos).map(g => {
        const t = WH.categoriaEditada(g.chave, valores);
        const precos = g.produtos.map(p => p.from_price_cents).filter(v => v != null);
        const menor = precos.length ? Math.min.apply(null, precos) : null;
        const titulo = t.tituloEditado ? t.titulo
          : (g.produtos.length === 1 ? g.produtos[0].name : g.titulo);
        const quantos = g.produtos.length > 1 ? WH.cru('acomodacoes.opcoes', '{n} opções', { n: g.produtos.length }) : '';
        return `
      <article class="unit-card" data-reveal>
        <div class="unit-card__media scene ${t.scene}" data-categoria="${WH.esc(g.chave)}">
          <span class="unit-card__tag">${WH.esc(t.tag)}</span>
          <span class="scene__label">${WH.esc(titulo)}</span>
        </div>
        <div class="unit-card__body">
          <h3 class="unit-card__title">${WH.esc(titulo)}</h3>
          <ul class="unit-card__specs">${t.specs.concat(quantos ? [quantos] : []).map(s => `<li>${WH.esc(s)}</li>`).join('')}</ul>
          ${t.desc ? `<p class="unit-card__desc">${WH.esc(t.desc)}</p>` : ''}
          <div class="unit-card__price">
            ${menor != null
              ? `<span class="v">${WH.brl(menor)}</span><span class="l">${WH.t('acomodacoes.preco-sufixo', '/ diária · a partir de')}</span>`
              : `<span class="v">${WH.t('acomodacoes.sob-consulta', 'Sob consulta')}</span><span class="l">${WH.t('acomodacoes.sob-consulta-nota', 'fale com a gente')}</span>`}
          </div>
          <a class="btn btn--secondary unit-card__cta" href="/disponibilidade.html?produto=${encodeURIComponent(g.produtos[0].code)}">${WH.t('acomodacoes.botao-card', 'Ver disponibilidade')}</a>
        </div>
      </article>`;
      }).join('');
      /* Foto da categoria, quando o gestor pôs uma: substitui o fundo desenhado. */
      if (window.WH_CMS) {
        grid.querySelectorAll('[data-categoria]').forEach(el => {
          const t = WH.categoriaEditada(el.dataset.categoria, valores);
          if (t.foto) WH_CMS.aplicarImagem(el, t.foto);
        });
      }
      animar();
    }).catch(() => editados.then(() => {
      grid.innerHTML = `
      <article class="unit-card is-visible">
        <div class="unit-card__body">
          <h3 class="unit-card__title">${WH.t('acomodacoes.falha-titulo', 'Acomodações')}</h3>
          <p class="unit-card__desc">${WH.t('acomodacoes.falha-texto', 'Não conseguimos carregar as acomodações agora. Consulte datas e valores na central de reservas.')}</p>
          <a class="btn btn--secondary unit-card__cta" href="/disponibilidade.html">${WH.t('acomodacoes.botao-card', 'Ver disponibilidade')}</a>
        </div>
      </article>`;
    }));
  }
})();
