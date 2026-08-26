package auth

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

const senhaDoCaso = "senha-de-teste-2026"

func servicoFalso(t *testing.T) (*Service, *bancoFalso) {
	t.Helper()

	hash, err := Hash(senhaDoCaso)
	if err != nil {
		t.Fatalf("hash da senha: %v", err)
	}
	banco := novoBancoFalso(hash)

	svc := NewService(
		NewRepository(banco),
		txFalso{},
		NovoEmissor("segredo-de-teste-com-mais-de-trinta-e-dois-bytes", 15*time.Minute),
		720*time.Hour,
		nil,
	)
	return svc, banco
}

func codigoDoErro(err error) string {
	if err == nil {
		return ""
	}
	return apperr.From(err).Code
}

// ─────────────────────────── Reuso de refresh ───────────────────────────────

// Em linguagem de negócio: quando o comprovante já trocado reaparece, o sistema
// assume roubo e derruba TODA a sessão daquele login — de verdade, no banco. Não
// basta responder 401: se a revogação não fica gravada, o ladrão continua
// renovando o token que roubou, para sempre.
//
// Este teste falhava com a implementação anterior, que revogava a família DENTRO
// da transação e em seguida devolvia TOKEN_REUSED: o rollback desfazia a
// revogação, e a defesa contra roubo de sessão não existia.
func TestReusoRevogaAFamiliaMesmoQueATransacaoSejaDesfeita(t *testing.T) {
	svc, banco := servicoFalso(t)
	ctx := context.Background()

	familia := uuid.New()
	sucessorID := uuid.New()
	revogado := time.Now().Add(-time.Minute)

	// R1: já rotacionado (é a cópia que o ladrão guardou).
	claroDoLadrao, hashDoLadrao, err := NovoRefreshToken()
	if err != nil {
		t.Fatalf("gerando token: %v", err)
	}
	banco.gravarToken(&tokenFalso{
		id: uuid.New(), usuarioID: banco.usuario.ID, familiaID: familia,
		hash: hashDoLadrao, expiraEm: time.Now().Add(720 * time.Hour),
		revogadoEm: &revogado, substPor: &sucessorID,
	})

	// R2: o sucessor vivo, que é com o que o ladrão vem renovando a sessão.
	claroAtual, hashAtual, err := NovoRefreshToken()
	if err != nil {
		t.Fatalf("gerando sucessor: %v", err)
	}
	banco.gravarToken(&tokenFalso{
		id: sucessorID, usuarioID: banco.usuario.ID, familiaID: familia,
		hash: hashAtual, expiraEm: time.Now().Add(720 * time.Hour),
	})

	if _, err := svc.Refresh(ctx, claroDoLadrao, Origem{IP: "203.0.113.66"}); codigoDoErro(err) != "TOKEN_REUSED" {
		t.Fatalf("code = %q, esperado TOKEN_REUSED", codigoDoErro(err))
	}

	// O estado do banco é a prova: a revogação precisa ter sido COMMITADA, não
	// desfeita junto com a transação que devolveu o erro.
	if vivos := banco.vivosDaFamilia(familia); vivos != 0 {
		t.Errorf("sobraram %d tokens vivos na família (esperado 0): a revogação foi desfeita pelo rollback", vivos)
	}
	if banco.revogacoesDeFamilia != 1 {
		t.Errorf("revogações efetivadas = %d, esperado 1", banco.revogacoesDeFamilia)
	}

	// E a consequência que interessa: o token que o ladrão vinha usando morre.
	if codigo := codigoDoErro(mustErr(svc.Refresh(ctx, claroAtual, Origem{IP: "203.0.113.66"}))); codigo != "TOKEN_INVALID" {
		t.Errorf("o token do ladrão respondeu %q; depois da detecção de roubo ele tem de estar morto", codigo)
	}
}

// Em linguagem de negócio: sair do sistema numa aba não pode fazer a outra aba
// acusar "sessão comprometida". Token derrubado sem sucessor é só inválido —
// pedir para entrar de novo, sem alarme e sem derrubar nada além.
func TestTokenRevogadoSemSucessorEhInvalidoENaoAcusaRoubo(t *testing.T) {
	svc, banco := servicoFalso(t)
	ctx := context.Background()

	familia := uuid.New()
	revogado := time.Now().Add(-time.Minute)

	claro, hash, err := NovoRefreshToken()
	if err != nil {
		t.Fatalf("gerando token: %v", err)
	}
	// Revogado pelo logout: revoked_at preenchido, replaced_by nulo.
	banco.gravarToken(&tokenFalso{
		id: uuid.New(), usuarioID: banco.usuario.ID, familiaID: familia,
		hash: hash, expiraEm: time.Now().Add(720 * time.Hour), revogadoEm: &revogado,
	})

	if codigo := codigoDoErro(mustErr(svc.Refresh(ctx, claro, Origem{}))); codigo != "TOKEN_INVALID" {
		t.Fatalf("code = %q, esperado TOKEN_INVALID: logout não é roubo", codigo)
	}
	if banco.revogacoesDeFamilia != 0 {
		t.Errorf("a família foi revogada %d vez(es) por causa de um logout normal", banco.revogacoesDeFamilia)
	}
}

// Token vencido também não é roubo — quem deixou uma aba velha aberta só precisa
// entrar de novo.
func TestRefreshVencidoEhInvalidoENaoRevogaNada(t *testing.T) {
	svc, banco := servicoFalso(t)
	ctx := context.Background()

	familia := uuid.New()
	claro, hash, err := NovoRefreshToken()
	if err != nil {
		t.Fatalf("gerando token: %v", err)
	}
	banco.gravarToken(&tokenFalso{
		id: uuid.New(), usuarioID: banco.usuario.ID, familiaID: familia,
		hash: hash, expiraEm: time.Now().Add(-time.Minute),
	})

	if codigo := codigoDoErro(mustErr(svc.Refresh(ctx, claro, Origem{}))); codigo != "TOKEN_INVALID" {
		t.Fatalf("code = %q, esperado TOKEN_INVALID", codigo)
	}
	if banco.revogacoesDeFamilia != 0 {
		t.Errorf("um token vencido derrubou %d família(s)", banco.revogacoesDeFamilia)
	}
}

// A rotação normal continua funcionando: o token apresentado morre apontando
// para o sucessor, e o sucessor nasce na MESMA família.
func TestRefreshRotacionaEComitaOSucessor(t *testing.T) {
	svc, banco := servicoFalso(t)
	ctx := context.Background()

	familia := uuid.New()
	claro, hash, err := NovoRefreshToken()
	if err != nil {
		t.Fatalf("gerando token: %v", err)
	}
	anterior := banco.gravarToken(&tokenFalso{
		id: uuid.New(), usuarioID: banco.usuario.ID, familiaID: familia,
		hash: hash, expiraEm: time.Now().Add(720 * time.Hour),
	})

	sess, err := svc.Refresh(ctx, claro, Origem{IP: "198.51.100.5"})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if sess.RefreshClaro == claro {
		t.Fatal("o refresh voltou igual: sem rotação, um token roubado vale 30 dias")
	}
	if anterior.revogadoEm == nil || anterior.substPor == nil {
		t.Fatal("o token apresentado não foi revogado com replaced_by")
	}

	novo := banco.token(HashRefreshToken(sess.RefreshClaro))
	if novo == nil {
		t.Fatal("o sucessor não foi commitado")
	}
	if novo.familiaID != familia {
		t.Error("o sucessor nasceu em outra família: deslogar num aparelho derrubaria os outros")
	}
}

// ─────────────────────────── Bloqueio de login ──────────────────────────────

// Em linguagem de negócio: errar a senha cinco vezes tranca QUEM ERROU, não a
// conta. Antes, qualquer um que conhecesse admin@wh.local mantinha o
// administrador para fora do sistema de graça, de IPs variados.
func TestBloqueioDeLoginTrancaOParEmailIPENaoAContaInteira(t *testing.T) {
	svc, banco := servicoFalso(t)
	ctx := context.Background()

	const ipDoAtacante = "203.0.113.66"
	const ipDoDono = "198.51.100.10"

	for i := 1; i <= tentativasDeLogin; i++ {
		_, err := svc.Login(ctx, PedidoLogin{Email: banco.usuario.Email, Senha: "errada"},
			Origem{IP: ipDoAtacante})
		if codigoDoErro(err) != "INVALID_CREDENTIALS" {
			t.Fatalf("tentativa %d: code = %q", i, codigoDoErro(err))
		}
	}

	// O atacante tranca a si mesmo, inclusive com a senha certa — senão saberia
	// que acertou.
	if _, err := svc.Login(ctx, PedidoLogin{Email: banco.usuario.Email, Senha: senhaDoCaso},
		Origem{IP: ipDoAtacante}); err == nil {
		t.Fatal("o IP que errou cinco vezes deveria estar bloqueado")
	}

	// E o dono entra normalmente do IP dele.
	sess, err := svc.Login(ctx, PedidoLogin{Email: banco.usuario.Email, Senha: senhaDoCaso},
		Origem{IP: ipDoDono})
	if err != nil {
		t.Fatalf("o dono ficou trancado fora por erros de outra pessoa: %v", err)
	}
	if sess.Resposta.AccessToken == "" {
		t.Error("login sem access token")
	}
}

// O teto global por e-mail existe para o ataque distribuído — mas não pode valer
// para um IP de onde a conta já entrou, senão ele volta a ser a mesma negação de
// serviço, só que mais cara para o atacante.
func TestTetoGlobalPorEmailIsentaIPQueJaAutenticou(t *testing.T) {
	svc, _ := servicoFalso(t)

	const email = "admin@wh.local"
	const ipDoDono = "198.51.100.10"

	// O dono entrou hoje deste IP.
	svc.registrarAcertoDeLogin(email, ipDoDono)

	// Ataque distribuído: um IP diferente a cada tentativa, para nunca encostar
	// no limite do par e-mail+IP.
	for i := 0; i < tentativasPorEmail+5; i++ {
		svc.registrarFalhaDeLogin(email, "203.0.113."+string(rune('0'+i%10)))
	}

	if !svc.loginBloqueado(email, "192.0.2.99") {
		t.Error("IP desconhecido deveria estar barrado pelo teto global durante o ataque")
	}
	if svc.loginBloqueado(email, ipDoDono) {
		t.Error("o dono, de um IP de onde já entrou, ficou trancado: o bloqueio virou negação de serviço")
	}
}

// ─────────────────────────── broker_id ──────────────────────────────────────

// O painel do corretor liga a conta ao cadastro de corretor por este campo; ele
// vinha nulo fixo, e a coluna existe desde a migration 20260820140000.
func TestLoginDevolveBrokerIDDoBanco(t *testing.T) {
	svc, banco := servicoFalso(t)
	ctx := context.Background()

	corretor := uuid.New()
	banco.usuario.BrokerID = &corretor

	sess, err := svc.Login(ctx, PedidoLogin{Email: banco.usuario.Email, Senha: senhaDoCaso}, Origem{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if sess.Resposta.Usuario.BrokerID == nil {
		t.Fatal("broker_id voltou nulo para uma conta ligada a um corretor")
	}
	if *sess.Resposta.Usuario.BrokerID != corretor {
		t.Errorf("broker_id = %v, esperado %v", *sess.Resposta.Usuario.BrokerID, corretor)
	}
}

// mustErr descarta a sessão e devolve só o erro, para o teste ler o code.
func mustErr(_ Sessao, err error) error { return err }

// ─────────────────────────── Corrida de rotação ─────────────────────────────

// A corrida de verdade está em rotacao_concorrente_integration_test.go, contra
// Postgres. Os dois casos abaixo reproduzem cada desfecho dela sem banco, pelo
// gancho do falso, para a regressão não passar despercebida num CI sem infra.

// Em linguagem de negócio: se, no instante entre conferir o comprovante e
// queimá-lo, alguém já o queimou, então existiam duas cópias do comprovante — e
// duas cópias significam roubo. Sessão inteira derrubada.
func TestRotacaoQuePerdeACorridaEhTratadaComoReuso(t *testing.T) {
	svc, banco := servicoFalso(t)
	ctx := context.Background()

	familia := uuid.New()
	claro, hash, err := NovoRefreshToken()
	if err != nil {
		t.Fatalf("gerando refresh: %v", err)
	}
	alvo := banco.gravarToken(&tokenFalso{
		id:        uuid.New(),
		usuarioID: banco.usuario.ID,
		familiaID: familia,
		hash:      hash,
		expiraEm:  time.Now().Add(time.Hour),
	})

	// A transação concorrente vence a corrida: rotaciona o token e emite o
	// sucessor dela ANTES de o nosso UPDATE avaliar a linha.
	vencedorID := uuid.New()
	banco.aoRotacionar = func() {
		agora := time.Now()
		banco.mu.Lock()
		alvo.revogadoEm, alvo.substPor = &agora, &vencedorID
		banco.mu.Unlock()
		banco.gravarToken(&tokenFalso{
			id:        vencedorID,
			usuarioID: banco.usuario.ID,
			familiaID: familia,
			hash:      "hash-do-sucessor-da-outra-rotacao",
			expiraEm:  time.Now().Add(time.Hour),
		})
	}

	if cod := codigoDoErro(mustErr(svc.Refresh(ctx, claro, Origem{}))); cod != "TOKEN_REUSED" {
		t.Fatalf("code = %q, esperado TOKEN_REUSED: a rotação perdida commitou em silêncio "+
			"e o mesmo token emitiu dois sucessores vivos", cod)
	}
	if vivos := banco.vivosDaFamilia(familia); vivos != 0 {
		t.Errorf("%d tokens da família continuaram vivos: sobrou ramo utilizável "+
			"para quem copiou o token", vivos)
	}
}

// Em linguagem de negócio: sair do sistema numa aba enquanto a outra renova a
// sessão não pode gritar "sessão comprometida". O comprovante foi cancelado sem
// gerar substituto — isso é sessão encerrada, não cópia em circulação.
func TestRotacaoContraLogoutSimultaneoEhTokenInvalidoENaoRoubo(t *testing.T) {
	svc, banco := servicoFalso(t)
	ctx := context.Background()

	familia := uuid.New()
	claro, hash, err := NovoRefreshToken()
	if err != nil {
		t.Fatalf("gerando refresh: %v", err)
	}
	alvo := banco.gravarToken(&tokenFalso{
		id:        uuid.New(),
		usuarioID: banco.usuario.ID,
		familiaID: familia,
		hash:      hash,
		expiraEm:  time.Now().Add(time.Hour),
	})

	// O logout da outra aba revoga SEM sucessor no meio do caminho.
	banco.aoRotacionar = func() {
		agora := time.Now()
		banco.mu.Lock()
		alvo.revogadoEm = &agora
		banco.mu.Unlock()
	}

	if cod := codigoDoErro(mustErr(svc.Refresh(ctx, claro, Origem{}))); cod != "TOKEN_INVALID" {
		t.Fatalf("code = %q, esperado TOKEN_INVALID: logout numa aba não pode acusar "+
			"roubo na outra", cod)
	}
}

// ─────────────────────────── Logout sem cookie ──────────────────────────────

// Em linguagem de negócio: quem clica em "sair" e, por qualquer motivo, não
// manda o comprovante junto, sai de todos os lugares. Antes, essa requisição
// respondia "saiu" e não derrubava nada — o usuário acreditava ter saído com a
// sessão intacta no banco.
func TestLogoutSemTokenDerrubaTodasAsSessoesDoUsuarioAutenticado(t *testing.T) {
	svc, banco := servicoFalso(t)
	ctx := context.Background()

	// Duas famílias vivas: o navegador e o celular do mesmo usuário.
	navegador, celular := uuid.New(), uuid.New()
	for _, familia := range []uuid.UUID{navegador, celular} {
		_, hash, err := NovoRefreshToken()
		if err != nil {
			t.Fatalf("gerando refresh: %v", err)
		}
		banco.gravarToken(&tokenFalso{
			id:        uuid.New(),
			usuarioID: banco.usuario.ID,
			familiaID: familia,
			hash:      hash,
			expiraEm:  time.Now().Add(time.Hour),
		})
	}

	autenticado := WithUser(ctx, &Usuario{ID: banco.usuario.ID})
	if err := svc.Logout(autenticado, ""); err != nil {
		t.Fatalf("logout sem token: %v", err)
	}
	for nome, familia := range map[string]uuid.UUID{"navegador": navegador, "celular": celular} {
		if vivos := banco.vivosDaFamilia(familia); vivos != 0 {
			t.Errorf("família %s ficou com %d token vivo depois do logout", nome, vivos)
		}
	}
}

// Sem token e sem identidade não há alvo — e a resposta continua 204, para o
// endpoint não virar oráculo de "existe sessão aqui".
func TestLogoutSemTokenESemIdentidadeNaoDerrubaNada(t *testing.T) {
	svc, banco := servicoFalso(t)

	familia := uuid.New()
	_, hash, err := NovoRefreshToken()
	if err != nil {
		t.Fatalf("gerando refresh: %v", err)
	}
	banco.gravarToken(&tokenFalso{
		id:        uuid.New(),
		usuarioID: banco.usuario.ID,
		familiaID: familia,
		hash:      hash,
		expiraEm:  time.Now().Add(time.Hour),
	})

	if err := svc.Logout(context.Background(), ""); err != nil {
		t.Fatalf("logout sem identidade: %v", err)
	}
	if vivos := banco.vivosDaFamilia(familia); vivos != 1 {
		t.Errorf("tokens vivos = %d, esperado 1: requisição sem identidade derrubou sessão alheia", vivos)
	}
}
