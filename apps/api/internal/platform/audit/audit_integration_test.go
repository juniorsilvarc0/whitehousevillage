//go:build integration

// Prova, contra Postgres de verdade, as duas promessas do pacote:
//
//  1. a trilha entra na TRANSAÇÃO DE QUEM CHAMA — o rollback do negócio desfaz
//     a linha de auditoria junto, e antes do commit ela não existe para mais
//     ninguém;
//  2. campo sensível não chega à tabela.
//
// Sem (1), `audit_log` acumula registros de operações que não aconteceram — e
// uma trilha que mente é pior do que trilha nenhuma, porque é consultada com
// confiança. Sem (2), a tela de auditoria vira o lugar mais fácil da instalação
// para colher hash de senha.
package audit

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

const segredoQueNaoPodeVazar = "$argon2id$v=19$m=65536$NUNCA_EM_AUDIT_LOG"

type unidadeDeTeste struct {
	Codigo    string `json:"code"`
	Nome      string `json:"name"`
	Ativa     bool   `json:"active"`
	SenhaHash string `json:"password_hash"`
}

// ambiente é o pool, o gerenciador de transação e uma entidade fictícia própria
// deste teste — o nome carrega um uuid para que a suíte inteira (que roda com
// `-p 1` num Postgres compartilhado) nunca conte linha de outro pacote.
type ambiente struct {
	pool     *pgxpool.Pool
	tx       *db.TxManager
	entidade string
	ctx      context.Context
	ator     uuid.UUID
}

func subir(t *testing.T) *ambiente {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL ausente: teste de integração pulado (use `make test-integration`)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	pool, err := db.New(ctx, url)
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// Ator e propriedade REAIS: `audit_log.actor_id` e `property_id` são chaves
	// estrangeiras, e um uuid inventado transformaria o teste num 23503 em vez
	// de provar o que ele quer provar.
	var ator, propriedade uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE deleted_at IS NULL LIMIT 1`).Scan(&ator); err != nil {
		t.Fatalf("banco sem usuário: rode o seed antes (%v)", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM properties LIMIT 1`).Scan(&propriedade); err != nil {
		t.Fatalf("banco sem propriedade: rode o seed antes (%v)", err)
	}

	a := &ambiente{
		pool:     pool,
		tx:       db.NewTxManager(pool),
		entidade: "auditoria_it_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
		ator:     ator,
	}

	// O contexto que uma requisição autenticada teria depois do middleware de
	// auth, do RequestID do httpx e do audit.Middleware.
	c := auth.WithUser(ctx, &auth.Usuario{ID: ator, PropertyID: propriedade})
	c = httpx.ComRequestID(c, "req-"+a.entidade)
	a.ctx = ComOrigem(c, Origem{IP: "198.51.100.4", UserAgent: "painel-integracao/1.0"})

	t.Cleanup(func() {
		limpar, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelar()
		_, _ = pool.Exec(limpar, `DELETE FROM audit_log WHERE entity = $1`, a.entidade)
	})
	return a
}

// linhas conta pelo POOL — fora de qualquer transação do teste. É essa distinção
// que dá sentido ao teste: se Registrar abrisse transação própria, o pool veria
// a linha mesmo com o negócio abortado.
func (a *ambiente) linhas(t *testing.T) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE entity = $1`, a.entidade).Scan(&n); err != nil {
		t.Fatalf("contando a trilha: %v", err)
	}
	return n
}

var erroDeNegocio = errors.New("regra de negócio recusou a operação")

func TestTrilhaEntraNaTransacaoDoChamador(t *testing.T) {
	a := subir(t)
	alvo := uuid.New()

	// ── A transação do negócio falha DEPOIS de auditar ──────────────────────
	var viaPoolDuranteATransacao int
	err := a.tx.Do(a.ctx, func(ctx context.Context) error {
		if err := Alteracao(ctx, a.pool, a.entidade, VerboAlterado, alvo,
			unidadeDeTeste{Codigo: "AP-03", Ativa: true},
			unidadeDeTeste{Codigo: "AP-03", Ativa: false},
		); err != nil {
			return err
		}
		// Ainda dentro da transação: outra conexão não pode enxergar a linha.
		// É a prova direta de que ela está na transação de quem chamou, e não
		// numa transação própria já comitada.
		viaPoolDuranteATransacao = a.linhas(t)
		return erroDeNegocio
	})

	if !errors.Is(err, erroDeNegocio) {
		t.Fatalf("erro = %v, esperado o erro de negócio", err)
	}
	if viaPoolDuranteATransacao != 0 {
		t.Errorf("a trilha estava visível fora da transação antes do commit (%d linhas): "+
			"Registrar abriu transação própria", viaPoolDuranteATransacao)
	}
	if n := a.linhas(t); n != 0 {
		t.Fatalf("%d linha(s) de auditoria sobreviveram ao rollback do negócio — "+
			"a trilha registra uma alteração que NÃO aconteceu", n)
	}

	// ── A mesma escrita, agora com o negócio comitando ──────────────────────
	if err := a.tx.Do(a.ctx, func(ctx context.Context) error {
		return Alteracao(ctx, a.pool, a.entidade, VerboAlterado, alvo,
			unidadeDeTeste{Codigo: "AP-03", Ativa: true},
			unidadeDeTeste{Codigo: "AP-03", Ativa: false},
		)
	}); err != nil {
		t.Fatalf("transação bem-sucedida: %v", err)
	}
	if n := a.linhas(t); n != 1 {
		t.Fatalf("%d linha(s) depois do commit, esperado 1 — a trilha não está sendo gravada", n)
	}
}

func TestTrilhaNaoGravaCampoSensivel(t *testing.T) {
	a := subir(t)
	alvo := uuid.New()

	if err := a.tx.Do(a.ctx, func(ctx context.Context) error {
		return Criacao(ctx, a.pool, a.entidade, VerboCriado, alvo, unidadeDeTeste{
			Codigo:    "AP-03",
			Nome:      "Apartamento 3",
			Ativa:     true,
			SenhaHash: segredoQueNaoPodeVazar,
		})
	}); err != nil {
		t.Fatalf("registrando: %v", err)
	}

	var antes, depois *string
	if err := a.pool.QueryRow(context.Background(),
		`SELECT before::text, after::text FROM audit_log WHERE entity = $1 AND entity_id = $2`,
		a.entidade, alvo).Scan(&antes, &depois); err != nil {
		t.Fatalf("lendo a linha: %v", err)
	}

	if antes != nil {
		t.Errorf("before = %q numa criação, esperado NULL", *antes)
	}
	if depois == nil {
		t.Fatal("after nulo numa criação")
	}
	if strings.Contains(*depois, segredoQueNaoPodeVazar) {
		t.Fatalf("O SEGREDO FOI GRAVADO em audit_log.after: %s", *depois)
	}
	if !strings.Contains(*depois, Redigido) {
		t.Errorf("after não marcou o campo redigido — o fato de a senha ter sido definida sumiu: %s", *depois)
	}
	if !strings.Contains(*depois, "AP-03") {
		t.Errorf("after perdeu o campo comum: %s", *depois)
	}
}

func TestTrilhaGuardaAtorIPUserAgentERequestID(t *testing.T) {
	a := subir(t)
	alvo := uuid.New()

	if err := a.tx.Do(a.ctx, func(ctx context.Context) error {
		return Exclusao(ctx, a.pool, a.entidade, VerboExcluido, alvo,
			unidadeDeTeste{Codigo: "AP-03", Ativa: true})
	}); err != nil {
		t.Fatalf("registrando: %v", err)
	}

	var (
		ator        uuid.UUID
		acao        string
		ip          *string
		userAgent   *string
		requestID   *string
		propriedade *uuid.UUID
	)
	if err := a.pool.QueryRow(context.Background(),
		`SELECT actor_id, action, host(ip), user_agent, request_id, property_id
		   FROM audit_log WHERE entity = $1 AND entity_id = $2`,
		a.entidade, alvo).Scan(&ator, &acao, &ip, &userAgent, &requestID, &propriedade); err != nil {
		t.Fatalf("lendo a linha: %v", err)
	}

	if ator != a.ator {
		t.Errorf("actor_id = %v, esperado %v", ator, a.ator)
	}
	if esperado := Acao(a.entidade, VerboExcluido); acao != esperado {
		t.Errorf("action = %q, esperado %q", acao, esperado)
	}
	if ip == nil || *ip != "198.51.100.4" {
		t.Errorf("ip = %v, esperado 198.51.100.4 vindo do contexto", ip)
	}
	if userAgent == nil || *userAgent != "painel-integracao/1.0" {
		t.Errorf("user_agent = %v", userAgent)
	}
	if requestID == nil || *requestID != "req-"+a.entidade {
		t.Errorf("request_id = %v — sem ele não dá para ligar a linha ao log da API", requestID)
	}
	if propriedade == nil {
		t.Error("property_id nulo: a propriedade do ator não foi aproveitada")
	}
}

// Um IP que não é endereço (o `bufconn` dos testes de handler, por exemplo) não
// pode virar 22P02 e abortar a transação do NEGÓCIO. A coluna é `inet`.
func TestIPInvalidoNaoDerrubaATransacaoDoNegocio(t *testing.T) {
	a := subir(t)
	alvo := uuid.New()

	ctx := ComOrigem(a.ctx, Origem{IP: "bufconn", UserAgent: "teste"})
	if err := a.tx.Do(ctx, func(ctx context.Context) error {
		return Criacao(ctx, a.pool, a.entidade, VerboCriado, alvo, unidadeDeTeste{Codigo: "AP-01"})
	}); err != nil {
		t.Fatalf("a auditoria derrubou a operação por causa do IP: %v", err)
	}

	var ip *string
	if err := a.pool.QueryRow(context.Background(),
		`SELECT host(ip) FROM audit_log WHERE entity = $1 AND entity_id = $2`,
		a.entidade, alvo).Scan(&ip); err != nil {
		t.Fatalf("lendo a linha: %v", err)
	}
	if ip != nil {
		t.Errorf("ip = %q, esperado NULL para valor que não é endereço", *ip)
	}
}
