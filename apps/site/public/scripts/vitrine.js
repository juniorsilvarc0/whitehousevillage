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
     Produto que existir no banco e não estiver aqui aparece com o card
     genérico; nada some por falta de texto. */
  const conteudo = {
    'apto-2s': {
      tag: 'Hospedagem', scene: 'scene--house',
      specs: ['2 suítes', 'cozinha equipada', 'varanda'],
      desc: 'Apartamento completo com duas suítes, sala integrada e varanda — ideal para famílias e grupos pequenos.'
    },
    'suite-piscina': {
      tag: 'Hospedagem', scene: 'scene--pool',
      specs: ['acesso direto à piscina', 'frigobar', 'ar-condicionado'],
      desc: 'Suítes com saída direta para a área da piscina. O pé na água a três passos da cama.'
    },
    'cobertura': {
      tag: 'Premium', scene: 'scene--rooftop',
      specs: ['rooftop', 'piscina privativa', 'espaço gourmet'],
      desc: 'Suítes, piscina, rooftop, cozinha, churrasqueira, espaço gourmet, sala e área externa — o produto mais desejado da casa.'
    },
    'completa': {
      tag: 'Exclusividade total', scene: 'scene--night',
      specs: ['complexo inteiro', 'eventos', 'exclusividade'],
      desc: 'O complexo inteiro sob exclusividade: todas as unidades, todas as áreas de lazer. Base para casamentos, retiros e eventos empresariais.'
    }
  };
  const conteudoGenerico = { tag: 'Hospedagem', scene: 'scene--house', specs: [], desc: '' };
  const textoDe = codigo => conteudo[codigo] || conteudoGenerico;

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
    })
  };

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

  return { api, textoDe, brl, rotulo, TIPOS, MESES, key, parse, toKey, addDays, dataCurta, dataBR, esc };
})();
