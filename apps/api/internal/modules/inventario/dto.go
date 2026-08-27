// Package inventario implementa o cadastro do que se vende (produtos), do que
// se ocupa (unidades físicas), da composição que liga um ao outro e da
// propriedade que abriga tudo.
//
// A distinção entre produto e unidade não é organizacional, é o que torna a
// defesa contra overbooking possível: `stay_blocks` só sabe travar unidade
// nominal, e é a composição (`unit_type_members`) que diz quantas linhas uma
// venda insere. Produto sem composição correta é overbooking silencioso.
package inventario

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Recurso é o código do catálogo de RBAC que protege TODAS as rotas deste
// módulo. Sai de `resources`, semeado por cmd/seed/acesso.go — a constante
// existe só para a tabela de rotas não digitar a string em dezessete linhas.
const Recurso = "inventory"

// Consumo — quantas unidades da composição uma venda ocupa.
//
// `one_member` insere UMA linha em stay_blocks (a unidade que o alocador
// escolher); `all_members` insere TODAS. É daqui que a exclusividade da White
// House Completa cai de graça da constraint do banco.
const (
	ConsomeUmMembro     = "one_member"
	ConsomeTodosMembros = "all_members"
)

// tamanhoMaximoDaComposicao é o teto do corpo de PUT /unit-types/{id}/members.
// Existe porque o corpo vira DELETE + INSERT numa transação: sem teto, uma
// lista de 100 mil uuids segura a tabela inteira enquanto é validada.
const tamanhoMaximoDaComposicao = 500

// padraoDeFuso aceita só a família America/*, como o contrato manda.
//
// Validado por padrão de texto e não por time.LoadLocation de propósito: a
// imagem de produção pode subir sem tzdata, e nesse caso LoadLocation recusaria
// até `America/Fortaleza` — o fuso que o sistema inteiro usa.
var padraoDeFuso = regexp.MustCompile(`^America/[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+)?$`)

// ─────────────────────────── Propriedade ────────────────────────────

// Propriedade é o schema `Propriedade` do contrato.
type Propriedade struct {
	ID           uuid.UUID `json:"id"`
	Nome         string    `json:"name"`
	Slug         string    `json:"slug"`
	Fuso         string    `json:"timezone"`
	Endereco     *string   `json:"address"`
	Cidade       *string   `json:"city"`
	UF           *string   `json:"state"`
	Ativa        bool      `json:"active"`
	CriadaEm     time.Time `json:"created_at"`
	AtualizadaEm time.Time `json:"updated_at"`
}

// PropriedadeAtualizar é o corpo do PATCH /properties/{id}.
//
// Não há POST nem DELETE de propriedade no contrato: criar a casa é ato de
// instalação, feito por migration/seed.
type PropriedadeAtualizar struct {
	Nome     httpx.Opt[string] `json:"name"`
	Fuso     httpx.Opt[string] `json:"timezone"`
	Endereco httpx.Opt[string] `json:"address"`
	Cidade   httpx.Opt[string] `json:"city"`
	UF       httpx.Opt[string] `json:"state"`
	Ativa    httpx.Opt[bool]   `json:"active"`
}

func (a *PropriedadeAtualizar) Normalizar() {
	a.Nome = aparado(a.Nome)
	a.Fuso = aparado(a.Fuso)
	a.Endereco = aparadoOuNulo(a.Endereco)
	a.Cidade = aparadoOuNulo(a.Cidade)
	a.UF = aparadoOuNulo(a.UF)
}

func (a PropriedadeAtualizar) Validar() map[string]string {
	erros := map[string]string{}

	exigirTexto(erros, "name", a.Nome, 2, 120)
	if a.Fuso.DeveLimpar() {
		erros["timezone"] = "não pode ser nulo."
	} else if v, ok := a.Fuso.Definido(); ok && !padraoDeFuso.MatchString(v) {
		// Mudar o fuso reinterpreta "hoje" no sistema inteiro — antecedência de
		// cancelamento, expiração de hold e classificação de noite saem dele.
		erros["timezone"] = "use um fuso da família America/ (ex.: America/Fortaleza)."
	}
	limitarTexto(erros, "address", a.Endereco, 200)
	limitarTexto(erros, "city", a.Cidade, 120)
	limitarTexto(erros, "state", a.UF, 2)
	if a.Ativa.DeveLimpar() {
		erros["active"] = "não pode ser nulo."
	}

	return erros
}

// ─────────────────────────── Produto ────────────────────────────────

// Produto é o schema `Produto` — o que se vende (`unit_types`).
type Produto struct {
	ID     uuid.UUID `json:"id"`
	Codigo string    `json:"code"`
	Nome   string    `json:"name"`
	// Capacidade é DECLARADA, nunca somada a partir da composição: a Completa
	// acomoda 24 embora as oito unidades somem 40. É decisão dos proprietários
	// sobre evento, e o sistema não a recalcula (spec §2).
	Capacidade         int       `json:"capacity"`
	Consome            string    `json:"consumes"`
	TaxaDeLimpezaCents int64     `json:"cleaning_fee_cents"`
	Descricao          *string   `json:"description"`
	Ordem              int       `json:"sort_order"`
	Ativo              bool      `json:"active"`
	CriadoEm           time.Time `json:"created_at"`
	AtualizadoEm       time.Time `json:"updated_at"`
}

// ProdutoEntrada é o corpo do POST e do PUT (schema `ProdutoCriar`).
//
// Ponteiro nos opcionais, e não Opt: aqui não existe o estado "ausente não
// mexe" — o PUT é substituição integral, e ausente significa "volta ao padrão
// do schema". Quem precisa dos três estados é o PATCH, abaixo.
type ProdutoEntrada struct {
	Codigo             string  `json:"code" validate:"required"`
	Nome               string  `json:"name" validate:"required,min=2,max=120"`
	Capacidade         *int    `json:"capacity" validate:"required"`
	Consome            string  `json:"consumes" validate:"required"`
	TaxaDeLimpezaCents *int64  `json:"cleaning_fee_cents"`
	Descricao          *string `json:"description"`
	Ordem              *int    `json:"sort_order"`
	Ativo              *bool   `json:"active"`
}

func (c *ProdutoEntrada) Normalizar() {
	c.Codigo = strings.TrimSpace(c.Codigo)
	c.Nome = strings.TrimSpace(c.Nome)
	c.Consome = strings.TrimSpace(c.Consome)
	c.Descricao = textoOuNulo(c.Descricao)
}

func (c ProdutoEntrada) Validar() map[string]string {
	erros := map[string]string{}

	if c.Codigo != "" && !httpx.ValidarValor(c.Codigo, "min=2,max=40") {
		erros["code"] = "deve ter entre 2 e 40 caracteres."
	} else if strings.ContainsAny(c.Codigo, " \t\n") {
		erros["code"] = "não pode conter espaços."
	}
	if c.Consome != "" && !ConsumoValido(c.Consome) {
		erros["consumes"] = "deve ser one_member ou all_members."
	}
	if c.Capacidade != nil && *c.Capacidade < 1 {
		erros["capacity"] = "deve ser no mínimo 1."
	}
	if c.TaxaDeLimpezaCents != nil && *c.TaxaDeLimpezaCents < 0 {
		erros["cleaning_fee_cents"] = "não pode ser negativa."
	}
	if c.Descricao != nil && !httpx.ValidarValor(*c.Descricao, "max=2000") {
		erros["description"] = "deve ter no máximo 2000 caracteres."
	}

	return erros
}

// ComPadroes aplica os defaults declarados no schema — é o que faz o PUT
// substituir de verdade, em vez de manter por omissão o que já estava lá.
func (c ProdutoEntrada) ComPadroes() ProdutoEntrada {
	if c.TaxaDeLimpezaCents == nil {
		c.TaxaDeLimpezaCents = novo[int64](0)
	}
	if c.Ordem == nil {
		c.Ordem = novo(0)
	}
	if c.Ativo == nil {
		c.Ativo = novo(true)
	}
	return c
}

// ProdutoAtualizar é o corpo do PATCH — ausente não mexe, `null` limpa.
type ProdutoAtualizar struct {
	Codigo             httpx.Opt[string] `json:"code"`
	Nome               httpx.Opt[string] `json:"name"`
	Capacidade         httpx.Opt[int]    `json:"capacity"`
	Consome            httpx.Opt[string] `json:"consumes"`
	TaxaDeLimpezaCents httpx.Opt[int64]  `json:"cleaning_fee_cents"`
	Descricao          httpx.Opt[string] `json:"description"`
	Ordem              httpx.Opt[int]    `json:"sort_order"`
	Ativo              httpx.Opt[bool]   `json:"active"`
}

func (a *ProdutoAtualizar) Normalizar() {
	a.Codigo = aparado(a.Codigo)
	a.Nome = aparado(a.Nome)
	a.Consome = aparado(a.Consome)
	a.Descricao = aparadoOuNulo(a.Descricao)
}

func (a ProdutoAtualizar) Validar() map[string]string {
	erros := map[string]string{}

	exigirTexto(erros, "code", a.Codigo, 2, 40)
	if v, ok := a.Codigo.Definido(); ok && strings.ContainsAny(v, " \t\n") {
		erros["code"] = "não pode conter espaços."
	}
	exigirTexto(erros, "name", a.Nome, 2, 120)

	if a.Capacidade.DeveLimpar() {
		erros["capacity"] = "não pode ser nulo."
	} else if v, ok := a.Capacidade.Definido(); ok && v < 1 {
		erros["capacity"] = "deve ser no mínimo 1."
	}

	if a.Consome.DeveLimpar() {
		erros["consumes"] = "não pode ser nulo."
	} else if v, ok := a.Consome.Definido(); ok && !ConsumoValido(v) {
		erros["consumes"] = "deve ser one_member ou all_members."
	}

	if a.TaxaDeLimpezaCents.DeveLimpar() {
		erros["cleaning_fee_cents"] = "não pode ser nulo."
	} else if v, ok := a.TaxaDeLimpezaCents.Definido(); ok && v < 0 {
		erros["cleaning_fee_cents"] = "não pode ser negativa."
	}

	limitarTexto(erros, "description", a.Descricao, 2000)

	if a.Ordem.DeveLimpar() {
		erros["sort_order"] = "não pode ser nulo."
	}
	if a.Ativo.DeveLimpar() {
		erros["active"] = "não pode ser nulo."
	}

	return erros
}

// ─────────────────────────── Unidade ────────────────────────────────

// Unidade é o schema `Unidade` — o que é ocupado e limpo, e o que a constraint
// `stay_no_overlap` protege.
//
// NÃO carrega produto: o vínculo é `unit_type_members`, muitos-para-muitos de
// propósito — `AP-01` é do Apartamento 2 Suítes E da Completa ao mesmo tempo.
type Unidade struct {
	ID           uuid.UUID `json:"id"`
	Codigo       string    `json:"code"`
	Nome         string    `json:"name"`
	Andar        *string   `json:"floor"`
	Observacoes  *string   `json:"notes"`
	Ordem        int       `json:"sort_order"`
	Ativa        bool      `json:"active"`
	CriadaEm     time.Time `json:"created_at"`
	AtualizadaEm time.Time `json:"updated_at"`
}

// UnidadeEntrada é o corpo do POST e do PUT (schema `UnidadeCriar`).
type UnidadeEntrada struct {
	Codigo      string  `json:"code" validate:"required"`
	Nome        string  `json:"name" validate:"required,min=2,max=120"`
	Andar       *string `json:"floor"`
	Observacoes *string `json:"notes"`
	Ordem       *int    `json:"sort_order"`
	Ativa       *bool   `json:"active"`
}

func (c *UnidadeEntrada) Normalizar() {
	c.Codigo = strings.TrimSpace(c.Codigo)
	c.Nome = strings.TrimSpace(c.Nome)
	c.Andar = textoOuNulo(c.Andar)
	c.Observacoes = textoOuNulo(c.Observacoes)
}

func (c UnidadeEntrada) Validar() map[string]string {
	erros := map[string]string{}

	if c.Codigo != "" && !httpx.ValidarValor(c.Codigo, "min=2,max=40") {
		erros["code"] = "deve ter entre 2 e 40 caracteres."
	} else if strings.ContainsAny(c.Codigo, " \t\n") {
		// O código é a chave de ordenação de toda inserção em lote de
		// stay_blocks; espaço no meio faz a ordem depender de collation.
		erros["code"] = "não pode conter espaços."
	}
	if c.Andar != nil && !httpx.ValidarValor(*c.Andar, "max=40") {
		erros["floor"] = "deve ter no máximo 40 caracteres."
	}
	if c.Observacoes != nil && !httpx.ValidarValor(*c.Observacoes, "max=2000") {
		erros["notes"] = "deve ter no máximo 2000 caracteres."
	}

	return erros
}

func (c UnidadeEntrada) ComPadroes() UnidadeEntrada {
	if c.Ordem == nil {
		c.Ordem = novo(0)
	}
	if c.Ativa == nil {
		c.Ativa = novo(true)
	}
	return c
}

// UnidadeAtualizar é o corpo do PATCH /units/{id}.
type UnidadeAtualizar struct {
	Codigo      httpx.Opt[string] `json:"code"`
	Nome        httpx.Opt[string] `json:"name"`
	Andar       httpx.Opt[string] `json:"floor"`
	Observacoes httpx.Opt[string] `json:"notes"`
	Ordem       httpx.Opt[int]    `json:"sort_order"`
	Ativa       httpx.Opt[bool]   `json:"active"`
}

func (a *UnidadeAtualizar) Normalizar() {
	a.Codigo = aparado(a.Codigo)
	a.Nome = aparado(a.Nome)
	a.Andar = aparadoOuNulo(a.Andar)
	a.Observacoes = aparadoOuNulo(a.Observacoes)
}

func (a UnidadeAtualizar) Validar() map[string]string {
	erros := map[string]string{}

	exigirTexto(erros, "code", a.Codigo, 2, 40)
	if v, ok := a.Codigo.Definido(); ok && strings.ContainsAny(v, " \t\n") {
		erros["code"] = "não pode conter espaços."
	}
	exigirTexto(erros, "name", a.Nome, 2, 120)
	limitarTexto(erros, "floor", a.Andar, 40)
	limitarTexto(erros, "notes", a.Observacoes, 2000)

	if a.Ordem.DeveLimpar() {
		erros["sort_order"] = "não pode ser nulo."
	}
	if a.Ativa.DeveLimpar() {
		erros["active"] = "não pode ser nulo."
	}

	return erros
}

// ─────────────────────────── Composição ─────────────────────────────

// UnidadeDaComposicao é um item de `unit_type_members`, sempre ordenado por
// `units.code`.
type UnidadeDaComposicao struct {
	UnidadeID uuid.UUID `json:"unit_id"`
	Codigo    string    `json:"unit_code"`
	Nome      string    `json:"unit_name"`
	// Ativa é o alarme da composição, e desde esta rodada ela só pode vir
	// `false` em dado LEGADO: gravar composição com unidade inativa passou a ser
	// 422, e desativar unidade que compõe produto passou a ser 409 (service.go).
	//
	// O campo fica porque a tela precisa MOSTRAR o buraco onde ele já existe:
	// membro inativo é um produto que declara N unidades e vende N-1 — no
	// `all_members`, a casa inteira entregue com uma unidade a menos.
	Ativa bool `json:"active"`
}

// ComposicaoEntrada é o corpo do PUT /unit-types/{id}/members. Substitui o
// conjunto inteiro: o que não vier deixa de fazer parte.
type ComposicaoEntrada struct {
	UnidadeIDs []uuid.UUID `json:"unit_ids"`
}

// Validar recusa as três formas de composição inválida.
//
// A lista vazia é a mais importante: produto sem composição não consome unidade
// nenhuma, e um produto VENDÁVEL que não consome unidade nenhuma é overbooking
// silencioso — a venda passa pela constraint porque não insere linha alguma em
// stay_blocks. Vale para os dois `consumes`, e com força extra no `all_members`:
// "todas as unidades" de um conjunto vazio é nenhuma.
func (c ComposicaoEntrada) Validar() map[string]string {
	erros := map[string]string{}

	if len(c.UnidadeIDs) == 0 {
		erros["unit_ids"] = "informe ao menos uma unidade: produto sem composição não pode ser vendido."
		return erros
	}
	if len(c.UnidadeIDs) > tamanhoMaximoDaComposicao {
		erros["unit_ids"] = fmt.Sprintf("no máximo %d unidades por produto.", tamanhoMaximoDaComposicao)
		return erros
	}

	vistos := map[uuid.UUID]int{}
	for i, id := range c.UnidadeIDs {
		campo := fmt.Sprintf("unit_ids[%d]", i)
		if id == uuid.Nil {
			erros[campo] = "identificador inválido."
			continue
		}
		if anterior, repetido := vistos[id]; repetido {
			// A chave é (unit_type_id, unit_id): deixar passar faria o INSERT
			// estourar 23505 no meio da transação e virar erro sem campo.
			erros[campo] = fmt.Sprintf("unidade repetida — já declarada no índice %d.", anterior)
			continue
		}
		vistos[id] = i
	}

	return erros
}

// ─────────────────────────── Filtros ────────────────────────────────

// Filtro é o que as três listagens recebem já resolvido pelo handler: nada do
// que o cliente digitou entra na consulta a não ser como argumento ($1, $2…) ou
// como valor JÁ MAPEADO pela whitelist de ordenação.
type Filtro struct {
	Busca     string
	Ativo     *bool
	ProdutoID *uuid.UUID // só /units: filtra pela composição
	OrderBy   string
	Pagina    int
	PorPagina int
}

// ConsumoValido é o enum `Consumo` do contrato.
func ConsumoValido(v string) bool {
	return v == ConsomeUmMembro || v == ConsomeTodosMembros
}

// ─────────────────────────── Utilidades ─────────────────────────────

func novo[T any](v T) *T { return &v }

// aparado apara o texto de um Opt presente, mantendo os estados ausente/null.
func aparado(o httpx.Opt[string]) httpx.Opt[string] {
	if v, ok := o.Definido(); ok {
		return httpx.De(strings.TrimSpace(v))
	}
	return o
}

// aparadoOuNulo trata string em branco como pedido de limpar. String vazia e
// NULL significam a mesma coisa numa coluna anulável; gravar as duas formas
// daria dois jeitos de dizer "sem valor".
func aparadoOuNulo(o httpx.Opt[string]) httpx.Opt[string] {
	if v, ok := o.Definido(); ok {
		if strings.TrimSpace(v) == "" {
			return httpx.Nulo[string]()
		}
		return httpx.De(strings.TrimSpace(v))
	}
	return o
}

func textoOuNulo(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}

// exigirTexto é a regra dos campos que a coluna declara NOT NULL: `null`
// explícito é 422 legível, não uma violação 23502 traduzida.
func exigirTexto(erros map[string]string, campo string, o httpx.Opt[string], min, max int) {
	if o.DeveLimpar() {
		erros[campo] = "não pode ser nulo."
		return
	}
	if v, ok := o.Definido(); ok && !httpx.ValidarValor(v, fmt.Sprintf("min=%d,max=%d", min, max)) {
		erros[campo] = fmt.Sprintf("deve ter entre %d e %d caracteres.", min, max)
	}
}

// limitarTexto vale para coluna anulável: `null` é aceito, tamanho não.
func limitarTexto(erros map[string]string, campo string, o httpx.Opt[string], max int) {
	if v, ok := o.Definido(); ok && !httpx.ValidarValor(v, fmt.Sprintf("max=%d", max)) {
		erros[campo] = fmt.Sprintf("deve ter no máximo %d caracteres.", max)
	}
}

// ─────────────────── Tipos das guardas de desativação ───────────────

// UnidadeResumida é o que a validação da composição precisa saber de uma
// unidade: que ela existe NESTA propriedade, e se está ativa.
//
// A segunda metade é nova nesta rodada. Compor um produto com unidade INATIVA
// é a mesma falha que desativar unidade que já compõe — só chega pela porta
// oposta: o produto passa a declarar oito unidades e a venda aloca sete,
// porque a consulta de candidatas filtra `u.active`.
type UnidadeResumida struct {
	Codigo string
	Ativa  bool
}

// VinculoDeComposicao é um produto que consome a unidade.
//
// `Consome` vem junto porque os dois valores têm gravidades diferentes:
// desativar unidade de um `one_member` encolhe o inventário vendável (ruim,
// recuperável); desativar unidade de um `all_members` quebra a EXCLUSIVIDADE do
// produto — a White House Completa continua sendo vendida pelo preço de oito
// unidades travando sete, e a oitava fica livre para um estranho.
type VinculoDeComposicao struct {
	Codigo  string
	Consome string
}

// O estado do produto NÃO entra aqui de propósito: produto inativo continua
// impedindo desativar a unidade que ele consome. Relaxar isso pareceria seguro
// (produto fora do ar não vende), mas reativar o produto não tem guarda — e a
// unidade já estaria fora, com a composição intacta. Seria o mesmo buraco, uma
// porta adiante.

// ReservaExclusiva é uma venda de produto `all_members` que JÁ ESTÁ DE PÉ e que
// não tem bloco nesta unidade — o buraco que a reativação abriria.
//
// Vai inteira para o `details` do 409: sem o código e as datas, o operador
// recebe "não pode" e não tem como agir.
type ReservaExclusiva struct {
	Codigo        string `json:"code"`
	ProdutoCodigo string `json:"unit_type_code"`
	CheckIn       string `json:"check_in"`
	CheckOut      string `json:"check_out"`
}

// ReservaViva é uma venda do produto que AINDA OCUPA o calendário — e que, por
// isso, congelou o que a composição significava no dia em que saiu.
//
// Ela existe por causa do CRÍTICO desta rodada: a exclusividade da Completa não
// cai só pela coluna `active`, cai também pela COMPOSIÇÃO. Com a casa vendida e
// oito blocos de pé, `PUT /unit-types/{completa}/members` acrescentando um nono
// apartamento devolvia 200 — e a reserva passava a declarar nove unidades
// segurando oito. A `EXCLUDE` não vê: o buraco é a AUSÊNCIA de uma linha em
// stay_blocks, e constraint nenhuma enxerga ausência.
//
// `UnidadeIDs` não vai para o JSON porque o operador age por CÓDIGO; ele existe
// para a guarda saber quais unidades a venda de fato segura.
type ReservaViva struct {
	ID         uuid.UUID   `json:"-"`
	UnidadeIDs []uuid.UUID `json:"-"`
	Codigo     string      `json:"code"`
	CheckIn    string      `json:"check_in"`
	CheckOut   string      `json:"check_out"`
	Unidades   []string    `json:"unit_codes"`
}

// Ocupa responde se esta venda segura a unidade.
func (r ReservaViva) Ocupa(unidade uuid.UUID) bool {
	for _, id := range r.UnidadeIDs {
		if id == unidade {
			return true
		}
	}
	return false
}
