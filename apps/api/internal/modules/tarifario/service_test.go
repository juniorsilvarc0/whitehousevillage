package tarifario

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// ─────────────────────────── Dublês ─────────────────────────────────────────

// repoFalso implementa só os métodos que cada teste usa. O `Repositorio`
// embutido (nulo) faz o tipo satisfazer a interface inteira; qualquer método não
// implementado que alguém chamar por engano estoura no teste em vez de passar
// despercebido.
type repoFalso struct {
	Repositorio

	propriedade uuid.UUID

	tabelas           map[uuid.UUID]TabelaDeTarifas
	tarifas           map[uuid.UUID]Tarifa
	feriados          map[uuid.UUID]Feriado
	periodos          map[uuid.UUID]PeriodoEspecial
	minimos           map[uuid.UUID]MinimoDeNoites
	criados           map[string]uuid.UUID
	vigentesTotal     int
	vigentesRestantes int
	desativadas       []uuid.UUID

	politicas   []PoliticaComercial
	publicacoes int

	// Grade: o que o repositório TINHA e o que o service mandou fazer.
	celulasGravadas  []CelulaGravada
	gradeSubstituida bool
	removidos        []uuid.UUID
	gravados         []CelulaDaGrade
	produtosAchados  int
	erroDaGrade      error

	// trilha guarda o que o service auditou. É o que permite provar a
	// auditoria sem Postgres: um teste que some daqui é uma escrita que voltou
	// a acontecer sem rastro.
	trilha []audit.Evento

	travas []int64
}

func (r *repoFalso) PropriedadePadrao(context.Context) (uuid.UUID, error) {
	return r.propriedade, nil
}

func (r *repoFalso) BuscarTabela(_ context.Context, _ uuid.UUID, id uuid.UUID) (TabelaDeTarifas, error) {
	t, ok := r.tabelas[id]
	if !ok {
		return TabelaDeTarifas{}, apperr.NotFound("Tabela de tarifas")
	}
	return t, nil
}

// Os cadastros do falso são um mapa por tabela. Criar devolve um id novo e
// guarda a linha; buscar devolve o que está lá ou o 404 do contrato — o
// bastante para os testes que exercitam a ORDEM das chamadas do service
// (ler o `antes`, escrever, reler, auditar).

func (r *repoFalso) BuscarTarifa(_ context.Context, _ uuid.UUID, id uuid.UUID) (Tarifa, error) {
	t, ok := r.tarifas[id]
	if !ok {
		return Tarifa{}, apperr.NotFound("Tarifa")
	}
	return t, nil
}

func (r *repoFalso) CriarTarifa(_ context.Context, _ uuid.UUID, e TarifaEntrada) (uuid.UUID, error) {
	id := uuid.New()
	r.tarifas[id] = Tarifa{ID: id, TabelaID: e.TabelaID, ProdutoID: e.ProdutoID, TipoDeData: e.TipoDeData, ValorCents: e.ValorCents}
	r.criados["rates"] = id
	return id, nil
}

func (r *repoFalso) AtualizarTarifa(_ context.Context, _ uuid.UUID, id uuid.UUID, a TarifaAtualizar) error {
	t, ok := r.tarifas[id]
	if !ok {
		return apperr.NotFound("Tarifa")
	}
	if v, definido := a.ValorCents.Definido(); definido {
		t.ValorCents = v
	}
	r.tarifas[id] = t
	return nil
}

func (r *repoFalso) ExcluirTarifa(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	if _, ok := r.tarifas[id]; !ok {
		return apperr.NotFound("Tarifa")
	}
	delete(r.tarifas, id)
	return nil
}

func (r *repoFalso) BuscarFeriado(_ context.Context, _ uuid.UUID, id uuid.UUID) (Feriado, error) {
	f, ok := r.feriados[id]
	if !ok {
		return Feriado{}, apperr.NotFound("Feriado")
	}
	return f, nil
}

func (r *repoFalso) ExcluirFeriado(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	if _, ok := r.feriados[id]; !ok {
		return apperr.NotFound("Feriado")
	}
	delete(r.feriados, id)
	return nil
}

func (r *repoFalso) BuscarPeriodo(_ context.Context, _ uuid.UUID, id uuid.UUID) (PeriodoEspecial, error) {
	p, ok := r.periodos[id]
	if !ok {
		return PeriodoEspecial{}, apperr.NotFound("Período especial")
	}
	return p, nil
}

func (r *repoFalso) ExcluirPeriodo(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	if _, ok := r.periodos[id]; !ok {
		return apperr.NotFound("Período especial")
	}
	delete(r.periodos, id)
	return nil
}

func (r *repoFalso) BuscarMinimo(_ context.Context, _ uuid.UUID, id uuid.UUID) (MinimoDeNoites, error) {
	m, ok := r.minimos[id]
	if !ok {
		return MinimoDeNoites{}, apperr.NotFound("Mínimo de noites")
	}
	return m, nil
}

func (r *repoFalso) ExcluirMinimo(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	if _, ok := r.minimos[id]; !ok {
		return apperr.NotFound("Mínimo de noites")
	}
	delete(r.minimos, id)
	return nil
}

func (r *repoFalso) ContarTabelasVigentes(_ context.Context, _ uuid.UUID, exceto *uuid.UUID) (int, error) {
	if exceto == nil {
		return r.vigentesTotal, nil
	}
	return r.vigentesRestantes, nil
}

func (r *repoFalso) DesativarTabela(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	r.desativadas = append(r.desativadas, id)
	return nil
}

func (r *repoFalso) CodigosDosProdutos(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	achados := r.produtosAchados
	if achados < 0 {
		achados = len(ids)
	}
	for i, id := range ids {
		if i >= achados {
			break
		}
		out[id] = "produto-" + id.String()[:8]
	}
	return out, nil
}

func (r *repoFalso) CelulasDoEscopo(_ context.Context, _ uuid.UUID, escopo []uuid.UUID) ([]CelulaGravada, error) {
	noEscopo := map[uuid.UUID]bool{}
	for _, id := range escopo {
		noEscopo[id] = true
	}
	out := []CelulaGravada{}
	for _, c := range r.celulasGravadas {
		if noEscopo[c.ProdutoID] {
			out = append(out, c)
		}
	}
	return out, nil
}

func (r *repoFalso) AplicarGrade(_ context.Context, _ uuid.UUID, remover []uuid.UUID, gravar []CelulaDaGrade) error {
	r.gradeSubstituida = true
	r.removidos = remover
	r.gravados = gravar
	return r.erroDaGrade
}

func (r *repoFalso) AuditarCriacao(_ context.Context, entidade, verbo string, id uuid.UUID, depois any) error {
	r.trilha = append(r.trilha, audit.Evento{
		Acao: audit.Acao(entidade, verbo), Entidade: entidade, EntidadeID: id,
		Depois: audit.Snapshot(depois),
	})
	return nil
}

func (r *repoFalso) AuditarAlteracao(_ context.Context, entidade, verbo string, id uuid.UUID, antes, depois any) error {
	a, d := audit.Diff(audit.Snapshot(antes), audit.Snapshot(depois))
	r.trilha = append(r.trilha, audit.Evento{
		Acao: audit.Acao(entidade, verbo), Entidade: entidade, EntidadeID: id, Antes: a, Depois: d,
	})
	return nil
}

func (r *repoFalso) AuditarExclusao(_ context.Context, entidade, verbo string, id uuid.UUID, antes any) error {
	r.trilha = append(r.trilha, audit.Evento{
		Acao: audit.Acao(entidade, verbo), Entidade: entidade, EntidadeID: id,
		Antes: audit.Snapshot(antes),
	})
	return nil
}

func (r *repoFalso) AuditarEvento(_ context.Context, ev audit.Evento) error {
	r.trilha = append(r.trilha, ev)
	return nil
}

// acoesAuditadas devolve os `action` gravados, na ordem.
func (r *repoFalso) acoesAuditadas() []string {
	out := make([]string, 0, len(r.trilha))
	for _, ev := range r.trilha {
		out = append(out, ev.Acao)
	}
	return out
}

func (r *repoFalso) GradeDaTabela(context.Context, uuid.UUID, uuid.UUID) ([]Tarifa, error) {
	return []Tarifa{}, nil
}

func (r *repoFalso) travarPublicacao(_ context.Context, chave int64) error {
	r.travas = append(r.travas, chave)
	return nil
}

func (r *repoFalso) UltimaPoliticaComercial(context.Context, uuid.UUID) (PoliticaComercial, bool, error) {
	if len(r.politicas) == 0 {
		return PoliticaComercial{}, false, nil
	}
	return r.politicas[len(r.politicas)-1], true, nil
}

func (r *repoFalso) PublicarPoliticaComercial(_ context.Context, _ uuid.UUID, e PoliticaComercialEntrada, herdado PoliticaComercial) (int, error) {
	r.publicacoes++
	versao := len(r.politicas) + 1
	r.politicas = append(r.politicas, PoliticaComercial{
		Versao:               versao,
		SinalPct:             *e.SinalPct,
		SaldoDiasAntes:       *e.SaldoDiasAntes,
		HoldHoras:            *e.HoldHoras,
		DescontoAutoPct:      *e.DescontoAutoPct,
		DescontoAprovacaoPct: *e.DescontoAprovacaoPct,
		CaucaoDeEventoCents:  e.CaucaoDeEventoCents.Ou(herdado.CaucaoDeEventoCents),
		ExtensaoDeHoldHoras:  e.ExtensaoDeHoldHoras.Ou(herdado.ExtensaoDeHoldHoras),
		ExtensoesDeHoldMax:   e.ExtensoesDeHoldMax.Ou(herdado.ExtensoesDeHoldMax),
		ValidoDe:             *e.ValidoDe,
	})
	return versao, nil
}

func (r *repoFalso) BuscarPoliticaComercialPorVersao(_ context.Context, _ uuid.UUID, versao int) (PoliticaComercial, error) {
	for _, p := range r.politicas {
		if p.Versao == versao {
			return p, nil
		}
	}
	return PoliticaComercial{}, apperr.NotFound("Política comercial")
}

// txFalso conta commit e rollback. O `Do` real desfaz o trabalho quando fn
// devolve erro; aqui o que se prova é que o service DEVOLVE erro nos casos em
// que o trabalho não pode ficar pela metade.
type txFalso struct{ commits, rollbacks int }

func (t *txFalso) Do(ctx context.Context, fn func(context.Context) error) error {
	if err := fn(ctx); err != nil {
		t.rollbacks++
		return err
	}
	t.commits++
	return nil
}

func data(t *testing.T, s string) *Data {
	t.Helper()
	d, err := calendar.Parse(s)
	if err != nil {
		t.Fatalf("data de teste inválida %q: %v", s, err)
	}
	return &Data{d}
}

func politicaValida(t *testing.T, validoDe string) PoliticaComercialEntrada {
	t.Helper()
	sinal, saldo, hold := 50.0, 7, 48
	auto, aprovacao := 5.0, 10.0
	return PoliticaComercialEntrada{
		SinalPct: &sinal, SaldoDiasAntes: &saldo, HoldHoras: &hold,
		DescontoAutoPct: &auto, DescontoAprovacaoPct: &aprovacao,
		ValidoDe: data(t, validoDe),
	}
}

// optDe monta um Opt "definido e não nulo" — o que o decoder produz para um
// campo presente com valor.
func optDe[T any](v T) httpx.Opt[T] { return httpx.Opt[T]{Set: true, Valid: true, Value: v} }

func codigo(err error) string {
	var e *apperr.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// ─────────────────────────── Testes ─────────────────────────────────────────

// A regra 7 do CLAUDE.md aplicada à política: publicar NUNCA reescreve a versão
// anterior, porque as reservas emitidas apontam para o número dela.
func TestPublicarPoliticaCriaVersaoNovaSemTocarNaAnterior(t *testing.T) {
	repo := &repoFalso{propriedade: uuid.New(), produtosAchados: -1}
	svc := NovoService(repo, &txFalso{})
	ctx := context.Background()

	primeira, err := svc.PublicarPoliticaComercial(ctx, politicaValida(t, "2026-01-01"))
	if err != nil {
		t.Fatalf("publicando a primeira: %v", err)
	}
	if primeira.Versao != 1 {
		t.Fatalf("primeira versão = %d, esperado 1", primeira.Versao)
	}

	segunda := politicaValida(t, "2026-06-01")
	novoSinal := 30.0
	segunda.SinalPct = &novoSinal

	publicada, err := svc.PublicarPoliticaComercial(ctx, segunda)
	if err != nil {
		t.Fatalf("publicando a segunda: %v", err)
	}
	if publicada.Versao != 2 {
		t.Fatalf("segunda versão = %d, esperado 2", publicada.Versao)
	}

	// A versão 1 continua legível e com o valor ORIGINAL: é o que a reserva que
	// congelou `policy_version = 1` precisa encontrar amanhã.
	antiga, err := svc.PoliticaComercial(ctx, &primeira.Versao)
	if err != nil {
		t.Fatalf("relendo a versão 1: %v", err)
	}
	if antiga.SinalPct != 50 {
		t.Fatalf("a versão 1 foi reescrita: sinal = %v, esperado 50", antiga.SinalPct)
	}
	if len(repo.politicas) != 2 {
		t.Fatalf("gravadas %d políticas, esperado 2 — publicar editou em vez de acrescentar", len(repo.politicas))
	}
}

// Antedatar é reescrever o passado por outro caminho: a versão nova passaria a
// valer num intervalo que já foi decidido pela anterior.
func TestPublicarPoliticaRecusaAntedatar(t *testing.T) {
	repo := &repoFalso{propriedade: uuid.New(), produtosAchados: -1}
	svc := NovoService(repo, &txFalso{})
	ctx := context.Background()

	if _, err := svc.PublicarPoliticaComercial(ctx, politicaValida(t, "2026-06-01")); err != nil {
		t.Fatalf("publicando a primeira: %v", err)
	}

	_, err := svc.PublicarPoliticaComercial(ctx, politicaValida(t, "2026-01-01"))
	if got := codigo(err); got != "POLICY_IMMUTABLE" {
		t.Fatalf("código = %q, esperado POLICY_IMMUTABLE (erro: %v)", got, err)
	}
	if len(repo.politicas) != 1 {
		t.Fatalf("a política antedatada foi gravada mesmo assim (%d versões)", len(repo.politicas))
	}
}

// Publicar sob trava: sem ela, duas publicações simultâneas leem o mesmo
// `max(version)` e a segunda morre no UNIQUE.
func TestPublicarPoliticaPegaATrava(t *testing.T) {
	repo := &repoFalso{propriedade: uuid.New(), produtosAchados: -1}
	svc := NovoService(repo, &txFalso{})

	if _, err := svc.PublicarPoliticaComercial(context.Background(), politicaValida(t, "2026-01-01")); err != nil {
		t.Fatalf("publicando: %v", err)
	}
	if len(repo.travas) != 1 || repo.travas[0] != ChaveDaTravaDePoliticaComercial {
		t.Fatalf("travas pegas = %v, esperado [%d]", repo.travas, ChaveDaTravaDePoliticaComercial)
	}
}

// O bulk é tudo-ou-nada: um produto inválido derruba a requisição ANTES de
// qualquer DELETE. Metade da grade no preço novo é pior do que não ter salvado.
func TestGradeComProdutoDesconhecidoNaoApagaNada(t *testing.T) {
	tabela := uuid.UUID(uuid.New())
	repo := &repoFalso{
		propriedade:     uuid.New(),
		tabelas:         map[uuid.UUID]TabelaDeTarifas{tabela: {ID: tabela, Ativa: true}},
		produtosAchados: 0, // nenhum dos produtos pedidos existe nesta propriedade
	}
	tx := &txFalso{}
	svc := NovoService(repo, tx)

	_, _, err := svc.SalvarGrade(context.Background(), GradeEntrada{
		TabelaID: tabela,
		Celulas:  []CelulaDaGrade{{ProdutoID: uuid.New(), TipoDeData: "normal", ValorCents: 85000}},
	})
	if codigo(err) != "VALIDATION_ERROR" {
		t.Fatalf("código = %q, esperado VALIDATION_ERROR (erro: %v)", codigo(err), err)
	}
	if repo.gradeSubstituida {
		t.Fatal("a grade foi apagada antes de a validação recusar o produto")
	}
	if tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("commits=%d rollbacks=%d, esperado 0/1", tx.commits, tx.rollbacks)
	}
}

// Falha DEPOIS do DELETE tem de abortar a transação inteira: é a transação que
// devolve a grade antiga, e o service não pode engolir o erro.
func TestGradeQueFalhaNoMeioAbortaATransacao(t *testing.T) {
	tabela := uuid.New()
	repo := &repoFalso{
		propriedade:     uuid.New(),
		tabelas:         map[uuid.UUID]TabelaDeTarifas{tabela: {ID: tabela, Ativa: true}},
		produtosAchados: -1,
		erroDaGrade:     errors.New("falha do banco no meio do INSERT"),
	}
	tx := &txFalso{}
	svc := NovoService(repo, tx)

	if _, _, err := svc.SalvarGrade(context.Background(), GradeEntrada{
		TabelaID: tabela,
		Celulas:  []CelulaDaGrade{{ProdutoID: uuid.New(), TipoDeData: "normal", ValorCents: 85000}},
	}); err == nil {
		t.Fatal("a falha no meio da gravação foi engolida")
	}
	if tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("commits=%d rollbacks=%d, esperado 0/1", tx.commits, tx.rollbacks)
	}
}

// Desativar a última tabela vigente pararia a venda: todo orçamento passaria a
// sair como RATE_NOT_FOUND.
func TestDesativarUltimaTabelaVigenteRecusa(t *testing.T) {
	id := uuid.New()
	repo := &repoFalso{
		propriedade:       uuid.New(),
		tabelas:           map[uuid.UUID]TabelaDeTarifas{id: {ID: id, Ativa: true}},
		vigentesTotal:     1,
		vigentesRestantes: 0,
		produtosAchados:   -1,
	}
	svc := NovoService(repo, &txFalso{})

	err := svc.DesativarTabela(context.Background(), id)
	if got := codigo(err); got != "RESOURCE_IN_USE" {
		t.Fatalf("código = %q, esperado RESOURCE_IN_USE (erro: %v)", got, err)
	}
	if len(repo.desativadas) != 0 {
		t.Fatal("a tabela foi desativada mesmo assim")
	}
}

func TestDesativarTabelaQuandoHaOutraVigentePassa(t *testing.T) {
	id := uuid.New()
	repo := &repoFalso{
		propriedade:       uuid.New(),
		tabelas:           map[uuid.UUID]TabelaDeTarifas{id: {ID: id, Ativa: true}},
		vigentesTotal:     2,
		vigentesRestantes: 1,
		produtosAchados:   -1,
	}
	svc := NovoService(repo, &txFalso{})

	if err := svc.DesativarTabela(context.Background(), id); err != nil {
		t.Fatalf("desativando com outra vigente: %v", err)
	}
	if len(repo.desativadas) != 1 {
		t.Fatal("a tabela não foi desativada")
	}
}

// O tarifário é configuração DA PROPRIEDADE do requisitante. Aceitar
// `property_id` do cliente daria a qualquer sessão um jeito de reprecificar a
// casa do vizinho — por isso a propriedade vem do contexto autenticado.
func TestPropriedadeVemDoUsuarioAutenticado(t *testing.T) {
	daSessao := uuid.New()
	repo := &repoFalso{propriedade: uuid.New(), produtosAchados: -1}
	svc := NovoService(repo, &txFalso{})

	ctx := auth.WithUser(context.Background(), &auth.Usuario{ID: uuid.New(), PropertyID: daSessao})
	resolvida, err := svc.propriedade(ctx)
	if err != nil {
		t.Fatalf("resolvendo a propriedade: %v", err)
	}
	if resolvida != daSessao {
		t.Fatalf("propriedade = %s, esperada a da sessão (%s)", resolvida, daSessao)
	}

	// Sem sessão (seed, tarefa de manutenção) cai na propriedade padrão.
	semSessao, err := svc.propriedade(context.Background())
	if err != nil {
		t.Fatalf("resolvendo sem sessão: %v", err)
	}
	if semSessao != repo.propriedade {
		t.Fatalf("sem sessão a propriedade deveria ser a padrão")
	}
}
