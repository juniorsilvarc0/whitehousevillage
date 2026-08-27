package inventario

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// ─────────────────────────── Dublês ─────────────────────────────────

// transacaoDireta executa o bloco sem transação de verdade. Basta para as
// regras testadas aqui: o que se afirma é a DECISÃO (recusar, substituir), e a
// atomicidade quem prova é o teste de integração, contra Postgres.
type transacaoDireta struct{}

func (transacaoDireta) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

// eventoDaTrilha é uma linha que o service mandou auditar.
type eventoDaTrilha struct {
	Entidade string
	Verbo    string
	ID       uuid.UUID
}

// trilhaFalsa guarda o que seria gravado em audit_log. O teste afirma QUE a
// escrita foi auditada e com qual verbo; que a linha entra na transação do
// negócio é o teste de integração de `platform/audit` que prova.
type trilhaFalsa struct {
	eventos []eventoDaTrilha
	erro    error
}

func (t *trilhaFalsa) registrar(entidade, verbo string, id uuid.UUID) error {
	t.eventos = append(t.eventos, eventoDaTrilha{Entidade: entidade, Verbo: verbo, ID: id})
	return t.erro
}

func (t *trilhaFalsa) Criacao(_ context.Context, entidade, verbo string, id uuid.UUID, _ any) error {
	return t.registrar(entidade, verbo, id)
}

func (t *trilhaFalsa) Alteracao(_ context.Context, entidade, verbo string, id uuid.UUID, _, _ any) error {
	return t.registrar(entidade, verbo, id)
}

// verbos devolve os verbos gravados para uma entidade, na ordem.
func (t *trilhaFalsa) verbos(entidade string) []string {
	out := []string{}
	for _, e := range t.eventos {
		if e.Entidade == entidade {
			out = append(out, e.Verbo)
		}
	}
	return out
}

// repoFalso implementa Repositorio guardando o mínimo em memória.
type repoFalso struct {
	produtos   map[uuid.UUID]Produto
	unidades   map[uuid.UUID]Unidade
	composicao map[uuid.UUID][]uuid.UUID

	reservasAtivas     int
	bloqueiosFuturos   int
	produtosDaUnidade  []VinculoDeComposicao
	reservasExclusivas []ReservaExclusiva
	// reservasVivas é o CRÍTICO desta rodada: vendas de pé cuja alocação
	// derivou da composição do produto. Elas são o que impede mexer no
	// conjunto e no `consumes` depois que a casa já foi vendida.
	reservasVivas []ReservaViva

	// Registram o que o service mandou gravar.
	desativouProduto bool
	desativouUnidade bool
	gravou           []uuid.UUID
}

func novoRepoFalso() *repoFalso {
	return &repoFalso{
		produtos:   map[uuid.UUID]Produto{},
		unidades:   map[uuid.UUID]Unidade{},
		composicao: map[uuid.UUID][]uuid.UUID{},
	}
}

func (r *repoFalso) ListarPropriedades(context.Context, uuid.UUID, Filtro) ([]Propriedade, int64, error) {
	return nil, 0, nil
}
func (r *repoFalso) BuscarPropriedade(context.Context, uuid.UUID, uuid.UUID) (Propriedade, error) {
	return Propriedade{}, nil
}
func (r *repoFalso) AtualizarPropriedade(context.Context, uuid.UUID, uuid.UUID, PropriedadeAtualizar) error {
	return nil
}

func (r *repoFalso) ListarProdutos(context.Context, uuid.UUID, Filtro) ([]Produto, int64, error) {
	return nil, 0, nil
}
func (r *repoFalso) BuscarProduto(_ context.Context, _ uuid.UUID, id uuid.UUID) (Produto, error) {
	p, ok := r.produtos[id]
	if !ok {
		return Produto{}, apperr.NotFound("Produto")
	}
	return p, nil
}
func (r *repoFalso) CriarProduto(_ context.Context, _ uuid.UUID, c ProdutoEntrada) (uuid.UUID, error) {
	id := uuid.New()
	r.produtos[id] = Produto{ID: id, Codigo: c.Codigo, Nome: c.Nome, Consome: c.Consome, Ativo: *c.Ativo}
	return id, nil
}
func (r *repoFalso) SubstituirProduto(_ context.Context, _ uuid.UUID, id uuid.UUID, c ProdutoEntrada) error {
	p := r.produtos[id]
	p.Codigo, p.Nome, p.Consome, p.Ativo = c.Codigo, c.Nome, c.Consome, *c.Ativo
	r.desativouProduto = r.desativouProduto || !p.Ativo
	r.produtos[id] = p
	return nil
}
func (r *repoFalso) AtualizarProduto(_ context.Context, _ uuid.UUID, id uuid.UUID, a ProdutoAtualizar) error {
	p := r.produtos[id]
	if v, ok := a.Ativo.Definido(); ok {
		p.Ativo = v
		r.desativouProduto = r.desativouProduto || !v
	}
	r.produtos[id] = p
	return nil
}
func (r *repoFalso) DesativarProduto(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	r.desativouProduto = true
	p := r.produtos[id]
	p.Ativo = false
	r.produtos[id] = p
	return nil
}
func (r *repoFalso) ContarReservasAtivasDoProduto(context.Context, uuid.UUID) (int, error) {
	return r.reservasAtivas, nil
}

func (r *repoFalso) ReservasVivasDoProduto(context.Context, uuid.UUID, uuid.UUID) ([]ReservaViva, error) {
	return r.reservasVivas, nil
}

func (r *repoFalso) Composicao(_ context.Context, id uuid.UUID) ([]UnidadeDaComposicao, error) {
	out := []UnidadeDaComposicao{}
	for _, unidade := range r.composicao[id] {
		u := r.unidades[unidade]
		out = append(out, UnidadeDaComposicao{UnidadeID: u.ID, Codigo: u.Codigo, Nome: u.Nome, Ativa: u.Ativa})
	}
	return out, nil
}
func (r *repoFalso) UnidadesDaPropriedade(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]UnidadeResumida, error) {
	achadas := map[uuid.UUID]UnidadeResumida{}
	for _, id := range ids {
		if u, existe := r.unidades[id]; existe {
			achadas[id] = UnidadeResumida{Codigo: u.Codigo, Ativa: u.Ativa}
		}
	}
	return achadas, nil
}
func (r *repoFalso) SubstituirComposicao(_ context.Context, _ uuid.UUID, id uuid.UUID, ids []uuid.UUID) error {
	r.gravou = append([]uuid.UUID{}, ids...)
	r.composicao[id] = append([]uuid.UUID{}, ids...)
	return nil
}

func (r *repoFalso) ListarUnidades(context.Context, uuid.UUID, Filtro) ([]Unidade, int64, error) {
	return nil, 0, nil
}
func (r *repoFalso) BuscarUnidade(_ context.Context, _ uuid.UUID, id uuid.UUID) (Unidade, error) {
	u, ok := r.unidades[id]
	if !ok {
		return Unidade{}, apperr.NotFound("Unidade")
	}
	return u, nil
}
func (r *repoFalso) CriarUnidade(_ context.Context, _ uuid.UUID, c UnidadeEntrada) (uuid.UUID, error) {
	id := uuid.New()
	r.unidades[id] = Unidade{ID: id, Codigo: c.Codigo, Nome: c.Nome, Ativa: *c.Ativa}
	return id, nil
}
func (r *repoFalso) SubstituirUnidade(_ context.Context, _ uuid.UUID, id uuid.UUID, c UnidadeEntrada) error {
	u := r.unidades[id]
	u.Codigo, u.Nome, u.Ativa = c.Codigo, c.Nome, *c.Ativa
	r.desativouUnidade = r.desativouUnidade || !u.Ativa
	r.unidades[id] = u
	return nil
}
func (r *repoFalso) AtualizarUnidade(_ context.Context, _ uuid.UUID, id uuid.UUID, a UnidadeAtualizar) error {
	u := r.unidades[id]
	if v, ok := a.Ativa.Definido(); ok {
		u.Ativa = v
		r.desativouUnidade = r.desativouUnidade || !v
	}
	r.unidades[id] = u
	return nil
}
func (r *repoFalso) DesativarUnidade(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	r.desativouUnidade = true
	u := r.unidades[id]
	u.Ativa = false
	r.unidades[id] = u
	return nil
}
func (r *repoFalso) ContarBloqueiosFuturosDaUnidade(context.Context, uuid.UUID, uuid.UUID) (int, error) {
	return r.bloqueiosFuturos, nil
}
func (r *repoFalso) ProdutosQueUsamAUnidade(context.Context, uuid.UUID) ([]VinculoDeComposicao, error) {
	return r.produtosDaUnidade, nil
}
func (r *repoFalso) ReservasExclusivasSemAUnidade(context.Context, uuid.UUID, uuid.UUID) ([]ReservaExclusiva, error) {
	return r.reservasExclusivas, nil
}

// ─────────────────────────── Ferramentas ────────────────────────────

func comAtor(propriedade uuid.UUID) context.Context {
	return auth.WithUser(context.Background(), &auth.Usuario{
		ID:         uuid.New(),
		PropertyID: propriedade,
		Permissoes: auth.Conjunto{},
	})
}

func (r *repoFalso) comProduto(codigo string, ativo bool) uuid.UUID {
	id := uuid.New()
	r.produtos[id] = Produto{ID: id, Codigo: codigo, Ativo: ativo, Consome: ConsomeTodosMembros}
	return id
}

func (r *repoFalso) comUnidade(codigo string) uuid.UUID {
	return r.comUnidadeAssim(codigo, true)
}

func (r *repoFalso) comUnidadeAssim(codigo string, ativa bool) uuid.UUID {
	id := uuid.New()
	r.unidades[id] = Unidade{ID: id, Codigo: codigo, Ativa: ativa}
	return id
}

func codigoDoErro(t *testing.T, err error) string {
	t.Helper()
	var e *apperr.Error
	if !errors.As(err, &e) {
		t.Fatalf("erro fora do vocabulário da API: %v", err)
	}
	return e.Code
}

// ─────────────────────────── Testes ─────────────────────────────────

// Não deixar apagar o chão de quem está de pé: produto com reserva viva não
// pode ser desativado, e a recusa precisa dizer quantas reservas segurando.
func TestDesativarProdutoComReservaAtivaEhRecusado(t *testing.T) {
	repo := novoRepoFalso()
	repo.reservasAtivas = 3
	id := repo.comProduto("completa", true)
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	err := svc.DesativarProduto(comAtor(uuid.New()), id)
	if err == nil {
		t.Fatal("desativar produto com reserva ativa deveria ser recusado")
	}
	if codigo := codigoDoErro(t, err); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE", codigo)
	}
	var e *apperr.Error
	errors.As(err, &e)
	if e.Status() != 409 {
		t.Fatalf("status = %d, esperado 409", e.Status())
	}
	detalhes, ok := e.Details.(map[string]any)
	if !ok || detalhes["reservations_count"] != 3 {
		t.Fatalf("details deveria trazer reservations_count = 3; veio %v", e.Details)
	}
	if repo.desativouProduto {
		t.Fatal("o produto foi desativado apesar da recusa")
	}
}

func TestDesativarProdutoSemReservaAtivaGrava(t *testing.T) {
	repo := novoRepoFalso()
	id := repo.comProduto("cobertura", true)
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	if err := svc.DesativarProduto(comAtor(uuid.New()), id); err != nil {
		t.Fatalf("desativação legítima falhou: %v", err)
	}
	if !repo.desativouProduto {
		t.Fatal("o produto não foi desativado")
	}
}

// DELETE repetido é no-op, não erro: a tela que perdeu a resposta e tentou de
// novo não deve ver conflito.
func TestDesativarProdutoJaInativoEhNoOp(t *testing.T) {
	repo := novoRepoFalso()
	repo.reservasAtivas = 5 // nem consultado: o produto já está fora do ar
	id := repo.comProduto("cobertura", false)
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	if err := svc.DesativarProduto(comAtor(uuid.New()), id); err != nil {
		t.Fatalf("desativar produto já inativo deveria ser no-op; veio %v", err)
	}
	if repo.desativouProduto {
		t.Fatal("gravou de novo o que já estava gravado")
	}
}

// Unidade com estadia futura marcada não sai do ar: a operação deixaria de
// saber qual quarto preparar para uma reserva de pé.
func TestDesativarUnidadeComOcupacaoFuturaEhRecusado(t *testing.T) {
	repo := novoRepoFalso()
	repo.bloqueiosFuturos = 2
	id := repo.comUnidade("AP-01")
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	err := svc.DesativarUnidade(comAtor(uuid.New()), id)
	if codigo := codigoDoErro(t, err); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE", codigo)
	}
	if repo.desativouUnidade {
		t.Fatal("a unidade foi desativada apesar da recusa")
	}
}

// Unidade que ainda compõe produto também é recusada: a venda seguinte da
// Completa travaria sete unidades em vez de oito, e a exclusividade sumiria
// sem ninguém ter mudado regra nenhuma.
func TestDesativarUnidadeQueCompoeProdutoEhRecusado(t *testing.T) {
	repo := novoRepoFalso()
	repo.produtosDaUnidade = []VinculoDeComposicao{
		{Codigo: "apto-2s", Consome: ConsomeUmMembro},
		{Codigo: "completa", Consome: ConsomeTodosMembros},
	}
	id := repo.comUnidade("AP-01")
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	err := svc.DesativarUnidade(comAtor(uuid.New()), id)
	if codigo := codigoDoErro(t, err); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE", codigo)
	}
	var e *apperr.Error
	errors.As(err, &e)
	detalhes := e.Details.(map[string]any)
	if produtos, ok := detalhes["unit_types"].([]string); !ok || len(produtos) != 2 {
		t.Fatalf("details deveria listar os produtos que seguram a unidade; veio %v", e.Details)
	}
}

func TestDesativarUnidadeLivreGrava(t *testing.T) {
	repo := novoRepoFalso()
	id := repo.comUnidade("AP-09")
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	if err := svc.DesativarUnidade(comAtor(uuid.New()), id); err != nil {
		t.Fatalf("desativação legítima falhou: %v", err)
	}
	if !repo.desativouUnidade {
		t.Fatal("a unidade não foi desativada")
	}
}

// PUT de composição SUBSTITUI: o que não veio deixa de fazer parte. É a mesma
// semântica do PUT /roles/{id}/permissions, e o que permite a tela mandar o
// estado inteiro da grade sem calcular diferença.
func TestSubstituirComposicaoTrocaOConjuntoInteiro(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProduto("completa", true)
	a, b, c := repo.comUnidade("AP-01"), repo.comUnidade("AP-02"), repo.comUnidade("AP-03")
	repo.composicao[produto] = []uuid.UUID{a, b}
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	gravada, err := svc.SubstituirComposicao(comAtor(uuid.New()), produto,
		ComposicaoEntrada{UnidadeIDs: []uuid.UUID{b, c}})
	if err != nil {
		t.Fatalf("substituição falhou: %v", err)
	}
	if len(gravada) != 2 {
		t.Fatalf("composição resultante = %d itens, esperado 2", len(gravada))
	}
	for _, item := range gravada {
		if item.UnidadeID == a {
			t.Fatal("AP-01 saiu do corpo e continuou na composição: o PUT acumulou em vez de substituir")
		}
	}
}

// Unidade de outra propriedade é recusada por índice, com 422. A FK aceitaria
// (units é uma tabela só) e o produto passaria a consumir apartamento de outra
// casa.
func TestSubstituirComposicaoRecusaUnidadeDeForaDaPropriedade(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProduto("completa", true)
	conhecida := repo.comUnidade("AP-01")
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	_, err := svc.SubstituirComposicao(comAtor(uuid.New()), produto,
		ComposicaoEntrada{UnidadeIDs: []uuid.UUID{conhecida, uuid.New()}})
	if codigo := codigoDoErro(t, err); codigo != "VALIDATION_ERROR" {
		t.Fatalf("code = %s, esperado VALIDATION_ERROR", codigo)
	}
	if repo.gravou != nil {
		t.Fatal("gravou a composição mesmo com unidade inválida no corpo")
	}
}

func TestComposicaoDeProdutoInexistenteEh404(t *testing.T) {
	repo := novoRepoFalso()
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	_, err := svc.Composicao(comAtor(uuid.New()), uuid.New())
	if codigo := codigoDoErro(t, err); codigo != "NOT_FOUND" {
		t.Fatalf("code = %s, esperado NOT_FOUND", codigo)
	}
}

// Sem identidade não há propriedade, e sem propriedade não há como isolar o
// cadastro. Falhar alto é melhor do que operar sobre a casa errada.
func TestServiceExigeIdentidadeNoContexto(t *testing.T) {
	svc := NewService(novoRepoFalso(), transacaoDireta{}, &trilhaFalsa{})

	_, _, err := svc.ListarProdutos(context.Background(), Filtro{Pagina: 1, PorPagina: 25})
	if codigo := codigoDoErro(t, err); codigo != "UNAUTHORIZED" {
		t.Fatalf("code = %s, esperado UNAUTHORIZED", codigo)
	}
}

// ─────────── CRÍTICO 1: a desativação não tem porta dos fundos ───────

// O defeito medido pela revisão: o DELETE recusava e o PATCH devolvia 200
// desativando a MESMA unidade. Como a consulta de candidatas filtra `u.active`,
// a venda seguinte da White House Completa (all_members, 8 unidades) travava
// sete — a oitava ficava livre para outra venda, sem a EXCLUDE ter o que
// detectar, porque a linha que faltava nunca foi inserida.
func TestPatchNaoDesativaUnidadeQueODeleteRecusa(t *testing.T) {
	casos := []struct {
		nome    string
		preparo func(*repoFalso)
	}{
		{"ocupação futura", func(r *repoFalso) { r.bloqueiosFuturos = 1 }},
		{"compõe produto exclusivo", func(r *repoFalso) {
			r.produtosDaUnidade = []VinculoDeComposicao{
				{Codigo: "completa", Consome: ConsomeTodosMembros},
			}
		}},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			repo := novoRepoFalso()
			c.preparo(repo)
			id := repo.comUnidade("AP-03")
			trilha := &trilhaFalsa{}
			svc := NewService(repo, transacaoDireta{}, trilha)
			ctx := comAtor(uuid.New())

			// Controle: o DELETE recusa. É a guarda que já existia.
			if err := svc.DesativarUnidade(ctx, id); codigoDoErro(t, err) != "RESOURCE_IN_USE" {
				t.Fatalf("controle — DELETE deveria recusar; veio %v", err)
			}

			_, err := svc.AtualizarUnidade(ctx, id, UnidadeAtualizar{Ativa: httpx.De(false)})
			if codigo := codigoDoErro(t, err); codigo != "RESOURCE_IN_USE" {
				t.Fatalf("PATCH active=false: code = %s, esperado RESOURCE_IN_USE", codigo)
			}
			if repo.unidades[id].Ativa == false {
				t.Fatal("a unidade foi desativada pelo PATCH apesar da recusa")
			}

			// PUT carrega `active` como qualquer outro campo: mesma guarda.
			_, err = svc.SubstituirUnidade(ctx, id, UnidadeEntrada{
				Codigo: "AP-03", Nome: "Apartamento 3", Ativa: novo(false)})
			if codigo := codigoDoErro(t, err); codigo != "RESOURCE_IN_USE" {
				t.Fatalf("PUT active=false: code = %s, esperado RESOURCE_IN_USE", codigo)
			}
			if !repo.unidades[id].Ativa {
				t.Fatal("a unidade foi desativada pelo PUT apesar da recusa")
			}
		})
	}
}

// A recusa precisa nomear o dano: `all_members` sai destacado porque a ação do
// operador é outra — não é esperar a estadia passar, é redefinir o produto (e
// rever o preço dele).
func TestRecusaDaDesativacaoSeparaProdutoExclusivo(t *testing.T) {
	repo := novoRepoFalso()
	repo.produtosDaUnidade = []VinculoDeComposicao{
		{Codigo: "apto-2s", Consome: ConsomeUmMembro},
		{Codigo: "completa", Consome: ConsomeTodosMembros},
	}
	id := repo.comUnidade("AP-03")
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	_, err := svc.AtualizarUnidade(comAtor(uuid.New()), id, UnidadeAtualizar{Ativa: httpx.De(false)})
	var e *apperr.Error
	if !errors.As(err, &e) {
		t.Fatalf("erro fora do vocabulário da API: %v", err)
	}
	detalhes := e.Details.(map[string]any)
	if todos := detalhes["unit_types"].([]string); len(todos) != 2 {
		t.Fatalf("details.unit_types = %v, esperado os dois produtos", todos)
	}
	exclusivos := detalhes["exclusive_unit_types"].([]string)
	if len(exclusivos) != 1 || exclusivos[0] != "completa" {
		t.Fatalf("details.exclusive_unit_types = %v, esperado [completa]", exclusivos)
	}
	if !strings.Contains(e.Message, "completa") {
		t.Fatalf("a mensagem não nomeia o produto exclusivo: %q", e.Message)
	}
}

// PATCH que não fala de `active` não é desativação: `sort_order` sozinho não
// pode disparar a guarda, senão o cadastro trava para sempre.
func TestPatchSemActiveNaoDisparaAGuarda(t *testing.T) {
	repo := novoRepoFalso()
	repo.bloqueiosFuturos = 3
	id := repo.comUnidade("AP-01")
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	if _, err := svc.AtualizarUnidade(comAtor(uuid.New()), id,
		UnidadeAtualizar{Ordem: httpx.De(7)}); err != nil {
		t.Fatalf("PATCH de sort_order em unidade ocupada deveria passar; veio %v", err)
	}
	if verbos := trilha.verbos(entidadeUnidade); len(verbos) != 1 || verbos[0] != "alterado" {
		t.Fatalf("verbos = %v, esperado [alterado]", verbos)
	}
}

// PATCH que reafirma `active:true` numa unidade já ativa também não é
// transição: não há decisão nova para guardar.
func TestPatchQueReafirmaOEstadoAtualPassa(t *testing.T) {
	repo := novoRepoFalso()
	repo.bloqueiosFuturos = 3
	id := repo.comUnidade("AP-01")
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	if _, err := svc.AtualizarUnidade(comAtor(uuid.New()), id,
		UnidadeAtualizar{Ativa: httpx.De(true)}); err != nil {
		t.Fatalf("reafirmar active=true deveria passar; veio %v", err)
	}
}

// O caminho de volta: reativar a unidade que ficou de fora de uma venda
// exclusiva ENTREGA o buraco. A recusa nomeia as reservas porque quem resolve
// não é este módulo.
func TestReativarUnidadeQueFaltouNumaVendaExclusivaEhRecusado(t *testing.T) {
	repo := novoRepoFalso()
	repo.reservasExclusivas = []ReservaExclusiva{
		{Codigo: "WH-2026-0002", ProdutoCodigo: "completa", CheckIn: "2026-10-06", CheckOut: "2026-10-08"},
	}
	id := repo.comUnidadeAssim("AP-03", false)
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	_, err := svc.AtualizarUnidade(comAtor(uuid.New()), id, UnidadeAtualizar{Ativa: httpx.De(true)})
	if codigo := codigoDoErro(t, err); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE", codigo)
	}
	var e *apperr.Error
	errors.As(err, &e)
	if !strings.Contains(e.Message, "WH-2026-0002") {
		t.Fatalf("a mensagem não nomeia a reserva conflitante: %q", e.Message)
	}
	if repo.unidades[id].Ativa {
		t.Fatal("a unidade foi reativada apesar da recusa")
	}
}

// Sem venda exclusiva pendurada, reativar é operação comum e precisa passar —
// senão a guarda vira cadeado sem chave.
func TestReativarUnidadeSemPendenciaPassaEEhAuditado(t *testing.T) {
	repo := novoRepoFalso()
	id := repo.comUnidadeAssim("AP-03", false)
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	if _, err := svc.AtualizarUnidade(comAtor(uuid.New()), id,
		UnidadeAtualizar{Ativa: httpx.De(true)}); err != nil {
		t.Fatalf("reativação legítima falhou: %v", err)
	}
	if verbos := trilha.verbos(entidadeUnidade); len(verbos) != 1 || verbos[0] != verboReativado {
		t.Fatalf("verbos = %v, esperado [%s]", verbos, verboReativado)
	}
}

// PATCH de produto com reserva viva: a mesma simetria do DELETE.
func TestPatchNaoDesativaProdutoComReservaAtiva(t *testing.T) {
	repo := novoRepoFalso()
	repo.reservasAtivas = 2
	id := repo.comProduto("completa", true)
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	_, err := svc.AtualizarProduto(comAtor(uuid.New()), id, ProdutoAtualizar{Ativo: httpx.De(false)})
	if codigo := codigoDoErro(t, err); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE", codigo)
	}
	if !repo.produtos[id].Ativo {
		t.Fatal("o produto foi desativado pelo PATCH apesar da recusa")
	}
}

// Unidade INATIVA na composição é a mesma falha entrando pela porta oposta: o
// produto declara N unidades e a venda aloca N-1.
func TestComposicaoRecusaUnidadeInativa(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProduto("completa", true)
	viva := repo.comUnidade("AP-01")
	morta := repo.comUnidadeAssim("AP-03", false)
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)

	_, err := svc.SubstituirComposicao(comAtor(uuid.New()), produto,
		ComposicaoEntrada{UnidadeIDs: []uuid.UUID{viva, morta}})
	if codigo := codigoDoErro(t, err); codigo != "VALIDATION_ERROR" {
		t.Fatalf("code = %s, esperado VALIDATION_ERROR", codigo)
	}
	var e *apperr.Error
	errors.As(err, &e)
	detalhes := e.Details.(map[string]string)
	if msg, ok := detalhes["unit_ids[1]"]; !ok || !strings.Contains(msg, "AP-03") {
		t.Fatalf("details deveria apontar o índice da unidade inativa; veio %v", e.Details)
	}
	if repo.gravou != nil {
		t.Fatal("gravou a composição mesmo com unidade inativa no corpo")
	}
}

// ─────────────────────────── ALTO 3: auditoria ──────────────────────

// Toda escrita do módulo grava trilha, e a desativação grava um verbo PRÓPRIO:
// "quem desativou AP-03?" precisa ser uma consulta por `action`, não um LIKE
// dentro do documento `after`. As três portas da desativação — DELETE, PATCH e
// PUT — gravam o MESMO verbo, porque a trilha responde pela decisão tomada, não
// pelo verbo HTTP que a carregou.
func TestDesativacaoPelasTresPortasGravaOMesmoVerbo(t *testing.T) {
	portas := map[string]func(*Service, context.Context, uuid.UUID) error{
		"DELETE": func(svc *Service, ctx context.Context, id uuid.UUID) error {
			return svc.DesativarUnidade(ctx, id)
		},
		"PATCH": func(svc *Service, ctx context.Context, id uuid.UUID) error {
			_, err := svc.AtualizarUnidade(ctx, id, UnidadeAtualizar{Ativa: httpx.De(false)})
			return err
		},
		"PUT": func(svc *Service, ctx context.Context, id uuid.UUID) error {
			_, err := svc.SubstituirUnidade(ctx, id, UnidadeEntrada{
				Codigo: "AP-09", Nome: "Apartamento 9", Ativa: novo(false)})
			return err
		},
	}

	for nome, chamar := range portas {
		t.Run(nome, func(t *testing.T) {
			repo := novoRepoFalso()
			id := repo.comUnidade("AP-09")
			trilha := &trilhaFalsa{}
			svc := NewService(repo, transacaoDireta{}, trilha)

			if err := chamar(svc, comAtor(uuid.New()), id); err != nil {
				t.Fatalf("desativação legítima falhou: %v", err)
			}
			verbos := trilha.verbos(entidadeUnidade)
			if len(verbos) != 1 || verbos[0] != verboDesativado {
				t.Fatalf("verbos = %v, esperado [%s]", verbos, verboDesativado)
			}
			if trilha.eventos[0].ID != id {
				t.Fatalf("a trilha aponta para outra entidade: %v", trilha.eventos[0].ID)
			}
		})
	}
}

// Criação e troca de composição também entram na trilha: sem elas, "quem tirou
// AP-03 da Completa?" continua sem resposta, e essa é a outra metade do mesmo
// dano.
func TestCriacaoEComposicaoEntramNaTrilha(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProduto("completa", true)
	a, b := repo.comUnidade("AP-01"), repo.comUnidade("AP-02")
	repo.composicao[produto] = []uuid.UUID{a}
	trilha := &trilhaFalsa{}
	svc := NewService(repo, transacaoDireta{}, trilha)
	ctx := comAtor(uuid.New())

	if _, err := svc.CriarUnidade(ctx, UnidadeEntrada{Codigo: "AP-10", Nome: "Apartamento 10"}); err != nil {
		t.Fatalf("criação falhou: %v", err)
	}
	if verbos := trilha.verbos(entidadeUnidade); len(verbos) != 1 || verbos[0] != "criado" {
		t.Fatalf("verbos de units = %v, esperado [criado]", verbos)
	}

	if _, err := svc.SubstituirComposicao(ctx, produto,
		ComposicaoEntrada{UnidadeIDs: []uuid.UUID{a, b}}); err != nil {
		t.Fatalf("substituição falhou: %v", err)
	}
	if verbos := trilha.verbos(entidadeComposicao); len(verbos) != 1 || verbos[0] != verboSubstituida {
		t.Fatalf("verbos de unit_type_members = %v, esperado [%s]", verbos, verboSubstituida)
	}
}

// Trilha que falha DERRUBA a escrita. É a única forma de "toda escrita é
// auditada" ser verdade: com o erro engolido, a operação passa e o rastro some
// — que é exatamente o estado em que a Fase 1 foi entregue (1626 tuplas de
// negócio, zero linhas em audit_log).
func TestFalhaDaTrilhaDerrubaAEscrita(t *testing.T) {
	repo := novoRepoFalso()
	id := repo.comUnidade("AP-09")
	trilha := &trilhaFalsa{erro: errors.New("audit_log indisponível")}
	svc := NewService(repo, transacaoDireta{}, trilha)

	if err := svc.DesativarUnidade(comAtor(uuid.New()), id); err == nil {
		t.Fatal("a desativação passou sem a trilha ter sido gravada")
	}
}

// ═══════════════════════════════════════════════════════════════════════
// O CRÍTICO DESTA RODADA: a exclusividade cai pela COMPOSIÇÃO.
//
// As guardas anteriores protegiam COLUNAS (`units.active`, `unit_types.active`)
// e todas passavam. O revisor entrou pelo CONJUNTO: com a casa vendida e oito
// blocos de pé, acrescentar um nono apartamento à composição devolvia 200, e a
// venda passava a declarar nove unidades segurando oito. A `EXCLUDE` não vê,
// porque o buraco é a AUSÊNCIA de uma linha em stay_blocks.
// ═══════════════════════════════════════════════════════════════════════

// comReservaViva registra uma venda de pé ocupando as unidades informadas.
func (r *repoFalso) comReservaViva(codigo string, unidades ...uuid.UUID) {
	viva := ReservaViva{
		ID: uuid.New(), Codigo: codigo,
		CheckIn: "2033-05-10", CheckOut: "2033-05-13",
		UnidadeIDs: append([]uuid.UUID{}, unidades...),
	}
	for _, id := range unidades {
		viva.Unidades = append(viva.Unidades, r.unidades[id].Codigo)
	}
	r.reservasVivas = append(r.reservasVivas, viva)
}

// composicaoDe monta o produto com as unidades já vinculadas.
func (r *repoFalso) composicaoDe(produto uuid.UUID, unidades ...uuid.UUID) {
	r.composicao[produto] = append([]uuid.UUID{}, unidades...)
}

func TestAcrescentarUnidadeNaComposicaoDeCasaVendidaEhRecusado(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProduto("completa", true)
	a, b := repo.comUnidade("AP-01"), repo.comUnidade("AP-02")
	nova := repo.comUnidade("AP-99")
	repo.composicaoDe(produto, a, b)
	repo.comReservaViva("WH-2026-0011", a, b)

	svc := NewService(repo, transacaoDireta{}, &trilhaFalsa{})
	_, err := svc.SubstituirComposicao(comAtor(uuid.New()), produto,
		ComposicaoEntrada{UnidadeIDs: []uuid.UUID{a, b, nova}})

	if err == nil {
		t.Fatal("acrescentar unidade à composição de uma casa já vendida deveria ser recusado")
	}
	if codigo := codigoDoErro(t, err); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE", codigo)
	}
	var e *apperr.Error
	errors.As(err, &e)
	if e.Status() != 409 {
		t.Fatalf("status = %d, esperado 409", e.Status())
	}
	detalhes, _ := e.Details.(map[string]any)
	if fmt.Sprint(detalhes["adding_unit_codes"]) != "[AP-99]" {
		t.Errorf("details.adding_unit_codes = %v, esperado [AP-99]", detalhes["adding_unit_codes"])
	}
	if detalhes["reservations_count"] != 1 {
		t.Errorf("details.reservations_count = %v, esperado 1", detalhes["reservations_count"])
	}
	// A recusa é inútil se não disser QUAL reserva resolver.
	if !strings.Contains(e.Message, "WH-2026-0011") {
		t.Errorf("a mensagem não nomeia a reserva a resolver: %q", e.Message)
	}
	if repo.gravou != nil {
		t.Fatalf("a composição foi gravada apesar da recusa: %v", repo.gravou)
	}
}

// A REMOÇÃO é a mesma falha pela porta oposta: a próxima venda da Completa
// travaria menos unidades do que promete, sem ninguém ter tocado em `active`.
func TestRemoverUnidadeDaComposicaoDeCasaVendidaEhRecusado(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProduto("completa", true)
	a, b, c := repo.comUnidade("AP-01"), repo.comUnidade("AP-02"), repo.comUnidade("AP-03")
	repo.composicaoDe(produto, a, b, c)
	repo.comReservaViva("WH-2026-0011", a, b, c)

	svc := NewService(repo, transacaoDireta{}, &trilhaFalsa{})
	_, err := svc.SubstituirComposicao(comAtor(uuid.New()), produto,
		ComposicaoEntrada{UnidadeIDs: []uuid.UUID{a, b}})

	if err == nil {
		t.Fatal("remover unidade da composição de uma casa já vendida deveria ser recusado")
	}
	if codigo := codigoDoErro(t, err); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE", codigo)
	}
	var e *apperr.Error
	errors.As(err, &e)
	detalhes, _ := e.Details.(map[string]any)
	if fmt.Sprint(detalhes["removing_unit_codes"]) != "[AP-03]" {
		t.Errorf("details.removing_unit_codes = %v, esperado [AP-03]", detalhes["removing_unit_codes"])
	}
	if repo.gravou != nil {
		t.Fatalf("a composição foi gravada apesar da recusa: %v", repo.gravou)
	}
}

// Sem venda viva a composição muda normalmente — a guarda protege a venda, não
// o cadastro.
func TestComposicaoMudaLivrementeSemVendaViva(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProduto("completa", true)
	a, b := repo.comUnidade("AP-01"), repo.comUnidade("AP-02")
	repo.composicaoDe(produto, a)

	svc := NewService(repo, transacaoDireta{}, &trilhaFalsa{})
	if _, err := svc.SubstituirComposicao(comAtor(uuid.New()), produto,
		ComposicaoEntrada{UnidadeIDs: []uuid.UUID{a, b}}); err != nil {
		t.Fatalf("composição sem venda viva deveria ser gravada: %v", err)
	}
	if len(repo.gravou) != 2 {
		t.Fatalf("gravou %v, esperado as duas unidades", repo.gravou)
	}
}

// Reenviar o MESMO conjunto é no-op mesmo com venda viva: a tela salva o
// formulário sem ninguém ter tocado na grade, e recusar isso ensinaria o
// operador a temer o botão.
func TestReenviarAMesmaComposicaoNaoEhRecusadoComVendaViva(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProduto("completa", true)
	a, b := repo.comUnidade("AP-01"), repo.comUnidade("AP-02")
	repo.composicaoDe(produto, a, b)
	repo.comReservaViva("WH-2026-0011", a, b)

	svc := NewService(repo, transacaoDireta{}, &trilhaFalsa{})
	// Ordem trocada de propósito: o que importa é o CONJUNTO, não a sequência.
	if _, err := svc.SubstituirComposicao(comAtor(uuid.New()), produto,
		ComposicaoEntrada{UnidadeIDs: []uuid.UUID{b, a}}); err != nil {
		t.Fatalf("reenviar o mesmo conjunto deveria passar: %v", err)
	}
}

// No `one_member` a venda escolheu UMA unidade e a segurou: crescer o pool não
// muda o que ela segura. Recusar toda adição travaria a operação normal — o
// apto-2s quase sempre tem venda viva.
func TestAcrescentarUnidadeEmOneMemberComVendaVivaEhPermitido(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProdutoAssim("apto-2s", ConsomeUmMembro)
	a, b := repo.comUnidade("AP-01"), repo.comUnidade("AP-02")
	repo.composicaoDe(produto, a)
	repo.comReservaViva("WH-2026-0012", a)

	svc := NewService(repo, transacaoDireta{}, &trilhaFalsa{})
	if _, err := svc.SubstituirComposicao(comAtor(uuid.New()), produto,
		ComposicaoEntrada{UnidadeIDs: []uuid.UUID{a, b}}); err != nil {
		t.Fatalf("acrescentar unidade a um one_member deveria passar: %v", err)
	}
}

// O que o `one_member` recusa é tirar a unidade que uma venda viva OCUPA: a
// estadia continuaria hospedando fora do produto vendido, e toda derivação
// produto → composição → unidades a deixaria de fora.
func TestRemoverUnidadeOcupadaDeOneMemberEhRecusado(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProdutoAssim("apto-2s", ConsomeUmMembro)
	a, b := repo.comUnidade("AP-01"), repo.comUnidade("AP-02")
	repo.composicaoDe(produto, a, b)
	repo.comReservaViva("WH-2026-0012", a)

	svc := NewService(repo, transacaoDireta{}, &trilhaFalsa{})
	_, err := svc.SubstituirComposicao(comAtor(uuid.New()), produto,
		ComposicaoEntrada{UnidadeIDs: []uuid.UUID{b}})
	if err == nil {
		t.Fatal("tirar da composição a unidade ocupada deveria ser recusado")
	}
	if codigo := codigoDoErro(t, err); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE", codigo)
	}
	var e *apperr.Error
	errors.As(err, &e)
	if !strings.Contains(e.Message, "AP-01") || !strings.Contains(e.Message, "WH-2026-0012") {
		t.Errorf("a mensagem não nomeia a unidade e a reserva: %q", e.Message)
	}
}

// Tirar unidade LIVRE de um `one_member` com venda viva em OUTRA unidade é
// operação legítima — o pool encolhe, ninguém dorme onde não devia.
func TestRemoverUnidadeLivreDeOneMemberEhPermitido(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProdutoAssim("apto-2s", ConsomeUmMembro)
	a, b := repo.comUnidade("AP-01"), repo.comUnidade("AP-02")
	repo.composicaoDe(produto, a, b)
	repo.comReservaViva("WH-2026-0012", a)

	svc := NewService(repo, transacaoDireta{}, &trilhaFalsa{})
	if _, err := svc.SubstituirComposicao(comAtor(uuid.New()), produto,
		ComposicaoEntrada{UnidadeIDs: []uuid.UUID{a}}); err != nil {
		t.Fatalf("tirar unidade livre de um one_member deveria passar: %v", err)
	}
}

// ─────────────── `consumes`: a mesma falha por outra coluna ───────────────

// all_members → one_member com venda viva: a casa inteira vendida por R$ 20 mil
// passaria a entregar UM apartamento, e os outros sete ficariam livres para
// estranhos. Medido ao vivo contra Postgres.
func TestTrocarConsumesDeProdutoVendidoEhRecusadoNoPatch(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProduto("completa", true)
	a := repo.comUnidade("AP-01")
	repo.composicaoDe(produto, a)
	repo.comReservaViva("WH-2026-0011", a)

	svc := NewService(repo, transacaoDireta{}, &trilhaFalsa{})
	_, err := svc.AtualizarProduto(comAtor(uuid.New()), produto,
		ProdutoAtualizar{Consome: httpx.De(ConsomeUmMembro)})
	if err == nil {
		t.Fatal("trocar consumes com venda viva deveria ser recusado")
	}
	if codigo := codigoDoErro(t, err); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE", codigo)
	}
	var e *apperr.Error
	errors.As(err, &e)
	detalhes, _ := e.Details.(map[string]any)
	if detalhes["current_consumes"] != ConsomeTodosMembros ||
		detalhes["requested_consumes"] != ConsomeUmMembro {
		t.Errorf("details = %v, esperado a transição all_members → one_member", detalhes)
	}
	if repo.produtos[produto].Consome != ConsomeTodosMembros {
		t.Fatal("o consumo foi trocado apesar da recusa")
	}
}

// A mesma decisão pela outra porta: o PUT reenvia o cadastro inteiro, e sem
// guarda quem não troca pelo PATCH troca pelo PUT — foi assim que a guarda de
// `active` foi contornada na rodada passada.
func TestTrocarConsumesDeProdutoVendidoEhRecusadoNoPut(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProduto("completa", true)
	a := repo.comUnidade("AP-01")
	repo.composicaoDe(produto, a)
	repo.comReservaViva("WH-2026-0011", a)

	svc := NewService(repo, transacaoDireta{}, &trilhaFalsa{})
	_, err := svc.SubstituirProduto(comAtor(uuid.New()), produto, ProdutoEntrada{
		Codigo: "completa", Nome: "White House Completa", Consome: ConsomeUmMembro,
	})
	if err == nil {
		t.Fatal("trocar consumes pelo PUT com venda viva deveria ser recusado")
	}
	if codigo := codigoDoErro(t, err); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE", codigo)
	}
	if repo.produtos[produto].Consome != ConsomeTodosMembros {
		t.Fatal("o consumo foi trocado apesar da recusa")
	}
}

// one_member → all_members é recusado pelo mesmo motivo, ao contrário: a venda
// de UM apartamento passaria a pertencer a um produto que declara o conjunto
// inteiro, e a invariante `|reservation_units| == |composição|` nasceria
// quebrada.
func TestTrocarConsumesDeOneMemberParaAllMembersComVendaVivaEhRecusado(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProdutoAssim("apto-2s", ConsomeUmMembro)
	a, b := repo.comUnidade("AP-01"), repo.comUnidade("AP-02")
	repo.composicaoDe(produto, a, b)
	repo.comReservaViva("WH-2026-0012", a)

	svc := NewService(repo, transacaoDireta{}, &trilhaFalsa{})
	_, err := svc.AtualizarProduto(comAtor(uuid.New()), produto,
		ProdutoAtualizar{Consome: httpx.De(ConsomeTodosMembros)})
	if err == nil {
		t.Fatal("one_member → all_members com venda viva deveria ser recusado")
	}
	if codigo := codigoDoErro(t, err); codigo != "RESOURCE_IN_USE" {
		t.Fatalf("code = %s, esperado RESOURCE_IN_USE", codigo)
	}
}

// PATCH que não menciona `consumes` não é tentativa de troca: o Opt ausente é o
// terceiro estado, e confundi-lo com "trocar" faria todo PATCH de `sort_order`
// bater na guarda.
func TestPatchSemConsumesNaoEsbarraNaGuardaDoConsumo(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProduto("completa", true)
	a := repo.comUnidade("AP-01")
	repo.composicaoDe(produto, a)
	repo.comReservaViva("WH-2026-0011", a)

	svc := NewService(repo, transacaoDireta{}, &trilhaFalsa{})
	if _, err := svc.AtualizarProduto(comAtor(uuid.New()), produto,
		ProdutoAtualizar{Ordem: httpx.De(7)}); err != nil {
		t.Fatalf("PATCH de sort_order com venda viva deveria passar: %v", err)
	}
}

// Reenviar o MESMO `consumes` é no-op — o PUT reenvia o cadastro inteiro toda
// vez, e recusar isso tornaria o produto vendido inteiramente imutável.
func TestReenviarOMesmoConsumesNaoEhRecusado(t *testing.T) {
	repo := novoRepoFalso()
	produto := repo.comProduto("completa", true)
	a := repo.comUnidade("AP-01")
	repo.composicaoDe(produto, a)
	repo.comReservaViva("WH-2026-0011", a)

	svc := NewService(repo, transacaoDireta{}, &trilhaFalsa{})
	if _, err := svc.SubstituirProduto(comAtor(uuid.New()), produto, ProdutoEntrada{
		Codigo: "completa", Nome: "White House Completa", Consome: ConsomeTodosMembros,
	}); err != nil {
		t.Fatalf("reenviar o mesmo consumes deveria passar: %v", err)
	}
}

// comProdutoAssim cria o produto com o consumo pedido — `comProduto` sempre
// nasce `all_members`, e metade destes testes precisa do outro lado.
func (r *repoFalso) comProdutoAssim(codigo, consome string) uuid.UUID {
	id := uuid.New()
	r.produtos[id] = Produto{ID: id, Codigo: codigo, Ativo: true, Consome: consome}
	return id
}
