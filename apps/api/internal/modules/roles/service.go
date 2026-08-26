package roles

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Repositorio é o repositório visto pelo service. Interface, e não o tipo
// concreto, para a validação da matriz ser testável sem Postgres.
type Repositorio interface {
	Listar(ctx context.Context, busca string, pagina, porPagina int) ([]Perfil, int64, error)
	Buscar(ctx context.Context, id uuid.UUID) (Perfil, error)
	Permissoes(ctx context.Context, roleID uuid.UUID) ([]auth.Permissao, error)
	Criar(ctx context.Context, c Criar) (uuid.UUID, error)
	Atualizar(ctx context.Context, id uuid.UUID, a Atualizar) error
	Excluir(ctx context.Context, id uuid.UUID) error
	SubstituirPermissoes(ctx context.Context, roleID uuid.UUID, m Matriz) error
	ContarUsuarios(ctx context.Context, roleID uuid.UUID) (int, error)
	TravarAdministradores(ctx context.Context) error
	ContarAdministradoresAtivosForaDoPerfil(ctx context.Context, roleID uuid.UUID) (int, error)
	Catalogo(ctx context.Context) ([]Recurso, error)
	MetadadosDoCatalogo(ctx context.Context) (map[string]MetaRecurso, error)
}

type Service struct {
	repo Repositorio
	tx   auth.Transacionador
}

func NewService(repo Repositorio, tx auth.Transacionador) *Service {
	return &Service{repo: repo, tx: tx}
}

// Listar não devolve `permissions`: a lista fica leve e o detalhe carrega a
// matriz.
func (s *Service) Listar(ctx context.Context, busca string, pagina, porPagina int) ([]Perfil, int64, error) {
	return s.repo.Listar(ctx, busca, pagina, porPagina)
}

// Buscar devolve perfil + matriz — é o que a tela de edição precisa numa
// chamada só.
func (s *Service) Buscar(ctx context.Context, id uuid.UUID) (PerfilDetalhe, error) {
	perfil, err := s.repo.Buscar(ctx, id)
	if err != nil {
		return PerfilDetalhe{}, err
	}
	perms, err := s.repo.Permissoes(ctx, id)
	if err != nil {
		return PerfilDetalhe{}, err
	}
	return PerfilDetalhe{Perfil: perfil, Permissoes: perms}, nil
}

// Criar registra o perfil sem nenhuma permissão: a matriz vem depois, por
// PUT /roles/{id}/permissions. Perfil nascer com acesso a algo por padrão seria
// conceder o que ninguém pediu.
func (s *Service) Criar(ctx context.Context, c Criar) (Perfil, error) {
	c.Normalizar()

	var criado Perfil
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		id, err := s.repo.Criar(ctx, c)
		if err != nil {
			return err
		}
		criado, err = s.repo.Buscar(ctx, id)
		return err
	})
	if err != nil {
		return Perfil{}, err
	}
	return criado, nil
}

// Atualizar renomeia. Perfil de sistema recusa: renomear o `admin` quebraria
// seed, filtro e teste que o referenciam pelo code.
func (s *Service) Atualizar(ctx context.Context, id uuid.UUID, a Atualizar) (Perfil, error) {
	a.Normalizar()

	perfil, err := s.repo.Buscar(ctx, id)
	if err != nil {
		return Perfil{}, err
	}
	if perfil.IsSystem {
		return Perfil{}, apperr.RoleImmutable
	}

	var atualizado Perfil
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.repo.Atualizar(ctx, id, a); err != nil {
			return err
		}
		atualizado, err = s.repo.Buscar(ctx, id)
		return err
	})
	if err != nil {
		return Perfil{}, err
	}
	return atualizado, nil
}

// Substituir é o PUT /roles/{id}: substituição integral do CADASTRO do perfil.
//
// Como `code` e `name` são os únicos campos mutáveis do cadastro e ambos são
// obrigatórios no PUT (conferido no handler), a substituição integral coincide
// com a atualização — a diferença que o contrato pede está na resposta, que traz
// a matriz junto para a tela reexibir o perfil inteiro sem uma segunda chamada.
//
// A matriz sai exatamente como estava: quem a troca é PUT
// /roles/{id}/permissions. Um PUT de cadastro que a zerasse por omissão tiraria
// o acesso de todos os usuários do perfil sem ninguém ter pedido isso.
func (s *Service) Substituir(ctx context.Context, id uuid.UUID, a Atualizar) (PerfilDetalhe, error) {
	perfil, err := s.Atualizar(ctx, id, a)
	if err != nil {
		return PerfilDetalhe{}, err
	}
	perms, err := s.repo.Permissoes(ctx, id)
	if err != nil {
		return PerfilDetalhe{}, err
	}
	return PerfilDetalhe{Perfil: perfil, Permissoes: perms}, nil
}

// Excluir recusa perfil de sistema e perfil com gente dentro.
//
// Excluir em cascata deixaria os usuários daquele perfil sem permissão nenhuma e
// sem aviso — quem administra descobriria pelo chamado de suporte.
func (s *Service) Excluir(ctx context.Context, id uuid.UUID) error {
	perfil, err := s.repo.Buscar(ctx, id)
	if err != nil {
		return err
	}
	if perfil.IsSystem {
		return apperr.RoleImmutable
	}

	usuarios, err := s.repo.ContarUsuarios(ctx, id)
	if err != nil {
		return err
	}
	if usuarios > 0 {
		return apperr.RoleInUse.WithDetails(map[string]any{"users_count": usuarios})
	}

	return s.tx.Do(ctx, func(ctx context.Context) error {
		return s.repo.Excluir(ctx, id)
	})
}

// Catalogo é a fonte única da grade de permissões da tela de perfis.
func (s *Service) Catalogo(ctx context.Context) ([]Recurso, error) {
	return s.repo.Catalogo(ctx)
}

// SubstituirPermissoes troca a matriz inteira.
//
// A matriz do perfil de sistema é imutável: bastaria remover `roles:editar` do
// admin para ninguém mais conseguir consertar a instalação — a chave trancada
// dentro do carro.
func (s *Service) SubstituirPermissoes(ctx context.Context, id uuid.UUID, m Matriz) ([]auth.Permissao, error) {
	perfil, err := s.repo.Buscar(ctx, id)
	if err != nil {
		return nil, err
	}
	if perfil.IsSystem {
		return nil, apperr.RoleImmutable
	}

	// Escopo ausente vale `all` (default do contrato) — a normalização vem antes
	// do teto de privilégio porque é o escopo EFETIVO que precisa ser comparado
	// com o do ator; comparar o vazio deixaria passar `all` disfarçado.
	m = m.ComEscopoPadrao()

	catalogo, err := s.repo.MetadadosDoCatalogo(ctx)
	if err != nil {
		return nil, err
	}

	// Validar contra o catálogo do BANCO antes de gravar. Três checagens, e as
	// três leem `resources`:
	//
	//   - recurso existe: a FK também barraria, mas o erro sairia como violação
	//     de constraint, sem dizer qual índice do array está errado;
	//   - o recurso oferece a ação: é a mesma checagem que o seed faz em
	//     `montarMatriz`. Sem ela a grade concede "excluir" no razão financeiro
	//     (append-only, spec §10) e no chat (mensagem não se apaga, spec §8);
	//   - escopo `own` só onde há dono: `own` vira `AND owner_id = $user` no
	//     SQL, e num recurso sem coluna de dono isso viraria filtro ignorado —
	//     a tela mentiria sobre o que o usuário vê.
	erros := map[string]string{}
	for i, p := range m {
		meta, existe := catalogo[p.Resource]
		if !existe {
			erros[fmt.Sprintf("permissions[%d].resource", i)] =
				fmt.Sprintf("recurso %q não existe no catálogo.", p.Resource)
			continue
		}
		if !meta.Oferece(p.Action) {
			erros[fmt.Sprintf("permissions[%d].action", i)] =
				fmt.Sprintf("o recurso %q não oferece a ação %q; oferece %s.",
					p.Resource, p.Action, strings.Join(meta.Acoes, ", "))
		}
		if p.Scope == auth.EscopoOwn && !meta.SuportaOwn {
			erros[fmt.Sprintf("permissions[%d].scope", i)] =
				fmt.Sprintf("o recurso %q não tem dono; use scope all.", p.Resource)
		}
	}
	if len(erros) > 0 {
		return nil, apperr.Validation(erros)
	}

	// Travas de escalada de privilégio (ver autoridade.go), DEPOIS da validação
	// contra o catálogo e não antes.
	//
	// A ordem importa para o contrato: uma célula que o catálogo não oferece —
	// `chat:excluir`, por exemplo — é célula que NINGUÉM possui, nem o
	// administrador. Autorizando primeiro, ela sairia sempre como 403 e o 422
	// que o contrato promete para grade inválida nunca mais apareceria. Grade
	// malformada é erro de entrada; grade acima do teto é falta de autoridade.
	if ator, temAtor := auth.UserFrom(ctx); temAtor {
		if err := autorizarMatriz(ator, perfil, m); err != nil {
			return nil, err
		}
	}

	var gravada []auth.Permissao
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.naoTirarOUltimoAdministrador(ctx, id, m); err != nil {
			return err
		}
		if err := s.repo.SubstituirPermissoes(ctx, id, m); err != nil {
			return err
		}
		gravada, err = s.repo.Permissoes(ctx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return gravada, nil
}

// naoTirarOUltimoAdministrador é a trava do "último administrador" pelo lado da
// MATRIZ: tirar `roles:editar` de um perfil rebaixa todos os usuários dele de
// uma vez, e é tão capaz de deixar a instalação sem ninguém quanto rebaixar ou
// desativar a última conta de administrador.
//
// Sequencialmente a trava do próprio perfil já bastaria (quem edita matriz tem
// `roles:editar` e não pode mexer no próprio perfil, então ele mesmo continua
// administrador). Em PARALELO, não: esta transação e a desativação de usuário do
// módulo users leem, cada uma, um mundo em que ainda sobra alguém. Por isso a
// contagem roda DENTRO da transação e sob a MESMA trava do outro módulo.
func (s *Service) naoTirarOUltimoAdministrador(ctx context.Context, id uuid.UUID, nova Matriz) error {
	// A matriz nova ainda concede acesso: não há como zerar administrador por
	// aqui, e nem vale pagar a trava.
	if concedeAdministracao(nova) {
		return nil
	}

	if err := s.repo.TravarAdministradores(ctx); err != nil {
		return err
	}

	// Lida DEPOIS da trava: antes dela, a matriz atual é um retrato que outra
	// transação pode estar reescrevendo neste instante.
	atual, err := s.repo.Permissoes(ctx, id)
	if err != nil {
		return err
	}
	if !concedeAdministracao(atual) {
		return nil // o perfil já não administrava nada: nada a proteger
	}

	restantes, err := s.repo.ContarAdministradoresAtivosForaDoPerfil(ctx, id)
	if err != nil {
		return err
	}
	if restantes == 0 {
		return apperr.Validation(map[string]string{
			"permissions": "este é o último perfil com usuários ativos capazes de administrar o sistema; " +
				"conceda o acesso a outro perfil antes de retirá-lo daqui.",
		})
	}
	return nil
}

// Meta monta o bloco de paginação da listagem.
func Meta(pagina, porPagina int, total int64) httpx.Meta {
	return httpx.Meta{Page: pagina, PerPage: porPagina, Total: total}
}
