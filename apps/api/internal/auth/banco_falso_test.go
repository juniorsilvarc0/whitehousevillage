package auth

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Banco e transação falsos para testar a FRONTEIRA TRANSACIONAL sem Postgres.
//
// O ponto do falso não é imitar SQL: é reproduzir a única coisa que o defeito de
// produção dependia — o rollback desfazer a escrita feita dentro da unidade que
// devolveu erro. Um repositório falso comum (que grava direto num mapa) esconde
// exatamente esse defeito, porque nele "revogar" sempre parece ter funcionado.
//
// A prova contra o banco de verdade está em sessao_integration_test.go; esta
// aqui roda em `go test ./...` sem infraestrutura, para a regressão não passar
// despercebida num CI sem banco.

// ─────────────────────────── Transação falsa ────────────────────────────────

type chaveUnidade struct{}

// unidade acumula as escritas de uma transação. Commit as aplica; rollback as
// joga fora — que é o comportamento de db.TxManager.Do.
type unidade struct{ pendentes []func() }

type txFalso struct{}

func (txFalso) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	u := &unidade{}
	if err := fn(context.WithValue(ctx, chaveUnidade{}, u)); err != nil {
		return err // rollback: nada do que a fn escreveu chega ao banco
	}
	for _, aplicar := range u.pendentes {
		aplicar()
	}
	return nil
}

func unidadeDe(ctx context.Context) *unidade {
	u, _ := ctx.Value(chaveUnidade{}).(*unidade)
	return u
}

// ─────────────────────────── Banco falso ────────────────────────────────────

type tokenFalso struct {
	id         uuid.UUID
	usuarioID  uuid.UUID
	familiaID  uuid.UUID
	hash       string
	expiraEm   time.Time
	revogadoEm *time.Time
	substPor   *uuid.UUID
}

type bancoFalso struct {
	mu sync.Mutex

	usuario   LinhaUsuario
	senhaHash string

	tokens  map[uuid.UUID]*tokenFalso
	porHash map[string]uuid.UUID

	// revogacoesDeFamilia conta só o que foi efetivamente aplicado (commitado).
	revogacoesDeFamilia int
	ultimoLogin         *time.Time

	// aoRotacionar roda DENTRO do Exec da rotação, antes de a linha ser
	// avaliada, e só uma vez. É o gancho que reproduz "outra transação commitou
	// entre o SELECT e o UPDATE" — a corrida de rotação, sem Postgres.
	aoRotacionar func()
}

func novoBancoFalso(senhaHash string) *bancoFalso {
	agora := time.Now()
	return &bancoFalso{
		usuario: LinhaUsuario{
			ID:         uuid.New(),
			PropertyID: uuid.New(),
			RoleID:     uuid.New(),
			RoleCode:   "gestor",
			RoleName:   "Gestor",
			Nome:       "Ana Gestora",
			Email:      "ana@wh.local",
			Ativo:      true,
			CriadoEm:   agora,
		},
		senhaHash: senhaHash,
		tokens:    map[uuid.UUID]*tokenFalso{},
		porHash:   map[string]uuid.UUID{},
	}
}

// gravarToken insere direto, sem passar por transação: é o estado de partida do
// teste, não o que está sendo verificado.
func (b *bancoFalso) gravarToken(t *tokenFalso) *tokenFalso {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens[t.id] = t
	b.porHash[t.hash] = t.id
	return t
}

func (b *bancoFalso) token(hash string) *tokenFalso {
	b.mu.Lock()
	defer b.mu.Unlock()
	id, ok := b.porHash[hash]
	if !ok {
		return nil
	}
	return b.tokens[id]
}

// vivosDaFamilia conta os tokens da família ainda sem revoked_at — é a pergunta
// que o teste de reuso faz ao banco depois do 401.
func (b *bancoFalso) vivosDaFamilia(familia uuid.UUID) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	vivos := 0
	for _, t := range b.tokens {
		if t.familiaID == familia && t.revogadoEm == nil {
			vivos++
		}
	}
	return vivos
}

// ─────────────────────────── db.DBTX ────────────────────────────────────────

func (b *bancoFalso) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "u.password_hash"):
		return b.credenciais(args[0].(string))

	case strings.Contains(sql, "replaced_by IS NOT NULL"):
		return b.linhaDoSucessor(args[0].(uuid.UUID))

	case strings.Contains(sql, "FROM refresh_tokens"):
		return b.linhaDoToken(args[0].(string))

	case strings.Contains(sql, "role_permissions"):
		return b.linhaDaSessao(args[0].(uuid.UUID))

	case strings.Contains(sql, "u.last_login_at, u.created_at"):
		return b.linhaDoUsuario(args[0].(uuid.UUID))

	case strings.Contains(sql, "INSERT INTO refresh_tokens"):
		return b.inserirRefresh(ctx, args)

	default:
		return linhaFalsa{err: fmt.Errorf("banco falso: QueryRow não previsto: %s", sql)}
	}
}

func (b *bancoFalso) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	var aplicar func()

	switch {
	case strings.Contains(sql, "replaced_by = $2"):
		anterior, novo := args[0].(uuid.UUID), args[1].(uuid.UUID)

		// O gancho da corrida roda antes da avaliação, como faria a transação
		// concorrente que commita enquanto esta espera o lock da linha.
		b.mu.Lock()
		gancho := b.aoRotacionar
		b.aoRotacionar = nil
		b.mu.Unlock()
		if gancho != nil {
			gancho()
		}

		// `WHERE ... AND revoked_at IS NULL`: token já revogado não afeta linha
		// nenhuma, e é esse zero que o repositório precisa enxergar.
		b.mu.Lock()
		t := b.tokens[anterior]
		vivo := t != nil && t.revogadoEm == nil
		b.mu.Unlock()
		if !vivo {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}

		aplicar = func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if t := b.tokens[anterior]; t != nil && t.revogadoEm == nil {
				agora := time.Now()
				t.revogadoEm, t.substPor = &agora, &novo
			}
		}

	case strings.Contains(sql, "WHERE family_id = $1"):
		familia := args[0].(uuid.UUID)
		aplicar = func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.revogacoesDeFamilia++
			agora := time.Now()
			for _, t := range b.tokens {
				if t.familiaID == familia && t.revogadoEm == nil {
					t.revogadoEm = &agora
				}
			}
		}

	case strings.Contains(sql, "WHERE user_id = $1"):
		usuario := args[0].(uuid.UUID)
		aplicar = func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			agora := time.Now()
			for _, t := range b.tokens {
				if t.usuarioID == usuario && t.revogadoEm == nil {
					t.revogadoEm = &agora
				}
			}
		}

	case strings.Contains(sql, "SET last_login_at"):
		aplicar = func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			agora := time.Now()
			b.ultimoLogin = &agora
		}

	default:
		return pgconn.CommandTag{}, fmt.Errorf("banco falso: Exec não previsto: %s", sql)
	}

	if u := unidadeDe(ctx); u != nil {
		u.pendentes = append(u.pendentes, aplicar)
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	aplicar()
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

// Query não é usado por nenhuma consulta do pacote auth.
func (b *bancoFalso) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, fmt.Errorf("banco falso: Query não previsto: %s", sql)
}

// ─────────────────────────── Consultas ──────────────────────────────────────

func (b *bancoFalso) credenciais(email string) pgx.Row {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !strings.EqualFold(email, b.usuario.Email) {
		return linhaFalsa{err: pgx.ErrNoRows}
	}
	return linhaFalsa{valores: []any{
		b.usuario.ID, b.usuario.PropertyID, b.usuario.RoleID, b.usuario.RoleCode,
		b.usuario.Nome, b.usuario.Email, b.senhaHash, b.usuario.Ativo,
	}}
}

func (b *bancoFalso) linhaDoUsuario(id uuid.UUID) pgx.Row {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id != b.usuario.ID {
		return linhaFalsa{err: pgx.ErrNoRows}
	}
	u := b.usuario
	return linhaFalsa{valores: []any{
		u.ID, u.PropertyID, u.RoleID, u.RoleCode, u.RoleName,
		u.Nome, u.Email, u.Telefone, u.BrokerID, u.Ativo, u.UltimoLogin, u.CriadoEm,
	}}
}

// linhaDaSessao responde ao CarregarSessao: identidade + matriz de permissões.
// A matriz vem vazia porque nenhum teste deste arquivo depende de permissão.
func (b *bancoFalso) linhaDaSessao(id uuid.UUID) pgx.Row {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id != b.usuario.ID || !b.usuario.Ativo {
		return linhaFalsa{err: pgx.ErrNoRows}
	}
	u := b.usuario
	return linhaFalsa{valores: []any{
		u.ID, u.PropertyID, u.RoleID, u.RoleCode, u.RoleName,
		// is_system: nenhum teste deste arquivo depende do perfil raiz, e o
		// dublê precisa acompanhar a ordem das colunas de CarregarSessao.
		false,
		u.Nome, u.Email, []byte("[]"),
	}}
}

func (b *bancoFalso) linhaDoToken(hash string) pgx.Row {
	t := b.token(hash)
	if t == nil {
		return linhaFalsa{err: pgx.ErrNoRows}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return linhaFalsa{valores: []any{
		t.id, t.usuarioID, t.familiaID, t.expiraEm, t.revogadoEm, t.substPor,
	}}
}

// linhaDoSucessor responde à releitura que separa as duas explicações para
// "zero linha afetada": token rotacionado por outra transação (reuso) ou
// simplesmente revogado sem sucessor (logout, troca de senha).
func (b *bancoFalso) linhaDoSucessor(id uuid.UUID) pgx.Row {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.tokens[id]
	if t == nil {
		return linhaFalsa{err: pgx.ErrNoRows}
	}
	return linhaFalsa{valores: []any{t.substPor != nil}}
}

func (b *bancoFalso) inserirRefresh(ctx context.Context, args []any) pgx.Row {
	novo := &tokenFalso{
		id:        uuid.New(),
		usuarioID: args[0].(uuid.UUID),
		familiaID: args[1].(uuid.UUID),
		hash:      args[2].(string),
		expiraEm:  args[5].(time.Time),
	}

	// O id volta na hora (RETURNING id), mas a linha só existe depois do commit.
	aplicar := func() { b.gravarToken(novo) }
	if u := unidadeDe(ctx); u != nil {
		u.pendentes = append(u.pendentes, aplicar)
	} else {
		aplicar()
	}
	return linhaFalsa{valores: []any{novo.id}}
}

// ─────────────────────────── Linha ──────────────────────────────────────────

type linhaFalsa struct {
	valores []any
	err     error
}

func (l linhaFalsa) Scan(destinos ...any) error {
	if l.err != nil {
		return l.err
	}
	if len(destinos) != len(l.valores) {
		return fmt.Errorf("banco falso: %d colunas para %d destinos", len(l.valores), len(destinos))
	}
	for i, d := range destinos {
		if err := atribuir(d, l.valores[i]); err != nil {
			return fmt.Errorf("coluna %d: %w", i, err)
		}
	}
	return nil
}

func atribuir(destino, valor any) error {
	switch d := destino.(type) {
	case *uuid.UUID:
		*d, _ = valor.(uuid.UUID)
	case **uuid.UUID:
		*d, _ = valor.(*uuid.UUID)
	case *string:
		*d, _ = valor.(string)
	case **string:
		*d, _ = valor.(*string)
	case *bool:
		*d, _ = valor.(bool)
	case *time.Time:
		*d, _ = valor.(time.Time)
	case **time.Time:
		*d, _ = valor.(*time.Time)
	case *[]byte:
		*d, _ = valor.([]byte)
	default:
		return fmt.Errorf("tipo de destino não suportado: %T", destino)
	}
	return nil
}
