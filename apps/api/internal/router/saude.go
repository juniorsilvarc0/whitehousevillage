package router

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// SchemaVersionEsperada é a última migration que este binário sabe consumir.
//
// Serve para o deploy falhar RUIDOSAMENTE quando a API subir antes do passo de
// migration: a alternativa é a API responder 200 e quebrar na primeira consulta
// a uma coluna que ainda não existe, com o erro aparecendo para o usuário final
// em vez de para o orquestrador.
//
// Sobe junto com a migration nova, sempre no mesmo commit.
const SchemaVersionEsperada uint64 = 20261009100000

// Saude responde os dois endpoints de sonda.
type Saude struct {
	pool *pgxpool.Pool
}

func NewSaude(pool *pgxpool.Pool) *Saude { return &Saude{pool: pool} }

// Vivo — GET /healthz. Responde enquanto o processo estiver de pé, sem tocar no
// banco: é a sonda de liveness, e reiniciar o container porque o Postgres caiu
// só troca uma indisponibilidade por duas.
func (s *Saude) Vivo(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Pronto — GET /readyz. Sonda de readiness de verdade: pinga o banco e confere a
// versão do schema. Responde 503 (e sai do balanceamento) se o banco não
// responder, se a migration estiver `dirty` ou se o schema estiver defasado.
func (s *Saude) Pronto(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.pool.Ping(ctx); err != nil {
		httpx.JSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":   "indisponivel",
			"database": "inacessivel",
			"detail":   err.Error(),
		})
		return
	}

	versao, sujo, err := db.SchemaVersion(ctx, s.pool)
	if err != nil {
		httpx.JSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":   "indisponivel",
			"database": "ok",
			"schema":   "ilegivel",
			"detail":   err.Error(),
		})
		return
	}

	switch {
	case sujo:
		// `dirty` significa migration interrompida no meio: o schema está em
		// estado desconhecido e servir tráfego em cima dele corrompe dado.
		httpx.JSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":         "indisponivel",
			"database":       "ok",
			"schema":         "dirty",
			"schema_version": versao,
		})
	case versao < SchemaVersionEsperada:
		httpx.JSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":          "indisponivel",
			"database":        "ok",
			"schema":          "defasado",
			"schema_version":  versao,
			"schema_esperada": SchemaVersionEsperada,
		})
	default:
		// Versão MAIOR que a esperada é aceita: durante um deploy progressivo a
		// migration nova já rodou e a instância antiga ainda serve — recusar aí
		// derrubaria a aplicação inteira no meio da janela.
		httpx.JSON(w, http.StatusOK, map[string]any{
			"status":         "ok",
			"database":       "ok",
			"schema_version": versao,
		})
	}
}
