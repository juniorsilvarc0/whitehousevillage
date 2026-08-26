package roles

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Testes da matriz de permissões contra o catálogo do BANCO.
//
// O defeito que eles cobrem: a validação lia uma lista em Go
// (`recursosComDono`) que divergia de `resources`. Consequência para o negócio,
// em duas etapas: o perfil Corretor ficava impossível de salvar (422 dizendo
// que "chat não tem dono") e, se o administrador obedecesse e trocasse para
// `all`, o corretor passava a ver orçamento, agenda, calendário e conversa de
// WhatsApp de todo mundo. O catálogo agora é dado, e a validação lê a mesma
// fonte que a grade da tela.

type repoFalso struct {
	perfis   map[uuid.UUID]Perfil
	perms    map[uuid.UUID][]auth.Permissao
	catalogo map[string]MetaRecurso

	// administradoresForaDoPerfil é o que a trava do último administrador vê
	// quando pergunta quem sobra fora deste perfil.
	administradoresForaDoPerfil int

	gravadas []Matriz
	travas   int
}

func (r *repoFalso) Listar(context.Context, string, int, int) ([]Perfil, int64, error) {
	return nil, 0, nil
}

func (r *repoFalso) Buscar(_ context.Context, id uuid.UUID) (Perfil, error) {
	p, ok := r.perfis[id]
	if !ok {
		return Perfil{}, apperr.NotFound("Perfil")
	}
	return p, nil
}

func (r *repoFalso) Permissoes(_ context.Context, id uuid.UUID) ([]auth.Permissao, error) {
	return r.perms[id], nil
}

func (r *repoFalso) Criar(context.Context, Criar) (uuid.UUID, error) { return uuid.New(), nil }

func (r *repoFalso) Atualizar(_ context.Context, id uuid.UUID, a Atualizar) error {
	p := r.perfis[id]
	if v, ok := a.Codigo.Definido(); ok {
		p.Codigo = v
	}
	if v, ok := a.Nome.Definido(); ok {
		p.Nome = v
	}
	r.perfis[id] = p
	return nil
}

func (r *repoFalso) Excluir(context.Context, uuid.UUID) error { return nil }

func (r *repoFalso) ContarUsuarios(context.Context, uuid.UUID) (int, error) { return 0, nil }

func (r *repoFalso) TravarAdministradores(context.Context) error {
	r.travas++
	return nil
}

func (r *repoFalso) ContarAdministradoresAtivosForaDoPerfil(context.Context, uuid.UUID) (int, error) {
	return r.administradoresForaDoPerfil, nil
}

func (r *repoFalso) Catalogo(context.Context) ([]Recurso, error) { return nil, nil }

func (r *repoFalso) MetadadosDoCatalogo(context.Context) (map[string]MetaRecurso, error) {
	return r.catalogo, nil
}

func (r *repoFalso) SubstituirPermissoes(_ context.Context, id uuid.UUID, m Matriz) error {
	r.gravadas = append(r.gravadas, m)
	r.perms[id] = m
	return nil
}

type txDireto struct{}

func (txDireto) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

// montar devolve o service com o catálogo do seed em miniatura: `chat` sem
// "excluir" e com dono (spec §8), `settings` com tudo e sem dono.
func montar(t *testing.T) (*Service, *repoFalso, uuid.UUID) {
	t.Helper()

	id := uuid.New()
	repo := &repoFalso{
		perfis:                      map[uuid.UUID]Perfil{id: {ID: id, Codigo: "corretor", Nome: "Corretor"}},
		perms:                       map[uuid.UUID][]auth.Permissao{},
		administradoresForaDoPerfil: 1,
		catalogo: map[string]MetaRecurso{
			"chat": {
				Acoes:      []string{auth.AcaoVer, auth.AcaoCriar, auth.AcaoEditar},
				SuportaOwn: true,
			},
			"settings": {Acoes: auth.AcoesValidas, SuportaOwn: false},
		},
	}
	return NewService(repo, txDireto{}), repo, id
}

func detalhesDoErro(t *testing.T, err error) map[string]string {
	t.Helper()

	if err == nil {
		t.Fatal("a matriz foi ACEITA; esperado 422 VALIDATION_ERROR")
	}
	var e *apperr.Error
	if !errors.As(err, &e) {
		t.Fatalf("erro sem código estável: %v", err)
	}
	if e.Code != "VALIDATION_ERROR" || e.Status() != 422 {
		t.Fatalf("erro = %s (%d), esperado VALIDATION_ERROR (422)", e.Code, e.Status())
	}
	d, ok := e.Details.(map[string]string)
	if !ok {
		t.Fatalf("details = %#v, esperado o mapa campo → mensagem", e.Details)
	}
	return d
}

// O razão financeiro é append-only (spec §10) e mensagem enviada não se apaga
// (spec §8): "excluir" não existe nesses recursos, e a grade não pode conceder
// o que o produto não faz.
func TestMatrizRecusaAcaoQueORecursoNaoOferece(t *testing.T) {
	svc, repo, id := montar(t)

	_, err := svc.SubstituirPermissoes(context.Background(), id, Matriz{
		{Resource: "chat", Action: auth.AcaoExcluir, Scope: auth.EscopoAll},
	})

	d := detalhesDoErro(t, err)
	if d["permissions[0].action"] == "" {
		t.Fatalf("o erro não aponta a ação inválida: %v", d)
	}
	if len(repo.gravadas) > 0 {
		t.Fatal("a ação fora do catálogo foi GRAVADA antes da recusa")
	}
}

// `own` vira `AND owner_id = $user` no SQL: num recurso sem coluna de dono
// viraria filtro ignorado, e a tela mentiria sobre o que o usuário vê.
func TestMatrizRecusaEscopoOwnOndeNaoHaDono(t *testing.T) {
	svc, repo, id := montar(t)

	_, err := svc.SubstituirPermissoes(context.Background(), id, Matriz{
		{Resource: "settings", Action: auth.AcaoVer, Scope: auth.EscopoOwn},
	})

	d := detalhesDoErro(t, err)
	if d["permissions[0].scope"] == "" {
		t.Fatalf("o erro não aponta o escopo inválido: %v", d)
	}
	if len(repo.gravadas) > 0 {
		t.Fatal("o escopo sem dono foi GRAVADO antes da recusa")
	}
}

// A regressão do Corretor: `chat` TEM dono no banco (`supports_own = true`), e
// salvar o perfil como ele está tem de funcionar. Com a lista em Go, que não
// conhecia `chat`, esta chamada devolvia 422.
func TestMatrizAceitaOwnQuandoOCatalogoDizQueORecursoTemDono(t *testing.T) {
	svc, repo, id := montar(t)

	gravada, err := svc.SubstituirPermissoes(context.Background(), id, Matriz{
		{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoOwn},
	})
	if err != nil {
		t.Fatalf("salvar o perfil Corretor como ele está deveria passar: %v", err)
	}
	if len(gravada) != 1 || gravada[0].Scope != auth.EscopoOwn {
		t.Fatalf("a matriz gravada perdeu o escopo own: %+v", gravada)
	}
	if len(repo.gravadas) != 1 {
		t.Fatalf("gravações = %d, esperado 1", len(repo.gravadas))
	}
}

// Recurso fora do catálogo continua recusado com o índice do array, e não com
// uma violação de FK traduzida.
func TestMatrizRecusaRecursoForaDoCatalogo(t *testing.T) {
	svc, _, id := montar(t)

	_, err := svc.SubstituirPermissoes(context.Background(), id, Matriz{
		{Resource: "chat.conversations", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	})

	d := detalhesDoErro(t, err)
	if d["permissions[0].resource"] == "" {
		t.Fatalf("o erro não aponta o recurso inexistente: %v", d)
	}
}

// ─────────────────────────── PUT /roles/{id} ────────────────────────────────

// O PUT é substituição integral do CADASTRO. A matriz não vem no corpo e sai
// exatamente como estava: um PUT que a zerasse por omissão tiraria o acesso de
// todos os usuários do perfil sem ninguém ter pedido isso.
func TestSubstituirTrocaOCadastroENaoEncostaNaMatriz(t *testing.T) {
	svc, repo, id := montar(t)
	repo.perms[id] = []auth.Permissao{{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoOwn}}

	detalhe, err := svc.Substituir(context.Background(), id, Atualizar{
		Codigo: httpx.De("recepcao"),
		Nome:   httpx.De("Recepção"),
	})
	if err != nil {
		t.Fatalf("Substituir: %v", err)
	}

	if detalhe.Codigo != "recepcao" || detalhe.Nome != "Recepção" {
		t.Fatalf("o cadastro não foi substituído: %+v", detalhe.Perfil)
	}
	if len(detalhe.Permissoes) != 1 || detalhe.Permissoes[0].Scope != auth.EscopoOwn {
		t.Fatalf("a resposta do PUT tem de trazer a matriz intacta: %+v", detalhe.Permissoes)
	}
	if len(repo.gravadas) != 0 {
		t.Fatal("o PUT do cadastro reescreveu a matriz de permissões")
	}
}

// Perfil de sistema não se altera nem pelo PUT: bastaria renomear o `admin`
// para quebrar seed, filtro e teste que o referenciam pelo code.
func TestSubstituirRecusaPerfilDeSistema(t *testing.T) {
	svc, repo, id := montar(t)
	p := repo.perfis[id]
	p.IsSystem = true
	repo.perfis[id] = p

	_, err := svc.Substituir(context.Background(), id, Atualizar{
		Codigo: httpx.De("outro"),
		Nome:   httpx.De("Outro"),
	})

	var e *apperr.Error
	if !errors.As(err, &e) || e.Code != "ROLE_IMMUTABLE" || e.Status() != 409 {
		t.Fatalf("erro = %v, esperado ROLE_IMMUTABLE (409)", err)
	}
}

// O `required: [code, name]` do PUT é conferido fora das tags, porque o mesmo
// DTO serve ao PATCH, onde os dois campos são opcionais.
func TestObrigatoriosDoPutCobramCodigoENome(t *testing.T) {
	faltando := Atualizar{}.ObrigatoriosDoPut()
	if faltando["code"] == "" || faltando["name"] == "" {
		t.Fatalf("PUT sem code/name deveria acusar os dois: %v", faltando)
	}

	completo := Atualizar{Codigo: httpx.De("recepcao"), Nome: httpx.De("Recepção")}.ObrigatoriosDoPut()
	if len(completo) != 0 {
		t.Fatalf("PUT completo não deveria acusar nada: %v", completo)
	}
}

// ─────────────────── Teto de privilégio da matriz ───────────────────────────
//
// O defeito ALTO da 3ª rodada: `SubstituirPermissoes` gravava a grade inteira
// sem comparar com o que o ator possui. Quem tinha `roles:editar` reescrevia o
// próprio perfil com as 79 células do catálogo e, na requisição seguinte,
// concedia qualquer coisa a qualquer um — o teto de `PATCH /users` virava
// enfeite, porque o atacante ampliava o próprio teto antes de usá-lo.

// comoAtor devolve o contexto de uma requisição autenticada por um usuário
// daquele perfil, com aquela matriz — a mesma montagem que o middleware faz a
// partir do banco.
func comoAtor(perfil uuid.UUID, celulas ...auth.Permissao) context.Context {
	return auth.WithUser(context.Background(), &auth.Usuario{
		ID:         uuid.New(),
		RoleID:     perfil,
		Permissoes: auth.NovoConjunto(celulas),
	})
}

func exigirForbidden(t *testing.T, err error) *apperr.Error {
	t.Helper()

	if err == nil {
		t.Fatal("a matriz foi ACEITA; esperado 403 FORBIDDEN")
	}
	var e *apperr.Error
	if !errors.As(err, &e) {
		t.Fatalf("erro sem código estável: %v", err)
	}
	if e.Code != "FORBIDDEN" || e.Status() != 403 {
		t.Fatalf("erro = %s (%d), esperado FORBIDDEN (403)", e.Code, e.Status())
	}
	return e
}

// Trava (a): ninguém grava célula que não possui. O ator vê `chat` e nada mais;
// pedir `settings` num perfil de terceiro é conceder o que ele não tem.
func TestMatrizNaoExcedeOPerfilDoAtor(t *testing.T) {
	svc, repo, id := montar(t)

	ctx := comoAtor(uuid.New(), // ator de OUTRO perfil: a trava (b) não interfere
		auth.Permissao{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
		auth.Permissao{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	)

	_, err := svc.SubstituirPermissoes(ctx, id, Matriz{
		{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: "settings", Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	})

	e := exigirForbidden(t, err)
	detalhes, ok := e.Details.(map[string]any)
	if !ok {
		t.Fatalf("details = %#v, esperado o mapa com as células que faltam", e.Details)
	}
	faltando, _ := detalhes["missing"].([]string)
	if len(faltando) != 1 || faltando[0] != "settings:editar:all" {
		t.Fatalf("details.missing = %v, esperado [settings:editar:all]", faltando)
	}
	if len(repo.gravadas) > 0 {
		t.Fatal("a matriz acima do teto foi GRAVADA antes da recusa")
	}
}

// Escopo entra na comparação: quem só enxerga o que é seu não concede a visão
// de tudo, senão a delegação ampliaria o alcance de quem delegou.
func TestMatrizNaoAmpliaOEscopoDoAtor(t *testing.T) {
	svc, repo, id := montar(t)

	ctx := comoAtor(uuid.New(),
		auth.Permissao{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
		auth.Permissao{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoOwn},
	)

	_, err := svc.SubstituirPermissoes(ctx, id, Matriz{
		{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	})

	exigirForbidden(t, err)
	if len(repo.gravadas) > 0 {
		t.Fatal("o escopo ampliado foi gravado")
	}
}

// Trava (b): ninguém edita a matriz do PRÓPRIO perfil — nem para menos. A
// matriz é o que limita o ator; deixá-lo mexer nela é deixar o preso guardar a
// chave da cela.
func TestNinguemEditaAMatrizDoProprioPerfil(t *testing.T) {
	svc, repo, id := montar(t)

	ctx := comoAtor(id, // o ator É deste perfil
		auth.Permissao{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
		auth.Permissao{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	)

	// Pedido inofensivo, dentro do próprio teto: mesmo assim recusa. O critério
	// é "é o meu perfil", não "o que estou pedindo é maior" — senão a escalada
	// continuaria em passos pequenos.
	_, err := svc.SubstituirPermissoes(ctx, id, Matriz{
		{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	})

	e := exigirForbidden(t, err)
	detalhes, _ := e.Details.(map[string]any)
	if detalhes["reason"] != "proprio_perfil" {
		t.Fatalf("details = %v, esperado reason=proprio_perfil", detalhes)
	}
	if len(repo.gravadas) > 0 {
		t.Fatal("a matriz do próprio perfil foi gravada")
	}
}

// Controle positivo: sem ele os dois testes acima passariam com a tela de
// perfis morta. Delegar o que se tem, em perfil de terceiro, continua
// funcionando.
func TestDelegarOQueSeTemContinuaFuncionando(t *testing.T) {
	svc, repo, id := montar(t)

	ctx := comoAtor(uuid.New(),
		auth.Permissao{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
		auth.Permissao{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoOwn},
	)

	if _, err := svc.SubstituirPermissoes(ctx, id, Matriz{
		{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoOwn},
	}); err != nil {
		t.Fatalf("delegar o que se tem deveria passar: %v", err)
	}
	if len(repo.gravadas) != 1 {
		t.Fatalf("gravações = %d, esperado 1", len(repo.gravadas))
	}
}

// Sem sessão no contexto não há ator a limitar: sobra seed e script de
// manutenção, onde quem roda já tem o banco na mão. Sem esta isenção o seed
// deixaria de conseguir montar a matriz inicial.
func TestSemAtorNoContextoAMatrizContinuaGravavel(t *testing.T) {
	svc, repo, id := montar(t)

	if _, err := svc.SubstituirPermissoes(context.Background(), id, Matriz{
		{Resource: "settings", Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	}); err != nil {
		t.Fatalf("sem ator a gravação deveria passar: %v", err)
	}
	if len(repo.gravadas) != 1 {
		t.Fatalf("gravações = %d, esperado 1", len(repo.gravadas))
	}
}

// ─────────────── Último administrador, pelo lado da matriz ──────────────────

// Tirar `roles:editar` da matriz rebaixa TODOS os usuários do perfil de uma vez.
// Quando não sobra administrador em nenhum outro perfil, a instalação fica sem
// ninguém capaz de consertar acesso — e não há caminho de volta pela API.
func TestMatrizNaoTiraOUltimoAdministrador(t *testing.T) {
	svc, repo, id := montar(t)
	repo.perms[id] = []auth.Permissao{
		{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	}
	repo.administradoresForaDoPerfil = 0

	_, err := svc.SubstituirPermissoes(context.Background(), id, Matriz{
		{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	})

	d := detalhesDoErro(t, err)
	if d["permissions"] == "" {
		t.Fatalf("o erro não explica que é o último perfil administrador: %v", d)
	}
	if len(repo.gravadas) > 0 {
		t.Fatal("a matriz foi gravada antes da recusa")
	}
	if repo.travas == 0 {
		t.Fatal("a contagem rodou sem a trava compartilhada: a corrida com /users continua aberta")
	}
}

// Sobrando administrador em outro perfil, a retirada passa — a trava protege a
// instalação, não o perfil.
func TestMatrizPodeTirarAdministracaoQuandoSobraOutroPerfil(t *testing.T) {
	svc, repo, id := montar(t)
	repo.perms[id] = []auth.Permissao{
		{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	}
	repo.administradoresForaDoPerfil = 1

	if _, err := svc.SubstituirPermissoes(context.Background(), id, Matriz{
		{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	}); err != nil {
		t.Fatalf("com outro perfil administrador a retirada deveria passar: %v", err)
	}
	if len(repo.gravadas) != 1 {
		t.Fatalf("gravações = %d, esperado 1", len(repo.gravadas))
	}
}

// Matriz que CONTINUA concedendo `roles:editar` não paga o preço da trava: não
// há como zerar administrador por esse caminho.
func TestMatrizQueMantemAdministracaoNaoPagaATrava(t *testing.T) {
	svc, repo, id := montar(t)
	repo.perms[id] = []auth.Permissao{
		{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	}
	repo.catalogo[auth.RecursoPerfis] = MetaRecurso{Acoes: auth.AcoesValidas, SuportaOwn: false}
	repo.administradoresForaDoPerfil = 0

	if _, err := svc.SubstituirPermissoes(context.Background(), id, Matriz{
		{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	}); err != nil {
		t.Fatalf("manter a administração deveria passar: %v", err)
	}
	if repo.travas != 0 {
		t.Fatalf("travas = %d: matriz que mantém administração não precisa serializar nada", repo.travas)
	}
}
