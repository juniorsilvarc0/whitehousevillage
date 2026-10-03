package site

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// chavesDoContrato é a lista do docs/site-cms.md §4, escrita à mão e na
// ordem das seções. Mudar o catálogo sem mudar o contrato (e esta lista)
// acende aqui — o site e o painel procuram exatamente estas chaves.
var chavesDoContrato = func() []string {
	k := []string{
		// 1. Google e redes
		"seo.inicio.titulo", "seo.inicio.descricao", "seo.disponibilidade.titulo", "seo.disponibilidade.descricao",
		// 2. Marca
		"marca.logo",
		// 3. Topo (capa)
		"inicio.local", "inicio.titulo", "inicio.texto", "inicio.video", "inicio.numeros",
		// 4. Faixa de temas
		"faixa.itens",
		// 5. A casa
		"casa.rotulo", "casa.titulo", "casa.texto", "casa.foto", "casa.numeros",
		// 6. Acomodações
		"acomodacoes.rotulo", "acomodacoes.titulo", "acomodacoes.texto",
	}
	for _, c := range []string{"duplex", "suites", "grand-villa", "classic-villa", "completa"} {
		for _, f := range []string{"titulo", "selo", "descricao", "itens", "foto"} {
			k = append(k, "categoria."+c+"."+f)
		}
	}
	return append(k,
		// 7. Eventos
		"eventos.rotulo", "eventos.titulo", "eventos.texto", "eventos.cards",
		// 8. Estrutura
		"estrutura.rotulo", "estrutura.titulo", "estrutura.texto", "estrutura.itens",
		// 9. Localização
		"local.rotulo", "local.titulo", "local.texto", "local.itens", "local.foto",
		// 10. Chamada final
		"chamada.titulo", "chamada.texto",
		// 11. Rodapé e contato
		"rodape.texto", "rodape.email", "rodape.horario", "rodape.endereco", "rodape.copyright",
		// 12. Página de disponibilidade
		"disp.rotulo", "disp.titulo", "disp.texto", "disp.tarifas.rotulo", "disp.tarifas.titulo", "disp.chamada.titulo",
		// 13. Página não encontrada
		"erro.titulo", "erro.texto",
	)
}()

var formatoDeChave = regexp.MustCompile(`^[a-z0-9]+([.-][a-z0-9]+)*$`)

func TestCatalogoTemExatamenteAsChavesDoContratoNaOrdem(t *testing.T) {
	var noCatalogo []string
	for _, s := range Catalogo() {
		for _, c := range s.Campos {
			noCatalogo = append(noCatalogo, c.Chave)
		}
	}
	if !slices.Equal(noCatalogo, chavesDoContrato) {
		t.Fatalf("catálogo diverge do docs/site-cms.md §4.\ncatálogo (%d): %v\ncontrato (%d): %v",
			len(noCatalogo), noCatalogo, len(chavesDoContrato), chavesDoContrato)
	}
	if len(noCatalogo) != 72 {
		t.Fatalf("esperadas 72 chaves, há %d", len(noCatalogo))
	}
}

func TestSecoesNaOrdemDoContrato(t *testing.T) {
	esperado := []string{"seo", "marca", "inicio", "faixa", "casa", "acomodacoes", "eventos",
		"estrutura", "local", "chamada", "rodape", "disp", "erro"}
	var got []string
	for _, s := range Catalogo() {
		got = append(got, s.Chave)
		if s.Rotulo == "" || len(s.Campos) == 0 {
			t.Errorf("seção %s sem rótulo ou sem campos", s.Chave)
		}
	}
	if !slices.Equal(got, esperado) {
		t.Fatalf("seções = %v, esperado %v", got, esperado)
	}
}

func TestChavesUnicasEBemFormadas(t *testing.T) {
	vistas := map[string]bool{}
	for _, s := range Catalogo() {
		for _, c := range s.Campos {
			if vistas[c.Chave] {
				t.Errorf("chave repetida: %s", c.Chave)
			}
			vistas[c.Chave] = true
			if !formatoDeChave.MatchString(c.Chave) {
				t.Errorf("chave fora do formato do banco: %q", c.Chave)
			}
			if c.Rotulo == "" {
				t.Errorf("%s sem rótulo", c.Chave)
			}
			if c.Tipo != TipoVideo && c.Max <= 0 {
				t.Errorf("%s sem max", c.Chave)
			}
			if c.Tipo == TipoLista {
				if len(c.Itens) == 0 {
					t.Errorf("%s: lista sem item_fields", c.Chave)
				}
				for _, s := range c.Itens {
					if s.Tipo != TipoTexto && s.Tipo != TipoTextoLongo && s.Tipo != TipoImagem {
						t.Errorf("%s.%s: subtipo %q não permitido", c.Chave, s.Chave, s.Tipo)
					}
				}
			} else if len(c.Itens) > 0 {
				t.Errorf("%s: item_fields em campo que não é lista", c.Chave)
			}
		}
	}
}

// Todo original passa pela validação do próprio tipo — se não passasse, o
// gestor não conseguiria salvar de volta o texto que o site já mostra.
// Imagem e vídeo originais são arquivos do próprio site (url, sem media_id):
// conferidos à parte.
func TestTodoOriginalPassaNaPropriaValidacao(t *testing.T) {
	for _, s := range Catalogo() {
		for _, c := range s.Campos {
			switch c.Tipo {
			case TipoImagem, TipoVideo:
				if ehNulo(c.Original) {
					continue
				}
				var o midiaOriginal
				if err := json.Unmarshal(c.Original, &o); err != nil || !strings.HasPrefix(o.URL, "/") {
					t.Errorf("%s: original de mídia inválido: %s", c.Chave, c.Original)
				}
			default:
				v, det := c.validar(c.Original)
				if len(det) > 0 {
					t.Errorf("%s: original reprovado: %v", c.Chave, det)
					continue
				}
				if string(v.JSON) != string(c.Original) && c.Tipo != TipoLista {
					t.Errorf("%s: validar mudou o original: %s → %s", c.Chave, c.Original, v.JSON)
				}
			}
		}
	}
}

func TestOriginaisDeTitulosNaoTemHTML(t *testing.T) {
	for _, s := range Catalogo() {
		for _, c := range s.Campos {
			if c.Tipo == TipoImagem || c.Tipo == TipoVideo {
				continue
			}
			o := string(c.Original)
			for _, proibido := range []string{"<em>", "<br", "&amp;", "&nbsp;", `<`} {
				if strings.Contains(o, proibido) {
					t.Errorf("%s: original com HTML %q: %s", c.Chave, proibido, o)
				}
			}
		}
	}
	c, _ := CampoDoCatalogo("casa.titulo")
	if string(c.Original) != `"Não é uma diária.\nÉ a casa *inteira* à sua disposição."` {
		t.Errorf("casa.titulo original = %s", c.Original)
	}
}

func campo(t *testing.T, chave string) Campo {
	t.Helper()
	c, ok := CampoDoCatalogo(chave)
	if !ok {
		t.Fatalf("%s fora do catálogo", chave)
	}
	return c
}

func TestValidacaoPorTipo(t *testing.T) {
	id := uuid.New()
	casos := []struct {
		nome, chave, valor string
		ok                 bool
	}{
		{"texto ok", "inicio.local", `"Praia"`, true},
		{"texto vazio", "inicio.local", `"   "`, false},
		{"texto com quebra", "inicio.local", `"a\nb"`, false},
		{"texto com número", "inicio.local", `12`, false},
		{"texto 200 letras com acento", "inicio.local", `"` + strings.Repeat("é", 200) + `"`, true},
		{"texto 201 letras", "inicio.local", `"` + strings.Repeat("a", 201) + `"`, false},
		{"título com quebra", "inicio.titulo", `"a\n*b*"`, true},
		{"texto longo 2001", "inicio.texto", `"` + strings.Repeat("a", 2001) + `"`, false},
		{"controle invisível", "inicio.texto", `"a\u0007b"`, false},
		{"email ok", "rodape.email", `"contato@casa.com.br"`, true},
		{"email ruim", "rodape.email", `"contato"`, false},
		{"imagem ok", "casa.foto", `{"media_id":"` + id.String() + `","alt":"Pôr do sol"}`, true},
		{"imagem ida e volta com url", "casa.foto", `{"media_id":"` + id.String() + `","url":"/x","alt":""}`, true},
		{"imagem sem id", "casa.foto", `{"alt":"x"}`, false},
		{"imagem campo estranho", "casa.foto", `{"media_id":"` + id.String() + `","x":1}`, false},
		{"imagem alt longo", "casa.foto", `{"media_id":"` + id.String() + `","alt":"` + strings.Repeat("a", 201) + `"}`, false},
		{"vídeo ok", "inicio.video", `{"media_id":"` + id.String() + `"}`, true},
		{"vídeo com alt", "inicio.video", `{"media_id":"` + id.String() + `","alt":"x"}`, false},
		{"vídeo string", "inicio.video", `"x"`, false},
		{"lista ok", "faixa.itens", `[{"texto":"Temporada"}]`, true},
		{"lista vazia", "faixa.itens", `[]`, true},
		{"lista sub-chave estranha", "faixa.itens", `[{"texto":"a","x":"b"}]`, false},
		{"lista falta sub-chave", "inicio.numeros", `[{"valor":"1"}]`, false},
		{"lista não é array", "faixa.itens", `{"texto":"a"}`, false},
		{"lista 25 itens", "faixa.itens", `[` + strings.TrimSuffix(strings.Repeat(`{"texto":"a"},`, 25), ",") + `]`, false},
		{"lista com foto nula", "eventos.cards", `[{"titulo":"a","texto":"","itens":"","foto":null}]`, true},
		{"lista com foto", "eventos.cards", `[{"titulo":"a","texto":"b","itens":"c\nd","foto":{"media_id":"` + id.String() + `"}}]`, true},
		{"lista foto inválida", "eventos.cards", `[{"titulo":"a","texto":"b","itens":"","foto":"x"}]`, false},
		{"emoji", "estrutura.itens", `[{"icone":"🍽️","titulo":"a","texto":"b"}]`, true},
		{"nulo", "inicio.local", `null`, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			v, det := campo(t, c.chave).validar(json.RawMessage(c.valor))
			if c.ok && len(det) > 0 {
				t.Fatalf("recusado: %v", det)
			}
			if !c.ok && len(det) == 0 {
				t.Fatalf("aceito: %s", v.JSON)
			}
		})
	}
}

func TestValidarNormalizaEColetaMidias(t *testing.T) {
	id := uuid.New()
	v, det := campo(t, "casa.foto").validar(json.RawMessage(`{"media_id":"` + id.String() + `","url":"/lixo","alt":" Pôr do sol "}`))
	if len(det) > 0 {
		t.Fatal(det)
	}
	if string(v.JSON) != `{"media_id":"`+id.String()+`","alt":"Pôr do sol"}` {
		t.Fatalf("gravado = %s (a url nunca é gravada)", v.JSON)
	}
	if len(v.Midias) != 1 || v.Midias[0].ID != id || v.Midias[0].Tipo != TipoImagem {
		t.Fatalf("mídias = %+v", v.Midias)
	}

	v, det = campo(t, "inicio.texto").validar(json.RawMessage(`"a\r\nb"`))
	if len(det) > 0 || string(v.JSON) != `"a\nb"` {
		t.Fatalf("CRLF não normalizado: %s %v", v.JSON, det)
	}

	v, det = campo(t, "eventos.cards").validar(json.RawMessage(
		`[{"titulo":"a","texto":"","itens":"","foto":{"media_id":"` + id.String() + `"}}]`))
	if len(det) > 0 || len(v.Midias) != 1 || v.Midias[0].Caminho != "value[0].foto" {
		t.Fatalf("mídia de lista: %+v %v", v.Midias, det)
	}
}

func TestResolucaoPainelEPublico(t *testing.T) {
	id := uuid.New()
	url := "/api/v1/public/media/" + id.String()

	img := campo(t, "casa.foto")
	gravado := json.RawMessage(`{"media_id":"` + id.String() + `","alt":"Praia"}`)
	painel, err := img.resolverGravado(gravado, false)
	if err != nil || string(painel) != `{"media_id":"`+id.String()+`","url":"`+url+`","alt":"Praia"}` {
		t.Fatalf("painel = %s (%v)", painel, err)
	}
	pub, err := img.resolverGravado(gravado, true)
	if err != nil || string(pub) != `{"url":"`+url+`","alt":"Praia"}` {
		t.Fatalf("público = %s (%v)", pub, err)
	}

	vid := campo(t, "inicio.video")
	pub, err = vid.resolverGravado(json.RawMessage(`{"media_id":"`+id.String()+`"}`), true)
	if err != nil || string(pub) != `{"url":"`+url+`"}` {
		t.Fatalf("vídeo público = %s (%v)", pub, err)
	}

	logo, err := campo(t, "marca.logo").resolverOriginal()
	if err != nil || string(logo) != `{"media_id":null,"url":"/assets/logo.png","alt":"White House"}` {
		t.Fatalf("logo original = %s (%v)", logo, err)
	}
	video, err := vid.resolverOriginal()
	if err != nil || string(video) != `{"media_id":null,"url":"/videos/hero.mp4"}` {
		t.Fatalf("vídeo original = %s (%v)", video, err)
	}
	cena, err := img.resolverOriginal()
	if err != nil || string(cena) != "null" {
		t.Fatalf("cena original = %s (%v)", cena, err)
	}

	cards := campo(t, "eventos.cards")
	pub, err = cards.resolverGravado(json.RawMessage(
		`[{"titulo":"a","texto":"","itens":"","foto":{"media_id":"`+id.String()+`","alt":""}},{"titulo":"b","texto":"","itens":"","foto":null}]`), true)
	if err != nil {
		t.Fatal(err)
	}
	var itens []struct {
		Foto *MidiaPublica `json:"foto"`
	}
	if err := json.Unmarshal(pub, &itens); err != nil || len(itens) != 2 || itens[0].Foto == nil ||
		itens[0].Foto.URL != url || itens[1].Foto != nil {
		t.Fatalf("lista pública = %s (%v)", pub, err)
	}
}

// "Sem foto hoje" é null — no campo e no subcampo de lista —, nunca "".
func TestSemFotoHojeEhNulo(t *testing.T) {
	semFoto := []string{"casa.foto", "local.foto"}
	for _, c := range []string{"duplex", "suites", "grand-villa", "classic-villa", "completa"} {
		semFoto = append(semFoto, "categoria."+c+".foto")
	}
	for _, k := range semFoto {
		c := campo(t, k)
		if string(c.Original) != "null" {
			t.Errorf("%s: original = %s, esperado null", k, c.Original)
		}
		r, err := c.resolverOriginal()
		if err != nil || string(r) != "null" {
			t.Errorf("%s: default_value = %s (%v), esperado null", k, r, err)
		}
	}
	cards := campo(t, "eventos.cards")
	var itens []map[string]json.RawMessage
	if err := json.Unmarshal(cards.Original, &itens); err != nil || len(itens) != 3 {
		t.Fatalf("eventos.cards original: %s (%v)", cards.Original, err)
	}
	for i, it := range itens {
		if string(it["foto"]) != "null" {
			t.Errorf("eventos.cards[%d].foto = %s, esperado null", i, it["foto"])
		}
	}
	if _, det := cards.validar(cards.Original); len(det) > 0 {
		t.Fatalf("o original de eventos.cards (foto null) não passa na própria validação: %v", det)
	}
}
