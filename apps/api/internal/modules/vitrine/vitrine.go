// Package vitrine é a superfície PÚBLICA da API: o que o site de vendas
// (apps/site) pergunta sem sessão nenhuma — catálogo, política exibida ao
// hóspede, calendário de livre/ocupado e orçamento.
//
// A regra do docs/unificacao-site-crm.md §3 é a razão de este pacote existir e
// de ele ser fino: o site pergunta, não calcula. Nenhuma conta mora aqui. As
// respostas saem do MESMO motor e das MESMAS consultas que o painel usa
// (módulo disponibilidade, domínio booking) — trocar uma tarifa no painel muda
// o preço do site sem editar arquivo nenhum, que é o critério de pronto do A2.
//
// O que este pacote faz é RECORTAR. As três invariantes do §5 do plano:
//
//  1. Resposta pública nunca carrega dado de terceiro nem de operação: dia
//     ocupado é só `available: false` — nem quem, nem quanto, nem por quê, nem
//     quantas unidades sobram (a contagem revelaria a ocupação da casa).
//  2. A defesa contra overbooking continua sendo o banco: nada aqui grava.
//  3. Toda rota é limitada por taxa (montagem em internal/router).
//
// E uma regra comercial: o público não negocia. O pedido de orçamento não tem
// campo de desconto — mandar `discount_pct` é campo desconhecido, recusado
// pelo decoder — e a resposta não carrega alçada, versão de política nem id de
// tabela, que são vocabulário interno de negociação.
package vitrine

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/disponibilidade"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

const (
	// janelaPublicaMaxDias é o maior recorte de calendário num pedido. O site
	// mostra dois meses por vez; três dão folga sem transformar a rota numa
	// exportação da ocupação do ano.
	janelaPublicaMaxDias = 93

	// horizontePublicoDias é até onde o calendário público olha. Mais longe
	// que isso a casa não tem tarifa publicada, e o calendário só diria
	// "indisponível" — pior que não mostrar.
	horizontePublicoDias = 548

	// passadoPublicoDias deixa o site desenhar o mês corrente inteiro (os dias
	// que já passaram aparecem apagados). Antes disso não há o que vender.
	passadoPublicoDias = 31
)

// casas resolve a casa que a vitrine vende.
//
// O sistema não é multi-tenant (CLAUDE.md): há UMA propriedade ativa. Mais de
// uma é configuração que a vitrine não sabe interpretar — e escolher uma ao
// acaso venderia a casa errada —, então a resposta é erro, não palpite.
type casas struct {
	pool *pgxpool.Pool

	mu   sync.Mutex
	casa uuid.UUID
}

func (c *casas) resolver(ctx context.Context) (uuid.UUID, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.casa != uuid.Nil {
		return c.casa, nil
	}
	linhas, err := c.pool.Query(ctx, `SELECT id FROM properties WHERE active ORDER BY created_at LIMIT 2`)
	if err != nil {
		return uuid.Nil, db.MapError(err)
	}
	ids, err := pgx.CollectRows(linhas, pgx.RowTo[uuid.UUID])
	if err != nil {
		return uuid.Nil, db.MapError(err)
	}
	if len(ids) != 1 {
		return uuid.Nil, apperr.Internal.WithCause(errors.New("vitrine: era esperada exatamente uma propriedade ativa"))
	}
	c.casa = ids[0]
	return c.casa, nil
}

// Handler serve as rotas /public/*.
type Handler struct {
	casas *casas
	disp  *disponibilidade.Servico
}

// NovoHandler segue o construtor combinado dos módulos: (pool, tx).
func NovoHandler(pool *pgxpool.Pool, tx *db.TxManager) *Handler {
	return &Handler{
		casas: &casas{pool: pool},
		disp:  disponibilidade.NovoServico(disponibilidade.NewRepository(pool), tx),
	}
}

// contexto marca a requisição com a casa da vitrine. É o único ponto que
// escreve essa marca — ver disponibilidade.ComCasaPublica.
func (h *Handler) contexto(r *http.Request) (context.Context, error) {
	casa, err := h.casas.resolver(r.Context())
	if err != nil {
		return nil, err
	}
	return disponibilidade.ComCasaPublica(r.Context(), casa), nil
}

// ─────────────────────────── GET /public/products ───────────────────────────

// ProdutoPublico é o card de uma acomodação. Só vitrine: nada de custo, dono,
// composição interna ou unidade física.
type ProdutoPublico struct {
	ID       uuid.UUID `json:"unit_type_id"`
	Codigo   string    `json:"code"`
	Nome     string    `json:"name"`
	Lotacao  int       `json:"capacity"`
	Limpeza  int64     `json:"cleaning_cents"`
	APartir  *int64    `json:"from_price_cents"`
	Tarifas  []Tarifa  `json:"rates"`
	Pacotes  []Pacote  `json:"packages"`
	Exclusiv bool      `json:"exclusive"`
	// SobConsulta: o produto não tem tarifa publicada nenhuma (a Completa). O
	// site mostra "sob consulta" e leva para o WhatsApp, em vez de um preço.
	SobConsulta bool `json:"on_request"`
}

// Pacote é o preço fechado de N diárias consecutivas nos tipos de data
// listados — o "2 diárias por R$ 6.500" da Grand Villa.
type Pacote struct {
	Noites int                 `json:"nights"`
	Tipos  []calendar.DateType `json:"date_types"`
	Total  int64               `json:"total_cents"`
}

// Tarifa é a diária de um tipo de data, com o mínimo de noites dele.
type Tarifa struct {
	Tipo      calendar.DateType `json:"date_type"`
	Preco     int64             `json:"price_cents"`
	MinNoites int               `json:"min_nights"`
}

// ordemDosTipos é a ordem da tabela de tarifas: do mais comum ao mais raro.
var ordemDosTipos = []calendar.DateType{
	calendar.Normal, calendar.Weekend, calendar.Holiday,
	calendar.HighSeason, calendar.NewYear, calendar.Carnival,
}

// Produtos — GET /public/products
func (h *Handler) Produtos(w http.ResponseWriter, r *http.Request) {
	ctx, err := h.contexto(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	catalogo, _, err := h.disp.Catalogo(ctx)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out := make([]ProdutoPublico, 0, len(catalogo))
	for _, p := range catalogo {
		item := ProdutoPublico{
			ID: p.ID, Codigo: p.Codigo, Nome: p.NomePublico, Lotacao: p.Capacidade,
			Limpeza:  int64(p.LimpezaCent),
			Tarifas:  []Tarifa{},
			Pacotes:  []Pacote{},
			Exclusiv: p.Consome == disponibilidade.ConsomeTodas,
		}
		for _, tipo := range ordemDosTipos {
			valor, ok := p.Tarifas[tipo]
			if !ok {
				continue
			}
			centavos := int64(valor)
			item.Tarifas = append(item.Tarifas, Tarifa{Tipo: tipo, Preco: centavos, MinNoites: p.MinNoites[tipo]})
			if item.APartir == nil || centavos < *item.APartir {
				item.APartir = &centavos
			}
		}
		for _, pk := range p.Pacotes {
			item.Pacotes = append(item.Pacotes, Pacote{Noites: pk.Nights, Tipos: pk.Types, Total: int64(pk.Total)})
		}
		item.SobConsulta = len(item.Tarifas) == 0
		out = append(out, item)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// ─────────────────────────── GET /public/policy ─────────────────────────────

// PoliticaPublica é o que o hóspede precisa saber antes de escolher a data.
// Nada da alçada de desconto: isso é regra de negociação interna.
type PoliticaPublica struct {
	Hoje          string  `json:"today"`
	SinalPct      float64 `json:"deposit_pct"`
	SaldoDias     int     `json:"balance_due_days"`
	PreReservaHrs int     `json:"hold_hours"`
}

// Politica — GET /public/policy
func (h *Handler) Politica(w http.ResponseWriter, r *http.Request) {
	ctx, err := h.contexto(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	_, politica, err := h.disp.Catalogo(ctx)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	hoje, err := h.disp.Hoje(ctx)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, PoliticaPublica{
		Hoje: hoje.String(), SinalPct: politica.DepositPct,
		SaldoDias: politica.BalanceDueDays, PreReservaHrs: politica.HoldHours,
	})
}

// ─────────────────────────── GET /public/availability ───────────────────────

// DiaPublico é uma célula do calendário público. `available` é booleano de
// propósito: o painel recebe quantas unidades sobram, o público não — a
// contagem diária somada no ano é a taxa de ocupação da casa.
//
// O preço só sai em dia vendável. Dia indisponível não tem preço a mostrar, e
// distinguir "ocupado" de "sem tarifa" seria revelar o motivo.
type DiaPublico struct {
	Data      string            `json:"date"`
	Livre     bool              `json:"available"`
	Tipo      calendar.DateType `json:"date_type"`
	Preco     *int64            `json:"price_cents"`
	MinNoites int               `json:"min_nights"`
	// SobConsulta: o dia não está à venda pelo site porque não tem tarifa
	// publicada (Réveillon da Grand Villa, qualquer dia da Completa) — não
	// porque alguém ocupou. O site manda para o WhatsApp em vez de dizer
	// "indisponível". É regra comercial da casa, não dado de terceiro.
	SobConsulta bool `json:"on_request"`
}

// Disponibilidade — GET /public/availability?unit_type_id=&from=&to=
func (h *Handler) Disponibilidade(w http.ResponseWriter, r *http.Request) {
	ctx, err := h.contexto(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	produto, err := disponibilidade.UUIDOpcionalDaQuery(r, "unit_type_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if produto == nil {
		httpx.Error(w, r, apperr.Validation(map[string]string{"unit_type_id": "é obrigatório."}))
		return
	}
	janela, err := disponibilidade.JanelaDaRequisicao(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	hoje, err := h.disp.Hoje(ctx)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := recortePublico(janela, hoje); err != nil {
		httpx.Error(w, r, err)
		return
	}

	linhas, err := h.disp.PorProduto(ctx, janela, produto)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out := []DiaPublico{}
	for _, linha := range linhas {
		for _, d := range linha.Dias {
			dia := DiaPublico{Data: d.Data, Livre: d.Disponivel > 0, Tipo: d.TipoDeData, MinNoites: d.MinNoites}
			if dia.Livre {
				dia.Preco = d.Preco
			}
			dia.SobConsulta = !dia.Livre && d.Motivo != nil && *d.Motivo == disponibilidade.MotivoSemTarifa
			out = append(out, dia)
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

// recortePublico limita a janela que o público pode pedir: curta, e perto de
// hoje. A rota interna aceita até um ano por pedido; aqui, quem pede é anônimo.
func recortePublico(j disponibilidade.Janela, hoje calendar.Date) error {
	if dias := j.De.Nights(j.Ate); dias > janelaPublicaMaxDias {
		return apperr.Validation(map[string]string{"to": "a janela pública é de no máximo 93 dias."})
	}
	if j.De.Before(hoje.AddDays(-passadoPublicoDias)) {
		return apperr.Validation(map[string]string{"from": "fora do calendário público."})
	}
	if j.Ate.After(hoje.AddDays(horizontePublicoDias)) {
		return apperr.Validation(map[string]string{"to": "fora do calendário público."})
	}
	return nil
}

// ─────────────────────────── POST /public/quotes ────────────────────────────

// PedidoPublico é o orçamento que o visitante pode pedir. Sem desconto, sem
// tabela nem versão de política, sem persistir, sem contato: o decoder recusa
// qualquer campo a mais, então `discount_pct` responde 422 em vez de ser
// ignorado em silêncio.
type PedidoPublico struct {
	UnitTypeID uuid.UUID `json:"unit_type_id" validate:"required"`
	CheckIn    string    `json:"check_in" validate:"required"`
	CheckOut   string    `json:"check_out" validate:"required"`
	Hospedes   int       `json:"guests_count" validate:"required,min=1"`
}

// OrcamentoPublico é o orçamento sem o vocabulário de negociação.
type OrcamentoPublico struct {
	Noites   int                                `json:"night_count"`
	Subtotal int64                              `json:"subtotal_cents"`
	Limpeza  int64                              `json:"cleaning_cents"`
	Total    int64                              `json:"total_cents"`
	Sinal    int64                              `json:"deposit_cents"`
	Saldo    int64                              `json:"balance_cents"`
	Media    int64                              `json:"avg_nightly_cents"`
	Minimo   int                                `json:"min_nights"`
	Linhas   []disponibilidade.LinhaDoOrcamento `json:"lines"`
	Diarias  []disponibilidade.NoiteDoOrcamento `json:"nights"`
}

// Orcar — POST /public/quotes
//
// 200, não 201: nada é gravado. Orçamento público não segura data — a
// pré-reserva pública (B1 do plano) depende de decisão do dono do negócio.
func (h *Handler) Orcar(w http.ResponseWriter, r *http.Request) {
	ctx, err := h.contexto(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[PedidoPublico](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	entrada, err := disponibilidade.Pedido{
		UnitTypeID: corpo.UnitTypeID,
		CheckIn:    corpo.CheckIn,
		CheckOut:   corpo.CheckOut,
		Hospedes:   corpo.Hospedes,
	}.Normalizar()
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	o, err := h.disp.Orcar(ctx, entrada)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, OrcamentoPublico{
		Noites: o.Noites, Subtotal: o.Subtotal, Limpeza: o.Limpeza, Total: o.Total,
		Sinal: o.Sinal, Saldo: o.Saldo, Media: o.MediaPorNoite, Minimo: o.MinNoites,
		Linhas: o.Linhas, Diarias: o.Diarias,
	})
}
