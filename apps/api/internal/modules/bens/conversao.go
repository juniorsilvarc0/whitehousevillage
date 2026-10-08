package bens

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
)

// Conversão da foto no envio (contrato: `POST /inventory/media`).
//
// JPEG e PNG enviados são guardados CONVERTIDOS: lado maior até 1280 px (só
// reduz, nunca amplia), JPEG qualidade 75, sem metadados. O motivo é disco,
// medido: foto de celular chega com 3 a 5 MB, e o inventário serve para
// reconhecer o objeto, não para imprimir.
//
//   - a orientação EXIF é APLICADA aos pixels antes, e o arquivo guardado sai
//     sem EXIF nenhum — a foto tirada em pé continua em pé, e a localização do
//     celular não vai para o volume;
//   - PNG com transparência é achatado sobre BRANCO (JPEG não tem alfa);
//   - a redução é a média de área da miniatura (`reduzir`), e a orientação é a
//     mesma `orientar`: uma implementação só para as duas;
//   - JPEG que já cabe em 1280 px, sem orientação a aplicar, e cuja
//     recodificação NÃO sai menor é guardado como veio, só sem os metadados
//     (regraDaFonteMantida): recodificar JPEG já comprimido perde qualidade e,
//     medido, cresce o arquivo.
//
// Exceções, guardadas COMO VIERAM (quem chama avisa no log e não recusa):
// WebP (sem decoder na biblioteca padrão, e formato raro não justifica
// dependência nova) e foto grande demais para decodificar com segurança
// (`orcamentoDeMemoria`, o mesmo teto da miniatura).
//
// Uma função só, pura — bytes de entrada, bytes JPEG de saída —, usada pelo
// envio do painel e pelo importador do levantamento.

const (
	// ladoDaFotoGuardada é o maior lado da foto no volume.
	ladoDaFotoGuardada = 1280
	// qualidadeDaFotoGuardada é a qualidade JPEG da foto no volume. A
	// miniatura continua com a dela (qualidadeJPEG).
	qualidadeDaFotoGuardada = 75
)

// errNaoConvertivel é o formato que a conversão não lê (WebP).
var errNaoConvertivel = errors.New("bens: só JPEG e PNG são convertidos")

// fotoConvertida é o que vai para o volume, e a imagem de onde sai a miniatura.
type fotoConvertida struct {
	// Dados é o JPEG a guardar.
	Dados []byte
	// Largura e Altura são as do arquivo guardado (já em pé).
	Largura, Altura int
	// Imagem é a foto já reduzida e em pé, na memória: a miniatura sai dela
	// sem decodificar o arquivo de novo.
	Imagem image.Image
	// FonteMantida diz que o guardado é a PRÓPRIA fonte, só sem metadados
	// (ver regraDaFonteMantida).
	FonteMantida bool
}

// converterFoto decodifica a foto (JPEG ou PNG), aplica a orientação EXIF,
// reduz o lado maior a 1280 px e codifica JPEG qualidade 75 — sem metadados,
// porque o codificador da biblioteca padrão não escreve APP1, APP2 nem COM.
//
// Erros: errNaoConvertivel (WebP), errSemOrcamento (grande demais para a
// memória) e os do decoder (foto corrompida ou numa variante que a stdlib não
// lê, como JPEG aritmético ou de 12 bits). Quem chama decide o que fazer.
func converterFoto(dados []byte, mime string) (fotoConvertida, error) {
	orientacao := 1
	var decode func(io.Reader) (image.Image, error)
	switch mime {
	case mimeJPEG:
		orientacao = orientacaoDoJPEG(bytes.NewReader(dados))
		decode = jpeg.Decode
	case mimePNG:
		decode = png.Decode
	default:
		return fotoConvertida{}, errNaoConvertivel
	}
	cfg, err := configDaImagem(bytes.NewReader(dados), mime)
	if err != nil {
		return fotoConvertida{}, fmt.Errorf("bens: cabeçalho da foto ilegível: %w", err)
	}
	if !cabeNoOrcamento(cfg) {
		return fotoConvertida{}, errSemOrcamento
	}
	img, err := decode(bytes.NewReader(dados))
	if err != nil {
		return fotoConvertida{}, fmt.Errorf("bens: foto não decodifica: %w", err)
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return fotoConvertida{}, errors.New("bens: foto sem pixels")
	}

	// Reduz ANTES de girar: a rotação roda sobre a foto já pequena. O lado
	// maior não muda com a rotação, então o encaixe vale nas duas ordens.
	lw, lh := encaixarNoLado(b.Dx(), b.Dy(), ladoDaFotoGuardada)
	emPe := orientar(reduzir(img, lw, lh), orientacao)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, emPe, &jpeg.Options{Quality: qualidadeDaFotoGuardada}); err != nil {
		return fotoConvertida{}, err
	}
	out := fotoConvertida{
		Dados: buf.Bytes(), Largura: emPe.Bounds().Dx(), Altura: emPe.Bounds().Dy(), Imagem: emPe,
	}
	if regraDaFonteMantida(mime, orientacao, lw == b.Dx() && lh == b.Dy()) {
		if limpa, ok := jpegSemMetadados(dados); ok && len(limpa) <= len(out.Dados) {
			out.Dados, out.FonteMantida = limpa, true
		}
	}
	return out, nil
}

// regraDaFonteMantida decide quando a FONTE pode ser guardada em vez da
// convertida: JPEG, já dentro de 1280 px, sem orientação a aplicar. Nesse caso
// a conversão não muda geometria nem orientação — só recodifica —, e
// recodificar um JPEG já comprimido perde qualidade e, medido no levantamento
// das casas (fotos já em 1280 px e qualidade 75, de outro codificador),
// AUMENTA o arquivo: o codificador da biblioteca padrão usa as tabelas de
// Huffman genéricas, sem otimização. Então, se a convertida não for menor, a
// fonte é guardada — mas SEM METADADOS (jpegSemMetadados), porque a regra do
// contrato é "sem metadados" em qualquer caso, inclusive a localização.
func regraDaFonteMantida(mime string, orientacao int, semReducao bool) bool {
	return mime == mimeJPEG && orientacao == 1 && semReducao
}

// configDaImagem lê só o cabeçalho de imagem (dimensões e modelo de cor).
func configDaImagem(r io.Reader, mime string) (image.Config, error) {
	switch mime {
	case mimeJPEG:
		return jpeg.DecodeConfig(r)
	case mimePNG:
		return png.DecodeConfig(r)
	}
	return image.Config{}, errNaoConvertivel
}

// cabeNoOrcamento estima o bitmap que o decoder vai alocar e confere contra
// `orcamentoDeMemoria`. Superestimar é o lado seguro: a consequência é a foto
// ser guardada como veio, sem miniatura.
func cabeNoOrcamento(cfg image.Config) bool {
	return int64(cfg.Width)*int64(cfg.Height)*bytesPorPixel(cfg.ColorModel) <= orcamentoDeMemoria
}

// jpegSemMetadados devolve o mesmo JPEG sem os segmentos de metadados — EXIF e
// XMP (APP1), ICC e MPF (APP2), IPTC (APP13), os demais APPn e os comentários
// (COM) —, sem recodificar nada: os dados comprimidos são copiados byte a byte.
//
// Ficam o APP0 "JFIF" (densidade, inofensivo) e o APP14 "Adobe", que diz ao
// decoder como converter as cores — tirá-lo mudaria a cor de JPEG CMYK/YCCK. A
// cópia para no EOI da imagem principal: o que vem DEPOIS dele (as imagens
// secundárias que o celular anexa, como mapa de profundidade, com EXIF
// próprio) não é copiado.
//
// ok=false quando a estrutura não é a esperada; aí quem chama fica com a
// convertida, que é o caminho seguro.
func jpegSemMetadados(dados []byte) ([]byte, bool) {
	if len(dados) < 4 || dados[0] != 0xFF || dados[1] != 0xD8 {
		return nil, false
	}
	out := make([]byte, 0, len(dados))
	out = append(out, 0xFF, 0xD8)
	i := 2
	for {
		if i+2 > len(dados) || dados[i] != 0xFF {
			return nil, false
		}
		marcador := dados[i+1]
		switch {
		case marcador == 0xFF: // preenchimento
			i++
			continue
		case marcador == 0xD9: // EOI
			return append(out, 0xFF, 0xD9), true
		case marcador == 0x01 || (marcador >= 0xD0 && marcador <= 0xD8):
			// TEM, RSTn e SOI repetido não aparecem fora dos dados comprimidos.
			return nil, false
		}
		if i+4 > len(dados) {
			return nil, false
		}
		tamanho := int(dados[i+2])<<8 | int(dados[i+3])
		fim := i + 2 + tamanho
		if tamanho < 2 || fim > len(dados) {
			return nil, false
		}
		if !metadadoDescartavel(marcador, dados[i+4:fim]) {
			out = append(out, dados[i:fim]...)
		}
		i = fim
		if marcador != 0xDA { // SOS: depois do cabeçalho vêm os dados comprimidos
			continue
		}
		// Copia os dados comprimidos até o próximo marcador de verdade. Dentro
		// deles, 0xFF só aparece seguido de 0x00 (byte de enchimento) ou de RSTn.
		for {
			if i+1 >= len(dados) {
				return nil, false
			}
			if dados[i] == 0xFF {
				prox := dados[i+1]
				if prox != 0x00 && (prox < 0xD0 || prox > 0xD7) {
					break
				}
				out = append(out, 0xFF, prox)
				i += 2
				continue
			}
			out = append(out, dados[i])
			i++
		}
	}
}

// metadadoDescartavel diz se o segmento é metadado que não vai para o volume.
func metadadoDescartavel(marcador byte, corpo []byte) bool {
	switch {
	case marcador == 0xFE: // COM
		return true
	case marcador == 0xE0: // APP0: fica só o JFIF
		return !bytes.HasPrefix(corpo, []byte("JFIF\x00"))
	case marcador == 0xEE: // APP14 "Adobe": transformação de cor, fica
		return !bytes.HasPrefix(corpo, []byte("Adobe"))
	case marcador >= 0xE1 && marcador <= 0xEF:
		return true
	}
	return false
}

// fotoPreparada é a foto pronta para o volume: convertida, ou como veio.
type fotoPreparada struct {
	// Dados é o que vai para o volume. Nil quando a foto vai como veio e quem
	// chama já tem os bytes num arquivo (o envio renomeia o temporário).
	Dados []byte
	Mime  string
	Ext   string
	// Miniatura é o JPEG da miniatura, gerado da imagem já na memória. Nil
	// quando não há (WebP, foto grande demais, foto que não decodifica).
	Miniatura []byte
	// ComoVeio diz que nada foi convertido; Motivo diz por quê (para o log).
	ComoVeio bool
	Motivo   error
	// FonteMantida: o guardado é a fonte sem metadados (regraDaFonteMantida).
	FonteMantida bool
}

// prepararFoto é a entrada única do volume para foto nova — o envio do painel
// e o importador passam por aqui. Converte JPEG/PNG e gera a miniatura da
// imagem já decodificada (uma decodificação só), tudo sob a trava de "uma de
// cada vez" da decodificação. `ler` só é chamado com a trava tomada, para que
// apenas UMA foto esteja inteira na memória de cada vez.
//
// WebP, foto grande demais e foto que a stdlib não decodifica saem
// `ComoVeio`, com o motivo: o contrato manda guardar como veio e avisar, não
// recusar. Erro só de leitura ou de contexto cancelado.
func prepararFoto(ctx context.Context, tipo tipoDeFoto, ler func() ([]byte, error)) (fotoPreparada, error) {
	comoVeio := fotoPreparada{Mime: tipo.mime, Ext: tipo.ext, ComoVeio: true}
	if tipo.mime != mimeJPEG && tipo.mime != mimePNG {
		comoVeio.Motivo = errNaoConvertivel
		return comoVeio, nil
	}

	select {
	case semaforoDeDecodificacao <- struct{}{}:
		defer func() { <-semaforoDeDecodificacao }()
	case <-ctx.Done():
		return fotoPreparada{}, ctx.Err()
	}

	dados, err := ler()
	if err != nil {
		return fotoPreparada{}, err
	}
	conv, err := converterFoto(dados, tipo.mime)
	if err != nil {
		comoVeio.Dados, comoVeio.Motivo = dados, err
		return comoVeio, nil
	}
	out := fotoPreparada{
		Dados: conv.Dados, Mime: mimeJPEG, Ext: "jpg", FonteMantida: conv.FonteMantida,
	}
	if mini, err := miniaturaDaImagem(conv.Imagem); err != nil {
		out.Motivo = fmt.Errorf("bens: miniatura não gerada: %w", err)
	} else {
		out.Miniatura = mini
	}
	return out, nil
}
