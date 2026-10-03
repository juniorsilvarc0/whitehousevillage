package site

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ─────────────────────────── Pedidos ────────────────────────────────────────

// PedidoGravar é o corpo de PUT /site/content/{key}.
//
// O valor chega cru porque a forma depende do TIPO do campo, e o tipo vem do
// catálogo (pela chave da rota), não do corpo. O service decodifica cada tipo
// numa struct própria (valor.go) — nada de mapa genérico.
type PedidoGravar struct {
	Value json.RawMessage `json:"value"`
}

// Validar só exige a presença; a forma é validada contra o catálogo.
func (p PedidoGravar) Validar() map[string]string {
	if ehNulo(p.Value) {
		return map[string]string{"value": "Preencha o campo. Para voltar ao texto original, use “Restaurar original”."}
	}
	return nil
}

// ─────────────────────────── Respostas ──────────────────────────────────────

// SubcampoResposta descreve uma coluna de item de lista.
type SubcampoResposta struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Kind  Tipo   `json:"kind"`
	Max   int    `json:"max"`
}

// CampoResposta é um campo do catálogo com o valor atual.
type CampoResposta struct {
	Key          string             `json:"key"`
	Label        string             `json:"label"`
	Kind         Tipo               `json:"kind"`
	Help         string             `json:"help"`
	Max          int                `json:"max"`
	Value        json.RawMessage    `json:"value"`
	DefaultValue json.RawMessage    `json:"default_value"`
	IsDefault    bool               `json:"is_default"`
	ItemFields   []SubcampoResposta `json:"item_fields,omitempty"`
	// UpdatedAt é nulo enquanto o campo estiver com o texto original.
	UpdatedAt *time.Time `json:"updated_at"`
}

// SecaoResposta agrupa campos.
type SecaoResposta struct {
	Key    string          `json:"key"`
	Label  string          `json:"label"`
	Fields []CampoResposta `json:"fields"`
}

// ConteudoResposta é o GET /site/content.
type ConteudoResposta struct {
	Sections []SecaoResposta `json:"sections"`
}

// PublicoResposta é o GET /public/site: só o que foi editado.
type PublicoResposta struct {
	Values map[string]json.RawMessage `json:"values"`
}

// MidiaResposta é o POST /site/media.
type MidiaResposta struct {
	ID    uuid.UUID `json:"id"`
	Kind  string    `json:"kind"`
	Mime  string    `json:"mime"`
	Bytes int64     `json:"bytes"`
	URL   string    `json:"url"`
}
