package site

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

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

// Fotos e vídeos do site (docs/site-cms.md §1, "Fotos e vídeos" e "Limites").
//
// O arquivo vai para o volume da API (MEDIA_DIR) com um nome que NUNCA vem do
// usuário (`<uuid>.<ext>`), e o tipo é decidido pelos BYTES, não pela
// extensão: um .png que é texto é recusado.

// Limites por tipo de mídia.
const (
	LimiteImagem int64 = 15 << 20  // 15 MB
	LimiteVideo  int64 = 300 << 20 // 300 MB

	// LimiteDoCorpo é o teto do corpo multipart inteiro: o maior arquivo mais
	// folga para os cabeçalhos das partes. O handler o aplica com
	// http.MaxBytesReader antes de ler qualquer byte.
	LimiteDoCorpo = LimiteVideo + 1<<20

	tamanhoDaCabeca  = 512
	maxNomeOriginal  = 255
	permissaoArquivo = 0o640
)

// Mensagens em linguagem de gestor — vão em details.file.
const (
	msgFotoInvalida  = "A foto precisa ser JPG, PNG ou WebP, até 15 MB."
	msgVideoInvalido = "O vídeo precisa ser MP4 ou WebM, até 300 MB."
	msgTipoInvalido  = "O arquivo precisa ser uma foto (JPG, PNG ou WebP, até 15 MB) ou um vídeo (MP4 ou WebM, até 300 MB)."
	msgArquivoVazio  = "Escolha um arquivo para enviar."
	msgEnvioCortado  = "O envio foi interrompido antes de terminar. Tente de novo."
)

// ErroDeArquivo monta o 422 do upload.
func ErroDeArquivo(msg string) *apperr.Error {
	return apperr.Validation(map[string]string{"file": msg})
}

// tipoDetectado é o que os bytes dizem que o arquivo é.
type tipoDetectado struct {
	Tipo   Tipo
	Mime   string
	Ext    string
	Limite int64
}

// marcasMP4 são as "brands" do cabeçalho ftyp aceitas como MP4. Fica de fora
// o QuickTime (`qt  `, o .mov do iPhone) e os contêineres de imagem
// (heic, mif1, avif), que também usam ftyp.
var marcasMP4 = map[string]bool{
	"isom": true, "iso2": true, "iso3": true, "iso4": true, "iso5": true, "iso6": true,
	"mp41": true, "mp42": true, "mp4v": true, "avc1": true, "M4V ": true, "dash": true, "mmp4": true,
}

// detectar decide o tipo pelos primeiros bytes. Os três formatos que o
// sniffer da biblioteca padrão não distingue com segurança (WebP, WebM, MP4)
// são conferidos explicitamente; JPEG e PNG ficam com http.DetectContentType.
func detectar(cabeca []byte) (tipoDetectado, bool) {
	switch {
	case len(cabeca) >= 12 && bytes.Equal(cabeca[0:4], []byte("RIFF")) && bytes.Equal(cabeca[8:12], []byte("WEBP")):
		return tipoDetectado{TipoImagem, "image/webp", "webp", LimiteImagem}, true
	case len(cabeca) >= 4 && bytes.Equal(cabeca[0:4], []byte{0x1A, 0x45, 0xDF, 0xA3}):
		// EBML: Matroska e WebM começam igual; o DocType diz qual é.
		if bytes.Contains(cabeca, []byte("webm")) {
			return tipoDetectado{TipoVideo, "video/webm", "webm", LimiteVideo}, true
		}
		return tipoDetectado{}, false
	case len(cabeca) >= 12 && bytes.Equal(cabeca[4:8], []byte("ftyp")):
		if marcasMP4[string(cabeca[8:12])] {
			return tipoDetectado{TipoVideo, "video/mp4", "mp4", LimiteVideo}, true
		}
		return tipoDetectado{}, false
	}
	switch http.DetectContentType(cabeca) {
	case "image/jpeg":
		return tipoDetectado{TipoImagem, "image/jpeg", "jpg", LimiteImagem}, true
	case "image/png":
		return tipoDetectado{TipoImagem, "image/png", "png", LimiteImagem}, true
	}
	return tipoDetectado{}, false
}

func mensagemDoTipo(t Tipo) string {
	if t == TipoVideo {
		return msgVideoInvalido
	}
	return msgFotoInvalida
}

// nomeOriginal guarda o nome que o gestor deu ao arquivo, só para exibição:
// sem diretório, sem caractere de controle, com teto de tamanho.
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
		nome = "arquivo"
	}
	if utf8.RuneCountInString(nome) > maxNomeOriginal {
		nome = string([]rune(nome)[:maxNomeOriginal])
	}
	return nome
}

// caminhoNoVolume monta o caminho do arquivo a partir da storage_key, sem
// deixar a chave escapar do diretório.
func (s *Servico) caminhoNoVolume(chave string) (string, error) {
	if s.dir == "" {
		return "", apperr.Internal.WithCause(errors.New("site: diretório de mídia não configurado"))
	}
	if chave == "" || chave != filepath.Base(chave) || strings.HasPrefix(chave, ".") {
		return "", apperr.Internal.WithCause(fmt.Errorf("site: storage_key suspeita %q", chave))
	}
	return filepath.Join(s.dir, chave), nil
}

// EnviarMidia grava o arquivo no volume e registra a linha em site_media.
//
// A ordem é a que mantém volume e banco coerentes: o arquivo é escrito num
// temporário DENTRO do volume (o rename é atômico só no mesmo sistema de
// arquivos), renomeado para o nome final e só então registrado. Se o
// registro falhar, o arquivo final é apagado — não sobra arquivo sem linha.
func (s *Servico) EnviarMidia(ctx context.Context, corpo io.Reader, nome string) (MidiaResposta, error) {
	if s.dir == "" {
		return MidiaResposta{}, apperr.Internal.WithCause(errors.New("site: diretório de mídia não configurado"))
	}

	cabeca := make([]byte, tamanhoDaCabeca)
	n, err := io.ReadFull(corpo, cabeca)
	switch {
	case errors.Is(err, io.EOF):
		return MidiaResposta{}, ErroDeArquivo(msgArquivoVazio)
	case errors.Is(err, io.ErrUnexpectedEOF):
		// Arquivo menor que 512 bytes: é tudo que há.
	case err != nil:
		return MidiaResposta{}, erroDeLeitura(err, "")
	}
	cabeca = cabeca[:n]

	tipo, ok := detectar(cabeca)
	if !ok {
		return MidiaResposta{}, ErroDeArquivo(msgTipoInvalido)
	}

	tmp, err := os.CreateTemp(s.dir, ".envio-*")
	if err != nil {
		return MidiaResposta{}, apperr.Internal.WithCause(fmt.Errorf("site: criando temporário: %w", err))
	}
	nomeTmp := tmp.Name()
	renomeado := false
	defer func() {
		if !renomeado {
			_ = tmp.Close()
			if err := os.Remove(nomeTmp); err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.WarnContext(ctx, "site: temporário de envio ficou no volume", "arquivo", nomeTmp, "err", err)
			}
		}
	}()

	if _, err := tmp.Write(cabeca); err != nil {
		return MidiaResposta{}, apperr.Internal.WithCause(err)
	}
	// +1 para enxergar o byte que passa do limite sem ler o resto do corpo.
	copiados, err := io.Copy(tmp, io.LimitReader(corpo, tipo.Limite-int64(n)+1))
	total := int64(n) + copiados
	if err != nil {
		return MidiaResposta{}, erroDeLeitura(err, tipo.Tipo)
	}
	if total > tipo.Limite {
		return MidiaResposta{}, ErroDeArquivo(mensagemDoTipo(tipo.Tipo))
	}
	if err := tmp.Sync(); err != nil {
		return MidiaResposta{}, apperr.Internal.WithCause(err)
	}
	if err := tmp.Close(); err != nil {
		return MidiaResposta{}, apperr.Internal.WithCause(err)
	}
	if err := os.Chmod(nomeTmp, permissaoArquivo); err != nil {
		return MidiaResposta{}, apperr.Internal.WithCause(err)
	}

	id := uuid.New()
	chave := id.String() + "." + tipo.Ext
	final, err := s.caminhoNoVolume(chave)
	if err != nil {
		return MidiaResposta{}, err
	}
	if err := os.Rename(nomeTmp, final); err != nil {
		return MidiaResposta{}, apperr.Internal.WithCause(fmt.Errorf("site: renomeando envio: %w", err))
	}
	renomeado = true

	m := Midia{
		ID: id, Tipo: string(tipo.Tipo), Mime: tipo.Mime, Bytes: total,
		NomeOrig: nomeOriginal(nome), StorageKey: chave,
	}
	if uid, ok := auth.UsuarioID(ctx); ok {
		m.CriadoPor = &uid
	}

	err = s.tx.Do(ctx, func(ctx context.Context) error {
		criada, err := s.repo.CriarMidia(ctx, m)
		if err != nil {
			return err
		}
		m = criada
		return audit.Criacao(ctx, s.repo.pool, EntidadeMidia, audit.VerboCriado, m.ID, m)
	})
	if err != nil {
		if rmErr := os.Remove(final); rmErr != nil {
			slog.ErrorContext(ctx, "site: arquivo sem registro ficou no volume", "arquivo", final, "err", rmErr)
		}
		return MidiaResposta{}, err
	}

	return MidiaResposta{ID: m.ID, Kind: m.Tipo, Mime: m.Mime, Bytes: m.Bytes, URL: URLDaMidia(m.ID)}, nil
}

// erroDeLeitura traduz a falha ao ler o corpo. O teto do corpo inteiro
// (http.MaxBytesReader) é "arquivo grande demais"; o resto é envio cortado.
func erroDeLeitura(err error, tipo Tipo) error {
	var grande *http.MaxBytesError
	if errors.As(err, &grande) {
		if tipo == TipoImagem {
			return ErroDeArquivo(msgFotoInvalida)
		}
		return ErroDeArquivo(msgVideoInvalido)
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return ErroDeArquivo(msgEnvioCortado).WithCause(err)
}

// AbrirMidia devolve o arquivo aberto e a linha. Quem chama fecha o arquivo.
func (s *Servico) AbrirMidia(ctx context.Context, id uuid.UUID) (*os.File, Midia, error) {
	m, ok, err := s.repo.BuscarMidia(ctx, id)
	if err != nil {
		return nil, Midia{}, err
	}
	if !ok {
		return nil, Midia{}, apperr.NotFound("Arquivo")
	}
	caminho, err := s.caminhoNoVolume(m.StorageKey)
	if err != nil {
		return nil, Midia{}, err
	}
	f, err := os.Open(caminho)
	if errors.Is(err, os.ErrNotExist) {
		// Linha sem arquivo: o volume foi perdido ou restaurado pela metade.
		// Para o visitante é 404; para quem opera, um aviso no log.
		slog.WarnContext(ctx, "site: mídia registrada sem arquivo no volume", "media_id", id, "arquivo", caminho)
		return nil, Midia{}, apperr.NotFound("Arquivo")
	}
	if err != nil {
		return nil, Midia{}, apperr.Internal.WithCause(err)
	}
	return f, m, nil
}
