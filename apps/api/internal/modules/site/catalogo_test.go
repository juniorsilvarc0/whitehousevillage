package site

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// chavesDoContrato é a lista do docs/site-cms.md §4 (rodada 1, 72 chaves) e
// §4b (rodada 2, 118 chaves), escrita à mão na ordem do painel: em cada seção,
// as chaves da rodada 1 e depois as da rodada 2. Mudar o catálogo sem mudar o
// contrato (e esta lista) acende aqui — o site e o painel procuram exatamente
// estas chaves.
var chavesDoContrato = func() []string {
	var k []string
	add := func(chaves ...string) { k = append(k, chaves...) }

	// 1. Google e redes
	add("seo.inicio.titulo", "seo.inicio.descricao", "seo.disponibilidade.titulo", "seo.disponibilidade.descricao")
	add("seo.erro.titulo")
	// 2. Marca
	add("marca.logo", "marca.nome", "marca.subtitulo", "marca.icone")
	// §4b. Menu e botões
	add("menu.inicio", "menu.acomodacoes", "menu.eventos", "menu.estrutura")
	add("menu.disponibilidade", "menu.reservar", "menu.falar-com-reservas", "menu.whatsapp-flutuante")
	add("menu.pular")
	// 3. Topo (capa)
	add("inicio.local", "inicio.titulo", "inicio.texto", "inicio.video")
	add("inicio.numeros", "inicio.botao-disponibilidade", "inicio.botao-acomodacoes", "inicio.rolar")
	// 4. Faixa de temas
	add("faixa.itens")
	// 5. A casa
	add("casa.rotulo", "casa.titulo", "casa.texto", "casa.foto")
	add("casa.numeros", "casa.foto-legenda")
	// 6. Acomodações (as categorias antes dos campos da rodada 2)
	add("acomodacoes.rotulo", "acomodacoes.titulo", "acomodacoes.texto", "categoria.duplex.titulo")
	add("categoria.duplex.selo", "categoria.duplex.descricao", "categoria.duplex.itens", "categoria.duplex.foto")
	add("categoria.suites.titulo", "categoria.suites.selo", "categoria.suites.descricao", "categoria.suites.itens")
	add("categoria.suites.foto", "categoria.grand-villa.titulo", "categoria.grand-villa.selo", "categoria.grand-villa.descricao")
	add("categoria.grand-villa.itens", "categoria.grand-villa.foto", "categoria.classic-villa.titulo", "categoria.classic-villa.selo")
	add("categoria.classic-villa.descricao", "categoria.classic-villa.itens", "categoria.classic-villa.foto", "categoria.completa.titulo")
	add("categoria.completa.selo", "categoria.completa.descricao", "categoria.completa.itens", "categoria.completa.foto")
	add("acomodacoes.botao", "acomodacoes.botao-card", "acomodacoes.preco-sufixo", "acomodacoes.sob-consulta")
	add("acomodacoes.sob-consulta-nota", "acomodacoes.opcoes", "acomodacoes.falha-titulo", "acomodacoes.falha-texto")
	// 7. Eventos
	add("eventos.rotulo", "eventos.titulo", "eventos.texto", "eventos.cards")
	// 8. Estrutura
	add("estrutura.rotulo", "estrutura.titulo", "estrutura.texto", "estrutura.itens")
	// 9. Localização
	add("local.rotulo", "local.titulo", "local.texto", "local.itens")
	add("local.foto", "local.foto-legenda")
	// 10. Chamada final
	add("chamada.titulo", "chamada.texto", "chamada.botao-datas", "chamada.botao-whatsapp")
	// 11. Rodapé e contato
	add("rodape.texto", "rodape.email", "rodape.horario", "rodape.endereco")
	add("rodape.copyright", "rodape.titulo-navegue", "rodape.titulo-reservas", "rodape.titulo-endereco")
	add("rodape.whatsapp")
	// 12. Página de disponibilidade
	add("disp.rotulo", "disp.titulo", "disp.texto", "disp.tarifas.rotulo")
	add("disp.tarifas.titulo", "disp.chamada.titulo", "disp.campo-produto", "disp.campo-hospedes")
	add("disp.legenda-livre", "disp.legenda-indisponivel", "disp.legenda-consulta", "disp.legenda-especial")
	add("disp.tarifas.texto", "disp.tarifas.minimos", "disp.tabela-produto", "disp.tabela-capacidade")
	add("disp.tabela-consulta", "disp.tabela-pacotes", "disp.chamada.texto", "disp.chamada.botao-inicio")
	// §4b. Calendário
	add("calendario.carregando", "calendario.noite-livre", "calendario.noites-livres", "calendario.falha")
	add("calendario.falha-mes", "calendario.botao-whatsapp", "calendario.dia-minimo", "calendario.dia-consulta")
	add("calendario.dia-indisponivel", "calendario.aviso-passou", "calendario.aviso-consulta", "calendario.aviso-indisponivel")
	add("calendario.aviso-longa", "calendario.aviso-intervalo", "calendario.kpi-livres", "calendario.kpi-diaria")
	add("calendario.kpi-diaria-nota", "calendario.kpi-sinal", "calendario.kpi-sinal-nota", "calendario.kpi-pre-reserva")
	add("calendario.kpi-pre-reserva-nota")
	// §4b. Orçamento
	add("orcamento.titulo", "orcamento.selo", "orcamento.detalhes", "orcamento.check-in")
	add("orcamento.check-out", "orcamento.escolha-entrada", "orcamento.escolha-saida", "orcamento.calculando")
	add("orcamento.sinal", "orcamento.saldo", "orcamento.saldo-prazo", "orcamento.pre-reserva")
	add("orcamento.pre-reserva-prazo", "orcamento.consulta-produto", "orcamento.consulta-data", "orcamento.consulta-datas")
	add("orcamento.botao-consultar", "orcamento.limpeza", "orcamento.total", "orcamento.sinal-valor")
	add("orcamento.saldo-valor", "orcamento.diaria-media", "orcamento.botao-whatsapp", "orcamento.nota")
	// §4b. Formulário de pré-reserva
	add("pre-reserva.titulo", "pre-reserva.nome", "pre-reserva.whatsapp", "pre-reserva.whatsapp-exemplo")
	add("pre-reserva.email", "pre-reserva.opcional", "pre-reserva.consentimento", "pre-reserva.consentimento-falta")
	add("pre-reserva.botao", "pre-reserva.enviando", "pre-reserva.nota", "pre-reserva.erro-conflito")
	add("pre-reserva.erro-tentativas", "pre-reserva.erro-conexao", "pre-reserva.erro-geral", "pre-reserva.ok-rotulo")
	add("pre-reserva.ok-texto", "pre-reserva.ok-proximo", "pre-reserva.ok-botao-whatsapp", "pre-reserva.ok-botao-nova")
	// §4b. Mensagens prontas do WhatsApp
	add("whatsapp.mensagem-consulta", "whatsapp.mensagem-orcamento", "whatsapp.mensagem-pre-reserva", "whatsapp.pergunta-sinal")
	// 13. Página não encontrada
	add("erro.titulo", "erro.texto", "erro.rotulo", "erro.botao-disponibilidade")
	add("erro.botao-inicio")
	return k
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
	if len(noCatalogo) != 190 {
		t.Fatalf("esperadas 190 chaves (72 do §4 + 118 do §4b), há %d", len(noCatalogo))
	}
}

func TestSecoesNaOrdemDoContrato(t *testing.T) {
	esperado := []string{"seo", "marca", "menu", "inicio", "faixa", "casa", "acomodacoes", "eventos",
		"estrutura", "local", "chamada", "rodape", "disp", "calendario", "orcamento", "pre-reserva",
		"whatsapp", "erro"}
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
