//go:build integration

package bens_test

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/bens"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Importador do levantamento fotográfico, contra Postgres real, com fixture
// SINTÉTICA gerada aqui: dois ambientes, três itens, três fotos (uma delas
// compartilhada por dois itens). Nada da casa real entra no repositório.

type cenarioDeImportacao struct {
	a       *ambiente
	unidade uuid.UUID
	sufixo  string
	lev     bens.Levantamento
	fotos   fstest.MapFS
	midia   string
	im      *bens.Importador
}

func (a *ambiente) cenarioDeImportacao(t *testing.T) *cenarioDeImportacao {
	t.Helper()
	unidade, codigo := a.unidade(t)
	s := sufixo()
	origem := "it-import-" + s + ":"
	a.mu.Lock()
	a.origensImportadas = append(a.origensImportadas, origem)
	a.chavesImportadas = append(a.chavesImportadas, "importacao-chatwoot-it"+s+"-")
	a.mu.Unlock()

	foto := func(letra string) string { return "it_" + s + "_" + letra + ".jpg" }
	daFoto := func(n int) string { return fmt.Sprintf("chatwoot:it%s:%d", s, n) }
	fotos := fstest.MapFS{
		foto("a"): {Data: jpegDeTeste(t, 640, 480)},
		foto("b"): {Data: jpegDeTeste(t, 320, 240)},
		foto("c"): {Data: jpegDeTeste(t, 200, 200)},
	}
	lev := bens.Levantamento{
		Unidade: codigo,
		Ambientes: []bens.AmbienteLevantado{
			{Nome: "Sala de estar", Tipo: "sala", Ordem: 2},
			{Nome: "Área externa", Tipo: "externa", Ordem: 1},
		},
		Itens: []bens.ItemLevantado{
			{
				SourceRef: origem + "taca", Nome: "Taça de vinho", Descricao: "Cristal fino", Categoria: "copo_taca", Confianca: "alta",
				Fotos:     []bens.FotoLevantada{{Arquivo: foto("a"), SourceRef: daFoto(1)}},
				Ambientes: map[string]bens.ColocacaoLevantada{"Sala de estar": {Quantidade: 6}},
			},
			{
				SourceRef: origem + "vaso", Nome: "Vaso de barro", Categoria: "externo", Confianca: "baixa",
				Fotos:     []bens.FotoLevantada{{Arquivo: foto("a"), SourceRef: daFoto(1)}, {Arquivo: foto("b"), SourceRef: daFoto(2)}},
				Ambientes: map[string]bens.ColocacaoLevantada{"Área externa": {Quantidade: 2}},
			},
			{
				SourceRef: origem + "prato", Nome: "Prato raso", Categoria: "louca", Confianca: "media",
				Fotos:     []bens.FotoLevantada{{Arquivo: foto("c"), SourceRef: daFoto(3)}},
				Ambientes: map[string]bens.ColocacaoLevantada{"Sala de estar": {Quantidade: 4}, "Área externa": {Quantidade: 1}},
			},
		},
	}
	midia := t.TempDir()
	return &cenarioDeImportacao{
		a: a, unidade: unidade, sufixo: s, lev: lev, fotos: fotos, midia: midia,
		im: bens.NovoImportador(a.pool, db.NewTxManager(a.pool), midia),
	}
}

func (c *cenarioDeImportacao) importar(t *testing.T, dryRun bool) bens.RelatorioDaImportacao {
	t.Helper()
	rel, err := c.im.Importar(c.a.ctx, c.lev, c.fotos, dryRun)
	if err != nil {
		t.Fatalf("importando (dry-run=%v): %v", dryRun, err)
	}
	return rel
}

func (c *cenarioDeImportacao) chave(n int) string {
	return fmt.Sprintf("importacao-chatwoot-it%s-%d.jpg", c.sufixo, n)
}

// noBanco conta o que a casa tem no banco agora.
type retratoDoBanco struct{ Comodos, Itens, Fotos, Ligacoes, Colocacoes int }

func (c *cenarioDeImportacao) noBanco(t *testing.T) retratoDoBanco {
	t.Helper()
	var r retratoDoBanco
	origem := "it-import-" + c.sufixo + ":%"
	chave := "importacao-chatwoot-it" + c.sufixo + "-%"
	if err := c.a.pool.QueryRow(c.a.ctx, `
		SELECT (SELECT count(*) FROM unit_rooms WHERE unit_id = $1),
		       (SELECT count(*) FROM inventory_items WHERE source_ref LIKE $2),
		       (SELECT count(*) FROM inventory_media WHERE storage_key LIKE $3),
		       (SELECT count(*) FROM inventory_item_media im JOIN inventory_media m ON m.id = im.media_id
		         WHERE m.storage_key LIKE $3),
		       (SELECT count(*) FROM room_inventory ri JOIN unit_rooms r ON r.id = ri.room_id WHERE r.unit_id = $1)`,
		c.unidade, origem, chave).Scan(&r.Comodos, &r.Itens, &r.Fotos, &r.Ligacoes, &r.Colocacoes); err != nil {
		t.Fatal(err)
	}
	return r
}

func (c *cenarioDeImportacao) arquivosNoVolume(t *testing.T) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(c.midia, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (c *cenarioDeImportacao) idDoItem(t *testing.T, nome string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := c.a.pool.QueryRow(c.a.ctx, `SELECT id FROM inventory_items WHERE source_ref = $1`,
		"it-import-"+c.sufixo+":"+nome).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (c *cenarioDeImportacao) idDoComodo(t *testing.T, codigo string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := c.a.pool.QueryRow(c.a.ctx, `SELECT id FROM unit_rooms WHERE unit_id = $1 AND code = $2`,
		c.unidade, codigo).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func exigirContagem(t *testing.T, nome string, got bens.ContagemDaImportacao, criados, existentes int) {
	t.Helper()
	if got.Criados != criados || got.Existentes != existentes {
		t.Errorf("%s: %d criados e %d existentes, esperado %d e %d", nome, got.Criados, got.Existentes, criados, existentes)
	}
}

// Rodar duas vezes: a segunda cria ZERO. Entre as duas, o gestor renomeia um
// cômodo, corrige um item e ajusta uma quantidade no painel — e a reimportação
// não desfaz nada disso nem cria cômodo fantasma.
func TestImportacaoRepetivelSoAcrescenta(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	c := a.cenarioDeImportacao(t)

	rel := c.importar(t, false)
	exigirContagem(t, "ambientes", rel.Ambientes, 2, 0)
	exigirContagem(t, "itens", rel.Itens, 3, 0)
	exigirContagem(t, "fotos", rel.Fotos, 3, 0)
	exigirContagem(t, "ligações", rel.Ligacoes, 4, 0)
	exigirContagem(t, "colocações", rel.Colocacoes, 4, 0)
	if rel.Pecas != 13 || rel.PecasCriadas != 13 || rel.ArquivosEscritos != 6 || rel.ArquivosNoVolume != 0 {
		t.Fatalf("primeira passada: %+v", rel)
	}

	// Cômodos: `code` declarado pelo levantamento, `kind` mapeado, ordem.
	linhas, err := a.pool.Query(a.ctx, `SELECT code, name, kind, sort_order, active FROM unit_rooms WHERE unit_id = $1 ORDER BY sort_order`, c.unidade)
	if err != nil {
		t.Fatal(err)
	}
	var comodos []string
	for linhas.Next() {
		var codigo, nome, tipo string
		var ordem int
		var ativo bool
		if err := linhas.Scan(&codigo, &nome, &tipo, &ordem, &ativo); err != nil {
			t.Fatal(err)
		}
		comodos = append(comodos, fmt.Sprintf("%s|%s|%s|%d|%v", codigo, nome, tipo, ordem, ativo))
	}
	linhas.Close()
	if fmt.Sprint(comodos) != "[area-externa|Área externa|area_externa|1|true sala-de-estar|Sala de estar|sala|2|true]" {
		t.Fatalf("cômodos: %v", comodos)
	}

	// Itens: categoria mapeada, `un`, custo não cotado, descrição.
	var categoria, medida string
	var custoNulo bool
	var descricao *string
	for nome, esperada := range map[string]string{"taca": "copo", "vaso": "decoracao", "prato": "louca"} {
		if err := a.pool.QueryRow(a.ctx, `SELECT category, unit_measure, replacement_cost_cents IS NULL, description
		                                    FROM inventory_items WHERE id = $1`, c.idDoItem(t, nome)).
			Scan(&categoria, &medida, &custoNulo, &descricao); err != nil {
			t.Fatal(err)
		}
		if categoria != esperada || medida != "un" || !custoNulo {
			t.Errorf("item %s: categoria %s, medida %s, custo nulo %v", nome, categoria, medida, custoNulo)
		}
	}

	// A foto compartilhada: UMA linha de mídia, DUAS ligações; sem autor; com
	// miniatura; e a galeria do vaso na ordem do levantamento.
	var ligacoes, semAutor, comMiniatura int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT (SELECT count(*) FROM inventory_item_media im JOIN inventory_media m ON m.id = im.media_id WHERE m.storage_key = $1),
		       (SELECT count(*) FROM inventory_media WHERE storage_key LIKE $2 AND created_by IS NULL),
		       (SELECT count(thumb_key) FROM inventory_media WHERE storage_key LIKE $2)`,
		c.chave(1), "importacao-chatwoot-it"+c.sufixo+"-%").Scan(&ligacoes, &semAutor, &comMiniatura); err != nil {
		t.Fatal(err)
	}
	if ligacoes != 2 || semAutor != 3 || comMiniatura != 3 {
		t.Fatalf("foto compartilhada %d ligações (esperado 2); sem autor %d/3; miniatura %d/3", ligacoes, semAutor, comMiniatura)
	}
	galeria, err := a.pool.Query(a.ctx, `
		SELECT m.storage_key, im.sort_order FROM inventory_item_media im JOIN inventory_media m ON m.id = im.media_id
		 WHERE im.item_id = $1 ORDER BY im.sort_order`, c.idDoItem(t, "vaso"))
	if err != nil {
		t.Fatal(err)
	}
	var ordem []string
	for galeria.Next() {
		var chave string
		var n int
		if err := galeria.Scan(&chave, &n); err != nil {
			t.Fatal(err)
		}
		ordem = append(ordem, fmt.Sprintf("%s#%d", chave, n))
	}
	galeria.Close()
	if fmt.Sprint(ordem) != fmt.Sprintf("[%s#0 %s#1]", c.chave(1), c.chave(2)) {
		t.Fatalf("galeria do vaso: %v", ordem)
	}

	// Confiança baixa vai na nota da colocação; as outras ficam sem nota.
	var notaDoVaso, notaDoPrato *string
	if err := a.pool.QueryRow(a.ctx, `
		SELECT (SELECT note FROM room_inventory WHERE item_id = $1),
		       (SELECT note FROM room_inventory WHERE item_id = $2 LIMIT 1)`,
		c.idDoItem(t, "vaso"), c.idDoItem(t, "prato")).Scan(&notaDoVaso, &notaDoPrato); err != nil {
		t.Fatal(err)
	}
	if notaDoVaso == nil || *notaDoVaso != "Identificado pela foto do levantamento com confiança baixa — confira na próxima contagem." || notaDoPrato != nil {
		t.Fatalf("nota do vaso %v; nota do prato %v", notaDoVaso, notaDoPrato)
	}
	if n := c.arquivosNoVolume(t); n != 6 {
		t.Fatalf("o volume deveria ter 3 originais e 3 miniaturas, tem %d arquivos", n)
	}

	// O gestor mexe no painel.
	sala := c.idDoComodo(t, "sala-de-estar")
	exigir(t, a.chamar(t, http.MethodPatch, "/rooms/"+sala.String(), g, map[string]any{"name": "Living"}), http.StatusOK, "renomeando")
	prato := c.idDoItem(t, "prato")
	exigir(t, a.chamar(t, http.MethodPatch, "/inventory/items/"+prato.String(), g,
		map[string]any{"name": "Prato fundo", "category": "outro"}), http.StatusOK, "corrigindo o item")
	taca := c.idDoItem(t, "taca")
	exigir(t, a.chamar(t, http.MethodPatch, "/inventory/placements/"+sala.String()+"_"+taca.String(), g,
		map[string]any{"expected_qty": 9}), http.StatusOK, "ajustando a quantidade")

	segunda := c.importar(t, false)
	exigirContagem(t, "ambientes (2ª)", segunda.Ambientes, 0, 2)
	exigirContagem(t, "itens (2ª)", segunda.Itens, 0, 3)
	exigirContagem(t, "fotos (2ª)", segunda.Fotos, 0, 3)
	exigirContagem(t, "ligações (2ª)", segunda.Ligacoes, 0, 4)
	exigirContagem(t, "colocações (2ª)", segunda.Colocacoes, 0, 4)
	if segunda.PecasCriadas != 0 || segunda.ArquivosEscritos != 0 || segunda.ArquivosNoVolume != 6 {
		t.Fatalf("a segunda passada não escreve nada: %+v", segunda)
	}

	if got := c.noBanco(t); got != (retratoDoBanco{Comodos: 2, Itens: 3, Fotos: 3, Ligacoes: 4, Colocacoes: 4}) {
		t.Fatalf("a reimportação mudou o banco: %+v", got)
	}
	var nomeDaSala, nomeDoPrato, categoriaDoPrato string
	var qtd int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT (SELECT name FROM unit_rooms WHERE id = $1), (SELECT name FROM inventory_items WHERE id = $2),
		       (SELECT category FROM inventory_items WHERE id = $2),
		       (SELECT expected_qty FROM room_inventory WHERE room_id = $1 AND item_id = $3)`,
		sala, prato, taca).Scan(&nomeDaSala, &nomeDoPrato, &categoriaDoPrato, &qtd); err != nil {
		t.Fatal(err)
	}
	if nomeDaSala != "Living" || nomeDoPrato != "Prato fundo" || categoriaDoPrato != "outro" || qtd != 9 {
		t.Fatalf("o que o gestor editou foi sobrescrito: sala %q, prato %q/%s, quantidade %d", nomeDaSala, nomeDoPrato, categoriaDoPrato, qtd)
	}

	// Uma entrada de auditoria por casa importada, sem ator.
	var trilha int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM audit_log WHERE action = 'units.inventario_importado' AND entity_id = $1 AND actor_id IS NULL`,
		c.unidade).Scan(&trilha); err != nil {
		t.Fatal(err)
	}
	if trilha != 2 {
		t.Fatalf("uma entrada de auditoria por passada, esperado 2, veio %d", trilha)
	}
}

// O dry-run não grava nada — nem no banco nem no disco — e prevê exatamente o
// que a execução faz.
func TestImportacaoDryRunNaoGravaNadaEPreveAExecucao(t *testing.T) {
	a := subir(t)
	c := a.cenarioDeImportacao(t)

	previsto := c.importar(t, true)
	if !previsto.DryRun || previsto.Itens.Criados != 3 || previsto.ArquivosEscritos != 6 {
		t.Fatalf("plano do dry-run: %+v", previsto)
	}
	if got := c.noBanco(t); got != (retratoDoBanco{}) {
		t.Fatalf("o dry-run gravou no banco: %+v", got)
	}
	if n := c.arquivosNoVolume(t); n != 0 {
		t.Fatalf("o dry-run escreveu %d arquivo(s) no disco", n)
	}
	if _, err := os.Stat(filepath.Join(c.midia, bens.SubdiretorioDoVolume)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("o dry-run não cria nem a pasta do volume: %v", err)
	}

	feito := c.importar(t, false)
	previsto.DryRun = false
	if fmt.Sprintf("%+v", previsto) != fmt.Sprintf("%+v", feito) {
		t.Fatalf("o dry-run previu outra coisa:\n previsto %+v\n feito    %+v", previsto, feito)
	}

	depois := c.importar(t, true)
	if depois.Ambientes.Criados+depois.Itens.Criados+depois.Fotos.Criados+depois.Ligacoes.Criados+
		depois.Colocacoes.Criados+depois.ArquivosEscritos != 0 {
		t.Fatalf("o dry-run depois da importação deveria prever zero criados: %+v", depois)
	}
}

// Conferência ABERTA: a casa é recusada inteira, sem gravar nada — no banco ou
// no disco. Cômodo ou colocação nova ficaria fora das linhas congeladas.
func TestImportacaoRecusaCasaComConferenciaAberta(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	c := a.cenarioDeImportacao(t)
	deposito := a.comodo(t, g, c.unidade, "Depósito", "outro", 9)
	balde := a.bem(t, g, "Balde", nil)
	a.colocar(t, g, deposito.ID, balde.ID, 1)
	conf := a.abrir(t, g, c.unidade)

	for _, dryRun := range []bool{true, false} {
		_, err := c.im.Importar(a.ctx, c.lev, c.fotos, dryRun)
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != apperr.CodeCountAlreadyOpen {
			t.Fatalf("dry-run=%v: esperado COUNT_ALREADY_OPEN, veio %v", dryRun, err)
		}
	}
	if got := c.noBanco(t); got != (retratoDoBanco{Comodos: 1, Colocacoes: 1}) {
		t.Fatalf("a casa recusada gravou: %+v", got)
	}
	if n := c.arquivosNoVolume(t); n != 0 {
		t.Fatalf("a casa recusada escreveu %d arquivo(s)", n)
	}

	// Cancelada a conferência, a mesma casa entra.
	exigir(t, a.chamar(t, http.MethodDelete, "/inventory/counts/"+conf.ID.String(), g, nil), http.StatusNoContent, "cancelando")
	if rel := c.importar(t, false); rel.Ambientes.Criados != 2 || rel.Itens.Criados != 3 {
		t.Fatalf("depois de cancelar, importa: %+v", rel)
	}
}

// Categoria fora do mapa aborta a casa ANTES de gravar qualquer coisa.
func TestImportacaoAbortaCategoriaForaDoMapa(t *testing.T) {
	a := subir(t)
	c := a.cenarioDeImportacao(t)
	c.lev.Itens[2].Categoria = "piscina"
	c.lev.Ambientes[0].Tipo = "terraco"

	_, err := c.im.Importar(a.ctx, c.lev, c.fotos, false)
	var e *bens.ErroDeLevantamento
	if !errors.As(err, &e) || len(e.Problemas) != 2 {
		t.Fatalf("esperado ErroDeLevantamento com os 2 problemas, veio %v", err)
	}
	if got := c.noBanco(t); got != (retratoDoBanco{}) {
		t.Fatalf("o levantamento recusado gravou: %+v", got)
	}
	if n := c.arquivosNoVolume(t); n != 0 {
		t.Fatalf("o levantamento recusado escreveu %d arquivo(s)", n)
	}
}

// Cômodo criado no painel ANTES da importação, com o mesmo nome e outro
// `code`, é reaproveitado (casa pelo `name`, a regra da cópia) — e não ganha
// um gêmeo.
func TestImportacaoCasaPeloNomeQuandoOCodigoDifere(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	c := a.cenarioDeImportacao(t)
	r := a.chamar(t, http.MethodPost, "/rooms", g, map[string]any{
		"unit_id": c.unidade, "name": "Área externa", "kind": "varanda", "code": "quintal", "sort_order": 7,
	})
	exigir(t, r, http.StatusCreated, "criando o cômodo no painel")
	quintal := dado[ambienteResp](t, r)

	rel := c.importar(t, false)
	exigirContagem(t, "ambientes", rel.Ambientes, 1, 1)
	if got := c.noBanco(t); got.Comodos != 2 {
		t.Fatalf("a unidade deveria ter 2 cômodos (o do painel e a sala), tem %d", got.Comodos)
	}
	var tipo string
	var ordem, colocacoes int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT kind, sort_order, (SELECT count(*) FROM room_inventory WHERE room_id = $1) FROM unit_rooms WHERE id = $1`,
		quintal.ID).Scan(&tipo, &ordem, &colocacoes); err != nil {
		t.Fatal(err)
	}
	if tipo != "varanda" || ordem != 7 || colocacoes != 2 {
		t.Fatalf("o cômodo do painel é reaproveitado sem ser reescrito e recebe as colocações: kind %s, ordem %d, %d colocações", tipo, ordem, colocacoes)
	}
}

// Uma transação por casa: a falha no meio desfaz o banco inteiro. Os arquivos
// escritos antes da falha ficam no volume (órfãos, de nome determinístico), e
// a reexecução os reaproveita em vez de escrever de novo.
func TestImportacaoFalhaNoMeioDesfazOBancoEReaproveitaOrfao(t *testing.T) {
	a := subir(t)
	c := a.cenarioDeImportacao(t)
	outraCasa := a.outraPropriedade(t)

	// A terceira foto (c.jpg) já tem a chave dela na outra casa: a importação
	// passa pelos cômodos, itens e duas fotos e falha na terceira.
	var intrusa uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO inventory_media (property_id, mime, bytes, original_name, storage_key)
		VALUES ($1, 'image/jpeg', 1, 'x.jpg', $2) RETURNING id`, outraCasa, c.chave(3)).Scan(&intrusa); err != nil {
		t.Fatal(err)
	}
	if _, err := c.im.Importar(a.ctx, c.lev, c.fotos, false); err == nil {
		t.Fatal("a chave de outra propriedade deveria abortar a casa")
	}
	if got := c.noBanco(t); got.Comodos != 0 || got.Itens != 0 || got.Ligacoes != 0 || got.Colocacoes != 0 {
		t.Fatalf("a falha no meio deixou a casa pela metade no banco: %+v", got)
	}
	orfaos := c.arquivosNoVolume(t)
	if orfaos != 4 {
		t.Fatalf("esperados 4 órfãos (2 originais e 2 miniaturas escritos antes da falha), há %d", orfaos)
	}

	if _, err := a.pool.Exec(a.ctx, `DELETE FROM inventory_media WHERE id = $1`, intrusa); err != nil {
		t.Fatal(err)
	}
	rel := c.importar(t, false)
	if rel.ArquivosNoVolume != 4 || rel.ArquivosEscritos != 2 || rel.Fotos.Criados != 3 {
		t.Fatalf("a reexecução reaproveita os órfãos: %+v", rel)
	}
	if n := c.arquivosNoVolume(t); n != 6 {
		t.Fatalf("o volume termina com 6 arquivos, tem %d", n)
	}
}

// trocarFoto põe outro conteúdo, com outra extensão, no lugar de uma foto do
// cenário — e nas referências do levantamento.
func (c *cenarioDeImportacao) trocarFoto(letra, ext string, dados []byte) string {
	velho := "it_" + c.sufixo + "_" + letra + ".jpg"
	novo := "it_" + c.sufixo + "_" + letra + "." + ext
	delete(c.fotos, velho)
	c.fotos[novo] = &fstest.MapFile{Data: dados}
	for i := range c.lev.Itens {
		for j := range c.lev.Itens[i].Fotos {
			if c.lev.Itens[i].Fotos[j].Arquivo == velho {
				c.lev.Itens[i].Fotos[j].Arquivo = novo
			}
		}
	}
	return novo
}

// A importação passa pela MESMA conversão do envio do painel: a foto grande
// vai a 1280 px, o PNG vira JPEG (e a chave, `.jpg`), a WebP vai como veio
// (chave `.webp`, sem miniatura). O registro descreve o arquivo guardado. E o
// que já está no volume não é reescrito nem reconvertido, mesmo que a origem
// mude.
func TestImportacaoConverteAsFotosComoOEnvio(t *testing.T) {
	a := subir(t)
	c := a.cenarioDeImportacao(t)

	grande := fotoDeCelular(t, 2400, 1800)
	c.trocarFoto("a", "jpg", grande)
	transparente := image.NewNRGBA(image.Rect(0, 0, 640, 480))
	for y := range 480 {
		for x := 320; x < 640; x++ {
			transparente.SetNRGBA(x, y, color.NRGBA{0, 0, 200, 255})
		}
	}
	var comAlfa bytes.Buffer
	if err := png.Encode(&comAlfa, transparente); err != nil {
		t.Fatal(err)
	}
	c.trocarFoto("b", "png", comAlfa.Bytes())
	webp := webp1x1(t)
	c.trocarFoto("c", "webp", webp)
	chaveDaWebP := fmt.Sprintf("importacao-chatwoot-it%s-3.webp", c.sufixo)

	if previsto := c.importar(t, true); previsto.ArquivosEscritos != 5 {
		t.Fatalf("o dry-run prevê 5 arquivos (2 fotos com miniatura e a WebP sem): %+v", previsto)
	}
	rel := c.importar(t, false)
	if rel.ArquivosEscritos != 5 || rel.ArquivosNoVolume != 0 || rel.Fotos.Criados != 3 {
		t.Fatalf("primeira passada: %+v", rel)
	}

	noVolume := func(chave string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(c.midia, bens.SubdiretorioDoVolume, chave))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, quer := range []struct {
		chave, mime string
		w, h        int
		miniatura   bool
	}{
		{c.chave(1), "image/jpeg", 1280, 960, true},
		{c.chave(2), "image/jpeg", 640, 480, true},
		{chaveDaWebP, "image/webp", 1, 1, false},
	} {
		var mime string
		var tamanho int64
		var w, h int
		var temMiniatura bool
		if err := a.pool.QueryRow(a.ctx, `SELECT mime, bytes, width, height, thumb_key IS NOT NULL FROM inventory_media WHERE storage_key = $1`,
			quer.chave).Scan(&mime, &tamanho, &w, &h, &temMiniatura); err != nil {
			t.Fatalf("%s: %v", quer.chave, err)
		}
		if mime != quer.mime || w != quer.w || h != quer.h || temMiniatura != quer.miniatura {
			t.Fatalf("%s: %s %d×%d miniatura=%v; esperado %s %d×%d miniatura=%v", quer.chave, mime, w, h, temMiniatura, quer.mime, quer.w, quer.h, quer.miniatura)
		}
		if guardado := noVolume(quer.chave); int64(len(guardado)) != tamanho {
			t.Fatalf("%s: o registro diz %d bytes, o volume tem %d", quer.chave, tamanho, len(guardado))
		}
	}
	if n := len(noVolume(c.chave(1))); n >= len(grande) {
		t.Fatalf("a foto grande guardada (%d bytes) tem de ser menor que a origem (%d)", n, len(grande))
	}
	daPNG, err := jpeg.Decode(bytes.NewReader(noVolume(c.chave(2))))
	if err != nil {
		t.Fatalf("o PNG é guardado como JPEG: %v", err)
	}
	if vr, vg, vb, _ := daPNG.At(100, 240).RGBA(); vr>>8 < 245 || vg>>8 < 245 || vb>>8 < 245 {
		t.Fatalf("o transparente do PNG vira branco, virou (%d,%d,%d)", vr>>8, vg>>8, vb>>8)
	}
	if !bytes.Equal(noVolume(chaveDaWebP), webp) {
		t.Fatal("a WebP vai como veio")
	}

	// A origem da foto grande muda; o volume já tem a dela e não reconverte.
	antes := noVolume(c.chave(1))
	c.fotos["it_"+c.sufixo+"_a.jpg"] = &fstest.MapFile{Data: fotoDeCelular(t, 3000, 2000)}
	segunda := c.importar(t, false)
	if segunda.ArquivosEscritos != 0 || segunda.ArquivosNoVolume != 5 {
		t.Fatalf("a segunda passada não escreve nada: %+v", segunda)
	}
	if !bytes.Equal(noVolume(c.chave(1)), antes) {
		t.Fatal("o arquivo que já estava no volume foi reescrito")
	}
}
