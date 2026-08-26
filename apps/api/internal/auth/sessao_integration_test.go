//go:build integration

// Integração de identidade e sessão contra Postgres real.
//
// O que vive aqui é o que só o banco sabe responder: rotação de refresh,
// detecção de reuso (que depende de `replaced_by` e `revoked_at` gravados na
// mesma transação), expiração e a morte imediata da sessão quando a conta é
// desativada. Testar isso com repositório falso provaria apenas que o falso
// concorda com o service.
package auth

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// senhaDoTeste é fixa e conhecida: teste com senha aleatória não é mais seguro,
// só mais difícil de depurar quando falha.
const senhaDoTeste = "senha-de-integracao-2026"

func poolDeIntegracao(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL ausente: teste de integração pulado (use `make test-integration`)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := db.New(ctx, url)
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// conta é o usuário que o teste acabou de criar.
type conta struct {
	ID     uuid.UUID
	RoleID uuid.UUID
	Email  string
}

// criarConta monta perfil + usuário próprios do teste. Cada teste tem os seus,
// com sufixo único: reaproveitar o usuário do seed faria um teste de bloqueio
// por tentativas derrubar o login dos outros.
func criarConta(t *testing.T, ctx context.Context, pool *pgxpool.Pool, apelido string) conta {
	t.Helper()

	sufixo := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]

	var propriedade uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO properties (name, slug, timezone)
		VALUES ('White House Village', 'white-house-village', 'America/Fortaleza')
		ON CONFLICT (slug) DO UPDATE SET slug = EXCLUDED.slug
		RETURNING id`).Scan(&propriedade); err != nil {
		t.Fatalf("propriedade: %v", err)
	}

	var papel uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO roles (code, name) VALUES ($1, $2) RETURNING id`,
		"qa-"+apelido+"-"+sufixo, "QA "+apelido).Scan(&papel); err != nil {
		t.Fatalf("perfil: %v", err)
	}

	hash, err := Hash(senhaDoTeste)
	if err != nil {
		t.Fatalf("hash da senha: %v", err)
	}

	email := "qa-" + apelido + "-" + sufixo + "@wh.local"
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (property_id, role_id, name, email, password_hash)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		propriedade, papel, "QA "+apelido, email, hash).Scan(&id); err != nil {
		t.Fatalf("usuário: %v", err)
	}

	t.Cleanup(func() {
		limpeza, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// users tem ON DELETE CASCADE em refresh_tokens e password_resets.
		if _, err := pool.Exec(limpeza, `DELETE FROM users WHERE id = $1`, id); err != nil {
			t.Errorf("limpando o usuário: %v", err)
		}
		if _, err := pool.Exec(limpeza, `DELETE FROM roles WHERE id = $1`, papel); err != nil {
			t.Errorf("limpando o perfil: %v", err)
		}
	})

	return conta{ID: id, RoleID: papel, Email: email}
}

// servicoDeAuth monta o Service com repositório e transação reais. Cada teste
// tem o seu: os contadores de tentativa vivem no Service, e compartilhá-lo faria
// um teste bloquear o outro.
func servicoDeAuth(t *testing.T, pool *pgxpool.Pool) *Service {
	t.Helper()
	return NewService(
		NewRepository(pool),
		db.NewTxManager(pool),
		NovoEmissor("segredo-de-integracao-com-mais-de-32-bytes", 15*time.Minute),
		720*time.Hour,
		nil,
	)
}

func codigo(err error) string {
	if err == nil {
		return ""
	}
	return apperr.From(err).Code
}

// ─────────────────────────── Login ──────────────────────────────────────────

// Em linguagem de negócio: quem digita a senha certa entra; quem erra a senha,
// quem digita um e-mail que não existe e quem teve a conta desativada ouvem
// exatamente a mesma frase. Diferenciar entregaria a lista de quem tem conta.
func TestLoginDistingueSenhaCertaEDaARespostaIdenticaAoResto(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "login")

	t.Run("senha_correta_abre_sessao", func(t *testing.T) {
		svc := servicoDeAuth(t, pool)
		sess, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: senhaDoTeste}, Origem{})
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		if sess.Resposta.AccessToken == "" {
			t.Error("access_token vazio")
		}
		if sess.RefreshClaro == "" {
			t.Error("refresh vazio — sem ele o painel não mantém sessão")
		}
		if sess.Resposta.Usuario.Email != c.Email {
			t.Errorf("user.email = %q, esperado %q", sess.Resposta.Usuario.Email, c.Email)
		}
		if sess.Resposta.ExpiresIn <= 0 || sess.Resposta.ExpiresIn > 900 {
			t.Errorf("expires_in = %d, esperado até 900 (15 min)", sess.Resposta.ExpiresIn)
		}

		// O último acesso é o que a tela de usuários mostra; se não gravar aqui,
		// não grava nunca.
		var ultimo *time.Time
		if err := pool.QueryRow(ctx, `SELECT last_login_at FROM users WHERE id = $1`, c.ID).Scan(&ultimo); err != nil {
			t.Fatalf("lendo last_login_at: %v", err)
		}
		if ultimo == nil {
			t.Error("last_login_at continuou nulo depois do login")
		}
	})

	t.Run("senha_errada", func(t *testing.T) {
		svc := servicoDeAuth(t, pool)
		_, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: "senha-errada"}, Origem{})
		if codigo(err) != "INVALID_CREDENTIALS" {
			t.Fatalf("code = %q, esperado INVALID_CREDENTIALS", codigo(err))
		}
		if apperr.From(err).Details != nil {
			t.Error("details preenchido: qualquer pista aqui vira oráculo de enumeração")
		}
	})

	t.Run("email_inexistente_responde_igual", func(t *testing.T) {
		svc := servicoDeAuth(t, pool)
		_, err := svc.Login(ctx, PedidoLogin{Email: "ninguem-" + c.Email, Senha: senhaDoTeste}, Origem{})
		if codigo(err) != "INVALID_CREDENTIALS" {
			t.Fatalf("code = %q, esperado INVALID_CREDENTIALS", codigo(err))
		}
	})

	t.Run("email_sem_diferenca_de_caixa", func(t *testing.T) {
		svc := servicoDeAuth(t, pool)
		if _, err := svc.Login(ctx, PedidoLogin{Email: strings.ToUpper(c.Email), Senha: senhaDoTeste}, Origem{}); err != nil {
			t.Fatalf("e-mail em caixa alta deveria entrar: %v", err)
		}
	})

	t.Run("conta_desativada_responde_igual", func(t *testing.T) {
		svc := servicoDeAuth(t, pool)
		outra := criarConta(t, ctx, pool, "desativada")
		if _, err := pool.Exec(ctx, `UPDATE users SET active = false WHERE id = $1`, outra.ID); err != nil {
			t.Fatalf("desativando: %v", err)
		}

		_, err := svc.Login(ctx, PedidoLogin{Email: outra.Email, Senha: senhaDoTeste}, Origem{})
		if codigo(err) != "INVALID_CREDENTIALS" {
			t.Fatalf("code = %q, esperado INVALID_CREDENTIALS", codigo(err))
		}
	})
}

// docs/spec.md §1: 5 tentativas erradas em 15 min bloqueiam o e-mail por 15 min.
//
// Em linguagem de negócio: quem está tentando adivinhar a senha de alguém tem
// cinco chances a cada quinze minutos — e, passadas elas, nem a senha certa
// abre, para o atacante não descobrir que acertou.
func TestCincoErrosBloqueiamOEmailAindaQueASenhaSejaCorrigida(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "bloqueio")
	svc := servicoDeAuth(t, pool)

	for i := 1; i <= 5; i++ {
		_, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: "errada"}, Origem{})
		if codigo(err) != "INVALID_CREDENTIALS" {
			t.Fatalf("tentativa %d: code = %q", i, codigo(err))
		}
	}

	_, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: senhaDoTeste}, Origem{})
	if err == nil {
		t.Fatal("a senha correta entrou depois de 5 erros — o e-mail deveria estar bloqueado por 15 min")
	}
	if codigo(err) != "INVALID_CREDENTIALS" {
		t.Fatalf("code = %q, esperado INVALID_CREDENTIALS (um code próprio de bloqueio confirmaria que a conta existe)", codigo(err))
	}
}

// ─────────────────────────── Rotação e reuso ────────────────────────────────

// Em linguagem de negócio: cada renovação de sessão queima o comprovante
// anterior. Quem apresentar o comprovante velho está com uma cópia — e uma cópia
// só existe se alguém copiou.
func TestRefreshRotacionaEMarcaOAnteriorComoSubstituido(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "rotacao")
	svc := servicoDeAuth(t, pool)

	primeira, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: senhaDoTeste}, Origem{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	segunda, err := svc.Refresh(ctx, primeira.RefreshClaro, Origem{})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if segunda.RefreshClaro == primeira.RefreshClaro {
		t.Fatal("o refresh voltou igual: sem rotação, um token roubado vale 30 dias")
	}
	if segunda.Resposta.AccessToken == "" {
		t.Error("refresh não devolveu access token novo")
	}

	var (
		revogadoEm  *time.Time
		substituido *uuid.UUID
	)
	if err := pool.QueryRow(ctx, `
		SELECT revoked_at, replaced_by FROM refresh_tokens WHERE token_hash = $1`,
		HashRefreshToken(primeira.RefreshClaro)).Scan(&revogadoEm, &substituido); err != nil {
		t.Fatalf("lendo o token anterior: %v", err)
	}
	if revogadoEm == nil {
		t.Error("o token apresentado não foi revogado")
	}
	if substituido == nil {
		t.Error("replaced_by ficou nulo — sem ele não há como distinguir reuso de token desconhecido")
	}

	// Continuidade da família: deslogar no celular não pode derrubar o navegador,
	// e para isso o sucessor tem de nascer na MESMA família do login.
	var familias int
	if err := pool.QueryRow(ctx,
		`SELECT count(DISTINCT family_id) FROM refresh_tokens WHERE user_id = $1`, c.ID).Scan(&familias); err != nil {
		t.Fatalf("contando famílias: %v", err)
	}
	if familias != 1 {
		t.Errorf("famílias = %d, esperado 1: a rotação abriu uma família nova", familias)
	}
}

// Em linguagem de negócio: se o comprovante velho reaparece, o sistema assume
// roubo, derruba a sessão inteira daquele login — a do ladrão e a do dono — e
// manda o dono entrar de novo. É a única defesa possível quando não dá para
// saber qual dos dois é o legítimo.
//
// O que este teste guarda é a FRONTEIRA TRANSACIONAL: a revogação roda em
// transação própria, que commita, e só então o service devolve TOKEN_REUSED.
// Enquanto as duas coisas dividiam a mesma unidade, o rollback do TxManager
// desfazia a revogação — o log dizia "família revogada", o cliente recebia 401 e
// o ladrão seguia rotacionando o token roubado indefinidamente.
func TestReusoDeRefreshRevogaAFamiliaInteira(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "reuso")
	svc := servicoDeAuth(t, pool)

	primeira, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: senhaDoTeste}, Origem{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	segunda, err := svc.Refresh(ctx, primeira.RefreshClaro, Origem{})
	if err != nil {
		t.Fatalf("rotação: %v", err)
	}

	// O ladrão apresenta a cópia do token já rotacionado.
	if _, err := svc.Refresh(ctx, primeira.RefreshClaro, Origem{}); codigo(err) != "TOKEN_REUSED" {
		t.Fatalf("code = %q, esperado TOKEN_REUSED", codigo(err))
	}

	// O estado do banco é a prova: depois da detecção de roubo, TODOS os tokens
	// da família precisam estar com revoked_at preenchido — e commitados.
	var total, revogados int
	if err := pool.QueryRow(ctx, `
		SELECT count(*), count(revoked_at)
		  FROM refresh_tokens
		 WHERE user_id = $1`, c.ID).Scan(&total, &revogados); err != nil {
		t.Fatalf("lendo o estado da família: %v", err)
	}
	if total == 0 {
		t.Fatal("nenhum refresh token no banco: o teste não provaria nada")
	}
	if revogados != total {
		t.Errorf("%d de %d tokens da família ficaram sem revoked_at: "+
			"a revogação foi desfeita pelo rollback da transação que devolveu TOKEN_REUSED",
			total-revogados, total)
	}

	// E o dono legítimo, com o token bom, também perde a sessão: é o preço de
	// não saber quem é quem. O ladrão, que vinha renovando com ele, para aqui.
	//
	// O código é TOKEN_INVALID e não TOKEN_REUSED: este token foi revogado sem
	// nunca ter gerado sucessor, então não é evidência de roubo — é só sessão
	// morta.
	if _, err := svc.Refresh(ctx, segunda.RefreshClaro, Origem{}); codigo(err) != "TOKEN_INVALID" {
		t.Errorf("o token que o ladrão vinha usando respondeu %q, esperado TOKEN_INVALID: "+
			"quem copiou o token não pode seguir dentro do sistema", codigo(err))
	}
}

// Em linguagem de negócio: passados os 30 dias, o comprovante não vale mais —
// mesmo nunca tendo sido usado.
func TestRefreshExpiradoNaoRenovaSessao(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "expirado")
	svc := servicoDeAuth(t, pool)
	repo := NewRepository(pool)

	claro, hash, err := NovoRefreshToken()
	if err != nil {
		t.Fatalf("gerando refresh: %v", err)
	}
	// Data fixa no passado em vez de esperar: `sleep` de 30 dias não é opção, e
	// o relógio do teste não pode decidir o resultado.
	if _, err := repo.CriarRefresh(ctx, c.ID, uuid.New(), hash, time.Now().Add(-time.Minute), "", ""); err != nil {
		t.Fatalf("gravando refresh vencido: %v", err)
	}

	if _, err := svc.Refresh(ctx, claro, Origem{}); codigo(err) != "TOKEN_INVALID" {
		t.Fatalf("code = %q, esperado TOKEN_INVALID", codigo(err))
	}

	// Token vencido NÃO é reuso: revogar a família aqui derrubaria as outras
	// sessões legítimas de quem só deixou uma aba velha aberta.
	if _, err := svc.Refresh(ctx, claro, Origem{}); codigo(err) != "TOKEN_INVALID" {
		t.Fatalf("segunda apresentação virou %q; vencido não é sinal de roubo", codigo(err))
	}
}

// Em linguagem de negócio: sair do sistema encerra aquele login inteiro, não só
// a última renovação.
func TestLogoutDerrubaAFamiliaEEhIdempotente(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "logout")
	svc := servicoDeAuth(t, pool)

	sess, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: senhaDoTeste}, Origem{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := svc.Logout(ctx, sess.RefreshClaro); err != nil {
		t.Fatalf("logout: %v", err)
	}

	// TOKEN_INVALID, e não TOKEN_REUSED: sair do sistema numa aba não pode fazer
	// a outra aba acusar "sessão comprometida". Token revogado sem sucessor é
	// sessão encerrada, não roubo.
	if codigo := codigo(mustErrDeSessao(svc.Refresh(ctx, sess.RefreshClaro, Origem{}))); codigo != "TOKEN_INVALID" {
		t.Fatalf("refresh depois do logout = %q, esperado TOKEN_INVALID", codigo)
	}

	// Repetir o logout, ou mandar um token que nunca existiu, também termina em
	// sucesso: um logout que falha deixa o usuário achando que continua logado.
	if err := svc.Logout(ctx, sess.RefreshClaro); err != nil {
		t.Errorf("segundo logout: %v", err)
	}
	if err := svc.Logout(ctx, "token-que-nunca-existiu"); err != nil {
		t.Errorf("logout de token desconhecido: %v", err)
	}
	if err := svc.Logout(ctx, ""); err != nil {
		t.Errorf("logout sem token: %v", err)
	}
}

// mustErrDeSessao descarta a sessão e devolve só o erro, para o teste ler o code.
func mustErrDeSessao(_ Sessao, err error) error { return err }

// docs/spec.md §1 diz "5 erros bloqueiam o e-mail", mas contar só por e-mail é
// negação de serviço de graça: quem conhece admin@wh.local tranca o
// administrador para sempre, de IPs variados. A contagem é por e-mail+IP.
//
// Em linguagem de negócio: quem erra a senha tranca a si mesmo, não a conta da
// outra pessoa.
func TestErrosDeUmIPNaoTrancamOLoginDoDono(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "dos")
	svc := servicoDeAuth(t, pool)

	const ipDoAtacante = "203.0.113.66"
	const ipDoDono = "198.51.100.10"

	for i := 1; i <= 5; i++ {
		if _, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: "errada"},
			Origem{IP: ipDoAtacante}); codigo(err) != "INVALID_CREDENTIALS" {
			t.Fatalf("tentativa %d: code = %q", i, codigo(err))
		}
	}

	if _, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: senhaDoTeste},
		Origem{IP: ipDoAtacante}); err == nil {
		t.Fatal("o IP que errou cinco vezes deveria estar bloqueado, mesmo acertando a senha")
	}

	if _, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: senhaDoTeste},
		Origem{IP: ipDoDono}); err != nil {
		t.Fatalf("o dono ficou trancado fora por erros de outra pessoa: %v", err)
	}
}

// broker_id liga a conta de um corretor ao cadastro dele. A coluna existe desde
// a migration 20260820140000; o campo vinha nulo fixo, e o painel do corretor
// ficava sem dono.
func TestUsuarioDevolveBrokerIDGravadoNoBanco(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "corretor")

	corretor := uuid.New()
	if _, err := pool.Exec(ctx, `UPDATE users SET broker_id = $2 WHERE id = $1`, c.ID, corretor); err != nil {
		t.Fatalf("gravando broker_id: %v", err)
	}

	linha, err := NewRepository(pool).BuscarUsuario(ctx, c.ID)
	if err != nil {
		t.Fatalf("buscando usuário: %v", err)
	}
	usuario := UsuarioDaLinha(linha)
	if usuario.BrokerID == nil {
		t.Fatal("broker_id voltou nulo para uma conta ligada a um corretor")
	}
	if *usuario.BrokerID != corretor {
		t.Errorf("broker_id = %v, esperado %v", *usuario.BrokerID, corretor)
	}
}

// ─────────────────────────── Recuperação de senha ───────────────────────────

// notificadorEspiao guarda o token em vez de mandar e-mail. É o único jeito de o
// teste conhecer o link que o usuário receberia.
type notificadorEspiao struct{ token, email string }

func (n *notificadorEspiao) EnviarRecuperacaoDeSenha(_ context.Context, email, token string) error {
	n.email, n.token = email, token
	return nil
}

// Em linguagem de negócio: o link de recuperação vale uma vez e por uma hora; ao
// trocar a senha, todas as sessões abertas caem — se a conta estava nas mãos de
// outra pessoa, ela perde o acesso no mesmo instante.
func TestRecuperacaoDeSenhaEhDeUsoUnicoEDerrubaAsSessoes(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "reset")

	espiao := &notificadorEspiao{}
	svc := NewService(
		NewRepository(pool),
		db.NewTxManager(pool),
		NovoEmissor("segredo-de-integracao-com-mais-de-32-bytes", 15*time.Minute),
		720*time.Hour,
		espiao,
	)

	sessaoAberta, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: senhaDoTeste}, Origem{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if err := svc.EsqueciSenha(ctx, c.Email, Origem{IP: "203.0.113.10"}); err != nil {
		t.Fatalf("esqueci a senha: %v", err)
	}
	if espiao.token == "" {
		t.Fatal("nenhum token foi entregue ao notificador")
	}

	const novaSenha = "nova-senha-de-integracao"
	if err := svc.TrocarSenha(ctx, PedidoTrocarSenha{Token: espiao.token, Senha: novaSenha}); err != nil {
		t.Fatalf("trocando a senha: %v", err)
	}

	// Uso único: o mesmo link não serve duas vezes.
	err = svc.TrocarSenha(ctx, PedidoTrocarSenha{Token: espiao.token, Senha: "outra-senha-qualquer"})
	if codigo(err) != "TOKEN_INVALID" {
		t.Fatalf("segundo uso do link: code = %q, esperado TOKEN_INVALID", codigo(err))
	}
	if status := apperr.From(err).Status(); status != 400 {
		t.Errorf("status = %d, esperado 400: o link do e-mail não é credencial de sessão, e 401 faria o painel mandar relogar", status)
	}

	// A sessão que estava aberta morre.
	if _, err := svc.Refresh(ctx, sessaoAberta.RefreshClaro, Origem{}); err == nil {
		t.Error("a sessão anterior sobreviveu à troca de senha")
	}

	// A senha nova entra e a antiga não.
	svcLimpo := servicoDeAuth(t, pool)
	if _, err := svcLimpo.Login(ctx, PedidoLogin{Email: c.Email, Senha: novaSenha}, Origem{}); err != nil {
		t.Errorf("a senha nova não entrou: %v", err)
	}
	if _, err := svcLimpo.Login(ctx, PedidoLogin{Email: c.Email, Senha: senhaDoTeste}, Origem{}); err == nil {
		t.Error("a senha antiga continuou valendo")
	}
}

// Em linguagem de negócio: pedir o link para um e-mail que não existe responde
// exatamente como pedir para um que existe. É o que impede alguém de descobrir
// quem tem conta testando endereços.
func TestEsqueciSenhaNaoRevelaSeOEmailExiste(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	svc := servicoDeAuth(t, pool)

	if err := svc.EsqueciSenha(ctx, "nao-existe-"+uuid.NewString()+"@wh.local", Origem{IP: "203.0.113.11"}); err != nil {
		t.Fatalf("e-mail inexistente deveria responder como os outros: %v", err)
	}
}

// ─────────────────────────── Sessão viva ────────────────────────────────────

// Em linguagem de negócio: desativar a conta de alguém corta o acesso na
// requisição seguinte, não daqui a 15 minutos. O crachá continua impresso, mas a
// portaria passou a recusá-lo.
func TestSessaoDeContaDesativadaMorreNaProximaRequisicao(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "desativar")
	repo := NewRepository(pool)

	if _, err := repo.CarregarSessao(ctx, c.ID); err != nil {
		t.Fatalf("sessão da conta ativa: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE users SET active = false WHERE id = $1`, c.ID); err != nil {
		t.Fatalf("desativando: %v", err)
	}

	_, err := repo.CarregarSessao(ctx, c.ID)
	if codigo(err) != "UNAUTHORIZED" {
		t.Fatalf("code = %q, esperado UNAUTHORIZED", codigo(err))
	}
}

// A matriz do perfil vale na requisição SEGUINTE, sem novo login — é a promessa
// do docs/spec.md §1 que justifica carregar permissão do banco a cada
// requisição, em vez de assá-la no token.
//
// Em linguagem de negócio: tirar o financeiro de um perfil tira o financeiro de
// quem está logado agora, sem esperar ninguém sair e entrar de novo.
func TestMatrizDoPerfilValeNaRequisicaoSeguinte(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "matriz")
	repo := NewRepository(pool)

	// O recurso precisa existir no catálogo antes de ser concedido — é a FK de
	// role_permissions que garante que ninguém conceda um recurso inventado.
	if _, err := pool.Exec(ctx, `
		INSERT INTO resources (code, label, group_label) VALUES ('reservations', 'Reservas', 'Operação')
		ON CONFLICT (code) DO NOTHING`); err != nil {
		t.Fatalf("catálogo: %v", err)
	}

	antes, err := repo.CarregarSessao(ctx, c.ID)
	if err != nil {
		t.Fatalf("sessão: %v", err)
	}
	if antes.Permissoes.Pode("reservations", AcaoVer) {
		t.Fatal("perfil recém-criado já podia ver reservas — permissão por omissão")
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO role_permissions (role_id, resource_code, action, scope)
		VALUES ($1, 'reservations', 'ver', 'own')`, c.RoleID); err != nil {
		t.Fatalf("concedendo permissão: %v", err)
	}

	depois, err := repo.CarregarSessao(ctx, c.ID)
	if err != nil {
		t.Fatalf("sessão depois da concessão: %v", err)
	}
	escopo, ok := depois.Permissoes.Escopo("reservations", AcaoVer)
	if !ok {
		t.Fatal("a permissão concedida não valeu na requisição seguinte")
	}
	if escopo != EscopoOwn {
		t.Errorf("escopo = %q, esperado own", escopo)
	}
}

// O QA achou este: /auth/logout autenticado SEM o cookie de refresh respondia
// 204 sem revogar nada. Na prática o cookie quase sempre vem — mas "quase
// sempre" é a parte perigosa: quando não vem (Path do cookie divergente,
// cliente sem jar, extensão bloqueando), o usuário lê "você saiu" com a sessão
// inteira viva no banco.
//
// Em linguagem de negócio: pedido de saída sem dizer de onde é pedido de sair
// de todos os lugares. A rota é autenticada, então ninguém derruba a sessão de
// outra pessoa.
func TestLogoutSemCookieDerrubaTodasAsSessoesDoUsuario(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "logoutsemcookie")
	svc := servicoDeAuth(t, pool)

	navegador, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: senhaDoTeste}, Origem{})
	if err != nil {
		t.Fatalf("login do navegador: %v", err)
	}
	celular, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: senhaDoTeste}, Origem{})
	if err != nil {
		t.Fatalf("login do celular: %v", err)
	}

	// É o que o middleware injeta na rota autenticada: o token não veio, mas a
	// identidade de quem pediu para sair veio.
	autenticado := WithUser(ctx, &Usuario{ID: c.ID})
	if err := svc.Logout(autenticado, ""); err != nil {
		t.Fatalf("logout sem cookie: %v", err)
	}

	var vivos int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1 AND revoked_at IS NULL`,
		c.ID).Scan(&vivos); err != nil {
		t.Fatalf("contando sessões vivas: %v", err)
	}
	if vivos != 0 {
		t.Errorf("%d sessões continuaram vivas depois do logout: a resposta disse 204 "+
			"e o usuário seguiu logado", vivos)
	}

	for nome, sess := range map[string]Sessao{"navegador": navegador, "celular": celular} {
		if cod := codigo(mustErrDeSessao(svc.Refresh(ctx, sess.RefreshClaro, Origem{}))); cod != "TOKEN_INVALID" {
			t.Errorf("refresh do %s depois do logout = %q, esperado TOKEN_INVALID", nome, cod)
		}
	}
}

// Sem token e sem identidade não há alvo. A resposta continua 204 — o endpoint
// não pode responder diferente conforme exista ou não sessão aberta.
func TestLogoutSemCookieESemIdentidadeNaoDerrubaSessaoAlheia(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "logoutanonimo")
	svc := servicoDeAuth(t, pool)

	sess, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: senhaDoTeste}, Origem{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := svc.Logout(ctx, ""); err != nil {
		t.Fatalf("logout sem identidade: %v", err)
	}
	if _, err := svc.Refresh(ctx, sess.RefreshClaro, Origem{}); err != nil {
		t.Errorf("a sessão foi derrubada por uma requisição sem identidade: %v", err)
	}
}
