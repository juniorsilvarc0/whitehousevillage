//go:build integration

package router

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// ─────────────────────────── Imagens de teste ───────────────────────────────

// qbImagem pinta w×h; `pinta` nil desenha um degradê.
func qbImagem(w, h int, pinta func(x, y int) color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if pinta != nil {
				img.Set(x, y, pinta(x, y))
			} else {
				img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
			}
		}
	}
	return img
}

func qbCodificarPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func qbCodificarJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// qbJPEGComOrientacao é o JPEG como o celular grava foto tirada EM PÉ: os
// pixels deitados e a tag EXIF Orientation dizendo como girar para mostrar.
// APP1 "Exif" com um TIFF big-endian de uma entrada (0x0112, SHORT).
func qbJPEGComOrientacao(t *testing.T, img image.Image, orientacao uint16) []byte {
	t.Helper()
	cru := qbCodificarJPEG(t, img)
	tiff := []byte{
		'M', 'M', 0x00, 0x2A, 0x00, 0x00, 0x00, 0x08, // cabeçalho, IFD0 no byte 8
		0x00, 0x01, // uma entrada
		0x01, 0x12, 0x00, 0x03, 0x00, 0x00, 0x00, 0x01, byte(orientacao >> 8), byte(orientacao), 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, // sem próximo IFD
	}
	seg := append([]byte("Exif\x00\x00"), tiff...)
	tam := len(seg) + 2
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1, byte(tam >> 8), byte(tam)}
	out = append(out, seg...)
	return append(out, cru[2:]...)
}

// WebP 1×1 sem perda (VP8L) — bytes reais de um WebP válido.
const qbWebP1x1 = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

type qbMidia struct {
	ID       uuid.UUID `json:"id"`
	Mime     string    `json:"mime"`
	Bytes    int64     `json:"bytes"`
	Largura  *int      `json:"width"`
	Altura   *int      `json:"height"`
	URL      string    `json:"url"`
	Miniatur string    `json:"thumb_url"`
}

func (a *ambiente) qbMidiasDoUsuario(t *testing.T, u uuid.UUID) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM inventory_media WHERE created_by = $1`, u).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// ─────────────────────────── 7. Upload ──────────────────────────────────────

// Em linguagem de negócio: a governanta fotografa o prato com o celular. O
// contrato aceita JPEG, PNG ou WebP até 15 MB, decididos pelos BYTES — e recusa
// com uma frase que ela entende (`details.file`) tudo o mais: o arquivo grande
// demais, o GIF, o vídeo, o SVG que se diz PNG, o PDF que se diz JPEG. Nada
// recusado deixa linha em `inventory_media`.
func TestBensQAEnvioDeFotoRecusaOQueNaoEhFotoAceita(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "bens-foto", a.qbPerfilDoSeed(t, "usuario"))
	a.qbFaxina(t, u)

	jpegDeVerdade := qbCodificarJPEG(t, qbImagem(64, 48, nil))
	acimaDoTeto := make([]byte, 15<<20+1) // 15 MiB + 1 byte: acima de 15 MB em qualquer leitura de "MB"
	copy(acimaDoTeto, jpegDeVerdade)

	casos := []struct {
		nome, arquivo, tipoDaParte string
		bytes                      []byte
	}{
		{"acima de 15 MB", "grande.jpg", "image/jpeg", acimaDoTeto},
		{"GIF", "animado.gif", "image/gif", append([]byte("GIF89a\x01\x00\x01\x00\x80\x00\x00"), make([]byte, 64)...)},
		{"GIF fingindo JPEG", "foto.jpg", "image/jpeg", append([]byte("GIF89a\x01\x00\x01\x00\x80\x00\x00"), make([]byte, 64)...)},
		{"vídeo MP4", "video.mp4", "video/mp4", append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 2, 0}, make([]byte, 64)...)},
		{"vídeo MP4 fingindo JPEG", "foto.jpg", "image/jpeg", append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0}, make([]byte, 64)...)},
		{"vídeo WebM", "video.webm", "video/webm", append([]byte{0x1A, 0x45, 0xDF, 0xA3}, make([]byte, 64)...)},
		{"vídeo AVI (RIFF que não é WEBP)", "video.webp", "image/webp", append([]byte("RIFF\x24\x00\x00\x00AVI LIST"), make([]byte, 64)...)},
		{"HEIC do iPhone", "IMG_0001.HEIC", "image/heic", append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c', 0, 0, 0, 0}, make([]byte, 64)...)},
		{"SVG com script fingindo PNG", "prato.png", "image/png", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(document.cookie)</script></svg>`)},
		{"HTML fingindo JPEG", "prato.jpg", "image/jpeg", []byte("<!DOCTYPE html><html><body><script>alert(1)</script></body></html>")},
		{"PDF fingindo JPEG", "nota.jpg", "image/jpeg", []byte("%PDF-1.4\n%âãÏÓ\n1 0 obj<<>>endobj\n")},
		{"BMP", "prato.bmp", "image/bmp", append([]byte("BM"), make([]byte, 64)...)},
		{"arquivo vazio", "vazio.jpg", "image/jpeg", nil},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			antes := a.qbMidiasDoUsuario(t, u.ID)
			r := a.qbEnviarFoto(t, u.Token, "file", c.arquivo, c.tipoDaParte, c.bytes)
			e := qbErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", c.nome)
			if msg, _ := e.Details["file"].(string); msg == "" {
				t.Errorf("a recusa tem de explicar em details.file, em linguagem de gestor: %s", r.Corpo)
			}
			if depois := a.qbMidiasDoUsuario(t, u.ID); depois != antes {
				t.Errorf("o arquivo recusado ganhou linha em inventory_media (%d → %d)", antes, depois)
			}
		})
	}

	t.Run("sem o campo file", func(t *testing.T) {
		r := a.qbEnviarFoto(t, u.Token, "foto", "prato.jpg", "image/jpeg", jpegDeVerdade)
		e := qbErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "multipart sem `file`")
		if e.Details["file"] == nil {
			t.Errorf("a recusa tem de apontar details.file: %s", r.Corpo)
		}
	})

	// O limite é "até 15 MB": 15.000.000 bytes estão dentro em qualquer leitura
	// de MB. É a foto de 48 MP do celular, e ela tem de entrar.
	t.Run("15.000.000 bytes entram", func(t *testing.T) {
		grande := make([]byte, 15_000_000)
		copy(grande, jpegDeVerdade)
		r := a.qbEnviarFoto(t, u.Token, "file", "48mp.jpg", "image/jpeg", grande)
		m := qbDado[qbMidia](t, r, http.StatusCreated, "foto de 15.000.000 bytes")
		if m.Bytes != 15_000_000 || m.Mime != "image/jpeg" {
			t.Fatalf("registro da foto grande: %+v", m)
		}
	})
}

// Em linguagem de negócio: a foto tirada com o celular EM PÉ chega com os
// pixels deitados e uma etiqueta EXIF dizendo "gire 90°". O navegador respeita
// a etiqueta ao mostrar o original; a miniatura (que é o que a grade carrega)
// tem de sair em pé também, senão o catálogo mostra metade dos pratos de lado.
func TestBensQAMiniaturaDoJPEGComOrientacaoExifSaiEmPe(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "bens-foto", a.qbPerfilDoSeed(t, "usuario"))
	a.qbFaxina(t, u)

	vermelho := color.RGBA{230, 20, 20, 255}
	azul := color.RGBA{20, 20, 230, 255}
	// Gravada deitada: 1200×800, canto superior esquerdo vermelho (300×200).
	deitada := qbImagem(1200, 800, func(x, y int) color.RGBA {
		if x < 300 && y < 200 {
			return vermelho
		}
		return azul
	})
	r := a.qbEnviarFoto(t, u.Token, "file", "em-pe.jpg", "image/jpeg", qbJPEGComOrientacao(t, deitada, 6))
	m := qbDado[qbMidia](t, r, http.StatusCreated, "JPEG com EXIF Orientation=6")
	if loc := r.Headers.Get("Location"); loc != m.URL {
		t.Errorf("Location = %q, esperado a url da entrega autenticada %q", loc, m.URL)
	}
	if m.Miniatur == m.URL || m.Miniatur != m.URL+"?size=thumb" {
		t.Fatalf("JPEG decodificável tem de gerar miniatura: url=%q thumb_url=%q", m.URL, m.Miniatur)
	}
	if m.Largura != nil && m.Altura != nil {
		t.Logf("dimensões declaradas: %d×%d (contrato não fixa se são as de exibição)", *m.Largura, *m.Altura)
	}

	mini := a.qbBaixar(t, u.Token, m.Miniatur)
	exigirStatusQB(t, mini, http.StatusOK, "baixando a miniatura")
	if ct := mini.Headers.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type da miniatura = %q", ct)
	}
	img, err := jpeg.Decode(bytes.NewReader(mini.Corpo))
	if err != nil {
		t.Fatalf("a miniatura não decodifica como JPEG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 320 || b.Dy() != 480 {
		t.Fatalf("miniatura %d×%d; a foto em pé (800×1200 na tela) reduzida a 480 no maior lado é 320×480", b.Dx(), b.Dy())
	}
	// Girada 90° no sentido horário, o canto vermelho vai para o canto SUPERIOR
	// DIREITO da foto em pé.
	ehVermelho := func(x, y int) bool {
		r, g, bl, _ := img.At(x, y).RGBA()
		return r>>8 > 150 && g>>8 < 90 && bl>>8 < 90
	}
	if !ehVermelho(300, 40) || ehVermelho(20, 40) || ehVermelho(300, 400) {
		t.Fatalf("a miniatura não está na orientação de exibição: superior direito vermelho=%v, superior esquerdo vermelho=%v, inferior direito vermelho=%v",
			ehVermelho(300, 40), ehVermelho(20, 40), ehVermelho(300, 400))
	}

	orig := a.qbBaixar(t, u.Token, m.URL)
	exigirStatusQB(t, orig, http.StatusOK, "baixando o original")
	if orig.Headers.Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Errorf("Cache-Control do original = %q", orig.Headers.Get("Cache-Control"))
	}
}

// Em linguagem de negócio: a foto PNG (print de tela, foto editada) também
// vira miniatura JPEG; a WebP, que a biblioteca padrão não lê, sai com
// `thumb_url == url` — a grade mostra o original, e a foto nunca deixa de
// valer por causa da miniatura.
func TestBensQAMiniaturaDoPNGEWebPSemMiniatura(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "bens-foto", a.qbPerfilDoSeed(t, "usuario"))
	a.qbFaxina(t, u)

	t.Run("PNG gera miniatura JPEG", func(t *testing.T) {
		original := qbPNG(t, 1000, 500)
		r := a.qbEnviarFoto(t, u.Token, "file", "print.png", "image/png", original)
		m := qbDado[qbMidia](t, r, http.StatusCreated, "PNG")
		if m.Mime != "image/png" {
			t.Errorf("mime = %q", m.Mime)
		}
		if m.Miniatur != m.URL+"?size=thumb" {
			t.Fatalf("PNG decodificável tem de gerar miniatura: url=%q thumb_url=%q", m.URL, m.Miniatur)
		}
		mini := a.qbBaixar(t, u.Token, m.Miniatur)
		exigirStatusQB(t, mini, http.StatusOK, "miniatura do PNG")
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(mini.Corpo))
		if err != nil || cfg.Width != 480 || cfg.Height != 240 {
			t.Fatalf("miniatura do PNG 1000×500: %d×%d (%v), esperado JPEG 480×240", cfg.Width, cfg.Height, err)
		}
		orig := a.qbBaixar(t, u.Token, m.URL)
		if orig.Headers.Get("Content-Type") != "image/png" || !bytes.Equal(orig.Corpo, original) {
			t.Errorf("o original do PNG não volta intacto: %q, %d bytes de %d", orig.Headers.Get("Content-Type"), len(orig.Corpo), len(original))
		}
	})

	t.Run("WebP sai com thumb_url igual a url", func(t *testing.T) {
		original, err := base64.StdEncoding.DecodeString(qbWebP1x1)
		if err != nil {
			t.Fatal(err)
		}
		r := a.qbEnviarFoto(t, u.Token, "file", "prato.webp", "image/webp", original)
		m := qbDado[qbMidia](t, r, http.StatusCreated, "WebP")
		if m.Mime != "image/webp" {
			t.Errorf("mime = %q", m.Mime)
		}
		if m.Miniatur != m.URL {
			t.Fatalf("WebP sem miniatura: thumb_url tem de ser igual a url — url=%q thumb_url=%q", m.URL, m.Miniatur)
		}
		// `?size=thumb` sem miniatura cai no original.
		mini := a.qbBaixar(t, u.Token, m.URL+"?size=thumb")
		exigirStatusQB(t, mini, http.StatusOK, "?size=thumb da WebP")
		if mini.Headers.Get("Content-Type") != "image/webp" || !bytes.Equal(mini.Corpo, original) {
			t.Errorf("?size=thumb da WebP deveria entregar o original: %q, %d bytes", mini.Headers.Get("Content-Type"), len(mini.Corpo))
		}
	})
}
