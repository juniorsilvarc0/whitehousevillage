package bens

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMapeamentoDoVocabularioDoLevantamento(t *testing.T) {
	tipos := map[string]string{"externa": "area_externa", "quarto": "quarto", "area_externa": "area_externa"}
	for de, para := range tipos {
		if got, ok := MapearTipoDeAmbiente(de); !ok || got != para {
			t.Errorf("tipo %q → %q (%v), esperado %q", de, got, ok, para)
		}
	}
	if _, ok := MapearTipoDeAmbiente("piscina"); ok {
		t.Error("tipo fora do mapa deveria ser recusado")
	}

	cats := []struct{ categoria, nome, para string }{
		{"copo_taca", "Taça de vinho", "copo"},
		{"externo", "Vaso de barro terracota", "decoracao"},
		{"externo", "Capa de proteção para mesa redonda", "utensilio"},
		{"externo", "Lona plástica branca", "utensilio"},
		{"externo", "Mesa redonda branca com capa de proteção", "mobilia"}, // prefixo, não "contém"
		{"externo", "Guarda-sol", "mobilia"},
		{"externo", "Rede de dormir", "mobilia"},
		{"louca", "Prato", "louca"},
	}
	for _, c := range cats {
		if got, ok := MapearCategoria(c.categoria, c.nome); !ok || got != c.para {
			t.Errorf("categoria %q (%q) → %q (%v), esperado %q", c.categoria, c.nome, got, ok, c.para)
		}
	}
	if _, ok := MapearCategoria("piscina", "Boia"); ok {
		t.Error("categoria fora do mapa deveria ser recusada")
	}
}

// A chave de volume é IDENTIDADE da foto: estas saídas não podem mudar, senão
// a próxima importação escreve cópias novas de todas as fotos.
func TestChaveDeOrigemEhEstavel(t *testing.T) {
	casos := map[string]string{
		"chatwoot:2184:368056": "chatwoot-2184-368056",
		"Chatwoot:2227:374885": "chatwoot-2227-374885",
		"::a//b::":             "a-b",
		"ção":                  "o", // bytes fora do ASCII viram separador
		"::":                   "",
	}
	for origem, want := range casos {
		if got := chaveDeOrigem(origem); got != want {
			t.Errorf("chaveDeOrigem(%q) = %q, esperado %q", origem, got, want)
		}
	}
}

func TestDecodificarLevantamentoRecusaCampoDesconhecido(t *testing.T) {
	if _, err := DecodificarLevantamento(strings.NewReader(`{"unidade":"GV-01","ambientes":[],"itens":[],"extra":1}`)); err == nil {
		t.Fatal("campo fora do formato deveria ser recusado")
	}
	if _, err := DecodificarLevantamento(strings.NewReader(`{"unidade":"GV-01"} {}`)); err == nil {
		t.Fatal("dois documentos deveriam ser recusados")
	}
	lev, err := DecodificarLevantamento(strings.NewReader(`{"unidade":"GV-01","ambientes":[{"nome":"Sala","tipo":"sala","ordem":1}],"itens":[]}`))
	if err != nil || lev.Unidade != "GV-01" || len(lev.Ambientes) != 1 {
		t.Fatalf("levantamento válido: %+v %v", lev, err)
	}
}

func imagemDeTeste(t *testing.T, formato string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	var buf bytes.Buffer
	var err error
	if formato == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func levantamentoDeUnidade(t *testing.T) (Levantamento, fstest.MapFS) {
	t.Helper()
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8X"), make([]byte, 40)...)
	fotos := fstest.MapFS{
		"a.jpg":  {Data: imagemDeTeste(t, "jpeg")},
		"b.png":  {Data: imagemDeTeste(t, "png")},
		"c.webp": {Data: webp},
	}
	lev := Levantamento{
		Unidade: "AP-01",
		// Fora de ordem de propósito: o `code` repetido ganha sufixo na ordem
		// de caminhada (`ordem`), não na ordem do arquivo.
		Ambientes: []AmbienteLevantado{
			{Nome: "SALA!", Tipo: "sala", Ordem: 3},
			{Nome: "Área externa", Tipo: "externa", Ordem: 1},
			{Nome: "Sala", Tipo: "sala", Ordem: 2},
		},
		Itens: []ItemLevantado{
			{
				SourceRef: "inventario:AP-01:taca", Nome: "Taça", Descricao: "  ", Categoria: "copo_taca", Confianca: "alta",
				Fotos:     []FotoLevantada{{Arquivo: "a.jpg", SourceRef: "chatwoot:1:10"}},
				Ambientes: map[string]ColocacaoLevantada{"SALA!": {Quantidade: 2}, "Área externa": {Quantidade: 1}},
			},
			{
				SourceRef: "inventario:AP-01:vaso", Nome: "Vaso de barro", Categoria: "externo", Confianca: "baixa",
				Fotos:     []FotoLevantada{{Arquivo: "a.jpg", SourceRef: "chatwoot:1:10"}, {Arquivo: "b.png", SourceRef: "chatwoot:1:11"}, {Arquivo: "c.webp", SourceRef: "chatwoot:1:12"}},
				Ambientes: map[string]ColocacaoLevantada{"Sala": {Quantidade: 3}},
			},
		},
	}
	return lev, fotos
}

func TestPlanoDaImportacao(t *testing.T) {
	lev, fotos := levantamentoDeUnidade(t)
	p, err := planejarImportacao(lev, fotos)
	if err != nil {
		t.Fatal(err)
	}

	codigos := []string{}
	for _, a := range p.Ambientes {
		codigos = append(codigos, a.Nome+"="+a.Codigo+"/"+a.Tipo)
	}
	if got := strings.Join(codigos, " "); got != "Área externa=area-externa/area_externa Sala=sala/sala SALA!=sala-2/sala" {
		t.Fatalf("ambientes na ordem de caminhada, com sufixo só na colisão: %s", got)
	}

	taca, vaso := p.Itens[0], p.Itens[1]
	if taca.Bem.Categoria != "copo" || taca.Bem.Descricao != nil || taca.Nota != nil {
		t.Fatalf("taça: copo_taca vira copo, descrição em branco vira nula, confiança alta sem nota: %+v", taca)
	}
	if len(taca.Colocacoes) != 2 || taca.Colocacoes[0].Ambiente != "Área externa" || taca.Colocacoes[1].Ambiente != "SALA!" {
		t.Fatalf("colocações na ordem de caminhada: %+v", taca.Colocacoes)
	}
	if vaso.Bem.Categoria != "decoracao" || vaso.Nota == nil || *vaso.Nota != notaDeConfiancaBaixa {
		t.Fatalf("vaso: externo começando com Vaso vira decoração, confiança baixa leva a nota: %+v", vaso)
	}
	if strings.Join(vaso.Fotos, ",") != "a.jpg,b.png,c.webp" {
		t.Fatalf("a galeria segue a ordem do levantamento: %v", vaso.Fotos)
	}

	// Uma foto por arquivo, compartilhada; chave determinística pela origem;
	// WebP sem miniatura (a stdlib não decodifica).
	if len(p.Fotos) != 3 {
		t.Fatalf("três arquivos distintos, uma foto cada: %+v", p.Fotos)
	}
	a, b, c := p.Fotos[0], p.Fotos[1], p.Fotos[2]
	if a.Chave != "importacao-chatwoot-1-10.jpg" || a.Miniatura != "importacao-chatwoot-1-10.thumb.jpg" || a.Mime != mimeJPEG || !a.Converter {
		t.Fatalf("foto a: %+v", a)
	}
	// O PNG é convertido como no envio do painel: vai para o volume como JPEG,
	// e a chave já diz isso no plano (o dry-run sabe sem converter).
	if b.Chave != "importacao-chatwoot-1-11.jpg" || b.Mime != mimeJPEG || b.MimeDaOrigem != mimePNG || !b.Converter {
		t.Fatalf("foto b: PNG convertido vira JPEG no volume: %+v", b)
	}
	if c.Chave != "importacao-chatwoot-1-12.webp" || c.Miniatura != "" || c.Converter || c.Mime != mimeWebP {
		t.Fatalf("foto c: WebP vai como veio, sem miniatura: %+v", c)
	}
	if p.Pecas != 6 {
		t.Fatalf("peças = %d, esperado 6", p.Pecas)
	}
	if p.Mapeados["tipo externa → area_externa"] != 1 || p.Mapeados["categoria copo_taca → copo"] != 1 ||
		p.Mapeados["categoria externo → decoracao"] != 1 {
		t.Fatalf("mapeamentos contados: %v", p.Mapeados)
	}
}

// Todos os problemas de uma vez, e nenhum passa: a casa é recusada antes de
// tocar no banco ou no disco.
func TestPlanoDaImportacaoListaTodosOsProblemas(t *testing.T) {
	lev, fotos := levantamentoDeUnidade(t)
	fotos["texto.jpg"] = &fstest.MapFile{Data: []byte("isto não é uma foto")}
	fotos["vazia.jpg"] = &fstest.MapFile{Data: []byte{}}
	lev.Ambientes = append(lev.Ambientes, AmbienteLevantado{Nome: "Piscina", Tipo: "piscina", Ordem: 9}, AmbienteLevantado{Nome: "Sala", Tipo: "sala", Ordem: 10})
	lev.Itens = append(lev.Itens,
		ItemLevantado{SourceRef: "inventario:AP-01:taca", Nome: "Repetido", Categoria: "louca", Confianca: "alta"},
		ItemLevantado{SourceRef: "inventario:AP-01:boia", Nome: "Boia", Categoria: "piscina", Confianca: "talvez",
			Fotos: []FotoLevantada{
				{Arquivo: "../fora.jpg", SourceRef: "x:1"},
				{Arquivo: "sumiu.jpg", SourceRef: "x:2"},
				{Arquivo: "texto.jpg", SourceRef: "x:3"},
				{Arquivo: "vazia.jpg", SourceRef: "x:4"},
				{Arquivo: "a.jpg", SourceRef: "outra:origem"},
			},
			Ambientes: map[string]ColocacaoLevantada{"Cozinha": {Quantidade: 1}, "Sala": {Quantidade: -1}}},
	)
	_, err := planejarImportacao(lev, fotos)
	var e *ErroDeLevantamento
	if !errors.As(err, &e) {
		t.Fatalf("esperado ErroDeLevantamento, veio %v", err)
	}
	texto := err.Error()
	for _, esperado := range []string{
		`tipo "piscina" fora do mapa`, `ambiente "Sala" repetido`, "`source_ref` repetido",
		`categoria "piscina" fora do mapa`, `confiança "talvez"`, `nome de arquivo inválido "../fora.jpg"`,
		`"sumiu.jpg": não encontrada`, `"texto.jpg": não é JPEG, PNG nem WebP pelos bytes`, `"vazia.jpg": arquivo vazio`,
		`foto "a.jpg" citada com duas origens`, `ambiente "Cozinha" não está na lista`, `quantidade -1`,
	} {
		if !strings.Contains(texto, esperado) {
			t.Errorf("o erro deveria citar %q:\n%s", esperado, texto)
		}
	}
}

// A foto acima de 15 MiB é recusada pelo tamanho, como no envio pelo painel.
func TestPlanoRecusaFotoAcimaDoLimite(t *testing.T) {
	lev, fotos := levantamentoDeUnidade(t)
	fotos["a.jpg"] = &fstest.MapFile{Data: append(imagemDeTeste(t, "jpeg"), make([]byte, LimiteDaFoto)...)}
	if _, err := planejarImportacao(lev, fotos); err == nil || !strings.Contains(err.Error(), "acima do limite") {
		t.Fatalf("foto grande demais deveria ser recusada: %v", err)
	}
}
