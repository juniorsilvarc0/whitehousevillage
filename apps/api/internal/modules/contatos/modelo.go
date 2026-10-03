package contatos

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/pii"
)

// Contato é o schema `Contato` do contrato.
//
// Datas de estadia e nascimento viajam como STRING `YYYY-MM-DD`, e não como
// time.Time: `birth_date` é `date` no banco (dia civil, sem instante), e
// serializar um time.Time devolveria "1990-05-02T00:00:00Z" — que é um instante
// UTC e, lido em America/Fortaleza, vira 01/05 às 21h. É a mesma decisão que
// `reservas` e `disponibilidade` já tomaram para check-in/check-out.
type Contato struct {
	ID              uuid.UUID  `json:"id"`
	Nome            string     `json:"name"`
	Email           *string    `json:"email"`
	Telefone        *string    `json:"phone_e164"`
	TipoDeDocumento *string    `json:"doc_type"`
	Documento       *string    `json:"doc_number"`
	Nascimento      *string    `json:"birth_date"`
	Cidade          *string    `json:"city"`
	Estado          *string    `json:"state"`
	Notas           *string    `json:"notes"`
	BaseLegal       *string    `json:"lgpd_basis"`
	OptInMarketing  bool       `json:"marketing_opt_in"`
	ConsentimentoEm *time.Time `json:"consent_at"`
	AnonimizadoEm   *time.Time `json:"anonymized_at"`
	CriadoEm        time.Time  `json:"created_at"`
	AtualizadoEm    time.Time  `json:"updated_at"`
}

// ContatoNaLista é o schema `ContatoNaLista`: a linha de `GET /contacts`.
//
// Documento, telefone e e-mail saem MASCARADOS (pii.Mascarar*), e
// `birth_date` e `notes` não existem como chave — nem `null`. Data de
// nascimento ajuda a reidentificar quem tem o nome, e texto livre não tem
// máscara possível. É um tipo próprio, e não um `Contato` com campos
// zerados, para que esquecer de mascarar seja erro de compilação: não há
// como devolver a ficha cheia por este tipo.
//
// Até 02/10/2026 a lista servia a ficha inteira sem rastro (F2-23: 11 CPFs
// completos para um corretor, `pii_access_log` parado em 43). A regra agora é
// do contrato: coleção mascara e não grava; a ficha devolve cheio e grava.
type ContatoNaLista struct {
	ID              uuid.UUID  `json:"id"`
	Nome            string     `json:"name"`
	Email           *string    `json:"email"`
	Telefone        *string    `json:"phone_e164"`
	TipoDeDocumento *string    `json:"doc_type"`
	Documento       *string    `json:"doc_number"`
	Cidade          *string    `json:"city"`
	Estado          *string    `json:"state"`
	BaseLegal       *string    `json:"lgpd_basis"`
	OptInMarketing  bool       `json:"marketing_opt_in"`
	ConsentimentoEm *time.Time `json:"consent_at"`
	AnonimizadoEm   *time.Time `json:"anonymized_at"`
	CriadoEm        time.Time  `json:"created_at"`
}

// NaLista é a única porta de `Contato` para `ContatoNaLista`.
func NaLista(c Contato) ContatoNaLista {
	return ContatoNaLista{
		ID:              c.ID,
		Nome:            c.Nome,
		Email:           pii.MascararEmail(c.Email),
		Telefone:        pii.MascararTelefone(c.Telefone),
		TipoDeDocumento: c.TipoDeDocumento,
		Documento:       pii.MascararDocumento(c.TipoDeDocumento, c.Documento),
		Cidade:          c.Cidade,
		Estado:          c.Estado,
		BaseLegal:       c.BaseLegal,
		OptInMarketing:  c.OptInMarketing,
		ConsentimentoEm: c.ConsentimentoEm,
		AnonimizadoEm:   c.AnonimizadoEm,
		CriadoEm:        c.CriadoEm,
	}
}

// Anonimizado diz se a ficha já foi esvaziada.
func (c Contato) Anonimizado() bool { return c.AnonimizadoEm != nil }

// Vinculos conta quantos registros apontam para o contato, por tipo. É o que o
// `DELETE` devolve em `details.references` ao recusar e o que o `/anonymize`
// devolve em `preserved` — a prova, na resposta, de que a venda não foi tocada.
//
// Os dois últimos campos NÃO estão no schema `VinculosDoContato` do contrato e
// estão aqui porque a contagem precisa ser COMPLETA para o `DELETE` ser
// honesto: `reservation_guests` e `quotes` também têm FK para `contacts`, e um
// contato que só é acompanhante de uma estadia passaria pelas seis contagens do
// contrato com zero em todas — o `DELETE` seguiria e o banco responderia 23503,
// que o MapError traduz para 422 genérico. O usuário receberia "valor
// inválido" onde o contrato promete `409 RESOURCE_IN_USE` com a contagem.
//
// PARA O INTEGRADOR: `VinculosDoContato` no openapi.yaml precisa dos dois
// campos. Acrescentá-los é aditivo (o schema não fecha `additionalProperties`),
// então a resposta de hoje já é válida contra o contrato — mas o contrato
// descreve menos do que a API entrega, e isso é dívida.
type Vinculos struct {
	Reservas      int `json:"reservations"`
	Leads         int `json:"leads"`
	Oportunidades int `json:"opportunities"`
	Atividades    int `json:"activities"`
	Notas         int `json:"notes"`

	// Conversas é sempre 0 hoje: a tabela de conversas do WhatsApp não existe
	// no schema (nenhuma migration a cria). O campo fica porque o contrato o
	// declara, e devolvê-lo ausente faria o painel distinguir "zero conversas"
	// de "não sei" sem ter como. Vira consulta de verdade no dia em que o
	// módulo de chat nascer.
	Conversas int `json:"conversations"`

	Hospedes   int `json:"reservation_guests"`
	Orcamentos int `json:"quotes"`

	// Corretores conta o cadastro de corretor (`brokers.contact_id`, FK
	// RESTRICT desde 20261002180000). Sem ele, DELETE da ficha de um corretor
	// estourava 23503 em brokers_contact_id_fkey e saía 422 genérico no lugar
	// do 409 RESOURCE_IN_USE com a contagem. Fora do schema VinculosDoContato
	// por enquanto, como os dois de cima — ver o relatório.
	Corretores int `json:"brokers"`
}

// Total é quantos registros de qualquer tipo apontam para o contato. Zero é a
// única condição em que o `DELETE` pode seguir.
func (v Vinculos) Total() int {
	return v.Reservas + v.Leads + v.Oportunidades + v.Atividades +
		v.Notas + v.Conversas + v.Hospedes + v.Orcamentos + v.Corretores
}

// ContatoCompleto é o schema `ContatoCompleto`: a ficha mais as contagens.
//
// `references` não aparece na LISTA de propósito — contá-lo é uma consulta por
// contato, e numa página de 25 seriam 25 contagens para desenhar um número que
// a lista não mostra.
type ContatoCompleto struct {
	Contato
	Vinculos Vinculos `json:"references"`
}

// ResultadoDeAnonimizacao é o corpo de `POST /contacts/{id}/anonymize`.
type ResultadoDeAnonimizacao struct {
	Contato Contato `json:"contact"`

	// Preservado é o que CONTINUA existindo apontando para este id. Está na
	// resposta porque quem anonimiza precisa ver, na hora, que a reserva e o
	// razão não foram tocados — é a diferença entre "eliminei o dado pessoal" e
	// "apaguei a venda".
	Preservado Vinculos `json:"preserved"`
}

// Consentimento isola o eixo de marketing, que é o que o titular mais pergunta.
type Consentimento struct {
	BaseLegal       *string    `json:"lgpd_basis"`
	OptInMarketing  bool       `json:"marketing_opt_in"`
	ConsentimentoEm *time.Time `json:"consent_at"`
}

// Exportacao é o pacote de portabilidade (LGPD art. 18, V).
//
// As quatro coleções chegam como JSON já montado pelo Postgres
// (`jsonb_agg`), e não como structs Go. Duas razões:
//
//   - o pacote precisa carregar reserva COM noites e valores congelados, lead,
//     oportunidade e atividade. Redeclarar essas quatro entidades aqui criaria
//     uma segunda definição de cada uma, que envelhece separada da de
//     `reservas` e `crm` — e o campo que sumir do pacote de portabilidade suma
//     em silêncio;
//   - importar `reservas` e `crm` para reusar os DTOs traria junto o escopo e
//     as regras de listagem deles, que aqui não valem: a portabilidade é do
//     TITULAR, não do corretor que está logado, e filtrar por dono devolveria
//     ao titular menos do que existe sobre ele.
type Exportacao struct {
	ExportadoEm   time.Time       `json:"exported_at"`
	Contato       ContatoCompleto `json:"contact"`
	Consentimento Consentimento   `json:"consent"`
	Reservas      json.RawMessage `json:"reservations"`
	Leads         json.RawMessage `json:"leads"`
	Oportunidades json.RawMessage `json:"opportunities"`
	Atividades    json.RawMessage `json:"activities"`
}
