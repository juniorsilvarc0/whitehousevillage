package contatos

import (
	"slices"
	"strings"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Criar é o corpo de `POST /contacts` e de `PUT /contacts/{id}` (schema
// ContatoCriar).
//
// `name` é o único obrigatório. Telefone opcional é decisão de produto, não
// esquecimento: o lead que chega por formulário às vezes só deixa o e-mail, e
// exigir telefone empurraria a operação a inventar `+5500000000000` — que a
// `UNIQUE` recusaria no segundo lead sem telefone. O índice é PARCIAL
// justamente para `NULL` não colidir com `NULL`.
type Criar struct {
	Nome            string     `json:"name" validate:"required,min=2,max=200"`
	Email           *string    `json:"email"`
	Telefone        *string    `json:"phone_e164"`
	TipoDeDocumento *string    `json:"doc_type"`
	Documento       *string    `json:"doc_number"`
	Nascimento      *string    `json:"birth_date"`
	Cidade          *string    `json:"city"`
	Estado          *string    `json:"state"`
	Notas           *string    `json:"notes"`
	BaseLegal       *string    `json:"lgpd_basis"`
	OptInMarketing  *bool      `json:"marketing_opt_in"`
	ConsentimentoEm *time.Time `json:"consent_at"`
}

// Normalizar aplica, in loco, tudo que a gravação precisa: apara texto,
// canoniza telefone e documento, esvazia string vazia para NULL.
//
// Roda no SERVICE, e não no Validar: `httpx.Decode` chama o gancho de validação
// com uma CÓPIA (receptor por valor), então normalizar lá não chegaria ao
// repositório. Validar confere a forma; Normalizar decide o que vai para a
// coluna. Os dois usam as MESMAS funções, senão "passou na validação" e "foi
// gravado" divergem.
func (c *Criar) Normalizar() {
	c.Nome = strings.Join(strings.Fields(c.Nome), " ")
	c.Email = minusculaOuNulo(c.Email)
	c.Cidade = aparadoOuNulo(c.Cidade)
	c.Notas = aparadoOuNulo(c.Notas)
	c.BaseLegal = aparadoOuNulo(c.BaseLegal)
	c.Nascimento = aparadoOuNulo(c.Nascimento)
	c.TipoDeDocumento = aparadoOuNulo(c.TipoDeDocumento)

	if c.Estado != nil {
		uf := strings.ToUpper(strings.TrimSpace(*c.Estado))
		c.Estado = nuloSeVazio(uf)
	}
	if c.Telefone != nil {
		if e164, err := NormalizarTelefone(*c.Telefone); err == nil {
			c.Telefone = nuloSeVazio(e164)
		}
	}
	if c.Documento != nil && c.TipoDeDocumento != nil {
		if doc, err := NormalizarDocumento(*c.TipoDeDocumento, *c.Documento); err == nil {
			c.Documento = nuloSeVazio(doc)
		}
	} else if c.Documento != nil {
		c.Documento = aparadoOuNulo(c.Documento)
	}

	// Tipo sem número não identifica ninguém e ainda entra na chave de
	// deduplicação por documento como metade de uma chave. Some junto.
	if c.Documento == nil {
		c.TipoDeDocumento = nil
	}
	// Opt-in desligado não carrega data de aceite: data sobrevivente faria o
	// relatório dizer que a pessoa aceitou.
	if !c.OptInOuPadrao() {
		c.ConsentimentoEm = nil
	}
}

// OptInOuPadrao aplica o default `false` do contrato.
func (c Criar) OptInOuPadrao() bool { return c.OptInMarketing != nil && *c.OptInMarketing }

// Validar cobre o que as tags não expressam: formato de telefone e documento,
// enums e a amarração entre opt-in e data de consentimento.
func (c Criar) Validar() map[string]string {
	erros := map[string]string{}

	validarEmail(erros, c.Email)
	validarTelefone(erros, c.Telefone)
	validarDocumento(erros, c.TipoDeDocumento, c.Documento)
	validarNascimento(erros, c.Nascimento)
	validarEstado(erros, c.Estado)
	validarNotas(erros, c.Notas)
	validarBaseLegal(erros, c.BaseLegal)

	// Consentimento sem data não é consentimento, é afirmação. É a única regra
	// da LGPD que o schema sozinho não consegue expressar, e por isso ela é
	// checada aqui e repetida no PATCH.
	if c.OptInOuPadrao() && c.ConsentimentoEm == nil {
		erros["consent_at"] = "é obrigatório quando marketing_opt_in é true."
	}
	return erros
}

// Atualizar é o corpo do `PATCH` (schema ContatoAtualizar).
//
// Todo campo é Opt porque o PATCH separa três coisas que um ponteiro só separa
// duas: ausente (não mexe), `null` (limpa) e valor (grava).
type Atualizar struct {
	Nome            httpx.Opt[string]    `json:"name"`
	Email           httpx.Opt[string]    `json:"email"`
	Telefone        httpx.Opt[string]    `json:"phone_e164"`
	TipoDeDocumento httpx.Opt[string]    `json:"doc_type"`
	Documento       httpx.Opt[string]    `json:"doc_number"`
	Nascimento      httpx.Opt[string]    `json:"birth_date"`
	Cidade          httpx.Opt[string]    `json:"city"`
	Estado          httpx.Opt[string]    `json:"state"`
	Notas           httpx.Opt[string]    `json:"notes"`
	BaseLegal       httpx.Opt[string]    `json:"lgpd_basis"`
	OptInMarketing  httpx.Opt[bool]      `json:"marketing_opt_in"`
	ConsentimentoEm httpx.Opt[time.Time] `json:"consent_at"`
}

// Normalizar apara e canoniza os campos presentes. String em branco é tratada
// como pedido de limpeza, igual ao `null`: obrigar o painel a saber a diferença
// entre `""` e `null` para apagar um telefone é armadilha.
func (a *Atualizar) Normalizar() {
	a.Nome = mapearOpt(a.Nome, func(v string) string { return strings.Join(strings.Fields(v), " ") })
	a.Email = limparSeBranco(mapearOpt(a.Email, func(v string) string {
		return strings.ToLower(strings.TrimSpace(v))
	}))
	a.Cidade = limparSeBranco(mapearOpt(a.Cidade, strings.TrimSpace))
	a.Notas = limparSeBranco(mapearOpt(a.Notas, strings.TrimSpace))
	a.BaseLegal = limparSeBranco(mapearOpt(a.BaseLegal, strings.TrimSpace))
	a.Nascimento = limparSeBranco(mapearOpt(a.Nascimento, strings.TrimSpace))
	a.TipoDeDocumento = limparSeBranco(mapearOpt(a.TipoDeDocumento, strings.TrimSpace))
	a.Estado = limparSeBranco(mapearOpt(a.Estado, func(v string) string {
		return strings.ToUpper(strings.TrimSpace(v))
	}))

	if v, ok := a.Telefone.Definido(); ok {
		if e164, err := NormalizarTelefone(v); err == nil {
			a.Telefone = limparSeBranco(httpx.De(e164))
		}
	}
	if v, ok := a.Documento.Definido(); ok {
		if tipo, temTipo := a.TipoDeDocumento.Definido(); temTipo {
			if doc, err := NormalizarDocumento(tipo, v); err == nil {
				a.Documento = limparSeBranco(httpx.De(doc))
			}
		} else {
			a.Documento = limparSeBranco(mapearOpt(a.Documento, strings.TrimSpace))
		}
	}

	// Revogar o opt-in limpa `consent_at` na MESMA escrita. Sem isto a
	// revogação precisaria de duas chamadas, e quem esquecesse a segunda
	// deixaria uma data de aceite viva numa ficha que recusou marketing.
	if v, ok := a.OptInMarketing.Definido(); ok && !v {
		a.ConsentimentoEm = httpx.Nulo[time.Time]()
	}
}

// Validar aplica as mesmas regras do POST aos campos presentes, mais a regra
// que só o PATCH tem: campo que a coluna exige NOT NULL não aceita `null`.
//
// `consentimentoAtual` é o que já está gravado — é como "ligar o opt-in sem
// mandar consent_at, porque ele já existe na ficha" continua sendo aceito sem
// abrir a porta para opt-in sem data nenhuma.
func (a Atualizar) Validar() map[string]string {
	erros := map[string]string{}

	if a.Nome.DeveLimpar() {
		erros["name"] = "não pode ser nulo."
	} else if v, ok := a.Nome.Definido(); ok && !httpx.ValidarValor(v, "min=2,max=200") {
		erros["name"] = "deve ter entre 2 e 200 caracteres."
	}
	if a.OptInMarketing.DeveLimpar() {
		erros["marketing_opt_in"] = "não pode ser nulo."
	}

	if v, ok := a.Email.Definido(); ok {
		validarEmail(erros, &v)
	}
	if v, ok := a.Telefone.Definido(); ok {
		validarTelefone(erros, &v)
	}
	if v, ok := a.Nascimento.Definido(); ok {
		validarNascimento(erros, &v)
	}
	if v, ok := a.Estado.Definido(); ok {
		validarEstado(erros, &v)
	}
	if v, ok := a.Notas.Definido(); ok {
		validarNotas(erros, &v)
	}
	if v, ok := a.BaseLegal.Definido(); ok {
		validarBaseLegal(erros, &v)
	}

	tipo, temTipo := a.TipoDeDocumento.Definido()
	doc, temDoc := a.Documento.Definido()
	switch {
	case temDoc && temTipo:
		validarDocumento(erros, &tipo, &doc)
	case temDoc && !temTipo:
		// Sem o tipo no mesmo corpo não há como validar nem comparar o número:
		// "12345678909" é CPF ou passaporte conforme o que a ficha já dizia, e
		// resolver isso lendo o banco tornaria a validação dependente de estado.
		erros["doc_type"] = "informe o tipo do documento junto com o número."
	case temTipo && !temDoc:
		validarDocumento(erros, &tipo, nil)
	}

	return erros
}

// ExigeConsentimento diz se este PATCH liga o opt-in de marketing. Quem chama
// (o service) confere se há data — no corpo ou já gravada.
func (a Atualizar) ExigeConsentimento() bool {
	v, ok := a.OptInMarketing.Definido()
	return ok && v
}

// PedidoDeAnonimizacao é o corpo de `POST /contacts/{id}/anonymize`.
//
// `reason` é obrigatório porque a ação é IRREVERSÍVEL: não há dado guardado em
// lugar nenhum para desfazê-la. É o que responde "por que esta ficha está
// vazia?" seis meses depois, e é o registro do pedido do titular que a ANPD
// pede em fiscalização.
type PedidoDeAnonimizacao struct {
	Motivo string `json:"reason" validate:"required,min=3,max=500"`
}

// Normalizar apara o motivo.
func (p *PedidoDeAnonimizacao) Normalizar() { p.Motivo = strings.TrimSpace(p.Motivo) }

// Validar pega o motivo que é só espaço em branco — que passa no `min=3` da tag
// e não explica nada a ninguém.
func (p PedidoDeAnonimizacao) Validar() map[string]string {
	if len(strings.TrimSpace(p.Motivo)) < 3 {
		return map[string]string{"reason": "descreva o motivo da anonimização (mínimo 3 caracteres)."}
	}
	return nil
}

// Filtro é a query de `GET /contacts` já resolvida.
type Filtro struct {
	Busca     string
	Telefone  string // já em E.164; vazio = não filtra
	Documento string
	OptIn     *bool

	// IncluirAnonimizados é `false` por padrão de propósito: a ficha
	// anonimizada existe só para sustentar a reserva antiga, e oferecê-la num
	// seletor de negócio novo é reintroduzir na operação o dado que o titular
	// mandou eliminar.
	IncluirAnonimizados bool

	OrderBy   string
	Pagina    int
	PorPagina int
}

// ─────────────────────────── Validações compartilhadas ──────────────

func validarEmail(erros map[string]string, v *string) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return
	}
	e := strings.TrimSpace(*v)
	// `*` é a máscara que GET /contacts devolve (`f***@gmail.com`), e o
	// validador de formato a APROVA — a RFC aceita `*` antes do `@`. Sem esta
	// recusa, o formulário preenchido a partir da lista gravaria a máscara por
	// cima do endereço verdadeiro, com 200. Nenhum cadastro real deste negócio
	// usa `*` no e-mail.
	if strings.Contains(e, "*") {
		erros["email"] = "e-mail mascarado não é aceito: preencha o formulário pela ficha (GET /contacts/{id})."
		return
	}
	if !httpx.ValidarValor(e, "email,max=254") {
		erros["email"] = "e-mail inválido."
	}
}

func validarTelefone(erros map[string]string, v *string) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return
	}
	if _, err := NormalizarTelefone(*v); err != nil {
		erros["phone_e164"] = "informe o telefone em E.164 (+5585999990000) ou como número nacional com DDD."
	}
}

func validarDocumento(erros map[string]string, tipo, numero *string) {
	if tipo != nil && strings.TrimSpace(*tipo) != "" && !slices.Contains(TiposDeDocumento, strings.TrimSpace(*tipo)) {
		erros["doc_type"] = "deve ser um de: cpf, cnpj, passaporte."
		return
	}
	if numero == nil || strings.TrimSpace(*numero) == "" {
		return
	}
	if tipo == nil || strings.TrimSpace(*tipo) == "" {
		erros["doc_type"] = "informe o tipo do documento junto com o número."
		return
	}
	if _, err := NormalizarDocumento(strings.TrimSpace(*tipo), *numero); err != nil {
		erros["doc_number"] = "documento inválido para o tipo informado."
	}
}

func validarNascimento(erros map[string]string, v *string) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return
	}
	d, err := time.Parse(time.DateOnly, strings.TrimSpace(*v))
	if err != nil {
		erros["birth_date"] = "use o formato AAAA-MM-DD."
		return
	}
	// Data de nascimento no futuro é erro de digitação, sempre. A comparação é
	// contra o dia UTC porque a folga de um fuso não muda o diagnóstico: quem
	// erra aqui erra por anos, não por horas.
	if d.After(time.Now().UTC().AddDate(0, 0, 1)) {
		erros["birth_date"] = "não pode estar no futuro."
	}
}

func validarEstado(erros map[string]string, v *string) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return
	}
	uf := strings.ToUpper(strings.TrimSpace(*v))
	if len(uf) != 2 || !alfanumerico(uf) {
		erros["state"] = "use a sigla de duas letras (CE)."
	}
}

func validarNotas(erros map[string]string, v *string) {
	if v != nil && len([]rune(*v)) > 2000 {
		erros["notes"] = "deve ter no máximo 2000 caracteres."
	}
}

func validarBaseLegal(erros map[string]string, v *string) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return
	}
	if !slices.Contains(BasesLegais, strings.TrimSpace(*v)) {
		erros["lgpd_basis"] = "deve ser um de: consentimento, contrato, obrigacao_legal, legitimo_interesse."
	}
}

// ─────────────────────────── Auxiliares ─────────────────────────────

func nuloSeVazio(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func aparadoOuNulo(p *string) *string {
	if p == nil {
		return nil
	}
	return nuloSeVazio(strings.TrimSpace(*p))
}

func minusculaOuNulo(p *string) *string {
	if p == nil {
		return nil
	}
	return nuloSeVazio(strings.ToLower(strings.TrimSpace(*p)))
}

func mapearOpt(o httpx.Opt[string], f func(string) string) httpx.Opt[string] {
	if v, ok := o.Definido(); ok {
		return httpx.De(f(v))
	}
	return o
}

// limparSeBranco transforma valor em branco no `null` explícito.
func limparSeBranco(o httpx.Opt[string]) httpx.Opt[string] {
	if v, ok := o.Definido(); ok && strings.TrimSpace(v) == "" {
		return httpx.Nulo[string]()
	}
	return o
}
