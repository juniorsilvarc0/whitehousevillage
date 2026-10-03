package site

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// O catálogo e o site falam das MESMAS chaves (docs/site-cms.md §1: "o teste
// confere que toda chave do HTML existe no catálogo e vice-versa").
//
// A varredura é de TEXTO sobre apps/site/public, de propósito simples:
//
//   - nos .html e .js, todo atributo data-cms, data-cms-titulo,
//     -paragrafos, -img, -video, -lista, -linhas, -email e -meta cujo valor
//     tenha cara de chave (com ponto) — os data-cms-campo* são subcampos de
//     item de lista e ficam de fora;
//   - nos .js, todo literal de string que comece por uma seção do catálogo
//     seguida de ponto ('orcamento.total', "calendario.falha") — é o que pega
//     WH.t('…'), WH.cru('…') e WH_CMS.t('…') inclusive quando a função foi
//     guardada num apelido.
//
// Um erro de digitação no site ('orcamento.totla') acende o primeiro
// sentido; uma chave do catálogo que o site não marca em lugar nenhum acende
// o segundo.
const raizDoSite = "../../../../site/public"

var (
	atributoCMS = regexp.MustCompile(`data-cms(?:-titulo|-paragrafos|-img|-video|-lista|-linhas|-email|-meta)?="([^"]*)"`)
	literalJS   = regexp.MustCompile("['\"`]([a-z0-9]+(?:[.-][a-z0-9]+)*)['\"`]")
)

// usoDinamico são as chaves que o site monta por concatenação — não aparecem
// como literal inteiro. Cada prefixo exige uma evidência textual no JS.
var usoDinamico = map[string]string{
	// vitrine.js, categoriaEditada: v['categoria.' + chave + '.' + campo]
	"categoria.": `'categoria.' + chave + '.' + campo`,
}

func chavesUsadasNoSite(t *testing.T) (map[string][]string, string) {
	t.Helper()
	secoes := map[string]bool{}
	for _, s := range Catalogo() {
		secoes[s.Chave] = true
	}
	secoes["categoria"] = true

	usadas := map[string][]string{}
	var todoJS strings.Builder
	arquivos := 0
	err := filepath.WalkDir(raizDoSite, func(caminho string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		ext := filepath.Ext(caminho)
		if d.IsDir() || (ext != ".html" && ext != ".js") {
			return nil
		}
		bruto, err := os.ReadFile(caminho)
		if err != nil {
			return err
		}
		arquivos++
		texto := string(bruto)
		nome := filepath.ToSlash(strings.TrimPrefix(caminho, raizDoSite+string(filepath.Separator)))
		for _, m := range atributoCMS.FindAllStringSubmatch(texto, -1) {
			if strings.Contains(m[1], ".") {
				usadas[m[1]] = append(usadas[m[1]], nome)
			}
		}
		if ext == ".js" {
			todoJS.WriteString(texto)
			for _, m := range literalJS.FindAllStringSubmatch(texto, -1) {
				i := strings.IndexByte(m[1], '.')
				if i > 0 && secoes[m[1][:i]] {
					usadas[m[1]] = append(usadas[m[1]], nome)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("varrendo %s: %v", raizDoSite, err)
	}
	if arquivos < 4 || len(usadas) < 100 {
		t.Fatalf("a varredura de %s leu %d arquivos e achou %d chaves — o caminho ou o formato da marcação mudou "+
			"e o teste deixou de proteger alguma coisa", raizDoSite, arquivos, len(usadas))
	}
	return usadas, todoJS.String()
}

func TestTodaChaveDoSiteExisteNoCatalogo(t *testing.T) {
	usadas, _ := chavesUsadasNoSite(t)
	var fora []string
	for chave, onde := range usadas {
		if _, ok := CampoDoCatalogo(chave); !ok {
			fora = append(fora, chave+" (em "+strings.Join(onde, ", ")+")")
		}
	}
	sort.Strings(fora)
	if len(fora) > 0 {
		t.Fatalf("o site usa chaves que o catálogo não tem — erro de digitação de um lado ou campo esquecido "+
			"no catalogo.go:\n  %s", strings.Join(fora, "\n  "))
	}
}

func TestTodaChaveDoCatalogoEhUsadaPeloSite(t *testing.T) {
	usadas, js := chavesUsadasNoSite(t)
	for prefixo, evidencia := range usoDinamico {
		if !strings.Contains(js, evidencia) {
			t.Errorf("o uso dinâmico de %q* sumiu do site (procurei %q): ou a montagem mudou, ou as chaves "+
				"%s* não são mais lidas", prefixo, evidencia, prefixo)
		}
	}
	var orfas []string
	for _, s := range Catalogo() {
		for _, c := range s.Campos {
			if _, ok := usadas[c.Chave]; ok {
				continue
			}
			dinamica := false
			for prefixo := range usoDinamico {
				if strings.HasPrefix(c.Chave, prefixo) {
					dinamica = true
				}
			}
			if !dinamica {
				orfas = append(orfas, c.Chave)
			}
		}
	}
	if len(orfas) > 0 {
		t.Fatalf("chaves do catálogo que o site não usa em lugar nenhum — o gestor editaria e nada mudaria:\n  %s",
			strings.Join(orfas, "\n  "))
	}
}
