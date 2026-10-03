//go:build integration

// Área administrativa do site (docs/site-cms.md) pela porta da frente: o que a
// gestão salva no painel chega ao site sem token, restaurar devolve o
// original, o tipo do arquivo é decidido pelos bytes, e a mídia sai com Range.
package router

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

type campoDoSiteQA struct {
	Key          string          `json:"key"`
	Kind         string          `json:"kind"`
	Max          int             `json:"max"`
	Value        json.RawMessage `json:"value"`
	DefaultValue json.RawMessage `json:"default_value"`
	IsDefault    bool            `json:"is_default"`
	ItemFields   []struct {
		Key  string `json:"key"`
		Kind string `json:"kind"`
	} `json:"item_fields"`
}

type conteudoDoSiteQA struct {
	Sections []struct {
		Key    string          `json:"key"`
		Label  string          `json:"label"`
		Fields []campoDoSiteQA `json:"fields"`
	} `json:"sections"`
}

type publicoDoSiteQA struct {
	Values map[string]json.RawMessage `json:"values"`
}

type midiaDoSiteQA struct {
	ID    uuid.UUID `json:"id"`
	Kind  string    `json:"kind"`
	Mime  string    `json:"mime"`
	Bytes int64     `json:"bytes"`
	URL   string    `json:"url"`
}

// gestorDoSite cria um usuário com site:ver e site:editar e agenda a limpeza
// do que ele gravar (as linhas referenciam o usuário, e a limpeza dele roda
// DEPOIS desta — t.Cleanup é LIFO).
func (a *ambiente) gestorDoSite(t *testing.T, chaves ...string) usuarioDeTeste {
	t.Helper()
	if _, err := a.pool.Exec(a.ctx, `
		INSERT INTO resources (code, label, group_label, actions, supports_own, sort_order)
		VALUES ('site', 'Site (textos, fotos e vídeos)', 'Site', ARRAY['ver','editar'], false, 70)
		ON CONFLICT (code) DO NOTHING`); err != nil {
		t.Fatalf("recurso site: %v", err)
	}
	perfil := a.criarPerfil(t, "site", []auth.Permissao{
		{Resource: "site", Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: "site", Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	})
	u := a.criarUsuario(t, "site", perfil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := a.pool.Exec(ctx, `DELETE FROM site_content WHERE key = ANY($1) OR updated_by = $2`, chaves, u.ID); err != nil {
			t.Logf("LIMPEZA INCOMPLETA (site_content): %v", err)
		}
		if _, err := a.pool.Exec(ctx, `DELETE FROM site_media WHERE created_by = $1`, u.ID); err != nil {
			t.Logf("LIMPEZA INCOMPLETA (site_media): %v", err)
		}
	})
	// Começa do original: nenhuma sobra de execução anterior.
	if _, err := a.pool.Exec(a.ctx, `DELETE FROM site_content WHERE key = ANY($1)`, chaves); err != nil {
		t.Fatalf("limpando chaves: %v", err)
	}
	return u
}

func (a *ambiente) publicoDoSite(t *testing.T) (publicoDoSiteQA, resposta) {
	t.Helper()
	r := a.chamar(t, http.MethodGet, "/public/site", "", nil)
	return envelopeDe[publicoDoSiteQA](t, r, http.StatusOK, "GET /public/site sem token"), r
}

func (a *ambiente) campoDoSite(t *testing.T, token, chave string) campoDoSiteQA {
	t.Helper()
	c := envelopeDe[conteudoDoSiteQA](t, a.chamar(t, http.MethodGet, "/site/content", token, nil),
		http.StatusOK, "GET /site/content")
	for _, s := range c.Sections {
		for _, f := range s.Fields {
			if f.Key == chave {
				return f
			}
		}
	}
	t.Fatalf("GET /site/content não tem %s", chave)
	return campoDoSiteQA{}
}

func TestSiteEditarTituloChegaAoSiteERestaurarSome(t *testing.T) {
	a := subirAPI(t)
	g := a.gestorDoSite(t, "chamada.titulo")

	antes := a.campoDoSite(t, g.Token, "chamada.titulo")
	if !antes.IsDefault || string(antes.Value) != `"Sua data ainda está *livre*?"` || antes.Kind != "titulo" {
		t.Fatalf("campo original inesperado: %+v", antes)
	}

	novo := "Sua data está *esperando*\npor você"
	gravado := envelopeDe[campoDoSiteQA](t,
		a.chamar(t, http.MethodPut, "/site/content/chamada.titulo", g.Token, map[string]any{"value": novo}),
		http.StatusOK, "PUT título")
	if gravado.IsDefault || string(gravado.Value) != mustJSON(t, novo) {
		t.Fatalf("PUT devolveu %+v", gravado)
	}

	pub, r := a.publicoDoSite(t)
	if string(pub.Values["chamada.titulo"]) != mustJSON(t, novo) {
		t.Fatalf("o site não recebeu o título: %s", r.Corpo)
	}
	if cc := r.Headers.Get("Cache-Control"); cc != "public, max-age=60" {
		t.Errorf("Cache-Control = %q", cc)
	}
	for _, proibido := range []string{"updated_by", "updated_at", "default_value", g.ID.String()} {
		if bytes.Contains(r.Corpo, []byte(proibido)) {
			t.Errorf("GET /public/site expõe %q", proibido)
		}
	}

	var trilhas int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM audit_log
		 WHERE entity = 'site_content' AND actor_id = $1 AND after->>'key' = 'chamada.titulo'`, g.ID).Scan(&trilhas); err != nil {
		t.Fatal(err)
	}
	if trilhas != 1 {
		t.Fatalf("esperada 1 linha de auditoria do PUT, há %d", trilhas)
	}

	if r := a.chamar(t, http.MethodDelete, "/site/content/chamada.titulo", g.Token, nil); r.Status != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", r.Status, r.Corpo)
	}
	pub, _ = a.publicoDoSite(t)
	if _, ainda := pub.Values["chamada.titulo"]; ainda {
		t.Fatal("restaurar não tirou o título do site")
	}
	if c := a.campoDoSite(t, g.Token, "chamada.titulo"); !c.IsDefault || string(c.Value) != string(c.DefaultValue) {
		t.Fatalf("depois de restaurar: %+v", c)
	}
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM audit_log
		 WHERE entity = 'site_content' AND actor_id = $1 AND action = 'site_content.excluido'`, g.ID).Scan(&trilhas); err != nil {
		t.Fatal(err)
	}
	if trilhas != 1 {
		t.Fatalf("esperada 1 linha de auditoria do DELETE, há %d", trilhas)
	}
}

func TestSiteValidacaoEChaveDesconhecida(t *testing.T) {
	a := subirAPI(t)
	g := a.gestorDoSite(t, "inicio.local", "faixa.itens", "casa.foto")

	casos := []struct {
		nome, chave string
		corpo       any
		status      int
		code        string
	}{
		{"chave fora do catálogo", "inicio.nao-existe", map[string]any{"value": "x"}, 404, "NOT_FOUND"},
		{"texto longo demais", "inicio.local", map[string]any{"value": strings.Repeat("a", 201)}, 422, "VALIDATION_ERROR"},
		{"tipo errado", "inicio.local", map[string]any{"value": 42}, 422, "VALIDATION_ERROR"},
		{"lista com sub-chave estranha", "faixa.itens", map[string]any{"value": []map[string]string{{"texto": "a", "cor": "azul"}}}, 422, "VALIDATION_ERROR"},
		{"foto que não existe", "casa.foto", map[string]any{"value": map[string]string{"media_id": uuid.NewString()}}, 422, "VALIDATION_ERROR"},
		{"sem value", "inicio.local", map[string]any{}, 422, "VALIDATION_ERROR"},
		{"campo estranho no corpo", "inicio.local", map[string]any{"value": "a", "x": 1}, 422, "VALIDATION_ERROR"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			r := a.chamar(t, http.MethodPut, "/site/content/"+c.chave, g.Token, c.corpo)
			if r.Status != c.status || r.codigoDeErro(t) != c.code {
				t.Fatalf("status %d, corpo %s", r.Status, r.Corpo)
			}
		})
	}
	if r := a.chamar(t, http.MethodDelete, "/site/content/inicio.nao-existe", g.Token, nil); r.Status != http.StatusNotFound {
		t.Fatalf("DELETE de chave desconhecida: %d", r.Status)
	}
	// Restaurar o que já está original: 204, sem erro.
	if r := a.chamar(t, http.MethodDelete, "/site/content/inicio.local", g.Token, nil); r.Status != http.StatusNoContent {
		t.Fatalf("DELETE idempotente: %d %s", r.Status, r.Corpo)
	}
}

func TestSiteSemPermissaoRecebe403EPublicasNaoPedemToken(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	sem := a.vendedor(t)

	if r := a.chamar(t, http.MethodPut, "/site/content/inicio.local", sem.Token, map[string]any{"value": "x"}); r.Status != http.StatusForbidden {
		t.Fatalf("PUT sem site:editar: %d %s", r.Status, r.Corpo)
	}
	if r := a.chamar(t, http.MethodGet, "/site/content", sem.Token, nil); r.Status != http.StatusForbidden {
		t.Fatalf("GET sem site:ver: %d", r.Status)
	}
	if r := a.chamar(t, http.MethodGet, "/site/content", "", nil); r.Status != http.StatusUnauthorized {
		t.Fatalf("GET /site/content sem token: %d", r.Status)
	}
	if r := a.chamar(t, http.MethodGet, "/public/site", "", nil); r.Status != http.StatusOK {
		t.Fatalf("GET /public/site sem token: %d", r.Status)
	}
	if r := a.chamar(t, http.MethodGet, "/public/media/"+uuid.NewString(), "", nil); r.Status != http.StatusNotFound || r.codigoDeErro(t) != "NOT_FOUND" {
		t.Fatalf("mídia inexistente sem token: %d %s", r.Status, r.Corpo)
	}
	if r := a.chamar(t, http.MethodGet, "/public/media/nao-e-uuid", "", nil); r.Status != http.StatusNotFound {
		t.Fatalf("id inválido: %d", r.Status)
	}
}

// enviar faz o POST multipart de /site/media.
func (a *ambiente) enviar(t *testing.T, token, nome string, conteudo []byte) resposta {
	t.Helper()
	var corpo bytes.Buffer
	mw := multipart.NewWriter(&corpo)
	fw, err := mw.CreateFormFile("file", nome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(conteudo); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(a.ctx, http.MethodPost, a.servidor.URL+PrefixoDaAPI+"/site/media", &corpo)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	return a.fazer(t, req)
}

func (a *ambiente) fazer(t *testing.T, req *http.Request) resposta {
	t.Helper()
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	lido, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resposta{Status: resp.StatusCode, Corpo: lido, Headers: resp.Header}
}

func pngDeTeste(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSiteEnvioDeFotoChegaAoSiteComRange(t *testing.T) {
	a := subirAPI(t)
	g := a.gestorDoSite(t, "casa.foto", "inicio.video")
	foto := pngDeTeste(t)

	r := a.enviar(t, g.Token, "praia.png", foto)
	m := envelopeDe[midiaDoSiteQA](t, r, http.StatusCreated, "POST /site/media")
	if m.Kind != "imagem" || m.Mime != "image/png" || m.Bytes != int64(len(foto)) ||
		m.URL != "/api/v1/public/media/"+m.ID.String() {
		t.Fatalf("mídia devolvida: %+v", m)
	}

	// Foto não serve em campo de vídeo.
	if r := a.chamar(t, http.MethodPut, "/site/content/inicio.video", g.Token,
		map[string]any{"value": map[string]string{"media_id": m.ID.String()}}); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("foto em campo de vídeo: %d %s", r.Status, r.Corpo)
	}

	gravado := envelopeDe[campoDoSiteQA](t, a.chamar(t, http.MethodPut, "/site/content/casa.foto", g.Token,
		map[string]any{"value": map[string]string{"media_id": m.ID.String(), "alt": "Pôr do sol na praia"}}),
		http.StatusOK, "PUT casa.foto")
	var resolvida struct {
		MediaID *uuid.UUID `json:"media_id"`
		URL     string     `json:"url"`
		Alt     string     `json:"alt"`
	}
	if err := json.Unmarshal(gravado.Value, &resolvida); err != nil || resolvida.MediaID == nil ||
		*resolvida.MediaID != m.ID || resolvida.URL != m.URL || resolvida.Alt != "Pôr do sol na praia" {
		t.Fatalf("valor resolvido no painel: %s", gravado.Value)
	}

	pub, _ := a.publicoDoSite(t)
	if string(pub.Values["casa.foto"]) != `{"url":"`+m.URL+`","alt":"Pôr do sol na praia"}` {
		t.Fatalf("site recebeu %s", pub.Values["casa.foto"])
	}

	// O arquivo, sem token, com o tipo certo e cache imutável.
	req, _ := http.NewRequestWithContext(a.ctx, http.MethodGet, a.servidor.URL+m.URL, nil)
	inteiro := a.fazer(t, req)
	if inteiro.Status != http.StatusOK || !bytes.Equal(inteiro.Corpo, foto) {
		t.Fatalf("GET mídia: %d, %d bytes", inteiro.Status, len(inteiro.Corpo))
	}
	if ct := inteiro.Headers.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := inteiro.Headers.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", cc)
	}

	req, _ = http.NewRequestWithContext(a.ctx, http.MethodGet, a.servidor.URL+m.URL, nil)
	req.Header.Set("Range", "bytes=0-9")
	pedaco := a.fazer(t, req)
	if pedaco.Status != http.StatusPartialContent || !bytes.Equal(pedaco.Corpo, foto[:10]) {
		t.Fatalf("Range: %d, %d bytes", pedaco.Status, len(pedaco.Corpo))
	}
	if cr := pedaco.Headers.Get("Content-Range"); !strings.HasPrefix(cr, "bytes 0-9/") {
		t.Errorf("Content-Range = %q", cr)
	}

	var trilhas int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM audit_log WHERE entity = 'site_media' AND entity_id = $1 AND actor_id = $2`,
		m.ID, g.ID).Scan(&trilhas); err != nil {
		t.Fatal(err)
	}
	if trilhas != 1 {
		t.Fatalf("esperada 1 linha de auditoria do envio, há %d", trilhas)
	}
}

func TestSiteEnvioRecusaTextoComNomeDeFoto(t *testing.T) {
	a := subirAPI(t)
	g := a.gestorDoSite(t)

	r := a.enviar(t, g.Token, "foto.png", []byte("isto é um texto qualquer, não uma foto"))
	if r.Status != http.StatusUnprocessableEntity || r.codigoDeErro(t) != "VALIDATION_ERROR" {
		t.Fatalf("status %d, corpo %s", r.Status, r.Corpo)
	}
	var env struct {
		Error struct {
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	r.decodificar(t, &env)
	if env.Error.Details["file"] == "" {
		t.Fatalf("422 sem details.file em linguagem do gestor: %s", r.Corpo)
	}
	var n int
	if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM site_media WHERE created_by = $1`, g.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("envio recusado deixou %d linha(s) em site_media", n)
	}

	// Sem permissão de edição, nem chega a ler o arquivo.
	sem := a.criarUsuario(t, "sem-site", a.criarPerfil(t, "sem-site", nil))
	if r := a.enviar(t, sem.Token, "praia.png", pngDeTeste(t)); r.Status != http.StatusForbidden {
		t.Fatalf("envio sem site:editar: %d", r.Status)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
