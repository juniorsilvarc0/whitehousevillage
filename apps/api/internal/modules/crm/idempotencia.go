package crm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Idempotência do `/win` — a única rota deste módulo que cria reserva.
//
// A mecânica é a de `internal/modules/reservas/idempotencia.go`, sobre a MESMA
// tabela `idempotency_keys` e a mesma PK `(key, endpoint, actor_id,
// property_id)`. Está duplicada aqui, e não importada, porque aquelas funções
// são métodos NÃO exportados do repositório de reservas e `internal/modules/
// reservas` não é pasta deste agente nesta rodada — exportá-las é uma edição do
// módulo vizinho, que é exatamente a colisão que a divisão por pasta evita.
//
// PARA O INTEGRADOR: as duas cópias pedem para virar um
// `internal/platform/idempotencia` com `Reservar`/`Guardar` recebendo o
// `db.DBTX`. Ver o relatório.
//
// O QUE NÃO MUDA, e é o que importa:
//
//   - a linha é inserida DENTRO da transação do trabalho, então uma requisição
//     que falha não deixa chave gravada — repetir depois de um `409
//     DATE_CONFLICT` é uma tentativa nova e legítima, não a repetição de um
//     sucesso;
//   - a chave é POR ATOR. Com a chave global, o corretor que reapresentasse a
//     chave do gerente receberia a resposta inteira do gerente — um canal de
//     leitura lateral que fura o escopo `own`.

const (
	tamanhoMinimoDaChave = 8
	tamanhoMaximoDaChave = 255
)

// respostaGuardada é o que se devolve quando a chave já foi usada com sucesso.
type respostaGuardada struct {
	Status int
	Corpo  []byte
}

// ChaveDeIdempotencia lê e valida o header. Ausente é 422 com o campo apontado:
// a rota exige, e um 400 genérico deixaria o cliente adivinhando.
func ChaveDeIdempotencia(bruto string) (string, error) {
	chave := strings.TrimSpace(bruto)
	if chave == "" {
		return "", apperr.Validation(map[string]string{
			"Idempotency-Key": "é obrigatório nesta rota: ganhar a oportunidade cria reserva.",
		})
	}
	if len(chave) < tamanhoMinimoDaChave || len(chave) > tamanhoMaximoDaChave {
		return "", apperr.Validation(map[string]string{"Idempotency-Key": "de 8 a 255 caracteres."})
	}
	return chave, nil
}

// impressao resume o pedido num hash estável.
//
// Serializa o DTO JÁ DECODIFICADO, e não os bytes crus: dois clientes que mandam
// os mesmos campos em ordem diferente fizeram o MESMO pedido, e responder
// IDEMPOTENCY_MISMATCH a eles seria punir formatação.
func impressao(corpo any) (string, error) {
	bruto, err := json.Marshal(corpo)
	if err != nil {
		return "", apperr.Internal.WithCause(err)
	}
	soma := sha256.Sum256(bruto)
	return hex.EncodeToString(soma[:]), nil
}

// dono identifica de QUEM é a chave — o mesmo eixo que o RBAC filtra em `own`.
type dono struct {
	Ator        uuid.UUID
	Propriedade uuid.UUID
}

func donoDo(u *auth.Usuario) dono { return dono{Ator: u.ID, Propriedade: u.PropertyID} }

// reservarChave toma a chave para esta requisição, no escopo do ator.
//
// Devolve (nil, nil) quando a chave é nova e o trabalho deve prosseguir, e
// (resposta, nil) quando ela já concluiu — aí o handler devolve a resposta
// original, com o MESMO status (inclusive 201) e o mesmo Location.
func (r *Repository) reservarChave(ctx context.Context, chave, rota, hash string, d dono) (*respostaGuardada, error) {
	const qTomar = `
		INSERT INTO idempotency_keys (key, endpoint, actor_id, property_id, request_hash)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (key, endpoint, actor_id, property_id) DO NOTHING`

	tag, err := r.exec(ctx).Exec(ctx, qTomar, chave, rota, d.Ator, d.Propriedade, hash)
	if err != nil {
		return nil, db.MapError(err)
	}
	if tag.RowsAffected() == 1 {
		return nil, nil
	}

	const qLer = `
		SELECT request_hash, status, response_body
		  FROM idempotency_keys
		 WHERE key = $1 AND endpoint = $2 AND actor_id = $3 AND property_id = $4`

	var (
		gravado string
		status  *int
		corpo   []byte
	)
	err = r.exec(ctx).QueryRow(ctx, qLer, chave, rota, d.Ator, d.Propriedade).Scan(&gravado, &status, &corpo)
	if errors.Is(err, pgx.ErrNoRows) {
		// A concorrente abortou entre o conflito e a leitura: a chave voltou a
		// estar livre, e tomar de novo é o certo.
		return r.reservarChave(ctx, chave, rota, hash, d)
	}
	if err != nil {
		return nil, db.MapError(err)
	}

	if gravado != hash {
		return nil, apperr.IdempotencyMismatch.WithDetails(map[string]any{
			"endpoint": rota,
			"hint":     "a mesma Idempotency-Key foi usada com outro corpo; gere uma chave nova.",
		})
	}
	if status == nil {
		return nil, apperr.IdempotencyMismatch.WithDetails(map[string]any{
			"endpoint": rota,
			"hint":     "chave em uso sem resposta registrada; gere uma chave nova.",
		})
	}
	return &respostaGuardada{Status: *status, Corpo: corpo}, nil
}

// guardarResposta grava a resposta na mesma transação do trabalho — é o que
// torna "oportunidade ganha" e "chave consumida" um fato só.
func (r *Repository) guardarResposta(ctx context.Context, chave, rota string, d dono, status int, corpo any) error {
	bruto, err := json.Marshal(corpo)
	if err != nil {
		return apperr.Internal.WithCause(err)
	}

	// O WHERE repete a chave INTEIRA: sem `actor_id`/`property_id`, o UPDATE
	// alcançaria a linha de outro usuário com a mesma chave — reabrindo pelo
	// lado da escrita o vazamento que a PK fechou pelo lado da leitura.
	const q = `
		UPDATE idempotency_keys SET status = $5, response_body = $6
		 WHERE key = $1 AND endpoint = $2 AND actor_id = $3 AND property_id = $4`

	_, err = r.exec(ctx).Exec(ctx, q, chave, rota, d.Ator, d.Propriedade, status, bruto)
	return db.MapError(err)
}
