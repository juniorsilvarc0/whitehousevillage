/*
 * Base de dados MOCADA — MVP de apresentação White House.
 * Nenhuma chamada de rede: tudo roda no navegador.
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
      rates: { normal: 850, fds: 1100, feriado: 1400, alta: 1600, reveillon: 3200, carnaval: 2600 },
      unidades: 3
    },
    {
      id: 'suite-piscina',
      nome: 'Suítes da Piscina',
      tag: 'Hospedagem',
      scene: 'scene--pool',
      capacidade: 3,
      specs: ['acesso direto à piscina', 'até 3 hóspedes', 'frigobar', 'ar-condicionado'],
      desc: 'Suítes com saída direta para a área da piscina. O pé na água a três passos da cama.',
      rates: { normal: 550, fds: 700, feriado: 900, alta: 1050, reveillon: 2100, carnaval: 1700 },
      unidades: 4
    },
    {
      id: 'cobertura',
      nome: 'White House Cobertura',
      tag: 'Premium',
      scene: 'scene--rooftop',
      capacidade: 10,
      specs: ['até 10 hóspedes', 'rooftop', 'piscina privativa', 'espaço gourmet'],
      desc: 'Suítes, piscina, rooftop, cozinha, churrasqueira, espaço gourmet, sala e área externa — o produto mais desejado da casa.',
      rates: { normal: 1900, fds: 2400, feriado: 3100, alta: 3600, reveillon: 7500, carnaval: 6000 },
      unidades: 1
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
      unidades: 1,
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

  /* ─── Reservas mocadas (o que hoje vive no WhatsApp + agenda física) ─── */
  const reservas = [
    { unit: 'apto-2s', de: '2026-08-21', ate: '2026-08-24', status: 'confirmada',  cliente: 'Família Andrade',        valor: 3300, origem: 'Indicação' },
    { unit: 'apto-2s', de: '2026-08-26', ate: '2026-08-30', status: 'pre-reserva', cliente: 'Larissa Fontes',         valor: 4100, origem: 'Instagram' },
    { unit: 'apto-2s', de: '2026-09-04', ate: '2026-09-07', status: 'confirmada',  cliente: 'Marcos Vieira',          valor: 3900, origem: 'Instagram' },
    { unit: 'apto-2s', de: '2026-09-19', ate: '2026-09-21', status: 'pre-reserva', cliente: 'Juliana Castro',         valor: 2200, origem: 'WhatsApp' },
    { unit: 'apto-2s', de: '2026-10-09', ate: '2026-10-12', status: 'confirmada',  cliente: 'Grupo Teresina',         valor: 4200, origem: 'Site' },
    { unit: 'apto-2s', de: '2026-12-28', ate: '2027-01-02', status: 'confirmada',  cliente: 'Família Nogueira',       valor: 16000, origem: 'Recorrente' },

    { unit: 'suite-piscina', de: '2026-08-22', ate: '2026-08-25', status: 'confirmada',  cliente: 'Renata Lopes',      valor: 2100, origem: 'Instagram' },
    { unit: 'suite-piscina', de: '2026-09-11', ate: '2026-09-14', status: 'pre-reserva', cliente: 'Pedro Sá',          valor: 2100, origem: 'WhatsApp' },
    { unit: 'suite-piscina', de: '2026-10-02', ate: '2026-10-05', status: 'confirmada',  cliente: 'Casal Moura',       valor: 2050, origem: 'Site' },
    { unit: 'suite-piscina', de: '2026-12-30', ate: '2027-01-02', status: 'confirmada',  cliente: 'Ana e amigos',      valor: 6300, origem: 'Indicação' },

    { unit: 'cobertura', de: '2026-08-28', ate: '2026-08-31', status: 'confirmada',  cliente: 'Aniversário 40 anos',   valor: 7200, origem: 'Evento' },
    { unit: 'cobertura', de: '2026-09-25', ate: '2026-09-28', status: 'confirmada',  cliente: 'Produção fotográfica',  valor: 6800, origem: 'Parceiro' },
    { unit: 'cobertura', de: '2026-10-23', ate: '2026-10-26', status: 'pre-reserva', cliente: 'Chá revelação Silva',   valor: 7100, origem: 'Instagram' },
    { unit: 'cobertura', de: '2026-11-13', ate: '2026-11-16', status: 'bloqueio',    cliente: 'Manutenção da piscina', valor: 0,   origem: 'Operação' },
    { unit: 'cobertura', de: '2026-12-27', ate: '2027-01-03', status: 'confirmada',  cliente: 'Réveillon Fontenele',   valor: 52500, origem: 'Recorrente' },


    { unit: 'suite-piscina', de: '2026-08-27', ate: '2026-08-31', status: 'confirmada',  cliente: 'Bruno Teixeira',    valor: 2800, origem: 'Site' },
    { unit: 'cobertura', de: '2026-08-21', ate: '2026-08-24', status: 'pre-reserva', cliente: 'Ensaio Vogue PI',       valor: 6300, origem: 'Parceiro' },
    { unit: 'apto-2s', de: '2026-09-11', ate: '2026-09-14', status: 'confirmada',  cliente: 'Turma da Medicina',       valor: 3600, origem: 'WhatsApp' },
    { unit: 'suite-piscina', de: '2026-09-18', ate: '2026-09-21', status: 'confirmada', cliente: 'Camila Rocha',       valor: 1950, origem: 'Instagram' },
    { unit: 'completa', de: '2026-11-06', ate: '2026-11-09', status: 'confirmada',  cliente: 'Mini wedding Beatriz & Caio', valor: 24700, origem: 'Evento' },
    { unit: 'completa', de: '2026-10-16', ate: '2026-10-18', status: 'pre-reserva', cliente: 'Retiro corporativo Norvex',   valor: 15800, origem: 'B2B' }
  ];

  /* ─── Política comercial (as regras que a reunião precisa fechar) ─── */
  const politica = {
    sinalPercentual: 50,
    prazoSaldoDias: 7,
    preReservaHoras: 48,
    descontoGestao: 5,
    descontoProprietario: 10,
    minNoites: { normal: 1, fds: 2, feriado: 3, alta: 3, reveillon: 4, carnaval: 4 },
    taxaLimpeza: { 'apto-2s': 180, 'suite-piscina': 120, 'cobertura': 350, 'completa': 900 },
    caucaoEvento: 2000
  };

  /* ─── Leads no funil (CRM) ─── */
  const leads = [
    { nome: 'Juliana Castro',      etapa: 'novo',        produto: 'apto-2s',       datas: '19–21 set', valor: 2200,  origem: 'WhatsApp',  dias: 0, tel: '(86) 99612-4410' },
    { nome: 'Escritório Braga',    etapa: 'novo',        produto: 'completa',      datas: '12–15 nov', valor: 21500, origem: 'Indicação', dias: 1, tel: '(86) 99884-2201' },
    { nome: 'Marina Aguiar',       etapa: 'atendimento', produto: 'suite-piscina', datas: '03–06 out', valor: 2050,  origem: 'Instagram', dias: 1, tel: '(86) 99231-7788' },
    { nome: 'Rodrigo Melo',        etapa: 'atendimento', produto: 'cobertura',     datas: '17–19 out', valor: 6100,  origem: 'Site',      dias: 2, tel: '(86) 99450-3312' },
    { nome: 'Chá revelação Silva', etapa: 'orcamento',   produto: 'cobertura',     datas: '23–26 out', valor: 7100,  origem: 'Instagram', dias: 3, tel: '(86) 99777-1020' },
    { nome: 'Norvex RH',           etapa: 'orcamento',   produto: 'completa',      datas: '16–18 out', valor: 15800, origem: 'B2B',       dias: 4, tel: '(86) 3322-4500' },
    { nome: 'Beatriz & Caio',      etapa: 'negociacao',  produto: 'completa',      datas: '06–09 nov', valor: 24700, origem: 'Evento',    dias: 6, tel: '(86) 99101-8844' },
    { nome: 'Larissa Fontes',      etapa: 'pre',         produto: 'apto-2s',       datas: '26–30 ago', valor: 4100,  origem: 'Instagram', dias: 1, tel: '(86) 99655-3311' },
    { nome: 'Ensaio Vogue PI',     etapa: 'pre',         produto: 'cobertura',     datas: '21–24 ago', valor: 6300,  origem: 'Parceiro',  dias: 2, tel: '(86) 99400-1177' },
    { nome: 'Família Nogueira',    etapa: 'confirmada',  produto: 'apto-2s',       datas: '28 dez–02 jan', valor: 16000, origem: 'Recorrente', dias: 9, tel: '(86) 99333-0099' },
    { nome: 'Réveillon Fontenele', etapa: 'confirmada',  produto: 'cobertura',     datas: '27 dez–03 jan', valor: 52500, origem: 'Recorrente', dias: 12, tel: '(86) 99222-7766' },
    { nome: 'Carla Bezerra',       etapa: 'perdida',     produto: 'cobertura',     datas: '12–14 set', valor: 5800,  origem: 'Instagram', dias: 5, tel: '(86) 99510-2244', motivo: 'Preço acima do orçamento' },
    { nome: 'Grupo Amigos PHB',    etapa: 'perdida',     produto: 'completa',      datas: '05–07 set', valor: 17200, origem: 'WhatsApp',  dias: 7, tel: '(86) 99612-9080', motivo: 'Data já ocupada' }
  ];

  const ETAPAS = [
    { id: 'novo',        label: 'Novo lead' },
    { id: 'atendimento', label: 'Em atendimento' },
    { id: 'orcamento',   label: 'Orçamento enviado' },
    { id: 'negociacao',  label: 'Negociação' },
    { id: 'pre',         label: 'Pré-reserva' },
    { id: 'confirmada',  label: 'Confirmada' },
    { id: 'perdida',     label: 'Perdida' }
  ];

  const pipeline = [
    { l: 'Novo lead', n: 38, p: 100 },
    { l: 'Em atendimento', n: 31, p: 82 },
    { l: 'Disponibilidade', n: 26, p: 68 },
    { l: 'Orçamento enviado', n: 19, p: 50 },
    { l: 'Negociação', n: 12, p: 32 },
    { l: 'Pré-reserva', n: 8, p: 21 },
    { l: 'Reserva confirmada', n: 6, p: 16 }
  ];

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
  function diffDays(a, b) {
    return Math.round((parse(b) - parse(a)) / 86400000);
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

  /* Status de um dia para um produto. "Completa" herda o bloqueio de qualquer unidade. */
  function status(unitId, s) {
    const unit = units.find(u => u.id === unitId);
    const ids = [unitId].concat(unit && unit.composto ? unit.composto : []);
    let out = null;
    for (const r of reservas) {
      if (ids.indexOf(r.unit) === -1) continue;
      if (!between(s, r.de, addDays(r.ate, -1))) continue;
      if (r.status === 'confirmada' || r.status === 'bloqueio') return { st: r.status, r: r };
      if (!out) out = { st: r.status, r: r };
    }
    /* Reservar a Completa também trava as unidades individuais */
    if (!unit || !unit.composto) {
      for (const r of reservas) {
        if (r.unit !== 'completa') continue;
        if (between(s, r.de, addDays(r.ate, -1))) {
          if (r.status === 'confirmada' || r.status === 'bloqueio') return { st: r.status, r: r };
          if (!out) out = { st: r.status, r: r };
        }
      }
    }
    return out || { st: 'livre', r: null };
  }

  function preco(unitId, s) {
    const u = units.find(x => x.id === unitId);
    return u.rates[tarifa(s).tipo];
  }

  const brl = n => n.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL', maximumFractionDigits: 0 });
  const MESES = ['janeiro','fevereiro','março','abril','maio','junho','julho','agosto','setembro','outubro','novembro','dezembro'];
  const dataCurta = s => { const d = parse(s); return String(d.getDate()).padStart(2,'0') + ' ' + MESES[d.getMonth()].slice(0,3); };

  return { units, feriados, periodos, reservas, politica, pipeline, leads, ETAPAS, HOJE, MESES,
           key, parse, toKey, addDays, diffDays, tarifa, status, preco, brl, dataCurta };
})();
