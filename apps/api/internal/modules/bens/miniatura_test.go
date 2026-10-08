package bens

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Bytes falsos: extensão .jpg, conteúdo texto. O tipo vem dos BYTES.
func TestDetectarFotoPelosBytes(t *testing.T) {
	if _, ok := detectarFoto([]byte("isto não é uma foto, é um texto com extensão .jpg")); ok {
		t.Fatal("texto não pode passar por foto")
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	if tipo, ok := detectarFoto(jpg.Bytes()); !ok || tipo.mime != mimeJPEG {
		t.Fatalf("JPEG não reconhecido: %+v %v", tipo, ok)
	}
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 20)...)
	if tipo, ok := detectarFoto(webp); !ok || tipo.mime != mimeWebP {
		t.Fatalf("WebP não reconhecido: %+v %v", tipo, ok)
	}
	// Vídeo não entra: o que identifica um prato é uma foto.
	mp4 := []byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2")
	if _, ok := detectarFoto(mp4); ok {
		t.Fatal("vídeo não pode ser aceito como foto de bem")
	}
}

func TestTamanhoDaMiniaturaNuncaAmplia(t *testing.T) {
	casos := []struct{ w, h, lw, lh int }{
		{4000, 3000, 480, 360},
		{3000, 4000, 360, 480},
		{300, 200, 300, 200},
		{10000, 10, 480, 1},
	}
	for _, c := range casos {
		lw, lh := encaixarNoLado(c.w, c.h, 480)
		if lw != c.lw || lh != c.lh {
			t.Errorf("%dx%d → %dx%d, esperado %dx%d", c.w, c.h, lw, lh, c.lw, c.lh)
		}
	}
}

// Média de área: um tabuleiro preto e branco de 1 px reduzido pela metade vira
// cinza uniforme — vizinho mais próximo daria preto ou branco puros.
func TestReduzirFazMediaDeArea(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			if (x+y)%2 == 0 {
				src.Set(x, y, color.White)
			} else {
				src.Set(x, y, color.Black)
			}
		}
	}
	out := reduzir(src, 4, 4)
	for y := range 4 {
		for x := range 4 {
			c := out.RGBAAt(x, y)
			if c.R < 120 || c.R > 135 || c.R != c.G || c.G != c.B || c.A != 0xff {
				t.Fatalf("pixel (%d,%d) = %+v, esperado cinza médio opaco", x, y, c)
			}
		}
	}
}

// PNG transparente vai para JPEG sobre BRANCO, não sobre preto.
func TestReduzirCompoeTransparenciaSobreBranco(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4)) // tudo transparente
	out := reduzir(src, 2, 2)
	if c := out.RGBAAt(0, 0); c.R != 0xff || c.G != 0xff || c.B != 0xff {
		t.Fatalf("transparente deveria virar branco, virou %+v", c)
	}
}

func TestOrientarGiraEEspelha(t *testing.T) {
	// 2×1: vermelho à esquerda, azul à direita.
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, color.RGBA{255, 0, 0, 255})
	src.Set(1, 0, color.RGBA{0, 0, 255, 255})

	gira := orientar(src, 6) // 90° horário: vira 1×2, vermelho em cima
	if b := gira.Bounds(); b.Dx() != 1 || b.Dy() != 2 {
		t.Fatalf("90° deveria trocar os eixos: %v", b)
	}
	if gira.RGBAAt(0, 0).R != 255 || gira.RGBAAt(0, 1).B != 255 {
		t.Fatal("90° horário deveria pôr o vermelho em cima")
	}
	anti := orientar(src, 8) // 90° anti-horário: azul em cima
	if anti.RGBAAt(0, 0).B != 255 || anti.RGBAAt(0, 1).R != 255 {
		t.Fatal("90° anti-horário deveria pôr o azul em cima")
	}
	espelho := orientar(src, 2)
	if espelho.RGBAAt(0, 0).B != 255 || espelho.RGBAAt(1, 0).R != 255 {
		t.Fatal("espelho horizontal deveria trocar os lados")
	}
	if orientar(src, 1) != src {
		t.Fatal("orientação 1 não mexe")
	}
}

// jpegComOrientacao monta um JPEG com o segmento APP1/EXIF trazendo a tag
// Orientation, no formato que o celular grava.
func jpegComOrientacao(t *testing.T, w, h, orientacao int, ordem binary.ByteOrder) []byte {
	t.Helper()
	var corpo bytes.Buffer
	if err := jpeg.Encode(&corpo, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	tiff := make([]byte, 8+2+12+4)
	if ordem == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	ordem.PutUint16(tiff[2:], 42)
	ordem.PutUint32(tiff[4:], 8)
	ordem.PutUint16(tiff[8:], 1)       // uma entrada
	ordem.PutUint16(tiff[10:], 0x0112) // Orientation
	ordem.PutUint16(tiff[12:], 3)      // SHORT
	ordem.PutUint32(tiff[14:], 1)      // count
	ordem.PutUint16(tiff[18:], uint16(orientacao))
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(app1)+2))
	seg = append(seg, app1...)

	b := corpo.Bytes()
	out := append([]byte{}, b[:2]...) // SOI
	out = append(out, seg...)
	return append(out, b[2:]...)
}

func TestOrientacaoDoJPEG(t *testing.T) {
	for _, ordem := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		if o := orientacaoDoJPEG(bytes.NewReader(jpegComOrientacao(t, 8, 4, 6, ordem))); o != 6 {
			t.Errorf("%v: orientação lida %d, esperado 6", ordem, o)
		}
	}
	if o := orientacaoDoJPEG(bytes.NewReader([]byte("lixo"))); o != 1 {
		t.Errorf("sem EXIF deveria ser 1, veio %d", o)
	}
}

// Foto em pé tirada no celular: os pixels estão deitados (8×4) e o EXIF diz
// "gire 90°". A miniatura e as dimensões saem EM PÉ (4×8), como o navegador
// mostra o original.
func TestMiniaturaRespeitaOrientacao(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "foto.jpg")
	if err := os.WriteFile(caminho, jpegComOrientacao(t, 8, 4, 6, binary.LittleEndian), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := medirFoto(caminho, mimeJPEG)
	if err != nil || d.largura != 4 || d.altura != 8 {
		t.Fatalf("dimensões = %+v (%v), esperado 4×8", d, err)
	}
	mini, err := gerarMiniatura(context.Background(), caminho, mimeJPEG)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(mini))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 4 || b.Dy() != 8 {
		t.Fatalf("miniatura %v, esperado 4×8", b)
	}
}

func TestMiniaturaDePNGGrandeSaiJPEGReduzido(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "foto.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1200, 900))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caminho, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	mini, err := gerarMiniatura(context.Background(), caminho, mimePNG)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(mini))
	if err != nil {
		t.Fatalf("a miniatura deveria ser JPEG: %v", err)
	}
	if cfg.Width != 480 || cfg.Height != 360 {
		t.Fatalf("miniatura %dx%d, esperado 480x360", cfg.Width, cfg.Height)
	}
}

// WebP não tem decoder na stdlib: sem miniatura e sem erro.
func TestWebPFicaSemMiniatura(t *testing.T) {
	mini, err := gerarMiniatura(context.Background(), "/nao/existe.webp", mimeWebP)
	if mini != nil || err != nil {
		t.Fatalf("WebP deveria sair (nil, nil), veio (%d bytes, %v)", len(mini), err)
	}
}

func TestDimensoesDoWebP(t *testing.T) {
	cab := func(formato string, corpo []byte) []byte {
		b := append([]byte("RIFF\x00\x00\x00\x00WEBP"+formato+"\x00\x00\x00\x00"), corpo...)
		return append(b, make([]byte, 16)...)
	}

	vp8x := make([]byte, 10)
	vp8x[4], vp8x[5], vp8x[6] = 0x1F, 0x03, 0x00 // largura-1 = 799
	vp8x[7], vp8x[8], vp8x[9] = 0x57, 0x02, 0x00 // altura-1 = 599
	if d, err := dimensoesDoWebP(cab("VP8X", vp8x)); err != nil || d.largura != 800 || d.altura != 600 {
		t.Errorf("VP8X: %+v %v", d, err)
	}

	vp8 := []byte{0, 0, 0, 0x9d, 0x01, 0x2a, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(vp8[6:], 640)
	binary.LittleEndian.PutUint16(vp8[8:], 480)
	if d, err := dimensoesDoWebP(cab("VP8 ", vp8)); err != nil || d.largura != 640 || d.altura != 480 {
		t.Errorf("VP8: %+v %v", d, err)
	}

	vp8l := make([]byte, 5)
	vp8l[0] = 0x2f
	binary.LittleEndian.PutUint32(vp8l[1:], uint32(1023)|uint32(767)<<14)
	if d, err := dimensoesDoWebP(cab("VP8L", vp8l)); err != nil || d.largura != 1024 || d.altura != 768 {
		t.Errorf("VP8L: %+v %v", d, err)
	}

	if _, err := dimensoesDoWebP([]byte("RIFF")); err == nil {
		t.Error("cabeçalho curto deveria dar erro")
	}
}
