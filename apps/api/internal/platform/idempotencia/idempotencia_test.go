package idempotencia

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// ─────────────────────────── Espião do executor ─────────────────────
//
// Substitui o pool para provar O QUE as duas funções mandam ao banco. O que
// importa aqui é o DONO: as três consultas nomeiam `actor_id` e `property_id`,
// e é isso que faz a chave ser "esta requisição, deste cliente" em vez de "esta
// requisição, no mundo".

type chamada struct {
	sql  string
	args []any
}

type espiao struct {
	chamadas []chamada
	tag      pgconn.CommandTag
	linha    pgx.Row
	erro     error
}

func (e *espiao) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	e.chamadas = append(e.chamadas, chamada{sql: sql, args: args})
	return e.tag, e.erro
}

func (e *espiao) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	e.chamadas = append(e.chamadas, chamada{sql: sql, args: args})
	return e.linha
}

func (e *espiao) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("nem Reservar nem Guardar usam Query")
}

// linhaFalsa é a linha que o SELECT devolveria para uma chave já tomada.
type linhaFalsa struct {
	hash   string
	status *int
	corpo  []byte
}

func (l linhaFalsa) Scan(destino ...any) error {
	*(destino[0].(*string)) = l.hash
	*(destino[1].(**int)) = l.status
	*(destino[2].(*[]byte)) = l.corpo
	return nil
}

func tag(linhas string) pgconn.CommandTag { return pgconn.NewCommandTag(linhas) }

func donoDeTeste() Dono { return Dono{Ator: uuid.New(), Propriedade: uuid.New()} }

// ─────────────────────────── O header ───────────────────────────────

func TestChaveEhObrigatoriaEDelimitada(t *testing.T) {
	if _, err := Chave("   "); err == nil {
		t.Fatal("chave ausente tem de ser recusada: a rota que a exige é irreversível")
	}
	if _, err := Chave("curta"); err == nil {
		t.Fatal("chave abaixo de 8 caracteres deveria ser recusada (minLength do contrato)")
	}
	if _, err := Chave(strings.Repeat("x", 256)); err == nil {
		t.Fatal("chave acima de 255 caracteres deveria ser recusada (maxLength do contrato)")
	}
	chave, err := Chave("  chave-de-teste-0001  ")
	if err != nil || chave != "chave-de-teste-0001" {
		t.Fatalf("chave = %q, err = %v", chave, err)
	}
}

// O campo apontado no 422 é o NOME DO HEADER, e não um nome de campo de corpo:
// é ele que o cliente tem de corrigir.
func TestChaveAusenteApontaOHeader(t *testing.T) {
	_, err := Chave("")
	e := apperr.From(err)
	if e.Code != "VALIDATION_ERROR" {
		t.Fatalf("code = %s, esperado VALIDATION_ERROR", e.Code)
	}
	campos, _ := e.Details.(map[string]string)
	if campos[NomeDoHeader] == "" {
		t.Fatalf("o 422 não aponta %q: details = %v", NomeDoHeader, e.Details)
	}
}

// ─────────────────────────── A impressão ────────────────────────────

// A impressão decide entre "repetição" e IDEMPOTENCY_MISMATCH: tem de ignorar
// FORMATAÇÃO e reagir a CONTEÚDO.
func TestImpressaoIgnoraFormatacaoEReageAConteudo(t *testing.T) {
	type pedido struct {
		Unidade string `json:"unit"`
		Noites  int    `json:"nights"`
	}

	a, err := Impressao(pedido{Unidade: "suite", Noites: 3})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := Impressao(pedido{Unidade: "suite", Noites: 3}); a != b {
		t.Fatal("o mesmo pedido produziu impressões diferentes")
	}
	if c, _ := Impressao(pedido{Unidade: "suite", Noites: 4}); a == c {
		t.Fatal("mudar o conteúdo tem de mudar a impressão")
	}

	// Mesmos campos, ordem diferente no JSON de origem: o hash é do DTO
	// DECODIFICADO, então isto é o MESMO pedido. Punir a ordem dos campos faria
	// o cliente receber IDEMPOTENCY_MISMATCH por formatação.
	var decodificado pedido
	if err := json.Unmarshal([]byte(`{"nights":3,"unit":"suite"}`), &decodificado); err != nil {
		t.Fatal(err)
	}
	if d, _ := Impressao(decodificado); a != d {
		t.Fatal("a ordem dos campos no JSON não pode mudar a impressão")
	}
}

// ─────────────────────────── O dono na consulta ─────────────────────

// A chave é (key, endpoint, actor_id, property_id) — a PK da tabela. Uma
// consulta que deixe o ator de fora responde a requisição de um usuário com o
// que outro guardou; foi assim que o corretor recebeu a reserva do gestor antes
// da migration 20260826120000.
func TestOAtorEAPropriedadeVaoEmTodaConsulta(t *testing.T) {
	d := donoDeTeste()
	ctx := context.Background()

	tomada := &espiao{tag: tag("INSERT 0 1")}
	guardada, err := Reservar(ctx, tomada, "chave-0001", "POST /x", "hash", d)
	if err != nil || guardada != nil {
		t.Fatalf("chave nova = (%v, %v), esperado (nil, nil) para o trabalho prosseguir", guardada, err)
	}
	conferirDono(t, "INSERT de Reservar", tomada.chamadas[0], d)

	escrita := &espiao{tag: tag("UPDATE 1")}
	if err := Guardar(ctx, escrita, "chave-0001", "POST /x", d, 201, map[string]any{"ok": true}); err != nil {
		t.Fatalf("Guardar = %v", err)
	}
	conferirDono(t, "UPDATE de Guardar", escrita.chamadas[0], d)

	// A LEITURA da chave já tomada é a terceira consulta — e a que devolve o
	// corpo guardado. Sem o ator nela, o replay de um usuário lê a linha de
	// outro.
	status := 201
	leitura := &espiao{
		tag:   tag("INSERT 0 0"),
		linha: linhaFalsa{hash: "hash", status: &status, corpo: []byte(`{"data":{}}`)},
	}
	if _, err := Reservar(ctx, leitura, "chave-0001", "POST /x", "hash", d); err != nil {
		t.Fatalf("replay = %v", err)
	}
	conferirDono(t, "SELECT de Reservar", leitura.chamadas[1], d)
}

func conferirDono(t *testing.T, qual string, c chamada, d Dono) {
	t.Helper()
	for _, coluna := range []string{"actor_id", "property_id"} {
		if !strings.Contains(c.sql, coluna) {
			t.Errorf("%s não nomeia %s: a chave deixou de ser por ator.\n%s", qual, coluna, c.sql)
		}
	}
	if len(c.args) < 4 {
		t.Fatalf("%s recebeu %d argumentos, esperado ao menos 4", qual, len(c.args))
	}
	if c.args[2] != d.Ator {
		t.Errorf("%s: $3 = %v, esperado o ator %s", qual, c.args[2], d.Ator)
	}
	if c.args[3] != d.Propriedade {
		t.Errorf("%s: $4 = %v, esperado a propriedade %s", qual, c.args[3], d.Propriedade)
	}
}

// ─────────────────────────── As duas recusas ────────────────────────

// Mesma chave, corpo diferente: o cliente mudou o pedido e reusou a chave.
// Devolver a resposta antiga faria a requisição NOVA ser respondida com o
// resultado da ANTIGA.
func TestCorpoDivergenteNaMesmaChaveEhRecusado(t *testing.T) {
	status := 201
	e := &espiao{
		tag:   tag("INSERT 0 0"),
		linha: linhaFalsa{hash: "hash-da-primeira", status: &status, corpo: []byte(`{"data":{}}`)},
	}

	_, err := Reservar(context.Background(), e, "chave-0001", "POST /x", "hash-de-agora", donoDeTeste())
	if apperr.From(err).Code != "IDEMPOTENCY_MISMATCH" {
		t.Fatalf("code = %s, esperado IDEMPOTENCY_MISMATCH", apperr.From(err).Code)
	}
}

// Chave tomada e SEM resposta gravada: a transação anterior morreu entre o
// INSERT e o UPDATE e o rollback deveria ter levado a linha. Executar de novo
// seria repetir um trabalho que talvez tenha acontecido — num pagamento, é a
// segunda saída de caixa.
func TestChaveTomadaSemRespostaEhRecusadaEmVezDeReexecutar(t *testing.T) {
	e := &espiao{
		tag:   tag("INSERT 0 0"),
		linha: linhaFalsa{hash: "hash", status: nil, corpo: nil},
	}

	guardada, err := Reservar(context.Background(), e, "chave-0001", "POST /x", "hash", donoDeTeste())
	if guardada != nil {
		t.Fatal("resposta devolvida sem status gravado")
	}
	if apperr.From(err).Code != "IDEMPOTENCY_MISMATCH" {
		t.Fatalf("code = %s, esperado IDEMPOTENCY_MISMATCH", apperr.From(err).Code)
	}
}
