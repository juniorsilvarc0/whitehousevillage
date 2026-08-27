package disponibilidade

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/money"
)

// chamar executa o handler já com identidade no contexto — é o que o
// middleware de autenticação faria antes dele.
func chamar(h http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	ctx, _ := contextoDe(auth.EscopoAll)
	rec := httptest.NewRecorder()
	h(rec, req.WithContext(ctx))
	return rec
}

func TestHandlerDevolveEnvelopeComData(t *testing.T) {
	completa := uuid.New()
	repo := &repoFalso{
		contexto: Contexto{RateTableID: uuid.New(), Politica: politicaV1()},
		cal:      calendarioV1(),
		minimos:  minimosV1(),
		produtos: []Produto{{ID: completa, Codigo: "completa", Nome: "White House Completa",
			Capacidade: 24, Consome: ConsomeTodas, LimpezaCent: money.FromReais(900)}},
		tarifas: map[uuid.UUID]map[calendar.DateType]money.Cents{
			completa: {calendar.Weekend: money.FromReais(6900), calendar.Normal: money.FromReais(5500)},
		},
		ocupacao: map[ChaveDia]Contagem{
			{Produto: completa, Dia: "2026-11-20"}: {Declaradas: 8, Ativas: 8, Ocupadas: 0},
		},
	}
	h := NovoHandlerCom(NovoServico(repo, nil))

	rec := chamar(h.PorProduto, httptest.NewRequest(http.MethodGet, "/availability?from=2026-11-20&to=2026-11-21", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	var env struct {
		Data []DisponibilidadeDoProduto `json:"data"`
		Meta *json.RawMessage           `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("corpo ilegível: %v — %s", err, rec.Body)
	}
	if len(env.Data) != 1 || len(env.Data[0].Dias) != 1 {
		t.Fatalf("data = %+v", env.Data)
	}
	// Sem `meta`: disponibilidade não é coleção paginável.
	if env.Meta != nil {
		t.Errorf("a resposta trouxe meta: %s", rec.Body)
	}
}

// Coleção vazia sai como `[]`, nunca `null` — o `.map()` do painel estoura
// justamente no caso mais comum, a janela sem nada.
func TestHandlerNuncaDevolveDataNula(t *testing.T) {
	repo := &repoFalso{contexto: Contexto{RateTableID: uuid.New(), Politica: politicaV1()}, cal: calendarioV1()}
	h := NovoHandlerCom(NovoServico(repo, nil))

	for nome, rec := range map[string]*httptest.ResponseRecorder{
		"por produto": chamar(h.PorProduto, httptest.NewRequest(http.MethodGet, "/availability?from=2026-11-20&to=2026-11-21", nil)),
		"por unidade": chamar(h.PorUnidade, httptest.NewRequest(http.MethodGet, "/availability/units?from=2026-11-20&to=2026-11-21", nil)),
	} {
		if !strings.Contains(rec.Body.String(), `"data":[]`) {
			t.Errorf("%s: corpo = %s", nome, rec.Body)
		}
	}
}

func TestHandlerRecusaJanelaInvalidaCom422(t *testing.T) {
	h := NovoHandlerCom(NovoServico(&repoFalso{}, nil))

	rec := chamar(h.PorUnidade, httptest.NewRequest(http.MethodGet, "/availability/units?from=2026-11-23&to=2026-11-20", nil))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"VALIDATION_ERROR"`) {
		t.Errorf("corpo = %s", rec.Body)
	}
}

// POST /quotes responde 200: nada foi criado. 201 faria o painel procurar um
// recurso que não existe.
func TestOrcarResponde200ENaoGravaNada(t *testing.T) {
	cobertura := uuid.New()
	repo := &repoFalso{
		contexto: Contexto{RateTableID: uuid.New(), Politica: politicaV1()},
		cal:      calendarioV1(),
		minimos:  minimosV1(),
		produtos: []Produto{{ID: cobertura, Codigo: "cobertura", Nome: "White House Cobertura",
			Capacidade: 10, Consome: ConsomeUma, LimpezaCent: money.FromReais(350)}},
		tarifas: map[uuid.UUID]map[calendar.DateType]money.Cents{
			cobertura: {calendar.Weekend: money.FromReais(2400), calendar.Normal: money.FromReais(1900)},
		},
	}
	h := NovoHandlerCom(NovoServico(repo, nil))

	// `guests_count`, e não `guests`: é o nome que o `openapi.yaml` declara e o
	// que o painel manda. Enquanto o DTO ler `guests`, este teste fica vermelho
	// — ver o bloco ⚠ em `dto_test.go`.
	corpo := `{"unit_type_id":"` + cobertura.String() + `","check_in":"2026-11-20","check_out":"2026-11-23","guests_count":8}`
	req := httptest.NewRequest(http.MethodPost, "/quotes", strings.NewReader(corpo))
	req.Header.Set("Content-Type", "application/json")

	rec := chamar(h.Orcar, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	var env struct {
		Data Orcamento `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("corpo ilegível: %v — %s", err, rec.Body)
	}
	if env.Data.Total != 705000 || env.Data.Sinal != 352500 {
		t.Errorf("total = %s, sinal = %s", money.Cents(env.Data.Total), money.Cents(env.Data.Sinal))
	}
	// Nenhuma reserva nem bloco: o handler não tem por onde gravar, e é isso
	// que o contrato promete ("não grava nada, não segura a data").
	if rec.Header().Get("Location") != "" {
		t.Error("orçamento não cria recurso — não pode devolver Location")
	}
}
