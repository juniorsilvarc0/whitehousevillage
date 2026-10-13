//go:build integration

// A Fotografia do TxManager: todas as leituras de uma tela no MESMO instante do
// banco, e nenhuma escrita.
//
// O defeito que ela fecha foi medido pelo QA da rooming list (13/10/2026): o
// `/full` da reserva lia a reserva e a lista de hóspedes em instruções
// separadas, em READ COMMITTED, e uma troca de titular comitada entre as duas
// leituras punha "Ana" no cabeçalho e "Beto" como titular na lista da MESMA
// resposta. Aqui a garantia é conferida na primitiva, sem a reserva no meio: um
// valor comitado por outra conexão entre duas leituras da fotografia não
// aparece na segunda — e aparece, no mesmo roteiro, dentro de um Do.
package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// marcadorDaFotografia grava linhas em `pii_access_log` com um `contact_id`
// sorteado — tabela sem FK de saída nessa coluna, e o id não existe em lugar
// nenhum: nenhum outro teste conta essas linhas, e a limpeza as encontra.
type marcadorDaFotografia struct {
	pool *pgxpool.Pool
	id   uuid.UUID
}

func novoMarcador(t *testing.T, pool *pgxpool.Pool) marcadorDaFotografia {
	t.Helper()
	m := marcadorDaFotografia{pool: pool, id: uuid.New()}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM pii_access_log WHERE contact_id = $1`, m.id); err != nil {
			t.Errorf("limpando o marcador: %v", err)
		}
	})
	return m
}

// gravarFora grava uma linha numa conexão PRÓPRIA, em autocommit — é a escrita
// concorrente que comita no meio da leitura.
func (m marcadorDaFotografia) gravarFora(t *testing.T) {
	t.Helper()
	if _, err := m.pool.Exec(context.Background(),
		`INSERT INTO pii_access_log (contact_id, reason) VALUES ($1, 'qa-fotografia')`, m.id); err != nil {
		t.Fatalf("gravando fora da transação: %v", err)
	}
}

func (m marcadorDaFotografia) contar(ctx context.Context) (int, error) {
	var n int
	err := From(ctx, m.pool).QueryRow(ctx, `SELECT count(*) FROM pii_access_log WHERE contact_id = $1`, m.id).Scan(&n)
	return n, err
}

// lerAntesEDepois roda o roteiro da meia tela dentro de `abrir`: lê, deixa
// outra conexão comitar uma linha, lê de novo. Devolve as duas contagens.
func lerAntesEDepois(t *testing.T, m marcadorDaFotografia, abrir func(context.Context, func(context.Context) error) error) (int, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var antes, depois int
	err := abrir(ctx, func(ctx context.Context) error {
		var err error
		if antes, err = m.contar(ctx); err != nil {
			return err
		}
		m.gravarFora(t)
		depois, err = m.contar(ctx)
		return err
	})
	if err != nil {
		t.Fatalf("transação: %v", err)
	}
	return antes, depois
}

func TestFotografiaLeTudoNoMesmoInstante(t *testing.T) {
	pool := abrirPool(t)
	tx := NewTxManager(pool)

	// O contraste, para o teste não provar nada por acaso: em Do (READ
	// COMMITTED), a segunda leitura enxerga o que comitou no meio.
	m := novoMarcador(t, pool)
	antes, depois := lerAntesEDepois(t, m, tx.Do)
	if antes != 0 || depois != 1 {
		t.Fatalf("premissa: em READ COMMITTED a segunda leitura deveria ver a linha nova (%d → %d)", antes, depois)
	}

	// Na fotografia, a segunda leitura é do mesmo instante da primeira.
	m = novoMarcador(t, pool)
	antes, depois = lerAntesEDepois(t, m, tx.Fotografia)
	if antes != 0 || depois != 0 {
		t.Fatalf("a fotografia enxergou a escrita comitada no meio dela: %d → %d", antes, depois)
	}
	// E a linha existe: quem lê DEPOIS da fotografia a vê.
	if n, err := m.contar(context.Background()); err != nil || n != 1 {
		t.Fatalf("depois da fotografia: %d linha(s), err %v", n, err)
	}
}

// Fotografia não escreve: o banco recusa a escrita (25006
// read_only_sql_transaction) — inclusive a trava de linha, que é escrita.
func TestFotografiaRecusaEscrita(t *testing.T) {
	pool := abrirPool(t)
	tx := NewTxManager(pool)
	m := novoMarcador(t, pool)

	for nome, sql := range map[string]string{
		"INSERT":                `INSERT INTO pii_access_log (contact_id, reason) VALUES ($1, 'qa-fotografia')`,
		"SELECT ... FOR UPDATE": `SELECT 1 FROM pii_access_log WHERE contact_id = $1 FOR UPDATE`,
	} {
		err := tx.Fotografia(context.Background(), func(ctx context.Context) error {
			_, err := From(ctx, pool).Exec(ctx, sql, m.id)
			return err
		})
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "25006" {
			t.Errorf("%s dentro da fotografia: esperado 25006, veio %v", nome, err)
		}
	}
	if n, err := m.contar(context.Background()); err != nil || n != 0 {
		t.Fatalf("a escrita recusada deixou %d linha(s) (err %v)", n, err)
	}
}

// Dentro de uma transação que já existe, a fotografia roda nela — como Do —, e
// o isolamento é o de quem a abriu: um snapshot não se troca no meio.
func TestFotografiaDentroDeUmaTransacaoUsaADeFora(t *testing.T) {
	pool := abrirPool(t)
	tx := NewTxManager(pool)
	m := novoMarcador(t, pool)

	desfazer := errors.New("desfazer")
	err := tx.Do(context.Background(), func(ctx context.Context) error {
		return tx.Fotografia(ctx, func(ctx context.Context) error {
			if _, err := From(ctx, pool).Exec(ctx,
				`INSERT INTO pii_access_log (contact_id, reason) VALUES ($1, 'qa-fotografia')`, m.id); err != nil {
				t.Errorf("a fotografia aninhada deveria escrever na transação de fora: %v", err)
			}
			if n, err := m.contar(ctx); err != nil || n != 1 {
				t.Errorf("dentro da transação de fora: %d linha(s), err %v", n, err)
			}
			return desfazer
		})
	})
	if !errors.Is(err, desfazer) {
		t.Fatalf("o erro de dentro deveria sair intacto: %v", err)
	}
	if n, err := m.contar(context.Background()); err != nil || n != 0 {
		t.Fatalf("o rollback da transação de fora não levou a linha: %d (err %v)", n, err)
	}
}
