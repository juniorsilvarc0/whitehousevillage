/*
 * Base de dados MOCADA — o que sobrou do MVP de apresentação.
 * Nenhuma chamada de rede: tudo roda no navegador, e NENHUM número daqui vale
 * como preço (ver apps/site/README.md).
 *
 * Este arquivo é servido em /scripts/data.js para qualquer visitante, então ele
 * não pode carregar nada que a página pública não deva mostrar. Em 02/10/2026
 * (passos D1–D3 de docs/unificacao-site-crm.md) saiu tudo o que só o back-office
 * mocado usava, todo dado de terceiro (quem reservou, quanto pagou, por onde
 * chegou, contatos do funil) e toda regra comercial interna. Ficou só o que a
 * home e a disponibilidade ainda leem. O resto morre no passo A2, quando
 * catálogo, calendário e orçamento passam a vir da API.
 */
window.WH = (function () {
  'use strict';

  /* ─── Produtos comercializáveis (Tabela Comercial V1 — valores de referência) ─── */
  const units = [
    {
      id: 'apto-2s',
      nome: 'Apartamento 2 Suítes',
      tag: 'Hospedagem',
      scene: 'scene--house',
      capacidade: 6,
      specs: ['2 suítes', 'até 6 hóspedes', 'cozinha equipada', 'varanda'],
      desc: 'Apartamento completo com duas suítes, sala integrada e varanda — ideal para famílias e grupos pequenos.',
      rates: { normal: 850, fds: 1100, feriado: 1400, alta: 1600, reveillon: 3200, carnaval: 2600 }
    },
    {
      id: 'suite-piscina',
      nome: 'Suítes da Piscina',
      tag: 'Hospedagem',
      scene: 'scene--pool',
      capacidade: 3,
      specs: ['acesso direto à piscina', 'até 3 hóspedes', 'frigobar', 'ar-condicionado'],
      desc: 'Suítes com saída direta para a área da piscina. O pé na água a três passos da cama.',
      rates: { normal: 550, fds: 700, feriado: 900, alta: 1050, reveillon: 2100, carnaval: 1700 }
    },
    {
      id: 'cobertura',
      nome: 'White House Cobertura',
      tag: 'Premium',
      scene: 'scene--rooftop',
      capacidade: 10,
      specs: ['até 10 hóspedes', 'rooftop', 'piscina privativa', 'espaço gourmet'],
      desc: 'Suítes, piscina, rooftop, cozinha, churrasqueira, espaço gourmet, sala e área externa — o produto mais desejado da casa.',
      rates: { normal: 1900, fds: 2400, feriado: 3100, alta: 3600, reveillon: 7500, carnaval: 6000 }
    },
    {
      id: 'completa',
      nome: 'White House Completa',
      tag: 'Exclusividade total',
      scene: 'scene--night',
      capacidade: 24,
      specs: ['complexo inteiro', 'até 24 hóspedes', 'eventos', 'exclusividade'],
      desc: 'O complexo inteiro sob exclusividade: todas as unidades, todas as áreas de lazer. Base para casamentos, retiros e eventos empresariais.',
      rates: { normal: 5500, fds: 6900, feriado: 8900, alta: 10500, reveillon: 21000, carnaval: 17000 },
      composto: ['apto-2s', 'suite-piscina', 'cobertura']
    }
  ];

  /* ─── Calendário comercial ─── */
  const feriados = {
    '2026-09-07': 'Independência',
    '2026-10-12': 'N. Sra. Aparecida',
    '2026-11-02': 'Finados',
    '2026-11-15': 'Proclamação',
    '2026-12-25': 'Natal',
    '2027-01-01': 'Ano Novo',
    '2027-04-21': 'Tiradentes'
  };

  const periodos = [
    { de: '2026-12-27', ate: '2027-01-02', tipo: 'reveillon', label: 'Réveillon' },
    { de: '2027-02-05', ate: '2027-02-10', tipo: 'carnaval',  label: 'Carnaval' },
    { de: '2026-12-15', ate: '2027-01-31', tipo: 'alta',      label: 'Alta temporada' },
    { de: '2027-07-01', ate: '2027-07-31', tipo: 'alta',      label: 'Férias de julho' }
  ];

  /* ─── Ocupação mocada do calendário ───
   * Só produto e período [de, ate): a página pública mostra livre ou ocupado e
   * nada além disso. Quem ocupa, por quanto, por qual canal e se é reserva,
   * pré-reserva ou bloqueio é dado de terceiro ou de operação — saiu daqui junto
   * com o back-office mocado (invariante 1 de docs/unificacao-site-crm.md §5).
   * As datas são as mesmas de antes: o calendário trava exatamente os mesmos dias. */
  const ocupacoes = [
    { unit: 'apto-2s',       de: '2026-08-21', ate: '2026-08-24' },
    { unit: 'apto-2s',       de: '2026-08-26', ate: '2026-08-30' },
    { unit: 'apto-2s',       de: '2026-09-04', ate: '2026-09-07' },
    { unit: 'apto-2s',       de: '2026-09-19', ate: '2026-09-21' },
    { unit: 'apto-2s',       de: '2026-10-09', ate: '2026-10-12' },
    { unit: 'apto-2s',       de: '2026-12-28', ate: '2027-01-02' },
    { unit: 'suite-piscina', de: '2026-08-22', ate: '2026-08-25' },
    { unit: 'suite-piscina', de: '2026-09-11', ate: '2026-09-14' },
    { unit: 'suite-piscina', de: '2026-10-02', ate: '2026-10-05' },
    { unit: 'suite-piscina', de: '2026-12-30', ate: '2027-01-02' },
    { unit: 'cobertura',     de: '2026-08-28', ate: '2026-08-31' },
    { unit: 'cobertura',     de: '2026-09-25', ate: '2026-09-28' },
    { unit: 'cobertura',     de: '2026-10-23', ate: '2026-10-26' },
    { unit: 'cobertura',     de: '2026-11-13', ate: '2026-11-16' },
    { unit: 'cobertura',     de: '2026-12-27', ate: '2027-01-03' },
    { unit: 'suite-piscina', de: '2026-08-27', ate: '2026-08-31' },
    { unit: 'cobertura',     de: '2026-08-21', ate: '2026-08-24' },
    { unit: 'apto-2s',       de: '2026-09-11', ate: '2026-09-14' },
    { unit: 'suite-piscina', de: '2026-09-18', ate: '2026-09-21' },
    { unit: 'completa',      de: '2026-11-06', ate: '2026-11-09' },
    { unit: 'completa',      de: '2026-10-16', ate: '2026-10-18' }
  ];

  /* ─── Política comercial exibida ao hóspede ───
   * Só o que a página pública mostra. Regra interna de negociação não mora num
   * arquivo que qualquer visitante baixa (passo D2): o público não negocia preço. */
  const politica = {
    sinalPercentual: 50,
    prazoSaldoDias: 7,
    preReservaHoras: 48,
    minNoites: { normal: 1, fds: 2, feriado: 3, alta: 3, reveillon: 4, carnaval: 4 },
    taxaLimpeza: { 'apto-2s': 180, 'suite-piscina': 120, 'cobertura': 350, 'completa': 900 }
  };

  /* ─── Helpers de data (strings YYYY-MM-DD, sem fuso) ─── */
  const HOJE = '2026-08-20';

  function key(y, m, d) {
    return y + '-' + String(m + 1).padStart(2, '0') + '-' + String(d).padStart(2, '0');
  }
  function parse(s) {
    const p = s.split('-').map(Number);
    return new Date(p[0], p[1] - 1, p[2]);
  }
  function toKey(dt) { return key(dt.getFullYear(), dt.getMonth(), dt.getDate()); }
  function addDays(s, n) {
    const dt = parse(s); dt.setDate(dt.getDate() + n); return toKey(dt);
  }
  function between(s, de, ate) { return s >= de && s <= ate; }

  /* Classifica a diária: réveillon > carnaval > feriado > alta > fim de semana > normal */
  function tarifa(s) {
    for (const p of periodos) {
      if (between(s, p.de, p.ate) && (p.tipo === 'reveillon' || p.tipo === 'carnaval')) {
        return { tipo: p.tipo, label: p.label };
      }
    }
    if (feriados[s]) return { tipo: 'feriado', label: feriados[s] };
    for (const p of periodos) {
      if (between(s, p.de, p.ate)) return { tipo: p.tipo, label: p.label };
    }
    const dow = parse(s).getDay();
    if (dow === 5 || dow === 6) return { tipo: 'fds', label: 'Fim de semana' };
    return { tipo: 'normal', label: 'Diária normal' };
  }

  /* Status de um dia para um produto: 'livre' ou 'ocupado', nunca o motivo.
     "Completa" fica ocupada se qualquer unidade estiver, e reservar a Completa
     também ocupa as unidades individuais. */
  function status(unitId, s) {
    const unit = units.find(u => u.id === unitId);
    const ids = [unitId].concat(unit && unit.composto ? unit.composto : []);
    if (!unit || !unit.composto) ids.push('completa');
    for (const o of ocupacoes) {
      if (ids.indexOf(o.unit) !== -1 && between(s, o.de, addDays(o.ate, -1))) return { st: 'ocupado' };
    }
    return { st: 'livre' };
  }

  function preco(unitId, s) {
    const u = units.find(x => x.id === unitId);
    return u.rates[tarifa(s).tipo];
  }

  const brl = n => n.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL', maximumFractionDigits: 0 });
  const MESES = ['janeiro','fevereiro','março','abril','maio','junho','julho','agosto','setembro','outubro','novembro','dezembro'];
  const dataCurta = s => { const d = parse(s); return String(d.getDate()).padStart(2,'0') + ' ' + MESES[d.getMonth()].slice(0,3); };

  return { units, ocupacoes, politica, HOJE, MESES,
           key, parse, addDays, tarifa, status, preco, brl, dataCurta };
})();
