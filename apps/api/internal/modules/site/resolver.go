package site

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Resolver transforma o valor GRAVADO (ids de mídia) no valor que se MOSTRA
// (endereços). Dois públicos, dois recortes:
//
//   - painel: {media_id, url, alt} — o id para regravar, a url para a prévia;
//   - site:   {url, alt}           — o site não precisa saber de id nenhum.

// PrefixoDaMidiaPublica é o caminho público de um arquivo enviado. O nginx do
// site tem um `location` próprio para ele (cache longo, sem no-store).
const PrefixoDaMidiaPublica = "/api/v1/public/media/"

// URLDaMidia monta o endereço público do arquivo.
func URLDaMidia(id uuid.UUID) string { return PrefixoDaMidiaPublica + id.String() }

// MidiaResolvida é a mídia como o painel recebe. media_id nulo = a mídia
// original do site (servida pelo próprio site, ex. /assets/logo.png).
type MidiaResolvida struct {
	MediaID *uuid.UUID `json:"media_id"`
	URL     string     `json:"url"`
	Alt     *string    `json:"alt,omitempty"`
}

// MidiaPublica é a mídia como o site recebe.
type MidiaPublica struct {
	URL string  `json:"url"`
	Alt *string `json:"alt,omitempty"`
}

var jsonNulo = json.RawMessage("null")

func ehNulo(b json.RawMessage) bool {
	t := bytes.TrimSpace(b)
	return len(t) == 0 || bytes.Equal(t, jsonNulo)
}

// resolverGravado resolve um valor de site_content.
func (c Campo) resolverGravado(gravado json.RawMessage, publico bool) (json.RawMessage, error) {
	switch c.Tipo {
	case TipoImagem:
		return resolverImagem(gravado, publico)
	case TipoVideo:
		var v valorVideo
		if err := json.Unmarshal(gravado, &v); err != nil {
			return nil, fmt.Errorf("site: vídeo gravado ilegível em %s: %w", c.Chave, err)
		}
		if publico {
			return jsonDe(MidiaPublica{URL: URLDaMidia(v.MediaID)}), nil
		}
		id := v.MediaID
		return jsonDe(MidiaResolvida{MediaID: &id, URL: URLDaMidia(id)}), nil
	case TipoLista:
		return c.resolverLista(gravado, publico)
	default:
		return gravado, nil
	}
}

func resolverImagem(gravado json.RawMessage, publico bool) (json.RawMessage, error) {
	if ehNulo(gravado) {
		return jsonNulo, nil
	}
	var v valorImagem
	if err := json.Unmarshal(gravado, &v); err != nil {
		return nil, fmt.Errorf("site: imagem gravada ilegível: %w", err)
	}
	alt := v.Alt
	if publico {
		return jsonDe(MidiaPublica{URL: URLDaMidia(v.MediaID), Alt: &alt}), nil
	}
	id := v.MediaID
	return jsonDe(MidiaResolvida{MediaID: &id, URL: URLDaMidia(id), Alt: &alt}), nil
}

func (c Campo) resolverLista(gravado json.RawMessage, publico bool) (json.RawMessage, error) {
	var itens []map[string]json.RawMessage
	if err := json.Unmarshal(gravado, &itens); err != nil {
		return nil, fmt.Errorf("site: lista gravada ilegível em %s: %w", c.Chave, err)
	}
	if itens == nil {
		itens = []map[string]json.RawMessage{}
	}
	for _, item := range itens {
		for _, s := range c.Itens {
			if s.Tipo != TipoImagem {
				continue
			}
			v, ok := item[s.Chave]
			if !ok {
				continue
			}
			r, err := resolverImagem(v, publico)
			if err != nil {
				return nil, fmt.Errorf("site: %s.%s: %w", c.Chave, s.Chave, err)
			}
			item[s.Chave] = r
		}
	}
	return jsonDe(itens), nil
}

// resolverOriginal resolve o valor original para o painel. Mídia original
// ganha media_id nulo: é o arquivo do próprio site, não um envio.
func (c Campo) resolverOriginal() (json.RawMessage, error) {
	switch c.Tipo {
	case TipoImagem, TipoVideo:
		if ehNulo(c.Original) {
			return jsonNulo, nil
		}
		var o midiaOriginal
		if err := json.Unmarshal(c.Original, &o); err != nil {
			return nil, fmt.Errorf("site: original ilegível em %s: %w", c.Chave, err)
		}
		r := MidiaResolvida{URL: o.URL}
		if c.Tipo == TipoImagem {
			alt := ""
			if o.Alt != nil {
				alt = *o.Alt
			}
			r.Alt = &alt
		}
		return jsonDe(r), nil
	default:
		// Texto e lista: o original já está na forma de exibição (as listas
		// originais não têm foto).
		return c.Original, nil
	}
}
