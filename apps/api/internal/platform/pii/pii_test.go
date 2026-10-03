package pii

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// execEspiao substitui o pool para provar o que Registrar manda ao banco — e,
// nos casos de recusa, que ele não manda nada.
type execEspiao struct {
	chamadas int
	sql      string
	args     []any
	erro     error
}

func (e *execEspiao) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	e.chamadas++
	e.sql = sql
	e.args = args
	return pgconn.CommandTag{}, e.erro
}

func (e *execEspiao) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("Registrar não consulta")
}

func (e *execEspiao) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("Registrar não consulta")
}

func TestRegistrarGravaAtorEntidadeEMotivo(t *testing.T) {
	ator := uuid.New()
	ctx := auth.WithUser(context.Background(), &auth.Usuario{ID: ator})
	contato := uuid.New()
	espiao := &execEspiao{}

	if err := Registrar(ctx, espiao, "contacts", contato, MotivoFicha); err != nil {
		t.Fatalf("Registrar = %v, esperado nil", err)
	}
	if espiao.chamadas != 1 {
		t.Fatalf("%d INSERT; esperado 1", espiao.chamadas)
	}
	if got := espiao.args[0].(*uuid.UUID); *got != ator {
		t.Errorf("actor_id = %s, esperado %s: sem o ator a linha não responde QUEM leu", got, ator)
	}
	if got := espiao.args[1].(uuid.UUID); got != contato {
		t.Errorf("contact_id = %s, esperado %s", got, contato)
	}
	if got := espiao.args[2].(string); got != MotivoFicha {
		t.Errorf("reason = %q, esperado %q", got, MotivoFicha)
	}
}

// Rota pública ou job sem usuário continua deixando rastro: a coluna é anulável
// justamente para o acesso sem ator não ser descartado.
func TestRegistrarSemUsuarioNoContextoGravaAtorNulo(t *testing.T) {
	espiao := &execEspiao{}
	if err := Registrar(context.Background(), espiao, "contacts", uuid.New(), MotivoExportacao); err != nil {
		t.Fatalf("Registrar = %v, esperado nil", err)
	}
	if espiao.args[0] != (*uuid.UUID)(nil) {
		t.Errorf("actor_id = %v, esperado nulo", espiao.args[0])
	}
}

// A garantia que o pacote existe para dar: erro do banco NÃO é engolido. Quem
// chama aborta a leitura com este erro.
func TestRegistrarNaoEngoleErroDoBanco(t *testing.T) {
	espiao := &execEspiao{erro: errors.New("pii_access_log fora do ar")}

	err := Registrar(context.Background(), espiao, "contacts", uuid.New(), MotivoFicha)
	if err == nil {
		t.Fatal("Registrar = nil com o INSERT falhando: a ficha seria servida sem registrar quem a leu")
	}
}

// `pii_access_log` só tem `contact_id`. Gravar o id de um corretor ali faria a
// consulta de acesso misturar duas populações — e pareceria certo.
func TestRegistrarRecusaEntidadeSemColunaNoLog(t *testing.T) {
	espiao := &execEspiao{}

	err := Registrar(context.Background(), espiao, "brokers", uuid.New(), MotivoFicha)
	if err == nil {
		t.Fatal("entidade sem coluna foi aceita")
	}
	if !errors.Is(err, errEntidadeSemColuna) {
		t.Errorf("erro = %v, esperado errEntidadeSemColuna", err)
	}
	if espiao.chamadas != 0 {
		t.Errorf("%d INSERT com entidade desconhecida; esperado 0", espiao.chamadas)
	}
}

func TestRegistrarRecusaMotivoForaDoVocabulario(t *testing.T) {
	espiao := &execEspiao{}

	err := Registrar(context.Background(), espiao, "contacts", uuid.New(), "detalhe")
	if err == nil {
		t.Fatal("motivo inventado foi aceito: a consulta por motivo passa a depender de quem digitou")
	}
	if !errors.Is(err, errMotivoDesconhecido) {
		t.Errorf("erro = %v, esperado errMotivoDesconhecido", err)
	}
	if espiao.chamadas != 0 {
		t.Errorf("%d INSERT com motivo desconhecido; esperado 0", espiao.chamadas)
	}
}

func TestRegistrarVariosGravaUmaInstrucaoComTodosOsIDs(t *testing.T) {
	espiao := &execEspiao{}
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	if err := RegistrarVarios(context.Background(), espiao, "contacts", ids, MotivoListaDeHospedes); err != nil {
		t.Fatalf("RegistrarVarios: %v", err)
	}
	if espiao.chamadas != 1 {
		t.Fatalf("instruções = %d, esperado 1", espiao.chamadas)
	}
	if got, ok := espiao.args[1].([]uuid.UUID); !ok || len(got) != 3 {
		t.Fatalf("ids enviados = %#v", espiao.args[1])
	}
	if got := espiao.args[2].(string); got != MotivoListaDeHospedes {
		t.Errorf("reason = %q", got)
	}
}

func TestRegistrarVariosSemPessoaNaoGravaMasConfereOMotivo(t *testing.T) {
	espiao := &execEspiao{}
	if err := RegistrarVarios(context.Background(), espiao, "contacts", nil, MotivoListaDeHospedes); err != nil {
		t.Fatalf("lista vazia virou erro: %v", err)
	}
	if espiao.chamadas != 0 {
		t.Fatalf("lista vazia gravou %d vezes", espiao.chamadas)
	}
	if err := RegistrarVarios(context.Background(), espiao, "contacts", nil, "lista"); !errors.Is(err, errMotivoDesconhecido) {
		t.Fatalf("motivo inventado com lista vazia passou: %v", err)
	}
}

func TestRegistrarVariosPropagaAFalhaDoBanco(t *testing.T) {
	espiao := &execEspiao{erro: errors.New("pii_access_log indisponível")}
	if err := RegistrarVarios(context.Background(), espiao, "contacts", []uuid.UUID{uuid.New()}, MotivoOportunidade); err == nil {
		t.Fatal("falha do banco foi engolida: a leitura sairia sem rastro")
	}
}
