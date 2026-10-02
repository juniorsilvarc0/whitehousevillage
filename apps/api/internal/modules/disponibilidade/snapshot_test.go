package disponibilidade

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/booking"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// snapshotDeTeste é um orçamento emitido de 3 noites por R$ 4.800,00.
func snapshotDeTeste() OrcamentoSalvo {
	return OrcamentoSalvo{
		Orcamento: Orcamento{
			Noites: 3, Subtotal: 480000, Total: 515000, Sinal: 257500,
			Diarias: []NoiteDoOrcamento{
				{Data: "2026-11-20", Tipo: calendar.Weekend, Preco: 160000},
				{Data: "2026-11-21", Tipo: calendar.Weekend, Preco: 160000},
				{Data: "2026-11-22", Tipo: calendar.Normal, Preco: 160000},
			},
		},
		ID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		UnitTypeID: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		CheckIn:    "2026-11-20",
		CheckOut:   "2026-11-23",
		Hospedes:   4,
	}
}

func entradaDoSnapshot(o OrcamentoSalvo) Entrada {
	return Entrada{
		UnitTypeID:  o.UnitTypeID,
		CheckIn:     calendar.MustParse(o.CheckIn),
		CheckOut:    calendar.MustParse(o.CheckOut),
		Hospedes:    o.Hospedes,
		DescontoPct: o.DescontoPct,
		IsEvento:    o.IsEvento,
	}
}

// O QUE O `/win` COMPRA COM ESTE MECANISMO: o motor não roda. Sem repositório,
// sem banco, sem calendário — se `Orcar` tentasse calcular, o nil do repositório
// derrubaria o teste.
func TestOrcamentoFixadoDevolveOSnapshotSemChamarOMotor(t *testing.T) {
	fixo := snapshotDeTeste()
	svc := NovoServico(nil, nil)

	saida, err := svc.Orcar(ComOrcamentoFixado(context.Background(), fixo), entradaDoSnapshot(fixo))
	if err != nil {
		t.Fatalf("orçando com snapshot fixado: %v", err)
	}
	if saida.Total != fixo.Total || saida.Subtotal != fixo.Subtotal || saida.Sinal != fixo.Sinal {
		t.Fatalf("o snapshot foi alterado no caminho: %+v", saida)
	}
	if len(saida.Diarias) != 3 || saida.Diarias[0].Preco != 160000 {
		t.Fatalf("as noites não são as do snapshot: %+v", saida.Diarias)
	}
}

// A trava do mecanismo: o snapshot só vale para a estadia que ele orçou.
// Devolver os valores de 3 noites para um pedido de 5 seria vender cinco noites
// pelo preço de três, em silêncio.
func TestOrcamentoFixadoRecusaPedidoDiferente(t *testing.T) {
	fixo := snapshotDeTeste()
	svc := NovoServico(nil, nil)

	casos := map[string]func(*Entrada){
		"outra estadia":  func(e *Entrada) { e.CheckOut = calendar.MustParse("2026-11-25") },
		"outro produto":  func(e *Entrada) { e.UnitTypeID = uuid.New() },
		"mais hóspedes":  func(e *Entrada) { e.Hospedes = 8 },
		"outro desconto": func(e *Entrada) { e.DescontoPct = 10 },
		"virou evento":   func(e *Entrada) { e.IsEvento = true },
	}
	for nome, mexer := range casos {
		t.Run(nome, func(t *testing.T) {
			e := entradaDoSnapshot(fixo)
			mexer(&e)

			_, err := svc.Orcar(ComOrcamentoFixado(context.Background(), fixo), e)
			if err == nil {
				t.Fatal("o snapshot foi aplicado a outro pedido")
			}
			if status := apperr.From(err).Status(); status != 500 {
				t.Fatalf("status = %d, esperado 500: divergir aqui é defeito de mecanismo, não pedido inválido", status)
			}
		})
	}
}

// Sem fixação, `Orcar` segue o caminho normal — e é o repositório nil que prova
// que ele tentou calcular de verdade.
func TestSemFixacaoOrcarNaoUsaSnapshotDeOutroContexto(t *testing.T) {
	svc := NovoServico(&repoFalso{}, nil)

	// Sem usuário no contexto o caminho normal para em Unauthorized, muito antes
	// de qualquer conta — o que importa é que ele NÃO devolveu um orçamento.
	if _, err := svc.Orcar(context.Background(), entradaDoSnapshot(snapshotDeTeste())); err == nil {
		t.Fatal("Orcar devolveu orçamento sem contexto e sem fixação")
	}
}

// ─────────────────────────── Validade ───────────────────────────────

// F2-05: sem `valid_until`, a validade é a `quote_validity_days` da política
// que precificou o orçamento — 15 aqui, longe do antigo 7 fixo, para o teste
// distinguir "leu a política" de "usou o número de sempre".
func TestValidadePadraoVemDaPoliticaQuePrecificou(t *testing.T) {
	agora := time.Date(2026, 8, 27, 15, 0, 0, 0, time.UTC)
	repo := &repoFalso{contexto: Contexto{Politica: booking.Policy{Version: 3, QuoteValidityDays: 15}}}
	svc := NovoServico(repo, nil)

	padrao, err := svc.validadeDoOrcamento(context.Background(), uuid.New(), Orcamento{PolicyVersion: 3}, nil, agora)
	if err != nil {
		t.Fatalf("validade padrão: %v", err)
	}
	if esperado := agora.AddDate(0, 0, 15); !padrao.Equal(esperado) {
		t.Fatalf("validade padrão = %s, esperado %s (15 dias da política)", padrao, esperado)
	}
}

// Política lida sem a coluna é defeito de leitura: 500, nunca um orçamento que
// nasce vencido e estoura no CHECK do COMMIT.
func TestValidadeComPoliticaSemColunaEhErroInterno(t *testing.T) {
	agora := time.Date(2026, 8, 27, 15, 0, 0, 0, time.UTC)
	svc := NovoServico(&repoFalso{contexto: Contexto{Politica: booking.Policy{Version: 3}}}, nil)

	_, err := svc.validadeDoOrcamento(context.Background(), uuid.New(), Orcamento{PolicyVersion: 3}, nil, agora)
	if got := apperr.From(err).Code; got != apperr.Internal.Code {
		t.Fatalf("código = %q, esperado %q (erro: %v)", got, apperr.Internal.Code, err)
	}
}

func TestValidadeNoPassadoOuNoInstanteDaEmissaoEhRecusada(t *testing.T) {
	agora := time.Date(2026, 8, 27, 15, 0, 0, 0, time.UTC)
	svc := NovoServico(&repoFalso{contexto: Contexto{Politica: booking.Policy{Version: 1, QuoteValidityDays: 7}}}, nil)

	for nome, pedido := range map[string]time.Time{
		"ontem":              agora.AddDate(0, 0, -1),
		"o próprio instante": agora, // `CHECK quotes_validade` exige estritamente maior
	} {
		t.Run(nome, func(t *testing.T) {
			_, err := svc.validadeDoOrcamento(context.Background(), uuid.New(), Orcamento{PolicyVersion: 1}, &pedido, agora)
			if err == nil {
				t.Fatal("proposta nasceu vencida")
			}
			ae := apperr.From(err)
			if ae.Code != "VALIDATION_ERROR" {
				t.Fatalf("código = %q", ae.Code)
			}
			if d, ok := ae.Details.(map[string]string); !ok || d["valid_until"] == "" {
				t.Fatalf("details sem valid_until: %#v", ae.Details)
			}
		})
	}
}

// ─────────────────────────── Agrupamento ────────────────────────────

// As linhas do orçamento relido saem do SNAPSHOT, na ordem em que o hóspede vive
// a estadia — e o unitário é o preço da noite, não uma média recomposta.
func TestAgrupamentoDasLinhasVemDoSnapshotNaOrdemDaEstadia(t *testing.T) {
	linhas := agruparEmLinhas([]NoiteDoOrcamento{
		{Data: "2026-11-20", Tipo: calendar.Weekend, Rotulo: "Fim de semana", Preco: 240000},
		{Data: "2026-11-21", Tipo: calendar.Weekend, Rotulo: "Fim de semana", Preco: 240000},
		{Data: "2026-11-22", Tipo: calendar.Normal, Rotulo: "Diária normal", Preco: 190000},
	})

	if len(linhas) != 2 {
		t.Fatalf("%d linhas, esperado 2", len(linhas))
	}
	if linhas[0].Tipo != calendar.Weekend || linhas[0].Noites != 2 ||
		linhas[0].Unitario != 240000 || linhas[0].Subtotal != 480000 {
		t.Fatalf("linha do fim de semana errada: %+v", linhas[0])
	}
	if linhas[1].Tipo != calendar.Normal || linhas[1].Noites != 1 || linhas[1].Subtotal != 190000 {
		t.Fatalf("linha da diária normal errada: %+v", linhas[1])
	}
}
