package site

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

func pngPequeno(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func ftyp(marca string) []byte {
	b := make([]byte, 32)
	binary.BigEndian.PutUint32(b[0:4], 32)
	copy(b[4:8], "ftyp")
	copy(b[8:12], marca)
	return b
}

func TestDetectarPelosBytes(t *testing.T) {
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 20)...)
	webm := append([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x9F, 0x42, 0x86, 0x81, 0x01, 0x42, 0x82, 0x84}, []byte("webm")...)
	mkv := append([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x9F, 0x42, 0x86, 0x81, 0x01, 0x42, 0x82, 0x88}, []byte("matroska")...)
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}

	casos := []struct {
		nome  string
		bytes []byte
		mime  string
	}{
		{"png", pngPequeno(t), "image/png"},
		{"jpeg", jpeg, "image/jpeg"},
		{"webp", webp, "image/webp"},
		{"webm", webm, "video/webm"},
		{"mp4 isom", ftyp("isom"), "video/mp4"},
		{"mp4 mp42", ftyp("mp42"), "video/mp4"},
		{"mov", ftyp("qt  "), ""},
		{"heic", ftyp("heic"), ""},
		{"matroska", mkv, ""},
		{"texto", []byte("isto não é uma foto"), ""},
		{"gif", []byte("GIF89a\x01\x00\x01\x00"), ""},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), ""},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			d, ok := detectar(c.bytes)
			if c.mime == "" {
				if ok {
					t.Fatalf("aceitou como %s", d.Mime)
				}
				return
			}
			if !ok || d.Mime != c.mime {
				t.Fatalf("detectado %q (ok=%v), esperado %q", d.Mime, ok, c.mime)
			}
		})
	}
}

func TestNomeOriginalSaneado(t *testing.T) {
	for entrada, esperado := range map[string]string{
		"foto.png":               "foto.png",
		"../../etc/passwd":       "passwd",
		`C:\fotos\praia.jpg`:     "praia.jpg",
		"a\x00b.png":             "ab.png",
		"":                       "arquivo",
		strings.Repeat("a", 300): strings.Repeat("a", 255),
	} {
		if got := nomeOriginal(entrada); got != esperado {
			t.Errorf("nomeOriginal(%q) = %q, esperado %q", entrada, got, esperado)
		}
	}
}

// txFalha simula a transação que não consegue gravar a linha.
type txFalha struct{}

func (txFalha) Do(context.Context, func(context.Context) error) error {
	return errors.New("banco fora")
}

func detalheDoArquivo(t *testing.T, err error) string {
	t.Helper()
	var e *apperr.Error
	if !errors.As(err, &e) || e.Code != apperr.CodeValidationError {
		t.Fatalf("esperado VALIDATION_ERROR, veio %v", err)
	}
	d, _ := e.Details.(map[string]string)
	return d["file"]
}

func TestEnvioRecusaSemTocarNoVolume(t *testing.T) {
	dir := t.TempDir()
	s := NovoServico(NewRepository(nil), txFalha{}, dir)

	if msg := detalheDoArquivo(t, mustErr(s.EnviarMidia(context.Background(), strings.NewReader("texto puro"), "foto.png"))); msg != msgTipoInvalido {
		t.Errorf("texto com nome .png: %q", msg)
	}
	if msg := detalheDoArquivo(t, mustErr(s.EnviarMidia(context.Background(), strings.NewReader(""), "x.png"))); msg != msgArquivoVazio {
		t.Errorf("vazio: %q", msg)
	}
	grande := append(pngPequeno(t), make([]byte, LimiteImagem)...)
	if msg := detalheDoArquivo(t, mustErr(s.EnviarMidia(context.Background(), bytes.NewReader(grande), "x.png"))); msg != msgFotoInvalida {
		t.Errorf("foto grande: %q", msg)
	}

	// Transação que falha: o arquivo já renomeado é apagado.
	if _, err := s.EnviarMidia(context.Background(), bytes.NewReader(pngPequeno(t)), "x.png"); err == nil {
		t.Fatal("esperado erro do banco")
	}

	sobras, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sobras) != 0 {
		t.Fatalf("o volume ficou com %d arquivo(s) de envios recusados: %v", len(sobras), sobras)
	}
}

func mustErr(_ MidiaResposta, err error) error { return err }

func TestCaminhoNoVolumeNaoEscapa(t *testing.T) {
	s := &Servico{dir: t.TempDir()}
	for _, ruim := range []string{"", "../x.png", "a/b.png", ".envio-1"} {
		if _, err := s.caminhoNoVolume(ruim); err == nil {
			t.Errorf("aceitou storage_key %q", ruim)
		}
	}
	p, err := s.caminhoNoVolume("abc.png")
	if err != nil || filepath.Dir(p) != s.dir {
		t.Fatalf("caminho = %q (%v)", p, err)
	}
}

// As rotas de arquivo estendem o prazo de rede; sob um ResponseWriter sem
// suporte (o recorder do teste) isso não pode virar erro nem pânico, e sob
// um servidor de verdade tem de funcionar.
func TestEstenderPrazo(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	estenderPrazo(req, httptest.NewRecorder(), func(rc *http.ResponseController) error {
		return rc.SetWriteDeadline(time.Now().Add(time.Minute))
	})

	var errLeitura, errEscrita error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		errLeitura = rc.SetReadDeadline(time.Now().Add(prazoDeEnvio))
		errEscrita = rc.SetWriteDeadline(time.Now().Add(prazoDeEntrega))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if errLeitura != nil || errEscrita != nil {
		t.Fatalf("servidor real recusou o prazo: leitura=%v escrita=%v", errLeitura, errEscrita)
	}
}

// O handler de upload, atrás de um servidor de verdade, estende o prazo e
// recusa o .png que é texto com 422 e details.file — sem banco.
func TestHandlerDeEnvioRecusaTextoComNomeDeFoto(t *testing.T) {
	h := &Handler{svc: NovoServico(NewRepository(nil), txFalha{}, t.TempDir())}
	srv := httptest.NewServer(http.HandlerFunc(h.EnviarMidia))
	defer srv.Close()

	var corpo bytes.Buffer
	mw := multipart.NewWriter(&corpo)
	if err := mw.WriteField("outro", "ignorado"); err != nil {
		t.Fatal(err)
	}
	fw, err := mw.CreateFormFile("file", "foto.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("isto é texto, não PNG")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Post(srv.URL, mw.FormDataContentType(), &corpo)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var env struct {
		Error struct {
			Code    string            `json:"code"`
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnprocessableEntity || env.Error.Code != "VALIDATION_ERROR" ||
		env.Error.Details["file"] != msgTipoInvalido {
		t.Fatalf("status %d, erro %+v", resp.StatusCode, env.Error)
	}
}
