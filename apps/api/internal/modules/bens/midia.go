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

// EnviarFoto recebe a foto, CONVERTE (JPEG/PNG → JPEG de lado maior até
// 1280 px, qualidade 75, sem metadados; conversao.go), grava no volume com a
// miniatura e registra a linha — que descreve o arquivo GUARDADO: `mime`,
// `bytes`, `width` e `height` são os da convertida, e o enviado não fica.
//
// WebP e foto grande demais para decodificar com segurança são guardados como
// vieram, com aviso no log e sem recusar o envio (o contrato). O mesmo vale
// para a foto que passou na detecção pelos bytes mas a biblioteca padrão não
// decodifica (JPEG aritmético ou de 12 bits, arquivo cortado depois do
// cabeçalho): é guardada como veio, avisada, e fica sem miniatura — como era
// antes da conversão.
//
// JPEG que já cabe em 1280 px, sem orientação a aplicar, e cuja conversão não
// sairia menor é guardado como veio MENOS os metadados (regraDaFonteMantida):
// recodificar só perderia qualidade e aumentaria o arquivo.
//
// A ordem mantém volume e banco coerentes: o envio é recebido num temporário
// DENTRO da pasta (sem passar inteiro pela memória enquanto a rede o entrega),
// convertido, a foto guardada e a miniatura são gravadas com rename atômico, e
// só então a linha nasce. Se o registro falhar, os arquivos são apagados — não
// sobra arquivo sem linha. A miniatura falhar NÃO falha o envio.
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

	recebido, enviados, err := receberNoVolume(ctx, s.dir, cabeca, corpo)
	if err != nil {
		return Midia{}, err
	}
	defer removerTemporario(ctx, recebido)

	prep, err := prepararFoto(ctx, tipo, func() ([]byte, error) { return os.ReadFile(recebido) })
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// Aba fechada esperando a vez da conversão: não é 500 (db.MapError).
			return Midia{}, err
		}
		return Midia{}, apperr.Internal.WithCause(err)
	}

	id := uuid.New()
	chave := id.String() + "." + prep.Ext
	final, err := s.caminhoNoVolume(chave)
	if err != nil {
		return Midia{}, err
	}
	if prep.ComoVeio {
		slog.WarnContext(ctx, "bens: foto guardada como veio, sem conversão",
			"media_id", id, "mime", prep.Mime, "bytes", enviados, "motivo", prep.Motivo)
		err = publicarNoVolume(recebido, final)
	} else {
		_, err = gravarNoVolume(ctx, s.dir, final, nil, bytes.NewReader(prep.Dados))
	}
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

	info, err := os.Stat(final)
	if err != nil {
		desfazer()
		return Midia{}, apperr.Internal.WithCause(err)
	}
	m := registroDeMidia{
		ID: id, PropriedadeID: prop, Mime: prep.Mime, Bytes: info.Size(),
		NomeOriginal: nomeOriginal(nome), ChaveArquivo: chave, CriadoPor: atorOuNulo(ctx),
	}
	if d, err := medirFoto(final, prep.Mime); err != nil {
		slog.WarnContext(ctx, "bens: não consegui medir a foto", "media_id", id, "err", err)
	} else {
		m.Largura, m.Altura = &d.largura, &d.altura
	}

	if prep.Miniatura == nil {
		if !prep.ComoVeio {
			slog.WarnContext(ctx, "bens: foto sem miniatura; a grade usa a foto", "media_id", id, "err", prep.Motivo)
		}
	} else {
		chaveMini := id.String() + ".thumb.jpg"
		caminhoMini, err := s.caminhoNoVolume(chaveMini)
		if err != nil {
			desfazer()
			return Midia{}, err
		}
		if _, err := gravarNoVolume(ctx, s.dir, caminhoMini, nil, bytes.NewReader(prep.Miniatura)); err != nil {
			slog.WarnContext(ctx, "bens: miniatura não gravada; a grade usa a foto", "media_id", id, "err", err)
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

// receberNoVolume escreve `cabeca` + `resto` num temporário DENTRO da pasta
// (o rename para o nome final só é atômico no mesmo sistema de arquivos),
// aplica o teto de 15 MB e sincroniza. Devolve o caminho do temporário e o
// total de bytes; quem chama publica (publicarNoVolume) ou apaga.
func receberNoVolume(ctx context.Context, dir string, cabeca []byte, resto io.Reader) (string, int64, error) {
	tmp, err := os.CreateTemp(dir, ".envio-*")
	if err != nil {
		return "", 0, apperr.Internal.WithCause(fmt.Errorf("bens: criando temporário: %w", err))
	}
	nomeTmp := tmp.Name()
	pronto := false
	defer func() {
		if !pronto {
			_ = tmp.Close()
			removerTemporario(ctx, nomeTmp)
		}
	}()

	if _, err := tmp.Write(cabeca); err != nil {
		return "", 0, apperr.Internal.WithCause(err)
	}
	// +1 para enxergar o byte que passa do teto sem ler o resto do corpo.
	copiados, err := io.Copy(tmp, io.LimitReader(resto, LimiteDaFoto-int64(len(cabeca))+1))
	total := int64(len(cabeca)) + copiados
	if err != nil {
		return "", 0, erroDeLeitura(err)
	}
	if total > LimiteDaFoto {
		return "", 0, ErroDeArquivo(msgFotoInvalida)
	}
	if err := tmp.Sync(); err != nil {
		return "", 0, apperr.Internal.WithCause(err)
	}
	if err := tmp.Close(); err != nil {
		return "", 0, apperr.Internal.WithCause(err)
	}
	pronto = true
	return nomeTmp, total, nil
}

// publicarNoVolume dá ao temporário a permissão final e o renomeia para o
// nome definitivo.
func publicarNoVolume(tmp, final string) error {
	if err := os.Chmod(tmp, permissaoArquivo); err != nil {
		return apperr.Internal.WithCause(err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return apperr.Internal.WithCause(fmt.Errorf("bens: renomeando envio: %w", err))
	}
	return nil
}

// gravarNoVolume escreve `cabeca` + `resto` no nome final, passando pelo
// temporário (receber + publicar). Devolve o total de bytes.
func gravarNoVolume(ctx context.Context, dir, final string, cabeca []byte, resto io.Reader) (int64, error) {
	tmp, total, err := receberNoVolume(ctx, dir, cabeca, resto)
	if err != nil {
		return 0, err
	}
	if err := publicarNoVolume(tmp, final); err != nil {
		removerTemporario(ctx, tmp)
		return 0, err
	}
	return total, nil
}

// removerTemporario apaga o temporário que sobrou; já publicado (renomeado),
// não há o que apagar.
func removerTemporario(ctx context.Context, caminho string) {
	if err := os.Remove(caminho); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.WarnContext(ctx, "bens: temporário de envio ficou no volume", "arquivo", caminho, "err", err)
	}
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
