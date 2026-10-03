package disponibilidade

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/booking"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/money"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// ─────────────────────────── Duplo do repositório ───────────────────────────

// repoFalso devolve estado fixo. Existe para provar a MONTAGEM — a tradução
// banco → domínio → contrato — sem Postgres. Os valores que ele devolve são os
// da Tabela Comercial V1 (docs/spec.md §3), então quando o número bate aqui e
// no teste de integração, sabe-se que o motor está certo E que o seed carrega o
// que o motor espera.
type repoFalso struct {
	contexto Contexto
	cal      calendar.Commercial
	tarifas  map[uuid.UUID]map[calendar.DateType]money.Cents
	minimos  map[calendar.DateType]int
	produtos []Produto
	ocupacao map[ChaveDia]Contagem
	mapa     []CelulaBruta

	// composicao é opcional: o produto que não aparece aqui é tratado como
	// composição ÍNTEGRA. É o padrão certo para um duplo — quase todo teste
	// deste arquivo fala de preço, não de inventário quebrado, e ter de
	// declarar a composição em cada um deles esconderia o assunto de cada
	// teste atrás de dado de cenário.
	composicao map[uuid.UUID]ComposicaoDoProduto
}

func (r *repoFalso) Contexto(context.Context, uuid.UUID, *uuid.UUID, *int) (Contexto, error) {
	return r.contexto, nil
}
func (r *repoFalso) Calendario(context.Context, uuid.UUID, Janela) (calendar.Commercial, error) {
	return r.cal, nil
}
func (r *repoFalso) Tarifas(_ context.Context, _ uuid.UUID, produto *uuid.UUID) (map[uuid.UUID]map[calendar.DateType]money.Cents, error) {
	if produto == nil {
		return r.tarifas, nil
	}
	return map[uuid.UUID]map[calendar.DateType]money.Cents{*produto: r.tarifas[*produto]}, nil
}
func (r *repoFalso) EstadiaMinima(context.Context, uuid.UUID) (map[calendar.DateType]int, error) {
	return r.minimos, nil
}
func (r *repoFalso) Produtos(_ context.Context, _ uuid.UUID, produto *uuid.UUID) ([]Produto, error) {
	if produto == nil {
		return r.produtos, nil
	}
	var out []Produto
	for _, p := range r.produtos {
		if p.ID == *produto {
			out = append(out, p)
		}
	}
	return out, nil
}
func (r *repoFalso) Composicao(_ context.Context, _, produto uuid.UUID) (ComposicaoDoProduto, error) {
	if c, ok := r.composicao[produto]; ok {
		return c, nil
	}
	return ComposicaoDoProduto{Declaradas: 1, Ativas: 1}, nil
}
func (r *repoFalso) Ocupacao(context.Context, uuid.UUID, Janela, *uuid.UUID) (map[ChaveDia]Contagem, error) {
	return r.ocupacao, nil
}
func (r *repoFalso) Mapa(context.Context, uuid.UUID, Janela, *uuid.UUID) ([]CelulaBruta, error) {
	return r.mapa, nil
}

// contextoDe monta a identidade da requisição com o escopo pedido.
func contextoDe(escopo string) (context.Context, uuid.UUID) {
	eu := uuid.New()
	u := &auth.Usuario{
		ID:         eu,
		PropertyID: uuid.New(),
		Permissoes: auth.NovoConjunto([]auth.Permissao{
			{Resource: recursoCalendario, Action: auth.AcaoVer, Scope: escopo},
		}),
	}
	return auth.WithUser(context.Background(), u), eu
}

// calendarioV1 é o calendário comercial semeado, recortado para os testes.
func calendarioV1() calendar.Commercial {
	return calendar.Commercial{
		Holidays: map[string]string{
			"2026-11-15": "Proclamação da República",
			"2026-12-25": "Natal",
			"2027-01-01": "Confraternização Universal",
		},
		Periods: []calendar.Period{
			{Name: "Réveillon 2026/2027", Type: calendar.NewYear,
				From: calendar.MustParse("2026-12-27"), To: calendar.MustParse("2027-01-02")},
			{Name: "Alta temporada 2026/2027", Type: calendar.HighSeason,
				From: calendar.MustParse("2026-12-15"), To: calendar.MustParse("2027-01-31")},
		},
		WeekendDays: []time.Weekday{time.Friday, time.Saturday},
	}
}

func politicaV1() booking.Policy {
	return booking.Policy{
		Version:             1,
		DepositPct:          50,
		BalanceDueDays:      7,
		HoldHours:           48,
		DiscountAutoPct:     5,
		DiscountApprovalPct: 10,
		EventDeposit:        money.FromReais(2000),
	}
}

func minimosV1() map[calendar.DateType]int {
	return map[calendar.DateType]int{
		calendar.Normal: 1, calendar.Weekend: 2, calendar.Holiday: 3,
		calendar.HighSeason: 3, calendar.NewYear: 4, calendar.Carnival: 4,
	}
}

// ─────────────────────────── O cenário dos proprietários ────────────────────

// A Cobertura de 20 a 23/11/2026 é o cenário demonstrado aos proprietários:
// sexta e sábado a R$ 2.400 e domingo a R$ 1.900, mais R$ 350 de limpeza, dá
// R$ 7.050 com sinal de R$ 3.525.
//
// Repare no que o número prova: o domingo NÃO é fim de semana na White House (o
// hóspede vai embora), a limpeza é cobrada UMA vez pela estadia, e o sinal sai
// de 50% do TOTAL — não de 50% das diárias.
func TestCoberturaDeNovembroFechaEmSeteMilECinquenta(t *testing.T) {
	cobertura := uuid.New()
	repo := &repoFalso{
		contexto: Contexto{RateTableID: uuid.New(), Politica: politicaV1()},
		cal:      calendarioV1(),
		minimos:  minimosV1(),
		produtos: []Produto{{
			ID: cobertura, Codigo: "cobertura", Nome: "White House Cobertura",
			Capacidade: 10, Consome: ConsomeUma, LimpezaCent: money.FromReais(350),
		}},
		tarifas: map[uuid.UUID]map[calendar.DateType]money.Cents{cobertura: {
			calendar.Normal: money.FromReais(1900), calendar.Weekend: money.FromReais(2400),
			calendar.Holiday: money.FromReais(3100), calendar.HighSeason: money.FromReais(3600),
			calendar.NewYear: money.FromReais(7500), calendar.Carnival: money.FromReais(6000),
		}},
	}

	ctx, _ := contextoDe(auth.EscopoAll)
	orcamento, err := NovoServico(repo, nil).Orcar(ctx, Entrada{
		UnitTypeID: cobertura,
		CheckIn:    calendar.MustParse("2026-11-20"),
		CheckOut:   calendar.MustParse("2026-11-23"),
		Hospedes:   8,
	})
	if err != nil {
		t.Fatalf("Orcar: %v", err)
	}

	esperado := map[string]int64{
		"noites":   3,
		"subtotal": 670000, // 2.400 + 2.400 + 1.900
		"limpeza":  35000,
		"total":    705000,
		"sinal":    352500,
		"saldo":    352500,
	}
	obtido := map[string]int64{
		"noites":   int64(orcamento.Noites),
		"subtotal": orcamento.Subtotal,
		"limpeza":  orcamento.Limpeza,
		"total":    orcamento.Total,
		"sinal":    orcamento.Sinal,
		"saldo":    orcamento.Saldo,
	}
	for campo, quero := range esperado {
		if obtido[campo] != quero {
			t.Errorf("%s = %d, quero %d (%s)", campo, obtido[campo], quero, money.Cents(quero))
		}
	}

	// A classificação noite a noite é o que a gestão mostra ao hóspede.
	tipos := []calendar.DateType{calendar.Weekend, calendar.Weekend, calendar.Normal}
	if len(orcamento.Diarias) != 3 {
		t.Fatalf("diárias = %d, quero 3", len(orcamento.Diarias))
	}
	for i, quero := range tipos {
		if orcamento.Diarias[i].Tipo != quero {
			t.Errorf("noite %s: tipo %q, quero %q", orcamento.Diarias[i].Data, orcamento.Diarias[i].Tipo, quero)
		}
	}
	// A data sai ISO, e não como objeto {Year,Month,Day}: é a razão de existir
	// o DTO em vez de serializar booking.Quote direto.
	if orcamento.Diarias[0].Data != "2026-11-20" {
		t.Errorf("data da primeira noite = %q, quero \"2026-11-20\"", orcamento.Diarias[0].Data)
	}
	// Duas linhas agrupadas (fds ×2 e normal ×1), na ordem em que o hóspede
	// vive a estadia.
	if len(orcamento.Linhas) != 2 || orcamento.Linhas[0].Noites != 2 || orcamento.Linhas[1].Noites != 1 {
		t.Errorf("linhas do orçamento inesperadas: %+v", orcamento.Linhas)
	}
	if orcamento.MinNoites != 2 {
		t.Errorf("min_nights = %d, quero 2 (a noite mais restritiva é fds)", orcamento.MinNoites)
	}
}

// Réveillon atravessando o ano: 28/12/2026 a 03/01/2027 classifica as SEIS
// noites como `reveillon` — inclusive 01/01, que é feriado nacional e está
// dentro da alta temporada. É a precedência resolvendo (100 > 80 > 60), e é o
// critério de aceite escrito em docs/spec.md §3.
func TestReveillonAtravessaOAnoESobrepoeFeriadoEAlta(t *testing.T) {
	completa := uuid.New()
	repo := &repoFalso{
		contexto: Contexto{RateTableID: uuid.New(), Politica: politicaV1()},
		cal:      calendarioV1(),
		minimos:  minimosV1(),
		produtos: []Produto{{
			ID: completa, Codigo: "completa", Nome: "White House Completa",
			Capacidade: 24, Consome: ConsomeTodas, LimpezaCent: money.FromReais(900),
		}},
		tarifas: map[uuid.UUID]map[calendar.DateType]money.Cents{completa: {
			calendar.Normal: money.FromReais(5500), calendar.Weekend: money.FromReais(6900),
			calendar.Holiday: money.FromReais(8900), calendar.HighSeason: money.FromReais(10500),
			calendar.NewYear: money.FromReais(21000), calendar.Carnival: money.FromReais(17000),
		}},
	}

	ctx, _ := contextoDe(auth.EscopoAll)
	orcamento, err := NovoServico(repo, nil).Orcar(ctx, Entrada{
		UnitTypeID: completa,
		CheckIn:    calendar.MustParse("2026-12-28"),
		CheckOut:   calendar.MustParse("2027-01-03"),
		Hospedes:   24,
	})
	if err != nil {
		t.Fatalf("Orcar: %v", err)
	}

	if len(orcamento.Diarias) != 6 {
		t.Fatalf("noites = %d, quero 6", len(orcamento.Diarias))
	}
	for _, n := range orcamento.Diarias {
		if n.Tipo != calendar.NewYear {
			t.Errorf("noite %s classificada como %q, quero \"reveillon\"", n.Data, n.Tipo)
		}
		if n.Preco != 2100000 {
			t.Errorf("noite %s a %s, quero R$ 21.000,00", n.Data, money.Cents(n.Preco))
		}
	}
	// Uma linha só: seis noites do mesmo tipo não viram seis linhas.
	if len(orcamento.Linhas) != 1 || orcamento.Linhas[0].Noites != 6 {
		t.Errorf("linhas = %+v, quero uma linha de 6 noites", orcamento.Linhas)
	}
	if quero := int64(6*2100000 + 90000); orcamento.Total != quero {
		t.Errorf("total = %d, quero %d", orcamento.Total, quero)
	}
	if orcamento.MinNoites != 4 {
		t.Errorf("min_nights = %d, quero 4", orcamento.MinNoites)
	}
}

// ─────────────────────────── A Completa e as oito unidades ──────────────────

// UMA unidade ocupada fecha a White House Completa. É a regra que impede a
// gestão de prometer a casa a um evento depois de vender um apartamento — e ela
// sai da COMPOSIÇÃO (`consumes = all_members`), nunca do nome do produto.
func TestCompletaFicaIndisponivelComUmaUnicaUnidadeOcupada(t *testing.T) {
	completa, apto := uuid.New(), uuid.New()
	livre, disputado := "2026-11-20", "2026-11-21"

	repo := &repoFalso{
		contexto: Contexto{RateTableID: uuid.New(), Politica: politicaV1()},
		cal:      calendarioV1(),
		minimos:  minimosV1(),
		produtos: []Produto{
			{ID: apto, Codigo: "apto-2s", Nome: "Apartamento 2 Suítes", Capacidade: 6, Consome: ConsomeUma},
			{ID: completa, Codigo: "completa", Nome: "White House Completa", Capacidade: 24, Consome: ConsomeTodas},
		},
		tarifas: map[uuid.UUID]map[calendar.DateType]money.Cents{
			apto:     {calendar.Weekend: money.FromReais(1100)},
			completa: {calendar.Weekend: money.FromReais(6900)},
		},
		ocupacao: map[ChaveDia]Contagem{
			{Produto: completa, Dia: livre}:     {Declaradas: 8, Ativas: 8, Ocupadas: 0},
			{Produto: completa, Dia: disputado}: {Declaradas: 8, Ativas: 8, Ocupadas: 1},
			{Produto: apto, Dia: livre}:         {Declaradas: 3, Ativas: 3, Ocupadas: 0},
			{Produto: apto, Dia: disputado}:     {Declaradas: 3, Ativas: 3, Ocupadas: 1},
		},
	}

	ctx, _ := contextoDe(auth.EscopoAll)
	linhas, err := NovoServico(repo, nil).PorProduto(ctx, Janela{
		De: calendar.MustParse(livre), Ate: calendar.MustParse("2026-11-22"),
	}, nil)
	if err != nil {
		t.Fatalf("PorProduto: %v", err)
	}
	if len(linhas) != 2 {
		t.Fatalf("produtos = %d, quero 2", len(linhas))
	}

	porProduto := map[uuid.UUID]DisponibilidadeDoProduto{}
	for _, l := range linhas {
		porProduto[l.UnitTypeID] = l
	}

	// A Completa é 0 ou 1, nunca "7 de 8": vender 7/8 da casa não existe.
	if got := porProduto[completa].Dias[0].Disponivel; got != 1 {
		t.Errorf("Completa em %s: available = %d, quero 1", livre, got)
	}
	if got := porProduto[completa].Dias[1].Disponivel; got != 0 {
		t.Errorf("Completa em %s com 1 unidade ocupada: available = %d, quero 0", disputado, got)
	}
	if got := porProduto[completa].TotalUnidades; got != 8 {
		t.Errorf("total_units da Completa = %d, quero 8", got)
	}

	// O apartamento, no mesmo dia, ainda tem duas das três unidades.
	if got := porProduto[apto].Dias[1].Disponivel; got != 2 {
		t.Errorf("Apartamento em %s: available = %d, quero 2", disputado, got)
	}
	// Preço e mínimo acompanham o tipo da noite (20 e 21/11/2026 são sexta e sábado).
	if p := porProduto[apto].Dias[0].Preco; p == nil || *p != 110000 {
		t.Errorf("preço do apartamento na sexta = %v, quero 110000", p)
	}
	if got := porProduto[apto].Dias[0].MinNoites; got != 2 {
		t.Errorf("min_nights de fds = %d, quero 2", got)
	}
}

// A tabela de "quantas dá para vender" — agora com as três condições que a
// venda impõe, não só a ocupação. Cada linha com `quero` 0 declara TAMBÉM o
// motivo, porque é o motivo que diz à gestão se ela precisa agir hoje.
func TestVendaveisPorTipoDeConsumo(t *testing.T) {
	casos := []struct {
		nome        string
		consome     string
		c           Contagem
		temTarifa   bool
		quero       int
		queroMotivo string
	}{
		{"apartamento com tudo livre", ConsomeUma, Contagem{3, 3, 0}, true, 3, ""},
		{"apartamento com uma ocupada", ConsomeUma, Contagem{3, 3, 1}, true, 2, ""},
		{"apartamento lotado", ConsomeUma, Contagem{3, 3, 3}, true, 0, MotivoOcupado},
		{"completa intacta", ConsomeTodas, Contagem{8, 8, 0}, true, 1, ""},
		{"completa com uma ocupada", ConsomeTodas, Contagem{8, 8, 1}, true, 0, MotivoOcupado},
		{"completa lotada", ConsomeTodas, Contagem{8, 8, 8}, true, 0, MotivoOcupado},
		{"produto sem composição", ConsomeUma, Contagem{0, 0, 0}, true, 0, MotivoUnidadeInativa},
		{"completa sem composição", ConsomeTodas, Contagem{0, 0, 0}, true, 0, MotivoUnidadeInativa},

		// As duas condições que a rota antiga ignorava.
		{"apartamento sem tarifa no dia", ConsomeUma, Contagem{3, 3, 0}, false, 0, MotivoSemTarifa},
		{"completa com AP-03 inativa", ConsomeTodas, Contagem{8, 7, 0}, true, 0, MotivoComposicaoIncompleta},
		{"apartamento com uma unidade inativa ainda vende as outras", ConsomeUma, Contagem{3, 2, 0}, true, 2, ""},
		{"apartamento com todas inativas", ConsomeUma, Contagem{3, 0, 0}, true, 0, MotivoUnidadeInativa},

		// Precedência: composição quebrada vence tarifa faltando, porque é o
		// defeito mais caro de descobrir tarde.
		{"completa quebrada E sem tarifa", ConsomeTodas, Contagem{8, 7, 0}, false, 0, MotivoComposicaoIncompleta},
		// E tarifa faltando vence ocupação: a gestão tem de agir de qualquer
		// jeito, mesmo que naquele dia também não houvesse o que vender.
		{"apartamento lotado E sem tarifa", ConsomeUma, Contagem{3, 3, 3}, false, 0, MotivoSemTarifa},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, motivoLido := vendaveis(c.consome, c.c, c.temTarifa)
			if got != c.quero {
				t.Errorf("vendaveis(%q, %+v, tarifa=%v) = %d, quero %d", c.consome, c.c, c.temTarifa, got, c.quero)
			}
			if c.queroMotivo == "" {
				if motivoLido != nil {
					t.Errorf("available = %d e mesmo assim veio unavailable_reason %q — o contrato pede null", got, *motivoLido)
				}
				return
			}
			if motivoLido == nil {
				t.Fatalf("available = 0 sem unavailable_reason: a tela pinta a célula cinza sem saber dizer por quê")
			}
			if *motivoLido != c.queroMotivo {
				t.Errorf("unavailable_reason = %q, quero %q", *motivoLido, c.queroMotivo)
			}
		})
	}
}

// ─────────────────────────── Mapa de ocupação ───────────────────────────────

func ptr[T any](v T) *T { return &v }

// O escopo `own` esconde a IDENTIFICAÇÃO da reserva alheia, nunca a OCUPAÇÃO.
// Um mapa que mostrasse a data do outro corretor como livre faria o corretor
// prometer a casa e a gravação estourar 409 depois.
func TestEscopoOwnEscondeOHospedeMasNuncaAOcupacao(t *testing.T) {
	ctx, eu := contextoDe(auth.EscopoOwn)
	unidade, bloco, minha, alheia := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	outro := uuid.New()

	repo := &repoFalso{
		cal: calendarioV1(),
		mapa: []CelulaBruta{
			{
				UnitID: unidade, UnitCode: "AP-01", UnitName: "Apartamento 201", Dia: "2026-11-20",
				StayBlockID: &bloco, BlocoStatus: ptr("confirmed"), BlocoSource: ptr("reservation"),
				ReservaID: &minha, ReservaCodigo: ptr("WH-2026-0001"), Hospede: ptr("Hóspede Meu"),
				DonoID: &eu,
			},
			{
				UnitID: unidade, UnitCode: "AP-01", UnitName: "Apartamento 201", Dia: "2026-11-21",
				StayBlockID: &bloco, BlocoStatus: ptr("hold"), BlocoSource: ptr("reservation"),
				ReservaID: &alheia, ReservaCodigo: ptr("WH-2026-0002"), Hospede: ptr("Hóspede do Outro"),
				DonoID: &outro,
			},
			{UnitID: unidade, UnitCode: "AP-01", UnitName: "Apartamento 201", Dia: "2026-11-22"},
		},
	}

	linhas, err := NovoServico(repo, nil).PorUnidade(ctx, Janela{
		De: calendar.MustParse("2026-11-20"), Ate: calendar.MustParse("2026-11-23"),
	}, nil)
	if err != nil {
		t.Fatalf("PorUnidade: %v", err)
	}
	if len(linhas) != 1 || len(linhas[0].Dias) != 3 {
		t.Fatalf("mapa = %d linha(s) / %d dias, quero 1 / 3", len(linhas), len(linhas[0].Dias))
	}

	minhaCelula, alheiaCelula, vazia := linhas[0].Dias[0], linhas[0].Dias[1], linhas[0].Dias[2]

	if minhaCelula.Hospede == nil || *minhaCelula.Hospede != "Hóspede Meu" {
		t.Errorf("a própria reserva deveria mostrar o hóspede, veio %v", minhaCelula.Hospede)
	}
	if minhaCelula.Status != StatusConfirmado {
		t.Errorf("status = %q, quero %q", minhaCelula.Status, StatusConfirmado)
	}

	if alheiaCelula.Status != StatusHold {
		t.Errorf("a ocupação alheia sumiu: status = %q, quero %q", alheiaCelula.Status, StatusHold)
	}
	if alheiaCelula.StayBlockID == nil {
		t.Error("a ocupação alheia perdeu o stay_block_id — o mapa passaria a mentir sobre a data")
	}
	if alheiaCelula.Hospede != nil || alheiaCelula.ReservaCodigo != nil || alheiaCelula.ReservaID != nil {
		t.Errorf("escopo own vazou identificação alheia: %+v", alheiaCelula)
	}

	if vazia.Status != StatusLivre || vazia.StayBlockID != nil {
		t.Errorf("dia sem bloco deveria ser livre, veio %+v", vazia)
	}
	// 20 e 21/11/2026 são sexta e sábado; 22 é domingo, que aqui é diária normal.
	if minhaCelula.TipoDeData != calendar.Weekend || vazia.TipoDeData != calendar.Normal {
		t.Errorf("tipos de data errados: %q e %q", minhaCelula.TipoDeData, vazia.TipoDeData)
	}
}

func TestEscopoAllMostraTodaIdentificacao(t *testing.T) {
	ctx, _ := contextoDe(auth.EscopoAll)
	unidade, bloco, alheia, outro := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	repo := &repoFalso{
		cal: calendarioV1(),
		mapa: []CelulaBruta{{
			UnitID: unidade, UnitCode: "AP-01", UnitName: "Apartamento 201", Dia: "2026-11-20",
			StayBlockID: &bloco, BlocoStatus: ptr("confirmed"), BlocoSource: ptr("reservation"),
			ReservaID: &alheia, ReservaCodigo: ptr("WH-2026-0002"), Hospede: ptr("Hóspede do Outro"),
			DonoID: &outro,
		}},
	}

	linhas, err := NovoServico(repo, nil).PorUnidade(ctx, Janela{
		De: calendar.MustParse("2026-11-20"), Ate: calendar.MustParse("2026-11-21"),
	}, nil)
	if err != nil {
		t.Fatalf("PorUnidade: %v", err)
	}
	if c := linhas[0].Dias[0]; c.Hospede == nil || c.ReservaCodigo == nil {
		t.Errorf("escopo all deveria ver tudo, veio %+v", c)
	}
}

func TestStatusDaCelulaSaiDoStatusOuDaOrigem(t *testing.T) {
	bloco := uuid.New()
	casos := []struct {
		nome   string
		celula CelulaBruta
		quero  string
	}{
		{"sem bloco", CelulaBruta{}, StatusLivre},
		{"pré-reserva", CelulaBruta{StayBlockID: &bloco, BlocoSource: ptr("reservation"), BlocoStatus: ptr("hold")}, StatusHold},
		{"confirmada", CelulaBruta{StayBlockID: &bloco, BlocoSource: ptr("reservation"), BlocoStatus: ptr("confirmed")}, StatusConfirmado},
		{"manutenção", CelulaBruta{StayBlockID: &bloco, BlocoSource: ptr("maintenance"), BlocoStatus: ptr("confirmed")}, StatusManutencao},
		{"uso do proprietário", CelulaBruta{StayBlockID: &bloco, BlocoSource: ptr("owner_hold"), BlocoStatus: ptr("confirmed")}, StatusProprietario},
		{"importação de OTA", CelulaBruta{StayBlockID: &bloco, BlocoSource: ptr("ota"), BlocoStatus: ptr("confirmed")}, StatusOTA},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := statusDaCelula(c.celula); got != c.quero {
				t.Errorf("statusDaCelula = %q, quero %q", got, c.quero)
			}
		})
	}
}

// ─────────────────────────── Tradução dos erros do motor ────────────────────

// O front reage ao Code, nunca ao texto: o código que sai da API tem de ser
// exatamente o que booking.RuleError carimbou, com os details intactos.
func TestTraduzirRegraPreservaCodigoStatusEDetalhes(t *testing.T) {
	casos := []struct {
		code   string
		status int
	}{
		{"VALIDATION_ERROR", 422},
		{"CAPACITY_EXCEEDED", 422},
		{"MIN_STAY_NOT_MET", 422},
		{"DISCOUNT_ABOVE_LIMIT", 422},
		// Emitido por booking.Build; o status vem de apperr.RateNotFound.
		{"RATE_NOT_FOUND", 422},
	}
	for _, c := range casos {
		t.Run(c.code, func(t *testing.T) {
			detalhes := map[string]any{"pista": c.code}
			traduzido := traduzirRegra(&booking.RuleError{
				Code: c.code, Message: "mensagem do motor", Details: detalhes,
			})

			var e *apperr.Error
			if !errors.As(traduzido, &e) {
				t.Fatalf("não virou *apperr.Error: %T", traduzido)
			}
			if e.Code != c.code {
				t.Errorf("code = %q, quero %q", e.Code, c.code)
			}
			if e.Status() != c.status {
				t.Errorf("status = %d, quero %d", e.Status(), c.status)
			}
			if e.Details == nil {
				t.Error("details perdidos na tradução")
			}
			if e.Message != "mensagem do motor" {
				t.Errorf("mensagem = %q", e.Message)
			}
		})
	}
}

// Erro que não vem do motor passa reto: embrulhar tudo em 422 esconderia falha
// de banco atrás de "dados inválidos".
func TestTraduzirRegraNaoMexeEmErroDeInfra(t *testing.T) {
	original := errors.New("conexão caiu")
	if got := traduzirRegra(original); !errors.Is(got, original) {
		t.Errorf("erro de infra foi reescrito: %v", got)
	}
}

// Regras do motor chegam à API pelo caminho real, não só pela função de
// tradução: capacidade e alçada saem de Orcar com o código certo.
func TestOrcarPropagaAsRegrasDoMotor(t *testing.T) {
	cobertura := uuid.New()
	repo := &repoFalso{
		contexto: Contexto{RateTableID: uuid.New(), Politica: politicaV1()},
		cal:      calendarioV1(),
		minimos:  minimosV1(),
		produtos: []Produto{{
			ID: cobertura, Codigo: "cobertura", Nome: "White House Cobertura",
			Capacidade: 10, Consome: ConsomeUma, LimpezaCent: money.FromReais(350),
		}},
		tarifas: map[uuid.UUID]map[calendar.DateType]money.Cents{cobertura: {
			calendar.Normal: money.FromReais(1900), calendar.Weekend: money.FromReais(2400),
			calendar.NewYear: money.FromReais(7500), calendar.HighSeason: money.FromReais(3600),
		}},
	}
	svc := NovoServico(repo, nil)
	ctx, _ := contextoDe(auth.EscopoAll)

	base := Entrada{
		UnitTypeID: cobertura,
		CheckIn:    calendar.MustParse("2026-11-20"),
		CheckOut:   calendar.MustParse("2026-11-23"),
		Hospedes:   8,
	}

	casos := []struct {
		nome     string
		ajustar  func(*Entrada)
		queroCod string
	}{
		{"acima da capacidade", func(e *Entrada) { e.Hospedes = 11 }, "CAPACITY_EXCEEDED"},
		{"desconto fora da alçada", func(e *Entrada) { e.DescontoPct = 15 }, "DISCOUNT_ABOVE_LIMIT"},
		{"check-out antes do check-in", func(e *Entrada) { e.CheckOut = e.CheckIn }, "VALIDATION_ERROR"},
		{"noite sem tarifa cadastrada", func(e *Entrada) {
			e.CheckIn = calendar.MustParse("2026-11-15") // feriado; a Cobertura não tem tarifa de feriado neste caso
			e.CheckOut = calendar.MustParse("2026-11-18")
		}, "RATE_NOT_FOUND"},
		{"abaixo do mínimo do réveillon", func(e *Entrada) {
			e.CheckIn = calendar.MustParse("2026-12-28")
			e.CheckOut = calendar.MustParse("2026-12-30")
		}, "MIN_STAY_NOT_MET"},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			e := base
			c.ajustar(&e)
			_, err := svc.Orcar(ctx, e)

			var apperro *apperr.Error
			if !errors.As(err, &apperro) {
				t.Fatalf("erro = %v (%T), quero *apperr.Error", err, err)
			}
			if apperro.Code != c.queroCod {
				t.Errorf("code = %q, quero %q", apperro.Code, c.queroCod)
			}
		})
	}
}

// Desconto dentro da alçada sai com o carimbo da autoridade e NÃO toca a
// limpeza: 5% sobre R$ 6.700 são R$ 335, e o total cai exatamente isso.
func TestDescontoIncideSoSobreAsDiarias(t *testing.T) {
	cobertura := uuid.New()
	repo := &repoFalso{
		contexto: Contexto{RateTableID: uuid.New(), Politica: politicaV1()},
		cal:      calendarioV1(),
		minimos:  minimosV1(),
		produtos: []Produto{{
			ID: cobertura, Codigo: "cobertura", Nome: "White House Cobertura",
			Capacidade: 10, Consome: ConsomeUma, LimpezaCent: money.FromReais(350),
		}},
		tarifas: map[uuid.UUID]map[calendar.DateType]money.Cents{cobertura: {
			calendar.Normal: money.FromReais(1900), calendar.Weekend: money.FromReais(2400),
		}},
	}

	ctx, _ := contextoDe(auth.EscopoAll)
	o, err := NovoServico(repo, nil).Orcar(ctx, Entrada{
		UnitTypeID:  cobertura,
		CheckIn:     calendar.MustParse("2026-11-20"),
		CheckOut:    calendar.MustParse("2026-11-23"),
		Hospedes:    8,
		DescontoPct: 5,
	})
	if err != nil {
		t.Fatalf("Orcar: %v", err)
	}
	if o.Desconto != 33500 {
		t.Errorf("desconto = %s, quero R$ 335,00 (5%% de R$ 6.700)", money.Cents(o.Desconto))
	}
	if o.Limpeza != 35000 {
		t.Errorf("limpeza = %s, quero R$ 350,00 — desconto não pode encostar nela", money.Cents(o.Limpeza))
	}
	if o.Total != 670000-33500+35000 {
		t.Errorf("total = %s", money.Cents(o.Total))
	}
	if o.Alcada != booking.AuthorityManager {
		t.Errorf("alçada = %q, quero %q", o.Alcada, booking.AuthorityManager)
	}
	if o.PolicyVersion != 1 {
		t.Errorf("policy_version = %d, quero 1", o.PolicyVersion)
	}
	if o.RateTableID != repo.contexto.RateTableID {
		t.Error("rate_table_id não é a tabela que precificou")
	}
}

// Produto que não existe (ou está desativado) é 404, nunca lista vazia.
func TestProdutoInexistenteEh404(t *testing.T) {
	repo := &repoFalso{contexto: Contexto{RateTableID: uuid.New(), Politica: politicaV1()}, cal: calendarioV1()}
	svc := NovoServico(repo, nil)
	ctx, _ := contextoDe(auth.EscopoAll)
	fantasma := uuid.New()

	if _, err := svc.Orcar(ctx, Entrada{UnitTypeID: fantasma, Hospedes: 2,
		CheckIn: calendar.MustParse("2026-11-20"), CheckOut: calendar.MustParse("2026-11-23")}); apperr.From(err).Status() != 404 {
		t.Errorf("Orcar com produto inexistente = %v, quero 404", err)
	}
	if _, err := svc.PorProduto(ctx, Janela{
		De: calendar.MustParse("2026-11-20"), Ate: calendar.MustParse("2026-11-21"),
	}, &fantasma); apperr.From(err).Status() != 404 {
		t.Errorf("PorProduto com filtro inexistente = %v, quero 404", err)
	}
}

// Sem identidade no contexto não há propriedade — e sem propriedade a consulta
// veria o calendário de outra casa.
func TestSemUsuarioNoContextoEh401(t *testing.T) {
	svc := NovoServico(&repoFalso{}, nil)
	if _, err := svc.PorProduto(context.Background(), Janela{
		De: calendar.MustParse("2026-11-20"), Ate: calendar.MustParse("2026-11-21"),
	}, nil); apperr.From(err).Status() != 401 {
		t.Errorf("erro = %v, quero 401", err)
	}
}
