/*
 * Vitrine do site: o cliente da API pública e o conteúdo editorial das
 * acomodações. Substitui o antigo data.js (passos A2 e D3 de
 * docs/unificacao-site-crm.md).
 *
 * A regra deste arquivo é a do plano: O SITE PERGUNTA, NÃO CALCULA. Nenhuma
 * tarifa, estadia mínima, sinal, prazo, desconto ou disponibilidade nasce aqui —
 * tudo vem de /api/v1/public/*, servido pela mesma API e pelo mesmo motor do
 * painel. Trocar uma tarifa no painel muda o que o visitante vê, sem editar
 * arquivo nenhum.
 *
 * O que mora aqui é só o que o banco não tem e não precisa ter: o texto de
 * vitrine de cada acomodação (selo, ambientação, diferenciais, descrição) e a
 * formatação de valores e datas. Formatar não é calcular: o dinheiro chega em
 * centavos e sai em reais sem arredondamento nenhum.
 */
window.WH = (function () {
  'use strict';

  /* ─── Conteúdo editorial, pelo código do produto no banco ───
     O NOME que o visitante vê vem da API (nome de vitrine, `public_name`); aqui
     mora só o que o banco não tem: a categoria em que o produto aparece, o
     selo, a ambientação e os diferenciais. Produto do banco sem entrada aqui
     aparece com o card genérico — nada some por falta de texto. */
  const CATEGORIAS = {
    duplex: {
      titulo: 'Apartamentos Duplex', tag: 'Hospedagem', scene: 'scene--house',
      specs: ['2 suítes', 'até 7 pessoas', 'acesso à piscina'],
      desc: 'Seis apartamentos duplex com duas suítes e acesso à piscina — Aurora, Brisa, Duna, Maré, Âmbar e Horizonte. Você escolhe o seu.'
    },
    suites: {
      titulo: 'Pool Suítes', tag: 'Pé na piscina', scene: 'scene--pool',
      specs: ['cama de casal', 'frigobar', 'acesso direto à piscina'],
      desc: 'Quatro suítes com saída para a piscina — Coral, Pérola, Concha e Oceano. Reserve uma, ou as quatro juntas para até 8 pessoas.'
    },
    'grand-villa': {
      titulo: 'White House Grand Villa', tag: 'Casa principal', scene: 'scene--rooftop',
      specs: ['4 suítes', 'elevador', 'rooftop com piscina privativa', 'vista para o mar'],
      desc: 'A casa principal triplex: amplos ambientes, elevador e rooftop com piscina privativa e vista para o mar. Pacotes de 2 e 4 diárias com valor especial.'
    },
    'classic-villa': {
      titulo: 'White House Classic Villa', tag: 'Casa rústica', scene: 'scene--house',
      specs: ['casa inteira', 'charme rústico', 'mínimo de 2 diárias'],
      desc: 'A casa rústica da White House, para quem quer a casa inteira com aconchego e privacidade.'
    },
    completa: {
      titulo: 'White House Completa', tag: 'Exclusividade total', scene: 'scene--night',
      specs: ['todas as unidades', 'eventos', 'exclusividade'],
      desc: 'O complexo inteiro sob exclusividade: duplex, suítes e villas. Base para casamentos, retiros e eventos — valores sob consulta.'
    }
  };
  const ORDEM_CATEGORIAS = ['duplex', 'suites', 'grand-villa', 'classic-villa', 'completa'];

  function categoriaDe(codigo) {
    if (/^duplex-/.test(codigo)) return 'duplex';
    if (/^suite-/.test(codigo) || codigo === 'pool-suites') return 'suites';
    return CATEGORIAS[codigo] ? codigo : null;
  }
  const conteudoGenerico = { titulo: '', tag: 'Hospedagem', scene: 'scene--house', specs: [], desc: '' };

  /* Textos da categoria com o que o gestor editou no painel (menu "Site"), lidos
     de window.WH_CONTEUDO (scripts/conteudo.js). Campo não editado fica com o
     texto acima. `editados` traz os valores já carregados; sem ele, só o original. */
  function categoriaEditada(chave, editados) {
    const base = CATEGORIAS[chave] || conteudoGenerico;
    const v = editados || {};
    const k = campo => v['categoria.' + chave + '.' + campo];
    const texto = x => (typeof x === 'string' ? x : null);
    const itens = Array.isArray(k('itens'))
      ? k('itens').map(i => (i && typeof i.texto === 'string' ? i.texto : '')).filter(Boolean)
      : null;
    const foto = k('foto');
    return Object.assign({}, base, {
      titulo: texto(k('titulo')) != null ? k('titulo') : base.titulo,
      tituloEditado: texto(k('titulo')) != null,
      tag: texto(k('selo')) != null ? k('selo') : base.tag,
      desc: texto(k('descricao')) != null ? k('descricao') : base.desc,
      specs: itens || base.specs,
      foto: foto && typeof foto === 'object' ? foto : null
    });
  }
  const textoDe = (codigo, editados) => {
    const c = categoriaDe(codigo);
    return c ? categoriaEditada(c, editados) : conteudoGenerico;
  };

  /* Agrupa a lista da API por categoria, na ordem da vitrine. Produto sem
     categoria conhecida vai para um grupo próprio no fim. */
  function agrupar(produtos) {
    const grupos = {};
    for (const p of produtos) {
      const c = categoriaDe(p.code) || 'outros';
      (grupos[c] = grupos[c] || []).push(p);
    }
    return ORDEM_CATEGORIAS.concat(['outros']).filter(c => grupos[c])
      .map(c => ({ chave: c, titulo: (CATEGORIAS[c] || {}).titulo || 'Outras acomodações', produtos: grupos[c] }));
  }

  /* ─── Cliente da API pública ───
     Mesma origem: o nginx do site encaminha /api/v1/public/ para a API. Erro
     vira exceção com o `code` estável do contrato — a página reage ao code,
     nunca ao texto. */
  const BASE = '/api/v1/public';

  async function pedir(caminho, opcoes) {
    let resposta;
    try {
      resposta = await fetch(BASE + caminho, Object.assign({ headers: { 'Accept': 'application/json' } }, opcoes));
    } catch (e) {
      throw { code: 'NETWORK', message: 'Sem conexão com a central de reservas.' };
    }
    let corpo = null;
    try { corpo = await resposta.json(); } catch (e) { /* corpo vazio ou HTML de erro do proxy */ }
    if (!resposta.ok) {
      const erro = (corpo && corpo.error) || {};
      throw {
        code: erro.code || ('HTTP_' + resposta.status),
        message: erro.message || 'A central de reservas não respondeu agora.',
        details: erro.details || {}
      };
    }
    return corpo ? corpo.data : null;
  }

  const api = {
    produtos: () => pedir('/products'),
    politica: () => pedir('/policy'),
    disponibilidade: (produtoId, de, ate) =>
      pedir('/availability?unit_type_id=' + encodeURIComponent(produtoId) +
            '&from=' + encodeURIComponent(de) + '&to=' + encodeURIComponent(ate)),
    orcar: (pedido) => pedir('/quotes', {
      method: 'POST',
      headers: { 'Accept': 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify(pedido)
    }),
    /* Pré-reserva: grava de verdade e segura a data. A chave de idempotência é
       de quem chama, uma por tentativa — repetir o envio (rede caiu, clique
       duplo) com a MESMA chave devolve a mesma pré-reserva, nunca uma segunda. */
    preReservar: (pedido, chave) => pedir('/holds', {
      method: 'POST',
      headers: { 'Accept': 'application/json', 'Content-Type': 'application/json', 'Idempotency-Key': chave },
      body: JSON.stringify(pedido)
    })
  };

  /* Chave de idempotência: aleatória e longa o bastante para não colidir entre
     visitantes. randomUUID só existe em contexto seguro (https/localhost). */
  function novaChave() {
    if (window.crypto && crypto.randomUUID) return 'site-' + crypto.randomUUID();
    const b = new Uint8Array(16);
    crypto.getRandomValues(b);
    return 'site-' + Array.from(b, x => x.toString(16).padStart(2, '0')).join('');
  }

  /* ─── Formatação ─── */

  /* Centavos → reais. Mostra centavos só quando existem: R$ 1.900 e não
     R$ 1.900,00, mas R$ 1.900,50 quando for o caso — nunca arredonda. */
  function brl(centavos) {
    const inteiro = centavos % 100 === 0;
    return (centavos / 100).toLocaleString('pt-BR', {
      style: 'currency', currency: 'BRL',
      minimumFractionDigits: inteiro ? 0 : 2, maximumFractionDigits: 2
    });
  }

  /* Rótulo do tipo de data (vocabulário fechado do contrato: TipoDeData). */
  const ROTULOS = {
    normal: 'Diária normal', fds: 'Fim de semana', feriado: 'Feriado',
    alta: 'Alta temporada', reveillon: 'Réveillon', carnaval: 'Carnaval'
  };
  const rotulo = tipo => ROTULOS[tipo] || tipo;
  const TIPOS = ['normal', 'fds', 'feriado', 'alta', 'reveillon', 'carnaval'];

  /* Datas como texto AAAA-MM-DD, sem fuso: o calendário é de DATAS, e um Date
     com hora atravessando fuso é a origem clássica do dia pulado. */
  const MESES = ['janeiro','fevereiro','março','abril','maio','junho','julho','agosto','setembro','outubro','novembro','dezembro'];
  function key(y, m, d) {
    return y + '-' + String(m + 1).padStart(2, '0') + '-' + String(d).padStart(2, '0');
  }
  function parse(s) {
    const p = s.split('-').map(Number);
    return new Date(p[0], p[1] - 1, p[2]);
  }
  function toKey(dt) { return key(dt.getFullYear(), dt.getMonth(), dt.getDate()); }
  function addDays(s, n) { const dt = parse(s); dt.setDate(dt.getDate() + n); return toKey(dt); }
  const dataCurta = s => { const d = parse(s); return String(d.getDate()).padStart(2, '0') + ' ' + MESES[d.getMonth()].slice(0, 3); };
  const dataBR = s => s.split('-').reverse().join('/');

  /* Escapa texto antes de ir para innerHTML — nomes vêm do banco. */
  const esc = s => String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

  /* Textos editáveis no painel (menu "Site"), com o texto de fábrica como
     reserva. `t` devolve HTML já escapado; `cru`, o texto puro. Os dois trocam
     {marcadores} pelos valores passados — números que vêm da API. */
  const semCms = {
    t: (chave, padrao, vars) => esc(padrao).replace(/\{([a-z][a-z0-9-]*)\}/g, (m, n) =>
      vars && n in vars ? (vars[n] && typeof vars[n] === 'object' ? vars[n].html : esc(vars[n])) : m),
    cru: (chave, padrao, vars) => padrao.replace(/\{([a-z][a-z0-9-]*)\}/g, (m, n) => vars && n in vars ? String(vars[n]) : m)
  };
  const t = (chave, padrao, vars) => (window.WH_CMS || semCms).t(chave, padrao, vars);
  const cru = (chave, padrao, vars) => (window.WH_CMS || semCms).cru(chave, padrao, vars);

  return { t, cru, api, novaChave, textoDe, categoriaEditada, agrupar, CATEGORIAS, brl, rotulo, TIPOS, MESES, key, parse, toKey, addDays, dataCurta, dataBR, esc };
})();
