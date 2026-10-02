// Package idempotencia guarda a resposta de um POST irreversível e a devolve
// quando o MESMO cliente reapresenta a MESMA requisição — para que o clique
// duplo, o retry do navegador e o proxy que reenvia não virem duas reservas nem
// dois pagamentos.
//
// # Por que é plataforma, e não um arquivo dentro do módulo que serve a rota
//
// Até esta entrega havia DUAS cópias: `internal/modules/reservas/idempotencia.go`
// (186 linhas) e `internal/modules/crm/idempotencia.go` (161), com o `diff` das
// assinaturas de função entre as duas VAZIO — a mesma coisa escrita duas vezes,
// pelo mesmo impedimento de pasta que gerou D3 e D6. Não era higiene: a chave é
// `(key, endpoint, actor_id, property_id)` e uma cópia que esqueça o ator
// devolve ao corretor a resposta guardada pelo gestor (ver abaixo). Manter três
// lugares onde esse esquecimento pode acontecer é manter três chances de
// reabri-lo.
//
// `POST /finance/payments` (F2-14) é literalmente "movimenta dinheiro" e nasceria
// como a TERCEIRA cópia. O dia em que uma das três divergir é o dia em que um
// pagamento entra duas vezes no razão porque o navegador repetiu o POST.
//
// # A chave é POR ATOR, não global
//
// Isto é correção de um vazamento MEDIDO na revisão da Fase 1: com a PK antiga
// `(key, endpoint)`, o admin criava a reserva com `Idempotency-Key: K` e o
// corretor que reapresentasse a MESMA chave recebia 201 com a reserva inteira do
// admin (código, hóspede, valor) — a mesma reserva que o `GET /reservations/{id}`
// devolve a ele como 404, porque o escopo dele é `own`. O replay furava o RBAC e
// virava canal de leitura lateral.
//
// A chave nunca significou "esta requisição, no mundo": significa "esta
// requisição, deste cliente". A migration 20260826120000 passou a PK para
// `(key, endpoint, actor_id, property_id)`, e é por isso que [Reservar] e
// [Guardar] exigem o [Dono]. Dois usuários com a mesma chave são duas
// requisições independentes — cada um faz o próprio trabalho e relê só a própria
// resposta.
//
// # Como a corrida é resolvida
//
// A linha de `idempotency_keys` é inserida DENTRO da transação do trabalho. Uma
// segunda requisição com a mesma chave bate no `ON CONFLICT DO NOTHING`, que
// ESPERA a primeira transação terminar (o índice único faz a inserção
// especulativa aguardar o token da concorrente). Se a primeira comitou, a
// segunda lê a resposta gravada e a devolve; se abortou, a segunda insere e faz
// o trabalho ela mesma. Não existe janela em que as duas achem que são a
// primeira, e não existe estado "em voo" ambíguo para tratar.
//
// # A linha vive na transação de quem chama
//
// É por isso que as duas funções recebem o executor e usam [db.From]: dentro de
// `TxManager.Do` elas escrevem na transação do negócio, e "reserva criada" e
// "chave consumida" viram um fato só. Chamar fora de transação é erro de quem
// chama, com dois modos de falha: a chave comitada sozinha antes de o trabalho
// falhar envenena a chave (toda repetição vira IDEMPOTENCY_MISMATCH, e o cliente
// nunca mais consegue tentar), ou a resposta guardada sobrevive ao rollback do
// trabalho e o cliente relê "pagamento registrado" de um pagamento que não
// existe.
//
// Requisição que FALHA não deixa chave gravada: o rollback leva a linha junto. É
// o comportamento desejado — repetir a mesma chave depois de um 409 de data
// ocupada é uma tentativa nova e legítima, não a repetição de um sucesso.
package idempotencia

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

// NomeDoHeader é o header do contrato. Fica aqui para o handler não repetir a
// string: quem lê o header e quem valida a chave são a mesma decisão.
const NomeDoHeader = "Idempotency-Key"

const (
	// tamanhoMinimoDaChave / tamanhoMaximoDaChave espelham o `minLength` /
	// `maxLength` do header na OpenAPI.
	tamanhoMinimoDaChave = 8
	tamanhoMaximoDaChave = 255
)

// Resposta é o que se devolve quando a chave já foi usada com sucesso: o corpo
// ORIGINAL, byte a byte, e o mesmo status (inclusive 201).
type Resposta struct {
	Status int
	Corpo  []byte
}

// Dono identifica de QUEM é a chave. É o eixo que a PK ganhou na migration
// 20260826120000 e o mesmo que o RBAC filtra em `scope='own'`.
//
// `Propriedade` entra junto porque a resposta guardada é o retrato de UMA casa:
// hoje ela é função do ator (`users.property_id` é NOT NULL), e no dia em que um
// usuário atender duas propriedades o modo de falha seria de novo devolver a
// resposta da casa errada.
type Dono struct {
	Ator        uuid.UUID
	Propriedade uuid.UUID
}

// DonoDe monta o dono a partir do usuário autenticado.
func DonoDe(u *auth.Usuario) Dono { return Dono{Ator: u.ID, Propriedade: u.PropertyID} }

// Chave lê e valida o valor do header. Ausente é 422 com o campo apontado — a
// rota exige, e responder 400 genérico deixaria o cliente adivinhando.
func Chave(bruto string) (string, error) {
	chave := strings.TrimSpace(bruto)
	if chave == "" {
		return "", apperr.Validation(map[string]string{
			NomeDoHeader: "é obrigatório nesta rota: ela cria reserva ou movimenta dinheiro.",
		})
	}
	if len(chave) < tamanhoMinimoDaChave || len(chave) > tamanhoMaximoDaChave {
		return "", apperr.Validation(map[string]string{
			NomeDoHeader: "de 8 a 255 caracteres.",
		})
	}
	return chave, nil
}

// Impressao resume o corpo do pedido num hash estável.
//
// Serializa o DTO JÁ DECODIFICADO, e não os bytes crus: dois clientes que mandam
// os mesmos campos em ordem diferente (ou com espaçamento diferente) fizeram o
// MESMO pedido, e responder IDEMPOTENCY_MISMATCH a eles seria punir formatação.
func Impressao(corpo any) (string, error) {
	bruto, err := json.Marshal(corpo)
	if err != nil {
		return "", apperr.Internal.WithCause(err)
	}
	soma := sha256.Sum256(bruto)
	return hex.EncodeToString(soma[:]), nil
}

// Reservar tenta tomar a chave para esta requisição, NO ESCOPO DO ATOR.
//
// Devolve (nil, nil) quando a chave é nova e o trabalho deve prosseguir, e
// (resposta, nil) quando a chave já concluiu — aí o handler devolve a resposta
// original, com o mesmo status e o mesmo Location.
func Reservar(ctx context.Context, exec db.DBTX, chave, rota, hash string, d Dono) (*Resposta, error) {
	const qTomar = `
		INSERT INTO idempotency_keys (key, endpoint, actor_id, property_id, request_hash)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (key, endpoint, actor_id, property_id) DO NOTHING`

	const qLer = `
		SELECT request_hash, status, response_body
		  FROM idempotency_keys
		 WHERE key = $1 AND endpoint = $2 AND actor_id = $3 AND property_id = $4`

	// Laço, e não recursão: a concorrente pode abortar entre o conflito e a
	// leitura, e aí a chave voltou a estar livre e tomar de novo é o certo. Cada
	// volta é uma tomada nova; em recursão isso empilharia quadro por volta.
	for {
		tag, err := db.From(ctx, exec).Exec(ctx, qTomar, chave, rota, d.Ator, d.Propriedade, hash)
		if err != nil {
			return nil, db.MapError(err)
		}
		if tag.RowsAffected() == 1 {
			return nil, nil
		}

		var (
			gravado string
			status  *int
			corpo   []byte
		)
		err = db.From(ctx, exec).
			QueryRow(ctx, qLer, chave, rota, d.Ator, d.Propriedade).
			Scan(&gravado, &status, &corpo)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
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
			// depois do INSERT e antes do UPDATE, e o rollback deveria ter
			// levado a linha. Chegar aqui é dado inconsistente — recusar é mais
			// honesto que executar de novo o que talvez já tenha acontecido.
			return nil, apperr.IdempotencyMismatch.WithDetails(map[string]any{
				"endpoint": rota,
				"hint":     "chave em uso sem resposta registrada; gere uma chave nova.",
			})
		}
		return &Resposta{Status: *status, Corpo: corpo}, nil
	}
}

// Guardar grava a resposta na mesma transação do trabalho — é o que torna
// "reserva criada" e "chave consumida" um fato só.
func Guardar(ctx context.Context, exec db.DBTX, chave, rota string, d Dono, status int, corpo any) error {
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

	_, err = db.From(ctx, exec).Exec(ctx, q, chave, rota, d.Ator, d.Propriedade, status, bruto)
	return db.MapError(err)
}
