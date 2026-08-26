package users

import (
	"context"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// SessoesDoUsuario é o que o service precisa do pacote auth para derrubar
// sessões. Interface estreita: trocar a senha e desativar a conta exigem que o
// acesso caia no mesmo instante, e nada além disso.
type SessoesDoUsuario interface {
	RevogarSessoesDoUsuario(ctx context.Context, usuarioID uuid.UUID) error
}

// Repositorio é o repositório visto pelo service. Interface, e não o tipo
// concreto, para as travas de privilégio serem testáveis sem Postgres: um
// defeito de autorização precisa de teste que rode em `go test ./...`, e não só
// no ambiente de integração que nem sempre sobe.
type Repositorio interface {
	Listar(ctx context.Context, f Filtro) ([]auth.LinhaUsuario, int64, error)
	Buscar(ctx context.Context, id uuid.UUID) (auth.LinhaUsuario, error)
	Criar(ctx context.Context, propriedadeID uuid.UUID, c Criar, senhaHash string) (uuid.UUID, error)
	Atualizar(ctx context.Context, id uuid.UUID, a Atualizar, senhaHash *string) error
	Desativar(ctx context.Context, id uuid.UUID) error
	PropriedadePadrao(ctx context.Context) (uuid.UUID, error)
	ContarAdministradoresAtivos(ctx context.Context, exceto uuid.UUID) (int, error)
	TravarAdministradores(ctx context.Context) error
	PermissoesDoPapel(ctx context.Context, roleID uuid.UUID) ([]auth.Permissao, error)
	RegistrarAuditoria(ctx context.Context, a Auditoria) error
}

type Service struct {
	repo    Repositorio
	sessoes SessoesDoUsuario
	tx      auth.Transacionador
}

func NewService(repo Repositorio, sessoes SessoesDoUsuario, tx auth.Transacionador) *Service {
	return &Service{repo: repo, sessoes: sessoes, tx: tx}
}

// Listar aplica o escopo antes de consultar: com `own`, o WHERE já nasce
// restrito, e o `total` da paginação conta só o que o usuário pode ver.
func (s *Service) Listar(ctx context.Context, f Filtro) ([]auth.UsuarioResposta, int64, error) {
	if id, restrito := s.escopoRestrito(ctx, auth.AcaoVer); restrito {
		f.ApenasID = &id
	}

	linhas, total, err := s.repo.Listar(ctx, f)
	if err != nil {
		return nil, 0, err
	}

	out := make([]auth.UsuarioResposta, 0, len(linhas))
	for _, l := range linhas {
		out = append(out, auth.UsuarioDaLinha(l))
	}
	return out, total, nil
}

func (s *Service) Buscar(ctx context.Context, id uuid.UUID) (auth.UsuarioResposta, error) {
	if err := s.exigirEscopo(ctx, auth.AcaoVer, id); err != nil {
		return auth.UsuarioResposta{}, err
	}
	linha, err := s.repo.Buscar(ctx, id)
	if err != nil {
		return auth.UsuarioResposta{}, err
	}
	return auth.UsuarioDaLinha(linha), nil
}

func (s *Service) Criar(ctx context.Context, c Criar) (auth.UsuarioResposta, error) {
	c.Normalizar()

	// Criar usuário é atribuir papel: vale o mesmo teto de privilégio do PATCH.
	// Sem isto, quem tem `users:criar` cadastraria uma conta com o perfil de
	// administrador, entraria nela com a senha que acabou de escolher e a trava
	// do PATCH viraria enfeite. A checagem vem ANTES do argon2 para uma
	// requisição negada não custar o hash.
	if ator, ok := auth.UserFrom(ctx); ok {
		if _, err := s.autorizarPapelAtribuido(ctx, ator, c.RoleID); err != nil {
			return auth.UsuarioResposta{}, err
		}
	}

	hash, err := auth.Hash(c.Senha)
	if err != nil {
		return auth.UsuarioResposta{}, apperr.Internal.WithCause(err)
	}

	// O novo usuário nasce na propriedade de quem o criou; sem contexto de
	// usuário (seed, script), cai na única propriedade ativa.
	propriedade := uuid.Nil
	if u, ok := auth.UserFrom(ctx); ok {
		propriedade = u.PropertyID
	}
	if propriedade == uuid.Nil {
		if propriedade, err = s.repo.PropriedadePadrao(ctx); err != nil {
			return auth.UsuarioResposta{}, err
		}
	}

	var criado auth.LinhaUsuario
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		id, err := s.repo.Criar(ctx, propriedade, c, hash)
		if err != nil {
			return err
		}
		criado, err = s.repo.Buscar(ctx, id)
		return err
	})
	if err != nil {
		return auth.UsuarioResposta{}, err
	}
	return auth.UsuarioDaLinha(criado), nil
}

// Substituir é o PUT: o corpo já chega normalizado para substituição integral.
func (s *Service) Substituir(ctx context.Context, id uuid.UUID, a Atualizar) (auth.UsuarioResposta, error) {
	return s.aplicar(ctx, id, a.ParaSubstituicao())
}

// Atualizar é o PATCH: só o que veio muda.
func (s *Service) Atualizar(ctx context.Context, id uuid.UUID, a Atualizar) (auth.UsuarioResposta, error) {
	return s.aplicar(ctx, id, a)
}

func (s *Service) aplicar(ctx context.Context, id uuid.UUID, a Atualizar) (auth.UsuarioResposta, error) {
	if err := s.exigirEscopo(ctx, auth.AcaoEditar, id); err != nil {
		return auth.UsuarioResposta{}, err
	}
	a.Normalizar()

	// Confirma a existência antes de qualquer regra: 404 vem antes de 422.
	alvo, err := s.repo.Buscar(ctx, id)
	if err != nil {
		return auth.UsuarioResposta{}, err
	}

	// Travas de escalada de privilégio (ver autoridade.go). Rodam antes de
	// qualquer escrita e antes do argon2 da senha nova.
	mud, err := s.autorizarMudancasSensiveis(ctx, alvo, a)
	if err != nil {
		return auth.UsuarioResposta{}, err
	}

	desativando := false
	if v, ok := a.Ativo.Definido(); ok && !v {
		desativando = true
		if err := s.naoDesativarAPropriaConta(ctx, id); err != nil {
			return auth.UsuarioResposta{}, err
		}
	}

	var senhaHash *string
	if senha, ok := a.Senha.Definido(); ok {
		h, err := auth.Hash(senha)
		if err != nil {
			return auth.UsuarioResposta{}, apperr.Internal.WithCause(err)
		}
		senhaHash = &h
	}

	var atualizado auth.LinhaUsuario
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		// A trava do último administrador roda AQUI DENTRO, e não lá em cima:
		// contar fora da transação é contar um número que outra requisição já
		// pode ter invalidado antes do UPDATE (ver exigirOutroAdministrador).
		if desativando || mud.rebaixouAdministrador {
			campo := "role_id"
			if desativando {
				campo = "active"
			}
			if err := s.exigirOutroAdministrador(ctx, id, campo); err != nil {
				return err
			}
		}

		if err := s.repo.Atualizar(ctx, id, a, senhaHash); err != nil {
			return err
		}
		// Trocar a senha, trocar o e-mail ou desativar a conta tem de derrubar o
		// acesso agora, e não daqui a 30 dias, quando o refresh vencer. O e-mail
		// entra na lista porque é a credencial de recuperação: mudou o endereço,
		// mudou quem consegue redefinir a senha daquela conta.
		if senhaHash != nil || desativando || mud.trocouEmail {
			if err := s.sessoes.RevogarSessoesDoUsuario(ctx, id); err != nil {
				return err
			}
		}
		// Auditoria dentro da MESMA transação: se o registro falhar, a troca de
		// e-mail não acontece. Trocar a credencial de recuperação sem deixar
		// rastro é exatamente o que o atacante quer.
		if mud.trocouEmail {
			if err := s.repo.RegistrarAuditoria(ctx, auditoriaDeEmail(ctx, alvo, mud)); err != nil {
				return err
			}
		}

		var err error
		atualizado, err = s.repo.Buscar(ctx, id)
		return err
	})
	if err != nil {
		return auth.UsuarioResposta{}, err
	}
	return auth.UsuarioDaLinha(atualizado), nil
}

// Desativar é o DELETE do contrato: mesmo efeito de PATCH {active:false}.
func (s *Service) Desativar(ctx context.Context, id uuid.UUID) error {
	if err := s.exigirEscopo(ctx, auth.AcaoExcluir, id); err != nil {
		return err
	}
	if _, err := s.repo.Buscar(ctx, id); err != nil {
		return err
	}
	if err := s.naoDesativarAPropriaConta(ctx, id); err != nil {
		return err
	}

	return s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.exigirOutroAdministrador(ctx, id, "active"); err != nil {
			return err
		}
		if err := s.repo.Desativar(ctx, id); err != nil {
			return err
		}
		return s.sessoes.RevogarSessoesDoUsuario(ctx, id)
	})
}

// naoDesativarAPropriaConta é a primeira das duas travas da desativação. Não
// tem corrida: quem é o ator não muda no meio da requisição.
func (s *Service) naoDesativarAPropriaConta(ctx context.Context, id uuid.UUID) error {
	if u, ok := auth.UserFrom(ctx); ok && u.ID == id {
		return apperr.Validation(map[string]string{
			"active": "você não pode desativar a própria conta.",
		})
	}
	return nil
}

// exigirOutroAdministrador é a trava do "último administrador" — a que impede a
// instalação de ficar sem ninguém capaz de consertar permissão. Uma vez sem
// administrador, não existe caminho pela API para voltar atrás: só SQL na mão.
//
// Ela SÓ vale dentro da transação que grava, e nesta ordem: trava primeiro,
// conta depois. A versão anterior contava fora da transação e nunca reconferia,
// e o defeito era reproduzível com duas requisições simultâneas — dois
// administradores se rebaixando ao mesmo tempo, os dois lendo "sobra 1", os
// dois gravando, zero administradores no fim.
//
// A trava é `pg_advisory_xact_lock` numa chave fixa, e não `SELECT ... FOR
// UPDATE` nas contas de administrador: o que precisa ser serializado é a
// CONTAGEM, e as linhas que ela olha mudam conforme o papel de cada um — travar
// as linhas de hoje não impede outra transação de criar o problema numa linha
// que a primeira consulta nem enxergou. A chave é única para todo o sistema, o
// escopo é a transação (some no commit ou no rollback, sem risco de vazar) e a
// espera é curta: só colide quem mexe em administrador.
func (s *Service) exigirOutroAdministrador(ctx context.Context, exceto uuid.UUID, campo string) error {
	if err := s.repo.TravarAdministradores(ctx); err != nil {
		return err
	}

	restantes, err := s.repo.ContarAdministradoresAtivos(ctx, exceto)
	if err != nil {
		return err
	}
	if restantes == 0 {
		return apperr.Validation(map[string]string{
			campo: "este é o último usuário ativo capaz de administrar o sistema.",
		})
	}
	return nil
}

// escopoRestrito devolve o id do usuário da requisição quando o escopo é `own`.
func (s *Service) escopoRestrito(ctx context.Context, acao string) (uuid.UUID, bool) {
	if auth.ScopeOf(ctx, auth.RecursoUsuarios, acao) != auth.EscopoOwn {
		return uuid.Nil, false
	}
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return uuid.Nil, false
	}
	return u.ID, true
}

// exigirEscopo aplica `own` no acesso a um registro específico. Devolve 404, e
// não 403: fora do escopo, o recurso não existe do ponto de vista do usuário —
// um 403 confirmaria que o id é de alguém.
func (s *Service) exigirEscopo(ctx context.Context, acao string, id uuid.UUID) error {
	proprio, restrito := s.escopoRestrito(ctx, acao)
	if restrito && proprio != id {
		return apperr.NotFound("Usuário")
	}
	return nil
}

// Meta monta o bloco de paginação da listagem.
func Meta(pagina, porPagina int, total int64) httpx.Meta {
	return httpx.Meta{Page: pagina, PerPage: porPagina, Total: total}
}
