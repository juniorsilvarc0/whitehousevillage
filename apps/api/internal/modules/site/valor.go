package site

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// O valor de cada tipo, como ele é gravado em site_content.value. A forma é
// validada AQUI, contra o catálogo — o banco só garante que há valor.

// valorImagem é o que se grava num campo (ou subcampo) imagem.
type valorImagem struct {
	MediaID uuid.UUID `json:"media_id"`
	Alt     string    `json:"alt"`
}

// valorVideo é o que se grava num campo vídeo.
type valorVideo struct {
	MediaID uuid.UUID `json:"media_id"`
}

// entradaDeMidia é o que o painel pode mandar. `url` é tolerada e descartada:
// o painel recebe a mídia resolvida ({media_id, url, alt}) e devolver o mesmo
// objeto não pode ser erro — mas a url nunca é gravada, ela sai do id.
type entradaDeMidia struct {
	MediaID *uuid.UUID `json:"media_id"`
	Alt     *string    `json:"alt"`
	URL     *string    `json:"url"`
}

// refMidia é uma mídia citada pelo valor, com o tipo que o campo exige. O
// service confere no banco que ela existe e é do tipo certo.
type refMidia struct {
	ID      uuid.UUID
	Tipo    Tipo
	Caminho string // chave em details ("value", "value[2].foto")
}

// valorValidado é o resultado de validar: o JSON normalizado que vai para o
// banco e as mídias que ele cita.
type valorValidado struct {
	JSON   json.RawMessage
	Midias []refMidia
}

const caminhoRaiz = "value"

// validar confere o valor bruto contra o tipo do campo. details em linguagem
// do gestor: são exibidos ao lado do campo no painel.
func (c Campo) validar(bruto json.RawMessage) (valorValidado, map[string]string) {
	det := map[string]string{}
	if len(bytes.TrimSpace(bruto)) == 0 || bytes.Equal(bytes.TrimSpace(bruto), []byte("null")) {
		det[caminhoRaiz] = "Preencha o campo. Para voltar ao texto original, use “Restaurar original”."
		return valorValidado{}, det
	}

	var out valorValidado
	switch c.Tipo {
	case TipoTexto, TipoTextoLongo, TipoTitulo:
		s, msg := validarTexto(bruto, c.Tipo, c.Max, true)
		if msg != "" {
			det[caminhoRaiz] = msg
			return out, det
		}
		if c.Formato == formatoEmail && !httpx.ValidarValor(s, "email") {
			det[caminhoRaiz] = "Escreva um endereço de e-mail válido, como reservas@exemplo.com.br."
			return out, det
		}
		out.JSON = jsonDe(s)

	case TipoImagem:
		v, msg := validarImagem(bruto, c.Max)
		if msg != "" {
			det[caminhoRaiz] = msg
			return out, det
		}
		out.JSON = jsonDe(v)
		out.Midias = append(out.Midias, refMidia{ID: v.MediaID, Tipo: TipoImagem, Caminho: caminhoRaiz})

	case TipoVideo:
		var e entradaDeMidia
		if err := decodificarEstrito(bruto, &e); err != nil || e.MediaID == nil || *e.MediaID == uuid.Nil {
			det[caminhoRaiz] = "Envie um vídeo e escolha-o para este campo."
			return out, det
		}
		if e.Alt != nil {
			det[caminhoRaiz] = "Vídeo não tem texto alternativo."
			return out, det
		}
		v := valorVideo{MediaID: *e.MediaID}
		out.JSON = jsonDe(v)
		out.Midias = append(out.Midias, refMidia{ID: v.MediaID, Tipo: TipoVideo, Caminho: caminhoRaiz})

	case TipoLista:
		return c.validarLista(bruto)

	default:
		det[caminhoRaiz] = "Tipo de campo desconhecido."
	}
	return out, det
}

func (c Campo) validarLista(bruto json.RawMessage) (valorValidado, map[string]string) {
	det := map[string]string{}
	var itens []map[string]json.RawMessage
	if err := json.Unmarshal(bruto, &itens); err != nil {
		det[caminhoRaiz] = "A lista veio num formato que o site não entende."
		return valorValidado{}, det
	}
	if len(itens) > c.Max {
		det[caminhoRaiz] = fmt.Sprintf("A lista pode ter no máximo %d itens.", c.Max)
		return valorValidado{}, det
	}

	var out valorValidado
	normalizados := make([]map[string]json.RawMessage, 0, len(itens))
	for i, item := range itens {
		base := fmt.Sprintf("%s[%d]", caminhoRaiz, i)
		if item == nil {
			det[base] = fmt.Sprintf("O item %d está vazio.", i+1)
			continue
		}
		declarados := map[string]bool{}
		for _, s := range c.Itens {
			declarados[s.Chave] = true
		}
		for k := range item {
			if !declarados[k] {
				det[base+"."+k] = fmt.Sprintf("O item %d tem um dado que este campo não usa (%q).", i+1, k)
			}
		}
		n := make(map[string]json.RawMessage, len(c.Itens))
		for _, s := range c.Itens {
			caminho := base + "." + s.Chave
			v, ok := item[s.Chave]
			if !ok {
				det[caminho] = fmt.Sprintf("Falta “%s” no item %d.", s.Rotulo, i+1)
				continue
			}
			switch s.Tipo {
			case TipoImagem:
				if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
					n[s.Chave] = json.RawMessage("null")
					continue
				}
				img, msg := validarImagem(v, s.Max)
				if msg != "" {
					det[caminho] = fmt.Sprintf("Item %d: %s", i+1, msg)
					continue
				}
				n[s.Chave] = jsonDe(img)
				out.Midias = append(out.Midias, refMidia{ID: img.MediaID, Tipo: TipoImagem, Caminho: caminho})
			default:
				// Dentro de lista, texto longo pode ficar vazio (um cartão
				// sem itens); texto curto, não.
				str, msg := validarTexto(v, s.Tipo, s.Max, s.Tipo == TipoTexto)
				if msg != "" {
					det[caminho] = fmt.Sprintf("Item %d, “%s”: %s", i+1, s.Rotulo, msg)
					continue
				}
				n[s.Chave] = jsonDe(str)
			}
		}
		normalizados = append(normalizados, n)
	}
	if len(det) > 0 {
		return valorValidado{}, det
	}
	out.JSON = jsonDe(normalizados)
	return out, nil
}

// validarTexto lê uma string JSON, normaliza as quebras de linha e confere
// tamanho (em letras, não em bytes) e caracteres.
func validarTexto(bruto json.RawMessage, tipo Tipo, max int, obrigatorio bool) (string, string) {
	var s string
	if err := json.Unmarshal(bruto, &s); err != nil {
		return "", "Escreva um texto."
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if !utf8.ValidString(s) {
		return "", "O texto tem caracteres inválidos."
	}
	if obrigatorio && strings.TrimSpace(s) == "" {
		return "", "Preencha o campo. Para voltar ao texto original, use “Restaurar original”."
	}
	if n := utf8.RuneCountInString(s); n > max {
		return "", fmt.Sprintf("O texto pode ter no máximo %d letras (tem %d).", max, n)
	}
	for _, r := range s {
		if r == '\n' {
			if tipo == TipoTexto {
				return "", "Este campo é de uma linha só: tire as quebras de linha."
			}
			continue
		}
		if r == '\t' {
			continue
		}
		if unicode.IsControl(r) {
			return "", "O texto tem caracteres invisíveis que o site não mostra; apague e digite de novo."
		}
	}
	return s, ""
}

func validarImagem(bruto json.RawMessage, maxAlt int) (valorImagem, string) {
	var e entradaDeMidia
	if err := decodificarEstrito(bruto, &e); err != nil || e.MediaID == nil || *e.MediaID == uuid.Nil {
		return valorImagem{}, "Envie uma foto e escolha-a para este campo."
	}
	v := valorImagem{MediaID: *e.MediaID}
	if e.Alt != nil {
		alt, msg := validarTexto(jsonDe(*e.Alt), TipoTexto, maxAlt, false)
		if msg != "" {
			return valorImagem{}, "Texto alternativo: " + msg
		}
		v.Alt = strings.TrimSpace(alt)
	}
	return v, ""
}

func decodificarEstrito(bruto json.RawMessage, alvo any) error {
	dec := json.NewDecoder(bytes.NewReader(bruto))
	dec.DisallowUnknownFields()
	if err := dec.Decode(alvo); err != nil {
		return err
	}
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return errors.New("mais de um documento JSON")
	}
	return nil
}
