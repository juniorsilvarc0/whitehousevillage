package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

const (
	// Contagem principal do login: e-mail + IP. 5 erros em 15 min trancam ESTE
	// par por 15 min (docs/spec.md §1).
	//
	// A contagem só por e-mail, que existia antes, era negação de serviço de
	// graça: quem conhece admin@wh.local errava cinco senhas de um IP qualquer e
	// mantinha o administrador trancado para sempre, sem custo nenhum. Ninguém
	// mais tranca a conta de outra pessoa a partir do seu próprio IP.
	tentativasDeLogin = 5
	janelaDeLogin     = 15 * time.Minute

	// Teto global por e-mail, para o ataque distribuído que dilui cinco
	// tentativas por IP entre centenas de IPs. É alto de propósito: quem faz o
	// trabalho de barrar força bruta é o par e-mail+IP, e este teto só entra
	// quando ele não basta.
	tentativasPorEmail = 20

	// O teto global NÃO se aplica a um IP de onde a conta já entrou com sucesso
	// dentro deste prazo. É essa isenção que impede o ataque distribuído de
	// trancar o dono da conta na própria casa: o atacante consegue no máximo
	// barrar IPs desconhecidos.
	janelaDeIPConhecido = 7 * 24 * time.Hour

	// Prazo da revogação de emergência disparada pela detecção de reuso. Curto,
	// porque roda depois de a resposta já estar decidida.
	prazoDaRevogacao = 5 * time.Second

	// O disparo de recuperação é limitado por e-mail e por IP: sem isso o
	// endpoint vira ferramenta de spam com o remetente da casa.
	disparosDeReset = 3
	janelaDeReset   = 15 * time.Minute

	// Token de recuperação: uso único, 1 h (contrato).
	validadeDoReset = time.Hour

	// Piso de tempo do /auth/password/forgot. A resposta é sempre 202; se o
	// caminho "e-mail existe" (grava token, dispara e-mail) demorasse mais que o
	// caminho "não existe", o cronômetro entregaria quem tem conta.
	pisoDoForgot = 150 * time.Millisecond
)

// Transacionador é o TxManager visto pelo service. Interface, e não o tipo
// concreto, para o service ser testável sem banco.
type Transacionador interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Notificador entrega o link de recuperação. A implementação real (e-mail) é do
// agente de integrações; a daqui é o contrato mínimo que o service exige.
type Notificador interface {
	EnviarRecuperacaoDeSenha(ctx context.Context, email, token string) error
}

// NotificadorDeLog é o fallback de desenvolvimento: registra o token no log em
// vez de mandar e-mail. NÃO serve para produção — e o service avisa no boot.
type NotificadorDeLog struct{}

func (NotificadorDeLog) EnviarRecuperacaoDeSenha(ctx context.Context, email, token string) error {
	slog.WarnContext(ctx, "recuperação de senha sem provedor de e-mail: token só no log",
		"email", email, "token", token)
	return nil
}

type Service struct {
	repo        *Repository
	tx          Transacionador
	jwt         *Emissor
	refreshTTL  time.Duration
	notificador Notificador

	// Contadores em memória — insuficientes para multi-instância, ver o
	// comentário do httpx.Limitador.
	tentativas *httpx.Limitador // falhas por e-mail+IP
	porEmail   *httpx.Limitador // teto global do e-mail, contra ataque distribuído
	conhecidos *httpx.Limitador // IPs de onde a conta já entrou (isentos do teto)
	disparos   *httpx.Limitador
}

func NewService(repo *Repository, tx Transacionador, emissor *Emissor, refreshTTL time.Duration, notificador Notificador) *Service {
	if notificador == nil {
		notificador = NotificadorDeLog{}
	}
	return &Service{
		repo:        repo,
		tx:          tx,
		jwt:         emissor,
		refreshTTL:  refreshTTL,
		notificador: notificador,
		tentativas:  httpx.NovoLimitador(tentativasDeLogin, janelaDeLogin),
		porEmail:    httpx.NovoLimitador(tentativasPorEmail, janelaDeLogin),
		conhecidos:  httpx.NovoLimitador(1, janelaDeIPConhecido),
		disparos:    httpx.NovoLimitador(disparosDeReset, janelaDeReset),
	}
}

// Origem descreve de onde veio a requisição, para carimbar a sessão.
type Origem struct {
	UserAgent string
	IP        string
}

// ─────────────────────────── Login ──────────────────────────────────────────

// Login autentica e abre uma família de sessão.
//
// Todos os caminhos de falha devolvem INVALID_CREDENTIALS, sem details: e-mail
// inexistente, senha errada, conta desativada e e-mail bloqueado por tentativas
// são indistinguíveis para quem chama. E todos gastam o mesmo tempo — o
// caminho "e-mail não existe" roda um argon2 contra o hash dummy de propósito.
func (s *Service) Login(ctx context.Context, p PedidoLogin, o Origem) (Sessao, error) {
	p.NormalizarEmail()

	if s.loginBloqueado(p.Email, o.IP) {
		// Ainda assim gasta o tempo do argon2: responder rápido aqui revelaria
		// que este e-mail específico está bloqueado, ou seja, que ele existe.
		GastarTempoDeVerificacao(p.Senha)
		return Sessao{}, apperr.InvalidCredentials
	}

	cred, err := s.repo.BuscarCredenciais(ctx, p.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		GastarTempoDeVerificacao(p.Senha)
		s.registrarFalhaDeLogin(p.Email, o.IP)
		return Sessao{}, apperr.InvalidCredentials
	}
	if err != nil {
		return Sessao{}, err
	}

	ok, err := Verify(cred.PasswordHash, p.Senha)
	if err != nil {
		// Hash corrompido no banco é problema nosso, não credencial errada.
		return Sessao{}, apperr.Internal.WithCause(err)
	}
	if !ok || !cred.Ativo {
		s.registrarFalhaDeLogin(p.Email, o.IP)
		return Sessao{}, apperr.InvalidCredentials
	}

	s.registrarAcertoDeLogin(p.Email, o.IP)

	var sess Sessao
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		// Família nova: cada login é uma árvore independente de sessões, então
		// deslogar no celular não derruba o navegador.
		familia := uuid.New()
		var err error
		sess, err = s.emitirSessao(ctx, cred.ID, familia, o)
		if err != nil {
			return err
		}
		return s.repo.RegistrarLogin(ctx, cred.ID)
	})
	if err != nil {
		return Sessao{}, err
	}
	return sess, nil
}

// ─────────────────────────── Refresh ────────────────────────────────────────

// Refresh rotaciona o token: revoga o apresentado e emite o sucessor na mesma
// família.
//
// Reapresentar um token já rotacionado só acontece com cliente bugado ou com
// token roubado, e o contrato trata os dois como roubo: revoga a família inteira
// e devolve TOKEN_REUSED. O dono legítimo refaz o login; o ladrão fica com um
// token morto.
func (s *Service) Refresh(ctx context.Context, tokenClaro string, o Origem) (Sessao, error) {
	tokenClaro = strings.TrimSpace(tokenClaro)
	if tokenClaro == "" {
		return Sessao{}, apperr.TokenInvalid
	}

	var sess Sessao
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		atual, err := s.repo.BuscarRefresh(ctx, HashRefreshToken(tokenClaro))
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.TokenInvalid
		}
		if err != nil {
			return err
		}

		// Reuso só sai daqui como sinal; quem revoga é o chamador, DEPOIS que
		// esta transação for desfeita. Ver reusoDetectado.
		if atual.Rotacionado() {
			return reusoDetectado{usuarioID: atual.UsuarioID, familiaID: atual.FamiliaID}
		}
		// Revogado sem sucessor não é roubo: é logout, troca de senha ou conta
		// desativada. Devolve TOKEN_INVALID e não mexe na família — o contrário
		// fazia sair numa aba acusar sessão comprometida na outra.
		if atual.Revogado() || atual.Expirado(time.Now()) {
			return apperr.TokenInvalid
		}

		// Carregar a sessão aqui recusa o refresh de conta desativada — o
		// DELETE de usuário revoga as famílias, mas a checagem fecha a corrida.
		if _, err := s.repo.CarregarSessao(ctx, atual.UsuarioID); err != nil {
			return apperr.TokenInvalid
		}

		sess, err = s.emitirSessao(ctx, atual.UsuarioID, atual.FamiliaID, o)
		if err != nil {
			return err
		}

		// O UPDATE condicional é o árbitro da corrida, e não o SELECT lá em
		// cima: entre ler o token e queimá-lo, outra requisição com o MESMO
		// token pode ter rotacionado. Perder a corrida vai pelo mesmo caminho
		// da reapresentação — revoga a família e devolve TOKEN_REUSED. Sem
		// isso, o ladrão que disparasse duas requisições em paralelo saía com
		// um ramo próprio e permanente da família e a vítima nunca via
		// TOKEN_REUSED, porque o ramo dela seguia rotacionando normalmente.
		//
		// O preço: um cliente que dispara dois refresh em paralelo com o mesmo
		// cookie (duas abas, clique duplo) derruba a própria sessão. É o mesmo
		// preço da detecção de reuso em si — de fora, "duas abas" e "duas
		// cópias" são indistinguíveis —, e cabe ao painel manter uma única
		// chamada de refresh em voo.
		resultado, err := s.repo.RotacionarRefresh(ctx, atual.ID, sess.novoTokenID)
		if err != nil {
			return err
		}
		switch resultado {
		case RotacaoAplicada:
			return nil
		case RotacaoPerdida:
			return reusoDetectado{usuarioID: atual.UsuarioID, familiaID: atual.FamiliaID}
		default:
			// Revogado sem sucessor no meio do caminho: logout ou troca de
			// senha em paralelo. Sessão morta, não roubo — acusar reuso aqui
			// faria sair numa aba gritar "sessão comprometida" na outra.
			return apperr.TokenInvalid
		}
	})

	var reuso reusoDetectado
	if errors.As(err, &reuso) {
		s.revogarFamiliaComprometida(ctx, reuso)
		return Sessao{}, apperr.TokenReused
	}
	if err != nil {
		return Sessao{}, err
	}
	return sess, nil
}

// reusoDetectado atravessa a transação de rotação carregando a família a
// derrubar.
//
// Existe porque a revogação NÃO pode acontecer lá dentro: o TxManager faz
// rollback em qualquer erro, então gravar a revogação e devolver TOKEN_REUSED na
// mesma unidade desfazia a própria revogação — o log dizia "família revogada", o
// cliente recebia 401 e no banco continuava tudo vivo, com o ladrão rotacionando
// o token roubado à vontade. Escrita de segurança nunca compartilha unidade
// transacional com o erro que ela justifica.
type reusoDetectado struct {
	usuarioID uuid.UUID
	familiaID uuid.UUID
}

func (reusoDetectado) Error() string { return "refresh token reutilizado" }

// revogarFamiliaComprometida derruba a família numa transação PRÓPRIA, que
// commita, e só então o chamador devolve o erro.
//
// O contexto é desligado do cancelamento do cliente de propósito: quem roubou o
// token pode cortar a conexão assim que vê o 401, e a revogação tem de chegar ao
// banco mesmo assim. Pressupõe ctx sem transação aberta — é o caso do handler,
// que é o único chamador de Refresh.
func (s *Service) revogarFamiliaComprometida(ctx context.Context, r reusoDetectado) {
	ctx, cancelar := context.WithTimeout(context.WithoutCancel(ctx), prazoDaRevogacao)
	defer cancelar()

	if err := s.tx.Do(ctx, func(ctx context.Context) error {
		return s.repo.RevogarFamilia(ctx, r.familiaID)
	}); err != nil {
		// A resposta ao cliente continua 401, mas falhar aqui significa sessão
		// comprometida ainda viva: é alarme, não aviso.
		slog.ErrorContext(ctx, "falha ao revogar família de refresh token reutilizado",
			"user_id", r.usuarioID, "family_id", r.familiaID, "err", err)
		return
	}
	slog.WarnContext(ctx, "refresh token reutilizado: família revogada",
		"user_id", r.usuarioID, "family_id", r.familiaID)
}

// ─────────────────────────── Logout ─────────────────────────────────────────

// Logout revoga a família inteira, não só o token apresentado: derrubar só o
// último elo deixaria vivo qualquer sucessor já emitido.
//
// Sem token no cookie nem no corpo, o alvo passa a ser TODAS as famílias ativas
// do usuário autenticado. Antes, esse caminho respondia 204 sem revogar coisa
// alguma: quem clicou em "sair" e teve o cookie perdido (Path errado, cookie
// bloqueado, cliente sem jar) recebia a confirmação de saída com a sessão
// intacta no banco — a pior combinação possível, porque o usuário acredita que
// saiu. Sair de tudo é a leitura correta de um pedido de saída sem alvo
// declarado; a rota é autenticada, então o usuário só derruba as próprias
// sessões.
//
// Idempotente por contrato — token desconhecido, já revogado ou usuário sem
// sessão aberta também terminam em 204. Um logout que falha deixa o usuário
// achando que continua logado, o que é pior que revogar duas vezes.
func (s *Service) Logout(ctx context.Context, tokenClaro string) error {
	tokenClaro = strings.TrimSpace(tokenClaro)
	if tokenClaro == "" {
		u, autenticado := UserFrom(ctx)
		if !autenticado {
			// Sem token e sem identidade não há o que revogar; a resposta
			// continua 204 para não virar oráculo de sessão.
			return nil
		}
		return s.repo.RevogarSessoesDoUsuario(ctx, u.ID)
	}

	atual, err := s.repo.BuscarRefresh(ctx, HashRefreshToken(tokenClaro))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.repo.RevogarFamilia(ctx, atual.FamiliaID)
}

// ─────────────────────────── Me ─────────────────────────────────────────────

// Me devolve a identidade da requisição e a matriz achatada. A matriz sai do
// contexto (já carregada pelo middleware), então não custa consulta extra.
func (s *Service) Me(ctx context.Context) (RespostaMe, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return RespostaMe{}, apperr.Unauthorized
	}

	linha, err := s.repo.BuscarUsuario(ctx, u.ID)
	if err != nil {
		return RespostaMe{}, err
	}

	perms := u.Permissoes.Lista()
	if perms == nil {
		perms = []Permissao{}
	}
	return RespostaMe{Usuario: UsuarioDaLinha(linha), Permissoes: perms}, nil
}

// ─────────────────────────── Recuperação de senha ───────────────────────────

// EsqueciSenha responde igual exista ou não a conta, e no mesmo tempo.
//
// O piso de tempo é o que fecha o canal lateral: sem ele, "e-mail existe" faz
// INSERT e chamada ao provedor de e-mail (dezenas de ms) e "não existe" volta
// num SELECT vazio (menos de um ms), e a diferença é medível de fora.
func (s *Service) EsqueciSenha(ctx context.Context, email string, o Origem) error {
	inicio := time.Now()
	defer func() { aguardarPiso(inicio, pisoDoForgot) }()

	email = strings.ToLower(strings.TrimSpace(email))
	if !s.disparos.Permitir("forgot:email:"+email) || !s.disparos.Permitir("forgot:ip:"+o.IP) {
		return apperr.RateLimited
	}

	usuarioID, err := s.repo.BuscarIDPorEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // 202 mesmo assim
	}
	if err != nil {
		return err
	}

	claro, hash, err := NovoTokenDeReset()
	if err != nil {
		return apperr.Internal.WithCause(err)
	}

	err = s.tx.Do(ctx, func(ctx context.Context) error {
		// Pedir um link novo invalida o anterior: dois links válidos ao mesmo
		// tempo dobram a janela de quem interceptou o primeiro.
		if err := s.repo.InvalidarResetsPendentes(ctx, usuarioID); err != nil {
			return err
		}
		return s.repo.CriarReset(ctx, usuarioID, hash, time.Now().Add(validadeDoReset))
	})
	if err != nil {
		return err
	}

	if err := s.notificador.EnviarRecuperacaoDeSenha(ctx, email, claro); err != nil {
		// Falha no envio não muda a resposta (continua 202): o código do erro
		// diria que o e-mail existe.
		slog.ErrorContext(ctx, "falha ao enviar recuperação de senha", "err", err)
	}
	return nil
}

// TrocarSenha consome o token de uso único e derruba TODAS as sessões: se a
// recuperação aconteceu porque a conta estava comprometida, o invasor perde o
// acesso no mesmo instante.
func (s *Service) TrocarSenha(ctx context.Context, p PedidoTrocarSenha) error {
	hash, err := Hash(p.Senha)
	if err != nil {
		return apperr.Internal.WithCause(err)
	}

	// 400, e não 401: o token do e-mail não é credencial de sessão, e responder
	// 401 faria o painel achar que precisa relogar em vez de pedir outro link.
	tokenInvalido := apperr.TokenInvalid.WithStatus(400)

	return s.tx.Do(ctx, func(ctx context.Context) error {
		reset, err := s.repo.BuscarReset(ctx, HashRefreshToken(p.Token))
		if errors.Is(err, pgx.ErrNoRows) {
			return tokenInvalido
		}
		if err != nil {
			return err
		}
		if reset.UsadoEm != nil || time.Now().After(reset.ExpiraEm) {
			return tokenInvalido
		}

		// O UPDATE condicional é quem decide: duas requisições simultâneas com o
		// mesmo token, só uma afeta linha.
		consumido, err := s.repo.ConsumirReset(ctx, reset.ID)
		if err != nil {
			return err
		}
		if !consumido {
			return tokenInvalido
		}

		if err := s.repo.AtualizarSenha(ctx, reset.UsuarioID, hash); err != nil {
			return err
		}
		return s.repo.RevogarSessoesDoUsuario(ctx, reset.UsuarioID)
	})
}

// ─────────────────────────── Auxiliares ─────────────────────────────────────

// loginBloqueado decide se esta combinação e-mail+IP está trancada.
//
// São duas contagens com pesos diferentes. A do par e-mail+IP é dura e vale para
// todo mundo — é ela que barra a força bruta. A do e-mail sozinho é um teto
// alto que só se aplica a IP desconhecido; sem essa isenção, o bloqueio por
// e-mail seria negação de serviço contra qualquer conta cujo endereço vazou.
func (s *Service) loginBloqueado(email, ip string) bool {
	if s.tentativas.Bloqueado(chaveDoPar(email, ip)) {
		return true
	}
	return s.porEmail.Bloqueado(chaveDoEmail(email)) &&
		!s.conhecidos.Marcado(chaveDoIPConhecido(email, ip))
}

// registrarFalhaDeLogin conta o erro nas duas escalas.
func (s *Service) registrarFalhaDeLogin(email, ip string) {
	s.tentativas.Registrar(chaveDoPar(email, ip))
	s.porEmail.Registrar(chaveDoEmail(email))
}

// registrarAcertoDeLogin zera o histórico e passa a reconhecer o IP: quem
// lembrou a senha não carrega o erro, e o IP de onde a conta entrou fica isento
// do teto global na próxima semana.
func (s *Service) registrarAcertoDeLogin(email, ip string) {
	s.tentativas.Limpar(chaveDoPar(email, ip))
	s.porEmail.Limpar(chaveDoEmail(email))
	s.conhecidos.Marcar(chaveDoIPConhecido(email, ip))
}

func chaveDoPar(email, ip string) string         { return "login:par:" + email + "|" + ip }
func chaveDoEmail(email string) string           { return "login:email:" + email }
func chaveDoIPConhecido(email, ip string) string { return "login:ok:" + email + "|" + ip }

// emitirSessao assina o access token e grava o refresh da família informada.
func (s *Service) emitirSessao(ctx context.Context, usuarioID, familiaID uuid.UUID, o Origem) (Sessao, error) {
	linha, err := s.repo.BuscarUsuario(ctx, usuarioID)
	if err != nil {
		return Sessao{}, err
	}

	access, expiraEm, err := s.jwt.Issue(usuarioID, linha.RoleCode)
	if err != nil {
		return Sessao{}, apperr.Internal.WithCause(err)
	}

	claro, hash, err := NovoRefreshToken()
	if err != nil {
		return Sessao{}, apperr.Internal.WithCause(err)
	}

	novoID, err := s.repo.CriarRefresh(ctx, usuarioID, familiaID, hash, time.Now().Add(s.refreshTTL), o.UserAgent, o.IP)
	if err != nil {
		return Sessao{}, err
	}

	return Sessao{
		Resposta: RespostaSessao{
			AccessToken: access,
			ExpiresIn:   int(time.Until(expiraEm).Seconds()),
			Usuario:     UsuarioDaLinha(linha),
		},
		RefreshClaro: claro,
		RefreshTTL:   s.refreshTTL,
		novoTokenID:  novoID,
	}, nil
}

// aguardarPiso segura a resposta até completar a duração mínima.
func aguardarPiso(inicio time.Time, piso time.Duration) {
	if restante := piso - time.Since(inicio); restante > 0 {
		time.Sleep(restante)
	}
}
