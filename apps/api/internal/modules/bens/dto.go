// Package bens é o inventário de bens físicos por ambiente — prato, taça,
// cama, toalha, eletrodoméstico — com foto, conferência, avaria, cópia entre
// unidades e exportação. Schema em 20261007170000 (docs/db.md §11); contrato na
// tag `Bens` da OpenAPI.
//
// Não confundir com `internal/modules/inventario`, que é o cadastro COMERCIAL
// (propriedade, produtos, unidades, composição) e mora noutro recurso de RBAC.
// Este módulo só lê de `units` o `id`, o `code` e o `name` — o título da tela —,
// e nunca tarifa, capacidade, composição ou ocupação.
//
// Três coisas atravessam o módulo inteiro e explicam a maior parte do código:
//
//   - a conferência CONGELA: a quantidade esperada é copiada de
//     `room_inventory` para `inventory_count_lines` na abertura, nunca lida por
//     JOIN depois (regra 7 do CLAUDE.md);
//   - uma conferência aberta por unidade é decisão do índice único parcial
//     `inventory_counts_aberta_idx`, nunca de SELECT antes de INSERT;
//   - `room_inventory` e `inventory_count_lines` não têm `property_id`, então é
//     ESTE código que impede misturar item de uma casa com cômodo de outra:
//     toda leitura e toda escrita passam pela propriedade do ator.
package bens

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Recurso é o código RBAC de TODAS as rotas deste módulo (cmd/seed/acesso.go).
// Não é `inventory`: aquele protege o cadastro comercial, e um recurso só faria
// a permissão de "contei 9 taças" apagar um produto que a casa vende.
const Recurso = "inventory.goods"

// Vocabulários fechados — espelham os CHECK de 20261007170000 e os enums da
// OpenAPI. Valor fora daqui é 422 no handler, e não 23514 traduzido do banco:
// o erro chega com o nome do campo, que é o que o painel gruda no input.
var (
	tiposDeAmbiente = []string{"quarto", "banheiro", "cozinha", "sala", "area_externa", "lavanderia", "varanda", "outro"}
	categorias      = []string{"louca", "talher", "copo", "cama", "banho", "mobilia", "eletro", "utensilio", "decoracao", "outro"}
	medidas         = []string{"un", "par", "jogo", "kg", "l", "m"}
	statusDeConf    = []string{StatusAberta, StatusFechada, StatusCancelada}
	tiposDeAvaria   = []string{"quebrado", "faltando", "avariado", "outro"}
	desfechos       = []string{"reposto", "consertado", "cobrado", "perda_aceita", "descartado"}
)

// Estados da conferência (`inventory_counts.status`).
const (
	StatusAberta    = "aberta"
	StatusFechada   = "fechada"
	StatusCancelada = "cancelada"
)

// AvariaFaltando é o `kind` que o fechamento abre para cada falta apurada.
const AvariaFaltando = "faltando"

// medidaPadrao é o DEFAULT de `inventory_items.unit_measure`: quase tudo se
// conta em peças.
const medidaPadrao = "un"

// Tetos de texto do contrato.
const (
	maxNomeDoAmbiente = 120
	maxNomeDoBem      = 160
	maxTextoLivre     = 2000

	// maxFotosPorBem é o `maxItems` de `PUT /inventory/items/{id}/photos`. Sem
	// teto, a galeria vira DELETE + INSERT de milhares de linhas numa transação.
	maxFotosPorBem = 24
)

// maxInteiro é o teto das colunas `int` do schema (quantidades e ordem). Acima
// disso o Postgres devolveria 22003, que chega como 422 sem nome de campo.
const maxInteiro = math.MaxInt32

// ═══════════════════════════ Respostas ════════════════════════════════════

// Ambiente é o schema `Ambiente`: um cômodo de uma unidade física.
//
// Os três agregados contam o que está GRAVADO — todas as colocações do cômodo,
// inclusive de bem desativado —, que é a leitura literal de "quantos bens
// distintos estão colocados neste ambiente". O filtro de ativos é da TELA
// (`/units/{id}/inventory`, `include_inactive`), e os `totals` de lá é que
// somam o que foi exibido.
type Ambiente struct {
	ID            uuid.UUID `json:"id"`
	UnidadeID     uuid.UUID `json:"unit_id"`
	UnidadeCodigo string    `json:"unit_code"`
	UnidadeNome   string    `json:"unit_name"`
	// Codigo é a identidade estável do cômodo na unidade (não editável):
	// é por ele que a cópia reencontra a cozinha depois de um rename.
	Codigo           string    `json:"code"`
	Nome             string    `json:"name"`
	Tipo             string    `json:"kind"`
	Ordem            int       `json:"sort_order"`
	Ativo            bool      `json:"active"`
	QtdBens          int       `json:"items_count"`
	QtdEsperadaTotal int64     `json:"expected_qty_total"`
	AvariasAbertas   int       `json:"open_issues_count"`
	CriadoEm         time.Time `json:"created_at"`
	AtualizadoEm     time.Time `json:"updated_at"`
}

// Midia é o schema `MidiaDeBem`. As duas URLs são da entrega AUTENTICADA
// (`GET /inventory/media/{id}`), nunca endereço público: a foto mostra o
// interior da casa.
type Midia struct {
	ID           uuid.UUID `json:"id"`
	Mime         string    `json:"mime"`
	Bytes        int64     `json:"bytes"`
	Largura      *int      `json:"width"`
	Altura       *int      `json:"height"`
	NomeOriginal string    `json:"original_name"`
	URL          string    `json:"url"`
	URLMiniatura string    `json:"thumb_url"`
	CriadoEm     time.Time `json:"created_at"`
}

// FotoDoBem é uma entrada da galeria; a capa é a de menor `sort_order`,
// desempatada por `media.id`.
type FotoDoBem struct {
	Midia Midia `json:"media"`
	Ordem int   `json:"sort_order"`
}

// Bem é o schema `Bem`: um item do CATÁLOGO da propriedade, não uma linha por
// cômodo. Os agregados contam todas as colocações gravadas (ver Ambiente).
type Bem struct {
	ID                    uuid.UUID `json:"id"`
	Nome                  string    `json:"name"`
	Descricao             *string   `json:"description"`
	Categoria             string    `json:"category"`
	Medida                string    `json:"unit_measure"`
	CustoDeReposicaoCents *int64    `json:"replacement_cost_cents"`
	Ativo                 bool      `json:"active"`
	OrigemDaImportacao    *string   `json:"source_ref"`
	Capa                  *Midia    `json:"cover"`
	QtdFotos              int       `json:"photos_count"`
	QtdColocacoes         int       `json:"placements_count"`
	QtdEsperadaTotal      int64     `json:"expected_qty_total"`
	AvariasAbertas        int       `json:"open_issues_count"`
	CriadoEm              time.Time `json:"created_at"`
	AtualizadoEm          time.Time `json:"updated_at"`
}

// BemCompleto é a ficha: o bem, a galeria inteira e onde ele está.
type BemCompleto struct {
	Bem
	Fotos      []FotoDoBem `json:"photos"`
	Colocacoes []Colocacao `json:"placements"`
}

// Colocacao é o schema `Colocacao`: o par (ambiente, bem) e a quantidade
// ESPERADA. O `id` é a chave natural `room_id_item_id` (ChaveDeColocacao).
type Colocacao struct {
	ID            string    `json:"id"`
	AmbienteID    uuid.UUID `json:"room_id"`
	AmbienteNome  string    `json:"room_name"`
	AmbienteTipo  string    `json:"room_kind"`
	UnidadeID     uuid.UUID `json:"unit_id"`
	UnidadeCodigo string    `json:"unit_code"`
	BemID         uuid.UUID `json:"item_id"`
	BemNome       string    `json:"item_name"`
	BemCategoria  string    `json:"item_category"`
	BemMedida     string    `json:"item_unit_measure"`
	Capa          *Midia    `json:"cover"`
	QtdEsperada   int       `json:"expected_qty"`
	Nota          *string   `json:"note"`
	CriadoEm      time.Time `json:"created_at"`
	AtualizadoEm  time.Time `json:"updated_at"`

	// custo vem junto para os `totals` da unidade sem segunda consulta; não sai
	// no JSON porque `Colocacao` não declara o campo.
	custo *int64
}

// ChaveDaColocacao monta o `id` de uma colocação. O separador é `_` porque
// UUID nunca contém `_`, então a leitura de volta é inequívoca.
func ChaveDaColocacao(ambiente, bem uuid.UUID) string {
	return ambiente.String() + "_" + bem.String()
}

// LerChaveDaColocacao desfaz ChaveDaColocacao. ok=false para qualquer outra
// forma — que o handler responde como 404, como todo id de rota ilegível.
func LerChaveDaColocacao(chave string) (ambiente, bem uuid.UUID, ok bool) {
	a, b, achou := strings.Cut(chave, "_")
	if !achou {
		return uuid.Nil, uuid.Nil, false
	}
	var err error
	if ambiente, err = uuid.Parse(a); err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	if bem, err = uuid.Parse(b); err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	return ambiente, bem, true
}

// AmbienteDoInventario é um cômodo na tela da unidade, com os bens dele.
type AmbienteDoInventario struct {
	Ambiente
	Bens []Colocacao `json:"items"`
}

// UnidadeResumida é o que esta rota pode dizer de uma unidade: o título da
// tela. Nada de tarifa, capacidade, composição ou ocupação — quem conta taças
// não ganha por aqui uma janela para o cadastro comercial.
type UnidadeResumida struct {
	ID     uuid.UUID `json:"id"`
	Codigo string    `json:"code"`
	Nome   string    `json:"name"`
}

// UnidadeDoInventario é a unidade física vista pelo inventário de bens
// (`GET /inventory/units`): o seletor de quem só tem `inventory.goods`. Só
// identificação e números do inventário — nada do cadastro comercial.
type UnidadeDoInventario struct {
	ID     uuid.UUID `json:"id"`
	Codigo string    `json:"code"`
	Nome   string    `json:"name"`
	Ativa  bool      `json:"active"`
	// Ambientes ATIVOS: zero é a unidade que ainda precisa da planta.
	Ambientes int `json:"rooms"`
	// Bens distintos colocados em ambientes ativos.
	Bens              int        `json:"items"`
	ConferenciaAberta *uuid.UUID `json:"open_count_id"`
	UltimaFechadaEm   *time.Time `json:"last_closed_at"`
}

// TotaisDaUnidade soma o que a tela EXIBIU (depois de `include_inactive`,
// `category` e `q`): é o rodapé do que está na frente de quem lê.
type TotaisDaUnidade struct {
	Ambientes             int    `json:"rooms"`
	Bens                  int    `json:"items"`
	QtdEsperada           int64  `json:"expected_qty"`
	AvariasAbertas        int    `json:"open_issues"`
	CustoDeReposicaoCents *int64 `json:"replacement_cost_cents"`
	BensSemCusto          int    `json:"uncosted_items"`
}

// InventarioDaUnidade é a tela inteira do inventário de uma unidade.
type InventarioDaUnidade struct {
	Unidade           UnidadeResumida        `json:"unit"`
	Ambientes         []AmbienteDoInventario `json:"rooms"`
	Totais            TotaisDaUnidade        `json:"totals"`
	ConferenciaAberta *Conferencia           `json:"open_count"`
	UltimaFechada     *Conferencia           `json:"last_closed_count"`
}

// Progresso é o rodapé da tela de contagem (`ProgressoDaConferencia`).
type Progresso struct {
	Linhas      int   `json:"lines"`
	Contadas    int   `json:"counted"`
	Pendentes   int   `json:"pending"`
	Divergentes int   `json:"diverging"`
	Faltas      int64 `json:"missing_qty"`
	Sobras      int64 `json:"surplus_qty"`
}

// Conferencia é o cabeçalho de uma conferência. `*_by_name` é o NOME do
// usuário e só ele: quem opera não é contato, e e-mail ou telefone de quem
// contou não têm o que fazer numa tela de inventário.
type Conferencia struct {
	ID               uuid.UUID  `json:"id"`
	UnidadeID        uuid.UUID  `json:"unit_id"`
	UnidadeCodigo    string     `json:"unit_code"`
	UnidadeNome      string     `json:"unit_name"`
	Status           string     `json:"status"`
	Nota             *string    `json:"note"`
	AbertaPor        *uuid.UUID `json:"opened_by"`
	AbertaPorNome    *string    `json:"opened_by_name"`
	AbertaEm         time.Time  `json:"opened_at"`
	EncerradaPor     *uuid.UUID `json:"closed_by"`
	EncerradaPorNome *string    `json:"closed_by_name"`
	EncerradaEm      *time.Time `json:"closed_at"`
	Progresso        Progresso  `json:"progress"`
	AtualizadaEm     time.Time  `json:"updated_at"`
}

// LinhaDeConferencia é uma linha da contagem. `expected_qty` é a CONGELADA na
// abertura; nome, foto e tipo do cômodo/bem são os de hoje (só a quantidade é
// fato histórico).
type LinhaDeConferencia struct {
	ID            uuid.UUID `json:"id"`
	ConferenciaID uuid.UUID `json:"count_id"`
	AmbienteID    uuid.UUID `json:"room_id"`
	AmbienteNome  string    `json:"room_name"`
	AmbienteTipo  string    `json:"room_kind"`
	BemID         uuid.UUID `json:"item_id"`
	BemNome       string    `json:"item_name"`
	BemDescricao  *string   `json:"item_description"`
	BemCategoria  string    `json:"item_category"`
	BemMedida     string    `json:"item_unit_measure"`
	Capa          *Midia    `json:"cover"`
	QtdEsperada   int       `json:"expected_qty"`
	QtdContada    *int      `json:"counted_qty"`
	Diferenca     *int      `json:"diff"`
	Nota          *string   `json:"note"`
	// CustoCongelado é o custo de reposição copiado do catálogo NO FECHAMENTO
	// (regra 7): nulo enquanto aberta, em cancelada e em bem não cotado.
	CustoCongelado *int64     `json:"replacement_cost_cents"`
	ContadaPor     *uuid.UUID `json:"counted_by"`
	ContadaPorNome *string    `json:"counted_by_name"`
	ContadaEm      *time.Time `json:"counted_at"`
}

// AmbienteDaConferencia agrupa as linhas de um cômodo, na ordem de caminhada.
type AmbienteDaConferencia struct {
	AmbienteID   uuid.UUID            `json:"room_id"`
	AmbienteNome string               `json:"room_name"`
	AmbienteTipo string               `json:"room_kind"`
	Progresso    Progresso            `json:"progress"`
	Linhas       []LinhaDeConferencia `json:"lines"`
}

// ConferenciaCompleta é a conferência inteira, ambiente por ambiente.
// `result` só existe em conferência `fechada`: é a apuração LIDA DE VOLTA, com
// o custo congelado nas linhas, nunca recalculada com o catálogo de hoje.
type ConferenciaCompleta struct {
	Conferencia
	Ambientes []AmbienteDaConferencia `json:"rooms"`
	Resultado *ApuracaoDaConferencia  `json:"result"`
}

// ApuracaoDaConferencia é o que o fechamento apurou (`ApuracaoDaConferencia`).
type ApuracaoDaConferencia struct {
	Divergencias   []Divergencia `json:"divergences"`
	AvariasCriadas int           `json:"issues_created"`
}

// LinhaContada é a resposta do gesto do celular: a linha e o rodapé.
type LinhaContada struct {
	Linha     LinhaDeConferencia `json:"line"`
	Progresso Progresso          `json:"progress"`
}

// Divergencia é uma linha do fechamento em que contado ≠ esperado
// (`DivergenciaDeConferencia`). O custo é o CONGELADO na linha.
type Divergencia struct {
	AmbienteID            uuid.UUID  `json:"room_id"`
	AmbienteNome          string     `json:"room_name"`
	BemID                 uuid.UUID  `json:"item_id"`
	BemNome               string     `json:"item_name"`
	QtdEsperada           int        `json:"expected_qty"`
	QtdContada            int        `json:"counted_qty"`
	Diferenca             int        `json:"diff"`
	CustoDeReposicaoCents *int64     `json:"replacement_cost_cents"`
	PerdaCents            *int64     `json:"loss_cents"`
	AvariaID              *uuid.UUID `json:"issue_id"`
}

// ResultadoDoFechamento é a resposta de `POST /inventory/counts/{id}/close`.
type ResultadoDoFechamento struct {
	Conferencia    Conferencia   `json:"count"`
	Divergencias   []Divergencia `json:"divergences"`
	AvariasCriadas int           `json:"issues_created"`
}

// Avaria é o schema `Avaria`. A reserva sai SÓ pelo código: "o que quebrou
// nesta estadia" se responde sem dizer quem dormiu nela.
type Avaria struct {
	ID                    uuid.UUID  `json:"id"`
	AmbienteID            uuid.UUID  `json:"room_id"`
	AmbienteNome          string     `json:"room_name"`
	UnidadeID             uuid.UUID  `json:"unit_id"`
	UnidadeCodigo         string     `json:"unit_code"`
	BemID                 uuid.UUID  `json:"item_id"`
	BemNome               string     `json:"item_name"`
	BemCategoria          string     `json:"item_category"`
	Capa                  *Midia     `json:"cover"`
	Tipo                  string     `json:"kind"`
	Qtd                   int        `json:"qty"`
	Nota                  *string    `json:"note"`
	CustoDeReposicaoCents *int64     `json:"replacement_cost_cents"`
	CustoTotalCents       *int64     `json:"total_cost_cents"`
	ReservaID             *uuid.UUID `json:"reservation_id"`
	ReservaCodigo         *string    `json:"reservation_code"`
	ConferenciaID         *uuid.UUID `json:"count_id"`
	Desfecho              *string    `json:"resolution"`
	RelatadaPor           *uuid.UUID `json:"reported_by"`
	RelatadaPorNome       *string    `json:"reported_by_name"`
	RelatadaEm            time.Time  `json:"reported_at"`
	ResolvidaPor          *uuid.UUID `json:"resolved_by"`
	ResolvidaPorNome      *string    `json:"resolved_by_name"`
	ResolvidaEm           *time.Time `json:"resolved_at"`
	AtualizadaEm          time.Time  `json:"updated_at"`
}

// ResultadoDaCopia é a resposta de `POST /units/{id}/inventory/copy`.
type ResultadoDaCopia struct {
	DryRun            bool               `json:"dry_run"`
	Origem            OrigemDaCopia      `json:"source"`
	AmbientesCriados  []AmbienteCopiado  `json:"rooms_created"`
	ColocacoesCriadas []ColocacaoCopia   `json:"placements_created"`
	Mantidas          []ColocacaoMantida `json:"kept"`
}

// OrigemDaCopia identifica a unidade de origem.
type OrigemDaCopia struct {
	UnidadeID uuid.UUID `json:"unit_id"`
	Codigo    string    `json:"code"`
}

// AmbienteCopiado é um cômodo que (vai) nascer no destino.
type AmbienteCopiado struct {
	Nome string `json:"name"`
	Tipo string `json:"kind"`
}

// ColocacaoCopia é uma colocação que (vai) nascer, com a quantidade da origem.
type ColocacaoCopia struct {
	AmbienteNome string    `json:"room_name"`
	BemID        uuid.UUID `json:"item_id"`
	BemNome      string    `json:"item_name"`
	QtdEsperada  int       `json:"expected_qty"`
}

// ColocacaoMantida já existia no destino e NÃO foi tocada.
type ColocacaoMantida struct {
	AmbienteNome string `json:"room_name"`
	BemNome      string `json:"item_name"`
	QtdOrigem    int    `json:"source_qty"`
	QtdAtual     int    `json:"current_qty"`
}

// ═══════════════════════════ Pedidos ══════════════════════════════════════
//
// POST e PUT usam ponteiro nos opcionais: ali não existe "ausente não mexe" — o
// PUT é substituição integral, e ausente volta ao padrão do schema. Quem precisa
// dos três estados é o PATCH, com httpx.Opt.
//
// A recusa de campo desconhecido (`source_ref`, `unit_id` no PUT de ambiente,
// `counted_at`, `resolved_by`…) não está escrita aqui: é o
// `DisallowUnknownFields` de httpx.Decode, e funciona justamente porque estes
// DTOs NÃO declaram esses campos.

// AmbienteCriar é o corpo de `POST /rooms`.
//
// `code` é opcional: ausente (ou nulo), o servidor deriva do `name`
// (BaseDoCodigo) e, se já existir na unidade, acrescenta `-2`, `-3`…
// Informado, tem de estar no formato do contrato e livre na unidade.
type AmbienteCriar struct {
	UnidadeID uuid.UUID `json:"unit_id"`
	Codigo    *string   `json:"code"`
	Nome      string    `json:"name"`
	Tipo      string    `json:"kind"`
	Ordem     *int      `json:"sort_order"`
	Ativo     *bool     `json:"active"`
}

func (c AmbienteCriar) Validar() map[string]string {
	erros := map[string]string{}
	if c.UnidadeID == uuid.Nil {
		erros["unit_id"] = "é obrigatório."
	}
	if c.Codigo != nil && !CodigoValido(*c.Codigo) {
		erros["code"] = "use até 60 caracteres minúsculos sem acento, números e hífen entre eles (ex.: suite-1-terreo)."
	}
	exigirNome(erros, "name", c.Nome, maxNomeDoAmbiente)
	exigirDoVocabulario(erros, "kind", c.Tipo, tiposDeAmbiente)
	limitarInteiro(erros, "sort_order", c.Ordem, math.MinInt32)
	return erros
}

// AmbienteSubstituir é o corpo de `PUT /rooms/{id}`. Sem `unit_id`: cômodo
// não muda de unidade, porque levaria junto o histórico da cozinha do AP-01.
type AmbienteSubstituir struct {
	Nome  string `json:"name"`
	Tipo  string `json:"kind"`
	Ordem *int   `json:"sort_order"`
	Ativo *bool  `json:"active"`
}

func (c AmbienteSubstituir) Validar() map[string]string {
	erros := map[string]string{}
	exigirNome(erros, "name", c.Nome, maxNomeDoAmbiente)
	exigirDoVocabulario(erros, "kind", c.Tipo, tiposDeAmbiente)
	limitarInteiro(erros, "sort_order", c.Ordem, math.MinInt32)
	return erros
}

// AmbienteAtualizar é o corpo de `PATCH /rooms/{id}`. Nenhuma das quatro
// colunas é anulável, então `null` é 422 em todas.
type AmbienteAtualizar struct {
	Nome  httpx.Opt[string] `json:"name"`
	Tipo  httpx.Opt[string] `json:"kind"`
	Ordem httpx.Opt[int]    `json:"sort_order"`
	Ativo httpx.Opt[bool]   `json:"active"`
}

func (a AmbienteAtualizar) Validar() map[string]string {
	erros := map[string]string{}
	naoNulo(erros, "name", a.Nome.DeveLimpar())
	naoNulo(erros, "kind", a.Tipo.DeveLimpar())
	naoNulo(erros, "sort_order", a.Ordem.DeveLimpar())
	naoNulo(erros, "active", a.Ativo.DeveLimpar())
	if v, ok := a.Nome.Definido(); ok {
		exigirNome(erros, "name", v, maxNomeDoAmbiente)
	}
	if v, ok := a.Tipo.Definido(); ok {
		exigirDoVocabulario(erros, "kind", v, tiposDeAmbiente)
	}
	if v, ok := a.Ordem.Definido(); ok {
		limitarInteiro(erros, "sort_order", &v, math.MinInt32)
	}
	return erros
}

// BemCriar é o corpo do `POST` e do `PUT` de `/inventory/items`.
type BemCriar struct {
	Nome                  string  `json:"name"`
	Descricao             *string `json:"description"`
	Categoria             string  `json:"category"`
	Medida                *string `json:"unit_measure"`
	CustoDeReposicaoCents *int64  `json:"replacement_cost_cents"`
	Ativo                 *bool   `json:"active"`
}

func (c BemCriar) Validar() map[string]string {
	erros := map[string]string{}
	exigirNome(erros, "name", c.Nome, maxNomeDoBem)
	if c.Descricao != nil {
		limitarTexto(erros, "description", *c.Descricao, maxTextoLivre)
	}
	exigirDoVocabulario(erros, "category", c.Categoria, categorias)
	if c.Medida != nil {
		exigirDoVocabulario(erros, "unit_measure", *c.Medida, medidas)
	}
	validarCusto(erros, c.CustoDeReposicaoCents)
	return erros
}

// BemAtualizar é o corpo de `PATCH /inventory/items/{id}`: `null` limpa
// `description` e devolve o custo a "não cotado"; nas outras colunas é 422.
type BemAtualizar struct {
	Nome                  httpx.Opt[string] `json:"name"`
	Descricao             httpx.Opt[string] `json:"description"`
	Categoria             httpx.Opt[string] `json:"category"`
	Medida                httpx.Opt[string] `json:"unit_measure"`
	CustoDeReposicaoCents httpx.Opt[int64]  `json:"replacement_cost_cents"`
	Ativo                 httpx.Opt[bool]   `json:"active"`
}

func (a BemAtualizar) Validar() map[string]string {
	erros := map[string]string{}
	naoNulo(erros, "name", a.Nome.DeveLimpar())
	naoNulo(erros, "category", a.Categoria.DeveLimpar())
	naoNulo(erros, "unit_measure", a.Medida.DeveLimpar())
	naoNulo(erros, "active", a.Ativo.DeveLimpar())
	if v, ok := a.Nome.Definido(); ok {
		exigirNome(erros, "name", v, maxNomeDoBem)
	}
	if v, ok := a.Descricao.Definido(); ok {
		limitarTexto(erros, "description", v, maxTextoLivre)
	}
	if v, ok := a.Categoria.Definido(); ok {
		exigirDoVocabulario(erros, "category", v, categorias)
	}
	if v, ok := a.Medida.Definido(); ok {
		exigirDoVocabulario(erros, "unit_measure", v, medidas)
	}
	if v, ok := a.CustoDeReposicaoCents.Definido(); ok {
		validarCusto(erros, &v)
	}
	return erros
}

// GaleriaDoBem é o corpo de `PUT /inventory/items/{id}/photos`. A ordem do
// array é a ordem de exibição; o primeiro é a capa.
type GaleriaDoBem struct {
	MidiaIDs []uuid.UUID `json:"media_ids"`
}

func (g GaleriaDoBem) Validar() map[string]string {
	erros := map[string]string{}
	if g.MidiaIDs == nil {
		// `[]` é aceito (bem sem foto é fila de trabalho, não defeito);
		// ausente ou `null` não, porque não diz o que a galeria deve ser.
		erros["media_ids"] = "é obrigatório (mande [] para tirar todas as fotos)."
		return erros
	}
	if len(g.MidiaIDs) > maxFotosPorBem {
		erros["media_ids"] = fmt.Sprintf("no máximo %d fotos por bem.", maxFotosPorBem)
		return erros
	}
	vistos := map[uuid.UUID]int{}
	for i, id := range g.MidiaIDs {
		campo := fmt.Sprintf("media_ids[%d]", i)
		if id == uuid.Nil {
			erros[campo] = "identificador inválido."
			continue
		}
		if anterior, repetido := vistos[id]; repetido {
			// A chave é (item_id, media_id): deixar passar faria o INSERT
			// estourar 23505 no meio da transação, sem apontar o índice.
			erros[campo] = fmt.Sprintf("foto repetida — já está no índice %d.", anterior)
			continue
		}
		vistos[id] = i
	}
	return erros
}

// ColocacaoCriar é o corpo de `POST /inventory/placements`.
type ColocacaoCriar struct {
	AmbienteID  uuid.UUID `json:"room_id"`
	BemID       uuid.UUID `json:"item_id"`
	QtdEsperada *int      `json:"expected_qty"`
	Nota        *string   `json:"note"`
}

func (c ColocacaoCriar) Validar() map[string]string {
	erros := map[string]string{}
	if c.AmbienteID == uuid.Nil {
		erros["room_id"] = "é obrigatório."
	}
	if c.BemID == uuid.Nil {
		erros["item_id"] = "é obrigatório."
	}
	exigirQuantidade(erros, "expected_qty", c.QtdEsperada, 0)
	if c.Nota != nil {
		limitarTexto(erros, "note", *c.Nota, maxTextoLivre)
	}
	return erros
}

// ColocacaoSubstituir é o corpo de `PUT /inventory/placements/{id}`. Sem
// `room_id` nem `item_id`: eles SÃO a identidade da linha.
type ColocacaoSubstituir struct {
	QtdEsperada *int    `json:"expected_qty"`
	Nota        *string `json:"note"`
}

func (c ColocacaoSubstituir) Validar() map[string]string {
	erros := map[string]string{}
	exigirQuantidade(erros, "expected_qty", c.QtdEsperada, 0)
	if c.Nota != nil {
		limitarTexto(erros, "note", *c.Nota, maxTextoLivre)
	}
	return erros
}

// ColocacaoAtualizar é o corpo de `PATCH /inventory/placements/{id}`.
type ColocacaoAtualizar struct {
	QtdEsperada httpx.Opt[int]    `json:"expected_qty"`
	Nota        httpx.Opt[string] `json:"note"`
}

func (a ColocacaoAtualizar) Validar() map[string]string {
	erros := map[string]string{}
	naoNulo(erros, "expected_qty", a.QtdEsperada.DeveLimpar())
	if v, ok := a.QtdEsperada.Definido(); ok {
		exigirQuantidade(erros, "expected_qty", &v, 0)
	}
	if v, ok := a.Nota.Definido(); ok {
		limitarTexto(erros, "note", v, maxTextoLivre)
	}
	return erros
}

// PedidoDeCopia é o corpo de `POST /units/{id}/inventory/copy`.
type PedidoDeCopia struct {
	OrigemID  uuid.UUID `json:"source_unit_id"`
	SoComodos *bool     `json:"rooms_only"`
}

func (p PedidoDeCopia) Validar() map[string]string {
	if p.OrigemID == uuid.Nil {
		return map[string]string{"source_unit_id": "é obrigatório."}
	}
	return nil
}

// ConferenciaAbrir é o corpo de `POST /inventory/counts`.
type ConferenciaAbrir struct {
	UnidadeID uuid.UUID `json:"unit_id"`
	Nota      *string   `json:"note"`
}

func (c ConferenciaAbrir) Validar() map[string]string {
	erros := map[string]string{}
	if c.UnidadeID == uuid.Nil {
		erros["unit_id"] = "é obrigatório."
	}
	if c.Nota != nil {
		limitarTexto(erros, "note", *c.Nota, maxTextoLivre)
	}
	return erros
}

// ConferenciaSubstituir é o corpo do `PUT`: o único campo editável de uma
// conferência é a observação.
type ConferenciaSubstituir struct {
	Nota *string `json:"note"`
}

func (c ConferenciaSubstituir) Validar() map[string]string {
	erros := map[string]string{}
	if c.Nota != nil {
		limitarTexto(erros, "note", *c.Nota, maxTextoLivre)
	}
	return erros
}

// ConferenciaAtualizar é o corpo do `PATCH`.
type ConferenciaAtualizar struct {
	Nota httpx.Opt[string] `json:"note"`
}

func (a ConferenciaAtualizar) Validar() map[string]string {
	erros := map[string]string{}
	if v, ok := a.Nota.Definido(); ok {
		limitarTexto(erros, "note", v, maxTextoLivre)
	}
	return erros
}

// ContagemDaLinha é o gesto do celular. Nenhum campo é obrigatório, mas o
// corpo vazio é 422 (`minProperties: 1`): ausente não mexe — `{"note": …}`
// anota sem tocar na contagem —, e `counted_qty: null` DESFAZ a contagem.
type ContagemDaLinha struct {
	QtdContada httpx.Opt[int]    `json:"counted_qty"`
	Nota       httpx.Opt[string] `json:"note"`
}

func (c ContagemDaLinha) Validar() map[string]string {
	erros := map[string]string{}
	if !c.QtdContada.Set && !c.Nota.Set {
		erros["body"] = "mande ao menos um campo: counted_qty (null desfaz a contagem) ou note."
		return erros
	}
	if v, ok := c.QtdContada.Definido(); ok {
		exigirQuantidade(erros, "counted_qty", &v, 0)
	}
	if v, ok := c.Nota.Definido(); ok {
		limitarTexto(erros, "note", v, maxTextoLivre)
	}
	return erros
}

// PedidoDeFechamento é o corpo (opcional) de `POST /inventory/counts/{id}/close`.
type PedidoDeFechamento struct {
	Nota         httpx.Opt[string] `json:"note"`
	AbrirAvarias *bool             `json:"raise_issues"`
}

func (p PedidoDeFechamento) Validar() map[string]string {
	erros := map[string]string{}
	if v, ok := p.Nota.Definido(); ok {
		limitarTexto(erros, "note", v, maxTextoLivre)
	}
	return erros
}

// maxCodigoDaReserva é folga sobre o formato `WH-2026-0142`; o que passar
// disso não é código de reserva, é texto colado no campo errado.
const maxCodigoDaReserva = 40

// AvariaCriar é o corpo de `POST /inventory/issues`. A reserva vem por
// `reservation_id` OU `reservation_code` — o código é o que a governanta tem
// na folha do check-out, e trocá-lo por id exigiria `reservations:ver`.
type AvariaCriar struct {
	AmbienteID    uuid.UUID  `json:"room_id"`
	BemID         uuid.UUID  `json:"item_id"`
	Tipo          string     `json:"kind"`
	Qtd           *int       `json:"qty"`
	Nota          *string    `json:"note"`
	ReservaID     *uuid.UUID `json:"reservation_id"`
	ReservaCodigo *string    `json:"reservation_code"`
	ConferenciaID *uuid.UUID `json:"count_id"`
}

func (c AvariaCriar) Validar() map[string]string {
	erros := map[string]string{}
	if c.AmbienteID == uuid.Nil {
		erros["room_id"] = "é obrigatório."
	}
	if c.BemID == uuid.Nil {
		erros["item_id"] = "é obrigatório."
	}
	exigirDoVocabulario(erros, "kind", c.Tipo, tiposDeAvaria)
	exigirQuantidade(erros, "qty", c.Qtd, 1)
	if c.Nota != nil {
		limitarTexto(erros, "note", *c.Nota, maxTextoLivre)
	}
	validarReferenciaDaReserva(erros, c.ReservaID != nil, c.ReservaCodigo)
	return erros
}

// AvariaSubstituir é o corpo do `PUT`. Sem `room_id`/`item_id` (identidade do
// fato), sem `count_id` e sem `resolved_*` (fatos do servidor).
type AvariaSubstituir struct {
	Tipo          string     `json:"kind"`
	Qtd           *int       `json:"qty"`
	Nota          *string    `json:"note"`
	ReservaID     *uuid.UUID `json:"reservation_id"`
	ReservaCodigo *string    `json:"reservation_code"`
	Desfecho      *string    `json:"resolution"`
}

func (c AvariaSubstituir) Validar() map[string]string {
	erros := map[string]string{}
	exigirDoVocabulario(erros, "kind", c.Tipo, tiposDeAvaria)
	exigirQuantidade(erros, "qty", c.Qtd, 1)
	if c.Nota != nil {
		limitarTexto(erros, "note", *c.Nota, maxTextoLivre)
	}
	if c.Desfecho != nil {
		exigirDoVocabulario(erros, "resolution", *c.Desfecho, desfechos)
	}
	validarReferenciaDaReserva(erros, c.ReservaID != nil, c.ReservaCodigo)
	return erros
}

// AvariaAtualizar é o corpo do `PATCH`; `resolution: null` REABRE a pendência.
//
// `reservation_id` e `reservation_code` são duas portas para o MESMO vínculo:
// qualquer um dos dois em `null` desvincula, e mandar os dois — mesmo que um
// seja `null` — é 422, porque o pedido diz duas coisas sobre uma coluna só.
type AvariaAtualizar struct {
	Tipo          httpx.Opt[string]    `json:"kind"`
	Qtd           httpx.Opt[int]       `json:"qty"`
	Nota          httpx.Opt[string]    `json:"note"`
	ReservaID     httpx.Opt[uuid.UUID] `json:"reservation_id"`
	ReservaCodigo httpx.Opt[string]    `json:"reservation_code"`
	Desfecho      httpx.Opt[string]    `json:"resolution"`
}

func (a AvariaAtualizar) Validar() map[string]string {
	erros := map[string]string{}
	naoNulo(erros, "kind", a.Tipo.DeveLimpar())
	naoNulo(erros, "qty", a.Qtd.DeveLimpar())
	if v, ok := a.Tipo.Definido(); ok {
		exigirDoVocabulario(erros, "kind", v, tiposDeAvaria)
	}
	if v, ok := a.Qtd.Definido(); ok {
		exigirQuantidade(erros, "qty", &v, 1)
	}
	if v, ok := a.Nota.Definido(); ok {
		limitarTexto(erros, "note", v, maxTextoLivre)
	}
	if v, ok := a.ReservaID.Definido(); ok && v == uuid.Nil {
		erros["reservation_id"] = "identificador inválido."
	}
	if a.ReservaID.Set && a.ReservaCodigo.Set {
		erros["reservation_code"] = msgReservaPorDuasPortas
	} else if v, ok := a.ReservaCodigo.Definido(); ok {
		validarCodigoDaReserva(erros, v)
	}
	if v, ok := a.Desfecho.Definido(); ok {
		exigirDoVocabulario(erros, "resolution", v, desfechos)
	}
	return erros
}

const msgReservaPorDuasPortas = "mande reservation_id OU reservation_code, não os dois."

// validarReferenciaDaReserva é a regra do POST e do PUT, onde `null` e ausente
// dizem a mesma coisa ("sem reserva"): os dois preenchidos é 422.
func validarReferenciaDaReserva(erros map[string]string, temID bool, codigo *string) {
	if codigo == nil {
		return
	}
	if temID {
		erros["reservation_code"] = msgReservaPorDuasPortas
		return
	}
	validarCodigoDaReserva(erros, *codigo)
}

func validarCodigoDaReserva(erros map[string]string, codigo string) {
	n := utf8.RuneCountInString(strings.TrimSpace(codigo))
	switch {
	case n == 0:
		erros["reservation_code"] = "informe o código da reserva (ex.: WH-2026-0142) ou mande null."
	case n > maxCodigoDaReserva:
		erros["reservation_code"] = fmt.Sprintf("deve ter no máximo %d caracteres.", maxCodigoDaReserva)
	}
}

// NormalizarCodigoDaReserva apara e põe em caixa alta: o código é gerado
// maiúsculo (`proximo_codigo_reserva()`), e "wh-2026-0142" digitado no celular
// é a mesma reserva.
func NormalizarCodigoDaReserva(codigo string) string {
	return strings.ToUpper(strings.TrimSpace(codigo))
}

// ═══════════════════════════ Filtros ══════════════════════════════════════
//
// O que as listagens recebem já resolvido pelo handler: nada do que o cliente
// digitou entra no SQL a não ser como argumento ($n) ou como fragmento JÁ
// MAPEADO por uma whitelist de ordenação deste pacote.

// FiltroDeAmbientes é o `GET /rooms`.
type FiltroDeAmbientes struct {
	UnidadeID *uuid.UUID
	Tipo      string
	Ativo     *bool
	Busca     string
	Ordem     string
	Pagina    int
	PorPagina int
}

// FiltroDeBens é o `GET /inventory/items`.
type FiltroDeBens struct {
	Categoria  string
	Ativo      *bool
	Busca      string
	UnidadeID  *uuid.UUID
	AmbienteID *uuid.UUID
	TemFoto    *bool
	Ordem      string
	Pagina     int
	PorPagina  int
}

// FiltroDeColocacoes é o `GET /inventory/placements`.
type FiltroDeColocacoes struct {
	UnidadeID  *uuid.UUID
	AmbienteID *uuid.UUID
	BemID      *uuid.UUID
	Categoria  string
	Busca      string
	Ordem      string
	Pagina     int
	PorPagina  int
}

// FiltroDeConferencias é o `GET /inventory/counts`. `De`/`Ate` são datas
// AAAA-MM-DD no fuso da propriedade, resolvido no SQL.
type FiltroDeConferencias struct {
	UnidadeID *uuid.UUID
	Status    string
	De        string
	Ate       string
	Ordem     string
	Pagina    int
	PorPagina int
}

// FiltroDeAvarias é o `GET /inventory/issues`.
type FiltroDeAvarias struct {
	UnidadeID     *uuid.UUID
	AmbienteID    *uuid.UUID
	BemID         *uuid.UUID
	Tipo          string
	Aberta        *bool
	Desfecho      string
	ReservaID     *uuid.UUID
	ConferenciaID *uuid.UUID
	De            string
	Ate           string
	Ordem         string
	Pagina        int
	PorPagina     int
}

// FiltroDaUnidade é o `GET /units/{id}/inventory`.
type FiltroDaUnidade struct {
	IncluirInativos bool
	Categoria       string
	Busca           string
}

// FiltroDeUnidades é o `GET /inventory/units`.
type FiltroDeUnidades struct {
	Ativa     *bool
	Busca     string
	Pagina    int
	PorPagina int
}

// FiltroDeExportacao é o `GET /inventory/export`.
type FiltroDeExportacao struct {
	UnidadeID       *uuid.UUID
	AmbienteID      *uuid.UUID
	Categoria       string
	IncluirInativos bool
}

// ═══════════════════════════ Validação ════════════════════════════════════

// DoVocabulario diz se v é um dos valores aceitos.
func DoVocabulario(v string, vocabulario []string) bool {
	for _, aceito := range vocabulario {
		if v == aceito {
			return true
		}
	}
	return false
}

func exigirDoVocabulario(erros map[string]string, campo, v string, vocabulario []string) {
	if v == "" {
		erros[campo] = "é obrigatório."
		return
	}
	if !DoVocabulario(v, vocabulario) {
		erros[campo] = "deve ser um de: " + strings.Join(vocabulario, ", ") + "."
	}
}

// exigirNome mede o texto APARADO: "   " passaria por um `min=1` cru e só
// estouraria no CHECK `btrim(name) <> ”`, como 422 sem nome de campo.
func exigirNome(erros map[string]string, campo, v string, max int) {
	n := utf8.RuneCountInString(strings.TrimSpace(v))
	switch {
	case n == 0:
		erros[campo] = "é obrigatório."
	case n > max:
		erros[campo] = fmt.Sprintf("deve ter no máximo %d caracteres.", max)
	}
}

func limitarTexto(erros map[string]string, campo, v string, max int) {
	if utf8.RuneCountInString(strings.TrimSpace(v)) > max {
		erros[campo] = fmt.Sprintf("deve ter no máximo %d caracteres.", max)
	}
}

func naoNulo(erros map[string]string, campo string, nulo bool) {
	if nulo {
		erros[campo] = "não pode ser nulo."
	}
}

func exigirQuantidade(erros map[string]string, campo string, v *int, min int) {
	switch {
	case v == nil:
		erros[campo] = "é obrigatório."
	case *v < min && min == 0:
		erros[campo] = "não pode ser negativa."
	case *v < min:
		erros[campo] = fmt.Sprintf("deve ser no mínimo %d.", min)
	case *v > maxInteiro:
		erros[campo] = fmt.Sprintf("deve ser no máximo %d.", maxInteiro)
	}
}

func limitarInteiro(erros map[string]string, campo string, v *int, min int) {
	if v != nil && (*v < min || *v > maxInteiro) {
		erros[campo] = "fora da faixa aceita."
	}
}

// validarCusto recusa zero: `null` é "não cotado", e zero seria um segundo
// jeito, errado, de escrever "não sei" (CHECK inventory_items_custo_positivo).
func validarCusto(erros map[string]string, v *int64) {
	if v != nil && *v <= 0 {
		erros["replacement_cost_cents"] = "deve ser maior que zero, em centavos (use null para \"não cotado\")."
	}
}

// ═══════════════════════════ Normalização ═════════════════════════════════

// textoOuNulo trata texto em branco como ausência: numa coluna anulável, "" e
// NULL dizem a mesma coisa, e gravar as duas formas daria dois jeitos de dizer
// "sem valor".
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

// textoDoOpt resolve um Opt de coluna anulável contra o valor atual: ausente
// mantém, `null` limpa, valor grava (aparado; em branco vira NULL).
func textoDoOpt(o httpx.Opt[string], atual *string) *string {
	if !o.Set {
		return atual
	}
	if v, ok := o.Definido(); ok {
		return textoOuNulo(&v)
	}
	return nil
}

func valorOu[T any](p *T, padrao T) T {
	if p == nil {
		return padrao
	}
	return *p
}
