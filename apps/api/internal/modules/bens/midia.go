package bens

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

// Foto de bem no volume de mídia da API.
//
// Mesmas regras da mídia do site (internal/modules/site/midia.go), com três
// diferenças de assunto:
//   - a tabela é `inventory_media`, com `property_id` — não `site_media`;
//   - os arquivos moram num SUBDIRETÓRIO próprio do volume (SubdiretorioDoVolume):
//     a foto do inventário mostra o interior da casa, e quem opera o volume
//     precisa enxergar, sem consultar o banco, o que é foto de divulgação e o
//     que não é;
//   - a entrega é AUTENTICADA (`inventory.goods:ver`) e o cache é `private`.

// SubdiretorioDoVolume é onde as fotos de bens ficam, dentro de MEDIA_DIR.
const SubdiretorioDoVolume = "bens"

// Limites do envio.
const (
	LimiteDaFoto int64 = 15 << 20 // 15 MB

	// LimiteDoCorpo é o teto do corpo multipart inteiro: a foto mais folga para
	// os cabeçalhos das partes.
	LimiteDoCorpo = LimiteDaFoto + 1<<20

	tamanhoDaCabeca  = 512
	maxNomeOriginal  = 255
	permissaoArquivo = 0o640
	permissaoPasta   = 0o750
)

// Tipos aceitos — detectados pelos BYTES, nunca pela extensão.
const (
	mimeJPEG = "image/jpeg"
	mimePNG  = "image/png"
	mimeWebP = "image/webp"
)

// Mensagens em linguagem de gestor — vão em `details.file`.
const (
	msgFotoInvalida = "A foto precisa ser JPG, PNG ou WebP, até 15 MB."
	msgSemArquivo   = "Escolha uma foto para enviar."
	msgEnvioCortado = "O envio foi interrompido antes de terminar. Tente de novo."
)

// Entidades da trilha — nomes de TABELA, que é o que quem lê `audit_log` abre
// em seguida.
const (
	entidadeAmbiente    = "unit_rooms"
	entidadeBem         = "inventory_items"
	entidadeColocacao   = "room_inventory"
	entidadeMidia       = "inventory_media"
	entidadeConferencia = "inventory_counts"
	entidadeLinha       = "inventory_count_lines"
	entidadeAvaria      = "inventory_issues"
)

// ErroDeArquivo monta o 422 do envio, com o motivo em `details.file`.
func ErroDeArquivo(msg string) *apperr.Error {
	return apperr.Validation(map[string]string{"file": msg})
}

type tipoDeFoto struct {
	mime string
	ext  string
}

// detectarFoto decide o tipo pelos primeiros bytes. WebP é conferido à mão
// (RIFF....WEBP) porque o sniffer da stdlib não o distingue com segurança;
// JPEG e PNG ficam com http.DetectContentType. Vídeo NÃO entra: o que
// identifica um prato é uma foto.
func detectarFoto(cabeca []byte) (tipoDeFoto, bool) {
	if len(cabeca) >= 12 && bytes.Equal(cabeca[0:4], []byte("RIFF")) && bytes.Equal(cabeca[8:12], []byte("WEBP")) {
		return tipoDeFoto{mimeWebP, "webp"}, true
	}
	switch http.DetectContentType(cabeca) {
	case mimeJPEG:
		return tipoDeFoto{mimeJPEG, "jpg"}, true
	case mimePNG:
		return tipoDeFoto{mimePNG, "png"}, true
	}
	return tipoDeFoto{}, false
}

// nomeOriginal guarda o nome que o arquivo tinha no aparelho, SÓ para exibição:
// sem diretório, sem caractere de controle, com teto. Nunca monta caminho.
func nomeOriginal(nome string) string {
	nome = filepath.Base(strings.ReplaceAll(nome, "\\", "/"))
	nome = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, nome)
	nome = strings.TrimSpace(nome)
	if nome == "" || nome == "." || nome == "/" {
		nome = "foto"
	}
	if utf8.RuneCountInString(nome) > maxNomeOriginal {
		nome = string([]rune(nome)[:maxNomeOriginal])
	}
	return nome
}

// caminhoNoVolume monta o caminho a partir da chave, sem deixá-la escapar do
// diretório. A chave é sempre `<uuid>.<ext>` gerada aqui; qualquer outra forma
// é defeito e vira 500, nunca leitura fora da pasta.
func (s *Service) caminhoNoVolume(chave string) (string, error) {
	if s.dir == "" {
		return "", apperr.Internal.WithCause(errors.New("bens: diretório de mídia não configurado (MEDIA_DIR)"))
	}
	if chave == "" || chave != filepath.Base(chave) || strings.HasPrefix(chave, ".") {
		return "", apperr.Internal.WithCause(fmt.Errorf("bens: storage_key suspeita %q", chave))
	}
	return filepath.Join(s.dir, chave), nil
}

// EnviarFoto grava a foto no volume, gera a miniatura e registra a linha.
//
// A ordem mantém volume e banco coerentes: o arquivo é escrito num temporário
// DENTRO da pasta (rename só é atômico no mesmo sistema de arquivos), renomeado
// para o nome final, a miniatura é gravada ao lado, e só então a linha nasce.
// Se o registro falhar, os dois arquivos são apagados — não sobra arquivo sem
// linha. A miniatura falhar NÃO falha o envio: o original continua valendo.
func (s *Service) EnviarFoto(ctx context.Context, corpo io.Reader, nome string) (Midia, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Midia{}, err
	}
	if s.dir == "" {
		return Midia{}, apperr.Internal.WithCause(errors.New("bens: diretório de mídia não configurado (MEDIA_DIR)"))
	}
	if err := os.MkdirAll(s.dir, permissaoPasta); err != nil {
		return Midia{}, apperr.Internal.WithCause(fmt.Errorf("bens: preparando %s: %w", s.dir, err))
	}

	cabeca := make([]byte, tamanhoDaCabeca)
	n, err := io.ReadFull(corpo, cabeca)
	switch {
	case errors.Is(err, io.EOF):
		return Midia{}, ErroDeArquivo(msgSemArquivo)
	case errors.Is(err, io.ErrUnexpectedEOF):
		// Arquivo menor que 512 bytes: é tudo que há.
	case err != nil:
		return Midia{}, erroDeLeitura(err)
	}
	cabeca = cabeca[:n]

	tipo, ok := detectarFoto(cabeca)
	if !ok {
		return Midia{}, ErroDeArquivo(msgFotoInvalida)
	}

	id := uuid.New()
	chave := id.String() + "." + tipo.ext
	final, err := s.caminhoNoVolume(chave)
	if err != nil {
		return Midia{}, err
	}
	total, err := gravarNoVolume(ctx, s.dir, final, cabeca, corpo)
	if err != nil {
		return Midia{}, err
	}
	gravados := []string{final}
	desfazer := func() {
		for _, arq := range gravados {
			if err := os.Remove(arq); err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.ErrorContext(ctx, "bens: arquivo sem registro ficou no volume", "arquivo", arq, "err", err)
			}
		}
	}

	m := registroDeMidia{
		ID: id, PropriedadeID: prop, Mime: tipo.mime, Bytes: total,
		NomeOriginal: nomeOriginal(nome), ChaveArquivo: chave, CriadoPor: atorOuNulo(ctx),
	}
	if d, err := medirFoto(final, tipo.mime); err != nil {
		slog.WarnContext(ctx, "bens: não consegui medir a foto", "media_id", id, "err", err)
	} else {
		m.Largura, m.Altura = &d.largura, &d.altura
	}

	if mini, err := gerarMiniatura(ctx, final, tipo.mime); err != nil {
		slog.WarnContext(ctx, "bens: foto sem miniatura; a grade usa o original", "media_id", id, "err", err)
	} else if mini != nil {
		chaveMini := id.String() + ".thumb.jpg"
		caminhoMini, err := s.caminhoNoVolume(chaveMini)
		if err != nil {
			desfazer()
			return Midia{}, err
		}
		if _, err := gravarNoVolume(ctx, s.dir, caminhoMini, nil, bytes.NewReader(mini)); err != nil {
			slog.WarnContext(ctx, "bens: miniatura não gravada; a grade usa o original", "media_id", id, "err", err)
		} else {
			gravados = append(gravados, caminhoMini)
			m.ChaveMiniatura = &chaveMini
		}
	}

	err = s.tx.Do(ctx, func(ctx context.Context) error {
		criada, err := s.repo.CriarMidia(ctx, m)
		if err != nil {
			return err
		}
		m = criada
		return audit.Criacao(ctx, s.repo.pool, entidadeMidia, audit.VerboCriado, m.ID, m)
	})
	if err != nil {
		desfazer()
		return Midia{}, err
	}
	return m.publica(), nil
}

// gravarNoVolume escreve `cabeca` + `resto` num temporário da pasta, aplica o
// teto de 15 MB, sincroniza e renomeia para `final`. Devolve o total de bytes.
func gravarNoVolume(ctx context.Context, dir, final string, cabeca []byte, resto io.Reader) (int64, error) {
	tmp, err := os.CreateTemp(dir, ".envio-*")
	if err != nil {
		return 0, apperr.Internal.WithCause(fmt.Errorf("bens: criando temporário: %w", err))
	}
	nomeTmp := tmp.Name()
	renomeado := false
	defer func() {
		if !renomeado {
			_ = tmp.Close()
			if err := os.Remove(nomeTmp); err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.WarnContext(ctx, "bens: temporário de envio ficou no volume", "arquivo", nomeTmp, "err", err)
			}
		}
	}()

	if _, err := tmp.Write(cabeca); err != nil {
		return 0, apperr.Internal.WithCause(err)
	}
	// +1 para enxergar o byte que passa do teto sem ler o resto do corpo.
	copiados, err := io.Copy(tmp, io.LimitReader(resto, LimiteDaFoto-int64(len(cabeca))+1))
	total := int64(len(cabeca)) + copiados
	if err != nil {
		return 0, erroDeLeitura(err)
	}
	if total > LimiteDaFoto {
		return 0, ErroDeArquivo(msgFotoInvalida)
	}
	if err := tmp.Sync(); err != nil {
		return 0, apperr.Internal.WithCause(err)
	}
	if err := tmp.Close(); err != nil {
		return 0, apperr.Internal.WithCause(err)
	}
	if err := os.Chmod(nomeTmp, permissaoArquivo); err != nil {
		return 0, apperr.Internal.WithCause(err)
	}
	if err := os.Rename(nomeTmp, final); err != nil {
		return 0, apperr.Internal.WithCause(fmt.Errorf("bens: renomeando envio: %w", err))
	}
	renomeado = true
	return total, nil
}

// erroDeLeitura traduz a falha ao ler o corpo: o teto do corpo inteiro
// (http.MaxBytesReader) é "grande demais"; o resto é envio cortado.
func erroDeLeitura(err error) error {
	var grande *http.MaxBytesError
	if errors.As(err, &grande) {
		return ErroDeArquivo(msgFotoInvalida)
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return ErroDeArquivo(msgEnvioCortado).WithCause(err)
}

// FotoAberta é o arquivo pronto para a entrega.
type FotoAberta struct {
	Arquivo *os.File
	Mime    string
	Midia   registroDeMidia
}

// AbrirFoto devolve o arquivo da foto (ou da miniatura). Quem chama fecha.
// Foto de outra casa é 404, como a que não existe.
func (s *Service) AbrirFoto(ctx context.Context, id uuid.UUID, miniatura bool) (FotoAberta, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return FotoAberta{}, err
	}
	m, ok, err := s.repo.BuscarMidia(ctx, prop, id)
	if err != nil {
		return FotoAberta{}, err
	}
	if !ok {
		return FotoAberta{}, apperr.NotFound("Foto")
	}

	chave, mime := m.ChaveArquivo, m.Mime
	if miniatura && m.ChaveMiniatura != nil {
		chave, mime = *m.ChaveMiniatura, mimeJPEG
	}
	caminho, err := s.caminhoNoVolume(chave)
	if err != nil {
		return FotoAberta{}, err
	}
	f, err := os.Open(caminho)
	if errors.Is(err, os.ErrNotExist) {
		// Linha sem arquivo: volume perdido ou restaurado pela metade. Para
		// quem pede é 404; para quem opera, um aviso no log.
		slog.WarnContext(ctx, "bens: foto registrada sem arquivo no volume", "media_id", id, "arquivo", caminho)
		return FotoAberta{}, apperr.NotFound("Foto")
	}
	if err != nil {
		return FotoAberta{}, apperr.Internal.WithCause(err)
	}
	return FotoAberta{Arquivo: f, Mime: mime, Midia: m}, nil
}
