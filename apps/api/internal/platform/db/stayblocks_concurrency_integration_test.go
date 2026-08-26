//go:build integration

// Teste de concorrência do overbooking — o teste que não pode faltar.
//
// A promessa do CLAUDE.md (regra 2) e do docs/spec.md §5 é que overbooking é
// impedido pelo BANCO, não por `SELECT` seguido de `INSERT`. Este arquivo é a
// prova: N goroutines disputam a mesma data e a constraint `EXCLUDE USING gist`
// de `stay_blocks` decide sozinha quem vence.
//
// Por que não há mock aqui: `EXCLUDE USING gist` não tem equivalente em memória.
// Um fake que "verifica sobreposição antes de inserir" passaria neste teste e
// perderia exatamente a corrida que o teste existe para cobrir — a janela entre
// o SELECT e o INSERT de duas requisições simultâneas.
package db

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// marcaQA carimba `stay_blocks.note` em tudo que este arquivo insere. É por ela
// que a limpeza encontra o que apagar sem tocar em dado de ninguém.
const marcaQA = "qa-overbooking"

// As oito unidades físicas da spec §2. Repetidas aqui de propósito: o teste
// monta o próprio inventário quando o seed não rodou, e um teste que depende de
// `make seed` ter sido executado antes não roda em CI limpo.
var unidadesDaCasa = []struct {
	Codigo string
	Nome   string
	Ordem  int32
}{
	{"AP-01", "Apartamento 201", 1},
	{"AP-02", "Apartamento 202", 2},
	{"AP-03", "Apartamento 203", 3},
	{"SP-01", "Suíte Piscina 1", 4},
	{"SP-02", "Suíte Piscina 2", 5},
	{"SP-03", "Suíte Piscina 3", 6},
	{"SP-04", "Suíte Piscina 4", 7},
	{"COB-01", "Cobertura", 8},
}

// abrirPool conecta no Postgres de integração. Sem DATABASE_URL o teste é
// pulado: `go test ./...` do dia a dia continua verde sem exigir banco, e o
// alvo `make test-integration` é quem garante a execução.
func abrirPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL ausente: teste de integração pulado (use `make test-integration`)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := New(ctx, url)
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// inventario é o que os cenários precisam saber sobre a casa.
type inventario struct {
	propriedade string            // uuid
	porCodigo   map[string]string // "COB-01" → uuid da unidade
	completa    []string          // uuids das unidades da Completa, ORDER BY code
}

// garantirInventario deixa a propriedade, as oito unidades e os dois produtos
// que os cenários usam. Tudo `ON CONFLICT DO NOTHING`: rodando depois do seed,
// reaproveita o que já existe e não reescreve nada.
func garantirInventario(t *testing.T, ctx context.Context, pool *pgxpool.Pool) inventario {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		INSERT INTO properties (name, slug, timezone, city, state)
		VALUES ('White House Village', 'white-house-village', 'America/Fortaleza', 'Luís Correia', 'PI')
		ON CONFLICT (slug) DO NOTHING`); err != nil {
		t.Fatalf("propriedade: %v", err)
	}

	var propriedade string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM properties WHERE slug = 'white-house-village'`).Scan(&propriedade); err != nil {
		t.Fatalf("lendo a propriedade: %v", err)
	}

	codigos := make([]string, 0, len(unidadesDaCasa))
	nomes := make([]string, 0, len(unidadesDaCasa))
	ordens := make([]int32, 0, len(unidadesDaCasa))
	for _, u := range unidadesDaCasa {
		codigos = append(codigos, u.Codigo)
		nomes = append(nomes, u.Nome)
		ordens = append(ordens, u.Ordem)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO units (property_id, code, name, sort_order)
		SELECT $1, u.code, u.nome, u.ordem
		  FROM unnest($2::text[], $3::text[], $4::int[]) AS u(code, nome, ordem)
		ON CONFLICT (property_id, code) DO NOTHING`,
		propriedade, codigos, nomes, ordens); err != nil {
		t.Fatalf("unidades: %v", err)
	}

	// A Completa consome TODAS as unidades e a Cobertura consome só a COB-01 —
	// é dessa composição que nasce a exclusividade que o cenário B exercita.
	if _, err := pool.Exec(ctx, `
		INSERT INTO unit_types (property_id, code, name, capacity, consumes, cleaning_fee_cents, sort_order)
		VALUES ($1, 'completa',  'White House Completa',  24, 'all_members', 90000, 4),
		       ($1, 'cobertura', 'White House Cobertura', 10, 'one_member',  35000, 3)
		ON CONFLICT (property_id, code) DO NOTHING`, propriedade); err != nil {
		t.Fatalf("produtos: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO unit_type_members (unit_type_id, unit_id)
		SELECT ut.id, u.id
		  FROM unit_types ut
		  JOIN units u ON u.property_id = ut.property_id
		 WHERE ut.property_id = $1
		   AND (ut.code = 'completa' OR (ut.code = 'cobertura' AND u.code = 'COB-01'))
		ON CONFLICT DO NOTHING`, propriedade); err != nil {
		t.Fatalf("composição dos produtos: %v", err)
	}

	inv := inventario{propriedade: propriedade, porCodigo: map[string]string{}}

	linhas, err := pool.Query(ctx,
		`SELECT code, id FROM units WHERE property_id = $1 ORDER BY code`, propriedade)
	if err != nil {
		t.Fatalf("lendo unidades: %v", err)
	}
	for linhas.Next() {
		var codigo, id string
		if err := linhas.Scan(&codigo, &id); err != nil {
			linhas.Close()
			t.Fatalf("lendo unidades: %v", err)
		}
		inv.porCodigo[codigo] = id
	}
	linhas.Close()
	if err := linhas.Err(); err != nil {
		t.Fatalf("lendo unidades: %v", err)
	}

	// ORDER BY u.code não é estética: vender a Completa insere oito linhas, e
	// duas vendas simultâneas que travassem as mesmas unidades em ordens
	// diferentes gerariam deadlock (40P01) em vez do 23P01 que o cliente
	// precisa ver. Ordem fixa transforma o impasse em fila.
	completa, err := pool.Query(ctx, `
		SELECT u.id
		  FROM unit_type_members m
		  JOIN unit_types ut ON ut.id = m.unit_type_id
		  JOIN units      u  ON u.id  = m.unit_id
		 WHERE ut.property_id = $1 AND ut.code = 'completa'
		 ORDER BY u.code`, propriedade)
	if err != nil {
		t.Fatalf("lendo a composição da Completa: %v", err)
	}
	for completa.Next() {
		var id string
		if err := completa.Scan(&id); err != nil {
			completa.Close()
			t.Fatalf("lendo a composição da Completa: %v", err)
		}
		inv.completa = append(inv.completa, id)
	}
	completa.Close()
	if err := completa.Err(); err != nil {
		t.Fatalf("lendo a composição da Completa: %v", err)
	}

	if len(inv.porCodigo) < len(unidadesDaCasa) {
		t.Fatalf("esperava ao menos %d unidades, achei %d", len(unidadesDaCasa), len(inv.porCodigo))
	}
	if len(inv.completa) != len(unidadesDaCasa) {
		t.Fatalf("a White House Completa deveria consumir as %d unidades, consome %d",
			len(unidadesDaCasa), len(inv.completa))
	}
	return inv
}

// ocupar insere uma ocupação. É a única escrita do arquivo: reserva, bloqueio
// de manutenção e importação de OTA são todos linhas desta tabela (db.md), então
// o teste não precisa de mais nada para representar o calendário inteiro.
func ocupar(ctx context.Context, exec DBTX, propriedade, unidade, status, entrada, saida, nota string) error {
	const q = `
		INSERT INTO stay_blocks (property_id, unit_id, source, status, period, expires_at, note)
		VALUES ($1, $2, 'reservation', $3,
		        daterange($4::date, $5::date, '[)'),
		        CASE WHEN $3 = 'hold' THEN now() + interval '48 hours' END,
		        $6)`

	_, err := exec.Exec(ctx, q, propriedade, unidade, status, entrada, saida, nota)
	return err
}

// ehConflitoDeDatas confirma que o erro é a constraint de exclusão falando, e
// não outra coisa qualquer que também falharia.
func ehConflitoDeDatas(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23P01"
}

// TestOverbookingEhImpedidoPeloBanco é o teste de regressão do CLAUDE.md §2.
//
// Os quatro cenários rodam como subtestes na ordem declarada, e a verificação
// final varre a tabela inteira: se qualquer um deles tivesse deixado passar uma
// sobreposição, ela apareceria ali.
func TestOverbookingEhImpedidoPeloBanco(t *testing.T) {
	pool := abrirPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	inv := garantirInventario(t, ctx, pool)

	// A limpeza roda depois dos subtestes e da verificação final.
	t.Cleanup(func() {
		limpeza, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(limpeza,
			`DELETE FROM stay_blocks WHERE note LIKE $1`, marcaQA+":%"); err != nil {
			t.Errorf("limpando as ocupações do teste: %v", err)
		}
	})

	t.Run("A_cinquenta_pedidos_da_mesma_unidade_no_mesmo_intervalo", func(t *testing.T) {
		cinquentaPedidosNaMesmaUnidade(t, ctx, pool, inv)
	})
	t.Run("B_cobertura_contra_completa", func(t *testing.T) {
		coberturaContraCompleta(t, ctx, pool, inv)
	})
	t.Run("C_back_to_back_e_permitido", func(t *testing.T) {
		backToBack(t, ctx, pool, inv)
	})
	t.Run("D_pre_reserva_bloqueia_e_expirada_libera", func(t *testing.T) {
		preReservaBloqueiaEExpiradaLibera(t, ctx, pool, inv)
	})

	t.Run("verificacao_final_sem_sobreposicao", func(t *testing.T) {
		semSobreposicao(t, ctx, pool)
	})
}

// Cenário A — 50 pedidos simultâneos da Cobertura para a mesma semana.
//
// Em linguagem de negócio: cinquenta pessoas clicam "reservar" no mesmo segundo
// para a única cobertura da casa. Uma leva; as outras 49 têm de ouvir "essas
// datas acabaram de ser ocupadas" (409), nunca "erro interno" (500) — é o
// critério de aceite literal do docs/spec.md §5.
//
// HISTÓRICO — o defeito que este subteste pegou e o que o consertou.
//
// Com o pool saturado (50 pedidos para 20 conexões), o Postgres devolvia
// `40P01 deadlock detected` no lugar de `23P01`: cada transação insere a própria
// entrada no índice ANTES de checar a constraint e, ao achar a entrada de outra
// transação em voo, espera por ela; duas esperas cruzadas viram nó. Três pontos
// deixavam isso chegar ao hóspede:
//
//  1. db.MapError não conhecia 40P01 e caía no default → INTERNAL / HTTP 500.
//  2. TxManager.Do repetia UMA vez, na hora, sem espera: as 49 perdedoras
//     voltavam em lockstep e colidiam de novo.
//  3. sem `lock_timeout` na sessão, cada espera só terminava quando o detector
//     de impasse acordava (`deadlock_timeout`, 1 s) — e as esperas se
//     encadeavam.
//
// Medido em Postgres 16: 49 de 49 perdedores recebendo 500 e o subteste levando
// de 83 s a 99 s, em cerca de metade das execuções. Depois do conserto (ver
// db.go: lock_timeout + repetição com jitter; errors.go: 40001/40P01/55P03 →
// DATE_CONFLICT), 30 execuções seguidas em ~2,5 s cada, sempre 1 vencedor e 49
// conflitos.
//
// A falha era intermitente porque depende de duas transações se cruzarem dentro
// da janela da constraint. Por isso o t.Logf abaixo é obrigatório: um `ok` do
// `go test` NÃO prova que a corrida foi disputada — a distribuição por código,
// impressa em toda execução, prova.
func cinquentaPedidosNaMesmaUnidade(t *testing.T, ctx context.Context, pool *pgxpool.Pool, inv inventario) {
	const (
		concorrentes = 50
		entrada      = "2031-01-10"
		saida        = "2031-01-13"
	)

	unidade := inv.porCodigo["COB-01"]

	// O pool é o de PRODUÇÃO (db.New: 20 conexões). Não é detalhe: com 50
	// pedidos para 20 conexões, parte deles abre a transação enquanto os outros
	// ainda esperam conexão, e é essa chegada escalonada que decide se a
	// constraint responde "data ocupada" ou "impasse". Um pool folgado só para o
	// teste esconderia exatamente o comportamento que a véspera de Réveillon
	// produz.
	tx := NewTxManager(pool)

	// Largada única: sem a barreira as goroutines se espalhariam no tempo e o
	// teste mediria 50 pedidos em fila — que é o que ele NÃO quer medir.
	var chegaram, tentativas atomic.Int32
	liberar := make(chan struct{})
	erros := make([]error, concorrentes)

	var wg sync.WaitGroup
	for i := range concorrentes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if chegaram.Add(1) == concorrentes {
				close(liberar)
			}
			<-liberar
			// Caminho de PRODUÇÃO, não SQL cru: TxManager.Do por fora e MapError
			// por dentro, como um service e um repositório fariam. É esse par
			// que decide se o hóspede vê 409 ou 500 — o INSERT solto mediria
			// uma coisa que a API nunca faz.
			erros[i] = tx.Do(ctx, func(ctx context.Context) error {
				tentativas.Add(1)
				return MapError(ocupar(ctx, From(ctx, pool), inv.propriedade, unidade,
					"confirmed", entrada, saida, fmt.Sprintf("%s:A:%d", marcaQA, i)))
			})
		}()
	}
	wg.Wait()

	vencedores, conflitos := 0, 0
	// Agrupa por código para o relatório sair legível: 49 linhas iguais de erro
	// escondem o número que importa, que é quantos hóspedes veriam cada coisa.
	porCodigo := map[string]int{}
	var amostra string

	for i, err := range erros {
		if err == nil {
			vencedores++
			continue
		}

		// O que o cliente veria: o code e o status que sairiam no envelope.
		traduzido := apperr.From(err)
		porCodigo[traduzido.Code]++

		if traduzido.Code == "DATE_CONFLICT" {
			conflitos++
			if traduzido.Status() != http.StatusConflict {
				t.Errorf("pedido %d: status = %d, esperado 409", i, traduzido.Status())
			}
			if !ehConflitoDeDatas(err) {
				t.Errorf("pedido %d: DATE_CONFLICT sem 23P01 por trás — a tradução veio de outro lugar", i)
			}
			continue
		}
		if amostra == "" {
			amostra = fmt.Sprintf("pedido %d → %s (HTTP %d): %v", i, traduzido.Code, traduzido.Status(), err)
		}
	}

	if vencedores != 1 {
		t.Fatalf("vencedores = %d, esperado exatamente 1 (respostas: %v)", vencedores, porCodigo)
	}
	if conflitos != concorrentes-1 {
		// A falha que este teste existe para proibir. Disputar data é situação
		// NORMAL de operação numa casa com uma única cobertura: quem perde a
		// corrida precisa ouvir "a data acabou de ser ocupada", não "erro
		// interno" — que manda o hóspede embora e acende alarme de incidente na
		// operação.
		t.Fatalf("de %d pedidos simultâneos, só %d receberiam 409 DATE_CONFLICT — respostas: %v\n"+
			"amostra: %s\n"+
			"transações executadas: %d (%d = o retry único do TxManager não bastou)\n"+
			"docs/spec.md §5 exige: exatamente uma vence, as demais 409, NENHUMA 500",
			concorrentes, conflitos, porCodigo, amostra, tentativas.Load(), tentativas.Load()-concorrentes)
	}

	// A distribuição por código sai SEMPRE (`go test -v`), não só na falha: foi
	// justamente um `ok` mudo que deixou este defeito atravessar duas revisões.
	t.Logf("50 pedidos simultâneos → vencedores=%d conflitos=%d respostas=%v transações=%d",
		vencedores, conflitos, porCodigo, tentativas.Load())

	var gravadas int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM stay_blocks
		 WHERE unit_id = $1 AND period && daterange($2::date, $3::date, '[)')
		   AND status IN ('hold','confirmed')`,
		unidade, entrada, saida).Scan(&gravadas); err != nil {
		t.Fatalf("conferindo o gravado: %v", err)
	}
	if gravadas != 1 {
		t.Fatalf("linhas ativas no intervalo = %d, esperado 1", gravadas)
	}
}

// Cenário B — a Cobertura sozinha contra a casa inteira.
//
// Em linguagem de negócio: um hóspede fecha a cobertura para o fim de semana no
// mesmo instante em que outro fecha a casa inteira para um casamento. Os dois
// não podem coexistir, e a casa inteira não pode entrar pela metade: ou entram
// as oito unidades, ou nenhuma.
func coberturaContraCompleta(t *testing.T, ctx context.Context, pool *pgxpool.Pool, inv inventario) {
	const rodadas = 10

	base := time.Date(2031, time.February, 3, 0, 0, 0, 0, time.UTC)
	cobertura := inv.porCodigo["COB-01"]

	for rodada := range rodadas {
		entrada := base.AddDate(0, 0, rodada*7).Format("2006-01-02")
		saida := base.AddDate(0, 0, rodada*7+3).Format("2006-01-02")

		largada := make(chan struct{})
		var (
			wg               sync.WaitGroup
			erroCob, erroCom error
		)

		wg.Add(2)
		go func() {
			defer wg.Done()
			<-largada
			erroCob = ocupar(ctx, pool, inv.propriedade, cobertura, "confirmed",
				entrada, saida, fmt.Sprintf("%s:B:cobertura:%d", marcaQA, rodada))
		}()
		go func() {
			defer wg.Done()
			<-largada
			// A Completa é uma transação só: oito INSERTs que entram juntos ou
			// não entram. Sem a transação, um conflito na sétima unidade
			// deixaria seis unidades bloqueadas por uma venda que não existe.
			erroCom = vender(ctx, pool, func(ctx context.Context, exec DBTX) error {
				for i, unidade := range inv.completa {
					if err := ocupar(ctx, exec, inv.propriedade, unidade, "confirmed",
						entrada, saida, fmt.Sprintf("%s:B:completa:%d:%d", marcaQA, rodada, i)); err != nil {
						return err
					}
				}
				return nil
			})
		}()
		close(largada)
		wg.Wait()

		vencedores := 0
		for nome, err := range map[string]error{"cobertura": erroCob, "completa": erroCom} {
			switch {
			case err == nil:
				vencedores++
			case ehConflitoDeDatas(err):
				if code := apperr.From(MapError(err)).Code; code != "DATE_CONFLICT" {
					t.Errorf("rodada %d, %s: code = %q, esperado DATE_CONFLICT", rodada, nome, code)
				}
			default:
				t.Errorf("rodada %d, %s: falhou por motivo diferente de sobreposição: %v", rodada, nome, err)
			}
		}
		if vencedores != 1 {
			t.Fatalf("rodada %d: vencedores = %d, esperado exatamente 1 (cobertura: %v · completa: %v)",
				rodada, vencedores, erroCob, erroCom)
		}

		// 1 linha = venceu a Cobertura · 8 = venceu a Completa. Qualquer valor
		// entre 2 e 7 seria a casa vendida pela metade — o pior desfecho
		// possível, porque a operação prepararia unidades para um evento que
		// não vai acontecer.
		var linhas int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM stay_blocks
			 WHERE property_id = $1 AND period && daterange($2::date, $3::date, '[)')
			   AND status IN ('hold','confirmed')`,
			inv.propriedade, entrada, saida).Scan(&linhas); err != nil {
			t.Fatalf("rodada %d: conferindo o gravado: %v", rodada, err)
		}
		if linhas != 1 && linhas != len(inv.completa) {
			t.Fatalf("rodada %d: %d unidades ocupadas — esperado 1 (cobertura) ou %d (completa)",
				rodada, linhas, len(inv.completa))
		}
	}
}

// Cenário C — back-to-back.
//
// Em linguagem de negócio: quem sai no dia 23 libera a unidade para quem entra
// no dia 23. É o que o `daterange` half-open `[in, out)` significa, e é
// dinheiro: recusar back-to-back esvaziaria uma noite entre cada duas estadias
// na alta temporada.
func backToBack(t *testing.T, ctx context.Context, pool *pgxpool.Pool, inv inventario) {
	unidade := inv.porCodigo["AP-01"]

	if err := ocupar(ctx, pool, inv.propriedade, unidade, "confirmed",
		"2031-03-20", "2031-03-23", marcaQA+":C:sai-dia-23"); err != nil {
		t.Fatalf("primeira estadia: %v", err)
	}

	if err := ocupar(ctx, pool, inv.propriedade, unidade, "confirmed",
		"2031-03-23", "2031-03-26", marcaQA+":C:entra-dia-23"); err != nil {
		t.Fatalf("check-in no mesmo dia do check-out anterior foi recusado: %v — "+
			"o daterange precisa ser half-open '[in, out)'", err)
	}

	// A recíproca: sobrepor uma noite (entrar no dia 22, quando o hóspede
	// anterior ainda dorme lá) continua sendo conflito.
	err := ocupar(ctx, pool, inv.propriedade, unidade, "confirmed",
		"2031-03-22", "2031-03-25", marcaQA+":C:sobrepoe-uma-noite")
	if !ehConflitoDeDatas(err) {
		t.Fatalf("sobrepor uma noite deveria dar 23P01, deu: %v", err)
	}
}

// Cenário D — pré-reserva segura a data; expirada, devolve.
//
// Em linguagem de negócio: a pré-reserva de 48 h tira a data do mercado sem
// pagamento nenhum. Quando o prazo vence e o job a marca como expirada, a data
// volta a ser vendável no mesmo instante — sem passar por lugar nenhum do
// código da aplicação.
func preReservaBloqueiaEExpiradaLibera(t *testing.T, ctx context.Context, pool *pgxpool.Pool, inv inventario) {
	const (
		entrada = "2031-04-05"
		saida   = "2031-04-08"
	)
	unidade := inv.porCodigo["SP-01"]

	if err := ocupar(ctx, pool, inv.propriedade, unidade, "hold",
		entrada, saida, marcaQA+":D:pre-reserva"); err != nil {
		t.Fatalf("pré-reserva: %v", err)
	}

	err := ocupar(ctx, pool, inv.propriedade, unidade, "confirmed",
		entrada, saida, marcaQA+":D:venda-bloqueada")
	if !ehConflitoDeDatas(err) {
		t.Fatalf("a pré-reserva deveria bloquear a data; a venda passou com: %v", err)
	}

	// O job de expiração é este UPDATE. Marcar em vez de esperar 48 h é o que
	// mantém o teste determinístico — nenhum `sleep` decide o resultado.
	tag, err := pool.Exec(ctx,
		`UPDATE stay_blocks SET status = 'expired' WHERE note = $1`, marcaQA+":D:pre-reserva")
	if err != nil {
		t.Fatalf("expirando a pré-reserva: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("linhas expiradas = %d, esperado 1", tag.RowsAffected())
	}

	if err := ocupar(ctx, pool, inv.propriedade, unidade, "confirmed",
		entrada, saida, marcaQA+":D:venda-liberada"); err != nil {
		t.Fatalf("com a pré-reserva expirada a data deveria estar livre, mas: %v", err)
	}
}

// semSobreposicao é a pergunta que fecha o arquivo: existe, em qualquer lugar da
// tabela, alguma unidade ocupada duas vezes ao mesmo tempo?
func semSobreposicao(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	const q = `
		SELECT a.unit_id::text, a.period::text, b.period::text
		  FROM stay_blocks a
		  JOIN stay_blocks b
		    ON b.unit_id = a.unit_id
		   AND b.id > a.id
		   AND b.period && a.period
		 WHERE a.status IN ('hold','confirmed')
		   AND b.status IN ('hold','confirmed')`

	linhas, err := pool.Query(ctx, q)
	if err != nil {
		t.Fatalf("consulta de verificação: %v", err)
	}
	defer linhas.Close()

	sobreposicoes := 0
	for linhas.Next() {
		var unidade, a, b string
		if err := linhas.Scan(&unidade, &a, &b); err != nil {
			t.Fatalf("consulta de verificação: %v", err)
		}
		sobreposicoes++
		t.Errorf("unidade %s ocupada duas vezes: %s e %s", unidade, a, b)
	}
	if err := linhas.Err(); err != nil {
		t.Fatalf("consulta de verificação: %v", err)
	}
	if sobreposicoes > 0 {
		t.Fatalf("%d sobreposições no calendário — a constraint EXCLUDE não está protegendo", sobreposicoes)
	}
}

// vender roda fn dentro de uma transação. Não usa o TxManager de propósito: ele
// repete a transação em 40001/40P01, e aqui o teste precisa ver o erro cru que a
// primeira tentativa produziu.
func vender(ctx context.Context, pool *pgxpool.Pool, fn func(context.Context, DBTX) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return err
	}
	return tx.Commit(ctx)
}
