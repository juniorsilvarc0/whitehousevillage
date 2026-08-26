package users

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Testes das travas de escalada de privilégio (CRÍTICO 2).
//
// Rodam sem infraestrutura, em `go test ./...`: um defeito de autorização não
// pode depender do ambiente de integração ter subido para ser detectado.
//
// O repositório falso GRAVA o que recebeu. É por isso que cada teste confere
// duas coisas — o status do erro E que nada foi escrito. Um teste que só olha o
// código de erro passaria mesmo se a trava recusasse DEPOIS do UPDATE.

// ─────────────────────────── Dublês ─────────────────────────────────────────

type repoFalso struct {
	usuarios map[uuid.UUID]auth.LinhaUsuario
	papeis   map[uuid.UUID][]auth.Permissao
	// administradoresRestantes é o que ContarAdministradoresAtivos devolve.
	administradoresRestantes int

	// Escritas observadas.
	atualizacoes []Atualizar
	senhasNovas  int
	criados      []Criar
	auditorias   []Auditoria
	travas       int
}

func (r *repoFalso) Listar(context.Context, Filtro) ([]auth.LinhaUsuario, int64, error) {
	return nil, 0, nil
}

func (r *repoFalso) Buscar(_ context.Context, id uuid.UUID) (auth.LinhaUsuario, error) {
	l, ok := r.usuarios[id]
	if !ok {
		return auth.LinhaUsuario{}, apperr.NotFound("Usuário")
	}
	return l, nil
}

func (r *repoFalso) Criar(_ context.Context, _ uuid.UUID, c Criar, _ string) (uuid.UUID, error) {
	r.criados = append(r.criados, c)
	id := uuid.New()
	r.usuarios[id] = auth.LinhaUsuario{ID: id, RoleID: c.RoleID}
	return id, nil
}

func (r *repoFalso) Atualizar(_ context.Context, id uuid.UUID, a Atualizar, senhaHash *string) error {
	r.atualizacoes = append(r.atualizacoes, a)
	if senhaHash != nil {
		r.senhasNovas++
	}
	if papel, ok := a.RoleID.Definido(); ok {
		l := r.usuarios[id]
		l.RoleID = papel
		r.usuarios[id] = l
	}
	return nil
}

func (r *repoFalso) Desativar(context.Context, uuid.UUID) error { return nil }

func (r *repoFalso) PropriedadePadrao(context.Context) (uuid.UUID, error) {
	return uuid.New(), nil
}

func (r *repoFalso) ContarAdministradoresAtivos(context.Context, uuid.UUID) (int, error) {
	return r.administradoresRestantes, nil
}

// TravarAdministradores conta as chamadas: o teste do "último administrador"
// cobra que a contagem venha DEPOIS da trava, e não solta no meio do caminho.
func (r *repoFalso) TravarAdministradores(context.Context) error {
	r.travas++
	return nil
}

func (r *repoFalso) RegistrarAuditoria(_ context.Context, a Auditoria) error {
	r.auditorias = append(r.auditorias, a)
	return nil
}

func (r *repoFalso) PermissoesDoPapel(_ context.Context, roleID uuid.UUID) ([]auth.Permissao, error) {
	return r.papeis[roleID], nil
}

// escreveu diz se alguma escrita chegou ao repositório.
func (r *repoFalso) escreveu() bool { return len(r.atualizacoes) > 0 || len(r.criados) > 0 }

// txDireto executa a função sem transação: aqui o que se testa é a decisão de
// autorização, que acontece ANTES de qualquer transação abrir.
type txDireto struct{}

func (txDireto) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type sessoesFalsas struct{ revogadas int }

func (s *sessoesFalsas) RevogarSessoesDoUsuario(context.Context, uuid.UUID) error {
	s.revogadas++
	return nil
}

// ─────────────────────────── Cenário ────────────────────────────────────────

// papeis do cenário: um administrador completo, um "suporte" que só mexe em
// cadastro de usuário, e um gestor que administra acesso mas não vê o
// financeiro.
type cenario struct {
	svc     *Service
	repo    *repoFalso
	sessoes *sessoesFalsas

	papelAdmin, papelSuporte, papelGestor uuid.UUID
	admin, suporte, gestor, comum         auth.LinhaUsuario
}

func permissoes(pares ...[3]string) []auth.Permissao {
	out := make([]auth.Permissao, 0, len(pares))
	for _, p := range pares {
		out = append(out, auth.Permissao{Resource: p[0], Action: p[1], Scope: p[2]})
	}
	return out
}

func montarCenario(t *testing.T) *cenario {
	t.Helper()

	c := &cenario{
		papelAdmin:   uuid.New(),
		papelSuporte: uuid.New(),
		papelGestor:  uuid.New(),
	}

	admin := permissoes(
		[3]string{auth.RecursoUsuarios, auth.AcaoVer, auth.EscopoAll},
		[3]string{auth.RecursoUsuarios, auth.AcaoCriar, auth.EscopoAll},
		[3]string{auth.RecursoUsuarios, auth.AcaoEditar, auth.EscopoAll},
		[3]string{auth.RecursoUsuarios, auth.AcaoExcluir, auth.EscopoAll},
		[3]string{auth.RecursoPerfis, auth.AcaoVer, auth.EscopoAll},
		[3]string{auth.RecursoPerfis, auth.AcaoEditar, auth.EscopoAll},
		[3]string{"settings", auth.AcaoEditar, auth.EscopoAll},
		[3]string{"finance.receivables", auth.AcaoVer, auth.EscopoAll},
	)
	// O perfil do achado: vê, cria e edita usuário, e nada mais.
	suporte := permissoes(
		[3]string{auth.RecursoUsuarios, auth.AcaoVer, auth.EscopoAll},
		[3]string{auth.RecursoUsuarios, auth.AcaoCriar, auth.EscopoAll},
		[3]string{auth.RecursoUsuarios, auth.AcaoEditar, auth.EscopoAll},
	)
	// Administra acesso, mas não enxerga financeiro nem parâmetros.
	gestor := permissoes(
		[3]string{auth.RecursoUsuarios, auth.AcaoVer, auth.EscopoAll},
		[3]string{auth.RecursoUsuarios, auth.AcaoCriar, auth.EscopoAll},
		[3]string{auth.RecursoUsuarios, auth.AcaoEditar, auth.EscopoAll},
		[3]string{auth.RecursoPerfis, auth.AcaoVer, auth.EscopoAll},
		[3]string{auth.RecursoPerfis, auth.AcaoEditar, auth.EscopoAll},
	)

	c.repo = &repoFalso{
		usuarios: map[uuid.UUID]auth.LinhaUsuario{},
		papeis: map[uuid.UUID][]auth.Permissao{
			c.papelAdmin:   admin,
			c.papelSuporte: suporte,
			c.papelGestor:  gestor,
		},
		administradoresRestantes: 1,
	}
	c.sessoes = &sessoesFalsas{}
	c.svc = NewService(c.repo, c.sessoes, txDireto{})

	c.admin = c.usuario(c.papelAdmin)
	c.suporte = c.usuario(c.papelSuporte)
	c.gestor = c.usuario(c.papelGestor)
	c.comum = c.usuario(c.papelSuporte)
	return c
}

func (c *cenario) usuario(papel uuid.UUID) auth.LinhaUsuario {
	l := auth.LinhaUsuario{
		ID:     uuid.New(),
		RoleID: papel,
		Ativo:  true,
		Email:  uuid.NewString()[:8] + "@wh.local",
	}
	c.repo.usuarios[l.ID] = l
	return l
}

// como devolve o contexto de uma requisição autenticada por aquele usuário — a
// mesma montagem que o middleware faz a partir do banco.
func (c *cenario) como(u auth.LinhaUsuario) context.Context {
	return auth.WithUser(context.Background(), &auth.Usuario{
		ID:         u.ID,
		RoleID:     u.RoleID,
		Permissoes: auth.NovoConjunto(c.repo.papeis[u.RoleID]),
	})
}

func exigirCodigo(t *testing.T, err error, code string, status int) {
	t.Helper()

	if err == nil {
		t.Fatalf("a operação foi ACEITA; esperado %s (%d)", code, status)
	}
	var e *apperr.Error
	if !errors.As(err, &e) {
		t.Fatalf("erro sem código estável: %v", err)
	}
	if e.Code != code || e.Status() != status {
		t.Fatalf("erro = %s (%d), esperado %s (%d)", e.Code, e.Status(), code, status)
	}
}

// ─────────────────────────── Trava 1 ────────────────────────────────────────

// Reprodução do achado, no caminho mais curto: o suporte grava o papel de
// administrador no PRÓPRIO cadastro. Como a matriz é lida do banco a cada
// requisição, na chamada seguinte ele já mandaria na instalação — sem relogar.
func TestNinguemAlteraOProprioPapel(t *testing.T) {
	c := montarCenario(t)

	_, err := c.svc.Atualizar(c.como(c.suporte), c.suporte.ID, Atualizar{
		RoleID: httpx.De(c.papelAdmin),
	})

	exigirCodigo(t, err, "VALIDATION_ERROR", 422)
	if c.repo.escreveu() {
		t.Fatal("a trava recusou DEPOIS de gravar: o UPDATE chegou ao repositório")
	}
}

// Nem o administrador muda o próprio papel — ele usa outra conta para isso.
// Mesma razão pela qual ninguém se desativa: o caminho para uma instalação sem
// administrador não pode passar por um clique distraído.
func TestNemOAdministradorAlteraOProprioPapel(t *testing.T) {
	c := montarCenario(t)

	_, err := c.svc.Atualizar(c.como(c.admin), c.admin.ID, Atualizar{
		RoleID: httpx.De(c.papelSuporte),
	})

	exigirCodigo(t, err, "VALIDATION_ERROR", 422)
	if c.repo.escreveu() {
		t.Fatal("o papel do próprio administrador foi gravado")
	}
}

// Reenviar o MESMO papel não é atribuição: o PUT obriga `role_id` no corpo, e
// recusar aqui impediria alguém de corrigir o próprio telefone pelo PUT.
func TestReenviarOMesmoPapelNaoEhAtribuicao(t *testing.T) {
	c := montarCenario(t)

	_, err := c.svc.Substituir(c.como(c.suporte), c.suporte.ID, Atualizar{
		Nome:   httpx.De("Suporte"),
		Email:  httpx.De("suporte@wh.local"),
		RoleID: httpx.De(c.papelSuporte),
	})
	if err != nil {
		t.Fatalf("PUT sem troca de papel deveria passar: %v", err)
	}
	if len(c.repo.atualizacoes) != 1 {
		t.Fatalf("escritas = %d, esperado 1", len(c.repo.atualizacoes))
	}
}

// ─────────────────────────── Trava 2 ────────────────────────────────────────

// `users:editar` cuida de cadastro; quem atribui papel administra ACESSO, e
// isso é `roles:editar`.
func TestQuemSoEditaUsuarioNaoMudaOPapelDeNinguem(t *testing.T) {
	c := montarCenario(t)

	_, err := c.svc.Atualizar(c.como(c.suporte), c.comum.ID, Atualizar{
		RoleID: httpx.De(c.papelAdmin),
	})

	exigirCodigo(t, err, "FORBIDDEN", 403)
	if c.repo.escreveu() {
		t.Fatal("o papel foi gravado sem autoridade para atribuí-lo")
	}
}

// Teto de privilégio: nem quem administra acesso concede o que não tem. O
// gestor não enxerga financeiro nem parâmetros, logo não promove ninguém a
// administrador — senão bastaria promover um terceiro e usar a conta dele.
func TestPapelAtribuidoNaoPodeExcederOPapelDoAtor(t *testing.T) {
	c := montarCenario(t)

	_, err := c.svc.Atualizar(c.como(c.gestor), c.comum.ID, Atualizar{
		RoleID: httpx.De(c.papelAdmin),
	})

	exigirCodigo(t, err, "FORBIDDEN", 403)
	if c.repo.escreveu() {
		t.Fatal("o papel acima do teto foi gravado")
	}

	var e *apperr.Error
	errors.As(err, &e)
	detalhes, ok := e.Details.(map[string]any)
	if !ok {
		t.Fatalf("details = %#v, esperado o mapa com as células que faltam", e.Details)
	}
	if detalhes["field"] != "role_id" {
		t.Errorf("details.field = %v, esperado role_id", detalhes["field"])
	}
	faltando, _ := detalhes["missing"].([]string)
	if len(faltando) == 0 {
		t.Fatal("details.missing veio vazio: o 403 não diz o que falta ao ator")
	}
}

// Controle positivo: sem ele os testes acima passariam mesmo que a trava
// recusasse TUDO, e o cadastro de usuários viraria um botão morto.
func TestQuemAdministraAcessoAtribuiPapelDentroDoSeuTeto(t *testing.T) {
	c := montarCenario(t)

	_, err := c.svc.Atualizar(c.como(c.gestor), c.comum.ID, Atualizar{
		RoleID: httpx.De(c.papelGestor),
	})
	if err != nil {
		t.Fatalf("atribuir um papel subconjunto do ator deveria passar: %v", err)
	}
	if len(c.repo.atualizacoes) != 1 {
		t.Fatalf("escritas = %d, esperado 1", len(c.repo.atualizacoes))
	}
	if papel, ok := c.repo.atualizacoes[0].RoleID.Definido(); !ok || papel != c.papelGestor {
		t.Fatalf("o UPDATE não levou o papel novo: %+v", c.repo.atualizacoes[0])
	}
}

// O escopo entra na comparação: um ator que só enxerga o que é seu não pode
// conceder a visão de tudo — a delegação ampliaria o alcance de quem delegou.
func TestAtorComEscopoOwnNaoConcedeEscopoAll(t *testing.T) {
	ator := auth.NovoConjunto(permissoes(
		[3]string{"reservations", auth.AcaoVer, auth.EscopoOwn},
	))

	seuProprioEscopo := excedentes(permissoes(
		[3]string{"reservations", auth.AcaoVer, auth.EscopoOwn},
	), ator)
	if len(seuProprioEscopo) != 0 {
		t.Fatalf("conceder own tendo own deveria caber: %v", seuProprioEscopo)
	}

	escopoMaior := excedentes(permissoes(
		[3]string{"reservations", auth.AcaoVer, auth.EscopoAll},
	), ator)
	if len(escopoMaior) != 1 {
		t.Fatalf("conceder all tendo só own deveria exceder o teto: %v", escopoMaior)
	}
}

// Criar usuário também é atribuir papel: sem a mesma trava, quem tem
// `users:criar` cadastraria um administrador e entraria nele com a senha que
// acabou de escolher.
func TestCriarUsuarioNaoConcedePapelAcimaDoAtor(t *testing.T) {
	c := montarCenario(t)

	_, err := c.svc.Criar(c.como(c.suporte), Criar{
		Nome:   "Novo",
		Email:  "novo@wh.local",
		Senha:  "senha-de-teste-2026",
		RoleID: c.papelAdmin,
	})

	exigirCodigo(t, err, "FORBIDDEN", 403)
	if c.repo.escreveu() {
		t.Fatal("a conta com perfil de administrador foi criada")
	}
}

// ─────────────────────────── Trava 3 ────────────────────────────────────────

// Quem escolhe a senha de alguém entra na conta e herda o acesso dela: é
// atribuição de papel por outro caminho, e exige a mesma autoridade.
func TestTrocarSenhaDeTerceiroExigeAutoridadeDeAcesso(t *testing.T) {
	c := montarCenario(t)

	_, err := c.svc.Atualizar(c.como(c.suporte), c.comum.ID, Atualizar{
		Senha: httpx.De("senha-nova-do-alheio"),
	})

	exigirCodigo(t, err, "FORBIDDEN", 403)
	if c.repo.escreveu() || c.repo.senhasNovas != 0 {
		t.Fatal("a senha de terceiro foi trocada por quem só edita cadastro")
	}
	if c.sessoes.revogadas != 0 {
		t.Fatal("sessões revogadas numa operação que deveria ter sido recusada")
	}
}

// E nem quem administra acesso troca a senha de alguém mais poderoso: seria
// tomar a conta do administrador tendo menos que ele.
func TestTrocarSenhaDeAlguemAcimaDoTetoEhRecusado(t *testing.T) {
	c := montarCenario(t)

	_, err := c.svc.Atualizar(c.como(c.gestor), c.admin.ID, Atualizar{
		Senha: httpx.De("senha-nova-do-admin"),
	})

	exigirCodigo(t, err, "FORBIDDEN", 403)
	if c.repo.senhasNovas != 0 {
		t.Fatal("a senha do administrador foi trocada por quem não tem o acesso dele")
	}
}

// A própria senha continua livre: é a credencial de quem já está autenticado.
func TestTrocarAPropriaSenhaContinuaPermitido(t *testing.T) {
	c := montarCenario(t)

	_, err := c.svc.Atualizar(c.como(c.suporte), c.suporte.ID, Atualizar{
		Senha: httpx.De("minha-senha-nova-2026"),
	})
	if err != nil {
		t.Fatalf("trocar a própria senha deveria passar: %v", err)
	}
	if c.repo.senhasNovas != 1 {
		t.Fatalf("senhas gravadas = %d, esperado 1", c.repo.senhasNovas)
	}
	// Trocar a senha derruba as sessões: uma senha nova que não expulsa quem
	// estava dentro não protege de nada.
	if c.sessoes.revogadas != 1 {
		t.Fatalf("sessões revogadas = %d, esperado 1", c.sessoes.revogadas)
	}
}

// ─────────────────────── Último administrador ───────────────────────────────

// A mesma trava da desativação, agora no papel: rebaixar o último
// administrador ativo deixa a instalação sem ninguém capaz de devolver acesso a
// quem quer que seja, e não há caminho de volta pela API.
func TestUltimoAdministradorNaoPerdeOPapel(t *testing.T) {
	c := montarCenario(t)
	c.repo.administradoresRestantes = 0

	_, err := c.svc.Atualizar(c.como(c.admin), c.gestor.ID, Atualizar{
		RoleID: httpx.De(c.papelSuporte),
	})

	exigirCodigo(t, err, "VALIDATION_ERROR", 422)
	if c.repo.escreveu() {
		t.Fatal("o último administrador foi rebaixado")
	}
}

// Havendo outro administrador ativo, o rebaixamento passa — a trava protege a
// instalação, não o cargo.
func TestRebaixarAdministradorPassaQuandoSobraOutro(t *testing.T) {
	c := montarCenario(t)
	c.repo.administradoresRestantes = 1

	if _, err := c.svc.Atualizar(c.como(c.admin), c.gestor.ID, Atualizar{
		RoleID: httpx.De(c.papelSuporte),
	}); err != nil {
		t.Fatalf("com outro administrador ativo o rebaixamento deveria passar: %v", err)
	}
}

// ─────────────────────────── Trava 4 (e-mail) ───────────────────────────────

// O CRÍTICO da 3ª rodada, no caminho mais curto: quem só tem `users:editar`
// escreve o e-mail do administrador. Não é edição de cadastro — é assumir o
// controle da recuperação de senha daquela conta e, minutos depois, do papel
// dela.
func TestTrocarEmailDeTerceiroExigeAutoridadeDeAcesso(t *testing.T) {
	c := montarCenario(t)

	_, err := c.svc.Atualizar(c.como(c.suporte), c.admin.ID, Atualizar{
		Email: httpx.De("eu@atacante.example"),
	})

	exigirCodigo(t, err, "FORBIDDEN", 403)
	if c.repo.escreveu() {
		t.Fatal("o e-mail de terceiro foi gravado por quem só edita cadastro")
	}
	if c.sessoes.revogadas != 0 || len(c.repo.auditorias) != 0 {
		t.Fatal("efeitos colaterais numa operação que deveria ter sido recusada")
	}
}

// E nem quem administra acesso troca o e-mail de alguém mais poderoso: seria
// tomar a conta do administrador tendo menos que ele, pela porta da
// recuperação de senha.
func TestTrocarEmailDeAlguemAcimaDoTetoEhRecusado(t *testing.T) {
	c := montarCenario(t)

	_, err := c.svc.Atualizar(c.como(c.gestor), c.admin.ID, Atualizar{
		Email: httpx.De("gestor.tomou@wh.local"),
	})

	exigirCodigo(t, err, "FORBIDDEN", 403)

	var e *apperr.Error
	errors.As(err, &e)
	detalhes, _ := e.Details.(map[string]any)
	if detalhes["field"] != "email" {
		t.Errorf("details.field = %v, esperado email", detalhes["field"])
	}
	if c.repo.escreveu() {
		t.Fatal("o e-mail do administrador foi gravado")
	}
}

// Caminho legítimo: com autoridade sobre o papel do alvo a troca passa — e
// derruba as sessões da conta afetada, com auditoria.
func TestTrocaDeEmailAutorizadaRevogaSessoesEAudita(t *testing.T) {
	c := montarCenario(t)
	anterior := c.comum.Email

	if _, err := c.svc.Atualizar(c.como(c.admin), c.comum.ID, Atualizar{
		Email: httpx.De("novo.endereco@wh.local"),
	}); err != nil {
		t.Fatalf("o administrador deveria poder trocar o e-mail: %v", err)
	}

	if c.sessoes.revogadas != 1 {
		t.Fatalf("sessões revogadas = %d, esperado 1: o endereço de recuperação mudou", c.sessoes.revogadas)
	}
	if len(c.repo.auditorias) != 1 {
		t.Fatalf("auditorias = %d, esperado 1", len(c.repo.auditorias))
	}
	a := c.repo.auditorias[0]
	if a.Acao != AcaoEmailAlterado || a.EntidadeID != c.comum.ID {
		t.Fatalf("linha de auditoria errada: %+v", a)
	}
	if a.Antes["email"] != anterior || a.Depois["email"] != "novo.endereco@wh.local" {
		t.Fatalf("a auditoria não guardou os dois endereços: %+v", a)
	}
}

// O PRÓPRIO e-mail continua livre: é o endereço de contato de quem já está
// autenticado. A sessão cai do mesmo jeito.
func TestTrocarOProprioEmailContinuaPermitido(t *testing.T) {
	c := montarCenario(t)

	if _, err := c.svc.Atualizar(c.como(c.suporte), c.suporte.ID, Atualizar{
		Email: httpx.De("meu.novo@wh.local"),
	}); err != nil {
		t.Fatalf("trocar o próprio e-mail deveria passar: %v", err)
	}
	if c.sessoes.revogadas != 1 {
		t.Fatalf("sessões revogadas = %d, esperado 1", c.sessoes.revogadas)
	}
	if len(c.repo.auditorias) != 1 {
		t.Fatalf("auditorias = %d, esperado 1", len(c.repo.auditorias))
	}
}

// Reenviar o mesmo endereço (o PUT obriga `email` no corpo) não é troca: não
// derruba sessão nem gera auditoria. Sem isto, corrigir um telefone pelo PUT
// deslogaria o colega.
func TestReenviarOMesmoEmailNaoEhTroca(t *testing.T) {
	c := montarCenario(t)

	if _, err := c.svc.Substituir(c.como(c.suporte), c.comum.ID, Atualizar{
		Nome:   httpx.De("Comum"),
		Email:  httpx.De(strings.ToUpper(c.comum.Email)), // caixa diferente é o MESMO e-mail
		RoleID: httpx.De(c.comum.RoleID),
	}); err != nil {
		t.Fatalf("PUT sem troca de e-mail deveria passar: %v", err)
	}
	if c.sessoes.revogadas != 0 || len(c.repo.auditorias) != 0 {
		t.Fatalf("reenviar o mesmo e-mail derrubou a sessão (%d) ou gerou auditoria (%d)",
			c.sessoes.revogadas, len(c.repo.auditorias))
	}
}

// ─────────────────── Trava do último administrador ──────────────────────────

// A trava só vale se a contagem acontecer DEPOIS da trava de concorrência e
// DENTRO da transação que grava. Este teste prende a ordem: sem a trava, a
// contagem é um número que outra requisição já pode ter invalidado.
func TestAContagemDeAdministradoresAconteceSobTrava(t *testing.T) {
	c := montarCenario(t)
	c.repo.administradoresRestantes = 0

	_, err := c.svc.Atualizar(c.como(c.admin), c.gestor.ID, Atualizar{
		RoleID: httpx.De(c.papelSuporte),
	})

	exigirCodigo(t, err, "VALIDATION_ERROR", 422)
	if c.repo.travas == 0 {
		t.Fatal("a contagem de administradores rodou sem trava: a corrida continua aberta")
	}
}

// O DELETE do contrato passa pela mesma trava.
func TestDesativarTambemContaSobTrava(t *testing.T) {
	c := montarCenario(t)
	c.repo.administradoresRestantes = 0

	err := c.svc.Desativar(c.como(c.admin), c.gestor.ID)

	exigirCodigo(t, err, "VALIDATION_ERROR", 422)
	if c.repo.travas == 0 {
		t.Fatal("o DELETE contou administradores sem trava")
	}
}
