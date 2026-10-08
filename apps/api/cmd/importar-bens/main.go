// Comando importar-bens — põe o levantamento fotográfico das casas no
// inventário de bens (internal/modules/bens/importacao.go).
//
// Uso:
//
//	importar-bens --dir <pasta> [--unidade GV-01] [--dry-run]
//
// A pasta tem `consolidado/<UNIDADE>.json` (um por casa) e `fotos/` (os
// arquivos citados). Sem `--unidade`, importa todas as casas de `consolidado/`,
// cada uma na SUA transação: uma casa recusada (conferência aberta, levantamento
// com problema) não impede as outras, e o código de saída é 1 se alguma falhou.
//
// `--dry-run` não grava nada — nem no banco (transação READ ONLY) nem no disco —
// e imprime o plano: o que seria criado e o que já existe.
//
// DATABASE_URL e MEDIA_DIR vêm da mesma configuração da API
// (internal/platform/config). As fotos vão para MEDIA_DIR/bens, que precisa ser
// o MESMO volume que a API serve — no stack de desenvolvimento, o volume nomeado
// montado em /data/midia do contêiner, e não uma pasta do host. Cada foto nova
// passa pela mesma conversão do envio do painel (JPEG de até 1280 px,
// qualidade 75, sem metadados); a que já está no volume não é reescrita.
//
// Este comando só lê flags e arquivos e imprime o relatório: validação,
// mapeamento de vocabulário e gravação são do módulo.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/bens"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/config"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// codigoDeUnidade protege o `--unidade`, que vira nome de arquivo.
var codigoDeUnidade = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	dir := flag.String("dir", "", "pasta do levantamento (com consolidado/ e fotos/)")
	unidade := flag.String("unidade", "", "importa só esta casa (ex.: GV-01); sem ela, todas")
	dryRun := flag.Bool("dry-run", false, "não grava nada no banco nem no disco; imprime o plano")
	flag.Parse()

	if *dir == "" || flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "uso: importar-bens --dir <pasta> [--unidade GV-01] [--dry-run]")
		os.Exit(2)
	}
	if *unidade != "" && !codigoDeUnidade.MatchString(*unidade) {
		fmt.Fprintf(os.Stderr, "--unidade %q não é um código de unidade\n", *unidade)
		os.Exit(2)
	}

	falhas, err := executar(*dir, *unidade, *dryRun, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "importar-bens:", err)
		os.Exit(1)
	}
	if falhas > 0 {
		fmt.Fprintf(os.Stderr, "importar-bens: %d casa(s) não importada(s)\n", falhas)
		os.Exit(1)
	}
}

func executar(dir, unidade string, dryRun bool, saida io.Writer) (int, error) {
	cfg, err := config.Load()
	if err != nil {
		return 0, fmt.Errorf("configuração inválida: %w", err)
	}
	arquivos, err := levantamentos(dir, unidade)
	if err != nil {
		return 0, err
	}

	// Ctrl-C cancela o contexto: a transação da casa em curso é desfeita.
	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()
	ctx, cancelar := context.WithTimeout(ctx, 30*time.Minute)
	defer cancelar()

	pool, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return 0, err
	}
	defer pool.Close()

	im := bens.NovoImportador(pool, db.NewTxManager(pool), cfg.MediaDir)
	fotos := os.DirFS(filepath.Join(dir, "fotos"))
	if dryRun {
		_, _ = fmt.Fprintln(saida, "DRY-RUN: nada será gravado no banco nem no disco.")
	} else {
		_, _ = fmt.Fprintf(saida, "Fotos em %s\n", filepath.Join(cfg.MediaDir, bens.SubdiretorioDoVolume))
	}

	falhas := 0
	for _, caminho := range arquivos {
		rel, err := importarCasa(ctx, im, caminho, fotos, dryRun)
		if err != nil {
			falhas++
			_, _ = fmt.Fprintf(saida, "\n== %s: NÃO IMPORTADA\n%v\n", filepath.Base(caminho), err)
			continue
		}
		imprimir(saida, rel)
	}
	return falhas, nil
}

// levantamentos lista os JSON a importar, em ordem estável.
func levantamentos(dir, unidade string) ([]string, error) {
	pasta := filepath.Join(dir, "consolidado")
	if unidade != "" {
		caminho := filepath.Join(pasta, unidade+".json")
		if _, err := os.Stat(caminho); err != nil {
			return nil, fmt.Errorf("levantamento da unidade %s: %w", unidade, err)
		}
		return []string{caminho}, nil
	}
	arquivos, err := filepath.Glob(filepath.Join(pasta, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(arquivos) == 0 {
		return nil, fmt.Errorf("nenhum levantamento em %s", pasta)
	}
	sort.Strings(arquivos)
	return arquivos, nil
}

// importarCasa lê um JSON e o entrega ao importador. O arquivo `GV-01.json`
// tem de dizer `"unidade": "GV-01"`: um levantamento renomeado por engano não
// pode ir parar na casa errada.
func importarCasa(ctx context.Context, im *bens.Importador, caminho string, fotos fs.FS, dryRun bool) (bens.RelatorioDaImportacao, error) {
	f, err := os.Open(caminho)
	if err != nil {
		return bens.RelatorioDaImportacao{}, err
	}
	defer func() { _ = f.Close() }()
	lev, err := bens.DecodificarLevantamento(f)
	if err != nil {
		return bens.RelatorioDaImportacao{}, err
	}
	if esperado := strings.TrimSuffix(filepath.Base(caminho), ".json"); strings.TrimSpace(lev.Unidade) != esperado {
		return bens.RelatorioDaImportacao{}, fmt.Errorf("o arquivo %s diz ser da unidade %q", filepath.Base(caminho), lev.Unidade)
	}
	return im.Importar(ctx, lev, fotos, dryRun)
}

func imprimir(saida io.Writer, rel bens.RelatorioDaImportacao) {
	titulo := rel.Unidade
	if rel.DryRun {
		titulo += " (dry-run: \"criados\" = seriam criados)"
	}
	linhas := []struct {
		nome string
		c    bens.ContagemDaImportacao
	}{
		{"ambientes", rel.Ambientes},
		{"itens", rel.Itens},
		{"fotos", rel.Fotos},
		{"ligações foto-item", rel.Ligacoes},
		{"colocações", rel.Colocacoes},
	}
	_, _ = fmt.Fprintf(saida, "\n== %s\n%-20s %9s %11s\n", titulo, "", "criados", "existentes")
	for _, l := range linhas {
		_, _ = fmt.Fprintf(saida, "%-20s %9d %11d\n", l.nome, l.c.Criados, l.c.Existentes)
	}
	_, _ = fmt.Fprintf(saida, "%-20s %d no levantamento, %d em colocações criadas\n", "peças", rel.Pecas, rel.PecasCriadas)
	verbo := "escritos"
	if rel.DryRun {
		verbo = "a escrever"
	}
	_, _ = fmt.Fprintf(saida, "%-20s %d %s, %d já no volume (originais e miniaturas)\n",
		"arquivos", rel.ArquivosEscritos, verbo, rel.ArquivosNoVolume)
	if len(rel.Mapeados) > 0 {
		chaves := make([]string, 0, len(rel.Mapeados))
		for k := range rel.Mapeados {
			chaves = append(chaves, k)
		}
		sort.Strings(chaves)
		partes := make([]string, 0, len(chaves))
		for _, k := range chaves {
			partes = append(partes, fmt.Sprintf("%s: %d", k, rel.Mapeados[k]))
		}
		_, _ = fmt.Fprintf(saida, "%-20s %s\n", "vocabulário mapeado", strings.Join(partes, "; "))
	}
}
