package bens

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// A planilha é CSV com três escolhas feitas para abrir no Excel em português
// do computador do gestor: separador `;`, UTF-8 COM BOM e decimal com vírgula.
// Sem as três, o Excel abre tudo numa coluna só, com "Pôr" escrito errado — e o
// arquivo não serviu para nada. Nenhuma biblioteca nova: encoding/csv basta.

// bom é a marca de ordem de bytes do UTF-8. É ela que faz o Excel ler o
// arquivo como UTF-8 em vez de Windows-1252.
const bom = "\xEF\xBB\xBF"

var cabecalhoDaPlanilha = []string{
	"Unidade", "Ambiente", "Tipo de ambiente", "Bem", "Descrição", "Categoria",
	"Unidade de medida", "Quantidade esperada", "Custo de reposição (R$)", "Pendências abertas",
}

// Rótulos de leitura humana dos vocabulários fechados. O código (`area_externa`)
// é contrato da API; a planilha é para gente.
var (
	rotuloDoAmbiente = map[string]string{
		"quarto": "Quarto", "banheiro": "Banheiro", "cozinha": "Cozinha", "sala": "Sala",
		"area_externa": "Área externa", "lavanderia": "Lavanderia", "varanda": "Varanda", "outro": "Outro",
	}
	rotuloDaCategoria = map[string]string{
		"louca": "Louça", "talher": "Talher", "copo": "Copo", "cama": "Cama", "banho": "Banho",
		"mobilia": "Mobília", "eletro": "Eletro", "utensilio": "Utensílio", "decoracao": "Decoração", "outro": "Outro",
	}
)

func rotulo(mapa map[string]string, codigo string) string {
	if r, ok := mapa[codigo]; ok {
		return r
	}
	return codigo
}

// escreverCSV grava a planilha inteira. Uma linha por colocação, na ordem em
// que chegou (a de caminhada pela casa).
func escreverCSV(w io.Writer, linhas []linhaExportada) error {
	if _, err := io.WriteString(w, bom); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	cw.UseCRLF = true // o Excel no Windows espera CRLF

	if err := cw.Write(cabecalhoDaPlanilha); err != nil {
		return err
	}
	for _, l := range linhas {
		descricao := ""
		if l.BemDescricao != nil {
			descricao = *l.BemDescricao
		}
		custo := ""
		if l.Custo != nil {
			custo = reais(*l.Custo)
		}
		if err := cw.Write([]string{
			celula(l.UnidadeCodigo),
			celula(l.AmbienteNome),
			rotulo(rotuloDoAmbiente, l.AmbienteTipo),
			celula(l.BemNome),
			celula(descricao),
			rotulo(rotuloDaCategoria, l.Categoria),
			l.Medida,
			strconv.Itoa(l.QtdEsperada),
			custo,
			strconv.Itoa(l.AvariasAbertas),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// reais formata centavos com vírgula decimal e sem separador de milhar
// ("1234,56"): é o que o Excel em português lê como número em qualquer
// configuração, e a conta sai de inteiros — dinheiro não passa por float.
func reais(centavos int64) string {
	sinal := ""
	if centavos < 0 {
		sinal, centavos = "-", -centavos
	}
	return fmt.Sprintf("%s%d,%02d", sinal, centavos/100, centavos%100)
}

// celula neutraliza a INJEÇÃO DE FÓRMULA: texto digitado no celular que
// começa com `=`, `+`, `-`, `@` (ou tab/CR) seria executado pelo Excel como
// fórmula ao abrir a planilha — `=HYPERLINK(...)` no nome de um bem viraria um
// link clicável para qualquer lugar. O apóstrofo na frente faz o Excel tratar
// a célula como texto, e é a recomendação da OWASP.
func celula(texto string) string {
	if texto == "" {
		return texto
	}
	switch texto[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + texto
	}
	return texto
}

// nomeDoArquivo monta `inventario-<recorte>-<AAAA-MM-DD>.csv`, só com ASCII
// minúsculo, dígito e hífen — o cabeçalho Content-Disposition não precisa de
// codificação e nenhum sistema de arquivos estranha o nome.
func nomeDoArquivo(escopo, hoje string) string {
	s := slug(escopo)
	if s == "" {
		s = "propriedade"
	}
	return "inventario-" + s + "-" + hoje + ".csv"
}

// semAcento cobre o que aparece em código de unidade e slug de casa no
// português. Tabela, e não golang.org/x/text: dependência nova no go.mod é
// decisão do tech-lead, e um nome de arquivo não justifica uma.
var semAcento = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// slug tira acento, baixa a caixa e troca o resto por hífen.
func slug(texto string) string {
	var b strings.Builder
	hifen := false
	for _, r := range semAcento.Replace(strings.ToLower(texto)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			hifen = false
		case !hifen && b.Len() > 0:
			b.WriteByte('-')
			hifen = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
