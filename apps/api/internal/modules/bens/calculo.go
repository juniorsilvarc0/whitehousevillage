package bens

import (
	"github.com/google/uuid"
)

// Aritmética do módulo, sem SQL e sem HTTP.
//
// Não mora em `internal/domain` porque aquele pacote é do tech-lead e não há,
// hoje, pacote de domínio para bens. O que está aqui é contagem e
// multiplicação de centavos — nada de tarifa nem disponibilidade —, e fica
// isolado em funções puras, testadas sem banco (calculo_test.go), para poder
// mudar de casa sem reescrita se o domínio ganhar um pacote `goods`.

// diferenca é `counted − expected`; nula enquanto a linha está pendente.
// Negativo é falta; positivo, sobra.
func diferenca(esperada int, contada *int) *int {
	if contada == nil {
		return nil
	}
	d := *contada - esperada
	return &d
}

// custoTotal é `qtd × custo de reposição`, em centavos. Nulo quando o bem não
// tem custo cotado — e nunca zero, que diria que a perda não custou nada.
func custoTotal(qtd int, custo *int64) *int64 {
	if custo == nil {
		return nil
	}
	v := int64(qtd) * *custo
	return &v
}

// somar acrescenta uma linha ao rodapé. É a MESMA aritmética de progressoDe
// (SQL): pendente não é divergente, e falta/sobra só contam linha contada.
func (p *Progresso) somar(esperada int, contada *int) {
	p.Linhas++
	if contada == nil {
		p.Pendentes++
		return
	}
	p.Contadas++
	switch d := *contada - esperada; {
	case d < 0:
		p.Divergentes++
		p.Faltas += int64(-d)
	case d > 0:
		p.Divergentes++
		p.Sobras += int64(d)
	}
}

// apuracao é o resultado do fechamento para UMA linha divergente.
type apuracao struct {
	Divergencia Divergencia
	// Falta > 0 abre avaria `faltando` com esta quantidade. Sobra nunca abre
	// nada: contar 14 onde se esperavam 12 é correção de cadastro, não avaria.
	Falta int
}

// apurar valora uma linha divergente. A perda é `falta × custo CONGELADO`, e
// só existe quando há FALTA e custo cotado no fechamento: sobra não é perda, e
// "não cotado" não vira zero.
func apurar(l linhaApurada) apuracao {
	diff := l.QtdContada - l.QtdEsperada
	a := apuracao{Divergencia: Divergencia{
		AmbienteID:            l.AmbienteID,
		AmbienteNome:          l.AmbienteNome,
		BemID:                 l.BemID,
		BemNome:               l.BemNome,
		QtdEsperada:           l.QtdEsperada,
		QtdContada:            l.QtdContada,
		Diferenca:             diff,
		CustoDeReposicaoCents: l.Custo,
		AvariaID:              l.AvariaID,
	}}
	if diff < 0 {
		a.Falta = -diff
		a.Divergencia.PerdaCents = custoTotal(a.Falta, l.Custo)
	}
	return a
}

// totalizar soma o que a tela da unidade EXIBIU. `items` são bens DISTINTOS (o
// mesmo prato em dois cômodos conta uma vez); o custo soma `qtd × custo` só dos
// cotados, e `uncosted_items` diz quantos bens ficaram fora da soma — sem isso
// o número pareceria o valor do enxoval inteiro.
func totalizar(ambientes []AmbienteDoInventario) TotaisDaUnidade {
	t := TotaisDaUnidade{Ambientes: len(ambientes)}
	vistos := map[uuid.UUID]bool{}
	semCusto := map[uuid.UUID]bool{}
	var (
		soma     int64
		temCusto bool
	)
	for _, a := range ambientes {
		t.AvariasAbertas += a.AvariasAbertas
		for _, c := range a.Bens {
			t.QtdEsperada += int64(c.QtdEsperada)
			vistos[c.BemID] = true
			if c.custo == nil {
				semCusto[c.BemID] = true
				continue
			}
			temCusto = true
			soma += int64(c.QtdEsperada) * *c.custo
		}
	}
	t.Bens = len(vistos)
	t.BensSemCusto = len(semCusto)
	if temCusto {
		t.CustoDeReposicaoCents = &soma
	}
	return t
}

// ─────────────────────────── Plano da cópia ─────────────────────────────────

// colocacaoDaCopia é uma colocação vista pela cópia. O cômodo vai pelo
// `code` — a identidade estável, que sobrevive a renomear — e pelo nome, que é
// só o que a resposta mostra.
type colocacaoDaCopia struct {
	AmbienteCodigo string
	AmbienteNome   string
	BemID          uuid.UUID
	BemNome        string
	Qtd            int
}

// planoDeCopia é o que a cópia vai fazer — idêntico na simulação e na
// execução, porque as duas saem desta mesma função. As colocações a criar já
// vêm com o `code` e o nome do cômodo DE DESTINO.
type planoDeCopia struct {
	Ambientes  []ambienteGravado
	Colocacoes []colocacaoDaCopia
	Mantidas   []ColocacaoMantida
}

type chaveDeColocacaoNoDestino struct {
	codigo string
	bem    uuid.UUID
}

// planejarCopia decide o que nasce no destino. A cópia só ACRESCENTA:
//
//   - o cômodo de origem casa com o de destino PELO `code`, que sobrevive a
//     renomear "Quarto grande" para "Suíte Master" num dos dois; sem `code`
//     igual, casa pelo `name`, porque `unit_rooms_nome_unico` não deixaria
//     criar o segundo. Cômodo casado é reaproveitado e NADA nele muda;
//   - o que não casa nasce no destino com `code`, `name`, `kind` e
//     `sort_order` da origem — a identidade viaja junto, e a próxima cópia o
//     reencontra mesmo que alguém o renomeie;
//   - colocação que já existe no destino é mantida COM A QUANTIDADE QUE TEM e
//     vai para `kept`, lado a lado com a da origem. Copiar o AP-01 para o
//     AP-02 não pode apagar os 8 pratos que alguém já contou no AP-02.
//
// Consequência: rodar duas vezes não cria nada na segunda.
//
// `origem*` já vem só com o que está ativo; `destino*` vem inteiro.
func planejarCopia(origemAmb, destinoAmb []ambienteGravado, origemCol, destinoCol []colocacaoDaCopia, soComodos bool) planoDeCopia {
	p := planoDeCopia{
		Ambientes:  []ambienteGravado{},
		Colocacoes: []colocacaoDaCopia{},
		Mantidas:   []ColocacaoMantida{},
	}

	porCodigo := map[string]ambienteGravado{}
	porNome := map[string]ambienteGravado{}
	for _, d := range destinoAmb {
		porCodigo[d.Codigo] = d
		porNome[d.Nome] = d
	}
	// destino: `code` do cômodo de origem → cômodo no destino (casado ou novo).
	destino := map[string]ambienteGravado{}
	for _, a := range origemAmb {
		d, casou := porCodigo[a.Codigo]
		if !casou {
			d, casou = porNome[a.Nome]
		}
		if !casou {
			d = ambienteGravado{Codigo: a.Codigo, Nome: a.Nome, Tipo: a.Tipo, Ordem: a.Ordem, Ativo: true}
			p.Ambientes = append(p.Ambientes, d)
			porCodigo[d.Codigo], porNome[d.Nome] = d, d
		}
		destino[a.Codigo] = d
	}
	if soComodos {
		return p
	}

	atual := map[chaveDeColocacaoNoDestino]int{}
	for _, c := range destinoCol {
		atual[chaveDeColocacaoNoDestino{c.AmbienteCodigo, c.BemID}] = c.Qtd
	}
	// Dois cômodos de origem podem cair no MESMO cômodo de destino (um casado
	// pelo `code`, outro pelo `name`): a colocação entra uma vez só.
	vista := map[chaveDeColocacaoNoDestino]bool{}
	for _, c := range origemCol {
		d, ok := destino[c.AmbienteCodigo]
		if !ok {
			continue
		}
		k := chaveDeColocacaoNoDestino{d.Codigo, c.BemID}
		if vista[k] {
			continue
		}
		vista[k] = true
		if qtd, existe := atual[k]; existe {
			p.Mantidas = append(p.Mantidas, ColocacaoMantida{
				AmbienteNome: d.Nome, BemNome: c.BemNome, QtdOrigem: c.Qtd, QtdAtual: qtd,
			})
			continue
		}
		p.Colocacoes = append(p.Colocacoes, colocacaoDaCopia{
			AmbienteCodigo: d.Codigo, AmbienteNome: d.Nome, BemID: c.BemID, BemNome: c.BemNome, Qtd: c.Qtd,
		})
	}
	return p
}

// resultado traduz o plano para a resposta do contrato.
func (p planoDeCopia) resultado(dryRun bool, origem UnidadeResumida) ResultadoDaCopia {
	out := ResultadoDaCopia{
		DryRun:            dryRun,
		Origem:            OrigemDaCopia{UnidadeID: origem.ID, Codigo: origem.Codigo},
		AmbientesCriados:  make([]AmbienteCopiado, 0, len(p.Ambientes)),
		ColocacoesCriadas: make([]ColocacaoCopia, 0, len(p.Colocacoes)),
		Mantidas:          p.Mantidas,
	}
	for _, a := range p.Ambientes {
		out.AmbientesCriados = append(out.AmbientesCriados, AmbienteCopiado{Nome: a.Nome, Tipo: a.Tipo})
	}
	for _, c := range p.Colocacoes {
		out.ColocacoesCriadas = append(out.ColocacoesCriadas, ColocacaoCopia{
			AmbienteNome: c.AmbienteNome, BemID: c.BemID, BemNome: c.BemNome, QtdEsperada: c.Qtd,
		})
	}
	return out
}
