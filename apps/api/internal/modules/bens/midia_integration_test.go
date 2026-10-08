//go:build integration

package bens_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/bens"
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

func (m midiaResp) dimensoes() string {
	if m.Largura == nil || m.Altura == nil {
		return "sem dimensões"
	}
	return fmt.Sprintf("%d×%d", *m.Largura, *m.Altura)
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

// O envio guarda a foto (convertida: aqui já cabe em 1280 px, então as
// dimensões ficam), gera a miniatura, devolve as duas URLs AUTENTICADAS; a
// galeria aponta para a foto e a capa aparece no catálogo.
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
	if m.dimensoes() != "1200×800" {
		t.Fatalf("dimensões: %s", m.dimensoes())
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

// ─────────────────────────── Conversão no envio ─────────────────────────────

// fotoDeCelular imita a foto do celular: w×h com textura — degradê mais um
// ruído determinístico de 4 bits, porque o degradê puro comprime demais e não
// representa foto — em JPEG qualidade 92. Em 4032×3024 sai com ~3,4 MB, o
// tamanho da foto de 12 MP de um celular.
func fotoDeCelular(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			ruido := uint8((uint32(x)*2654435761 ^ uint32(y)*2246822519) >> 28)
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(x/16)+ruido, uint8(y/12)+ruido, uint8((x+y)/24)+ruido, 0xff
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// webp1x1 é um WebP sem perda (VP8L) de 1×1 — bytes reais de um WebP válido.
func webp1x1(t *testing.T) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// pngGigante tem só o cabeçalho de um PNG 20000×20000 RGBA (1,6 GB
// decodificado): passa na detecção pelos bytes, e o orçamento de memória
// recusa a decodificação pelo cabeçalho.
func pngGigante() []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 20000)
	binary.BigEndian.PutUint32(ihdr[4:], 20000)
	ihdr[8], ihdr[9] = 8, 6
	chunk := append([]byte("IHDR"), ihdr...)
	out := binary.BigEndian.AppendUint32([]byte("\x89PNG\r\n\x1a\n"), 13)
	out = append(out, chunk...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
}

// logCapturado guarda o que a API escreve no log durante o teste.
type logCapturado struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logCapturado) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logCapturado) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func capturarLog(t *testing.T) *logCapturado {
	t.Helper()
	l := &logCapturado{}
	anterior := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(l, nil)))
	t.Cleanup(func() { slog.SetDefault(anterior) })
	return l
}

// arquivoNoVolume é o caminho da foto no volume, pela chave registrada.
func (a *ambiente) arquivoNoVolume(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var chave string
	if err := a.pool.QueryRow(a.ctx, `SELECT storage_key FROM inventory_media WHERE id = $1`, id).Scan(&chave); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(a.dirMidia, bens.SubdiretorioDoVolume, chave)
}

// arquivosDoVolume lista a pasta das fotos: o que o envio deixou lá.
func (a *ambiente) arquivosDoVolume(t *testing.T) []string {
	t.Helper()
	entradas, err := os.ReadDir(filepath.Join(a.dirMidia, bens.SubdiretorioDoVolume))
	if err != nil {
		t.Fatal(err)
	}
	nomes := make([]string, 0, len(entradas))
	for _, e := range entradas {
		nomes = append(nomes, e.Name())
	}
	sort.Strings(nomes)
	return nomes
}

// A foto de 12 MP do celular: 201, guardada em JPEG com o lado maior em 1280,
// menor que a enviada, e o arquivo no volume é o que a resposta descreve. O
// enviado não fica no volume.
func TestFotoDoCelularEhGuardadaConvertida(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)

	enviada := fotoDeCelular(t, 4032, 3024)
	r := a.enviarArquivo(t, g, "IMG_20261007_101500.jpg", enviada)
	exigir(t, r, http.StatusCreated, "enviando a foto do celular")
	m := dado[midiaResp](t, r)
	if m.Mime != "image/jpeg" || m.NomeOriginal != "IMG_20261007_101500.jpg" {
		t.Fatalf("mime %q, nome %q", m.Mime, m.NomeOriginal)
	}
	if m.dimensoes() != "1280×960" {
		t.Fatalf("4032×3024 deveria ser guardada em 1280×960, a resposta diz %s", m.dimensoes())
	}
	if m.Bytes <= 0 || m.Bytes >= int64(len(enviada)) {
		t.Fatalf("guardada com %d bytes; enviada com %d", m.Bytes, len(enviada))
	}
	t.Logf("enviada: %d bytes (4032×3024, qualidade 92); guardada: %d bytes (1280×960, qualidade 75) — %.1f%% do enviado",
		len(enviada), m.Bytes, 100*float64(m.Bytes)/float64(len(enviada)))

	// O arquivo no volume é o descrito: tamanho, tipo e dimensões.
	caminho := a.arquivoNoVolume(t, m.ID)
	info, err := os.Stat(caminho)
	if err != nil || info.Size() != m.Bytes {
		t.Fatalf("o arquivo no volume tem de ter %d bytes: %v %v", m.Bytes, info, err)
	}
	f, err := os.Open(caminho)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(f)
	_ = f.Close()
	if err != nil || cfg.Width != 1280 || cfg.Height != 960 {
		t.Fatalf("o arquivo no volume: %d×%d (%v)", cfg.Width, cfg.Height, err)
	}
	if got := a.arquivosDoVolume(t); fmt.Sprint(got) != fmt.Sprintf("[%s.jpg %s.thumb.jpg]", m.ID, m.ID) {
		t.Fatalf("no volume ficam só a guardada e a miniatura (o enviado e o temporário não): %v", got)
	}

	// A entrega é o arquivo guardado; a miniatura sai dele.
	original := a.baixar(t, g, m.URL)
	exigir(t, original, http.StatusOK, "baixando a foto")
	if int64(len(original.Corpo)) != m.Bytes || original.Cabecalho.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("a entrega: %d bytes, %q", len(original.Corpo), original.Cabecalho.Get("Content-Type"))
	}
	if m.URLMiniatura != m.URL+"?size=thumb" {
		t.Fatalf("a foto convertida tem miniatura: %q", m.URLMiniatura)
	}
	mini := a.baixar(t, g, m.URLMiniatura)
	exigir(t, mini, http.StatusOK, "baixando a miniatura")
	if cfg, err := jpeg.DecodeConfig(bytes.NewReader(mini.Corpo)); err != nil || cfg.Width != 480 || cfg.Height != 360 {
		t.Fatalf("miniatura %d×%d (%v), esperado 480×360", cfg.Width, cfg.Height, err)
	}

	var bytesNoBanco int64
	var largura, altura int
	if err := a.pool.QueryRow(a.ctx, `SELECT bytes, width, height FROM inventory_media WHERE id = $1`, m.ID).
		Scan(&bytesNoBanco, &largura, &altura); err != nil {
		t.Fatal(err)
	}
	if bytesNoBanco != m.Bytes || largura != 1280 || altura != 960 {
		t.Fatalf("o registro descreve o arquivo guardado: %d bytes, %d×%d", bytesNoBanco, largura, altura)
	}
}

// A foto tirada em pé (pixels deitados + EXIF Orientation=6) é guardada EM
// PÉ, e sem EXIF: o arquivo do volume não manda o navegador girar de novo.
func TestFotoEmPeEhGuardadaEmPeSemExif(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)

	deitada := image.NewRGBA(image.Rect(0, 0, 1200, 800))
	for y := range 800 {
		for x := range 1200 {
			c := color.RGBA{20, 20, 230, 255}
			if x < 300 && y < 200 {
				c = color.RGBA{230, 20, 20, 255}
			}
			deitada.SetRGBA(x, y, c)
		}
	}
	var cru bytes.Buffer
	if err := jpeg.Encode(&cru, deitada, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
	tiff := []byte{'M', 'M', 0x00, 0x2A, 0x00, 0x00, 0x00, 0x08, 0x00, 0x01,
		0x01, 0x12, 0x00, 0x03, 0x00, 0x00, 0x00, 0x01, 0x00, 0x06, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	enviada := append([]byte{0xFF, 0xD8, 0xFF, 0xE1, byte((len(app1) + 2) >> 8), byte(len(app1) + 2)}, app1...)
	enviada = append(enviada, cru.Bytes()[2:]...)

	r := a.enviarArquivo(t, g, "em-pe.jpg", enviada)
	exigir(t, r, http.StatusCreated, "enviando a foto em pé")
	m := dado[midiaResp](t, r)
	if m.dimensoes() != "800×1200" {
		t.Fatalf("a guardada está em pé: 800×1200, a resposta diz %s", m.dimensoes())
	}
	original := a.baixar(t, g, m.URL)
	exigir(t, original, http.StatusOK, "baixando a foto")
	if bytes.Contains(original.Corpo, []byte("Exif\x00\x00")) {
		t.Fatal("a foto guardada ainda carrega EXIF")
	}
	img, err := jpeg.Decode(bytes.NewReader(original.Corpo))
	if err != nil {
		t.Fatal(err)
	}
	if vr, vg, vb, _ := img.At(700, 100).RGBA(); vr>>8 < 150 || vg>>8 > 90 || vb>>8 > 90 {
		t.Fatal("girada 90° no sentido horário, o canto vermelho vai para o superior direito")
	}
}

// PNG — inclusive com transparência — é guardado como JPEG sobre branco.
func TestPNGEhGuardadoComoJPEG(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)

	img := image.NewNRGBA(image.Rect(0, 0, 1000, 500))
	for y := range 500 {
		for x := 500; x < 1000; x++ {
			img.SetNRGBA(x, y, color.NRGBA{0, 0, 200, 255}) // metade direita azul; a esquerda transparente
		}
	}
	var enviada bytes.Buffer
	if err := png.Encode(&enviada, img); err != nil {
		t.Fatal(err)
	}
	r := a.enviarArquivo(t, g, "print.png", enviada.Bytes())
	exigir(t, r, http.StatusCreated, "enviando PNG")
	m := dado[midiaResp](t, r)
	if m.Mime != "image/jpeg" || m.NomeOriginal != "print.png" {
		t.Fatalf("PNG é guardado como JPEG e o nome enviado fica: mime %q, nome %q", m.Mime, m.NomeOriginal)
	}
	if m.dimensoes() != "1000×500" {
		t.Fatalf("dimensões %s", m.dimensoes())
	}
	original := a.baixar(t, g, m.URL)
	exigir(t, original, http.StatusOK, "baixando a foto")
	if original.Cabecalho.Get("Content-Type") != "image/jpeg" || int64(len(original.Corpo)) != m.Bytes {
		t.Fatalf("a entrega: %q, %d bytes", original.Cabecalho.Get("Content-Type"), len(original.Corpo))
	}
	guardada, err := jpeg.Decode(bytes.NewReader(original.Corpo))
	if err != nil {
		t.Fatalf("o guardado não é JPEG: %v", err)
	}
	if vr, vg, vb, _ := guardada.At(100, 250).RGBA(); vr>>8 < 245 || vg>>8 < 245 || vb>>8 < 245 {
		t.Fatalf("o transparente vira branco, virou (%d,%d,%d)", vr>>8, vg>>8, vb>>8)
	}
	if !strings.HasSuffix(a.arquivoNoVolume(t, m.ID), m.ID.String()+".jpg") {
		t.Fatalf("a chave do volume tem a extensão do guardado: %s", a.arquivoNoVolume(t, m.ID))
	}
	mini := a.baixar(t, g, m.URLMiniatura)
	exigir(t, mini, http.StatusOK, "baixando a miniatura")
	if cfg, err := jpeg.DecodeConfig(bytes.NewReader(mini.Corpo)); err != nil || cfg.Width != 480 || cfg.Height != 240 {
		t.Fatalf("miniatura %d×%d (%v), esperado 480×240", cfg.Width, cfg.Height, err)
	}
}

// As exceções vão COMO VIERAM, com aviso no log e sem recusar: WebP (a
// biblioteca padrão não lê) e foto grande demais para decodificar com
// segurança. Nenhuma tem miniatura.
func TestWebPEFotoGiganteSaoGuardadasComoVieram(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	registro := capturarLog(t)

	for _, c := range []struct {
		nome, mime string
		enviada    []byte
		w, h       int
	}{
		{"prato.webp", "image/webp", webp1x1(t), 1, 1},
		{"panorama.png", "image/png", pngGigante(), 20000, 20000},
	} {
		r := a.enviarArquivo(t, g, c.nome, c.enviada)
		exigir(t, r, http.StatusCreated, "enviando "+c.nome)
		m := dado[midiaResp](t, r)
		if m.Mime != c.mime || m.Bytes != int64(len(c.enviada)) || m.dimensoes() != fmt.Sprintf("%d×%d", c.w, c.h) {
			t.Fatalf("%s vai como veio: %+v", c.nome, m)
		}
		if m.URLMiniatura != m.URL {
			t.Fatalf("%s: sem miniatura, thumb_url = url: %q", c.nome, m.URLMiniatura)
		}
		guardada, err := os.ReadFile(a.arquivoNoVolume(t, m.ID))
		if err != nil || !bytes.Equal(guardada, c.enviada) {
			t.Fatalf("%s: o volume guarda os bytes enviados (%v)", c.nome, err)
		}
		if !strings.Contains(registro.String(), m.ID.String()) || !strings.Contains(registro.String(), "foto guardada como veio") {
			t.Fatalf("%s: a exceção é avisada no log com o id da foto:\n%s", c.nome, registro.String())
		}
	}
	if n := len(a.arquivosDoVolume(t)); n != 2 {
		t.Fatalf("no volume, as duas fotos e nada mais (sem miniatura, sem temporário): %v", a.arquivosDoVolume(t))
	}
}
