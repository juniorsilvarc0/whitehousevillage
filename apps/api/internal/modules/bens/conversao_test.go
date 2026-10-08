package bens

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// ─────────────────────────── Imagens de teste ───────────────────────────────

// degrade pinta w×h com um degradê (escrita direta nos pixels: 12 MP em
// milissegundos, e não em segundos de Set).
func degrade(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(x*7), uint8(y*5), uint8((x+y)*3), 0xff
		}
	}
	return img
}

func codificarJPEG(t *testing.T, img image.Image, qualidade int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: qualidade}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// comSegmentos insere segmentos logo depois do SOI de um JPEG.
func comSegmentos(jpg []byte, segmentos ...[]byte) []byte {
	out := append([]byte{}, jpg[:2]...)
	for _, s := range segmentos {
		out = append(out, s...)
	}
	return append(out, jpg[2:]...)
}

func segmento(marcador byte, corpo []byte) []byte {
	s := []byte{0xFF, marcador, 0, 0}
	binary.BigEndian.PutUint16(s[2:], uint16(len(corpo)+2))
	return append(s, corpo...)
}

// exifComOrientacao é o APP1 que o celular grava: TIFF big-endian com a tag
// Orientation (0x0112) e a Make (0x010F, "Celular"), para haver metadado além
// da orientação.
func exifComOrientacao(orientacao uint16) []byte {
	tiff := []byte{'M', 'M', 0x00, 0x2A, 0x00, 0x00, 0x00, 0x08, 0x00, 0x02,
		0x01, 0x0F, 0x00, 0x02, 0x00, 0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x26, // Make → offset 38
		0x01, 0x12, 0x00, 0x03, 0x00, 0x00, 0x00, 0x01, byte(orientacao >> 8), byte(orientacao), 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00}
	tiff = append(tiff, []byte("Celular\x00")...)
	return segmento(0xE1, append([]byte("Exif\x00\x00"), tiff...))
}

// marcadoresAntesDoSOS lista os segmentos do cabeçalho de um JPEG.
func marcadoresAntesDoSOS(t *testing.T, jpg []byte) []byte {
	t.Helper()
	var out []byte
	i := 2
	for i+4 <= len(jpg) {
		m := jpg[i+1]
		if m == 0xDA {
			return out
		}
		out = append(out, m)
		i += 2 + int(binary.BigEndian.Uint16(jpg[i+2:i+4]))
	}
	t.Fatal("JPEG sem SOS")
	return nil
}

// tabelaDeLuminancia devolve a primeira tabela de quantização (DQT, id 0).
func tabelaDeLuminancia(t *testing.T, jpg []byte) []byte {
	t.Helper()
	i := 2
	for i+4 <= len(jpg) {
		m, tam := jpg[i+1], int(binary.BigEndian.Uint16(jpg[i+2:i+4]))
		if m == 0xDB && jpg[i+4]&0x0f == 0 {
			return jpg[i+5 : i+5+64]
		}
		i += 2 + tam
	}
	t.Fatal("JPEG sem tabela de quantização de luminância")
	return nil
}

func decodificarJPEG(t *testing.T, jpg []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		t.Fatalf("o arquivo guardado não decodifica como JPEG: %v", err)
	}
	return img
}

// ─────────────────────────── A conversão ────────────────────────────────────

func TestConversaoReduzOLadoMaiorA1280(t *testing.T) {
	fonte := codificarJPEG(t, degrade(3000, 4000), 92)
	conv, err := converterFoto(fonte, mimeJPEG)
	if err != nil {
		t.Fatal(err)
	}
	if conv.Largura != 960 || conv.Altura != 1280 {
		t.Fatalf("3000×4000 deveria virar 960×1280, virou %d×%d", conv.Largura, conv.Altura)
	}
	b := decodificarJPEG(t, conv.Dados).Bounds()
	if b.Dx() != 960 || b.Dy() != 1280 {
		t.Fatalf("o arquivo guardado tem %d×%d", b.Dx(), b.Dy())
	}
	if len(conv.Dados) >= len(fonte) || conv.FonteMantida {
		t.Fatalf("a convertida (%d bytes) deveria ser menor que a fonte (%d) e não a própria fonte", len(conv.Dados), len(fonte))
	}
}

func TestConversaoNaoAmplia(t *testing.T) {
	conv, err := converterFoto(codificarJPEG(t, degrade(800, 600), 95), mimeJPEG)
	if err != nil {
		t.Fatal(err)
	}
	if b := decodificarJPEG(t, conv.Dados).Bounds(); b.Dx() != 800 || b.Dy() != 600 || conv.Largura != 800 {
		t.Fatalf("800×600 não é ampliada: %d×%d", b.Dx(), b.Dy())
	}
}

// A foto tirada em pé: pixels deitados (1200×800) e EXIF Orientation=6. A
// guardada sai EM PÉ (800×1200), com o canto vermelho no superior direito, e
// SEM EXIF nenhum — nem a orientação, nem o resto.
func TestConversaoAplicaAOrientacaoESaiSemExif(t *testing.T) {
	deitada := degrade(1200, 800)
	vermelho := color.RGBA{230, 20, 20, 255}
	for y := range 200 {
		for x := range 300 {
			deitada.SetRGBA(x, y, vermelho)
		}
	}
	fonte := comSegmentos(codificarJPEG(t, deitada, 92), exifComOrientacao(6), segmento(0xFE, []byte("comentário com dado")))
	if orientacaoDoJPEG(bytes.NewReader(fonte)) != 6 {
		t.Fatal("a fonte do teste deveria ter Orientation=6")
	}

	conv, err := converterFoto(fonte, mimeJPEG)
	if err != nil {
		t.Fatal(err)
	}
	img := decodificarJPEG(t, conv.Dados)
	if b := img.Bounds(); b.Dx() != 800 || b.Dy() != 1200 || conv.Largura != 800 || conv.Altura != 1200 {
		t.Fatalf("a foto guardada deveria estar em pé (800×1200), está %d×%d", b.Dx(), b.Dy())
	}
	ehVermelho := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		return r>>8 > 150 && g>>8 < 90 && b>>8 < 90
	}
	if !ehVermelho(700, 100) || ehVermelho(100, 100) {
		t.Fatal("girada 90° no sentido horário, o canto vermelho tem de ir para o superior direito")
	}
	for _, m := range marcadoresAntesDoSOS(t, conv.Dados) {
		if (m >= 0xE1 && m <= 0xEF) || m == 0xFE {
			t.Fatalf("a foto guardada carrega metadado (marcador 0x%X)", m)
		}
	}
	if orientacaoDoJPEG(bytes.NewReader(conv.Dados)) != 1 {
		t.Fatal("a orientação já foi aplicada; o arquivo não pode mandar girar de novo")
	}
}

// PNG com transparência vira JPEG achatado sobre BRANCO, não sobre preto.
func TestConversaoDePNGComAlfaSaiSobreBranco(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 200, 100))
	for y := range 100 {
		for x := 100; x < 200; x++ {
			img.SetNRGBA(x, y, color.NRGBA{0, 0, 200, 255}) // metade direita azul opaca
		}
	}
	var fonte bytes.Buffer
	if err := png.Encode(&fonte, img); err != nil {
		t.Fatal(err)
	}
	conv, err := converterFoto(fonte.Bytes(), mimePNG)
	if err != nil {
		t.Fatal(err)
	}
	out := decodificarJPEG(t, conv.Dados)
	r, g, b, _ := out.At(40, 50).RGBA()
	if r>>8 < 245 || g>>8 < 245 || b>>8 < 245 {
		t.Fatalf("o transparente deveria virar branco, virou (%d,%d,%d)", r>>8, g>>8, b>>8)
	}
	if r, _, b, _ := out.At(160, 50).RGBA(); r>>8 > 40 || b>>8 < 150 {
		t.Fatal("o opaco tem de continuar azul")
	}
}

// A qualidade é 75, conferida pela TABELA DE QUANTIZAÇÃO: a do arquivo
// guardado é a mesma de um JPEG codificado com qualidade 75 pelo mesmo
// codificador, e não a de 80 nem a de 90.
func TestConversaoUsaQualidade75(t *testing.T) {
	conv, err := converterFoto(codificarJPEG(t, degrade(1600, 1200), 95), mimeJPEG)
	if err != nil {
		t.Fatal(err)
	}
	guardada := tabelaDeLuminancia(t, conv.Dados)
	referencia := func(q int) []byte { return tabelaDeLuminancia(t, codificarJPEG(t, degrade(8, 8), q)) }
	if !bytes.Equal(guardada, referencia(75)) {
		t.Fatalf("a tabela de luminância não é a da qualidade 75:\n guardada %v\n q75      %v", guardada, referencia(75))
	}
	if bytes.Equal(guardada, referencia(80)) || bytes.Equal(guardada, referencia(90)) {
		t.Fatal("as tabelas de 75, 80 e 90 deveriam diferir")
	}
}

// Regra da fonte mantida: JPEG já dentro de 1280 px, sem orientação a
// aplicar, cuja recodificação não sai menor — guarda-se a FONTE, sem
// recodificar, mas SEM METADADOS: o EXIF e o comentário saem, os dados
// comprimidos ficam byte a byte.
func TestFonteMantidaQuandoRecodificarNaoEncolhe(t *testing.T) {
	// Qualidade 40: recodificar em 75 só pode crescer.
	cru := codificarJPEG(t, degrade(1000, 800), 40)
	exif, comentario := exifComOrientacao(1), segmento(0xFE, []byte("localização: -3.7,-38.5"))
	fonte := comSegmentos(cru, exif, comentario)

	conv, err := converterFoto(fonte, mimeJPEG)
	if err != nil {
		t.Fatal(err)
	}
	if !conv.FonteMantida {
		t.Fatal("a recodificação em 75 de uma fonte em 40 cresce: a fonte deveria ser mantida")
	}
	if !bytes.Equal(conv.Dados, cru) {
		t.Fatalf("a fonte mantida tem de ser a original menos o EXIF e o comentário (%d bytes), veio %d", len(cru), len(conv.Dados))
	}
	if conv.Largura != 1000 || conv.Altura != 800 || conv.Imagem == nil {
		t.Fatalf("dimensões e imagem para a miniatura: %d×%d", conv.Largura, conv.Altura)
	}

	// Com orientação a aplicar, a regra não vale: recodifica e gira.
	girada, err := converterFoto(comSegmentos(cru, exifComOrientacao(6)), mimeJPEG)
	if err != nil || girada.FonteMantida || girada.Largura != 800 {
		t.Fatalf("com orientação 6 a foto é recodificada e girada: %+v %v", girada.FonteMantida, err)
	}
	// PNG nunca é mantido: o volume guarda JPEG.
	var p bytes.Buffer
	if err := png.Encode(&p, degrade(64, 48)); err != nil {
		t.Fatal(err)
	}
	if c, err := converterFoto(p.Bytes(), mimePNG); err != nil || c.FonteMantida {
		t.Fatalf("PNG é sempre convertido: %v %v", c.FonteMantida, err)
	}
}

// jpegSemMetadados tira APP1 (EXIF/XMP), APP2 (ICC/MPF), APP13, os demais
// APPn e COM, mantém o JFIF e o Adobe, copia os dados comprimidos byte a byte e
// para no EOI — o que vem depois (a imagem secundária que o celular anexa,
// com EXIF próprio) não vai.
func TestJPEGSemMetadados(t *testing.T) {
	cru := codificarJPEG(t, degrade(64, 48), 80)
	jfif := segmento(0xE0, []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"))
	adobe := segmento(0xEE, []byte("Adobe\x00\x64\x00\x00\x00\x00\x01"))
	sujo := comSegmentos(cru, jfif, exifComOrientacao(1), segmento(0xE2, []byte("ICC_PROFILE\x00...")),
		segmento(0xED, []byte("Photoshop 3.0\x00")), adobe, segmento(0xFE, []byte("comentário")))
	sujo = append(sujo, codificarJPEG(t, degrade(8, 8), 50)...) // imagem secundária depois do EOI

	limpo, ok := jpegSemMetadados(sujo)
	if !ok {
		t.Fatal("JPEG bem formado deveria ser limpo")
	}
	if want := comSegmentos(cru, jfif, adobe); !bytes.Equal(limpo, want) {
		t.Fatalf("o limpo deveria ser o cru com só JFIF e Adobe (%d bytes), veio %d", len(want), len(limpo))
	}
	if _, err := jpeg.Decode(bytes.NewReader(limpo)); err != nil {
		t.Fatalf("o limpo não decodifica: %v", err)
	}

	for nome, ruim := range map[string][]byte{
		"vazio":            nil,
		"não é JPEG":       []byte("isto não é JPEG"),
		"segmento cortado": cru[:20],
		"sem EOI":          cru[:len(cru)-2],
	} {
		if _, ok := jpegSemMetadados(ruim); ok {
			t.Errorf("%s: deveria devolver ok=false", nome)
		}
	}
}

// ─────────────────────────── As exceções ────────────────────────────────────

// pngGigante tem só o cabeçalho de um PNG 20000×20000 (1,6 GB decodificado):
// o orçamento recusa SEM decodificar.
func pngGigante() []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 20000)
	binary.BigEndian.PutUint32(ihdr[4:], 20000)
	ihdr[8], ihdr[9] = 8, 6 // 8 bits, RGBA
	chunk := append([]byte("IHDR"), ihdr...)
	out := []byte("\x89PNG\r\n\x1a\n")
	out = binary.BigEndian.AppendUint32(out, 13)
	out = append(out, chunk...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
}

func TestPrepararFotoExcecoesGuardadasComoVieram(t *testing.T) {
	ctx := context.Background()
	ler := func(b []byte) func() ([]byte, error) { return func() ([]byte, error) { return b, nil } }

	// WebP: nem é lido.
	lido := false
	prep, err := prepararFoto(ctx, tipoDeFoto{mimeWebP, "webp"}, func() ([]byte, error) { lido = true; return nil, nil })
	if err != nil || !prep.ComoVeio || !errors.Is(prep.Motivo, errNaoConvertivel) || prep.Ext != "webp" || lido {
		t.Fatalf("WebP vai como veio sem ser lido: %+v %v (lido=%v)", prep, err, lido)
	}

	// Grande demais: recusado pelo cabeçalho, sem decodificar.
	prep, err = prepararFoto(ctx, tipoDeFoto{mimePNG, "png"}, ler(pngGigante()))
	if err != nil || !prep.ComoVeio || !errors.Is(prep.Motivo, errSemOrcamento) || prep.Miniatura != nil || prep.Mime != mimePNG {
		t.Fatalf("foto grande demais vai como veio, sem miniatura: %+v %v", prep, err)
	}

	// Corrompida: cabeçalho de JPEG e lixo depois — vai como veio.
	corrompida := append(codificarJPEG(t, degrade(64, 48), 80)[:200], bytes.Repeat([]byte{0x42}, 400)...)
	prep, err = prepararFoto(ctx, tipoDeFoto{mimeJPEG, "jpg"}, ler(corrompida))
	if err != nil || !prep.ComoVeio || prep.Motivo == nil || prep.Miniatura != nil {
		t.Fatalf("foto que não decodifica vai como veio: %+v %v", prep, err)
	}

	// Convertível: JPEG no volume e miniatura da imagem já na memória.
	prep, err = prepararFoto(ctx, tipoDeFoto{mimePNG, "png"}, ler(func() []byte {
		var b bytes.Buffer
		_ = png.Encode(&b, degrade(3000, 2000))
		return b.Bytes()
	}()))
	if err != nil || prep.ComoVeio || prep.Mime != mimeJPEG || prep.Ext != "jpg" {
		t.Fatalf("PNG convertível vira JPEG: %+v %v", prep, err)
	}
	if cfg, err := jpeg.DecodeConfig(bytes.NewReader(prep.Miniatura)); err != nil || cfg.Width != 480 || cfg.Height != 320 {
		t.Fatalf("miniatura da convertida 1280×853: %d×%d (%v), esperado 480×320", cfg.Width, cfg.Height, err)
	}

	// Contexto cancelado não converte.
	cancelado, cancelar := context.WithCancel(ctx)
	cancelar()
	semaforoDeDecodificacao <- struct{}{} // trava ocupada: só o contexto libera a espera
	defer func() { <-semaforoDeDecodificacao }()
	if _, err := prepararFoto(cancelado, tipoDeFoto{mimeJPEG, "jpg"}, ler(corrompida)); !errors.Is(err, context.Canceled) {
		t.Fatalf("com a trava ocupada e o contexto cancelado, a espera termina com o erro do contexto: %v", err)
	}
}
