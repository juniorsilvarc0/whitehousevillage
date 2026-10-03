/*
 * Textos, fotos e vídeos editados pelo gestor no painel (menu "Site").
 *
 * O HTML já traz o texto original. Este arquivo pede /api/v1/public/site, que
 * devolve só o que foi editado, e troca o que estiver marcado com data-cms*.
 * Se o pedido falhar ou demorar, nada muda: o visitante vê o texto original.
 *
 * Tudo o que vem de lá é tratado como texto: nunca vira HTML. A única marcação
 * que nasce aqui é a do destaque (*palavra* vira itálico), da quebra de linha e
 * dos parágrafos — sempre depois de escapar o texto.
 *
 * Marcações (docs/site-cms.md §7):
 *   data-cms="k"            texto
 *   data-cms-titulo="k"     título com *destaque* e quebra de linha
 *   data-cms-paragrafos="k" texto longo (linha em branco = novo parágrafo)
 *   data-cms-linhas="k"     texto longo, um <li> por linha
 *   data-cms-email="k"      texto e link mailto:
 *   data-cms-meta="k"       atributo content de um <meta>
 *   data-cms-img="k"        <img> (src/alt) ou bloco .scene (foto de fundo)
 *   data-cms-video="k"      <video>
 *   data-cms-lista="k"      lista, desenhada a partir do <template> filho; dentro
 *                           dele, data-cms-campo[-titulo|-paragrafos|-linhas|-img]
 *                           apontam para o subcampo de cada item, e
 *                           data-ciclo-classes="a|b|c" dá ao item i as classes da
 *                           posição i (o que se repetir volta ao começo).
 *
 * window.WH_CONTEUDO é uma promessa que sempre resolve: com o mapa de valores
 * editados, ou com {} se não deu. A vitrine lê dela os textos das categorias.
 */
(function () {
  'use strict';

  var ESPERA_MS = 3000;

  function escapar(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  /* Escapa primeiro; só então *x* vira <em>x</em> e a quebra de linha vira <br>. */
  function tituloHTML(s) {
    return escapar(s)
      .replace(/\r\n?/g, '\n')
      .replace(/\*([^*\n]+)\*/g, '<em>$1</em>')
      .replace(/\n/g, '<br>');
  }

  function paragrafosDe(s) {
    return String(s).replace(/\r\n?/g, '\n').split(/\n\s*\n/)
      .map(function (p) { return p.trim(); })
      .filter(Boolean);
  }

  function linhasDe(s) {
    return String(s).replace(/\r\n?/g, '\n').split('\n')
      .map(function (l) { return l.trim(); })
      .filter(Boolean);
  }

  /* Só endereço do próprio site: caminho absoluto ("/...", nunca "//host")
     ou URL com a mesma origem. Nada de host de terceiro recebendo o visitante. */
  function enderecoSeguro(url) {
    if (typeof url !== 'string' || !url) return null;
    var caminho = url;
    if (/^https?:\/\//i.test(url)) {
      var u;
      try { u = new URL(url); } catch (e) { return null; }
      if (u.origin !== window.location.origin) return null;
      caminho = u.pathname + u.search;
    }
    if (!/^\/(?!\/)[A-Za-z0-9\-._~\/%?=&]*$/.test(caminho)) return null;
    return caminho;
  }

  function ehTexto(v) { return typeof v === 'string'; }

  function aplicarTexto(el, v) { if (ehTexto(v)) el.textContent = v; }

  function aplicarTitulo(el, v) { if (ehTexto(v)) el.innerHTML = tituloHTML(v); }

  /* Num <p> não cabe outro <p>: os parágrafos viram quebras duplas. Em bloco
     (div), cada parágrafo é um <p>. */
  function aplicarParagrafos(el, v) {
    if (!ehTexto(v)) return;
    var partes = paragrafosDe(v);
    if (el.tagName === 'P' || el.tagName === 'SPAN') {
      el.innerHTML = partes.map(function (p) {
        return escapar(p).replace(/\n/g, '<br>');
      }).join('<br><br>');
      return;
    }
    el.textContent = '';
    partes.forEach(function (p) {
      var novo = document.createElement('p');
      novo.innerHTML = escapar(p).replace(/\n/g, '<br>');
      el.appendChild(novo);
    });
  }

  function aplicarLinhas(el, v) {
    if (!ehTexto(v)) return;
    var tag = (el.tagName === 'UL' || el.tagName === 'OL') ? 'li' : 'span';
    el.textContent = '';
    linhasDe(v).forEach(function (l) {
      var item = document.createElement(tag);
      item.textContent = l;
      el.appendChild(item);
    });
  }

  function aplicarEmail(el, v) {
    if (!ehTexto(v)) return;
    var email = v.trim();
    el.textContent = email;
    if (el.tagName === 'A') {
      if (/^[^\s@<>"'()]+@[^\s@<>"'()]+\.[^\s@<>"'()]+$/.test(email)) el.setAttribute('href', 'mailto:' + email);
      else el.removeAttribute('href');
    }
  }

  function aplicarMeta(el, v) { if (ehTexto(v)) el.setAttribute('content', v); }

  function aplicarImagem(el, v) {
    if (!v || typeof v !== 'object') return;
    var url = enderecoSeguro(v.url);
    if (!url) return;
    var alt = typeof v.alt === 'string' ? v.alt : '';
    if (el.tagName === 'LINK') { el.setAttribute('href', url); return; }
    if (el.tagName === 'IMG') {
      el.setAttribute('src', url);
      /* Logo decorativo (o link ao redor já tem nome): o alt continua vazio. */
      if (!el.hasAttribute('data-cms-decorativa')) el.setAttribute('alt', alt);
      return;
    }
    el.style.backgroundImage = 'url("' + url + '")';
    el.style.backgroundSize = 'cover';
    el.style.backgroundPosition = 'center';
    el.style.backgroundRepeat = 'no-repeat';
    el.classList.add('scene--foto');
    if (alt) { el.setAttribute('role', 'img'); el.setAttribute('aria-label', alt); }
    var rotulos = el.querySelectorAll(':scope > .scene__label');
    for (var i = 0; i < rotulos.length; i++) rotulos[i].hidden = true;
  }

  function aplicarVideo(el, v) {
    if (!v || typeof v !== 'object' || el.tagName !== 'VIDEO') return;
    var url = enderecoSeguro(v.url);
    if (!url) return;
    var fontes = el.querySelectorAll('source');
    for (var i = 0; i < fontes.length; i++) fontes[i].remove();
    var fonte = document.createElement('source');
    fonte.setAttribute('src', url);
    el.appendChild(fonte);
    el.load();
    if (el.autoplay) {
      var tocar = el.play();
      if (tocar && tocar.catch) tocar.catch(function () { /* navegador recusou tocar sozinho */ });
    }
  }

  /* Subcampos de um item de lista. O próprio elemento raiz do item também pode
     carregar uma marcação (ex.: o card de evento é o bloco da foto). */
  var SUBCAMPOS = [
    ['data-cms-campo', aplicarTexto],
    ['data-cms-campo-titulo', aplicarTitulo],
    ['data-cms-campo-paragrafos', aplicarParagrafos],
    ['data-cms-campo-linhas', aplicarLinhas],
    ['data-cms-campo-img', aplicarImagem]
  ];

  function preencherItem(raiz, item) {
    SUBCAMPOS.forEach(function (par) {
      var attr = par[0];
      var alvos = Array.prototype.slice.call(raiz.querySelectorAll('[' + attr + ']'));
      if (raiz.hasAttribute(attr)) alvos.unshift(raiz);
      alvos.forEach(function (el) {
        var chave = el.getAttribute(attr);
        if (Object.prototype.hasOwnProperty.call(item, chave)) par[1](el, item[chave]);
      });
    });
  }

  function aplicarLista(el, v) {
    if (!Array.isArray(v)) return;
    var modelo = null;
    for (var i = 0; i < el.children.length; i++) {
      if (el.children[i].tagName === 'TEMPLATE') { modelo = el.children[i]; break; }
    }
    if (!modelo) return;
    Array.prototype.slice.call(el.children).forEach(function (filho) {
      if (filho !== modelo) filho.remove();
    });
    v.forEach(function (item, indice) {
      if (!item || typeof item !== 'object') return;
      var copia = modelo.content.cloneNode(true);
      var raiz = copia.firstElementChild;
      if (!raiz) return;
      var ciclo = raiz.getAttribute('data-ciclo-classes');
      if (ciclo) {
        var opcoes = ciclo.split('|');
        opcoes[indice % opcoes.length].split(/\s+/).filter(Boolean).forEach(function (c) { raiz.classList.add(c); });
        raiz.removeAttribute('data-ciclo-classes');
      }
      preencherItem(raiz, item);
      el.appendChild(copia);
    });
  }

  var MARCACOES = [
    ['data-cms', aplicarTexto],
    ['data-cms-titulo', aplicarTitulo],
    ['data-cms-paragrafos', aplicarParagrafos],
    ['data-cms-linhas', aplicarLinhas],
    ['data-cms-email', aplicarEmail],
    ['data-cms-meta', aplicarMeta],
    ['data-cms-img', aplicarImagem],
    ['data-cms-video', aplicarVideo],
    ['data-cms-lista', aplicarLista]
  ];

  function aplicar(valores) {
    MARCACOES.forEach(function (par) {
      var attr = par[0];
      document.querySelectorAll('[' + attr + ']').forEach(function (el) {
        var chave = el.getAttribute(attr);
        if (!Object.prototype.hasOwnProperty.call(valores, chave)) return;
        /* Frase com prazo e sinal: quem escreve é o main.js, que troca {horas},
           {sinal} e {dias} pelos números da política vigente. */
        if (el.hasAttribute('data-politica-texto')) return;
        try {
          par[1](el, valores[chave]);
          el.setAttribute('data-cms-aplicado', '');
        } catch (e) { /* um campo com problema não derruba os outros */ }
      });
    });
  }

  /* Vídeo da capa no celular (inicio.video-celular): opcional. Com a tela EM
     PÉ, o <video> da capa troca para a versão vertical (9:16); deitada, volta
     para a de computador. Sem vídeo de celular, nada muda: o celular segue
     com o vídeo de cima, recortado no meio. */
  function videoDoCelular(valores) {
    var celular = valores['inicio.video-celular'];
    var el = document.querySelector('[data-cms-video="inicio.video"]');
    if (!el || !celular || typeof celular !== 'object' || !enderecoSeguro(celular.url) || !window.matchMedia) return;
    var emPe = window.matchMedia('(orientation: portrait)');
    var original = null;
    function escolher() {
      if (original === null) {
        var f = el.querySelector('source');
        original = valores['inicio.video'] || { url: (f && f.getAttribute('src')) || el.getAttribute('src') };
      }
      aplicarVideo(el, emPe.matches ? celular : original);
    }
    escolher();
    if (emPe.addEventListener) emPe.addEventListener('change', escolher);
  }

  function buscar() {
    if (!window.fetch) return Promise.resolve({});
    var controle = window.AbortController ? new AbortController() : null;
    var relogio = setTimeout(function () { if (controle) controle.abort(); }, ESPERA_MS);
    var pedido = fetch('/api/v1/public/site', {
      headers: { 'Accept': 'application/json' },
      signal: controle ? controle.signal : undefined
    }).then(function (r) {
      if (!r.ok) throw new Error('sem conteúdo');
      return r.json();
    }).then(function (corpo) {
      var valores = corpo && corpo.data && corpo.data.values;
      return (valores && typeof valores === 'object' && !Array.isArray(valores)) ? valores : {};
    });
    /* Sem AbortController, o relógio ainda vence a corrida. */
    var limite = new Promise(function (resolve) { setTimeout(function () { resolve({}); }, ESPERA_MS + 50); });
    return Promise.race([pedido, limite])
      .catch(function () { return {}; })
      .then(function (v) { clearTimeout(relogio); return v; });
  }

  /* Valores já carregados, para os textos que o JavaScript desenha (orçamento,
     formulário, avisos). Vazio até a resposta chegar. */
  var carregados = {};

  /* Preenche {nome} com o valor de `vars`. O texto é escapado antes; o valor
     também, a não ser que venha como {html: '...'} — marcação feita pelo próprio
     site, nunca pelo conteúdo editado. Marcador sem valor fica como está. */
  function preencher(modelo, vars) {
    return escapar(modelo).replace(/\{([a-z][a-z0-9-]*)\}/g, function (todo, nome) {
      if (!vars || !Object.prototype.hasOwnProperty.call(vars, nome)) return todo;
      var v = vars[nome];
      if (v && typeof v === 'object' && typeof v.html === 'string') return v.html;
      return escapar(v == null ? '' : v);
    });
  }

  /* Texto pronto para ir para innerHTML: o editado, se houver, senão o padrão. */
  function t(chave, padrao, vars) {
    var v = carregados[chave];
    return preencher(typeof v === 'string' && v.trim() ? v : padrao, vars);
  }

  /* O texto cru (sem escapar), para quem vai usar textContent ou montar a
     mensagem do WhatsApp. */
  function cru(chave, padrao, vars) {
    var v = carregados[chave];
    var modelo = typeof v === 'string' && v.trim() ? v : padrao;
    return modelo.replace(/\{([a-z][a-z0-9-]*)\}/g, function (todo, nome) {
      return vars && Object.prototype.hasOwnProperty.call(vars, nome) ? String(vars[nome]) : todo;
    });
  }

  window.WH_CONTEUDO = buscar().then(function (valores) {
    carregados = valores;
    aplicar(valores);
    try { videoDoCelular(valores); } catch (e) { /* fica o vídeo de computador */ }
    return valores;
  }, function () { return {}; });

  window.WH_CMS = { t: t, cru: cru, preencher: preencher, escapar: escapar, tituloHTML: tituloHTML, paragrafosDe: paragrafosDe, enderecoSeguro: enderecoSeguro, aplicarImagem: aplicarImagem };
})();
