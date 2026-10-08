package bens

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
)

// A miniatura é feita com a biblioteca padrão, sem dependência nova (o go.mod
// é decisão do tech-lead): decodifica JPEG/PNG, reduz por MÉDIA DE ÁREA escrita
// à mão e codifica JPEG. WebP não tem decoder na stdlib — fica sem miniatura,
// `thumb_key` nulo e `thumb_url` igual a `url`, como o contrato permite.
//
// Não é refinamento: uma grade de 200 bens baixando fotos de 15 MB no celular,
// dentro do apartamento, não é tela utilizável.

const (
	// ladoDaMiniatura é o maior lado da miniatura, em pixels: a célula da grade
	// tem ~240 px CSS, e 480 cobre tela de densidade 2x.
	ladoDaMiniatura = 480
	qualidadeJPEG   = 80

	// orcamentoDeMemoria é o teto do bitmap DECODIFICADO, estimado pelo
	// DecodeConfig antes de decodificar. 15 MB de PNG bem comprimido podem
	// virar gigabytes na memória (a "bomba de descompressão"); acima do teto a
	// foto é aceita e só fica sem miniatura.
	orcamentoDeMemoria = 256 << 20
)

// semaforoDeDecodificacao serializa TODA decodificação de foto — a conversão
// no envio (conversao.go) e a miniatura: duas fotos de 48 MP decodificadas ao
// mesmo tempo dobram o pico de memória do processo, e o ganho de paralelismo
// aqui é nenhum — o gargalo do envio é a rede do celular.
var semaforoDeDecodificacao = make(chan struct{}, 1)

var errSemOrcamento = errors.New("bens: foto grande demais para decodificar com segurança")

// dimensoes é o tamanho da foto como ela APARECE (já com a orientação EXIF):
// é o que a tela usa para reservar o espaço da imagem.
type dimensoes struct {
	largura, altura int
}

// medirFoto lê só o cabeçalho e devolve as dimensões de exibição. Falha de
// leitura não invalida o envio: as colunas são anuláveis.
func medirFoto(caminho, mime string) (*dimensoes, error) {
	f, err := os.Open(caminho)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	switch mime {
	case mimeWebP:
		cabeca := make([]byte, 64)
		n, err := io.ReadFull(f, cabeca)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, err
		}
		return dimensoesDoWebP(cabeca[:n])
	case mimeJPEG:
		cfg, err := jpeg.DecodeConfig(f)
		if err != nil {
			return nil, err
		}
		d := dimensoes{cfg.Width, cfg.Height}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		if trocaEixos(orientacaoDoJPEG(f)) {
			d.largura, d.altura = d.altura, d.largura
		}
		return &d, nil
	case mimePNG:
		cfg, err := png.DecodeConfig(f)
		if err != nil {
			return nil, err
		}
		return &dimensoes{cfg.Width, cfg.Height}, nil
	}
	return nil, fmt.Errorf("bens: tipo sem medição: %s", mime)
}

// gerarMiniatura decodifica a foto JÁ GUARDADA no volume, reduz e devolve o
// JPEG da miniatura. É o caminho de quem não tem a imagem na memória — o
// importador reaproveitando um arquivo que já estava no volume. Foto nova
// gera a miniatura da imagem convertida (prepararFoto), sem decodificar duas
// vezes. WebP devolve (nil, nil): sem miniatura, sem erro.
func gerarMiniatura(ctx context.Context, caminho, mime string) ([]byte, error) {
	if mime != mimeJPEG && mime != mimePNG {
		return nil, nil
	}

	select {
	case semaforoDeDecodificacao <- struct{}{}:
		defer func() { <-semaforoDeDecodificacao }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	f, err := os.Open(caminho)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	orientacao := 1
	var (
		cfg    image.Config
		decode func(io.Reader) (image.Image, error)
	)
	if mime == mimeJPEG {
		orientacao = orientacaoDoJPEG(f)
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		cfg, err = jpeg.DecodeConfig(f)
		decode = jpeg.Decode
	} else {
		cfg, err = png.DecodeConfig(f)
		decode = png.Decode
	}
	if err != nil {
		return nil, err
	}
	if !cabeNoOrcamento(cfg) {
		return nil, errSemOrcamento
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	img, err := decode(f)
	if err != nil {
		return nil, err
	}
	return codificarMiniatura(img, orientacao)
}

// miniaturaDaImagem gera a miniatura de uma imagem já na memória e já em pé
// (a convertida no envio).
func miniaturaDaImagem(img image.Image) ([]byte, error) {
	return codificarMiniatura(img, 1)
}

// codificarMiniatura reduz ao lado da miniatura, aplica a orientação sobre a
// imagem já pequena e codifica JPEG.
func codificarMiniatura(img image.Image, orientacao int) ([]byte, error) {
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, errors.New("bens: foto sem pixels")
	}
	lw, lh := encaixarNoLado(b.Dx(), b.Dy(), ladoDaMiniatura)
	reduzida := orientar(reduzir(img, lw, lh), orientacao)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, reduzida, &jpeg.Options{Quality: qualidadeJPEG}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// bytesPorPixel estima o bitmap que o decoder vai alocar. Superestimar é o
// lado seguro: a consequência é só ficar sem miniatura.
func bytesPorPixel(m color.Model) int64 {
	switch m {
	case color.GrayModel, color.AlphaModel:
		return 1
	case color.Gray16Model, color.Alpha16Model:
		return 2
	case color.YCbCrModel, color.NYCbCrAModel:
		return 3
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	default:
		return 4
	}
}

// encaixarNoLado encaixa w×h num quadrado de lado `lado`, mantendo a
// proporção — a miniatura (480) e a foto guardada (1280). Nunca AMPLIA: foto
// menor que o lado sai do mesmo tamanho.
func encaixarNoLado(w, h, lado int) (int, int) {
	maior := max(w, h)
	if maior <= lado {
		return w, h
	}
	lw := max(1, (w*lado+maior/2)/maior)
	lh := max(1, (h*lado+maior/2)/maior)
	return lw, lh
}

// leitorDePixel devolve a cor de (x, y) em RGBA de 16 bits PRÉ-MULTIPLICADO.
type leitorDePixel func(x, y int) (r, g, b, a uint32)

// leitorDe escolhe o caminho rápido para os formatos que os decoders da stdlib
// entregam com mais frequência (YCbCr do JPEG; NRGBA/RGBA do PNG) e cai no
// `At()` genérico para o resto (Paletted, Gray, CMYK…). A redução visita CADA
// pixel da foto uma vez — numa foto de 12 MP, a diferença entre o acesso direto
// e a interface `color.Color` é a diferença entre décimos de segundo e segundos.
func leitorDe(img image.Image) leitorDePixel {
	switch m := img.(type) {
	case *image.YCbCr:
		return func(x, y int) (uint32, uint32, uint32, uint32) {
			yi, ci := m.YOffset(x, y), m.COffset(x, y)
			r, g, b := color.YCbCrToRGB(m.Y[yi], m.Cb[ci], m.Cr[ci])
			return uint32(r) * 0x101, uint32(g) * 0x101, uint32(b) * 0x101, 0xffff
		}
	case *image.NRGBA:
		return func(x, y int) (uint32, uint32, uint32, uint32) {
			i := m.PixOffset(x, y)
			return color.NRGBA{R: m.Pix[i], G: m.Pix[i+1], B: m.Pix[i+2], A: m.Pix[i+3]}.RGBA()
		}
	case *image.RGBA:
		return func(x, y int) (uint32, uint32, uint32, uint32) {
			i := m.PixOffset(x, y)
			return uint32(m.Pix[i]) * 0x101, uint32(m.Pix[i+1]) * 0x101, uint32(m.Pix[i+2]) * 0x101, uint32(m.Pix[i+3]) * 0x101
		}
	default:
		return func(x, y int) (uint32, uint32, uint32, uint32) { return img.At(x, y).RGBA() }
	}
}

// reduzir faz o downscale por MÉDIA DE ÁREA ("box filter"): cada pixel da
// miniatura é a média de todos os pixels da foto que caem no retângulo dele.
// É o filtro certo para REDUZIR — vizinho mais próximo serrilha, e bilinear
// sem pré-filtro ignora a maior parte dos pixels e cintila em textura fina
// (toalha listrada, talher, renda).
//
// Os retângulos são de borda inteira: a coluna x da foto pertence à coluna
// x·lw/w da miniatura, e a linha y à linha y·lh/h. Como lw ≤ w e lh ≤ h, toda
// célula recebe ao menos um pixel e todo pixel cai em exatamente uma célula.
// Percorre a foto linha a linha, na ordem da memória.
//
// A transparência do PNG é composta sobre BRANCO: JPEG não tem alfa, e o fundo
// preto que sobraria de um pré-multiplicado cru transformaria o prato
// recortado num prato numa sala escura.
func reduzir(img image.Image, lw, lh int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, lw, lh))
	ler := leitorDe(img)

	coluna := make([]int, w)
	for x := range w {
		coluna[x] = x * lw / w
	}
	sr := make([]uint64, lw)
	sg := make([]uint64, lw)
	sb := make([]uint64, lw)
	sa := make([]uint64, lw)
	n := make([]uint64, lw)

	y := 0
	for ly := range lh {
		clear(sr)
		clear(sg)
		clear(sb)
		clear(sa)
		clear(n)
		for fim := (ly + 1) * h / lh; y < fim; y++ {
			for x := range w {
				r, g, bl, a := ler(b.Min.X+x, b.Min.Y+y)
				c := coluna[x]
				sr[c] += uint64(r)
				sg[c] += uint64(g)
				sb[c] += uint64(bl)
				sa[c] += uint64(a)
				n[c]++
			}
		}
		for lx := range lw {
			if n[lx] == 0 {
				continue
			}
			a := sa[lx] / n[lx]
			fundo := 0xffff - a // sobre branco: c_premult + branco·(1 − α)
			i := out.PixOffset(lx, ly)
			out.Pix[i] = uint8((sr[lx]/n[lx] + fundo) >> 8)
			out.Pix[i+1] = uint8((sg[lx]/n[lx] + fundo) >> 8)
			out.Pix[i+2] = uint8((sb[lx]/n[lx] + fundo) >> 8)
			out.Pix[i+3] = 0xff
		}
	}
	return out
}

// orientar aplica a orientação EXIF (1–8) aos pixels. O decoder da stdlib
// IGNORA o EXIF, e o navegador o RESPEITA ao mostrar a foto — sem isto a foto
// tirada com o celular em pé sairia deitada. Roda sobre a imagem já reduzida
// (a miniatura, no máximo 480; a foto guardada, no máximo 1280).
func orientar(src *image.RGBA, orientacao int) *image.RGBA {
	if orientacao < 2 || orientacao > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if trocaEixos(orientacao) {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for dy := range dh {
		for dx := range dw {
			var sx, sy int
			switch orientacao {
			case 2: // espelho horizontal
				sx, sy = w-1-dx, dy
			case 3: // 180°
				sx, sy = w-1-dx, h-1-dy
			case 4: // espelho vertical
				sx, sy = dx, h-1-dy
			case 5: // transposição
				sx, sy = dy, dx
			case 6: // 90° horário
				sx, sy = dy, h-1-dx
			case 7: // transversa
				sx, sy = w-1-dy, h-1-dx
			case 8: // 90° anti-horário
				sx, sy = w-1-dy, dx
			}
			copy(dst.Pix[dst.PixOffset(dx, dy):dst.PixOffset(dx, dy)+4], src.Pix[src.PixOffset(sx, sy):src.PixOffset(sx, sy)+4])
		}
	}
	return dst
}

// trocaEixos diz se a orientação gira 90°/270° (largura e altura trocam).
func trocaEixos(orientacao int) bool { return orientacao >= 5 && orientacao <= 8 }

// tetoDoCabecalhoJPEG limita quanto se lê procurando o EXIF: ele mora no APP1,
// logo no começo; 256 KB cobrem com folga até EXIF com miniatura embutida.
const tetoDoCabecalhoJPEG = 256 << 10

// orientacaoDoJPEG lê a tag 0x0112 (Orientation) do IFD0 do EXIF. Qualquer
// coisa fora do esperado devolve 1 (normal): orientação errada é feio,
// derrubar o envio por causa dela seria pior.
func orientacaoDoJPEG(r io.Reader) int {
	buf, err := io.ReadAll(io.LimitReader(r, tetoDoCabecalhoJPEG))
	if err != nil || len(buf) < 4 || buf[0] != 0xFF || buf[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(buf) {
		if buf[i] != 0xFF {
			return 1
		}
		marcador := buf[i+1]
		if marcador == 0xFF { // preenchimento entre segmentos
			i++
			continue
		}
		if marcador == 0xDA || marcador == 0xD9 { // início dos dados / fim
			return 1
		}
		tamanho := int(binary.BigEndian.Uint16(buf[i+2 : i+4]))
		if tamanho < 2 || i+2+tamanho > len(buf) {
			return 1
		}
		seg := buf[i+4 : i+2+tamanho]
		if marcador == 0xE1 && len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
			return orientacaoDoTIFF(seg[6:])
		}
		i += 2 + tamanho
	}
	return 1
}

func orientacaoDoTIFF(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var ordem binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		ordem = binary.LittleEndian
	case "MM":
		ordem = binary.BigEndian
	default:
		return 1
	}
	if ordem.Uint16(t[2:4]) != 42 {
		return 1
	}
	ifd := int(ordem.Uint32(t[4:8]))
	if ifd < 8 || ifd+2 > len(t) {
		return 1
	}
	entradas := int(ordem.Uint16(t[ifd : ifd+2]))
	for k := range entradas {
		e := ifd + 2 + k*12
		if e+12 > len(t) {
			return 1
		}
		if ordem.Uint16(t[e:e+2]) == 0x0112 && ordem.Uint16(t[e+2:e+4]) == 3 { // SHORT
			v := int(ordem.Uint16(t[e+8 : e+10]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// dimensoesDoWebP lê largura e altura do cabeçalho RIFF/WEBP, nos três
// formatos de bitstream: VP8X (estendido), VP8 (com perda) e VP8L (sem perda).
// Não decodifica nada — a stdlib não tem decoder de WebP.
func dimensoesDoWebP(b []byte) (*dimensoes, error) {
	if len(b) < 30 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return nil, errors.New("bens: cabeçalho WebP curto ou inválido")
	}
	le24 := func(p []byte) int { return int(p[0]) | int(p[1])<<8 | int(p[2])<<16 }
	switch string(b[12:16]) {
	case "VP8X":
		return &dimensoes{1 + le24(b[24:27]), 1 + le24(b[27:30])}, nil
	case "VP8 ":
		if b[23] != 0x9d || b[24] != 0x01 || b[25] != 0x2a {
			return nil, errors.New("bens: VP8 sem código de início")
		}
		w := int(binary.LittleEndian.Uint16(b[26:28]) & 0x3fff)
		h := int(binary.LittleEndian.Uint16(b[28:30]) & 0x3fff)
		return &dimensoes{w, h}, nil
	case "VP8L":
		if b[20] != 0x2f {
			return nil, errors.New("bens: VP8L sem assinatura")
		}
		bits := binary.LittleEndian.Uint32(b[21:25])
		return &dimensoes{int(bits&0x3fff) + 1, int((bits>>14)&0x3fff) + 1}, nil
	}
	return nil, errors.New("bens: WebP de formato desconhecido")
}
