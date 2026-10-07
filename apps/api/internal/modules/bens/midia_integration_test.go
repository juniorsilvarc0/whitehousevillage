//go:build integration

package bens_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

type midiaResp struct {
	ID           uuid.UUID `json:"id"`
	Mime         string    `json:"mime"`
	Bytes        int64     `json:"bytes"`
	Largura      *int      `json:"width"`
	Altura       *int      `json:"height"`
	NomeOriginal string    `json:"original_name"`
	URL          string    `json:"url"`
	URLMiniatura string    `json:"thumb_url"`
}

// Extensão .jpg, conteúdo texto: o tipo vem dos BYTES, e a recusa é 422 com
// `details.file` em linguagem de gestor.
func TestUploadComBytesFalsosEh422(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)

	r := a.enviarArquivo(t, g, "foto-do-prato.jpg", []byte("isto é texto puro, não uma foto de prato"))
	exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "enviando texto como .jpg")
	if msg, _ := r.detalhes(t)["file"].(string); msg == "" {
		t.Fatalf("o 422 deveria explicar em details.file: %s", r.Corpo)
	}
	var n int
	if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM inventory_media WHERE original_name = 'foto-do-prato.jpg'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("o arquivo recusado ganhou linha em inventory_media")
	}
}

func jpegDeTeste(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// O envio guarda o original, gera a miniatura, devolve as duas URLs
// AUTENTICADAS; a galeria aponta para a foto e a capa aparece no catálogo.
func TestFotoDoBemDoEnvioAteACapa(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)

	r := a.enviarArquivo(t, g, "../../etc/prato.png", jpegDeTeste(t, 1200, 800))
	exigir(t, r, http.StatusCreated, "enviando foto")
	m := dado[midiaResp](t, r)
	if m.Mime != "image/jpeg" {
		t.Fatalf("o tipo vem dos bytes (é JPEG, apesar do .png): %s", m.Mime)
	}
	if m.NomeOriginal != "prato.png" {
		t.Fatalf("o nome original perde o caminho: %q", m.NomeOriginal)
	}
	if m.Largura == nil || *m.Largura != 1200 || m.Altura == nil || *m.Altura != 800 {
		t.Fatalf("dimensões: %v × %v", m.Largura, m.Altura)
	}
	if m.URL != "/api/v1/inventory/media/"+m.ID.String() || m.URLMiniatura != m.URL+"?size=thumb" {
		t.Fatalf("urls: %q %q", m.URL, m.URLMiniatura)
	}
	if loc := r.Cabecalho.Get("Location"); loc != m.URL {
		t.Fatalf("Location = %q", loc)
	}

	// Entrega: autenticada, cache privado e imutável.
	original := a.baixar(t, g, m.URL)
	exigir(t, original, http.StatusOK, "baixando original")
	if original.Cabecalho.Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", original.Cabecalho.Get("Cache-Control"))
	}
	if int64(len(original.Corpo)) != m.Bytes {
		t.Fatalf("original com %d bytes, registrado %d", len(original.Corpo), m.Bytes)
	}
	mini := a.baixar(t, g, m.URLMiniatura)
	exigir(t, mini, http.StatusOK, "baixando miniatura")
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(mini.Corpo))
	if err != nil || cfg.Width != 480 || cfg.Height != 320 {
		t.Fatalf("miniatura %dx%d (%v), esperado 480x320", cfg.Width, cfg.Height, err)
	}
	if r := a.baixar(t, "", m.URL); r.Status != http.StatusUnauthorized {
		t.Fatalf("a foto mostra o interior da casa: sem token tem de ser 401, veio %d", r.Status)
	}
	exigirErro(t, a.baixar(t, g, m.URL+"?size=gigante"), http.StatusUnprocessableEntity, "VALIDATION_ERROR", "size inválido")

	// Galeria → capa.
	b := a.bem(t, g, "Travessa", nil)
	r = a.chamar(t, http.MethodPut, fmt.Sprintf("/inventory/items/%s/photos", b.ID), g, map[string]any{"media_ids": []uuid.UUID{m.ID}})
	exigir(t, r, http.StatusOK, "aplicando a galeria")
	r = a.chamar(t, http.MethodGet, "/inventory/items?has_photo=true&q="+b.Nome[len(b.Nome)-8:], g, nil)
	itens, _ := lista[bemResp](t, r)
	if len(itens) != 1 || itens[0].Capa == nil || itens[0].Capa.ID != m.ID || itens[0].QtdFotos != 1 {
		t.Fatalf("a capa deveria ser a foto aplicada: %s", r.Corpo)
	}

	// Repetida e inexistente são 422 com o índice; vazia é aceita.
	exigirErro(t, a.chamar(t, http.MethodPut, fmt.Sprintf("/inventory/items/%s/photos", b.ID), g,
		map[string]any{"media_ids": []uuid.UUID{m.ID, m.ID}}), http.StatusUnprocessableEntity, "VALIDATION_ERROR", "repetida")
	r = a.chamar(t, http.MethodPut, fmt.Sprintf("/inventory/items/%s/photos", b.ID), g,
		map[string]any{"media_ids": []uuid.UUID{uuid.New()}})
	exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "inexistente")
	if r.detalhes(t)["media_ids[0]"] == nil {
		t.Fatalf("o 422 deveria apontar o índice: %s", r.Corpo)
	}
	r = a.chamar(t, http.MethodPut, fmt.Sprintf("/inventory/items/%s/photos", b.ID), g, map[string]any{"media_ids": []uuid.UUID{}})
	exigir(t, r, http.StatusOK, "tirando todas as fotos")
	// Desvincular não apaga o arquivo.
	exigir(t, a.baixar(t, g, m.URL), http.StatusOK, "a foto desvinculada continua no volume")
}

func (a *ambiente) baixar(t *testing.T, token, url string) resposta {
	t.Helper()
	req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, a.servidor.URL+url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return a.enviar(t, req, token)
}
