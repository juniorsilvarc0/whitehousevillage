package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SchemaVersion lê a tabela de controle do golang-migrate.
//
// Devolve (versão, dirty, erro). Banco sem a tabela — ou com ela vazia — é banco
// sem migration aplicada: devolve 0 sem erro, e quem chama decide (o /readyz
// recusa servir). Tratar isso como erro faria o readyz responder 500 em vez do
// 503 que o orquestrador entende como "ainda não pronto".
func SchemaVersion(ctx context.Context, exec DBTX) (uint64, bool, error) {
	var (
		versao int64
		dirty  bool
	)
	err := exec.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&versao, &dirty)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, false, nil
	case err != nil:
		if ehTabelaInexistente(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("lendo schema_migrations: %w", err)
	}
	if versao < 0 {
		return 0, dirty, nil
	}
	return uint64(versao), dirty, nil
}

func ehTabelaInexistente(err error) bool {
	var pg interface{ SQLState() string }
	if errors.As(err, &pg) {
		return pg.SQLState() == "42P01" // undefined_table
	}
	return false
}
