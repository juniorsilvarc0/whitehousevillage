package reservas

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

// Idempotência de POST que cria reserva ou move dinheiro.
//
// CLAUDE.md, convenções de API: `Idempotency-Key` é obrigatório em POST que cria
// reserva ou dinheiro. O contrato exige em três rotas — POST /reservations,
// /confirm e /reschedule — porque as três são irreversíveis sob concorrência:
// um clique duplo, um retry do navegador ou um proxy repetindo a requisição não
// podem virar duas reservas nem dois sinais.
//
// COMO A CORRIDA É RESOLVIDA: a linha de `idempotency_keys` é inserida DENTRO da
// mesma transação do trabalho. Uma segunda requisição com a mesma chave bate no
// `ON CONFLICT DO NOTHING`, que ESPERA a primeira transação terminar (o índice
// único faz a inserção especulativa aguardar o token da concorrente). Se a
// primeira comitou, a segunda lê a resposta gravada e a devolve; se abortou, a
// segunda insere e faz o trabalho ela mesma. Não existe janela em que as duas
// acham que são a primeira, e não existe estado "em voo" ambíguo para tratar.
//
// A CHAVE É POR ATOR, não global — e isso é correção de um vazamento MEDIDO na
// revisão: com a PK antiga `(key, endpoint)`, o admin criava a reserva com
// `Idempotency-Key: K` e o corretor que reapresentasse a MESMA chave recebia
// 201 com a reserva inteira do admin (código, hóspede, valor) — a mesma reserva
// que o `GET /reservations/{id}` devolve a ele como 404, porque o escopo dele é
// `own`. O replay furava o RBAC e virava canal de leitura lateral.
//
// A chave nunca significou "esta requisição, no mundo": significa "esta
// requisição, deste cliente". A migration 20260826120000 passou a PK para
// `(key, endpoint, actor_id, property_id)`, e é por isso que toda função daqui
// recebe o ATOR. Dois usuários com a mesma chave são duas requisições
// independentes — cada um faz o próprio trabalho e relê só a própria resposta.
//
// Requisição que FALHA não deixa chave gravada: o rollback leva a linha junto.
// É o comportamento desejado — repetir com a mesma chave depois de um 409 de
// data ocupada é uma nova tentativa legítima, não a repetição de um sucesso.

const (
	// tamanhoMinimoDaChave / tamanhoMaximoDaChave espelham o `minLength`/
	// `maxLength` do header na OpenAPI.
	tamanhoMinimoDaChave = 8
	tamanhoMaximoDaChave = 255
)

// respostaGuardada é o que se devolve quando a chave já foi usada com sucesso.
type respostaGuardada struct {
	Status int
	Corpo  []byte
}

// ChaveDeIdempotencia lê e valida o header. Ausente é 422 com o campo apontado —
// a rota exige, e responder 400 genérico deixaria o cliente adivinhando.
func ChaveDeIdempotencia(bruto string) (string, error) {
	chave := strings.TrimSpace(bruto)
	if chave == "" {
		return "", apperr.Validation(map[string]string{
			"Idempotency-Key": "é obrigatório nesta rota.",
		})
	}
	if len(chave) < tamanhoMinimoDaChave || len(chave) > tamanhoMaximoDaChave {
		return "", apperr.Validation(map[string]string{
			"Idempotency-Key": "de 8 a 255 caracteres.",
		})
	}
	return chave, nil
}

// impressao resume o corpo do pedido num hash estável.
//
// Serializa o DTO JÁ DECODIFICADO, e não os bytes crus: dois clientes que mandam
// os mesmos campos em ordem diferente (ou com espaçamento diferente) fizeram o
// MESMO pedido, e responder IDEMPOTENCY_MISMATCH a eles seria punir formatação.
func impressao(corpo any) (string, error) {
	bruto, err := json.Marshal(corpo)
	if err != nil {
		return "", apperr.Internal.WithCause(err)
	}
	soma := sha256.Sum256(bruto)
	return hex.EncodeToString(soma[:]), nil
}

// dono identifica de QUEM é a chave. É o eixo que a PK ganhou na migration
// 20260826120000 e o mesmo que o RBAC filtra em `scope='own'`.
//
// `Propriedade` entra junto porque a resposta guardada é o retrato de UMA casa:
// hoje ela é função do ator (`users.property_id` é NOT NULL), e no dia em que um
// usuário atender duas propriedades o modo de falha seria de novo devolver a
// resposta da casa errada.
type dono struct {
	Ator        uuid.UUID
	Propriedade uuid.UUID
}

func donoDo(u *auth.Usuario) dono { return dono{Ator: u.ID, Propriedade: u.PropertyID} }

// reservarChave tenta tomar a chave para esta requisição, NO ESCOPO DO ATOR.
//
// Devolve (nil, nil) quando a chave é nova e o trabalho deve prosseguir, e
// (resposta, nil) quando a chave já concluiu — aí o handler devolve a resposta
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
		// A concorrente abortou entre o conflito e a leitura. Repetir a tomada
		// é o certo: a chave voltou a estar livre.
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
		// Chave tomada e sem resposta gravada: a transação anterior morreu
		// depois do INSERT e antes do UPDATE, e o rollback deveria ter levado a
		// linha. Chegar aqui é dado inconsistente — recusar é mais honesto que
		// executar de novo o que talvez já tenha acontecido.
		return nil, apperr.IdempotencyMismatch.WithDetails(map[string]any{
			"endpoint": rota,
			"hint":     "chave em uso sem resposta registrada; gere uma chave nova.",
		})
	}
	return &respostaGuardada{Status: *status, Corpo: corpo}, nil
}

// guardarResposta grava a resposta na mesma transação do trabalho — é o que
// torna "reserva criada" e "chave consumida" um fato só.
func (r *Repository) guardarResposta(ctx context.Context, chave, rota string, d dono, status int, corpo any) error {
	bruto, err := json.Marshal(corpo)
	if err != nil {
		return apperr.Internal.WithCause(err)
	}

	// O WHERE repete a chave INTEIRA. Sem `actor_id`/`property_id` aqui, o
	// UPDATE alcançaria a linha de outro usuário que tenha a mesma
	// `Idempotency-Key` — seria reabrir pelo lado da escrita o vazamento que a
	// PK nova fechou pelo lado da leitura.
	const q = `
		UPDATE idempotency_keys SET status = $5, response_body = $6
		 WHERE key = $1 AND endpoint = $2 AND actor_id = $3 AND property_id = $4`

	_, err = r.exec(ctx).Exec(ctx, q, chave, rota, d.Ator, d.Propriedade, status, bruto)
	return db.MapError(err)
}
