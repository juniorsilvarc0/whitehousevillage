package bens

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Importador do levantamento fotográfico das casas (cmd/importar-bens).
//
// É o que põe o inventário real no painel: um JSON consolidado por unidade
// (`consolidado/GV-01.json`) com ambientes, itens, fotos e colocações, mais a
// pasta das fotos citadas. A regra que atravessa tudo aqui é a de um
// importador que pode rodar de novo:
//
//   - REPETÍVEL: a segunda passada não cria nada. Cada entidade tem uma chave
//     natural estável — `code` do cômodo, `source_ref` do item, `storage_key`
//     determinística da foto, os pares da galeria e da colocação — e cada
//     escrita é `INSERT … ON CONFLICT DO NOTHING`, com a constraint decidindo;
//   - SÓ ACRESCENTA: o que já existe NUNCA é reescrito. O gestor pode ter
//     renomeado o cômodo, corrigido o nome ou a categoria do item, ajustado a
//     quantidade; a reimportação não desfaz nada disso;
//   - UMA TRANSAÇÃO POR CASA, com a mesma trava por unidade da cópia e da
//     abertura de conferência. Casa com conferência ABERTA é recusada inteira:
//     cômodo ou colocação nova ficaria fora das linhas congeladas, e a contagem
//     fecharia "completa" sem ter olhado para ela;
//   - VALIDAÇÃO ANTES DE TUDO: vocabulário fora do mapa, nome fora do limite do
//     contrato, foto que não é foto pelos bytes — qualquer problema aborta a
//     casa inteira antes de tocar no banco ou no disco;
//   - A FOTO PASSA PELA MESMA CONVERSÃO DO ENVIO DO PAINEL (conversao.go):
//     JPEG e PNG vão para o volume em JPEG de até 1280 px, qualidade 75, sem
//     metadados — e a chave já sai `.jpg` no plano, decidida pelo cabeçalho;
//     WebP e foto grande demais vão como vieram, com aviso. Arquivo que já
//     está no volume não é reescrito nem reconvertido.
//
// O dry-run percorre EXATAMENTE o mesmo caminho, numa transação READ ONLY, e
// troca cada INSERT pela pergunta "isto já está lá?". Assim o plano que ele
// imprime é o que a execução faria, e não uma segunda implementação dela.
//
// ARQUIVO ÓRFÃO, dito para ninguém se surpreender: as fotos vão para o volume
// ANTES do commit (só depois da recusa por conferência aberta, então casa
// recusada não deixa arquivo). Se a transação falhar depois disso, sobram no
// volume arquivos sem linha em `inventory_media`. O nome deles é determinístico
// (derivado da origem da foto), e a reexecução os encontra e reaproveita em vez
// de escrever de novo — o órfão só vive até a próxima passada bem-sucedida.

// Mapeamento de vocabulário do levantamento para o do contrato (decidido pelo
// tech-lead em 07/10/2026).
const (
	tipoLevantadoExterna    = "externa"
	categoriaLevantadaCopo  = "copo_taca"
	categoriaLevantadaExter = "externo"
)

// notaDeConfiancaBaixa vai na colocação do item que o levantamento identificou
// pela foto com confiança baixa.
const notaDeConfiancaBaixa = "Identificado pela foto do levantamento com confiança baixa — confira na próxima contagem."

// confiancas é o vocabulário fechado do campo `confianca`.
var confiancas = []string{"alta", "media", "baixa"}

const (
	// prefixoDaChaveImportada separa no volume a foto que veio do levantamento
	// da enviada pelo painel (`<uuid>.<ext>`) — e torna a chave legível por
	// quem opera o volume.
	prefixoDaChaveImportada = "importacao-"
	maxOrigem               = 500
	maxChaveImportada       = 200
)

// ─────────────────────────── O formato do levantamento ──────────────────────

// Levantamento é um `consolidado/<UNIDADE>.json`.
type Levantamento struct {
	Unidade   string              `json:"unidade"`
	Ambientes []AmbienteLevantado `json:"ambientes"`
	Itens     []ItemLevantado     `json:"itens"`
}

// AmbienteLevantado é um cômodo da casa, na ordem de caminhada (`ordem`).
type AmbienteLevantado struct {
	Nome  string `json:"nome"`
	Tipo  string `json:"tipo"`
	Ordem int    `json:"ordem"`
}

// ItemLevantado é um bem identificado nas fotos.
type ItemLevantado struct {
	SourceRef string                        `json:"source_ref"`
	Nome      string                        `json:"nome"`
	Descricao string                        `json:"descricao"`
	Categoria string                        `json:"categoria"`
	Confianca string                        `json:"confianca"`
	Fotos     []FotoLevantada               `json:"fotos"`
	Ambientes map[string]ColocacaoLevantada `json:"ambientes"`
}

// FotoLevantada é uma foto do item; o mesmo arquivo pode mostrar vários itens.
type FotoLevantada struct {
	Arquivo   string `json:"arquivo"`
	SourceRef string `json:"source_ref"`
	Legenda   string `json:"legenda"`
}

// ColocacaoLevantada é quanto do item há num cômodo. As `fotos` daqui são um
// subconjunto das do item e não entram no banco (a galeria é do item).
type ColocacaoLevantada struct {
	Quantidade int      `json:"quantidade"`
	Fotos      []string `json:"fotos"`
}

// DecodificarLevantamento lê o JSON com campo desconhecido RECUSADO: formato
// que mudou é erro ruidoso, não campo ignorado em silêncio.
func DecodificarLevantamento(r io.Reader) (Levantamento, error) {
	var lev Levantamento
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&lev); err != nil {
		return Levantamento{}, fmt.Errorf("levantamento ilegível: %w", err)
	}
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return Levantamento{}, errors.New("levantamento com mais de um documento JSON")
	}
	return lev, nil
}

// MapearTipoDeAmbiente traduz o tipo do levantamento para o vocabulário de
// `unit_rooms.kind`. `externa` vira `area_externa`; o resto tem de já ser
// válido.
func MapearTipoDeAmbiente(tipo string) (string, bool) {
	if tipo == tipoLevantadoExterna {
		return "area_externa", true
	}
	return tipo, DoVocabulario(tipo, tiposDeAmbiente)
}

// MapearCategoria traduz a categoria do levantamento para a do catálogo.
// `copo_taca` vira `copo`; `externo` não é categoria de objeto e vai para a do
// objeto pelo nome — "Vaso…" é decoração, "Capa…"/"Lona…" é utensílio, o resto
// (cadeiras, mesas, chaises, ombrelones, guarda-sóis, rede) é mobília.
func MapearCategoria(categoria, nome string) (string, bool) {
	switch categoria {
	case categoriaLevantadaCopo:
		return "copo", true
	case categoriaLevantadaExter:
		n := strings.ToLower(strings.TrimSpace(nome))
		switch {
		case strings.HasPrefix(n, "vaso"):
			return "decoracao", true
		case strings.HasPrefix(n, "capa"), strings.HasPrefix(n, "lona"):
			return "utensilio", true
		default:
			return "mobilia", true
		}
	}
	return categoria, DoVocabulario(categoria, categorias)
}

// chaveDeOrigem deriva o pedaço estável da `storage_key` a partir da origem da
// foto ("chatwoot:2184:368056" → "chatwoot-2184-368056"): ASCII minúsculo,
// dígito e hífen. É IDENTIDADE da foto no volume — mudar esta regra faria a
// próxima importação escrever cópias novas de todas as fotos —, por isso não
// reaproveita o `slug` do CSV, que pode mudar por motivo de planilha.
func chaveDeOrigem(origem string) string {
	var b strings.Builder
	hifen := false
	for i := 0; i < len(origem); i++ {
		c := origem[i]
		switch {
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			b.WriteByte(c)
			hifen = false
		case c >= 'A' && c <= 'Z':
			b.WriteByte(c - 'A' + 'a')
			hifen = false
		case !hifen && b.Len() > 0:
			b.WriteByte('-')
			hifen = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// arquivoSeguro aceita só nome simples de arquivo dentro de `fotos/`: nada de
// caminho, de `..` nem de arquivo oculto. O nome vem do JSON, e é ele que abre
// o arquivo.
func arquivoSeguro(nome string) bool {
	return nome != "" && fs.ValidPath(nome) && !strings.Contains(nome, "/") &&
		!strings.HasPrefix(nome, ".") && filepath.Base(nome) == nome
}

// ─────────────────────────── O plano validado ───────────────────────────────

type ambienteDoPlano struct {
	Nome   string
	Codigo string
	Tipo   string
	Ordem  int
}

type colocacaoDoPlano struct {
	Ambiente string
	Qtd      int
}

type itemDoPlano struct {
	Bem        bemImportado
	Fotos      []string
	Colocacoes []colocacaoDoPlano
	Nota       *string
}

type fotoDoPlano struct {
	Arquivo string
	Origem  string
	// MimeDaOrigem é o tipo do arquivo do levantamento; Mime, o do arquivo que
	// vai para o volume (JPEG quando Converter).
	MimeDaOrigem string
	Mime         string
	// Converter: JPEG/PNG que cabe na memória é convertido como no envio do
	// painel (conversao.go). WebP e foto grande demais vão como vieram.
	Converter bool
	Chave     string
	Miniatura string
}

// planoDaImportacao é a casa já validada, mapeada e com as chaves calculadas.
type planoDaImportacao struct {
	Unidade   string
	Ambientes []ambienteDoPlano
	Itens     []itemDoPlano
	Fotos     []fotoDoPlano
	Pecas     int
	Mapeados  map[string]int
}

// ErroDeLevantamento lista TODOS os problemas da casa de uma vez: quem corrige
// o JSON corrige tudo numa passada, em vez de descobrir um problema por vez.
type ErroDeLevantamento struct {
	Unidade   string
	Problemas []string
}

func (e *ErroDeLevantamento) Error() string {
	return fmt.Sprintf("levantamento %s recusado antes de gravar (%d problema(s)):\n  - %s",
		e.Unidade, len(e.Problemas), strings.Join(e.Problemas, "\n  - "))
}

// planejarImportacao valida a casa inteira e devolve o plano. Nenhum acesso ao
// banco nem ao volume: só ao JSON e às fotos de origem (tipo pelos bytes e
// tamanho). Qualquer problema é ErroDeLevantamento, e a casa não é tocada.
func planejarImportacao(lev Levantamento, fotos fs.FS) (planoDaImportacao, error) {
	var problemas []string
	falha := func(formato string, args ...any) { problemas = append(problemas, fmt.Sprintf(formato, args...)) }

	p := planoDaImportacao{Unidade: strings.TrimSpace(lev.Unidade), Mapeados: map[string]int{}}
	if p.Unidade == "" {
		falha("`unidade` vazia")
	}

	// Ambientes na ordem de caminhada: é nessa ordem que o `code` repetido
	// dentro do JSON ganha sufixo, e o primeiro fica com a base.
	ordenados := append([]AmbienteLevantado(nil), lev.Ambientes...)
	sort.SliceStable(ordenados, func(i, j int) bool {
		if ordenados[i].Ordem != ordenados[j].Ordem {
			return ordenados[i].Ordem < ordenados[j].Ordem
		}
		return ordenados[i].Nome < ordenados[j].Nome
	})
	ordemDoAmbiente := map[string]int{}
	codigos := map[string]bool{}
	for _, a := range ordenados {
		nome := strings.TrimSpace(a.Nome)
		switch {
		case nome == "":
			falha("ambiente sem nome (ordem %d)", a.Ordem)
			continue
		case utf8.RuneCountInString(nome) > maxNomeDoAmbiente:
			falha("ambiente %q: nome com mais de %d caracteres", nome, maxNomeDoAmbiente)
			continue
		}
		if _, repetido := ordemDoAmbiente[nome]; repetido {
			falha("ambiente %q repetido", nome)
			continue
		}
		tipo, ok := MapearTipoDeAmbiente(a.Tipo)
		if !ok {
			falha("ambiente %q: tipo %q fora do mapa", nome, a.Tipo)
		} else if tipo != a.Tipo {
			p.Mapeados["tipo "+a.Tipo+" → "+tipo]++
		}
		if a.Ordem < math.MinInt32 || a.Ordem > maxInteiro {
			falha("ambiente %q: ordem %d fora da faixa", nome, a.Ordem)
		}
		base := BaseDoCodigo(nome)
		codigo := base
		for n := 2; codigos[codigo]; n++ {
			codigo = CandidatoDoCodigo(base, n)
		}
		codigos[codigo] = true
		ordemDoAmbiente[nome] = len(p.Ambientes)
		p.Ambientes = append(p.Ambientes, ambienteDoPlano{Nome: nome, Codigo: codigo, Tipo: tipo, Ordem: a.Ordem})
	}

	origens := map[string]bool{}
	origemDoArquivo := map[string]string{}
	arquivoDaOrigem := map[string]string{}
	for i, it := range lev.Itens {
		rotulo := fmt.Sprintf("item %d (%q)", i+1, it.SourceRef)
		origem := strings.TrimSpace(it.SourceRef)
		switch {
		case origem == "":
			falha("%s: `source_ref` vazio", rotulo)
		case len(origem) > maxOrigem:
			falha("%s: `source_ref` com mais de %d bytes", rotulo, maxOrigem)
		case origens[origem]:
			falha("%s: `source_ref` repetido", rotulo)
		}
		origens[origem] = true

		nome := strings.TrimSpace(it.Nome)
		if n := utf8.RuneCountInString(nome); n == 0 || n > maxNomeDoBem {
			falha("%s: nome vazio ou com mais de %d caracteres", rotulo, maxNomeDoBem)
		}
		if utf8.RuneCountInString(strings.TrimSpace(it.Descricao)) > maxTextoLivre {
			falha("%s: descrição com mais de %d caracteres", rotulo, maxTextoLivre)
		}
		categoria, ok := MapearCategoria(it.Categoria, nome)
		if !ok {
			falha("%s: categoria %q fora do mapa", rotulo, it.Categoria)
		} else if categoria != it.Categoria {
			p.Mapeados["categoria "+it.Categoria+" → "+categoria]++
		}
		var nota *string
		switch it.Confianca {
		case "baixa":
			n := notaDeConfiancaBaixa
			nota = &n
		case "alta", "media":
		default:
			falha("%s: confiança %q fora de %v", rotulo, it.Confianca, confiancas)
		}

		item := itemDoPlano{
			Bem: bemImportado{
				OrigemDaImportacao: origem, Nome: nome, Descricao: textoOuNulo(&it.Descricao), Categoria: categoria,
			},
			Nota: nota,
		}

		vistas := map[string]bool{}
		for _, f := range it.Fotos {
			if !arquivoSeguro(f.Arquivo) {
				falha("%s: foto com nome de arquivo inválido %q", rotulo, f.Arquivo)
				continue
			}
			if vistas[f.Arquivo] {
				falha("%s: foto %q repetida na galeria", rotulo, f.Arquivo)
				continue
			}
			vistas[f.Arquivo] = true
			fo := strings.TrimSpace(f.SourceRef)
			if fo == "" {
				falha("%s: foto %q sem `source_ref`", rotulo, f.Arquivo)
				continue
			}
			if outra, ok := origemDoArquivo[f.Arquivo]; ok && outra != fo {
				falha("foto %q citada com duas origens (%q e %q)", f.Arquivo, outra, fo)
				continue
			}
			if outro, ok := arquivoDaOrigem[fo]; ok && outro != f.Arquivo {
				falha("origem %q citada em dois arquivos (%q e %q)", fo, outro, f.Arquivo)
				continue
			}
			if _, conhecida := origemDoArquivo[f.Arquivo]; !conhecida {
				origemDoArquivo[f.Arquivo], arquivoDaOrigem[fo] = fo, f.Arquivo
				p.Fotos = append(p.Fotos, fotoDoPlano{Arquivo: f.Arquivo, Origem: fo})
			}
			item.Fotos = append(item.Fotos, f.Arquivo)
		}

		colocados := map[string]bool{}
		for nomeDoAmbiente, c := range it.Ambientes {
			amb := strings.TrimSpace(nomeDoAmbiente)
			if _, ok := ordemDoAmbiente[amb]; !ok {
				falha("%s: ambiente %q não está na lista de ambientes", rotulo, nomeDoAmbiente)
				continue
			}
			if colocados[amb] {
				falha("%s: ambiente %q aparece duas vezes nas colocações", rotulo, amb)
				continue
			}
			colocados[amb] = true
			if c.Quantidade < 0 || c.Quantidade > maxInteiro {
				falha("%s: quantidade %d em %q fora da faixa", rotulo, c.Quantidade, amb)
				continue
			}
			item.Colocacoes = append(item.Colocacoes, colocacaoDoPlano{Ambiente: amb, Qtd: c.Quantidade})
			p.Pecas += c.Quantidade
		}
		// O mapa do JSON não tem ordem; a colocação sai na ordem de caminhada.
		sort.Slice(item.Colocacoes, func(a, b int) bool {
			return ordemDoAmbiente[item.Colocacoes[a].Ambiente] < ordemDoAmbiente[item.Colocacoes[b].Ambiente]
		})
		p.Itens = append(p.Itens, item)
	}

	// As fotos de origem: existem, são foto PELOS BYTES (JPEG, PNG ou WebP),
	// cabem no limite do envio pelo painel, e a chave do volume é única.
	donoDaChave := map[string]string{}
	for i := range p.Fotos {
		f := &p.Fotos[i]
		tipo, convertivel, problema := conferirFotoDeOrigem(fotos, f.Arquivo)
		if problema != "" {
			falha("foto %q: %s", f.Arquivo, problema)
			continue
		}
		// O que vai para o volume decide a chave: a convertida é JPEG, e a
		// decisão sai do CABEÇALHO (tipo e tamanho), não da decodificação —
		// assim o dry-run sabe a chave sem converter nada.
		ext := tipo.ext
		f.MimeDaOrigem, f.Mime, f.Converter = tipo.mime, tipo.mime, convertivel
		if convertivel {
			ext, f.Mime = "jpg", mimeJPEG
		}
		base := chaveDeOrigem(f.Origem)
		switch {
		case base == "":
			falha("foto %q: origem %q não gera chave de volume", f.Arquivo, f.Origem)
			continue
		case len(base) > maxChaveImportada:
			falha("foto %q: origem %q longa demais para chave de volume", f.Arquivo, f.Origem)
			continue
		}
		if outra, ok := donoDaChave[base]; ok {
			falha("fotos de origens %q e %q caem na mesma chave de volume %q", outra, f.Origem, base)
			continue
		}
		donoDaChave[base] = f.Origem
		f.Chave = prefixoDaChaveImportada + base + "." + ext
		if convertivel {
			f.Miniatura = prefixoDaChaveImportada + base + ".thumb.jpg"
		}
	}

	if len(problemas) > 0 {
		return p, &ErroDeLevantamento{Unidade: p.Unidade, Problemas: problemas}
	}
	return p, nil
}

// conferirFotoDeOrigem abre a foto no diretório do levantamento e confere
// tamanho e tipo pelos bytes; de JPEG e PNG lê também o cabeçalho de imagem,
// que diz se a foto cabe na memória para ser convertida. Devolve o problema
// em linguagem de quem corrige.
func conferirFotoDeOrigem(fotos fs.FS, arquivo string) (tipo tipoDeFoto, convertivel bool, problema string) {
	f, err := fotos.Open(arquivo)
	if err != nil {
		return tipoDeFoto{}, false, "não encontrada na pasta de fotos"
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return tipoDeFoto{}, false, "ilegível"
	}
	switch {
	case info.Size() == 0:
		return tipoDeFoto{}, false, "arquivo vazio"
	case info.Size() > LimiteDaFoto:
		return tipoDeFoto{}, false, fmt.Sprintf("%d bytes, acima do limite de 15 MiB", info.Size())
	}
	cabeca := make([]byte, tamanhoDaCabeca)
	n, err := io.ReadFull(f, cabeca)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return tipoDeFoto{}, false, "ilegível"
	}
	tipo, ok := detectarFoto(cabeca[:n])
	if !ok {
		return tipoDeFoto{}, false, "não é JPEG, PNG nem WebP pelos bytes"
	}
	if tipo.mime == mimeWebP {
		return tipo, false, ""
	}
	cfg, err := configDaImagem(io.MultiReader(bytes.NewReader(cabeca[:n]), f), tipo.mime)
	if err != nil {
		return tipoDeFoto{}, false, "cabeçalho de imagem ilegível: " + err.Error()
	}
	return tipo, cabeNoOrcamento(cfg), ""
}

// ─────────────────────────── O importador ───────────────────────────────────

// Importador grava o levantamento de uma casa por vez.
type Importador struct {
	s *Service
}

// NovoImportador monta o importador sobre o pool, com o mesmo volume de mídia
// da API (MEDIA_DIR, subdiretório `bens`).
func NovoImportador(pool *pgxpool.Pool, tx *db.TxManager, dirDeMidia string) *Importador {
	dir := ""
	if dirDeMidia != "" {
		dir = filepath.Join(dirDeMidia, SubdiretorioDoVolume)
	}
	return &Importador{s: NewService(NewRepository(pool), tx, dir)}
}

// ContagemDaImportacao separa o que nasceu nesta passada do que já existia.
type ContagemDaImportacao struct {
	Criados    int `json:"criados"`
	Existentes int `json:"existentes"`
}

// RelatorioDaImportacao é o resultado de uma casa. No dry-run, "criados" é
// "seriam criados" e "arquivos escritos" é "seriam escritos".
type RelatorioDaImportacao struct {
	Unidade    string               `json:"unidade"`
	DryRun     bool                 `json:"dry_run"`
	Ambientes  ContagemDaImportacao `json:"ambientes"`
	Itens      ContagemDaImportacao `json:"itens"`
	Fotos      ContagemDaImportacao `json:"fotos"`
	Ligacoes   ContagemDaImportacao `json:"ligacoes"`
	Colocacoes ContagemDaImportacao `json:"colocacoes"`
	// Pecas é a soma das quantidades do levantamento; PecasCriadas, a das
	// colocações que nasceram nesta passada.
	Pecas        int `json:"pecas"`
	PecasCriadas int `json:"pecas_criadas"`
	// Arquivos no volume (originais e miniaturas): escritos agora e os que já
	// estavam lá — inclusive órfãos de uma passada que falhou, reaproveitados.
	ArquivosEscritos int            `json:"arquivos_escritos"`
	ArquivosNoVolume int            `json:"arquivos_no_volume"`
	Mapeados         map[string]int `json:"mapeados"`
}

// Importar valida, planeja e grava a casa numa transação. Com `dryRun`, nada é
// gravado no banco (a transação é READ ONLY) nem no disco, e o relatório diz o
// que a execução faria.
func (im *Importador) Importar(ctx context.Context, lev Levantamento, fotos fs.FS, dryRun bool) (RelatorioDaImportacao, error) {
	rel := RelatorioDaImportacao{Unidade: strings.TrimSpace(lev.Unidade), DryRun: dryRun}
	plano, err := planejarImportacao(lev, fotos)
	if err != nil {
		return rel, err
	}
	unidades, err := im.s.repo.UnidadesPorCodigo(ctx, plano.Unidade)
	if err != nil {
		return rel, err
	}
	switch len(unidades) {
	case 0:
		return rel, fmt.Errorf("a unidade %s não existe no banco", plano.Unidade)
	case 1:
	default:
		return rel, fmt.Errorf("o código %s existe em %d propriedades: o importador não sabe em qual gravar", plano.Unidade, len(unidades))
	}
	if !dryRun && im.s.dir == "" {
		return rel, errors.New("MEDIA_DIR não configurado: as fotos não teriam onde ser gravadas")
	}

	err = im.s.tx.Do(ctx, func(ctx context.Context) error {
		// A transação pode ser repetida pelo TxManager (contenção): o relatório
		// recomeça junto, e os arquivos já escritos são reaproveitados.
		rel = RelatorioDaImportacao{
			Unidade: plano.Unidade, DryRun: dryRun, Pecas: plano.Pecas, Mapeados: plano.Mapeados,
		}
		e := &execucaoDaImportacao{
			s: im.s, plano: &plano, rel: &rel, fotos: fotos, dryRun: dryRun,
			prop: unidades[0].Propriedade, unidade: unidades[0].ID, simulados: map[uuid.UUID]bool{},
		}
		return e.rodar(ctx)
	})
	return rel, err
}

// execucaoDaImportacao é uma passada sobre uma casa, dentro da transação.
// `simulados` são os ids inventados pelo dry-run para o que ainda não existe:
// ligação e colocação que os citam "seriam criadas" sem perguntar ao banco.
type execucaoDaImportacao struct {
	s         *Service
	plano     *planoDaImportacao
	rel       *RelatorioDaImportacao
	fotos     fs.FS
	dryRun    bool
	prop      uuid.UUID
	unidade   uuid.UUID
	simulados map[uuid.UUID]bool
}

func (e *execucaoDaImportacao) simular() uuid.UUID {
	id := uuid.New()
	e.simulados[id] = true
	return id
}

func (e *execucaoDaImportacao) rodar(ctx context.Context) error {
	r := e.s.repo
	if e.dryRun {
		if err := r.SomenteLeitura(ctx); err != nil {
			return err
		}
	}
	// A mesma trava por unidade da cópia e da abertura de conferência: com ela,
	// "não há conferência aberta" continua verdade até o COMMIT da casa.
	if err := r.TravarInventarioDaUnidade(ctx, e.unidade); err != nil {
		return err
	}
	aberta, err := r.ConferenciaAbertaDaUnidade(ctx, e.prop, e.unidade)
	if err != nil {
		return err
	}
	if aberta != nil {
		return apperr.CountAlreadyOpen.
			WithMessage(fmt.Sprintf("A unidade %s tem uma conferência aberta: feche ou cancele antes de importar — "+
				"cômodo ou colocação nova ficaria fora das linhas congeladas.", e.plano.Unidade)).
			WithDetails(conferenciaJaAberta{ID: aberta.ID, AbertaEm: aberta.AbertaEm})
	}

	ambientes, err := e.ambientes(ctx)
	if err != nil {
		return err
	}
	itens, err := e.itens(ctx)
	if err != nil {
		return err
	}
	midias, err := e.midias(ctx)
	if err != nil {
		return err
	}
	if err := e.ligacoes(ctx, itens, midias); err != nil {
		return err
	}
	if err := e.colocacoes(ctx, ambientes, itens); err != nil {
		return err
	}
	if e.dryRun {
		return nil
	}
	// Uma entrada por casa, com os totais — a convenção da cópia entre
	// unidades (`units.inventario_copiado`). O ator é nulo: não há usuário; a
	// origem de cada item fica no próprio `source_ref`.
	return audit.Registrar(ctx, r.pool, audit.Evento{
		PropriedadeID: e.prop,
		Acao:          audit.Acao("units", "inventario_importado"),
		Entidade:      "units",
		EntidadeID:    e.unidade,
		Depois:        audit.Snapshot(e.rel),
	})
}

// ambientes casa cada cômodo do levantamento com o da unidade pelo `code` e,
// sem `code` igual, pelo `name` — a regra da cópia. Casado é reaproveitado sem
// tocar em nada; o resto nasce com o `code` DECLARADO aqui, `kind` mapeado e
// `sort_order` = `ordem`. Devolve nome no levantamento → id do cômodo.
func (e *execucaoDaImportacao) ambientes(ctx context.Context) (map[string]uuid.UUID, error) {
	destino, err := e.s.repo.AmbientesParaCopia(ctx, e.prop, e.unidade, false)
	if err != nil {
		return nil, err
	}
	porCodigo := map[string]ambienteGravado{}
	porNome := map[string]ambienteGravado{}
	for _, d := range destino {
		porCodigo[d.Codigo], porNome[d.Nome] = d, d
	}
	dono := map[uuid.UUID]string{}
	out := map[string]uuid.UUID{}
	for _, a := range e.plano.Ambientes {
		d, casou := porCodigo[a.Codigo]
		if !casou {
			d, casou = porNome[a.Nome]
		}
		if casou {
			// Dois cômodos do levantamento no MESMO cômodo da casa juntariam
			// dois inventários num só, em silêncio. É conflito de nome no
			// painel, e quem resolve é gente.
			if outro, ja := dono[d.ID]; ja {
				return nil, fmt.Errorf("os ambientes %q e %q do levantamento caem no mesmo cômodo da unidade (%q, code %s): "+
					"renomeie um deles no painel ou no levantamento", outro, a.Nome, d.Nome, d.Codigo)
			}
			dono[d.ID] = a.Nome
			out[a.Nome] = d.ID
			e.rel.Ambientes.Existentes++
			continue
		}
		if e.dryRun {
			out[a.Nome] = e.simular()
			e.rel.Ambientes.Criados++
			continue
		}
		id, criado, err := e.s.repo.CriarAmbienteImportado(ctx, e.prop, e.unidade, ambienteGravado{
			Codigo: a.Codigo, Nome: a.Nome, Tipo: a.Tipo, Ordem: a.Ordem, Ativo: true,
		})
		if err != nil {
			return nil, err
		}
		if !criado {
			// Só acontece se o painel criou o cômodo entre a leitura acima e
			// este INSERT (o POST /rooms não toma a trava da unidade). Desfazer
			// a casa e rodar de novo é o caminho seguro: a próxima passada casa.
			return nil, fmt.Errorf("o ambiente %q (code %s) acabou de ser criado na unidade por outra via; rode a importação de novo", a.Nome, a.Codigo)
		}
		out[a.Nome] = id
		e.rel.Ambientes.Criados++
	}
	return out, nil
}

// itens grava o catálogo. Item que já existe (mesma origem) não é reescrito.
func (e *execucaoDaImportacao) itens(ctx context.Context) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, len(e.plano.Itens))
	for i, it := range e.plano.Itens {
		if !e.dryRun {
			id, criado, err := e.s.repo.CriarBemImportado(ctx, e.prop, it.Bem)
			if err != nil {
				return nil, err
			}
			if criado {
				ids[i] = id
				e.rel.Itens.Criados++
				continue
			}
		}
		id, existe, err := e.s.repo.BemPorOrigem(ctx, e.prop, it.Bem.OrigemDaImportacao)
		if err != nil {
			return nil, err
		}
		switch {
		case existe:
			ids[i] = id
			e.rel.Itens.Existentes++
		case e.dryRun:
			ids[i] = e.simular()
			e.rel.Itens.Criados++
		default:
			return nil, fmt.Errorf("item %q: conflito de origem sem linha para ler", it.Bem.OrigemDaImportacao)
		}
	}
	return ids, nil
}

// midias grava UMA linha de `inventory_media` por arquivo, compartilhada por
// todos os itens que a citam. Os arquivos vão para o volume antes (só se ainda
// não estiverem lá); a linha que já existe é reaproveitada. Devolve arquivo →
// id da foto.
func (e *execucaoDaImportacao) midias(ctx context.Context) (map[string]uuid.UUID, error) {
	if !e.dryRun {
		if err := os.MkdirAll(e.s.dir, permissaoPasta); err != nil {
			return nil, apperr.Internal.WithCause(fmt.Errorf("bens: preparando %s: %w", e.s.dir, err))
		}
	}
	out := map[string]uuid.UUID{}
	for _, f := range e.plano.Fotos {
		id, dona, existe, err := e.s.repo.MidiaPorChave(ctx, f.Chave)
		if err != nil {
			return nil, err
		}
		if existe && dona != e.prop {
			return nil, fmt.Errorf("a chave de volume %s já pertence a outra propriedade", f.Chave)
		}
		if e.dryRun {
			e.contarArquivos(f)
			if existe {
				out[f.Arquivo] = id
				e.rel.Fotos.Existentes++
			} else {
				out[f.Arquivo] = e.simular()
				e.rel.Fotos.Criados++
			}
			continue
		}

		// Os arquivos são garantidos mesmo quando a linha já existe: um volume
		// restaurado pela metade volta inteiro na próxima passada.
		m, err := e.garantirArquivos(ctx, f)
		if err != nil {
			return nil, err
		}
		if existe {
			out[f.Arquivo] = id
			e.rel.Fotos.Existentes++
			continue
		}
		novo, criado, err := e.s.repo.CriarMidiaImportada(ctx, m)
		if err != nil {
			return nil, err
		}
		if !criado {
			return nil, fmt.Errorf("foto %s: conflito de chave sem linha para ler", f.Chave)
		}
		out[f.Arquivo] = novo
		e.rel.Fotos.Criados++
	}
	return out, nil
}

// contarArquivos é a parte "disco" do dry-run: só pergunta se o arquivo já
// está no volume (os.Stat), sem escrever nem criar diretório.
func (e *execucaoDaImportacao) contarArquivos(f fotoDoPlano) {
	for _, chave := range []string{f.Chave, f.Miniatura} {
		if chave == "" {
			continue
		}
		if caminho, err := e.s.caminhoNoVolume(chave); err == nil && arquivoExiste(caminho) {
			e.rel.ArquivosNoVolume++
		} else {
			e.rel.ArquivosEscritos++
		}
	}
}

// garantirArquivos escreve no volume a foto e a miniatura que ainda não
// estiverem lá, e devolve a linha a registrar — com `mime`, `bytes` e
// dimensões do arquivo GUARDADO, conferidos nele. Arquivo que já está no
// volume não é reescrito nem reconvertido (inclusive o órfão de uma passada
// que falhou). Foto nova passa pela mesma conversão do envio do painel.
func (e *execucaoDaImportacao) garantirArquivos(ctx context.Context, f fotoDoPlano) (registroDeMidia, error) {
	final, err := e.s.caminhoNoVolume(f.Chave)
	if err != nil {
		return registroDeMidia{}, err
	}
	var miniatura []byte // a gerada junto com a conversão, da imagem na memória
	if arquivoExiste(final) {
		e.rel.ArquivosNoVolume++
	} else {
		dados, mini, err := e.prepararOrigem(ctx, f)
		if err != nil {
			return registroDeMidia{}, err
		}
		if _, err := gravarNoVolume(ctx, e.s.dir, final, nil, bytes.NewReader(dados)); err != nil {
			return registroDeMidia{}, fmt.Errorf("foto %q: %w", f.Arquivo, err)
		}
		e.rel.ArquivosEscritos++
		miniatura = mini
	}

	info, err := os.Stat(final)
	if err != nil {
		return registroDeMidia{}, apperr.Internal.WithCause(err)
	}
	guardada, err := tipoDoArquivo(final)
	if err != nil {
		return registroDeMidia{}, fmt.Errorf("foto %q no volume (%s): %w", f.Arquivo, f.Chave, err)
	}
	m := registroDeMidia{
		PropriedadeID: e.prop, Mime: guardada.mime, Bytes: info.Size(),
		NomeOriginal: nomeOriginal(f.Arquivo), ChaveArquivo: f.Chave,
	}
	if d, err := medirFoto(final, guardada.mime); err != nil {
		slog.WarnContext(ctx, "bens: importação sem medir a foto", "arquivo", f.Arquivo, "err", err)
	} else {
		m.Largura, m.Altura = &d.largura, &d.altura
	}

	if f.Miniatura == "" {
		return m, nil
	}
	caminhoMini, err := e.s.caminhoNoVolume(f.Miniatura)
	if err != nil {
		return registroDeMidia{}, err
	}
	if arquivoExiste(caminhoMini) {
		e.rel.ArquivosNoVolume++
		m.ChaveMiniatura = &f.Miniatura
		return m, nil
	}
	if miniatura == nil {
		// A foto já estava no volume e a miniatura não: sai do arquivo guardado.
		if miniatura, err = gerarMiniatura(ctx, final, guardada.mime); err != nil || miniatura == nil {
			slog.WarnContext(ctx, "bens: foto importada sem miniatura; a grade usa a foto", "arquivo", f.Arquivo, "err", err)
			return m, nil
		}
	}
	if _, err := gravarNoVolume(ctx, e.s.dir, caminhoMini, nil, bytes.NewReader(miniatura)); err != nil {
		slog.WarnContext(ctx, "bens: miniatura importada não gravada; a grade usa a foto", "arquivo", f.Arquivo, "err", err)
		return m, nil
	}
	e.rel.ArquivosEscritos++
	m.ChaveMiniatura = &f.Miniatura
	return m, nil
}

// prepararOrigem lê a foto do levantamento e devolve o que vai para o volume
// e a miniatura dela. A convertível passa pela mesma prepararFoto do envio do
// painel; as exceções planejadas (WebP, grande demais) vão como vieram, com
// aviso. O tipo é conferido pelos bytes DE NOVO: o arquivo de origem pode ter
// mudado desde a validação, e foto planejada para conversão que não converte
// aborta a casa — a chave dela já diz JPEG.
func (e *execucaoDaImportacao) prepararOrigem(ctx context.Context, f fotoDoPlano) ([]byte, []byte, error) {
	ler := func() ([]byte, error) { return fs.ReadFile(e.fotos, f.Arquivo) }
	if !f.Converter {
		dados, err := ler()
		if err != nil {
			return nil, nil, fmt.Errorf("foto %q: %w", f.Arquivo, err)
		}
		if tipo, ok := detectarFoto(dados[:min(len(dados), tamanhoDaCabeca)]); !ok || tipo.mime != f.MimeDaOrigem {
			return nil, nil, fmt.Errorf("foto %q mudou desde a validação: não é mais %s pelos bytes", f.Arquivo, f.MimeDaOrigem)
		}
		motivo := errSemOrcamento // o plano só deixa de converter WebP e foto grande demais
		if f.MimeDaOrigem == mimeWebP {
			motivo = errNaoConvertivel
		}
		slog.WarnContext(ctx, "bens: foto importada como veio, sem conversão",
			"arquivo", f.Arquivo, "mime", f.MimeDaOrigem, "bytes", len(dados), "motivo", motivo)
		return dados, nil, nil
	}
	prep, err := prepararFoto(ctx, tipoDeFoto{mime: f.MimeDaOrigem}, ler)
	if err != nil {
		return nil, nil, fmt.Errorf("foto %q: %w", f.Arquivo, err)
	}
	if prep.ComoVeio {
		return nil, nil, fmt.Errorf("foto %q não pôde ser convertida (%v): corrija ou reexporte a foto e rode de novo", f.Arquivo, prep.Motivo)
	}
	if prep.Miniatura == nil {
		slog.WarnContext(ctx, "bens: foto importada sem miniatura; a grade usa a foto", "arquivo", f.Arquivo, "err", prep.Motivo)
	}
	return prep.Dados, prep.Miniatura, nil
}

// tipoDoArquivo detecta, pelos bytes, o tipo de um arquivo do volume.
func tipoDoArquivo(caminho string) (tipoDeFoto, error) {
	arq, err := os.Open(caminho)
	if err != nil {
		return tipoDeFoto{}, err
	}
	defer func() { _ = arq.Close() }()
	cabeca := make([]byte, tamanhoDaCabeca)
	n, err := io.ReadFull(arq, cabeca)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return tipoDeFoto{}, err
	}
	tipo, ok := detectarFoto(cabeca[:n])
	if !ok {
		return tipoDeFoto{}, errors.New("o arquivo no volume não é JPEG, PNG nem WebP pelos bytes")
	}
	return tipo, nil
}

// ligacoes monta as galerias: `sort_order` = posição da foto no item.
func (e *execucaoDaImportacao) ligacoes(ctx context.Context, itens []uuid.UUID, midias map[string]uuid.UUID) error {
	for i, it := range e.plano.Itens {
		for ordem, arquivo := range it.Fotos {
			bem, midia := itens[i], midias[arquivo]
			criada, err := e.gravarOuPerguntar(bem, midia,
				func() (bool, error) { return e.s.repo.LigarFotoImportada(ctx, bem, midia, ordem) },
				func() (bool, error) { return e.s.repo.LigacaoExiste(ctx, bem, midia) })
			if err != nil {
				return err
			}
			contar(&e.rel.Ligacoes, criada)
		}
	}
	return nil
}

// colocacoes põe cada bem nos cômodos com a quantidade do levantamento.
// Colocação que já existe mantém a quantidade que tem (pode ter sido ajustada
// no painel); confiança baixa vai na `note`.
func (e *execucaoDaImportacao) colocacoes(ctx context.Context, ambientes map[string]uuid.UUID, itens []uuid.UUID) error {
	for i, it := range e.plano.Itens {
		for _, c := range it.Colocacoes {
			ambiente, bem := ambientes[c.Ambiente], itens[i]
			criada, err := e.gravarOuPerguntar(ambiente, bem,
				func() (bool, error) { return e.s.repo.ColocarImportado(ctx, ambiente, bem, c.Qtd, it.Nota) },
				func() (bool, error) { return e.s.repo.ColocacaoExiste(ctx, ambiente, bem) })
			if err != nil {
				return err
			}
			contar(&e.rel.Colocacoes, criada)
			if criada {
				e.rel.PecasCriadas += c.Qtd
			}
		}
	}
	return nil
}

// gravarOuPerguntar é o ponto em que execução e dry-run se separam para um
// par (ligação ou colocação): a execução grava (`ON CONFLICT DO NOTHING`) e
// diz se criou; o dry-run diz que "criaria" quando uma das pontas ainda não
// existe e, senão, pergunta ao banco se o par já está lá.
func (e *execucaoDaImportacao) gravarOuPerguntar(a, b uuid.UUID,
	gravar func() (bool, error), existe func() (bool, error)) (bool, error) {
	if !e.dryRun {
		return gravar()
	}
	if e.simulados[a] || e.simulados[b] {
		return true, nil
	}
	ja, err := existe()
	return !ja, err
}

func contar(c *ContagemDaImportacao, criado bool) {
	if criado {
		c.Criados++
	} else {
		c.Existentes++
	}
}

func arquivoExiste(caminho string) bool {
	info, err := os.Stat(caminho)
	return err == nil && info.Mode().IsRegular()
}
