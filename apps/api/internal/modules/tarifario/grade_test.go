package tarifario

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// celula monta uma célula do corpo.
func celula(produto uuid.UUID, tipo string, valor int64) CelulaDaGrade {
	return CelulaDaGrade{ProdutoID: produto, TipoDeData: tipo, ValorCents: valor}
}

// gravada monta uma célula como o banco a devolveria.
func gravada(produto uuid.UUID, codigo, tipo string, valor int64) CelulaGravada {
	return CelulaGravada{ID: uuid.New(), ProdutoID: produto, ProdutoCodigo: codigo, TipoDeData: tipo, ValorCents: valor}
}

// O achado BAIXO 10, reduzido à sua menor forma: o plano de um produto não pode
// conter nenhuma remoção de outro.
//
// A prova aqui é dupla e de propósito. `planejarGrade` só recebe as células do
// ESCOPO — então mesmo que a aritmética errasse, não haveria como ela nomear a
// célula de um produto que ela não viu; e o `escopoConhecido`/`CelulasDoEscopo`
// garantem que o repositório não as leia. O teste de integração fecha o outro
// lado, contra o banco.
func TestPlanoDeUmProdutoSoRemoveCelulaDele(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	codigos := map[uuid.UUID]string{a: "apto-2s", b: "cobertura"}

	// O banco tem os dois produtos; o escopo é só o A, então só as células do A
	// chegam ao planejamento — é o que CelulasDoEscopo faz com `= ANY($2)`.
	atuaisDoEscopo := []CelulaGravada{
		gravada(a, "apto-2s", "normal", 85000),
		gravada(a, "apto-2s", "fds", 95000),
	}

	plano := planejarGrade([]uuid.UUID{a}, codigos, atuaisDoEscopo,
		[]CelulaDaGrade{celula(a, "normal", 99000)})

	if plano.Resumo.Removidas != 1 {
		t.Fatalf("removed = %d, esperado 1 (só o `fds` do produto A)", plano.Resumo.Removidas)
	}
	if len(plano.Remover) != 1 || plano.Remover[0] != atuaisDoEscopo[1].ID {
		t.Fatalf("a remoção não caiu na célula certa: %v", plano.Remover)
	}
	for rotulo := range plano.Antes {
		if len(rotulo) >= 9 && rotulo[:9] == "cobertura" {
			t.Fatalf("o plano do produto A mexeu numa célula do produto B (%s)", rotulo)
		}
	}
}

// As quatro contagens do `meta` são o que avisa o operador de uma remoção que
// ele não pretendia. Cada uma tem de contar a coisa certa.
func TestResumoSeparaCriadaAtualizadaInalteradaERemovida(t *testing.T) {
	p := uuid.New()
	codigos := map[uuid.UUID]string{p: "apto-2s"}

	atuais := []CelulaGravada{
		gravada(p, "apto-2s", "normal", 85000),   // reenviada com OUTRO valor  → updated
		gravada(p, "apto-2s", "fds", 95000),      // reenviada IDÊNTICA         → unchanged
		gravada(p, "apto-2s", "feriado", 105000), // não veio no corpo          → removed
	}
	enviadas := []CelulaDaGrade{
		celula(p, "normal", 99000),
		celula(p, "fds", 95000),
		celula(p, "alta", 150000), // não existia → created
	}

	r := planejarGrade([]uuid.UUID{p}, codigos, atuais, enviadas).Resumo
	if r.Criadas != 1 || r.Atualizadas != 1 || r.Inalteradas != 1 || r.Removidas != 1 {
		t.Fatalf("created=%d updated=%d unchanged=%d removed=%d, esperado 1/1/1/1",
			r.Criadas, r.Atualizadas, r.Inalteradas, r.Removidas)
	}
}

// A trilha precisa responder "quem triplicou a tarifa?" sem uma segunda
// consulta: chave legível e os dois valores lado a lado.
func TestTrilhaDaGradeGuardaOValorVelhoEONovoPorCelula(t *testing.T) {
	p := uuid.New()
	codigos := map[uuid.UUID]string{p: "cobertura"}

	plano := planejarGrade([]uuid.UUID{p}, codigos,
		[]CelulaGravada{gravada(p, "cobertura", "reveillon", 250000)},
		[]CelulaDaGrade{celula(p, "reveillon", 750000)})

	if plano.Antes["cobertura.reveillon"] != int64(250000) {
		t.Fatalf("before = %v, esperado 250000 em `cobertura.reveillon`", plano.Antes)
	}
	if plano.Depois["cobertura.reveillon"] != int64(750000) {
		t.Fatalf("after = %v, esperado 750000 em `cobertura.reveillon`", plano.Depois)
	}
}

// Célula reenviada idêntica é ruído na trilha: ela entra na conta de
// `unchanged` e sai do before/after, senão a linha da tarifa que TRIPLICOU fica
// escondida entre vinte que não mudaram.
func TestCelulaInalteradaNaoEntraNaTrilha(t *testing.T) {
	p := uuid.New()
	plano := planejarGrade([]uuid.UUID{p}, map[uuid.UUID]string{p: "apto-2s"},
		[]CelulaGravada{gravada(p, "apto-2s", "normal", 85000)},
		[]CelulaDaGrade{celula(p, "normal", 85000)})

	if len(plano.Antes) != 0 || len(plano.Depois) != 0 {
		t.Fatalf("a trilha registrou uma célula que não mudou: antes=%v depois=%v", plano.Antes, plano.Depois)
	}
}

// O escopo declarado é o que decide o que vai ser APAGADO. Aceitar célula fora
// dele ampliaria o raio de ação em silêncio — que é exatamente o defeito.
func TestCelulaForaDoEscopoDeclaradoEhRecusada(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	erros := GradeEntrada{
		TabelaID:   uuid.New(),
		ProdutoIDs: []uuid.UUID{a},
		Celulas:    []CelulaDaGrade{celula(a, "normal", 85000), celula(b, "normal", 95000)},
	}.Validar()

	if erros["rates[1].unit_type_id"] == "" {
		t.Fatalf("a célula fora do escopo passou: %v", erros)
	}
	if erros["rates[0].unit_type_id"] != "" {
		t.Fatalf("a célula DENTRO do escopo foi recusada: %v", erros)
	}
}

// Ausente e vazio são coisas diferentes, e a diferença decide o raio de ação da
// chamada inteira. Quem trocar o slice por outro tipo precisa ver este teste
// reprovar.
func TestEscopoAusenteVemDasCelulasEEscopoVazioNaoTocaNada(t *testing.T) {
	a, b := uuid.New(), uuid.New()

	deduzido := GradeEntrada{Celulas: []CelulaDaGrade{
		celula(a, "normal", 1), celula(a, "fds", 2), celula(b, "normal", 3),
	}}
	if got := deduzido.Escopo(); len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("escopo deduzido = %v, esperado [A B] sem repetição", got)
	}
	if deduzido.EscopoDeclarado() {
		t.Fatal("corpo sem `unit_type_ids` foi lido como escopo declarado")
	}

	vazio := GradeEntrada{ProdutoIDs: []uuid.UUID{}}
	if !vazio.EscopoDeclarado() {
		t.Fatal("`unit_type_ids: []` foi lido como ausente — a chamada passaria a deduzir escopo de `rates`")
	}
	if len(vazio.Escopo()) != 0 {
		t.Fatalf("escopo vazio devolveu %v", vazio.Escopo())
	}
}

// Zerar um produto é ato explícito: citá-lo no escopo sem mandar célula. É o
// único caminho para apagar, e é deliberado que ele exija nomear o alvo.
func TestProdutoCitadoSemCelulaEhZerado(t *testing.T) {
	p := uuid.New()
	g := GradeEntrada{TabelaID: uuid.New(), ProdutoIDs: []uuid.UUID{p}}
	if erros := g.Validar(); len(erros) != 0 {
		t.Fatalf("corpo sem `rates` foi recusado: %v", erros)
	}

	plano := planejarGrade(g.Escopo(), map[uuid.UUID]string{p: "apto-2s"},
		[]CelulaGravada{gravada(p, "apto-2s", "normal", 85000), gravada(p, "apto-2s", "fds", 95000)},
		g.Celulas)

	if plano.Resumo.Removidas != 2 || len(plano.Remover) != 2 {
		t.Fatalf("removed = %d, esperado 2 — o produto citado sem célula não foi zerado", plano.Resumo.Removidas)
	}
	if len(plano.Gravar) != 0 {
		t.Fatalf("o plano quer gravar %d células num pedido que não mandou nenhuma", len(plano.Gravar))
	}
}

// ─────────────────────────── Auditoria ──────────────────────────────────────

// O ALTO 3: nenhuma escrita do tarifário pode ficar sem rastro. A prova é feita
// no service, com repositório falso, para cobrir os SETE caminhos numa
// execução — a versão de integração conta as linhas que sobram no banco.
func TestTodaEscritaDoTarifarioDeixaRastro(t *testing.T) {
	tabela, tarifa, feriado, periodo, minimo := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()

	repo := &repoFalso{
		propriedade:     uuid.New(),
		tabelas:         map[uuid.UUID]TabelaDeTarifas{tabela: {ID: tabela, Nome: "V1", Ativa: true}},
		tarifas:         map[uuid.UUID]Tarifa{tarifa: {ID: tarifa, TabelaID: tabela, ValorCents: 85000}},
		feriados:        map[uuid.UUID]Feriado{feriado: {ID: feriado, Nome: "Independência", Ativo: true}},
		periodos:        map[uuid.UUID]PeriodoEspecial{periodo: {ID: periodo, Nome: "Alta", Ativo: true}},
		minimos:         map[uuid.UUID]MinimoDeNoites{minimo: {ID: minimo, TabelaID: tabela, Noites: 2}},
		criados:         map[string]uuid.UUID{},
		produtosAchados: -1,
		vigentesTotal:   2, vigentesRestantes: 1,
	}
	svc := NovoService(repo, &txFalso{})
	ctx := context.Background()

	novoValor := 255000
	casos := []struct {
		nome     string
		esperado string
		fazer    func() error
	}{
		{"criar tarifa", "rates.criado", func() error {
			_, err := svc.CriarTarifa(ctx, TarifaEntrada{TabelaID: tabela, ProdutoID: uuid.New(), TipoDeData: "normal", ValorCents: 85000})
			return err
		}},
		{"triplicar tarifa", "rates.alterado", func() error {
			var a TarifaAtualizar
			a.ValorCents = optDe(int64(novoValor))
			_, err := svc.AtualizarTarifa(ctx, tarifa, a)
			return err
		}},
		{"excluir tarifa", "rates.excluido", func() error { return svc.ExcluirTarifa(ctx, tarifa) }},
		{"salvar a grade", "rate_tables.grade_salva", func() error {
			_, _, err := svc.SalvarGrade(ctx, GradeEntrada{TabelaID: tabela, Celulas: []CelulaDaGrade{celula(uuid.New(), "normal", 85000)}})
			return err
		}},
		{"desativar tabela", "rate_tables.desativada", func() error { return svc.DesativarTabela(ctx, tabela) }},
		{"excluir feriado", "holidays.excluido", func() error { return svc.ExcluirFeriado(ctx, feriado) }},
		{"excluir período", "special_periods.excluido", func() error { return svc.ExcluirPeriodo(ctx, periodo) }},
		{"excluir mínimo", "min_nights_rules.excluido", func() error { return svc.ExcluirMinimo(ctx, minimo) }},
		{"publicar política comercial", "commercial_policies.publicada", func() error {
			_, err := svc.PublicarPoliticaComercial(ctx, politicaValida(t, "2026-09-01"))
			return err
		}},
	}

	for _, caso := range casos {
		repo.trilha = nil
		if err := caso.fazer(); err != nil {
			t.Fatalf("%s: %v", caso.nome, err)
		}
		acoes := repo.acoesAuditadas()
		if len(acoes) != 1 || acoes[0] != caso.esperado {
			t.Fatalf("%s deixou a trilha %v, esperado exatamente [%s]", caso.nome, acoes, caso.esperado)
		}
	}
}

// A trilha da alteração precisa carregar o valor VELHO. Sem o `before` lido
// antes do UPDATE, a linha diz que a tarifa mudou e não diz para quê veio.
func TestTrilhaDaAlteracaoGuardaOValorAnterior(t *testing.T) {
	tarifa := uuid.New()
	repo := &repoFalso{
		propriedade:     uuid.New(),
		tarifas:         map[uuid.UUID]Tarifa{tarifa: {ID: tarifa, ValorCents: 85000}},
		criados:         map[string]uuid.UUID{},
		produtosAchados: -1,
	}
	svc := NovoService(repo, &txFalso{})

	var a TarifaAtualizar
	a.ValorCents = optDe(int64(255000))
	if _, err := svc.AtualizarTarifa(context.Background(), tarifa, a); err != nil {
		t.Fatalf("atualizando: %v", err)
	}

	if len(repo.trilha) != 1 {
		t.Fatalf("trilha com %d linhas, esperado 1", len(repo.trilha))
	}
	ev := repo.trilha[0]
	if ev.Antes["amount_cents"] != float64(85000) {
		t.Fatalf("before = %v, esperado amount_cents 85000 (o valor de antes do UPDATE)", ev.Antes)
	}
	if ev.Depois["amount_cents"] != float64(255000) {
		t.Fatalf("after = %v, esperado amount_cents 255000", ev.Depois)
	}
}
