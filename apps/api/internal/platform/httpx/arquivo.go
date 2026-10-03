package httpx

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// ErrSemArquivo diz que o corpo multipart não trouxe a parte pedida (ou nem é
// multipart).
var ErrSemArquivo = errors.New("httpx: corpo sem a parte de arquivo")

// ParteDeArquivo é o equivalente de Decode para upload: limita o corpo inteiro
// a `limite` bytes (http.MaxBytesReader) e avança no multipart EM FLUXO até a
// parte `campo`, descartando as outras. Nada é bufferizado em memória nem no
// /tmp — ao contrário de ParseMultipartForm —, então um vídeo de centenas de
// MB vai direto para onde quem chama mandar.
//
// Mora aqui, e não no módulo, pela mesma razão de Decode: `r.Body` só é lido
// dentro de httpx (internal/router/decoders_test.go).
//
// Quem chama fecha a parte. Erro de leitura que passa do limite carrega
// *http.MaxBytesError (errors.As).
func ParteDeArquivo(w http.ResponseWriter, r *http.Request, campo string, limite int64) (*multipart.Part, error) {
	if r.Body == nil {
		return nil, ErrSemArquivo
	}
	r.Body = http.MaxBytesReader(w, r.Body, limite)
	leitor, err := r.MultipartReader()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSemArquivo, err)
	}
	for {
		p, err := leitor.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, ErrSemArquivo
		}
		if err != nil {
			return nil, err
		}
		if p.FormName() == campo {
			return p, nil
		}
		if _, err := io.Copy(io.Discard, p); err != nil {
			return nil, err
		}
		if err := p.Close(); err != nil {
			return nil, err
		}
	}
}
