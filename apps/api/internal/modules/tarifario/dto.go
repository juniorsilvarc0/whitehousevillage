// Package tarifario implementa o tarifário e o calendário comercial: tabelas de
// tarifas versionadas por vigência, a grade produto × tipo de data, feriados,
// períodos especiais, mínimo de noites e as duas políticas versionadas
// (comercial e de cancelamento).
//
// A regra que atravessa o módulo inteiro: NADA aqui reescreve o passado. Tarifa
// é o preço do PRÓXIMO orçamento; o preço já vendido está congelado em
// `reservation_nights` e em `reservation_pricing` (CLAUDE.md regra 7). Política
// não se edita — cada `PUT` publica uma VERSÃO nova, porque as reservas emitidas
// apontam para a versão que estava valendo quando foram criadas.
package tarifario

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/booking"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// RecursoConfiguracoes é o código do recurso RBAC que protege o tarifário
// inteiro — o mesmo `x-rbac: { recurso: settings }` do contrato e o mesmo código
// semeado em `cmd/seed/acesso.go`.
//
// Mora aqui, e não em `internal/auth/recursos.go`, porque aquele arquivo é
// compartilhado e três módulos estão sendo escritos em paralelo nesta rodada.
// Quando a poeira baixar, o lugar dele é lá, junto de RecursoUsuarios e
// RecursoPerfis.
const RecursoConfiguracoes = "settings"

// ─────────────────────────── Data civil ─────────────────────────────────────

// Data é a data civil do contrato (`YYYY-MM-DD`) com serialização JSON.
//
// Embrulha `calendar.Date` em vez de usar `time.Time` pela razão que o próprio
// pacote calendar documenta: "21 de agosto" é o mesmo dia em qualquer fuso, e
// carregar hora junto é a origem clássica da data que pula um dia. O tipo mora
// aqui e não no domínio porque `internal/domain` é puro — JSON é detalhe de
// transporte.
type Data struct {
	calendar.Date
}

// DataDe converte o que o pgx devolve para uma coluna `date`.
func DataDe(t time.Time) Data {
	return Data{calendar.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}}
}

// Tempo é o que o pgx aceita como parâmetro de coluna `date`. Meia-noite UTC
// porque a coluna não guarda hora: qualquer outro horário só criaria a chance de
// o driver arredondar para o dia vizinho.
func (d Data) Tempo() time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// Definida diz se a data foi realmente preenchida (o zero value não é uma data).
func (d Data) Definida() bool { return d.Year != 0 }

func (d Data) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d *Data) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("data deve ser texto no formato YYYY-MM-DD")
	}
	parsed, err := calendar.Parse(strings.TrimSpace(s))
	if err != nil {
		return fmt.Errorf("data inválida: use YYYY-MM-DD")
	}
	d.Date = parsed
	return nil
}

// ponteiroDeTempo traduz a data opcional para o parâmetro do pgx.
func ponteiroDeTempo(d *Data) *time.Time {
	if d == nil {
		return nil
	}
	t := d.Tempo()
	return &t
}

// dataDePonteiro traduz a coluna anulável de volta para o DTO.
func dataDePonteiro(t *time.Time) *Data {
	if t == nil {
		return nil
	}
	d := DataDe(*t)
	return &d
}

// ─────────────────────────── Vocabulários ───────────────────────────────────

// tagTipoDeData é o enum `TipoDeData` do contrato, na forma que o validator
// entende. Os seis valores são os mesmos do CHECK de `date_type_rules` — a
// precedência entre eles é DADO (spec §3), e por isso não aparece em lugar
// nenhum deste pacote.
const tagTipoDeData = "oneof=normal fds feriado alta reveillon carnaval"

// tagEspecie é o enum de `special_periods.kind`.
const tagEspecie = "oneof=reveillon carnaval alta evento"

// TipoDeDataValido confere o vocabulário fechado das noites.
func TipoDeDataValido(t string) bool {
	return httpx.ValidarValor(t, tagTipoDeData)
}

// ─────────────────────────── Tabela de tarifas ──────────────────────────────

// TabelaDeTarifas é o schema `TabelaDeTarifas`: a tabela versionada por
// vigência. A vigente numa data é a de maior `valid_from` que cobre a data e
// está ativa; `valid_to` nulo é "sem fim".
type TabelaDeTarifas struct {
	ID        uuid.UUID `json:"id"`
	Nome      string    `json:"name"`
	ValidoDe  Data      `json:"valid_from"`
	ValidoAte *Data     `json:"valid_to"`
	Ativa     bool      `json:"active"`
	CriadaEm  time.Time `json:"created_at"`
}

// TabelaEntrada é o corpo de POST e PUT — o contrato usa o MESMO schema nos
// dois, então a substituição integral já vem coberta pelos `required`.
type TabelaEntrada struct {
	Nome      string          `json:"name" validate:"required,min=2,max=120"`
	ValidoDe  *Data           `json:"valid_from" validate:"required"`
	ValidoAte *Data           `json:"valid_to"`
	Ativa     httpx.Opt[bool] `json:"active"`
}

func (e *TabelaEntrada) Normalizar() { e.Nome = strings.TrimSpace(e.Nome) }

func (e TabelaEntrada) Validar() map[string]string {
	erros := map[string]string{}
	if e.ValidoDe != nil && e.ValidoAte != nil && e.ValidoAte.Before(e.ValidoDe.Date) {
		erros["valid_to"] = "não pode ser anterior a valid_from."
	}
	return erros
}

// TabelaAtualizar é o corpo do PATCH.
type TabelaAtualizar struct {
	Nome      httpx.Opt[string] `json:"name"`
	ValidoDe  httpx.Opt[Data]   `json:"valid_from"`
	ValidoAte httpx.Opt[Data]   `json:"valid_to"`
	Ativa     httpx.Opt[bool]   `json:"active"`
}

func (a *TabelaAtualizar) Normalizar() {
	if v, ok := a.Nome.Definido(); ok {
		a.Nome = httpx.De(strings.TrimSpace(v))
	}
}

// Validar cobre só o que dá para julgar sem ler o banco. O cruzamento
// `valid_to >= valid_from` fica no service: num PATCH que manda só uma das duas
// pontas, a outra vem da linha gravada, e validar aqui julgaria metade do fato.
func (a TabelaAtualizar) Validar() map[string]string {
	erros := map[string]string{}
	naoNulo(erros, "name", a.Nome.DeveLimpar())
	naoNulo(erros, "valid_from", a.ValidoDe.DeveLimpar())
	naoNulo(erros, "active", a.Ativa.DeveLimpar())

	if v, ok := a.Nome.Definido(); ok && !httpx.ValidarValor(v, "min=2,max=120") {
		erros["name"] = "deve ter entre 2 e 120 caracteres."
	}
	return erros
}

// ─────────────────────────── Tarifa ─────────────────────────────────────────

// Tarifa é uma célula da grade produto × tipo de data.
type Tarifa struct {
	ID            uuid.UUID `json:"id"`
	TabelaID      uuid.UUID `json:"rate_table_id"`
	ProdutoID     uuid.UUID `json:"unit_type_id"`
	ProdutoCodigo string    `json:"unit_type_code"`
	TipoDeData    string    `json:"date_type"`
	ValorCents    int64     `json:"amount_cents"`
}

// TarifaEntrada é o corpo de POST e PUT /rates/{id}.
type TarifaEntrada struct {
	TabelaID   uuid.UUID `json:"rate_table_id" validate:"required"`
	ProdutoID  uuid.UUID `json:"unit_type_id" validate:"required"`
	TipoDeData string    `json:"date_type" validate:"required,oneof=normal fds feriado alta reveillon carnaval"`
	ValorCents int64     `json:"amount_cents" validate:"required,gt=0"`
}

// TarifaAtualizar é o corpo do PATCH /rates/{id}.
type TarifaAtualizar struct {
	ValorCents httpx.Opt[int64]     `json:"amount_cents"`
	TipoDeData httpx.Opt[string]    `json:"date_type"`
	ProdutoID  httpx.Opt[uuid.UUID] `json:"unit_type_id"`
}

func (a TarifaAtualizar) Validar() map[string]string {
	erros := map[string]string{}
	naoNulo(erros, "amount_cents", a.ValorCents.DeveLimpar())
	naoNulo(erros, "date_type", a.TipoDeData.DeveLimpar())
	naoNulo(erros, "unit_type_id", a.ProdutoID.DeveLimpar())

	if v, ok := a.ValorCents.Definido(); ok && v <= 0 {
		erros["amount_cents"] = "deve ser maior que zero."
	}
	if v, ok := a.TipoDeData.Definido(); ok && !TipoDeDataValido(v) {
		erros["date_type"] = "deve ser um de: normal, fds, feriado, alta, reveillon, carnaval."
	}
	return erros
}

// CelulaDaGrade é uma célula no corpo de POST /rates/bulk. A tabela vem uma vez
// só, no nível de cima.
//
// Sem tags de validação de propósito: o validator só desce em elemento de slice
// com `dive`, e o `dive` perde o índice no caminho do erro. Quem valida célula a
// célula é GradeEntrada.Validar(), que devolve `rates[3].amount_cents`.
type CelulaDaGrade struct {
	ProdutoID  uuid.UUID `json:"unit_type_id"`
	TipoDeData string    `json:"date_type"`
	ValorCents int64     `json:"amount_cents"`
}

// GradeEntrada é o corpo de POST /rates/bulk.
//
// Duas coisas, e a distinção é o coração da rota: `unit_type_ids` é o ESCOPO
// (quais produtos esta chamada reescreve) e `rates` é o CONTEÚDO (as células que
// ficam). Dentro do escopo a semântica é de substituição: o par
// `(unit_type_id, date_type)` que não vier aqui é removido. Fora do escopo nada
// é tocado.
//
// A versão anterior não tinha escopo e o raio de ação era a tabela inteira — o
// BAIXO 10 desta revisão: salvar 6 células de um produto apagou as 18 dos outros
// três, e a venda deles passou a responder RATE_NOT_FOUND.
type GradeEntrada struct {
	TabelaID uuid.UUID `json:"rate_table_id" validate:"required"`

	// ProdutoIDs distingue AUSENTE de VAZIO, e a distinção é carregada pelo
	// `nil` do slice — que é exatamente o que o encoding/json produz para campo
	// ausente e para `null`, e o que ele nunca produz para `[]`.
	//
	//   ausente/null → o escopo é deduzido dos produtos que aparecem em `rates`
	//                  (o caminho comum da tela, que salva o que editou);
	//   `[]`         → escopo vazio: a chamada não faz nada.
	//
	// Quem for mexer aqui: teste com `EscopoDeclarado()`, nunca com
	// `len(ProdutoIDs) == 0` — as duas situações têm significados diferentes.
	ProdutoIDs []uuid.UUID `json:"unit_type_ids"`

	// Celulas é opcional: produto citado em `unit_type_ids` SEM nenhuma célula
	// aqui é zerado. É deliberado que apagar exija nomear o alvo — remoção nunca
	// deve ser consequência de omissão.
	Celulas []CelulaDaGrade `json:"rates"`
}

// EscopoDeclarado diz se o cliente mandou a lista de produtos.
func (g GradeEntrada) EscopoDeclarado() bool { return g.ProdutoIDs != nil }

// Escopo devolve os produtos que esta chamada reescreve, sem repetição e em
// ordem estável (a de chegada), para o `meta` da resposta ser conferível.
func (g GradeEntrada) Escopo() []uuid.UUID {
	vistos := map[uuid.UUID]bool{}
	escopo := []uuid.UUID{}

	acrescentar := func(id uuid.UUID) {
		if id == uuid.Nil || vistos[id] {
			return
		}
		vistos[id] = true
		escopo = append(escopo, id)
	}

	if g.EscopoDeclarado() {
		for _, id := range g.ProdutoIDs {
			acrescentar(id)
		}
		return escopo
	}
	for _, c := range g.Celulas {
		acrescentar(c.ProdutoID)
	}
	return escopo
}

// Validar recusa o par repetido e a célula fora do escopo ANTES de a transação
// abrir.
//
// Sem a checagem do par repetido, os dois valores do mesmo par entrariam no
// mesmo INSERT, o `UNIQUE (rate_table_id, unit_type_id, date_type)` estouraria
// no meio da transação e a tela receberia um erro de constraint em vez de saber
// QUAL linha da grade ela duplicou.
//
// Sem a checagem do escopo, mandar preço de um produto que não está em
// `unit_type_ids` ampliaria o escopo em silêncio — e o escopo é justamente o que
// decide o que vai ser APAGADO. Cliente confuso sobre o raio de ação da própria
// chamada é o que produziu o BAIXO 10; aqui isso é erro de cliente, não licença.
func (g GradeEntrada) Validar() map[string]string {
	erros := map[string]string{}
	vistos := map[string]int{}

	declarados := map[uuid.UUID]bool{}
	for i, id := range g.ProdutoIDs {
		if id == uuid.Nil {
			erros[fmt.Sprintf("unit_type_ids[%d]", i)] = "é obrigatório."
			continue
		}
		declarados[id] = true
	}

	for i, c := range g.Celulas {
		// As células são validadas aqui, e não por tag `dive`: o caminho do erro
		// sai como `rates[3].amount_cents`, que é o que a grade da tela precisa
		// para pintar a célula errada.
		if c.ProdutoID == uuid.Nil {
			erros[fmt.Sprintf("rates[%d].unit_type_id", i)] = "é obrigatório."
		}
		if !TipoDeDataValido(c.TipoDeData) {
			erros[fmt.Sprintf("rates[%d].date_type", i)] = "deve ser um de: normal, fds, feriado, alta, reveillon, carnaval."
		}
		if c.ValorCents <= 0 {
			erros[fmt.Sprintf("rates[%d].amount_cents", i)] = "deve ser maior que zero."
		}
		if g.EscopoDeclarado() && c.ProdutoID != uuid.Nil && !declarados[c.ProdutoID] {
			erros[fmt.Sprintf("rates[%d].unit_type_id", i)] =
				"não está em unit_type_ids — cite o produto no escopo ou remova a célula."
		}

		chave := c.ProdutoID.String() + ":" + c.TipoDeData
		if anterior, repetido := vistos[chave]; repetido {
			erros["rates"] = fmt.Sprintf(
				"par (unit_type_id, date_type) repetido nos índices %d e %d.", anterior, i)
			return erros
		}
		vistos[chave] = i
	}
	return erros
}

// ─────────────────────────── Feriado ────────────────────────────────────────

// Feriado é uma data que classifica a noite como `feriado`. Inativo deixa de
// classificar — some do cálculo seguinte sem apagar o histórico.
type Feriado struct {
	ID    uuid.UUID `json:"id"`
	Data  Data      `json:"date"`
	Nome  string    `json:"name"`
	Ativo bool      `json:"active"`
}

type FeriadoEntrada struct {
	Data  *Data           `json:"date" validate:"required"`
	Nome  string          `json:"name" validate:"required,min=2,max=120"`
	Ativo httpx.Opt[bool] `json:"active"`
}

func (e *FeriadoEntrada) Normalizar() { e.Nome = strings.TrimSpace(e.Nome) }

type FeriadoAtualizar struct {
	Data  httpx.Opt[Data]   `json:"date"`
	Nome  httpx.Opt[string] `json:"name"`
	Ativo httpx.Opt[bool]   `json:"active"`
}

func (a *FeriadoAtualizar) Normalizar() {
	if v, ok := a.Nome.Definido(); ok {
		a.Nome = httpx.De(strings.TrimSpace(v))
	}
}

func (a FeriadoAtualizar) Validar() map[string]string {
	erros := map[string]string{}
	naoNulo(erros, "date", a.Data.DeveLimpar())
	naoNulo(erros, "name", a.Nome.DeveLimpar())
	naoNulo(erros, "active", a.Ativo.DeveLimpar())

	if v, ok := a.Nome.Definido(); ok && !httpx.ValidarValor(v, "min=2,max=120") {
		erros["name"] = "deve ter entre 2 e 120 caracteres."
	}
	return erros
}

// ─────────────────────────── Período especial ───────────────────────────────

// PeriodoEspecial é faixa do calendário comercial, INCLUSIVA nas duas pontas —
// ao contrário da estadia, que é half-open. Períodos podem se sobrepor de
// propósito (Réveillon dentro da alta temporada) e a precedência resolve; é por
// isso que não existe constraint de exclusão nesta tabela.
type PeriodoEspecial struct {
	ID        uuid.UUID `json:"id"`
	Nome      string    `json:"name"`
	Especie   string    `json:"kind"`
	ComecaEm  Data      `json:"starts_on"`
	TerminaEm Data      `json:"ends_on"`
	Ativo     bool      `json:"active"`
}

type PeriodoEntrada struct {
	Nome      string          `json:"name" validate:"required,min=2,max=120"`
	Especie   string          `json:"kind" validate:"required,oneof=reveillon carnaval alta evento"`
	ComecaEm  *Data           `json:"starts_on" validate:"required"`
	TerminaEm *Data           `json:"ends_on" validate:"required"`
	Ativo     httpx.Opt[bool] `json:"active"`
}

func (e *PeriodoEntrada) Normalizar() { e.Nome = strings.TrimSpace(e.Nome) }

func (e PeriodoEntrada) Validar() map[string]string {
	erros := map[string]string{}
	if e.ComecaEm != nil && e.TerminaEm != nil && e.TerminaEm.Before(e.ComecaEm.Date) {
		erros["ends_on"] = "não pode ser anterior a starts_on."
	}
	return erros
}

type PeriodoAtualizar struct {
	Nome      httpx.Opt[string] `json:"name"`
	Especie   httpx.Opt[string] `json:"kind"`
	ComecaEm  httpx.Opt[Data]   `json:"starts_on"`
	TerminaEm httpx.Opt[Data]   `json:"ends_on"`
	Ativo     httpx.Opt[bool]   `json:"active"`
}

func (a *PeriodoAtualizar) Normalizar() {
	if v, ok := a.Nome.Definido(); ok {
		a.Nome = httpx.De(strings.TrimSpace(v))
	}
}

func (a PeriodoAtualizar) Validar() map[string]string {
	erros := map[string]string{}
	naoNulo(erros, "name", a.Nome.DeveLimpar())
	naoNulo(erros, "kind", a.Especie.DeveLimpar())
	naoNulo(erros, "starts_on", a.ComecaEm.DeveLimpar())
	naoNulo(erros, "ends_on", a.TerminaEm.DeveLimpar())
	naoNulo(erros, "active", a.Ativo.DeveLimpar())

	if v, ok := a.Nome.Definido(); ok && !httpx.ValidarValor(v, "min=2,max=120") {
		erros["name"] = "deve ter entre 2 e 120 caracteres."
	}
	if v, ok := a.Especie.Definido(); ok && !httpx.ValidarValor(v, tagEspecie) {
		erros["kind"] = "deve ser um de: reveillon, carnaval, alta, evento."
	}
	return erros
}

// ─────────────────────────── Mínimo de noites ───────────────────────────────

// MinimoDeNoites é a estadia mínima por tipo de data. Vale o MAIOR mínimo entre
// as noites da estadia — quem aplica isso é `booking.Build`, não este módulo.
type MinimoDeNoites struct {
	ID         uuid.UUID `json:"id"`
	TabelaID   uuid.UUID `json:"rate_table_id"`
	TipoDeData string    `json:"date_type"`
	Noites     int       `json:"nights"`
}

type MinimoEntrada struct {
	TabelaID   uuid.UUID `json:"rate_table_id" validate:"required"`
	TipoDeData string    `json:"date_type" validate:"required,oneof=normal fds feriado alta reveillon carnaval"`
	Noites     int       `json:"nights" validate:"required,gt=0"`
}

type MinimoAtualizar struct {
	TipoDeData httpx.Opt[string] `json:"date_type"`
	Noites     httpx.Opt[int]    `json:"nights"`
}

func (a MinimoAtualizar) Validar() map[string]string {
	erros := map[string]string{}
	naoNulo(erros, "date_type", a.TipoDeData.DeveLimpar())
	naoNulo(erros, "nights", a.Noites.DeveLimpar())

	if v, ok := a.TipoDeData.Definido(); ok && !TipoDeDataValido(v) {
		erros["date_type"] = "deve ser um de: normal, fds, feriado, alta, reveillon, carnaval."
	}
	if v, ok := a.Noites.Definido(); ok && v <= 0 {
		erros["nights"] = "deve ser maior que zero."
	}
	return erros
}

// ─────────────────────────── Política comercial ─────────────────────────────

// PoliticaComercial é versionada: a reserva grava `policy_version` na criação e
// é essa versão que vale para ela até o fim.
//
// `quote_validity_days` (F2-05) é a validade do orçamento emitido sem
// `valid_until`: quem a lê é `disponibilidade.Emitir`, pela versão que
// precificou o orçamento, e o resultado fica congelado em `quotes.valid_until`.
type PoliticaComercial struct {
	ID                   uuid.UUID `json:"id"`
	Versao               int       `json:"version"`
	SinalPct             float64   `json:"deposit_pct"`
	SaldoDiasAntes       int       `json:"balance_due_days"`
	HoldHoras            int       `json:"hold_hours"`
	DescontoAutoPct      float64   `json:"discount_auto_pct"`
	DescontoAprovacaoPct float64   `json:"discount_approval_pct"`
	CaucaoDeEventoCents  int64     `json:"event_deposit_cents"`
	ExtensaoDeHoldHoras  int       `json:"hold_extension_hours"`
	ExtensoesDeHoldMax   int       `json:"hold_max_extensions"`
	ValidadeOrcamentoDia int       `json:"quote_validity_days"`
	ValidoDe             Data      `json:"valid_from"`
	CriadaEm             time.Time `json:"created_at"`
}

// PoliticaComercialEntrada é o corpo do PUT. `version` NÃO é aceito: quem numera
// é o servidor, e aceitar o número do cliente é a porta para reescrever uma
// versão já publicada.
//
// Os campos `Opt` ausentes (ou `null`) HERDAM da última versão publicada — quem
// faz a herança é o banco, copiando a linha anterior (ver
// Repository.PublicarPoliticaComercial). Na primeira publicação da casa caem
// no DEFAULT da coluna.
type PoliticaComercialEntrada struct {
	SinalPct             *float64         `json:"deposit_pct" validate:"required,gte=0,lte=100"`
	SaldoDiasAntes       *int             `json:"balance_due_days" validate:"required,gte=0"`
	HoldHoras            *int             `json:"hold_hours" validate:"required,gt=0"`
	DescontoAutoPct      *float64         `json:"discount_auto_pct" validate:"required,gte=0,lte=100"`
	DescontoAprovacaoPct *float64         `json:"discount_approval_pct" validate:"required,gte=0,lte=100"`
	CaucaoDeEventoCents  httpx.Opt[int64] `json:"event_deposit_cents"`
	ExtensaoDeHoldHoras  httpx.Opt[int]   `json:"hold_extension_hours"`
	ExtensoesDeHoldMax   httpx.Opt[int]   `json:"hold_max_extensions"`
	ValidadeOrcamentoDia httpx.Opt[int]   `json:"quote_validity_days"`
	ValidoDe             *Data            `json:"valid_from" validate:"required"`
}

// Validar guarda a única regra que as tags não expressam: a alçada de aprovação
// não pode ser MENOR que a automática.
//
// Uma faixa de aprovação abaixo da automática deixaria um intervalo em que o
// desconto é ao mesmo tempo livre e proibido — `booking.Policy.Authority`
// resolveria pelo primeiro `case` e a tela mostraria "fecha agora" para um
// desconto que a política considera negado.
func (e PoliticaComercialEntrada) Validar() map[string]string {
	erros := map[string]string{}
	if e.DescontoAutoPct != nil && e.DescontoAprovacaoPct != nil &&
		*e.DescontoAprovacaoPct < *e.DescontoAutoPct {
		erros["discount_approval_pct"] = "não pode ser menor que discount_auto_pct."
	}
	if v, ok := e.CaucaoDeEventoCents.Definido(); ok && v < 0 {
		erros["event_deposit_cents"] = "não pode ser negativo."
	}
	if v, ok := e.ExtensaoDeHoldHoras.Definido(); ok && v <= 0 {
		erros["hold_extension_hours"] = "deve ser maior que zero."
	}
	if v, ok := e.ExtensoesDeHoldMax.Definido(); ok && v < 0 {
		erros["hold_max_extensions"] = "não pode ser negativo."
	}
	// O piso espelha o `CHECK (quote_validity_days > 0)`; o teto é a guarda de
	// digitação do domínio (3650 no lugar de 365 congelaria o preço por dez
	// anos em `quotes.valid_until`, sem volta).
	if v, ok := e.ValidadeOrcamentoDia.Definido(); ok && (v < 1 || v > booking.QuoteValidityMaxDays) {
		erros["quote_validity_days"] = fmt.Sprintf("deve estar entre 1 e %d.", booking.QuoteValidityMaxDays)
	}
	return erros
}

// ─────────────────────────── Política de cancelamento ───────────────────────

// FaixaDeCancelamento é a faixa por antecedência, em dias até o check-in.
// `null` nos limites é "sem piso" / "sem teto" — é o `-1` de `booking.Tier`
// traduzido para SQL.
type FaixaDeCancelamento struct {
	DiasMin      *int    `json:"days_before_min"`
	DiasMax      *int    `json:"days_before_max"`
	DevolucaoPct float64 `json:"refund_pct"`
	Rotulo       string  `json:"label"`
	Ordem        int     `json:"sort_order"`
}

// FaixaEntrada é a faixa como ela chega no PUT. `sort_order` é ponteiro para
// distinguir "não mandei" (assume a posição no array) de "mandei zero".
// Pelo mesmo motivo de CelulaDaGrade, sem tags: quem valida é
// PoliticaDeCancelamentoEntrada.Validar(), que sabe o índice da faixa.
type FaixaEntrada struct {
	DiasMin      *int     `json:"days_before_min"`
	DiasMax      *int     `json:"days_before_max"`
	DevolucaoPct *float64 `json:"refund_pct"`
	Rotulo       string   `json:"label"`
	Ordem        *int     `json:"sort_order"`
}

type PoliticaDeCancelamento struct {
	ID       uuid.UUID             `json:"id"`
	Versao   int                   `json:"version"`
	Nome     string                `json:"name"`
	ValidoDe Data                  `json:"valid_from"`
	Faixas   []FaixaDeCancelamento `json:"tiers"`
}

// PoliticaDeCancelamentoEntrada é o corpo do PUT: cria versão nova com as faixas
// INTEIRAS. Não existe endpoint de faixa avulsa porque uma faixa sozinha não é
// uma política — ver Validar().
type PoliticaDeCancelamentoEntrada struct {
	Nome     string         `json:"name" validate:"required,min=2,max=120"`
	ValidoDe *Data          `json:"valid_from" validate:"required"`
	Faixas   []FaixaEntrada `json:"tiers" validate:"required,min=1"`
}

func (e *PoliticaDeCancelamentoEntrada) Normalizar() {
	e.Nome = strings.TrimSpace(e.Nome)
	for i := range e.Faixas {
		e.Faixas[i].Rotulo = strings.TrimSpace(e.Faixas[i].Rotulo)
		if e.Faixas[i].Ordem == nil {
			// Sem `sort_order` explícito, vale a posição no array: é a ordem em
			// que a tela desenhou as faixas, e é ela que `booking` percorre.
			ordem := i + 1
			e.Faixas[i].Ordem = &ordem
		}
	}
}

// Validar recusa a política que o motor não saberia aplicar.
//
// `booking.CancellationPolicy.Simulate` devolve a PRIMEIRA faixa aplicável e,
// não achando nenhuma, retém tudo "para decidir manualmente". Um buraco entre
// duas faixas viraria, na prática, retenção integral silenciosa numa
// antecedência que ninguém quis punir; uma sobreposição faria a devolução
// depender da ordem de avaliação, que é exatamente o tipo de regra que ninguém
// consegue explicar ao hóspede.
//
// Por isso a cobertura exigida é [0, ∞): todo dia de antecedência cai em uma, e
// só uma, faixa.
func (e PoliticaDeCancelamentoEntrada) Validar() map[string]string {
	erros := map[string]string{}

	ordens := map[int]int{}
	for i, f := range e.Faixas {
		if f.DevolucaoPct == nil {
			erros[fmt.Sprintf("tiers[%d].refund_pct", i)] = "é obrigatório."
		} else if *f.DevolucaoPct < 0 || *f.DevolucaoPct > 100 {
			erros[fmt.Sprintf("tiers[%d].refund_pct", i)] = "deve estar entre 0 e 100."
		}
		if !httpx.ValidarValor(f.Rotulo, "required,min=2,max=120") {
			erros[fmt.Sprintf("tiers[%d].label", i)] = "deve ter entre 2 e 120 caracteres."
		}
		if f.DiasMin != nil && *f.DiasMin < 0 {
			erros[fmt.Sprintf("tiers[%d].days_before_min", i)] = "não pode ser negativo; use null para 'sem piso'."
		}
		if f.DiasMax != nil && *f.DiasMax < 0 {
			erros[fmt.Sprintf("tiers[%d].days_before_max", i)] = "não pode ser negativo; use null para 'sem teto'."
		}
		if f.DiasMin != nil && f.DiasMax != nil && *f.DiasMax < *f.DiasMin {
			erros[fmt.Sprintf("tiers[%d].days_before_max", i)] = "não pode ser menor que days_before_min."
		}
		if f.Ordem != nil {
			if anterior, repetido := ordens[*f.Ordem]; repetido {
				erros[fmt.Sprintf("tiers[%d].sort_order", i)] = fmt.Sprintf(
					"repetido — já usado no índice %d. A chave natural é (policy_id, sort_order).", anterior)
			}
			ordens[*f.Ordem] = i
		}
	}
	if len(erros) > 0 {
		return erros
	}

	if msg := cobrituraCompleta(e.Faixas); msg != "" {
		erros["tiers"] = msg
	}
	return erros
}

// semTeto é o "infinito" das faixas: a antecedência máxima possível. Um número
// finito bem acima de qualquer reserva real evita aritmética com sentinela.
const semTeto = 1 << 30

// cobrituraCompleta confere que as faixas cobrem [0, ∞) sem buraco e sem
// sobreposição. Devolve a mensagem do problema, ou vazio quando está certo.
func cobrituraCompleta(faixas []FaixaEntrada) string {
	type intervalo struct{ lo, hi int }

	ordenados := make([]intervalo, 0, len(faixas))
	for _, f := range faixas {
		lo, hi := 0, semTeto
		if f.DiasMin != nil {
			lo = *f.DiasMin
		}
		if f.DiasMax != nil {
			hi = *f.DiasMax
		}
		ordenados = append(ordenados, intervalo{lo, hi})
	}
	// Ordena por piso: a ordem de avaliação (sort_order) é da mais generosa à
	// mais restritiva, o oposto desta — e o que se confere aqui é cobertura, que
	// não depende da ordem em que a tela mandou.
	for i := 1; i < len(ordenados); i++ {
		for j := i; j > 0 && ordenados[j].lo < ordenados[j-1].lo; j-- {
			ordenados[j], ordenados[j-1] = ordenados[j-1], ordenados[j]
		}
	}

	if ordenados[0].lo != 0 {
		return fmt.Sprintf("as faixas precisam cobrir a partir de 0 dias de antecedência; a mais restritiva começa em %d.", ordenados[0].lo)
	}
	for i := 1; i < len(ordenados); i++ {
		anterior, atual := ordenados[i-1], ordenados[i]
		switch {
		case atual.lo <= anterior.hi:
			return fmt.Sprintf("faixas sobrepostas: %s e %s compartilham dias de antecedência.",
				rotuloDoIntervalo(anterior.lo, anterior.hi), rotuloDoIntervalo(atual.lo, atual.hi))
		case atual.lo > anterior.hi+1:
			return fmt.Sprintf("buraco entre as faixas: nenhuma cobre de %d a %d dias de antecedência.",
				anterior.hi+1, atual.lo-1)
		}
	}
	if ordenados[len(ordenados)-1].hi != semTeto {
		return fmt.Sprintf("a faixa mais generosa precisa ser aberta (days_before_max nulo); hoje ela para em %d dias.",
			ordenados[len(ordenados)-1].hi)
	}
	return ""
}

func rotuloDoIntervalo(lo, hi int) string {
	if hi >= semTeto {
		return fmt.Sprintf("[%d, sem teto]", lo)
	}
	return fmt.Sprintf("[%d, %d]", lo, hi)
}

// ─────────────────────────── Filtros de listagem ────────────────────────────

// FiltroDeTabelas é o `?active` e o `?on` de GET /rate-tables.
type FiltroDeTabelas struct {
	Ativa *bool
	Em    *Data
}

// FiltroDeTarifas é o recorte da grade em GET /rates.
type FiltroDeTarifas struct {
	TabelaID   *uuid.UUID
	ProdutoID  *uuid.UUID
	TipoDeData *string
}

// FiltroDeCalendario serve a feriados e períodos: janela de datas e ativo.
type FiltroDeCalendario struct {
	De      *Data
	Ate     *Data
	Especie *string
	Ativo   *bool
}

// FiltroDeMinimos é o recorte de GET /min-nights.
type FiltroDeMinimos struct {
	TabelaID   *uuid.UUID
	TipoDeData *string
}

// ─────────────────────────── Auxiliares ─────────────────────────────────────

// naoNulo registra o erro de `null` explícito em campo que não aceita nulo. O
// Opt distingue ausente de nulo justamente para esta mensagem existir.
func naoNulo(erros map[string]string, campo string, deveLimpar bool) {
	if deveLimpar {
		erros[campo] = "não pode ser nulo."
	}
}

// Meta monta o bloco de paginação das listagens do módulo.
func Meta(pagina, porPagina int, total int64) httpx.Meta {
	return httpx.Meta{Page: pagina, PerPage: porPagina, Total: total}
}
