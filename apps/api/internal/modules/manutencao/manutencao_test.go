package manutencao

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/maintenance"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

func TestCriarValidaAFormaENaoARegra(t *testing.T) {
	ok := OrdemDeManutencaoCriar{UnidadeID: uuid.New(), Titulo: "Ar da suíte"}
	if f := ok.Validar(); len(f) != 0 {
		t.Fatalf("corpo mínimo recusado: %v", f)
	}
	zero, prioridade := int64(0), "altissima"
	ruim := OrdemDeManutencaoCriar{
		Titulo: "   ", Descricao: ptr(strings.Repeat("é", tamanhoDaDescricao+1)), Prioridade: &prioridade,
		CustoCents: &zero, Bloqueio: &PeriodoDoBloqueio{De: "10/11/2026"},
	}
	f := ruim.Validar()
	for _, campo := range []string{"unit_id", "title", "description", "priority", "cost_cents", "block.from", "block.to"} {
		if f[campo] == "" {
			t.Errorf("faltou erro em %s: %v", campo, f)
		}
	}
	// O teto conta CARACTERES: 200 "ç" (400 bytes) passam.
	if f := (OrdemDeManutencaoCriar{UnidadeID: uuid.New(), Titulo: strings.Repeat("ç", 200)}).Validar(); len(f) != 0 {
		t.Fatalf("200 caracteres multibyte passam: %v", f)
	}
	// O período passado é REGRA (precisa de hoje): o DTO não o recusa.
	if f := (OrdemDeManutencaoCriar{UnidadeID: uuid.New(), Titulo: "x",
		Bloqueio: &PeriodoDoBloqueio{De: "2001-01-01", Ate: "2001-01-02"}}).Validar(); len(f) != 0 {
		t.Fatalf("o DTO só confere forma: %v", f)
	}
}

func TestAtualizarSeparaAusenteDeNulo(t *testing.T) {
	vazio := OrdemDeManutencaoAtualizar{}
	if !vazio.vazio() || len(vazio.Validar()) != 0 {
		t.Fatal("corpo vazio é válido e não mexe em nada")
	}
	so := OrdemDeManutencaoAtualizar{CustoCents: httpx.Nulo[int64]()}
	if e := so.edicao(); !e.Cost || e.Other || so.vazio() {
		t.Fatalf("`cost_cents: null` é edição só do custo: %+v", e)
	}
	if e := (OrdemDeManutencaoAtualizar{Descricao: httpx.Nulo[string]()}).edicao(); !e.Other {
		t.Fatal("campo presente, mesmo nulo, é edição de outra coisa")
	}
	f := (OrdemDeManutencaoAtualizar{Titulo: httpx.Nulo[string](), Prioridade: httpx.Nulo[string](),
		CustoCents: httpx.De[int64](-5)}).Validar()
	for _, campo := range []string{"title", "priority", "cost_cents"} {
		if f[campo] == "" {
			t.Errorf("faltou erro em %s: %v", campo, f)
		}
	}
	// A regra do estado é do domínio: concluída aceita só o custo.
	if err := maintenance.CheckEdit(maintenance.Done, so.edicao()); err != nil {
		t.Fatalf("concluída aceita apagar o custo: %v", err)
	}
}

func TestConclusaoRecusaCustoNulo(t *testing.T) {
	if f := (ConclusaoDaOrdem{}).Validar(); len(f) != 0 {
		t.Fatalf("sem corpo conclui: %v", f)
	}
	if f := (ConclusaoDaOrdem{CustoCents: httpx.Nulo[int64]()}).Validar(); f["cost_cents"] == "" {
		t.Fatal("`cost_cents: null` na conclusão é 422: o schema declara inteiro")
	}
	if f := (ConclusaoDaOrdem{CustoCents: httpx.De[int64](0)}).Validar(); f["cost_cents"] == "" {
		t.Fatal("custo zero é 422")
	}
}

func TestTraduzirRegraUsaOCatalogo(t *testing.T) {
	// VALIDATION_ERROR do período aninhado ganha o prefixo.
	hoje := calendar.MustParse("2026-10-09")
	_, err := maintenance.Replan(nil, maintenance.Period{From: hoje.AddDays(-1), To: hoje.AddDays(1)}, hoje)
	var e *apperr.Error
	if !errors.As(traduzirRegra(err, "block.", nil), &e) || e.Code != apperr.CodeValidationError || e.Status() != http.StatusUnprocessableEntity {
		t.Fatalf("período: %v", e)
	}
	if d, _ := e.Details.(map[string]any); d["block.from"] == nil || d["from"] != nil {
		t.Fatalf("details aninhados: %v", e.Details)
	}

	// Encerrada: 409 do catálogo, com o closed_at da ordem.
	fechada := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	_, err = maintenance.Next(maintenance.Done, maintenance.Cancel)
	if !errors.As(traduzirRegra(err, "", &ordemGravada{FechadaEm: &fechada}), &e) ||
		e.Code != apperr.CodeMaintenanceOrderClosed || e.Status() != http.StatusConflict {
		t.Fatalf("encerrada: %v", e)
	}
	if d, _ := e.Details.(map[string]any); d["closed_at"] != fechada || d["editable"] != "so_custo" {
		t.Fatalf("details da encerrada: %v", e.Details)
	}

	// Transição fora da máquina: 409 com as ações que restam.
	_, err = maintenance.Next(maintenance.InProgress, maintenance.Start)
	if !errors.As(traduzirRegra(err, "", nil), &e) || e.Code != apperr.CodeInvalidStateTransition {
		t.Fatalf("transição: %v", e)
	}

	// Erro que não é regra (programação) é 500.
	if !errors.As(traduzirRegra(errors.New("x"), "", nil), &e) || e.Status() != http.StatusInternalServerError {
		t.Fatalf("erro comum: %v", e)
	}
}

func TestBloqueioDaRespostaTemAFaseDeHoje(t *testing.T) {
	id := uuid.New()
	for _, c := range []struct {
		de, ate, status, fase string
	}{
		{"2026-10-10", "2026-10-12", "confirmed", "agendado"},
		{"2026-10-09", "2026-10-12", "confirmed", "em_curso"},
		{"2026-10-01", "2026-10-09", "confirmed", "encerrado"},
		{"2026-10-10", "2026-10-12", "cancelled", "liberado"},
	} {
		b, err := bloqueioDaResposta(id, c.de, c.ate, c.status, "2026-10-09")
		if err != nil || b.Fase != c.fase {
			t.Errorf("[%s, %s) %s: fase %q (%v), esperado %q", c.de, c.ate, c.status, b.Fase, err, c.fase)
		}
	}
	if b, _ := bloqueioDaResposta(id, "2026-10-10", "2026-10-15", "confirmed", "2026-10-09"); b.Noites != 5 {
		t.Fatalf("noites de [10, 15): %d", b.Noites)
	}
}

func TestFiltroDaListaRecusaVocabularioDesconhecido(t *testing.T) {
	ler := func(q string) (FiltroDeOrdens, error) {
		return filtroDaQuery(httptest.NewRequest(http.MethodGet, "/maintenance-orders?"+q, nil))
	}
	f, err := ler("")
	if err != nil || f.Ordem != OrdemPadrao || f.Pagina != 1 || f.PorPagina != 25 {
		t.Fatalf("padrões: %+v %v", f, err)
	}
	f, err = ler("open=true&status=em_andamento&priority=urgente&sort=-closed_at&issue_id=" + uuid.NewString())
	if err != nil || f.Aberta == nil || !*f.Aberta || f.AvariaID == nil || f.Ordem != "-closed_at" {
		t.Fatalf("filtros: %+v %v", f, err)
	}
	if f, err := ler("open=false"); err != nil || f.Aberta == nil || *f.Aberta {
		t.Fatalf("open=false: %+v %v", f, err)
	}
	for _, q := range []string{"sort=prioridade", "status=fechada", "priority=altissima", "unit_id=1", "open=sim", "open=abertas",
		"q=" + strings.Repeat("a", 201)} {
		if _, err := ler(q); err == nil {
			t.Errorf("%s deveria ser 422", q)
		}
	}
}

func TestEscaparLike(t *testing.T) {
	if got := escaparLike(`100%_\`); got != `100\%\_\\` {
		t.Fatalf("escaparLike: %q", got)
	}
}

func ptr[T any](v T) *T { return &v }
